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
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// businessObjects lists the changes of business objects. New entities,
// actions, determinations and validations are allowed, and so are removed
// ones and stricter field properties: compiling the code that uses them
// again decides. Another root, parent, foreign key or numbering of an
// entity and removing a business object are destructive.
func businessObjects(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.BusinessObjectNames() {
		w := want.BusinessObjects[name]
		c, ok := cur.BusinessObjects[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateBusinessObject, Name: name, Detail: fmt.Sprintf("root %s, %d entities", w.Root, len(w.Entities))})
			continue
		}
		var details []string
		destructive := false
		note := func(d bool, format string, args ...any) {
			details = append(details, fmt.Sprintf(format, args...))
			destructive = destructive || d
		}
		if c.Root != w.Root {
			note(true, "root %s -> %s", c.Root, w.Root)
		}
		if c.BehaviorClass != w.BehaviorClass {
			note(false, "behavior class %s -> %s", c.BehaviorClass.Or("none"), w.BehaviorClass.Or("none"))
		}
		if c.DataClass != w.DataClass {
			note(false, "data class %s -> %s", c.DataClass, w.DataClass)
		}
		for _, ce := range c.Entities {
			we := w.Entity(ce.Table)
			if we == nil {
				note(false, "entity %s removed", ce.Table)
				continue
			}
			if ce.Parent != we.Parent || ce.ForeignKey != we.ForeignKey {
				note(true, "entity %s: parent %s by %s -> %s by %s", ce.Table, ce.Parent.Or("none"), ce.ForeignKey.Or("none"), we.Parent.Or("none"), we.ForeignKey.Or("none"))
			}
			if ce.Numbering != we.Numbering {
				note(true, "entity %s: numbering %s -> %s", ce.Table, ce.Numbering, we.Numbering)
			}
			if !sameJSON(behaviorOf(ce), behaviorOf(*we)) {
				note(false, "entity %s: behavior", ce.Table)
			}
		}
		for _, we := range w.Entities {
			if c.Entity(we.Table) == nil {
				note(false, "entity %s added", we.Table)
			}
		}
		if !maps.Equal(c.Description, w.Description) {
			note(false, "description")
		}
		if len(details) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterBusinessObject, Name: name, Detail: strings.Join(details, ", "), Destructive: destructive})
		}
	}
	for _, name := range cur.BusinessObjectNames() {
		if _, ok := want.BusinessObjects[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropBusinessObject, Name: name, Destructive: true, Detail: "code that uses it does not compile any more"})
		}
	}
	return out
}

// businessObjectExtensions compares the extensions of business objects.
// Adding, changing and removing them is not destructive by itself; the
// tables of their own entities follow the rules of tables. A field that
// becomes mandatory gets a notice: rows without a value stay and are
// checked when their instance is next saved.
func businessObjectExtensions(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, w := range want.SortedBusinessObjectExtensions() {
		name := w.BusinessObject + " by " + string(w.Package)
		c, ok := cur.BusinessObjectExtensions[w.Key()]
		if !ok {
			out = append(out, plan.Change{Kind: plan.AddBusinessObjectExtension, Name: name, Detail: fmt.Sprintf("%d entities", len(w.Entities))})
		} else if !sameJSON(c, w) {
			out = append(out, plan.Change{Kind: plan.AlterBusinessObjectExtension, Name: name, Detail: "behavior"})
		}
		for _, e := range w.Entities {
			for _, f := range slices.Sorted(maps.Keys(e.Fields)) {
				was := false
				if c != nil {
					if ce := c.Entity(e.Table); ce != nil {
						was = ce.Fields[f].Mandatory
					}
				}
				if e.Fields[f].Mandatory && !was && !e.Parent.Valid() {
					out = append(out, plan.Change{Kind: plan.AlterBusinessObjectExtension, Name: name, Notice: true,
						Detail: fmt.Sprintf("%s becomes mandatory: existing instances of %s without a value stay and are checked when they are next saved", f, e.Table)})
				}
			}
		}
	}
	for _, c := range cur.SortedBusinessObjectExtensions() {
		if _, ok := want.BusinessObjectExtensions[c.Key()]; !ok {
			out = append(out, plan.Change{Kind: plan.DropBusinessObjectExtension, Name: c.BusinessObject + " by " + string(c.Package),
				Detail: "code that uses its actions and entities does not compile any more"})
		}
	}
	return out
}

// behaviorOf returns what of an entity is behavior: everything except
// the table, its place in the tree and the location.
func behaviorOf(e model.Entity) model.Entity {
	e.Parent, e.ForeignKey, e.Numbering = model.None[string](), model.None[string](), ""
	return e
}

func sameJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
