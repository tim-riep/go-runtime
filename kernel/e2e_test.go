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
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestEndToEnd walks through the life cycle of the example packages with
// two tenants: install, upgrade, constraint checks across tenant borders,
// tenant deletion and removal of all packages.
func TestEndToEnd(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		consistent(t, k)

		// Two tenants.
		for _, id := range []string{"ACME", "GLOBEX"} {
			if _, err := k.Tenants().Create(ctx, id, id+" tenant"); err != nil {
				t.Fatal(err)
			}
		}

		// All three packages in one call, given in arbitrary order.
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)

		mustInsert(t, k, "COUNTRIES", values{"COUNTRY_CODE": "DE", "NAME": "Germany", "CURRENCY": "EUR"})
		mustInsert(t, k, "COUNTRIES", values{"COUNTRY_CODE": "AT", "NAME": "Austria", "CURRENCY": "EUR"})
		mustInsert(t, k, "COMPANIES", values{model.TenantField: "ACME", "COMPANY_ID": "A1", "NAME": "Acme GmbH", "COUNTRY": "DE",
			"CAPITAL": "1000000.00", "FOUNDED_ON": time.Date(1999, 12, 31, 0, 0, 0, 0, time.UTC), "/TROTTO/OTTO_ID": "P-ACME"})
		mustInsert(t, k, "COMPANIES", values{model.TenantField: "GLOBEX", "COMPANY_ID": "G1", "NAME": "Globex AG", "COUNTRY": "AT"})
		mustInsert(t, k, "BANK_ACCOUNTS", values{model.TenantField: "ACME", "COMPANY_ID": "A1", "ACCOUNT_NO": 1, "IBAN": "DE02120300000000202051",
			"BALANCE": "-12.34", "OPENED_AT": time.Date(2024, 5, 6, 7, 8, 9, 123456000, time.UTC)})
		mustInsert(t, k, "BANK_ACCOUNTS", values{model.TenantField: "GLOBEX", "COMPANY_ID": "G1", "ACCOUNT_NO": 1, "IBAN": "AT611904300234573201"})

		// Upgrade of the extending package.
		if _, err := k.Upgrade(ctx, testutil.Example(t, "upgrade/TROTTO"), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
		mustInsert(t, k, "OTTO_CONNECTIONS", values{model.TenantField: "ACME", "COMPANY_ID": "A1", "ENDPOINT": "https://partner.example"})
		mustInsert(t, k, "OTTO_SHOPS", values{model.TenantField: "ACME", "SHOP_ID": "S1", "COMPANY_ID": "A1", "NAME": "Acme Shop"})
		if _, err := k.DB().ExecContext(ctx, `UPDATE "COMPANIES" SET "/TROTTO/MAIN_SHOP" = 'S1' WHERE "COMPANY_ID" = 'A1'`); err != nil {
			t.Fatal(err)
		}

		w := where(k, model.TenantField, "COMPANY_ID")
		if v := scalar(t, k, "COMPANIES", "CAPITAL", w, "ACME", "A1"); v != "1000000.00" {
			t.Errorf("CAPITAL = %v", v)
		}
		if v := scalar(t, k, "BANK_ACCOUNTS", "BALANCE", w, "ACME", "A1"); v != "-12.34" {
			t.Errorf("BALANCE = %v", v)
		}
		if v := scalar(t, k, "BANK_ACCOUNTS", "OPENED_AT", w, "ACME", "A1"); !v.(time.Time).Equal(time.Date(2024, 5, 6, 7, 8, 9, 123456000, time.UTC)) {
			t.Errorf("OPENED_AT = %v", v)
		}
		if v := scalar(t, k, "COMPANIES", "/TROTTO/OTTO_ID", w, "ACME", "A1"); v != "P-ACME" {
			t.Errorf("OTTO_ID after upgrade = %v", v)
		}

		// Constraints reject invalid rows.
		failing := []struct {
			what  string
			table string
			row   values
			kind  rterr.ViolationKind
		}{
			{"unknown country", "COMPANIES", values{model.TenantField: "ACME", "COMPANY_ID": "A2", "NAME": "X", "COUNTRY": "FR"}, rterr.ViolationForeignKey},
			{"unknown company", "BANK_ACCOUNTS", values{model.TenantField: "ACME", "COMPANY_ID": "NONE", "ACCOUNT_NO": 2, "IBAN": "X"}, rterr.ViolationForeignKey},
			{"company of another tenant", "BANK_ACCOUNTS", values{model.TenantField: "GLOBEX", "COMPANY_ID": "A1", "ACCOUNT_NO": 2, "IBAN": "X"}, rterr.ViolationForeignKey},
			{"shop of another tenant", "COMPANIES", values{model.TenantField: "GLOBEX", "COMPANY_ID": "G2", "NAME": "X", "COUNTRY": "AT", "/TROTTO/MAIN_SHOP": "S1"}, rterr.ViolationForeignKey},
			{"unknown tenant", "COMPANIES", values{model.TenantField: "NOBODY", "COMPANY_ID": "N1", "NAME": "X", "COUNTRY": "DE"}, rterr.ViolationForeignKey},
			{"invalid fixed value", "COMPANIES", values{model.TenantField: "ACME", "COMPANY_ID": "A3", "NAME": "X", "COUNTRY": "DE", "STATUS": raw{"MERGED"}}, rterr.ViolationCheck},
			{"CHAR too long", "COUNTRIES", values{"COUNTRY_CODE": raw{"DEU"}, "NAME": "X"}, rterr.ViolationCheck},
			{"invalid date", "COMPANIES", values{model.TenantField: "ACME", "COMPANY_ID": "A4", "NAME": "X", "COUNTRY": "DE", "FOUNDED_ON": raw{"2023-02-30"}}, rterr.ViolationCheck},
			{"missing NOT NULL field", "COMPANIES", values{model.TenantField: "ACME", "COMPANY_ID": "A5", "COUNTRY": "DE"}, rterr.ViolationNotNull},
			{"duplicate key", "COMPANIES", values{model.TenantField: "ACME", "COMPANY_ID": "A1", "NAME": "X", "COUNTRY": "DE"}, rterr.ViolationUnique},
		}
		for _, f := range failing {
			violation(t, insert(t, k, f.table, f.row), f.kind, f.what)
		}
		// The same key is fine in another tenant.
		mustInsert(t, k, "COMPANIES", values{model.TenantField: "GLOBEX", "COMPANY_ID": "A1", "NAME": "Globex A1", "COUNTRY": "DE"})

		// A tenant with data cannot be deleted directly.
		_, err := k.DB().ExecContext(ctx, `DELETE FROM "/RT/TENANTS" WHERE "TENANT" = 'GLOBEX'`)
		violation(t, k.Dialect().ClassifyError(err), rterr.ViolationForeignKey, "deleting a tenant with data")

		// Deleting the tenant removes its rows only.
		p, err := k.Tenants().Delete(ctx, "GLOBEX", kernel.Options{Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(p.Text(), "delete-rows COMPANIES") {
			t.Errorf("plan:\n%s", p.Text())
		}
		if n := count(t, k, "COMPANIES"); n != 1 {
			t.Errorf("companies left: %d", n)
		}
		if n := count(t, k, "BANK_ACCOUNTS"); n != 1 {
			t.Errorf("bank accounts left: %d", n)
		}
		if n := count(t, k, model.TenantTable); n != 1 {
			t.Errorf("tenants left: %d", n)
		}
		consistent(t, k)

		// Remove all packages again, dependents first.
		for _, id := range []model.PackageID{"TROTTO", "TRFIN", "TRBASE"} {
			if _, err := k.Uninstall(ctx, id, kernel.Options{Confirm: true}); err != nil {
				t.Fatalf("uninstall %s: %v", id, err)
			}
			consistent(t, k)
		}
		sameSchema(t, k, testutil.KernelSchema(t))
		for _, table := range []string{"COUNTRIES", "COMPANIES", "BANK_ACCOUNTS", "OTTO_SHOPS"} {
			if ok, _ := k.Dialect().TableExists(ctx, k.DB(), table); ok {
				t.Errorf("table %s still exists", table)
			}
		}

		log, err := k.Log(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ops []string
		for i := len(log) - 1; i >= 0; i-- {
			ops = append(ops, log[i].Operation)
		}
		if got := strings.Join(ops, " "); got != "MIGRATE INSTALL UPGRADE TENANT_DELETE UNINSTALL UNINSTALL UNINSTALL" {
			t.Errorf("log: %s", got)
		}
	})
}
