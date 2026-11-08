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
	"strconv"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// TestShippedHandlers runs the ping and asset handlers of TRBASE and the
// company handlers of TRFIN.
func TestShippedHandlers(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"ACME", "BETA"} {
			if _, err := k.Tenants().Create(ctx, id, model.None[string]()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := k.Users().Create(ctx, "anna", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"/TRFIN/SERVICE_USER", "/TRFIN/COMPANY_ADMIN"} {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
		for _, p := range []string{"/trbase", "/trfin"} {
			if err := k.ActivateHTTPService(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		srv := httptest.NewServer(k.HTTPHandler())
		t.Cleanup(srv.Close)
		c := client{t, srv.URL}
		anna := basicAuth("anna", "secret")

		if a := c.do("GET", "/trbase/ping", nil, ""); a.status != 200 || a.body != `{"status":"ok"}` || a.header.Get("Content-Type") != "application/json" {
			t.Fatalf("ping: %d %q %v", a.status, a.body, a.header)
		}

		// Assets with ETag, Last-Modified, HEAD and 304.
		css := string(exampleFile(t, "TRBASE", "web/css/base.css"))
		a := c.do("GET", "/trbase/web/css/base.css", nil, "")
		etag, modified := a.header.Get("ETag"), a.header.Get("Last-Modified")
		if a.status != 200 || a.body != css || !strings.HasPrefix(a.header.Get("Content-Type"), "text/css") || etag == "" || modified == "" ||
			a.header.Get("Content-Length") != strconv.Itoa(len(css)) || a.header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("asset: %d %v", a.status, a.header)
		}
		if a := c.do("HEAD", "/trbase/web/css/base.css", nil, ""); a.status != 200 || a.body != "" || a.header.Get("ETag") != etag || a.header.Get("Content-Length") != strconv.Itoa(len(css)) {
			t.Errorf("HEAD: %d %v", a.status, a.header)
		}
		if a := c.do("GET", "/trbase/web/css/base.css", func(r *http.Request) { r.Header.Set("If-None-Match", etag) }, ""); a.status != 304 || a.body != "" {
			t.Errorf("If-None-Match: %d %q", a.status, a.body)
		}
		if a := c.do("GET", "/trbase/web/css/base.css", func(r *http.Request) { r.Header.Set("If-None-Match", `"other"`) }, ""); a.status != 200 {
			t.Errorf("other ETag: %d", a.status)
		}
		if a := c.do("GET", "/trbase/web/css/base.css", func(r *http.Request) { r.Header.Set("If-Modified-Since", modified) }, ""); a.status != 304 {
			t.Errorf("If-Modified-Since: %d", a.status)
		}
		for _, p := range []string{"/trbase/web/css/missing.css", "/trbase/web/css", "/trbase/web/../ping", "/trbase/web/"} {
			if a := c.do("GET", p, nil, ""); a.status != 404 {
				t.Errorf("%s: %d %s", p, a.status, a.body)
			}
		}
		if a := c.do("POST", "/trbase/web/css/base.css", nil, ""); a.status != 404 {
			t.Errorf("POST asset: %d", a.status)
		}

		// Companies: POST creates, GET lists the companies of the tenant.
		mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY", `"DE"`, `"Germany"`)
		a = c.do("POST", "/trfin/companies", anna, `{"COMPANY_ID":"C1","NAME":"Acme","COUNTRY":"DE","STATUS":"ACTIVE"}`)
		if a.status != 201 || !strings.Contains(a.body, `"NAME":"Acme"`) {
			t.Fatalf("POST: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trfin/companies", anna, `{"NAME":1}`); a.status != 400 {
			t.Errorf("invalid body: %d %s", a.status, a.body)
		}
		a = c.do("GET", "/trfin/companies", anna, "")
		var list []map[string]any
		if err := json.Unmarshal([]byte(a.body), &list); a.status != 200 || err != nil || len(list) != 1 || list[0]["COMPANY_ID"] != "C1" {
			t.Fatalf("GET: %d %s", a.status, a.body)
		}
		// A clerk reads the companies of its countries and creates none:
		// the request is rolled back.
		mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY", `"FR"`, `"France"`)
		if a := c.do("POST", "/trfin/companies", anna, `{"COMPANY_ID":"C2","NAME":"Bleu","COUNTRY":"FR","STATUS":"ACTIVE"}`); a.status != 201 {
			t.Fatalf("POST FR: %d %s", a.status, a.body)
		}
		if _, err := k.Users().Create(ctx, "carl", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"/TRFIN/SERVICE_USER", "/TRFIN/COMPANY_CLERK"} {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "carl", Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
		carl := basicAuth("carl", "secret")
		if a := c.do("GET", "/trfin/companies", carl, ""); a.status != 200 || !strings.Contains(a.body, `"C1"`) || strings.Contains(a.body, `"C2"`) {
			t.Errorf("GET as clerk: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trfin/companies", carl, `{"COMPANY_ID":"C3","NAME":"Drei","COUNTRY":"DE","STATUS":"ACTIVE"}`); a.status != 403 ||
			!strings.Contains(a.body, `"forbidden"`) {
			t.Errorf("POST as clerk: %d %s", a.status, a.body)
		}
		if got := mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "FIND", `"C3"`); got != "null" {
			t.Errorf("C3 was stored: %s", got)
		}
		if f, err := k.LastFailedCheck(ctx, "carl", "ACME"); err != nil || string(f.Values["ACTIVITY"]) != `"CREATE"` || f.Site.Class.Or("") != "/TRFIN/COMPANY_INTAKE" {
			t.Errorf("last failed: %+v %v", f, err)
		}
		if err := k.Users().AddTenant(ctx, "anna", "BETA"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/SERVICE_USER", Tenant: "BETA"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		a = c.do("GET", "/trfin/companies", func(r *http.Request) { anna(r); r.Header.Set("X-Tenant", "BETA") }, "")
		if a.status != 200 || a.body != "[]" {
			t.Errorf("BETA: %d %s", a.status, a.body)
		}
		if a := c.do("DELETE", "/trfin/companies", anna, ""); a.status != 404 {
			t.Errorf("DELETE: %d", a.status)
		}
	})
}
