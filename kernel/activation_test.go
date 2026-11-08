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

// TestImplementationActivation deactivates and activates implementations
// globally and per tenant, where the setting of a tenant wins, and checks
// that another kernel on the same database sees every change with its
// next execution, without a restart.
func TestImplementationActivation(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			initialized(t, k)
			if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0")), writePackage(t, stepImplFiles("1.0.0"))}, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			for _, tenant := range []string{"ACME", "BETA"} {
				if _, err := k.Tenants().Create(ctx, tenant, model.None[string]()); err != nil {
					t.Fatal(err)
				}
			}
			other, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			steps := func(tenant, want string) {
				t.Helper()
				for _, kk := range []*kernel.Kernel{k, other} {
					expectCall(t, kk, tenant, "/TRSTEPS/RUN", "STEPS", want, `"GB"`, "12", `"x"`)
				}
			}
			impls := kernel.ImplementationOptions{By: "test"}
			acme := kernel.ImplementationOptions{Tenant: "ACME", By: "test"}
			steps("ACME", `"2 true x[A:r][C:r]"`)
			if err := k.Implementations().Deactivate(ctx, "/TRSTEPIMPL/A", impls); err != nil {
				t.Fatal(err)
			}
			steps("ACME", `"1 true x[C:r]"`)
			steps("BETA", `"1 true x[C:r]"`)
			// The setting of a tenant wins over the global one.
			if err := k.Implementations().Activate(ctx, "/TRSTEPIMPL/A", acme); err != nil {
				t.Fatal(err)
			}
			if err := k.Implementations().Deactivate(ctx, "/TRSTEPIMPL/C", acme); err != nil {
				t.Fatal(err)
			}
			steps("ACME", `"1 true x[A:r]"`)
			steps("BETA", `"1 true x[C:r]"`)
			// Reset returns to the global activation.
			if err := k.Implementations().Reset(ctx, "/TRSTEPIMPL/A", acme); err != nil {
				t.Fatal(err)
			}
			steps("ACME", `"0 false x"`)
			if err := k.Implementations().Reset(ctx, "/TRSTEPIMPL/A", impls); err != nil {
				t.Fatal(err)
			}
			steps("ACME", `"1 true x[A:r]"`)
			steps("BETA", `"2 true x[A:r][C:r]"`)

			states, err := k.Implementations().States(ctx, "")
			if err != nil || len(states) != 1 || states[0].Implementation != "/TRSTEPIMPL/C" || states[0].Tenant != "ACME" || states[0].Active || states[0].ChangedBy != "test" {
				t.Fatalf("states: %+v %v", states, err)
			}
			history, err := k.Implementations().History(ctx, "/TRSTEPIMPL/A")
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, c := range history {
				kinds = append(kinds, c.Kind+" "+c.Tenant.Or("-")+" "+c.OldState.Or("-")+">"+c.NewState.Or("-"))
			}
			if got := strings.Join(kinds, ", "); got != "installed - ->active, deactivate - ->inactive, activate ACME ->active, reset ACME active>-, reset - inactive>-" {
				t.Errorf("history: %s", got)
			}
			// Unknown implementations and tenants.
			var nf *rterr.NotFound
			if err := k.Implementations().Deactivate(ctx, "/TRSTEPIMPL/NOPE", impls); !errors.As(err, &nf) || nf.Kind != "implementation" {
				t.Errorf("unknown implementation: %v", err)
			}
			if err := k.Implementations().Deactivate(ctx, "/TRSTEPIMPL/A", kernel.ImplementationOptions{Tenant: "NOPE"}); !errors.As(err, &nf) || nf.Kind != "tenant" {
				t.Errorf("unknown tenant: %v", err)
			}

			// Upgrades keep the settings; uninstalling removes them and
			// records the removal.
			if _, err := k.Upgrade(ctx, writePackage(t, stepImplFiles("1.1.0")), kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			steps("ACME", `"1 true x[A:r]"`)
			if _, err := k.Uninstall(ctx, "TRSTEPIMPL", kernel.Options{Confirm: true}); err != nil {
				t.Fatal(err)
			}
			if states, _ := k.Implementations().States(ctx, ""); len(states) != 0 {
				t.Errorf("settings after uninstalling: %+v", states)
			}
			history, _ = k.Implementations().History(ctx, "/TRSTEPIMPL/C")
			if last := history[len(history)-1]; last.Kind != kernel.ChangeImplementationRemoved || last.ChangedBy != "kernel" {
				t.Errorf("removal: %+v", last)
			}
			steps("ACME", `"0 false x"`)
			consistent(t, k)
		})
	}
}

// TestImplementationOverlapOnActivation rejects activating an
// implementation of a single extension point that would match the same
// filter values as another active one, globally and in a tenant.
func TestImplementationOverlapOnActivation(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0")), writePackage(t, stepImplFiles("1.0.0"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		if err := k.Implementations().Deactivate(ctx, "/TRSTEPIMPL/STRICT_DE", kernel.ImplementationOptions{}); err != nil {
			t.Fatal(err)
		}
		// With /TRSTEPIMPL/STRICT_DE inactive another implementation for DE
		// can be installed.
		files := map[string]string{
			"manifest.json": `{ "id": "TRSTEPOTHER", "version": "1.0.0", "requires": [{ "id": "TRBASE" }, { "id": "TRSTEPS" }], "languageVersion": 1 }`,
			"src/DE.class":  strings.ReplaceAll(priceClass("DE"), "TRSTEPIMPL", "TRSTEPOTHER"),
			"schema/extension-implementations/STRICT.json": strings.ReplaceAll(implementation("STRICT", "/TRSTEPS/STRICT", "DE",
				`, "filters": { "COUNTRY": [{ "pattern": "D*" }] }`), "TRSTEPIMPL", "TRSTEPOTHER"),
		}
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		var ov *rterr.ImplementationOverlap
		err := k.Implementations().Activate(ctx, "/TRSTEPIMPL/STRICT_DE", kernel.ImplementationOptions{})
		if !errors.As(err, &ov) || ov.Tenant != "" || !strings.Contains(err.Error(), "/TRSTEPIMPL/STRICT_DE and /TRSTEPOTHER/STRICT both match when active everywhere: COUNTRY = DE") {
			t.Fatalf("global activation: %v", err)
		}
		err = k.Implementations().Activate(ctx, "/TRSTEPIMPL/STRICT_DE", kernel.ImplementationOptions{Tenant: "ACME"})
		if !errors.As(err, &ov) || ov.Tenant != "ACME" || !strings.Contains(err.Error(), "both match in tenant ACME: COUNTRY = DE") {
			t.Fatalf("activation in a tenant: %v", err)
		}
		// Deactivating the other one in the tenant first allows it there.
		if err := k.Implementations().Deactivate(ctx, "/TRSTEPOTHER/STRICT", kernel.ImplementationOptions{Tenant: "ACME"}); err != nil {
			t.Fatal(err)
		}
		if err := k.Implementations().Activate(ctx, "/TRSTEPIMPL/STRICT_DE", kernel.ImplementationOptions{Tenant: "ACME"}); err != nil {
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "STRICT", `"DE 1"`, `"DE"`)
		// Reinstalling the other package in a way that overlaps in the
		// tenant is rejected at installation.
		if _, err := k.Implementations().States(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := k.Implementations().Reset(ctx, "/TRSTEPOTHER/STRICT", kernel.ImplementationOptions{Tenant: "ACME"}); !errors.As(err, &ov) {
			t.Fatalf("reset to an overlap: %v", err)
		}
	})
}
