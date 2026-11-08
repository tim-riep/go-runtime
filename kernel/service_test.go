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

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

const companyService = "/TRFIN/COMPANY_SERVICE"

// installCompanies installs TRFIN and TROTTO, which extends the companies
// that the service of TRFIN offers.
func installCompanies(t *testing.T, k *kernel.Kernel) *plan.Plan {
	t.Helper()
	initialized(t, k)
	p, err := k.Install(context.Background(), []*model.Package{testutil.Example(t, "TRFIN"), testutil.Example(t, "TROTTO")}, kernel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestServiceLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		p := installCompanies(t, k)
		found := false
		for _, c := range p.Changes {
			found = found || c.Kind == plan.CreateService && c.Name == companyService
		}
		if !found {
			t.Errorf("plan without create-service: %+v", p.Changes)
		}
		list, err := k.Services(ctx, "TRFIN")
		if err != nil || len(list) != 1 || list[0].Path != "/rt/odata/trfin/company_service/" || list[0].Active {
			t.Fatalf("services: %+v, %v", list, err)
		}
		d, err := k.Service(ctx, companyService)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.EntitySets) != 4 || d.EntitySets[0].Name != "Companies" || len(d.Contained) != 1 || d.Description["de"] != "Firmen" {
			t.Errorf("service from the repository: %+v", d.Service)
		}
		var nav []string
		for _, n := range d.EntitySets[0].Type.Navigations {
			nav = append(nav, n.Name)
		}
		if len(nav) != 3 || nav[2] != "Country" {
			t.Errorf("navigations of companies: %v", nav)
		}

		if err := k.ActivateService(ctx, companyService, "anna"); err != nil {
			t.Fatal(err)
		}
		if d, _ := k.Service(ctx, companyService); !d.Active || d.ChangedBy.Or("") != "anna" {
			t.Errorf("activated: %+v", d)
		}
		// An upgrade keeps the activation.
		next := testutil.Example(t, "TRFIN")
		next.Manifest.Version = "1.0.1"
		if _, err := k.Upgrade(ctx, next, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if d, _ := k.Service(ctx, companyService); !d.Active {
			t.Error("upgrade lost the activation")
		}
		if err := k.DeactivateService(ctx, companyService, ""); err != nil {
			t.Fatal(err)
		}
		if d, _ := k.Service(ctx, companyService); d.Active {
			t.Error("still active")
		}
		var nf *rterr.NotFound
		if err := k.ActivateService(ctx, "/TRFIN/NOTHING", ""); !errors.As(err, &nf) {
			t.Errorf("activate unknown: %v", err)
		}

		// Uninstalling removes the service and its activation.
		if err := k.ActivateService(ctx, companyService, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{AllowDestructive: true, Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if d, _ := k.Service(ctx, companyService); len(d.Contained) != 0 {
			t.Errorf("the service still offers the extension of TROTTO: %+v", d.Contained)
		}
		if _, err := k.Uninstall(ctx, "TRFIN", kernel.Options{AllowDestructive: true, Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Service(ctx, companyService); !errors.As(err, &nf) {
			t.Errorf("service after uninstall: %v", err)
		}
		if err := k.ActivateService(ctx, companyService, ""); !errors.As(err, &nf) {
			t.Errorf("activation after uninstall: %v", err)
		}
	})
}
