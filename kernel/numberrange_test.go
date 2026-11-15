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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

func numbersPackage(t *testing.T) *model.Package {
	return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "numberranges", "TRNUM"))
}

// numbersKernel installs TRNUM, creates the tenant ACME with the users
// admin, who maintains the intervals of TRNUM, and bob, who reads those of
// tickets, and adds the intervals the tests use.
func numbersKernel(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	if _, err := k.Install(ctx, []*model.Package{numbersPackage(t)}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]string{"admin": "/TRNUM/ADMIN", "bob": "/TRNUM/READER"} {
		if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, iv := range []kernel.NumberInterval{
		{Object: "/TRNUM/DOCUMENT", ID: "01", Subobject: model.Some("1000"), FiscalYear: model.Some[int64](2026), From: "1", To: "9999999999"},
		{Object: "/TRNUM/DOCUMENT", ID: "01", Subobject: model.Some("2000"), FiscalYear: model.Some[int64](2026), From: "5000000000", To: "5999999999"},
		{Object: "/TRNUM/DOCUMENT", ID: "01", Subobject: model.Some("1000"), FiscalYear: model.Some[int64](2027), From: "1", To: "9999999999"},
		{Object: "/TRNUM/TICKET", ID: "01", From: "1", To: "4"},
		{Object: "/TRNUM/TICKET", ID: "X1", From: "5000", To: "5999", External: true},
		{Object: "/TRNUM/PARTNER", ID: "01", From: "1", To: "999999"},
	} {
		if err := k.AddNumberInterval(ctx, "ACME", iv, kernel.NumberRangeOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
}

func runNumbers(k *kernel.Kernel, user, method string, args ...string) (string, error) {
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME", Language: "en"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := k.Run(context.Background(), opts, "/TRNUM/NUMBERS", method, raw)
	return string(out), err
}

func mustNumbers(t *testing.T, k *kernel.Kernel, method string, args ...string) string {
	t.Helper()
	out, err := runNumbers(k, "", method, args...)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return out
}

func numberRangeError(err error, kind rterr.NumberRangeErrorKind) bool {
	var ne *rterr.NumberRangeError
	return errors.As(err, &ne) && ne.Kind == kind
}

func TestNumberRangeMaintenance(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		numbersKernel(t, k)
		opts := kernel.NumberRangeOptions{By: "test"}
		add := func(iv kernel.NumberInterval) error { return k.AddNumberInterval(ctx, "ACME", iv, opts) }

		list, err := k.NumberIntervals(ctx, "ACME", "/TRNUM/DOCUMENT", opts)
		if err != nil || len(list) != 3 || list[0].Subobject.Or("") != "1000" || list[0].FiscalYear.Or(0) != 2026 || list[0].From != "0000000001" ||
			list[1].FiscalYear.Or(0) != 2027 || list[2].Subobject.Or("") != "2000" || list[0].Current.Valid() {
			t.Fatalf("intervals: %+v %v", list, err)
		}
		for _, c := range []struct {
			iv   kernel.NumberInterval
			kind rterr.NumberRangeErrorKind
			text string
		}{
			{kernel.NumberInterval{Object: "/TRNUM/NOPE", ID: "01", From: "1", To: "2"}, rterr.NumberRangeNotFound, "does not exist"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "1", From: "1", To: "2"}, rterr.NumberRangeArgument, "two upper-case letters or digits"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "02", From: "4", To: "9"}, rterr.NumberRangeArgument, "overlaps with interval 01"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "01", From: "100", To: "200"}, rterr.NumberRangeArgument, "exists already"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "02", From: "9", To: "10000"}, rterr.NumberRangeArgument, "up to 4 digits"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "02", From: "9", To: "8"}, rterr.NumberRangeArgument, "greater than the to number"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "02", From: "1a", To: "8"}, rterr.NumberRangeArgument, "is not a number"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "02", From: "10", To: "20", Current: model.Some("30")}, rterr.NumberRangeArgument, "outside of the interval"},
			{kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "02", From: "10", To: "20", Subobject: model.Some("1000")}, rterr.NumberRangeArgument, "has no subobject"},
			{kernel.NumberInterval{Object: "/TRNUM/DOCUMENT", ID: "02", From: "1", To: "2", FiscalYear: model.Some[int64](2026)}, rterr.NumberRangeArgument, "needs a value of its subobject COMPANY"},
			{kernel.NumberInterval{Object: "/TRNUM/DOCUMENT", ID: "02", From: "1", To: "2", Subobject: model.Some("1000")}, rterr.NumberRangeArgument, "needs a fiscal year"},
			{kernel.NumberInterval{Object: "/TRNUM/DOCUMENT", ID: "02", From: "1", To: "2", Subobject: model.Some("10000"), FiscalYear: model.Some[int64](2026)}, rterr.NumberRangeArgument, "at most 4"},
		} {
			if err := add(c.iv); !numberRangeError(err, c.kind) || !strings.Contains(err.Error(), c.text) {
				t.Errorf("%+v: %v", c.iv, err)
			}
		}
		// Another subobject and another year do not overlap.
		if err := add(kernel.NumberInterval{Object: "/TRNUM/DOCUMENT", ID: "02", Subobject: model.Some(""), FiscalYear: model.Some[int64](2026), From: "1", To: "9"}); err != nil {
			t.Fatalf("the empty string as subobject: %v", err)
		}

		// Moving the current number back and deleting a used interval need
		// a confirmation.
		mustNumbers(t, k, "TICKETS", "2")
		back := kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "01", From: "1", To: "4", Current: model.Some("1")}
		if err := k.ChangeNumberInterval(ctx, "ACME", back, opts); !numberRangeError(err, rterr.NumberRangeConfirmation) {
			t.Fatalf("moving back: %v", err)
		}
		if err := k.ChangeNumberInterval(ctx, "ACME", back, kernel.NumberRangeOptions{By: "test", Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if got := mustNumbers(t, k, "TICKETS", "1"); got != `["0002"]` {
			t.Errorf("after moving back: %s", got)
		}
		if err := k.ChangeNumberInterval(ctx, "ACME", kernel.NumberInterval{Object: "/TRNUM/TICKET", ID: "01", From: "1", To: "8", Current: model.Some("2")}, opts); err != nil {
			t.Fatalf("a longer interval: %v", err)
		}
		if err := k.DeleteNumberInterval(ctx, "ACME", "/TRNUM/TICKET", "01", model.None[string](), model.None[int64](), opts); !numberRangeError(err, rterr.NumberRangeConfirmation) {
			t.Fatalf("deleting a used interval: %v", err)
		}
		if err := k.DeleteNumberInterval(ctx, "ACME", "/TRNUM/TICKET", "01", model.None[string](), model.None[int64](), kernel.NumberRangeOptions{By: "test", Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if err := k.DeleteNumberInterval(ctx, "ACME", "/TRNUM/DOCUMENT", "02", model.Some(""), model.Some[int64](2026), opts); err != nil {
			t.Fatalf("deleting an unused interval: %v", err)
		}
		history, err := k.NumberRangeHistory(ctx, "ACME", "/TRNUM/TICKET", opts)
		var kinds []string
		for _, h := range history {
			kinds = append(kinds, fmt.Sprintf("%s %s %t", h.Kind, h.Interval, h.Confirmed))
		}
		if err != nil || strings.Join(kinds, ",") != "add 01 false,add X1 false,change 01 true,change 01 false,delete 01 true" {
			t.Errorf("history: %v %v", kinds, err)
		}
		if _, err := k.NumberIntervals(ctx, "BETA", "/TRNUM/TICKET", opts); err == nil {
			t.Errorf("an unknown tenant")
		}
	})
}

func TestNumberRangeGivesNumbers(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		numbersKernel(t, k)
		doc := func(company string, year int) string {
			return mustNumbers(t, k, "DOCUMENT", `"`+company+`"`, fmt.Sprint(year))
		}
		if got := doc("1000", 2026); got != `"0000000001"` {
			t.Fatalf("first number: %s", got)
		}
		// A rollback returns the number.
		if _, err := runNumbers(k, "", "DOCUMENT_FAILING", `"1000"`, "2026"); err == nil {
			t.Fatal("the failing method succeeded")
		}
		if got := doc("1000", 2026); got != `"0000000002"` {
			t.Errorf("after a rollback: %s", got)
		}
		// Subobjects and years count on their own.
		if got := doc("2000", 2026) + doc("1000", 2027) + doc("1000", 2026); got != `"5000000000""0000000001""0000000003"` {
			t.Errorf("per subobject and year: %s", got)
		}
		for _, c := range []struct {
			args []string
			want string
		}{
			{[]string{`"/TRNUM/DOCUMENT"`, `"01"`, `"3000"`, "2026"}, "NOT_FOUND"},
			{[]string{`"/TRNUM/DOCUMENT"`, `"01"`, "null", "2026"}, "ARGUMENT_INVALID"},
			{[]string{`"/TRNUM/DOCUMENT"`, `"01"`, `"1000"`, "null"}, "ARGUMENT_INVALID"},
			{[]string{`"/TRNUM/TICKET"`, `"X1"`, "null", "null"}, "ARGUMENT_INVALID"},
			{[]string{`"/TRNUM/NOPE"`, `"01"`, "null", "null"}, "NOT_FOUND"},
			{[]string{`"/TRBASE/NUMBER_RANGE"`, `"01"`, "null", "null"}, "NOT_FOUND"},
		} {
			if got := mustNumbers(t, k, "FAILURE", c.args...); got != `"`+c.want+`"` {
				t.Errorf("%v: %s", c.args, got)
			}
		}

		// External numbers.
		for number, want := range map[string]string{"5001": "true", "6000": "false", "501": "false", "50a1": "false"} {
			if got := mustNumbers(t, k, "EXTERNAL", `"`+number+`"`); got != want {
				t.Errorf("external %s: %s", number, got)
			}
		}

		// A session of its own: the number counts with its commit.
		if a, b := mustNumbers(t, k, "TICKET_IN_SESSION", "false"), mustNumbers(t, k, "TICKET_IN_SESSION", "true"); a != `"0001"` || b != `"0001"` {
			t.Errorf("in a session: %s %s", a, b)
		}

		// The end of an interval and the warning before.
		var log bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
		got := mustNumbers(t, k, "TICKETS", "3")
		slog.SetDefault(old)
		if got != `["0002","0003","0004"]` {
			t.Errorf("tickets: %s", got)
		}
		if n := strings.Count(log.String(), "reached its warning threshold"); n != 1 {
			t.Errorf("one warning: %s", log.String())
		}
		if got := mustNumbers(t, k, "FAILURE", `"/TRNUM/TICKET"`, `"01"`, "null", "null"); got != `"EXHAUSTED"` {
			t.Errorf("after the last number: %s", got)
		}
		if _, err := runNumbers(k, "", "TICKETS", "1"); err == nil || !strings.Contains(err.Error(), "has given its last number 0004") {
			t.Errorf("unhandled: %v", err)
		}
	})
}

// TestNumberRangeBuffered reserves blocks per kernel; a new kernel on the
// same database reserves a new block, which leaves a gap.
func TestNumberRangeBuffered(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			numbersKernel(t, k)
			a, b := mustNumbers(t, k, "PARTNER"), mustNumbers(t, k, "PARTNER")
			list, _ := k.NumberIntervals(ctx, "ACME", "/TRNUM/PARTNER", kernel.NumberRangeOptions{})
			k.Close()
			if a != `"000001"` || b != `"000002"` || list[0].Current.Or("") != "000005" {
				t.Fatalf("first block: %s %s %+v", a, b, list)
			}
			k2, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k2.Close()
			if got := mustNumbers(t, k2, "PARTNER"); got != `"000006"` {
				t.Errorf("after a restart: %s", got)
			}
			// A change of the intervals drops the block.
			if err := k2.ChangeNumberInterval(ctx, "ACME", kernel.NumberInterval{Object: "/TRNUM/PARTNER", ID: "01", From: "1", To: "999999", Current: model.Some("100")},
				kernel.NumberRangeOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
			if got := mustNumbers(t, k2, "PARTNER"); got != `"000101"` {
				t.Errorf("after a change: %s", got)
			}
		})
	}
}

func TestNumberRangeAdministration(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		numbersKernel(t, k)
		ticket := `{"RANGE_NAME":"/TRNUM/TICKET","INTERVAL_ID":"02","SUBOBJECT":null,"FISCAL_YEAR":null,"FROM_NO":"100","TO_NO":"199","CURRENT_NO":null,"EXTERNAL":false,"USED_PERCENT":null,"CHANGED_AT":null,"CHANGED_BY":null}`
		if _, err := runNumbers(k, "bob", "ADD", ticket); !notAuthorized(err) {
			t.Fatalf("bob adds: %v", err)
		}
		if _, err := runNumbers(k, "", "ADD", ticket); !notAuthorized(err) {
			t.Fatalf("without user: %v", err)
		}
		if got, err := runNumbers(k, "bob", "OBJECTS"); err != nil || !strings.Contains(got, `"/TRNUM/TICKET"`) || strings.Contains(got, `"/TRNUM/DOCUMENT"`) {
			t.Errorf("objects bob may read: %s %v", got, err)
		}
		if _, err := runNumbers(k, "admin", "ADD", ticket); err != nil {
			t.Fatal(err)
		}
		got, err := runNumbers(k, "bob", "INTERVALS", `"/TRNUM/TICKET"`)
		if err != nil || !strings.Contains(got, `"INTERVAL_ID":"02","SUBOBJECT":null,"FISCAL_YEAR":null,"FROM_NO":"0100","TO_NO":"0199","CURRENT_NO":null`) {
			t.Errorf("intervals: %s %v", got, err)
		}
		used := strings.Replace(strings.Replace(ticket, `"CURRENT_NO":null`, `"CURRENT_NO":"150"`, 1), `"INTERVAL_ID":"02"`, `"INTERVAL_ID":"02"`, 1)
		if _, err := runNumbers(k, "admin", "CHANGE", used, "false"); err != nil {
			t.Fatal(err)
		}
		var ex *vm.Exception
		if _, err := runNumbers(k, "admin", "CHANGE", ticket, "false"); !errors.As(err, &ex) || ex.Class != "/TRBASE/NUMBER_RANGE_CONFIRMATION_REQUIRED" {
			t.Errorf("moving back: %v", err)
		}
		if _, err := runNumbers(k, "admin", "REMOVE", `"/TRNUM/TICKET"`, `"02"`, "false"); !errors.As(err, &ex) || ex.Class != "/TRBASE/NUMBER_RANGE_CONFIRMATION_REQUIRED" {
			t.Errorf("deleting a used interval: %v", err)
		}
		if _, err := runNumbers(k, "admin", "REMOVE", `"/TRNUM/TICKET"`, `"02"`, "true"); err != nil {
			t.Fatal(err)
		}
		if got, err := runNumbers(k, "admin", "HISTORY"); err != nil || strings.Count(got, `"CHANGED_BY":"admin"`) != 3 || !strings.Contains(got, `"KIND":"delete"`) {
			t.Errorf("history: %s %v", got, err)
		}
	})
}

// TestNumberRangeInCode checks literal objects when the code compiles and
// keeps used objects from being removed.
func TestNumberRangeInCode(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		numbersKernel(t, k)
		user := func(requires, object string) *model.Package {
			fsys := fstest.MapFS{
				"manifest.json": {Data: []byte(`{"id":"TRNUMUSE","version":"1.0.0","languageVersion":1,"requires":[{"id":"TRBASE"}` + requires + `]}`)},
				"src/USE.class": {Data: []byte("public class /TRNUMUSE/USE\n\n  public static method NEXT() returns STRING raises /TRBASE/NUMBER_RANGE_ERROR\n" +
					"    return /TRBASE/NUMBER_RANGE.NEXT(\"" + object + "\", \"01\", null, null)\n  end\n\nend\n")},
			}
			p, err := pkgload.Load(fsys, "TRNUMUSE")
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		if _, err := k.Install(ctx, []*model.Package{user(`,{"id":"TRNUM"}`, "/TRNUM/NOPE")}, kernel.Options{}); err == nil || !strings.Contains(err.Error(), "number range object /TRNUM/NOPE does not exist") {
			t.Errorf("unknown object: %v", err)
		}
		if _, err := k.Install(ctx, []*model.Package{user("", "/TRNUM/TICKET")}, kernel.Options{}); err == nil || !strings.Contains(err.Error(), "does not require TRNUM") {
			t.Errorf("object of a package not required: %v", err)
		}
		if _, err := k.Install(ctx, []*model.Package{user(`,{"id":"TRNUM"}`, "/TRNUM/TICKET")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		uses, err := k.CodeUses(ctx, "/TRNUM/TICKET")
		if err != nil || len(uses) != 4 || uses[3].Package != "TRNUMUSE" || uses[3].Kind != "number-range" {
			t.Errorf("uses: %+v %v", uses, err)
		}

		// Removing the object fails while code uses it; a shorter number
		// removes the intervals.
		dir := t.TempDir()
		if err := os.CopyFS(dir, os.DirFS(filepath.Join(testutil.ModuleRoot(t), "testdata", "numberranges", "TRNUM"))); err != nil {
			t.Fatal(err)
		}
		write := func(rel, text string) {
			if err := os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("manifest.json", strings.Replace(string(must(os.ReadFile(filepath.Join(dir, "manifest.json")))), `"1.0.0"`, `"1.1.0"`, 1))
		ticket := filepath.Join("schema", "number-ranges", "TICKET.json")
		original := string(must(os.ReadFile(filepath.Join(dir, ticket))))
		if err := os.Remove(filepath.Join(dir, ticket)); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{AllowDestructive: true}); err == nil || !strings.Contains(err.Error(), "/TRNUM/TICKET") {
			t.Errorf("removing a used object: %v", err)
		}
		write(ticket, strings.Replace(original, `"length": 4`, `"length": 3`, 1))
		p, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{DryRun: true})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) {
			t.Fatalf("a shorter number: %v %v", p, err)
		}
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{AllowDestructive: true}); err != nil {
			t.Fatal(err)
		}
		if list, err := k.NumberIntervals(ctx, "ACME", "/TRNUM/TICKET", kernel.NumberRangeOptions{}); err != nil || len(list) != 0 {
			t.Errorf("intervals after a shorter number: %+v %v", list, err)
		}
		if list, _ := k.NumberIntervals(ctx, "ACME", "/TRNUM/DOCUMENT", kernel.NumberRangeOptions{}); len(list) != 3 {
			t.Errorf("other objects keep their intervals: %+v", list)
		}
	})
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// TestNumberRangeParallel gives numbers in parallel executions under
// PostgreSQL: they wait for each other and get unique numbers without gaps.
func TestNumberRangeParallel(t *testing.T) {
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
			numbersKernel(t, k)
			var mu sync.Mutex
			var got []string
			var wg sync.WaitGroup
			errs := make(chan error, 8)
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range 50 {
						out, err := runNumbers(k, "", "DOCUMENT", `"1000"`, "2026")
						if err != nil {
							errs <- err
							return
						}
						mu.Lock()
						got = append(got, out)
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatal(err)
			}
			slices.Sort(got)
			for i, n := range got {
				if want := fmt.Sprintf(`"%010d"`, i+1); n != want {
					t.Fatalf("number %d is %s, not %s", i+1, n, want)
				}
			}
		})
	}
}
