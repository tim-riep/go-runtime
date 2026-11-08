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

// businessObjectExtensions checks the extensions of business objects
// against the business objects as they are: an extension that a change of
// its business object breaks fails here with its package and file.
func (v *validator) businessObjectExtensions(owners map[string]string) {
	for _, x := range v.s.SortedBusinessObjectExtensions() {
		v.businessObjectExtension(x, owners)
	}
}

func (v *validator) businessObjectExtension(x *model.BusinessObjectExtension, owners map[string]string) {
	obj := "business object extension " + x.BusinessObject + " by " + string(x.Package)
	loc := x.Src
	fail := func(at rterr.Location, format string, args ...any) {
		v.fail(rterr.CodeBusinessObject, at, obj, format, args...)
	}
	b := v.s.BusinessObjects[x.BusinessObject]
	switch {
	case b == nil:
		fail(loc.At("/businessObject"), "the business object %s does not exist", x.BusinessObject)
		return
	case b.Owner == x.Package:
		fail(loc.At("/businessObject"), "a package does not extend its own business object; change its definition")
		return
	case !v.s.Requires(x.Package)[b.Owner]:
		fail(loc.At("/businessObject"), "%s belongs to %s, which %s does not require", b.Name, b.Owner, x.Package)
		return
	}
	if class, ok := x.BehaviorClass.Get(); ok {
		v.ownSource(class, model.SourceClass, "class", x.Package, loc.At("/behaviorClass"), obj)
	} else if x.HasBehavior() {
		fail(loc, "it needs a behavior class for its determinations, validations and actions")
	}
	own := model.Namespace(x.Package)
	// The business object as the extension sees it: with the extensions
	// of the packages it requires, without the others and its own.
	merged := v.s.MergedBusinessObject(b.Name, func(p model.PackageID) bool { return p != x.Package && v.s.Requires(x.Package)[p] })
	names := map[string]bool{}
	for _, e := range merged.Entities {
		for _, d := range e.Determinations {
			names[d.Name] = true
		}
		for _, val := range e.Validations {
			names[val.Name] = true
		}
		for _, a := range e.Actions {
			names[a.Name] = true
		}
	}
	method := func(name, what string, at rterr.Location) {
		if names[name] {
			fail(at, "%s is the name of another determination, validation or action of %s", name, b.Name)
		}
		names[name] = true
		if !strings.HasPrefix(name, own) {
			fail(at, "the %s %s needs the namespace %s of the extending package", what, name, own)
		}
	}
	// The extension as a business object of the extending package, for
	// the checks the entities share with business objects.
	ext := &model.BusinessObject{Name: b.Name, Owner: x.Package, DataClass: b.DataClass, Root: b.Root, BehaviorClass: x.BehaviorClass}
	for i := range x.Entities {
		e := &x.Entities[i]
		if e.Parent.Valid() {
			v.ownEntity(x, b, merged, ext, e, owners, fail, method)
			continue
		}
		base := merged.Entity(e.Table)
		if base == nil {
			if owner, ok := owners[e.Table]; ok && owner == b.Name {
				fail(e.Src, "%s is an entity of an extension of %s by a package that %s does not require", e.Table, b.Name, x.Package)
			} else {
				fail(e.Src, "%s is no entity of %s; an entity of the extending package needs a parent", e.Table, b.Name)
			}
			continue
		}
		v.extendedEntity(x, merged, base, e, fail, method)
	}
}

// extendedEntity checks an entity of the business object or of another
// extension that the extension adds field properties and behavior to.
func (v *validator) extendedEntity(x *model.BusinessObjectExtension, b *model.BusinessObject, base, e *model.Entity, fail func(rterr.Location, string, ...any),
	method func(name, what string, at rterr.Location)) {
	loc := e.Src
	switch {
	case len(e.Operations) > 0:
		fail(loc.At("/operations"), "an extension cannot change the operations of %s", e.Table)
	case e.Numbering != "":
		fail(loc.At("/numbering"), "an extension cannot change the numbering of %s", e.Table)
	case len(e.Administrative.Fields()) > 0:
		fail(loc.At("/administrative"), "an extension cannot change the administrative fields of %s", e.Table)
	case e.ETag.Valid():
		fail(loc.At("/etag"), "an extension cannot change the ETag of %s", e.Table)
	case e.Authorization != nil:
		fail(loc.At("/authorization"), "an extension cannot change the authorization of %s; give its actions an authorization of their own", e.Table)
	case e.ForeignKey.Valid():
		fail(loc.At("/foreignKey"), "only dependent entities of the extending package have a foreign key")
	case e.Extensible.Valid():
		fail(loc.At("/extensible"), "an extension cannot change what %s allows to extend", e.Table)
	}
	own := model.Namespace(x.Package)
	allow := func(kind model.ExtensionKind, n int, at string) {
		if n > 0 && !b.Allows(base, kind) {
			fail(loc.At(at), "%s does not allow extensions with %s on %s", b.Name, kind, e.Table)
		}
	}
	allow(model.ExtendFieldProperties, len(e.Fields), "/fields")
	allow(model.ExtendDeterminations, len(e.Determinations), "/determinations")
	allow(model.ExtendValidations, len(e.Validations), "/validations")
	allow(model.ExtendActions, len(e.Actions), "/actions")
	for _, name := range slices.Sorted(maps.Keys(e.Fields)) {
		at := loc.At("/fields/" + name)
		switch f := v.physicalField(e.Table, name); {
		case f == nil || name == model.TenantField:
			fail(at, "%s is no field of %s", name, e.Table)
		case !strings.HasPrefix(name, own):
			owner := b.Owner
			if pkg, _, ok := model.SplitName(name); ok {
				owner = pkg
			}
			fail(at, "the field %s belongs to %s; an extension sets the properties of fields of its own package only", name, owner)
		}
	}
	behavior := &model.Entity{Table: e.Table, Operations: model.EntityOperations, Determinations: e.Determinations, Validations: e.Validations,
		Actions: e.Actions, Src: loc}
	ext := &model.BusinessObject{Name: b.Name, Owner: x.Package}
	v.entityBehavior(ext, behavior, fail, method)
	v.visibleTriggerFields(x, e, fail)
	for i, a := range e.Actions {
		v.extensionAction(x, b, base, a, loc.At("/actions/"+strconv.Itoa(i)), fail)
	}
}

// visibleTriggerFields rejects trigger fields of extensions of packages
// that the extending package does not require.
func (v *validator) visibleTriggerFields(x *model.BusinessObjectExtension, e *model.Entity, fail func(rterr.Location, string, ...any)) {
	check := func(t model.Trigger, at rterr.Location) {
		for _, f := range t.Fields {
			if pkg, _, ok := model.SplitName(f); ok && !v.s.Visible(x.Package, pkg) {
				fail(at.At("/fields"), "the field %s belongs to %s, which %s does not require", f, pkg, x.Package)
			}
		}
	}
	for i, d := range e.Determinations {
		check(d.Trigger, e.Src.At("/determinations/"+strconv.Itoa(i)))
	}
	for i, val := range e.Validations {
		check(val.Trigger, e.Src.At("/validations/"+strconv.Itoa(i)))
	}
}

// extensionAction checks the authorization of an action of an extension
// on an entity it does not own: an authorization object of its own
// package with fields of the entity, or the authorization of the entity;
// with an activity of that object when it has the field ACTIVITY.
func (v *validator) extensionAction(x *model.BusinessObjectExtension, b *model.BusinessObject, base *model.Entity, a model.Action, at rterr.Location,
	fail func(rterr.Location, string, ...any)) {
	var o *model.AuthorizationObject
	if au := a.Authorization; au != nil {
		o = v.s.AuthorizationObjects[au.Object]
		switch {
		case o == nil:
			fail(at.At("/authorization/object"), "authorization object %s does not exist", au.Object)
			return
		case !v.s.Visible(x.Package, o.Owner):
			fail(at.At("/authorization/object"), "authorization object %s belongs to %s, which %s does not require", au.Object, o.Owner, x.Package)
		}
		for _, f := range o.Fields {
			if !f.IsActivity() && au.Fields[f.Name] == "" {
				fail(at.At("/authorization/fields"), "the field %s of %s needs a field of %s", f.Name, o.Name, base.Table)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(au.Fields)) {
			f, ok := o.Field(name)
			switch {
			case !ok || f.IsActivity():
				fail(at.At("/authorization/fields/"+name), "%s is no field of %s that an entity field gives", name, o.Name)
			case au.Fields[name] == model.TenantField || v.physicalField(base.Table, au.Fields[name]) == nil:
				fail(at.At("/authorization/fields/"+name), "%s is no field of %s", au.Fields[name], base.Table)
			}
		}
	} else if ra, ok := b.AuthorizationOf(base); ok {
		o = v.s.AuthorizationObjects[ra.Authorization.Object]
	}
	if o == nil {
		return
	}
	act, ok := a.Activity.Get()
	switch {
	case !o.HasActivity() && ok:
		fail(at.At("/activity"), "%s has no field ACTIVITY; leave out the activity", o.Name)
	case o.HasActivity() && !ok:
		fail(at, "the action needs an activity of %s", o.Name)
	case ok && !slices.Contains(o.Activities, act):
		fail(at.At("/activity"), "%s does not allow the activity %s; it allows %s", o.Name, act, strings.Join(o.Activities, ", "))
	}
}

// ownEntity checks a dependent entity of the extending package: a table
// of its own below an entity of the business object, with the rules of
// entities of business objects.
func (v *validator) ownEntity(x *model.BusinessObjectExtension, b, merged, ext *model.BusinessObject, e *model.Entity, owners map[string]string,
	fail func(rterr.Location, string, ...any), method func(name, what string, at rterr.Location)) {
	loc := e.Src
	t := v.s.Tables[e.Table]
	root := v.s.Tables[b.Root]
	parent := e.Parent.Or("")
	switch {
	case t == nil:
		fail(loc, "table %s does not exist", e.Table)
		return
	case t.Owner != x.Package:
		fail(loc, "table %s belongs to %s; an extension adds dependent entities of tables of its own package", e.Table, t.Owner)
		return
	case t.DataClass != b.DataClass:
		fail(loc, "table %s has the data class %s; business object %s has %s, which all its entities have", e.Table, t.DataClass, b.Name, b.DataClass)
	case root != nil && t.TenantSpecific != root.TenantSpecific:
		fail(loc, "table %s is tenant-specific: %v, but the root %s: %v; all entities have the same tenant flag", e.Table, t.TenantSpecific, b.Root, root.TenantSpecific)
	}
	if other, ok := owners[e.Table]; ok {
		fail(loc, "table %s already belongs to business object %s; a table belongs to at most one business object", e.Table, other)
	}
	owners[e.Table] = b.Name
	pe := merged.Entity(parent)
	if pe == nil {
		pe = x.Entity(parent)
		if pe != nil && !pe.Parent.Valid() {
			pe = nil
		}
	}
	switch {
	case pe == nil:
		fail(loc.At("/parent"), "the parent %s is no entity of %s that %s sees", parent, b.Name, x.Package)
	case !x.Owns(parent) && !merged.Allows(merged.Entity(parent), model.ExtendEntities):
		fail(loc.At("/parent"), "%s does not allow extensions with entities on %s", b.Name, parent)
	}
	for at := parent; ; {
		p := x.Entity(at)
		if p == nil || !p.Parent.Valid() {
			break
		}
		if at = p.Parent.Or(""); at == e.Table {
			fail(loc.At("/parent"), "%s cannot be reached from the root; the parents form a cycle", e.Table)
			break
		}
	}
	fkName := e.ForeignKey.Or("")
	switch fk := t.ForeignKey(fkName); {
	case fk == nil:
		fail(loc.At("/foreignKey"), "table %s has no foreign key %s", e.Table, fkName)
	case fk.Target != parent:
		fail(loc.At("/foreignKey"), "the foreign key %s references %s, not the parent %s", fkName, fk.Target, parent)
	case !coversKey(fk, v.s.Tables[parent]):
		fail(loc.At("/foreignKey"), "the foreign key %s does not cover the key of %s", fkName, parent)
	}
	for _, a := range e.Actions {
		if a.Activity.Valid() || a.Authorization != nil {
			fail(loc.At("/actions"), "the action %s of an entity of the extension takes its activity from the authorization of the entity", a.Name)
		}
	}
	v.entityFields(ext, e, t, fail)
	v.entityBehavior(ext, e, fail, method)
}
