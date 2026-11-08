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

package model

import (
	"cmp"
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/rterr"
)

// ExtensionKind is a kind of extension of a business object.
type ExtensionKind string

// Kinds of extensions of business objects.
const (
	// ExtendFieldProperties: properties of fields of the extending package.
	ExtendFieldProperties ExtensionKind = "fieldProperties"
	ExtendDeterminations  ExtensionKind = "determinations"
	ExtendValidations     ExtensionKind = "validations"
	ExtendActions         ExtensionKind = "actions"
	// ExtendEntities: dependent entities of the extending package below an
	// entity.
	ExtendEntities ExtensionKind = "entities"
)

// ExtensionKinds lists the kinds of extensions in their order.
var ExtensionKinds = []ExtensionKind{ExtendFieldProperties, ExtendDeterminations, ExtendValidations, ExtendActions, ExtendEntities}

// BusinessObjectExtension extends a business object of another package
// (schema/business-object-extensions/*.json): properties of the fields of
// the extending package, determinations, validations and actions on the
// entities, and dependent entities of its own tables, with a behavior
// class of its own.
type BusinessObjectExtension struct {
	BusinessObject string    `json:"businessObject"`
	Package        PackageID `json:"package"`
	File           string    `json:"file"`
	// BehaviorClass is the class with the methods of its determinations,
	// validations and actions; none when it has none.
	BehaviorClass Optional[string] `json:"behaviorClass"`
	// Entities are the extended entities, which have no parent and only
	// field properties and behavior, followed by the own entities in the
	// order of the tree.
	Entities []Entity       `json:"entities"`
	Src      rterr.Location `json:"-"`
}

// BusinessObjectExtensionKey identifies an extension: one package extends
// one business object at most once.
type BusinessObjectExtensionKey struct {
	Package        PackageID
	BusinessObject string
}

// Key returns the identity of the extension.
func (x *BusinessObjectExtension) Key() BusinessObjectExtensionKey {
	return BusinessObjectExtensionKey{Package: x.Package, BusinessObject: x.BusinessObject}
}

// Entity returns the entity of a table in the extension, or nil.
func (x *BusinessObjectExtension) Entity(table string) *Entity {
	for i := range x.Entities {
		if x.Entities[i].Table == table {
			return &x.Entities[i]
		}
	}
	return nil
}

// Owns reports whether the extension adds the entity of a table.
func (x *BusinessObjectExtension) Owns(table string) bool {
	e := x.Entity(table)
	return e != nil && e.Parent.Valid()
}

// HasBehavior reports whether the extension has determinations,
// validations or actions, which need a behavior class.
func (x *BusinessObjectExtension) HasBehavior() bool {
	for _, e := range x.Entities {
		if len(e.Determinations)+len(e.Validations)+len(e.Actions) > 0 {
			return true
		}
	}
	return false
}

// Allows reports whether a business object allows a kind of extension on
// an entity: the entity's list when it has one, else that of the business
// object; without a list everything is allowed.
func (b *BusinessObject) Allows(e *Entity, kind ExtensionKind) bool {
	if e != nil {
		if l, ok := e.Extensible.Get(); ok {
			return slices.Contains(l, kind)
		}
	}
	if l, ok := b.Extensible.Get(); ok {
		return slices.Contains(l, kind)
	}
	return true
}

// BusinessObjectExtensionsOf returns the extensions of a business object in the order
// their behavior runs: by the level of the extending package among the
// extending packages, where a package requiring another one comes after
// it, then by package ID.
func (s *Schema) BusinessObjectExtensionsOf(name string) []*BusinessObjectExtension {
	var out []*BusinessObjectExtension
	byPackage := map[PackageID]*BusinessObjectExtension{}
	for k, x := range s.BusinessObjectExtensions {
		if k.BusinessObject == name {
			out = append(out, x)
			byPackage[k.Package] = x
		}
	}
	levels := map[PackageID]int{}
	var level func(id PackageID, seen map[PackageID]bool) int
	level = func(id PackageID, seen map[PackageID]bool) int {
		if l, ok := levels[id]; ok {
			return l
		}
		if seen[id] {
			return 0
		}
		seen[id] = true
		l := 0
		if info := s.Packages[id]; info != nil {
			for _, d := range info.Manifest.Requires {
				if byPackage[d.ID] != nil {
					l = max(l, level(d.ID, seen)+1)
				}
			}
		}
		levels[id] = l
		return l
	}
	for _, x := range out {
		level(x.Package, map[PackageID]bool{})
	}
	slices.SortFunc(out, func(a, b *BusinessObjectExtension) int {
		return cmp.Or(cmp.Compare(levels[a.Package], levels[b.Package]), cmp.Compare(a.Package, b.Package))
	})
	return out
}

// SortedBusinessObjectExtensions returns all extensions ordered by
// business object and package.
func (s *Schema) SortedBusinessObjectExtensions() []*BusinessObjectExtension {
	out := slices.Collect(maps.Values(s.BusinessObjectExtensions))
	slices.SortFunc(out, func(a, b *BusinessObjectExtension) int {
		return cmp.Or(cmp.Compare(a.BusinessObject, b.BusinessObject), cmp.Compare(a.Package, b.Package))
	})
	return out
}

// MergedBusinessObject returns a business object with its extensions of
// the packages include accepts merged in, or nil when it does not exist.
// Behavior, entities and field properties carry the package they come
// from in Origin; the extensions follow the base in the order of
// BusinessObjectExtensionsOf. Field properties of the base and the extensions combine.
// A nil include merges all extensions.
func (s *Schema) MergedBusinessObject(name string, include func(PackageID) bool) *BusinessObject {
	base := s.BusinessObjects[name]
	if base == nil {
		return nil
	}
	b := cloneBusinessObject(base)
	for i := range b.Entities {
		e := &b.Entities[i]
		e.Origin = b.Owner
		setOrigin(e, b.Owner)
	}
	var added []Entity
	for _, x := range s.BusinessObjectExtensionsOf(name) {
		if include != nil && !include(x.Package) {
			continue
		}
		b.Extensions = append(b.Extensions, ExtensionRef{Package: x.Package, BehaviorClass: x.BehaviorClass})
		for _, xe := range x.Entities {
			xe = cloneEntity(xe)
			setOrigin(&xe, x.Package)
			if xe.Parent.Valid() {
				xe.Origin = x.Package
				added = append(added, xe)
				continue
			}
			e := b.Entity(xe.Table)
			if e == nil {
				for i := range added {
					if added[i].Table == xe.Table {
						e = &added[i]
					}
				}
			}
			if e == nil {
				continue
			}
			mergeFields(e, xe.Fields, x.Package)
			e.Determinations = append(e.Determinations, xe.Determinations...)
			e.Validations = append(e.Validations, xe.Validations...)
			e.Actions = append(e.Actions, xe.Actions...)
		}
	}
	b.Entities = append(b.Entities, added...)
	b.Entities = SortEntities(b.Root, b.Entities)
	return b
}

// ExtensionRef names an extension merged into a business object.
type ExtensionRef struct {
	Package       PackageID        `json:"package"`
	BehaviorClass Optional[string] `json:"behaviorClass"`
}

// setOrigin sets the origin of the behavior of an entity.
func setOrigin(e *Entity, pkg PackageID) {
	for i := range e.Determinations {
		e.Determinations[i].Origin = pkg
	}
	for i := range e.Validations {
		e.Validations[i].Origin = pkg
	}
	for i := range e.Actions {
		e.Actions[i].Origin = pkg
	}
}

// mergeFields combines field properties of an extension with those of an
// entity and records the packages that set them.
func mergeFields(e *Entity, fields map[string]FieldProperties, pkg PackageID) {
	for name, p := range fields {
		q := e.Fields[name]
		e.Fields[name] = FieldProperties{ReadOnly: q.ReadOnly || p.ReadOnly, ReadOnlyOnUpdate: q.ReadOnlyOnUpdate || p.ReadOnlyOnUpdate,
			Mandatory: q.Mandatory || p.Mandatory}
		if e.FieldOrigins == nil {
			e.FieldOrigins = map[string][]PackageID{}
		}
		e.FieldOrigins[name] = append(e.FieldOrigins[name], pkg)
	}
}

// BehaviorClassOf returns the behavior class of a package in a merged
// business object: that of the base or of an extension.
func (b *BusinessObject) BehaviorClassOf(pkg PackageID) Optional[string] {
	if pkg == b.Owner {
		return b.BehaviorClass
	}
	for _, x := range b.Extensions {
		if x.Package == pkg {
			return x.BehaviorClass
		}
	}
	return None[string]()
}

func cloneEntity(e Entity) Entity {
	e.Operations = slices.Clone(e.Operations)
	e.Fields = maps.Clone(e.Fields)
	if e.Fields == nil {
		e.Fields = map[string]FieldProperties{}
	}
	e.FieldOrigins = nil
	e.Determinations = slices.Clone(e.Determinations)
	e.Validations = slices.Clone(e.Validations)
	e.Actions = slices.Clone(e.Actions)
	for i, a := range e.Actions {
		if a.Authorization != nil {
			c := *a.Authorization
			c.Fields = maps.Clone(c.Fields)
			e.Actions[i].Authorization = &c
		}
	}
	if l, ok := e.Extensible.Get(); ok {
		e.Extensible = Some(slices.Clone(l))
	}
	if e.Authorization != nil {
		a := *e.Authorization
		a.Fields, a.Activities = maps.Clone(a.Fields), maps.Clone(a.Activities)
		e.Authorization = &a
	}
	return e
}

func cloneBusinessObjectExtension(x *BusinessObjectExtension) *BusinessObjectExtension {
	c := *x
	c.Entities = make([]Entity, len(x.Entities))
	for i, e := range x.Entities {
		c.Entities[i] = cloneEntity(e)
	}
	return &c
}

// SortChildren orders dependent entities of an extension as trees below
// entities outside the list: those whose parent is not in the list by
// name, each followed by its children by name, depth first. Entities in
// cycles follow by name, so that validation reports them.
func SortChildren(entities []Entity) []Entity {
	byName := map[string]Entity{}
	for _, e := range entities {
		byName[e.Table] = e
	}
	var out []Entity
	done := map[string]bool{}
	var visit func(table string)
	visit = func(table string) {
		if done[table] {
			return
		}
		done[table] = true
		out = append(out, byName[table])
		for _, name := range slices.Sorted(maps.Keys(byName)) {
			if p, _ := byName[name].Parent.Get(); p == table {
				visit(name)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		if p, _ := byName[name].Parent.Get(); byName[p].Table == "" {
			visit(name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		visit(name)
	}
	return out
}
