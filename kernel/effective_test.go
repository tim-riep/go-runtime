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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// TestEffectiveAuthorizations resolves the roles of a user and drops the
// cached result after every kind of change, also when another kernel on the
// same database makes it.
func TestEffectiveAuthorizations(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			withRoleKernel(t, k)
			other, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()

			today := time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC)
			values := map[string]string{"COMPANY_ID": "C1", "COUNTRY": "DE", "ACTIVITY": "READ"}
			effective := func() (bool, int64) {
				t.Helper()
				before := k.EffectiveComputed()
				e, err := k.EffectiveAuthorizations(ctx, "anna", "ACME", today)
				if err != nil {
					t.Fatal(err)
				}
				return e.Allows("/TRFIN/COMPANY", values), k.EffectiveComputed() - before
			}
			step := func(name string, change func(), wantAllowed bool) {
				t.Helper()
				change()
				allowed, computed := effective()
				if allowed != wantAllowed || computed != 1 {
					t.Errorf("%s: allowed %v (want %v), computed %d times", name, allowed, wantAllowed, computed)
				}
				if _, computed := effective(); computed != 0 {
					t.Errorf("%s: not cached", name)
				}
			}
			step("no roles", func() {}, false)
			var clerk kernel.RoleAssignment
			step("assigned by another kernel", func() {
				a, _, err := other.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/COMPANY_CLERK", Tenant: "ACME"}, kernel.RoleOptions{})
				if err != nil {
					t.Fatal(err)
				}
				clerk = a
			}, true)
			step("locked", func() {
				if err := other.Users().Lock(ctx, "anna"); err != nil {
					t.Fatal(err)
				}
			}, false)
			step("unlocked", func() {
				if err := other.Users().Unlock(ctx, "anna"); err != nil {
					t.Fatal(err)
				}
			}, true)
			step("unassigned", func() {
				if err := other.Roles().Unassign(ctx, clerk.ID, kernel.RoleOptions{}); err != nil {
					t.Fatal(err)
				}
			}, false)
			step("local role", func() {
				importRole(t, other, `{ "name": "/LOCAL/READER", "texts": { "en": { "label": "Reader" } }, "authorizations": [{ "object": "/TRFIN/COMPANY",
					"fields": { "COMPANY_ID": [{ "pattern": "*" }], "COUNTRY": [{ "value": "AT" }], "ACTIVITY": [{ "value": "READ" }] } }] }`)
				if _, _, err := other.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/LOCAL/READER", Tenant: "ACME"}, kernel.RoleOptions{}); err != nil {
					t.Fatal(err)
				}
			}, false)
			step("local role changed", func() {
				importRole(t, other, `{ "name": "/LOCAL/READER", "texts": { "en": { "label": "Reader" } }, "authorizations": [{ "object": "/TRFIN/COMPANY",
					"fields": { "COMPANY_ID": [{ "pattern": "*" }], "COUNTRY": [{ "pattern": "D*" }], "ACTIVITY": [{ "value": "READ" }] } }] }`)
			}, true)
			step("upgrade with a new field", func() {
				if _, err := other.Upgrade(ctx, finWithField(t, "1.1.0", true), kernel.Options{}); err != nil {
					t.Fatal(err)
				}
			}, false)
			e, _ := k.EffectiveAuthorizations(ctx, "anna", "ACME", today)
			if len(e.NotEffective) != 1 || e.NotEffective[0].Origin.Role != "/LOCAL/READER" {
				t.Errorf("not effective after the upgrade: %+v", e.NotEffective)
			}
			step("tenant removed from the user", func() {
				if err := other.Users().RemoveTenant(ctx, "anna", "ACME"); err != nil {
					t.Fatal(err)
				}
			}, false)
		})
	}
}

// TestAllAuthorizationsSignIn records every sign-in of a user with all
// authorizations.
func TestAllAuthorizationsSignIn(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withRoleKernel(t, k)
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: model.AllAuthorizationsRole, Tenant: "ACME"}, kernel.RoleOptions{By: "cli"}); err != nil {
			t.Fatal(err)
		}
		e, err := k.EffectiveAuthorizations(ctx, "anna", "ACME", time.Now())
		if err != nil || !e.All || !e.Allows("/TROTTO/SYNC", nil) {
			t.Fatalf("all authorizations: %+v %v", e, err)
		}
		if e, _ := k.EffectiveAuthorizations(ctx, "anna", "BETA", time.Now()); e.All {
			t.Error("all authorizations in another tenant")
		}
		if err := k.ActivateHTTPService(ctx, "/trfin"); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(k.HTTPHandler())
		defer srv.Close()
		for _, tenant := range []string{"ACME", "ACME", "BETA"} {
			r, _ := http.NewRequest("GET", srv.URL+"/trfin/companies", nil)
			r.SetBasicAuth("anna", "secret")
			r.Header.Set("X-Tenant", tenant)
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		}
		history, _ := k.Roles().History(ctx, model.AllAuthorizationsRole, "")
		var kinds []string
		for _, h := range history {
			kinds = append(kinds, h.Kind+" "+h.Tenant.Or("")+" "+h.ChangedBy)
		}
		if got := strings.Join(kinds, ", "); got != "assign ACME cli, all-authorizations-sign-in ACME anna, all-authorizations-sign-in ACME anna" {
			t.Errorf("history: %s", got)
		}
	})
}
