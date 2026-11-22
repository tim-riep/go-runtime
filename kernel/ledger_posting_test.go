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
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

const finEntries = "/rt/odata/trfin/journal_entry_service/"

// postingKernel is ledgerKernel with what postings need besides:
//
//   - intervals 01 (1 to 999999) and 02 (1000000 to 1999999) of
//     /TRFIN/JOURNAL_ENTRY in 1000 and 2000 for 2026
//   - the document types DR (accounts and customers, reversed with DZ),
//     DZ (all), KR (accounts and suppliers), L2 (accounts, only the ledger
//     2L) and AL (accounts and customers, only the group ALL)
//   - the accounts 110000 (open items), 120000 (posted only
//     automatically) and 130000 (blocked in the chart) in 1000, 400000 in
//     2000, and open periods of 2026 in 2000
//   - the partner 0000000001 as supplier in 1000 with the reconciliation
//     account 160000 and the alternative account 141000, and the partner
//     e.later, a customer in 1000 from 2026-07-01 on
//   - the users anna (accountant) and carl (entries of the type SA in
//     1000 in the ledger 0L)
func postingKernel(t *testing.T, k *kernel.Kernel) ledgerEnv {
	t.Helper()
	e := ledgerKernel(t, k)
	ctx := context.Background()
	for _, c := range []string{"1000", "2000"} {
		for _, iv := range []struct{ id, from, to string }{{"01", "1", "999999"}, {"02", "1000000", "1999999"}} {
			if err := k.AddNumberInterval(ctx, "ACME", kernel.NumberInterval{Object: "/TRFIN/JOURNAL_ENTRY", ID: iv.id, Subobject: model.Some(c),
				FiscalYear: model.Some[int64](2026), From: iv.from, To: iv.to}, kernel.NumberRangeOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for user, roles := range map[string][]string{
		"anna": {"/TRFIN/ACCOUNTANT", "/TRFIN/ODATA_USER"},
		"carl": {"/TRFINTEST/POSTING_CLERK", "/TRFIN/ODATA_USER"},
	} {
		if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, r := range roles {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: r, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := k.ActivateService(ctx, "/TRFIN/JOURNAL_ENTRY_SERVICE", "test"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, body string }{
		{finConfig + "DocumentTypes", `{"DOCUMENT_TYPE":"DZ","INTERVAL":"02","ALLOW_ACCOUNTS":true,"ALLOW_CUSTOMERS":true,"ALLOW_SUPPLIERS":true}`},
		{finConfig + "DocumentTypes", `{"DOCUMENT_TYPE":"DR","INTERVAL":"02","ALLOW_ACCOUNTS":true,"ALLOW_CUSTOMERS":true,"REVERSAL_TYPE":"DZ"}`},
		{finConfig + "DocumentTypes", `{"DOCUMENT_TYPE":"KR","INTERVAL":"02","ALLOW_ACCOUNTS":true,"ALLOW_SUPPLIERS":true}`},
		{finConfig + "DocumentTypes", `{"DOCUMENT_TYPE":"L2","INTERVAL":"01","ALLOW_ACCOUNTS":true,"LEDGER":"2L"}`},
		{finConfig + "DocumentTypes", `{"DOCUMENT_TYPE":"AL","INTERVAL":"01","ALLOW_ACCOUNTS":true,"ALLOW_CUSTOMERS":true,"LEDGER_GROUP":"ALL"}`},
		{finConfig + "PostingPeriods", `{"COMPANY":"2000","FISCAL_YEAR":2026,"FROM_PERIOD":1,"TO_PERIOD":12}`},
		{finMaster + "Accounts", `{"CHART":"TRAC","ACCOUNT":"110000","KIND":"BALANCE_SHEET"}`},
		{finMaster + "Accounts", `{"CHART":"TRAC","ACCOUNT":"120000","KIND":"BALANCE_SHEET"}`},
		{finMaster + "Accounts", `{"CHART":"TRAC","ACCOUNT":"130000","KIND":"BALANCE_SHEET","BLOCKED":true}`},
		{finMaster + "CompanyAccounts", `{"COMPANY":"1000","ACCOUNT":"110000","OPEN_ITEMS":true}`},
		{finMaster + "CompanyAccounts", `{"COMPANY":"1000","ACCOUNT":"120000","AUTOMATIC_ONLY":true}`},
		{finMaster + "CompanyAccounts", `{"COMPANY":"1000","ACCOUNT":"130000"}`},
		{finMaster + "CompanyAccounts", `{"COMPANY":"2000","ACCOUNT":"400000"}`},
		{finMaster + "Suppliers", `{"PARTNER":"0000000001","COMPANY":"1000","RECONCILIATION_ACCOUNT":"160000","ALTERNATIVE_ACCOUNT":"141000"}`},
	} {
		if a := e.c.do("POST", c.path, e.as("admin"), c.body); a.status != 201 {
			t.Fatalf("%s %s: %d %s", c.path, c.body, a.status, a.body)
		}
	}
	a := e.c.do("POST", bpService+"Partners", e.as("admin"), `{"GROUPING":"0001","CATEGORY":"ORGANIZATION","NAME1":"Later","PartnerRoles":[{"ROLE":"/TRFIN/CUSTOMER","VALID_FROM":"2026-07-01"}]}`)
	var partner struct{ PARTNER string }
	if err := json.Unmarshal([]byte(a.body), &partner); a.status != 201 || err != nil {
		t.Fatalf("partner: %d %s", a.status, a.body)
	}
	e.later = partner.PARTNER
	if a := e.c.do("POST", finMaster+"Customers", e.as("admin"), `{"PARTNER":"`+e.later+`","COMPANY":"1000","RECONCILIATION_ACCOUNT":"140000","ALTERNATIVE_ACCOUNT":"161000"}`); a.status != 201 {
		t.Fatalf("customer: %d %s", a.status, a.body)
	}
	return e
}

// item is an item of a posting: kind (empty for an account), account,
// partner, D or C and the amount.
type item struct{ kind, account, partner, dc, amount string }

func acc(account, dc, amount string) item { return item{"", account, "", dc, amount} }
func cust(partner, dc, amount string) item {
	return item{"CUSTOMER", "", partner, dc, amount}
}
func supp(partner, dc, amount string) item {
	return item{"SUPPLIER", "", partner, dc, amount}
}

// posting writes a /TRFIN/POSTING as JSON; extra holds further members
// such as "LEDGER":"2L".
func posting(company, typ, date, extra string, items ...item) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"COMPANY":"%s","DOCUMENT_TYPE":"%s","DOCUMENT_DATE":"%s","POSTING_DATE":"%s"`, company, typ, date, date)
	if extra != "" {
		b.WriteString("," + extra)
	}
	b.WriteString(`,"ITEMS":[`)
	for i, it := range items {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"DEBIT_CREDIT":"%s","AMOUNT":"%s"`, it.dc, it.amount)
		if it.kind != "" {
			fmt.Fprintf(&b, `,"ACCOUNT_KIND":"%s"`, it.kind)
		}
		if it.account != "" {
			fmt.Fprintf(&b, `,"ACCOUNT":"%s"`, it.account)
		}
		if it.partner != "" {
			fmt.Fprintf(&b, `,"PARTNER":"%s"`, it.partner)
		}
		b.WriteString("}")
	}
	b.WriteString("]}")
	return b.String()
}

// created returns the key of the journal entry with a link in the result
// of a save, failing the test when there is none.
func created(t *testing.T, out, link string) (year int, no string) {
	t.Helper()
	var r struct {
		CREATED []struct{ ENTITY, LINK, KEY string }
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, c := range r.CREATED {
		if c.ENTITY == "/TRFIN/JOURNAL_ENTRIES" && c.LINK == link {
			var key struct {
				FISCAL_YEAR int
				DOCUMENT_NO string
			}
			if err := json.Unmarshal([]byte(c.KEY), &key); err != nil {
				t.Fatal(err)
			}
			return key.FISCAL_YEAR, key.DOCUMENT_NO
		}
	}
	t.Fatalf("no journal entry %s created: %s", link, out)
	return 0, ""
}

// post posts as user and returns the number of the new entry.
func (e ledgerEnv) post(user, p string) string {
	e.t.Helper()
	out := e.run(user, "POST", p)
	_, no := created(e.t, out, "entry")
	return no
}

// consistent fails the test when the ledgers are not consistent.
func (e ledgerEnv) consistent() {
	e.t.Helper()
	if got := e.run("", "CONSISTENCY"); got != "" {
		e.t.Errorf("ledgers not consistent:\n%s", got)
	}
}

// ifMatch sends a request as user with If-Match * and checks the status
// and that the answer contains the texts.
func (e ledgerEnv) ifMatch(method, path, user, body string, status int, wants ...string) answer {
	e.t.Helper()
	a := e.c.do(method, path, func(r *http.Request) {
		e.as(user)(r)
		r.Header.Set("If-Match", "*")
	}, body)
	if a.status != status {
		e.t.Errorf("%s %s %s: %d, want %d: %s", method, path, body, a.status, status, a.body)
		return a
	}
	wantIn(e.t, a.body, wants...)
	return a
}

// entryPath is the path of a journal entry in the service.
func entryPath(company string, year int, no string) string {
	return fmt.Sprintf("%sJournalEntries(COMPANY='%s',FISCAL_YEAR=%d,DOCUMENT_NO='%s')", finEntries, company, year, no)
}

// lines returns the ledger entries of a journal entry, a line each.
func (e ledgerEnv) lines(company, no string) string {
	e.t.Helper()
	return e.run("", "LEDGER_LINES", `"`+company+`"`, "2026", `"`+no+`"`)
}

// check runs the action CHECK as user and checks that its messages
// contain the texts, or none when no text is given.
func (e ledgerEnv) check(user, p string, wants ...string) {
	e.t.Helper()
	got := e.run(user, "CHECK", p)
	if len(wants) == 0 {
		if !strings.Contains(got, `"MESSAGES":[]`) {
			e.t.Errorf("check of %s: %s", p, got)
		}
		return
	}
	wantIn(e.t, got, wants...)
}

func TestLedgerPostingChecks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		ok := []item{acc("100000", "D", "100.00"), acc("400000", "C", "100.00")}
		e.check("anna", posting("1000", "SA", "2026-03-01", "", ok...))
		wantIn(t, e.run("anna", "CHECK", posting("1000", "SA", "2026-03-01", "", ok...)), `"FISCAL_YEAR":2026,"PERIOD":3`)

		// The entry.
		e.check("anna", posting("9999", "SA", "2026-03-01", "", ok...), `"ITEM_NO":null,"FIELD":"COMPANY","TEXT":"the company 9999 does not exist"`)
		e.check("anna", posting("1000", "XX", "2026-03-01", "", ok...), `"FIELD":"DOCUMENT_TYPE","TEXT":"the document type XX does not exist"`)
		e.check("anna", posting("1000", "SA", "2025-03-01", "", ok...), `"FIELD":"POSTING_DATE","TEXT":"the period 3 of 2025 is not open for postings in the company 1000"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "100.00")), `"FIELD":null,"TEXT":"a journal entry has at least two items, not 1"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "100.00"), acc("400000", "C", "90.00")),
			`"TEXT":"debit 100.00 and credit 90.00 differ by 10.00"`)

		// Ledgers and groups.
		e.check("anna", posting("1000", "SA", "2026-03-01", `"LEDGER":"0L","LEDGER_GROUP":"ALL"`, ok...), `"FIELD":"LEDGER_GROUP","TEXT":"a journal entry goes to a ledger or to a group of ledgers, not to both"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", `"LEDGER":"9L"`, ok...), `"FIELD":"LEDGER","TEXT":"the ledger 9L does not exist"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", `"LEDGER_GROUP":"NONE"`, ok...), `"TEXT":"the group of ledgers NONE does not exist"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", `"LEDGER":"3L"`, ok...), `"FIELD":"LEDGER","TEXT":"the ledgers 3L are not assigned to the company 1000"`)
		e.check("anna", posting("2000", "SA", "2026-03-01", `"LEDGER_GROUP":"ALL"`, acc("100000", "D", "1.00"), acc("400000", "C", "1.00")),
			`"FIELD":"LEDGER_GROUP","TEXT":"the ledgers 2L are not assigned to the company 2000"`)
		e.check("anna", posting("1000", "L2", "2026-03-01", `"LEDGER":"0L"`, ok...), `"TEXT":"journal entries of the type L2 go only to the ledger 2L"`)
		e.check("anna", posting("1000", "L2", "2026-03-01", "", ok...))
		e.check("anna", posting("1000", "AL", "2026-03-01", `"LEDGER":"3L"`, ok...), `"TEXT":"journal entries of the type AL go only to the ledgers of the group ALL"`)
		e.check("anna", posting("1000", "AL", "2026-03-01", `"LEDGER":"0L"`, ok...))

		// Items.
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "0.00"), acc("400000", "C", "0.00")),
			`"ITEM_NO":1,"FIELD":"AMOUNT","TEXT":"the amount is greater than zero, not 0.00"`, `"ITEM_NO":2,"FIELD":"AMOUNT"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "1.00"), cust("0000000001", "C", "1.00")),
			`"ITEM_NO":2,"FIELD":"ACCOUNT_KIND","TEXT":"the document type SA allows no items of the kind CUSTOMER"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("999999", "D", "1.00"), acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"ACCOUNT","TEXT":"the account 999999 does not exist in the company 1000"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("130000", "D", "1.00"), acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"ACCOUNT","TEXT":"the account 130000 is blocked in the chart TRAC"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("140000", "D", "1.00"), acc("400000", "C", "1.00")),
			`"TEXT":"the account 140000 is a reconciliation account for CUSTOMERS; post to the customer or supplier"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("120000", "D", "1.00"), acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"ACCOUNT","TEXT":"the account 120000 is posted only automatically"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("", "D", "1.00"), acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"ACCOUNT","TEXT":"the item needs an account"`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", item{"", "100000", "0000000001", "D", "1.00"}, acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"PARTNER","TEXT":"an item on an account has no partner"`)

		// Customers and suppliers.
		e.check("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "1.00"), acc("400000", "C", "1.00")))
		e.check("anna", posting("1000", "DR", "2026-03-01", "", cust("", "D", "1.00"), acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"PARTNER","TEXT":"an item of the kind CUSTOMER needs a partner"`)
		e.check("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000099", "D", "1.00"), acc("400000", "C", "1.00")),
			`"TEXT":"the partner 0000000099 does not exist"`)
		e.check("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000002", "D", "1.00"), acc("400000", "C", "1.00")),
			`"TEXT":"the partner 0000000002 has no role /TRFIN/CUSTOMER"`, `"TEXT":"the partner 0000000002 has no data in the role /TRFIN/CUSTOMER in the company 1000"`)
		e.check("anna", posting("1000", "DR", "2026-03-01", "", cust(e.later, "D", "1.00"), acc("400000", "C", "1.00")),
			`"ITEM_NO":1,"FIELD":"PARTNER","TEXT":"the role /TRFIN/CUSTOMER of the partner `+e.later+` is not valid on 2026-03-01"`)
		e.check("anna", posting("1000", "DR", "2026-07-01", "", cust(e.later, "D", "1.00"), acc("400000", "C", "1.00")))
		e.check("anna", posting("2000", "DZ", "2026-03-01", "", supp("0000000001", "C", "1.00"), acc("400000", "D", "1.00")),
			`"TEXT":"the partner 0000000001 has no data in the role /TRFIN/SUPPLIER in the company 2000"`)
		e.ifMatch("PATCH", finMaster+"Customers(PARTNER='0000000001',COMPANY='1000')", "admin", `{"BLOCKED":true}`, 204)
		e.check("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "1.00"), acc("400000", "C", "1.00")),
			`"TEXT":"the partner 0000000001 is blocked in the role /TRFIN/CUSTOMER in the company 1000"`)
		e.ifMatch("PATCH", finMaster+"Customers(PARTNER='0000000001',COMPANY='1000')", "admin", `{"BLOCKED":false}`, 204)
		e.ifMatch("PATCH", bpService+"Partners('"+e.later+"')", "admin", `{"BLOCKED":true}`, 204)
		e.check("anna", posting("1000", "DR", "2026-07-01", "", cust(e.later, "D", "1.00"), acc("400000", "C", "1.00")),
			`"TEXT":"the partner `+e.later+` is blocked"`)
		wantIn(t, e.run("admin", "BLOCK_COMPANY_ACCOUNT", `"1000"`, `"110000"`), `"FAILURES":[]`)
		e.check("anna", posting("1000", "SA", "2026-03-01", "", acc("110000", "D", "1.00"), acc("400000", "C", "1.00")),
			`"TEXT":"the account 110000 is blocked in the company 1000"`)

		// Decimals of the currency.
		e.send("POST", finConfig+"Currencies", "admin", `{"CURRENCY":"JPY","DECIMALS":0}`, 201)
		e.ifMatch("PATCH", finConfig+"Companies('2000')", "admin", `{"CURRENCY":"JPY"}`, 204)
		e.check("anna", posting("2000", "SA", "2026-03-01", "", acc("100000", "D", "100.50"), acc("400000", "C", "100.50")),
			`"ITEM_NO":1,"FIELD":"AMOUNT","TEXT":"the currency JPY has 0 decimals; the amount 100.50 has more"`)
		e.check("anna", posting("2000", "SA", "2026-03-01", "", acc("100000", "D", "100.00"), acc("400000", "C", "100.00")))

		// Authorizations: carl posts entries of the type SA in 1000 to 0L.
		e.check("carl", posting("1000", "SA", "2026-03-01", `"LEDGER":"0L"`, ok...))
		e.check("carl", posting("1000", "SA", "2026-03-01", "", ok...), `"FIELD":"LEDGER","TEXT":"you may not post to the ledger 2L"`)
		e.check("carl", posting("1000", "DR", "2026-03-01", `"LEDGER":"0L"`, ok...), `"TEXT":"you may not post journal entries of the type DR in the company 1000"`)

		// POST runs the same checks as validations, at the item and field,
		// and writes nothing.
		out := e.run("anna", "POST", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "0.00"), acc("400000", "C", "0.00")))
		wantIn(t, out, `"ENTITY":"/TRFIN/JOURNAL_ENTRY_ITEMS","OPERATION":"create"`, `"REASON":"VALIDATION"`, `"TEXT":"the amount is greater than zero, not 0.00"`, `"FIELD":"AMOUNT"`)
		out = e.run("anna", "POST", posting("1000", "SA", "2025-03-01", "", ok...))
		wantIn(t, out, `"ENTITY":"/TRFIN/JOURNAL_ENTRIES"`, `"TEXT":"the period 3 of 2025 is not open for postings in the company 1000"`, `"FIELD":"POSTING_DATE"`)
		if got := e.run("", "CONSISTENCY"); got != "" {
			t.Errorf("%s", got)
		}
		e.send("GET", finEntries+"JournalEntries", "anna", "", 200, `"value":[]`)
	})
}

func TestLedgerPostingNumbers(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		sa := func(company, account string) string {
			return posting(company, "SA", "2026-03-01", "", acc(account, "D", "10.00"), acc("400000", "C", "10.00"))
		}
		for _, want := range []struct{ company, account, no string }{
			{"1000", "100000", "0000000001"}, {"1000", "100000", "0000000002"}, {"2000", "100000", "0000000001"},
		} {
			if no := e.post("anna", sa(want.company, want.account)); no != want.no {
				t.Errorf("number in %s: %s, want %s", want.company, no, want.no)
			}
		}
		// The interval of the document type.
		if no := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "5.00"), acc("400000", "C", "5.00"))); no != "0001000000" {
			t.Errorf("number of DR: %s", no)
		}
		// A failed check takes no number.
		wantIn(t, e.run("anna", "POST", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "1.00"))), `"REASON":"VALIDATION"`)
		// A failed save method gives its number back: the totals of 110000
		// overflow.
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"140000"`, `"161000"`), `"FAILURES":[]`)
		e.run("", "FILL_TOTALS", `"0L"`, `"1000"`, `"110000"`, "2026", "3")
		out := e.run("anna", "POST", sa("1000", "110000"))
		wantIn(t, out, `"CREATED":[]`, `"REASON":"BEHAVIOR"`, "/TRFIN/ACCOUNT_TOTALS.DEBIT no longer holds the result")
		e.run("", "CLEAR_TOTALS", `"0L"`, `"1000"`, `"110000"`)
		if got := e.lines("1000", "0000000003"); got != "" {
			t.Errorf("ledger entries of a failed save: %s", got)
		}
		if no := e.post("anna", sa("1000", "110000")); no != "0000000003" {
			t.Errorf("number after a failed save: %s", no)
		}
		// Several entries in one save take numbers in their order.
		out = e.run("anna", "POST_ALL", "["+posting("1000", "SA", "2026-03-01", `"LINK":"a"`, acc("100000", "D", "1.00"), acc("400000", "C", "1.00"))+","+
			posting("1000", "SA", "2026-03-01", `"LINK":"b"`, acc("100000", "D", "2.00"), acc("400000", "C", "2.00"))+"]")
		if _, a := created(t, out, "a"); a != "0000000004" {
			t.Errorf("first of two: %s", a)
		}
		if _, b := created(t, out, "b"); b != "0000000005" {
			t.Errorf("second of two: %s", b)
		}
		e.consistent()
	})
}

func TestLedgerPostingLedgers(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		items := []item{acc("100000", "D", "100.00"), acc("400000", "C", "100.00")}
		// Without ledger: the ledgers of the company, 0L and 2L in 1000.
		no := e.post("anna", posting("1000", "SA", "2026-03-01", "", items...))
		if got, want := e.lines("1000", no), "0L 1 1 100000 - 100.00 ITEM\n0L 2 2 400000 - -100.00 ITEM\n2L 1 1 100000 - 100.00 ITEM\n2L 2 2 400000 - -100.00 ITEM"; got != want {
			t.Errorf("without ledger:\n%s\nwant\n%s", got, want)
		}
		// Only 0L in 2000.
		no = e.post("anna", posting("2000", "SA", "2026-03-01", "", items...))
		if got := e.lines("2000", no); strings.Count(got, "\n") != 1 || strings.Contains(got, "2L") {
			t.Errorf("without ledger in 2000:\n%s", got)
		}
		// One ledger.
		no = e.post("anna", posting("1000", "SA", "2026-03-01", `"LEDGER":"2L"`, items...))
		if got := e.lines("1000", no); strings.Count(got, "\n") != 1 || strings.Contains(got, "0L") {
			t.Errorf("to 2L:\n%s", got)
		}
		// A group.
		no = e.post("anna", posting("1000", "SA", "2026-03-01", `"LEDGER_GROUP":"ALL"`, items...))
		if got := e.lines("1000", no); strings.Count(got, "\n") != 3 {
			t.Errorf("to ALL:\n%s", got)
		}
		// The document type restricted to 2L posts there without ledger.
		no = e.post("anna", posting("1000", "L2", "2026-03-01", "", items...))
		if got := e.lines("1000", no); strings.Count(got, "\n") != 1 || strings.Contains(got, "0L") {
			t.Errorf("type L2:\n%s", got)
		}
		e.send("GET", entryPath("1000", 2026, no), "anna", "", 200, `"LEDGER":"2L"`, `"LEDGER_GROUP":null`, `"PERIOD":3`, `"CURRENCY":"EUR"`, `"KIND":"NORMAL"`, `"STATUS":"POSTED"`)
		// Totals equal the ledger entries.
		e.consistent()
		if got := e.run("", "ACCOUNT_BALANCE", `"0L"`, `"1000"`, `"100000"`); got != "200.00" {
			t.Errorf("100000 in 0L: %s", got)
		}
		if got := e.run("", "ACCOUNT_BALANCE", `"2L"`, `"1000"`, `"400000"`); got != "-400.00" {
			t.Errorf("400000 in 2L: %s", got)
		}
		// Open items come from the account.
		no = e.post("anna", posting("1000", "SA", "2026-03-01", "", acc("110000", "D", "1.00"), acc("400000", "C", "1.00")))
		e.send("GET", entryPath("1000", 2026, no)+"?$expand=JournalEntryItems", "anna", "", 200, `"ITEM_NO":1,"ACCOUNT_KIND":"ACCOUNT","ACCOUNT":"110000"`, `"OPEN_ITEM":true`, `"OPEN_ITEM":false`)

		// Ledgers and the currency of a company with movements stay.
		e.ifMatch("PATCH", finConfig+"Ledgers('2L')", "admin", `{"PRIMARY":true}`, 400, "journal entries are posted; the primary ledger stays")
		e.ifMatch("DELETE", finConfig+"Ledgers('2L')", "admin", "", 400, "the ledger 2L has movements; it stays")
		e.ifMatch("DELETE", finConfig+"CompanyLedgers(COMPANY='1000',LEDGER='2L')", "admin", "", 400, "the ledger 2L has movements in the company 1000; it stays assigned")
		e.ifMatch("PATCH", finConfig+"Companies('1000')", "admin", `{"CURRENCY":"USD"}`, 400, "the company 1000 has movements; its currency stays")
		e.ifMatch("DELETE", finConfig+"DocumentTypes('L2')", "admin", "", 400, "journal entries of the type L2 exist")
		e.ifMatch("DELETE", finConfig+"Ledgers('3L')", "admin", "", 204)
	})
}

func TestLedgerImmutability(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		no := e.post("anna", posting("1000", "SA", "2026-03-01", `"HEADER_TEXT":"first"`, acc("100000", "D", "100.00"), acc("400000", "C", "100.00")))
		wantIn(t, e.run("anna", "SET_TEXTS", `"1000"`, "2026", `"`+no+`"`, `"second"`, "1", `"A-1"`), `"FAILURES":[]`)
		e.send("GET", entryPath("1000", 2026, no)+"?$expand=JournalEntryItems", "anna", "", 200, `"HEADER_TEXT":"second"`, `"ITEM_TEXT":"second","ASSIGNMENT":"A-1"`, `"VERSION":1`)
		wantIn(t, e.run("anna", "ADD_ITEM", `"1000"`, "2026", `"`+no+`"`), "the journal entry "+no+" of 2026 is posted; it gets no further items")
		path := entryPath("1000", 2026, no)
		e.ifMatch("PATCH", path, "anna", `{"HEADER_TEXT":"third","REFERENCE":"R"}`, 400, "REFERENCE")
		e.ifMatch("PATCH", path, "anna", `{"POSTING_DATE":"2026-04-01"}`, 400, "POSTING_DATE")
		e.ifMatch("PATCH", path, "anna", `{"STATUS":"REVERSED"}`, 400, "STATUS")
		e.ifMatch("PATCH", path, "anna", `{"HEADER_TEXT":"third"}`, 204)
		e.ifMatch("DELETE", path, "anna", "", 405)
		item := fmt.Sprintf("%sJournalEntryItems(COMPANY='1000',FISCAL_YEAR=2026,DOCUMENT_NO='%s',ITEM_NO=1)", finEntries, no)
		e.ifMatch("PATCH", item, "anna", `{"AMOUNT":"200.00"}`, 400, "AMOUNT")
		e.ifMatch("PATCH", item, "anna", `{"ACCOUNT":"110000"}`, 400, "ACCOUNT")
		e.ifMatch("PATCH", item, "anna", `{"ITEM_TEXT":"changed"}`, 204)
		e.ifMatch("DELETE", item, "anna", "", 405)
		e.send("POST", path+"/JournalEntryItems", "anna", `{"ITEM_NO":3,"ACCOUNT":"100000","DEBIT_CREDIT":"D","AMOUNT":"1.00"}`, 400, "is posted; it gets no further items")
		e.send("POST", finEntries+"LedgerEntries", "anna", `{}`, 405)
		e.send("POST", finEntries+"JournalEntries", "anna", `{"COMPANY":"1000","FISCAL_YEAR":2026,"DOCUMENT_NO":"0000000099","DOCUMENT_TYPE":"SA","DOCUMENT_DATE":"2026-03-01","POSTING_DATE":"2026-03-01"}`, 400, "READ_ONLY_PROPERTY")
		if got := e.lines("1000", no); strings.Count(got, "\n") != 3 {
			t.Errorf("ledger entries after changes of texts:\n%s", got)
		}
		e.consistent()
	})
}

func TestLedgerReversal(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		no := e.post("anna", posting("1000", "DR", "2026-03-10", `"REFERENCE":"INV-1"`, cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		out := e.run("anna", "REVERSE", `"1000"`, "2026", `"`+no+`"`, "null")
		year, rev := created(t, out, "reversal1")
		if year != 2026 || rev != "0001000001" {
			t.Fatalf("reversal %d %s: %s", year, rev, out)
		}
		// References both ways; the reversal has the reversal type DZ.
		e.send("GET", entryPath("1000", 2026, no), "anna", "", 200, `"STATUS":"REVERSED"`, `"REVERSAL_YEAR":2026,"REVERSAL_NO":"`+rev+`"`, `"REVERSED_YEAR":null`)
		e.send("GET", entryPath("1000", 2026, rev)+"?$expand=JournalEntryItems", "anna", "", 200, `"DOCUMENT_TYPE":"DZ"`, `"POSTING_DATE":"2026-03-10"`, `"REFERENCE":"INV-1"`,
			`"KIND":"REVERSAL","STATUS":"POSTED","REVERSAL_YEAR":null,"REVERSAL_NO":null,"REVERSED_YEAR":2026,"REVERSED_NO":"`+no+`"`,
			`"ACCOUNT_KIND":"CUSTOMER","ACCOUNT":"140000","PARTNER":"0000000001","DEBIT_CREDIT":"C","AMOUNT":100`)
		if got, want := e.lines("1000", rev), "0L 1 1 140000 0000000001 -100.00 ITEM\n0L 2 2 400000 - 100.00 ITEM\n2L 1 1 140000 0000000001 -100.00 ITEM\n2L 2 2 400000 - 100.00 ITEM"; got != want {
			t.Errorf("reversal:\n%s\nwant\n%s", got, want)
		}
		if got := e.run("", "BALANCE", `"0L"`, `"1000"`, `"/TRFIN/CUSTOMER"`, `"0000000001"`); got != "0.00" {
			t.Errorf("balance after the reversal: %s", got)
		}
		// No second reversal, neither later nor in the same execution.
		wantIn(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+no+`"`, "null"), "the journal entry "+no+" of 2026 is reversed already")
		no = e.post("anna", posting("1000", "SA", "2026-03-10", "", acc("100000", "D", "7.00"), acc("400000", "C", "7.00")))
		wantIn(t, e.run("anna", "REVERSE_TWICE", `"1000"`, "2026", `"`+no+`"`), "the journal entry "+no+" of 2026 is reversed already")
		// A reversal on another date posts in its period; the type SA
		// reverses with itself.
		out = e.run("anna", "REVERSE", `"1000"`, "2026", `"`+no+`"`, `"2026-04-15"`)
		_, rev = created(t, out, "reversal1")
		e.send("GET", entryPath("1000", 2026, rev), "anna", "", 200, `"DOCUMENT_TYPE":"SA"`, `"POSTING_DATE":"2026-04-15","PERIOD":4`)
		// The reversal goes to the ledgers of the reversed entry, also when
		// the company has other ledgers by now.
		no = e.post("anna", posting("1000", "SA", "2026-03-10", `"LEDGER":"2L"`, acc("100000", "D", "3.00"), acc("400000", "C", "3.00")))
		_, rev = created(t, e.run("anna", "REVERSE", `"1000"`, "2026", `"`+no+`"`, "null"), "reversal1")
		if got := e.lines("1000", rev); strings.Count(got, "\n") != 1 || strings.Contains(got, "0L") {
			t.Errorf("reversal of an entry of 2L:\n%s", got)
		}
		// Reversing needs CANCEL: carl may post but not reverse entries of
		// other types.
		no = e.post("anna", posting("1000", "DR", "2026-03-10", "", cust("0000000001", "D", "1.00"), acc("400000", "C", "1.00")))
		wantIn(t, e.run("carl", "REVERSE", `"1000"`, "2026", `"`+no+`"`, "null"), `"REASON":"NOT_AUTHORIZED"`)
		e.consistent()
	})
}

func TestLedgerReclassification(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		balance := func(ledger, role, want string) {
			t.Helper()
			if got := e.run("", "BALANCE", `"`+ledger+`"`, `"1000"`, `"`+role+`"`, `"0000000001"`); got != want {
				t.Errorf("balance of %s in %s: %s, want %s", role, ledger, got, want)
			}
		}
		accountBalance := func(ledger, account, want string) {
			t.Helper()
			if got := e.run("", "ACCOUNT_BALANCE", `"`+ledger+`"`, `"1000"`, `"`+account+`"`); got != want {
				t.Errorf("%s in %s: %s, want %s", account, ledger, got, want)
			}
		}
		// A customer: invoice of 100, payment of 150, a credit balance of 50
		// on the alternative account 161000, then an invoice of 80 back.
		inv := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		if got := e.lines("1000", inv); strings.Contains(got, "RECLASSIFICATION") {
			t.Errorf("invoice reclassified:\n%s", got)
		}
		pay := e.post("anna", posting("1000", "DZ", "2026-03-05", "", acc("100000", "D", "150.00"), cust("0000000001", "C", "150.00")))
		if got, want := e.lines("1000", pay), "0L 1 1 100000 - 150.00 ITEM\n0L 2 2 140000 0000000001 -150.00 ITEM\n0L 3 - 140000 0000000001 50.00 RECLASSIFICATION\n0L 4 - 161000 0000000001 -50.00 RECLASSIFICATION\n"+
			"2L 1 1 100000 - 150.00 ITEM\n2L 2 2 140000 0000000001 -150.00 ITEM\n2L 3 - 140000 0000000001 50.00 RECLASSIFICATION\n2L 4 - 161000 0000000001 -50.00 RECLASSIFICATION"; got != want {
			t.Errorf("payment:\n%s\nwant\n%s", got, want)
		}
		// The entry shows its two items only.
		e.send("GET", entryPath("1000", 2026, pay)+"/JournalEntryItems?$count=true", "anna", "", 200, `"@odata.count":2`)
		balance("0L", "/TRFIN/CUSTOMER", "-50.00")
		accountBalance("0L", "140000", "0.00")
		accountBalance("0L", "161000", "-50.00")
		e.consistent()
		// The alternative account stays while it holds a balance.
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"140000"`, `"160000"`), "is on the alternative account 161000; it stays")
		back := e.post("anna", posting("1000", "DR", "2026-03-10", "", cust("0000000001", "D", "80.00"), acc("400000", "C", "80.00")))
		if got := e.lines("1000", back); !strings.Contains(got, "0L 3 - 140000 0000000001 -50.00 RECLASSIFICATION\n0L 4 - 161000 0000000001 50.00 RECLASSIFICATION") {
			t.Errorf("back to a debit balance:\n%s", got)
		}
		balance("0L", "/TRFIN/CUSTOMER", "30.00")
		accountBalance("0L", "140000", "30.00")
		accountBalance("0L", "161000", "0.00")

		// A supplier: invoice of 200, payment of 250, a debit balance of 50
		// on the alternative account 141000.
		e.post("anna", posting("1000", "KR", "2026-03-01", "", acc("400000", "D", "200.00"), supp("0000000001", "C", "200.00")))
		pay = e.post("anna", posting("1000", "KR", "2026-03-05", "", supp("0000000001", "D", "250.00"), acc("100000", "C", "250.00")))
		if got := e.lines("1000", pay); !strings.Contains(got, "0L 3 - 160000 0000000001 -50.00 RECLASSIFICATION\n0L 4 - 141000 0000000001 50.00 RECLASSIFICATION") {
			t.Errorf("supplier payment:\n%s", got)
		}
		balance("0L", "/TRFIN/SUPPLIER", "50.00")
		accountBalance("0L", "160000", "0.00")
		accountBalance("0L", "141000", "50.00")
		e.consistent()

		// Per ledger: a credit of 40 in 2L only gives 2L a credit balance;
		// a credit of 20 to the group ALL reclassifies in 2L only.
		e.post("anna", posting("1000", "DZ", "2026-03-12", `"LEDGER":"2L"`, acc("100000", "D", "40.00"), cust("0000000001", "C", "40.00")))
		balance("0L", "/TRFIN/CUSTOMER", "30.00")
		balance("2L", "/TRFIN/CUSTOMER", "-10.00")
		group := e.post("anna", posting("1000", "AL", "2026-03-15", "", acc("100000", "D", "20.00"), cust("0000000001", "C", "20.00")))
		got := e.lines("1000", group)
		if strings.Count(got, "\n") != 5 || strings.Contains(got, "0L 3") || !strings.Contains(got, "2L 3 - 140000 0000000001 20.00 RECLASSIFICATION\n2L 4 - 161000 0000000001 -20.00 RECLASSIFICATION") {
			t.Errorf("group:\n%s", got)
		}
		accountBalance("2L", "161000", "-30.00")
		accountBalance("0L", "161000", "0.00")
		e.consistent()
		// The reversal cancels the reclassification.
		out := e.run("anna", "REVERSE", `"1000"`, "2026", `"`+group+`"`, "null")
		_, rev := created(t, out, "reversal1")
		if got := e.lines("1000", rev); !strings.Contains(got, "2L 3 - 140000 0000000001 -20.00 RECLASSIFICATION\n2L 4 - 161000 0000000001 20.00 RECLASSIFICATION") {
			t.Errorf("reversal of the group:\n%s", got)
		}
		balance("2L", "/TRFIN/CUSTOMER", "-10.00")
		accountBalance("2L", "161000", "-10.00")
		e.consistent()
	})
}

func TestLedgerParallelPostings(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		if !strings.HasSuffix(t.Name(), "postgres") {
			t.Skip("SQLite has one writer")
		}
		e := postingKernel(t, k)
		var wg sync.WaitGroup
		errs := make(chan string, 8)
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p := posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00"))
				if i%2 == 1 {
					p = posting("1000", "DZ", "2026-03-01", "", acc("100000", "D", "130.00"), cust("0000000001", "C", "130.00"))
				}
				out, err := k.Run(context.Background(), kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}, "/TRFINTEST/API", "POST", []json.RawMessage{json.RawMessage(p)})
				if err != nil || !strings.Contains(string(out), `\"FAILURES\":[]`) && !strings.Contains(string(out), `"FAILURES":[]`) {
					errs <- fmt.Sprintf("%v %s", err, out)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if got := e.run("", "BALANCE", `"0L"`, `"1000"`, `"/TRFIN/CUSTOMER"`, `"0000000001"`); got != "-120.00" {
			t.Errorf("balance after parallel postings: %s", got)
		}
		e.consistent()
	})
}

func TestLedgerPostingAuthorizations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		sa := e.post("carl", posting("1000", "SA", "2026-03-01", `"LEDGER":"0L"`, acc("100000", "D", "1.00"), acc("400000", "C", "1.00")))
		wantIn(t, e.run("carl", "POST", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "1.00"), acc("400000", "C", "1.00"))), "you may not post to the ledger 2L")
		wantIn(t, e.run("carl", "POST", posting("1000", "DR", "2026-03-01", `"LEDGER":"0L"`, acc("100000", "D", "1.00"), acc("400000", "C", "1.00"))), "you may not post journal entries of the type DR in the company 1000")
		wantIn(t, e.run("carl", "POST", posting("2000", "SA", "2026-03-01", `"LEDGER":"0L"`, acc("100000", "D", "1.00"), acc("400000", "C", "1.00"))), "you may not post journal entries of the type SA in the company 2000")
		dr := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "1.00"), acc("400000", "C", "1.00")))
		other := e.post("anna", posting("2000", "SA", "2026-03-01", "", acc("100000", "D", "1.00"), acc("400000", "C", "1.00")))
		two := e.post("anna", posting("1000", "SA", "2026-03-01", `"LEDGER":"2L"`, acc("100000", "D", "1.00"), acc("400000", "C", "1.00")))

		// carl reads entries of the type SA in 1000 and ledger entries of
		// 0L; dora reads all.
		a := e.send("GET", finEntries+"JournalEntries", "carl", "", 200, `"DOCUMENT_NO":"`+sa+`"`, `"DOCUMENT_NO":"`+two+`"`)
		if strings.Contains(a.body, dr) || strings.Contains(a.body, `"COMPANY":"2000"`) {
			t.Errorf("carl reads other entries: %s", a.body)
		}
		a = e.send("GET", finEntries+"LedgerEntries", "carl", "", 200, `"DOCUMENT_NO":"`+sa+`"`)
		if strings.Contains(a.body, `"LEDGER":"2L"`) || strings.Contains(a.body, dr) || strings.Contains(a.body, `"COMPANY":"2000"`) {
			t.Errorf("carl reads other ledger entries: %s", a.body)
		}
		e.send("GET", entryPath("1000", 2026, two)+"/LedgerEntries", "carl", "", 200, `"value":[]`)
		e.send("GET", entryPath("1000", 2026, dr), "carl", "", 404)
		a = e.send("GET", finEntries+"LedgerEntries?$count=true", "dora", "", 200, `"DOCUMENT_NO":"`+other+`"`, `"LEDGER":"2L"`)
		wantIn(t, a.body, `"@odata.count":10`)

		// Over OData: creating needs CREATE for company and document type,
		// changing texts UPDATE, reversing CANCEL.
		body := `{"COMPANY":"2000","DOCUMENT_TYPE":"SA","DOCUMENT_DATE":"2026-03-01","POSTING_DATE":"2026-03-01","LEDGER":"0L","JournalEntryItems":[` +
			`{"ITEM_NO":1,"ACCOUNT":"100000","DEBIT_CREDIT":"D","AMOUNT":"1.00"},{"ITEM_NO":2,"ACCOUNT":"400000","DEBIT_CREDIT":"C","AMOUNT":"1.00"}]}`
		e.send("POST", finEntries+"JournalEntries", "carl", body, 403)
		e.send("POST", finEntries+"JournalEntries", "dora", strings.Replace(body, "2000", "1000", 1), 403)
		e.ifMatch("PATCH", entryPath("1000", 2026, sa), "dora", `{"HEADER_TEXT":"x"}`, 403)
		e.ifMatch("PATCH", entryPath("1000", 2026, sa), "carl", `{"HEADER_TEXT":"x"}`, 204)
		e.send("POST", entryPath("1000", 2026, sa)+"/TRFIN.REVERSE", "dora", `{}`, 403)
		e.consistent()
	})
}

func TestLedgerPostingOData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := postingKernel(t, k)
		e.send("GET", finEntries+"$metadata", "dora", "", 200, "TRFIN_JOURNAL_ENTRIES", "TRFIN_LEDGER_ENTRIES", "LedgerEntries", `Name="POST"`, `Name="CHECK"`, `Name="REVERSE"`)
		// Creating an entry with its items gives the final key.
		body := `{"COMPANY":"1000","DOCUMENT_TYPE":"DR","DOCUMENT_DATE":"2026-03-01","POSTING_DATE":"2026-03-02","HEADER_TEXT":"web","JournalEntryItems":[` +
			`{"ITEM_NO":1,"ACCOUNT_KIND":"CUSTOMER","PARTNER":"0000000001","DEBIT_CREDIT":"D","AMOUNT":"119.00"},{"ITEM_NO":2,"ACCOUNT":"400000","DEBIT_CREDIT":"C","AMOUNT":"119.00"}]}`
		a := e.send("POST", finEntries+"JournalEntries", "anna", body, 201, `"DOCUMENT_NO":"0001000000"`, `"FISCAL_YEAR":2026`, `"PERIOD":3`)
		if !strings.HasSuffix(a.header.Get("Location"), "JournalEntries(COMPANY='1000',FISCAL_YEAR=2026,DOCUMENT_NO='0001000000')") {
			t.Errorf("location: %s", a.header.Get("Location"))
		}
		e.send("GET", entryPath("1000", 2026, "0001000000")+"/JournalEntryItems", "anna", "", 200, `"ACCOUNT":"140000"`, `"DUE_DATE":"2026-03-02"`, `"OPEN_ITEM":true`)
		e.send("GET", entryPath("1000", 2026, "0001000000")+"/LedgerEntries?$filter=LEDGER%20eq%20'0L'", "anna", "", 200, `"LINE_KIND":"ITEM"`, `"PARTNER_ROLE":"/TRFIN/CUSTOMER"`)
		// Failed checks name the item and field.
		bad := strings.Replace(body, `"AMOUNT":"119.00"},{`, `"AMOUNT":"0.00"},{`, 1)
		e.send("POST", finEntries+"JournalEntries", "anna", bad, 400, `"target":"JournalEntryItems(1)/AMOUNT"`, "the amount is greater than zero, not 0.00", "differ by")
		// The action POST takes the entry with its items and answers with
		// the final key.
		p := posting("1000", "SA", "2026-03-03", `"LINK":"web"`, acc("100000", "D", "5.00"), acc("400000", "C", "5.00"))
		e.send("POST", finEntries+"POST", "anna", p, 200, `"LINK":"web","COMPANY":"1000","FISCAL_YEAR":2026,"DOCUMENT_NO":"0000000001"`)
		e.send("POST", finEntries+"POST", "anna", posting("1000", "SA", "2026-03-03", "", acc("100000", "D", "5.00")), 400, "a journal entry has at least two items, not 1")
		// CHECK writes nothing and answers with the messages.
		e.send("POST", finEntries+"CHECK", "anna", posting("1000", "SA", "2026-03-03", "", acc("100000", "D", "5.00"), acc("130000", "C", "5.00")), 200,
			`"FISCAL_YEAR":2026,"PERIOD":3`, `{"ITEM_NO":2,"FIELD":"ACCOUNT","TEXT":"the account 130000 is blocked in the chart TRAC"}`)
		// REVERSE answers with the reversed entry.
		e.send("POST", entryPath("1000", 2026, "0000000001")+"/TRFIN.REVERSE", "anna", `{"POSTING_DATE":"2026-03-04"}`, 200, `"STATUS":"REVERSED"`, `"REVERSAL_NO":"0000000002"`)
		e.send("GET", finEntries+"JournalEntries?$filter=KIND%20eq%20'REVERSAL'", "anna", "", 200, `"REVERSED_NO":"0000000001"`, `"POSTING_DATE":"2026-03-04"`)
		e.consistent()
	})
}
