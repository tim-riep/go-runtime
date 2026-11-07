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

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/vm"
)

// TestExampleCode runs the code of the example packages: TRFIN reads and
// writes companies, TROTTO syncs them and writes the audit log of TRBASE
// in a session of its own, and its hook is inserted into TRFIN once it is
// installed.
func TestExampleCode(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRBASE"), testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const S = "/TRFIN/COMPANY_SERVICE"
		mustCall(t, k, "ACME", S, "ADD_COUNTRY", `"DE"`, `"Germany"`)
		mustCall(t, k, "ACME", S, "ADD_COUNTRY", `"DE"`, `"Germany"`)
		if got := mustCall(t, k, "ACME", S, "CREATE", `"C1"`, `"Zeta"`, `"DE"`); !strings.Contains(got, `"STATUS":"ACTIVE"`) {
			t.Errorf("CREATE without TROTTO = %s", got)
		}

		// Installing TROTTO recompiles TRFIN, whose include of the hook
		// now finds it.
		p, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TROTTO")}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range p.Changes {
			found = found || c.Kind == plan.CompilePackage && c.String() == "compile-package TRFIN (includes /TROTTO/COMPANY_HOOK if found)"
		}
		if !found {
			t.Errorf("no recompilation of TRFIN in %v", p.Changes)
		}
		rec := &vm.LogRecorder{}
		out, err := call(k, kernel.ExecOptions{Tenant: "ACME", VM: vm.Options{Log: rec}}, S, "CREATE", `"C2"`, `"Alpha"`, `"DE"`)
		if err != nil || !strings.Contains(out, `"STATUS":"BLOCKED"`) {
			t.Errorf("CREATE with TROTTO = %s, %v", out, err)
		}
		if len(rec.Entries) != 1 || rec.Entries[0].Text != "C2 waits for the OTTO sync" {
			t.Errorf("log = %v", rec.Entries)
		}
		expectCall(t, k, "ACME", S, "BLOCK_COUNTRY", "1", `"DE"`)
		mustCall(t, k, "ACME", S, "RENAME", `"C1"`, `"Zeta AG"`)
		if got := mustCall(t, k, "ACME", S, "IN_COUNTRY", `"DE"`); !strings.Contains(got, `"NAME":"Alpha"`) || strings.Index(got, "Alpha") > strings.Index(got, "Zeta AG") {
			t.Errorf("IN_COUNTRY = %s", got)
		}

		count := func() (int, string) {
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
			// cannot insert while the default session has updated companies.
			_, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, "/TROTTO/COMPANY_SYNC", "SYNC", `"DE"`)
			if exceptionClass(err) != "/RT/SESSION_ERROR" {
				t.Errorf("SYNC on SQLite: %v", err)
			}
			if n, _ := count(); n != 0 {
				t.Errorf("audit log has %d rows", n)
			}
			return
		}
		expectCall(t, k, "ACME", "/TROTTO/COMPANY_SYNC", "SYNC", "2", `"DE"`)
		if got := mustCall(t, k, "ACME", S, "FIND", `"C1"`); !strings.Contains(got, `"/TROTTO/OTTO_ID":"OTTO-C1"`) {
			t.Errorf("FIND C1 after SYNC = %s", got)
		}
		expectCall(t, k, "ACME", "/TROTTO/COMPANY_SYNC", "SYNC", "0", `"DE"`)
		if n, text := count(); n != 2 || text != "Synced 0 companies for DE" {
			t.Errorf("audit log: %d %q", n, text)
		}
	})
}
