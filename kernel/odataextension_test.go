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
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// metadataImplementation returns the files of an implementation of
// /TRBASE/ODATA_METADATA with a class whose method ADJUST has body.
func metadataImplementation(pkg, name string, order int, filters, body string) map[string]string {
	return map[string]string{
		"schema/extension-implementations/" + name + ".json": fmt.Sprintf(`{ "name": "/%s/%s", "description": { "en": "%s" },
			"extensionPoint": "/TRBASE/ODATA_METADATA", "class": "/%s/%s", "filters": { %s }, "order": %d }`, pkg, name, name, pkg, name, filters, order),
		"src/" + name + ".class": canonical(name+".class", fmt.Sprintf(`public class /%s/%s implements /TRBASE/ODATA_METADATA_EXTENSION

  public method ADJUST(metadata: /TRBASE/ODATA_METADATA) returns /TRBASE/ODATA_METADATA
%s
    return metadata
  end

end
`, pkg, name, body)),
	}
}

// canonical returns source text in canonical form.
func canonical(file, text string) string {
	out, errs := syntax.Format(file, text)
	if len(errs) > 0 {
		panic(fmt.Sprint(errs))
	}
	return out
}

// extensionPackage returns a package with implementations of the metadata.
func extensionPackage(t *testing.T, id string, impls ...map[string]string) *model.Package {
	t.Helper()
	files := map[string]string{"manifest.json": `{ "id": "` + id + `", "version": "1.0.0", "requires": [{ "id": "TRBASE" }], "languageVersion": 1 }`}
	for _, impl := range impls {
		for k, v := range impl {
			files[k] = v
		}
	}
	return writePackage(t, files)
}

// The implementations of TREXT annotate the items of TRODATA: UI adds a
// vocabulary of its own with a placeholder in the language of the request
// and renames the field NAME, SUFFIX then adds to that name.
var (
	uiImplementation = metadataImplementation("TREXT", "UI", 10, `"SERVICE_NAME": [{ "value": "/TRODATA/ITEMS" }]`, `
    var r: /TRBASE/ODATA_REFERENCE
    r.URI = "https://example.com/vocabularies/Example.UI.xml"
    r.NAMESPACE = "Example.UI"
    r.ALIAS = "UI"
    metadata.REFERENCES.APPEND(r)
    var text = "Enter a name"
    if metadata.LANGUAGE = "de"
      text = "Namen eingeben"
    end
    metadata.ANNOTATIONS.APPEND(/TRBASE/ODATA_ANNOTATIONS.TEXT("TRODATA.TRODATA_ITEMS/NAME", "UI.Placeholder", text))
    var out: /TRBASE/ODATA_ANNOTATION_ARRAY = []
    for a in metadata.ANNOTATIONS
      var b = a
      if b.TARGET = "TRODATA.TRODATA_ITEMS/NAME" and b.TERM = "Core.Description"
        b.VALUE = /TRBASE/ODATA_ANNOTATIONS.TEXT(b.TARGET, b.TERM, "Item name").VALUE
      end
      out.APPEND(b)
    end
    metadata.ANNOTATIONS = out`)
	suffixImplementation = metadataImplementation("TREXT", "SUFFIX", 20, `"PACKAGE_ID": [{ "value": "TRODATA" }]`, `
    var out: /TRBASE/ODATA_ANNOTATION_ARRAY = []
    for a in metadata.ANNOTATIONS
      var b = a
      if b.TARGET = "TRODATA.TRODATA_ITEMS/NAME" and b.TERM = "Core.Description"
        var text = ""
        for v in b.VALUE
          text = v.TEXT default ""
        end
        b.VALUE = /TRBASE/ODATA_ANNOTATIONS.TEXT(b.TARGET, b.TERM, text + " (checked)").VALUE
      end
      out.APPEND(b)
    end
    metadata.ANNOTATIONS = out`)
)

// The implementations of TRBAD break the rules on the service of TRFIN.
var (
	renameImplementation = metadataImplementation("TRBAD", "RENAME", 1, `"PACKAGE_ID": [{ "value": "TRFIN" }]`, `
    var sets: /TRBASE/ODATA_ENTITY_SET_ARRAY = []
    for s in metadata.ENTITY_SETS
      var c = s
      c.NAME = c.NAME + "X"
      sets.APPEND(c)
    end
    metadata.ENTITY_SETS = sets`)
	uncomputeImplementation = metadataImplementation("TRBAD", "UNCOMPUTE", 2, `"PACKAGE_ID": [{ "value": "TRFIN" }]`, `
    var out: /TRBASE/ODATA_ANNOTATION_ARRAY = []
    for a in metadata.ANNOTATIONS
      if a.TERM <> "Core.Computed"
        out.APPEND(a)
      end
    end
    metadata.ANNOTATIONS = out`)
	writeImplementation = metadataImplementation("TRBAD", "WRITE", 3, `"PACKAGE_ID": [{ "value": "TRFIN" }]`, `
    var row: /TRBAD/CALLS
    row.SERVICE = metadata.SERVICE_NAME
    insert row`)
)

// recordErrors makes the default logger keep its messages at level error
// with their attributes.
func recordErrors(t *testing.T) func() string {
	var mu sync.Mutex
	var b strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, b: &b}, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return b.String()
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func TestODataMetadataExtensions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		c := odataKernel(t, k)
		if _, err := k.Install(ctx, []*model.Package{odataPackage(t)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRODATA/EDITOR", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		for _, svc := range []string{companyService, itemsService} {
			if err := k.ActivateService(ctx, svc, "test"); err != nil {
				t.Fatal(err)
			}
		}
		anna := basicAuth("anna", "secret")
		const items = "/rt/odata/trodata/items/$metadata"
		const companies = "/rt/odata/trfin/company_service/$metadata"
		plain := c.do("GET", items, anna, "")
		plainCompanies := c.do("GET", companies, anna, "")

		// Two implementations run in their order; the second sees the
		// changes of the first.
		if _, err := k.Install(ctx, []*model.Package{extensionPackage(t, "TREXT", uiImplementation, suffixImplementation)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		a := c.do("GET", items, anna, "")
		if a.status != 200 || !strings.Contains(a.body, `<Annotation Term="UI.Placeholder" String="Enter a name"/>`) ||
			!strings.Contains(a.body, `<edmx:Reference Uri="https://example.com/vocabularies/Example.UI.xml">`) ||
			!strings.Contains(a.body, `<Annotation Term="Core.Description" String="Item name (checked)"/>`) {
			t.Fatalf("extended metadata: %d %s", a.status, a.body)
		}
		extended := a.header.Get("ETag")
		if extended == plain.header.Get("ETag") {
			t.Error("the ETag ignores the implementations")
		}
		de := c.do("GET", items, func(r *http.Request) { anna(r); r.Header.Set("Accept-Language", "de") }, "")
		if !strings.Contains(de.body, `String="Namen eingeben"`) {
			t.Errorf("German placeholder: %s", de.body)
		}
		js := c.do("GET", items+"?$format=json", anna, "")
		if !strings.Contains(js.body, `"@UI.Placeholder": "Enter a name"`) {
			t.Errorf("JSON: %s", js.body)
		}
		// The filters select the service.
		if a := c.do("GET", companies, anna, ""); a.body != plainCompanies.body {
			t.Error("the implementations changed the service of TRFIN")
		}
		// Without extensions the metadata are those of the kernel.
		doc, err := k.ServiceMetadata(ctx, itemsService, kernel.ServiceMetadataOptions{WithoutExtensions: true})
		if err != nil || string(doc) != plain.body {
			t.Errorf("without extensions: %v", err)
		}

		// Deactivating an implementation builds the document again with a
		// new ETag; the point is activated only globally.
		if err := k.Implementations().Deactivate(ctx, "/TREXT/SUFFIX", kernel.ImplementationOptions{}); err != nil {
			t.Fatal(err)
		}
		a = c.do("GET", items, func(r *http.Request) { anna(r); r.Header.Set("If-None-Match", extended) }, "")
		if a.status != 200 || !strings.Contains(a.body, `String="Item name"/>`) || a.header.Get("ETag") == extended {
			t.Errorf("after deactivation: %d %v", a.status, a.header)
		}
		var inv *rterr.InvalidArgument
		if err := k.Implementations().Activate(ctx, "/TREXT/SUFFIX", kernel.ImplementationOptions{Tenant: "ACME"}); !errors.As(err, &inv) {
			t.Errorf("activation in a tenant: %v", err)
		}
		if err := k.Implementations().Activate(ctx, "/TREXT/SUFFIX", kernel.ImplementationOptions{}); err != nil {
			t.Fatal(err)
		}
		if a := c.do("GET", items, anna, ""); a.header.Get("ETag") != extended {
			t.Errorf("activated again: %v", a.header)
		}

		// Implementations that break the rules fail the metadata with 500
		// and a log entry; there is no fallback to the metadata without
		// them.
		logged := recordErrors(t)
		calls := map[string]string{"schema/tables/CALLS.json": `{ "name": "/TRBAD/CALLS", "tenantSpecific": false, "dataClass": "runtimeConfiguration",
			"fields": [{ "name": "SERVICE", "domain": "/TRBASE/TEXT" }], "primaryKey": ["SERVICE"] }`}
		if _, err := k.Install(ctx, []*model.Package{extensionPackage(t, "TRBAD", renameImplementation, uncomputeImplementation, writeImplementation, calls)},
			kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		only := func(name string) {
			t.Helper()
			for _, impl := range []string{"RENAME", "UNCOMPUTE", "WRITE"} {
				change := k.Implementations().Deactivate
				if impl == name {
					change = k.Implementations().Activate
				}
				if err := change(ctx, "/TRBAD/"+impl, kernel.ImplementationOptions{}); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, c2 := range []struct{ impl, reason string }{
			{"RENAME", "changed the structure of the metadata"},
			{"UNCOMPUTE", "removed Core.Computed"},
			{"WRITE", "only reads"},
		} {
			only(c2.impl)
			if a := c.do("GET", companies, anna, ""); a.status != 500 || odataError(t, a) != "INTERNAL_ERROR" || strings.Contains(a.body, "TRBAD") {
				t.Errorf("%s: %d %s", c2.impl, a.status, a.body)
			}
			if l := logged(); !strings.Contains(l, "implementation=/TRBAD/"+c2.impl) || !strings.Contains(l, "package=TRBAD") || !strings.Contains(l, c2.reason) {
				t.Errorf("%s: log %s", c2.impl, l)
			}
			var me *kernel.MetadataExtensionError
			if _, err := k.ServiceMetadata(ctx, companyService, kernel.ServiceMetadataOptions{}); !errors.As(err, &me) || me.Implementation != "/TRBAD/"+c2.impl {
				t.Errorf("%s: rt service metadata: %v", c2.impl, err)
			}
		}
		if err := k.Implementations().Deactivate(ctx, "/TRBAD/WRITE", kernel.ImplementationOptions{}); err != nil {
			t.Fatal(err)
		}
		if a := c.do("GET", companies, anna, ""); a.status != 200 || a.body != plainCompanies.body {
			t.Errorf("without bad implementations: %d", a.status)
		}
	})
}
