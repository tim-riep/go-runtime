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

package pkgload

import (
	"fmt"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/schemas"
)

// RolesDir holds the roles of a package, one per file named after the
// role.
const RolesDir = SchemaDir + "/roles"

type roleDoc struct {
	Name           string                 `json:"name"`
	Texts          map[string]model.Label `json:"texts"`
	Authorizations *[]model.Authorization `json:"authorizations"`
	Roles          []string               `json:"roles"`
}

func init() {
	register(ObjectType{
		Kind: "role", Document: jsonIn(RolesDir), Schema: schemas.Role,
		NewDoc:      func() any { return &roleDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*roleDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*roleDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*roleDoc)
			var out []NameRef
			if d.Authorizations != nil {
				for i, a := range *d.Authorizations {
					out = append(out, ref(a.Object, fmt.Sprintf("/authorizations/%d/object", i))...)
				}
			}
			for i, r := range d.Roles {
				out = append(out, ref(r, fmt.Sprintf("/roles/%d", i))...)
			}
			return out
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			pkg.Roles = append(pkg.Roles, RoleOf(doc.(*roleDoc), l.owner(), file, l.loc(file)))
		},
	})
}

// RoleOf builds a role from its document.
func RoleOf(d *roleDoc, owner model.PackageID, file string, loc rterr.Location) model.Role {
	r := model.Role{Name: d.Name, Owner: owner, File: file, Texts: d.Texts, Roles: d.Roles, Composite: d.Roles != nil, Src: loc}
	if d.Authorizations != nil {
		r.Authorizations = *d.Authorizations
	}
	return r
}
