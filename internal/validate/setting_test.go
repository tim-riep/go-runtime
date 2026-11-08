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

package validate

import (
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

var allOps = model.Operations{Create: true, Update: true, Delete: true}

// settingPackages are the test packages for settings: TRSET with the
// setting /TRSET/PARTNERS and TRSETX, which extends it.
type settingPackages struct{ base, set, ext *model.Package }

// loadSettings loads TRBASE, TRSET and TRSETX 1.0.0.
func loadSettings(t *testing.T) settingPackages {
	return settingPackages{testutil.Example(t, "TRBASE"), testutil.SettingPackage(t, "TRSET"), testutil.SettingPackage(t, "TRSETX")}
}

// upgraded loads the packages with TRSETX 1.1.0, whose setting extension
// has two levels.
func upgraded(t *testing.T) settingPackages {
	e := loadSettings(t)
	e.ext = testutil.SettingPackage(t, "upgrade/TRSETX")
	return e
}

func (e settingPackages) schema(t *testing.T) *model.Schema {
	return testutil.AddPackages(t, testutil.KernelSchema(t), e.base, e.set, e.ext)
}

// settingTable returns the table of p with the given name.
func settingTable(t *testing.T, p *model.Package, name string) *model.Table {
	t.Helper()
	for i := range p.Tables {
		if p.Tables[i].Name == name {
			return &p.Tables[i]
		}
	}
	t.Fatalf("table %s not in package %s", name, p.Manifest.ID)
	return nil
}

// expectSetting expects a setting error that names the setting.
func expectSetting(t *testing.T, s *model.Schema, code rterr.Code, setting, contains string) {
	t.Helper()
	err := Schema(s, Options{})
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Code == code && v.Object == "setting "+setting && strings.Contains(v.Message, contains) {
			return
		}
	}
	t.Fatalf("expected %s error for setting %s containing %q, got:\n%v", code, setting, contains, err)
}

func TestSettingsOfTestPackagesAreValid(t *testing.T) {
	for _, e := range []settingPackages{loadSettings(t), upgraded(t)} {
		s := e.schema(t)
		if err := Schema(s, Options{}); err != nil {
			t.Fatal(err)
		}
		if got := s.SettingOf("/TRSETX/CONNECTIONS"); got != "/TRSET/PARTNERS" {
			t.Fatalf("/TRSETX/CONNECTIONS belongs to %q", got)
		}
	}
}

func TestSettingWithApplicationTable(t *testing.T) {
	e := loadSettings(t)
	ext := &e.ext.SettingExtensions[0]
	ext.Tables = append(ext.Tables, model.SettingTable{Table: "/TRSET/PAYMENTS", Parent: model.Some("/TRSET/PARTNERS"), ForeignKey: model.Some("ACCOUNT"), Operations: allOps})
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "data class application")
}

func TestSettingNeedsOneClass(t *testing.T) {
	e := loadSettings(t)
	settingTable(t, e.set, "/TRSET/ACCOUNTS").DataClass = model.RuntimeConfiguration
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "same class")
}

func TestClassChangeBreaksSetting(t *testing.T) {
	e := loadSettings(t)
	settingTable(t, e.set, "/TRSET/PARTNERS").DataClass = model.Application // the root
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "/TRSET/PARTNERS has the data class application")
}

func TestSettingNeedsOneTenantFlag(t *testing.T) {
	e := loadSettings(t)
	st := &e.set.Settings[0]
	st.Tables = append(st.Tables, model.SettingTable{Table: "/TRSET/COUNTRIES", Parent: model.Some("/TRSET/PARTNERS"), ForeignKey: model.Some("COUNTRY"), Operations: allOps})
	expectSetting(t, e.schema(t), rterr.CodeTenant, "/TRSET/PARTNERS", "same tenant flag")
}

func TestTableInTwoSettings(t *testing.T) {
	e := loadSettings(t)
	e.set.Settings = append(e.set.Settings, model.Setting{ID: "/TRSET/ACCOUNTS", Texts: map[string]model.SettingText{"en": {Label: "Accounts"}},
		Root: model.SettingTable{Table: "/TRSET/ACCOUNTS", Operations: allOps}})
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "already belongs to setting /TRSET/ACCOUNTS")
}

func TestSettingTableNotVisible(t *testing.T) {
	e := loadSettings(t)
	st := &e.set.Settings[0]
	st.Tables = append(st.Tables, model.SettingTable{Table: "/TRSETX/CONNECTIONS", Parent: model.Some("/TRSET/PARTNERS"), ForeignKey: model.Some("PARTNER"), Operations: allOps})
	e.ext.SettingExtensions = nil
	expectSetting(t, e.schema(t), rterr.CodeNotVisible, "/TRSET/PARTNERS", "which TRSET does not require")
}

func TestKernelTableInSetting(t *testing.T) {
	e := loadSettings(t)
	e.set.Settings[0].Root.Table = model.TenantTable
	e.set.Settings[0].Tables = nil
	e.ext.SettingExtensions = nil
	expectSetting(t, e.schema(t), rterr.CodeReservedObject, "/TRSET/PARTNERS", "kernel table")
}

func TestSettingUnknownRoot(t *testing.T) {
	e := loadSettings(t)
	e.set.Settings[0].Root.Table = "NOPE"
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "root table NOPE does not exist")
}

func TestSettingForeignKeyMissing(t *testing.T) {
	e := loadSettings(t)
	e.ext.SettingExtensions[0].Tables[0].ForeignKey = model.Some("NOPE")
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "has no foreign key NOPE")
}

func TestSettingForeignKeyToOtherTable(t *testing.T) {
	e := upgraded(t)
	e.ext.SettingExtensions[0].Tables[1].Parent = model.Some("/TRSET/PARTNERS") // /TRSETX/SHOPS.CONNECTION references /TRSETX/CONNECTIONS
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "references /TRSETX/CONNECTIONS, not the parent /TRSET/PARTNERS")
}

func TestSettingParentNotInSetting(t *testing.T) {
	e := loadSettings(t)
	e.ext.SettingExtensions[0].Tables[0].Parent = model.Some("/TRSET/COUNTRIES")
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "parent /TRSET/COUNTRIES of table /TRSETX/CONNECTIONS is not part of the setting")
}

func TestSettingCycleIsNotConnected(t *testing.T) {
	e := upgraded(t)
	e.ext.SettingExtensions[0].Tables[0].Parent = model.Some("/TRSETX/SHOPS") // /TRSETX/CONNECTIONS <-> /TRSETX/SHOPS
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "not connected to the root table")
}

func TestSettingTexts(t *testing.T) {
	e := loadSettings(t)
	delete(e.set.Settings[0].Texts, "en")
	expectSetting(t, e.schema(t), rterr.CodeText, "/TRSET/PARTNERS", "English")
	e = loadSettings(t)
	e.set.Settings[0].Texts["de"] = model.SettingText{Label: strings.Repeat("x", model.MaxSettingLabel+1)}
	expectSetting(t, e.schema(t), rterr.CodeText, "/TRSET/PARTNERS", "label in de")
}

func TestSettingID(t *testing.T) {
	e := loadSettings(t)
	e.set.Settings[0].ID = "/TRSET/partners"
	e.ext.SettingExtensions = nil
	expectSetting(t, e.schema(t), rterr.CodeName, "/TRSET/partners", "after the namespace")
}

func TestSettingExtensionRules(t *testing.T) {
	e := loadSettings(t)
	e.ext.SettingExtensions[0].Setting = "NOPE"
	expectSetting(t, e.schema(t), rterr.CodeSetting, "NOPE", "does not exist")

	e = loadSettings(t)
	e.set.SettingExtensions = []model.SettingExtension{{Setting: "/TRSET/PARTNERS",
		Tables: []model.SettingTable{{Table: "/TRSET/PAYMENTS", Parent: model.Some("/TRSET/ACCOUNTS"), ForeignKey: model.Some("ACCOUNT"), Operations: allOps}}}}
	expectSetting(t, e.schema(t), rterr.CodeSetting, "/TRSET/PARTNERS", "own setting")
}

// trx is a package that extends /TRSET/PARTNERS below /TRSETX/CONNECTIONS,
// which TRSETX added.
func trx(requires ...model.PackageID) *model.Package {
	p := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0"}}
	for _, r := range requires {
		p.Manifest.Requires = append(p.Manifest.Requires, model.Dependency{ID: r})
	}
	p.Tables = []model.Table{{Name: "/TRX/X_OPTIONS", TenantSpecific: true, DataClass: model.Configuration,
		Fields:      []model.Field{{Name: "PARTNER_ID", LocalName: "PARTNER_ID", DataElement: model.Some("/TRSET/PARTNER"), Origin: model.OriginTable}},
		PrimaryKey:  []string{"PARTNER_ID"},
		ForeignKeys: []model.ForeignKey{{Name: "CONNECTION", Target: "/TRSETX/CONNECTIONS", Origin: model.OriginTable, Columns: []model.ColumnPair{{Field: "PARTNER_ID", References: "PARTNER_ID"}}}}}}
	p.SettingExtensions = []model.SettingExtension{{Setting: "/TRSET/PARTNERS",
		Tables: []model.SettingTable{{Table: "/TRX/X_OPTIONS", Parent: model.Some("/TRSETX/CONNECTIONS"), ForeignKey: model.Some("CONNECTION"), Operations: allOps}}}}
	return p
}

func TestExtensionBelowOtherExtension(t *testing.T) {
	ok := testutil.AddPackages(t, loadSettings(t).schema(t), trx("TRSETX"))
	if err := Schema(ok, Options{}); err != nil {
		t.Fatalf("hanging below a table of a required package must work: %v", err)
	}
	if ok.SettingOf("/TRX/X_OPTIONS") != "/TRSET/PARTNERS" {
		t.Fatal("/TRX/X_OPTIONS not in the setting")
	}
	bad := testutil.AddPackages(t, loadSettings(t).schema(t), trx("TRSET"))
	expectSetting(t, bad, rterr.CodeNotVisible, "/TRSET/PARTNERS", "added by package TRSETX, which TRX does not require")
}

func TestSettingExtensionNeedsOwner(t *testing.T) {
	e := loadSettings(t)
	s := testutil.AddPackages(t, testutil.KernelSchema(t), e.base, e.set)
	p := trx("TRBASE")
	p.Tables = nil
	p.SettingExtensions[0].Tables[0] = model.SettingTable{Table: "/TRSET/PAYMENTS", Parent: model.Some("/TRSET/ACCOUNTS"), ForeignKey: model.Some("ACCOUNT"), Operations: allOps}
	testutil.AddPackages(t, s, p)
	expectSetting(t, s, rterr.CodeNotVisible, "/TRSET/PARTNERS", "which TRX does not require")
}

func TestSettingNameConflict(t *testing.T) {
	s := loadSettings(t).schema(t)
	other := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0"}, Settings: []model.Setting{{ID: "/TRSET/PARTNERS"}}}
	var nc *rterr.NameConflict
	if err := s.AddPackage(other, testutil.Epoch, testutil.Epoch); !errors.As(err, &nc) || nc.Kind != "setting" {
		t.Fatalf("got %v", err)
	}
}

func TestUninstallRemovesSettingExtension(t *testing.T) {
	s := upgraded(t).schema(t)
	s.RemovePackage("TRSETX")
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(s.SettingNodes("/TRSET/PARTNERS")) != 2 || len(s.SettingExtensions) != 0 {
		t.Fatalf("nodes: %+v", s.SettingNodes("/TRSET/PARTNERS"))
	}
}
