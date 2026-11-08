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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

const localClerk = `{
  "name": "/LOCAL/CLERK",
  "texts": { "en": { "label": "Local clerk" } },
  "authorizations": [{ "object": "/TRFIN/COMPANY", "fields": {
    "COMPANY_ID": [{ "value": "C1" }], "COUNTRY": [{ "pattern": "*" }], "ACTIVITY": [{ "value": "READ" }] } }]
}`

func importRole(t *testing.T, k *kernel.Kernel, js string) {
	t.Helper()
	if _, err := k.Roles().Import(context.Background(), []byte(js), "role.json", kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
}

// finWithField returns TRFIN in a new version whose /TRFIN/COMPANY has the
// field REGION, which its checks in code leave out; with roles, its roles
// grant it.
func finWithField(t *testing.T, version string, roles bool) *model.Package {
	t.Helper()
	dir := testutil.CopyExample(t, "TRFIN")
	path := filepath.Join(dir, "schema", "authorization-objects", "COMPANY.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `{ "name": "ACTIVITY" }`, `{ "name": "ACTIVITY" }, { "name": "REGION", "dataElement": "/TRBASE/NAME" }`, 1))
	testutil.WriteFile(t, dir, "schema/authorization-objects/COMPANY.json", data)
	regionInChecks(t, dir)
	if roles {
		for _, f := range []string{"COMPANY_CLERK", "COMPANY_ADMIN"} {
			p := filepath.Join(dir, "schema", "roles", f+".json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			b = []byte(strings.Replace(string(b), `"COMPANY_ID": [{ "pattern": "*" }],`, `"COMPANY_ID": [{ "pattern": "*" }], "REGION": [{ "pattern": "*" }],`, 1))
			testutil.WriteFile(t, dir, "schema/roles/"+f+".json", b)
		}
	}
	p := testutil.LoadDir(t, dir)
	p.Manifest.Version = version
	return p
}

// regionInChecks adds REGION = any to the checks of /TRFIN/COMPANY in the
// sources of TRFIN in dir.
func regionInChecks(t *testing.T, dir string) {
	t.Helper()
	for _, f := range []string{"COMPANY_API.class", "COMPANY_INTAKE.class"} {
		b, err := os.ReadFile(filepath.Join(dir, "src", f))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		var out []string
		for _, l := range lines {
			if trimmed := strings.TrimLeft(l, " "); strings.HasPrefix(trimmed, "ACTIVITY = ") {
				out = append(out, l[:len(l)-len(trimmed)]+"REGION = any")
			}
			out = append(out, l)
		}
		testutil.WriteFile(t, dir, "src/"+f, []byte(strings.Join(out, "\n")))
	}
}

// TestRoleLifecycle installs package roles, maintains local roles and
// follows changes of authorization objects.
func TestRoleLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		roles := k.Roles()
		clerk, err := roles.Get(ctx, "/TRFIN/COMPANY_CLERK")
		if err != nil || len(clerk.Authorizations) != 1 || len(clerk.Authorizations[0].Problems) != 0 || clerk.Authorizations[0].AllActivities {
			t.Fatalf("clerk: %+v %v", clerk, err)
		}
		if admin, err := roles.Get(ctx, "/TRFIN/COMPANY_ADMIN"); err != nil || !admin.Authorizations[0].AllActivities {
			t.Errorf("admin: %+v %v", admin, err)
		}

		// A package whose role does not fit its object is not installed.
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRROLE", "version": "1.0.0", "requires": [{ "id": "TRFIN" }] }`))
		testutil.WriteFile(t, dir, "schema/roles/READER.json", []byte(`{ "name": "/TRROLE/READER", "texts": { "en": { "label": "Reader" } },
			"authorizations": [{ "object": "/TRFIN/COMPANY", "fields": { "COMPANY_ID": [{ "pattern": "*" }], "ACTIVITY": [{ "value": "READ" }] } }] }`))
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{}); err == nil || !strings.Contains(err.Error(), "field COUNTRY: is missing") {
			t.Fatalf("role without a field: %v", err)
		}
		testutil.WriteFile(t, dir, "schema/roles/READER.json", []byte(`{ "name": "/TRROLE/READER", "texts": { "en": { "label": "Reader" } },
			"authorizations": [{ "object": "/TRFIN/COMPANY", "fields": { "COMPANY_ID": [{ "pattern": "*" }], "COUNTRY": [{ "value": "DE" }], "ACTIVITY": [{ "value": "READ" }] } }] }`))
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}

		// Local roles: namespace, values, create, export.
		var ia *rterr.InvalidArgument
		if _, err := roles.Import(ctx, []byte(strings.Replace(localClerk, "/LOCAL/CLERK", "/TRFIN/MINE", 1)), "role.json", kernel.RoleOptions{}); !errors.As(err, &ia) {
			t.Errorf("local role outside /LOCAL/: %v", err)
		}
		if _, err := roles.Import(ctx, []byte(strings.Replace(localClerk, `"READ"`, `"APPROVE"`, 1)), "role.json", kernel.RoleOptions{}); err == nil ||
			!strings.Contains(err.Error(), "APPROVE is not allowed for /TRFIN/COMPANY") {
			t.Errorf("activity not allowed: %v", err)
		}
		importRole(t, k, localClerk)
		var conf *rterr.ConfirmationRequired
		if err := roles.Create(ctx, &model.Role{Name: "/LOCAL/CLERK", Texts: map[string]model.Label{"en": {Label: "x"}}}, kernel.RoleOptions{}); err == nil {
			t.Error("a second role with the same name")
		}
		out, err := roles.Export(ctx, "/LOCAL/CLERK")
		if err != nil || !strings.Contains(string(out), `"value": "C1"`) {
			t.Fatalf("export: %s %v", out, err)
		}
		importRole(t, k, `{ "name": "/LOCAL/OPERATIONS", "texts": { "en": { "label": "Operations" } }, "roles": ["/LOCAL/CLERK", "/TRFIN/COMPANY_CLERK", "/TROTTO/SYNC_OPERATOR"] }`)
		importRole(t, k, `{ "name": "/LOCAL/OTTO", "texts": { "en": { "label": "OTTO" } }, "authorizations": [
			{ "object": "/TROTTO/SYNC", "fields": { "COUNTRY": [{ "pattern": "*" }], "ACTIVITY": [{ "pattern": "*" }] } },
			{ "object": "/TRFIN/STATUS_SCOPE", "fields": { "STATUS": [{ "value": "ACTIVE" }], "COUNTRY": [{ "pattern": "*" }] } }] }`)
		if err := roles.Create(ctx, &model.Role{Name: "/LOCAL/NESTED", Texts: map[string]model.Label{"en": {Label: "x"}}, Composite: true, Roles: []string{"/LOCAL/OPERATIONS"}},
			kernel.RoleOptions{}); err == nil || !strings.Contains(err.Error(), "/LOCAL/OPERATIONS is a composite role") {
			t.Errorf("composite role in composite role: %v", err)
		}

		// A new field of an object: the roles of TRFIN must grant it; other
		// roles grant nothing on the object until maintained.
		if _, err := k.Upgrade(ctx, finWithField(t, "1.1.0", false), kernel.Options{}); err == nil || !strings.Contains(err.Error(), "field REGION: is missing") {
			t.Fatalf("upgrade with roles that miss the new field: %v", err)
		}
		p, err := k.Upgrade(ctx, finWithField(t, "1.1.0", true), kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		text := planText(p)
		for _, want := range []string{"incomplete-authorization /LOCAL/CLERK (authorization 1 on /TRFIN/COMPANY grants nothing: field REGION: is missing",
			"incomplete-authorization /TRROLE/READER"} {
			if !strings.Contains(text, want) {
				t.Errorf("plan lacks %q:\n%s", want, text)
			}
		}
		if d, err := roles.Get(ctx, "/LOCAL/CLERK"); err != nil || len(d.Authorizations[0].Problems) != 1 {
			t.Errorf("local role after the upgrade: %+v %v", d, err)
		}
		list, _ := roles.List(ctx)
		for _, r := range list {
			if (r.Name == "/LOCAL/CLERK" || r.Name == "/TRROLE/READER") != (r.Incomplete == 1) {
				t.Errorf("list: %+v", r)
			}
		}
		// Maintained, the role is complete again.
		importRole(t, k, strings.Replace(localClerk, `"COUNTRY": [{ "pattern": "*" }],`, `"COUNTRY": [{ "pattern": "*" }], "REGION": [{ "pattern": "*" }],`, 1))
		if d, _ := roles.Get(ctx, "/LOCAL/CLERK"); len(d.Authorizations[0].Problems) != 0 {
			t.Errorf("maintained role: %+v", d.Authorizations[0].Problems)
		}

		// Deleting a role in a composite role needs a confirmation.
		if err := roles.Delete(ctx, "/LOCAL/CLERK", kernel.RoleOptions{}); !errors.As(err, &conf) {
			t.Fatalf("delete a used role: %v", err)
		}
		if err := roles.Delete(ctx, "/LOCAL/CLERK", kernel.RoleOptions{Confirm: true, By: "test"}); err != nil {
			t.Fatal(err)
		}
		if ops, _ := roles.Get(ctx, "/LOCAL/OPERATIONS"); strings.Join(ops.Role.Roles, ",") != "/TRFIN/COMPANY_CLERK,/TROTTO/SYNC_OPERATOR" {
			t.Errorf("composite role after the deletion: %v", ops.Role.Roles)
		}
		var nf *rterr.NotFound
		if err := roles.Delete(ctx, "/TRFIN/COMPANY_CLERK", kernel.RoleOptions{}); errors.As(err, &nf) || err == nil {
			t.Errorf("delete a package role: %v", err)
		}

		// Uninstalling TROTTO removes its role from the local composite role
		// and the authorization on its object from the local role.
		if _, err := k.Uninstall(ctx, "TRROLE", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		p, err = k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		if text := planText(p); !strings.Contains(text, "alter-role /LOCAL/OPERATIONS") || !strings.Contains(text, "alter-role /LOCAL/OTTO") {
			t.Errorf("uninstall plan:\n%s", text)
		}
		s, _ := k.Schema(ctx)
		if r := s.Roles["/LOCAL/OPERATIONS"]; r == nil || strings.Join(r.Roles, ",") != "/TRFIN/COMPANY_CLERK" {
			t.Errorf("composite role after the uninstall: %+v", r)
		}
		if r := s.Roles["/LOCAL/OTTO"]; r == nil || len(r.Authorizations) != 1 || r.Authorizations[0].Object != "/TRFIN/STATUS_SCOPE" {
			t.Errorf("local role after the uninstall: %+v", r)
		}

		// The history has every change, with old and new state.
		history, err := roles.History(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, h := range history {
			kinds = append(kinds, h.Kind+" "+h.Role.Or("")+" "+h.ChangedBy)
		}
		want := []string{"create-role /LOCAL/CLERK test", "create-role /LOCAL/OPERATIONS test", "create-role /LOCAL/OTTO test", "update-role /LOCAL/CLERK test",
			"update-role /LOCAL/OPERATIONS test", "delete-role /LOCAL/CLERK test", "role-changed /LOCAL/OPERATIONS kernel", "role-changed /LOCAL/OTTO kernel"}
		if strings.Join(kinds, "\n") != strings.Join(want, "\n") {
			t.Errorf("history:\n%s", strings.Join(kinds, "\n"))
		}
		var old map[string]any
		if err := json.Unmarshal([]byte(history[3].OldState.Or("")), &old); err != nil || old["name"] != "/LOCAL/CLERK" || !history[3].NewState.Valid() {
			t.Errorf("states of an update: %+v %v", history[3], err)
		}
		consistent(t, k)
	})
}

var _ = plan.AlterRole
