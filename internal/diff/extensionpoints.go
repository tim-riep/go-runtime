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

// extensionPoints lists the changes of extension points and
// implementations. Implementations of other packages rely on the filter
// fields, the use and the interface of an extension point, so removing a
// point or a filter field, changing the type of a field, switching between
// single and multiple, another interface and forbidding commits are
// destructive. New filter fields, texts, the fallback and allowing commits
// are not; a new field without default fails validation as long as an
// implementation does not name it.
func extensionPoints(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.ExtensionPointNames() {
		w := want.ExtensionPoints[name]
		c, ok := cur.ExtensionPoints[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateExtensionPoint, Name: name, Detail: fmt.Sprintf("%s, %d filter fields", w.Use, len(w.Filters))})
			continue
		}
		var details []string
		destructive := false
		note := func(d bool, format string, args ...any) {
			details = append(details, fmt.Sprintf(format, args...))
			destructive = destructive || d
		}
		if c.Interface != w.Interface {
			note(true, "interface %s -> %s", c.Interface, w.Interface)
		}
		if c.Use != w.Use {
			note(true, "use %s -> %s", c.Use, w.Use)
		}
		if c.Fallback != w.Fallback {
			note(false, "fallback %s -> %s", c.Fallback.Or("none"), w.Fallback.Or("none"))
		}
		if c.CommitAllowed != w.CommitAllowed {
			note(c.CommitAllowed, "commit allowed %t -> %t", c.CommitAllowed, w.CommitAllowed)
		}
		for _, cf := range c.Filters {
			wf, ok := w.Filter(cf.Name)
			if !ok {
				note(true, "filter field %s removed", cf.Name)
				continue
			}
			if detail, d, changed := compareAuthorizationFields(cur, want, filterAsField(cf), filterAsField(wf)); changed {
				note(d, "filter field %s: %s", cf.Name, detail)
			}
			if !sameEntries(cf.Default, wf.Default) {
				note(false, "filter field %s: default", cf.Name)
			}
		}
		for _, wf := range w.Filters {
			if _, ok := c.Filter(wf.Name); !ok {
				note(false, "filter field %s added", wf.Name)
			}
		}
		if len(details) == 0 && !slices.Equal(filterNames(c), filterNames(w)) {
			note(false, "filter fields reordered")
		}
		if !maps.Equal(c.Description, w.Description) {
			note(false, "description")
		}
		if len(details) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterExtensionPoint, Name: name, Detail: strings.Join(details, ", "), Destructive: destructive})
		}
	}
	for _, name := range cur.ExtensionPointNames() {
		if _, ok := want.ExtensionPoints[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropExtensionPoint, Name: name, Destructive: true, Detail: "code that runs it does not compile any more"})
		}
	}
	for _, name := range want.ImplementationNames() {
		w := want.Implementations[name]
		c, ok := cur.Implementations[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateImplementation, Name: name, Detail: "of " + w.ExtensionPoint + " by " + w.Class})
			continue
		}
		var details []string
		if c.ExtensionPoint != w.ExtensionPoint {
			details = append(details, "extension point "+c.ExtensionPoint+" -> "+w.ExtensionPoint)
		}
		if c.Class != w.Class {
			details = append(details, "class "+c.Class+" -> "+w.Class)
		}
		if c.Order != w.Order {
			details = append(details, fmt.Sprintf("order %d -> %d", c.Order, w.Order))
		}
		if !maps.EqualFunc(c.Filters, w.Filters, func(a, b []model.ValueEntry) bool { return sameEntries(model.Some(a), model.Some(b)) }) {
			details = append(details, "filter values")
		}
		if !maps.Equal(c.Description, w.Description) {
			details = append(details, "description")
		}
		if len(details) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterImplementation, Name: name, Detail: strings.Join(details, ", ")})
		}
	}
	for _, name := range cur.ImplementationNames() {
		if _, ok := want.Implementations[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropImplementation, Name: name, Detail: "of " + cur.Implementations[name].ExtensionPoint})
		}
	}
	return out
}

func filterAsField(f model.ExtensionFilter) model.AuthorizationField {
	return model.AuthorizationField{Name: f.Name, DataElement: f.DataElement, Domain: f.Domain}
}

func filterNames(p *model.ExtensionPoint) []string {
	var out []string
	for _, f := range p.Filters {
		out = append(out, f.Name)
	}
	return out
}

// sameEntries compares entries by their canonical JSON form, which is the
// same for entries read from files and from the repository.
func sameEntries(a, b model.Optional[[]model.ValueEntry]) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
