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

package structure_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/structure"
)

var en = map[string]model.Label{"en": {Label: "Address"}, "de": {Label: "Adresse"}}

func elem(name string, kind model.TypeKind, typ string) model.Element {
	return model.Element{Component: &model.Component{Name: name, Type: model.TypeRef{Kind: kind, Name: typ}}}
}

func schema(t *testing.T) *model.Schema {
	country := elem("COUNTRY", model.TypeDataElement, "/TRBASE/COUNTRY")
	country.Component.DefaultJSON = []byte(`""`)
	pkg := &model.Package{
		Manifest: model.Manifest{ID: "TRS", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRBASE"}, {ID: "TRFIN"}}},
		Structures: []model.Structure{
			{Name: "/TRS/ADDRESS", Texts: en, Elements: []model.Element{elem("STREET", model.TypeDomain, "/TRBASE/TEXT"), country}},
			{Name: "/TRS/GROUP", Texts: en, Elements: []model.Element{elem("SEAT", model.TypeStructure, "/TRS/ADDRESS"),
				elem("MEMBERS", model.TypeArray, "/TRS/SITE_ARRAY"), {Included: model.Some("/TRS/ADDRESS")}}},
		},
		ArrayTypes: []model.ArrayType{{Name: "/TRS/SITE_ARRAY", Texts: en, Row: model.TypeRef{Kind: model.TypeStructure, Name: "/TRS/SITES"},
			Key: &model.ArrayKey{Fields: []string{"SITE_ID"}}}},
		Tables: []model.Table{{Name: "/TRS/SITES", TenantSpecific: true, DataClass: model.Application,
			Fields:   []model.Field{{Name: "SITE_ID", LocalName: "SITE_ID", Origin: model.OriginTable, DataElement: model.Some("/TRFIN/COMPANY")}},
			Includes: []model.TableInclude{{Name: "/TRS/ADDRESS", After: 1}}, PrimaryKey: []string{"SITE_ID"}}},
	}
	ext := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRS"}, {ID: "TRBASE"}}},
		StructureExtensions: []model.StructureExtension{{Structure: "/TRS/ADDRESS",
			Components: []model.Component{{Name: "/TRX/PICKUP", Type: model.TypeRef{Kind: model.TypeDomain, Name: "/TRBASE/TEXT"}}}}}}
	s := testutil.AddPackages(t, testutil.KernelSchema(t), testutil.Example(t, "TRBASE"), testutil.Example(t, "TRFIN"), pkg, ext)
	if err := validate.Schema(s, validate.Options{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResolve(t *testing.T) {
	s := schema(t)
	addr, err := structure.Resolve(s, []string{"en"}, "/TRS/ADDRESS")
	if err != nil {
		t.Fatal(err)
	}
	if addr.Kind != structure.KindStructure || !addr.Flat || len(addr.Components) != 3 || len(addr.Texts) != 1 || addr.Extensions[0] != "TRX" {
		t.Fatalf("address: %+v", addr)
	}
	c := addr.Component("COUNTRY")
	if c.DataElement == nil || c.DataElement.Name != "/TRBASE/COUNTRY" || c.Default != model.ValueDefault("") || c.Origin.Declared != "/TRS/ADDRESS" {
		t.Fatalf("country: %+v", c)
	}
	if p := addr.Component("/TRX/PICKUP"); p.Origin.Extension != model.Some[model.PackageID]("TRX") || p.Origin.Package != "TRX" {
		t.Fatalf("pickup: %+v", p.Origin)
	}

	group, err := structure.Resolve(s, nil, "/TRS/GROUP")
	if err != nil {
		t.Fatal(err)
	}
	if group.Flat || group.Component("SEAT").Structure.Name != "/TRS/ADDRESS" || group.Component("STREET").Origin.Includes[0] != "/TRS/ADDRESS" {
		t.Fatalf("group: %+v", group)
	}
	rows := group.Component("MEMBERS").ArrayType
	if rows.Kind != structure.KindArrayType || rows.Array.Key.Unique || rows.Array.Row.Structure.Kind != structure.KindTable {
		t.Fatalf("array: %+v", rows)
	}
	// A table is a flat structure without tenant field and keys.
	sites := rows.Array.Row.Structure
	var names []string
	for _, c := range sites.Components {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "SITE_ID,STREET,COUNTRY,/TRX/PICKUP" || !sites.Flat {
		t.Fatalf("sites: %v", names)
	}
	b, err := group.JSON()
	if err != nil || !strings.Contains(string(b), `"formatVersion": 1`) || !strings.Contains(string(b), `"default": ""`) ||
		strings.Contains(string(b), model.TenantField) {
		t.Fatalf("json: %v\n%s", err, b)
	}

	var nf *rterr.NotFound
	if _, err := structure.Resolve(s, nil, "/TRS/NOPE"); !errors.As(err, &nf) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestUsesOf(t *testing.T) {
	uses := schema(t).UsesOf("/TRS/ADDRESS")
	var got []string
	for _, u := range uses {
		got = append(got, u.Kind+" "+u.Name+" "+u.Via)
	}
	want := "structure /TRS/GROUP component SEAT|structure /TRS/GROUP include|structure-extension TRX extension|table /TRS/SITES include"
	if strings.Join(got, "|") != want {
		t.Fatalf("uses:\n%s", strings.Join(got, "\n"))
	}
}
