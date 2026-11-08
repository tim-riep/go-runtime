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
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"strings"
)

func domainField(name, domain string) model.Field {
	return model.Field{Name: name, LocalName: name, Origin: model.OriginTable, Domain: domain}
}

func fk(name, target string, pairs ...string) model.ForeignKey {
	k := model.ForeignKey{Name: name, Origin: model.OriginTable, Target: target}
	for i := 0; i < len(pairs); i += 2 {
		k.Columns = append(k.Columns, model.ColumnPair{Field: pairs[i], References: pairs[i+1]})
	}
	return k
}

// boPackage returns TRS with the tables ORDERS (root), ITEMS (key ORDER_ID
// and ITEM_NO) and NOTES (key NOTE_ID of type UUID) and the business
// object /TRS/ORDER over them.
func boPackage() model.Package {
	orders := table("/TRS/ORDERS", true, []model.Field{domainField("ORDER_ID", "/TRBASE/NAME"), domainField("NAME", "/TRBASE/NAME"),
		domainField("CHANGED_BY", "/TRBASE/NAME"), domainField("CHANGED_AT", "/TRBASE/TIMESTAMP"), domainField("COUNT", "/TRBASE/COUNTER")}, nil, "ORDER_ID")
	items := table("/TRS/ITEMS", true, []model.Field{domainField("ORDER_ID", "/TRBASE/NAME"), domainField("ITEM_NO", "/TRBASE/COUNTER"),
		domainField("TEXT", "/TRBASE/TEXT")}, nil, "ORDER_ID", "ITEM_NO")
	items.ForeignKeys = []model.ForeignKey{fk("ORDER", "/TRS/ORDERS", "ORDER_ID", "ORDER_ID")}
	notes := table("/TRS/NOTES", true, []model.Field{domainField("NOTE_ID", "/TRBASE/GUID"), domainField("ORDER_ID", "/TRBASE/NAME")}, nil, "NOTE_ID")
	notes.ForeignKeys = []model.ForeignKey{fk("ORDER", "/TRS/ORDERS", "ORDER_ID", "ORDER_ID")}
	all := []model.EntityOperation{model.OperationCreate, model.OperationUpdate, model.OperationDelete}
	b := model.BusinessObject{Name: "/TRS/ORDER", Description: enDescription, Root: "/TRS/ORDERS", BehaviorClass: model.Some("/TRS/ORDER_BEHAVIOR"), DataClass: model.Application,
		Entities: []model.Entity{
			{Table: "/TRS/ORDERS", Operations: all, Numbering: model.NumberingExternal,
				Fields:         map[string]model.FieldProperties{"NAME": {Mandatory: true}, "ORDER_ID": {ReadOnlyOnUpdate: true}},
				Administrative: model.AdministrativeFields{ChangedBy: model.Some("CHANGED_BY"), ChangedAt: model.Some("CHANGED_AT")},
				ETag:           model.Some("CHANGED_AT"),
				Determinations: []model.Determination{{Name: "SET_NAME", When: model.WhenModify, Trigger: model.Trigger{On: []model.EntityOperation{model.OperationCreate}}}},
				Validations: []model.Validation{{Name: "CHECK_NAME", Trigger: model.Trigger{On: []model.EntityOperation{model.OperationUpdate},
					Fields: []string{"NAME"}}}},
				Actions: []model.Action{{Name: "CLOSE", Instance: true, Parameter: model.Some("/TRBASE/ADDRESS"), Result: model.Some(model.ResultSelf), Modifies: true}},
				Authorization: &model.EntityAuthorization{Object: "/TRFIN/COMPANY", Fields: map[string]string{"COMPANY_ID": "ORDER_ID", "COUNTRY": "NAME"},
					Activities: map[string]string{"read": "READ", "create": "CREATE", "update": "UPDATE", "delete": "DELETE", "CLOSE": "UPDATE"}}},
			{Table: "/TRS/ITEMS", Parent: model.Some("/TRS/ORDERS"), ForeignKey: model.Some("ORDER"), Operations: all, Numbering: model.NumberingExternal,
				Fields: map[string]model.FieldProperties{}},
			{Table: "/TRS/NOTES", Parent: model.Some("/TRS/ORDERS"), ForeignKey: model.Some("ORDER"), Operations: all, Numbering: model.NumberingUUID,
				Fields: map[string]model.FieldProperties{}},
		}}
	return model.Package{
		Tables:          []model.Table{orders, items, notes},
		Sources:         []model.Source{{Name: "/TRS/ORDER_BEHAVIOR", Kind: model.SourceClass, File: "src/ORDER_BEHAVIOR.class"}},
		BusinessObjects: []model.BusinessObject{b},
	}
}

func withBO(t *testing.T, change func(p *model.Package, b *model.BusinessObject)) *model.Schema {
	p := boPackage()
	change(&p, &p.BusinessObjects[0])
	return withTRS(t, trs(p))
}

func TestBusinessObjectValid(t *testing.T) {
	if err := Schema(withBO(t, func(*model.Package, *model.BusinessObject) {}), Options{}); err != nil {
		t.Fatal(err)
	}
}

// TestBusinessObjectOfConfiguration accepts a business object of
// configuration without behavior class and authorization.
func TestBusinessObjectOfConfiguration(t *testing.T) {
	s := withBO(t, func(p *model.Package, b *model.BusinessObject) {
		b.DataClass, b.BehaviorClass = model.RuntimeConfiguration, model.None[string]()
		for i := range p.Tables {
			p.Tables[i].DataClass = model.RuntimeConfiguration
		}
		for i := range b.Entities {
			e := &b.Entities[i]
			e.Determinations, e.Validations, e.Actions, e.Authorization = nil, nil, nil, nil
		}
	})
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestBusinessObjectRules(t *testing.T) {
	entity := func(b *model.BusinessObject, table string) *model.Entity { return b.Entity(table) }
	for _, c := range []struct {
		name     string
		change   func(p *model.Package, b *model.BusinessObject)
		contains string
	}{
		{"table of another package", func(_ *model.Package, b *model.BusinessObject) {
			b.Entities = append(b.Entities, model.Entity{Table: "/TRFIN/BANK_ACCOUNTS", Parent: model.Some("/TRS/ORDERS"), ForeignKey: model.Some("COMPANY"),
				Numbering: model.NumberingExternal})
		}, "table /TRFIN/BANK_ACCOUNTS belongs to TRFIN; the entities of a business object are tables of its own package"},
		{"foreign key to another table", func(p *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ITEMS").Parent = model.Some("/TRS/NOTES")
		}, "the foreign key ORDER references /TRS/ORDERS, not the parent /TRS/NOTES"},
		{"missing foreign key", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ITEMS").ForeignKey = model.Some("NOPE")
		},
			"table /TRS/ITEMS has no foreign key NOPE"},
		{"no parent", func(_ *model.Package, b *model.BusinessObject) { entity(b, "/TRS/ITEMS").Parent = model.None[string]() },
			"/TRS/ITEMS is no root and needs a parent and a foreign key"},
		{"table in two business objects", func(p *model.Package, b *model.BusinessObject) {
			other := *b
			other.Name, other.Entities = "/TRS/OTHER", b.Entities[:1]
			p.BusinessObjects = append(p.BusinessObjects, other)
		}, "table /TRS/ORDERS already belongs to business object"},
		{"table in a setting", func(p *model.Package, _ *model.BusinessObject) {
			p.Tables[2].DataClass = model.Configuration
			p.Settings = []model.Setting{{ID: "/TRS/NOTES", Texts: map[string]model.SettingText{"en": {Label: "Notes"}},
				Root: model.SettingTable{Table: "/TRS/NOTES", Operations: model.Operations{Create: true, Update: true, Delete: true}}}}
		}, "table /TRS/NOTES belongs to setting /TRS/NOTES; a table belongs to a setting or a business object, not both"},
		{"different tenant flags", func(p *model.Package, _ *model.BusinessObject) { p.Tables[1].TenantSpecific = false },
			"table /TRS/ITEMS is tenant-specific: false, but the root /TRS/ORDERS: true"},
		{"uuid with a key of two fields", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ITEMS").Numbering = model.NumberingUUID
		},
			"numbering uuid needs a key of one field of type UUID; /TRS/ITEMS has the key ORDER_ID, ITEM_NO"},
		{"uuid with a key of another type", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Numbering = model.NumberingUUID
		},
			"numbering uuid needs a key of one field of type UUID"},
		{"unknown field", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Fields["NOPE"] = model.FieldProperties{ReadOnly: true}
		},
			"field NOPE is no field of /TRS/ORDERS"},
		{"administrative field of the wrong type", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Administrative.CreatedAt = model.Some("NAME")
		}, "the administrative field NAME is CHAR; it needs TIMESTAMP"},
		{"ETag of the wrong type", func(_ *model.Package, b *model.BusinessObject) { entity(b, "/TRS/ORDERS").ETag = model.Some("NAME") },
			"the ETag NAME is CHAR; it needs TIMESTAMP or UUID or INT8"},
		{"method twice", func(_ *model.Package, b *model.BusinessObject) { entity(b, "/TRS/ORDERS").Actions[0].Name = "SET_NAME" },
			"the method SET_NAME is the determination of /TRS/ORDERS and the action of /TRS/ORDERS"},
		{"static action with self", func(_ *model.Package, b *model.BusinessObject) { entity(b, "/TRS/ORDERS").Actions[0].Instance = false },
			"a static action has no instances to return as self"},
		{"unknown parameter", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Actions[0].Parameter = model.Some("/TRS/NOPE")
		}, "structure /TRS/NOPE does not exist"},
		{"trigger field without update", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Validations[0].On = []model.EntityOperation{model.OperationCreate}
		}, "fields trigger on update, which is not among the operations"},
		{"authorization with an unknown field", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Authorization.Fields["REGION"] = "NAME"
		}, "REGION is no field of /TRFIN/COMPANY that an entity field gives"},
		{"authorization field missing", func(_ *model.Package, b *model.BusinessObject) {
			delete(entity(b, "/TRS/ORDERS").Authorization.Fields, "COUNTRY")
		}, "the field COUNTRY of /TRFIN/COMPANY needs a field of /TRS/ORDERS"},
		{"authorization with an unknown entity field", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Authorization.Fields["COUNTRY"] = "NOPE"
		}, "NOPE is no field of /TRS/ORDERS"},
		{"activity not allowed", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Authorization.Activities["CLOSE"] = "APPROVE"
		}, "/TRFIN/COMPANY does not allow the activity APPROVE; it allows READ, CREATE, UPDATE, DELETE, EXPORT"},
		{"activity missing", func(_ *model.Package, b *model.BusinessObject) {
			delete(entity(b, "/TRS/ORDERS").Authorization.Activities, "read")
		}, "the activity for read is missing"},
		{"activity of something else", func(_ *model.Package, b *model.BusinessObject) {
			entity(b, "/TRS/ORDERS").Authorization.Activities["OPEN"] = "UPDATE"
		}, "OPEN is neither read, an operation nor an action of /TRS/ORDERS"},
		{"system class", func(p *model.Package, _ *model.BusinessObject) { p.Tables[2].DataClass = model.System },
			"table /TRS/NOTES has the data class system"},
		{"reserved field name", func(p *model.Package, _ *model.BusinessObject) {
			p.Tables[2].Fields = append(p.Tables[2].Fields, domainField("LINK", "/TRBASE/NAME"))
		}, "table /TRS/NOTES has a field LINK"},
		{"root not listed", func(_ *model.Package, b *model.BusinessObject) { b.Root = "/TRS/ITEMS" },
			"the root has no parent"},
		{"behavior class missing", func(_ *model.Package, b *model.BusinessObject) { b.BehaviorClass = model.Some("/TRS/NOPE") },
			"the class /TRS/NOPE does not exist in TRS"},
		{"behavior without behavior class", func(_ *model.Package, b *model.BusinessObject) { b.BehaviorClass = model.None[string]() },
			"it needs a behavior class for its determinations, validations and actions"},
		{"entity of another data class", func(p *model.Package, _ *model.BusinessObject) { p.Tables[1].DataClass = model.Configuration },
			"table /TRS/ITEMS has the data class configuration; business object /TRS/ORDER has application, which all its entities have"},
		{"configuration with a table of application data", func(p *model.Package, b *model.BusinessObject) {
			b.DataClass = model.Configuration
			p.Tables[0].DataClass, p.Tables[1].DataClass = model.Configuration, model.Configuration
		}, "table /TRS/NOTES has the data class application; business object /TRS/ORDER has configuration"},
		{"unknown data class", func(_ *model.Package, b *model.BusinessObject) { b.DataClass = model.System },
			"the data class is configuration or runtimeConfiguration"},
		{"application data without authorization", func(_ *model.Package, b *model.BusinessObject) { entity(b, "/TRS/ORDERS").Authorization = nil },
			"the root /TRS/ORDERS needs an authorization"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := withBO(t, c.change)
			err := Schema(s, Options{})
			for _, v := range rterr.All[*rterr.ValidationError](err) {
				if strings.Contains(v.Error(), c.contains) {
					return
				}
			}
			t.Fatalf("expected an error containing %q, got:\n%v", c.contains, err)
		})
	}
}
