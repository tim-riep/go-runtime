// Copyright 2026 The go-runtime Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package archive unpacks zip and gzip data under limits: sizes and the
// ratio of unpacked to packed bytes are measured while unpacking, never
// taken from the headers, which may lie. It also checks and decodes the
// names of zip entries.
package archive

import (
	"archive/zip"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/native"
)

// LimitError reports an exceeded limit.
type LimitError struct {
	Limit string // e.g. "total size"
	Max   int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("the %s exceeds the limit of %d", e.Limit, e.Max)
}

// ErrCorrupt reports data that cannot be unpacked.
var ErrCorrupt = errors.New("archive: the data is corrupt")

// Meter measures the bytes of one archive: what was read packed and what
// was unpacked, over all entries.
type Meter struct {
	ctx      context.Context
	l        native.ArchiveLimits
	packed   int64
	unpacked int64
	entries  int
}

// NewMeter returns a meter for one archive.
func NewMeter(ctx context.Context, l native.ArchiveLimits) *Meter {
	return &Meter{ctx: ctx, l: l}
}

// Entry counts an entry.
func (m *Meter) Entry() error {
	if m.entries++; m.entries > m.l.Entries {
		return &LimitError{Limit: "number of entries", Max: int64(m.l.Entries)}
	}
	return nil
}

// Unpacked returns the bytes unpacked so far.
func (m *Meter) Unpacked() int64 { return m.unpacked }

type packedReader struct {
	m *Meter
	r io.Reader
}

func (p *packedReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.m.packed += int64(n)
	return n, err
}

type unpackedReader struct {
	m     *Meter
	r     io.Reader
	entry int64
}

func (u *unpackedReader) Read(b []byte) (int, error) {
	if err := u.m.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := u.r.Read(b)
	u.entry += int64(n)
	u.m.unpacked += int64(n)
	m := u.m
	switch {
	case u.entry > m.l.Entry:
		return n, &LimitError{Limit: "size of an entry", Max: m.l.Entry}
	case m.unpacked > m.l.Total:
		return n, &LimitError{Limit: "total unpacked size", Max: m.l.Total}
	case m.unpacked > m.l.RatioFrom && m.unpacked > m.l.Ratio*max(m.packed, 1):
		return n, &LimitError{Limit: "ratio of unpacked to packed size", Max: m.l.Ratio}
	}
	return n, err
}

// Wrap measures an unpacking reader whose packed input is counted by
// Packed.
func (m *Meter) Wrap(unpacked io.Reader) io.Reader { return &unpackedReader{m: m, r: unpacked} }

// Packed counts the packed input of an unpacking reader.
func (m *Meter) Packed(r io.Reader) io.Reader { return &packedReader{m: m, r: r} }

// ReadAll reads a measured reader; read errors other than limits and the
// context mean corrupt data.
func ReadAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	var le *LimitError
	switch {
	case err == nil, errors.As(err, &le), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return b, err
	}
	return b, fmt.Errorf("%w: %v", ErrCorrupt, err)
}

// OpenEntry opens a zip entry with its own decompressor, so that the
// packed bytes are counted as read, whatever the header claims.
func (m *Meter) OpenEntry(f *zip.File) (io.Reader, error) {
	raw, err := f.OpenRaw()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	packed := m.Packed(raw)
	var r io.Reader
	switch f.Method {
	case zip.Store:
		r = packed
	case zip.Deflate:
		r = flate.NewReader(packed)
	default:
		return nil, fmt.Errorf("%w: entry %s uses the unsupported method %d", ErrCorrupt, f.Name, f.Method)
	}
	return m.Wrap(&crcReader{r: r, want: f.CRC32, size: f.UncompressedSize64, h: newCRC()}), nil
}

// Name checks and normalizes the name of a zip entry: a leading ./ and
// repeated slashes go; absolute paths, drives, .. segments, backslashes,
// control characters and empty names are rejected. Folders end with /.
func Name(name string, folder bool) (string, error) {
	bad := func(why string) (string, error) { return "", fmt.Errorf("%q %s", name, why) }
	if !utf8.ValidString(name) {
		return bad("is no valid UTF-8")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return bad("contains a control character")
		}
	}
	switch {
	case strings.Contains(name, `\`):
		return bad("contains a backslash")
	case strings.HasPrefix(name, "/"):
		return bad("is an absolute path")
	case len(name) >= 2 && name[1] == ':':
		return bad("names a drive")
	}
	isDir := folder || strings.HasSuffix(name, "/")
	var segs []string
	for _, s := range strings.Split(name, "/") {
		switch s {
		case "", ".":
			continue
		case "..":
			return bad("leaves the archive with ..")
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		return bad("is empty")
	}
	out := strings.Join(segs, "/")
	if isDir {
		out += "/"
	}
	return out, nil
}

// DecodeName returns the name of an entry as text: UTF-8 when the entry
// says so, or when its bytes are valid UTF-8 beyond ASCII, as many tools
// write it without saying; otherwise code page 437.
func DecodeName(raw string, flagUTF8 bool) string {
	if flagUTF8 || utf8.ValidString(raw) {
		return raw
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(cp437[c-0x80])
		}
	}
	return b.String()
}

// cp437 are the characters 0x80 to 0xFF of code page 437.
var cp437 = []rune("ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜ¢£¥₧ƒáíóúñÑªº¿⌐¬½¼¡«»░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀αßΓπΣσµτΦΘΩδ∞φε∩≡±≥≤⌠⌡÷≈°∙·√ⁿ²■ ")
