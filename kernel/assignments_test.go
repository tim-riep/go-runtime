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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func day(s string) model.Optional[time.Time] {
	t, err := time.Parse(model.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return model.Some(t)
}

// withRoleKernel installs the examples and sets up the tenants ACME and
// BETA and the user anna, who works in both.
func withRoleKernel(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ACME", "BETA", "GAMMA"} {
		if _, err := k.Tenants().Create(ctx, id, model.None[string]()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.Users().Create(ctx, "anna", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
		t.Fatal(err)
	}
	if err := k.Users().AddTenant(ctx, "anna", "BETA"); err != nil {
		t.Fatal(err)
	}
}

func assign(t *testing.T, k *kernel.Kernel, user, role, tenant string, from, to model.Optional[time.Time]) (kernel.RoleAssignment, []kernel.RoleAssignment) {
	t.Helper()
	a, overlaps, err := k.Roles().Assign(context.Background(), kernel.Assignment{User: user, Role: role, Tenant: tenant, ValidFrom: from, ValidTo: to},
		kernel.RoleOptions{By: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return a, overlaps
}

// TestRoleAssignments assigns roles in tenants and removes them with
// roles, packages, tenants of users and tenants.
func TestRoleAssignments(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		withRoleKernel(t, k)
		roles := k.Roles()
		none := model.None[time.Time]()
		var ia *rterr.InvalidArgument
		if _, _, err := roles.Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/COMPANY_CLERK", Tenant: "GAMMA"}, kernel.RoleOptions{}); !errors.As(err, &ia) ||
			!strings.Contains(err.Error(), "user anna does not work in tenant GAMMA") {
			t.Errorf("tenant of another user: %v", err)
		}
		var nf *rterr.NotFound
		if _, _, err := roles.Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/NOPE", Tenant: "ACME"}, kernel.RoleOptions{}); !errors.As(err, &nf) {
			t.Errorf("unknown role: %v", err)
		}
		if _, _, err := roles.Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/COMPANY_CLERK", Tenant: "ACME", ValidFrom: day("2026-02-01"), ValidTo: day("2026-01-01")},
			kernel.RoleOptions{}); !errors.As(err, &ia) {
			t.Errorf("last day before the first: %v", err)
		}
		first, _ := assign(t, k, "anna", "/TRFIN/COMPANY_CLERK", "ACME", day("2026-01-01"), day("2026-06-30"))
		_, overlaps := assign(t, k, "anna", "/TRFIN/COMPANY_CLERK", "ACME", day("2026-06-30"), none)
		if len(overlaps) != 1 || overlaps[0].ID != first.ID {
			t.Errorf("overlap on the last day: %+v", overlaps)
		}
		if _, overlaps := assign(t, k, "anna", "/TRFIN/COMPANY_CLERK", "ACME", none, day("2025-12-31")); len(overlaps) != 0 {
			t.Errorf("no overlap: %+v", overlaps)
		}
		assign(t, k, "anna", "/TRFIN/COMPANY_ADMIN", "BETA", none, none)
		assign(t, k, "anna", "/TROTTO/SYNC_OPERATOR", "ACME", none, none)
		list, err := roles.Assignments(ctx, "anna", "ACME")
		if err != nil || len(list) != 4 {
			t.Fatalf("assignments in ACME: %+v %v", list, err)
		}
		if err := roles.Unassign(ctx, first.ID, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		if err := roles.Unassign(ctx, first.ID, kernel.RoleOptions{}); !errors.As(err, &nf) {
			t.Errorf("unassign twice: %v", err)
		}

		// An upgrade that removes an assigned role is rejected.
		fin := testutil.CopyExample(t, "TRFIN")
		if err := os.Remove(filepath.Join(fin, "schema", "roles", "COMPANY_ADMIN.json")); err != nil {
			t.Fatal(err)
		}
		pkg := testutil.LoadDir(t, fin)
		pkg.Manifest.Version = "1.1.0"
		if _, err := k.Upgrade(ctx, pkg, kernel.Options{AllowDestructive: true}); err == nil ||
			!strings.Contains(err.Error(), "the operation removes roles that are assigned to users: /TRFIN/COMPANY_ADMIN has 1 assignments") {
			t.Errorf("upgrade without an assigned role: %v", err)
		}

		// Uninstalling TROTTO removes the assignments of its role.
		p, err := k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(planText(p), "delete-rows /RT/ROLE_ASSIGNMENTS (1 role assignments)") {
			t.Errorf("uninstall plan:\n%s", planText(p))
		}
		// Losing the tenant BETA removes the assignment there.
		if err := k.Users().RemoveTenant(ctx, "anna", "BETA", kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		if list, _ := roles.Assignments(ctx, "anna", ""); len(list) != 2 || list[0].Tenant != "ACME" || list[1].Tenant != "ACME" {
			t.Errorf("assignments left: %+v", list)
		}
		// Deleting ACME removes the rest.
		if _, err := k.Tenants().Delete(ctx, "ACME", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if list, _ := roles.Assignments(ctx, "anna", ""); len(list) != 0 {
			t.Errorf("assignments after deleting the tenant: %+v", list)
		}

		history, err := roles.History(ctx, "", "anna")
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, h := range history {
			kinds = append(kinds, h.Kind+" "+h.Role.Or("")+" "+h.Tenant.Or("")+" "+h.ChangedBy)
		}
		want := []string{
			"assign /TRFIN/COMPANY_CLERK ACME test", "assign /TRFIN/COMPANY_CLERK ACME test", "assign /TRFIN/COMPANY_CLERK ACME test",
			"assign /TRFIN/COMPANY_ADMIN BETA test", "assign /TROTTO/SYNC_OPERATOR ACME test",
			"unassign /TRFIN/COMPANY_CLERK ACME test",
			"assignment-removed /TROTTO/SYNC_OPERATOR ACME kernel",
			"assignment-removed /TRFIN/COMPANY_ADMIN BETA test",
			"assignment-removed /TRFIN/COMPANY_CLERK ACME kernel", "assignment-removed /TRFIN/COMPANY_CLERK ACME kernel",
		}
		if strings.Join(kinds, "\n") != strings.Join(want, "\n") {
			t.Errorf("history:\n%s", strings.Join(kinds, "\n"))
		}
		if !history[5].OldState.Valid() || history[5].NewState.Valid() || !history[0].NewState.Valid() {
			t.Errorf("states: %+v %+v", history[0], history[5])
		}
		consistent(t, k)
	})
}
