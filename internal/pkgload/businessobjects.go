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
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
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
		Name          string                     `json:"name"`
		Instance      *bool                      `json:"instance"`
		Parameter     model.Optional[string]     `json:"parameter"`
		Result        model.Optional[string]     `json:"result"`
		Modifies      *bool                      `json:"modifies"`
		Activity      model.Optional[string]     `json:"activity"`
		Authorization *model.ActionAuthorization `json:"authorization"`
	} `json:"actions"`
	Authorization *model.EntityAuthorization            `json:"authorization"`
	Extensible    model.Optional[[]model.ExtensionKind] `json:"extensible"`
}

type businessObjectDoc struct {
	Name          string                                `json:"name"`
	Description   map[string]string                     `json:"description"`
	Root          string                                `json:"root"`
	DataClass     string                                `json:"dataClass"`
	BehaviorClass model.Optional[string]                `json:"behaviorClass"`
	Extensible    model.Optional[[]model.ExtensionKind] `json:"extensible"`
	Entities      map[string]*entityDoc                 `json:"entities"`
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
			return append(out, entityRefs(d.Entities)...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*businessObjectDoc)
			loc := l.loc(file)
			b := model.BusinessObject{Name: d.Name, Owner: l.owner(), File: file, Description: d.Description, Root: d.Root,
				DataClass: model.DataClass(d.DataClass), BehaviorClass: d.BehaviorClass, Extensible: d.Extensible, Src: loc}
			if b.DataClass == "" {
				b.DataClass = model.Application
			}
			var entities []model.Entity
			for table, e := range d.Entities {
				entities = append(entities, e.entity(table, loc.At("/entities/"+table), false))
			}
			b.Entities = model.SortEntities(d.Root, entities)
			pkg.BusinessObjects = append(pkg.BusinessObjects, b)
		},
	})
}

// entityRefs returns the names the entities of a business object or an
// extension refer to.
func entityRefs(entities map[string]*entityDoc) []NameRef {
	var out []NameRef
	field := func(name, pointer string) {
		if isGlobal(name) {
			out = append(out, ref(name, pointer)...)
		}
	}
	for _, table := range slices.Sorted(maps.Keys(entities)) {
		e := entities[table]
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
			if au := a.Authorization; au != nil {
				out = append(out, ref(au.Object, at+"/actions/"+a.Name+"/authorization/object")...)
				for _, f := range slices.Sorted(maps.Keys(au.Fields)) {
					field(au.Fields[f], at+"/actions/"+a.Name+"/authorization/fields/"+f)
				}
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
}

// entity returns the model of an entity. Entities of extensions without
// parent extend another entity; they keep their numbering empty when the
// file gives none, so that validation finds a numbering they may not set.
func (e *entityDoc) entity(table string, loc rterr.Location, extension bool) model.Entity {
	en := model.Entity{Table: table, Parent: e.Parent, ForeignKey: e.ForeignKey, Operations: e.Operations,
		Numbering: model.Numbering(e.Numbering), Fields: e.Fields, Administrative: e.Administrative, ETag: e.ETag,
		Authorization: e.Authorization, Extensible: e.Extensible, Src: loc}
	if en.Numbering == "" && (!extension || en.Parent.Valid()) {
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
		act := model.Action{Name: a.Name, Instance: a.Instance == nil || *a.Instance, Parameter: a.Parameter,
			Result: a.Result, Modifies: a.Modifies == nil || *a.Modifies, Activity: a.Activity, Authorization: a.Authorization}
		if au := act.Authorization; au != nil && au.Fields == nil {
			au.Fields = map[string]string{}
		}
		en.Actions = append(en.Actions, act)
	}
	return en
}

// BusinessObjectExtensionsDir holds the extensions of business objects of
// other packages, one per file named after the business object.
const BusinessObjectExtensionsDir = SchemaDir + "/business-object-extensions"

type businessObjectExtensionDoc struct {
	BusinessObject string                 `json:"businessObject"`
	BehaviorClass  model.Optional[string] `json:"behaviorClass"`
	Entities       map[string]*entityDoc  `json:"entities"`
}

func init() {
	register(ObjectType{
		Kind: "business object extension", Document: jsonIn(BusinessObjectExtensionsDir), Schema: schemas.BusinessObjectExtension,
		NewDoc:   func() any { return &businessObjectExtensionDoc{} },
		FileName: func(doc any) (string, bool) { return fileNameOf(doc.(*businessObjectExtensionDoc).BusinessObject) },
		// The extension defines its determinations, validations and
		// actions and its own entities, which carry the namespace of the
		// package.
		Definitions: func(doc any) []NameRef {
			d := doc.(*businessObjectExtensionDoc)
			var out []NameRef
			for _, table := range slices.Sorted(maps.Keys(d.Entities)) {
				e := d.Entities[table]
				at := "/entities/" + table
				for i, t := range e.Determinations {
					out = append(out, ref(t.Name, at+"/determinations/"+strconv.Itoa(i)+"/name")...)
				}
				for i, t := range e.Validations {
					out = append(out, ref(t.Name, at+"/validations/"+strconv.Itoa(i)+"/name")...)
				}
				for i, a := range e.Actions {
					out = append(out, ref(a.Name, at+"/actions/"+strconv.Itoa(i)+"/name")...)
				}
			}
			return out
		},
		References: func(doc any) []NameRef {
			d := doc.(*businessObjectExtensionDoc)
			out := append(ref(d.BusinessObject, "/businessObject"), ref(d.BehaviorClass.Or(""), "/behaviorClass")...)
			return append(out, entityRefs(d.Entities)...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*businessObjectExtensionDoc)
			loc := l.loc(file)
			x := model.BusinessObjectExtension{BusinessObject: d.BusinessObject, Package: l.owner(), File: file, BehaviorClass: d.BehaviorClass, Src: loc}
			var extended, own []model.Entity
			for _, table := range slices.Sorted(maps.Keys(d.Entities)) {
				e := d.Entities[table].entity(table, loc.At("/entities/"+table), true)
				if e.Parent.Valid() {
					own = append(own, e)
				} else {
					extended = append(extended, e)
				}
			}
			x.Entities = append(extended, model.SortChildren(own)...)
			pkg.BusinessObjectExtensions = append(pkg.BusinessObjectExtensions, x)
		},
	})
}
