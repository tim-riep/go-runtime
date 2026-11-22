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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

const (
	finConfig = "/rt/odata/trfin/configuration_service/"
	finMaster = "/rt/odata/trfin/master_data_service/"
	bpService = "/rt/odata/trbp/partner_service/"
)

// ledgerEnv is a kernel with TRBP, TRFIN and the test package TRFINTEST
// and the configuration and master data the tests of TRFIN share.
type ledgerEnv struct {
	t  *testing.T
	k  *kernel.Kernel
	c  client
	as func(user string) func(*http.Request)
}

// send sends a request as user and checks the status and that the answer
// contains the texts.
func (e ledgerEnv) send(method, path, user, body string, status int, wants ...string) answer {
	e.t.Helper()
	a := e.c.do(method, path, e.as(user), body)
	if a.status != status {
		e.t.Errorf("%s %s %s: %d, want %d: %s", method, path, body, a.status, status, a.body)
		return a
	}
	wantIn(e.t, a.body, wants...)
	return a
}

// run calls a method of /TRFINTEST/API as user and returns its result.
func (e ledgerEnv) run(user, method string, args ...string) string {
	e.t.Helper()
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := e.k.Run(context.Background(), opts, "/TRFINTEST/API", method, raw)
	if err != nil {
		e.t.Fatalf("%s: %v", method, err)
	}
	var s string
	if json.Unmarshal(out, &s) == nil {
		return s
	}
	return string(out)
}

// ledgerKernel installs TRBP and TRFIN from their sources with the test
// package TRFINTEST, the tenant ACME and the users admin (configuration
// and master data of TRFIN and TRBP), clara (accounts and customers of the
// company 1000), chris (accounts of the chart TRDE) and dora (display),
// activates the services and sets up:
//
//   - the currency EUR, the operational chart TRAC (6 digits) and the
//     country chart TRDE (4 digits, DE)
//   - the ledgers 0L (primary), 2L and 3L and the group ALL of 0L and 2L
//   - the companies 1000 (with TRDE and the ledger 2L) and 2000
//   - the document type SA, the open periods 1 to 12 of 2026 in 1000
//   - the balance sheet structure BS01 of TRAC
//   - accounts of TRAC and TRDE and of the companies, with the
//     reconciliation accounts 140000, 142000 and 150000 for customers
//     and 160000 and 161000 for suppliers in 1000
//   - the partners 0000000001 (customer in 1000, supplier) and 0000000002
//     (without roles of TRFIN)
func ledgerKernel(t *testing.T, k *kernel.Kernel) ledgerEnv {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	root := testutil.ModuleRoot(t)
	pkgs := []*model.Package{
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRBP")),
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRFIN")),
		testutil.LoadDir(t, filepath.Join(root, "testdata", "ledger", "TRFINTEST")),
	}
	if _, err := k.Install(ctx, pkgs, kernel.Options{}); err != nil {
		t.Fatalf("%v\n%s", err, objectErrors(err))
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	if err := k.AddNumberInterval(ctx, "ACME", kernel.NumberInterval{Object: "/TRBP/PARTNER", ID: "01", From: "1", To: "4999999999"}, kernel.NumberRangeOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
	roles := map[string][]string{
		"admin": {"/TRFIN/CONFIGURATION", "/TRFIN/MASTER_DATA", "/TRFIN/ODATA_USER", "/TRBP/PARTNER_ADMIN", "/TRBP/ODATA_USER"},
		"clara": {"/TRFINTEST/CLERK", "/TRFIN/ODATA_USER"},
		"chris": {"/TRFINTEST/CHART_CLERK", "/TRFIN/ODATA_USER"},
		"dora":  {"/TRFIN/DISPLAY", "/TRFIN/ODATA_USER"},
	}
	for user, rs := range roles {
		if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: r, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range []string{"/TRFIN/CONFIGURATION_SERVICE", "/TRFIN/MASTER_DATA_SERVICE", "/TRBP/PARTNER_SERVICE"} {
		if err := k.ActivateService(ctx, s, "test"); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(k.HTTPHandler())
	t.Cleanup(srv.Close)
	e := ledgerEnv{t: t, k: k, c: client{t, srv.URL}, as: func(user string) func(*http.Request) {
		return func(r *http.Request) {
			r.SetBasicAuth(user, "secret")
			r.Header.Set("X-Tenant", "ACME")
			r.Header.Set("Content-Type", "application/json")
		}
	}}
	creates := []struct{ path, body string }{
		{finConfig + "Currencies", `{"CURRENCY":"EUR","DECIMALS":2,"CurrencyTexts":[{"LANGUAGE":"en","LABEL":"Euro"}]}`},
		{finConfig + "Charts", `{"CHART":"TRAC","KIND":"OPERATIONAL","ACCOUNT_LENGTH":6,"ChartTexts":[{"LANGUAGE":"en","LABEL":"Operational"}]}`},
		{finConfig + "Charts", `{"CHART":"TRDE","KIND":"COUNTRY","ACCOUNT_LENGTH":4,"COUNTRY":"DE"}`},
		{finConfig + "Ledgers", `{"LEDGER":"0L","PRIMARY":true,"LedgerTexts":[{"LANGUAGE":"en","LABEL":"Primary"}]}`},
		{finConfig + "Ledgers", `{"LEDGER":"2L","PRIMARY":false}`},
		{finConfig + "Ledgers", `{"LEDGER":"3L"}`},
		{finConfig + "Companies", `{"COMPANY":"1000","NAME":"Acme","COUNTRY":"DE","CURRENCY":"EUR","CHART":"TRAC","COUNTRY_CHART":"TRDE","CompanyLedgers":[{"LEDGER":"2L"}]}`},
		{finConfig + "Companies", `{"COMPANY":"2000","NAME":"Acme Two","COUNTRY":"DE","CURRENCY":"EUR","CHART":"TRAC"}`},
		{finConfig + "LedgerGroups", `{"LEDGER_GROUP":"ALL","REPRESENTATIVE":"0L","LedgerGroupMembers":[{"LEDGER":"0L"},{"LEDGER":"2L"}]}`},
		{finConfig + "DocumentTypes", `{"DOCUMENT_TYPE":"SA","INTERVAL":"01","ALLOW_ACCOUNTS":true}`},
		{finConfig + "PostingPeriods", `{"COMPANY":"1000","FISCAL_YEAR":2026,"FROM_PERIOD":1,"TO_PERIOD":12}`},
		{finConfig + "StatementStructures", `{"STRUCTURE":"BS01","CHART":"TRAC"}`},
		{finConfig + "StatementNodes", `{"STRUCTURE":"BS01","NODE":"ASSETS","SIDE":"ASSETS","SORT_ORDER":1}`},
		{finConfig + "StatementNodes", `{"STRUCTURE":"BS01","NODE":"CASH","PARENT_NODE":"ASSETS","SIDE":"ASSETS"}`},
		{finConfig + "StatementNodes", `{"STRUCTURE":"BS01","NODE":"RECV","PARENT_NODE":"ASSETS","SIDE":"ASSETS"}`},
		{finConfig + "StatementNodes", `{"STRUCTURE":"BS01","NODE":"LIAB","SIDE":"LIABILITIES","SORT_ORDER":2}`},
		{finConfig + "StatementNodes", `{"STRUCTURE":"BS01","NODE":"RESULT","PARENT_NODE":"LIAB","SIDE":"LIABILITIES","KIND":"RESULT"}`},
		{finConfig + "StatementNodes", `{"STRUCTURE":"BS01","NODE":"UNASSIGNED","SIDE":"ASSETS","KIND":"UNASSIGNED"}`},
		{finConfig + "StatementNodeRanges", `{"STRUCTURE":"BS01","NODE":"CASH","RANGE_NO":1,"FROM_ACCOUNT":"100000","TO_ACCOUNT":"109999"}`},
		{finConfig + "StatementNodeRanges", `{"STRUCTURE":"BS01","NODE":"RECV","RANGE_NO":1,"FROM_ACCOUNT":"140000","TO_ACCOUNT":"149999","BALANCE":"DEBIT"}`},
		{finConfig + "StatementNodeRanges", `{"STRUCTURE":"BS01","NODE":"LIAB","RANGE_NO":1,"FROM_ACCOUNT":"140000","TO_ACCOUNT":"169999","BALANCE":"CREDIT"}`},
		{finConfig + "StatementNodeRanges", `{"STRUCTURE":"BS01","NODE":"LIAB","RANGE_NO":2,"FROM_ACCOUNT":"160000","TO_ACCOUNT":"169999","BALANCE":"DEBIT"}`},
	}
	for _, a := range []struct{ chart, account, kind string }{
		{"TRAC", "100000", "BALANCE_SHEET"}, {"TRAC", "140000", "BALANCE_SHEET"}, {"TRAC", "141000", "BALANCE_SHEET"},
		{"TRAC", "142000", "BALANCE_SHEET"}, {"TRAC", "150000", "BALANCE_SHEET"}, {"TRAC", "160000", "BALANCE_SHEET"},
		{"TRAC", "161000", "BALANCE_SHEET"}, {"TRAC", "400000", "PROFIT_LOSS"},
		{"TRDE", "1000", "BALANCE_SHEET"}, {"TRDE", "1400", "BALANCE_SHEET"}, {"TRDE", "1401", "BALANCE_SHEET"},
		{"TRDE", "1600", "BALANCE_SHEET"},
	} {
		creates = append(creates, struct{ path, body string }{finMaster + "Accounts",
			`{"CHART":"` + a.chart + `","ACCOUNT":"` + a.account + `","KIND":"` + a.kind + `","AccountTexts":[{"LANGUAGE":"en","SHORT_TEXT":"Account ` + a.account + `"}]}`})
	}
	for _, body := range []string{
		`{"COMPANY":"1000","ACCOUNT":"100000","COUNTRY_ACCOUNT":"1000"}`,
		`{"COMPANY":"1000","ACCOUNT":"140000","COUNTRY_ACCOUNT":"1400","RECONCILIATION":"CUSTOMERS"}`,
		`{"COMPANY":"1000","ACCOUNT":"142000","COUNTRY_ACCOUNT":"1401","RECONCILIATION":"CUSTOMERS"}`,
		`{"COMPANY":"1000","ACCOUNT":"150000","RECONCILIATION":"CUSTOMERS"}`,
		`{"COMPANY":"1000","ACCOUNT":"141000","RECONCILIATION":"CUSTOMERS"}`,
		`{"COMPANY":"1000","ACCOUNT":"160000","COUNTRY_ACCOUNT":"1600","RECONCILIATION":"SUPPLIERS"}`,
		`{"COMPANY":"1000","ACCOUNT":"161000","RECONCILIATION":"SUPPLIERS"}`,
		`{"COMPANY":"1000","ACCOUNT":"400000"}`,
		`{"COMPANY":"2000","ACCOUNT":"100000"}`,
	} {
		creates = append(creates, struct{ path, body string }{finMaster + "CompanyAccounts", body})
	}
	creates = append(creates,
		struct{ path, body string }{bpService + "Groupings", `{"GROUPING":"0001","INTERVAL":"01","EXTERNAL":false}`},
		struct{ path, body string }{bpService + "Partners", `{"GROUPING":"0001","CATEGORY":"ORGANIZATION","NAME1":"Customer","PartnerRoles":[{"ROLE":"/TRFIN/CUSTOMER"},{"ROLE":"/TRFIN/SUPPLIER"}]}`},
		struct{ path, body string }{bpService + "Partners", `{"GROUPING":"0001","CATEGORY":"ORGANIZATION","NAME1":"Other","PartnerRoles":[{"ROLE":"/TRBP/GENERAL"}]}`},
		struct{ path, body string }{finMaster + "Customers", `{"PARTNER":"0000000001","COMPANY":"1000","RECONCILIATION_ACCOUNT":"140000","ALTERNATIVE_ACCOUNT":"161000"}`},
	)
	for _, c := range creates {
		if a := e.c.do("POST", c.path, e.as("admin"), c.body); a.status != 201 {
			t.Fatalf("%s %s: %d %s", c.path, c.body, a.status, a.body)
		}
	}
	return e
}

func TestLedgerConfiguration(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		e.send("POST", finConfig+"Currencies", "admin", `{"CURRENCY":"BHD","DECIMALS":3}`, 400, "a currency has 0 to 2 decimals, not 3")
		e.send("POST", finConfig+"Charts", "admin", `{"CHART":"TRXX","KIND":"COUNTRY","ACCOUNT_LENGTH":4}`, 400, "the chart of a country needs the country")
		e.send("POST", finConfig+"Charts", "admin", `{"CHART":"TRXX","KIND":"OPERATIONAL","ACCOUNT_LENGTH":4,"COUNTRY":"DE"}`, 400, "an operational chart belongs to no country")
		e.send("POST", finConfig+"Charts", "admin", `{"CHART":"TRXX","KIND":"OPERATIONAL","ACCOUNT_LENGTH":11}`, 400, "account numbers have 1 to 10 characters, not 11")

		// Companies have an operational chart and a country chart of their
		// country; the primary ledger needs no assignment.
		e.send("POST", finConfig+"Companies", "admin", `{"COMPANY":"3000","NAME":"X","COUNTRY":"DE","CURRENCY":"EUR","CHART":"TRDE"}`, 400, "the chart TRDE is no operational chart")
		e.send("POST", finConfig+"Companies", "admin", `{"COMPANY":"3000","NAME":"X","COUNTRY":"DE","CURRENCY":"EUR","CHART":"TRAC","COUNTRY_CHART":"TRAC"}`, 400, "the chart TRAC is no chart of a country")
		e.send("POST", finConfig+"Companies", "admin", `{"COMPANY":"3000","NAME":"X","COUNTRY":"FR","CURRENCY":"EUR","CHART":"TRAC","COUNTRY_CHART":"TRDE"}`, 400, "the chart TRDE belongs to another country than the company")
		e.send("POST", finConfig+"CompanyLedgers", "admin", `{"COMPANY":"2000","LEDGER":"0L"}`, 400, "the primary ledger 0L belongs to every company without assignment")
		e.send("PATCH", finConfig+"Companies('1000')", "admin", `{"COUNTRY_CHART":null}`, 400, "accounts of the company 1000 have country accounts; the country chart stays")

		// Exactly one primary ledger.
		wantIn(t, e.run("admin", "SET_PRIMARY", `"2L"`, "true"), `"REASON":"VALIDATION"`, "exactly one ledger is the primary ledger, not 0L, 2L")
		wantIn(t, e.run("admin", "SET_PRIMARY", `"0L"`, "false"), "exactly one ledger is the primary ledger, not none")
		wantIn(t, e.run("admin", "DELETE_LEDGER", `"3L"`), `"FAILURES":[]`)
		e.send("POST", finConfig+"Ledgers", "admin", `{"LEDGER":"4L","PRIMARY":true}`, 400, "exactly one ledger is the primary ledger, not 0L, 4L")

		// The representative of a group is a member; the members of a group
		// need not be assigned to all companies, postings check it.
		e.send("POST", finConfig+"LedgerGroups", "admin", `{"LEDGER_GROUP":"BAD","REPRESENTATIVE":"2L","LedgerGroupMembers":[{"LEDGER":"0L"}]}`, 400, "the representative 2L is no member of the group BAD")
		wantIn(t, e.run("admin", "REMOVE_MEMBER", `"ALL"`, `"0L"`), "the representative 0L is no member of the group ALL")
		if got := e.run("", "UNASSIGNED_MEMBERS", `"2000"`, `"ALL"`); got != "2L" {
			t.Errorf("unassigned in 2000: %q", got)
		}
		if got := e.run("", "UNASSIGNED_MEMBERS", `"1000"`, `"ALL"`); got != "" {
			t.Errorf("unassigned in 1000: %q", got)
		}
		for _, c := range []struct{ company, ledger, group, want string }{
			{"1000", "null", "null", "0L,2L"}, {"2000", "null", "null", "0L"}, {"2000", "null", `"ALL"`, "0L,2L"}, {"2000", `"2L"`, "null", "2L"},
		} {
			if got := e.run("", "TARGETS", `"`+c.company+`"`, c.ledger, c.group); got != c.want {
				t.Errorf("ledgers of %s %s %s: %q, want %q", c.company, c.ledger, c.group, got, c.want)
			}
		}

		// Document types.
		e.send("POST", finConfig+"DocumentTypes", "admin", `{"DOCUMENT_TYPE":"XX","INTERVAL":"01"}`, 400, "allows no items")
		e.send("POST", finConfig+"DocumentTypes", "admin", `{"DOCUMENT_TYPE":"XX","INTERVAL":"01","ALLOW_ACCOUNTS":true,"LEDGER":"2L","LEDGER_GROUP":"ALL"}`, 400, "not to both")
		e.send("POST", finConfig+"DocumentTypes", "admin", `{"DOCUMENT_TYPE":"XX","INTERVAL":"01","ALLOW_ACCOUNTS":true,"REVERSAL_TYPE":"YY"}`, 400, "the document type YY does not exist")
		e.send("POST", finConfig+"DocumentTypes", "admin", `{"DOCUMENT_TYPE":"AB","INTERVAL":"02","ALLOW_ACCOUNTS":true,"ALLOW_CUSTOMERS":true,"REVERSAL_TYPE":"SA","LEDGER_GROUP":"ALL"}`, 201)
		e.send("DELETE", finConfig+"DocumentTypes('SA')", "admin", "", 400, "the document type AB reverses with SA")

		// Posting periods and the fiscal calendar.
		e.send("POST", finConfig+"PostingPeriods", "admin", `{"COMPANY":"1000","FISCAL_YEAR":2027,"FROM_PERIOD":0,"TO_PERIOD":13}`, 400, "periods run from 1 to 12, not 0", "periods run from 1 to 12, not 13")
		e.send("POST", finConfig+"PostingPeriods", "admin", `{"COMPANY":"1000","FISCAL_YEAR":2027,"FROM_PERIOD":5,"TO_PERIOD":3}`, 400, "the open periods end with 3 before they start with 5")
		e.send("PATCH", finConfig+"PostingPeriods(COMPANY='1000',FISCAL_YEAR=2026)", "admin", `{"FROM_PERIOD":3,"TO_PERIOD":4}`, 204)
		for _, c := range []struct {
			year, period string
			want         string
		}{{"2026", "3", "true"}, {"2026", "4", "true"}, {"2026", "5", "false"}, {"2026", "2", "false"}, {"2027", "3", "false"}} {
			if got := e.run("", "IS_OPEN", `"1000"`, c.year, c.period); got != c.want {
				t.Errorf("period %s/%s open: %s", c.year, c.period, got)
			}
		}
		if got := e.run("", "PERIOD_OF", `"1000"`, `"2026-03-15"`); got != "2026/3" {
			t.Errorf("period of 2026-03-15: %q", got)
		}
		if got := e.run("", "PERIOD_OF", `"2000"`, `"2025-12-31"`); got != "2025/12" {
			t.Errorf("period of 2025-12-31: %q", got)
		}

		// Charts and companies in use stay.
		e.send("DELETE", finConfig+"Charts('TRDE')", "admin", "", 400, "the chart TRDE has accounts", "companies use the chart TRDE")
		e.send("DELETE", finConfig+"Companies('2000')", "admin", "", 400, "the company 2000 has accounts")
	})
}

func TestLedgerStatementStructure(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		ranges := finConfig + "StatementNodeRanges"
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"CASH","RANGE_NO":2,"FROM_ACCOUNT":"105000","TO_ACCOUNT":"110000"}`, 400, "the range overlaps the range 1 of the node CASH for the same balance")
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"RECV","RANGE_NO":2,"FROM_ACCOUNT":"145000","TO_ACCOUNT":"145999","BALANCE":"ANY"}`, 400, "the range overlaps the range 1 of the node RECV")
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"CASH","RANGE_NO":2,"FROM_ACCOUNT":"145000","TO_ACCOUNT":"145999","BALANCE":"DEBIT"}`, 400, "the range overlaps the range 1 of the node RECV")
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"RESULT","RANGE_NO":1,"FROM_ACCOUNT":"400000","TO_ACCOUNT":"499999"}`, 400, "the node RESULT of the kind RESULT takes no ranges of accounts")
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"CASH","RANGE_NO":2,"FROM_ACCOUNT":"11000","TO_ACCOUNT":"119999"}`, 400, "the accounts of the chart TRAC have 6 characters, not 11000")
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"CASH","RANGE_NO":2,"FROM_ACCOUNT":"119999","TO_ACCOUNT":"110000"}`, 400, "the range ends with 110000 before it starts with 119999")
		// The debit and the credit balance of an account go to different
		// nodes without overlap.
		e.send("POST", ranges, "admin", `{"STRUCTURE":"BS01","NODE":"CASH","RANGE_NO":2,"FROM_ACCOUNT":"110000","TO_ACCOUNT":"119999","BALANCE":"DEBIT"}`, 201)

		nodes := finConfig + "StatementNodes"
		e.send("POST", nodes, "admin", `{"STRUCTURE":"BS01","NODE":"RESULT2","SIDE":"LIABILITIES","KIND":"RESULT"}`, 400, "the structure BS01 has the node RESULT of the kind RESULT already")
		e.send("POST", nodes, "admin", `{"STRUCTURE":"BS01","NODE":"BANK","PARENT_NODE":"ASSETS","SIDE":"LIABILITIES"}`, 400, "the node BANK is on the side LIABILITIES, its parent node ASSETS on ASSETS")
		e.send("POST", nodes, "admin", `{"STRUCTURE":"BS01","NODE":"BANK","PARENT_NODE":"NOWHERE","SIDE":"ASSETS"}`, 400, "the parent node NOWHERE does not exist in the structure BS01")
		e.send("PATCH", nodes+"(STRUCTURE='BS01',NODE='ASSETS')", "admin", `{"PARENT_NODE":"CASH"}`, 400, "the node ASSETS is below itself")
		e.send("PATCH", nodes+"(STRUCTURE='BS01',NODE='LIAB')", "admin", `{"KIND":"UNASSIGNED"}`, 400, "of the kind UNASSIGNED already", "the node LIAB of the kind UNASSIGNED takes no ranges of accounts")
		wantIn(t, e.run("admin", "DELETE_NODE", `"BS01"`, `"ASSETS"`), "the parent node ASSETS does not exist in the structure BS01")

		for _, c := range []struct{ account, side, want string }{
			{"100500", "DEBIT", "CASH"}, {"100500", "CREDIT", "CASH"}, {"115000", "DEBIT", "CASH"}, {"115000", "CREDIT", "-"},
			{"140000", "DEBIT", "RECV"}, {"140000", "CREDIT", "LIAB"}, {"400000", "DEBIT", "-"},
		} {
			if got := e.run("", "NODE_OF", `"BS01"`, `"`+c.account+`"`, `"`+c.side+`"`); got != c.want {
				t.Errorf("node of %s %s: %q, want %q", c.account, c.side, got, c.want)
			}
		}
		if got := e.run("", "SAME_NODES", `"1000"`, `"140000"`, `"142000"`); got != "true" {
			t.Errorf("140000 and 142000: %s", got)
		}
		if got := e.run("", "SAME_NODES", `"1000"`, `"140000"`, `"150000"`); got != "false" {
			t.Errorf("140000 and 150000: %s", got)
		}
		if got := e.run("", "SAME_NODES", `"1000"`, `"140000"`, `"141000"`); got != "false" {
			t.Errorf("140000 and 141000 without country account: %s", got)
		}
	})
}

func TestLedgerMasterData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		accounts, companyAccounts, customers := finMaster+"Accounts", finMaster+"CompanyAccounts", finMaster+"Customers"
		e.send("POST", accounts, "admin", `{"CHART":"TRAC","ACCOUNT":"12345","KIND":"BALANCE_SHEET"}`, 400, "the accounts of the chart TRAC have 6 characters, not 12345")
		e.send("POST", accounts, "admin", `{"CHART":"TRDE","ACCOUNT":"100000","KIND":"BALANCE_SHEET"}`, 400, "the accounts of the chart TRDE have 4 characters, not 100000")

		// Accounts of a company: of its operational chart, with a country
		// account of its country chart that no other account has.
		e.send("POST", companyAccounts, "admin", `{"COMPANY":"1000","ACCOUNT":"999999"}`, 400, "the account 999999 does not exist in the chart TRAC of the company 1000")
		wantIn(t, e.run("admin", "SET_RECONCILIATION", `"1000"`, `"400000"`, `"SUPPLIERS"`), `"FIELD":"RECONCILIATION"`, "a reconciliation account is a balance sheet account; 400000 is not")
		e.send("POST", companyAccounts, "admin", `{"COMPANY":"2000","ACCOUNT":"140000","COUNTRY_ACCOUNT":"1400"}`, 400, "the company 2000 has no country chart")
		e.send("POST", companyAccounts, "admin", `{"COMPANY":"2000","ACCOUNT":"140000","RECONCILIATION":"CUSTOMERS","OPEN_ITEMS":true}`, 400, "a reconciliation account has no open items of its own")

		// The country account is unique in a company and exists.
		e.send("POST", accounts, "admin", `{"CHART":"TRAC","ACCOUNT":"170000","KIND":"BALANCE_SHEET"}`, 201)
		e.send("POST", companyAccounts, "admin", `{"COMPANY":"1000","ACCOUNT":"170000","COUNTRY_ACCOUNT":"1000"}`, 400, "the country account 1000 belongs to the account 100000 of the company already")
		e.send("POST", companyAccounts, "admin", `{"COMPANY":"1000","ACCOUNT":"170000","COUNTRY_ACCOUNT":"9999"}`, 400, "the account 9999 does not exist in the country chart TRDE")

		// Customers and suppliers need the role, reconciliation accounts of
		// their kind and alternative accounts of the opposite kind.
		e.send("POST", customers, "admin", `{"PARTNER":"0000000002","COMPANY":"1000","RECONCILIATION_ACCOUNT":"140000","ALTERNATIVE_ACCOUNT":"161000"}`, 400, "the partner 0000000002 has no role /TRFIN/CUSTOMER")
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"160000"`, `"161000"`), `"FIELD":"RECONCILIATION_ACCOUNT"`, "the account 160000 is no reconciliation account for CUSTOMERS")
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"140000"`, `"141000"`), `"FIELD":"ALTERNATIVE_ACCOUNT"`, "the alternative account 141000 of a customer is a reconciliation account for SUPPLIERS")
		e.send("POST", finMaster+"Suppliers", "admin", `{"PARTNER":"0000000001","COMPANY":"1000","RECONCILIATION_ACCOUNT":"160000"}`, 400, "ALTERNATIVE_ACCOUNT")
		e.send("POST", finMaster+"Suppliers", "admin", `{"PARTNER":"0000000001","COMPANY":"1000","RECONCILIATION_ACCOUNT":"160000","ALTERNATIVE_ACCOUNT":"141000"}`, 201)
		e.send("GET", customers, "admin", "", 200, `"ROLE":"/TRFIN/CUSTOMER"`, `"RECONCILIATION_ACCOUNT":"140000"`)

		// A role with company data stays; a used reconciliation account keeps
		// its kind and stays.
		wantIn(t, e.run("admin", "REMOVE_ROLE", `"0000000001"`, `"/TRFIN/CUSTOMER"`), "the partner 0000000001 has data in the companies 1000 in the role /TRFIN/CUSTOMER; remove them first")
		wantIn(t, e.run("admin", "SET_RECONCILIATION", `"1000"`, `"140000"`, `"SUPPLIERS"`), "the partner 0000000001 uses the account 140000; its kind of reconciliation stays")
		wantIn(t, e.run("admin", "DELETE_COMPANY_ACCOUNT", `"1000"`, `"140000"`), "the partner 0000000001 uses the account 140000 in the company 1000")
		wantIn(t, e.run("admin", "SET_ACCOUNT_KIND", `"TRAC"`, `"140000"`, `"PROFIT_LOSS"`), "the account 140000 is a reconciliation account in the company 1000 and stays a balance sheet account")
		wantIn(t, e.run("admin", "DELETE_ACCOUNT", `"TRAC"`, `"100000"`), "the account 100000 exists in the company 1000", "the account 100000 exists in the company 2000")
		wantIn(t, e.run("admin", "DELETE_ACCOUNT", `"TRDE"`, `"1000"`), "the account 1000 is a country account in the company 1000")

		// Without movements the reconciliation account changes freely.
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"150000"`, `"161000"`), `"FAILURES":[]`)
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"140000"`, `"161000"`), `"FAILURES":[]`)

		// With movements: deleting is rejected, blocking is not; the
		// reconciliation account changes only to one of the same nodes, the
		// alternative account not while a balance is on it.
		e.run("", "ADD_ACCOUNT_TOTALS", `"0L"`, `"1000"`, `"400000"`)
		e.run("", "ADD_PARTNER_TOTALS", `"2L"`, `"1000"`, `"/TRFIN/CUSTOMER"`, `"0000000001"`, `"-5.00"`)
		wantIn(t, e.run("admin", "DELETE_ACCOUNT", `"TRAC"`, `"400000"`), "the account 400000 exists in the company 1000", "the account 400000 has movements; block it instead")
		wantIn(t, e.run("admin", "DELETE_COMPANY_ACCOUNT", `"1000"`, `"400000"`), "the account 400000 has movements in the company 1000; block it instead")
		wantIn(t, e.run("admin", "BLOCK_COMPANY_ACCOUNT", `"1000"`, `"400000"`), `"FAILURES":[]`)
		wantIn(t, e.run("admin", "SET_ACCOUNT_KIND", `"TRAC"`, `"400000"`, `"BALANCE_SHEET"`), "the account 400000 has movements; its kind stays")
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"150000"`, `"161000"`), "the customer 0000000001 has movements; the new reconciliation account belongs to other nodes of the balance sheet than 140000")
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"142000"`, `"161000"`), `"FAILURES":[]`)
		e.send("POST", accounts, "admin", `{"CHART":"TRAC","ACCOUNT":"162000","KIND":"BALANCE_SHEET"}`, 201)
		e.send("POST", companyAccounts, "admin", `{"COMPANY":"1000","ACCOUNT":"162000","RECONCILIATION":"SUPPLIERS"}`, 201)
		wantIn(t, e.run("admin", "SET_CUSTOMER_ACCOUNTS", `"0000000001"`, `"1000"`, `"142000"`, `"162000"`), "a part of the balance of the customer 0000000001 is on the alternative account 161000; it stays")
		wantIn(t, e.run("admin", "DELETE_CUSTOMER", `"0000000001"`, `"1000"`), "the customer 0000000001 has movements in the company 1000; block it instead")

		// A role with company data stays also without movements; a partner
		// without movements goes with its roles and company data.
		wantIn(t, e.run("admin", "REMOVE_ROLE", `"0000000001"`, `"/TRFIN/SUPPLIER"`), "in the role /TRFIN/SUPPLIER; remove them first")
		wantIn(t, e.run("admin", "DELETE_PARTNER", `"0000000002"`), `"FAILURES":[]`)
	})
}

func TestLedgerAuthorizations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		companyAccounts := finMaster + "CompanyAccounts"

		// clara works with the accounts and customers of 1000 and reads the
		// accounts of TRAC.
		a := e.send("GET", companyAccounts, "clara", "", 200, `"COMPANY":"1000"`)
		if strings.Contains(a.body, `"COMPANY":"2000"`) {
			t.Errorf("clara reads accounts of 2000: %s", a.body)
		}
		e.send("POST", companyAccounts, "clara", `{"COMPANY":"2000","ACCOUNT":"400000"}`, 403)
		e.send("POST", companyAccounts, "clara", `{"COMPANY":"1000","ACCOUNT":"170000"}`, 400, "the account 170000 does not exist")
		e.send("POST", finMaster+"Accounts", "clara", `{"CHART":"TRAC","ACCOUNT":"170000","KIND":"BALANCE_SHEET"}`, 403)
		a = e.send("GET", finMaster+"Accounts", "clara", "", 200, `"ACCOUNT":"100000"`)
		if strings.Contains(a.body, `"CHART":"TRDE"`) {
			t.Errorf("clara reads accounts of TRDE: %s", a.body)
		}
		e.send("POST", finMaster+"Suppliers", "admin", `{"PARTNER":"0000000001","COMPANY":"1000","RECONCILIATION_ACCOUNT":"160000","ALTERNATIVE_ACCOUNT":"141000"}`, 201)
		a = e.send("GET", finMaster+"Customers", "clara", "", 200, `"PARTNER":"0000000001"`)
		if a = e.send("GET", finMaster+"Suppliers", "clara", "", 200); strings.Contains(a.body, `"PARTNER"`) {
			t.Errorf("clara reads suppliers: %s", a.body)
		}
		e.send("POST", finMaster+"Suppliers", "clara", `{"PARTNER":"0000000001","COMPANY":"2000","RECONCILIATION_ACCOUNT":"160000","ALTERNATIVE_ACCOUNT":"141000"}`, 403)

		// chris maintains the accounts of TRDE only.
		e.send("POST", finMaster+"Accounts", "chris", `{"CHART":"TRDE","ACCOUNT":"1700","KIND":"BALANCE_SHEET"}`, 201)
		e.send("POST", finMaster+"Accounts", "chris", `{"CHART":"TRAC","ACCOUNT":"170000","KIND":"BALANCE_SHEET"}`, 403)
		if a = e.send("GET", companyAccounts, "chris", "", 200); strings.Contains(a.body, `"ACCOUNT"`) {
			t.Errorf("chris reads accounts of companies: %s", a.body)
		}

		// Configuration is read with /TRBASE/CONFIGURATION; dora reads all
		// and changes nothing.
		e.send("GET", finConfig+"Companies", "dora", "", 200, `"COMPANY":"1000"`, `"COMPANY":"2000"`)
		e.send("POST", finConfig+"Ledgers", "dora", `{"LEDGER":"5L"}`, 403)
		e.send("GET", finMaster+"Customers", "dora", "", 200, `"PARTNER":"0000000001"`)
		e.send("GET", finConfig+"Companies", "chris", "", 200)
		if a = e.send("GET", finConfig+"Companies", "chris", "", 200); strings.Contains(a.body, `"COMPANY"`) {
			t.Errorf("chris reads companies: %s", a.body)
		}
	})
}

func TestLedgerOData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		e := ledgerKernel(t, k)
		e.send("GET", finConfig+"$metadata", "dora", "", 200, "TRFIN_COMPANIES", "TRFIN_STATEMENT_NODE_RANGES", "PostingPeriods")
		e.send("GET", finMaster+"$metadata", "dora", "", 200, "TRFIN_ACCOUNTS", "TRFIN_CUSTOMER_COMPANIES", "Suppliers")
		e.send("GET", finConfig+"Companies('1000')?$expand=CompanyLedgers", "dora", "", 200, `"COUNTRY_CHART":"TRDE"`, `"LEDGER":"2L"`)
		e.send("GET", finConfig+"Companies('2000')", "dora", "", 200, `"COUNTRY_CHART":null`)
		e.send("GET", finConfig+"LedgerGroups('ALL')?$expand=LedgerGroupMembers", "dora", "", 200, `"REPRESENTATIVE":"0L"`, `"LEDGER":"2L"`)
		e.send("GET", finConfig+"StatementNodes?$filter=STRUCTURE%20eq%20'BS01'%20and%20KIND%20eq%20'RESULT'", "dora", "", 200, `"NODE":"RESULT"`)
		e.send("GET", finMaster+"Accounts(CHART='TRAC',ACCOUNT='140000')?$expand=AccountTexts", "dora", "", 200, `"SHORT_TEXT":"Account 140000"`)
		e.send("GET", finMaster+"CompanyAccounts?$filter=RECONCILIATION%20eq%20'CUSTOMERS'&$count=true", "dora", "", 200, `"@odata.count":4`)
		e.send("GET", finMaster+"Partners('0000000001')?$expand=Customers", "dora", "", 200, `"RECONCILIATION_ACCOUNT":"140000"`, `"ALTERNATIVE_ACCOUNT":"161000"`)
		e.send("GET", finMaster+"Customers", "dora", "", 200, `"BLOCKED":false`)
		wantIn(t, e.run("admin", "DELETE_PARTNER", `"0000000001"`), `"FAILURES":[]`)
		e.send("GET", finMaster+"Customers", "dora", "", 200, `"value":[]`)
	})
}
