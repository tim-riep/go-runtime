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

	"github.com/tim-riep/go-runtime/kernel"
)

// TestLedgerNumberRangeNode maintains the intervals of the general ledger
// through the node /trfin/number-ranges: admin has /TRFIN/CONFIGURATION,
// dora and clara may not maintain number ranges.
func TestLedgerNumberRangeNode(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		if err := k.ActivateHTTPService(context.Background(), "/trfin"); err != nil {
			t.Fatal(err)
		}
		const node = "/trfin/number-ranges"
		const intervals = node + "/intervals"
		journal := `{"RANGE_NAME":"/TRFIN/JOURNAL_ENTRY","INTERVAL_ID":"01","SUBOBJECT":"1000","FISCAL_YEAR":2027,"FROM_NO":"1","TO_NO":"999999","CURRENT_NO":null,"EXTERNAL":false}`

		// The objects of the general ledger and their intervals.
		a := e.send("GET", node, "admin", "", 200, `"NAME":"/TRBP/PARTNER"`, `"NAME":"/TRFIN/JOURNAL_ENTRY"`, `"SUBOBJECT_NAME":"COMPANY"`)
		if ct := a.header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type: %s", ct)
		}
		// The partners took numbers; PostgreSQL buffers them in blocks.
		before := e.send("GET", intervals+"?object=/TRBP/PARTNER", "admin", "", 200, `"INTERVAL_ID":"01"`).body
		if strings.Contains(before, `"CURRENT_NO":null`) || strings.Contains(before, `"CURRENT_NO":"0000000001"`) {
			t.Fatalf("partner interval: %s", before)
		}
		e.send("GET", intervals+"?object=/TRFIN/JOURNAL_ENTRY", "admin", "", 200, `[]`)

		// Adding, changing and deleting an interval answers with the
		// intervals of the object; the changes are committed.
		e.send("POST", intervals, "admin", journal, 200, `"SUBOBJECT":"1000","FISCAL_YEAR":2027`, `"TO_NO":"0000999999"`)
		e.send("GET", intervals+"?object=/TRFIN/JOURNAL_ENTRY", "admin", "", 200, `"FISCAL_YEAR":2027`)
		e.send("POST", intervals, "admin", journal, 400)
		e.send("PUT", intervals, "admin", strings.Replace(journal, `"999999"`, `"500000"`, 1), 200, `"TO_NO":"0000500000"`)
		e.send("DELETE", intervals, "admin", journal, 200, `[]`)
		e.send("GET", node+"/history?object=/TRFIN/JOURNAL_ENTRY", "admin", "", 200, `"KIND":"add"`, `"KIND":"delete"`, `"CHANGED_BY":"admin"`)

		// Moving the current number back needs confirm.
		partner := `{"RANGE_NAME":"/TRBP/PARTNER","INTERVAL_ID":"01","SUBOBJECT":null,"FISCAL_YEAR":null,"FROM_NO":"1","TO_NO":"4999999999","CURRENT_NO":"1","EXTERNAL":false}`
		e.send("PUT", intervals, "admin", partner, 409, `"error":`)
		if got := e.send("GET", intervals+"?object=/TRBP/PARTNER", "admin", "", 200).body; got != before {
			t.Errorf("unconfirmed change: %s, before %s", got, before)
		}
		e.send("PUT", intervals+"?confirm=true", "admin", partner, 200, `"CURRENT_NO":"0000000001"`)

		// Errors.
		e.send("GET", intervals+"?object=/TRBASE/OTHER", "admin", "", 403, "the number range object /TRBASE/OTHER is not one of the general ledger")
		e.send("GET", intervals, "admin", "", 403, "is not one of the general ledger")
		e.send("POST", intervals, "admin", strings.Replace(journal, "/TRFIN/JOURNAL_ENTRY", "/TRBASE/OTHER", 1), 403)
		e.send("POST", intervals, "admin", `{"RANGE_NAME":"/TRFIN/JOURNAL_ENTRY"}`, 400, "the body is no interval")
		e.send("DELETE", intervals, "admin", strings.Replace(journal, `"01"`, `"77"`, 1), 404)
		a = e.send("POST", node, "admin", "{}", 405)
		if allow := a.header.Get("Allow"); allow != "GET" {
			t.Errorf("Allow: %q", allow)
		}
		e.send("PATCH", intervals, "admin", journal, 405)
		e.send("GET", node+"/nothing", "admin", "", 404, "there is nothing at /trfin/number-ranges/nothing")

		// Authorizations: the node needs /TRBASE/NUMBER_RANGE, which only
		// the configuration of TRFIN grants.
		e.send("GET", node, "dora", "", 403)
		e.send("GET", intervals+"?object=/TRBP/PARTNER", "clara", "", 403)
		e.send("POST", intervals, "dora", journal, 403)
		e.send("GET", intervals+"?object=/TRFIN/JOURNAL_ENTRY", "admin", "", 200, `[]`)
	})
}
