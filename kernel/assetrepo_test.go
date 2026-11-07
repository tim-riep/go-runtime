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
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/kernelpkg"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

func assetPackage(t *testing.T, id string) *model.Package {
	return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "assets", id))
}

// withAssetKernel runs fn on every database with a kernel that has the
// kernel package TRAKERNEL, German, the tenant ACME and the packages
// TRARLIB, TRAROTHER and TRAREPO, which requires TRARLIB but not
// TRAROTHER.
func withAssetKernel(t *testing.T, fn func(t *testing.T, k *kernel.Kernel)) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			pkgs, err := kernelpkg.Packages()
			if err != nil {
				t.Fatal(err)
			}
			k, err := kernel.OpenConfig(ctx, db.DSN, kernel.Config{KernelPackages: append(pkgs, assetPackage(t, "TRAKERNEL"))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { k.Close() })
			initialized(t, k)
			if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
				t.Fatal(err)
			}
			pkgs = []*model.Package{assetPackage(t, "TRARLIB"), assetPackage(t, "TRAROTHER"), assetPackage(t, "TRAREPO")}
			if _, err := k.Install(ctx, pkgs, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			fn(t, k)
		})
	}
}

// runIn calls a method of /TRAREPO/READER in a language and returns the
// result, or the class and text of the exception.
func runIn(t *testing.T, k *kernel.Kernel, lang, method string, args ...string) string {
	t.Helper()
	out, err := call(k, kernel.ExecOptions{Tenant: "ACME", Language: lang}, "/TRAREPO/READER", method, args...)
	if err != nil {
		var e *vm.Exception
		if errors.As(err, &e) {
			return e.Class + ": " + e.Text
		}
		t.Fatalf("%s(%s): %v", method, strings.Join(args, ", "), err)
	}
	return out
}

func assetInfo(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	return m
}

// TestAssetRepositoryClass runs all methods of /TRBASE/ASSET_REPOSITORY.
func TestAssetRepositoryClass(t *testing.T) {
	withAssetKernel(t, func(t *testing.T, k *kernel.Kernel) {
		lib := []string{`"TRARLIB"`, `"docs"`}
		with := func(path string) []string { return append(lib, path) }
		run := func(method string, args ...string) string { return runIn(t, k, "en", method, args...) }

		if got := run("IDS"); got != `"TRARLIB docs"` {
			t.Errorf("IDS = %s", got)
		}
		// Listing: top level, a folder, an empty folder, the tree.
		if got := run("LIST", with(`""`)...); got != `["a.txt","bad.txt","big.txt","empty","empty.txt","hello.txt","only.txt","sub"]` {
			t.Errorf("LIST top level = %s", got)
		}
		if got := run("LIST", with(`"sub"`)...); got != `["sub/deep","sub/x.txt"]` {
			t.Errorf("LIST sub = %s", got)
		}
		if got := run("LIST", with(`"empty"`)...); got != `[]` {
			t.Errorf("LIST empty = %s", got)
		}
		var tree []map[string]any
		if err := json.Unmarshal([]byte(run("TREE", with(`"sub"`)...)), &tree); err != nil || len(tree) != 3 || tree[0]["PATH"] != "sub/deep" || tree[1]["PATH"] != "sub/deep/y.txt" || tree[2]["PATH"] != "sub/x.txt" {
			t.Errorf("LIST_TREE sub = %v %v", tree, err)
		}
		if err := json.Unmarshal([]byte(run("TREE", with(`""`)...)), &tree); err != nil || len(tree) != 11 {
			t.Errorf("LIST_TREE top level: %d entries, %v", len(tree), err)
		}

		// INFO of an asset, a folder and the top level; null against "".
		a := assetInfo(t, run("INFO", with(`"a.txt"`)...))
		if a["IS_FOLDER"] != false || a["SIZE"] != float64(5) || a["MIME_TYPE"] != "text/plain" || a["DESCRIPTION"] != "First file" ||
			a["LANGUAGE"] != nil || a["HAS_NEUTRAL"] != true || a["MODIFIED_AT"] == nil || a["ETAG"] != `"`+a["SHA256"].(string)+`"` {
			t.Errorf("INFO a.txt = %v", a)
		}
		if d := assetInfo(t, runIn(t, k, "de", "INFO", with(`"a.txt"`)...)); d["DESCRIPTION"] != "Erste Datei" {
			t.Errorf("INFO a.txt in German = %v", d)
		}
		if e := assetInfo(t, run("INFO", with(`"empty.txt"`)...)); e["DESCRIPTION"] != "" || e["SIZE"] != float64(0) {
			t.Errorf("INFO empty.txt = %v", e)
		}
		if x := assetInfo(t, run("INFO", with(`"sub/x.txt"`)...)); x["DESCRIPTION"] != nil {
			t.Errorf("INFO sub/x.txt = %v", x)
		}
		f := assetInfo(t, run("INFO", with(`"empty"`)...))
		if f["IS_FOLDER"] != true || f["MIME_TYPE"] != nil || f["SHA256"] != nil || f["MODIFIED_AT"] != nil || f["SIZE"] != float64(0) || f["DESCRIPTION"] != "" {
			t.Errorf("INFO empty = %v", f)
		}
		if r := assetInfo(t, run("INFO", with(`""`)...)); r["IS_FOLDER"] != true || r["PATH"] != "" {
			t.Errorf("INFO top level = %v", r)
		}
		h := assetInfo(t, runIn(t, k, "de", "INFO", with(`"hello.txt"`)...))
		if h["LANGUAGE"] != "de" || h["SIZE"] != float64(5) || h["HAS_NEUTRAL"] != true || len(h["LANGUAGES"].([]any)) != 2 {
			t.Errorf("INFO hello.txt in German = %v", h)
		}

		// Contents with language fallback.
		for _, c := range []struct{ lang, path, want string }{
			{"en", "hello.txt", "hello"}, {"de", "hello.txt", "hallo"}, {"fr", "hello.txt", "hello"},
			{"de", "only.txt", "english"}, {"de", "a.txt", "alpha"}, {"en", "empty.txt", ""},
		} {
			if got := runIn(t, k, c.lang, "TEXT", with(`"`+c.path+`"`)...); got != `"`+c.want+`"` {
				t.Errorf("GET_TEXT %s in %s = %s, want %q", c.path, c.lang, got, c.want)
			}
		}
		if got := run("TEXT_IN", append(with(`"hello.txt"`), `"de"`)...); got != `"hallo"` {
			t.Errorf("GET_IN_LANGUAGE = %s", got)
		}
		if got := run("DATA", with(`"empty.txt"`)...); got != `""` {
			t.Errorf("GET of an empty file = %s", got)
		}
		if got := run("DATA", with(`"a.txt"`)...); got != `"`+base64.StdEncoding.EncodeToString([]byte("alpha"))+`"` {
			t.Errorf("GET a.txt = %s", got)
		}

		// Existence and errors.
		for path, want := range map[string]string{`""`: "true", `"sub/deep"`: "true", `"a.txt"`: "true", `"nope.txt"`: "false", `"hello_de.txt"`: "false"} {
			if got := run("EXISTS", with(path)...); got != want {
				t.Errorf("EXISTS %s = %s", path, got)
			}
		}
		for _, p := range []string{`"/a.txt"`, `"sub/"`, `"sub//x.txt"`, `".."`, `"sub/.x"`, `"a b"`} {
			if got := run("EXISTS", with(p)...); !strings.HasPrefix(got, "/TRBASE/ASSET_PATH_INVALID: ") {
				t.Errorf("EXISTS %s = %s", p, got)
			}
		}
		for method, path := range map[string]string{"TEXT": `"nope.txt"`, "INFO": `"nope.txt"`, "LIST": `"a.txt"`, "DATA": `"sub"`} {
			if got := run(method, with(path)...); !strings.HasPrefix(got, "/TRBASE/ASSET_NOT_FOUND: ") {
				t.Errorf("%s %s = %s", method, path, got)
			}
		}
		if got := run("TEXT", `"TRARLIB"`, `"nope"`, `"a.txt"`); got != "/TRBASE/ASSET_REPOSITORY_NOT_FOUND: package TRARLIB has no asset repository nope" {
			t.Errorf("missing repository: %s", got)
		}
		if got := run("TEXT", with(`"bad.txt"`)...); got != "/TRBASE/ASSET_ENCODING_INVALID: asset bad.txt is not valid UTF-8" {
			t.Errorf("invalid UTF-8: %s", got)
		}
		if got := run("TEXT", with(`"big.txt"`)...); got != `"`+strings.Repeat("x", 4096)+`"` {
			t.Errorf("big.txt below the limit: %.40s", got)
		}
		k.SetMaxAssetRead(1024)
		if got := run("DATA", with(`"big.txt"`)...); got != "/TRBASE/ASSET_TOO_LARGE: asset big.txt has 4096 bytes, more than the limit of 1024" {
			t.Errorf("too large: %s", got)
		}
		k.SetMaxAssetRead(kernel.DefaultMaxAssetRead)

		// Repositories with their labels.
		if got := runIn(t, k, "de", "REPOSITORIES", `"TRARLIB"`); got != `[{"PACKAGE_ID":"TRARLIB","REPOSITORY_ID":"docs","LABEL":"Dokumente"}]` {
			t.Errorf("LIST_REPOSITORIES = %s", got)
		}
		if got := run("REPOSITORIES", `"TRAREPO"`); got != `[{"PACKAGE_ID":"TRAREPO","REPOSITORY_ID":"own","LABEL":"Own files"}]` {
			t.Errorf("LIST_REPOSITORIES own = %s", got)
		}

		// The example of the language reference.
		rec := &vm.LogRecorder{}
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME", VM: vm.Options{Log: rec}}, "/TRAREPO/READER", "EXAMPLE"); err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, e := range rec.Entries {
			lines = append(lines, e.Text)
		}
		if got := strings.Join(lines, "\n"); !strings.HasPrefix(got, "Loaded hello.txt (5 bytes, text/plain): hello\na.txt 0 language variants\n") ||
			!strings.Contains(got, "hello.txt 2 language variants") || strings.Contains(got, "sub ") {
			t.Errorf("example:\n%s", got)
		}
	})
}

// TestAssetRepositoryAccess checks the rule for reading: the own package,
// required packages, and any package for kernel packages, whose code
// keeps the rights of the package that calls it.
func TestAssetRepositoryAccess(t *testing.T) {
	withAssetKernel(t, func(t *testing.T, k *kernel.Kernel) {
		run := func(method string, args ...string) string { return runIn(t, k, "en", method, args...) }
		if got := run("TEXT", `"TRAREPO"`, `"own"`, `"own.txt"`); got != `"own"` {
			t.Errorf("own package: %s", got)
		}
		if got := run("TEXT", `"TRARLIB"`, `"docs"`, `"a.txt"`); got != `"alpha"` {
			t.Errorf("required package: %s", got)
		}
		denied := "/TRBASE/ASSET_ACCESS_DENIED: package TRAREPO may not read the asset repositories of package TRAROTHER"
		if got := run("TEXT", `"TRAROTHER"`, `"misc"`, `"m.txt"`); got != denied {
			t.Errorf("package not required: %s", got)
		}
		if got := run("REPOSITORIES", `"TRAROTHER"`); got != denied {
			t.Errorf("repositories of a package not required: %s", got)
		}
		// A missing repository of a package that may not be read is denied too.
		if got := run("TEXT", `"TRAROTHER"`, `"nope"`, `"m.txt"`); got != denied {
			t.Errorf("missing repository of a package not required: %s", got)
		}
		// Through a kernel package the rights stay those of TRAREPO.
		if got := run("VIA_KERNEL", `"TRAROTHER"`, `"misc"`, `"m.txt"`); got != denied {
			t.Errorf("through a kernel package: %s", got)
		}
		if got := run("VIA_KERNEL", `"TRARLIB"`, `"docs"`, `"a.txt"`); got != `"alpha"` {
			t.Errorf("required package through a kernel package: %s", got)
		}
		// Called by the kernel itself, the kernel package reads any package.
		out, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, "/TRAKERNEL/ANY", "READ", `"TRAROTHER"`, `"misc"`, `"m.txt"`)
		if err != nil || out != `"other"` {
			t.Errorf("kernel package: %s %v", out, err)
		}
	})
}

// TestAssetRepositoryCache reads a content once per execution and keeps
// small contents across executions by hash; an upgrade with new content
// gives the new content.
func TestAssetRepositoryCache(t *testing.T) {
	withAssetKernel(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		before := k.ContentReads()
		if got := runIn(t, k, "en", "TWICE"); got != `"alphaalpha"` {
			t.Fatalf("TWICE = %s", got)
		}
		if n := k.ContentReads() - before; n > 1 {
			t.Errorf("read %d times in one execution", n)
		}
		runIn(t, k, "en", "TWICE")
		if n := k.ContentReads() - before; n > 1 {
			t.Errorf("read %d times in two executions", n)
		}

		dir := testutil.CopyDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "assets", "TRARLIB"))
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRARLIB", "version": "1.1.0" }`))
		testutil.WriteFile(t, dir, "assets/docs/a.txt", []byte("omega"))
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if got := runIn(t, k, "en", "TWICE"); got != `"omegaomega"` {
			t.Errorf("after the upgrade: %s", got)
		}
	})
}

// TestAssetsClassRemoved opens a database whose installed package uses
// /TRBASE/ASSETS of an older TRBASE: the kernel without it does not open,
// and the error names the package.
func TestAssetsClassRemoved(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			old := testutil.Example(t, "TRBASE")
			old.Manifest.Version = "1.0.0"
			old.Sources = append(old.Sources, model.Source{Name: "/TRBASE/ASSETS", Kind: model.SourceClass, File: "src/ASSETS.class",
				Text: "public class /TRBASE/ASSETS\n\n  public static method READ(pkg: STRING, repository: STRING, path: STRING) returns BYTES\n    return \"\" as BYTES\n  end\n\nend\n"})
			k, err := kernel.OpenConfig(ctx, db.DSN, kernel.Config{KernelPackages: []*model.Package{old}})
			if err != nil {
				t.Fatal(err)
			}
			initialized(t, k)
			dir := t.TempDir()
			testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TROLD", "version": "1.0.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }] }`))
			testutil.WriteFile(t, dir, "src/READER.class", []byte("public class /TROLD/READER\n\n  public static method READ() returns BYTES\n    return /TRBASE/ASSETS.READ(\"TROLD\", \"x\", \"a\")\n  end\n\nend\n"))
			if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			k.Close()

			_, err = kernel.Open(ctx, db.DSN)
			var up *rterr.KernelPackageUpgrade
			if !errors.As(err, &up) || !strings.Contains(err.Error(), "TROLD") || !strings.Contains(err.Error(), "/TRBASE/ASSETS") {
				t.Fatalf("expected the upgrade of TRBASE to fail in TROLD, got %v", err)
			}
		})
	}
}

// TestAssetRepositoryLiterals checks OPEN with string literals when the
// code compiles: the repository must exist and be readable, it is
// recorded as used, and an upgrade that removes it is rejected.
func TestAssetRepositoryLiterals(t *testing.T) {
	withAssetKernel(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		uses, err := k.CodeUses(ctx, "TRARLIB")
		if err != nil || len(uses) == 0 || uses[0].Package != "TRAREPO" || uses[0].Kind != "asset-repository" || uses[0].Member != model.Some("docs") {
			t.Fatalf("uses of TRARLIB: %+v %v", uses, err)
		}

		for src, want := range map[string]string{
			`OPEN("TRAROTHER", "misc")`: "TRAUSE may not read the asset repositories of package TRAROTHER: TRAUSE does not require TRAROTHER",
			`OPEN("TRARLIB", "nope")`:   "package TRARLIB has no asset repository nope",
		} {
			dir := t.TempDir()
			testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRAUSE", "version": "1.0.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }, { "id": "TRARLIB" }] }`))
			testutil.WriteFile(t, dir, "src/USE.class", []byte("public class /TRAUSE/USE\n\n  public static method M() raises /TRBASE/ASSET_ERROR\n    var repo = /TRBASE/ASSET_REPOSITORY."+src+"\n  end\n\nend\n"))
			_, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{})
			var ce *rterr.CompileError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %v", src, err)
			}
		}

		// Without the repository docs, TRAREPO does not compile.
		dir := testutil.CopyDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "assets", "TRARLIB"))
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRARLIB", "version": "1.1.0" }`))
		if err := os.RemoveAll(filepath.Join(dir, "assets", "docs")); err != nil {
			t.Fatal(err)
		}
		testutil.WriteFile(t, dir, "assets/other/asset.json", []byte(`{ "description": { "en": "Other" } }`))
		testutil.WriteFile(t, dir, "assets/other/o.txt", []byte("o"))
		_, err = k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || ce.Package != "TRAREPO" || !strings.Contains(err.Error(), "package TRARLIB has no asset repository docs") {
			t.Fatalf("upgrade without docs: %v", err)
		}
	})
}

// TestSendAsset streams an asset that is too large to load into the
// memory of an execution, with the headers of the asset handler.
func TestSendAsset(t *testing.T) {
	withAssetKernel(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if err := k.ActivateHTTPService(ctx, "/trarepo"); err != nil {
			t.Fatal(err)
		}
		k.SetMaxAssetRead(1024)
		if got := runIn(t, k, "en", "DATA", `"TRARLIB"`, `"docs"`, `"big.txt"`); !strings.HasPrefix(got, "/TRBASE/ASSET_TOO_LARGE: ") {
			t.Fatalf("GET of big.txt: %s", got)
		}
		srv := httptest.NewServer(k.HTTPHandler())
		t.Cleanup(srv.Close)
		c := client{t, srv.URL}

		sent := c.do("GET", "/trarepo/files/big.txt", nil, "")
		served := c.do("GET", "/trarepo/assets/big.txt", nil, "")
		if sent.status != 200 || sent.body != strings.Repeat("x", 4096) {
			t.Fatalf("SEND_ASSET: %d %.40q", sent.status, sent.body)
		}
		for _, h := range []string{"Content-Type", "Content-Length", "ETag", "Last-Modified", "Cache-Control"} {
			if sent.header.Get(h) == "" || sent.header.Get(h) != served.header.Get(h) {
				t.Errorf("%s: %q, the asset handler sends %q", h, sent.header.Get(h), served.header.Get(h))
			}
		}
		etag := sent.header.Get("ETag")
		if a := c.do("GET", "/trarepo/files/big.txt", func(r *http.Request) { r.Header.Set("If-None-Match", etag) }, ""); a.status != 304 || a.body != "" {
			t.Errorf("If-None-Match: %d %q", a.status, a.body)
		}
		if a := c.do("HEAD", "/trarepo/files/big.txt", nil, ""); a.status != 200 || a.body != "" || a.header.Get("Content-Length") != "4096" {
			t.Errorf("HEAD: %d %v", a.status, a.header)
		}
		if a := c.do("GET", "/trarepo/files/hello.txt?lang=de", nil, ""); a.status != 200 || a.body != "hallo" {
			t.Errorf("language of the request: %d %q", a.status, a.body)
		}
		if a := c.do("GET", "/trarepo/files/nope.txt", nil, ""); a.status != 404 {
			t.Errorf("missing asset: %d", a.status)
		}
	})
}
