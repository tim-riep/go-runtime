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
	"encoding/hex"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func exampleFile(t *testing.T, pkg, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.ModuleRoot(t), "examples", pkg, "assets", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readAsset(t *testing.T, k *kernel.Kernel, pkg model.PackageID, repo, path, lang string) ([]byte, *kernel.AssetInfo) {
	t.Helper()
	r, err := k.OpenAsset(context.Background(), pkg, repo, path, lang)
	if err != nil {
		t.Fatalf("open %s %s %s (%s): %v", pkg, repo, path, lang, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b, r.Info()
}

func TestReadAssets(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}

		logo, info := readAsset(t, k, "TRFIN", "web", "images/logo.png", "")
		if !bytes.Equal(logo, exampleFile(t, "TRFIN", "web/images/logo.png")) {
			t.Fatal("logo differs from the package file")
		}
		sum := sha256.Sum256(logo)
		if info.MimeType != "image/png" || info.Size != int64(len(logo)) || info.ETag() != `"`+hex.EncodeToString(sum[:])+`"` || info.ModifiedAt.IsZero() {
			t.Fatalf("info: %+v", info)
		}

		// Language variants with fallback.
		for _, c := range []struct{ path, lang, want, chosen string }{
			{"invoice/invoice.xsl", "de", "invoice/invoice_de.xsl", "de"},
			{"invoice/invoice.xsl", "pl", "invoice/invoice_en.xsl", "en"},
			{"invoice/invoice.xsl", "", "invoice/invoice_en.xsl", "en"},
			{"letter/letter.xsl", "de", "letter/letter_de.xsl", "de"},
			{"letter/letter.xsl", "fr", "letter/letter.xsl", ""},
			{"letter/letter.xsl", "", "letter/letter.xsl", ""},
		} {
			b, info := readAsset(t, k, "TRFIN", "templates", c.path, c.lang)
			if !bytes.Equal(b, exampleFile(t, "TRFIN", "templates/"+c.want)) || info.Language != c.chosen || info.Source != c.want {
				t.Errorf("%s in %q: got %s (language %q)", c.path, c.lang, info.Source, info.Language)
			}
		}
		_, err := k.OpenAsset(ctx, "TRFIN", "templates", "invoice/invoice_de.xsl", "")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("variant files must not be readable under their own path: %v", err)
		}
		var nf *rterr.AssetNotFound
		if _, err := k.OpenAsset(ctx, "TRFIN", "nope", "x", ""); !errors.As(err, &nf) || nf.Path != "" {
			t.Fatalf("unknown repository: %v", err)
		}

		// Same folder name, separate repositories.
		if ok, _ := k.AssetExists(ctx, "TRBASE", "web", "css/base.css"); !ok {
			t.Fatal("base.css missing in TRBASE web")
		}
		if ok, _ := k.AssetExists(ctx, "TRFIN", "web", "css/base.css"); ok {
			t.Fatal("base.css of TRBASE visible in TRFIN web")
		}
		repos, err := k.AssetRepositories(ctx, "")
		if err != nil || len(repos) != 3 || repos[0].Package != "TRBASE" || repos[1].ID != "templates" || repos[2].ID != "web" {
			t.Fatalf("repositories: %v %+v", err, repos)
		}

		list, err := k.ListAssets(ctx, "TRFIN", "templates", "invoice")
		if err != nil || len(list) != 1 || list[0].Path != "invoice/invoice.xsl" || len(list[0].Variants) != 2 || list[0].HasNeutral {
			t.Fatalf("list: %v %+v", err, list)
		}
		top, _ := k.ListAssets(ctx, "TRFIN", "templates", "")
		if len(top) != 2 || !top[0].Folder || top[0].Description["de"] != "Rechnungen" {
			t.Fatalf("top level: %+v", top)
		}
		consistent(t, k)
	})
}

func TestAssetStreamingAndFS(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		dir := filepath.Join(t.TempDir(), "TRBIG")
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{"id":"TRBIG","version":"1.0.0"}`))
		testutil.WriteFile(t, dir, "assets/data/asset.json", []byte(`{"description":{"en":"Large files"},
			"files":{"page.html":{"languages":{"de":"page_de.html"}}}}`))
		big := make([]byte, 600<<10+123) // more than two chunks
		for i := range big {
			big[i] = byte(i*7 + i/1024)
		}
		testutil.WriteFile(t, dir, "assets/data/big.bin", big)
		testutil.WriteFile(t, dir, "assets/data/page.html", []byte(`{{define "page"}}neutral {{.}}{{end}}`))
		testutil.WriteFile(t, dir, "assets/data/page_de.html", []byte(`{{define "page"}}deutsch {{.}}{{end}}`))
		testutil.WriteFile(t, dir, "assets/data/sub/inner.txt", []byte("inner"))
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if n := count(t, k, "/RT/ASSET_CHUNKS"); n != 3+3 {
			t.Fatalf("chunks: %d", n)
		}

		got, _ := readAsset(t, k, "TRBIG", "data", "big.bin", "")
		if !bytes.Equal(got, big) {
			t.Fatal("streamed content differs")
		}
		r, err := k.OpenAsset(ctx, "TRBIG", "data", "big.bin", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Seek(300<<10, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		part := make([]byte, 1000)
		if _, err := io.ReadFull(r, part); err != nil || !bytes.Equal(part, big[300<<10:300<<10+1000]) {
			t.Fatalf("seek: %v", err)
		}
		r.Close()

		de, err := k.AssetFS(ctx, "TRBIG", "data", "de")
		if err != nil {
			t.Fatal(err)
		}
		if err := fstest.TestFS(de, "big.bin", "page.html", "sub/inner.txt"); err != nil {
			t.Fatal(err)
		}
		var files []string
		_ = fs.WalkDir(de, ".", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, p)
			}
			return err
		})
		if strings.Join(files, ",") != "big.bin,page.html,sub/inner.txt" {
			t.Fatalf("files: %v", files)
		}
		b, _ := fs.ReadFile(de, "page.html")
		if string(b) != `{{define "page"}}deutsch {{.}}{{end}}` {
			t.Fatalf("German version: %s", b)
		}
		en, _ := k.AssetFS(ctx, "TRBIG", "data", "en")
		b, _ = fs.ReadFile(en, "page.html")
		if !strings.Contains(string(b), "neutral") {
			t.Fatalf("fallback to the language-independent version: %s", b)
		}
		if _, err := fs.Stat(de, "page_de.html"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("variant file visible in the file system")
		}
		tmpl, err := template.ParseFS(de, "page.html")
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := tmpl.ExecuteTemplate(&out, "page", "<b>"); err != nil || out.String() != "deutsch &lt;b&gt;" {
			t.Fatalf("template: %v %q", err, out.String())
		}
		consistent(t, k)

		// The check detects contents that no longer match their hash.
		if _, err := k.DB().ExecContext(ctx, `UPDATE "/RT/ASSET_CHUNKS" SET "DATA" = `+k.Dialect().Placeholder(1)+
			` WHERE "SEQ" = 2`, make([]byte, 256<<10)); err != nil {
			t.Fatal(err)
		}
		d, err := k.Check(ctx)
		if err != nil || len(d) != 1 || !strings.Contains(d[0].Problem, "does not match its SHA-256") {
			t.Fatalf("check: %v %v", err, d)
		}
	})
}
