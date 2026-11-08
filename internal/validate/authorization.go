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

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// authorizationTypes are the base types of fields of authorization
// objects: simple types whose values grants name as single values, ranges
// and patterns.
var authorizationTypes = []model.BaseType{model.Char, model.Int4, model.Int8, model.Bool, model.UUID}

func (v *validator) authorizationObjects() {
	for _, name := range v.s.AuthorizationObjectNames() {
		v.authorizationObject(v.s.AuthorizationObjects[name])
	}
}

func (v *validator) authorizationObject(o *model.AuthorizationObject) {
	obj := "authorization object " + o.Name
	loc := o.Src
	v.objectName(o.Name, o.Owner, loc.At("/name"), obj)
	v.labels(obj, loc, o.Texts, o.Languages())
	switch n := len(o.Fields); {
	case n == 0:
		v.fail(rterr.CodeAuthorization, loc.At("/fields"), obj, "an authorization object needs at least one field")
	case n > model.MaxAuthorizationFields:
		v.fail(rterr.CodeAuthorization, loc.At("/fields"), obj, "an authorization object has at most %d fields, not %d", model.MaxAuthorizationFields, n)
	}
	seen := map[string]bool{}
	for i, f := range o.Fields {
		floc := loc.At(fmt.Sprintf("/fields/%d", i))
		fobj := obj + ", field " + f.Name
		switch {
		case !model.ValidLocalName(f.Name) || len(f.Name) > model.MaxAuthorizationFieldName:
			v.fail(rterr.CodeName, floc.At("/name"), fobj, "field names consist of up to %d upper-case letters, digits and underscores and start with a letter", model.MaxAuthorizationFieldName)
		case seen[f.Name]:
			v.fail(rterr.CodeDuplicate, floc.At("/name"), fobj, "the field %s is declared twice", f.Name)
		}
		seen[f.Name] = true
		v.authorizationField(f, o.Owner, floc, fobj)
	}
	has := o.HasActivity()
	switch {
	case has && len(o.Activities) == 0:
		v.fail(rterr.CodeAuthorization, loc.At("/activities"), obj, "an authorization object with the field %s lists the activities that can be granted", model.ActivityField)
	case !has && len(o.Activities) > 0:
		v.fail(rterr.CodeAuthorization, loc.At("/activities"), obj, "activities need the field %s", model.ActivityField)
	}
	codes := v.activityCodes()
	for i, a := range o.Activities {
		aloc := loc.At(fmt.Sprintf("/activities/%d", i))
		switch {
		case !slices.Contains(codes, a):
			v.fail(rterr.CodeAuthorization, aloc, obj, "%s is no activity; the activities are %v", a, codes)
		case slices.Index(o.Activities, a) != i:
			v.fail(rterr.CodeDuplicate, aloc, obj, "the activity %s is listed twice", a)
		}
	}
}

// authorizationField checks the type of a field: a visible data element or
// domain of a simple type, none for ACTIVITY.
func (v *validator) authorizationField(f model.AuthorizationField, owner model.PackageID, loc rterr.Location, obj string) {
	de, hasDE := f.DataElement.Get()
	dom, hasDom := f.Domain.Get()
	switch {
	case f.IsActivity():
		if hasDE || hasDom {
			v.fail(rterr.CodeReservedObject, loc, obj, "the kernel gives the field %s its type; it has neither dataElement nor domain", model.ActivityField)
		}
		return
	case hasDE && hasDom:
		v.fail(rterr.CodeDataElement, loc, obj, "a field references either a data element or a domain, not both")
		return
	case !hasDE && !hasDom:
		v.fail(rterr.CodeDataElement, loc, obj, "a field needs a data element or a domain; only the field %s has none", model.ActivityField)
		return
	}
	if d := v.valueField(de, dom, hasDE, owner, loc, obj); d != nil && !slices.Contains(authorizationTypes, d.Type) {
		v.fail(rterr.CodeAuthorization, loc, obj, "type %s is not allowed; fields of authorization objects are CHAR(n), INT4, INT8, BOOL or UUID", d.TypeString())
	}
}

// valueField checks the data element or domain of a field whose values
// are given as single values, ranges and patterns, and returns its domain
// or nil.
func (v *validator) valueField(de, dom string, hasDE bool, owner model.PackageID, loc rterr.Location, obj string) *model.Domain {
	if hasDE {
		v.typeRef(model.TypeRef{Kind: model.TypeDataElement, Name: de}, owner, loc, obj)
		if d := v.s.DataElements[de]; d != nil {
			return v.s.Domains[d.Domain]
		}
		return nil
	}
	v.typeRef(model.TypeRef{Kind: model.TypeDomain, Name: dom}, owner, loc, obj)
	return v.s.Domains[dom]
}

// activityCodes returns the codes of the activities, the fixed values of
// the domain of the field ACTIVITY.
func (v *validator) activityCodes() []string {
	var out []string
	if d := v.s.Domains[model.ActivityDomain]; d != nil {
		for _, fv := range d.Values {
			out = append(out, fv.Value)
		}
	}
	return out
}
