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
	"encoding/json"
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/schemas"
)

// ServicesDir holds the services of a package, one per file named after
// the service.
const ServicesDir = SchemaDir + "/services"

type entitySetDoc struct {
	BusinessObject model.Optional[string]                  `json:"businessObject"`
	Entity         model.Optional[string]                  `json:"entity"`
	Table          model.Optional[string]                  `json:"table"`
	ObjectType     model.Optional[string]                  `json:"objectType"`
	ReadOnly       bool                                    `json:"readOnly"`
	Operations     model.Optional[[]model.EntityOperation] `json:"operations"`
	Actions        model.Optional[[]string]                `json:"actions"`
	HiddenFields   []string                                `json:"hiddenFields"`
	Navigations    map[string]string                       `json:"navigations"`
}

type serviceDoc struct {
	Name        string                   `json:"name"`
	Description map[string]string        `json:"description"`
	Extensions  json.RawMessage          `json:"extensions"`
	EntitySets  map[string]*entitySetDoc `json:"entitySets"`
}

func init() {
	register(ObjectType{
		Kind: "service", Document: jsonIn(ServicesDir), Schema: schemas.Service,
		NewDoc:      func() any { return &serviceDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*serviceDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*serviceDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*serviceDoc)
			var out []NameRef
			field := func(name, pointer string) {
				if isGlobal(name) {
					out = append(out, ref(name, pointer)...)
				}
			}
			for _, name := range slices.Sorted(maps.Keys(d.EntitySets)) {
				e := d.EntitySets[name]
				at := "/entitySets/" + name
				out = append(out, ref(e.BusinessObject.Or(""), at+"/businessObject")...)
				out = append(out, ref(e.Entity.Or(""), at+"/entity")...)
				out = append(out, ref(e.Table.Or(""), at+"/table")...)
				out = append(out, ref(e.ObjectType.Or(""), at+"/objectType")...)
				for _, a := range e.Actions.Or(nil) {
					field(a, at+"/actions")
				}
				for _, f := range e.HiddenFields {
					field(f, at+"/hiddenFields")
				}
				for _, fk := range slices.Sorted(maps.Keys(e.Navigations)) {
					field(fk, at+"/navigations/"+fk)
				}
			}
			return out
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*serviceDoc)
			loc := l.loc(file)
			svc := model.Service{Name: d.Name, Owner: l.owner(), File: file, Description: d.Description, Extensions: model.ExtensionsAll, Src: loc}
			var rule string
			var listed []model.PackageID
			switch {
			case len(d.Extensions) == 0:
			case json.Unmarshal(d.Extensions, &rule) == nil:
				svc.Extensions = model.ExtensionRule(rule)
			case json.Unmarshal(d.Extensions, &listed) == nil:
				svc.Extensions, svc.ExtensionPackages = model.ExtensionsListed, listed
				slices.Sort(svc.ExtensionPackages)
			}
			for _, name := range slices.Sorted(maps.Keys(d.EntitySets)) {
				e := d.EntitySets[name]
				set := model.EntitySet{Name: name, BusinessObject: e.BusinessObject, Entity: e.Entity, Table: e.Table, ObjectType: e.ObjectType,
					ReadOnly: e.ReadOnly || e.Table.Valid() || e.ObjectType.Valid(), Operations: e.Operations, Actions: e.Actions, HiddenFields: e.HiddenFields,
					Navigations: e.Navigations, Src: loc.At("/entitySets/" + name)}
				svc.EntitySets = append(svc.EntitySets, set)
			}
			pkg.Services = append(pkg.Services, svc)
		},
	})
}
