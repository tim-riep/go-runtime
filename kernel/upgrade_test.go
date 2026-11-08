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
	"github.com/tim-riep/go-runtime/rterr"
)

const tenantA = "ACME"

// withData installs the examples and adds one tenant, country, company and
// bank account.
func withData(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	initialized(t, k)
	pkgs := examples(t)
	if k.TargetVersion() < 5 {
		pkgs = legacyExamples(t)
	}
	if k.TargetVersion() < 11 { // before authorization objects
		for _, p := range pkgs {
			p.AuthorizationObjects = nil
		}
	}
	if k.TargetVersion() < 12 { // before roles
		for _, p := range pkgs {
			p.Roles = nil
			p.DataElements = withoutKernelDomains(p.DataElements)
		}
	}
	if _, err := k.Install(context.Background(), pkgs, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	mustInsert(t, k, model.TenantTable, values{"TENANT": tenantA, "CREATED_AT": testutil.Epoch})
	mustInsert(t, k, "/TRFIN/COUNTRIES", values{"COUNTRY_CODE": "DE", "NAME": "Germany", "CURRENCY": "EUR"})
	mustInsert(t, k, "/TRFIN/COMPANIES", values{model.TenantField: tenantA, "COMPANY_ID": "C1", "NAME": "Acme GmbH", "COUNTRY": "DE",
		"CAPITAL": "25000.50", "FOUNDED_ON": "2020-02-29", "/TROTTO/OTTO_ID": "OTTO-1"})
	mustInsert(t, k, "/TRFIN/BANK_ACCOUNTS", values{model.TenantField: tenantA, "COMPANY_ID": "C1", "ACCOUNT_NO": 1, "IBAN": "DE00123"})
}

func TestUpgradeKeepsData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		up := testutil.Example(t, "upgrade/TROTTO")
		p, err := upgrade(k, ctx, up, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Destructive()) != 0 {
			t.Fatalf("unexpected destructive changes: %v", p.Destructive())
		}
		w := where(k, model.TenantField, "COMPANY_ID")
		if v := scalar(t, k, "/TRFIN/COMPANIES", "/TROTTO/OTTO_ID", w, tenantA, "C1"); v != "OTTO-1" {
			t.Errorf("OTTO_ID = %v", v)
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "/TROTTO/ACTIVE", w, tenantA, "C1"); v != false {
			t.Errorf("default of new field = %v", v)
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "CAPITAL", w, tenantA, "C1"); v != "25000.50" {
			t.Errorf("CAPITAL = %v", v)
		}
		if n := count(t, k, "/TRFIN/BANK_ACCOUNTS"); n != 1 {
			t.Errorf("bank accounts after rebuild: %d", n)
		}
		// The longer CHAR accepts 32 characters now.
		long := strings.Repeat("X", 32)
		mustInsert(t, k, "/TRFIN/COMPANIES", values{model.TenantField: tenantA, "COMPANY_ID": "C2", "NAME": "Two", "COUNTRY": "DE", "/TROTTO/OTTO_ID": long})
		// The rebuilt table still enforces its foreign keys.
		violation(t, insert(t, k, "/TRFIN/COMPANIES", values{model.TenantField: tenantA, "COMPANY_ID": "C3", "NAME": "Three", "COUNTRY": "XX"}), rterr.ViolationForeignKey, "unknown country")
		violation(t, insert(t, k, "/TRFIN/BANK_ACCOUNTS", values{model.TenantField: tenantA, "COMPANY_ID": "NOPE", "ACCOUNT_NO": 2, "IBAN": "X"}), rterr.ViolationForeignKey, "unknown company")

		s, _ := k.Schema(ctx)
		if s.Packages["TROTTO"].Manifest.Version != "1.1.0" {
			t.Fatalf("version not updated")
		}
		if !s.Packages["TROTTO"].UpdatedAt.After(s.Packages["TROTTO"].InstalledAt) && !s.Packages["TROTTO"].UpdatedAt.Equal(s.Packages["TROTTO"].InstalledAt) {
			t.Error("timestamps")
		}
		want := testutil.AddPackages(t, baseSchema(t, k), testutil.Example(t, "TRFIN"), up)
		sameSchema(t, k, want)
		consistent(t, k)
	})
}

func TestUpgradeDestructive(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		if _, err := upgrade(k, ctx, testutil.Example(t, "upgrade/TROTTO"), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		// Version 1.2.0 shortens OTTO_ID and drops the shop table.
		next := testutil.Example(t, "upgrade/TROTTO")
		next.Manifest.Version = "1.2.0"
		next.Tables = next.Tables[:1] // /TROTTO/OTTO_CONNECTIONS
		next.SettingExtensions[0].Tables = next.SettingExtensions[0].Tables[:1]
		next.Extensions[0].ForeignKeys = nil
		next.Extensions[0].Fields = next.Extensions[0].Fields[:2]
		for i := range next.Domains {
			if next.Domains[i].Name == "/TROTTO/OTTO_ID" {
				next.Domains[i].Length = 10
			}
		}
		var doms []model.Domain
		for _, d := range next.Domains {
			if d.Name != "/TROTTO/OTTO_SHOP_ID" {
				doms = append(doms, d)
			}
		}
		next.Domains = doms

		_, err := upgrade(k, ctx, next, kernel.Options{})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) || len(dc.Changes) < 3 {
			t.Fatalf("expected DestructiveChange, got %v", err)
		}
		dry, err := upgrade(k, ctx, next, kernel.Options{DryRun: true, AllowDestructive: true})
		if err != nil || dry.Executed || !strings.Contains(dry.Text(), "[DESTRUCTIVE]") {
			t.Fatalf("dry run: %v\n%s", err, dry.Text())
		}
		if _, err := upgrade(k, ctx, next, kernel.Options{AllowDestructive: true}); err != nil {
			t.Fatal(err)
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "/TROTTO/OTTO_ID", where(k, "COMPANY_ID"), "C1"); v != "OTTO-1" {
			t.Errorf("OTTO_ID = %v", v)
		}
		violation(t, insert(t, k, "/TRFIN/COMPANIES", values{model.TenantField: tenantA, "COMPANY_ID": "C9", "NAME": "N", "COUNTRY": "DE", "/TROTTO/OTTO_ID": raw{"ELEVEN-CHAR"}}),
			rterr.ViolationCheck, "shortened field")
		consistent(t, k)
	})
}

func TestShorteningFailsOnExistingData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		next := testutil.Example(t, "TROTTO")
		next.Manifest.Version = "1.0.1"
		next.Domains[0].Length = 3 // "OTTO-1" does not fit
		_, err := upgrade(k, ctx, next, kernel.Options{AllowDestructive: true})
		var ee *rterr.ExecError
		if !errors.As(err, &ee) {
			t.Fatalf("expected ExecError, got %v", err)
		}
		s, _ := k.Schema(ctx)
		if s.Packages["TROTTO"].Manifest.Version != "1.0.0" || s.Domains["/TROTTO/OTTO_ID"].Length != 20 {
			t.Fatal("failed upgrade changed the repository")
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "/TROTTO/OTTO_ID", where(k, "COMPANY_ID"), "C1"); v != "OTTO-1" {
			t.Errorf("data changed: %v", v)
		}
	})
}

func TestUpgradeCannotBreakDependents(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		withData(t, k)
		fin := testutil.Example(t, "TRFIN")
		fin.Manifest.Version = "1.1.0"
		var tables []model.Table
		for _, tb := range fin.Tables {
			if tb.Name == "/TRFIN/COUNTRIES" {
				tables = append(tables, tb)
			}
		}
		fin.Tables = tables
		fin.Structures, fin.ArrayTypes = nil, nil
		_, err := upgrade(k, context.Background(), fin, kernel.Options{AllowDestructive: true})
		var v *rterr.ValidationError
		if !errors.As(err, &v) || v.Code != rterr.CodeUnknownTable {
			t.Fatalf("expected the extension of TROTTO to block the upgrade, got %v", err)
		}

		major := testutil.Example(t, "TRBASE")
		major.Manifest.Version = "2.0.0"
		var dep *rterr.DependencyError
		if _, err := upgrade(k, context.Background(), major, kernel.Options{}); err != nil {
			t.Fatalf(">=1.0.0 accepts 2.0.0: %v", err)
		}
		fin = testutil.Example(t, "TRFIN")
		fin.Manifest.Requires[0].Constraint = model.Some("^1.0.0")
		_, err = upgrade(k, context.Background(), fin, kernel.Options{})
		if !errors.As(err, &dep) || dep.Kind != rterr.DependencyVersion {
			t.Fatalf("expected version conflict, got %v", err)
		}
	})
}

func TestUpgradeChecks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		var ni *rterr.NotInstalled
		other := testutil.Example(t, "TROTTO")
		other.Manifest.ID = "TRNEW"
		if _, err := upgrade(k, ctx, other, kernel.Options{}); !errors.As(err, &ni) {
			t.Fatalf("expected NotInstalled, got %v", err)
		}
		old := testutil.Example(t, "TROTTO")
		old.Manifest.Version = "0.9.0"
		var ia *rterr.InvalidArgument
		if _, err := upgrade(k, ctx, old, kernel.Options{}); !errors.As(err, &ia) {
			t.Fatalf("expected downgrade to be rejected, got %v", err)
		}
		p, err := k.Plan(ctx, testutil.Example(t, "upgrade/TROTTO"))
		if err != nil || p.Operation != "upgrade" || p.Executed {
			t.Fatalf("plan: %v %+v", err, p)
		}
	})
}

func TestUninstall(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withData(t, k)
		if _, err := upgrade(k, ctx, testutil.Example(t, "upgrade/TROTTO"), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		var inUse *rterr.InUse
		if _, err := k.Uninstall(ctx, "TRFIN", kernel.Options{Confirm: true}); !errors.As(err, &inUse) || inUse.RequiredBy[0] != "TROTTO" {
			t.Fatalf("expected InUse, got %v", err)
		}
		var conf *rterr.ConfirmationRequired
		if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{}); !errors.As(err, &conf) {
			t.Fatalf("expected ConfirmationRequired, got %v", err)
		}
		if p, err := k.Uninstall(ctx, "TROTTO", kernel.Options{DryRun: true}); err != nil || p.Executed {
			t.Fatalf("dry run: %v", err)
		}
		if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "NAME", where(k, "COMPANY_ID"), "C1"); v != "Acme GmbH" {
			t.Errorf("company lost: %v", v)
		}
		if exists, _ := k.Dialect().TableExists(ctx, k.DB(), "/TROTTO/OTTO_SHOPS"); exists {
			t.Error("/TROTTO/OTTO_SHOPS still exists")
		}
		sameSchema(t, k, testutil.AddPackages(t, baseSchema(t, k), testutil.Example(t, "TRFIN")))
		consistent(t, k)
		var ni *rterr.NotInstalled
		if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true}); !errors.As(err, &ni) {
			t.Fatalf("expected NotInstalled, got %v", err)
		}
	})
}
