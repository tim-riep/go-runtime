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
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// lateKernel installs TRLATE with the tenant ACME, an interval of
// /TRLATE/DOC for the companies 1000 and 2000 in 2026 and the user clara,
// who works with documents of 1000.
func lateKernel(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	pkg := testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "businessobjects", "TRLATE"))
	if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"1000", "2000"} {
		if err := k.AddNumberInterval(ctx, "ACME", kernel.NumberInterval{Object: "/TRLATE/DOC", ID: "01", Subobject: model.Some(c),
			FiscalYear: model.Some[int64](2026), From: "1", To: "9999999999"}, kernel.NumberRangeOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.Users().Create(ctx, "clara", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "clara", Role: "/TRLATE/CLERK", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
}

func runLate(k *kernel.Kernel, user, method string, args ...string) (string, error) {
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := k.Run(context.Background(), opts, "/TRLATE/API", method, raw)
	var s string
	if err == nil && json.Unmarshal(out, &s) == nil {
		return s, nil
	}
	return string(out), err
}

func TestLateNumbering(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		lateKernel(t, k)
		got, err := runLate(k, "clara", "CREATE", `"1000"`, "2026", `["one", "two"]`, `["1.50", "2.25"]`)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`{"ENTITY":"/TRLATE/DOCS","LINK":"d1","KEY":"{\"COMPANY\":\"1000\",\"FISCAL_YEAR\":2026,\"DOC_NO\":\"0000000001\"}"}`,
			`{"ENTITY":"/TRLATE/DOCS","LINK":"d2","KEY":"{\"COMPANY\":\"1000\",\"FISCAL_YEAR\":2026,\"DOC_NO\":\"0000000002\"}"}`,
			`"KEY":"{\"COMPANY\":\"1000\",\"FISCAL_YEAR\":2026,\"DOC_NO\":\"0000000002\",\"ITEM_NO\":2}"`,
			`"FAILURES":[]`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("result without %s:\n%s", want, got)
			}
		}
		if got, _ := runLate(k, "", "TOTAL", `"1000"`); got != "7.50" {
			t.Errorf("total written by the save method: %s", got)
		}
		if got, _ := runLate(k, "", "LINES"); got != "4" {
			t.Errorf("lines written by the save method: %s", got)
		}

		// A failing save method writes nothing and returns the number.
		got, err = runLate(k, "clara", "CREATE", `"1000"`, "2026", `["FAIL"]`, `["9.00"]`)
		if err != nil || !strings.Contains(got, `"REASON":"VALIDATION"`) && !strings.Contains(got, `"REASON":"BEHAVIOR"`) || !strings.Contains(got, "the save method fails") {
			t.Fatalf("failing save method: %s %v", got, err)
		}
		if got, _ := runLate(k, "", "TOTAL", `"1000"`); got != "7.50" {
			t.Errorf("total after the failure: %s", got)
		}
		got, _ = runLate(k, "clara", "CREATE", `"1000"`, "2026", `["three"]`, `["1.00"]`)
		if !strings.Contains(got, `\"DOC_NO\":\"0000000003\"`) {
			t.Errorf("the number of the failed save is given again: %s", got)
		}
		// Without an interval the numbering fails the instance.
		got, _ = runLate(k, "clara", "CREATE", `"3000"`, "2026", `["x"]`, `[]`)
		if !strings.Contains(got, "the interval 01 does not exist") {
			t.Errorf("without interval: %s", got)
		}

		// The authorization does not check KIND, mapped to *.
		if got, err := runLate(k, "clara", "CREATE", `"1000"`, "2026", `["clara"]`, `[]`); err != nil || !strings.Contains(got, `"FAILURES":[]`) {
			t.Errorf("clara in 1000: %s %v", got, err)
		}
		if got, _ := runLate(k, "clara", "CREATE", `"2000"`, "2026", `["clara"]`, `[]`); !strings.Contains(got, "NOT_AUTHORIZED") {
			t.Errorf("clara in 2000: %s", got)
		}
	})
}

func TestLateNumberingOverOData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		lateKernel(t, k)
		ctx := context.Background()
		if err := k.ActivateService(ctx, "/TRLATE/DOC_SERVICE", "test"); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(k.HTTPHandler())
		defer srv.Close()
		c := client{t, srv.URL}
		user := func(r *http.Request) {
			r.SetBasicAuth("clara", "secret")
			r.Header.Set("X-Tenant", "ACME")
			r.Header.Set("Content-Type", "application/json")
		}
		a := c.do("POST", "/rt/odata/trlate/doc_service/Docs", user,
			`{"COMPANY":"1000","FISCAL_YEAR":2026,"TEXT":"web","Items":[{"ITEM_NO":1,"AMOUNT":"4.00"}]}`)
		if a.status != 201 || !strings.HasSuffix(a.header.Get("Location"), "Docs(COMPANY='1000',FISCAL_YEAR=2026,DOC_NO='0000000001')") {
			t.Fatalf("create: %d %s %s", a.status, a.header.Get("Location"), a.body)
		}
		if a := c.do("GET", "/rt/odata/trlate/doc_service/Lines", user, ""); a.status != 200 || !strings.Contains(a.body, `"DOC_NO":"0000000001","LINE_NO":1,"AMOUNT":4`) {
			t.Errorf("lines: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/rt/odata/trlate/doc_service/Lines", user, `{"COMPANY":"1000","FISCAL_YEAR":2026,"DOC_NO":"0000000001","LINE_NO":9,"AMOUNT":"1.00"}`); a.status != 405 {
			t.Errorf("lines are not created over OData: %d %s", a.status, a.body)
		}
		// An action that creates a document names it by its link; the
		// answer has the final key that late numbering gave it.
		if a := c.do("POST", "/rt/odata/trlate/doc_service/CREATE_DOC", user, `{"COMPANY":"1000","FISCAL_YEAR":2026,"TEXT":"action"}`); a.status != 200 ||
			!strings.Contains(a.body, `"LINK":"new","COMPANY":"1000","FISCAL_YEAR":2026,"DOC_NO":"0000000002"`) {
			t.Errorf("action: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/rt/odata/trlate/doc_service/Docs(COMPANY='1000',FISCAL_YEAR=2026,DOC_NO='0000000002')", user, ""); a.status != 200 || !strings.Contains(a.body, `"TEXT":"action"`) {
			t.Errorf("document of the action: %d %s", a.status, a.body)
		}
	})
}

func TestMaintainedBySaveRules(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		lateKernel(t, k)
		fsys := fstest.MapFS{
			"manifest.json":   {Data: []byte(`{"id":"TRLATEUSE","version":"1.0.0","languageVersion":1,"requires":[{"id":"TRBASE"},{"id":"TRLATE"}]}`)},
			"src/WRITE.class": {Data: []byte("public class /TRLATEUSE/WRITE\n\n  public static method RUN()\n    delete from /TRLATE/LINES\n  end\n\nend\n")},
		}
		p, err := pkgload.Load(fsys, "TRLATEUSE")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := k.Install(context.Background(), []*model.Package{p}, kernel.Options{DryRun: true}); err == nil || !strings.Contains(err.Error(), "maintained by the save method of business object /TRLATE/DOC") {
			t.Errorf("writing a maintained entity elsewhere: %v", err)
		}
	})
}
