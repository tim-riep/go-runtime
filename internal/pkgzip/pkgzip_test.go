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

package pkgzip_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/pkgzip"
	"github.com/tim-riep/go-runtime/rterr"
)

type entry struct {
	name string
	data string
	mode fs.FileMode
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func read(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestOpen reads packages at the root, below the top-level directory of a
// repository archive and in a directory of it.
func TestOpen(t *testing.T) {
	root := archive(t, entry{name: "manifest.json", data: "m"}, entry{name: "src/", mode: fs.ModeDir | 0o755}, entry{name: "src/A.class", data: "a"})
	fsys, err := pkgzip.Open("root.zip", root, pkgzip.DefaultLimits, pkgzip.Options{})
	if err != nil || read(t, fsys, "manifest.json") != "m" || read(t, fsys, "src/A.class") != "a" {
		t.Fatalf("root: %v", err)
	}
	// A single top-level directory without manifest at the root is removed.
	nested := archive(t, entry{name: "TRFIN/manifest.json", data: "m"}, entry{name: "TRFIN/src/A.class", data: "a"})
	if fsys, err := pkgzip.Open("nested.zip", nested, pkgzip.DefaultLimits, pkgzip.Options{}); err != nil || read(t, fsys, "manifest.json") != "m" {
		t.Fatalf("nested: %v", err)
	}
	repo := archive(t, entry{name: "tim-riep-erp-0123abc/README.md", data: "r"}, entry{name: "tim-riep-erp-0123abc/packages/TRFIN/manifest.json", data: "fin"},
		entry{name: "tim-riep-erp-0123abc/packages/TROTTO/manifest.json", data: "otto"})
	fsys, err = pkgzip.Open("repo.zip", repo, pkgzip.DefaultLimits, pkgzip.Options{StripTop: true, Dir: "packages/TROTTO"})
	if err != nil || read(t, fsys, "manifest.json") != "otto" {
		t.Fatalf("directory: %v", err)
	}
	if _, err := fs.ReadFile(fsys, "../TRFIN/manifest.json"); err == nil {
		t.Error("left the directory")
	}
	_, err = pkgzip.Open("repo.zip", repo, pkgzip.DefaultLimits, pkgzip.Options{StripTop: true, Dir: "packages/NONE"})
	expectRejected(t, err, "no directory packages/NONE")
}

func expectRejected(t *testing.T, err error, want string) {
	t.Helper()
	var ua *rterr.UnsafeArchive
	if !errors.As(err, &ua) || !strings.Contains(err.Error(), want) {
		t.Errorf("expected rejection %q, got %v", want, err)
	}
}

// TestRejected rejects entries outside the package, absolute paths,
// links, duplicates and archives beyond the limits.
func TestRejected(t *testing.T) {
	m := entry{name: "manifest.json", data: "m"}
	for _, c := range []struct {
		entries []entry
		want    string
	}{
		{[]entry{m, {name: "../evil.txt", data: "x"}}, "leaves the package"},
		{[]entry{m, {name: "src/../../evil.txt", data: "x"}}, "leaves the package"},
		{[]entry{m, {name: "/etc/passwd", data: "x"}}, "absolute path"},
		{[]entry{m, {name: "C:/x.txt", data: "x"}}, "absolute path"},
		{[]entry{m, {name: `src\..\x`, data: "x"}}, "backslash"},
		{[]entry{m, {name: "src/link", data: "/etc/passwd", mode: fs.ModeSymlink | 0o777}}, "symbolic link"},
		{[]entry{m, {name: "dev", data: "", mode: fs.ModeDevice | 0o644}}, "no regular file"},
		{[]entry{m, {name: "a.txt", data: "1"}, {name: "./a.txt", data: "2"}}, "twice"},
	} {
		_, err := pkgzip.Open("bad.zip", archive(t, c.entries...), pkgzip.DefaultLimits, pkgzip.Options{})
		expectRejected(t, err, c.want)
	}
	big := archive(t, m, entry{name: "big.txt", data: strings.Repeat("x", 2000)}, entry{name: "b.txt", data: strings.Repeat("y", 900)})
	for _, c := range []struct {
		l    pkgzip.Limits
		want string
	}{
		{pkgzip.Limits{Archive: 100, Total: 1 << 20, File: 1 << 20, Entries: 10}, "more than the limit of 100"},
		{pkgzip.Limits{Archive: 1 << 20, Total: 1 << 20, File: 1000, Entries: 10}, `"big.txt" has more than the limit of 1000 bytes`},
		{pkgzip.Limits{Archive: 1 << 20, Total: 2500, File: 1 << 20, Entries: 10}, "files have more than the limit of 2500 bytes"},
		{pkgzip.Limits{Archive: 1 << 20, Total: 1 << 20, File: 1 << 20, Entries: 2}, "3 entries, more than the limit of 2"},
	} {
		_, err := pkgzip.Open("big.zip", big, c.l, pkgzip.Options{})
		expectRejected(t, err, c.want)
	}
	_, err := pkgzip.Open("none.zip", []byte("no zip"), pkgzip.DefaultLimits, pkgzip.Options{})
	expectRejected(t, err, "no ZIP archive")
	_, err = pkgzip.Open("flat.zip", archive(t, m, entry{name: "x/y", data: "1"}), pkgzip.DefaultLimits, pkgzip.Options{StripTop: true})
	expectRejected(t, err, "no single top-level directory")
}
