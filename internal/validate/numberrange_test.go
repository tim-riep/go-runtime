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

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// numberRange returns a valid number range object of TRS with a company as
// subobject.
func numberRange() model.NumberRange {
	return model.NumberRange{Name: "/TRS/DOCUMENT", Texts: en, Length: 10, PerFiscalYear: true, Buffering: model.BufferingNone,
		WarningPercent: 90, Subobject: model.Some(model.NumberRangeSubobject{Name: "COMPANY", DataElement: model.Some("/TRDEMOFIN/COMPANY")})}
}

func withNumberRange(t *testing.T, change func(n *model.NumberRange)) *model.Schema {
	n := numberRange()
	change(&n)
	return withTRS(t, trs(model.Package{NumberRanges: []model.NumberRange{n}}))
}

func TestNumberRangeValid(t *testing.T) {
	for _, s := range []*model.Schema{
		withNumberRange(t, func(n *model.NumberRange) {}),
		withNumberRange(t, func(n *model.NumberRange) {
			n.Subobject, n.PerFiscalYear, n.Length = model.None[model.NumberRangeSubobject](), false, 20
			n.Buffering, n.BlockSize = model.BufferingBuffered, 100
		}),
		withNumberRange(t, func(n *model.NumberRange) {
			n.Subobject = model.Some(model.NumberRangeSubobject{Name: "COUNT", Domain: model.Some("/TRBASE/COUNTER")})
		}),
	} {
		if err := Schema(s, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNumberRangeRules(t *testing.T) {
	sub := func(s model.NumberRangeSubobject) model.Optional[model.NumberRangeSubobject] { return model.Some(s) }
	for _, c := range []struct {
		name     string
		change   func(n *model.NumberRange)
		code     rterr.Code
		contains string
	}{
		{"too long", func(n *model.NumberRange) { n.Length = 21 }, rterr.CodeNumberRange, "1 to 20 digits, not 21"},
		{"no digits", func(n *model.NumberRange) { n.Length = 0 }, rterr.CodeNumberRange, "1 to 20 digits, not 0"},
		{"block without buffering", func(n *model.NumberRange) { n.BlockSize = 10 }, rterr.CodeNumberRange, "a block size needs the buffering mode buffered"},
		{"block too large", func(n *model.NumberRange) { n.Buffering, n.BlockSize = model.BufferingBuffered, 10001 }, rterr.CodeNumberRange, "1 to 10000 numbers"},
		{"warning", func(n *model.NumberRange) { n.WarningPercent = 0 }, rterr.CodeNumberRange, "1 to 100 percent, not 0"},
		{"subobject name", func(n *model.NumberRange) {
			n.Subobject = sub(model.NumberRangeSubobject{Name: "company", DataElement: model.Some("/TRDEMOFIN/COMPANY")})
		},
			rterr.CodeName, "the name of a subobject"},
		{"subobject without type", func(n *model.NumberRange) { n.Subobject = sub(model.NumberRangeSubobject{Name: "COMPANY"}) },
			rterr.CodeDataElement, "either a data element or a domain"},
		{"subobject DATE", func(n *model.NumberRange) {
			n.Subobject = sub(model.NumberRangeSubobject{Name: "DAY", Domain: model.Some("/TRBASE/DATE")})
		},
			rterr.CodeNumberRange, "type DATE is not allowed"},
		{"namespace", func(n *model.NumberRange) { n.Name = "/TRDEMOFIN/MINE" }, rterr.CodeName, "must start with the namespace /TRS/"},
		{"no English", func(n *model.NumberRange) { n.Texts = map[string]model.Label{"de": {Label: "Belege"}} }, rterr.CodeText, "texts in English"},
	} {
		t.Run(c.name, func(t *testing.T) { expect(t, withNumberRange(t, c.change), c.code, c.contains) })
	}
}
