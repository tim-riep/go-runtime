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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

const allRole = model.AllAuthorizationsRole

// TestAllAuthorizationsRoleDefined checks the role in a new database: a
// role of the kernel with texts in the languages set up, which nothing can
// change.
func TestAllAuthorizationsRoleDefined(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		r := s.Roles[allRole]
		if r == nil || r.Owner != model.Kernel || r.Composite || len(r.Authorizations) != 0 || r.Texts["en"].Label != "All authorizations" ||
			!r.Texts["en"].Description.Valid() || len(r.Texts) != 1 {
			t.Fatalf("role after init: %+v", r)
		}
		if _, err := k.Languages().Add(ctx, "de", ""); err != nil {
			t.Fatal(err)
		}
		if s, _ := k.Schema(ctx); s.Roles[allRole].Texts["de"].Label != "Alle Berechtigungen" {
			t.Errorf("German text: %+v", s.Roles[allRole].Texts)
		}

		roles := k.Roles()
		var ia *rterr.InvalidArgument
		role := `{ "name": "/RT/ALL_AUTHORIZATIONS", "texts": { "en": { "label": "Mine" } }, "authorizations": [] }`
		if _, err := roles.Import(ctx, []byte(role), "all.json", kernel.RoleOptions{}); !errors.As(err, &ia) {
			t.Errorf("import with the name: %v", err)
		}
		if err := roles.Update(ctx, &model.Role{Name: allRole, Texts: map[string]model.Label{"en": {Label: "x"}}}, kernel.RoleOptions{}); !errors.As(err, &ia) {
			t.Errorf("update: %v", err)
		}
		if err := roles.Delete(ctx, allRole, kernel.RoleOptions{Confirm: true}); !errors.As(err, &ia) || !strings.Contains(err.Error(), "role of the kernel") {
			t.Errorf("delete: %v", err)
		}
		if err := roles.Create(ctx, &model.Role{Name: "/LOCAL/WITH_ALL", Texts: map[string]model.Label{"en": {Label: "x"}}, Composite: true, Roles: []string{allRole}},
			kernel.RoleOptions{}); err == nil || !strings.Contains(err.Error(), "is no member of composite roles") {
			t.Errorf("local composite role with it: %v", err)
		}

		// A package cannot define roles in the namespace of the kernel.
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRSNEAK", "version": "1.0.0" }`))
		testutil.WriteFile(t, dir, "schema/roles/ALL_AUTHORIZATIONS.json", []byte(role))
		_, err = kernel.LoadDir(dir)
		var nv *rterr.NamespaceViolation
		if !errors.As(err, &nv) || nv.Name != allRole {
			t.Errorf("package with a role of the kernel: %v", err)
		}
		d, err := roles.Get(ctx, allRole)
		// It covers the objects of TRBASE, installed with the kernel.
		if err != nil || !d.All || len(d.Authorizations) != 3 || d.Authorizations[0].Authorization.Object != "/TRBASE/ROLE_ADMIN" || !d.Authorizations[0].AllActivities {
			t.Errorf("covered objects: %+v %v", d, err)
		}
	})
}
