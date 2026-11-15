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

package diff

import (
	"fmt"
	"maps"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// numberRanges lists the changes of number range objects. Intervals in the
// tenants hold numbers of the old length for the old subobject and years,
// so a shorter length, another subobject and another division by year are
// destructive and remove the intervals; a longer length, texts, buffering
// and the warning threshold are not.
func numberRanges(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.NumberRangeNames() {
		w := want.NumberRanges[name]
		c, ok := cur.NumberRanges[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateNumberRange, Name: name, Detail: fmt.Sprintf("%d digits", w.Length)})
			continue
		}
		if detail, destructive := NumberRangeChange(cur, want, c, w); detail != "" {
			if destructive {
				detail += "; removes its intervals in all tenants"
			}
			out = append(out, plan.Change{Kind: plan.AlterNumberRange, Name: name, Detail: detail, Destructive: destructive})
		}
	}
	for _, name := range cur.NumberRangeNames() {
		if _, ok := want.NumberRanges[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropNumberRange, Name: name, Destructive: true, Detail: "removes its intervals in all tenants"})
		}
	}
	return out
}

// NumberRangeChange describes how a number range object changes and
// whether the change removes its intervals; "" means no change.
func NumberRangeChange(cur, want *model.Schema, c, w *model.NumberRange) (string, bool) {
	var details []string
	destructive := false
	note := func(d bool, format string, args ...any) {
		details = append(details, fmt.Sprintf(format, args...))
		destructive = destructive || d
	}
	if c.Length != w.Length {
		note(w.Length < c.Length, "length %d -> %d", c.Length, w.Length)
	}
	cs, cok := c.Subobject.Get()
	ws, wok := w.Subobject.Get()
	switch {
	case cok && !wok:
		note(true, "subobject %s removed", cs.Name)
	case !cok && wok:
		note(true, "subobject %s added", ws.Name)
	case cok && wok && (cs.Name != ws.Name || subobjectType(cur, cs) != subobjectType(want, ws)):
		note(true, "subobject %s %s -> %s %s", cs.Name, subobjectType(cur, cs), ws.Name, subobjectType(want, ws))
	case cok && wok && cs != ws:
		note(false, "subobject %s: reference", cs.Name)
	}
	if c.PerFiscalYear != w.PerFiscalYear {
		note(true, "per fiscal year %t -> %t", c.PerFiscalYear, w.PerFiscalYear)
	}
	if c.Buffering != w.Buffering || c.BlockSize != w.BlockSize {
		note(false, "buffering %s -> %s", bufferingText(c), bufferingText(w))
	}
	if c.WarningPercent != w.WarningPercent {
		note(false, "warning at %d%% -> %d%%", c.WarningPercent, w.WarningPercent)
	}
	if !maps.Equal(c.Texts, w.Texts) {
		note(false, "texts")
	}
	return strings.Join(details, ", "), destructive
}

// subobjectType is the type of the domain of a subobject.
func subobjectType(s *model.Schema, sub model.NumberRangeSubobject) string {
	dom := sub.Domain.Or("")
	if de, ok := sub.DataElement.Get(); ok {
		if d := s.DataElements[de]; d != nil {
			dom = d.Domain
		}
	}
	if d := s.Domains[dom]; d != nil {
		return d.TypeString()
	}
	return "?"
}

func bufferingText(n *model.NumberRange) string {
	if n.Buffering == model.BufferingBuffered {
		return fmt.Sprintf("buffered(%d)", n.BlockSize)
	}
	return string(n.Buffering)
}
