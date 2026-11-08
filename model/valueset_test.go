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

package model

import (
	"encoding/json"
	"testing"
)

func valueEntries(t *testing.T, js string) []ValueEntry {
	t.Helper()
	var out []ValueEntry
	if err := json.Unmarshal([]byte(js), &out); err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return out
}

func TestEntriesOverlap(t *testing.T) {
	char := &Domain{Name: "/X/CODE", Type: Char, Length: 10}
	int8 := &Domain{Name: "/X/NO", Type: Int8}
	fixed := &Domain{Name: "/X/STATUS", Type: Char, Length: 1, Values: []FixedValue{{Value: "A"}, {Value: "B"}, {Value: "C"}}}
	flag := &Domain{Name: "/X/FLAG", Type: Bool}
	for _, c := range []struct {
		d      *Domain
		a, b   string
		common string // "" for no overlap
	}{
		{char, `[{"value": "DE"}]`, `[{"value": "DE"}, {"value": "AT"}]`, "DE"},
		{char, `[{"value": "DE"}]`, `[{"value": "AT"}]`, ""},
		{char, `[{"value": ""}]`, `[{"value": ""}]`, `""`},
		{char, `[{"pattern": "*"}]`, `[{"value": "AT"}]`, "AT"},
		{char, `[{"value": "AT"}]`, `[{"pattern": "*"}]`, "AT"},
		{char, `[{"from": "A", "to": "F"}]`, `[{"value": "DE"}]`, "DE"},
		{char, `[{"from": "A", "to": "C"}]`, `[{"value": "DE"}]`, ""},
		{char, `[{"from": "A", "to": "F"}]`, `[{"from": "D", "to": "Z"}]`, "D to F"},
		{char, `[{"from": "A", "to": "D"}]`, `[{"from": "D", "to": "Z"}]`, "D"},
		{char, `[{"from": "A", "to": "C"}]`, `[{"from": "D", "to": "Z"}]`, ""},
		{char, `[{"pattern": "D*"}]`, `[{"value": "DE"}]`, "DE"},
		{char, `[{"pattern": "D*"}]`, `[{"value": "AT"}]`, ""},
		{char, `[{"pattern": "D*"}]`, `[{"pattern": "DE*"}]`, "DE*"},
		{char, `[{"pattern": "D*"}]`, `[{"pattern": "E*"}]`, ""},
		{char, `[{"pattern": "D*"}]`, `[{"from": "A", "to": "DA"}]`, "D* from A to DA"},
		{char, `[{"pattern": "D*"}]`, `[{"from": "DZ", "to": "F"}]`, "D* from DZ to F"},
		{char, `[{"pattern": "D*"}]`, `[{"from": "E", "to": "F"}]`, ""},
		{char, `[{"pattern": "D*"}]`, `[{"from": "A", "to": "CZ"}]`, ""},
		{int8, `[{"from": 1, "to": 9}]`, `[{"value": 10}]`, ""},
		{int8, `[{"from": 1, "to": 10}]`, `[{"value": 10}]`, "10"},
		{int8, `[{"pattern": "12*"}]`, `[{"from": 100, "to": 119}]`, ""},
		{int8, `[{"pattern": "12*"}]`, `[{"from": 100, "to": 120}]`, "12* from 100 to 120"},
		{int8, `[{"pattern": "12*"}]`, `[{"from": 13, "to": 119}]`, ""},
		{int8, `[{"pattern": "12*"}]`, `[{"from": 1200, "to": 1200}]`, "12* from 1200 to 1200"},
		{int8, `[{"pattern": "-5*"}]`, `[{"from": -60, "to": -51}]`, "-5* from -60 to -51"},
		{int8, `[{"pattern": "-5*"}]`, `[{"from": -4, "to": 10}]`, ""},
		{int8, `[{"pattern": "-5*"}]`, `[{"from": -49, "to": -6}]`, ""},
		{int8, `[{"pattern": "-*"}]`, `[{"from": -1, "to": 10}]`, "-* from -1 to 10"},
		{int8, `[{"pattern": "-*"}]`, `[{"from": 0, "to": 10}]`, ""},
		{int8, `[{"pattern": "9*"}]`, `[{"from": 9223372036854775807, "to": 9223372036854775807}]`, "9* from 9223372036854775807 to 9223372036854775807"},
		{fixed, `[{"from": "A", "to": "B"}]`, `[{"pattern": "*"}]`, "A, B"},
		{fixed, `[{"value": "A"}]`, `[{"value": "B"}, {"value": "C"}]`, ""},
		{flag, `[{"value": true}]`, `[{"pattern": "*"}]`, "true"},
		{flag, `[{"value": true}]`, `[{"value": false}]`, ""},
	} {
		got, ok := EntriesOverlap(c.d, valueEntries(t, c.a), valueEntries(t, c.b))
		if ok != (c.common != "") || got != c.common {
			t.Errorf("%s: %s and %s: got %q, %v; want %q", c.d.Name, c.a, c.b, got, ok, c.common)
		}
	}
}

func TestImplementationsOverlap(t *testing.T) {
	s := NewSchema()
	s.Domains["/X/CODE"] = &Domain{Name: "/X/CODE", Type: Char, Length: 2}
	s.Domains["/X/NO"] = &Domain{Name: "/X/NO", Type: Int4}
	s.ExtensionPoints["/X/P"] = &ExtensionPoint{Name: "/X/P", Use: UseSingle, Filters: []ExtensionFilter{
		{Name: "COUNTRY", Domain: Some("/X/CODE")},
		{Name: "CLASS", Domain: Some("/X/NO"), Default: Some(valueEntries(t, `[{"pattern": "*"}]`))},
	}}
	impl := func(name, country, class string) *Implementation {
		i := &Implementation{Name: name, ExtensionPoint: "/X/P", Filters: map[string][]ValueEntry{"COUNTRY": valueEntries(t, country)}}
		if class != "" {
			i.Filters["CLASS"] = valueEntries(t, class)
		}
		return i
	}
	a := impl("/X/A", `[{"value": "DE"}, {"value": "AT"}]`, "")
	b := impl("/X/B", `[{"pattern": "A*"}]`, `[{"value": 3}]`)
	got, ok := s.ImplementationsOverlap(a, b)
	if !ok || len(got) != 2 || got[0] != (FilterOverlap{Field: "COUNTRY", Values: "AT"}) || got[1] != (FilterOverlap{Field: "CLASS", Values: "3"}) {
		t.Fatalf("overlap of %s and %s: %v, %v", a.Name, b.Name, got, ok)
	}
	c := impl("/X/C", `[{"pattern": "*"}]`, `[{"from": 4, "to": 9}]`)
	if _, ok := s.ImplementationsOverlap(b, c); ok {
		t.Fatalf("%s and %s overlap", b.Name, c.Name)
	}
	s.ExtensionPoints["/X/P"].Filters = nil
	if got, ok := s.ImplementationsOverlap(b, c); !ok || len(got) != 0 {
		t.Fatalf("implementations of a point without filter fields must overlap: %v, %v", got, ok)
	}
}
