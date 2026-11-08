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
	"testing"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestMigrateFromVersion1 creates a database with the first kernel
// version, installs packages and adds data, then migrates it.
func TestMigrateFromVersion1(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		k.SetTargetVersion(1)
		withData(t, k)
		consistent(t, k)

		k.SetTargetVersion(kernelschema.Latest())
		if _, err := k.Schema(ctx); !errors.Is(err, rterr.ErrNotInitialized) {
			t.Fatalf("expected ErrNotInitialized before the migration, got %v", err)
		}
		p, err := k.Init(ctx, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		notices := map[string]string{}
		for _, c := range p.Notices() {
			if c.Kind == plan.AlterDataClass {
				notices[c.Table] = c.Detail
			}
		}
		if notices["/TRFIN/COMPANIES"] != "none -> application, provisional until package TRFIN is applied again" || notices[model.TenantTable] != "" {
			t.Fatalf("migration notices: %v", notices)
		}
		consistent(t, k)

		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if c := s.Tables["/TRFIN/COMPANIES"].DataClass; c != model.Application {
			t.Errorf("provisional class of /TRFIN/COMPANIES = %q", c)
		}
		if c := s.Tables[model.TenantTable].DataClass; c != model.System {
			t.Errorf("class of the tenant table = %q", c)
		}
		if len(s.Packages) != 3 || s.Packages["TRFIN"].InstalledAt.IsZero() {
			t.Fatalf("packages after migration: %v", s.PackageIDs())
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "NAME", where(k, "COMPANY_ID"), "C1"); v != "Acme GmbH" {
			t.Errorf("data lost: %v", v)
		}

		// Applying the packages again sets their real classes and restores
		// the data elements, which the first kernel version did not store.
		if _, err := k.Languages().Add(ctx, "de", ""); err != nil {
			t.Fatal(err)
		}
		up, err := k.Upgrade(ctx, testutil.Example(t, "TRFIN"), kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(up.Destructive()) != 0 || len(up.Notices()) == 0 {
			t.Fatalf("class changes must be notices: %+v", up.Changes)
		}
		s, _ = k.Schema(ctx)
		if c := s.Tables["/TRFIN/COMPANIES"].DataClass; c != model.Configuration {
			t.Errorf("class after upgrade = %q", c)
		}
		consistent(t, k)
	})
}

// TestMigrateFromVersion4 migrates a database of the kernel version before
// structures and installs packages with structures and includes afterwards.
func TestMigrateFromVersion4(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		k.SetTargetVersion(4)
		withData(t, k)
		consistent(t, k)

		k.SetTargetVersion(kernelschema.Latest())
		p, err := k.Init(ctx, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Destructive()) != 0 || change(p, plan.CreateTable, "/RT/STRUCTURES") == nil ||
			change(p, plan.AddColumn, "/RT/FIELDS.INCLUDE_NAME") == nil || change(p, plan.AlterValues, "/RT/FIELDS.ORIGIN") == nil {
			t.Fatalf("migration plan:\n%s", p.Text())
		}
		consistent(t, k)
		if v := scalar(t, k, "/TRFIN/COMPANIES", "NAME", where(k, "COMPANY_ID"), "C1"); v != "Acme GmbH" {
			t.Errorf("data lost: %v", v)
		}
		if _, err := k.Install(ctx, []*model.Package{trs("1.0.0"), trx()}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
	})
}

// TestMigrateByteOrder gives text columns of kernel and package tables
// that compare by the default collation of a PostgreSQL database the byte
// order when the kernel migrates to version 14.
func TestMigrateByteOrder(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		k.SetTargetVersion(13)
		withData(t, k)
		if k.Dialect().Name() != "postgres" {
			return
		}
		for _, s := range []string{
			`ALTER TABLE "/TRFIN/COMPANIES" ALTER COLUMN "COMPANY_ID" TYPE VARCHAR(10) COLLATE "default", ALTER COLUMN "NAME" TYPE VARCHAR(80) COLLATE "default"`,
			`ALTER TABLE "/RT/USERS" ALTER COLUMN "USER_NAME" TYPE VARCHAR(64) COLLATE "default"`,
		} {
			if _, err := k.DB().ExecContext(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
		k.SetTargetVersion(kernelschema.Latest())
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
		var coll string
		if err := k.DB().QueryRowContext(ctx, `SELECT collation_name FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = '/TRFIN/COMPANIES' AND column_name = 'COMPANY_ID'`).Scan(&coll); err != nil || coll != "C" {
			t.Errorf("collation %q, %v", coll, err)
		}
		if v := scalar(t, k, "/TRFIN/COMPANIES", "NAME", where(k, "COMPANY_ID"), "C1"); v != "Acme GmbH" {
			t.Errorf("data lost: %v", v)
		}
	})
}
