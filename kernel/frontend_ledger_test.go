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
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// ledgerUIPackages are TRFRONTEND with a stand-in for TRUI, TRBP, TRFIN
// and their user interfaces TRBPUI and TRFINUI.
func ledgerUIPackages(t *testing.T) []*model.Package {
	t.Helper()
	root := testutil.ModuleRoot(t)
	return []*model.Package{
		testutil.LoadDir(t, filepath.Join(root, "testdata", "frontend", "TRUI")),
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRFRONTEND")),
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRBP")),
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRFIN")),
		testutil.LoadDir(t, filepath.Join(root, "packages", "TRBPUI")),
		testutil.LoadDir(t, filepath.Join(root, "packages", "TRFINUI")),
	}
}

// csdl is a metadata document in JSON: the entity types of its namespace
// and their annotations.
type csdl struct {
	namespace   string
	types       []string
	annotations map[string]map[string]any
}

func readCSDL(t *testing.T, doc []byte) csdl {
	t.Helper()
	var md map[string]any
	if err := json.Unmarshal(doc, &md); err != nil {
		t.Fatal(err)
	}
	c := csdl{annotations: map[string]map[string]any{}}
	for k, v := range md {
		if strings.HasPrefix(k, "$") {
			continue
		}
		c.namespace = k
		ns, _ := v.(map[string]any)
		for name, el := range ns {
			if m, ok := el.(map[string]any); ok && m["$Kind"] == "EntityType" {
				c.types = append(c.types, name)
			}
		}
		ann, _ := ns["$Annotations"].(map[string]any)
		for target, terms := range ann {
			c.annotations[target], _ = terms.(map[string]any)
		}
	}
	sort.Strings(c.types)
	return c
}

// term is the value of a term of an entity type, nil without it.
func (c csdl) term(typ, term string) map[string]any {
	v, _ := c.annotations[c.namespace+"."+typ]["@Frontend."+term].(map[string]any)
	return v
}

// paths are the values of $PropertyPath in a collection, directly or in
// the member key of its records.
func paths(list any, key string) string {
	var out []string
	items, _ := list.([]any)
	for _, x := range items {
		m, _ := x.(map[string]any)
		if key != "" {
			m, _ = m[key].(map[string]any)
		}
		if p, ok := m["$PropertyPath"].(string); ok {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// sections are the IDs of the sections of a detail page, with the
// navigation of their table as ID>navigation, e.g. ROLES>PartnerRoles.
func sections(detail map[string]any) string {
	var out []string
	items, _ := detail["Sections"].([]any)
	for _, x := range items {
		s, _ := x.(map[string]any)
		id, _ := s["Id"].(string)
		if nav, ok := s["Table"].(map[string]any); ok {
			id += ">" + nav["$NavigationPropertyPath"].(string)
		}
		out = append(out, id)
	}
	return strings.Join(out, " ")
}

func TestFrontendLedgerUIMetadata(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		p, err := k.Install(ctx, ledgerUIPackages(t), kernel.Options{})
		if err != nil {
			t.Fatalf("%v\n%s", err, objectErrors(err))
		}
		for _, c := range p.Changes {
			if c.Kind == plan.ObjectMessage {
				t.Errorf("message of %s: %s", c.Name, c.Detail)
			}
		}
		meta := func(service, lang string) csdl {
			t.Helper()
			doc, err := k.ServiceMetadata(ctx, service, kernel.ServiceMetadataOptions{Language: lang, Format: "json"})
			if err != nil {
				t.Fatal(err)
			}
			return readCSDL(t, doc)
		}

		// Every entity type of the services has a list and a detail page,
		// but the partner roles, which are objects of a type.
		for _, service := range []string{"/TRBP/PARTNER_SERVICE", "/TRFIN/CONFIGURATION_SERVICE", "/TRFIN/MASTER_DATA_SERVICE", "/TRFIN/JOURNAL_ENTRY_SERVICE"} {
			m := meta(service, "en")
			for _, typ := range m.types {
				if typ == "TRBP_PARTNER_ROLE" {
					continue
				}
				if m.term(typ, "ListPage") == nil || m.term(typ, "DetailPage") == nil || m.term(typ, "Header") == nil {
					t.Errorf("%s: %s has no pages: %v", service, typ, m.annotations[m.namespace+"."+typ])
				}
			}
		}

		// The partner in its own service, which offers the company data of
		// the extension of TRFIN along with the partner.
		bp := meta("/TRBP/PARTNER_SERVICE", "en")
		partner := "TRBP_PARTNERS"
		if got := paths(bp.term(partner, "ListPage")["Columns"], "Property"); got != "PARTNER CATEGORY NAME1 FIRST_NAME LAST_NAME SEARCH_TERM GROUPING BLOCKED" {
			t.Errorf("columns of partners: %s", got)
		}
		if got := sections(bp.term(partner, "DetailPage")); got != "GENERAL ROLES>PartnerRoles /TRFINUI/CUSTOMER_DATA>TRFIN_CUSTOMER_COMPANIES /TRFINUI/SUPPLIER_DATA>TRFIN_SUPPLIER_COMPANIES" {
			t.Errorf("sections of partners in TRBP: %s", got)
		}
		// In the master data of TRFIN the partner shows its company data.
		md := meta("/TRFIN/MASTER_DATA_SERVICE", "de")
		got := sections(md.term(partner, "DetailPage"))
		if !strings.HasPrefix(got, "GENERAL ROLES /TRFINUI/CUSTOMER_DATA>") || !strings.Contains(got, " /TRFINUI/SUPPLIER_DATA>") {
			t.Errorf("sections of partners in TRFIN: %s", got)
		}
		if h := md.term("TRFIN_CUSTOMER_COMPANIES", "Header"); h["TypeNamePlural"] != "Debitoren" {
			t.Errorf("header of customers: %v", h)
		}

		// Configuration and journal entries.
		cs := meta("/TRFIN/CONFIGURATION_SERVICE", "de")
		if h := cs.term("TRFIN_COMPANIES", "Header"); h["TypeNamePlural"] != "Buchungskreise" {
			t.Errorf("header of companies: %v", h)
		}
		if got := sections(cs.term("TRFIN_STATEMENT_NODES", "DetailPage")); !strings.Contains(got, "RANGES>") || !strings.Contains(got, "TEXTS>") {
			t.Errorf("sections of statement nodes: %s", got)
		}
		js := meta("/TRFIN/JOURNAL_ENTRY_SERVICE", "en")
		entry := js.term("TRFIN_JOURNAL_ENTRIES", "DetailPage")
		if got := sections(entry); !strings.Contains(got, "ITEMS>") || !strings.Contains(got, "LEDGER_ENTRIES>") || !strings.Contains(got, "CLEARED_ITEMS>") {
			t.Errorf("sections of journal entries: %s", got)
		}
		if acts, _ := entry["Actions"].([]any); len(acts) != 2 || !strings.HasSuffix(acts[0].(string), ".REVERSE") || !strings.HasSuffix(acts[1].(string), ".RESET_CLEARING") {
			t.Errorf("actions of journal entries: %v", entry["Actions"])
		}
		if got := paths(js.term("TRFIN_LEDGER_ENTRIES", "ListPage")["FilterFields"], ""); !strings.Contains(got, "OPEN_ITEM") || !strings.HasPrefix(got, "LEDGER COMPANY") {
			t.Errorf("filters of ledger entries: %s", got)
		}
	})
}

func TestFrontendLedgerUIStartPage(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, ledgerUIPackages(t), kernel.Options{}); err != nil {
			t.Fatalf("%v\n%s", err, objectErrors(err))
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		if err := k.ActivateHTTPService(ctx, "/trfrontend"); err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"/TRBP/PARTNER_SERVICE", "/TRFIN/CONFIGURATION_SERVICE", "/TRFIN/MASTER_DATA_SERVICE", "/TRFIN/JOURNAL_ENTRY_SERVICE"} {
			if err := k.ActivateService(ctx, s, "test"); err != nil {
				t.Fatal(err)
			}
		}
		users := map[string][]string{
			"setup": {"/TRFIN/CONFIGURATION", "/TRFIN/MASTER_DATA", "/TRFIN/ACCOUNTANT", "/TRBP/PARTNER_ADMIN"},
			"paula": {"/TRFRONTEND/USER", "/TRBP/PARTNER_ADMIN", "/TRBP/ODATA_USER"},
			"anna":  {"/TRFRONTEND/USER", "/TRFIN/ACCOUNTANT", "/TRFIN/ODATA_USER"},
			"mark":  {"/TRFRONTEND/USER", "/TRFIN/MASTER_DATA", "/TRFIN/ODATA_USER"},
			"conny": {"/TRFRONTEND/USER", "/TRFIN/CONFIGURATION", "/TRFIN/ODATA_USER", "/TRBP/ODATA_USER"},
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
		if _, err := k.Run(ctx, kernel.ExecOptions{Tenant: "ACME", User: model.Some("setup")}, "/TRFIN/SAMPLE_DATA", "SETUP", nil); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(k.HTTPHandler())
		t.Cleanup(srv.Close)
		c := client{t, srv.URL}

		start := func(user, lang string) (spaces, apps string, tiles map[string]bool) {
			t.Helper()
			h := readHome(t, c, basicAuth(user, "secret"), lang)
			var s, a []string
			for _, x := range h.Spaces {
				var pages []string
				for _, p := range x.Pages {
					pages = append(pages, p.Name)
				}
				s = append(s, x.Name+"="+x.Title+"["+strings.Join(pages, ",")+"]")
			}
			for _, x := range h.Apps {
				a = append(a, x.Name)
			}
			tiles = map[string]bool{}
			for _, x := range h.Tiles {
				tiles[x.Name] = true
			}
			sort.Strings(a)
			return strings.Join(s, " "), strings.Join(a, " "), tiles
		}

		spaces, apps, _ := start("paula", "de")
		if spaces != "/TRBPUI/PARTNERS_SPACE=Geschäftspartner[/TRBPUI/PARTNERS_PAGE]" || apps != "/TRBPUI/GROUPINGS /TRBPUI/PARTNERS" {
			t.Errorf("start page of the partner administrator: %s; %s", spaces, apps)
		}
		spaces, apps, tiles := start("anna", "en")
		if spaces != "/TRFINUI/ACCOUNTING_SPACE=Accounting[/TRFINUI/ACCOUNTING_PAGE] /TRFINUI/MASTER_DATA_SPACE=General ledger master data[/TRFINUI/MASTER_DATA_PAGE]" ||
			!strings.Contains(apps, "/TRFINUI/JOURNAL_ENTRIES") || !strings.Contains(apps, "/TRFINUI/CUSTOMERS") || strings.Contains(apps, "/TRFINUI/COMPANIES") ||
			!tiles["/TRFINUI/OPEN_ITEMS_TILE"] {
			t.Errorf("start page of the accountant: %s; %s; %v", spaces, apps, tiles)
		}
		spaces, apps, _ = start("mark", "en")
		if spaces != "/TRFINUI/MASTER_DATA_SPACE=General ledger master data[/TRFINUI/MASTER_DATA_PAGE]" || strings.Contains(apps, "/TRFINUI/JOURNAL_ENTRIES") {
			t.Errorf("start page of master data: %s; %s", spaces, apps)
		}
		spaces, apps, _ = start("conny", "de")
		if spaces != "/TRFINUI/CONFIGURATION_SPACE=Konfiguration Hauptbuch[/TRFINUI/CONFIGURATION_PAGE,/TRBPUI/CONFIGURATION_PAGE]" ||
			!strings.Contains(apps, "/TRBPUI/GROUPINGS") || !strings.Contains(apps, "/TRFINUI/DOCUMENT_TYPES") || strings.Contains(apps, "/TRBPUI/PARTNERS") {
			t.Errorf("start page of the configuration: %s; %s", spaces, apps)
		}

		// The metric of open items counts the open items of the sample data
		// as the service does.
		tile := func(user, name string) string {
			t.Helper()
			a := c.do("GET", "/trfrontend/api/tile-value?name="+url.QueryEscape(name), basicAuth(user, "secret"), "")
			var v struct{ VALUE json.Number }
			if a.status != 200 || json.Unmarshal([]byte(a.body), &v) != nil {
				t.Fatalf("tile %s: %d %s", name, a.status, a.body)
			}
			return v.VALUE.String()
		}
		a := c.do("GET", "/rt/odata/trfin/journal_entry_service/JournalEntryItems/$count?$filter="+url.QueryEscape("OPEN_ITEM eq true and CLEARING_NO eq null"), basicAuth("anna", "secret"), "")
		if a.status != 200 || a.body == "0" {
			t.Fatalf("open items: %d %s", a.status, a.body)
		}
		if got := tile("anna", "/TRFINUI/OPEN_ITEMS_TILE"); got != strings.TrimSpace(a.body) {
			t.Errorf("open items on the tile: %s, service %s", got, a.body)
		}
		if got := tile("paula", "/TRBPUI/BLOCKED_PARTNERS_TILE"); got != "0" {
			t.Errorf("blocked partners: %s", got)
		}
	})
}
