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

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// extensionFiles are the files of TRS, which extends /TRFIN/COMPANY: a
// field of its own with properties, behavior on the companies, an action
// with an authorization object of its own and the channels of a company
// as dependent entity.
func extensionFiles() map[string]string {
	return map[string]string{
		"manifest.json":                    `{ "id": "TRS", "version": "1.0.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }, { "id": "TRFIN" }] }`,
		"schema/extensions/COMPANIES.json": `{ "table": "/TRFIN/COMPANIES", "fields": [{ "name": "/TRS/RATING", "domain": "/TRBASE/COUNTER" }] }`,
		"schema/tables/CHANNELS.json": `{ "name": "/TRS/CHANNELS", "tenantSpecific": true, "dataClass": "application", "fields": [
			{ "name": "CHANNEL_ID", "domain": "/TRBASE/GUID" }, { "name": "COMPANY_ID", "dataElement": "/TRFIN/COMPANY", "notNull": true },
			{ "name": "CHANNEL", "domain": "/TRBASE/TEXT" } ], "primaryKey": ["CHANNEL_ID"],
			"foreignKeys": [{ "name": "COMPANY", "table": "/TRFIN/COMPANIES", "fields": [{ "field": "COMPANY_ID", "references": "COMPANY_ID" }] }] }`,
		"schema/authorization-objects/MARKET.json": `{ "name": "/TRS/MARKET", "texts": { "en": { "label": "Market" } },
			"fields": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }, { "name": "ACTIVITY" }], "activities": ["EXECUTE"] }`,
		"src/COMPANY_BEHAVIOR.class": "public class /TRS/COMPANY_BEHAVIOR\nend\n",
		"schema/business-object-extensions/COMPANY.json": `{ "businessObject": "/TRFIN/COMPANY", "behaviorClass": "/TRS/COMPANY_BEHAVIOR",
			"entities": {
				"/TRFIN/COMPANIES": {
					"fields": { "/TRS/RATING": { "readOnly": true } },
					"determinations": [{ "name": "/TRS/RATE", "when": "save", "on": ["create"] }],
					"validations": [{ "name": "/TRS/CHECK_MARKET", "on": ["create", "update"], "fields": ["COUNTRY"] }],
					"actions": [
						{ "name": "/TRS/SYNC", "result": "self", "activity": "EXECUTE",
							"authorization": { "object": "/TRS/MARKET", "fields": { "COUNTRY": "COUNTRY" } } },
						{ "name": "/TRS/RECHECK", "activity": "UPDATE" } ]
				},
				"/TRS/CHANNELS": { "parent": "/TRFIN/COMPANIES", "foreignKey": "COMPANY", "operations": ["create", "update", "delete"], "numbering": "uuid" }
			} }`,
	}
}

// withExtension loads TRS with changed files into the schema of the
// examples.
func withExtension(t *testing.T, change func(files map[string]string)) *model.Schema {
	t.Helper()
	files := extensionFiles()
	change(files)
	fsys := fstest.MapFS{}
	for name, text := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(text)}
	}
	p, err := pkgload.Load(fsys, "TRS")
	if err != nil {
		t.Fatal(err)
	}
	return withTRS(t, p)
}

// replaceIn replaces text in a file.
func replaceIn(files map[string]string, file, old, new string) {
	if !strings.Contains(files[file], old) {
		panic("no " + old + " in " + file)
	}
	files[file] = strings.Replace(files[file], old, new, 1)
}

const extFile = "schema/business-object-extensions/COMPANY.json"

func TestBusinessObjectExtensionValid(t *testing.T) {
	s := withExtension(t, func(map[string]string) {})
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	b := s.MergedBusinessObject("/TRFIN/COMPANY", nil)
	root := b.Entity("/TRFIN/COMPANIES")
	if b.Entity("/TRS/CHANNELS") == nil || b.Entity("/TRS/CHANNELS").Origin != "TRS" || !root.Fields["/TRS/RATING"].ReadOnly ||
		root.Determinations[len(root.Determinations)-1].Origin != "TRS" || root.Actions[0].Origin != "TRFIN" || len(b.Extensions) != 1 {
		t.Errorf("merged: %+v", b)
	}
}

func TestBusinessObjectExtensionRules(t *testing.T) {
	for _, c := range []struct {
		name     string
		change   func(files map[string]string)
		contains string
	}{
		{"properties of a field of the base", func(f map[string]string) {
			replaceIn(f, extFile, `"/TRS/RATING": { "readOnly": true }`, `"NAME": { "readOnly": true }`)
		}, "the field NAME belongs to TRFIN; an extension sets the properties of fields of its own package only"},
		{"operations of the base", func(f map[string]string) {
			replaceIn(f, extFile, `"fields": { "/TRS/RATING"`, `"operations": ["create"], "fields": { "/TRS/RATING"`)
		}, "an extension cannot change the operations of /TRFIN/COMPANIES"},
		{"authorization of the base", func(f map[string]string) {
			replaceIn(f, extFile, `"fields": { "/TRS/RATING"`, `"authorization": { "object": "/TRS/MARKET", "activities": {} }, "fields": { "/TRS/RATING"`)
		}, "an extension cannot change the authorization of /TRFIN/COMPANIES"},
		{"ETag of the base", func(f map[string]string) {
			replaceIn(f, extFile, `"fields": { "/TRS/RATING"`, `"etag": "/TRS/RATING", "fields": { "/TRS/RATING"`)
		}, "an extension cannot change the ETag of /TRFIN/COMPANIES"},
		{"name of the base", func(f map[string]string) {
			replaceIn(f, extFile, `"/TRS/RECHECK"`, `"/TRS/SYNC"`)
		}, "/TRS/SYNC is the name of another determination, validation or action of /TRFIN/COMPANY"},
		{"activity the base does not allow", func(f map[string]string) {
			replaceIn(f, extFile, `"activity": "UPDATE"`, `"activity": "APPROVE"`)
		}, "/TRFIN/COMPANY does not allow the activity APPROVE"},
		{"action without activity", func(f map[string]string) {
			replaceIn(f, extFile, `, "activity": "UPDATE"`, ``)
		}, "the action needs an activity of /TRFIN/COMPANY"},
		{"authorization field missing", func(f map[string]string) {
			replaceIn(f, extFile, `"fields": { "COUNTRY": "COUNTRY" }`, `"fields": {}`)
		}, "the field COUNTRY of /TRS/MARKET needs a field of /TRFIN/COMPANIES"},
		{"unknown trigger field", func(f map[string]string) {
			replaceIn(f, extFile, `"fields": ["COUNTRY"]`, `"fields": ["COLOR"]`)
		}, "COLOR is no field of /TRFIN/COMPANIES"},
		{"behavior without behavior class", func(f map[string]string) {
			replaceIn(f, extFile, `"behaviorClass": "/TRS/COMPANY_BEHAVIOR",`, ``)
		}, "it needs a behavior class for its determinations, validations and actions"},
		{"entity of another package", func(f map[string]string) {
			replaceIn(f, extFile, `"/TRS/CHANNELS": { "parent"`, `"/TRFIN/PAYMENTS": { "parent"`)
		}, "table /TRFIN/PAYMENTS belongs to TRFIN; an extension adds dependent entities of tables of its own package"},
		{"foreign key that does not lead to the parent", func(f map[string]string) {
			replaceIn(f, extFile, `"parent": "/TRFIN/COMPANIES"`, `"parent": "/TRFIN/CONTACTS"`)
		}, "the foreign key COMPANY references /TRFIN/COMPANIES, not the parent /TRFIN/CONTACTS"},
		{"own entity of another data class", func(f map[string]string) {
			replaceIn(f, "schema/tables/CHANNELS.json", `"dataClass": "application"`, `"dataClass": "configuration"`)
		}, "table /TRS/CHANNELS has the data class configuration; business object /TRFIN/COMPANY has application"},
		{"own entity without parent", func(f map[string]string) {
			replaceIn(f, extFile, `"parent": "/TRFIN/COMPANIES", `, ``)
		}, "/TRS/CHANNELS is no entity of /TRFIN/COMPANY; an entity of the extending package needs a parent"},
		{"business object that does not exist", func(f map[string]string) {
			replaceIn(f, extFile, `"businessObject": "/TRFIN/COMPANY"`, `"businessObject": "/TRFIN/NOPE"`)
			f["schema/business-object-extensions/NOPE.json"] = f[extFile]
			delete(f, extFile)
		}, "the business object /TRFIN/NOPE does not exist"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Schema(withExtension(t, c.change), Options{})
			for _, v := range rterr.All[*rterr.ValidationError](err) {
				if strings.Contains(v.Error(), c.contains) {
					if !strings.Contains(v.Error(), "business object extension /TRFIN/") {
						t.Errorf("the error does not name the extension: %v", v)
					}
					return
				}
			}
			t.Fatalf("expected an error containing %q, got:\n%v", c.contains, err)
		})
	}
}

// TestBusinessObjectRestrictsExtensions applies the kinds of extensions a
// business object allows, also when it narrows them later.
func TestBusinessObjectRestrictsExtensions(t *testing.T) {
	for _, c := range []struct {
		name     string
		bo       func(b *model.BusinessObject)
		contains string
	}{
		{"no extensions", func(b *model.BusinessObject) { b.Extensible = model.Some([]model.ExtensionKind{}) },
			"/TRFIN/COMPANY does not allow extensions with fieldProperties on /TRFIN/COMPANIES"},
		{"no actions", func(b *model.BusinessObject) {
			b.Extensible = model.Some([]model.ExtensionKind{model.ExtendFieldProperties, model.ExtendDeterminations, model.ExtendValidations, model.ExtendEntities})
		}, "/TRFIN/COMPANY does not allow extensions with actions on /TRFIN/COMPANIES"},
		{"no entities below the root", func(b *model.BusinessObject) {
			b.Entity("/TRFIN/COMPANIES").Extensible = model.Some([]model.ExtensionKind{model.ExtendFieldProperties, model.ExtendDeterminations,
				model.ExtendValidations, model.ExtendActions})
		}, "/TRFIN/COMPANY does not allow extensions with entities on /TRFIN/COMPANIES"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := withExtension(t, func(map[string]string) {})
			c.bo(s.BusinessObjects["/TRFIN/COMPANY"])
			expect(t, s, rterr.CodeBusinessObject, c.contains)
		})
	}
	// An entity cannot allow more than its business object.
	s := withExtension(t, func(map[string]string) {})
	b := s.BusinessObjects["/TRFIN/COMPANY"]
	b.Extensible = model.Some([]model.ExtensionKind{model.ExtendFieldProperties})
	b.Entity("/TRFIN/CONTACTS").Extensible = model.Some([]model.ExtensionKind{model.ExtendActions})
	expect(t, s, rterr.CodeBusinessObject, "/TRFIN/COMPANY allows no extensions with actions; an entity narrows what the business object allows")
}

// TestBusinessObjectExtensionBrokenByBase rejects changes of the business
// object that an installed extension depends on, naming the extension.
func TestBusinessObjectExtensionBrokenByBase(t *testing.T) {
	s := withExtension(t, func(map[string]string) {})
	b := s.BusinessObjects["/TRFIN/COMPANY"]
	var kept []model.Entity
	for _, e := range b.Entities {
		if e.Table != "/TRFIN/BANK_ACCOUNTS" {
			kept = append(kept, e)
		}
	}
	b.Entities = kept
	b.DataClass = model.Configuration
	err := Schema(s, Options{})
	if err == nil || !strings.Contains(err.Error(), "business object extension /TRFIN/COMPANY by TRS") ||
		!strings.Contains(err.Error(), "table /TRS/CHANNELS has the data class application; business object /TRFIN/COMPANY has configuration") {
		t.Errorf("data class of the base changed: %v", err)
	}
}

// TestBusinessObjectExtensionStructureField sets properties of a field
// that a structure extension of the package adds through an include, and
// rejects those of a field another package adds that way.
func TestBusinessObjectExtensionStructureField(t *testing.T) {
	s := withExtension(t, func(f map[string]string) {
		f["schema/structure-extensions/ADDRESS.json"] = `{ "structure": "/TRBASE/ADDRESS", "components": [{ "name": "/TRS/FLOOR", "dataElement": "/TRBASE/NAME" }] }`
		replaceIn(f, extFile, `"/TRS/RATING": { "readOnly": true }`, `"/TRS/RATING": { "readOnly": true }, "/TRS/FLOOR": { "mandatory": true }`)
	})
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	if !s.MergedBusinessObject("/TRFIN/COMPANY", nil).Entity("/TRFIN/COMPANIES").Fields["/TRS/FLOOR"].Mandatory {
		t.Error("property of the field of the structure extension")
	}
	s = withExtension(t, func(f map[string]string) {
		replaceIn(f, extFile, `"/TRS/RATING": { "readOnly": true }`, `"/TROTTO/PICKUP_POINT": { "readOnly": true }`)
		replaceIn(f, "manifest.json", `{ "id": "TRFIN" }`, `{ "id": "TRFIN" }, { "id": "TROTTO" }`)
	})
	expect(t, s, rterr.CodeBusinessObject, "the field /TROTTO/PICKUP_POINT belongs to TROTTO; an extension sets the properties of fields of its own package only")
}
