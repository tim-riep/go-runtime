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
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/internal/objects"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// objectTypes checks the package-defined object types and resolves their
// content structures for the objects.
func (v *validator) objectTypes() {
	v.types = map[string]*objects.Type{}
	for _, name := range v.s.ObjectTypeNames() {
		t := v.s.ObjectTypes[name]
		obj := "object type " + name
		v.objectName(name, t.Owner, t.Src.At("/name"), obj)
		v.descriptions(obj, t.Src, t.Description, t.Languages())
		if b, ok := t.Behavior.Get(); ok {
			if pkg, _, split := model.SplitName(b); !split || pkg != t.Owner {
				v.fail(rterr.CodeObjectType, t.Src.At("/behavior"), obj, "the behavior class %s must belong to %s", b, t.Owner)
			}
		}
		rt, problems := objects.Resolve(v.s, t, nil)
		for _, p := range problems {
			at := t.Src.At("/content")
			msg := p.Message
			if p.Pointer != "" {
				msg = fmt.Sprintf("%s (component %s of %s)", p.Message, strings.TrimPrefix(p.Pointer, "/"), t.Content)
			}
			v.fail(rterr.CodeObjectType, at, obj, "%s", msg)
		}
		if len(problems) == 0 {
			v.types[name] = rt
		}
	}
}

// objects checks the objects of all packages against the target state:
// their type, content, texts and references. So a change of a type, of its
// content structure or of an object a reference names that breaks an
// installed object fails with the name of the object and its package. The
// objects get their canonical content, hash and references.
func (v *validator) objects() {
	for _, name := range v.s.ObjectNames() {
		o := v.s.Objects[name]
		obj := fmt.Sprintf("object %s of package %s", name, o.Owner)
		v.objectName(name, o.Owner, o.Src.At("/name"), obj)
		t := v.s.ObjectTypes[o.Type]
		if t == nil {
			v.fail(rterr.CodeObject, o.Src, obj, "object type %s does not exist", o.Type)
			continue
		}
		if t.Owner != o.Owner && !v.requiresDirectly(o.Owner, t.Owner) {
			v.fail(rterr.CodeNotVisible, o.Src, obj, "object type %s belongs to %s, which %s does not require", o.Type, t.Owner, o.Owner)
			continue
		}
		rt := v.types[o.Type]
		if rt == nil {
			v.fail(rterr.CodeObject, o.Src, obj, "object type %s is invalid", o.Type)
			continue
		}
		checked, problems := rt.Check(o.Content)
		for _, p := range problems {
			v.fail(rterr.CodeObject, o.Src.At("/content"+p.Pointer), obj, "%s", p.Message)
		}
		if checked == nil {
			continue
		}
		o.Content, o.Hash, o.References = checked.Content, checked.Hash, checked.References
		v.knownLanguages(obj, checked.Languages)
		for _, r := range o.References {
			if msg := v.reference(o.Owner, r); msg != "" {
				v.fail(rterr.CodeObject, o.Src.At("/content"+r.Pointer), obj, "%s", msg)
			}
		}
	}
}

func (v *validator) requiresDirectly(id, other model.PackageID) bool {
	info := v.s.Packages[id]
	return info != nil && slices.ContainsFunc(info.Manifest.Requires, func(d model.Dependency) bool { return d.ID == other })
}

// reference checks that a reference names an existing object that the
// package of the referring object may use; it returns the problem or "".
func (v *validator) reference(owner model.PackageID, r model.ObjectReference) string {
	visible := func(what, name string, of model.PackageID) string {
		if !v.s.Visible(owner, of) {
			return fmt.Sprintf("%s %s belongs to %s, which %s does not require", what, name, of, owner)
		}
		return ""
	}
	switch r.Kind {
	case model.RefPackage:
		id := model.PackageID(r.Target)
		if _, ok := v.s.Packages[id]; !ok {
			return fmt.Sprintf("package %s is not installed", r.Target)
		}
		return visible("package", r.Target, id)
	case model.RefBusinessObject:
		b := v.s.BusinessObjects[r.Target]
		if b == nil {
			return fmt.Sprintf("business object %s does not exist", r.Target)
		}
		return visible("business object", r.Target, b.Owner)
	case model.RefEntity:
		return v.entityRef(owner, r.Target, visible)
	case model.RefField:
		if msg := v.entityRef(owner, r.Target, visible); msg != "" {
			return msg
		}
		f := v.physicalField(r.Target, r.Part)
		if f == nil {
			return fmt.Sprintf("%s is no field of %s", r.Part, r.Target)
		}
		return visible("field", r.Part, f.Owner)
	case model.RefAction:
		b := v.s.MergedBusinessObject(r.Target, nil)
		if b == nil {
			return fmt.Sprintf("business object %s does not exist", r.Target)
		}
		for _, e := range b.Entities {
			for _, a := range e.Actions {
				if a.Name == r.Part {
					if pkg, _, ok := model.SplitName(a.Name); ok {
						return visible("action", a.Name, pkg)
					}
					return visible("business object", b.Name, b.Owner)
				}
			}
		}
		return fmt.Sprintf("%s is no action of %s", r.Part, r.Target)
	case model.RefService:
		svc := v.s.Services[r.Target]
		if svc == nil {
			return fmt.Sprintf("service %s does not exist", r.Target)
		}
		return visible("service", r.Target, svc.Owner)
	case model.RefEntitySet:
		svc := v.s.Services[r.Target]
		if svc == nil {
			return fmt.Sprintf("service %s does not exist", r.Target)
		}
		if svc.EntitySet(r.Part) == nil {
			return fmt.Sprintf("%s is no entity set of %s", r.Part, r.Target)
		}
		return visible("service", r.Target, svc.Owner)
	case model.RefRole:
		role := v.s.Roles[r.Target]
		if role == nil || role.Owner == model.LocalPackageID {
			return fmt.Sprintf("package role %s does not exist", r.Target)
		}
		return visible("role", r.Target, role.Owner)
	case model.RefHTTPNode:
		if h := v.s.HTTPServices[r.Target]; h != nil {
			return visible("node", r.Target, h.Owner)
		}
		for id := range v.s.Packages {
			if model.HTTPPackagePath(id) == r.Target {
				return visible("node", r.Target, id)
			}
		}
		return fmt.Sprintf("node %s does not exist", r.Target)
	case model.RefAssetRepository:
		id := model.PackageID(r.Target)
		if v.s.AssetRepositories[model.AssetRepositoryKey{Package: id, ID: r.Part}] == nil {
			return fmt.Sprintf("package %s has no asset repository %s", r.Target, r.Part)
		}
		return visible("asset repository", r.Part, id)
	case model.RefObject:
		target := v.s.Objects[r.Target]
		if target == nil {
			return fmt.Sprintf("object %s does not exist", r.Target)
		}
		if r.ObjectType != "" && target.Type != r.ObjectType {
			return fmt.Sprintf("object %s has the type %s, not %s", r.Target, target.Type, r.ObjectType)
		}
		return visible("object", r.Target, target.Owner)
	}
	return ""
}

func (v *validator) entityRef(owner model.PackageID, table string, visible func(what, name string, of model.PackageID) string) string {
	b, e := v.s.BusinessObjectOf(table)
	if b == nil || e == nil {
		return fmt.Sprintf("%s is no entity of a business object", table)
	}
	if t := v.s.Tables[table]; t != nil {
		if msg := visible("entity", table, t.Owner); msg != "" {
			return msg
		}
	}
	return visible("business object", b.Name, b.Owner)
}
