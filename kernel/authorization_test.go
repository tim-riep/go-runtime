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
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// authPackage writes a package TRAUTH that requires TRFIN, with the object
// /TRAUTH/DOC whose fields and activities are given as JSON.
func authPackage(t *testing.T, version, fields, activities string) *model.Package {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRAUTH", "version": "`+version+`", "requires": [{ "id": "TRBASE" }, { "id": "TRFIN" }] }`))
	doc := `{ "name": "/TRAUTH/DOC", "texts": { "en": { "label": "Documents" } }, "fields": ` + fields
	if activities != "" {
		doc += `, "activities": ` + activities
	}
	testutil.WriteFile(t, dir, "schema/authorization-objects/DOC.json", []byte(doc+" }"))
	return testutil.LoadDir(t, dir)
}

// TestAuthorizationObjectLifecycle installs authorization objects, reads
// them back and applies the rules for upgrades.
func TestAuthorizationObjectLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		o := s.AuthorizationObjects["/TRFIN/COMPANY"]
		if o == nil || o.Owner != "TRFIN" || len(o.Fields) != 3 || o.Fields[1] != (model.AuthorizationField{Name: "COUNTRY", DataElement: model.Some("/TRBASE/COUNTRY")}) ||
			!o.Fields[2].IsActivity() || strings.Join(o.Activities, ",") != "READ,CREATE,UPDATE,DELETE,EXPORT" || o.Texts["de"].Label != "Stammdaten Firma" {
			t.Fatalf("read back: %+v", o)
		}
		if s.AuthorizationObjects["/TROTTO/SYNC"] == nil || s.AuthorizationObjects["/TRFIN/STATUS_SCOPE"].HasActivity() {
			t.Fatal("objects of the examples")
		}
		de, err := k.DataElement(ctx, "/TRBASE/COUNTRY")
		if err != nil || !strings.Contains(strings.Join(de.UsedBy, " "), "/TRFIN/COMPANY.COUNTRY") {
			t.Errorf("uses of /TRBASE/COUNTRY: %v %v", de, err)
		}
		act, err := k.DataElement(ctx, model.ActivityDomain)
		if err != nil || strings.Join(act.UsedBy, " ") != "/TRBASE/CONFIGURATION.ACTIVITY /TRBASE/HTTP_SERVICE.ACTIVITY /TRBASE/ROLE_ADMIN.ACTIVITY /TRBASE/ROLE_ASSIGNMENT.ACTIVITY /TRBASE/USER_ADMIN.ACTIVITY /TRFIN/COMPANY.ACTIVITY /TROTTO/SYNC.ACTIVITY" {
			t.Errorf("uses of ACTIVITY: %v %v", act, err)
		}

		base := `[{ "name": "CODE", "dataElement": "/TRFIN/COMPANY" }, { "name": "ACTIVITY" }]`
		if _, err := k.Install(ctx, []*model.Package{authPackage(t, "1.0.0", base, `["READ", "UPDATE"]`)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		// A new field and a new activity are allowed.
		grown := `[{ "name": "CODE", "dataElement": "/TRFIN/COMPANY" }, { "name": "ACTIVITY" }, { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }]`
		p, err := k.Upgrade(ctx, authPackage(t, "1.1.0", grown, `["READ", "UPDATE", "APPROVE"]`), kernel.Options{})
		if err != nil || !strings.Contains(planText(p), "alter-authorization-object /TRAUTH/DOC (field COUNTRY added, activity APPROVE added)") {
			t.Fatalf("growing upgrade: %v\n%s", err, planText(p))
		}
		for _, c := range []struct{ name, fields, activities, want string }{
			{"field removed", base, `["READ", "UPDATE", "APPROVE"]`, "field COUNTRY removed"},
			{"type changed", `[{ "name": "CODE", "dataElement": "/TRBASE/NAME" }, { "name": "ACTIVITY" }, { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }]`,
				`["READ", "UPDATE", "APPROVE"]`, "field CODE: type data element /TRFIN/COMPANY -> data element /TRBASE/NAME"},
			{"activity removed", grown, `["READ", "APPROVE"]`, "activity UPDATE removed"},
		} {
			_, err := k.Upgrade(ctx, authPackage(t, "1.2.0", c.fields, c.activities), kernel.Options{})
			var dc *rterr.DestructiveChange
			if !errors.As(err, &dc) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// The object removed: destructive too, and allowed explicitly.
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRAUTH", "version": "1.3.0", "requires": [{ "id": "TRFIN" }] }`))
		_, err = k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) || !strings.Contains(err.Error(), "drop-authorization-object /TRAUTH/DOC") {
			t.Fatalf("object removed: %v", err)
		}
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{AllowDestructive: true}); err != nil {
			t.Fatal(err)
		}
		if s, _ := k.Schema(ctx); s.AuthorizationObjects["/TRAUTH/DOC"] != nil {
			t.Error("the object is still there")
		}
		consistent(t, k)
	})
}

// TestAuthorizationTypeOfOtherPackage rejects an upgrade of a package
// whose domain becomes a type that a field of another package's
// authorization object cannot have.
func TestAuthorizationTypeOfOtherPackage(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		lib := func(version, typ string) *model.Package {
			dir := t.TempDir()
			testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRALIB", "version": "`+version+`" }`))
			testutil.WriteFile(t, dir, "schema/domains/CODE.json", []byte(`{ "name": "/TRALIB/CODE", "description": "Code", "type": `+typ+` }`))
			return testutil.LoadDir(t, dir)
		}
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRAUSE", "version": "1.0.0", "requires": [{ "id": "TRALIB" }] }`))
		testutil.WriteFile(t, dir, "schema/authorization-objects/CODES.json", []byte(`{ "name": "/TRAUSE/CODES", "texts": { "en": { "label": "Codes" } },
			"fields": [{ "name": "CODE", "domain": "/TRALIB/CODE" }] }`))
		if _, err := k.Install(ctx, []*model.Package{lib("1.0.0", `"CHAR", "length": 5`), testutil.LoadDir(t, dir)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		_, err := k.Upgrade(ctx, lib("1.1.0", `"DEC", "length": 5, "decimals": 0`), kernel.Options{AllowDestructive: true})
		if err == nil || !strings.Contains(err.Error(), "authorization object /TRAUSE/CODES, field CODE: type DEC(5,0) is not allowed") {
			t.Fatalf("expected the upgrade to fail with the object, got %v", err)
		}
	})
}

func planText(p *plan.Plan) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range p.Changes {
		b.WriteString(c.String() + "\n")
	}
	return b.String()
}
