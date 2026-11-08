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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

func compiled(p *plan.Plan, id string) bool {
	if p == nil {
		return false
	}
	for _, c := range p.Changes {
		if c.Kind == plan.CompilePackage && c.Name == id {
			return true
		}
	}
	return false
}

// TestImplementationWithoutRecompiling installs and removes an
// implementation without compiling the package of the extension point
// again; the next execution sees the change.
func TestImplementationWithoutRecompiling(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "STEPS", `"0 false x"`, `"GB"`, "12", `"x"`)
		p, err := k.Install(ctx, []*model.Package{writePackage(t, stepImplFiles("1.0.0"))}, kernel.Options{})
		if err != nil || compiled(p, "TRSTEPS") || !compiled(p, "TRSTEPIMPL") {
			t.Fatalf("installation: %v\n%s", err, planText(p))
		}
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "STEPS", `"2 true x[A:r][C:r]"`, `"GB"`, "12", `"x"`)
		p, err = k.Uninstall(ctx, "TRSTEPIMPL", kernel.Options{Confirm: true})
		if err != nil || compiled(p, "TRSTEPS") || !strings.Contains(planText(p), "drop-implementation /TRSTEPIMPL/A") {
			t.Fatalf("removal: %v\n%s", err, planText(p))
		}
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "STEPS", `"0 false x"`, `"GB"`, "12", `"x"`)
	})
}

// TestInterfaceChange compiles the implementing packages again when the
// interface of an extension point changes: an implementation that misses
// a new method rejects the upgrade, one that extends the open base class
// of the defining package takes the new method from it.
func TestInterfaceChange(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		inherit := map[string]string{
			"manifest.json": `{ "id": "TRINHERIT", "version": "1.0.0", "requires": [{ "id": "TRBASE" }, { "id": "TRPOINT" }], "languageVersion": 1 }`,
			"src/CHECKS.class": "public class /TRINHERIT/CHECKS extends /TRPOINT/DEFAULT_CHECKS\n\n" +
				"  public override method CHECK(code: STRING) returns STRING\n    return \"inherited \" + code\n  end\n\nend\n",
			"schema/extension-implementations/CHECKS.json": `{ "name": "/TRINHERIT/CHECKS", "description": { "en": "Inherited" },
				"extensionPoint": "/TRPOINT/CHECKS", "class": "/TRINHERIT/CHECKS", "filters": { "COUNTRY": [{ "value": "AT" }] } }`,
		}
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, pointFiles("1.0.0", countryFilter)), writePackage(t, inherit),
			writePackage(t, implFiles("TRIMPL", "1.0.0", map[string]string{"A": `, "filters": { "COUNTRY": [{ "value": "DE" }] }`}))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		grown := pointFiles("1.1.0", countryFilter)
		grown["src/CHECKS.interface"] = "public interface /TRPOINT/CHECKS\n\n  method CHECK(code: STRING) returns STRING\n  method LABEL() returns STRING\n\nend\n"
		grown["src/DEFAULT_CHECKS.class"] = "public open class /TRPOINT/DEFAULT_CHECKS implements /TRPOINT/CHECKS\n\n" +
			"  public open method CHECK(code: STRING) returns STRING\n    return code\n  end\n\n" +
			"  public open method LABEL() returns STRING\n    return \"default\"\n  end\n\nend\n"
		_, err := k.Upgrade(ctx, writePackage(t, grown), kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || ce.Package != "TRIMPL" || !strings.Contains(err.Error(), "/TRIMPL/A_CHECKS does not implement LABEL of /TRPOINT/CHECKS") {
			t.Fatalf("upgrade with an implementation that misses the method: %v", err)
		}
		if _, err := k.Uninstall(ctx, "TRIMPL", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		p, err := k.Upgrade(ctx, writePackage(t, grown), kernel.Options{})
		if err != nil || !strings.Contains(planText(p), "compile-package TRINHERIT (requires TRPOINT)") {
			t.Fatalf("upgrade: %v\n%s", err, planText(p))
		}
	})
}

// TestGenerationOfRunningExecution keeps the implementations of the
// generation an execution started with, also when another one is
// installed meanwhile; the next execution sees it.
func TestGenerationOfRunningExecution(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		args := []json.RawMessage{json.RawMessage(`"GB"`), json.RawMessage("12"), json.RawMessage(`"x"`)}
		var before, during []byte
		err := k.Execute(ctx, kernel.ExecOptions{Tenant: "ACME"}, func(ctx context.Context, m *vm.Machine) error {
			var err error
			if before, err = m.CallJSON(ctx, "/TRSTEPS/RUN", "STEPS", args); err != nil {
				return err
			}
			if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepImplFiles("1.0.0"))}, kernel.Options{}); err != nil {
				return err
			}
			during, err = m.CallJSON(ctx, "/TRSTEPS/RUN", "STEPS", args)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != `"0 false x"` || string(during) != `"0 false x"` {
			t.Errorf("running execution: %s, %s", before, during)
		}
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "STEPS", `"2 true x[A:r][C:r]"`, `"GB"`, "12", `"x"`)
	})
}
