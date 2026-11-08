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

package setting_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/setting"
)

// schema loads TRBASE, TRSET and, unless ext is empty, the extending
// package ext from testdata/settings, e.g. "TRSETX" or "upgrade/TRSETX".
func schema(t *testing.T, ext string) *model.Schema {
	s := testutil.AddPackages(t, testutil.KernelSchema(t), testutil.Example(t, "TRBASE"), testutil.SettingPackage(t, "TRSET"))
	if ext != "" {
		testutil.AddPackages(t, s, testutil.SettingPackage(t, ext))
	}
	if err := validate.Schema(s, validate.Options{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func resolve(t *testing.T, s *model.Schema, langs ...string) *setting.Definition {
	d, err := setting.Resolve(s, langs, "/TRSET/PARTNERS")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func tree(d *setting.Definition) []string {
	var out []string
	d.Walk(func(t *setting.Table, depth int) {
		out = append(out, string(rune('0'+depth))+" "+t.Name+" "+string(t.Origin))
	})
	return out
}

func TestResolveGolden(t *testing.T) {
	d := resolve(t, schema(t, "TRSETX"), "en", "de")
	b, err := d.JSON()
	if err != nil {
		t.Fatal(err)
	}
	testutil.Golden(t, "setting-TRSET-PARTNERS.json", string(b))
}

func TestResolveMergesExtensions(t *testing.T) {
	d := resolve(t, schema(t, "upgrade/TRSETX"), "en", "de")
	want := []string{"0 /TRSET/PARTNERS TRSET", "1 /TRSET/ACCOUNTS TRSET", "1 /TRSETX/CONNECTIONS TRSETX", "2 /TRSETX/SHOPS TRSETX"}
	if got := tree(d); !slices.Equal(got, want) {
		t.Fatalf("tree %v, want %v", got, want)
	}
	if !d.TenantSpecific || d.DataClass != model.Configuration || !slices.Equal(d.Extensions, []model.PackageID{"TRSETX"}) {
		t.Fatalf("definition: %+v", d)
	}
	root := d.Root
	if root.ParentKey != nil || !root.Operations.Delete || !slices.Equal(root.PrimaryKey, []string{"PARTNER_ID"}) {
		t.Fatalf("root: %+v", root)
	}
	if root.Field(model.TenantField) != nil {
		t.Fatal("the tenant field must not appear")
	}
	country := root.Field("COUNTRY")
	if country.DataElement == nil || country.DataElement.Texts["de"].Long != "Länderschlüssel" || country.Type != model.Char || country.Length != 2 {
		t.Fatalf("COUNTRY: %+v", country)
	}
	if len(country.ValueHelps) != 1 || country.ValueHelps[0].Table != "/TRSET/COUNTRIES" || len(country.ValueHelps[0].Fields) != 1 {
		t.Fatalf("value help of COUNTRY: %+v", country.ValueHelps)
	}
	status := root.Field("STATUS")
	if status.DataElement != nil || len(status.FixedValues) != 3 || status.Default != model.ValueDefault("ACTIVE") {
		t.Fatalf("STATUS: %+v", status)
	}
	ext := root.Field("/TRSETX/EXT_ID")
	if ext == nil || ext.Origin != model.OriginExtension || ext.Owner != "TRSETX" || ext.Length != 32 || ext.DataElement.Name != "/TRSETX/EXT_PARTNER" {
		t.Fatalf("extension field: %+v", ext)
	}
	accounts := d.Table("/TRSET/ACCOUNTS")
	if accounts.Operations.Delete || accounts.ParentKey.Name != "PARTNER" || !slices.Equal(accounts.ParentKey.Fields, []model.ColumnPair{{Field: "PARTNER_ID", References: "PARTNER_ID"}}) {
		t.Fatalf("/TRSET/ACCOUNTS: %+v", accounts)
	}
	cid := accounts.Field("PARTNER_ID")
	if !cid.FromParent || !cid.Key || accounts.Field("IBAN").FromParent {
		t.Fatalf("parent key fields: %+v", cid)
	}
	if bal := accounts.Field("BALANCE"); bal.Type != model.Dec || bal.Length != 15 || bal.Decimals != 2 {
		t.Fatalf("BALANCE: %+v", bal)
	}
	shops := d.Table("/TRSETX/SHOPS")
	if shops.ParentKey.Table != "/TRSETX/CONNECTIONS" || !shops.Field("PARTNER_ID").FromParent || shops.Field("SHOP_ID").FromParent {
		t.Fatalf("/TRSETX/SHOPS: %+v", shops)
	}
}

func TestResolveWithoutExtension(t *testing.T) {
	d := resolve(t, schema(t, ""), "en", "de")
	if got := tree(d); !slices.Equal(got, []string{"0 /TRSET/PARTNERS TRSET", "1 /TRSET/ACCOUNTS TRSET"}) {
		t.Fatalf("tree %v", got)
	}
	if d.Root.Field("/TRSETX/EXT_ID") != nil || len(d.Extensions) != 0 {
		t.Fatal("TRSETX is not installed")
	}
}

func TestResolveLanguages(t *testing.T) {
	d := resolve(t, schema(t, "TRSETX"), "en")
	if _, ok := d.Texts["de"]; ok {
		t.Fatal("German label of the setting returned")
	}
	if _, ok := d.Root.Field("NAME").DataElement.Texts["de"]; ok {
		t.Fatal("German field label returned")
	}
	if d.Texts["en"].Label != "Partners" {
		t.Fatalf("texts: %+v", d.Texts)
	}
}

func TestResolveUnknown(t *testing.T) {
	var nf *rterr.NotFound
	if _, err := setting.Resolve(schema(t, ""), []string{"en"}, "NOPE"); !errors.As(err, &nf) {
		t.Fatalf("got %v", err)
	}
}
