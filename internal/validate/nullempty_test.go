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
	"encoding/json"
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func domainOf(p *model.Package, name string) *model.Domain {
	for i := range p.Domains {
		if p.Domains[i].Name == name {
			return &p.Domains[i]
		}
	}
	return nil
}

func TestThreeDefaultKinds(t *testing.T) {
	e := load(t)
	f := e.fin.Tables[2].Fields // COUNTRIES: COUNTRY_CODE, NAME, CURRENCY
	f[2].DefaultJSON = json.RawMessage(`null`)
	s := e.schema(t)
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	c := s.Tables["/TRFIN/COUNTRIES"]
	if c.Field("CURRENCY").Default != model.NullDefault() || c.Field("NAME").Default != model.NoDefault() {
		t.Fatalf("defaults: %+v %+v", c.Field("CURRENCY").Default, c.Field("NAME").Default)
	}
	e = load(t)
	e.fin.Tables[2].Fields[2].DefaultJSON = json.RawMessage(`""`)
	s = e.schema(t)
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := s.Tables["/TRFIN/COUNTRIES"].Field("CURRENCY").Default; got != model.ValueDefault("") {
		t.Fatalf(`default "": %+v`, got)
	}
}

func TestDefaultNullOnNotNullField(t *testing.T) {
	e := load(t)
	e.fin.Tables[2].Fields[1].DefaultJSON = json.RawMessage(`null`) // COUNTRIES.NAME is notNull
	expect(t, e.schema(t), rterr.CodeDefault, "cannot have the default null")
}

func TestDomainDefaultNullOnNotNullField(t *testing.T) {
	e := load(t)
	domainOf(e.fin, "/TRFIN/IBAN").DefaultJSON = json.RawMessage(`null`) // BANK_ACCOUNTS.IBAN is notNull
	expect(t, e.schema(t), rterr.CodeDefault, "cannot have the default null")
}

func TestExtensionFieldNeedsValueDefault(t *testing.T) {
	e := load(t)
	f := &e.otto.Extensions[0].Fields[0]
	f.NotNull, f.DefaultJSON = true, json.RawMessage(`null`)
	expect(t, e.schema(t), rterr.CodeExtension, "nullable or have a default")
}

func TestAllowEmptyOnlyForStrings(t *testing.T) {
	e := load(t)
	domainOf(e.fin, "/TRFIN/ACCOUNT_NO").AllowEmpty = model.Some(false)
	expect(t, e.schema(t), rterr.CodeTypeParams, "allowEmpty applies only to CHAR and STRING")
}

func TestEmptyFixedValueContradictsAllowEmpty(t *testing.T) {
	e := load(t)
	d := domainOf(e.fin, "/TRFIN/COMPANY_STATUS")
	d.Values = append(d.Values, model.FixedValue{Value: ""})
	if err := Schema(e.schema(t), Options{}); err != nil {
		t.Fatalf(`"" is a valid fixed value: %v`, err)
	}
	d.AllowEmpty = model.Some(false)
	expect(t, e.schema(t), rterr.CodeFixedValue, "contradicts allowEmpty false")
}

func TestEmptyDefaultContradictsAllowEmpty(t *testing.T) {
	e := load(t)
	domainOf(e.fin, "/TRFIN/IBAN").AllowEmpty = model.Some(false)
	e.fin.Tables[0].Fields[2].DefaultJSON = json.RawMessage(`""`) // BANK_ACCOUNTS.IBAN
	expect(t, e.schema(t), rterr.CodeDefault, "does not allow")
}
