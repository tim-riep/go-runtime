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
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// subobjectTypes are the base types of subobjects of number ranges, whose
// values are kept in text form.
var subobjectTypes = []model.BaseType{model.Char, model.Int4, model.Int8}

func (v *validator) numberRanges() {
	for _, name := range v.s.NumberRangeNames() {
		v.numberRange(v.s.NumberRanges[name])
	}
}

func (v *validator) numberRange(n *model.NumberRange) {
	obj := "number range " + n.Name
	loc := n.Src
	v.objectName(n.Name, n.Owner, loc.At("/name"), obj)
	v.labels(obj, loc, n.Texts, n.Languages())
	if n.Length < 1 || n.Length > model.MaxNumberLength {
		v.fail(rterr.CodeNumberRange, loc.At("/length"), obj, "numbers have 1 to %d digits, not %d", model.MaxNumberLength, n.Length)
	}
	switch n.Buffering {
	case model.BufferingNone:
		if n.BlockSize != 0 {
			v.fail(rterr.CodeNumberRange, loc.At("/buffering"), obj, "a block size needs the buffering mode %s", model.BufferingBuffered)
		}
	case model.BufferingBuffered:
		if n.BlockSize < 1 || n.BlockSize > model.MaxNumberRangeBlock {
			v.fail(rterr.CodeNumberRange, loc.At("/buffering/blockSize"), obj, "a block has 1 to %d numbers, not %d", model.MaxNumberRangeBlock, n.BlockSize)
		}
	default:
		v.fail(rterr.CodeNumberRange, loc.At("/buffering/mode"), obj, "the buffering mode is %s or %s, not %q", model.BufferingNone, model.BufferingBuffered, n.Buffering)
	}
	if n.WarningPercent < 1 || n.WarningPercent > 100 {
		v.fail(rterr.CodeNumberRange, loc.At("/warningPercent"), obj, "the warning threshold is 1 to 100 percent, not %d", n.WarningPercent)
	}
	s, ok := n.Subobject.Get()
	if !ok {
		return
	}
	sloc := loc.At("/subobject")
	if !model.ValidLocalName(s.Name) || len(s.Name) > model.MaxAuthorizationFieldName {
		v.fail(rterr.CodeName, sloc.At("/name"), obj, "the name of a subobject consists of up to %d upper-case letters, digits and underscores and starts with a letter", model.MaxAuthorizationFieldName)
	}
	de, hasDE := s.DataElement.Get()
	dom, hasDom := s.Domain.Get()
	if hasDE == hasDom {
		v.fail(rterr.CodeDataElement, sloc, obj, "a subobject references either a data element or a domain")
		return
	}
	if d := v.valueField(de, dom, hasDE, n.Owner, sloc, obj); d != nil && !slices.Contains(subobjectTypes, d.Type) {
		v.fail(rterr.CodeNumberRange, sloc, obj, "type %s is not allowed; a subobject is CHAR(n), INT4 or INT8", d.TypeString())
	}
}
