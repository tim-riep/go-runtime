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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// aggregateKernel installs TRAGG with the tenant ACME and the user bob,
// who reads the accounts starting with 1.
func aggregateKernel(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	pkg := testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "aggregates", "TRAGG"))
	if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Users().Create(ctx, "bob", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "bob", Role: "/TRAGG/READER", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
}

func runSums(k *kernel.Kernel, user, method string, args ...string) (string, error) {
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := k.Run(context.Background(), opts, "/TRAGG/SUMS", method, raw)
	return string(out), err
}

func mustSums(t *testing.T, k *kernel.Kernel, method string, args ...string) string {
	t.Helper()
	out, err := runSums(k, "", method, args...)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var s string
	if json.Unmarshal([]byte(out), &s) == nil {
		return s
	}
	return out
}

func TestAggregates(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		aggregateKernel(t, k)
		if got := mustSums(t, k, "TOTAL", "null"); got != `{"SUM_AMOUNT":null,"COUNT":0,"SUM_QTY":null,"MAX_PERIOD":null}` {
			t.Errorf("over no row: %s", got)
		}
		for i, p := range [][]string{
			{`"1000"`, "1", `"0.10"`, "2", `"b"`},
			{`"1000"`, "1", `"0.20"`, "null", "null"},
			{`"1000"`, "2", `"-1.00"`, "3", `"a"`},
			{`"2000"`, "1", `"1234567890123456.78"`, "null", `""`},
			{`"2000"`, "3", `"0.01"`, "5", `"c"`},
			{`"3000"`, "2", `"7.00"`, "null", "null"},
		} {
			mustSums(t, k, "ADD", append([]string{fmt.Sprint(i + 1)}, p...)...)
		}
		want := `[{"ACCOUNT":"1000","SUM_AMOUNT":"-0.70","COUNT":3,"MIN_PERIOD":1,"MAX_NOTE":"b","UNITS":5},` +
			`{"ACCOUNT":"2000","SUM_AMOUNT":"1234567890123456.79","COUNT":2,"MIN_PERIOD":1,"MAX_NOTE":"c","UNITS":5},` +
			`{"ACCOUNT":"3000","SUM_AMOUNT":"7.00","COUNT":1,"MIN_PERIOD":2,"MAX_NOTE":null,"UNITS":null}]`
		if got := mustSums(t, k, "PER_ACCOUNT"); got != want {
			t.Errorf("per account:\n%s\nwant\n%s", got, want)
		}
		if got := mustSums(t, k, "PER_PERIOD"); got != `["2000/1:1234567890123456.78","3000/2:7.00","1000/1:0.30","2000/3:0.01"]` {
			t.Errorf("per period, ordered by the sum: %s", got)
		}
		if got := mustSums(t, k, "TOTAL", "null"); got != `{"SUM_AMOUNT":"1234567890123463.09","COUNT":6,"SUM_QTY":10,"MAX_PERIOD":3}` {
			t.Errorf("total: %s", got)
		}
		if got := mustSums(t, k, "TOTAL", `"9999"`); got != `{"SUM_AMOUNT":null,"COUNT":0}` {
			t.Errorf("total of no row: %s", got)
		}
		if got, err := runSums(k, "bob", "READABLE"); err != nil || !strings.Contains(got, `[{\"ACCOUNT\":\"1000\",\"SUM_AMOUNT\":\"-0.70\"}]`) {
			t.Errorf("with the authorizations of bob: %s %v", got, err)
		}
		if got, err := runSums(k, "", "READABLE"); err != nil || got != `"[]"` {
			t.Errorf("without user: %s %v", got, err)
		}

		// Changes by a value.
		mustSums(t, k, "OPEN", `"1000"`)
		if n := mustSums(t, k, "BOOK", `"1000"`, `"10.50"`); n != "1" {
			t.Errorf("booked rows: %s", n)
		}
		mustSums(t, k, "BOOK", `"1000"`, `"-0.25"`)
		mustSums(t, k, "TAKE", `"1000"`, `"5.00"`)
		if got := mustSums(t, k, "BALANCE", `"1000"`); got != `{"ACCOUNT":"1000","BALANCE":"5.25","POSTINGS":2,"LIMIT_AMOUNT":"5.00"}` {
			t.Errorf("balance: %s", got)
		}
		if n := mustSums(t, k, "BOOK", `"none"`, `"1.00"`); n != "0" {
			t.Errorf("no row: %s", n)
		}
		mustSums(t, k, "OPEN", `"BIG"`)
		if got := mustSums(t, k, "OVERFLOWS"); got != "OVERFLOW" {
			t.Errorf("a result the field cannot hold: %s", got)
		}
	})
}

// TestAggregateSumBeyondColumns sums more than 18 digits: PostgreSQL keeps
// the sum exact, SQLite, which sums the scaled integers of DEC, overflows.
func TestAggregateSumBeyondColumns(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		aggregateKernel(t, k)
		for i := range 10 {
			mustSums(t, k, "ADD", fmt.Sprint(i+1), `"1000"`, "1", `"9999999999999999.99"`, "null", "null")
		}
		got := mustSums(t, k, "SUM_OVERFLOWS")
		want := `{"SUM_AMOUNT":"99999999999999999.90"}`
		if k.Dialect().Name() == "sqlite" {
			want = "OVERFLOW"
		}
		if got != want {
			t.Errorf("sum: %s, want %s", got, want)
		}
	})
}

func TestAggregateRules(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		aggregateKernel(t, k)
		for _, c := range []struct{ code, msg string }{
			{`var x = select ACCOUNT, sum(AMOUNT) from /TRAGG/POSTINGS`, "ACCOUNT is not aggregated, so it must be a field of group by"},
			{`var x = select sum(AMOUNT) from /TRAGG/POSTINGS group by ACCOUNT`, "ACCOUNT of group by must be a column of the select list"},
			{`var x = select sum(ACCOUNT) from /TRAGG/POSTINGS`, "sum needs a field of a number type"},
			{`var x = select sum(AMOUNT), sum(AMOUNT) from /TRAGG/POSTINGS`, "two columns named SUM_AMOUNT"},
			{`var x = select sum(AMOUNT) as total from /TRAGG/POSTINGS`, "upper case"},
			{`var x = select ACCOUNT from /TRAGG/POSTINGS group by ACCOUNT order by AMOUNT`, "it has no column AMOUNT"},
			{`var x = select from /TRAGG/POSTINGS group by ACCOUNT`, "group by needs a select list"},
			{`var x = select sum(NOPE) from /TRAGG/POSTINGS`, "has no field NOPE"},
			{`update /TRAGG/BALANCES set LIMIT_AMOUNT = LIMIT_AMOUNT + 1.00`, "LIMIT_AMOUNT may be null"},
			{`update /TRAGG/BALANCES set ACCOUNT = ACCOUNT + "x"`, "needs a field of a number type"},
			{`update /TRAGG/BALANCES set BALANCE = BALANCE + 1.00 - 2.00`, "unexpected"},
			{`update /TRAGG/LOCKED set BALANCE = BALANCE + 1.00`, "is an entity of business object /TRAGG/LOCKED"},
		} {
			fsys := fstest.MapFS{
				"manifest.json": {Data: []byte(`{"id":"TRAGGBAD","version":"1.0.0","languageVersion":1,"requires":[{"id":"TRBASE"},{"id":"TRAGG"}]}`)},
				"src/BAD.class": {Data: []byte("public class /TRAGGBAD/BAD\n\n  public static method RUN()\n    " + c.code + "\n  end\n\nend\n")},
			}
			p, err := pkgload.Load(fsys, "TRAGGBAD")
			if err == nil {
				_, err = k.Install(context.Background(), []*model.Package{p}, kernel.Options{DryRun: true})
			}
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Errorf("%s: %v", c.code, err)
			}
		}
	})
}

// TestAggregateChangesInParallel changes a field by a value in parallel
// executions under PostgreSQL: no change is lost.
func TestAggregateChangesInParallel(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		if db.Name == "sqlite" {
			continue
		}
		t.Run(db.Name, func(t *testing.T) {
			k, err := kernel.Open(context.Background(), db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			aggregateKernel(t, k)
			mustSums(t, k, "OPEN", `"1000"`)
			var wg sync.WaitGroup
			errs := make(chan error, 16)
			for range 16 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range 100 {
						if _, err := runSums(k, "", "BOOK", `"1000"`, `"1.00"`); err != nil {
							errs <- err
							return
						}
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatal(err)
			}
			if got := mustSums(t, k, "BALANCE", `"1000"`); got != `{"ACCOUNT":"1000","BALANCE":"1600.00","POSTINGS":1600,"LIMIT_AMOUNT":null}` {
				t.Errorf("after 1600 changes: %s", got)
			}
		})
	}
}
