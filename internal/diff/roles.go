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
	"reflect"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// roles lists the changes of package roles. Local roles change only
// through their own operations. Roles carry no data; that removed roles
// are no longer used is checked by the validation and, for assignments,
// by the kernel.
func roles(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.RoleNames() {
		w := want.Roles[name]
		if w.Owner == model.Local {
			continue
		}
		c, ok := cur.Roles[name]
		if !ok {
			detail := fmt.Sprintf("%d authorizations", len(w.Authorizations))
			if w.Composite {
				detail = "composite of " + strings.Join(w.Roles, ", ")
			}
			out = append(out, plan.Change{Kind: plan.CreateRole, Name: name, Detail: detail})
			continue
		}
		var details []string
		if !maps.Equal(c.Texts, w.Texts) {
			details = append(details, "texts")
		}
		if !slices.Equal(c.Roles, w.Roles) {
			details = append(details, "roles "+strings.Join(w.Roles, ", "))
		}
		if !sameAuthorizations(c.Authorizations, w.Authorizations) {
			details = append(details, fmt.Sprintf("%d authorizations", len(w.Authorizations)))
		}
		if len(details) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterRole, Name: name, Detail: strings.Join(details, ", ")})
		}
	}
	for _, name := range cur.RoleNames() {
		if c := cur.Roles[name]; c.Owner != model.Local && want.Roles[name] == nil {
			out = append(out, plan.Change{Kind: plan.DropRole, Name: name})
		}
	}
	return out
}

// sameAuthorizations compares authorizations by their stored form.
func sameAuthorizations(a, b []model.Authorization) bool {
	strip := func(as []model.Authorization) []model.Authorization {
		out := make([]model.Authorization, len(as))
		for i, x := range as {
			fields := map[string][]model.ValueEntry{}
			for f, es := range x.Fields {
				for _, e := range es {
					e.LowJSON, e.HighJSON = nil, nil
					fields[f] = append(fields[f], e)
				}
			}
			out[i] = model.Authorization{Object: x.Object, Fields: fields}
		}
		return out
	}
	return reflect.DeepEqual(strip(a), strip(b))
}
