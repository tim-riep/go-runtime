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
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestUsers creates users and tokens, signs them in and locks a user
// after failed logins.
func TestUsers(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		for _, id := range []string{"ACME", "BETA"} {
			if _, err := k.Tenants().Create(ctx, id, model.None[string]()); err != nil {
				t.Fatal(err)
			}
		}
		us := k.Users()
		if _, err := us.Create(ctx, "anna", "secret", kernel.UserOptions{Tenant: model.Some("ACME"), Language: model.Some("de")}); err != nil {
			t.Fatal(err)
		}
		var ia *rterr.InvalidArgument
		if _, err := us.Create(ctx, "anna", "x", kernel.UserOptions{}); !errors.As(err, &ia) {
			t.Errorf("duplicate user: %v", err)
		}
		if _, err := us.Create(ctx, "Anna Smith", "x", kernel.UserOptions{}); !errors.As(err, &ia) {
			t.Errorf("invalid name: %v", err)
		}
		if err := us.AddTenant(ctx, "anna", "BETA"); err != nil {
			t.Fatal(err)
		}
		u, err := us.Get(ctx, "anna")
		if err != nil || u.Status != "active" || u.DefaultTenant.Or("") != "ACME" || !slices.Equal(u.Tenants, []string{"ACME", "BETA"}) || u.Language.Or("") != "de" {
			t.Fatalf("user: %+v %v", u, err)
		}

		basic := func(user, pw string) (*kernel.Principal, error) {
			r := httptest.NewRequest("GET", "/", nil)
			r.SetBasicAuth(user, pw)
			return k.Authenticate(ctx, r, []string{model.AuthBasic, model.AuthToken})
		}
		if p, err := basic("anna", "secret"); err != nil || p.Name != "anna" || !slices.Equal(p.Tenants, []string{"ACME", "BETA"}) {
			t.Fatalf("basic: %+v %v", p, err)
		}

		token, info, err := k.Tokens().Create(ctx, "anna", "cli", model.Some(time.Now().Add(time.Hour)))
		if err != nil || !strings.HasPrefix(token, "rt_") {
			t.Fatalf("token: %s %v", token, err)
		}
		bearer := func(tok string) (*kernel.Principal, error) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Authorization", "Bearer "+tok)
			return k.Authenticate(ctx, r, []string{model.AuthToken})
		}
		if p, err := bearer(token); err != nil || p.Name != "anna" {
			t.Fatalf("bearer: %+v %v", p, err)
		}
		list, _ := k.Tokens().List(ctx, "anna")
		if len(list) != 1 || list[0].Label != "cli" || strings.Contains(list[0].Hash, token) {
			t.Errorf("tokens: %+v", list)
		}
		expired, _, _ := k.Tokens().Create(ctx, "anna", "old", model.Some(time.Now().Add(-time.Hour)))
		if _, err := bearer(expired); !errors.Is(err, auth.ErrInvalid) {
			t.Errorf("expired token: %v", err)
		}
		if err := k.Tokens().Revoke(ctx, info.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := bearer(token); !errors.Is(err, auth.ErrInvalid) {
			t.Errorf("revoked token: %v", err)
		}

		// Five failed logins lock the user, also for the right password.
		for range 5 {
			if _, err := basic("anna", "wrong"); !errors.Is(err, auth.ErrInvalid) {
				t.Fatal(err)
			}
		}
		if u, _ := us.Get(ctx, "anna"); u.Status != "locked" || u.FailedLogins != 5 {
			t.Fatalf("after failed logins: %+v", u)
		}
		if _, err := basic("anna", "secret"); !errors.Is(err, auth.ErrInvalid) {
			t.Error("a locked user signed in")
		}
		if err := us.Unlock(ctx, "anna"); err != nil {
			t.Fatal(err)
		}
		if _, err := basic("anna", "secret"); err != nil {
			t.Errorf("after unlock: %v", err)
		}
		if err := us.SetPassword(ctx, "anna", "new"); err != nil {
			t.Fatal(err)
		}
		if _, err := basic("anna", "secret"); !errors.Is(err, auth.ErrInvalid) {
			t.Error("the old password still works")
		}

		// Deleting a tenant removes it from its users.
		if _, err := k.Tenants().Delete(ctx, "ACME", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if u, _ := us.Get(ctx, "anna"); u.DefaultTenant.Valid() || !slices.Equal(u.Tenants, []string{"BETA"}) {
			t.Errorf("after deleting ACME: %+v", u)
		}
		if err := us.RemoveTenant(ctx, "anna", "BETA"); err != nil {
			t.Fatal(err)
		}
	})
}
