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

package natives

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/internal/archive"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// The exceptions of archives.
const (
	errArchive        = "/TRBASE/ARCHIVE_ERROR"
	errCorrupt        = "/TRBASE/ARCHIVE_CORRUPT"
	errLimit          = "/TRBASE/ARCHIVE_LIMIT_EXCEEDED"
	errEntryNotFound  = "/TRBASE/ZIP_ENTRY_NOT_FOUND"
	errNameInvalid    = "/TRBASE/ZIP_NAME_INVALID"
	errEntryEncrypted = "/TRBASE/ZIP_ENTRY_ENCRYPTED"
)

// archiveErr turns errors of unpacking into exceptions; errors of the
// context stay, so that the time limit of the execution applies.
func archiveErr(err error) error {
	var le *archive.LimitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &le):
		return native.Raise(errLimit, le.Limit, le.Max)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return native.Raise(errCorrupt, err.Error())
}

func archiveLimits(c *native.Context) native.ArchiveLimits { return c.Limits.OrDefault().Archive }

// zipStart is the earliest time of the zip format, the time of entries
// added without one.
var zipStart = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// zipEntry is an entry of an archive: read from an opened archive (file)
// or added (data, packed).
type zipEntry struct {
	name      string
	kind      ZipEntryKind
	modified  model.Optional[time.Time]
	comment   string
	encrypted bool
	file      *zip.File
	data      []byte // added: the content
	packed    []byte // added: the content compressed, or as stored
	method    uint16
	crc       uint32
}

// zipArchive is the Go value of /TRBASE/ZIP_ARCHIVE.
type zipArchive struct {
	entries []*zipEntry
	byName  map[string]*zipEntry
}

const zipClass = "/TRBASE/ZIP_ARCHIVE"

func (a *zipArchive) entry(name string) (*zipEntry, error) {
	n, err := archive.Name(name, false)
	if err != nil {
		return nil, native.Raise(errNameInvalid, name, err.Error())
	}
	e := a.byName[n]
	if e == nil {
		e = a.byName[n+"/"]
	}
	if e == nil {
		return nil, native.Raise(errEntryNotFound, name)
	}
	return e, nil
}

func (a *zipArchive) add(e *zipEntry) error {
	if _, dup := a.byName[e.name]; dup {
		return native.Raise(errNameInvalid, e.name, "is in the archive already")
	}
	a.entries = append(a.entries, e)
	a.byName[e.name] = e
	return nil
}

// content unpacks an entry under the limits.
func (e *zipEntry) content(c *native.Context) ([]byte, error) {
	switch {
	case e.encrypted:
		return nil, native.Raise(errEntryEncrypted, e.name)
	case e.file == nil:
		return bytes.Clone(e.data), nil
	}
	m := archive.NewMeter(c, archiveLimits(c))
	r, err := m.OpenEntry(e.file)
	if err != nil {
		return nil, archiveErr(err)
	}
	b, err := archive.ReadAll(r)
	return b, archiveErr(err)
}

func openZip(c *native.Context, data []byte) (*zipArchive, error) {
	if err := c.CheckSize("the archive", len(data)); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, native.Raise(errCorrupt, "the data is no zip archive: "+err.Error())
	}
	m := archive.NewMeter(c, archiveLimits(c))
	a := &zipArchive{byName: map[string]*zipEntry{}}
	for _, f := range zr.File {
		if err := m.Entry(); err != nil {
			return nil, archiveErr(err)
		}
		raw := archive.DecodeName(f.Name, f.Flags&0x800 != 0)
		name, err := archive.Name(raw, f.Mode().IsDir())
		if err != nil {
			return nil, native.Raise(errNameInvalid, raw, err.Error())
		}
		e := &zipEntry{name: name, kind: ZipEntryKindFile, comment: f.Comment, encrypted: f.Flags&0x1 != 0, file: f}
		switch {
		case f.Mode()&fs.ModeSymlink != 0:
			e.kind = ZipEntryKindLink
		case len(name) > 0 && name[len(name)-1] == '/':
			e.kind = ZipEntryKindFolder
		}
		if !f.Modified.IsZero() {
			e.modified = model.Some(f.Modified.UTC())
		}
		if err := a.add(e); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// addData adds an entry with content, compressed with deflate unless
// stored.
func (a *zipArchive) addData(name string, data []byte, folder bool, modified model.Optional[time.Time], stored bool) error {
	n, err := archive.Name(name, folder)
	if err != nil {
		return native.Raise(errNameInvalid, name, err.Error())
	}
	e := &zipEntry{name: n, kind: ZipEntryKindFile, modified: modified, data: data, crc: crc32.ChecksumIEEE(data), method: zip.Deflate}
	if folder {
		e.kind, e.data, e.crc, e.method = ZipEntryKindFolder, nil, 0, zip.Store
	}
	if stored || folder {
		e.method, e.packed = zip.Store, e.data
	} else {
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		w.Write(data)
		w.Close()
		e.packed = buf.Bytes()
	}
	return a.add(e)
}

func (a *zipArchive) save() ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range a.entries {
		var h zip.FileHeader
		var raw io.Reader
		if e.file != nil {
			h = e.file.FileHeader
			r, err := e.file.OpenRaw()
			if err != nil {
				return nil, native.Raise(errCorrupt, err.Error())
			}
			raw = r
		} else {
			h = zip.FileHeader{Method: e.method, CRC32: e.crc, CompressedSize64: uint64(len(e.packed)), UncompressedSize64: uint64(len(e.data))}
			if e.kind == ZipEntryKindFolder {
				h.SetMode(fs.ModeDir | 0o755)
			} else {
				h.SetMode(0o644)
			}
			raw = bytes.NewReader(e.packed)
		}
		h.Name, h.Comment, h.NonUTF8 = e.name, e.comment, false
		h.Flags |= 0x800
		h.Modified = e.modified.Or(zipStart)
		f, err := w.CreateRaw(&h)
		if err != nil {
			return nil, native.Raise(errCorrupt, err.Error())
		}
		if _, err := io.Copy(f, raw); err != nil {
			return nil, native.Raise(errCorrupt, err.Error())
		}
	}
	if err := w.Close(); err != nil {
		return nil, native.Raise(errCorrupt, err.Error())
	}
	return buf.Bytes(), nil
}

// Close implements io.Closer for the release of the object.
func (a *zipArchive) Close() error {
	a.entries, a.byName = nil, map[string]*zipEntry{}
	return nil
}

func init() {
	const raises = " raises " + errArchive
	native.Default.RegisterClass(pkg, zipClass, (*zipArchive)(nil))
	static(zipClass, "OPEN", "(data: BYTES) returns /TRBASE/ZIP_ARCHIVE"+raises, openZip)
	static(zipClass, "CREATE", "() returns /TRBASE/ZIP_ARCHIVE", func(c *native.Context) (*zipArchive, error) {
		return &zipArchive{byName: map[string]*zipEntry{}}, nil
	})
	instance(zipClass, "SAVE", "() returns BYTES"+raises, func(c *native.Context, a *zipArchive) ([]byte, error) { return a.save() })
	native.Default.Register(native.Method{Package: pkg, Class: zipClass, Method: "CLOSE", Signature: "()", Releases: true,
		Func: func(c *native.Context, a *zipArchive) error { return nil }})
	instance(zipClass, "ENTRIES", "() returns /TRBASE/ZIP_ENTRY_ARRAY", func(c *native.Context, a *zipArchive) ([]ZipEntry, error) {
		out := make([]ZipEntry, 0, len(a.entries))
		for _, e := range a.entries {
			z := ZipEntry{Name: e.name, Kind: e.kind, ModifiedAt: e.modified, Comment: e.comment, Encrypted: e.encrypted}
			if e.file != nil {
				z.CompressedSize, z.Size, z.Crc32 = int64(e.file.CompressedSize64), int64(e.file.UncompressedSize64), int64(e.file.CRC32)
			} else {
				z.CompressedSize, z.Size, z.Crc32 = int64(len(e.packed)), int64(len(e.data)), int64(e.crc)
			}
			out = append(out, z)
		}
		return out, nil
	})
	instance(zipClass, "HAS", "(name: STRING) returns BOOL", func(c *native.Context, a *zipArchive, name string) (bool, error) {
		_, err := a.entry(name)
		return err == nil, nil
	})
	instance(zipClass, "GET", "(name: STRING) returns BYTES"+raises, func(c *native.Context, a *zipArchive, name string) ([]byte, error) {
		e, err := a.entry(name)
		if err != nil {
			return nil, err
		}
		return e.content(c)
	})
	instance(zipClass, "GET_TEXT", "(name: STRING) returns STRING"+raises, func(c *native.Context, a *zipArchive, name string) (string, error) {
		e, err := a.entry(name)
		if err != nil {
			return "", err
		}
		b, err := e.content(c)
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", native.Raise(errCorrupt, "the entry "+e.name+" is no UTF-8 text")
		}
		return string(b), nil
	})
	instance(zipClass, "ADD", "(name: STRING, data: BYTES)"+raises, func(c *native.Context, a *zipArchive, name string, data []byte) error {
		return a.addData(name, data, false, model.None[time.Time](), false)
	})
	instance(zipClass, "ADD_TEXT", "(name: STRING, text: STRING)"+raises, func(c *native.Context, a *zipArchive, name, text string) error {
		return a.addData(name, []byte(text), false, model.None[time.Time](), false)
	})
	instance(zipClass, "ADD_FOLDER", "(name: STRING)"+raises, func(c *native.Context, a *zipArchive, name string) error {
		return a.addData(name, nil, true, model.None[time.Time](), true)
	})
	instance(zipClass, "ADD_ENTRY", "(name: STRING, data: BYTES, modified: TIMESTAMP?, stored: BOOL)"+raises,
		func(c *native.Context, a *zipArchive, name string, data []byte, modified model.Optional[time.Time], stored bool) error {
			return a.addData(name, data, false, modified, stored)
		})
	instance(zipClass, "REMOVE", "(name: STRING)"+raises, func(c *native.Context, a *zipArchive, name string) error {
		e, err := a.entry(name)
		if err != nil {
			return err
		}
		delete(a.byName, e.name)
		for i, x := range a.entries {
			if x == e {
				a.entries = append(a.entries[:i], a.entries[i+1:]...)
				break
			}
		}
		return nil
	})
	instance("/TRBASE/HTTP_RESPONSE", "SEND_ZIP_ENTRY", "(archive: /TRBASE/ZIP_ARCHIVE, name: STRING)"+raises,
		func(c *native.Context, resp *HTTPResponse, a *zipArchive, name string) error {
			e, err := a.entry(name)
			if err != nil {
				return err
			}
			if e.encrypted {
				return native.Raise(errEntryEncrypted, e.name)
			}
			limits := archiveLimits(c)
			resp.Body.Reset()
			resp.Serve = func(ctx context.Context, w http.ResponseWriter) int64 {
				var r io.Reader = bytes.NewReader(e.data)
				if e.file != nil {
					m := archive.NewMeter(ctx, limits)
					var err error
					if r, err = m.OpenEntry(e.file); err != nil {
						slog.Warn("sending a zip entry", "entry", e.name, "error", err)
						return 0
					}
				}
				w.WriteHeader(resp.Status)
				n, err := io.Copy(w, r)
				if err != nil {
					slog.Warn("sending a zip entry", "entry", e.name, "error", err)
				}
				return n
			}
			return nil
		})
}

// gzipData decompresses gzip data with all its members.
func gzipData(c *native.Context, data []byte) ([]byte, error) {
	if err := c.CheckSize("the gzip data", len(data)); err != nil {
		return nil, err
	}
	m := archive.NewMeter(c, archiveLimits(c))
	zr, err := gzip.NewReader(m.Packed(bytes.NewReader(data)))
	if err != nil {
		return nil, native.Raise(errCorrupt, "the data is no gzip data: "+err.Error())
	}
	b, err := archive.ReadAll(m.Wrap(zr))
	return b, archiveErr(err)
}

func gzipCompress(data []byte, level int, name model.Optional[string]) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, native.Raise(errArchive, "the level must be 1 to 9")
	}
	w.Name = name.Or("")
	w.Write(data)
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func init() {
	const g, raises = "/TRBASE/GZIP", " raises " + errArchive
	static(g, "COMPRESS", "(data: BYTES) returns BYTES"+raises, func(c *native.Context, data []byte) ([]byte, error) {
		return gzipCompress(data, gzip.DefaultCompression, model.None[string]())
	})
	static(g, "COMPRESS_WITH", "(data: BYTES, level: INT4, name: STRING?) returns BYTES"+raises,
		func(c *native.Context, data []byte, level int32, name model.Optional[string]) ([]byte, error) {
			if level < 1 || level > 9 {
				return nil, native.Raise(errArchive, "the level must be 1 to 9")
			}
			return gzipCompress(data, int(level), name)
		})
	static(g, "DECOMPRESS", "(data: BYTES) returns BYTES"+raises, gzipData)
	static(g, "DECOMPRESS_TEXT", "(data: BYTES) returns STRING"+raises, func(c *native.Context, data []byte) (string, error) {
		b, err := gzipData(c, data)
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", native.Raise(errCorrupt, "the data is no UTF-8 text")
		}
		return string(b), nil
	})
	static(g, "IS_GZIP", "(data: BYTES) returns BOOL", func(c *native.Context, data []byte) (bool, error) {
		return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b, nil
	})
}
