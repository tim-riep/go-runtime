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
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

const serviceFile = "schema/services/SHOP.json"

// serviceFiles are the files of TRSV, which offers the companies of TRFIN
// with the extension of TROTTO, the countries for reading, the exchange
// rates as table of configuration and a business object of its own that
// allows only updates.
func serviceFiles() map[string]string {
	return map[string]string{
		"manifest.json": `{ "id": "TRSV", "version": "1.0.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }, { "id": "TRFIN" }] }`,
		"schema/tables/SETUP.json": `{ "name": "/TRSV/SETUP", "tenantSpecific": false, "dataClass": "configuration", "fields": [
			{ "name": "SETUP_ID", "domain": "/TRBASE/NAME" }, { "name": "VALUE", "domain": "/TRBASE/TEXT" } ], "primaryKey": ["SETUP_ID"] }`,
		"schema/business-objects/SETUP.json": `{ "name": "/TRSV/SETUP", "description": { "en": "Setup" }, "dataClass": "configuration",
			"root": "/TRSV/SETUP", "entities": { "/TRSV/SETUP": { "operations": ["update"] } } }`,
		serviceFile: `{ "name": "/TRSV/SHOP", "description": { "en": "Shop", "de": "Laden" },
			"entitySets": {
				"Companies": { "businessObject": "/TRFIN/COMPANY", "entity": "/TRFIN/COMPANIES", "actions": ["BLOCK"], "hiddenFields": ["CAPITAL"] },
				"Contacts": { "businessObject": "/TRFIN/COMPANY", "entity": "/TRFIN/CONTACTS" },
				"Countries": { "businessObject": "/TRFIN/COUNTRY", "entity": "/TRFIN/COUNTRIES", "readOnly": true },
				"ExchangeRates": { "table": "/TRFIN/EXCHANGE_RATES" },
				"Setup": { "businessObject": "/TRSV/SETUP", "entity": "/TRSV/SETUP" }
			} }`,
	}
}

// withService loads TRSV with changed files into the schema of the
// examples.
func withService(t *testing.T, change func(files map[string]string)) *model.Schema {
	t.Helper()
	files := serviceFiles()
	change(files)
	fsys := fstest.MapFS{}
	for name, text := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(text)}
	}
	p, err := pkgload.Load(fsys, "TRSV")
	if err != nil {
		t.Fatal(err)
	}
	return withTRS(t, p)
}

func TestServiceModel(t *testing.T) {
	s := withService(t, func(map[string]string) {})
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	m, problems := odata.Build(s, nil, s.Services["/TRSV/SHOP"])
	if len(problems) > 0 {
		t.Fatal(odata.Problems(problems))
	}
	if got := s.Services["/TRSV/SHOP"].Path(); got != "/rt/odata/trsv/shop/" {
		t.Errorf("path %s", got)
	}
	companies := m.Set("Companies").Type
	if companies.Name != "TRFIN_COMPANIES" || companies.Property("TROTTO_OTTO_ID") == nil || companies.Property("CAPITAL") != nil ||
		companies.Property("/RT/TENANT") != nil || len(companies.Key) != 1 || companies.Key[0].Name != "COMPANY_ID" || companies.ETag.Name != "CHANGED_AT" {
		t.Errorf("companies: %+v", companies)
	}
	if p := companies.Property("TROTTO_OTTO_ID"); !p.Computed || p.Field.Name != "/TROTTO/OTTO_ID" {
		t.Errorf("extension field: %+v", p)
	}
	if p := companies.Property("STATUS"); !p.Computed || companies.Property("COMPANY_ID").Computed || !companies.Property("COMPANY_ID").Immutable ||
		!companies.Property("NAME").Mandatory || !companies.Property("CREATED_AT").Computed {
		t.Errorf("field properties: %+v", companies.Properties)
	}
	var actions []string
	for _, a := range companies.Actions {
		actions = append(actions, a.Name)
	}
	if strings.Join(actions, ",") != "BLOCK,TROTTO_SYNC" {
		t.Errorf("actions %v: listed ones of TRFIN, those of the extension of TROTTO", actions)
	}
	contacts := companies.Navigation("Contacts")
	if contacts == nil || !contacts.Collection || contacts.Partner != "Companies" || contacts.Target.Name != "Contacts" {
		t.Errorf("navigation to contacts: %+v", contacts)
	}
	if n := m.Set("Contacts").Type.Navigation("Companies"); n == nil || n.Collection || n.Partner != "Contacts" {
		t.Errorf("navigation to the company: %+v", n)
	}
	if n := companies.Navigation("Countries"); n == nil || n.Collection || n.ForeignKey != "COUNTRY" {
		t.Errorf("navigation along the foreign key: %+v", n)
	}
	channels := companies.Navigation("TROTTO_COMPANY_CHANNELS")
	if channels == nil || !channels.Contained || channels.Target != nil || len(m.Contained) != 1 || !channels.Type.Allows(model.OperationCreate) {
		t.Errorf("contained channels: %+v", channels)
	}
	if c := m.Set("Countries").Type; len(c.Operations) != 0 || len(c.Actions) != 0 {
		t.Errorf("read-only countries: %+v", c)
	}
	if r := m.Set("ExchangeRates").Type; len(r.Operations) != 0 || r.Property("RATE") == nil {
		t.Errorf("table: %+v", r)
	}
	if ops := m.Set("Setup").Type.Operations; len(ops) != 1 || ops[0] != model.OperationUpdate {
		t.Errorf("setup: %v", ops)
	}

	none := withService(t, func(f map[string]string) {
		replaceIn(f, serviceFile, `"description": { "en": "Shop", "de": "Laden" },`, `"description": { "en": "Shop" }, "extensions": "none",`)
	})
	m, _ = odata.Build(none, nil, none.Services["/TRSV/SHOP"])
	companies = m.Set("Companies").Type
	if companies.Property("TROTTO_OTTO_ID") != nil || companies.Action("TROTTO_SYNC") != nil || companies.Navigation("TROTTO_COMPANY_CHANNELS") != nil {
		t.Errorf("a service without extensions offers those of TROTTO: %+v", companies)
	}
}

func TestServiceRules(t *testing.T) {
	for _, c := range []struct {
		name     string
		change   func(files map[string]string)
		contains string
	}{
		{"more operations than the business object", func(f map[string]string) {
			replaceIn(f, serviceFile, `"entity": "/TRSV/SETUP" }`, `"entity": "/TRSV/SETUP", "operations": ["update", "create"] }`)
		}, "/TRSV/SETUP of /TRSV/SETUP does not allow create; a service offers at most the operations of the entity"},
		{"unknown action", func(f map[string]string) {
			replaceIn(f, serviceFile, `"actions": ["BLOCK"]`, `"actions": ["BLOCK", "ARCHIVE"]`)
		}, "/TRFIN/COMPANIES of /TRFIN/COMPANY has no action ARCHIVE"},
		{"hidden key field", func(f map[string]string) {
			replaceIn(f, serviceFile, `"hiddenFields": ["CAPITAL"]`, `"hiddenFields": ["COMPANY_ID"]`)
		}, "the key field COMPANY_ID cannot be hidden"},
		{"hidden mandatory field", func(f map[string]string) {
			replaceIn(f, serviceFile, `"hiddenFields": ["CAPITAL"]`, `"hiddenFields": ["NAME"]`)
		}, "the mandatory field NAME cannot be hidden"},
		{"hidden unknown field", func(f map[string]string) {
			replaceIn(f, serviceFile, `"hiddenFields": ["CAPITAL"]`, `"hiddenFields": ["CAPITOL"]`)
		}, "/TRFIN/COMPANIES has no field CAPITOL"},
		{"collision of public names", func(f map[string]string) {
			replaceIn(f, serviceFile, `"Countries": {`, `"COUNTRY": {`)
		}, "COUNTRY and the navigation COUNTRY of /TRFIN/COMPANIES both have the public name COUNTRY"},
		{"named navigation colliding", func(f map[string]string) {
			replaceIn(f, serviceFile, `"hiddenFields": ["CAPITAL"]`, `"hiddenFields": ["CAPITAL"], "navigations": { "COUNTRY": "NAME" }`)
		}, "NAME and the navigation COUNTRY of /TRFIN/COMPANIES both have the public name NAME"},
		{"table of application data", func(f map[string]string) {
			replaceIn(f, serviceFile, `"table": "/TRFIN/EXCHANGE_RATES"`, `"table": "/TRFIN/ORDERS"`)
		}, "/TRFIN/ORDERS has the data class application; a service offers tables without business object only of configuration"},
		{"table of a business object", func(f map[string]string) {
			replaceIn(f, serviceFile, `"table": "/TRFIN/EXCHANGE_RATES"`, `"table": "/TRFIN/COUNTRIES"`)
		}, "/TRFIN/COUNTRIES belongs to the business object /TRFIN/COUNTRY; offer it with businessObject and entity"},
		{"read only with operations", func(f map[string]string) {
			replaceIn(f, serviceFile, `"readOnly": true }`, `"readOnly": true, "operations": ["update"] }`)
		}, "a read-only entity set offers no operations"},
		{"entity of another business object", func(f map[string]string) {
			replaceIn(f, serviceFile, `"businessObject": "/TRFIN/COUNTRY", "entity": "/TRFIN/COUNTRIES"`, `"businessObject": "/TRFIN/COMPANY", "entity": "/TRFIN/COUNTRIES"`)
		}, "/TRFIN/COUNTRIES is not an entity of /TRFIN/COMPANY"},
		{"entity offered twice", func(f map[string]string) {
			replaceIn(f, serviceFile, `"Setup": {`, `"People": { "businessObject": "/TRFIN/COMPANY", "entity": "/TRFIN/CONTACTS" }, "Setup": {`)
		}, "/TRFIN/CONTACTS is offered by the entity sets Contacts and People; offer an entity once"},
	} {
		t.Run(c.name, func(t *testing.T) {
			expect(t, withService(t, c.change), rterr.CodeService, c.contains)
		})
	}
}

// TestServiceBrokenByChange checks that a change of a business object
// that breaks an installed service fails with the name of the service.
func TestServiceBrokenByChange(t *testing.T) {
	s := withService(t, func(map[string]string) {})
	s.BusinessObjects["/TRSV/SETUP"].Entities[0].Operations = nil
	s.Services["/TRSV/SHOP"].EntitySets[4].Operations = model.Some([]model.EntityOperation{model.OperationUpdate})
	expect(t, s, rterr.CodeService, "service /TRSV/SHOP")
}

// TestServiceOfPackageNotRequired checks that a service cannot offer a
// business object of a package its package does not require.
func TestServiceOfPackageNotRequired(t *testing.T) {
	files := serviceFiles()
	files["manifest.json"] = `{ "id": "TRSV", "version": "1.0.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }] }`
	fsys := fstest.MapFS{}
	for name, text := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(text)}
	}
	_, err := pkgload.Load(fsys, "TRSV")
	if err == nil || !strings.Contains(err.Error(), "/TRFIN/COMPANY") {
		t.Fatalf("expected a namespace error naming /TRFIN/COMPANY, got %v", err)
	}
}
