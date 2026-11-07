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

package validate

import (
	"fmt"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func (v *validator) roles() {
	for _, name := range v.s.RoleNames() {
		v.role(v.s.Roles[name])
	}
}

// role checks the name, texts and references of a role. Package roles
// reference authorization objects and roles of the own and required
// packages, local roles any. Whether the authorizations fit their objects
// is checked by the kernel, since only the roles of the packages an
// operation installs must fit; see model.CheckAuthorization.
func (v *validator) role(r *model.Role) {
	obj := "role " + r.Name
	loc := r.Src
	v.objectName(r.Name, r.Owner, loc.At("/name"), obj)
	v.labels(obj, loc, r.Texts, r.Languages())
	visible := func(owner model.PackageID) bool { return r.Owner == model.Local || v.s.Visible(r.Owner, owner) }
	if r.Composite {
		if len(r.Authorizations) > 0 {
			v.fail(rterr.CodeRole, loc, obj, "a composite role has roles, no authorizations")
		}
		if len(r.Roles) == 0 {
			v.fail(rterr.CodeRole, loc.At("/roles"), obj, "a composite role has at least one role")
		}
		seen := map[string]bool{}
		for i, name := range r.Roles {
			mloc := loc.At(fmt.Sprintf("/roles/%d", i))
			m := v.s.Roles[name]
			switch {
			case name == model.AllAuthorizationsRole:
				v.fail(rterr.CodeReservedObject, mloc, obj, "%s is no member of composite roles; it is assigned on its own", name)
			case seen[name]:
				v.fail(rterr.CodeDuplicate, mloc, obj, "the role %s is listed twice", name)
			case m == nil:
				v.fail(rterr.CodeRole, mloc, obj, "role %s does not exist", name)
			case m.Composite:
				v.fail(rterr.CodeRole, mloc, obj, "%s is a composite role; composite roles contain single roles only", name)
			case !visible(m.Owner):
				v.fail(rterr.CodeNotVisible, mloc, obj, "role %s belongs to %s, which %s does not require", name, m.Owner, r.Owner)
			}
			seen[name] = true
		}
		return
	}
	for i, a := range r.Authorizations {
		aloc := loc.At(fmt.Sprintf("/authorizations/%d", i))
		o := v.s.AuthorizationObjects[a.Object]
		switch {
		case o == nil:
			v.fail(rterr.CodeRole, aloc.At("/object"), obj, "authorization object %s does not exist", a.Object)
		case !visible(o.Owner):
			v.fail(rterr.CodeNotVisible, aloc.At("/object"), obj, "authorization object %s belongs to %s, which %s does not require", a.Object, o.Owner, r.Owner)
		}
		for _, f := range a.FieldNames() {
			if len(a.Fields[f]) == 0 {
				v.fail(rterr.CodeRole, aloc.At("/fields/"+f), obj, "field %s has no values", f)
			}
		}
	}
}
