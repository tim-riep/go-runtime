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

// upgraded loads the examples with TROTTO 1.1.0, whose setting extension
// has two levels.
func upgraded(t *testing.T) examples {
	e := load(t)
	e.otto = testutil.Example(t, "upgrade/TROTTO")
	return e
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

func TestSettingsOfExamplesAreValid(t *testing.T) {
	for _, e := range []examples{load(t), upgraded(t)} {
		s := e.schema(t)
		if err := Schema(s, Options{}); err != nil {
			t.Fatal(err)
		}
		if got := s.SettingOf("OTTO_CONNECTIONS"); got != "TRFIN_COMPANIES" {
			t.Fatalf("OTTO_CONNECTIONS belongs to %q", got)
		}
	}
}

func TestSettingWithApplicationTable(t *testing.T) {
	e := load(t)
	ext := &e.otto.SettingExtensions[0]
	ext.Tables = append(ext.Tables, model.SettingTable{Table: "PAYMENTS", Parent: "COMPANIES", ForeignKey: "ACCOUNT", Operations: allOps})
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "data class application")
}

func TestSettingNeedsOneClass(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].DataClass = model.RuntimeConfiguration // BANK_ACCOUNTS
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "same class")
}

func TestClassChangeBreaksSetting(t *testing.T) {
	e := load(t)
	e.fin.Tables[1].DataClass = model.Application // COMPANIES, the root
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "COMPANIES has the data class application")
}

func TestSettingNeedsOneTenantFlag(t *testing.T) {
	e := load(t)
	st := &e.fin.Settings[0]
	st.Tables = append(st.Tables, model.SettingTable{Table: "COUNTRIES", Parent: "COMPANIES", ForeignKey: "COUNTRY", Operations: allOps})
	expectSetting(t, e.schema(t), rterr.CodeTenant, "TRFIN_COMPANIES", "same tenant flag")
}

func TestTableInTwoSettings(t *testing.T) {
	e := load(t)
	e.fin.Settings = append(e.fin.Settings, model.Setting{ID: "TRFIN_ACCOUNTS", Texts: map[string]model.SettingText{"en": {Label: "Accounts"}},
		Root: model.SettingTable{Table: "BANK_ACCOUNTS", Operations: allOps}})
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "already belongs to setting TRFIN_ACCOUNTS")
}

func TestSettingTableNotVisible(t *testing.T) {
	e := load(t)
	st := &e.fin.Settings[0]
	st.Tables = append(st.Tables, model.SettingTable{Table: "OTTO_CONNECTIONS", Parent: "COMPANIES", ForeignKey: "COMPANY", Operations: allOps})
	e.otto.SettingExtensions = nil
	expectSetting(t, e.schema(t), rterr.CodeNotVisible, "TRFIN_COMPANIES", "which TRFIN does not require")
}

func TestKernelTableInSetting(t *testing.T) {
	e := load(t)
	e.fin.Settings[0].Root.Table = model.TenantTable
	e.fin.Settings[0].Tables = nil
	e.otto.SettingExtensions = nil
	expectSetting(t, e.schema(t), rterr.CodeReservedObject, "TRFIN_COMPANIES", "kernel table")
}

func TestSettingUnknownRoot(t *testing.T) {
	e := load(t)
	e.fin.Settings[0].Root.Table = "NOPE"
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "root table NOPE does not exist")
}

func TestSettingForeignKeyMissing(t *testing.T) {
	e := load(t)
	e.otto.SettingExtensions[0].Tables[0].ForeignKey = "NOPE"
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "has no foreign key NOPE")
}

func TestSettingForeignKeyToOtherTable(t *testing.T) {
	e := upgraded(t)
	e.otto.SettingExtensions[0].Tables[1].Parent = "COMPANIES" // OTTO_SHOPS.CONNECTION references OTTO_CONNECTIONS
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "references OTTO_CONNECTIONS, not the parent COMPANIES")
}

func TestSettingParentNotInSetting(t *testing.T) {
	e := load(t)
	e.otto.SettingExtensions[0].Tables[0].Parent = "COUNTRIES"
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "parent COUNTRIES of table OTTO_CONNECTIONS is not part of the setting")
}

func TestSettingCycleIsNotConnected(t *testing.T) {
	e := upgraded(t)
	e.otto.SettingExtensions[0].Tables[0].Parent = "OTTO_SHOPS" // OTTO_CONNECTIONS <-> OTTO_SHOPS
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "not connected to the root table")
}

func TestSettingTexts(t *testing.T) {
	e := load(t)
	delete(e.fin.Settings[0].Texts, "en")
	expectSetting(t, e.schema(t), rterr.CodeText, "TRFIN_COMPANIES", "English")
	e = load(t)
	e.fin.Settings[0].Texts["de"] = model.SettingText{Label: strings.Repeat("x", model.MaxSettingLabel+1)}
	expectSetting(t, e.schema(t), rterr.CodeText, "TRFIN_COMPANIES", "label in de")
}

func TestSettingID(t *testing.T) {
	e := load(t)
	e.fin.Settings[0].ID = "companies"
	e.otto.SettingExtensions = nil
	expectSetting(t, e.schema(t), rterr.CodeSetting, "companies", "upper-case")
}

func TestSettingExtensionRules(t *testing.T) {
	e := load(t)
	e.otto.SettingExtensions[0].Setting = "NOPE"
	expectSetting(t, e.schema(t), rterr.CodeSetting, "NOPE", "does not exist")

	e = load(t)
	e.fin.SettingExtensions = []model.SettingExtension{{Setting: "TRFIN_COMPANIES",
		Tables: []model.SettingTable{{Table: "PAYMENTS", Parent: "BANK_ACCOUNTS", ForeignKey: "ACCOUNT", Operations: allOps}}}}
	expectSetting(t, e.schema(t), rterr.CodeSetting, "TRFIN_COMPANIES", "own setting")
}

// trx is a package that extends TRFIN_COMPANIES below OTTO_CONNECTIONS,
// which TROTTO added.
func trx(requires ...model.PackageID) *model.Package {
	p := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0"}}
	for _, r := range requires {
		p.Manifest.Requires = append(p.Manifest.Requires, model.Dependency{ID: r})
	}
	p.Tables = []model.Table{{Name: "X_OPTIONS", TenantSpecific: true, DataClass: model.Configuration,
		Fields:      []model.Field{{Name: "COMPANY_ID", LocalName: "COMPANY_ID", DataElement: "COMPANY", Origin: model.OriginTable}},
		PrimaryKey:  []string{"COMPANY_ID"},
		ForeignKeys: []model.ForeignKey{{Name: "CONNECTION", Target: "OTTO_CONNECTIONS", Origin: model.OriginTable, Columns: []model.ColumnPair{{Field: "COMPANY_ID", References: "COMPANY_ID"}}}}}}
	p.SettingExtensions = []model.SettingExtension{{Setting: "TRFIN_COMPANIES",
		Tables: []model.SettingTable{{Table: "X_OPTIONS", Parent: "OTTO_CONNECTIONS", ForeignKey: "CONNECTION", Operations: allOps}}}}
	return p
}

func TestExtensionBelowOtherExtension(t *testing.T) {
	ok := testutil.AddPackages(t, load(t).schema(t), trx("TROTTO"))
	if err := Schema(ok, Options{}); err != nil {
		t.Fatalf("hanging below a table of a required package must work: %v", err)
	}
	if ok.SettingOf("X_OPTIONS") != "TRFIN_COMPANIES" {
		t.Fatal("X_OPTIONS not in the setting")
	}
	bad := testutil.AddPackages(t, load(t).schema(t), trx("TRFIN"))
	expectSetting(t, bad, rterr.CodeNotVisible, "TRFIN_COMPANIES", "added by package TROTTO, which TRX does not require")
}

func TestSettingExtensionNeedsOwner(t *testing.T) {
	e := load(t)
	s := testutil.AddPackages(t, testutil.KernelSchema(t), e.base, e.fin)
	p := trx("TRBASE")
	p.Tables = nil
	p.SettingExtensions[0].Tables[0] = model.SettingTable{Table: "PAYMENTS", Parent: "BANK_ACCOUNTS", ForeignKey: "ACCOUNT", Operations: allOps}
	testutil.AddPackages(t, s, p)
	expectSetting(t, s, rterr.CodeNotVisible, "TRFIN_COMPANIES", "which TRX does not require")
}

func TestSettingNameConflict(t *testing.T) {
	e := load(t)
	s := e.schema(t)
	other := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0"}, Settings: []model.Setting{{ID: "TRFIN_COMPANIES"}}}
	var nc *rterr.NameConflict
	if err := s.AddPackage(other, testutil.Epoch, testutil.Epoch); !errors.As(err, &nc) || nc.Kind != "setting" {
		t.Fatalf("got %v", err)
	}
}

func TestUninstallRemovesSettingExtension(t *testing.T) {
	s := upgraded(t).schema(t)
	s.RemovePackage("TROTTO")
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(s.SettingNodes("TRFIN_COMPANIES")) != 2 || len(s.SettingExtensions) != 0 {
		t.Fatalf("nodes: %+v", s.SettingNodes("TRFIN_COMPANIES"))
	}
}
