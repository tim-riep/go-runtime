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
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// trbase returns the example TRBASE with changed address components.
func trbase(t *testing.T, version string, change func(*model.Structure)) *model.Package {
	p := testutil.Example(t, "TRBASE")
	p.Manifest.Version = version
	for i := range p.Structures {
		if p.Structures[i].Name == "/TRBASE/ADDRESS" {
			change(&p.Structures[i])
		}
	}
	return p
}

// TestStructureExamples runs the example packages through installation,
// upgrades of the address in TRBASE and the removal of TROTTO, which
// extends the address.
func TestStructureExamples(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		consistent(t, k)

		// Every column records the include it comes from and the structure
		// that declares it, also through two levels of includes.
		w := where(k, "TABLE_NAME", "FIELD_NAME")
		for _, c := range []struct{ table, field, include, source string }{
			{"/TRFIN/COMPANIES", "COUNTRY", "/TRBASE/ADDRESS", "/TRBASE/ADDRESS"},
			{"/TRFIN/COMPANIES", "/TROTTO/PICKUP_POINT", "/TRBASE/ADDRESS", "/TRBASE/ADDRESS"},
			{"/TRFIN/COMPANIES_ARCHIVE", "CITY", "/TRFIN/COMPANIES", "/TRBASE/ADDRESS"},
			{"/TRFIN/COMPANIES_ARCHIVE", "/TROTTO/PICKUP_POINT", "/TRFIN/COMPANIES", "/TRBASE/ADDRESS"},
			{"/TRFIN/COMPANIES_ARCHIVE", "NAME", "/TRFIN/COMPANIES", "/TRFIN/COMPANIES"},
			{"/TRFIN/COMPANIES_ARCHIVE", "/TROTTO/OTTO_ID", "/TRFIN/COMPANIES", "/TRFIN/COMPANIES"},
		} {
			got := []any{scalar(t, k, "/RT/FIELDS", "ORIGIN", w, c.table, c.field), scalar(t, k, "/RT/FIELDS", "INCLUDE_NAME", w, c.table, c.field),
				scalar(t, k, "/RT/FIELDS", "SOURCE_NAME", w, c.table, c.field)}
			if got[0] != "INCLUDE" || got[1] != c.include || got[2] != c.source {
				t.Errorf("%s.%s: %v", c.table, c.field, got)
			}
		}
		s, _ := k.Schema(ctx)
		archive := s.Physical().Tables["/TRFIN/COMPANIES_ARCHIVE"]
		if archive.Field(model.TenantField) == nil || len(archive.ForeignKeys) != 1 || archive.PrimaryKey[1] != "ARCHIVE_NO" {
			t.Fatalf("archive takes over keys of the companies: %v %v", archive.PrimaryKey, archive.ForeignKeys)
		}

		// null and "" stay apart in included columns.
		mustInsert(t, k, "/TRFIN/COMPANIES_ARCHIVE", values{model.TenantField: tenantA, "ARCHIVE_NO": 1, "ARCHIVED_AT": testutil.Epoch,
			"COMPANY_ID": "C1", "NAME": "Acme GmbH", "STREET": "", "COUNTRY": "DE"})
		mustInsert(t, k, "/TRFIN/COMPANIES_ARCHIVE", values{model.TenantField: tenantA, "ARCHIVE_NO": 2, "ARCHIVED_AT": testutil.Epoch, "NAME": "Two"})
		aw := where(k, model.TenantField, "ARCHIVE_NO")
		if v := scalar(t, k, "/TRFIN/COMPANIES_ARCHIVE", "STREET", aw, tenantA, 1); v != "" {
			t.Errorf("empty street: %#v", v)
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES_ARCHIVE", "STREET", aw, tenantA, 2); v != nil {
			t.Errorf("null street: %#v", v)
		}

		// The resolved definitions are the same in every dialect.
		for _, name := range []string{"/TRBASE/ADDRESS", "/TRFIN/COMPANY_GROUP", "/TRFIN/COMPANY_ARRAY", "/TRFIN/COMPANIES"} {
			d, err := k.Structure(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.MarshalIndent(d, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			testutil.Golden(t, "structure-"+strings.ReplaceAll(strings.Trim(name, "/"), "/", "-")+".json", string(b)+"\n")
		}

		// A new component becomes a column of every table that includes the
		// address, directly or through the companies.
		region := trbase(t, "50.0.0", func(st *model.Structure) {
			st.Elements = append(st.Elements, model.Element{Component: &model.Component{Name: "REGION",
				Type: model.TypeRef{Kind: model.TypeDomain, Name: "/TRBASE/NAME"}}})
		})
		p, err := upgrade(k, ctx, region, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"/TRFIN/COMPANIES", "/TRFIN/COMPANIES_ARCHIVE"} {
			if c := change(p, plan.AddColumn, table+".REGION"); c == nil || c.Via != "/TRBASE/ADDRESS" || c.Destructive {
				t.Fatalf("add-column %s.REGION: %+v", table, c)
			}
		}
		testutil.Golden(t, "upgrade-trbase-new-component."+p.Dialect+".sql", p.SQL())
		if v := scalar(t, k, "/TRFIN/COMPANIES_ARCHIVE", "STREET", aw, tenantA, 1); v != "" {
			t.Errorf("street after upgrade: %#v", v)
		}
		consistent(t, k)

		// A removed component is destructive in every including table. The
		// entity statements of TRFIN name the fields of the address; a
		// version without POSTAL_CODE in them comes first.
		fin := testutil.Example(t, "TRFIN")
		for i := range fin.Sources {
			fin.Sources[i].Text = strings.ReplaceAll(fin.Sources[i].Text, "POSTAL_CODE, ", "")
		}
		fin.Manifest.Version = "9.0.0"
		if _, err := k.Upgrade(ctx, fin, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		noPostal := trbase(t, "50.1.0", func(st *model.Structure) {
			st.Elements = append(st.Elements, model.Element{Component: &model.Component{Name: "REGION",
				Type: model.TypeRef{Kind: model.TypeDomain, Name: "/TRBASE/NAME"}}})
			st.Elements = append(st.Elements[:1], st.Elements[2:]...) // without POSTAL_CODE
		})
		_, err = upgrade(k, ctx, noPostal, kernel.Options{})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) || len(dc.Changes) != 2 {
			t.Fatalf("expected two destructive changes, got %v", err)
		}
		p, err = upgrade(k, ctx, noPostal, kernel.Options{AllowDestructive: true})
		if err != nil {
			t.Fatal(err)
		}
		if c := change(p, plan.DropColumn, "/TRFIN/COMPANIES_ARCHIVE.POSTAL_CODE"); c == nil || c.Via != "/TRBASE/ADDRESS" {
			t.Fatalf("drop-column: %+v", c)
		}
		testutil.Golden(t, "upgrade-trbase-removed-component."+p.Dialect+".sql", p.SQL())
		if v := scalar(t, k, "/TRFIN/COMPANIES", "COUNTRY", where(k, model.TenantField, "COMPANY_ID"), tenantA, "C1"); v != "DE" {
			t.Errorf("country after rebuild: %v", v)
		}
		consistent(t, k)

		// Removing TROTTO takes its address component out of all tables.
		if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		s, _ = k.Schema(ctx)
		for _, table := range []string{"/TRFIN/COMPANIES", "/TRFIN/COMPANIES_ARCHIVE"} {
			if s.Physical().Tables[table].Field("/TROTTO/PICKUP_POINT") != nil {
				t.Errorf("%s keeps the pickup point", table)
			}
		}
		d, err := k.Structure(ctx, "/TRBASE/ADDRESS")
		if err != nil || len(d.Definition.Extensions) != 0 || d.Definition.Component("/TROTTO/PICKUP_POINT") != nil {
			t.Fatalf("address after uninstall: %v %+v", err, d)
		}
		consistent(t, k)
	})
}
