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
	"github.com/tim-riep/go-runtime/schemas"
)

// AuthorizationObjectsDir holds the authorization objects of a package, one
// per file named after the object.
const AuthorizationObjectsDir = SchemaDir + "/authorization-objects"

type authorizationObjectDoc struct {
	Name   string                 `json:"name"`
	Texts  map[string]model.Label `json:"texts"`
	Fields []struct {
		Name        string                 `json:"name"`
		DataElement model.Optional[string] `json:"dataElement"`
		Domain      model.Optional[string] `json:"domain"`
	} `json:"fields"`
	Activities []string `json:"activities"`
}

func init() {
	register(ObjectType{
		Kind: "authorization object", Document: jsonIn(AuthorizationObjectsDir), Schema: schemas.AuthorizationObject,
		NewDoc:      func() any { return &authorizationObjectDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*authorizationObjectDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*authorizationObjectDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			var out []NameRef
			for i, f := range doc.(*authorizationObjectDoc).Fields {
				out = append(out, ref(f.DataElement.Or(""), fmt.Sprintf("/fields/%d/dataElement", i))...)
				out = append(out, ref(f.Domain.Or(""), fmt.Sprintf("/fields/%d/domain", i))...)
			}
			return out
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*authorizationObjectDoc)
			o := model.AuthorizationObject{Name: d.Name, Owner: l.owner(), File: file, Texts: d.Texts, Activities: d.Activities, Src: l.loc(file)}
			for _, f := range d.Fields {
				o.Fields = append(o.Fields, model.AuthorizationField{Name: f.Name, DataElement: f.DataElement, Domain: f.Domain})
			}
			pkg.AuthorizationObjects = append(pkg.AuthorizationObjects, o)
		},
	})
}
