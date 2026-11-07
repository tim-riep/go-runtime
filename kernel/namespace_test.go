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
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestNamespaceViolationNeverReachesTheDatabase loads a package that
// references a package it does not require. Loading fails, so nothing can
// be planned or installed.
func TestNamespaceViolationNeverReachesTheDatabase(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		before, _ := k.Log(ctx)
		dir := filepath.Join(t.TempDir(), "TRBAD")
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{"id":"TRBAD","version":"1.0.0","requires":[{"id":"TRBASE"}]}`))
		testutil.WriteFile(t, dir, "schema/tables/THINGS.json", []byte(`{"name":"/TRBAD/THINGS","tenantSpecific":false,"dataClass":"application",
			"fields":[{"name":"ID","dataElement":"/TRFIN/COMPANY"},{"name":"NAME","domain":"NAME"}],"primaryKey":["ID"]}`))
		testutil.WriteFile(t, dir, "schema/domains/X.json", []byte(`{"name":"/TRBASE/X","type":"BOOL"}`))
		pkg, err := kernel.LoadDir(dir)
		if pkg != nil {
			t.Fatal("package with violations was loaded")
		}
		if n := len(rterr.All[*rterr.NamespaceViolation](err)); n != 3 {
			t.Fatalf("expected 3 violations, got %d: %v", n, err)
		}
		if !strings.Contains(err.Error(), "reference /TRFIN/COMPANY: expected namespace /TRBAD/, /TRBASE/, /RT/") {
			t.Errorf("message: %v", err)
		}
		log, _ := k.Log(ctx)
		if len(log) != len(before) {
			t.Fatalf("the database was touched: %+v", log)
		}
	})
}

// TestLongGeneratedNames installs objects whose constraint names exceed 63
// bytes; the kernel shortens them with a hash, and the database keeps them
// unchanged.
func TestLongGeneratedNames(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		dir := filepath.Join(t.TempDir(), "TRLONGNAMES")
		table := "/TRLONGNAMES/STATEMENT_LINE_STATEMENT_LINE_ARCHIVE_OF_THE_YEAR" // 61 bytes
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{"id":"TRLONGNAMES","version":"1.0.0","requires":[{"id":"TRBASE"}]}`))
		testutil.WriteFile(t, dir, "schema/tables/"+strings.TrimPrefix(table, "/TRLONGNAMES/")+".json", []byte(`{"name":"`+table+`",
			"tenantSpecific":true,"dataClass":"application",
			"fields":[{"name":"LINE_IDENTIFIER_WITH_A_VERY_LONG_NAME","domain":"/TRBASE/COUNTER"},{"name":"COUNTRY_OF_THE_STATEMENT_LINE","dataElement":"/TRBASE/COUNTRY"}],
			"primaryKey":["LINE_IDENTIFIER_WITH_A_VERY_LONG_NAME"]}`))
		pkg := testutil.LoadDir(t, dir)
		if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		pk := model.PrimaryKeyName(table)
		if len(pk) != model.MaxIdentifierBytes || !strings.Contains(pk, "~") {
			t.Fatalf("primary key name %q", pk)
		}
		var stored string
		if err := k.DB().QueryRowContext(ctx, `SELECT "PK_NAME" FROM "/RT/TABLES" WHERE "TABLE_NAME" = `+k.Dialect().Placeholder(1), table).Scan(&stored); err != nil || stored != pk {
			t.Fatalf("stored primary key name %q: %v", stored, err)
		}
		mustInsert(t, k, model.TenantTable, values{"TENANT": "ACME", "CREATED_AT": testutil.Epoch})
		mustInsert(t, k, table, values{model.TenantField: "ACME", "LINE_IDENTIFIER_WITH_A_VERY_LONG_NAME": 1, "COUNTRY_OF_THE_STATEMENT_LINE": "DE"})
		violation(t, insert(t, k, table, values{model.TenantField: "ACME", "LINE_IDENTIFIER_WITH_A_VERY_LONG_NAME": 2, "COUNTRY_OF_THE_STATEMENT_LINE": raw{"DEU"}}),
			rterr.ViolationCheck, "shortened check name")
		consistent(t, k)
	})
}
