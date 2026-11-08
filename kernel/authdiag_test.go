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
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestLastFailedCheck keeps the last failed check of a user in a tenant
// with its place and the grants of the user, also when the execution is
// rolled back; authorized and executions without user leave it alone.
func TestLastFailedCheck(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k := openCheckKernel(t, db.DSN)
			anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
			var nf *rterr.NotFound
			if _, err := k.LastFailedCheck(ctx, "anna", "ACME"); !errors.As(err, &nf) {
				t.Fatalf("before: %v", err)
			}
			if _, err := call(k, anna, "/TRAUTH/CHECKS", "UPDATE_COMPANY", `"C1"`, `"FR"`); err == nil {
				t.Fatal("not denied")
			}
			f, err := k.LastFailedCheck(ctx, "anna", "ACME")
			if err != nil || f.Object != "/TRFIN/COMPANY" || string(f.Values["COUNTRY"]) != `"FR"` || string(f.Values["ACTIVITY"]) != `"UPDATE"` ||
				f.Site.Class.Or("") != "/TRAUTH/CHECKS" || f.Site.Method.Or("") != "UPDATE_COMPANY" || f.Site.Line.Or(0) != 6 ||
				f.Site.Package.Or("") != "TRAUTH" || !strings.Contains(string(f.Grants), "/TRFIN/COMPANY_CLERK") {
				t.Fatalf("last failed: %+v %v", f, err)
			}
			// authorized and executions without user are not recorded.
			if got, err := call(k, anna, "/TRAUTH/CHECKS", "CAN", `"FR"`, `"READ"`); err != nil || got != "false" {
				t.Fatal(got, err)
			}
			_, _ = call(k, kernel.ExecOptions{Tenant: "ACME"}, "/TRAUTH/CHECKS", "UPDATE_COMPANY", `"C1"`, `"DE"`)
			if g, _ := k.LastFailedCheck(ctx, "anna", "ACME"); g == nil || !g.FailedAt.Equal(f.FailedAt) {
				t.Errorf("overwritten: %+v", g)
			}
			// A failed check of a native method names the method.
			if _, err := call(k, anna, "/TRTEST/NATIVES", "AUTHORIZE", `"FR"`, "false"); err == nil {
				t.Fatal("native not denied")
			}
			if g, _ := k.LastFailedCheck(ctx, "anna", "ACME"); g == nil || g.Site.Kernel.Or("") != "/TRTEST/NATIVES.AUTHORIZE" || string(g.Values["COMPANY_ID"]) != `"C1"` {
				t.Errorf("native: %+v", g)
			}
		})
	}
}

// TestTrace records the checks of a user with their result, the filters
// with condition and rows, up to the maximum, and nothing after stop.
func TestTrace(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k := openCheckKernel(t, db.DSN)
			anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
			mustCall(t, k, "ACME", "/TRAUTH/DOC_ACCESS", "ADD", "1", `"A"`, "1")
			if _, err := k.Roles().Import(ctx, []byte(`{"name":"/LOCAL/DOCS","texts":{"en":{"label":"Documents"}},"authorizations":[
				{"object":"/TRAUTH/DOC","fields":{"CODE":[{"value":"A"}],"LEVEL":[{"pattern":"*"}],"ACTIVITY":[{"value":"READ"}]}}]}`), "d.json", kernel.RoleOptions{}); err != nil {
				t.Fatal(err)
			}
			assign(t, k, "anna", "/LOCAL/DOCS", "ACME", model.None[time.Time](), model.None[time.Time]())
			if r, err := k.Trace(ctx, "anna", ""); err != nil || r.Trace != nil || !r.Ended || len(r.Entries) != 0 {
				t.Fatalf("no trace: %+v %v", r, err)
			}
			if _, err := k.StartTrace(ctx, "anna", kernel.TraceOptions{Tenant: model.Some("ACME"), MaxEntries: 4, By: "test"}); err != nil {
				t.Fatal(err)
			}
			mustRun := func(method string, args ...string) {
				t.Helper()
				_, _ = call(k, anna, "/TRAUTH/CHECKS", method, args...)
			}
			mustRun("UPDATE_COMPANY", `"C1"`, `"PT"`)
			mustRun("CAN", `"FR"`, `"READ"`)
			if got, err := call(k, anna, "/TRAUTH/DOC_ACCESS", "FIRST", "10"); err != nil || !strings.Contains(got, `"CODE":"A"`) {
				t.Fatalf("first: %s %v", got, err)
			}
			mustRun("UPDATE_COMPANY", `"C1"`, `"FR"`)
			mustRun("UPDATE_COMPANY", `"C1"`, `"DE"`) // beyond the maximum
			r, err := k.Trace(ctx, "anna", "ACME")
			if err != nil || r.Trace == nil || r.Ended || len(r.Entries) != 4 {
				t.Fatalf("trace: %+v %v", r, err)
			}
			e := r.Entries
			if e[0].Mode != "check" || !e[0].Allowed.Or(false) || e[0].Site.Line.Or(0) != 6 ||
				e[1].Mode != "expression" || e[1].Allowed.Or(true) || string(e[1].Values["COMPANY_ID"]) != "null" ||
				e[2].Mode != "filter" || e[2].Rows.Or(-1) != 1 || !strings.Contains(e[2].Condition.Or(""), `"CODE"`) || !strings.Contains(string(e[2].Values["CODE"]), "column") ||
				e[3].Allowed.Or(true) {
				t.Errorf("entries: %+v", e)
			}
			if err := k.StopTrace(ctx, "anna"); err != nil {
				t.Fatal(err)
			}
			if _, err := k.StartTrace(ctx, "anna", kernel.TraceOptions{Tenant: model.Some("BETA")}); err != nil {
				t.Fatal(err)
			}
			mustRun("UPDATE_COMPANY", `"C1"`, `"PT"`)
			if r, _ := k.Trace(ctx, "anna", ""); len(r.Entries) != 0 {
				t.Errorf("other tenant: %+v", r.Entries)
			}
			if err := k.StopTrace(ctx, "anna"); err != nil {
				t.Fatal(err)
			}
			if r, _ := k.Trace(ctx, "anna", ""); r.Trace != nil || !r.Ended {
				t.Errorf("stopped: %+v", r)
			}
		})
	}
}

// TestTestAuthorizations runs test methods that grant themselves
// authorizations with /TRBASE/TEST_AUTHORIZATIONS; outside of rt test the
// methods fail.
func TestTestAuthorizations(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			k := openCheckKernel(t, db.DSN)
			results, err := k.Test(context.Background(), "TRAUTH", nil, kernel.ExecOptions{})
			if err != nil || len(results) != 3 {
				t.Fatalf("%+v %v", results, err)
			}
			for _, r := range results {
				if !r.Passed {
					t.Errorf("%s.%s: %s (line %d)", r.Class, r.Method, r.Message, r.Line)
				}
			}
			anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
			if _, err := call(k, anna, "/TRAUTH/CHECKS", "GRANT_ALL"); err == nil || !strings.Contains(err.Error(), "works only in test methods") {
				t.Errorf("outside a test: %v", err)
			}
		})
	}
}
