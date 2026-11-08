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
	"github.com/tim-riep/go-runtime/vm"
)

// TestExampleCode runs the code of the example packages: TRFIN reads and
// writes companies, TROTTO syncs them and writes the audit log of TRBASE
// in a session of its own, and implements the extension point of TRFIN
// that checks new companies once it is installed.
func TestExampleCode(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Users().Create(ctx, "ops", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		const S = "/TRFIN/COMPANY_SERVICE"
		boss := companyAdmin(t, k, "ACME")
		mustCallAs(t, k, boss, S, "ADD_COUNTRY", `"DE"`, `"Germany"`)
		mustCallAs(t, k, boss, S, "ADD_COUNTRY", `"DE"`, `"Germany"`)
		if got := mustCallAs(t, k, boss, S, "CREATE", `"C1"`, `"Zeta"`, `"DE"`); !strings.Contains(got, `"STATUS":"ACTIVE"`) {
			t.Errorf("CREATE without TROTTO = %s", got)
		}

		// Installing TROTTO brings an implementation of the extension point
		// /TRFIN/COMPANY_CHECKS. TRFIN is compiled again only because TROTTO
		// adds a field to /TRFIN/COMPANIES, not for the implementation.
		p, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TROTTO")}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range p.Changes {
			if c.Kind == plan.CompilePackage && c.Name == "TRFIN" && c.Detail != "uses /TRFIN/COMPANIES" {
				t.Errorf("TRFIN compiled again: %s", c)
			}
		}
		if !strings.Contains(planText(p), "create-implementation /TROTTO/COMPANY_CHECKS (of /TRFIN/COMPANY_CHECKS by /TROTTO/COMPANY_CHECKS)") {
			t.Errorf("plan:\n%s", planText(p))
		}
		rec := &vm.LogRecorder{}
		out, err := call(k, kernel.ExecOptions{Tenant: "ACME", User: boss.User, VM: vm.Options{Log: rec}}, S, "CREATE", `"C2"`, `"Alpha"`, `"DE"`)
		if err != nil || !strings.Contains(out, `"STATUS":"BLOCKED"`) {
			t.Errorf("CREATE with TROTTO = %s, %v", out, err)
		}
		if len(rec.Entries) != 1 || rec.Entries[0].Text != "C2 waits for the OTTO sync as OTTO-C2" {
			t.Errorf("log = %v", rec.Entries)
		}
		// Other countries are not checked by TROTTO, and a company without
		// name in Germany is rejected.
		mustCallAs(t, k, boss, S, "ADD_COUNTRY", `"FR"`, `"France"`)
		if got := mustCallAs(t, k, boss, S, "CREATE", `"C3"`, `"Beta"`, `"FR"`); !strings.Contains(got, `"STATUS":"ACTIVE"`) {
			t.Errorf("CREATE in FR = %s", got)
		}
		_, err = call(k, boss, S, "CREATE", `"C4"`, `""`, `"DE"`)
		var exc *vm.Exception
		if !errors.As(err, &exc) || exc.Class != "/TRFIN/COMPANY_REJECTED" || exc.Text != "company C4 rejected: OTTO needs a name" {
			t.Errorf("CREATE without name: %v", err)
		}
		// Deactivated for the tenant, TROTTO checks nothing there.
		if err := k.Implementations().Deactivate(ctx, "/TROTTO/COMPANY_CHECKS", kernel.ImplementationOptions{Tenant: "ACME"}); err != nil {
			t.Fatal(err)
		}
		if got := mustCallAs(t, k, boss, S, "CREATE", `"C5"`, `"Gamma"`, `"DE"`); !strings.Contains(got, `"STATUS":"ACTIVE"`) {
			t.Errorf("CREATE with TROTTO inactive = %s", got)
		}
		if err := k.Implementations().Reset(ctx, "/TROTTO/COMPANY_CHECKS", kernel.ImplementationOptions{Tenant: "ACME"}); err != nil {
			t.Fatal(err)
		}
		// C1 and C5 are active in DE, C2 waits for the sync.
		if got := mustCallAs(t, k, boss, S, "BLOCK_COUNTRY", `"DE"`); got != "2" {
			t.Errorf("BLOCK_COUNTRY = %s", got)
		}
		mustCallAs(t, k, boss, S, "RENAME", `"C1"`, `"Zeta AG"`)
		if got := mustCall(t, k, "ACME", S, "IN_COUNTRY", `"DE"`); !strings.Contains(got, `"NAME":"Alpha"`) || strings.Index(got, "Alpha") > strings.Index(got, "Zeta AG") {
			t.Errorf("IN_COUNTRY = %s", got)
		}

		// New companies in Germany got their partner ID when they were
		// saved, C1 from before TROTTO has none. The action /TROTTO/SYNC
		// needs EXECUTE on /TROTTO/MARKETPLACE for the country.
		if got := mustCall(t, k, "ACME", S, "FIND", `"C2"`); !strings.Contains(got, `"/TROTTO/OTTO_ID":"OTTO-C2"`) {
			t.Errorf("FIND C2 = %s", got)
		}
		if got := mustCall(t, k, "ACME", S, "FIND", `"C3"`); !strings.Contains(got, `"/TROTTO/OTTO_ID":null`) {
			t.Errorf("FIND C3 in FR = %s", got)
		}
		if got, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, "/TROTTO/COMPANY_SYNC", "SYNC", `"DE"`); err != nil || got != "0" {
			t.Errorf("SYNC without user: %s %v", got, err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "ops", Role: "/TROTTO/SYNC_OPERATOR", Tenant: "ACME"}, kernel.RoleOptions{}); err != nil {
			t.Fatal(err)
		}
		ops := kernel.ExecOptions{Tenant: "ACME", User: model.Some("ops")}
		// A sales channel goes with its company: the operator gives C5 its
		// partner ID with the action and adds the channel below it.
		r := result(t, mustCallAs(t, k, ops, "/TROTTO/COMPANY_SYNC", "CONNECT", `"C5"`, `"fashion"`))
		if len(r["FAILURES"]) != 0 || count(t, k, "/TROTTO/COMPANY_CHANNELS") != 1 {
			t.Errorf("CONNECT: %v", r)
		}
		// A company on OTTO stays in a country of the marketplace.
		if _, err := call(k, boss, S, "MOVE", `"C5"`, `"FR"`); err == nil || !strings.Contains(err.Error(), "C5 is on OTTO, which has no marketplace in FR") {
			t.Errorf("MOVE to FR: %v", err)
		}
		sync := func(want string) {
			t.Helper()
			if got, err := call(k, ops, "/TROTTO/COMPANY_SYNC", "SYNC", `"DE"`); err != nil || got != want {
				t.Errorf("SYNC = %s, %v; want %s", got, err, want)
			}
		}
		audited := func() (int, string) {
			var n int
			var text *string
			if err := k.DB().QueryRowContext(ctx, `SELECT COUNT(*), MIN("TEXT") FROM "/TRBASE/AUDIT_LOG"`).Scan(&n, &text); err != nil {
				t.Fatal(err)
			}
			if text == nil {
				return n, ""
			}
			return n, *text
		}
		if k.Dialect().Name() == "sqlite" {
			// SQLite has one writing transaction at a time: the audit session
			// writes first, and the companies cannot be saved at the end of
			// the execution while it holds the database.
			_, err := call(k, ops, "/TROTTO/COMPANY_SYNC", "SYNC", `"DE"`)
			var ns *rterr.BusinessObjectsNotSaved
			if !errors.As(err, &ns) || !strings.Contains(err.Error(), "/RT/LOCK_TIMEOUT") {
				t.Errorf("SYNC on SQLite: %v", err)
			}
			if got := mustCall(t, k, "ACME", S, "FIND", `"C1"`); strings.Contains(got, "OTTO-C1") {
				t.Errorf("C1 synced on SQLite: %s", got)
			}
			return
		}
		sync("1")
		if got := mustCall(t, k, "ACME", S, "FIND", `"C1"`); !strings.Contains(got, `"/TROTTO/OTTO_ID":"OTTO-C1"`) {
			t.Errorf("FIND C1 after SYNC = %s", got)
		}
		sync("0")
		if n, text := audited(); n != 3 || text != "Synced 0 companies for DE" {
			t.Errorf("audit log: %d %q", n, text)
		}
	})
}
