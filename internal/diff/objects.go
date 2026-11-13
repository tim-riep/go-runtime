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
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// objects lists the changes of package-defined object types and their
// objects. They hold no data of tables, so no change is destructive. An
// object changes when its canonical content does.
func objects(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.ObjectTypeNames() {
		w := want.ObjectTypes[name]
		c, ok := cur.ObjectTypes[name]
		switch {
		case !ok:
			out = append(out, plan.Change{Kind: plan.CreateObjectType, Name: name, Detail: "content " + w.Content})
		case !sameJSON(c, w):
			out = append(out, plan.Change{Kind: plan.AlterObjectType, Name: name, Detail: "content " + w.Content})
		}
	}
	for _, name := range cur.ObjectTypeNames() {
		if _, ok := want.ObjectTypes[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropObjectType, Name: name})
		}
	}
	for _, name := range want.ObjectNames() {
		w := want.Objects[name]
		c, ok := cur.Objects[name]
		switch {
		case !ok:
			out = append(out, plan.Change{Kind: plan.CreateObject, Name: name, Detail: "of type " + w.Type})
		case c.Hash != w.Hash || c.Type != w.Type:
			out = append(out, plan.Change{Kind: plan.AlterObject, Name: name, Detail: "of type " + w.Type})
		}
	}
	for _, name := range cur.ObjectNames() {
		if _, ok := want.Objects[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropObject, Name: name, Detail: "of type " + cur.Objects[name].Type})
		}
	}
	return out
}
