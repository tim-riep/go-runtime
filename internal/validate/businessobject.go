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
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// businessObjects checks the business objects: their tables, the tree of
// entities and the behavior of every entity. Whether the behavior class
// has the methods is checked by the compiler.
func (v *validator) businessObjects() {
	owners := map[string]string{} // table -> business object
	for _, name := range v.s.BusinessObjectNames() {
		v.businessObject(v.s.BusinessObjects[name], owners)
	}
	v.businessObjectExtensions(owners)
}

func (v *validator) businessObject(b *model.BusinessObject, owners map[string]string) {
	obj := "business object " + b.Name
	loc := b.Src
	fail := func(at rterr.Location, format string, args ...any) {
		v.fail(rterr.CodeBusinessObject, at, obj, format, args...)
	}
	v.objectName(b.Name, b.Owner, loc.At("/name"), obj)
	v.descriptions(obj, loc, b.Description, b.Languages())
	if class, ok := b.BehaviorClass.Get(); ok {
		v.ownSource(class, model.SourceClass, "class", b.Owner, loc.At("/behaviorClass"), obj)
	} else if b.HasBehavior() {
		fail(loc, "it needs a behavior class for its determinations, validations and actions")
	}
	switch b.DataClass {
	case model.Application, model.Configuration, model.RuntimeConfiguration:
	default:
		fail(loc.At("/dataClass"), "the data class is configuration or runtimeConfiguration, not %q", b.DataClass)
	}
	root := v.s.Tables[b.Root]
	if re := b.Entity(b.Root); re != nil && re.Authorization == nil && b.DataClass == model.Application {
		fail(re.Src, "the root %s needs an authorization; only business objects of configuration take /TRBASE/CONFIGURATION", b.Root)
	}
	if b.Entity(b.Root) == nil {
		fail(loc.At("/root"), "the root %s is no entity; list it under entities", b.Root)
	}
	methods := map[string]string{}
	method := func(name, what string, at rterr.Location) {
		if other, ok := methods[name]; ok {
			fail(at, "the method %s is the %s and the %s; every determination, validation and action has a method of its own", name, other, what)
		}
		methods[name] = what
	}
	reached := map[string]bool{}
	var walk func(table string)
	walk = func(table string) {
		if reached[table] {
			return
		}
		reached[table] = true
		for _, c := range b.Children(table) {
			walk(c.Table)
		}
	}
	walk(b.Root)
	for i := range b.Entities {
		e := &b.Entities[i]
		eloc := e.Src
		t := v.s.Tables[e.Table]
		switch {
		case t == nil:
			fail(eloc, "table %s does not exist", e.Table)
			continue
		case t.Owner != b.Owner:
			fail(eloc, "table %s belongs to %s; the entities of a business object are tables of its own package", e.Table, t.Owner)
			continue
		case t.DataClass == model.System:
			fail(eloc, "table %s has the data class system, which business objects cannot have", e.Table)
		case t.DataClass != b.DataClass:
			fail(eloc, "table %s has the data class %s; business object %s has %s, which all its entities have", e.Table, t.DataClass, b.Name, b.DataClass)
		case root != nil && t.TenantSpecific != root.TenantSpecific:
			fail(eloc, "table %s is tenant-specific: %v, but the root %s: %v; all entities have the same tenant flag", e.Table, t.TenantSpecific, b.Root, root.TenantSpecific)
		}
		if other, ok := owners[e.Table]; ok {
			fail(eloc, "table %s already belongs to business object %s; a table belongs to at most one business object", e.Table, other)
		}
		owners[e.Table] = b.Name
		parent, hasParent := e.Parent.Get()
		switch {
		case e.Table == b.Root && hasParent:
			fail(eloc.At("/parent"), "the root has no parent")
		case e.Table != b.Root && !hasParent:
			fail(eloc, "%s is no root and needs a parent and a foreign key", e.Table)
		case hasParent && b.Entity(parent) == nil:
			fail(eloc.At("/parent"), "the parent %s is no entity of %s", parent, b.Name)
		case hasParent && !reached[e.Table]:
			fail(eloc.At("/parent"), "%s cannot be reached from the root; the parents form a cycle", e.Table)
		}
		if fkName, ok := e.ForeignKey.Get(); ok || hasParent {
			fk := t.ForeignKey(fkName)
			switch {
			case !hasParent:
				fail(eloc.At("/foreignKey"), "only dependent entities have a foreign key to their parent")
			case fk == nil:
				fail(eloc.At("/foreignKey"), "table %s has no foreign key %s", e.Table, fkName)
			case fk.Target != parent:
				fail(eloc.At("/foreignKey"), "the foreign key %s references %s, not the parent %s", fkName, fk.Target, parent)
			case !coversKey(fk, v.s.Tables[parent]):
				fail(eloc.At("/foreignKey"), "the foreign key %s does not cover the key of %s", fkName, parent)
			}
		}
		if l, ok := e.Extensible.Get(); ok {
			if all, ok := b.Extensible.Get(); ok {
				for _, k := range l {
					if !slices.Contains(all, k) {
						fail(eloc.At("/extensible"), "%s allows no extensions with %s; an entity narrows what the business object allows", b.Name, k)
					}
				}
			}
		}
		v.entityFields(b, e, t, fail)
		v.entityBehavior(b, e, fail, method)
	}
}

// coversKey reports whether a foreign key references the whole primary
// key of the parent.
func coversKey(fk *model.ForeignKey, parent *model.Table) bool {
	if parent == nil {
		return false
	}
	var refs []string
	for _, c := range fk.Columns {
		refs = append(refs, c.References)
	}
	for _, k := range parent.PrimaryKey {
		if !slices.Contains(refs, k) {
			return false
		}
	}
	return true
}

// entityFields checks the numbering, the properties, the administrative
// fields and the ETag of an entity against the fields of its table.
func (v *validator) entityFields(b *model.BusinessObject, e *model.Entity, t *model.Table, fail func(rterr.Location, string, ...any)) {
	loc := e.Src
	switch e.Numbering {
	case model.NumberingExternal:
	case model.NumberingUUID:
		if len(t.PrimaryKey) != 1 || v.baseType(e.Table, t.PrimaryKey[0]) != model.UUID {
			fail(loc.At("/numbering"), "numbering uuid needs a key of one field of type UUID; %s has the key %s", e.Table, strings.Join(t.PrimaryKey, ", "))
		}
	default:
		fail(loc.At("/numbering"), "numbering is external or uuid, not %q", e.Numbering)
	}
	for _, name := range []string{model.LinkField, model.ParentLinkField} {
		if v.physicalField(e.Table, name) != nil {
			fail(loc, "table %s has a field %s, which the rows that create instances use to link them", e.Table, name)
		}
	}
	field := func(name, at, what string) bool {
		if name == model.TenantField || v.physicalField(e.Table, name) == nil {
			fail(loc.At(at), "%s %s is no field of %s", what, name, e.Table)
			return false
		}
		return true
	}
	for _, name := range slices.Sorted(maps.Keys(e.Fields)) {
		field(name, "/fields/"+name, "field")
	}
	a := e.Administrative
	for _, f := range []struct {
		name  model.Optional[string]
		at    string
		types []model.BaseType
	}{
		{a.CreatedBy, "/administrative/createdBy", []model.BaseType{model.Char, model.String}},
		{a.CreatedAt, "/administrative/createdAt", []model.BaseType{model.Timestamp}},
		{a.ChangedBy, "/administrative/changedBy", []model.BaseType{model.Char, model.String}},
		{a.ChangedAt, "/administrative/changedAt", []model.BaseType{model.Timestamp}},
	} {
		name, ok := f.name.Get()
		if !ok || !field(name, f.at, "the administrative field") {
			continue
		}
		if typ := v.baseType(e.Table, name); !slices.Contains(f.types, typ) {
			fail(loc.At(f.at), "the administrative field %s is %s; it needs %s", name, typ, typeList(f.types))
		}
	}
	if name, ok := e.ETag.Get(); ok && field(name, "/etag", "the ETag") {
		types := []model.BaseType{model.Timestamp, model.UUID, model.Int8}
		if typ := v.baseType(e.Table, name); !slices.Contains(types, typ) {
			fail(loc.At("/etag"), "the ETag %s is %s; it needs %s", name, typ, typeList(types))
		}
	}
}

func typeList(types []model.BaseType) string {
	var out []string
	for _, t := range types {
		out = append(out, string(t))
	}
	return strings.Join(out, " or ")
}

// baseType returns the base type of a column, or "".
func (v *validator) baseType(table, field string) model.BaseType {
	f := v.physicalField(table, field)
	if f == nil {
		return ""
	}
	if d := v.physical().Domain(f); d != nil {
		return d.Type
	}
	return ""
}

// entityBehavior checks determinations, validations, actions and the
// authorization of an entity.
func (v *validator) entityBehavior(b *model.BusinessObject, e *model.Entity, fail func(rterr.Location, string, ...any), method func(name, what string, at rterr.Location)) {
	loc := e.Src
	trigger := func(t model.Trigger, at rterr.Location) {
		for _, op := range t.On {
			if !slices.Contains(model.EntityOperations, op) {
				fail(at.At("/on"), "%q is no operation; they are create, update and delete", op)
			}
		}
		for _, f := range t.Fields {
			if f == model.TenantField || v.physicalField(e.Table, f) == nil {
				fail(at.At("/fields"), "%s is no field of %s", f, e.Table)
			}
		}
		if len(t.Fields) > 0 && !slices.Contains(t.On, model.OperationUpdate) {
			fail(at.At("/fields"), "fields trigger on update, which is not among the operations")
		}
	}
	for i, d := range e.Determinations {
		at := loc.At("/determinations/" + strconv.Itoa(i))
		method(d.Name, "determination of "+e.Table, at)
		if d.When != model.WhenModify && d.When != model.WhenSave {
			fail(at.At("/when"), "a determination runs on modify or save, not %q", d.When)
		}
		trigger(d.Trigger, at)
	}
	for i, val := range e.Validations {
		at := loc.At("/validations/" + strconv.Itoa(i))
		method(val.Name, "validation of "+e.Table, at)
		trigger(val.Trigger, at)
	}
	for i, a := range e.Actions {
		at := loc.At("/actions/" + strconv.Itoa(i))
		method(a.Name, "action of "+e.Table, at)
		if p, ok := a.Parameter.Get(); ok {
			v.structureRef(p, b.Owner, at.At("/parameter"), fail)
		}
		if r, ok := a.Result.Get(); ok {
			switch {
			case r == model.ResultSelf && !a.Instance:
				fail(at.At("/result"), "a static action has no instances to return as self")
			case r != model.ResultSelf:
				v.structureRef(r, b.Owner, at.At("/result"), fail)
			}
		}
	}
	au := e.Authorization
	if au == nil {
		return
	}
	at := loc.At("/authorization")
	o := v.s.AuthorizationObjects[au.Object]
	switch {
	case o == nil:
		fail(at.At("/object"), "authorization object %s does not exist", au.Object)
		return
	case !v.s.Visible(b.Owner, o.Owner):
		fail(at.At("/object"), "authorization object %s belongs to %s, which %s does not require", au.Object, o.Owner, b.Owner)
	}
	for _, f := range o.Fields {
		if !f.IsActivity() && au.Fields[f.Name] == "" {
			fail(at.At("/fields"), "the field %s of %s needs a field of %s", f.Name, o.Name, e.Table)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(au.Fields)) {
		f, ok := o.Field(name)
		switch {
		case !ok || f.IsActivity():
			fail(at.At("/fields/"+name), "%s is no field of %s that an entity field gives", name, o.Name)
		case au.Fields[name] == model.TenantField || v.physicalField(e.Table, au.Fields[name]) == nil:
			fail(at.At("/fields/"+name), "%s is no field of %s", au.Fields[name], e.Table)
		}
	}
	needed := []string{model.ReadActivityKey}
	for _, op := range e.Operations {
		needed = append(needed, string(op))
	}
	for _, a := range e.Actions {
		needed = append(needed, a.Name)
	}
	if !o.HasActivity() {
		if len(au.Activities) > 0 {
			fail(at.At("/activities"), "%s has no field ACTIVITY; leave out the activities", o.Name)
		}
		return
	}
	for _, key := range needed {
		if _, ok := au.Activities[key]; !ok {
			fail(at.At("/activities"), "the activity for %s is missing", key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(au.Activities)) {
		act := au.Activities[key]
		switch {
		case !slices.Contains(needed, key):
			fail(at.At("/activities/"+key), "%s is neither read, an operation nor an action of %s", key, e.Table)
		case !slices.Contains(o.Activities, act):
			fail(at.At("/activities/"+key), "%s does not allow the activity %s; it allows %s", o.Name, act, strings.Join(o.Activities, ", "))
		}
	}
}

// structureRef checks the structure of a parameter or result: a visible
// structure or table.
func (v *validator) structureRef(name string, owner model.PackageID, at rterr.Location, fail func(rterr.Location, string, ...any)) {
	_, isStructure := v.s.Structures[name]
	_, isTable := v.s.Tables[name]
	typeOwner, ok := v.s.TypeOwner(name)
	switch {
	case !isStructure && !isTable:
		fail(at, "structure %s does not exist", name)
	case !ok || !v.s.Visible(owner, typeOwner):
		fail(at, "structure %s belongs to %s, which %s does not require", name, typeOwner, owner)
	}
}
