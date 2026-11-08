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
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

var enDescription = map[string]string{"en": "Checks"}

// pointPackage returns TRS with the extension point /TRS/CHECKS on the
// interface /TRS/CHECKS, filtered by COUNTRY, and the implementation
// /TRS/DE for DE.
func pointPackage(t *testing.T) model.Package {
	return model.Package{
		Sources: []model.Source{
			{Name: "/TRS/CHECKS", Kind: model.SourceInterface, File: "src/CHECKS.interface"},
			{Name: "/TRS/DE_CHECKS", Kind: model.SourceClass, File: "src/DE_CHECKS.class"},
			{Name: "/TRS/DEFAULT", Kind: model.SourceClass, File: "src/DEFAULT.class"},
		},
		ExtensionPoints: []model.ExtensionPoint{{Name: "/TRS/CHECKS", Description: enDescription, Interface: "/TRS/CHECKS", Use: model.UseSingle,
			Filters: []model.ExtensionFilter{{Name: "COUNTRY", DataElement: model.Some("/TRBASE/COUNTRY")}}, Fallback: model.Some("/TRS/DEFAULT")}},
		Implementations: []model.Implementation{{Name: "/TRS/DE", Description: enDescription, ExtensionPoint: "/TRS/CHECKS", Class: "/TRS/DE_CHECKS",
			Filters: map[string][]model.ValueEntry{"COUNTRY": entries(t, `[{"value": "DE"}]`)}}},
	}
}

func withPoint(t *testing.T, change func(p *model.ExtensionPoint, impl *model.Implementation)) *model.Schema {
	p := pointPackage(t)
	change(&p.ExtensionPoints[0], &p.Implementations[0])
	return withTRS(t, trs(p))
}

func TestExtensionPointValid(t *testing.T) {
	for _, s := range []*model.Schema{
		withPoint(t, func(*model.ExtensionPoint, *model.Implementation) {}),
		// Without filter fields and without fallback.
		withPoint(t, func(p *model.ExtensionPoint, impl *model.Implementation) {
			p.Filters, p.Fallback, impl.Filters = nil, model.None[string](), map[string][]model.ValueEntry{}
		}),
		// Ranges, patterns and *; a field with a default may be left out.
		withPoint(t, func(p *model.ExtensionPoint, impl *model.Implementation) {
			p.Filters = append(p.Filters, model.ExtensionFilter{Name: "COUNT", Domain: model.Some("/TRBASE/COUNTER"), Default: model.Some(entries(t, `[{"pattern": "*"}]`))})
			impl.Filters["COUNTRY"] = entries(t, `[{"from": "AT", "to": "DE"}, {"pattern": "F*"}, {"pattern": "*"}]`)
		}),
	} {
		if err := Schema(s, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExtensionPointRules(t *testing.T) {
	six := []model.ExtensionFilter{}
	for _, n := range []string{"A", "B", "C", "D", "E", "F"} {
		six = append(six, model.ExtensionFilter{Name: n, DataElement: model.Some("/TRBASE/NAME")})
	}
	for _, c := range []struct {
		name     string
		change   func(p *model.ExtensionPoint, impl *model.Implementation)
		code     rterr.Code
		contains string
	}{
		{"namespace", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Name = "/TRFIN/CHECKS" }, rterr.CodeName, "must start with the namespace /TRS/"},
		{"no English", func(p *model.ExtensionPoint, _ *model.Implementation) {
			p.Description = map[string]string{"de": "Prüfungen"}
		},
			rterr.CodeText, "a description in English (en) is mandatory"},
		{"interface missing", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Interface = "/TRS/NOPE" },
			rterr.CodeExtensionPoint, "the interface /TRS/NOPE does not exist in TRS"},
		{"interface is a class", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Interface = "/TRS/DEFAULT" },
			rterr.CodeExtensionPoint, "/TRS/DEFAULT is no interface"},
		{"interface of another package", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Interface = "/TRBASE/HTTP_HANDLER" },
			rterr.CodeExtensionPoint, "the interface /TRBASE/HTTP_HANDLER must belong to TRS"},
		{"fallback missing", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Fallback = model.Some("/TRS/NOPE") },
			rterr.CodeExtensionPoint, "the class /TRS/NOPE does not exist in TRS"},
		{"fallback is an interface", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Fallback = model.Some("/TRS/CHECKS") },
			rterr.CodeExtensionPoint, "/TRS/CHECKS is no class"},
		{"use", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Use = "some" }, rterr.CodeExtensionPoint, `use is single or multiple, not "some"`},
		{"six filters", func(p *model.ExtensionPoint, impl *model.Implementation) {
			p.Filters = six
			impl.Filters = map[string][]model.ValueEntry{}
		},
			rterr.CodeExtensionPoint, "at most 5 filter fields, not 6"},
		{"filter twice", func(p *model.ExtensionPoint, _ *model.Implementation) { p.Filters = append(p.Filters, p.Filters[0]) },
			rterr.CodeDuplicate, "the field COUNTRY is declared twice"},
		{"filter without type", func(p *model.ExtensionPoint, _ *model.Implementation) {
			p.Filters[0].DataElement = model.None[string]()
		},
			rterr.CodeDataElement, "a filter field needs a data element or a domain"},
		{"filter of type DEC", func(p *model.ExtensionPoint, impl *model.Implementation) {
			p.Filters[0] = model.ExtensionFilter{Name: "COUNTRY", Domain: model.Some("/TRBASE/AMOUNT")}
		}, rterr.CodeExtensionPoint, "type DEC(15,2) is not allowed; filter fields are CHAR(n), INT4, INT8, BOOL or UUID"},
		{"bad default", func(p *model.ExtensionPoint, _ *model.Implementation) {
			p.Filters = append(p.Filters, model.ExtensionFilter{Name: "COUNT", Domain: model.Some("/TRBASE/COUNTER"), Default: model.Some(entries(t, `[{"value": "x"}]`))})
		}, rterr.CodeExtensionPoint, "filter field COUNT, default"},
		{"unknown extension point", func(_ *model.ExtensionPoint, impl *model.Implementation) { impl.ExtensionPoint = "/TRS/NOPE" },
			rterr.CodeExtensionPoint, "extension point /TRS/NOPE does not exist"},
		{"class missing", func(_ *model.ExtensionPoint, impl *model.Implementation) { impl.Class = "/TRS/NOPE" },
			rterr.CodeExtensionPoint, "the class /TRS/NOPE does not exist in TRS"},
		{"class of another package", func(_ *model.ExtensionPoint, impl *model.Implementation) { impl.Class = "/TRFIN/COMPANY_SERVICE" },
			rterr.CodeExtensionPoint, "the class /TRFIN/COMPANY_SERVICE must belong to TRS"},
		{"missing filter field", func(_ *model.ExtensionPoint, impl *model.Implementation) {
			impl.Filters = map[string][]model.ValueEntry{}
		},
			rterr.CodeExtensionPoint, "the filter field COUNTRY of /TRS/CHECKS is missing"},
		{"unknown filter field", func(_ *model.ExtensionPoint, impl *model.Implementation) {
			impl.Filters["REGION"] = entries(t, `[{"value": "N"}]`)
		},
			rterr.CodeExtensionPoint, "/TRS/CHECKS has no filter field REGION; its filter fields are COUNTRY"},
		{"no values", func(_ *model.ExtensionPoint, impl *model.Implementation) { impl.Filters["COUNTRY"] = nil },
			rterr.CodeExtensionPoint, "the field has no values"},
		{"value of the wrong type", func(_ *model.ExtensionPoint, impl *model.Implementation) {
			impl.Filters["COUNTRY"] = entries(t, `[{"value": 49}]`)
		},
			rterr.CodeExtensionPoint, "implementation /TRS/DE, filter field COUNTRY"},
		{"value too long", func(_ *model.ExtensionPoint, impl *model.Implementation) {
			impl.Filters["COUNTRY"] = entries(t, `[{"value": "DEU"}]`)
		},
			rterr.CodeExtensionPoint, "implementation /TRS/DE, filter field COUNTRY"},
		{"bad pattern", func(_ *model.ExtensionPoint, impl *model.Implementation) {
			impl.Filters["COUNTRY"] = entries(t, `[{"pattern": "*D"}]`)
		},
			rterr.CodeExtensionPoint, `pattern "*D" is no pattern`},
		{"empty range", func(_ *model.ExtensionPoint, impl *model.Implementation) {
			impl.Filters["COUNTRY"] = entries(t, `[{"from": "DE", "to": "AT"}]`)
		},
			rterr.CodeExtensionPoint, `range "DE" to "AT" is empty`},
	} {
		t.Run(c.name, func(t *testing.T) { expect(t, withPoint(t, c.change), c.code, c.contains) })
	}
}

// TestImplementationNeedsDefiningPackage rejects an implementation of a
// package that does not require the package of the extension point.
func TestImplementationNeedsDefiningPackage(t *testing.T) {
	impl := pointPackage(t).Implementations[0]
	impl.Name, impl.Class = "/TRT/DE", "/TRT/DE_CHECKS"
	p := &model.Package{Manifest: model.Manifest{ID: "TRT", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRBASE"}}},
		Sources:         []model.Source{{Name: "/TRT/DE_CHECKS", Kind: model.SourceClass, File: "src/DE_CHECKS.class"}},
		Implementations: []model.Implementation{impl}}
	s := testutil.AddPackages(t, load(t).schema(t), trs(pointPackage(t)), p)
	expect(t, s, rterr.CodeNotVisible, "extension point /TRS/CHECKS belongs to TRS, which TRT does not require")
}

// TestFilterTypeChangeElsewhere rejects a domain of another package that
// changes to a type filter fields cannot have.
func TestFilterTypeChangeElsewhere(t *testing.T) {
	s := withPoint(t, func(*model.ExtensionPoint, *model.Implementation) {})
	d := s.Domains["/TRBASE/COUNTRY_CODE"]
	d.Type, d.Length, d.Decimals = model.Dec, 10, 2
	expect(t, s, rterr.CodeExtensionPoint, "extension point /TRS/CHECKS, filter field COUNTRY: type DEC(10,2) is not allowed")
}
