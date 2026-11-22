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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// reportsKernel is postingKernel with the year 2025 open in 1000 with the
// intervals 01 and 02, the node /trfin active and the user rita, who may
// run the reports of 1000 in the ledger 0L but not export them.
func reportsKernel(t *testing.T, k *kernel.Kernel) ledgerEnv {
	t.Helper()
	e := postingKernel(t, k)
	ctx := context.Background()
	for _, iv := range []struct{ id, from, to string }{{"01", "1", "999999"}, {"02", "1000000", "1999999"}} {
		if err := k.AddNumberInterval(ctx, "ACME", kernel.NumberInterval{Object: "/TRFIN/JOURNAL_ENTRY", ID: iv.id, Subobject: model.Some("1000"),
			FiscalYear: model.Some[int64](2025), From: iv.from, To: iv.to}, kernel.NumberRangeOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	e.send("POST", finConfig+"PostingPeriods", "admin", `{"COMPANY":"1000","FISCAL_YEAR":2025,"FROM_PERIOD":1,"TO_PERIOD":12}`, 201)
	if _, err := k.Users().Create(ctx, "rita", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"/TRFINTEST/REPORT_CLERK", "/TRFIN/ODATA_USER"} {
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "rita", Role: r, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.ActivateHTTPService(ctx, "/trfin"); err != nil {
		t.Fatal(err)
	}
	return e
}

// call runs a public static method of a class of TRFIN in ACME as user
// with the arguments as JSON.
func (e ledgerEnv) call(user, class, method string, args ...string) (string, error) {
	e.t.Helper()
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := e.k.Run(context.Background(), opts, class, method, raw)
	return string(out), err
}

// report runs a report of /TRFIN/REPORTS as user with a selection and
// decodes its result into out.
func (e ledgerEnv) report(user, method, selection string, out any) {
	e.t.Helper()
	got, err := e.call(user, "/TRFIN/REPORTS", method, selection)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, selection, err)
	}
	if err := json.Unmarshal([]byte(got), out); err != nil {
		e.t.Fatalf("%s: %v: %s", method, err, got)
	}
}

type balanceRow struct {
	ACCOUNT, PARTNER_ROLE, PARTNER                                                  string
	CARRY_FORWARD, PRIOR_BALANCE, DEBIT, CREDIT, PERIOD_BALANCE, CUMULATIVE_BALANCE string
}

func (r balanceRow) String() string {
	id := r.ACCOUNT
	if r.PARTNER != "" {
		id = r.PARTNER_ROLE + " " + r.PARTNER
	}
	return fmt.Sprintf("%s %s %s %s %s %s %s", id, r.CARRY_FORWARD, r.PRIOR_BALANCE, r.DEBIT, r.CREDIT, r.PERIOD_BALANCE, r.CUMULATIVE_BALANCE)
}

type balanceReport struct {
	LEDGER, CHART    string
	ROWS, UNASSIGNED []balanceRow
	TOTALS           balanceRow
}

func lines[T fmt.Stringer](rows []T) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.String())
	}
	return strings.Join(out, "\n")
}

type openItemRow struct {
	DOCUMENT_NO, DUE_DATE, AMOUNT string
	ITEM_NO, DAYS_OVERDUE         int
	CLEARING_DATE                 *string
}

func (r openItemRow) String() string {
	cleared := "-"
	if r.CLEARING_DATE != nil {
		cleared = *r.CLEARING_DATE
	}
	return fmt.Sprintf("%s/%d %s %d %s %s", r.DOCUMENT_NO, r.ITEM_NO, r.DUE_DATE, r.DAYS_OVERDUE, r.AMOUNT, cleared)
}

type openItemReport struct {
	ROWS                                                                                 []openItemRow
	TOTAL, NOT_DUE, OVERDUE, OVERDUE_1_30, OVERDUE_31_60, OVERDUE_61_90, OVERDUE_OVER_90 string
}

type statementRow struct {
	LEVEL                 int
	NODE, SIDE, NODE_KIND string
	ACCOUNT               *string
	AMOUNT                string
}

func (r statementRow) String() string {
	name := r.NODE
	if r.ACCOUNT != nil {
		name = "  " + *r.ACCOUNT
	}
	return fmt.Sprintf("%s%s %s", strings.Repeat("  ", r.LEVEL-1), name, r.AMOUNT)
}

type accountBalance struct{ CHART, ACCOUNT, BALANCE string }

func (a accountBalance) String() string { return a.CHART + " " + a.ACCOUNT + " " + a.BALANCE }

type statementReport struct {
	ROWS                                                []statementRow
	TOTAL_ASSETS, TOTAL_LIABILITIES, RESULT, DIFFERENCE string
	BALANCED                                            bool
	UNASSIGNED                                          []accountBalance
	PROBLEMS                                            []struct{ TEXT string }
}

func TestLedgerReportBalances(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := reportsKernel(t, k)
		e.post("anna", posting("1000", "SA", "2025-05-01", "", acc("100000", "D", "1000.00"), acc("400000", "C", "1000.00")))
		e.post("anna", posting("1000", "DR", "2025-06-01", "", cust("0000000001", "D", "500.00"), acc("400000", "C", "500.00")))
		feb := e.post("anna", posting("1000", "SA", "2026-02-10", "", acc("100000", "D", "200.00"), acc("400000", "C", "200.00")))
		e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "300.00"), acc("400000", "C", "300.00")))
		apr := e.post("anna", posting("1000", "SA", "2026-04-10", "", acc("400000", "D", "50.00"), acc("100000", "C", "50.00")))

		// The carry-forward of 2025 for balance sheet accounts only, the
		// periods 1 and 2 as prior periods, 3 to 12 as range.
		var r balanceReport
		e.report("anna", "BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_PERIOD":3}`, &r)
		want := "100000 1000.00 200.00 0.00 50.00 -50.00 1150.00\n" +
			"140000 500.00 0.00 300.00 0.00 300.00 800.00\n" +
			"400000 0.00 -200.00 50.00 300.00 -250.00 -450.00"
		if got := lines(r.ROWS); got != want || r.LEDGER != "0L" || r.CHART != "TRAC" {
			t.Errorf("balances %s %s:\n%s\nwant\n%s", r.LEDGER, r.CHART, got, want)
		}
		if got := r.TOTALS.String(); got != " 1500.00 0.00 350.00 350.00 0.00 1500.00" {
			t.Errorf("totals: %q", got)
		}
		// 2025 alone: no carry-forward, all periods.
		e.report("anna", "BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2025,"LEDGER":"2L"}`, &r)
		want = "100000 0.00 0.00 1000.00 0.00 1000.00 1000.00\n" +
			"140000 0.00 0.00 500.00 0.00 500.00 500.00\n" +
			"400000 0.00 0.00 0.00 1500.00 -1500.00 -1500.00"
		if got := lines(r.ROWS); got != want || r.LEDGER != "2L" {
			t.Errorf("balances of 2025 in %s:\n%s\nwant\n%s", r.LEDGER, got, want)
		}
		// A range of accounts.
		e.report("anna", "BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_ACCOUNT":"140000","TO_ACCOUNT":"149999"}`, &r)
		if got := lines(r.ROWS); got != "140000 500.00 0.00 300.00 0.00 300.00 800.00" {
			t.Errorf("range of accounts:\n%s", got)
		}
		// The country chart: the country accounts of 100000 and 140000;
		// 400000 has none.
		e.report("anna", "BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_PERIOD":3,"CHART_KIND":"COUNTRY"}`, &r)
		want = "1000 1000.00 200.00 0.00 50.00 -50.00 1150.00\n1400 500.00 0.00 300.00 0.00 300.00 800.00"
		if got := lines(r.ROWS); got != want || r.CHART != "TRDE" {
			t.Errorf("balances of the country chart %s:\n%s\nwant\n%s", r.CHART, got, want)
		}
		if got := lines(r.UNASSIGNED); got != "400000 0.00 -200.00 50.00 300.00 -250.00 -450.00" {
			t.Errorf("accounts without country account:\n%s", got)
		}
		_, err := e.call("anna", "/TRFIN/REPORTS", "BALANCES", `{"COMPANY":"2000","FISCAL_YEAR":2026,"CHART_KIND":"COUNTRY"}`)
		wantIn(t, fmt.Sprint(err), "the company 2000 has no country chart")
		_, err = e.call("anna", "/TRFIN/REPORTS", "BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_PERIOD":4,"TO_PERIOD":3}`)
		wantIn(t, fmt.Sprint(err), "the periods 4 to 3 are no range of the periods 1 to 12")
		_, err = e.call("anna", "/TRFIN/REPORTS", "BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"LEDGER":"9L"}`)
		wantIn(t, fmt.Sprint(err), "the ledger 9L does not exist")

		// Partners carry their balance forward.
		var p balanceReport
		e.report("anna", "PARTNER_BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_PERIOD":3}`, &p)
		if got := lines(p.ROWS); got != "/TRFIN/CUSTOMER 0000000001 500.00 0.00 300.00 0.00 300.00 800.00" {
			t.Errorf("balances of partners:\n%s", got)
		}
		e.report("anna", "PARTNER_BALANCES", `{"COMPANY":"1000","FISCAL_YEAR":2026,"PARTNER_ROLE":"/TRFIN/SUPPLIER"}`, &p)
		if len(p.ROWS) != 0 {
			t.Errorf("suppliers: %v", p.ROWS)
		}

		// Line items of an account with the keys of their entries, also
		// through its country account.
		var li struct {
			ROWS []struct {
				DOCUMENT_NO, ACCOUNT, AMOUNT string
				FISCAL_YEAR, PERIOD          int
			}
			TOTAL string
		}
		e.report("anna", "LINE_ITEMS", `{"COMPANY":"1000","FISCAL_YEAR":2026,"ACCOUNT":"100000"}`, &li)
		if len(li.ROWS) != 2 || li.ROWS[0].DOCUMENT_NO != feb || li.ROWS[1].DOCUMENT_NO != apr || li.ROWS[1].AMOUNT != "-50.00" || li.TOTAL != "150.00" {
			t.Errorf("line items: %+v", li)
		}
		e.report("anna", "LINE_ITEMS", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_PERIOD":3,"CHART_KIND":"COUNTRY","ACCOUNT":"1000"}`, &li)
		if len(li.ROWS) != 1 || li.ROWS[0].DOCUMENT_NO != apr || li.ROWS[0].PERIOD != 4 || li.ROWS[0].ACCOUNT != "100000" {
			t.Errorf("line items of the country account: %+v", li)
		}
	})
}

func TestLedgerReportOpenItemsAndBalanceSheet(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := reportsKernel(t, k)
		old := e.post("anna", posting("1000", "DR", "2025-06-01", "", cust("0000000001", "D", "500.00"), acc("400000", "C", "500.00")))
		e.post("anna", posting("1000", "SA", "2025-05-01", "", acc("100000", "D", "1000.00"), acc("400000", "C", "1000.00")))
		inv := e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "300.00"), acc("400000", "C", "300.00")))
		later := posting("1000", "DR", "2026-04-01", "", cust("0000000001", "D", "70.00"), acc("400000", "C", "70.00"))
		later = strings.Replace(later, `"PARTNER":"0000000001"}`, `"PARTNER":"0000000001","DUE_DATE":"2026-05-01"}`, 1)
		due := e.post("anna", later)
		e.post("anna", posting("1000", "SA", "2026-04-10", "", acc("400000", "D", "50.00"), acc("100000", "C", "50.00")))
		e.post("anna", posting("1000", "SA", "2026-04-12", "", acc("110000", "D", "30.00"), acc("100000", "C", "30.00")))
		e.clear("anna", clearing("1000", "DZ", "2026-04-20", []string{inv + "/1"}, acc("100000", "D", "300.00"), cust("0000000001", "C", "300.00")))

		// Open items at a key date before the clearing, with days overdue
		// and their buckets: 318 days, 45 days, not due for 16 days.
		var o openItemReport
		e.report("anna", "OPEN_ITEMS", `{"COMPANY":"1000","KEY_DATE":"2026-04-15","PARTNER":"0000000001"}`, &o)
		want := old + "/1 2025-06-01 318 500.00 -\n" + inv + "/1 2026-03-01 45 300.00 2026-04-20\n" + due + "/1 2026-05-01 -16 70.00 -"
		if got := lines(o.ROWS); got != want {
			t.Errorf("open items before the clearing:\n%s\nwant\n%s", got, want)
		}
		if got := strings.Join([]string{o.TOTAL, o.NOT_DUE, o.OVERDUE, o.OVERDUE_1_30, o.OVERDUE_31_60, o.OVERDUE_61_90, o.OVERDUE_OVER_90}, " "); got != "870.00 70.00 800.00 0.00 300.00 0.00 500.00" {
			t.Errorf("totals by due date: %s", got)
		}
		// After the clearing the invoice and the payment are not open.
		e.report("anna", "OPEN_ITEMS", `{"COMPANY":"1000","KEY_DATE":"2026-04-30","LEDGER":"2L","PARTNER_ROLE":"/TRFIN/CUSTOMER"}`, &o)
		want = old + "/1 2025-06-01 333 500.00 -\n" + due + "/1 2026-05-01 -1 70.00 -"
		if got := lines(o.ROWS); got != want || o.OVERDUE_OVER_90 != "500.00" || o.NOT_DUE != "70.00" {
			t.Errorf("open items after the clearing:\n%s\nwant\n%s", got, want)
		}
		e.report("anna", "OPEN_ITEMS", `{"COMPANY":"1000","KEY_DATE":"2026-04-30","ACCOUNT":"110000"}`, &o)
		if len(o.ROWS) != 1 || o.ROWS[0].AMOUNT != "30.00" {
			t.Errorf("open items of the account 110000: %v", o.ROWS)
		}

		// The balance sheet at a key date in the middle of a period: the
		// result of both years goes to the node RESULT, 110000 has no node
		// and goes to UNASSIGNED, which has the sort order 0.
		var s statementReport
		e.report("anna", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-04-15"}`, &s)
		want = "UNASSIGNED 30.00\n    110000 30.00\nASSETS 1790.00\n  CASH 920.00\n      100000 920.00\n  RECV 870.00\n      140000 870.00\n" +
			"LIAB 1820.00\n  RESULT 1820.00"
		if got := lines(s.ROWS); got != want {
			t.Errorf("balance sheet:\n%s\nwant\n%s", got, want)
		}
		if !s.BALANCED || s.TOTAL_ASSETS != "1820.00" || s.TOTAL_LIABILITIES != "1820.00" || s.RESULT != "1820.00" || s.DIFFERENCE != "0.00" || len(s.PROBLEMS) != 0 {
			t.Errorf("balance sheet: %+v", s)
		}
		if got := lines(s.UNASSIGNED); got != "TRAC 110000 30.00" {
			t.Errorf("unassigned accounts: %s", got)
		}
		// With the clearing of 2026-04-20.
		e.report("anna", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-04-30","LEDGER":"2L","STRUCTURE":"BS01"}`, &s)
		if !s.BALANCED || s.TOTAL_ASSETS != "1820.00" || !strings.Contains(lines(s.ROWS), "CASH 1220.00\n      100000 1220.00\n  RECV 570.00") {
			t.Errorf("balance sheet after the clearing:\n%s\n%+v", lines(s.ROWS), s)
		}
		_, err := e.call("anna", "/TRFIN/REPORTS", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-04-30","CHART_KIND":"COUNTRY"}`)
		wantIn(t, fmt.Sprint(err), "the chart TRDE has no balance sheet structure")
		_, err = e.call("anna", "/TRFIN/REPORTS", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-04-30","STRUCTURE":"BS99"}`)
		wantIn(t, fmt.Sprint(err), "the chart TRAC has no balance sheet structure BS99")

		// Without the nodes for unassigned accounts and for the result the
		// balance sheet tells what is missing.
		wantIn(t, e.run("admin", "DELETE_NODE", `"BS01"`, `"UNASSIGNED"`), `"FAILURES":[]`)
		e.report("anna", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-04-15"}`, &s)
		if s.BALANCED || s.DIFFERENCE != "-30.00" || lines(s.UNASSIGNED) != "TRAC 110000 30.00" || len(s.PROBLEMS) != 2 ||
			s.PROBLEMS[0].TEXT != "the structure BS01 has no node for unassigned accounts; 1 accounts are missing" ||
			s.PROBLEMS[1].TEXT != "assets 1790.00 and liabilities 1820.00 differ by -30.00" {
			t.Errorf("balance sheet without the node for unassigned accounts: %+v", s)
		}
		wantIn(t, e.run("admin", "DELETE_NODE", `"BS01"`, `"RESULT"`), `"FAILURES":[]`)
		e.report("anna", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-04-15"}`, &s)
		if s.BALANCED || s.TOTAL_LIABILITIES != "0.00" || len(s.PROBLEMS) != 3 ||
			s.PROBLEMS[0].TEXT != "the structure BS01 has no node for the result; the result 1820.00 is missing" {
			t.Errorf("balance sheet without the node for the result: %+v", s)
		}
		e.consistent()
	})
}

func TestLedgerReportReclassificationAndReconciliation(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := reportsKernel(t, k)
		type reclassification struct {
			ROWS []struct{ PARTNER_ROLE, PARTNER, BALANCE, EXPECTED, POSTED, DIFFERENCE string }
		}
		check := func(date, want string) {
			t.Helper()
			var r reclassification
			e.report("anna", "RECLASSIFICATION_CHECK", `{"COMPANY":"1000","KEY_DATE":"`+date+`"}`, &r)
			var got []string
			for _, row := range r.ROWS {
				got = append(got, strings.Join([]string{row.PARTNER_ROLE, row.PARTNER, row.BALANCE, row.EXPECTED, row.POSTED, row.DIFFERENCE}, " "))
			}
			if strings.Join(got, "\n") != want {
				t.Errorf("reclassification at %s:\n%s\nwant\n%s", date, strings.Join(got, "\n"), want)
			}
		}
		// The customer pays more than its invoice: 50 go to the alternative
		// account.
		e.post("anna", posting("1000", "DR", "2026-03-01", "", cust("0000000001", "D", "100.00"), acc("400000", "C", "100.00")))
		e.post("anna", posting("1000", "DZ", "2026-03-10", "", acc("100000", "D", "150.00"), cust("0000000001", "C", "150.00")))
		check("2026-03-07", "")
		check("2026-03-31", "")
		// An invoice posted back to 2026-03-05 moves the 50 back with that
		// date: before the payment the posted reclassification is wrong.
		e.post("anna", posting("1000", "DR", "2026-03-05", "", cust("0000000001", "D", "80.00"), acc("400000", "C", "80.00")))
		check("2026-03-07", "/TRFIN/CUSTOMER 0000000001 180.00 0.00 50.00 50.00")
		check("2026-03-10", "")
		check("2026-03-31", "")
		// The supplier side: a supplier with a debit balance.
		e.post("anna", posting("1000", "KR", "2026-03-02", "", supp("0000000001", "D", "40.00"), acc("400000", "C", "40.00")))
		check("2026-03-07", "/TRFIN/CUSTOMER 0000000001 180.00 0.00 50.00 50.00")
		check("2026-03-31", "")

		// The reconciliation is clean, and finds a manipulated row of totals.
		type result struct {
			CONSISTENT bool
			PROBLEMS   []struct{ TEXT string }
		}
		reconcile := func(user string) result {
			t.Helper()
			out, err := e.call(user, "/TRFIN/LEDGER_RECONCILIATION", "RUN", `"1000"`, "2026")
			if err != nil {
				t.Fatal(err)
			}
			var r result
			if err := json.Unmarshal([]byte(out), &r); err != nil {
				t.Fatal(err)
			}
			return r
		}
		if r := reconcile("anna"); !r.CONSISTENT || len(r.PROBLEMS) != 0 {
			t.Errorf("reconciliation: %+v", r)
		}
		e.send("POST", finEntries+"RECONCILE", "anna", `{"COMPANY":"1000","FISCAL_YEAR":2026}`, 200, `"CONSISTENT":true`, `"PROBLEMS":[]`)
		if got := e.run("", "TAMPER_TOTALS", `"2L"`, `"1000"`, `"100000"`, "2026", "3", `"1.00"`); got != "1" {
			t.Fatalf("tampered rows: %s", got)
		}
		r := reconcile("anna")
		if r.CONSISTENT || len(r.PROBLEMS) != 1 || r.PROBLEMS[0].TEXT != "the totals of the account 100000 in the ledger 2L and period 3 are 151.00 debit and 0.00 credit, the ledger entries 150.00 and 0.00" {
			t.Errorf("reconciliation of manipulated totals: %+v", r)
		}
		e.send("POST", finEntries+"RECONCILE", "anna", `{"COMPANY":"1000","FISCAL_YEAR":2026}`, 200, `"CONSISTENT":false`, "the totals of the account 100000 in the ledger 2L")
		// rita reads only the ledger 0L, which is consistent; carl may not
		// reconcile.
		if r := reconcile("rita"); !r.CONSISTENT {
			t.Errorf("reconciliation of the ledger 0L: %+v", r)
		}
		if _, err := e.call("carl", "/TRFIN/LEDGER_RECONCILIATION", "RUN", `"1000"`, "2026"); !notAuthorized(err) {
			t.Errorf("carl: %v", err)
		}
		e.send("POST", finEntries+"RECONCILE", "carl", `{"COMPANY":"1000","FISCAL_YEAR":2026}`, 400, "you may not reconcile the company 1000")
		e.send("POST", finEntries+"RECONCILE", "anna", `{"COMPANY":"9999","FISCAL_YEAR":2026}`, 400, "the company 9999 does not exist")
		if _, err := e.call("anna", "/TRFINTEST/API", "TAMPER_TOTALS", `"2L"`, `"1000"`, `"100000"`, "2026", "3", `"-1.00"`); err != nil {
			t.Fatal(err)
		}
		e.consistent()
	})
}

func TestLedgerReportService(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := reportsKernel(t, k)
		e.post("anna", posting("1000", "SA", "2026-03-01", "", acc("100000", "D", "100.00"), acc("400000", "C", "100.00")))
		e.post("anna", posting("2000", "SA", "2026-03-01", "", acc("100000", "D", "7.00"), acc("400000", "C", "7.00")))
		const reports = "/trfin/reports/"
		balances := `{"COMPANY":"1000","FISCAL_YEAR":2026}`

		// JSON.
		a := e.send("POST", reports+"balances", "anna", balances, 200, `"COMPANY":"1000"`, `"ACCOUNT":"100000","SHORT_TEXT":"Account 100000","KIND":"BALANCE_SHEET"`, `"CUMULATIVE_BALANCE":"100.00"`)
		if ct := a.header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type: %s", ct)
		}
		for _, c := range []struct{ report, body, want string }{
			{"partner-balances", balances, `"ROWS":[]`},
			{"open-items", `{"COMPANY":"1000","KEY_DATE":"2026-03-31"}`, `"TOTAL":"0.00"`},
			{"balance-sheet", `{"COMPANY":"1000","KEY_DATE":"2026-03-31"}`, `"BALANCED":true`},
			{"line-items", `{"COMPANY":"1000","FISCAL_YEAR":2026,"ACCOUNT":"400000"}`, `"AMOUNT":"-100.00"`},
			{"reclassification-check", `{"COMPANY":"1000","KEY_DATE":"2026-03-31"}`, `"ROWS":[]`},
			{"reconciliation", balances, `"CONSISTENT":true`},
		} {
			e.send("POST", reports+c.report, "anna", c.body, 200, c.want)
		}

		// xlsx with the same rows.
		a = e.send("POST", reports+"balances?format=xlsx", "anna", balances, 200)
		if ct := a.header.Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
			t.Errorf("content type: %s", ct)
		}
		wantIn(t, a.header.Get("Content-Disposition"), `filename="balances-1000.xlsx"`)
		f, err := excelize.OpenReader(bytes.NewReader([]byte(a.body)))
		if err != nil {
			t.Fatalf("workbook: %v", err)
		}
		rows, err := f.GetRows("Balances")
		if err != nil || len(rows) != 3 {
			t.Fatalf("rows: %v %v", rows, err)
		}
		if got := strings.Join(rows[0], "|"); got != "Account|Short text|Kind|Carry-forward|Prior periods|Debit|Credit|Balance of range|Cumulative balance" {
			t.Errorf("heading: %s", got)
		}
		if got := strings.Join(rows[1], "|"); got != "100000|Account 100000|BALANCE_SHEET|0.00|0.00|100.00|0.00|100.00|100.00" {
			t.Errorf("row: %s", got)
		}
		a = e.send("POST", reports+"balance-sheet?format=xlsx", "anna", `{"COMPANY":"1000","KEY_DATE":"2026-03-31"}`, 200)
		if f, err := excelize.OpenReader(bytes.NewReader([]byte(a.body))); err != nil {
			t.Errorf("balance sheet as workbook: %v", err)
		} else if rows, err := f.GetRows("Balance sheet"); err != nil || len(rows) < 2 || rows[0][0] != "Level" {
			t.Errorf("balance sheet rows: %v %v", rows, err)
		}

		// Errors.
		e.send("POST", reports+"nothing", "anna", balances, 404, `"error":"there is no report nothing"`)
		e.send("GET", reports+"balances", "anna", "", 405, "reports are run with POST")
		e.send("POST", reports+"balances?format=csv", "anna", balances, 400, "the format csv is neither json nor xlsx")
		e.send("POST", reports+"balances", "anna", `{"COMPANY":"1000"}`, 400, "the selection does not fit the report balances")
		e.send("POST", reports+"balances", "anna", `{"COMPANY":"9999","FISCAL_YEAR":2026}`, 400, "the company 9999 does not exist")

		// Authorizations by company: rita runs the reports of 1000 but
		// exports none and sees only the ledger 0L; carl runs none.
		e.send("POST", reports+"balances", "rita", balances, 200, `"CUMULATIVE_BALANCE":"100.00"`)
		e.send("POST", reports+"balances", "rita", `{"COMPANY":"2000","FISCAL_YEAR":2026}`, 403)
		e.send("POST", reports+"balances?format=xlsx", "rita", balances, 403)
		e.send("POST", reports+"balances", "rita", `{"COMPANY":"1000","FISCAL_YEAR":2026,"LEDGER":"2L"}`, 200, `"ROWS":[]`)
		e.send("POST", reports+"line-items", "rita", `{"COMPANY":"1000","FISCAL_YEAR":2026,"LEDGER":"2L"}`, 200, `"ROWS":[]`)
		e.send("POST", reports+"line-items", "rita", `{"COMPANY":"1000","FISCAL_YEAR":2026}`, 200, `"DOCUMENT_TYPE":"SA"`)
		e.send("POST", reports+"balances", "carl", balances, 403)
		e.send("POST", reports+"balances", "anna", `{"COMPANY":"2000","FISCAL_YEAR":2026}`, 200, `"CUMULATIVE_BALANCE":"7.00"`)
		if _, err := e.call("rita", "/TRFIN/REPORTS", "OPEN_ITEMS", `{"COMPANY":"2000","KEY_DATE":"2026-03-31"}`); !notAuthorized(err) {
			t.Errorf("open items of 2000 for rita: %v", err)
		}
		if _, err := e.call("", "/TRFIN/REPORTS", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-03-31"}`); !notAuthorized(err) {
			t.Errorf("balance sheet without user: %v", err)
		}
	})
}

func TestLedgerSampleData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		ctx := context.Background()
		if _, err := k.Tenants().Create(ctx, "DEMO", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Users().Create(ctx, "setup", "secret", kernel.UserOptions{Tenant: model.Some("DEMO")}); err != nil {
			t.Fatal(err)
		}
		for _, r := range []string{"/TRFIN/CONFIGURATION", "/TRFIN/MASTER_DATA", "/TRFIN/ACCOUNTANT", "/TRBP/PARTNER_ADMIN"} {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "setup", Role: r, Tenant: "DEMO"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
		run := func(class, method string, args ...string) (string, error) {
			var raw []json.RawMessage
			for _, a := range args {
				raw = append(raw, json.RawMessage(a))
			}
			out, err := k.Run(ctx, kernel.ExecOptions{Tenant: "DEMO", User: model.Some("setup")}, class, method, raw)
			return string(out), err
		}
		out, err := run("/TRFIN/SAMPLE_DATA", "SETUP")
		if err != nil {
			t.Fatal(err)
		}
		wantIn(t, out, "set up the sample data of TRFIN", "and 17 journal entries")
		// The balance sheets of the sample data balance, by both charts and
		// in both ledgers, the reconciliation is clean.
		for _, sel := range []string{
			`{"COMPANY":"1000","KEY_DATE":"2026-12-31"}`,
			`{"COMPANY":"1000","KEY_DATE":"2026-12-31","CHART_KIND":"COUNTRY"}`,
			`{"COMPANY":"1000","KEY_DATE":"2026-12-31","LEDGER":"2L"}`,
			`{"COMPANY":"1000","KEY_DATE":"2025-12-31"}`,
			`{"COMPANY":"2000","KEY_DATE":"2026-12-31"}`,
		} {
			out, err := run("/TRFIN/REPORTS", "BALANCE_SHEET", sel)
			if err != nil {
				t.Fatal(err)
			}
			var s statementReport
			if err := json.Unmarshal([]byte(out), &s); err != nil {
				t.Fatal(err)
			}
			if !s.BALANCED || len(s.PROBLEMS) != 0 || s.TOTAL_ASSETS == "0.00" {
				t.Errorf("balance sheet %s:\n%s\n%+v", sel, lines(s.ROWS), s)
			}
		}
		out, err = run("/TRFIN/REPORTS", "BALANCE_SHEET", `{"COMPANY":"1000","KEY_DATE":"2026-12-31"}`)
		wantIn(t, out, `"TOTAL_ASSETS":"76300.00"`, `"RESULT":"23300.00"`)
		for _, year := range []string{"2025", "2026"} {
			out, err := run("/TRFIN/LEDGER_RECONCILIATION", "RUN", `"1000"`, year)
			if err != nil || !strings.Contains(out, `"CONSISTENT":true`) {
				t.Errorf("reconciliation of %s: %s %v", year, out, err)
			}
		}
		out, err = run("/TRFIN/REPORTS", "OPEN_ITEMS", `{"COMPANY":"1000","KEY_DATE":"2026-12-31"}`)
		if err != nil || strings.Count(out, `"DOCUMENT_NO"`) != 4 {
			t.Errorf("open items of the sample data: %s %v", out, err)
		}
		// A second setup changes nothing.
		_, err = run("/TRFIN/SAMPLE_DATA", "SETUP")
		wantIn(t, fmt.Sprint(err), "the tenant has 1 currencies, 2 charts, 2 ledgers and 2 companies of TRFIN")
		// ACME keeps its own data.
		e.send("GET", finConfig+"Companies('1000')", "admin", "", 200, `"NAME":"Acme"`)
	})
}
