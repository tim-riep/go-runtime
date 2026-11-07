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

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func TestTenants(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k) // creates ACME
		ten := k.Tenants()
		var ia *rterr.InvalidArgument
		if _, err := ten.Create(ctx, "acme", model.None[string]()); !errors.As(err, &ia) {
			t.Fatalf("lower-case tenant accepted: %v", err)
		}
		if _, err := ten.Create(ctx, "GLOBEX", model.Some("Globex Corp")); err != nil {
			t.Fatal(err)
		}
		violation(t, func() error { _, err := ten.Create(ctx, "GLOBEX", model.None[string]()); return err }(), rterr.ViolationUnique, "duplicate tenant")
		list, err := ten.List(ctx)
		if err != nil || len(list) != 2 || list[1].ID != "GLOBEX" || list[1].Description != model.Some("Globex Corp") || list[1].CreatedAt.IsZero() {
			t.Fatalf("list: %v %+v", err, list)
		}

		mustInsert(t, k, "/TRFIN/COMPANIES", values{model.TenantField: "GLOBEX", "COMPANY_ID": "C1", "NAME": "Globex", "COUNTRY": "DE"})

		var conf *rterr.ConfirmationRequired
		if _, err := ten.Delete(ctx, tenantA, kernel.Options{}); !errors.As(err, &conf) {
			t.Fatalf("expected ConfirmationRequired, got %v", err)
		}
		dry, err := ten.Delete(ctx, tenantA, kernel.Options{DryRun: true})
		if err != nil || dry.Executed {
			t.Fatalf("dry run: %v", err)
		}
		var order []string
		for _, c := range dry.Changes {
			order = append(order, c.Table+" "+c.Detail)
		}
		if got := strings.Join(order, ", "); got != "/TROTTO/OTTO_CONNECTIONS 0 rows, /TRFIN/PAYMENTS 0 rows, /TRFIN/COMPANIES_ARCHIVE 0 rows, /TRFIN/BANK_ACCOUNTS 1 rows, /TRFIN/COMPANIES 1 rows, /TRBASE/AUDIT_LOG 0 rows, /RT/TENANTS tenant ACME" {
			t.Fatalf("dependent tables must be emptied first: %s", got)
		}
		if !strings.Contains(dry.SQL(), "DELETE FROM") {
			t.Fatalf("plan:\n%s", dry.SQL())
		}
		if _, err := ten.Delete(ctx, tenantA, kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if n := count(t, k, "/TRFIN/COMPANIES"); n != 1 {
			t.Errorf("companies left: %d, want only the one of GLOBEX", n)
		}
		if n := count(t, k, "/TRFIN/BANK_ACCOUNTS"); n != 0 {
			t.Errorf("bank accounts left: %d", n)
		}
		list, _ = ten.List(ctx)
		if len(list) != 1 || list[0].ID != "GLOBEX" {
			t.Fatalf("tenants: %+v", list)
		}
		log, _ := k.Log(ctx)
		if log[0].Operation != "TENANT_DELETE" || log[0].Target != model.Some(tenantA) {
			t.Errorf("log: %+v", log[0])
		}
		var nf *rterr.NotFound
		if _, err := ten.Delete(ctx, tenantA, kernel.Options{Confirm: true}); !errors.As(err, &nf) {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})
}

func TestTenantReferencedBySharedTable(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		notes := &model.Package{
			Manifest: model.Manifest{ID: "TRNOTES", Version: "1.0.0"},
			Tables: []model.Table{{
				Name: "/TRNOTES/TENANT_NOTES", TenantSpecific: false, DataClass: model.Application,
				Fields: []model.Field{
					{Name: "NOTE_ID", LocalName: "NOTE_ID", Domain: model.TenantDomain, Origin: model.OriginTable},
					{Name: "OWNER", LocalName: "OWNER", Domain: model.TenantDomain, Origin: model.OriginTable},
				},
				PrimaryKey: []string{"NOTE_ID"},
				ForeignKeys: []model.ForeignKey{{Name: "OWNER", Target: model.TenantTable, Origin: model.OriginTable,
					Columns: []model.ColumnPair{{Field: "OWNER", References: model.TenantTableKey}}}},
			}},
		}
		if _, err := k.Install(ctx, []*model.Package{notes}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		mustInsert(t, k, "/TRNOTES/TENANT_NOTES", values{"NOTE_ID": "N1", "OWNER": tenantA})
		var ref *rterr.TenantReferenced
		if _, err := k.Tenants().Delete(ctx, tenantA, kernel.Options{Confirm: true}); !errors.As(err, &ref) || ref.Tables[0] != "/TRNOTES/TENANT_NOTES" {
			t.Fatalf("expected TenantReferenced, got %v", err)
		}
		// The plain foreign key also blocks deleting the tenant row directly.
		_, err := k.DB().ExecContext(ctx, `DELETE FROM "/RT/TENANTS"`)
		violation(t, k.Dialect().ClassifyError(err), rterr.ViolationForeignKey, "tenant with data")
	})
}

func TestCheck(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		if d, err := k.Check(ctx); err != nil || len(d) != 0 {
			t.Fatalf("fresh install: %v %v", err, d)
		}
		for _, stmt := range []string{
			`ALTER TABLE "/TRFIN/COUNTRIES" ADD COLUMN "EXTRA" TEXT`,
			`CREATE TABLE "STRAY" ("X" INTEGER)`,
			`DROP TABLE "/TRFIN/PAYMENTS"`,
		} {
			if _, err := k.DB().ExecContext(ctx, stmt); err != nil {
				t.Fatal(err)
			}
		}
		d, err := k.Check(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{
			"/TRFIN/COUNTRIES.EXTRA: column is not in the repository",
			"/TRFIN/PAYMENTS: table is missing in the database",
			"STRAY: table is not in the repository",
		}
		if len(d) != len(want) {
			t.Fatalf("got %v", d)
		}
		for i := range want {
			if d[i].String() != want[i] {
				t.Errorf("got %q, want %q", d[i], want[i])
			}
		}
	})
}
