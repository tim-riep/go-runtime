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
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

var labelEn = map[string]model.Label{"en": {Label: "Label"}}

func component(name, dataElement string) model.Element {
	return model.Element{Component: &model.Component{Name: name, Type: model.TypeRef{Kind: model.TypeDataElement, Name: dataElement}}}
}

func dataField(name, dataElement string) model.Field {
	return model.Field{Name: name, LocalName: name, Origin: model.OriginTable, DataElement: model.Some(dataElement)}
}

var countryFK = model.ForeignKey{Name: "COUNTRY", Origin: model.OriginTable, Target: "/TRFIN/COUNTRIES",
	Columns: []model.ColumnPair{{Field: "COUNTRY", References: "COUNTRY_CODE"}}}

// trs is a package with an address structure included into a table.
// Version 1.1.0 adds POSTAL, 1.2.0 also drops CITY, 1.3.0 drops COUNTRY.
func trs(version string) *model.Package {
	elems := []model.Element{component("STREET", "/TRBASE/NAME"), component("CITY", "/TRBASE/NAME"), component("COUNTRY", "/TRBASE/COUNTRY")}
	switch version {
	case "1.1.0":
		elems = append(elems, component("POSTAL", "/TRBASE/NAME"))
	case "1.2.0":
		elems = []model.Element{elems[0], elems[2], component("POSTAL", "/TRBASE/NAME")}
	case "1.3.0":
		elems = elems[:2]
	}
	return &model.Package{
		Manifest: model.Manifest{ID: "TRS", Version: version, Requires: []model.Dependency{{ID: "TRBASE"}, {ID: "TRFIN"}}},
		Structures: []model.Structure{
			{Name: "/TRS/ADDRESS", Texts: labelEn, Elements: elems},
			{Name: "/TRS/GROUP", Texts: labelEn, Elements: []model.Element{component("NAME", "/TRBASE/NAME"),
				{Component: &model.Component{Name: "SEAT", Type: model.TypeRef{Kind: model.TypeStructure, Name: "/TRS/ADDRESS"}}},
				{Component: &model.Component{Name: "SITES", Type: model.TypeRef{Kind: model.TypeArray, Name: "/TRS/SITE_ARRAY"}}}}},
		},
		ArrayTypes: []model.ArrayType{{Name: "/TRS/SITE_ARRAY", Texts: labelEn, Row: model.TypeRef{Kind: model.TypeStructure, Name: "/TRS/SITES"},
			Key: &model.ArrayKey{Fields: []string{"SITE_ID"}, Unique: true}}},
		Tables: []model.Table{{Name: "/TRS/SITES", TenantSpecific: true, DataClass: model.Application,
			Fields: []model.Field{dataField("SITE_ID", "/TRFIN/COMPANY")}, Includes: []model.TableInclude{{Name: "/TRS/ADDRESS", After: 1}},
			PrimaryKey: []string{"SITE_ID"}}},
	}
}

// trx extends the address of TRS and includes it into a table of its own
// with a foreign key over the included COUNTRY.
func trx() *model.Package {
	return &model.Package{
		Manifest: model.Manifest{ID: "TRX", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRS"}, {ID: "TRFIN"}, {ID: "TRBASE"}}},
		StructureExtensions: []model.StructureExtension{{Structure: "/TRS/ADDRESS",
			Components: []model.Component{{Name: "/TRX/PICKUP", Type: model.TypeRef{Kind: model.TypeDataElement, Name: "/TRBASE/NAME"}}}}},
		Tables: []model.Table{{Name: "/TRX/DEPOTS", DataClass: model.Application,
			Fields: []model.Field{dataField("DEPOT_ID", "/TRFIN/COMPANY")}, Includes: []model.TableInclude{{Name: "/TRS/ADDRESS", After: 1}},
			PrimaryKey: []string{"DEPOT_ID"}, ForeignKeys: []model.ForeignKey{countryFK}}},
	}
}

func change(p *plan.Plan, kind plan.ChangeKind, object string) *plan.Change {
	for i, c := range p.Changes {
		if c.Kind == kind && c.Object() == object {
			return &p.Changes[i]
		}
	}
	return nil
}

func TestStructureLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		if _, err := k.Install(ctx, []*model.Package{trs("1.0.0"), trx()}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		examples := []*model.Package{testutil.Example(t, "TRFIN"), testutil.Example(t, "TROTTO")}
		sameSchema(t, k, testutil.AddPackages(t, baseSchema(t, k), append(examples, trs("1.0.0"), trx())...))
		consistent(t, k)

		w := where(k, "TABLE_NAME", "FIELD_NAME")
		for _, c := range []struct{ table, field, origin, include, source string }{
			{"/TRS/SITES", "CITY", "INCLUDE", "/TRS/ADDRESS", "/TRS/ADDRESS"},
			{"/TRS/SITES", "/TRX/PICKUP", "INCLUDE", "/TRS/ADDRESS", "/TRS/ADDRESS"},
			{"/TRX/DEPOTS", "COUNTRY", "INCLUDE", "/TRS/ADDRESS", "/TRS/ADDRESS"},
		} {
			if o := scalar(t, k, "/RT/FIELDS", "ORIGIN", w, c.table, c.field); o != c.origin {
				t.Errorf("%s.%s origin %v", c.table, c.field, o)
			}
			if i := scalar(t, k, "/RT/FIELDS", "INCLUDE_NAME", w, c.table, c.field); i != c.include {
				t.Errorf("%s.%s include %v", c.table, c.field, i)
			}
			if s := scalar(t, k, "/RT/FIELDS", "SOURCE_NAME", w, c.table, c.field); s != c.source {
				t.Errorf("%s.%s source %v", c.table, c.field, s)
			}
		}
		var n int
		if err := k.DB().QueryRowContext(ctx, `SELECT count(*) FROM "/RT/STRUCTURE_ELEMENTS" WHERE "PACKAGE_ID" IN ('TRS', 'TRX')`).Scan(&n); err != nil || n != 3+1+3 {
			t.Fatalf("structure elements: %d", n)
		}

		// null and "" stay apart in included columns; the foreign key over
		// the included COUNTRY is enforced.
		mustInsert(t, k, "/TRX/DEPOTS", values{"DEPOT_ID": "D1", "STREET": "", "COUNTRY": "DE"})
		mustInsert(t, k, "/TRX/DEPOTS", values{"DEPOT_ID": "D2", "COUNTRY": "DE"})
		dw := where(k, "DEPOT_ID")
		if v := scalar(t, k, "/TRX/DEPOTS", "STREET", dw, "D1"); v != "" {
			t.Errorf("empty street: %#v", v)
		}
		if v := scalar(t, k, "/TRX/DEPOTS", "STREET", dw, "D2"); v != nil {
			t.Errorf("null street: %#v", v)
		}
		violation(t, insert(t, k, "/TRX/DEPOTS", values{"DEPOT_ID": "D3", "COUNTRY": "XX"}), rterr.ViolationForeignKey, "included country")

		// A new component becomes a column of every including table, also
		// in other packages.
		p, err := k.Upgrade(ctx, trs("1.1.0"), kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"/TRS/SITES", "/TRX/DEPOTS"} {
			c := change(p, plan.AddColumn, table+".POSTAL")
			if c == nil || c.Via != "/TRS/ADDRESS" || c.Destructive {
				t.Fatalf("add-column %s.POSTAL: %+v\n%s", table, c, p.Text())
			}
		}
		if change(p, plan.AlterStructure, "/TRS/ADDRESS") == nil || !strings.Contains(p.Text(), "via /TRS/ADDRESS") {
			t.Fatalf("plan:\n%s", p.Text())
		}
		if v := scalar(t, k, "/TRX/DEPOTS", "STREET", dw, "D1"); v != "" {
			t.Errorf("street after upgrade: %#v", v)
		}
		consistent(t, k)

		// A removed component is destructive.
		_, err = k.Upgrade(ctx, trs("1.2.0"), kernel.Options{})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) {
			t.Fatalf("expected DestructiveChange, got %v", err)
		}
		p, err = k.Upgrade(ctx, trs("1.2.0"), kernel.Options{AllowDestructive: true})
		if err != nil {
			t.Fatal(err)
		}
		if c := change(p, plan.DropColumn, "/TRX/DEPOTS.CITY"); c == nil || c.Via != "/TRS/ADDRESS" || !c.Destructive {
			t.Fatalf("drop-column: %+v", c)
		}
		consistent(t, k)

		// Removing COUNTRY would break the foreign key of TRX.
		_, err = k.Upgrade(ctx, trs("1.3.0"), kernel.Options{AllowDestructive: true})
		if err == nil || !strings.Contains(err.Error(), "/TRX/DEPOTS") {
			t.Fatalf("expected rejection naming /TRX/DEPOTS, got %v", err)
		}

		// Uninstalling the extension removes its column everywhere.
		if _, err := k.Uninstall(ctx, "TRX", kernel.Options{}); err == nil {
			t.Fatal("uninstall without confirmation")
		}
		if _, err := k.Uninstall(ctx, "TRX", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		s, _ := k.Schema(ctx)
		if s.Physical().Tables["/TRS/SITES"].Field("/TRX/PICKUP") != nil || len(s.ExtensionsOfStructure("/TRS/ADDRESS")) != 0 {
			t.Fatal("extension column left after uninstall")
		}
		consistent(t, k)

		// The check compares the stored columns with the resolved includes.
		if _, err := k.DB().ExecContext(ctx, `UPDATE "/RT/FIELDS" SET "SOURCE_NAME" = `+k.Dialect().Placeholder(1)+` WHERE `+
			`"TABLE_NAME" = '/TRS/SITES' AND "FIELD_NAME" = 'STREET'`, "/TRS/OTHER"); err != nil {
			t.Fatal(err)
		}
		d, err := k.Check(ctx)
		if err != nil || len(d) != 1 || d[0].Object != "/TRS/SITES.STREET" || !strings.Contains(d[0].Problem, "declared by /TRS/OTHER") {
			t.Fatalf("check: %v %v", err, d)
		}
	})
}

// TestFixedValueNames stores the names of fixed values in code and treats
// a changed name as a change of the domain that is not destructive.
func TestFixedValueNames(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		pkg := func(version, name string) *model.Package {
			return &model.Package{Manifest: model.Manifest{ID: "TRN", Version: version},
				Domains: []model.Domain{{Name: "/TRN/LEVEL", Type: model.Char, Length: 1,
					Values: []model.FixedValue{{Value: "h", Name: model.Some(name)}, {Value: "N"}}}}}
		}
		if _, err := k.Install(ctx, []*model.Package{pkg("1.0.0", "HIGH")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		sameSchema(t, k, testutil.AddPackages(t, baseSchema(t, k), pkg("1.0.0", "HIGH")))
		p, err := k.Upgrade(ctx, pkg("1.1.0", "TOP"), kernel.Options{})
		if err != nil || len(p.Destructive()) != 0 || change(p, plan.AlterDomain, "/TRN/LEVEL") == nil {
			t.Fatalf("rename: %v %+v", err, p)
		}
		s, _ := k.Schema(ctx)
		if n, _ := s.Domains["/TRN/LEVEL"].Values[0].CodeName(); n != "TOP" {
			t.Fatalf("name after upgrade: %v", s.Domains["/TRN/LEVEL"].Values)
		}
		consistent(t, k)
	})
}
