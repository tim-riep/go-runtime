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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func httpPackage(t *testing.T) string {
	return filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "http", "TRHTTP")
}

// client sends requests to a test server.
type client struct {
	t   *testing.T
	url string
}

type answer struct {
	status int
	header http.Header
	body   string
}

func (c client) do(method, path string, setup func(r *http.Request), body string) answer {
	c.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r, err := http.NewRequest(method, c.url+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if setup != nil {
		setup(r)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return answer{resp.StatusCode, resp.Header, string(b)}
}

func basicAuth(user, pw string) func(r *http.Request) {
	return func(r *http.Request) { r.SetBasicAuth(user, pw) }
}

func errorCode(t *testing.T, a answer) (string, string) {
	t.Helper()
	var e struct{ Code, Message string }
	if err := json.Unmarshal([]byte(a.body), &e); err != nil {
		t.Fatalf("no error answer: %d %s", a.status, a.body)
	}
	return e.Code, e.Message
}

// httpKernel installs TRHTTP, creates the tenants ACME and BETA and the
// user anna in ACME, and starts a test server.
func httpKernel(t *testing.T, k *kernel.Kernel) client {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, httpPackage(t))}, kernel.Options{}); err != nil {
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
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRHTTP/CALLER", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(k.HTTPHandler())
	t.Cleanup(srv.Close)
	return client{t, srv.URL}
}

// TestHTTPHandlers runs requests through handler chains with
// authentication, tenants, languages, commit and rollback.
func TestHTTPHandlers(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		c := httpKernel(t, k)
		anna := basicAuth("anna", "secret")

		// Inactive nodes do not exist.
		if a := c.do("GET", "/trhttp/echo", anna, ""); a.status != 404 {
			t.Fatalf("inactive: %d %s", a.status, a.body)
		}
		if err := k.ActivateHTTPService(ctx, "/trhttp"); err != nil {
			t.Fatal(err)
		}
		a := c.do("GET", "/trhttp/echo", nil, "")
		if a.status != 401 || len(a.header.Values("WWW-Authenticate")) != 2 {
			t.Fatalf("without credentials: %d %v", a.status, a.header)
		}
		if code, _ := errorCode(t, a); code != "unauthorized" {
			t.Errorf("code: %s", code)
		}
		a = c.do("GET", "/trhttp/echo/a/b?q=1&q=2", func(r *http.Request) { anna(r); r.Header.Set("X-Test", "") }, "")
		want := "method=GET path=/trhttp/echo rest=a/b q=1 qs=2 h= p=hello e= user=anna tenant=ACME lang=en body="
		if a.status != 200 || a.body != want || a.header.Get("X-Passed") != "nope" {
			t.Fatalf("echo: %d %q %v", a.status, a.body, a.header)
		}
		if a := c.do("POST", "/trhttp/echo", anna, "short"); !strings.HasSuffix(a.body, "body=short") {
			t.Errorf("POST: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trhttp/echo", anna, strings.Repeat("x", 17)); a.status != 413 {
			t.Errorf("large body: %d %s", a.status, a.body)
		}
		// A foreign tenant is forbidden; the language of the answer follows the request.
		a = c.do("GET", "/trhttp/echo?lang=de", func(r *http.Request) { anna(r); r.Header.Set("X-Tenant", "BETA") }, "")
		if code, msg := errorCode(t, a); a.status != 403 || code != "forbidden" || !strings.Contains(msg, "Mandanten") {
			t.Errorf("foreign tenant: %d %s %s", a.status, code, msg)
		}
		if a := c.do("GET", "/trhttp/public", nil, ""); a.status != 200 || !strings.Contains(a.body, "user=<null> tenant=ACME") {
			t.Errorf("anonymous: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trhttp/public", func(r *http.Request) { r.Header.Set("X-Tenant", "BETA") }, ""); a.status != 403 {
			t.Errorf("anonymous in another tenant: %d", a.status)
		}

		// The status decides about the commit, an exception rolls back.
		if a := c.do("POST", "/trhttp/notes?text=a", anna, ""); a.status != 201 {
			t.Fatalf("201: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trhttp/notes?text=b&status=409", anna, ""); a.status != 409 {
			t.Errorf("409: %d", a.status)
		}
		a = c.do("POST", "/trhttp/notes?text=c&fail=1", anna, "")
		if code, _ := errorCode(t, a); a.status != 500 || code != "internal-error" || strings.Contains(a.body, "INDEX") {
			t.Errorf("exception: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trhttp/notes", anna, ""); a.body != "1" {
			t.Errorf("notes stored: %s", a.body)
		}

		a = c.do("GET", "/trhttp/slow", anna, "")
		if code, _ := errorCode(t, a); a.status != 503 || code != "unavailable" || a.header.Get("Retry-After") == "" {
			t.Errorf("time limit: %d %s", a.status, a.body)
		}
		if err := k.DeactivateHTTPService(ctx, "/trhttp/echo"); err != nil {
			t.Fatal(err)
		}
		if a := c.do("GET", "/trhttp/echo", anna, ""); a.status != 404 {
			t.Errorf("after deactivation: %d", a.status)
		}
	})
}

// TestHandlerReferences rejects service nodes whose handler classes are
// missing or unusable, and upgrades that remove a used handler class.
func TestHandlerReferences(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		for _, c := range []struct{ class, source, want string }{
			{"/TRHTTP/MISSING", "", "/TRHTTP/MISSING is not a class"},
			{"/TRHTTP/PLAIN", "public class /TRHTTP/PLAIN\nend\n", "does not implement /TRBASE/HTTP_HANDLER"},
			{"/TRHTTP/ARGS", "public class /TRHTTP/ARGS implements /TRBASE/HTTP_HANDLER\n\n  public constructor(x: INT4)\n  end\n\n" +
				"  public method HANDLE(request: /TRBASE/HTTP_REQUEST, response: /TRBASE/HTTP_RESPONSE) returns BOOL\n    return true\n  end\n\nend\n",
				"needs a public constructor without parameters"},
		} {
			dir := testutil.CopyDir(t, httpPackage(t))
			testutil.WriteFile(t, dir, "schema/http-services/bad.json", []byte(`{"path":"bad","texts":{"en":{"label":"Bad"}},"handlers":[{"class":"`+c.class+`"}]}`))
			if c.source != "" {
				testutil.WriteFile(t, dir, "src/"+strings.TrimPrefix(c.class, "/TRHTTP/")+".class", []byte(c.source))
			}
			_, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{})
			var ce *rterr.CompileError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "schema/http-services/bad.json") {
				t.Errorf("%s: %v", c.class, err)
			}
		}
		// A class of a package that is not required fails the namespace check.
		dir := testutil.CopyDir(t, httpPackage(t))
		testutil.WriteFile(t, dir, "schema/http-services/bad.json", []byte(`{"path":"bad","texts":{"en":{"label":"Bad"}},"handlers":[{"class":"/TRFIN/COMPANY_API"}]}`))
		if _, err := kernel.LoadDir(dir); err == nil || !strings.Contains(err.Error(), "/TRFIN/COMPANY_API") {
			t.Errorf("foreign package: %v", err)
		}

		// A package that uses /TRHTTP/ECHO blocks an upgrade without it.
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, httpPackage(t))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		user := filepath.Join(t.TempDir(), "TRHUSE")
		testutil.WriteFile(t, user, "manifest.json", []byte(`{"id":"TRHUSE","version":"1.0.0","requires":[{"id":"TRHTTP"}]}`))
		testutil.WriteFile(t, user, "schema/http-services/echo.json", []byte(`{"path":"echo","texts":{"en":{"label":"Echo"}},"handlers":[{"class":"/TRHTTP/ECHO"}]}`))
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, user)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		up := testutil.CopyDir(t, httpPackage(t))
		testutil.WriteFile(t, up, "manifest.json", []byte(`{ "id": "TRHTTP", "version": "1.1.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }] }`))
		testutil.WriteFile(t, up, "schema/http-services/echo.json", []byte(`{"path":"echo","texts":{"en":{"label":"Echo"}}}`))
		testutil.WriteFile(t, up, "schema/http-services/public.json", []byte(`{"path":"public","texts":{"en":{"label":"Public"}}}`))
		_ = removeFile(up, "src/ECHO.class")
		_, err := k.Upgrade(ctx, testutil.LoadDir(t, up), kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || ce.Package != "TRHUSE" || !strings.Contains(err.Error(), "/TRHTTP/ECHO is not a class") {
			t.Errorf("upgrade without a used handler: %v", err)
		}
	})
}

// TestNodeChecks requires EXECUTE on a node for requests with sign-in and
// the additional check of a node, which its subtree inherits and null
// leaves out; anonymous nodes are not checked.
func TestNodeChecks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		c := httpKernel(t, k)
		if err := k.ActivateHTTPService(ctx, "/trhttp"); err != nil {
			t.Fatal(err)
		}
		for _, u := range []string{"bob", "carl"} {
			if _, err := k.Users().Create(ctx, u, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "bob", Role: "/TRHTTP/CALLER", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		expect := func(user, path string, want int) {
			t.Helper()
			if a := c.do("GET", path, basicAuth(user, "secret"), ""); a.status != want {
				t.Errorf("%s %s: %d %s, want %d", user, path, a.status, a.body, want)
			}
		}
		expect("carl", "/trhttp/version", 403)
		expect("bob", "/trhttp/version", 200)
		expect("bob", "/trhttp/guarded", 403)
		expect("bob", "/trhttp/guarded/inner", 403)
		expect("bob", "/trhttp/guarded/open", 200)
		if a := c.do("GET", "/trhttp/public", nil, ""); a.status == 403 {
			t.Errorf("anonymous: %d", a.status)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "bob", Role: "/TRHTTP/AREA_A", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		expect("bob", "/trhttp/guarded", 200)
		expect("bob", "/trhttp/guarded/inner", 200)
		expect("carl", "/trhttp/guarded/open", 403)

		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if a := s.HTTPServices["/trhttp/guarded"].Settings.Authorization; a.Check == nil || a.Check.Fields["AREA"].Value != "A" {
			t.Errorf("stored check: %+v", a)
		}
		if a := s.HTTPServices["/trhttp/guarded/open"].Settings.Authorization; !a.Set || a.Check != nil {
			t.Errorf("stored null: %+v", a)
		}
		if a := s.HTTPServices["/trhttp/guarded/inner"].Settings.Authorization; a.Set {
			t.Errorf("stored inheritance: %+v", a)
		}
	})
}
