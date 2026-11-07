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
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

const (
	tBlobs  = "/RT/ASSET_BLOBS"
	tChunks = "/RT/ASSET_CHUNKS"
)

func assetKinds(p *plan.Plan) []string {
	var out []string
	for _, c := range p.Changes {
		if c.Repository != "" {
			s := string(c.Kind) + " " + c.Repository
			if c.Path != "" {
				s += " " + c.Path
			}
			if c.Language != "" {
				s += " " + c.Language
			}
			out = append(out, s)
		}
	}
	return out
}

func TestAssetStorage(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		// The kernel package TRBASE brings assets of its own.
		blobs0, chunks0 := count(t, k, tBlobs), count(t, k, tChunks)
		p, err := k.Install(ctx, examples(t), kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := assetKinds(p); !slices.Contains(got, "add-asset templates invoice/invoice.xsl") || !slices.Contains(got, "add-asset-repository web") {
			t.Fatalf("plan: %v", got)
		}
		s, _ := k.Schema(ctx)
		hashes := s.AssetHashes()
		if n := count(t, k, tBlobs); n != len(hashes) || n != 8 {
			t.Fatalf("blobs: %d, distinct contents %d", n, len(hashes))
		}
		consistent(t, k)

		// Same content in another package is stored once.
		dir := testutil.CopyExample(t, "TRBASE")
		pkgDir := filepath.Join(t.TempDir(), "TRDUP")
		testutil.WriteFile(t, pkgDir, "manifest.json", []byte(`{"id":"TRDUP","version":"1.0.0"}`))
		testutil.WriteFile(t, pkgDir, "assets/web/asset.json", []byte(`{"description":{"en":"Copy"}}`))
		css, _ := os.ReadFile(filepath.Join(dir, "assets/web/css/base.css"))
		testutil.WriteFile(t, pkgDir, "assets/web/copy.css", css)
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, pkgDir)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if n := count(t, k, tBlobs); n != 8 {
			t.Fatalf("identical content stored twice: %d blobs", n)
		}

		// Size limit.
		k.SetMaxAssetSize(100)
		_, err = k.Upgrade(ctx, testutil.Example(t, "TRFIN"), kernel.Options{})
		var v *rterr.ValidationError
		if !errors.As(err, &v) || v.Code != rterr.CodeAsset || !strings.Contains(err.Error(), "the limit is 100") {
			t.Fatalf("size limit: %v", err)
		}
		k.SetMaxAssetSize(model.DefaultMaxAssetSize)

		// Removing all packages removes all contents.
		for _, id := range []model.PackageID{"TRDUP", "TROTTO", "TRFIN"} {
			if _, err := k.Uninstall(ctx, id, kernel.Options{Confirm: true}); err != nil {
				t.Fatal(err)
			}
		}
		if b, c := count(t, k, tBlobs), count(t, k, tChunks); b != blobs0 || c != chunks0 {
			t.Fatalf("contents left: %d blobs, %d chunks", b, c)
		}
		consistent(t, k)
	})
}

func TestAssetUpgrade(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		before, _ := k.Schema(ctx)
		logoTime := before.AssetRepositories[model.AssetRepositoryKey{Package: "TRFIN", ID: "web"}].Entries["images/logo.png"].Content.ModifiedAt

		dir := testutil.CopyExample(t, "TRFIN")
		testutil.WriteFile(t, dir, "assets/web/css/finance.css", []byte(".amount { color: black; }\n")) // changed
		testutil.WriteFile(t, dir, "assets/web/js/app.js", []byte("console.log('finance');\n"))         // new
		if err := os.Remove(filepath.Join(dir, "assets/templates/letter/letter_de.xsl")); err != nil {  // variant removed
			t.Fatal(err)
		}
		testutil.WriteFile(t, dir, "assets/templates/asset.json", []byte(`{"description":{"en":"Finance print templates"},
			"files":{"invoice/invoice.xsl":{"languages":{"en":"invoice/invoice_en.xsl","de":"invoice/invoice_de.xsl"}}}}`))
		testutil.WriteFile(t, dir, "assets/mail/asset.json", []byte(`{"description":{"en":"Mail templates"}}`)) // new repository
		testutil.WriteFile(t, dir, "assets/mail/welcome.html", []byte("<p>Welcome</p>\n"))
		fin := testutil.LoadDir(t, dir)
		fin.Manifest.Version = "1.1.0"

		p, err := k.Upgrade(ctx, fin, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		got := assetKinds(p)
		for _, want := range []string{
			"replace-asset web css/finance.css", "add-asset-folder web js", "add-asset web js/app.js",
			"drop-asset-variant templates letter/letter.xsl de", "alter-asset-repository templates",
			"add-asset-repository mail", "add-asset mail welcome.html",
		} {
			if !slices.Contains(got, want) {
				t.Errorf("missing %q in %v", want, got)
			}
		}
		if len(p.Destructive()) != 0 || len(p.Steps) != 0 {
			t.Fatalf("asset changes must not be destructive or need statements: %v", p.Destructive())
		}
		after, _ := k.Schema(ctx)
		web := after.AssetRepositories[model.AssetRepositoryKey{Package: "TRFIN", ID: "web"}]
		if !web.Entries["images/logo.png"].Content.ModifiedAt.Equal(logoTime) {
			t.Error("unchanged asset got a new time")
		}
		if !web.Entries["css/finance.css"].Content.ModifiedAt.After(logoTime) && !web.Entries["css/finance.css"].Content.ModifiedAt.Equal(logoTime) {
			t.Error("changed asset kept an older time")
		}
		if n := count(t, k, tBlobs); n != len(after.AssetHashes()) {
			t.Fatalf("blobs %d, referenced %d", n, len(after.AssetHashes()))
		}
		consistent(t, k)
	})
}

func TestMigrateFromVersion2(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		k.SetTargetVersion(2)
		initialized(t, k)
		if _, err := k.Install(ctx, legacyExamples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		k.SetTargetVersion(3)
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
		// The assets of packages installed before arrive with the next
		// upgrade.
		if _, err := k.Upgrade(ctx, legacyExample(t, "TRFIN"), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		s, _ := k.Schema(ctx)
		if len(s.AssetRepositories) != 2 {
			t.Fatalf("repositories: %d", len(s.AssetRepositories))
		}
		consistent(t, k)
	})
}
