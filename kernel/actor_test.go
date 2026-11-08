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
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestActorChecks checks user and role functions called on behalf of an
// actor, and filters lists to what the actor may read.
func TestActorChecks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withRoleKernel(t, k)
		for _, u := range []string{"alex", "bob", "rita"} {
			if _, err := k.Users().Create(ctx, u, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := k.Roles().Import(ctx, []byte(`{"name":"/LOCAL/USER_DESK","texts":{"en":{"label":"User desk"}},"authorizations":[
			{"object":"/TRBASE/USER_ADMIN","fields":{"USER_NAME":[{"pattern":"a*"}],"ACTIVITY":[{"value":"READ"},{"value":"UPDATE"}]}},
			{"object":"/TRBASE/ROLE_ADMIN","fields":{"ROLE_NAME":[{"pattern":"/TRFIN/*"}],"ACTIVITY":[{"value":"READ"}]}},
			{"object":"/TRBASE/ROLE_ASSIGNMENT","fields":{"ROLE_NAME":[{"value":"/TRFIN/COMPANY_CLERK"}],"TENANT":[{"value":"ACME"}],
				"ACTIVITY":[{"value":"CREATE"},{"value":"READ"}]}}]}`), "desk.json", kernel.RoleOptions{}); err != nil {
			t.Fatal(err)
		}
		assign(t, k, "rita", "/LOCAL/USER_DESK", "ACME", model.None[time.Time](), model.None[time.Time]())
		rita := kernel.Actor{User: "rita", Tenant: "ACME"}
		users, roles := k.Users().As(rita), k.Roles().As(rita)
		denied := func(name string, err error, object string) {
			t.Helper()
			var na *rterr.NotAuthorized
			if !errors.As(err, &na) || na.Object != object || na.User != "rita" {
				t.Errorf("%s: %v", name, err)
			}
		}

		if _, err := users.Get(ctx, "anna"); err != nil {
			t.Errorf("get anna: %v", err)
		}
		_, err := users.Get(ctx, "bob")
		denied("get bob", err, kernel.UserAdminObject)
		_, err = users.Create(ctx, "amy", "secret", kernel.UserOptions{})
		denied("create", err, kernel.UserAdminObject)
		if err := users.Lock(ctx, "alex"); err != nil {
			t.Errorf("lock alex: %v", err)
		}
		denied("lock bob", users.Lock(ctx, "bob"), kernel.UserAdminObject)
		if list, err := users.List(ctx); err != nil || len(list) != 2 || list[0].Name != "alex" || list[1].Name != "anna" {
			t.Errorf("users: %v %v", list, err)
		}

		a, _, err := roles.Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/COMPANY_CLERK", Tenant: "ACME"}, kernel.RoleOptions{})
		if err != nil || a.CreatedBy != "rita" {
			t.Errorf("assign: %+v %v", a, err)
		}
		_, _, err = roles.Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/COMPANY_ADMIN", Tenant: "ACME"}, kernel.RoleOptions{})
		denied("assign admin", err, kernel.RoleAssignmentObject)
		denied("unassign", roles.Unassign(ctx, a.ID, kernel.RoleOptions{}), kernel.RoleAssignmentObject)
		if list, err := roles.Assignments(ctx, "", ""); err != nil || len(list) != 1 || list[0].ID != a.ID {
			t.Errorf("assignments: %+v %v", list, err)
		}
		if list, err := roles.List(ctx); err != nil || len(list) != 3 || list[0].Name != "/TRFIN/COMPANY_ADMIN" {
			t.Errorf("roles: %+v %v", list, err)
		}
		_, err = roles.Get(ctx, "/LOCAL/USER_DESK")
		denied("get role", err, kernel.RoleAdminObject)
		_, err = roles.Import(ctx, []byte(`{"name":"/LOCAL/X","texts":{"en":{"label":"X"}},"authorizations":[]}`), "x.json", kernel.RoleOptions{})
		denied("import", err, kernel.RoleAdminObject)

		// Without actor nothing is checked.
		if err := k.Roles().Unassign(ctx, a.ID, kernel.RoleOptions{}); err != nil {
			t.Errorf("unassign without actor: %v", err)
		}
		if list, err := k.Users().List(ctx); err != nil || len(list) != 4 {
			t.Errorf("all users: %d %v", len(list), err)
		}
	})
}
