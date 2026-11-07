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

package pkgload

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func assetFS() fstest.MapFS {
	fsys := validFS()
	fsys["assets/web/asset.json"] = file(`{"description":{"en":"Web"},
		"folders":{"empty":{"description":{"en":"Nothing yet"}}},
		"files":{"images/logo.png":{"mimeType":"image/x-custom"},
		         "tpl/mail.html":{"languages":{"en":"tpl/mail_en.html","de":"tpl/mail_de.html"}}}}`)
	fsys["assets/web/css/app.css"] = file("body{}")
	fsys["assets/web/images/logo.png"] = file("png")
	fsys["assets/web/tpl/mail.html"] = file("neutral")
	fsys["assets/web/tpl/mail_en.html"] = file("english")
	fsys["assets/web/tpl/mail_de.html"] = file("deutsch")
	fsys["assets/web/data/blob.bin"] = file("x")
	fsys["assets/print/asset.json"] = file(`{"description":{"en":"Print"}}`)
	fsys["assets/print/sub/asset.json"] = file(`{}`)
	return fsys
}

func repo(t *testing.T, pkg *model.Package, id string) *model.AssetRepository {
	t.Helper()
	for i := range pkg.AssetRepositories {
		if pkg.AssetRepositories[i].ID == id {
			return &pkg.AssetRepositories[i]
		}
	}
	t.Fatalf("no repository %s", id)
	return nil
}

func TestLoadAssets(t *testing.T) {
	pkg, err := Load(assetFS(), "mem")
	if err != nil {
		t.Fatal(err)
	}
	web := repo(t, pkg, "web")
	if web.Package != "TRX" || web.Description["en"] != "Web" {
		t.Fatalf("repository: %+v", web)
	}
	want := []string{"css", "css/app.css", "data", "data/blob.bin", "empty", "images", "images/logo.png", "tpl", "tpl/mail.html"}
	if got := web.Paths(); !equal(got, want) {
		t.Fatalf("paths %v, want %v", got, want)
	}
	if e := web.Entries["empty"]; !e.Folder || e.Description["en"] != "Nothing yet" {
		t.Fatalf("declared folder: %+v", e)
	}
	if e := web.Entries["css/app.css"]; e.MimeType != "text/css" || e.Content.Size != 6 {
		t.Fatalf("css: %+v", e)
	}
	sum := sha256.Sum256([]byte("body{}"))
	if web.Entries["css/app.css"].Content.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("hash")
	}
	if e := web.Entries["images/logo.png"]; e.MimeType != "image/x-custom" {
		t.Fatalf("mime override: %+v", e)
	}
	if e := web.Entries["data/blob.bin"]; e.MimeType != "application/octet-stream" {
		t.Fatalf("unknown extension: %+v", e)
	}
	mail := web.Entries["tpl/mail.html"]
	if mail.Content == nil || mail.Content.Source != "tpl/mail.html" || len(mail.Variants) != 2 || mail.Variants["de"].Source != "tpl/mail_de.html" {
		t.Fatalf("variants: %+v", mail)
	}
	r, err := mail.Variants["de"].Open()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "deutsch" {
		t.Fatalf("variant content %q", b)
	}
	print := repo(t, pkg, "print")
	if print.Entries["sub/asset.json"] == nil {
		t.Fatal("asset.json below the root is an ordinary asset")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLoadAssetErrors(t *testing.T) {
	cases := map[string]struct {
		edit    func(fsys fstest.MapFS)
		file    string
		pointer string
	}{
		"file directly below assets": {func(f fstest.MapFS) { f["assets/logo.png"] = file("x") }, "assets/logo.png", ""},
		"folder without asset.json":  {func(f fstest.MapFS) { f["assets/fonts/a.woff2"] = file("x") }, "assets/fonts", ""},
		"missing description":        {func(f fstest.MapFS) { f["assets/print/asset.json"] = file(`{}`) }, "assets/print/asset.json", ""},
		"description without English": {func(f fstest.MapFS) {
			f["assets/print/asset.json"] = file(`{"description":{"de":"Druck"}}`)
		}, "assets/print/asset.json", "/description"},
		"variant file missing":    {func(f fstest.MapFS) { delete(f, "assets/web/tpl/mail_de.html") }, "assets/web/asset.json", "/files/tpl~1mail.html/languages/de"},
		"file entry without file": {func(f fstest.MapFS) { delete(f, "assets/web/images/logo.png") }, "assets/web/asset.json", "/files/images~1logo.png"},
		"invalid language code": {func(f fstest.MapFS) {
			f["assets/print/asset.json"] = file(`{"description":{"en":"P"},"files":{"x.txt":{"languages":{"EN":"y.txt"}}}}`)
		}, "assets/print/asset.json", ""}, // propertyNames errors point at the document
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := assetFS()
			c.edit(fsys)
			_, err := Load(fsys, "mem")
			for _, v := range rterr.All[*rterr.ValidationError](err) {
				if v.Location.File == c.file && v.Location.Pointer == c.pointer {
					return
				}
			}
			t.Fatalf("no error at %s%s in:\n%v", c.file, c.pointer, err)
		})
	}
}

func TestLoadRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	for p, f := range assetFS() {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "assets/web/css/hosts.css")); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	_, err := Load(os.DirFS(dir), dir)
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Location.File == "assets/web/css/hosts.css" {
			return
		}
	}
	t.Fatalf("symlink not rejected: %v", err)
}
