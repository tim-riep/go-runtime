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
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/schemas"
)

// BusinessObjectsDir holds the business objects of a package, one per file
// named after the business object.
const BusinessObjectsDir = SchemaDir + "/business-objects"

type triggerDoc struct {
	Name   string                  `json:"name"`
	When   string                  `json:"when"`
	On     []model.EntityOperation `json:"on"`
	Fields []string                `json:"fields"`
}

type entityDoc struct {
	Parent         model.Optional[string]           `json:"parent"`
	ForeignKey     model.Optional[string]           `json:"foreignKey"`
	Operations     []model.EntityOperation          `json:"operations"`
	Numbering      string                           `json:"numbering"`
	Fields         map[string]model.FieldProperties `json:"fields"`
	Administrative model.AdministrativeFields       `json:"administrative"`
	ETag           model.Optional[string]           `json:"etag"`
	Determinations []triggerDoc                     `json:"determinations"`
	Validations    []triggerDoc                     `json:"validations"`
	Actions        []struct {
		Name      string                 `json:"name"`
		Instance  *bool                  `json:"instance"`
		Parameter model.Optional[string] `json:"parameter"`
		Result    model.Optional[string] `json:"result"`
		Modifies  *bool                  `json:"modifies"`
	} `json:"actions"`
	Authorization *model.EntityAuthorization `json:"authorization"`
}

type businessObjectDoc struct {
	Name          string                 `json:"name"`
	Description   map[string]string      `json:"description"`
	Root          string                 `json:"root"`
	DataClass     string                 `json:"dataClass"`
	BehaviorClass model.Optional[string] `json:"behaviorClass"`
	Entities      map[string]*entityDoc  `json:"entities"`
}

// isGlobal reports a name with namespace, such as an extension field.
func isGlobal(name string) bool { return strings.HasPrefix(name, "/") }

func init() {
	register(ObjectType{
		Kind: "business object", Document: jsonIn(BusinessObjectsDir), Schema: schemas.BusinessObject,
		NewDoc:      func() any { return &businessObjectDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*businessObjectDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*businessObjectDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*businessObjectDoc)
			out := append(ref(d.Root, "/root"), ref(d.BehaviorClass.Or(""), "/behaviorClass")...)
			field := func(name, pointer string) {
				if isGlobal(name) {
					out = append(out, ref(name, pointer)...)
				}
			}
			for _, table := range slices.Sorted(maps.Keys(d.Entities)) {
				e := d.Entities[table]
				at := "/entities/" + table
				out = append(out, ref(table, at)...)
				out = append(out, ref(e.Parent.Or(""), at+"/parent")...)
				for _, f := range slices.Sorted(maps.Keys(e.Fields)) {
					field(f, at+"/fields/"+f)
				}
				for _, f := range e.Administrative.Fields() {
					field(f, at+"/administrative")
				}
				field(e.ETag.Or(""), at+"/etag")
				for _, t := range append(slices.Clone(e.Determinations), e.Validations...) {
					for _, f := range t.Fields {
						field(f, at+"/"+t.Name)
					}
				}
				for _, a := range e.Actions {
					out = append(out, ref(a.Parameter.Or(""), at+"/actions/"+a.Name+"/parameter")...)
					if r := a.Result.Or(model.ResultSelf); r != model.ResultSelf {
						out = append(out, ref(r, at+"/actions/"+a.Name+"/result")...)
					}
				}
				if au := e.Authorization; au != nil {
					out = append(out, ref(au.Object, at+"/authorization/object")...)
					for _, f := range slices.Sorted(maps.Keys(au.Fields)) {
						field(au.Fields[f], at+"/authorization/fields/"+f)
					}
				}
			}
			return out
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*businessObjectDoc)
			loc := l.loc(file)
			b := model.BusinessObject{Name: d.Name, Owner: l.owner(), File: file, Description: d.Description, Root: d.Root,
				DataClass: model.DataClass(d.DataClass), BehaviorClass: d.BehaviorClass, Src: loc}
			if b.DataClass == "" {
				b.DataClass = model.Application
			}
			var entities []model.Entity
			for table, e := range d.Entities {
				en := model.Entity{Table: table, Parent: e.Parent, ForeignKey: e.ForeignKey, Operations: e.Operations,
					Numbering: model.Numbering(e.Numbering), Fields: e.Fields, Administrative: e.Administrative, ETag: e.ETag,
					Authorization: e.Authorization, Src: loc.At("/entities/" + table)}
				if en.Numbering == "" {
					en.Numbering = model.NumberingExternal
				}
				if en.Fields == nil {
					en.Fields = map[string]model.FieldProperties{}
				}
				if a := en.Authorization; a != nil {
					if a.Fields == nil {
						a.Fields = map[string]string{}
					}
					if a.Activities == nil {
						a.Activities = map[string]string{}
					}
				}
				for _, t := range e.Determinations {
					en.Determinations = append(en.Determinations, model.Determination{Name: t.Name, When: model.When(t.When),
						Trigger: model.Trigger{On: t.On, Fields: t.Fields}})
				}
				for _, t := range e.Validations {
					en.Validations = append(en.Validations, model.Validation{Name: t.Name, Trigger: model.Trigger{On: t.On, Fields: t.Fields}})
				}
				for _, a := range e.Actions {
					en.Actions = append(en.Actions, model.Action{Name: a.Name, Instance: a.Instance == nil || *a.Instance, Parameter: a.Parameter,
						Result: a.Result, Modifies: a.Modifies == nil || *a.Modifies})
				}
				entities = append(entities, en)
			}
			b.Entities = model.SortEntities(d.Root, entities)
			pkg.BusinessObjects = append(pkg.BusinessObjects, b)
		},
	})
}
