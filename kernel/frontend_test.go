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
	"fmt"
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
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRFRONTEND")),
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
		a := c.do("GET", configRoot+"Apps?$orderby=NAME&$filter="+url.QueryEscape("PACKAGE_ID eq 'TRFDEMO'"), adam, "")
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
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRFRONTEND")),
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
	var ns map[string]any
	for k, v := range md {
		if !strings.HasPrefix(k, "$") {
			ns, _ = v.(map[string]any)
		}
	}
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

// homeKernel serves the frontend with the demo content: clara has the
// role of the company clerk and may start the service of companies, nora
// has the role but not the service. ACME has companies in Germany, Austria
// and the United States, one of them blocked.
func homeKernel(t *testing.T, k *kernel.Kernel) client {
	t.Helper()
	ctx := context.Background()
	c := frontendServer(t, k)
	for _, path := range []string{"/trfrontend"} {
		if err := k.ActivateHTTPService(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.ActivateService(ctx, "/TRFIN/COMPANY_SERVICE", "test"); err != nil {
		t.Fatal(err)
	}
	users := map[string][]string{
		"clara": {"/TRFIN/COMPANY_CLERK", "/TRFIN/ODATA_USER", "/TRFRONTEND/USER"},
		"nora":  {"/TRFIN/COMPANY_CLERK", "/TRFRONTEND/USER"},
	}
	for user, roles := range users {
		if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, role := range roles {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, country := range []string{"DE", "AT", "US"} {
		mustInsert(t, k, "/TRFIN/COUNTRIES", values{"COUNTRY_CODE": country, "NAME": country})
	}
	for i, x := range []struct{ id, country, status string }{{"C1", "DE", "ACTIVE"}, {"C2", "AT", "ACTIVE"}, {"C3", "DE", "BLOCKED"}, {"C4", "US", "ACTIVE"}} {
		mustInsert(t, k, "/TRFIN/COMPANIES", values{"/RT/TENANT": "ACME", "COMPANY_ID": x.id, "NAME": "Company " + string(rune('A'+i)),
			"COUNTRY": x.country, "STATUS": x.status})
	}
	return c
}

// home is the start page as the shell reads it.
type home struct {
	Spaces []struct {
		Name, Title string
		Pages       []struct {
			Name, Title string
			Sections    []struct {
				ID, Title string
				Tiles     []string
			}
		}
	}
	Tiles []struct{ Name, App, Kind, Title string }
	Apps  []struct {
		Name, Title string
		Target      struct{ Kind, URL, Entity_Set string }
	}
}

func readHome(t *testing.T, c client, user func(*http.Request), lang string) home {
	t.Helper()
	a := c.do("GET", "/trfrontend/api/home?lang="+lang, user, "")
	var h home
	if a.status != 200 || json.Unmarshal([]byte(a.body), &h) != nil {
		t.Fatalf("home: %d %s", a.status, a.body)
	}
	return h
}

func TestFrontendStartPage(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := homeKernel(t, k)
		clara, nora := basicAuth("clara", "secret"), basicAuth("nora", "secret")

		h := readHome(t, c, clara, "de")
		if len(h.Spaces) != 1 || h.Spaces[0].Name != "/TRFDEMO/COMPANIES_SPACE" || h.Spaces[0].Pages[0].Title != "Firmen" {
			t.Fatalf("spaces: %+v", h.Spaces)
		}
		// The tile of the help is in no app collection of the role.
		if tiles := h.Spaces[0].Pages[0].Sections[0].Tiles; strings.Join(tiles, " ") != "/TRFDEMO/COMPANIES_TILE /TRFDEMO/ACTIVE_TILE" {
			t.Errorf("tiles of the section: %v", tiles)
		}
		if len(h.Apps) != 1 || h.Apps[0].Title != "Firmen" || h.Apps[0].Target.URL != "/rt/odata/trfin/company_service/" || h.Apps[0].Target.Kind != "LIST" {
			t.Errorf("apps: %+v", h.Apps)
		}
		// Without the start of the service, the app and its tiles are gone.
		if h := readHome(t, c, nora, "en"); len(h.Apps) != 0 || len(h.Tiles) != 0 || len(h.Spaces[0].Pages[0].Sections) != 0 {
			t.Errorf("start page of nora: %+v", h)
		}
		// The metric counts what the user may read: active companies in
		// Germany, Austria and Poland to Slovakia.
		a := c.do("GET", "/trfrontend/api/tile-value?name="+url.QueryEscape("/TRFDEMO/ACTIVE_TILE"), clara, "")
		if a.status != 200 || !strings.Contains(a.body, `"VALUE":2`) {
			t.Errorf("metric: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/tile-value?name="+url.QueryEscape("/TRFDEMO/ACTIVE_TILE"), nora, ""); a.status != 404 {
			t.Errorf("metric of a tile nora does not see: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/app?name="+url.QueryEscape("/TRFDEMO/COMPANIES"), clara, ""); a.status != 200 || !strings.Contains(a.body, `"PARAMETERS":[{"NAME":"mode"`) {
			t.Errorf("app: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/app?name="+url.QueryEscape("/TRFDEMO/HELP"), clara, ""); a.status != 404 {
			t.Errorf("app outside the collections: %d", a.status)
		}
		if a := c.do("GET", "/trfrontend/api/home", nil, ""); a.status != 401 {
			t.Errorf("without sign-in: %d", a.status)
		}

		// Local entries join the shipped ones.
		adam := func(r *http.Request) {
			basicAuth("adam", "secret")(r)
			r.Header.Set("Content-Type", "application/json")
		}
		for _, x := range []struct{ set, body string }{
			{"LocalApps", `{"NAME":"/LOCAL/WIKI","TARGET_KIND":"URL","URL":"https://example.org/wiki","LocalAppTexts":[{"LANGUAGE":"en","TITLE":"Wiki"}]}`},
			{"LocalTiles", `{"NAME":"/LOCAL/WIKI_TILE","APP":"/LOCAL/WIKI","SIZE":"WIDE"}`},
			{"LocalAppCollections", `{"NAME":"/LOCAL/TOOLS","LocalAppCollectionTexts":[{"LANGUAGE":"en","TITLE":"Tools"}],
				"LocalAppCollectionTiles":[{"POSITION":1,"TILE":"/LOCAL/WIKI_TILE"},{"POSITION":2,"TILE":"/TRFDEMO/HELP_TILE"}]}`},
			{"LocalPages", `{"NAME":"/LOCAL/TOOLS_PAGE","LocalPageTexts":[{"LANGUAGE":"en","TITLE":"Tools"}],
				"LocalPageSections":[{"SECTION":"MAIN","POSITION":1,"LocalPageSectionTiles":[{"POSITION":1,"TILE":"/LOCAL/WIKI_TILE"},{"POSITION":2,"TILE":"/TRFDEMO/ACTIVE_TILE"}]}]}`},
			{"LocalSpaces", `{"NAME":"/LOCAL/TOOLS_SPACE","LocalSpaceTexts":[{"LANGUAGE":"en","TITLE":"Tools"}],"LocalSpacePages":[{"POSITION":1,"PAGE":"/LOCAL/TOOLS_PAGE"}]}`},
			{"LocalRoleContents", `{"ROLE":"/TRFIN/COMPANY_CLERK","LocalRoleCollections":[{"APP_COLLECTION":"/LOCAL/TOOLS"}],
				"LocalRoleSpaces":[{"POSITION":1,"SPACE":"/LOCAL/TOOLS_SPACE"}]}`},
		} {
			if a := c.do("POST", configRoot+x.set, adam, x.body); a.status != 201 {
				t.Fatalf("%s: %d %s", x.set, a.status, a.body)
			}
		}
		h = readHome(t, c, clara, "en")
		var spaces, apps []string
		for _, s := range h.Spaces {
			spaces = append(spaces, s.Name)
		}
		for _, a := range h.Apps {
			apps = append(apps, a.Name)
		}
		if strings.Join(spaces, " ") != "/TRFDEMO/COMPANIES_SPACE /LOCAL/TOOLS_SPACE" || strings.Join(apps, " ") != "/TRFDEMO/COMPANIES /LOCAL/WIKI /TRFDEMO/HELP" {
			t.Errorf("with local entries: spaces %v, apps %v", spaces, apps)
		}
		if tiles := h.Spaces[0].Pages[0].Sections[0].Tiles; len(tiles) != 3 {
			t.Errorf("the help tile is in a collection of the role now: %v", tiles)
		}
		if tiles := h.Spaces[1].Pages[0].Sections[0].Tiles; strings.Join(tiles, " ") != "/LOCAL/WIKI_TILE /TRFDEMO/ACTIVE_TILE" {
			t.Errorf("local page: %v", tiles)
		}
	})
}

func TestFrontendShell(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := homeKernel(t, k)
		// The page of the shell needs no sign-in, at the node and as index.html.
		for _, p := range []string{"/trfrontend", "/trfrontend/", "/trfrontend/index.html"} {
			a := c.do("GET", p, nil, "")
			if a.status != 200 || !strings.Contains(a.body, `<script type="importmap">`) || !strings.HasPrefix(a.header.Get("Content-Type"), "text/html") {
				t.Errorf("%s: %d %s %s", p, a.status, a.header.Get("Content-Type"), a.body)
			}
		}
		if a := c.do("POST", "/trfrontend/", nil, "x"); a.status == 200 {
			t.Errorf("POST to the shell: %d", a.status)
		}
		if a := c.do("GET", "/trfrontend/missing.js", nil, ""); a.status != 404 {
			t.Errorf("missing file: %d", a.status)
		}

		// The shell signs in at /rt/session and reads the start page with the cookie.
		a := c.do("POST", "/rt/session", func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }, `{"user":"clara","password":"secret","language":"de"}`)
		if a.status != 200 {
			t.Fatalf("sign-in: %d %s", a.status, a.body)
		}
		var cookie string
		for _, sc := range a.header.Values("Set-Cookie") {
			if strings.HasPrefix(sc, "rt_session=") {
				cookie = strings.SplitN(sc, ";", 2)[0]
			}
		}
		withCookie := func(r *http.Request) { r.Header.Set("Cookie", cookie) }
		h := c.do("GET", "/trfrontend/api/home", withCookie, "")
		if h.status != 200 || !strings.Contains(h.body, `"TITLE":"Firmen"`) {
			t.Errorf("start page with the session: %d %s", h.status, h.body)
		}
	})
}

func TestFrontendRolesAPI(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := frontendServer(t, k)
		ctx := context.Background()
		if err := k.ActivateHTTPService(ctx, "/trfrontend"); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Users().Create(ctx, "bob", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		adam := func(r *http.Request) {
			basicAuth("adam", "secret")(r)
			r.Header.Set("Content-Type", "application/json")
		}
		// The administrator gets the shipped administration on the start page.
		h := readHome(t, c, adam, "de")
		if len(h.Spaces) != 1 || h.Spaces[0].Name != "/TRFRONTEND/ADMINISTRATION_SPACE" || len(h.Apps) != 7 || len(h.Tiles) != 7 {
			t.Errorf("start page of the administrator: %+v", h)
		}
		meta := c.do("GET", configRoot+"$metadata?$format=json", adam, "")
		lp := annotation(t, []byte(meta.body), "TRFRONTEND.TRFRONTEND_LOCAL_APPS", "Frontend.ListPage")
		if m, _ := lp.(map[string]any); m["MergeSet"] != "Apps" || fmt.Sprint(m["Actions"]) != "[TRFRONTEND.COPY_APP]" {
			t.Errorf("list page of local apps: %v", lp)
		}
		if a := c.do("GET", "/trfrontend/api/roles", adam, ""); a.status != 200 || !strings.Contains(a.body, `"NAME":"/TRFRONTEND/USER"`) {
			t.Fatalf("roles: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/role?name="+url.QueryEscape("/TRFRONTEND/USER"), adam, ""); a.status != 200 || !strings.Contains(a.body, `"KIND":"VALUE"`) {
			t.Errorf("package role: %d %s", a.status, a.body)
		}
		role := `{"NAME":"/LOCAL/CLERKS","COMPOSITE":false,"TEXTS":[{"LANGUAGE":"en","LABEL":"Clerks","DESCRIPTION":null}],"ROLES":[],"AUTHORIZATIONS":[` +
			`{"OBJECT":"/TRBASE/HTTP_SERVICE","FIELDS":[{"FIELD":"PACKAGE_ID","VALUES":[{"KIND":"VALUE","LOW":"TRFRONTEND","HIGH":null}]},` +
			`{"FIELD":"SERVICE_PATH","VALUES":[{"KIND":"PATTERN","LOW":"/trfrontend*","HIGH":null}]},` +
			`{"FIELD":"ACTIVITY","VALUES":[{"KIND":"VALUE","LOW":"EXECUTE","HIGH":null}]}],"ROLE_NAME":null,"COMPOSITE":null}]}`
		if a := c.do("POST", "/trfrontend/api/role", adam, role); a.status != 201 {
			t.Fatalf("create: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trfrontend/api/role", adam, role); a.status != 400 || !strings.Contains(a.body, "role-error") {
			t.Errorf("create twice: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/role?name="+url.QueryEscape("/LOCAL/CLERKS"), adam, ""); a.status != 200 || !strings.Contains(a.body, `"LABEL":"Clerks"`) || !strings.Contains(a.body, `"LOW":"/trfrontend*"`) {
			t.Errorf("role: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/role", adam, ""); a.status != 400 || !strings.Contains(a.body, "the parameter name is missing") {
			t.Errorf("role without name: %d %s", a.status, a.body)
		}
		a := c.do("POST", "/trfrontend/api/assignments", adam, `{"USER_NAME":"bob","ROLE_NAME":"/LOCAL/CLERKS","TENANT":"ACME","VALID_FROM":null,"VALID_TO":null}`)
		var assigned struct{ Assignment struct{ ID string } }
		if a.status != 201 || json.Unmarshal([]byte(a.body), &assigned) != nil || assigned.Assignment.ID == "" {
			t.Fatalf("assign: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/assignments?user=bob", adam, ""); a.status != 200 || !strings.Contains(a.body, `"ROLE_NAME":"/LOCAL/CLERKS"`) {
			t.Errorf("assignments: %d %s", a.status, a.body)
		}
		// A role in use needs a confirmation to go.
		if a := c.do("DELETE", "/trfrontend/api/role?name="+url.QueryEscape("/LOCAL/CLERKS"), adam, ""); a.status != 409 || !strings.Contains(a.body, "confirmation-required") {
			t.Errorf("delete in use: %d %s", a.status, a.body)
		}
		if a := c.do("DELETE", "/trfrontend/api/assignment?id="+url.QueryEscape(assigned.Assignment.ID), adam, ""); a.status != 204 {
			t.Errorf("unassign: %d %s", a.status, a.body)
		}
		if a := c.do("DELETE", "/trfrontend/api/role?name="+url.QueryEscape("/LOCAL/CLERKS"), adam, ""); a.status != 204 {
			t.Errorf("delete: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trfrontend/api/history?role="+url.QueryEscape("/LOCAL/CLERKS"), adam, ""); a.status != 200 || !strings.Contains(a.body, `"CHANGED_BY":"adam"`) {
			t.Errorf("history: %d %s", a.status, a.body)
		}
		for _, p := range []string{"users", "authorization-objects", "authorizations?user=bob&tenant=ACME"} {
			if a := c.do("GET", "/trfrontend/api/"+p, adam, ""); a.status != 200 {
				t.Errorf("%s: %d %s", p, a.status, a.body)
			}
		}
		// Without the administration of roles bob sees no roles and may change none.
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "bob", Role: "/TRFRONTEND/USER", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		bob := func(r *http.Request) {
			basicAuth("bob", "secret")(r)
			r.Header.Set("Content-Type", "application/json")
		}
		if a := c.do("GET", "/trfrontend/api/roles", bob, ""); a.status != 200 || a.body != "[]" {
			t.Errorf("roles of bob: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trfrontend/api/role", bob, role); a.status != 403 {
			t.Errorf("create by bob: %d %s", a.status, a.body)
		}
	})
}

func TestFrontendExamples(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, uiPackages(t), kernel.Options{}); err != nil {
			t.Fatalf("%v\n%s", err, objectErrors(err))
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		if err := k.ActivateHTTPService(ctx, "/trfrontend"); err != nil {
			t.Fatal(err)
		}
		if err := k.ActivateService(ctx, "/TRFIN/COMPANY_SERVICE", "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Users().Create(ctx, "olga", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"/TRFIN/COMPANY_CLERK", "/TRFIN/ODATA_USER", "/TRFRONTEND/USER", "/TROTTO/SYNC_OPERATOR"} {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "olga", Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
		srv := httptest.NewServer(k.HTTPHandler())
		t.Cleanup(srv.Close)
		c := client{t, srv.URL}
		h := readHome(t, c, basicAuth("olga", "secret"), "de")
		var spaces []string
		for _, s := range h.Spaces {
			spaces = append(spaces, s.Name+"="+s.Title)
		}
		if strings.Join(spaces, " ") != "/TRFINUI/COMPANIES_SPACE=Firmen /TROTTOUI/OTTO_SPACE=OTTO" || len(h.Tiles) != 3 || len(h.Apps) != 2 {
			t.Errorf("start page of the examples: %+v", h)
		}
		a := c.do("GET", "/trfrontend/api/tile-value?name="+url.QueryEscape("/TROTTOUI/WITHOUT_PARTNER_TILE"), basicAuth("olga", "secret"), "")
		if a.status != 200 || !strings.Contains(a.body, `"VALUE":0`) {
			t.Errorf("metric of TROTTOUI: %d %s", a.status, a.body)
		}
	})
}
