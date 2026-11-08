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

package kernel_test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"hash/crc32"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

func b64(b []byte) string { return strconv.Quote(base64.StdEncoding.EncodeToString(b)) }

func js(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func archiveFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.ModuleRoot(t), "testdata", "archives", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// forged returns a zip archive with one deflated entry whose header
// claims other sizes than its content has.
func forged(t *testing.T, name string, content []byte, claimed uint64) []byte {
	t.Helper()
	var packed bytes.Buffer
	fw, _ := flate.NewWriter(&packed, flate.BestCompression)
	fw.Write(content)
	fw.Close()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(content), CompressedSize64: uint64(packed.Len()), UncompressedSize64: claimed}
	f, err := w.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(packed.Bytes())
	w.Close()
	return buf.Bytes()
}

// TestArchives reads archives of several tools and rejects names, links,
// encrypted entries and bombs with forged sizes.
func TestArchives(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "archives", "TRARCH"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		const A = "/TRARCH/ARCHIVES"
		text := func(data []byte, name, want string) {
			t.Helper()
			if got := mustCall(t, k, "", A, "TEXT", b64(data), js(name)); got != js(want) && !strings.HasPrefix(got, js(want)[:len(js(want))-1]) {
				t.Errorf("TEXT(%s) = %s, want %q", name, got, want)
			}
		}
		entries := func(data []byte) []map[string]any {
			t.Helper()
			var out []map[string]any
			if err := json.Unmarshal([]byte(mustCall(t, k, "", A, "ENTRIES", b64(data))), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		info := archiveFile(t, "infozip.zip")
		text(info, "a.txt", "text:hello")
		text(info, "empty.txt", "text:")
		text(info, "missing.txt", "missing")
		text(info, "Grüße.txt", "text:grüße") // UTF-8 without the flag
		text(info, "dir/inner.txt", "text:inner")
		text(archiveFile(t, "ditto-flat.zip"), "Grüße.txt", "text:grüße")
		text(archiveFile(t, "python.zip"), "Grüße.txt", "text:grüße")
		text(archiveFile(t, "zip64.zip"), "big.txt", "text:zip64 content")
		text(archiveFile(t, "encrypted.zip"), "a.txt", "encrypted a.txt")
		if e := entries(archiveFile(t, "encrypted.zip")); e[0]["ENCRYPTED"] != true {
			t.Errorf("encrypted: %v", e)
		}
		if e := entries(archiveFile(t, "symlink.zip")); e[1]["KIND"] != "LINK" || e[1]["NAME"] != "link" {
			t.Errorf("symbolic link: %v", e)
		}
		text(archiveFile(t, "symlink.zip"), "link", "text:a.txt") // the target as text, never followed
		if e := entries(info); e[3]["KIND"] != "FOLDER" || e[3]["NAME"] != "dir/" || e[0]["SIZE"] != float64(5) {
			t.Errorf("entries: %v", e)
		}

		// A name in code page 437 without the UTF-8 flag.
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		f, _ := w.CreateHeader(&zip.FileHeader{Name: "Gr\x81\xe1e.txt", NonUTF8: true})
		f.Write([]byte("cp437"))
		w.Close()
		text(buf.Bytes(), "Grüße.txt", "text:cp437")

		// Bad names in archives and when adding.
		for _, name := range []string{"../x", "/x", `C:\x`, "a\x01b", "a/../../x"} {
			text(forged(t, name, []byte("x"), 1), "x", "name ")
			if got := mustCall(t, k, "", A, "ADD_NAME", js(name)); got != `"rejected"` {
				t.Errorf("ADD_NAME(%q) = %s", name, got)
			}
		}
		if got := mustCall(t, k, "", A, "ADD_NAME", `"x.txt"`); got != `"rejected"` {
			t.Errorf("duplicate: %s", got)
		}
		if got := mustCall(t, k, "", A, "ADD_NAME", `".//a//b.txt"`); got != `"added a/b.txt"` {
			t.Errorf("normalized: %s", got)
		}
		// Bombs: forged sizes, and a ratio beyond the limit.
		small := native.DefaultLimits
		small.Archive.Entry, small.Archive.Ratio = 1<<20, 1<<40
		k.SetFormatLimits(small)
		text(forged(t, "bomb.txt", bytes.Repeat([]byte("a"), 2<<20), 10), "bomb.txt", "limit size of an entry")
		k.SetFormatLimits(native.Limits{})
		text(forged(t, "lie.txt", []byte("12345"), 3), "lie.txt", "corrupt ")
		text(forged(t, "zeros.txt", make([]byte, 3<<20), 3<<20), "zeros.txt", "limit ratio of unpacked to packed size")

		// gzip: several members, a wrong checksum, a bomb and no gzip at all.
		gz := func(parts ...string) []byte {
			var b bytes.Buffer
			for _, p := range parts {
				zw := gzip.NewWriter(&b)
				zw.Write([]byte(p))
				zw.Close()
			}
			return b.Bytes()
		}
		if got := mustCall(t, k, "", A, "GUNZIP", b64(gz("eins ", "zwei"))); got != `"text:eins zwei"` {
			t.Errorf("members: %s", got)
		}
		bad := gz("checksum")
		bad[len(bad)-8] ^= 0xff
		if got := mustCall(t, k, "", A, "GUNZIP", b64(bad)); !strings.HasPrefix(got, `"corrupt `) {
			t.Errorf("checksum: %s", got)
		}
		if got := mustCall(t, k, "", A, "GUNZIP", b64([]byte("plain"))); !strings.HasPrefix(got, `"corrupt the data is no gzip data`) {
			t.Errorf("no gzip: %s", got)
		}
		bomb := gz(strings.Repeat("\x00", 300<<20))
		if got := mustCall(t, k, "", A, "GUNZIP", b64(bomb)); got != `"limit ratio of unpacked to packed size"` {
			t.Errorf("gzip bomb: %s", got)
		}
		// An entry goes into the response while it is unpacked.
		if err := k.ActivateHTTPService(ctx, "/trarch"); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(k.HTTPHandler())
		defer srv.Close()
		c := client{t, srv.URL}
		if a := c.do("GET", "/trarch/entry", nil, ""); a.status != 200 || a.body != "streamed report" || a.header.Get("Content-Type") != "text/plain" {
			t.Errorf("SEND_ZIP_ENTRY: %d %q %v", a.status, a.body, a.header)
		}
		if a := c.do("GET", "/trarch/entry?name=none.txt", nil, ""); a.status != 500 {
			t.Errorf("missing entry: %d", a.status)
		}
		results, err := k.Test(ctx, "TRARCH", nil, kernel.ExecOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if !r.Passed {
				t.Errorf("%s.%s: %s (line %d)", r.Class, r.Method, r.Message, r.Line)
			}
		}
	})
}
