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
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

const itemsService = "/TRODATA/ITEMS"

// odataPackage loads the package TRODATA of the tests of OData: items of
// all types with behavior, parts and regions.
func odataPackage(t *testing.T) *model.Package {
	t.Helper()
	return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "odata", "TRODATA"))
}

// TestODataMetadataGolden compares the metadata of the services of TRFIN
// and TRODATA in both formats and languages with golden files.
func TestODataMetadataGolden(t *testing.T) {
	db := testutil.Databases(t)[0]
	k, err := kernel.Open(context.Background(), db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	ctx := context.Background()
	installCompanies(t, k)
	if _, err := k.Install(ctx, []*model.Package{odataPackage(t)}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, svc := range []struct{ name, file string }{{companyService, "company-service"}, {itemsService, "items"}} {
		for _, lang := range []string{"en", "de"} {
			for _, format := range []string{kernel.MetadataXML, kernel.MetadataJSON} {
				doc, err := k.ServiceMetadata(ctx, svc.name, kernel.ServiceMetadataOptions{Language: lang, Format: format})
				if err != nil {
					t.Fatal(err)
				}
				if format == kernel.MetadataXML {
					if err := wellFormed(doc); err != nil {
						t.Errorf("%s is no XML: %v", svc.file, err)
					}
				} else if !json.Valid(doc) {
					t.Errorf("%s is no JSON", svc.file)
				}
				testutil.Golden(t, "odata/"+svc.file+"."+lang+"."+format, string(doc))
			}
		}
	}
}

func TestODataMetadataOverHTTP(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		c := odataKernel(t, k)
		anna := basicAuth("anna", "secret")
		if err := k.ActivateService(ctx, companyService, "test"); err != nil {
			t.Fatal(err)
		}
		const md = "/rt/odata/trfin/company_service/$metadata"
		a := c.do("GET", md, anna, "")
		etag := a.header.Get("ETag")
		if a.status != 200 || !strings.HasPrefix(a.header.Get("Content-Type"), "application/xml") || etag == "" ||
			a.header.Get("Content-Language") != "en" || !strings.Contains(a.header.Get("Vary"), "Accept-Language") ||
			a.header.Get("OData-Version") != "4.0" || !strings.Contains(a.body, `<EntitySet Name="Companies"`) {
			t.Fatalf("metadata: %d %v %s", a.status, a.header, a.body)
		}
		if a := c.do("GET", md, func(r *http.Request) { anna(r); r.Header.Set("If-None-Match", etag) }, ""); a.status != 304 || a.body != "" {
			t.Errorf("If-None-Match: %d", a.status)
		}
		de := c.do("GET", md, func(r *http.Request) { anna(r); r.Header.Set("Accept-Language", "de-DE,de;q=0.9") }, "")
		if de.status != 200 || de.header.Get("Content-Language") != "de" || !strings.Contains(de.body, `String="Firmen"`) || de.header.Get("ETag") == etag {
			t.Errorf("German: %d %v", de.status, de.header)
		}
		js := c.do("GET", md, func(r *http.Request) { anna(r); r.Header.Set("Accept", "application/json") }, "")
		var doc map[string]any
		if err := json.Unmarshal([]byte(js.body), &doc); err != nil || js.status != 200 || doc["$Version"] != "4.01" || doc["$EntityContainer"] != "TRFIN.Container" ||
			!strings.HasPrefix(js.header.Get("Content-Type"), "application/json") {
			t.Errorf("JSON by Accept: %d %v %.200s", js.status, js.header, js.body)
		}
		if a := c.do("GET", md+"?$format=json", anna, ""); a.body != js.body {
			t.Error("$format=json differs from Accept")
		}
		if a := c.do("GET", md+"?$format=atom", anna, ""); a.status != 406 || odataError(t, a) != "NOT_ACCEPTABLE" {
			t.Errorf("$format=atom: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/rt/odata/$vocabularies/RT.Fields.xml", anna, ""); a.status != 200 || !strings.Contains(a.body, `<Term Name="Mandatory"`) {
			t.Errorf("vocabulary: %d", a.status)
		}
		if a := c.do("GET", "/rt/odata/$vocabularies/RT.Fields.json", anna, ""); a.status != 200 || !json.Valid([]byte(a.body)) {
			t.Errorf("vocabulary in JSON: %d", a.status)
		}
		// A new generation, such as an upgrade, builds the document again.
		next := testutil.Example(t, "TRFIN")
		next.Manifest.Version = "1.0.1"
		next.Services[0].Description["en"] = "Companies and contacts"
		if _, err := k.Upgrade(ctx, next, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if a := c.do("GET", md, func(r *http.Request) { anna(r); r.Header.Set("If-None-Match", etag) }, ""); a.status != 200 ||
			!strings.Contains(a.body, "Companies and contacts") {
			t.Errorf("after upgrade: %d", a.status)
		}
	})
}

// wellFormed reads an XML document to its end.
func wellFormed(doc []byte) error {
	d := xml.NewDecoder(bytes.NewReader(doc))
	for {
		if _, err := d.Token(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}
