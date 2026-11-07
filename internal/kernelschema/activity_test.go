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

package kernelschema_test

import (
	"slices"
	"testing"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/model"
)

// TestActivities checks that the activities of the kernel binary match the
// fixed values of the domain of the field ACTIVITY, and that the codes of
// the first version never change: new activities only come at the end.
func TestActivities(t *testing.T) {
	first := []string{"READ", "CREATE", "UPDATE", "DELETE", "EXECUTE", "APPROVE", "REJECT", "CANCEL", "IMPORT", "EXPORT", "PRINT", "SEND", "ADMINISTER"}
	var codes []string
	for _, a := range model.Activities {
		codes = append(codes, a.Code)
		for _, l := range []string{"en", "de"} {
			if txt := a.Texts[l]; txt.Label == "" || txt.Description == "" {
				t.Errorf("%s has no text in %s", a.Code, l)
			}
		}
	}
	if len(codes) < len(first) || !slices.Equal(codes[:len(first)], first) {
		t.Fatalf("codes changed: %v", codes)
	}
	s, err := kernelschema.Schema(kernelschema.Latest())
	if err != nil {
		t.Fatal(err)
	}
	d := s.Domains[model.ActivityDomain]
	if d == nil || s.DataElements[model.ActivityDomain] == nil {
		t.Fatal("the kernel has no domain or data element ACTIVITY")
	}
	var values []string
	for _, v := range d.Values {
		values = append(values, v.Value)
		if n, ok := v.CodeName(); !ok || n != v.Value {
			t.Errorf("fixed value %s has the name %s in code", v.Value, n)
		}
	}
	if !slices.Equal(values, codes) {
		t.Errorf("fixed values %v differ from the activities %v", values, codes)
	}
	for _, a := range model.Activities {
		if len(a.Code) > d.Length {
			t.Errorf("%s is longer than the domain", a.Code)
		}
	}
}
