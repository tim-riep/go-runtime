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
	"reflect"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// stripComponents removes what is not stored in the repository, so
// definitions loaded from a package compare equal to those read back.
func stripComponents(cs []model.Component) []model.Component {
	out := make([]model.Component, len(cs))
	for i, c := range cs {
		c.Src, c.DefaultJSON = rterr.Location{}, nil
		out[i] = c
	}
	return out
}

func stripElements(es []model.Element) []model.Element {
	out := make([]model.Element, len(es))
	for i, e := range es {
		e.Src = rterr.Location{}
		if e.Component != nil {
			e.Component = &stripComponents([]model.Component{*e.Component})[0]
		}
		out[i] = e
	}
	return out
}

// structures lists the changes of structures, structure extensions and
// array types. They only update the repository; the columns they bring
// into tables are compared with the tables.
func structures(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.StructureNames() {
		w := want.Structures[name]
		c, ok := cur.Structures[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateStructure, Name: name, Detail: fmt.Sprintf("%d elements", len(w.Elements))})
			continue
		}
		var parts []string
		if !reflect.DeepEqual(c.Texts, w.Texts) {
			parts = append(parts, "texts")
		}
		if !reflect.DeepEqual(stripElements(c.Elements), stripElements(w.Elements)) {
			parts = append(parts, "elements")
		}
		if len(parts) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterStructure, Name: name, Detail: strings.Join(parts, ", ")})
		}
	}
	for _, name := range cur.StructureNames() {
		if _, ok := want.Structures[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropStructure, Name: name})
		}
	}
	for _, w := range want.SortedStructureExtensions() {
		c, ok := cur.StructureExtensions[w.Key()]
		name := w.Structure + " by " + string(w.Package)
		switch {
		case !ok:
			out = append(out, plan.Change{Kind: plan.AddStructureExtension, Name: name})
		case !reflect.DeepEqual(stripComponents(c.Components), stripComponents(w.Components)):
			out = append(out, plan.Change{Kind: plan.AlterStructureExtension, Name: name})
		}
	}
	for _, c := range cur.SortedStructureExtensions() {
		if _, ok := want.StructureExtensions[c.Key()]; !ok {
			out = append(out, plan.Change{Kind: plan.DropStructureExtension, Name: c.Structure + " by " + string(c.Package)})
		}
	}
	for _, name := range want.ArrayTypeNames() {
		w := want.ArrayTypes[name]
		c, ok := cur.ArrayTypes[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateArrayType, Name: name, Detail: "rows " + w.Row.Name})
			continue
		}
		var parts []string
		if !reflect.DeepEqual(c.Texts, w.Texts) {
			parts = append(parts, "texts")
		}
		if c.Row != w.Row {
			parts = append(parts, "rows "+c.Row.Name+" -> "+w.Row.Name)
		}
		if !reflect.DeepEqual(c.Key, w.Key) {
			parts = append(parts, "key")
		}
		if len(parts) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterArrayType, Name: name, Detail: strings.Join(parts, "; ")})
		}
	}
	for _, name := range cur.ArrayTypeNames() {
		if _, ok := want.ArrayTypes[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropArrayType, Name: name})
		}
	}
	return out
}
