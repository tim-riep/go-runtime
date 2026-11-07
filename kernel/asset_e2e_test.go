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
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// TestAssetLifecycle installs the examples with a large file added to
// TRFIN, reads assets in several ways, upgrades TRFIN and removes all
// packages again.
func TestAssetLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		fin := testutil.CopyExample(t, "TRFIN")
		big := bytes.Repeat([]byte("0123456789abcdef"), 40000) // 640000 bytes, three chunks
		testutil.WriteFile(t, fin, "assets/web/data/report.bin", big)
		pkgs := []*model.Package{testutil.Example(t, "TRBASE"), testutil.LoadDir(t, fin), testutil.Example(t, "TROTTO")}
		if _, err := k.Install(ctx, pkgs, kernel.Options{}); err != nil {
			t.Fatal(err)
		}

		// Same folder name in two packages: two repositories.
		baseCSS, _ := readAsset(t, k, "TRBASE", "web", "css/base.css", model.None[string]())
		finCSS, _ := readAsset(t, k, "TRFIN", "web", "css/finance.css", model.None[string]())
		if !bytes.Equal(baseCSS, exampleFile(t, "TRBASE", "web/css/base.css")) || !bytes.Equal(finCSS, exampleFile(t, "TRFIN", "web/css/finance.css")) {
			t.Fatal("contents differ from the package files")
		}
		if ok, _ := k.AssetExists(ctx, "TRBASE", "web", "css/finance.css"); ok {
			t.Fatal("repositories web of TRBASE and TRFIN are mixed up")
		}

		// With and without language, with fallback.
		de, info := readAsset(t, k, "TRFIN", "templates", "invoice/invoice.xsl", model.Some("de"))
		if !bytes.Equal(de, exampleFile(t, "TRFIN", "templates/invoice/invoice_de.xsl")) || info.Language != model.Some("de") {
			t.Fatalf("German version: %+v", info)
		}
		pl, info := readAsset(t, k, "TRFIN", "templates", "invoice/invoice.xsl", model.Some("pl"))
		if !bytes.Equal(pl, exampleFile(t, "TRFIN", "templates/invoice/invoice_en.xsl")) || info.Language != model.Some("en") {
			t.Fatalf("fallback to English: %+v", info)
		}
		neutral, info := readAsset(t, k, "TRFIN", "templates", "letter/letter.xsl", model.Some("fr"))
		if !bytes.Equal(neutral, exampleFile(t, "TRFIN", "templates/letter/letter.xsl")) || info.Language.Valid() {
			t.Fatalf("fallback to the language-independent version: %+v", info)
		}

		// Streaming a file larger than one chunk with a small buffer.
		r, err := k.OpenAsset(ctx, "TRFIN", "web", "data/report.bin", model.None[string]())
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		n, err := io.CopyBuffer(h, struct{ io.Reader }{r}, make([]byte, 4096))
		r.Close()
		want := sha256.Sum256(big)
		if err != nil || n != int64(len(big)) || !bytes.Equal(h.Sum(nil), want[:]) {
			t.Fatalf("streaming: %d bytes, %v", n, err)
		}

		// A repository as file system.
		fsys, err := k.AssetFS(ctx, "TRFIN", "templates", model.Some("de"))
		if err != nil {
			t.Fatal(err)
		}
		var files []string
		if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, p)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(files, []string{"invoice/invoice.xsl", "letter/letter.xsl"}) {
			t.Fatalf("files: %v", files)
		}
		if b, _ := fs.ReadFile(fsys, "letter/letter.xsl"); !bytes.Equal(b, exampleFile(t, "TRFIN", "templates/letter/letter_de.xsl")) {
			t.Fatal("file system returned the wrong version")
		}

		// Upgrade: changed, new and removed asset, new repository.
		testutil.WriteFile(t, fin, "assets/web/css/finance.css", []byte(".amount { color: black; }\n"))
		testutil.WriteFile(t, fin, "assets/web/js/app.js", []byte("console.log('finance');\n"))
		if err := os.Remove(filepath.Join(fin, "assets/web/data/report.bin")); err != nil {
			t.Fatal(err)
		}
		testutil.WriteFile(t, fin, "assets/mail/asset.json", []byte(`{"description":{"en":"Mail templates","de":"E-Mail-Vorlagen"}}`))
		testutil.WriteFile(t, fin, "assets/mail/welcome.html", []byte("<p>Welcome</p>\n"))
		next := testutil.LoadDir(t, fin)
		next.Manifest.Version = "1.1.0"
		if _, err := k.Upgrade(ctx, next, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if b, _ := readAsset(t, k, "TRFIN", "web", "css/finance.css", model.None[string]()); string(b) != ".amount { color: black; }\n" {
			t.Fatalf("changed asset: %q", b)
		}
		if b, _ := readAsset(t, k, "TRFIN", "web", "js/app.js", model.None[string]()); string(b) != "console.log('finance');\n" {
			t.Fatalf("new asset: %q", b)
		}
		if _, err := k.OpenAsset(ctx, "TRFIN", "web", "data/report.bin", model.None[string]()); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("removed asset still readable: %v", err)
		}
		if b, _ := readAsset(t, k, "TRFIN", "mail", "welcome.html", model.None[string]()); string(b) != "<p>Welcome</p>\n" {
			t.Fatalf("new repository: %q", b)
		}
		s, _ := k.Schema(ctx)
		if n := count(t, k, tBlobs); n != len(s.AssetHashes()) {
			t.Fatalf("%d blobs for %d referenced contents", n, len(s.AssetHashes()))
		}
		consistent(t, k)

		// Uninstalling removes every repository and content.
		for _, id := range []model.PackageID{"TROTTO", "TRFIN", "TRBASE"} {
			if _, err := k.Uninstall(ctx, id, kernel.Options{Confirm: true}); err != nil {
				t.Fatal(err)
			}
		}
		if repos, _ := k.AssetRepositories(ctx, ""); len(repos) != 0 {
			t.Fatalf("repositories left: %+v", repos)
		}
		if b, c := count(t, k, tBlobs), count(t, k, tChunks); b != 0 || c != 0 {
			t.Fatalf("contents left: %d blobs, %d chunks", b, c)
		}
		consistent(t, k)
	})
}
