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

func TestSettingLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}

		// The resolved definition is the same in every dialect.
		d, err := k.Setting(ctx, "TRFIN_COMPANIES")
		if err != nil {
			t.Fatal(err)
		}
		b, err := d.JSON()
		if err != nil {
			t.Fatal(err)
		}
		testutil.Golden(t, "setting-TRFIN_COMPANIES.json", string(b))
		list, err := k.Settings(ctx)
		if err != nil || len(list) != 1 || list[0].Tables != 3 || list[0].Label != "Companies" {
			t.Fatalf("settings: %v %+v", err, list)
		}

		// A change of TRFIN that breaks the extension of TROTTO is rejected
		// and names the setting.
		fin := testutil.Example(t, "TRFIN")
		fin.Manifest.Version = "1.1.0"
		tableOf(fin, "COMPANIES").DataClass = model.RuntimeConfiguration
		tableOf(fin, "BANK_ACCOUNTS").DataClass = model.RuntimeConfiguration
		_, err = k.Upgrade(ctx, fin, kernel.Options{AllowDestructive: true})
		rejectedFor(t, err, "TRFIN_COMPANIES")
		if !strings.Contains(err.Error(), "OTTO_CONNECTIONS has the data class configuration") {
			t.Errorf("message should name the table of TROTTO: %v", err)
		}

		// Dropping the foreign key a dependent table hangs on is rejected.
		fin = testutil.Example(t, "TRFIN")
		fin.Manifest.Version = "1.1.0"
		tableOf(fin, "BANK_ACCOUNTS").ForeignKeys = nil
		tableOf(fin, "PAYMENTS").ForeignKeys = nil
		_, err = k.Upgrade(ctx, fin, kernel.Options{AllowDestructive: true})
		rejectedFor(t, err, "TRFIN_COMPANIES")

		// A valid class change is applied and shown as notice.
		fin = testutil.Example(t, "TRFIN")
		fin.Manifest.Version = "1.1.0"
		tableOf(fin, "EXCHANGE_RATES").DataClass = model.Configuration
		p, err := k.Upgrade(ctx, fin, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if n := p.Notices(); len(n) != 1 || n[0].Kind != plan.AlterDataClass || n[0].Table != "EXCHANGE_RATES" || len(p.Steps) != 0 {
			t.Fatalf("class change: %+v, %d steps", p.Changes, len(p.Steps))
		}
		if !strings.Contains(p.Text(), "[DATA CLASS]") {
			t.Errorf("plan text:\n%s", p.Text())
		}
		if got, _ := k.Table(ctx, "EXCHANGE_RATES"); got.Table.DataClass != model.Configuration || got.Setting != "" {
			t.Fatalf("EXCHANGE_RATES: %+v", got.Table)
		}

		// Upgrading TROTTO adds a table below its own extension table.
		if _, err := k.Upgrade(ctx, testutil.Example(t, "upgrade/TROTTO"), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		d, _ = k.Setting(ctx, "TRFIN_COMPANIES")
		if got := tables(d); !slices.Equal(got, []string{"COMPANIES", "BANK_ACCOUNTS", "OTTO_CONNECTIONS", "OTTO_SHOPS"}) {
			t.Fatalf("after upgrade: %v", got)
		}
		consistent(t, k)

		// Without TROTTO the setting resolves without its tables and fields.
		if _, err := k.Uninstall(ctx, "TROTTO", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		d, err = k.Setting(ctx, "TRFIN_COMPANIES")
		if err != nil {
			t.Fatal(err)
		}
		if got := tables(d); !slices.Equal(got, []string{"COMPANIES", "BANK_ACCOUNTS"}) || len(d.Extensions) != 0 {
			t.Fatalf("after uninstall: %v %v", got, d.Extensions)
		}
		for _, f := range d.Root.Fields {
			if f.Owner == "TROTTO" {
				t.Errorf("field %s of TROTTO left", f.Name)
			}
		}
		consistent(t, k)
	})
}
