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
	"slices"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func dataElement(p *model.Package, name string) *model.DataElement {
	for i := range p.DataElements {
		if p.DataElements[i].Name == name {
			return &p.DataElements[i]
		}
	}
	return nil
}

func TestDataElementResolvesDomain(t *testing.T) {
	s := load(t).schema(t)
	if err := Schema(s, Options{Languages: map[string]bool{"en": true, "de": true}}); err != nil {
		t.Fatal(err)
	}
	f := s.Physical().Tables["COMPANIES"].Field("COUNTRY")
	if f.DataElement != "COUNTRY" || f.Domain != "COUNTRY_CODE" {
		t.Fatalf("physical field: %+v", f)
	}
	if s.Tables["COMPANIES"].Field("COUNTRY").Domain != "" {
		t.Fatal("the declaration must not carry the domain")
	}
	if ext := s.Physical().Tables["COMPANIES"].Field("/TROTTO/OTTO_ID"); ext.Domain != "OTTO_ID" {
		t.Fatalf("extension field: %+v", ext)
	}
}

func TestFieldWithDomainAndDataElement(t *testing.T) {
	e := load(t)
	e.fin.Tables[1].Fields[0].Domain = "COMPANY_ID" // COMPANIES.COMPANY_ID has a data element
	expect(t, e.schema(t), rterr.CodeDataElement, "not both")
}

func TestFieldWithoutType(t *testing.T) {
	e := load(t)
	e.fin.Tables[1].Fields[0].DataElement = ""
	expect(t, e.schema(t), rterr.CodeDataElement, "needs a domain or a data element")
}

func TestUnknownDataElement(t *testing.T) {
	e := load(t)
	e.otto.Extensions[0].Fields[0].DataElement = "NOPE"
	expect(t, e.schema(t), rterr.CodeDataElement, "data element NOPE does not exist")
}

func TestDataElementNotVisible(t *testing.T) {
	e := load(t)
	e.fin.Tables[2].Fields[0].DataElement = "OTTO_PARTNER" // TRFIN does not require TROTTO
	expect(t, e.schema(t), rterr.CodeNotVisible, "data element OTTO_PARTNER")
}

func TestDataElementDomainNotVisible(t *testing.T) {
	e := load(t)
	dataElement(e.base, "NAME").Domain = "COMPANY_ID" // owned by TRFIN
	expect(t, e.schema(t), rterr.CodeNotVisible, "data element NAME")
}

func TestDataElementUnknownDomain(t *testing.T) {
	e := load(t)
	dataElement(e.base, "NAME").Domain = "NOPE"
	expect(t, e.schema(t), rterr.CodeUnknownDomain, "data element NAME")
}

func TestDataElementNeedsEnglish(t *testing.T) {
	e := load(t)
	delete(dataElement(e.base, "NAME").Texts, "en")
	expect(t, e.schema(t), rterr.CodeText, "English")
}

func TestDataElementLabelLength(t *testing.T) {
	e := load(t)
	d := dataElement(e.base, "NAME")
	en := d.Texts["en"]
	en.Short = strings.Repeat("x", model.MaxShortLabel+1)
	d.Texts["en"] = en
	expect(t, e.schema(t), rterr.CodeText, "short label")
}

func TestDataElementNameConflict(t *testing.T) {
	e := load(t)
	e.fin.DataElements = append(e.fin.DataElements, model.DataElement{Name: "NAME", Domain: "COMPANY_ID", Texts: map[string]model.DataElementText{}})
	s := testutil.AddPackages(t, testutil.KernelSchema(t), e.base)
	var nc *rterr.NameConflict
	if err := s.AddPackage(e.fin, testutil.Epoch, testutil.Epoch); !errors.As(err, &nc) || nc.Kind != "data element" {
		t.Fatalf("got %v", err)
	}
}

func TestMissingLanguages(t *testing.T) {
	err := Schema(load(t).schema(t), Options{Languages: map[string]bool{"en": true}})
	var ml *rterr.MissingLanguages
	if !errors.As(err, &ml) || !slices.Equal(ml.Languages, []string{"de"}) || !slices.Contains(ml.Objects, "data element COUNTRY") {
		t.Fatalf("got %v", err)
	}
	if n := len(rterr.All[*rterr.MissingLanguages](err)); n != 1 {
		t.Fatalf("missing languages must be reported once, got %d", n)
	}
}

func TestDataElementTypeCompatibility(t *testing.T) {
	e := load(t)
	dataElement(e.fin, "COMPANY").Domain = "IBAN" // every COMPANY_ID changes, keys stay consistent
	if err := Schema(e.schema(t), Options{}); err != nil {
		t.Fatalf("consistent change rejected: %v", err)
	}
	e = load(t)
	e.otto.Tables[0].Fields[0].DataElement = "NAME" // OTTO_CONNECTIONS.COMPANY_ID CHAR(80) -> COMPANIES CHAR(10)
	expect(t, e.schema(t), rterr.CodeTypeMismatch, "CHAR(80)")
}
