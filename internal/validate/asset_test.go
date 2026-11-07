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

package validate

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func assetRepo(s *model.Schema, pkg model.PackageID, id string) *model.AssetRepository {
	return s.AssetRepositories[model.AssetRepositoryKey{Package: pkg, ID: id}]
}

func expectAsset(t *testing.T, s *model.Schema, opts Options, contains string) {
	t.Helper()
	err := Schema(s, opts)
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Code == rterr.CodeAsset && strings.Contains(v.Error(), contains) {
			return
		}
	}
	t.Fatalf("expected asset error containing %q, got:\n%v", contains, err)
}

func TestExampleAssetsAreValid(t *testing.T) {
	s := load(t).schema(t)
	if err := Schema(s, Options{Languages: map[string]bool{"en": true, "de": true}, MaxAssetSize: model.DefaultMaxAssetSize}); err != nil {
		t.Fatal(err)
	}
	base, fin := assetRepo(s, "TRBASE", "web"), assetRepo(s, "TRFIN", "web")
	if base == nil || fin == nil || base == fin {
		t.Fatal("web of TRBASE and web of TRFIN must be separate repositories")
	}
	if base.Entries["images/logo.png"] != nil || fin.Entries["css/base.css"] != nil {
		t.Fatal("entries mixed up between the repositories")
	}
	inv := assetRepo(s, "TRFIN", "templates").Entries["invoice/invoice.xsl"]
	if inv.Content != nil || !slices.Equal(inv.Languages(), []string{"de", "en"}) || inv.MimeType != "application/xslt+xml" {
		t.Fatalf("invoice: %+v", inv)
	}
	if assetRepo(s, "TRFIN", "templates").Entries["invoice/invoice_de.xsl"] != nil {
		t.Fatal("variant files must not be reachable under their own path")
	}
}

func TestAssetRepositoryID(t *testing.T) {
	for _, id := range []string{"Web", "1web", "web app", "web.app", strings.Repeat("a", 33), "wéb"} {
		e := load(t)
		e.fin.AssetRepositories[1].ID = id
		expectAsset(t, e.schema(t), Options{}, "repository IDs")
	}
}

func TestAssetPathRules(t *testing.T) {
	bad := map[string]string{
		"a b.css":                              "may only contain",
		".hidden":                              "starts with a dot",
		"css/../x.css":                         "starts with a dot",
		"css/./x.css":                          "starts with a dot",
		"css//x.css":                           "double slashes",
		"/x.css":                               "leading",
		"ümlaut.css":                           "may only contain",
		"tab\t.css":                            "may only contain",
		strings.Repeat("a", 101):               "bytes, the limit is 100",
		strings.Repeat("abcdefghi/", 26) + "x": "the limit is 255",
	}
	for p, want := range bad {
		if got := model.CheckAssetPath(p); !strings.Contains(got, want) {
			t.Errorf("CheckAssetPath(%q) = %q, want %q", p, got, want)
		}
	}
	for _, p := range []string{"css/base.css", "a-b_c.d/E.F", "x"} {
		if got := model.CheckAssetPath(p); got != "" {
			t.Errorf("CheckAssetPath(%q) = %q", p, got)
		}
	}
	e := load(t)
	s := e.schema(t)
	r := assetRepo(s, "TRFIN", "web")
	r.Entries["images/bad name.png"] = &model.AssetEntry{Path: "images/bad name.png", MimeType: "image/png", Content: r.Entries["images/logo.png"].Content}
	expectAsset(t, s, Options{}, "invalid path")
}

func TestAssetCaseCollision(t *testing.T) {
	s := load(t).schema(t)
	r := assetRepo(s, "TRFIN", "web")
	r.Entries["images/Logo.png"] = &model.AssetEntry{Path: "images/Logo.png", MimeType: "image/png", Content: r.Entries["images/logo.png"].Content}
	expectAsset(t, s, Options{}, "only in upper and lower case")
}

func TestAssetSizeLimit(t *testing.T) {
	expectAsset(t, load(t).schema(t), Options{MaxAssetSize: 100}, "the limit is 100")
}

func TestAssetVariantLanguages(t *testing.T) {
	err := Schema(load(t).schema(t), Options{Languages: map[string]bool{"en": true}})
	var ml *rterr.MissingLanguages
	if !errors.As(err, &ml) || !slices.Contains(ml.Objects, "asset invoice/invoice.xsl in repository templates of package TRFIN") {
		t.Fatalf("got %v", err)
	}
}

func TestAssetDescriptionNeedsEnglish(t *testing.T) {
	s := load(t).schema(t)
	assetRepo(s, "TRFIN", "web").Entries["images/logo.png"].Description = map[string]string{"de": "Logo"}
	err := Schema(s, Options{})
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Code == rterr.CodeText && strings.Contains(v.Object, "images/logo.png") {
			return
		}
	}
	t.Fatalf("got %v", err)
}

func TestAssetStructure(t *testing.T) {
	s := load(t).schema(t)
	r := assetRepo(s, "TRFIN", "web")
	delete(r.Entries, "images")
	expectAsset(t, s, Options{}, "parent folder images does not exist")

	s = load(t).schema(t)
	r = assetRepo(s, "TRFIN", "web")
	r.Entries["images"].MimeType = "image/png"
	expectAsset(t, s, Options{}, "folders have no content")

	s = load(t).schema(t)
	assetRepo(s, "TRFIN", "web").Entries["images/logo.png"].MimeType = "PNG"
	expectAsset(t, s, Options{}, "invalid media type")

	s = load(t).schema(t)
	inv := assetRepo(s, "TRFIN", "templates").Entries["invoice/invoice.xsl"]
	inv.Variants = nil
	expectAsset(t, s, Options{}, "neither content nor language variants")
}

func TestRemoveLanguageRemovesVariants(t *testing.T) {
	s := load(t).schema(t)
	n := s.RemoveLanguage("de")
	r := assetRepo(s, "TRFIN", "templates")
	if r.Entries["letter/letter.xsl"].Variants["de"] != nil || r.Entries["invoice/invoice.xsl"].Variants["de"] != nil || n == 0 {
		t.Fatalf("variants left: %+v", r.Entries["letter/letter.xsl"])
	}
	if err := Schema(s, Options{Languages: map[string]bool{"en": true}}); err != nil {
		t.Fatal(err)
	}
}
