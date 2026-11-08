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

// Package pkgzip reads package archives in ZIP format into an in-memory
// file system. It rejects entries outside the package, links and archives
// beyond the limits; nothing of an archive is executed or written to disk.
package pkgzip

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/rterr"
)

// Limits restrict an archive.
type Limits struct {
	Archive int64 // size of the archive in bytes
	Total   int64 // sum of the sizes of all files, as read
	File    int64 // size of one file, as read
	Entries int   // number of entries, directories included
}

// DefaultLimits are the limits unless configured otherwise.
var DefaultLimits = Limits{Archive: 64 << 20, Total: 256 << 20, File: 32 << 20, Entries: 20_000}

// Options select the package in an archive.
type Options struct {
	// Dir is the directory of the package in the archive, "" for its root.
	Dir string
	// StripTop removes the single top-level directory that the archives
	// of a repository have, such as owner-repo-sha/. Without it, a single
	// top-level directory is removed only when the root has no
	// manifest.json.
	StripTop bool
}

// Open reads an archive into a file system with the package at its root.
// name names the archive in errors.
func Open(name string, data []byte, l Limits, opts Options) (fs.FS, error) {
	reject := func(entry, format string, args ...any) error {
		return &rterr.UnsafeArchive{Archive: name, Entry: entry, Reason: sprintf(format, args...)}
	}
	if int64(len(data)) > l.Archive {
		return nil, reject("", "it has %d bytes, more than the limit of %d", len(data), l.Archive)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, reject("", "it is no ZIP archive: %v", err)
	}
	if len(zr.File) > l.Entries {
		return nil, reject("", "it has %d entries, more than the limit of %d", len(zr.File), l.Entries)
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		raw := f.Name
		mode := f.Mode()
		switch {
		case strings.Contains(raw, `\`):
			return nil, reject(raw, "contains a backslash")
		case strings.HasPrefix(raw, "/") || len(raw) >= 2 && raw[1] == ':':
			return nil, reject(raw, "is an absolute path")
		case mode&fs.ModeSymlink != 0:
			return nil, reject(raw, "is a symbolic link")
		case f.Flags&0x1 != 0:
			return nil, reject(raw, "is encrypted")
		}
		for _, seg := range strings.Split(strings.TrimSuffix(raw, "/"), "/") {
			if seg == ".." {
				return nil, reject(raw, "leaves the package")
			}
		}
		if strings.HasSuffix(raw, "/") || mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return nil, reject(raw, "is no regular file")
		}
		clean := path.Clean(raw)
		if _, dup := files[clean]; dup {
			return nil, reject(raw, "is in the archive twice")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, reject(raw, "cannot be read: %v", err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, l.File+1))
		rc.Close()
		if err != nil {
			return nil, reject(raw, "cannot be read: %v", err)
		}
		if int64(len(b)) > l.File {
			return nil, reject(raw, "has more than the limit of %d bytes", l.File)
		}
		if total += int64(len(b)); total > l.Total {
			return nil, reject("", "its files have more than the limit of %d bytes", l.Total)
		}
		files[clean] = b
	}
	if _, root := files["manifest.json"]; opts.StripTop || !root && opts.Dir == "" {
		if top, ok := commonTop(files); ok {
			files = below(files, top)
		} else if opts.StripTop {
			return nil, reject("", "it has no single top-level directory")
		}
	}
	if opts.Dir != "" {
		files = below(files, path.Clean(opts.Dir))
		if len(files) == 0 {
			return nil, reject("", "it has no directory %s", opts.Dir)
		}
	}
	out := fstest.MapFS{}
	for p, b := range files {
		out[p] = &fstest.MapFile{Data: b, Mode: 0o444}
	}
	return out, nil
}

// commonTop returns the first segment that all paths share.
func commonTop(files map[string][]byte) (string, bool) {
	top := ""
	for p := range files {
		first, _, nested := strings.Cut(p, "/")
		if !nested || top != "" && first != top {
			return "", false
		}
		top = first
	}
	return top, top != ""
}

// below returns the files under dir, with paths relative to it.
func below(files map[string][]byte, dir string) map[string][]byte {
	out := map[string][]byte{}
	for p, b := range files {
		if rest, ok := strings.CutPrefix(p, dir+"/"); ok {
			out[rest] = b
		}
	}
	return out
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
