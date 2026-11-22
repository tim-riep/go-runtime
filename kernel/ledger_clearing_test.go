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
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// clearing writes a /TRFIN/CLEARING as JSON: the open items of 2026 as
// "number/item" and the new items.
func clearing(company, typ, date string, open []string, items ...item) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"COMPANY":"%s","DOCUMENT_TYPE":"%s","POSTING_DATE":"%s","OPEN_ITEMS":[`, company, typ, date)
	for i, o := range open {
		no, n, _ := strings.Cut(o, "/")
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"FISCAL_YEAR":2026,"DOCUMENT_NO":"%s","ITEM_NO":%s}`, no, n)
	}
	b.WriteString("]")
	p := posting(company, typ, date, "", items...)
	b.WriteString(p[strings.Index(p, `,"ITEMS":[`):])
	return b.String()
}

// clear clears as user and returns the number of the clearing entry.
func (e ledgerEnv) clear(user, c string) string {
	e.t.Helper()
	_, no := created(e.t, e.run(user, "CLEAR", c), "entry")
	return no
}

// clearing returns the clearing of the items of an entry and of their
// ledger entries.
func (e ledgerEnv) clearing(no string) string {
	e.t.Helper()
	return e.run("", "CLEARING_LINES", `"1000"`, "2026", `"`+no+`"`)
}

// openAt returns the open items of a ledger in 1000 at a key date.
func (e ledgerEnv) openAt(ledger, date string) string {
	e.t.Helper()
	return e.run("", "OPEN_AT", `"`+ledger+`"`, `"1000"`, `"`+date+`"`)
}

func TestLedgerClearingChecks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		inv := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		pay := e.post("anna", posting("1000", "DZ", "2026-03-05", "", acc("100000", "D", "90.00"), cust("0000000001", "C", "90.00")))
		gl := e.post("anna", posting("1000", "SA", "2026-03-05", "", acc("110000", "D", "90.00"), acc("400000", "C", "90.00")))
		later := e.post("anna", posting("1000", "DR", "2026-07-01", "", cust(e.later, "D", "90.00"), acc("400000", "C", "90.00")))
		only0L := e.post("anna", posting("1000", "DR", "2026-03-01", `"LEDGER":"0L"`, cust("0000000001", "D", "10.00"), acc("400000", "C", "10.00")))
		rejected := func(c string, wants ...string) {
			t.Helper()
			out := e.run("anna", "CLEAR", c)
			wantIn(t, out, append([]string{`"SEVERITY":"ERROR"`}, wants...)...)
			if strings.Contains(out, `"CREATED":[{`) {
				t.Errorf("clearing created an entry: %s", out)
			}
		}

		// The items sum up to zero, with the new items on the same partner.
		rejected(clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", pay + "/2"}), "the items to clear on the customer 0000000001 sum up to 10.00, not to zero")
		rejected(clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", pay + "/2"}, acc("100000", "D", "20.00"), cust("0000000001", "C", "20.00")),
			"sum up to -10.00, not to zero")
		// One account or partner.
		rejected(clearing("1000", "DZ", "2026-07-10", []string{inv + "/1", later + "/1"}),
			"the items to clear are on the customer 0000000001 and on the customer "+e.later+"; only items of one account or partner are cleared together")
		rejected(clearing("1000", "DZ", "2026-03-10", []string{pay + "/2", gl + "/1"}), "are on the customer 0000000001 and on the account 110000")
		// Open items only, each once, existing, posted before the clearing.
		rejected(clearing("1000", "DZ", "2026-03-10", []string{inv + "/2"}), "the item 2 of the journal entry "+inv+" of 2026 is no open item")
		rejected(clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", inv + "/1"}), "the item 1 of the journal entry "+inv+" of 2026 is selected twice")
		rejected(clearing("1000", "DZ", "2026-03-10", []string{"0000000099/1"}), "the item 1 of the journal entry 0000000099 of 2026 does not exist in the company 1000")
		rejected(clearing("1000", "DZ", "2026-03-03", []string{inv + "/1", pay + "/2"}), "is posted on 2026-03-05, after the clearing on 2026-03-03")
		rejected(clearing("1000", "DZ", "2026-03-10", nil), "a clearing clears at least one open item")
		// The same ledgers.
		rejected(clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", only0L + "/1"}),
			"the item 1 of the journal entry "+only0L+" of 2026 is posted to the ledgers 0L, the items before to 0L, 2L; only items of the same ledgers are cleared together")
		// The new items are checked as in every posting.
		out := e.run("anna", "CLEAR", clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", pay + "/2"}, acc("100000", "D", "10.00"), cust("0000000001", "C", "10.00"), acc("400000", "C", "1.00")))
		wantIn(t, out, `"REASON":"VALIDATION"`, "debit 10.00 and credit 11.00 differ by -1.00")
		// carl may not clear with the type DZ.
		out = e.run("carl", "CLEAR", clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", pay + "/2"}, acc("100000", "D", "10.00"), cust("0000000001", "C", "10.00")))
		wantIn(t, out, "you may not post journal entries of the type DZ in the company 1000")
		// Nothing is cleared.
		for _, no := range []string{inv, pay, gl, later, only0L} {
			if got := e.clearing(no); strings.Contains(got, "2026/") {
				t.Errorf("cleared %s:\n%s", no, got)
			}
		}
		e.send("GET", finEntries+"JournalEntries?$filter=KIND%20eq%20'CLEARING'", "anna", "", 200, `"value":[]`)
		e.consistent()
	})
}

func TestLedgerClearingWithoutPosting(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		inv := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		pay := e.post("anna", posting("1000", "DZ", "2026-03-05", "", acc("100000", "D", "100.00"), cust("0000000001", "C", "100.00")))
		if got, want := e.openAt("0L", "2026-03-31"), inv+"/1 100.00\n"+pay+"/2 -100.00"; got != want {
			t.Errorf("open items before:\n%s\nwant\n%s", got, want)
		}
		clr := e.clear("anna", clearing("1000", "DZ", "2026-03-20", []string{inv + "/1", pay + "/2"}))
		// A clearing entry without items and ledger entries lists the items.
		e.send("GET", entryPath("1000", 2026, clr)+"?$expand=JournalEntryItems,ClearedItems", "anna", "", 200,
			`"KIND":"CLEARING","STATUS":"POSTED"`, `"JournalEntryItems":[]`,
			`"LINE_NO":1,"ITEM_YEAR":2026,"ITEM_DOCUMENT_NO":"`+inv+`","ITEM_NO":1`, `"LINE_NO":2,"ITEM_YEAR":2026,"ITEM_DOCUMENT_NO":"`+pay+`","ITEM_NO":2`)
		if got := e.lines("1000", clr); got != "" {
			t.Errorf("ledger entries of a clearing without items:\n%s", got)
		}
		c := "2026/" + clr + " 2026-03-20"
		if got, want := e.clearing(inv), "1 "+c+"\n2 -\n0L 1 "+c+"\n0L 2 -\n2L 1 "+c+"\n2L 2 -"; got != want {
			t.Errorf("cleared invoice:\n%s\nwant\n%s", got, want)
		}
		if got := e.clearing(pay); !strings.Contains(got, "2 "+c+"\n") || !strings.Contains(got, "2L 2 "+c) {
			t.Errorf("cleared payment:\n%s", got)
		}
		// Open items at a key date: open before the clearing date, cleared
		// from it on.
		if got, want := e.openAt("2L", "2026-03-19"), inv+"/1 100.00\n"+pay+"/2 -100.00"; got != want {
			t.Errorf("open items before the clearing:\n%s\nwant\n%s", got, want)
		}
		if got := e.openAt("0L", "2026-03-20"); got != "" {
			t.Errorf("open items after the clearing:\n%s", got)
		}
		e.consistent()
		// Cleared items are not cleared again, and their entries are not
		// reversed; nor is the clearing entry while it clears.
		wantIn(t, e.run("anna", "CLEAR", clearing("1000", "DZ", "2026-03-21", []string{inv + "/1"})), "the item 1 of the journal entry "+inv+" of 2026 is cleared already")
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+inv+`"`, "null"), "the items 1 of the journal entry "+inv+" are cleared; reset the clearing first")
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+clr+`"`, "null"), "the journal entry "+clr+" of 2026 clears items; reset the clearing first")
		// Resetting needs CANCEL.
		wantIn(t, e.run("carl", "RESET_CLEARING", `"1000"`, "2026", `"`+clr+`"`), `"REASON":"NOT_AUTHORIZED"`)
		wantIn(t, e.run("anna", "RESET_CLEARING", `"1000"`, "2026", `"`+inv+`"`), "the journal entry "+inv+" of 2026 is no clearing entry")
		// Resetting opens the items; the clearing entry without items is
		// reversed.
		wantIn(t, e.run("anna", "RESET_CLEARING", `"1000"`, "2026", `"`+clr+`"`), `"FAILURES":[]`)
		if got, want := e.clearing(inv), "1 -\n2 -\n0L 1 -\n0L 2 -\n2L 1 -\n2L 2 -"; got != want {
			t.Errorf("invoice after the reset:\n%s\nwant\n%s", got, want)
		}
		e.send("GET", entryPath("1000", 2026, clr), "anna", "", 200, `"STATUS":"REVERSED"`)
		if got := e.openAt("0L", "2026-03-31"); got != inv+"/1 100.00\n"+pay+"/2 -100.00" {
			t.Errorf("open items after the reset:\n%s", got)
		}
		wantIn(t, e.run("anna", "RESET_CLEARING", `"1000"`, "2026", `"`+clr+`"`), "the clearing entry "+clr+" of 2026 clears no items")
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+clr+`"`, "null"), "the journal entry "+clr+" of 2026 is reversed already")
		e.consistent()
		// The items are cleared again, and then the invoice is not reversed.
		again := e.clear("anna", clearing("1000", "DZ", "2026-03-25", []string{pay + "/2", inv + "/1"}))
		if got := e.clearing(inv); !strings.HasPrefix(got, "1 2026/"+again+" 2026-03-25\n") {
			t.Errorf("cleared again:\n%s", got)
		}
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+inv+`"`, "null"), "are cleared; reset the clearing first")
		e.consistent()
	})
}

func TestLedgerClearingWithPosting(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		balance := func(ledger, want string) {
			t.Helper()
			if got := e.run("", "BALANCE", `"`+ledger+`"`, `"1000"`, `"/TRFIN/CUSTOMER"`, `"0000000001"`); got != want {
				t.Errorf("balance in %s: %s, want %s", ledger, got, want)
			}
		}
		// A customer pays two invoices: the payment clears them.
		inv1 := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		inv2 := e.post("anna", posting("1000", "DR", "2026-03-02", "", cust("0000000001", "D", "19.00"), acc("400000", "C", "19.00")))
		pay := clearing("1000", "DZ", "2026-03-10", []string{inv1 + "/1", inv2 + "/1"}, acc("100000", "D", "119.00"), cust("0000000001", "C", "119.00"))
		clr := e.clear("anna", pay)
		e.send("GET", entryPath("1000", 2026, clr)+"?$expand=JournalEntryItems,ClearedItems", "anna", "", 200,
			`"DOCUMENT_TYPE":"DZ"`, `"KIND":"CLEARING"`, `"ACCOUNT":"140000","PARTNER":"0000000001","DEBIT_CREDIT":"C","AMOUNT":119`, `"OPEN_ITEM":true`,
			`"ITEM_DOCUMENT_NO":"`+inv2+`"`)
		c := "2026/" + clr + " 2026-03-10"
		if got, want := e.clearing(clr), "1 -\n2 "+c+"\n0L 1 -\n0L 2 "+c+"\n2L 1 -\n2L 2 "+c; got != want {
			t.Errorf("clearing entry:\n%s\nwant\n%s", got, want)
		}
		for _, no := range []string{inv1, inv2} {
			if got := e.clearing(no); !strings.HasPrefix(got, "1 "+c+"\n") || strings.Count(got, c) != 3 {
				t.Errorf("invoice %s:\n%s", no, got)
			}
		}
		balance("0L", "0.00")
		balance("2L", "0.00")
		if got := e.openAt("0L", "2026-03-31"); got != "" {
			t.Errorf("open items:\n%s", got)
		}
		e.consistent()

		// A clearing posts to the ledgers of its items: an invoice of 2L
		// only is paid in 2L only.
		inv3 := e.post("anna", posting("1000", "DR", "2026-03-11", `"LEDGER":"2L"`, cust("0000000001", "D", "50.00"), acc("400000", "C", "50.00")))
		clr2 := e.clear("anna", clearing("1000", "DZ", "2026-03-12", []string{inv3 + "/1"}, acc("100000", "D", "50.00"), cust("0000000001", "C", "50.00")))
		if got := e.lines("1000", clr2); strings.Count(got, "\n") != 1 || strings.Contains(got, "0L") {
			t.Errorf("clearing in 2L:\n%s", got)
		}
		e.send("GET", entryPath("1000", 2026, clr2), "anna", "", 200, `"LEDGER":"2L"`)
		balance("0L", "0.00")
		balance("2L", "0.00")
		e.consistent()

		// Reversing cleared entries is rejected; after the reset the
		// clearing entry and the invoice are reversed.
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+clr+`"`, "null"), "the items 2 of the journal entry "+clr+" are cleared; reset the clearing first")
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+inv1+`"`, "null"), "the items 1 of the journal entry "+inv1+" are cleared; reset the clearing first")
		wantIn(t, e.run("anna", "RESET_CLEARING", `"1000"`, "2026", `"`+clr+`"`), `"FAILURES":[]`)
		e.send("GET", entryPath("1000", 2026, clr), "anna", "", 200, `"STATUS":"POSTED"`)
		for _, no := range []string{inv1, inv2, clr} {
			if got := e.clearing(no); strings.Contains(got, "2026/") {
				t.Errorf("%s after the reset:\n%s", no, got)
			}
		}
		if got, want := e.openAt("0L", "2026-03-31"), inv1+"/1 100.00\n"+inv2+"/1 19.00\n"+clr+"/2 -119.00"; got != want {
			t.Errorf("open items after the reset:\n%s\nwant\n%s", got, want)
		}
		e.consistent()
		_, revClr := created(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+clr+`"`, "null"), "reversal1")
		e.send("GET", entryPath("1000", 2026, clr), "anna", "", 200, `"STATUS":"REVERSED"`, `"REVERSAL_NO":"`+revClr+`"`)
		if got := e.lines("1000", revClr); strings.Count(got, "\n") != 3 {
			t.Errorf("reversal of the clearing entry:\n%s", got)
		}
		balance("0L", "119.00")
		_, rev := created(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+inv1+`"`, "null"), "reversal1")
		balance("0L", "19.00")
		// The invoice and its reversal clear each other.
		e.clear("anna", clearing("1000", "DZ", "2026-03-31", []string{inv1 + "/1", rev + "/1"}))
		if got, want := e.openAt("0L", "2026-03-31"), inv2+"/1 19.00\n"+clr+"/2 -119.00\n"+revClr+"/2 119.00"; got != want {
			t.Errorf("open items at the end:\n%s\nwant\n%s", got, want)
		}
		e.consistent()
	})
}

func TestLedgerClearingOData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		e.send("GET", finEntries+"$metadata", "dora", "", 200, `Name="CLEAR"`, `Name="RESET_CLEARING"`, "TRFIN_OPEN_ITEM_KEY")
		inv := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		// CLEAR answers with the final key of the clearing entry.
		c := clearing("1000", "DZ", "2026-03-10", []string{inv + "/1"}, acc("100000", "D", "100.00"), cust("0000000001", "C", "100.00"))
		c = strings.Replace(c, `{"COMPANY"`, `{"LINK":"web","HEADER_TEXT":"payment","COMPANY"`, 1)
		e.send("POST", finEntries+"CLEAR", "anna", strings.Replace(c, `"DEBIT_CREDIT":"C","AMOUNT":"100.00"`, `"DEBIT_CREDIT":"C","AMOUNT":"90.00"`, 1), 400, "sum up to 10.00, not to zero")
		e.send("POST", finEntries+"CLEAR", "dora", c, 403)
		e.send("POST", finEntries+"CLEAR", "anna", c, 200, `"LINK":"web","COMPANY":"1000","FISCAL_YEAR":2026,"DOCUMENT_NO":"0001000001"`)
		e.send("GET", entryPath("1000", 2026, inv)+"/JournalEntryItems?$filter=CLEARING_NO%20ne%20null", "anna", "", 200,
			`"ITEM_NO":1`, `"CLEARING_YEAR":2026,"CLEARING_NO":"0001000001","CLEARING_DATE":"2026-03-10"`)
		e.send("GET", finEntries+"LedgerEntries?$filter=CLEARING_NO%20eq%20'0001000001'&$count=true", "anna", "", 200, `"@odata.count":4`)
		e.send("GET", entryPath("1000", 2026, "0001000001")+"/ClearedItems", "anna", "", 200, `"ITEM_DOCUMENT_NO":"`+inv+`"`)
		// RESET_CLEARING answers with the clearing entry.
		e.send("POST", entryPath("1000", 2026, "0001000001")+"/TRFIN.RESET_CLEARING", "dora", `{}`, 403)
		e.send("POST", entryPath("1000", 2026, "0001000001")+"/TRFIN.RESET_CLEARING", "anna", `{}`, 200, `"KIND":"CLEARING","STATUS":"POSTED"`)
		e.send("POST", entryPath("1000", 2026, "0001000001")+"/TRFIN.RESET_CLEARING", "anna", `{}`, 400, "clears no items")
		e.send("GET", finEntries+"LedgerEntries?$filter=CLEARING_NO%20ne%20null&$count=true", "anna", "", 200, `"@odata.count":0`)
		e.consistent()
	})
}

func TestLedgerParallelClearing(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		if !strings.HasSuffix(t.Name(), "postgres") {
			t.Skip("SQLite has one writer")
		}
		e := postingKernel(t, k)
		inv := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		// Clearings with a payment and without items race for the invoice.
		pay := e.post("anna", posting("1000", "DZ", "2026-03-02", "", acc("100000", "D", "100.00"), cust("0000000001", "C", "100.00")))
		var wg sync.WaitGroup
		var mu sync.Mutex
		var won []string
		for i := range 6 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := clearing("1000", "DZ", "2026-03-10", []string{inv + "/1"}, acc("100000", "D", "100.00"), cust("0000000001", "C", "100.00"))
				if i%2 == 1 {
					c = clearing("1000", "DZ", "2026-03-10", []string{inv + "/1", pay + "/2"})
				}
				out, err := k.Run(context.Background(), kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}, "/TRFINTEST/API", "CLEAR", []json.RawMessage{json.RawMessage(c)})
				if err != nil {
					t.Error(err)
					return
				}
				var s string
				if json.Unmarshal(out, &s) != nil {
					s = string(out)
				}
				mu.Lock()
				defer mu.Unlock()
				if strings.Contains(s, `"FAILURES":[]`) && !strings.Contains(s, `"SEVERITY":"ERROR"`) {
					won = append(won, s)
				} else {
					t.Logf("lost: %s", s)
				}
			}()
		}
		wg.Wait()
		if len(won) != 1 {
			t.Fatalf("%d clearings succeeded:\n%s", len(won), strings.Join(won, "\n"))
		}
		_, clr := created(t, won[0], "entry")
		c := "2026/" + clr + " 2026-03-10"
		if got := e.clearing(inv); !strings.HasPrefix(got, "1 "+c+"\n") || strings.Count(got, c) != 3 {
			t.Errorf("invoice:\n%s", got)
		}
		e.send("GET", finEntries+"JournalEntries?$filter=KIND%20eq%20'CLEARING'&$count=true", "anna", "", 200, `"@odata.count":1`)
		e.send("GET", finEntries+"LedgerEntries?$filter=CLEARING_NO%20ne%20null%20and%20CLEARING_NO%20ne%20'"+clr+"'&$count=true", "anna", "", 200, `"@odata.count":0`)
		e.consistent()
	})
}
