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

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

var en = map[string]model.Label{"en": {Label: "Label"}}

func comp(name string, kind model.TypeKind, typ string) model.Element {
	return model.Element{Component: &model.Component{Name: name, Type: model.TypeRef{Kind: kind, Name: typ}}}
}

func include(name string) model.Element { return model.Element{Included: model.Some(name)} }

func structure(name string, elems ...model.Element) model.Structure {
	return model.Structure{Name: name, Texts: en, Elements: elems}
}

func table(name string, tenant bool, fields []model.Field, includes []model.TableInclude, pk ...string) model.Table {
	return model.Table{Name: name, TenantSpecific: tenant, DataClass: model.Application, Fields: fields, Includes: includes, PrimaryKey: pk}
}

func field(name, dataElement string) model.Field {
	return model.Field{Name: name, LocalName: name, Origin: model.OriginTable, DataElement: model.Some(dataElement)}
}

// trs builds a package requiring TRBASE and TRFIN.
func trs(p model.Package) *model.Package {
	p.Manifest = model.Manifest{ID: "TRS", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRBASE"}, {ID: "TRFIN"}}}
	return &p
}

func withTRS(t *testing.T, p *model.Package) *model.Schema {
	return testutil.AddPackages(t, load(t).schema(t), p)
}

func expectStructure(t *testing.T, s *model.Schema, code rterr.Code, contains string) {
	t.Helper()
	err := Schema(s, Options{})
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Code == code && strings.Contains(v.Error(), contains) {
			return
		}
	}
	t.Fatalf("expected %s error containing %q, got:\n%v", code, contains, err)
}

var address = structure("/TRS/ADDRESS",
	comp("STREET", model.TypeDataElement, "/TRBASE/NAME"),
	comp("CITY", model.TypeDataElement, "/TRBASE/NAME"),
	comp("COUNTRY", model.TypeDataElement, "/TRBASE/COUNTRY"))

func TestStructuresAndIncludesResolve(t *testing.T) {
	s := withTRS(t, trs(model.Package{
		Structures: []model.Structure{address,
			structure("/TRS/GROUP", comp("NAME", model.TypeDataElement, "/TRBASE/NAME"),
				comp("SEAT", model.TypeStructure, "/TRS/ADDRESS"), comp("MEMBERS", model.TypeArray, "/TRS/COMPANY_ARRAY"))},
		ArrayTypes: []model.ArrayType{{Name: "/TRS/COMPANY_ARRAY", Texts: en, Row: model.TypeRef{Kind: model.TypeStructure, Name: "/TRFIN/COMPANIES"},
			Key: &model.ArrayKey{Fields: []string{"COMPANY_ID"}, Unique: true}}},
		Tables: []model.Table{
			table("/TRS/SITES", true, []model.Field{field("SITE_ID", "/TRFIN/COMPANY")}, []model.TableInclude{{Name: "/TRS/ADDRESS", After: 1}}, "SITE_ID", "CITY"),
			table("/TRS/ARCHIVE", true, []model.Field{field("ARCHIVE_NO", "/TRFIN/COMPANY")}, []model.TableInclude{{Name: "/TRFIN/COMPANIES", After: 1}}, "ARCHIVE_NO"),
		},
	}))
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	r := s.Resolver()
	if !r.Flat("/TRS/ADDRESS") || r.Flat("/TRS/GROUP") || !r.Flat("/TRFIN/COMPANIES") {
		t.Fatal("flat or deep detected wrongly")
	}
	sites := s.Physical().Tables["/TRS/SITES"]
	var names []string
	for _, f := range sites.Fields {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "/RT/TENANT,SITE_ID,STREET,CITY,COUNTRY" {
		t.Fatalf("columns: %v", names)
	}
	if c := sites.Field("CITY"); c.Origin != model.OriginInclude || c.Included != model.Some("/TRS/ADDRESS") || c.Source != model.Some("/TRS/ADDRESS") || !c.NotNull {
		t.Fatalf("included key column: %+v", c)
	}

	// The tenant field, keys and foreign keys of an included table are not
	// taken over; its extension fields are.
	archive := s.Physical().Tables["/TRS/ARCHIVE"]
	tenants := 0
	for _, f := range archive.Fields {
		if f.Name == model.TenantField {
			tenants++
		}
	}
	if tenants != 1 || archive.PrimaryKey[1] != "ARCHIVE_NO" || len(archive.PrimaryKey) != 2 {
		t.Fatalf("archive keys: %v", archive.PrimaryKey)
	}
	if archive.ForeignKey("COUNTRY") != nil || archive.Field("/TROTTO/OTTO_ID") == nil || archive.Field("COMPANY_ID").NotNull {
		t.Fatalf("archive columns: %+v", archive.Fields)
	}
}

func TestIncludeCollision(t *testing.T) {
	s := withTRS(t, trs(model.Package{
		Structures: []model.Structure{address},
		Tables: []model.Table{table("/TRS/SITES", false, []model.Field{field("SITE_ID", "/TRFIN/COMPANY"), field("CITY", "/TRBASE/NAME")},
			[]model.TableInclude{{Name: "/TRS/ADDRESS", After: 1}}, "SITE_ID")},
	}))
	expectStructure(t, s, rterr.CodeStructure, "component CITY of /TRS/SITES collides with component CITY from include /TRS/ADDRESS")
	s = withTRS(t, trs(model.Package{Structures: []model.Structure{address,
		structure("/TRS/TWICE", include("/TRS/ADDRESS"), comp("STREET", model.TypeDomain, "/TRBASE/TEXT"))}}))
	expectStructure(t, s, rterr.CodeStructure, "collides with component STREET from include /TRS/ADDRESS")
}

func TestDeepStructureInTable(t *testing.T) {
	s := withTRS(t, trs(model.Package{
		Structures: []model.Structure{address, structure("/TRS/DEEP", comp("SEAT", model.TypeStructure, "/TRS/ADDRESS"))},
		Tables:     []model.Table{table("/TRS/T", false, []model.Field{field("ID", "/TRFIN/COMPANY")}, []model.TableInclude{{Name: "/TRS/DEEP", After: 1}}, "ID")},
	}))
	expectStructure(t, s, rterr.CodeStructure, "includes the deep structure /TRS/DEEP")
}

func TestTablesIncludingEachOther(t *testing.T) {
	s := withTRS(t, trs(model.Package{Tables: []model.Table{
		table("/TRS/A", false, []model.Field{field("A_ID", "/TRFIN/COMPANY")}, []model.TableInclude{{Name: "/TRS/B", After: 1}}, "A_ID"),
		table("/TRS/B", false, []model.Field{field("B_ID", "/TRFIN/COMPANY")}, []model.TableInclude{{Name: "/TRS/A", After: 1}}, "B_ID"),
	}}))
	expectStructure(t, s, rterr.CodeStructure, "contains itself: /TRS/A -> include /TRS/B -> include /TRS/A")
}

func TestCycleThroughIncludeNestedAndArray(t *testing.T) {
	s := withTRS(t, trs(model.Package{
		Structures: []model.Structure{
			structure("/TRS/A", include("/TRS/B")),
			structure("/TRS/B", comp("C", model.TypeStructure, "/TRS/C")),
			structure("/TRS/C", comp("ITEMS", model.TypeArray, "/TRS/A_ARRAY")),
		},
		ArrayTypes: []model.ArrayType{{Name: "/TRS/A_ARRAY", Texts: en, Row: model.TypeRef{Kind: model.TypeStructure, Name: "/TRS/A"}}},
	}))
	expectStructure(t, s, rterr.CodeStructure, "/TRS/A -> include /TRS/B -> component C /TRS/C -> component ITEMS /TRS/A_ARRAY -> row type /TRS/A")
}

func TestStructureReferenceNotVisible(t *testing.T) {
	p := trs(model.Package{Structures: []model.Structure{structure("/TRS/S", include("/TROTTO/OTTO_CONNECTIONS"))}})
	expectStructure(t, withTRS(t, p), rterr.CodeNotVisible, "/TROTTO/OTTO_CONNECTIONS belongs to package TROTTO, which TRS does not require")
}

func TestTenantFieldNameReserved(t *testing.T) {
	p := trs(model.Package{Structures: []model.Structure{structure("/TRS/S", comp(model.TenantField, model.TypeDomain, model.TenantDomain))}})
	expectStructure(t, withTRS(t, p), rterr.CodeName, "reserved for the tenant field")
}

func TestExtensionMakesIncludedStructureDeep(t *testing.T) {
	base := trs(model.Package{
		Structures: []model.Structure{address, structure("/TRS/POINT", comp("LAT", model.TypeDomain, "/TRBASE/TEXT"))},
		Tables:     []model.Table{table("/TRS/SITES", false, []model.Field{field("SITE_ID", "/TRFIN/COMPANY")}, []model.TableInclude{{Name: "/TRS/ADDRESS", After: 1}}, "SITE_ID")},
	})
	ext := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRS"}}},
		StructureExtensions: []model.StructureExtension{{Structure: "/TRS/ADDRESS",
			Components: []model.Component{{Name: "/TRX/GEO", Type: model.TypeRef{Kind: model.TypeStructure, Name: "/TRS/POINT"}}}}}}
	s := testutil.AddPackages(t, withTRS(t, base), ext)
	expectStructure(t, s, rterr.CodeStructure, "the extension of /TRS/ADDRESS by TRX makes it deep")
}

func TestStructureExtensionRules(t *testing.T) {
	ext := func(id model.PackageID, requires []model.Dependency, c model.Component) *model.Package {
		return &model.Package{Manifest: model.Manifest{ID: id, Version: "1.0.0", Requires: requires},
			StructureExtensions: []model.StructureExtension{{Structure: "/TRS/ADDRESS", Components: []model.Component{c}}}}
	}
	pickup := model.Component{Name: "/TRX/PICKUP", Type: model.TypeRef{Kind: model.TypeDataElement, Name: "/TRBASE/NAME"}}
	base := func() *model.Schema { return withTRS(t, trs(model.Package{Structures: []model.Structure{address}})) }

	s := testutil.AddPackages(t, base(), ext("TRX", []model.Dependency{{ID: "TRS"}}, pickup))
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	if comps, _ := s.Resolver().Components("/TRS/ADDRESS"); comps[len(comps)-1].Name != "/TRX/PICKUP" || comps[len(comps)-1].Extension != model.Some[model.PackageID]("TRX") {
		t.Fatalf("extension component: %+v", comps)
	}
	s = testutil.AddPackages(t, base(), ext("TRX", []model.Dependency{{ID: "TRBASE"}}, pickup))
	expectStructure(t, s, rterr.CodeNotVisible, "which TRX does not require")
	notNull := pickup
	notNull.NotNull = true
	s = testutil.AddPackages(t, base(), ext("TRX", []model.Dependency{{ID: "TRS"}}, notNull))
	expectStructure(t, s, rterr.CodeExtension, "must be nullable or have a default")
	local := pickup
	local.Name = "PICKUP"
	s = testutil.AddPackages(t, base(), ext("TRX", []model.Dependency{{ID: "TRS"}}, local))
	expectStructure(t, s, rterr.CodeName, "must start with the namespace /TRX/")
}

func TestComponentRules(t *testing.T) {
	nested := comp("SEAT", model.TypeStructure, "/TRS/ADDRESS")
	nested.Component.NotNull = true
	s := withTRS(t, trs(model.Package{Structures: []model.Structure{address, structure("/TRS/S", nested)}}))
	// notNull on a structure or array component is allowed.
	if err := Schema(s, Options{}); err != nil {
		t.Fatalf("notNull on a structure component: %v", err)
	}

	nullDefault := comp("CITY", model.TypeDataElement, "/TRBASE/NAME")
	nullDefault.Component.NotNull, nullDefault.Component.DefaultJSON = true, []byte("null")
	s = withTRS(t, trs(model.Package{Structures: []model.Structure{structure("/TRS/S", nullDefault)}}))
	expectStructure(t, s, rterr.CodeDefault, "cannot have the default null")

	s = withTRS(t, trs(model.Package{Structures: []model.Structure{structure("/TRS/S", comp("X", model.TypeArray, "/TRS/NOPE"))}}))
	expectStructure(t, s, rterr.CodeStructure, "arrayType /TRS/NOPE does not exist")
}

func TestArrayTypeRules(t *testing.T) {
	arr := func(row model.TypeRef, key *model.ArrayKey) *model.Schema {
		return withTRS(t, trs(model.Package{Structures: []model.Structure{address},
			ArrayTypes: []model.ArrayType{{Name: "/TRS/ARR", Texts: en, Row: row, Key: key}}}))
	}
	structRow := model.TypeRef{Kind: model.TypeStructure, Name: "/TRS/ADDRESS"}
	expectStructure(t, arr(structRow, &model.ArrayKey{Fields: []string{"ZIP"}}), rterr.CodeStructure, "key field ZIP is not a component of /TRS/ADDRESS")
	expectStructure(t, arr(model.TypeRef{Kind: model.TypeDomain, Name: "/TRBASE/TEXT"}, &model.ArrayKey{Fields: []string{"X"}}), rterr.CodeStructure, "only arrays of structures or tables have a key")
	if err := Schema(arr(structRow, &model.ArrayKey{Fields: []string{"CITY", "STREET"}, Unique: true}), Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestNamesShareOneNamespace(t *testing.T) {
	s := testutil.AddPackages(t, load(t).schema(t))
	err := s.AddPackage(&model.Package{Manifest: model.Manifest{ID: "TRFIN2"}, Structures: []model.Structure{{Name: "/TRFIN/COMPANIES", Texts: en}}}, testutil.Epoch, testutil.Epoch)
	if err == nil || !strings.Contains(err.Error(), "already owned by TRFIN") {
		t.Fatalf("structure with the name of a table: %v", err)
	}
}
