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

// partnerKernel installs the package TRBP from its sources with the test
// package TRBPTEST, the tenant ACME, the intervals 01 (internal) and XX
// (external) of /TRBP/PARTNER, the groupings 0001 and 0002 (internal) and
// EXT1 (external), the user admin with all roles of TRBP and the user
// clara, who works with customers of 0001. It returns a client of the
// service /TRBP/PARTNER_SERVICE.
func partnerKernel(t *testing.T, k *kernel.Kernel) (client, func(user string) func(*http.Request)) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	root := testutil.ModuleRoot(t)
	pkgs := []*model.Package{
		testutil.LoadPackageDir(t, filepath.Join(root, "packages", "TRBP")),
		testutil.LoadDir(t, filepath.Join(root, "testdata", "partners", "TRBPTEST")),
	}
	if _, err := k.Install(ctx, pkgs, kernel.Options{}); err != nil {
		t.Fatalf("%v\n%s", err, objectErrors(err))
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	for _, in := range []kernel.NumberInterval{
		{Object: "/TRBP/PARTNER", ID: "01", From: "1", To: "4999999999"},
		{Object: "/TRBP/PARTNER", ID: "XX", From: "5000000000", To: "9999999999", External: true},
	} {
		if err := k.AddNumberInterval(ctx, "ACME", in, kernel.NumberRangeOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	roles := map[string][]string{
		"admin": {"/TRBP/PARTNER_ADMIN", "/TRBP/CONFIGURATION", "/TRBP/ODATA_USER"},
		"clara": {"/TRBPTEST/CLERK", "/TRBP/ODATA_USER"},
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
	if err := k.ActivateService(ctx, "/TRBP/PARTNER_SERVICE", "test"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(k.HTTPHandler())
	t.Cleanup(srv.Close)
	c := client{t, srv.URL}
	as := func(user string) func(*http.Request) {
		return func(r *http.Request) {
			r.SetBasicAuth(user, "secret")
			r.Header.Set("X-Tenant", "ACME")
			r.Header.Set("Content-Type", "application/json")
		}
	}
	for _, g := range []string{
		`{"GROUPING":"0001","INTERVAL":"01","EXTERNAL":false}`,
		`{"GROUPING":"0002","INTERVAL":"01","EXTERNAL":false}`,
		`{"GROUPING":"EXT1","INTERVAL":"XX","EXTERNAL":true}`,
	} {
		if a := c.do("POST", "/rt/odata/trbp/partner_service/Groupings", as("admin"), g); a.status != 201 {
			t.Fatalf("grouping %s: %d %s", g, a.status, a.body)
		}
	}
	if a := c.do("POST", "/rt/odata/trbp/partner_service/GroupingTexts", as("admin"), `{"GROUPING":"0001","LANGUAGE":"en","LABEL":"Customers"}`); a.status != 201 {
		t.Fatalf("grouping text: %d %s", a.status, a.body)
	}
	return c, as
}

// runPartner calls a method of /TRBPTEST/API as user and returns its
// result.
func runPartner(t *testing.T, k *kernel.Kernel, user, method string, args ...string) string {
	t.Helper()
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := k.Run(context.Background(), opts, "/TRBPTEST/API", method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var s string
	if json.Unmarshal(out, &s) == nil {
		return s
	}
	return string(out)
}

func wantIn(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("without %s:\n%s", w, got)
		}
	}
}

func TestPartnerNumbering(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		partnerKernel(t, k)
		got := runPartner(t, k, "admin", "CREATE", `"0001"`, `"ORGANIZATION"`, `"Acme"`, "null", `["/TRBP/GENERAL"]`)
		wantIn(t, got, `"KEY":"{\"PARTNER\":\"0000000001\"}"`, `"FAILURES":[]`)
		got = runPartner(t, k, "admin", "CREATE", `"0002"`, `"PERSON"`, `"Miller"`, "null", `[]`)
		wantIn(t, got, `"KEY":"{\"PARTNER\":\"0000000002\"}"`, `"FAILURES":[]`)
		if got := runPartner(t, k, "", "ROLES"); got != "0000000001:/TRBP/GENERAL:0001" {
			t.Errorf("roles take the grouping of their partner: %s", got)
		}

		// External groupings take the number of the caller from their
		// external interval.
		got = runPartner(t, k, "admin", "CREATE", `"EXT1"`, `"ORGANIZATION"`, `"Outside"`, `"5000000001"`, `["/TRBP/GENERAL"]`)
		wantIn(t, got, `"FAILURES":[]`)
		wantIn(t, runPartner(t, k, "", "ROLES"), "5000000001:/TRBP/GENERAL:EXT1")
		got = runPartner(t, k, "admin", "CREATE", `"EXT1"`, `"ORGANIZATION"`, `"Outside"`, `"0000000099"`, `[]`)
		wantIn(t, got, `"REASON":"VALIDATION"`, `"FIELD":"PARTNER"`, "the number 0000000099 is not in the interval XX of the grouping EXT1")
		got = runPartner(t, k, "admin", "CREATE", `"EXT1"`, `"ORGANIZATION"`, `"Outside"`, "null", `[]`)
		wantIn(t, got, `"REASON":"VALIDATION"`, "takes the numbers of its partners from outside")
		got = runPartner(t, k, "admin", "CREATE", `"0001"`, `"ORGANIZATION"`, `"Inside"`, `"0000000777"`, `[]`)
		wantIn(t, got, `"REASON":"VALIDATION"`, "gives the numbers of its partners; leave the number out")
		got = runPartner(t, k, "admin", "CREATE", `"NONE"`, `"ORGANIZATION"`, `"Nowhere"`, "null", `[]`)
		wantIn(t, got, "the grouping NONE does not exist")
	})
}

func TestPartnerCategoriesAndRoles(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		partnerKernel(t, k)
		got := runPartner(t, k, "admin", "CREATE", `"0001"`, `"ORGANIZATION"`, `""`, "null", `[]`)
		wantIn(t, got, `"FIELD":"NAME1"`, "an organization needs a name")
		got = runPartner(t, k, "admin", "CREATE", `"0001"`, `"ORGANIZATION"`, `"Acme"`, "null", `["/TRBPTEST/EMPLOYEE"]`)
		wantIn(t, got, `"FIELD":"ROLE"`, "a partner of the category ORGANIZATION cannot take the role /TRBPTEST/EMPLOYEE")
		got = runPartner(t, k, "admin", "CREATE", `"0001"`, `"ORGANIZATION"`, `"Acme"`, "null", `["/TRBPTEST/NOPE"]`)
		wantIn(t, got, "the partner role /TRBPTEST/NOPE does not exist")

		// Roles of the test package for a person.
		got = runPartner(t, k, "admin", "CREATE", `"0001"`, `"PERSON"`, `"Miller"`, "null", `["/TRBPTEST/EMPLOYEE", "/TRBPTEST/CUSTOMER"]`)
		wantIn(t, got, `"FAILURES":[]`)
		var saved struct {
			CREATED []struct{ ENTITY, KEY string }
		}
		if err := json.Unmarshal([]byte(got), &saved); err != nil || len(saved.CREATED) == 0 {
			t.Fatalf("%s %v", got, err)
		}
		var key struct{ PARTNER string }
		_ = json.Unmarshal([]byte(saved.CREATED[0].KEY), &key)
		if got := runPartner(t, k, "", "ROLES"); !strings.Contains(got, key.PARTNER+":/TRBPTEST/CUSTOMER:0001") || !strings.Contains(got, key.PARTNER+":/TRBPTEST/EMPLOYEE:0001") {
			t.Errorf("roles: %s", got)
		}
		got = runPartner(t, k, "admin", "RENAME", `"`+key.PARTNER+`"`, `"Miller Ltd"`, `"Miller"`)
		wantIn(t, got, `"FIELD":"NAME1"`, "a person has no name of an organization")
	})
}

func TestPartnerAuthorizations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c, as := partnerKernel(t, k)
		// The root checks the grouping and any role, the roles their role
		// and the grouping of their partner.
		got := runPartner(t, k, "clara", "CREATE", `"0001"`, `"ORGANIZATION"`, `"Acme"`, "null", `["/TRBPTEST/CUSTOMER"]`)
		wantIn(t, got, `"FAILURES":[]`, `"KEY":"{\"PARTNER\":\"0000000001\"}"`)
		got = runPartner(t, k, "clara", "CREATE", `"0002"`, `"ORGANIZATION"`, `"Other"`, "null", `[]`)
		wantIn(t, got, `"REASON":"NOT_AUTHORIZED"`)
		got = runPartner(t, k, "clara", "CREATE", `"0001"`, `"ORGANIZATION"`, `"Acme"`, "null", `["/TRBP/GENERAL"]`)
		wantIn(t, got, `"ENTITY":"/TRBP/PARTNER_ROLES"`, `"REASON":"NOT_AUTHORIZED"`)
		got = runPartner(t, k, "clara", "ADD_ROLE", `"0000000001"`, `"/TRBP/GENERAL"`)
		wantIn(t, got, `"REASON":"NOT_AUTHORIZED"`)

		got = runPartner(t, k, "admin", "CREATE", `"0002"`, `"ORGANIZATION"`, `"Other"`, "null", `["/TRBPTEST/CUSTOMER"]`)
		wantIn(t, got, `"FAILURES":[]`)
		got = runPartner(t, k, "admin", "ADD_ROLE", `"0000000001"`, `"/TRBP/GENERAL"`)
		wantIn(t, got, `"FAILURES":[]`)

		// Reading filters by grouping and role.
		a := c.do("GET", "/rt/odata/trbp/partner_service/PartnerRoles", as("clara"), "")
		if a.status != 200 || !strings.Contains(a.body, `"ROLE":"/TRBPTEST/CUSTOMER","GROUPING":"0001"`) || strings.Contains(a.body, "/TRBP/GENERAL") || strings.Contains(a.body, `"0002"`) {
			t.Errorf("roles of clara: %d %s", a.status, a.body)
		}
		a = c.do("GET", "/rt/odata/trbp/partner_service/Partners", as("clara"), "")
		if a.status != 200 || !strings.Contains(a.body, "0000000001") || strings.Contains(a.body, `"0002"`) {
			t.Errorf("partners of clara: %d %s", a.status, a.body)
		}
		a = c.do("GET", "/rt/odata/trbp/partner_service/PartnerRoles", as("admin"), "")
		if a.status != 200 || strings.Count(a.body, `"ROLE":`) != 3 {
			t.Errorf("roles of admin: %d %s", a.status, a.body)
		}
	})
}

func TestPartnerOverOData(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c, as := partnerKernel(t, k)
		a := c.do("POST", "/rt/odata/trbp/partner_service/Partners", as("admin"),
			`{"GROUPING":"0001","CATEGORY":"ORGANIZATION","NAME1":"Web","PartnerRoles":[{"ROLE":"/TRBP/GENERAL"}]}`)
		if a.status != 201 || !strings.HasSuffix(a.header.Get("Location"), "Partners('0000000001')") {
			t.Fatalf("create: %d %s %s", a.status, a.header.Get("Location"), a.body)
		}
		if a := c.do("GET", "/rt/odata/trbp/partner_service/PartnerRoles", as("admin"), ""); a.status != 200 || !strings.Contains(a.body, `"PARTNER":"0000000001","ROLE":"/TRBP/GENERAL","GROUPING":"0001"`) {
			t.Errorf("roles: %d %s", a.status, a.body)
		}
		a = c.do("POST", "/rt/odata/trbp/partner_service/Partners", as("admin"), `{"GROUPING":"0001","CATEGORY":"PERSON","NAME1":"Web"}`)
		if a.status != 400 || !strings.Contains(a.body, "a person needs a last name") {
			t.Errorf("person with a name of an organization: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/rt/odata/trbp/partner_service/Roles", as("clara"), ""); a.status != 200 || !strings.Contains(a.body, "/TRBP/GENERAL") || !strings.Contains(a.body, "/TRBPTEST/EMPLOYEE") {
			t.Errorf("partner roles: %d %s", a.status, a.body)
		}
	})
}
