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

package pkgload

import (
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func structureFS() map[string]string {
	return map[string]string{
		"schema/structures/ADDRESS.json": `{"name":"/TRX/ADDRESS","texts":{"en":{"label":"Address"},"de":{"label":"Adresse"}},
			"components":[{"name":"STREET","domain":"/TRX/QTY"},{"include":"/TRBASE/BASE"},
			{"name":"ITEMS","arrayType":"/TRX/ITEMS"},{"name":"ORDER","structure":"/TRX/ORDERS","description":""}]}`,
		"schema/array-types/ITEMS.json": `{"name":"/TRX/ITEMS","texts":{"en":{"label":"Items"}},
			"rowType":{"structure":"/TRX/ORDERS"},"key":{"fields":["ORDER_ID"],"unique":true}}`,
		"schema/structure-extensions/BASE.json": `{"structure":"/TRBASE/BASE","components":[{"name":"/TRX/EXTRA","domain":"/TRX/QTY","default":null}]}`,
		"schema/tables/ORDERS.json": `{"name":"/TRX/ORDERS","tenantSpecific":false,"dataClass":"application",
			"fields":[{"name":"ORDER_ID","domain":"/TRBASE/ID"},{"include":"/TRX/ADDRESS"},{"name":"NOTE","domain":"/TRBASE/ID"}],
			"primaryKey":["ORDER_ID"]}`,
	}
}

func TestLoadStructures(t *testing.T) {
	fsys := validFS()
	for p, d := range structureFS() {
		fsys[p] = file(d)
	}
	pkg, err := Load(fsys, "mem")
	if err != nil {
		t.Fatal(err)
	}
	st := pkg.Structures[0]
	if st.Name != "/TRX/ADDRESS" || len(st.Elements) != 4 || st.Texts["de"].Label != "Adresse" {
		t.Fatalf("structure: %+v", st)
	}
	if inc, ok := st.Elements[1].Included.Get(); !ok || inc != "/TRBASE/BASE" || st.Elements[1].Component != nil {
		t.Fatalf("include element: %+v", st.Elements[1])
	}
	if c := st.Elements[2].Component; c.Type != (model.TypeRef{Kind: model.TypeArray, Name: "/TRX/ITEMS"}) || c.Origin != model.ComponentStructure {
		t.Fatalf("array component: %+v", c)
	}
	if c := st.Elements[3].Component; c.Type.Kind != model.TypeStructure || c.Description != model.Some("") {
		t.Fatalf("nested component: %+v", c)
	}
	a := pkg.ArrayTypes[0]
	if a.Row.Name != "/TRX/ORDERS" || a.Key == nil || !a.Key.Unique || a.Key.Fields[0] != "ORDER_ID" {
		t.Fatalf("array type: %+v", a)
	}
	e := pkg.StructureExtensions[0]
	if e.Structure != "/TRBASE/BASE" || e.Components[0].Name != "/TRX/EXTRA" || string(e.Components[0].DefaultJSON) != "null" {
		t.Fatalf("extension: %+v", e)
	}
	var orders *model.Table
	for i := range pkg.Tables {
		if pkg.Tables[i].Name == "/TRX/ORDERS" {
			orders = &pkg.Tables[i]
		}
	}
	if len(orders.Fields) != 2 || len(orders.Includes) != 1 || orders.Includes[0].After != 1 || orders.Includes[0].Name != "/TRX/ADDRESS" {
		t.Fatalf("table includes: %+v %+v", orders.Fields, orders.Includes)
	}
}

func TestStructureFormatErrors(t *testing.T) {
	cases := map[string]struct{ path, doc, file string }{
		"two types":       {"schema/structures/S.json", `{"name":"/TRX/S","texts":{"en":{"label":"S"}},"components":[{"name":"A","domain":"/TRX/QTY","dataElement":"/TRX/QTY"}]}`, "schema/structures/S.json"},
		"no type":         {"schema/structures/S.json", `{"name":"/TRX/S","texts":{"en":{"label":"S"}},"components":[{"name":"A"}]}`, "schema/structures/S.json"},
		"no English":      {"schema/structures/S.json", `{"name":"/TRX/S","texts":{"de":{"label":"S"}},"components":[{"name":"A","domain":"/TRX/QTY"}]}`, "schema/structures/S.json"},
		"array row type":  {"schema/array-types/A.json", `{"name":"/TRX/A","texts":{"en":{"label":"A"}},"rowType":{"arrayType":"/TRX/A"}}`, "schema/array-types/A.json"},
		"extension local": {"schema/structure-extensions/E.json", `{"structure":"/TRBASE/BASE","components":[{"name":"x","domain":"/TRX/QTY"}]}`, "schema/structure-extensions/E.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := validFS()
			fsys[c.path] = file(c.doc)
			_, err := Load(fsys, "mem")
			for _, v := range rterr.All[*rterr.ValidationError](err) {
				if v.Code == rterr.CodeSchema && v.Location.File == c.file {
					return
				}
			}
			t.Fatalf("not rejected by the schema: %v", err)
		})
	}
}

func TestStructureReferenceToPackageNotRequired(t *testing.T) {
	fsys := validFS()
	fsys["schema/structures/S.json"] = file(`{"name":"/TRX/S","texts":{"en":{"label":"S"}},
		"components":[{"include":"/TRFIN/COMPANIES"},{"name":"A","arrayType":"/TRFIN/COMPANY_ARRAY"}]}`)
	expectViolation(t, fsys, "schema/structures/S.json", "/components/0/include", "/TRFIN/COMPANIES")
	expectViolation(t, fsys, "schema/structures/S.json", "/components/1/arrayType", "/TRFIN/COMPANY_ARRAY")
	fsys = validFS()
	fsys["schema/tables/ORDERS.json"] = file(`{"name":"/TRX/ORDERS","tenantSpecific":false,"dataClass":"application",
		"fields":[{"name":"ORDER_ID","domain":"/TRBASE/ID"},{"include":"/TRFIN/ADDRESS"}],"primaryKey":["ORDER_ID"]}`)
	expectViolation(t, fsys, "schema/tables/ORDERS.json", "/fields/1/include", "/TRFIN/ADDRESS")
}
