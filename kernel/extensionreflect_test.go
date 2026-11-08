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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// TestExtensionPointDescription describes extension points with their
// implementations, the activation and the code that runs them, in Go and
// through /TRBASE/REFLECTOR, and traces the selection for a user.
func TestExtensionPointDescription(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0")), writePackage(t, stepImplFiles("1.0.0"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		if err := k.Implementations().Deactivate(ctx, "/TRSTEPIMPL/C", kernel.ImplementationOptions{Tenant: "ACME"}); err != nil {
			t.Fatal(err)
		}
		d, err := k.ExtensionPoint(ctx, "/TRSTEPS/STEPS")
		if err != nil {
			t.Fatal(err)
		}
		if d.Package != "TRSTEPS" || d.Use != model.UseMultiple || d.Interface != "/TRSTEPS/STEPS" || len(d.Filters) != 2 || d.Filters[0].Labels["de"].Medium != "Land" ||
			d.Filters[1].Type != model.Int8 || d.Description["de"] != "Schritte" || len(d.Implementations) != 3 || d.Implementations[0].Name != "/TRSTEPIMPL/B" {
			t.Fatalf("description: %+v", d.ExtensionPoint)
		}
		c := d.Implementations[2]
		if c.Name != "/TRSTEPIMPL/C" || c.Activation == nil || !c.Activation.Active || c.Activation.Global.Valid() || c.Activation.Tenants["ACME"] {
			t.Fatalf("activation of C: %+v", c.Activation)
		}
		var uses []string
		for _, u := range d.CodeUses {
			uses = append(uses, u.Member.Or(""))
		}
		if got := strings.Join(uses, ","); got != ",COUNTRY,NO,REMEMBER,APPEND," {
			t.Errorf("uses: %s", got)
		}
		js, _ := json.Marshal(d)
		if !strings.Contains(string(js), `"filters":{"COUNTRY":[{"pattern":"*"}],"NO":[{"pattern":"1*"}]}`) {
			t.Errorf("JSON: %s", js)
		}
		list, err := k.ExtensionPoints(ctx, "TRSTEPS")
		if err != nil || len(list) != 3 || list[0].Name != "/TRSTEPS/PRICE" {
			t.Fatalf("list: %v %v", list, err)
		}
		if _, err := k.ExtensionPoints(ctx, "NOPE"); err == nil {
			t.Error("extension points of a package that is not installed")
		}

		// In code, with the activation in the tenant of the execution.
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "REFLECT", `"MULTIPLE /TRSTEPS/STEPS 2 INT8 Steps /TRSTEPIMPL/B:true:2 /TRSTEPIMPL/A:true:3 /TRSTEPIMPL/C:false:2"`)

		// A trace of the selection for a user.
		if _, err := k.Users().Create(ctx, "anna", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.StartImplementationTrace(ctx, "anna", kernel.TraceOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}, "/TRSTEPS/RUN", "STEPS", `"GB"`, "12", `"x"`); err != nil {
			t.Fatal(err)
		}
		tr, err := k.ImplementationTrace(ctx, "anna", "")
		if err != nil || tr.Trace == nil || tr.Ended || len(tr.Entries) != 1 {
			t.Fatalf("trace: %+v %v", tr, err)
		}
		e := tr.Entries[0]
		var reasons []string
		for _, s := range e.Selection {
			reasons = append(reasons, s.Implementation+": "+s.Reason)
		}
		if e.ExtensionPoint != "/TRSTEPS/STEPS" || e.Filters["COUNTRY"] != "GB" || e.Filters["NO"] != "12" || e.Site.Method.Or("") != "STEPS" ||
			strings.Join(reasons, "; ") != "/TRSTEPIMPL/B: the filter field COUNTRY does not match; /TRSTEPIMPL/A: selected; /TRSTEPIMPL/C: inactive in tenant ACME" {
			t.Fatalf("entry: %+v %s", e, strings.Join(reasons, "; "))
		}
		// Executions without the user record nothing.
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, "/TRSTEPS/RUN", "STEPS", `"GB"`, "12", `"x"`); err != nil {
			t.Fatal(err)
		}
		if err := k.StopImplementationTrace(ctx, "anna"); err != nil {
			t.Fatal(err)
		}
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}, "/TRSTEPS/RUN", "STEPS", `"GB"`, "12", `"x"`); err != nil {
			t.Fatal(err)
		}
		if tr, _ := k.ImplementationTrace(ctx, "anna", ""); tr.Trace != nil || len(tr.Entries) != 1 {
			t.Fatalf("after stop: %+v", tr)
		}
	})
}

// TestReflectedExtensionPointLiteral rejects a literal that names an
// extension point that does not exist when the code is compiled.
func TestReflectedExtensionPointLiteral(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		files := stepsFiles("1.0.0")
		files["src/RUN.class"] = strings.Replace(files["src/RUN.class"], `EXTENSION_POINT("/TRSTEPS/STEPS")`, `EXTENSION_POINT("/TRSTEPS/NOPE")`, 1)
		_, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{})
		if err == nil || !strings.Contains(err.Error(), "extension point /TRSTEPS/NOPE does not exist") {
			t.Fatalf("unknown extension point: %v", err)
		}
	})
}
