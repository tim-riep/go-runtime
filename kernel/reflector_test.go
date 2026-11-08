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
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// TestReflector reads authorization objects and activities with their
// texts in the languages set up.
func TestReflector(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		o, err := k.AuthorizationObject(ctx, "/TRFIN/COMPANY")
		if err != nil {
			t.Fatal(err)
		}
		if o.Package != "TRFIN" || o.Texts["de"].Label != "Stammdaten Firma" || o.Texts["en"].Description != model.Some("Reading and changing companies by company and country") ||
			len(o.Fields) != 3 || len(o.Activities) != 5 || o.Activities[4].Code != "EXPORT" || o.Activities[4].Texts["de"].Label != "Exportieren" {
			t.Fatalf("object: %+v", o)
		}
		country := o.Fields[1]
		if country.Activity || country.DataElement != model.Some("/TRBASE/COUNTRY") || country.Domain != "/TRBASE/COUNTRY_CODE" ||
			country.Type != model.Char || country.Length != model.Some(2) || country.Labels["de"].Medium == "" || len(country.Values) != 0 {
			t.Errorf("COUNTRY: %+v", country)
		}
		act := o.Fields[2]
		if !act.Activity || act.DataElement != model.Some(model.ActivityDomain) || act.Labels["de"].Medium != "Aktivität" ||
			act.Length != model.Some(16) || len(act.Values) != 5 || act.Values[0].Value != "READ" || act.Values[0].Texts["de"].Label != "Lesen" {
			t.Errorf("ACTIVITY: %+v", act)
		}
		status, err := k.AuthorizationObject(ctx, "/TRFIN/STATUS_SCOPE")
		if err != nil {
			t.Fatal(err)
		}
		f := status.Fields[0]
		// The object has no German text; a domain field has no labels and
		// fixed values without texts per language.
		if _, ok := status.Texts["de"]; ok || status.Texts["en"].Description.Valid() || len(status.Activities) != 0 ||
			f.DataElement.Valid() || len(f.Labels) != 0 || len(f.Values) != 3 || f.Values[1].Name != model.Some("BLOCKED") || f.Values[1].Texts != nil {
			t.Errorf("STATUS_SCOPE: %+v", status)
		}
		var nf *rterr.NotFound
		if _, err := k.AuthorizationObject(ctx, "/TRFIN/NOPE"); !errors.As(err, &nf) {
			t.Errorf("unknown object: %v", err)
		}
		list, err := k.AuthorizationObjects(ctx, "TRFIN")
		if err != nil || len(list) != 2 || list[0].Name != "/TRFIN/COMPANY" || list[1].Name != "/TRFIN/STATUS_SCOPE" {
			t.Errorf("objects of TRFIN: %v %v", list, err)
		}
		if all, err := k.AuthorizationObjects(ctx, ""); err != nil || len(all) != 6 || all[5].Name != "/TROTTO/SYNC" {
			t.Errorf("all objects: %v %v", all, err)
		}
		acts, err := k.Activities(ctx)
		if err != nil || len(acts) != 13 || acts[12].Code != "ADMINISTER" || acts[12].Texts["de"].Description == "" {
			t.Errorf("activities: %v %v", acts, err)
		}
		b, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		testutil.Golden(t, "reflector-TRFIN-COMPANY.json", string(b)+"\n")
	})
}

// TestReflectorLanguages gives texts only in the languages set up.
func TestReflectorLanguages(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		acts, err := k.Activities(ctx)
		if err != nil || len(acts) != 13 || len(acts[0].Texts) != 1 || acts[0].Texts["en"].Label != "Read" {
			t.Errorf("activities in English only: %v %v", acts, err)
		}
		var ni *rterr.NotInstalled
		if _, err := k.AuthorizationObjects(ctx, "TRFIN"); !errors.As(err, &ni) {
			t.Errorf("objects of a package not installed: %v", err)
		}
	})
}

func reflPackage(t *testing.T) *model.Package {
	return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "reflector", "TRREFL"))
}

// TestReflectorInCode reads authorization objects and activities with
// /TRBASE/REFLECTOR, with texts in the language of the execution.
func TestReflectorInCode(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, append(examples(t), reflPackage(t)), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		run := func(lang, class, method string, args ...string) string {
			t.Helper()
			out, err := call(k, kernel.ExecOptions{Language: lang}, class, method, args...)
			if err != nil {
				var e *vm.Exception
				if errors.As(err, &e) {
					return e.Class + ": " + e.Text
				}
				t.Fatalf("%s.%s: %v", class, method, err)
			}
			return out
		}
		const R, F = "/TRREFL/READER", "/TRFIN/COMPANY_AUTHORIZATION"
		if got := run("de", F, "FIELDS"); got != `["COMPANY_ID: Firma","COUNTRY: Land","ACTIVITY: Aktivität"]` {
			t.Errorf("FIELDS in German = %s", got)
		}
		if got := run("fr", F, "FIELDS"); got != `["COMPANY_ID: Company","COUNTRY: Country","ACTIVITY: Activity"]` {
			t.Errorf("FIELDS in French falls back to English = %s", got)
		}
		if run("en", F, "ALLOWS", `"EXPORT"`) != "true" || run("en", F, "ALLOWS", `"APPROVE"`) != "false" {
			t.Error("ALLOWS")
		}
		var o struct {
			NAME, PACKAGE_ID string
			LABEL            *string
			DESCRIPTION      *string
			FIELDS           []struct {
				NAME, DOMAIN, TYPE string
				IS_ACTIVITY        bool
				DATA_ELEMENT       *string
				LENGTH             *int
				LABEL, HEADING     *string
				VALUES             []struct {
					VALUE       string
					NAME        *string
					DESCRIPTION *string
				}
			}
			ACTIVITIES []struct {
				CODE  string
				LABEL *string
			}
			ACTIVITY_CODES []string
		}
		if err := json.Unmarshal([]byte(run("de", R, "OBJECT", `"/TRFIN/COMPANY"`)), &o); err != nil {
			t.Fatal(err)
		}
		act := o.FIELDS[2]
		if o.PACKAGE_ID != "TRFIN" || *o.LABEL != "Stammdaten Firma" || !act.IS_ACTIVITY || *act.DATA_ELEMENT != "/RT/ACTIVITY" || *act.LENGTH != 16 ||
			act.VALUES[0].VALUE != "READ" || *act.VALUES[0].DESCRIPTION != "Lesen" || *o.ACTIVITIES[1].LABEL != "Anlegen" || len(o.ACTIVITY_CODES) != 5 {
			t.Errorf("object in German: %+v", o)
		}
		// Missing texts are null: no German text, no description, a domain field without labels.
		if err := json.Unmarshal([]byte(run("de", R, "OBJECT", `"/TRFIN/STATUS_SCOPE"`)), &o); err != nil {
			t.Fatal(err)
		}
		status := o.FIELDS[0]
		if *o.LABEL != "Companies by status" || o.DESCRIPTION != nil || status.DATA_ELEMENT != nil || status.LABEL != nil || status.HEADING != nil ||
			len(status.VALUES) != 3 || *status.VALUES[1].NAME != "BLOCKED" || len(o.ACTIVITIES) != 0 {
			t.Errorf("object without German texts: %+v", o)
		}
		if got := run("en", R, "OBJECT", `"/TRFIN/NOPE"`); got != "/TRBASE/AUTHORIZATION_OBJECT_NOT_FOUND: authorization object /TRFIN/NOPE does not exist" {
			t.Errorf("unknown object: %s", got)
		}
		if run("en", R, "EXISTS", `"/TRFIN/COMPANY"`) != "true" || run("en", R, "EXISTS", `"/TRFIN/NOPE"`) != "false" {
			t.Error("EXISTS")
		}
		if got := run("en", R, "NAMES", `"TROTTO"`); got != `["/TROTTO/SYNC"]` {
			t.Errorf("objects of TROTTO: %s", got)
		}
		if got := run("en", R, "NAMES", "null"); got != `["/TRBASE/ROLE_ADMIN","/TRBASE/ROLE_ASSIGNMENT","/TRBASE/USER_ADMIN","/TRFIN/COMPANY","/TRFIN/STATUS_SCOPE","/TROTTO/SYNC"]` {
			t.Errorf("all objects: %s", got)
		}
		if got := run("en", R, "CODES", `"/TRFIN/COMPANY"`); got != `"true false 5"` {
			t.Errorf("CODES: %s", got)
		}
		if got := run("en", R, "OTTO"); got != `"TROTTO"` {
			t.Errorf("object of a package not required: %s", got)
		}
		var acts []struct{ CODE, LABEL, DESCRIPTION string }
		if err := json.Unmarshal([]byte(run("de", R, "ACTIVITIES")), &acts); err != nil || len(acts) != 13 || acts[5].CODE != "APPROVE" || acts[5].LABEL != "Genehmigen" {
			t.Errorf("activities: %v %v", acts, err)
		}
	})
}

// TestReflectorLiterals checks AUTHORIZATION_OBJECT with a literal when
// the code compiles, records the use and rejects an upgrade that removes
// the object.
func TestReflectorLiterals(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		uses, err := k.CodeUses(ctx, "/TRFIN/COMPANY")
		found := false
		for _, u := range uses {
			found = found || u.Kind == "authorization-object" && u.Package == "TRFIN"
		}
		if err != nil || !found {
			t.Fatalf("uses of /TRFIN/COMPANY: %+v %v", uses, err)
		}
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRUSEA", "version": "1.0.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }, { "id": "TRFIN" }] }`))
		testutil.WriteFile(t, dir, "src/USE.class", []byte("public class /TRUSEA/USE\n\n  public static method M() raises /TRBASE/AUTHORIZATION_OBJECT_NOT_FOUND\n    var obj = /TRBASE/REFLECTOR.AUTHORIZATION_OBJECT(\"/TRFIN/NOPE\")\n  end\n\nend\n"))
		var ce *rterr.CompileError
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{}); !errors.As(err, &ce) || !strings.Contains(err.Error(), "authorization object /TRFIN/NOPE does not exist") {
			t.Errorf("unknown object with a literal: %v", err)
		}
		fin := testutil.CopyExample(t, "TRFIN")
		for _, f := range []string{"authorization-objects/COMPANY.json", "roles/COMPANY_CLERK.json", "roles/COMPANY_ADMIN.json"} {
			if err := os.Remove(filepath.Join(fin, "schema", filepath.FromSlash(f))); err != nil {
				t.Fatal(err)
			}
		}
		pkg := testutil.LoadDir(t, fin)
		pkg.Manifest.Version = "1.1.0"
		_, err = k.Upgrade(ctx, pkg, kernel.Options{AllowDestructive: true})
		if !errors.As(err, &ce) || ce.Package != "TRFIN" || !strings.Contains(err.Error(), "authorization object /TRFIN/COMPANY does not exist") {
			t.Errorf("upgrade without the object: %v", err)
		}
	})
}

// TestNotNullArrayComponent uses a structure whose array component is
// never null: not optional in code, empty at the start and required in
// JSON.
func TestNotNullArrayComponent(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, append(examples(t), reflPackage(t)), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		const B = "/TRREFL/BOXES"
		expectCall(t, k, "", B, "EMPTY", "0")
		expectCall(t, k, "", B, "READ", `"2"`, `"{\"LABEL\":null,\"CODES\":[\"READ\",\"SEND\"]}"`)
		expectCall(t, k, "", B, "READ", `"invalid"`, `"{\"LABEL\":null,\"CODES\":null}"`)
		expectCall(t, k, "", B, "READ", `"invalid"`, `"{\"LABEL\":\"x\"}"`)
	})
}
