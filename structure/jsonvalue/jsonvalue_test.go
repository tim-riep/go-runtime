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

package jsonvalue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
)

func definition() *structure.Definition {
	row := &structure.Definition{Name: "/T/ROW", Kind: structure.KindStructure, Components: []structure.Component{
		{Name: "K", Kind: structure.Elementary, Type: model.Char, Length: 2, NotNull: true},
	}}
	rows := &structure.Definition{Name: "/T/ROWS", Kind: structure.KindArrayType,
		Array: &structure.ArrayDef{Row: structure.Component{Kind: structure.Nested, Structure: row}, Key: &structure.Key{Fields: []string{"K"}, Unique: true}}}
	return &structure.Definition{Name: "/T/S", Kind: structure.KindStructure, Components: []structure.Component{
		{Name: "NAME", Kind: structure.Elementary, Type: model.Char, Length: 5, AllowEmpty: model.Some(true)},
		{Name: "CODE", Kind: structure.Elementary, Type: model.Char, Length: 5, AllowEmpty: model.Some(false)},
		{Name: "AMOUNT", Kind: structure.Elementary, Type: model.Dec, Length: 7, Decimals: 2},
		{Name: "COUNT", Kind: structure.Elementary, Type: model.Int4, NotNull: true, Default: model.ValueDefault("3")},
		{Name: "STATUS", Kind: structure.Elementary, Type: model.Char, Length: 1,
			FixedValues: []model.FixedValue{{Value: "a", Name: model.Some("ACTIVE")}, {Value: "c", Name: model.Some("CLOSED")}}},
		{Name: "ROWS", Kind: structure.Array, ArrayType: rows},
	}}
}

func TestCanonical(t *testing.T) {
	v, problems := Check(definition(), json.RawMessage(`{"NAME":"","AMOUNT":12.5,"STATUS":"CLOSED","ROWS":[{"K":"x"}]}`))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	want := `{"NAME":"","CODE":null,"AMOUNT":"12.50","COUNT":3,"STATUS":"CLOSED","ROWS":[{"K":"x"}]}`
	if got := string(v.Canonical()); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if s := v.Field("STATUS"); s.Text != "c" {
		t.Errorf("status text %q", s.Text)
	}
	if v.Field("NAME").Null || !v.Field("CODE").Null {
		t.Error("null and empty string mixed up")
	}
}

func TestProblems(t *testing.T) {
	for doc, want := range map[string]string{
		`{"CODE":""}`:                    "does not allow the empty string",
		`{"NAME":"toolong"}`:             "CHAR(5)",
		`{"STATUS":"c"}`:                 "no fixed value",
		`{"COUNT":null}`:                 "null is not allowed",
		`{"OTHER":1}`:                    "has no component OTHER",
		`{"ROWS":[{"K":"a"},{"K":"a"}]}`: "same key",
		`{"ROWS":[null]}`:                "cannot be null",
		`{"AMOUNT":"1.234"}`:             "decimal places",
		`[]`:                             "expected an object",
		`{"ROWS":[{}]}`:                  "needs K",
	} {
		_, problems := Check(definition(), json.RawMessage(doc))
		var all []string
		for _, p := range problems {
			all = append(all, p.String())
		}
		if !strings.Contains(strings.Join(all, "\n"), want) {
			t.Errorf("%s: want %q, got %v", doc, want, all)
		}
	}
}

func TestSchema(t *testing.T) {
	s := Schema(definition(), "en")
	b, _ := json.Marshal(s)
	for _, want := range []string{`"additionalProperties":false`, `"enum":["ACTIVE","CLOSED"]`, `"$ref":"#/$defs/~1T~1ROWS"`, `"minLength":1`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("schema lacks %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), `"required":["COUNT"]`) {
		t.Error("a component with a default is not required")
	}
}
