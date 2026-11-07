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

func TestOptionalJSON(t *testing.T) {
	type doc struct {
		A Optional[string] `json:"a"`
		B Optional[string] `json:"b"`
		C Optional[string] `json:"c"`
	}
	b, err := json.Marshal(doc{A: None[string](), B: Some(""), C: Some("x")})
	if err != nil || string(b) != `{"a":null,"b":"","c":"x"}` {
		t.Fatalf("marshal: %s %v", b, err)
	}
	var d doc
	if err := json.Unmarshal([]byte(`{"a":null,"b":""}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.A.Valid() || !d.B.Valid() || d.B.Or("?") != "" || d.C.Valid() {
		t.Fatalf("unmarshal: %+v", d)
	}
	if None[string]().String() != "<null>" || Some("").String() != `""` {
		t.Fatal("display must distinguish null and the empty string")
	}
}

func TestDefaultJSON(t *testing.T) {
	type doc struct {
		Default Default `json:"default,omitzero"`
	}
	for _, c := range []struct {
		d    Default
		want string
	}{
		{NoDefault(), `{}`},
		{NullDefault(), `{"default":null}`},
		{ValueDefault(""), `{"default":""}`},
		{ValueDefault("x"), `{"default":"x"}`},
	} {
		b, err := json.Marshal(doc{c.d})
		if err != nil || string(b) != c.want {
			t.Errorf("%v: %s %v", c.d, b, err)
		}
		var back doc
		if err := json.Unmarshal(b, &back); err != nil || back.Default != c.d {
			t.Errorf("round trip of %v: %+v %v", c.d, back.Default, err)
		}
	}
}
