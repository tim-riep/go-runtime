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

package planner

import (
	"slices"
	"testing"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/dialect/postgres"
	"github.com/tim-riep/go-runtime/dialect/sqlite"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

var dialects = []dialect.Dialect{sqlite.New(), postgres.New()}

func valid(t *testing.T, s *model.Schema) *model.Schema {
	t.Helper()
	if err := validate.Schema(s, validate.Options{}); err != nil {
		t.Fatal(err)
	}
	return s
}

// schemas returns the states the golden files describe.
func schemas(t *testing.T) (kernel, installed, upgraded, removed *model.Schema) {
	kernel = valid(t, testutil.KernelSchema(t))
	installed = valid(t, testutil.AddPackages(t, kernel.Clone(),
		testutil.Example(t, "TRBASE"), testutil.Example(t, "TRFIN"), testutil.Example(t, "TROTTO")))
	upgraded = installed.Clone()
	upgraded.RemovePackage("TROTTO")
	upgraded = valid(t, testutil.AddPackages(t, upgraded, testutil.Example(t, "upgrade/TROTTO")))
	removed = upgraded.Clone()
	removed.RemovePackage("TROTTO")
	removed = valid(t, removed)
	return
}

func TestGoldenDDL(t *testing.T) {
	kernel, installed, upgraded, removed := schemas(t)
	cases := []struct {
		name      string
		op        plan.Operation
		cur, want *model.Schema
	}{
		{"kernel-init", plan.Migrate, model.NewSchema(), kernel},
		{"install-examples", plan.Install, kernel, installed},
		{"upgrade-trotto", plan.Upgrade, installed, upgraded},
		{"uninstall-trotto", plan.Uninstall, upgraded, removed},
	}
	for _, d := range dialects {
		for _, c := range cases {
			t.Run(d.Name()+"/"+c.name, func(t *testing.T) {
				p, err := Build(d, c.op, c.cur, c.want, nil)
				if err != nil {
					t.Fatal(err)
				}
				testutil.Golden(t, c.name+"."+d.Name()+".sql", p.SQL())
			})
		}
	}
}

func kinds(p *plan.Plan) []plan.ChangeKind {
	var out []plan.ChangeKind
	for _, c := range p.Changes {
		out = append(out, c.Kind)
	}
	return out
}

func TestUpgradeIsSafe(t *testing.T) {
	_, installed, upgraded, _ := schemas(t)
	p, err := Build(sqlite.New(), plan.Upgrade, installed, upgraded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := p.Destructive(); len(d) > 0 {
		t.Fatalf("upgrade of TROTTO must be safe, got %v", d)
	}
	for _, k := range []plan.ChangeKind{plan.CreateDomain, plan.AlterDomain, plan.CreateTable, plan.AddColumn, plan.AlterType, plan.AddForeignKey} {
		if !slices.Contains(kinds(p), k) {
			t.Errorf("missing %s in %v", k, kinds(p))
		}
	}
}

func TestDestructiveClassification(t *testing.T) {
	_, installed, _, _ := schemas(t)
	shrink := func(f func(s *model.Schema)) []plan.Change {
		want := installed.Clone()
		f(want)
		p, err := Build(postgres.New(), plan.Upgrade, installed, want, nil)
		if err != nil {
			t.Fatal(err)
		}
		return p.Destructive()
	}
	cases := map[string]func(s *model.Schema){
		"shorter char":        func(s *model.Schema) { s.Domains["OTTO_ID"].Length = 10 },
		"removed fixed value": func(s *model.Schema) { s.Domains["COMPANY_STATUS"].Values = s.Domains["COMPANY_STATUS"].Values[:2] },
		"new fixed values":    func(s *model.Schema) { s.Domains["IBAN"].Values = []model.FixedValue{{Value: "X"}} },
		"removed table":       func(s *model.Schema) { delete(s.Tables, "BANK_ACCOUNTS") },
		"tenant flag":         func(s *model.Schema) { s.Tables["COUNTRIES"].TenantSpecific = true },
		"type change":         func(s *model.Schema) { s.Domains["ACCOUNT_NO"].Type = model.Int8 },
		"primary key":         func(s *model.Schema) { s.Tables["COUNTRIES"].PrimaryKey = []string{"COUNTRY_CODE", "NAME"} },
		"set not null":        func(s *model.Schema) { s.Tables["COMPANIES"].Fields[4].NotNull = true },
		"removed field":       func(s *model.Schema) { s.Tables["COUNTRIES"].Fields = s.Tables["COUNTRIES"].Fields[:2] },
		"removed foreign key": func(s *model.Schema) { s.Tables["COMPANIES"].ForeignKeys = nil },
		"not null without def": func(s *model.Schema) {
			s.Tables["COUNTRIES"].Fields = append(s.Tables["COUNTRIES"].Fields, model.Field{Name: "X", Domain: "NAME", NotNull: true})
		},
	}
	for name, f := range cases {
		if len(shrink(f)) == 0 {
			t.Errorf("%s: not classified as destructive", name)
		}
	}
	safe := map[string]func(s *model.Schema){
		"data class":  func(s *model.Schema) { s.Tables["COUNTRIES"].DataClass = model.Application },
		"longer char": func(s *model.Schema) { s.Domains["NAME"].Length = 100 },
		"new fixed value": func(s *model.Schema) {
			s.Domains["COMPANY_STATUS"].Values = append(s.Domains["COMPANY_STATUS"].Values, model.FixedValue{Value: "MERGED"})
		},
		"drop not null": func(s *model.Schema) { s.Tables["COUNTRIES"].Fields[1].NotNull = false },
	}
	for name, f := range safe {
		if d := shrink(f); len(d) != 0 {
			t.Errorf("%s: classified as destructive: %v", name, d)
		}
	}
}

func TestDataElementChanges(t *testing.T) {
	_, installed, _, _ := schemas(t)
	build := func(f func(s *model.Schema)) *plan.Plan {
		want := installed.Clone()
		f(want)
		p, err := Build(sqlite.New(), plan.Upgrade, installed, want, nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Switching a field to a data element with the same domain changes only
	// the repository.
	p := build(func(s *model.Schema) {
		f := s.Tables["BANK_ACCOUNTS"].Field("IBAN")
		s.DataElements["IBAN"] = &model.DataElement{Name: "IBAN", Owner: "TRFIN", Domain: "IBAN", Texts: map[string]model.DataElementText{}}
		f.Domain, f.DataElement = "", "IBAN"
	})
	if got := kinds(p); !slices.Equal(got, []plan.ChangeKind{plan.CreateDataElement, plan.AlterFieldReference}) || len(p.Steps) != 0 {
		t.Fatalf("changes %v, %d steps", got, len(p.Steps))
	}
	// Texts never need statements.
	p = build(func(s *model.Schema) {
		en := s.DataElements["NAME"].Texts["en"]
		en.Long = "Full name"
		s.DataElements["NAME"].Texts["en"] = en
	})
	if len(p.Steps) != 0 || len(p.Destructive()) != 0 || kinds(p)[0] != plan.AlterDataElement {
		t.Fatalf("text change: %v", p.Changes)
	}
	// A new domain of a data element changes the type of every field using
	// it and follows the rules of type changes.
	p = build(func(s *model.Schema) { s.DataElements["NAME"].Domain = "CURRENCY" })
	n := 0
	for _, c := range p.Changes {
		if c.Kind == plan.AlterType {
			n++
			if !c.Destructive {
				t.Errorf("CHAR(80) -> CHAR(3) must be destructive: %v", c)
			}
		}
	}
	if n != 2 { // COMPANIES.NAME and COUNTRIES.NAME
		t.Fatalf("expected a type change per field, got %v", p.Changes)
	}
}
