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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/lang/testrun"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

func dbpkg(t *testing.T, name string) *model.Package {
	return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "db", name))
}

// dbKernel installs TRDB and TRDX and creates the tenants ACME and BETA.
func dbKernel(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	if _, err := k.Install(ctx, []*model.Package{dbpkg(t, "TRDB"), dbpkg(t, "TRDX")}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ACME", "BETA"} {
		if _, err := k.Tenants().Create(ctx, id, model.Optional[string]{}); err != nil {
			t.Fatal(err)
		}
	}
}

// call runs a method for a tenant; args are JSON values.
func call(k *kernel.Kernel, opts kernel.ExecOptions, class, method string, args ...string) (string, error) {
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		raw[i] = json.RawMessage(a)
	}
	out, err := k.Run(context.Background(), opts, class, method, raw)
	return string(out), err
}

func mustCall(t *testing.T, k *kernel.Kernel, tenant, class, method string, args ...string) string {
	t.Helper()
	out, err := call(k, kernel.ExecOptions{Tenant: tenant}, class, method, args...)
	if err != nil {
		t.Fatalf("%s.%s: %v", class, method, err)
	}
	return out
}

func expectCall(t *testing.T, k *kernel.Kernel, tenant, class, method, want string, args ...string) {
	t.Helper()
	if got := mustCall(t, k, tenant, class, method, args...); got != want {
		t.Errorf("%s.%s(%s) = %s, want %s", class, method, strings.Join(args, ", "), got, want)
	}
}

func exceptionClass(err error) string {
	var e *vm.Exception
	if errors.As(err, &e) {
		return e.Class
	}
	return ""
}

// TestDatabaseStatements runs select, insert, update and delete of code
// on both databases.
func TestDatabaseStatements(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		dbKernel(t, k)
		const S = "/TRDB/STORE"
		row := mustCall(t, k, "ACME", S, "ADD", `"A"`, `"Alpha"`, `"5.00"`)
		for _, part := range []string{`"ITEM_ID":"A"`, `"NOTE":null`, `"AMOUNT":"5.00"`, `"ACTIVE":false`, `"DUE":null`, `"COUNTRY":"DE"`, `"/TRDX/TAG":null`} {
			if !strings.Contains(row, part) {
				t.Errorf("row %s lacks %s", row, part)
			}
		}
		mustCall(t, k, "ACME", S, "ADD", `"B"`, `"Beta"`, `"0.50"`)
		mustCall(t, k, "ACME", S, "ADD", `"C"`, `"Gamma"`, `"2.00"`)
		// Tenants are separate.
		mustCall(t, k, "BETA", S, "ADD", `"A"`, `"Other"`, `"1.00"`)
		expectCall(t, k, "ACME", S, "COUNT", "3")
		expectCall(t, k, "BETA", S, "COUNT", "1")

		// null and "" stay apart in values and conditions.
		mustCall(t, k, "ACME", S, "ADD_NOTE", `"A"`, `""`)
		mustCall(t, k, "ACME", S, "ADD_NOTE", `"B"`, `null`)
		expectCall(t, k, "ACME", S, "WITHOUT_NOTE", `["B","C"]`)
		expectCall(t, k, "ACME", S, "EMPTY_NOTE", `["A"]`)
		expectCall(t, k, "ACME", S, "NOTE_IS", "1", `""`)
		if got := mustCall(t, k, "ACME", S, "GET", `"A"`); !strings.Contains(got, `"NOTE":""`) {
			t.Errorf("GET A = %s", got)
		}
		expectCall(t, k, "ACME", S, "GET", "null", `"X"`)

		expectCall(t, k, "ACME", S, "TOP", `["A","C"]`, "2")
		expectCall(t, k, "ACME", S, "AMONG", "1", `["A","B","X"]`)
		expectCall(t, k, "ACME", S, "AMONG", "0", `[]`)

		// Constraint violations are exceptions that code catches.
		if got := mustCall(t, k, "ACME", S, "DUPLICATE", `"A"`); !strings.HasPrefix(got, `"duplicate on`) {
			t.Errorf("DUPLICATE = %s", got)
		}
		expectCall(t, k, "ACME", S, "BAD_GROUP", `"foreign key"`, `"Z"`)
		if got := mustCall(t, k, "ACME", S, "MISSING_ROW", `"Q"`); !strings.Contains(got, "found no row") {
			t.Errorf("MISSING_ROW = %s", got)
		}
		expectCall(t, k, "ACME", S, "COUNT", "3")

		expectCall(t, k, "ACME", S, "ACTIVATE", "2", `"1.00"`)
		if got := mustCall(t, k, "ACME", S, "GET", `"A"`); !strings.Contains(got, `"ACTIVE":true`) || !strings.Contains(got, `"NOTE":null`) {
			t.Errorf("GET A after ACTIVATE = %s", got)
		}
		// A loop over a cursor updates the rows it reads.
		expectCall(t, k, "ACME", S, "TOUCH_ALL", "3")
		if got := mustCall(t, k, "ACME", S, "GET", `"C"`); !strings.Contains(got, `"NAME":"Gamma!"`) {
			t.Errorf("GET C after TOUCH_ALL = %s", got)
		}
		expectCall(t, k, "ACME", S, "REMOVE_SMALL", "1")
		expectCall(t, k, "ACME", S, "COUNT", "2")
		expectCall(t, k, "BETA", S, "COUNT", "1")

		// Extension fields of a package that requires TRDB.
		expectCall(t, k, "ACME", "/TRDX/TAGS", "TAG", "1", `"A"`, `"red"`)
		expectCall(t, k, "ACME", "/TRDX/TAGS", "TAGGED", `["A:red"]`, `"red"`)
		expectCall(t, k, "BETA", "/TRDX/TAGS", "TAGGED", `[]`, `"red"`)

		// The limit of a select into an array.
		_, err := call(k, kernel.ExecOptions{Tenant: "ACME", MaxRows: 1}, S, "ALL")
		if exceptionClass(err) != "/RT/TOO_MANY_ROWS" {
			t.Errorf("ALL with MaxRows 1: %v", err)
		}
		_, err = call(k, kernel.ExecOptions{Tenant: "NOBODY"}, S, "COUNT")
		var nf *rterr.NotFound
		if !errors.As(err, &nf) {
			t.Errorf("unknown tenant: %v", err)
		}
	})
}

// TestTransactions checks commit and rollback in code and at the end of
// an execution.
func TestTransactions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		dbKernel(t, k)
		const S = "/TRDB/STORE"
		_, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, S, "ADD_THEN_FAIL", `"F"`)
		if exceptionClass(err) != "/TRDB/MISSING" {
			t.Fatalf("ADD_THEN_FAIL: %v", err)
		}
		expectCall(t, k, "ACME", S, "GET", "null", `"F"`)

		mustCall(t, k, "ACME", S, "ADD_COMMIT_ROLLBACK", `"K"`, `"L"`)
		if got := mustCall(t, k, "ACME", S, "GET", `"K"`); got == "null" {
			t.Error("K was not committed")
		}
		expectCall(t, k, "ACME", S, "GET", "null", `"L"`)

		// RollbackUnlessCommitted drops what the code did not commit.
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME", End: kernel.RollbackUnlessCommitted}, S, "ADD", `"R"`, `"r"`, `"1.00"`); err != nil {
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", S, "GET", "null", `"R"`)

		// Cursors end with their loop, by break or by an exception, so
		// commit is allowed after it.
		expectCall(t, k, "ACME", S, "EXCEPTION_IN_LOOP", "1")
		expectCall(t, k, "ACME", S, "BREAK_IN_LOOP", "1")
	})
}

// TestSessions checks several sessions of one execution.
func TestSessions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		dbKernel(t, k)
		const S, X = "/TRDB/SESSIONS", "/TRDB/STORE"
		sqlite := k.Dialect().Name() == "sqlite"
		opts := kernel.ExecOptions{Tenant: "ACME", LockTimeout: 300 * time.Millisecond}

		// A log session commits while the default session rolls back.
		_, err := call(k, opts, S, "LOG_AND_FAIL", `"G"`)
		if exceptionClass(err) != "/TRDB/MISSING" {
			t.Fatalf("LOG_AND_FAIL: %v", err)
		}
		if mustCall(t, k, "ACME", X, "GET", `"G"`) == "null" {
			t.Error("the row of the log session was not committed")
		}
		expectCall(t, k, "ACME", X, "GET", "null", `"GX"`)

		// Sessions without commit are rolled back with a warning.
		rec := &vm.LogRecorder{}
		o := opts
		o.VM.Log = rec
		if _, err := call(k, o, S, "FORGET", `"H"`); err != nil {
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", X, "GET", "null", `"H"`)
		if len(rec.Entries) != 1 || rec.Entries[0].Level != "warning" || rec.Entries[0].Text != "session 2 rolled back at the end of the execution" {
			t.Errorf("log = %v", rec.Entries)
		}

		// Another session sees committed rows only. A read transaction of
		// SQLite keeps its snapshot; PostgreSQL reads committed rows.
		want := `"0 1"`
		if sqlite {
			want = `"0 0"`
		}
		if got, err := call(k, opts, S, "ISOLATED", `"I"`); err != nil || got != want {
			t.Errorf("ISOLATED = %s, %v; want %s", got, err, want)
		}

		// A session that waits for a lock of another session of the same
		// execution: a lock timeout on PostgreSQL, at once a session error
		// on SQLite, which has only one writing transaction.
		want = `"lock timeout"`
		if sqlite {
			want = `"session error"`
		}
		if got, err := call(k, opts, S, "SELF_BLOCK", `"J"`); err != nil || got != want {
			t.Errorf("SELF_BLOCK = %s, %v; want %s", got, err, want)
		}
		if _, err := call(k, opts, S, "ONE_AFTER_THE_OTHER", `"M"`, `"N"`); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{`"M"`, `"N"`} {
			if mustCall(t, k, "ACME", X, "GET", id) == "null" {
				t.Errorf("%s missing", id)
			}
		}

		_, err = call(k, opts, S, "TOO_MANY")
		if exceptionClass(err) != "/RT/SESSION_ERROR" {
			t.Errorf("TOO_MANY: %v", err)
		}
		expectCall(t, k, "ACME", S, "COMMIT_IN_LOOP", `"session error"`)
		n := mustCall(t, k, "ACME", X, "COUNT")
		expectCall(t, k, "ACME", S, "COMMIT_OTHER_IN_LOOP", n)
		expectCall(t, k, "ACME", S, "WITH_PARAMETER", n)
	})
}

// TestTestIsolation runs test methods that write rows: each runs in a
// tenant of its own, and nothing they do stays.
func TestTestIsolation(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		dbKernel(t, k)
		ctx := context.Background()
		check := func(results []testrun.Result) {
			t.Helper()
			got := map[string]bool{}
			for _, r := range results {
				got[r.Method] = r.Passed
			}
			if len(results) != 3 || !got["ADDS_AND_COMMITS"] || !got["STARTS_EMPTY"] || got["FAILS"] {
				t.Errorf("results = %+v", results)
			}
		}
		results, err := k.Test(ctx, "TRDB", nil, kernel.ExecOptions{})
		if err != nil {
			t.Fatal(err)
		}
		check(results)
		// The same tests from the directory, compiled but not installed.
		results, err = k.Test(ctx, "", dbpkg(t, "TRDB"), kernel.ExecOptions{})
		if err != nil {
			t.Fatal(err)
		}
		check(results)
		tenants, err := k.Tenants().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(tenants) != 2 {
			t.Errorf("test tenants were not deleted: %v", tenants)
		}
		expectCall(t, k, "ACME", "/TRDB/STORE", "COUNT", "0")
	})
}

// companyAdmin creates the user boss with the role /TRFIN/COMPANY_ADMIN in
// a tenant and returns the options to run code as boss there. The
// business object /TRFIN/COMPANY checks the authorization of every change.
func companyAdmin(t *testing.T, k *kernel.Kernel, tenant string) kernel.ExecOptions {
	t.Helper()
	ctx := context.Background()
	if _, err := k.Users().Create(ctx, "boss", "secret", kernel.UserOptions{Tenant: model.Some(tenant)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "boss", Role: "/TRFIN/COMPANY_ADMIN", Tenant: tenant}, kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
	return kernel.ExecOptions{Tenant: tenant, User: model.Some("boss")}
}

// mustCallAs runs a method with options and fails the test on an error.
func mustCallAs(t *testing.T, k *kernel.Kernel, opts kernel.ExecOptions, class, method string, args ...string) string {
	t.Helper()
	out, err := call(k, opts, class, method, args...)
	if err != nil {
		t.Fatalf("%s.%s: %v", class, method, err)
	}
	return out
}
