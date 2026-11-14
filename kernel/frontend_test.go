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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// frontendPackages loads TRFRONTEND from its sources with a stand-in for
// TRUI, TRFIN and TROTTO, and the demo content TRFDEMO.
func frontendPackages(t *testing.T) []*model.Package {
	t.Helper()
	root := testutil.ModuleRoot(t)
	return []*model.Package{
		testutil.LoadDir(t, filepath.Join(root, "testdata", "frontend", "TRUI")),
		testutil.LoadDir(t, filepath.Join(root, "packages", "TRFRONTEND")),
		testutil.Example(t, "TRFIN"),
		testutil.Example(t, "TROTTO"),
		testutil.LoadDir(t, filepath.Join(root, "testdata", "frontend", "TRFDEMO")),
	}
}

// demoWith copies TRFDEMO with changed files.
func demoWith(t *testing.T, changes map[string]string) *model.Package {
	t.Helper()
	dir := testutil.CopyDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "frontend", "TRFDEMO"))
	for p, data := range changes {
		if data == "" {
			_ = os.Remove(filepath.Join(dir, p))
			continue
		}
		testutil.WriteFile(t, dir, p, []byte(data))
	}
	return testutil.LoadDir(t, dir)
}

func TestFrontendInstall(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		p, err := k.Install(ctx, frontendPackages(t), kernel.Options{})
		if err != nil {
			t.Fatalf("%v\n%s", err, objectErrors(err))
		}
		var notes []string
		for _, c := range p.Changes {
			if c.Kind == plan.ObjectMessage {
				notes = append(notes, c.Name+": "+c.Detail)
			}
		}
		if len(notes) != 1 || !strings.Contains(notes[0], "the tile /TRFDEMO/HELP_TILE of the page /TRFDEMO/COMPANIES_PAGE is in no app collection of the role /TRFIN/COMPANY_CLERK") {
			t.Errorf("warnings: %v", notes)
		}
		types, err := k.ObjectTypes(ctx, "TRFRONTEND")
		if err != nil || len(types) != 7 {
			t.Fatalf("object types: %d %v", len(types), err)
		}
	})
}

func TestFrontendObjectRules(t *testing.T) {
	cases := []struct {
		name, file, doc, want string
	}{
		{"large simple tile", "objects/TRFRONTEND/TILE/HELP_TILE.json", `{"name":"/TRFDEMO/HELP_TILE","content":{"APP":"/TRFDEMO/HELP","SIZE":"LARGE"}}`,
			"only content tiles are LARGE"},
		{"metric tile without metric", "objects/TRFRONTEND/TILE/HELP_TILE.json", `{"name":"/TRFDEMO/HELP_TILE","content":{"APP":"/TRFDEMO/HELP","KIND":"METRIC"}}`,
			"a metric tile needs METRIC"},
		{"icon", "objects/TRFRONTEND/APP/HELP.json", `{"name":"/TRFDEMO/HELP","content":{"TEXTS":[{"LANGUAGE":"en","TITLE":"Help"}],"ICON":"Help Me",
			"TARGET":{"KIND":"URL","URL":"https://example.org"}}}`, "is no name of an icon of TRUI"},
		{"list without entity set", "objects/TRFRONTEND/APP/HELP.json", `{"name":"/TRFDEMO/HELP","content":{"TEXTS":[{"LANGUAGE":"en","TITLE":"Help"}],
			"TARGET":{"KIND":"LIST","SERVICE":"/TRFIN/COMPANY_SERVICE"}}}`, "needs SERVICE and ENTITY_SET"},
		{"unknown entity set", "objects/TRFRONTEND/APP/HELP.json", `{"name":"/TRFDEMO/HELP","content":{"TEXTS":[{"LANGUAGE":"en","TITLE":"Help"}],
			"TARGET":{"KIND":"LIST","SERVICE":"/TRFIN/COMPANY_SERVICE","ENTITY_SET":"Nope"}}}`, "Nope is no entity set of /TRFIN/COMPANY_SERVICE"},
		{"tile of a missing app", "objects/TRFRONTEND/TILE/HELP_TILE.json", `{"name":"/TRFDEMO/HELP_TILE","content":{"APP":"/TRFDEMO/NOPE"}}`,
			"object /TRFDEMO/NOPE does not exist"},
		{"section of another namespace", "objects/TRFRONTEND/UI_ANNOTATIONS/COMPANIES_UI.json", `{"name":"/TRFDEMO/COMPANIES_UI","content":{"ENTITY":"/TRFIN/COMPANIES",
			"DETAIL":{"SECTIONS":[{"ID":"/TROTTO/MARKET"}]}}}`, "carries the namespace of another package"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
				initialized(t, k)
				pkgs := frontendPackages(t)
				pkgs[len(pkgs)-1] = demoWith(t, map[string]string{c.file: c.doc})
				_, err := k.Install(context.Background(), pkgs, kernel.Options{})
				if err == nil || !strings.Contains(err.Error()+objectErrors(err), c.want) {
					t.Fatalf("want %q, got %v", c.want, err)
				}
			})
		})
	}
}

// frontendServer installs the frontend with the demo content and gives
// adam the administration of the start page in ACME.
func frontendServer(t *testing.T, k *kernel.Kernel) client {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	if _, err := k.Install(ctx, frontendPackages(t), kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Users().Create(ctx, "adam", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "adam", Role: "/TRFRONTEND/ADMINISTRATOR", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := k.ActivateService(ctx, "/TRFRONTEND/CONFIGURATION_SERVICE", "test"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(k.HTTPHandler())
	t.Cleanup(srv.Close)
	return client{t, srv.URL}
}

const configRoot = "/rt/odata/trfrontend/configuration_service/"

func TestFrontendLocalConfiguration(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := frontendServer(t, k)
		adam := basicAuth("adam", "secret")
		withJSON := func(r *http.Request) { adam(r); r.Header.Set("Content-Type", "application/json") }

		// The shipped entries are read only.
		var list struct {
			Value []map[string]any `json:"value"`
		}
		a := c.do("GET", configRoot+"Apps?$orderby=NAME", adam, "")
		if a.status != 200 || json.Unmarshal([]byte(a.body), &list) != nil || len(list.Value) != 2 || list.Value[0]["NAME"] != "/TRFDEMO/COMPANIES" {
			t.Fatalf("shipped apps: %d %s", a.status, a.body)
		}

		// A local app needs the namespace /LOCAL/.
		a = c.do("POST", configRoot+"LocalApps", withJSON, `{"NAME":"/TRFDEMO/MINE","TARGET_KIND":"URL","URL":"https://example.org"}`)
		if a.status < 400 || !strings.Contains(a.body, "/LOCAL/") {
			t.Errorf("name without /LOCAL/: %d %s", a.status, a.body)
		}
		a = c.do("POST", configRoot+"LocalApps", withJSON, `{"NAME":"/LOCAL/MINE","TARGET_KIND":"URL","URL":"https://example.org"}`)
		if a.status != 201 {
			t.Fatalf("local app: %d %s", a.status, a.body)
		}

		// A local copy of a shipped entry, with its dependent entities.
		a = c.do("POST", configRoot+"COPY_APP", withJSON, `{"SOURCE":"/TRFDEMO/COMPANIES","NAME":"/LOCAL/COMPANIES"}`)
		if a.status != 200 || !strings.Contains(a.body, `"NAME":"/LOCAL/COMPANIES"`) {
			t.Fatalf("copy: %d %s", a.status, a.body)
		}
		key := "('" + url.PathEscape("/LOCAL/COMPANIES") + "')"
		a = c.do("GET", configRoot+"LocalApps"+key, adam, "")
		if a.status != 200 || !strings.Contains(a.body, `"ENTITY_SET":"Companies"`) || !strings.Contains(a.body, `"ICON":"building"`) {
			t.Errorf("copied app: %d %s", a.status, a.body)
		}
		a = c.do("GET", configRoot+"LocalAppTexts/$count", adam, "")
		if a.body != "2" {
			t.Errorf("copied texts: %s", a.body)
		}
		a = c.do("GET", configRoot+"LocalAppParameters/$count", adam, "")
		if a.body != "1" {
			t.Errorf("copied parameters: %s", a.body)
		}
		// A deep copy of a page with its sections and tiles.
		a = c.do("POST", configRoot+"COPY_PAGE", withJSON, `{"SOURCE":"/TRFDEMO/COMPANIES_PAGE","NAME":"/LOCAL/PAGE"}`)
		if a.status != 200 {
			t.Fatalf("copy of a page: %d %s", a.status, a.body)
		}
		if a := c.do("GET", configRoot+"LocalPageSectionTiles/$count", adam, ""); a.body != "3" {
			t.Errorf("copied tiles of sections: %s", a.body)
		}
		// Copying a missing entry creates nothing and says why.
		a = c.do("POST", configRoot+"COPY_TILE", withJSON, `{"SOURCE":"/TRFDEMO/NOPE","NAME":"/LOCAL/NOPE"}`)
		if !strings.Contains(a.body, "there is no shipped object /TRFDEMO/NOPE") {
			t.Errorf("copy of a missing entry: %d %s", a.status, a.body)
		}
		if a := c.do("GET", configRoot+"LocalTiles/$count", adam, ""); a.body != "0" {
			t.Errorf("tiles after a failed copy: %s", a.body)
		}
		// Without the administration the configuration is closed.
		if _, err := k.Users().Create(context.Background(), "bob", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		if a := c.do("GET", configRoot+"LocalApps", basicAuth("bob", "secret"), ""); a.status != 403 {
			t.Errorf("without role: %d", a.status)
		}
	})
}

// uiPackages are TRFRONTEND with TRFIN and TROTTO and their UI annotations.
func uiPackages(t *testing.T) []*model.Package {
	t.Helper()
	root := testutil.ModuleRoot(t)
	return []*model.Package{
		testutil.LoadDir(t, filepath.Join(root, "testdata", "frontend", "TRUI")),
		testutil.LoadDir(t, filepath.Join(root, "packages", "TRFRONTEND")),
		testutil.Example(t, "TRFIN"),
		testutil.Example(t, "TROTTO"),
		testutil.Example(t, "TRFINUI"),
		testutil.Example(t, "TROTTOUI"),
	}
}

// annotation returns the value of a term of a target in a metadata
// document in JSON.
func annotation(t *testing.T, doc []byte, target, term string) any {
	t.Helper()
	var md map[string]any
	if err := json.Unmarshal(doc, &md); err != nil {
		t.Fatal(err)
	}
	ns, _ := md["TRFIN"].(map[string]any)
	ann, _ := ns["$Annotations"].(map[string]any)
	targets, _ := ann[target].(map[string]any)
	return targets["@"+term]
}

func TestFrontendMetadata(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, uiPackages(t), kernel.Options{}); err != nil {
			t.Fatalf("%v\n%s", err, objectErrors(err))
		}
		meta := func(lang string) []byte {
			t.Helper()
			doc, err := k.ServiceMetadata(ctx, "/TRFIN/COMPANY_SERVICE", kernel.ServiceMetadataOptions{Language: lang, Format: "json"})
			if err != nil {
				t.Fatal(err)
			}
			return doc
		}
		doc := meta("en")
		if !strings.Contains(string(doc), `"$Alias": "Frontend"`) && !strings.Contains(string(doc), `"$Alias":"Frontend"`) {
			t.Errorf("no reference to the vocabulary: %s", doc)
		}
		list, _ := annotation(t, doc, "TRFIN.TRFIN_COMPANIES", "Frontend.ListPage").(map[string]any)
		var columns []string
		for _, c := range list["Columns"].([]any) {
			columns = append(columns, c.(map[string]any)["Property"].(map[string]any)["$PropertyPath"].(string))
		}
		if strings.Join(columns, " ") != "COMPANY_ID NAME TROTTO_OTTO_ID CITY COUNTRY STATUS" {
			t.Errorf("columns: %v", columns)
		}
		if fmt := list["Actions"].([]any); len(fmt) != 2 || fmt[0] != "TRFIN.BLOCK" || fmt[1] != "TRFIN.TROTTO_SYNC" {
			t.Errorf("actions: %v", list["Actions"])
		}
		detail, _ := annotation(t, doc, "TRFIN.TRFIN_COMPANIES", "Frontend.DetailPage").(map[string]any)
		var sections []string
		var main []string
		table := ""
		for _, s := range detail["Sections"].([]any) {
			sec := s.(map[string]any)
			sections = append(sections, sec["Id"].(string))
			if sec["Id"] == "GENERAL" {
				for _, f := range sec["Groups"].([]any)[0].(map[string]any)["Fields"].([]any) {
					main = append(main, f.(map[string]any)["$PropertyPath"].(string))
				}
			}
			if nav, ok := sec["Table"].(map[string]any); ok {
				table = nav["$NavigationPropertyPath"].(string)
			}
		}
		if strings.Join(sections, " ") != "GENERAL /TROTTOUI/MARKETPLACE CONTACTS" || strings.Join(main, " ") != "COMPANY_ID NAME TROTTO_OTTO_ID STATUS FOUNDED_ON" ||
			table == "" {
			t.Errorf("detail: sections %v, fields %v, table %q", sections, main, table)
		}
		header, _ := annotation(t, meta("de"), "TRFIN.TRFIN_COMPANIES", "Frontend.Header").(map[string]any)
		if header["TypeNamePlural"] != "Firmen" || header["Title"].(map[string]any)["$PropertyPath"] != "NAME" {
			t.Errorf("header in German: %v", header)
		}

		// A changed annotation reaches the metadata at once.
		dir := testutil.CopyExample(t, "TROTTOUI")
		file := filepath.Join(dir, "objects", "TRFRONTEND", "UI_ANNOTATIONS", "COMPANIES_UI.json")
		data, _ := os.ReadFile(file)
		testutil.WriteFile(t, dir, "objects/TRFRONTEND/UI_ANNOTATIONS/COMPANIES_UI.json", []byte(strings.Replace(string(data), `"WIDTH": 10`, `"WIDTH": 12`, 1)))
		testutil.WriteFile(t, dir, "manifest.json", []byte(strings.Replace(string(mustRead(t, filepath.Join(dir, "manifest.json"))), `"1.0.0"`, `"1.0.1"`, 1)))
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(meta("en")), `"Width": 12`) && !strings.Contains(string(meta("en")), `"Width":12`) {
			t.Errorf("metadata after the upgrade: %s", meta("en"))
		}
		if n := scalar(t, k, "/RT/METADATA_GENERATIONS", "GENERATION", where(k, "SERVICE_NAME"), "*"); n.(int64) < 2 {
			t.Errorf("metadata discarded %v times", n)
		}
	})
}
