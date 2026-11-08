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

// Package odata derives the model of a service from its definition, the
// business objects and the tables: entity sets, entity types with their
// properties and navigations, and actions, each with its technical and
// its public name. Validation, metadata, queries and changes all work on
// this model.
package odata

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Model is the model of a service.
type Model struct {
	Service *model.Service
	// Namespace is the namespace of the schema in the metadata: the
	// package ID of the service.
	Namespace string
	// Sets are the entity sets in the order of their names.
	Sets []*Set
	// Contained are the entity types of dependent entities of extensions
	// that the service offers through navigations of their parents.
	Contained []*EntityType
	// Imports are the static actions, offered as action imports.
	Imports []*Action
}

// Set is an entity set.
type Set struct {
	Def  *model.EntitySet
	Name string
	Type *EntityType
}

// EntityType is the type of the instances of an entity set or of a
// contained navigation.
type EntityType struct {
	// Name is the public name: the mapped name of the table.
	Name  string
	Table *model.Table
	// BusinessObject and Entity are set for entities of business objects;
	// the business object is merged with the extensions the service
	// offers.
	BusinessObject *model.BusinessObject
	Entity         *model.Entity
	Properties     []*Property
	// Key lists the key properties in the order of the primary key,
	// without the tenant field.
	Key         []*Property
	Navigations []*Navigation
	// Actions are the instance actions bound to the type.
	Actions []*Action
	// Operations are the operations offered.
	Operations []model.EntityOperation
	// ETag is the property with the ETag, if the entity has one.
	ETag *Property
}

// Property is a field offered by a type.
type Property struct {
	Name   string
	Field  *model.Field
	Domain *model.Domain
	// DataElement is the data element of the field, if it has one.
	DataElement *model.DataElement
	Nullable    bool
	Key         bool
	// Computed fields are set by the kernel or the behavior only;
	// Immutable fields only on create.
	Computed  bool
	Immutable bool
	Mandatory bool
}

// Navigation leads from an entity type to related instances.
type Navigation struct {
	Name string
	// Target is the entity set of the target, or nil for a contained
	// navigation, whose instances only exist below their parent.
	Target *Set
	Type   *EntityType
	// Collection navigations lead to the dependent instances of a parent.
	Collection bool
	Nullable   bool
	// Contained navigations lead to dependent entities of extensions.
	Contained bool
	// Partner is the navigation back, if there is one.
	Partner string
	// Constraints map fields of the source to fields of the target for
	// navigations along a foreign key of the source; Reverse ones along a
	// foreign key of the target to the source.
	Constraints []model.ColumnPair
	Reverse     []model.ColumnPair
	// ForeignKey is the foreign key the navigation follows.
	ForeignKey string
}

// Action is an action of a business object offered by the service.
type Action struct {
	Name   string
	Action *model.Action
	// Type is the entity type an instance action is bound to; nil for
	// static actions.
	Type *EntityType
}

// Problem is a reason a service cannot be offered.
type Problem struct {
	At      rterr.Location
	Message string
}

// Property returns the property with the given public name.
func (t *EntityType) Property(name string) *Property {
	for _, p := range t.Properties {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// FieldProperty returns the property of a field by its technical name.
func (t *EntityType) FieldProperty(field string) *Property {
	for _, p := range t.Properties {
		if p.Field.Name == field {
			return p
		}
	}
	return nil
}

// Navigation returns the navigation with the given public name.
func (t *EntityType) Navigation(name string) *Navigation {
	for _, n := range t.Navigations {
		if n.Name == name {
			return n
		}
	}
	return nil
}

// Action returns the bound action with the given public name.
func (t *EntityType) Action(name string) *Action {
	for _, a := range t.Actions {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// Allows reports whether the type offers an operation.
func (t *EntityType) Allows(op model.EntityOperation) bool { return slices.Contains(t.Operations, op) }

// Set returns the entity set with the given public name.
func (m *Model) Set(name string) *Set {
	for _, s := range m.Sets {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Import returns the action import with the given public name.
func (m *Model) Import(name string) *Action {
	for _, a := range m.Imports {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// Types returns all entity types: those of the entity sets, then the
// contained ones.
func (m *Model) Types() []*EntityType {
	var out []*EntityType
	for _, s := range m.Sets {
		out = append(out, s.Type)
	}
	return append(out, m.Contained...)
}

// builder collects the model and the problems of a service.
type builder struct {
	s        *model.Schema
	phys     *model.Physical
	svc      *model.Service
	m        *Model
	problems []Problem
	// bySource finds the entity set of a table.
	bySource map[string]*Set
}

func (b *builder) fail(at rterr.Location, format string, args ...any) {
	b.problems = append(b.problems, Problem{At: at, Message: fmt.Sprintf(format, args...)})
}

// offered reports whether the service offers what a package adds to the
// business objects and tables of another one.
func (b *builder) offered(owner model.PackageID) func(model.PackageID) bool {
	return func(p model.PackageID) bool { return p == owner || b.svc.Offers(p) }
}

// Build derives the model of a service. phys is the physical layout of s;
// nil computes it. The model is complete only without problems.
func Build(s *model.Schema, phys *model.Physical, svc *model.Service) (*Model, []Problem) {
	if phys == nil {
		phys = s.Physical()
	}
	b := &builder{s: s, phys: phys, svc: svc, m: &Model{Service: svc, Namespace: string(svc.Owner)}, bySource: map[string]*Set{}}
	for i := range svc.EntitySets {
		def := &svc.EntitySets[i]
		if t := b.set(def); t != nil {
			set := &Set{Def: def, Name: def.Name, Type: t}
			if other := b.bySource[t.Table.Name]; other != nil {
				b.fail(def.Src, "%s is offered by the entity sets %s and %s; offer an entity once", t.Table.Name, other.Name, def.Name)
				continue
			}
			b.bySource[t.Table.Name] = set
			b.m.Sets = append(b.m.Sets, set)
		}
	}
	if len(b.problems) > 0 {
		return b.m, b.problems
	}
	for _, set := range b.m.Sets {
		b.navigations(set.Type, set)
	}
	b.names()
	return b.m, b.problems
}

// set builds the entity type of an entity set and checks its definition.
func (b *builder) set(def *model.EntitySet) *EntityType {
	if name, ok := def.Table.Get(); ok {
		return b.tableSet(def, name)
	}
	boName := def.BusinessObject.Or("")
	base := b.s.BusinessObjects[boName]
	if base == nil {
		b.fail(def.Src.At("/businessObject"), "the business object %s does not exist", boName)
		return nil
	}
	bo := b.s.MergedBusinessObject(boName, b.offered(base.Owner))
	table := def.Entity.Or("")
	e := bo.Entity(table)
	if e == nil {
		b.fail(def.Src.At("/entity"), "%s is not an entity of %s", table, boName)
		return nil
	}
	t := b.entityType(bo, e, def, false)
	if t == nil {
		return nil
	}
	ops := e.Operations
	if listed, ok := def.Operations.Get(); ok {
		for _, op := range listed {
			if !e.Allows(op) {
				b.fail(def.Src.At("/operations"), "%s of %s does not allow %s; a service offers at most the operations of the entity", table, boName, op)
			}
		}
		ops = listed
	}
	if def.ReadOnly {
		if len(def.Operations.Or(nil)) > 0 {
			b.fail(def.Src.At("/operations"), "a read-only entity set offers no operations")
		}
		ops = nil
	}
	t.Operations = slices.Clone(ops)
	slices.Sort(t.Operations)
	if listed, ok := def.Actions.Get(); ok {
		for _, name := range listed {
			if e.Action(name) == nil {
				b.fail(def.Src.At("/actions"), "%s of %s has no action %s; a service offers at most the actions of the entity", table, boName, name)
			}
		}
	}
	if def.ReadOnly && len(def.Actions.Or(nil)) > 0 {
		b.fail(def.Src.At("/actions"), "a read-only entity set offers no actions")
	}
	if !def.ReadOnly {
		visible := b.s.Requires(b.svc.Owner)
		for i := range e.Actions {
			a := &e.Actions[i]
			nameable := a.Origin == b.svc.Owner || visible[a.Origin]
			if listed, ok := def.Actions.Get(); ok && nameable && !slices.Contains(listed, a.Name) {
				continue
			}
			act := &Action{Name: model.PublicName(a.Name), Action: a}
			if a.Instance {
				act.Type = t
				t.Actions = append(t.Actions, act)
			} else {
				b.m.Imports = append(b.m.Imports, act)
			}
		}
	}
	return t
}

// tableSet builds the entity type of a table without business object.
func (b *builder) tableSet(def *model.EntitySet, name string) *EntityType {
	tab := b.s.Tables[name]
	if tab == nil {
		b.fail(def.Src.At("/table"), "the table %s does not exist", name)
		return nil
	}
	if bo, _ := b.s.BusinessObjectOf(name); bo != nil {
		b.fail(def.Src.At("/table"), "%s belongs to the business object %s; offer it with businessObject and entity", name, bo.Name)
		return nil
	}
	if tab.DataClass != model.Configuration && tab.DataClass != model.RuntimeConfiguration {
		b.fail(def.Src.At("/table"), "%s has the data class %s; a service offers tables without business object only of configuration", name, tab.DataClass)
		return nil
	}
	if def.Operations.Valid() && len(def.Operations.Or(nil)) > 0 {
		b.fail(def.Src.At("/operations"), "a table without business object is offered for reading only")
	}
	if def.Actions.Valid() && len(def.Actions.Or(nil)) > 0 {
		b.fail(def.Src.At("/actions"), "a table without business object has no actions")
	}
	return b.entityType(nil, nil, def, false)
}

// entityType builds the properties of an entity type. bo and e are nil
// for tables without business object; contained types belong to a
// contained navigation.
func (b *builder) entityType(bo *model.BusinessObject, e *model.Entity, def *model.EntitySet, contained bool) *EntityType {
	name := def.Source()
	if e != nil {
		name = e.Table
	}
	tab := b.phys.Tables[name]
	if tab == nil {
		b.fail(def.Src, "the table %s does not exist", name)
		return nil
	}
	t := &EntityType{Name: model.PublicName(name), Table: tab, BusinessObject: bo, Entity: e}
	owner := b.s.Tables[name].Owner
	offered := b.offered(owner)
	var hidden []string
	if def != nil {
		hidden = def.HiddenFields
	}
	for _, f := range hidden {
		if tab.Field(f) == nil {
			b.fail(def.Src.At("/hiddenFields"), "%s has no field %s", name, f)
		}
	}
	parentFields := map[string]bool{}
	if e != nil {
		if fkName, ok := e.ForeignKey.Get(); ok {
			if fk := tab.ForeignKey(fkName); fk != nil {
				for _, c := range fk.Columns {
					parentFields[c.Field] = true
				}
			}
		}
	}
	for i := range tab.Fields {
		f := &tab.Fields[i]
		if f.Origin == model.OriginTenant || (f.Owner != owner && !offered(f.Owner)) {
			continue
		}
		p := &Property{Name: model.PublicName(f.Name), Field: f, Domain: b.phys.Domain(f), Nullable: !f.NotNull,
			Key: slices.Contains(tab.PrimaryKey, f.Name)}
		if de, ok := f.DataElement.Get(); ok {
			p.DataElement = b.phys.DataElements[de]
		}
		if e != nil {
			props := e.Property(f.Name)
			p.Mandatory = props.Mandatory
			// The fields of the foreign key to the parent are set from
			// the parent for contained entities and fixed once created.
			p.Computed = props.ReadOnly || e.Administered(f.Name) || (p.Key && e.Numbering == model.NumberingUUID) ||
				parentFields[f.Name] && contained
			p.Immutable = !p.Computed && (props.ReadOnlyOnUpdate || p.Key || parentFields[f.Name])
		}
		if p.Key {
			p.Nullable = false
		}
		if slices.Contains(hidden, f.Name) {
			switch {
			case p.Key:
				b.fail(def.Src.At("/hiddenFields"), "the key field %s cannot be hidden", f.Name)
			case p.Mandatory:
				b.fail(def.Src.At("/hiddenFields"), "the mandatory field %s cannot be hidden", f.Name)
			case parentFields[f.Name]:
				b.fail(def.Src.At("/hiddenFields"), "%s leads to the parent entity and cannot be hidden", f.Name)
			case e != nil && e.ETag.Or("") == f.Name:
				b.fail(def.Src.At("/hiddenFields"), "the ETag field %s cannot be hidden", f.Name)
			}
			continue
		}
		t.Properties = append(t.Properties, p)
		if e != nil && e.ETag.Or("") == f.Name {
			t.ETag = p
		}
	}
	for _, k := range tab.PrimaryKey {
		if p := t.FieldProperty(k); p != nil {
			t.Key = append(t.Key, p)
		}
	}
	return t
}

// navigations adds the navigations of a type: to the dependent entities
// and the parent entity of a business object, and along foreign keys to
// other entity sets. Dependent entities of extensions without an entity
// set of their own become contained navigations.
func (b *builder) navigations(t *EntityType, set *Set) {
	var def *model.EntitySet
	if set != nil {
		def = set.Def
	}
	if e := t.Entity; e != nil {
		for _, child := range t.BusinessObject.Children(e.Table) {
			fk := b.phys.Tables[child.Table].ForeignKey(child.ForeignKey.Or(""))
			if fk == nil {
				continue
			}
			if target := b.bySource[child.Table]; target != nil {
				t.Navigations = append(t.Navigations, &Navigation{Name: target.Name, Target: target, Type: target.Type, Collection: true,
					Partner: b.parentName(t), Reverse: fk.Columns, ForeignKey: fk.Name})
				continue
			}
			if child.Origin == t.BusinessObject.Owner {
				continue
			}
			ct := b.entityType(t.BusinessObject, child, &model.EntitySet{Name: model.PublicName(child.Table), Entity: model.Some(child.Table), Src: t.BusinessObject.Src}, true)
			if ct == nil {
				continue
			}
			ct.Operations = slices.Clone(child.Operations)
			slices.Sort(ct.Operations)
			if def != nil && def.ReadOnly {
				ct.Operations = nil
			}
			for i := range child.Actions {
				a := &child.Actions[i]
				if a.Instance && (def == nil || !def.ReadOnly) {
					ct.Actions = append(ct.Actions, &Action{Name: model.PublicName(a.Name), Action: a, Type: ct})
				}
			}
			b.m.Contained = append(b.m.Contained, ct)
			t.Navigations = append(t.Navigations, &Navigation{Name: model.PublicName(child.Table), Type: ct, Collection: true, Contained: true,
				Reverse: fk.Columns, ForeignKey: fk.Name})
			b.navigations(ct, nil)
		}
	}
	parentFK := ""
	if e := t.Entity; e != nil && e.Parent.Valid() {
		parentFK = e.ForeignKey.Or("")
		if parent := b.bySource[e.Parent.Or("")]; parent != nil && set != nil {
			fk := t.Table.ForeignKey(parentFK)
			t.Navigations = append(t.Navigations, &Navigation{Name: parent.Name, Target: parent, Type: parent.Type, Partner: set.Name,
				Constraints: fk.Columns, ForeignKey: fk.Name})
		}
	}
	named := map[string]string{}
	if def != nil {
		named = def.Navigations
		for fk := range named {
			if t.Table.ForeignKey(fk) == nil {
				b.fail(def.Src.At("/navigations/"+fk), "%s has no foreign key %s", t.Table.Name, fk)
			}
		}
	}
	for i := range t.Table.ForeignKeys {
		fk := &t.Table.ForeignKeys[i]
		target := b.bySource[fk.Target]
		if fk.Name == parentFK || target == nil || t.FieldProperty(fk.Columns[0].Field) == nil {
			if def != nil && named[fk.Name] != "" && target == nil {
				b.fail(def.Src.At("/navigations/"+fk.Name), "%s leads to %s, which the service does not offer", fk.Name, fk.Target)
			}
			continue
		}
		name := named[fk.Name]
		if name == "" {
			name = target.Name
		}
		nullable := false
		for _, c := range fk.Columns {
			if p := t.FieldProperty(c.Field); p != nil && p.Nullable {
				nullable = true
			}
		}
		t.Navigations = append(t.Navigations, &Navigation{Name: name, Target: target, Type: target.Type, Nullable: nullable,
			Constraints: fk.Columns, ForeignKey: fk.Name})
	}
}

// parentName returns the name of the navigation from the entity set of a
// dependent entity back to the set of type t, which is that of the set.
func (b *builder) parentName(t *EntityType) string {
	if set := b.bySource[t.Table.Name]; set != nil {
		return set.Name
	}
	return ""
}

// names checks that the public names are unique where OData needs them:
// properties and navigations in a type, actions bound to a type, entity
// sets and action imports in the container.
func (b *builder) names() {
	at := b.svc.Src
	for _, t := range b.m.Types() {
		seen := map[string]string{}
		add := func(public, technical string) {
			if !model.ValidODataIdentifier(public) {
				b.fail(at, "%s of %s gives the name %s, which is no identifier of OData", technical, t.Table.Name, public)
			}
			if other, ok := seen[public]; ok {
				b.fail(at, "%s and %s of %s both have the public name %s", other, technical, t.Table.Name, public)
			}
			seen[public] = technical
		}
		for _, p := range t.Properties {
			add(p.Name, p.Field.Name)
		}
		for _, n := range t.Navigations {
			add(n.Name, "the navigation "+n.ForeignKey)
		}
		actions := map[string]string{}
		for _, a := range t.Actions {
			if other, ok := actions[a.Name]; ok {
				b.fail(at, "the actions %s and %s of %s both have the public name %s", other, a.Action.Name, t.Table.Name, a.Name)
			}
			actions[a.Name] = a.Action.Name
		}
	}
	types := map[string]string{}
	for _, t := range b.m.Types() {
		if other, ok := types[t.Name]; ok && other != t.Table.Name {
			b.fail(at, "the tables %s and %s both have the public name %s", other, t.Table.Name, t.Name)
		}
		types[t.Name] = t.Table.Name
	}
	container := map[string]string{}
	for _, s := range b.m.Sets {
		container[s.Name] = "the entity set " + s.Name
	}
	for _, a := range b.m.Imports {
		if other, ok := container[a.Name]; ok {
			b.fail(at, "the static action %s and %s both have the public name %s", a.Action.Name, other, a.Name)
		}
		container[a.Name] = "the static action " + a.Action.Name
	}
}

// Problems formats problems for messages.
func Problems(ps []Problem) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Message)
	}
	return strings.Join(out, "; ")
}
