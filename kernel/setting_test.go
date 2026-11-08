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
	"slices"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/setting"
)

func tables(d *setting.Definition) []string {
	var out []string
	d.Walk(func(t *setting.Table, _ int) { out = append(out, t.Name) })
	return out
}

// rejectedFor expects a validation error that names the setting.
func rejectedFor(t *testing.T, err error, settingID string) {
	t.Helper()
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Object == "setting "+settingID {
			return
		}
	}
	t.Fatalf("expected a rejection naming setting %s, got %v", settingID, err)
}

func tableOf(p *model.Package, name string) *model.Table {
	for i := range p.Tables {
		if p.Tables[i].Name == name {
			return &p.Tables[i]
		}
	}
	return nil
}

// settingPackages returns the test packages TRSET and TRSETX 1.0.0.
func settingPackages(t *testing.T) []*model.Package {
	return []*model.Package{testutil.SettingPackage(t, "TRSETX"), testutil.SettingPackage(t, "TRSET")}
}

// nextTRSET returns TRSET as version 1.1.0 for an upgrade.
func nextTRSET(t *testing.T) *model.Package {
	p := testutil.SettingPackage(t, "TRSET")
	p.Manifest.Version = "1.1.0"
	return p
}

func TestSettingLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, settingPackages(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}

		// The resolved definition is the same in every dialect.
		d, err := k.Setting(ctx, "/TRSET/PARTNERS")
		if err != nil {
			t.Fatal(err)
		}
		b, err := d.JSON()
		if err != nil {
			t.Fatal(err)
		}
		testutil.Golden(t, "setting-TRSET-PARTNERS.json", string(b))
		list, err := k.Settings(ctx)
		if err != nil || len(list) != 1 || list[0].Tables != 3 || list[0].Label != "Partners" {
			t.Fatalf("settings: %v %+v", err, list)
		}

		// A change of TRSET that breaks the extension of TRSETX is rejected
		// and names the setting.
		set := nextTRSET(t)
		tableOf(set, "/TRSET/PARTNERS").DataClass = model.RuntimeConfiguration
		tableOf(set, "/TRSET/ACCOUNTS").DataClass = model.RuntimeConfiguration
		_, err = k.Upgrade(ctx, set, kernel.Options{AllowDestructive: true})
		rejectedFor(t, err, "/TRSET/PARTNERS")
		if !strings.Contains(err.Error(), "/TRSETX/CONNECTIONS has the data class configuration") {
			t.Errorf("message should name the table of TRSETX: %v", err)
		}

		// Dropping the foreign key a dependent table hangs on is rejected.
		set = nextTRSET(t)
		tableOf(set, "/TRSET/ACCOUNTS").ForeignKeys = nil
		tableOf(set, "/TRSET/PAYMENTS").ForeignKeys = nil
		_, err = k.Upgrade(ctx, set, kernel.Options{AllowDestructive: true})
		rejectedFor(t, err, "/TRSET/PARTNERS")

		// A valid class change is applied and shown as notice.
		set = nextTRSET(t)
		tableOf(set, "/TRSET/RATES").DataClass = model.Configuration
		p, err := k.Upgrade(ctx, set, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if n := p.Notices(); len(n) != 1 || n[0].Kind != plan.AlterDataClass || n[0].Table != "/TRSET/RATES" || len(p.Steps) != 0 {
			t.Fatalf("class change: %+v, %d steps", p.Changes, len(p.Steps))
		}
		if !strings.Contains(p.Text(), "[DATA CLASS]") {
			t.Errorf("plan text:\n%s", p.Text())
		}
		if got, _ := k.Table(ctx, "/TRSET/RATES"); got.Table.DataClass != model.Configuration || got.Setting.Valid() {
			t.Fatalf("/TRSET/RATES: %+v", got.Table)
		}

		// Upgrading TRSETX adds a table below its own extension table.
		if _, err := k.Upgrade(ctx, testutil.SettingPackage(t, "upgrade/TRSETX"), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		d, _ = k.Setting(ctx, "/TRSET/PARTNERS")
		if got := tables(d); !slices.Equal(got, []string{"/TRSET/PARTNERS", "/TRSET/ACCOUNTS", "/TRSETX/CONNECTIONS", "/TRSETX/SHOPS"}) {
			t.Fatalf("after upgrade: %v", got)
		}
		if f := d.Root.Field("/TRSETX/EXT_ID"); f == nil || f.Length != 32 {
			t.Fatalf("extension field after upgrade: %+v", f)
		}
		consistent(t, k)

		// Without TRSETX the setting resolves without its tables and fields.
		if _, err := k.Uninstall(ctx, "TRSETX", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		d, err = k.Setting(ctx, "/TRSET/PARTNERS")
		if err != nil {
			t.Fatal(err)
		}
		if got := tables(d); !slices.Equal(got, []string{"/TRSET/PARTNERS", "/TRSET/ACCOUNTS"}) || len(d.Extensions) != 0 {
			t.Fatalf("after uninstall: %v %v", got, d.Extensions)
		}
		for _, f := range d.Root.Fields {
			if f.Owner == "TRSETX" {
				t.Errorf("field %s of TRSETX left", f.Name)
			}
		}
		consistent(t, k)
	})
}
