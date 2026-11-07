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
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// authorizationObjects lists the changes of authorization objects. Grants
// will name their fields and activities, so removing an object, a field or
// an activity and changing the type of a field are destructive; new
// fields, new activities, texts and the order are not.
func authorizationObjects(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.AuthorizationObjectNames() {
		w := want.AuthorizationObjects[name]
		c, ok := cur.AuthorizationObjects[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateAuthorizationObject, Name: name, Detail: fmt.Sprintf("%d fields", len(w.Fields))})
			continue
		}
		var details []string
		destructive := false
		note := func(d bool, format string, args ...any) {
			details = append(details, fmt.Sprintf(format, args...))
			destructive = destructive || d
		}
		for _, cf := range c.Fields {
			wf, ok := w.Field(cf.Name)
			if !ok {
				note(true, "field %s removed", cf.Name)
				continue
			}
			if detail, d, changed := compareAuthorizationFields(cur, want, cf, wf); changed {
				note(d, "field %s: %s", cf.Name, detail)
			}
		}
		for _, wf := range w.Fields {
			if _, ok := c.Field(wf.Name); !ok {
				note(false, "field %s added", wf.Name)
			}
		}
		if !slices.Equal(fieldNames(c), fieldNames(w)) && len(details) == 0 {
			note(false, "fields reordered")
		}
		for _, a := range c.Activities {
			if !slices.Contains(w.Activities, a) {
				note(true, "activity %s removed", a)
			}
		}
		for _, a := range w.Activities {
			if !slices.Contains(c.Activities, a) {
				note(false, "activity %s added", a)
			}
		}
		if len(details) == 0 && !slices.Equal(c.Activities, w.Activities) {
			note(false, "activities reordered")
		}
		if !maps.Equal(c.Texts, w.Texts) {
			note(false, "texts")
		}
		if len(details) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterAuthorizationObject, Name: name, Detail: strings.Join(details, ", "), Destructive: destructive})
		}
	}
	for _, name := range cur.AuthorizationObjectNames() {
		if _, ok := want.AuthorizationObjects[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropAuthorizationObject, Name: name, Destructive: true, Detail: "grants of its fields become invalid"})
		}
	}
	return out
}

func fieldNames(o *model.AuthorizationObject) []string {
	var out []string
	for _, f := range o.Fields {
		out = append(out, f.Name)
	}
	return out
}

// compareAuthorizationFields compares a field by its reference and its
// resolved type: another data element or domain is destructive, like
// another base type or a shorter CHAR; a longer CHAR is not.
func compareAuthorizationFields(cur, want *model.Schema, c, w model.AuthorizationField) (string, bool, bool) {
	if c.DataElement != w.DataElement || c.Domain != w.Domain {
		return fmt.Sprintf("type %s -> %s", fieldRef(c), fieldRef(w)), true, true
	}
	cd, wd := cur.Domains[cur.AuthorizationFieldDomain(c)], want.Domains[want.AuthorizationFieldDomain(w)]
	if cd == nil || wd == nil || cd.TypeString() == wd.TypeString() {
		return "", false, false
	}
	detail := cd.TypeString() + " -> " + wd.TypeString()
	return detail, !(cd.Type == model.Char && wd.Type == model.Char && wd.Length > cd.Length), true
}

func fieldRef(f model.AuthorizationField) string {
	if f.IsActivity() && !f.DataElement.Valid() && !f.Domain.Valid() {
		return "activity"
	}
	if de, ok := f.DataElement.Get(); ok {
		return "data element " + de
	}
	return "domain " + f.Domain.Or("")
}
