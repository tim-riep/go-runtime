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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// roleAdminKernel installs TRADMIN with the users admin, who maintains
// roles, and bob, who has PLAIN and BUNDLE, in the tenant ACME.
func roleAdminKernel(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	pkg := testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "roles", "TRADMIN"))
	if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"admin", "bob"} {
		if _, err := k.Users().Create(ctx, u, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
	}
	assign := func(user, role string, from, to model.Optional[time.Time]) {
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME", ValidFrom: from, ValidTo: to}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	none := model.None[time.Time]()
	assign("admin", "/TRADMIN/ADMIN", none, none)
	assign("bob", "/TRADMIN/BUNDLE", none, none)
	assign("bob", "/TRADMIN/PLAIN", none, none)
	// Expired and future assignments do not count.
	assign("bob", "/TRADMIN/ADMIN", none, model.Some(time.Now().AddDate(0, 0, -2)))
	assign("bob", "/TRADMIN/ADMIN", model.Some(time.Now().AddDate(0, 0, 2)), none)
}

func runAs(t *testing.T, k *kernel.Kernel, user, method string, args ...string) (string, error) {
	t.Helper()
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	opts := kernel.ExecOptions{Tenant: "ACME", Language: "en"}
	if user != "" {
		opts.User = model.Some(user)
	}
	out, err := k.Run(context.Background(), opts, "/TRADMIN/ROLES", method, raw)
	return string(out), err
}

func mustRunAs(t *testing.T, k *kernel.Kernel, user, method string, args ...string) string {
	t.Helper()
	out, err := runAs(t, k, user, method, args...)
	if err != nil {
		t.Fatalf("%s as %s: %v", method, user, err)
	}
	return out
}

func notAuthorized(err error) bool {
	var ex *vm.Exception
	return errors.As(err, &ex) && ex.Class == model.NotAuthorizedClass
}

func TestEffectiveRoles(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		roleAdminKernel(t, k)
		want := `["/TRADMIN/BUNDLE via -","/TRADMIN/EMPTY via /TRADMIN/BUNDLE","/TRADMIN/PLAIN via -","/TRADMIN/PLAIN via /TRADMIN/BUNDLE"]`
		if got := mustRunAs(t, k, "bob", "MINE"); got != want {
			t.Errorf("roles of bob:\n%s\nwant\n%s", got, want)
		}
		if got := mustRunAs(t, k, "", "MINE"); got != `[]` {
			t.Errorf("without user: %s", got)
		}
		if err := k.Users().Lock(context.Background(), "bob"); err != nil {
			t.Fatal(err)
		}
		if got := mustRunAs(t, k, "bob", "MINE"); got != `[]` {
			t.Errorf("locked: %s", got)
		}
	})
}

func TestRoleAdministration(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		roleAdminKernel(t, k)

		// Without the authorizations of role maintenance.
		if _, err := runAs(t, k, "bob", "CREATE", `"/LOCAL/READER"`, `false`); !notAuthorized(err) {
			t.Fatalf("create without authorization: %v", err)
		}
		if got := mustRunAs(t, k, "bob", "NAMES"); got != `[]` {
			t.Errorf("roles bob may read: %s", got)
		}
		if _, err := runAs(t, k, "", "NAMES"); !notAuthorized(err) {
			t.Errorf("without user: %v", err)
		}

		// With them.
		if got := mustRunAs(t, k, "admin", "NAMES"); !strings.Contains(got, `"/TRADMIN/PLAIN TRADMIN"`) || !strings.Contains(got, `"/RT/ALL_AUTHORIZATIONS RT"`) {
			t.Errorf("roles: %s", got)
		}
		mustRunAs(t, k, "admin", "CREATE", `"/LOCAL/READER"`, `false`)
		if got := mustRunAs(t, k, "admin", "EFFECT", `"/LOCAL/READER"`); got != `["/TRADMIN/THING from /LOCAL/READER"]` {
			t.Errorf("effect: %s", got)
		}
		if got := mustRunAs(t, k, "admin", "EFFECT", `"/TRADMIN/BUNDLE"`); got != `["/TRADMIN/THING from /TRADMIN/PLAIN"]` {
			t.Errorf("effect of the composite role: %s", got)
		}
		// An exception after the creation rolls it back with the execution.
		if _, err := runAs(t, k, "admin", "CREATE", `"/LOCAL/ROLLED_BACK"`, `true`); err == nil {
			t.Fatal("no exception")
		}
		if r, _ := k.Roles().Get(ctx, "/LOCAL/ROLLED_BACK"); r != nil {
			t.Error("the creation was not rolled back")
		}
		if _, err := runAs(t, k, "admin", "CREATE", `"/TRADMIN/OTHER"`, `false`); err == nil || !strings.Contains(err.Error(), "local roles are named /LOCAL/") {
			t.Errorf("package namespace: %v", err)
		}

		// Assignments with the history of changes.
		id := strings.Trim(mustRunAs(t, k, "admin", "ASSIGN", `"bob"`, `"/LOCAL/READER"`, `"ACME"`), `"`)
		if got := mustRunAs(t, k, "admin", "ASSIGNED", `"bob"`); !strings.Contains(got, `"/LOCAL/READER in ACME"`) {
			t.Errorf("assignments: %s", got)
		}
		if _, err := runAs(t, k, "admin", "ASSIGN", `"bob"`, `"/RT/ALL_AUTHORIZATIONS"`, `"ACME"`); err == nil || !strings.Contains(err.Error(), "only on the command line") {
			t.Errorf("all authorizations: %v", err)
		}
		if got := mustRunAs(t, k, "admin", "OBJECTS", `"bob"`, `"ACME"`); !strings.Contains(got, `"/TRADMIN/THING from /TRADMIN/PLAIN via /TRADMIN/BUNDLE"`) ||
			!strings.Contains(got, `"/TRADMIN/THING from /LOCAL/READER via -"`) {
			t.Errorf("effective authorizations: %s", got)
		}
		if got := mustRunAs(t, k, "admin", "DELETE", `"/LOCAL/READER"`, `false`); got != `"confirm"` {
			t.Errorf("delete an assigned role: %s", got)
		}
		if got := mustRunAs(t, k, "admin", "DELETE", `"/TRADMIN/PLAIN"`, `true`); !strings.Contains(got, "package roles change with their package") {
			t.Errorf("delete a package role: %s", got)
		}
		mustRunAs(t, k, "admin", "UNASSIGN", `"`+id+`"`)
		if got := mustRunAs(t, k, "admin", "DELETE", `"/LOCAL/READER"`, `false`); got != `"deleted"` {
			t.Errorf("delete: %s", got)
		}
		want := `["create-role by admin","assign by admin","unassign by admin","delete-role by admin"]`
		if got := mustRunAs(t, k, "admin", "HISTORY", `"/LOCAL/READER"`); got != want {
			t.Errorf("history:\n%s\nwant\n%s", got, want)
		}
		if _, err := runAs(t, k, "bob", "HISTORY", `"/LOCAL/READER"`); !notAuthorized(err) {
			t.Errorf("history without authorization: %v", err)
		}

		// Users without password and tokens.
		if got := mustRunAs(t, k, "admin", "USERS"); got != `["admin 1","bob 1"]` {
			t.Errorf("users: %s", got)
		}
		if got := mustRunAs(t, k, "bob", "USERS"); got != `[]` {
			t.Errorf("users bob may read: %s", got)
		}
	})
}
