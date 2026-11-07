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
	"github.com/tim-riep/go-runtime/internal/httpsvc"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// structures checks structures, array types, structure extensions and the
// includes of tables. Cycles are reported once with their full chain;
// objects on a cycle are not resolved further.
func (v *validator) structures() {
	r := v.s.Resolver()
	onCycle := map[string]bool{}
	for _, c := range v.s.Cycles() {
		first := strings.SplitN(c, " -> ", 2)[0]
		v.fail(rterr.CodeStructure, v.srcOf(first), v.typeObject(first), "contains itself: %s", c)
		for _, part := range strings.Split(c, " -> ") {
			fields := strings.Fields(part)
			onCycle[fields[len(fields)-1]] = true
		}
	}
	for _, name := range v.s.StructureNames() {
		v.structure(r, v.s.Structures[name], onCycle[name])
	}
	for _, e := range v.s.SortedStructureExtensions() {
		v.structureExtension(e)
	}
	for _, name := range v.s.ArrayTypeNames() {
		v.arrayType(r, v.s.ArrayTypes[name], onCycle)
	}
	for _, name := range v.s.TableNames() {
		v.tableIncludes(r, v.s.Tables[name], onCycle[name])
	}
}

func (v *validator) typeObject(name string) string {
	switch {
	case v.s.Structures[name] != nil:
		return "structure " + name
	case v.s.Tables[name] != nil:
		return "table " + name
	case v.s.ArrayTypes[name] != nil:
		return "array type " + name
	}
	return name
}

func (v *validator) srcOf(name string) rterr.Location {
	switch {
	case v.s.Structures[name] != nil:
		return v.s.Structures[name].Src
	case v.s.Tables[name] != nil:
		return v.s.Tables[name].Src
	case v.s.ArrayTypes[name] != nil:
		return v.s.ArrayTypes[name].Src
	}
	return rterr.Location{}
}

// labels checks the labels of a structure or array type.
func (v *validator) labels(obj string, loc rterr.Location, texts map[string]model.Label, langs []string) {
	for _, lang := range langs {
		if n := utf8.RuneCountInString(texts[lang].Label); n == 0 || n > model.MaxSettingLabel {
			v.fail(rterr.CodeText, loc.At("/texts/"+lang+"/label"), obj, "label in %s must have 1 to %d characters", lang, model.MaxSettingLabel)
		}
	}
	v.languages(obj, loc, langs)
}

// typeRef checks that a referenced type exists, has the expected kind and
// is visible to owner.
func (v *validator) typeRef(t model.TypeRef, owner model.PackageID, loc rterr.Location, obj string) {
	var found bool
	var typeOwner model.PackageID
	switch t.Kind {
	case model.TypeDataElement:
		if d, ok := v.s.DataElements[t.Name]; ok {
			found, typeOwner = true, d.Owner
		}
	case model.TypeDomain:
		if d, ok := v.s.Domains[t.Name]; ok {
			found, typeOwner = true, d.Owner
		}
	case model.TypeStructure:
		if v.s.ArrayTypes[t.Name] == nil {
			typeOwner, found = v.s.TypeOwner(t.Name)
		}
	case model.TypeArray:
		if a, ok := v.s.ArrayTypes[t.Name]; ok {
			found, typeOwner = true, a.Owner
		}
	default:
		v.fail(rterr.CodeStructure, loc, obj, "a component needs exactly one of dataElement, domain, structure and arrayType")
		return
	}
	switch {
	case !found:
		v.fail(rterr.CodeStructure, loc.At("/"+string(t.Kind)), obj, "%s %s does not exist", t.Kind, t.Name)
	case !v.s.Visible(owner, typeOwner):
		v.fail(rterr.CodeNotVisible, loc.At("/"+string(t.Kind)), obj, "%s belongs to package %s, which %s does not require", t.Name, typeOwner, owner)
	}
}

// component checks a component declared by owner and normalizes its
// default.
func (v *validator) component(parent string, c *model.Component, owner model.PackageID) {
	obj := "component " + parent + "." + c.Name
	switch {
	case c.Name == model.TenantField:
		v.fail(rterr.CodeName, c.Src.At("/name"), obj, "the name %s is reserved for the tenant field", model.TenantField)
	case c.Origin == model.ComponentExtension:
		v.objectName(c.Name, owner, c.Src.At("/name"), obj)
	case !model.ValidLocalName(c.Name):
		v.fail(rterr.CodeName, c.Src.At("/name"), obj, "components of a structure have no namespace; they consist of upper-case letters, digits and underscores and start with a letter")
	default:
		v.identifier(c.Name, c.Src.At("/name"), obj)
	}
	v.typeRef(c.Type, owner, c.Src, obj)
	if !c.Type.Elementary() {
		if c.NotNull {
			v.fail(rterr.CodeStructure, c.Src.At("/notNull"), obj, "notNull applies only to components with a data element or domain")
		}
		if len(c.DefaultJSON) > 0 || c.Default.Kind != model.DefaultNone {
			v.fail(rterr.CodeDefault, c.Src.At("/default"), obj, "defaults apply only to components with a data element or domain")
		}
		return
	}
	d := v.elementaryDomain(c.Type)
	if d == nil {
		return
	}
	v.defaultValue(&c.DefaultJSON, &c.Default, d, c.Src.At("/default"), obj)
	eff := c.Default
	if eff.Kind == model.DefaultNone {
		eff = d.Default
	}
	if c.NotNull && eff.Kind == model.DefaultNull {
		v.fail(rterr.CodeDefault, c.Src.At("/default"), obj, "a component with notNull cannot have the default null")
	}
}

// elementaryDomain returns the domain of a data element or domain type.
func (v *validator) elementaryDomain(t model.TypeRef) *model.Domain {
	if t.Kind == model.TypeDataElement {
		if de, ok := v.s.DataElements[t.Name]; ok {
			return v.s.Domains[de.Domain]
		}
		return nil
	}
	return v.s.Domains[t.Name]
}

func (v *validator) structure(r *model.Resolver, st *model.Structure, onCycle bool) {
	obj := "structure " + st.Name
	v.objectName(st.Name, st.Owner, st.Src.At("/name"), obj)
	v.labels(obj, st.Src, st.Texts, st.Languages())
	for _, e := range st.Elements {
		switch inc, ok := e.Included.Get(); {
		case ok:
			v.typeRef(model.TypeRef{Kind: model.TypeStructure, Name: inc}, st.Owner, e.Src, obj)
		case e.Component != nil:
			v.component(st.Name, e.Component, st.Owner)
		}
	}
	if !onCycle {
		v.collisions(r, st.Name, obj, st.Src)
	}
}

// collisions reports components whose names collide after includes and
// extensions are resolved, naming both origins.
func (v *validator) collisions(r *model.Resolver, name, obj string, loc rterr.Location) {
	comps, ok := r.Components(name)
	if !ok {
		return
	}
	seen := map[string]model.ResolvedComponent{}
	for _, c := range comps {
		if prev, dup := seen[c.Name]; dup {
			v.fail(rterr.CodeStructure, loc, obj, "%s collides with %s", describeOrigin(c), describeOrigin(prev))
			continue
		}
		seen[c.Name] = c
	}
}

func describeOrigin(c model.ResolvedComponent) string {
	s := "component " + c.Name
	if len(c.Includes) > 0 {
		s += " from include " + strings.Join(c.Includes, " > ")
	} else {
		s += " of " + c.Declared
	}
	if p, ok := c.Extension.Get(); ok {
		s += fmt.Sprintf(" (added by %s)", p)
	}
	return s
}

func (v *validator) structureExtension(e *model.StructureExtension) {
	obj := "extension of structure " + e.Structure + " by " + string(e.Package)
	st, ok := v.s.Structures[e.Structure]
	switch {
	case !ok:
		v.fail(rterr.CodeStructure, e.Src.At("/structure"), obj, "structure %s does not exist", e.Structure)
		return
	case st.Owner == e.Package:
		v.fail(rterr.CodeExtension, e.Src.At("/structure"), obj, "a package cannot extend its own structure")
	case !v.s.Requires(e.Package)[st.Owner]:
		v.fail(rterr.CodeNotVisible, e.Src.At("/structure"), obj, "structure %s belongs to package %s, which %s does not require", st.Name, st.Owner, e.Package)
	}
	for i := range e.Components {
		c := &e.Components[i]
		v.component(e.Structure, c, e.Package)
		if c.NotNull && c.Default.Kind != model.DefaultValue {
			if d := v.elementaryDomain(c.Type); d == nil || d.Default.Kind != model.DefaultValue || c.Default.Kind == model.DefaultNull {
				v.fail(rterr.CodeExtension, c.Src.At("/notNull"), obj, "extension component %s must be nullable or have a default, tables that include the structure may already contain rows", c.Name)
			}
		}
	}
}

func (v *validator) arrayType(r *model.Resolver, a *model.ArrayType, onCycle map[string]bool) {
	obj := "array type " + a.Name
	v.objectName(a.Name, a.Owner, a.Src.At("/name"), obj)
	v.labels(obj, a.Src, a.Texts, a.Languages())
	if a.Row.Kind == model.TypeArray {
		v.fail(rterr.CodeStructure, a.Src.At("/rowType"), obj, "rows of an array type are a structure, a table, a data element or a domain")
		return
	}
	v.typeRef(a.Row, a.Owner, a.Src.At("/rowType"), obj)
	if a.Key == nil {
		return
	}
	if a.Row.Kind != model.TypeStructure {
		v.fail(rterr.CodeStructure, a.Src.At("/key"), obj, "only arrays of structures or tables have a key")
		return
	}
	if onCycle[a.Name] || onCycle[a.Row.Name] {
		return
	}
	comps, ok := r.Components(a.Row.Name)
	if !ok {
		return
	}
	byName := map[string]model.ResolvedComponent{}
	for _, c := range comps {
		byName[c.Name] = c
	}
	for i, f := range a.Key.Fields {
		c, ok := byName[f]
		switch {
		case !ok:
			v.fail(rterr.CodeStructure, a.Src.At(fmt.Sprintf("/key/fields/%d", i)), obj, "key field %s is not a component of %s", f, a.Row.Name)
		case !c.Type.Elementary():
			v.fail(rterr.CodeStructure, a.Src.At(fmt.Sprintf("/key/fields/%d", i)), obj, "key field %s is not elementary", f)
		}
		if slices.Index(a.Key.Fields, f) != i {
			v.fail(rterr.CodeStructure, a.Src.At(fmt.Sprintf("/key/fields/%d", i)), obj, "key field %s is listed twice", f)
		}
	}
}

// tableIncludes checks the includes of a table: they exist, are visible and
// flat, and their columns do not collide with other columns.
func (v *validator) tableIncludes(r *model.Resolver, t *model.Table, onCycle bool) {
	obj := "table " + t.Name
	for _, inc := range t.Includes {
		ref := model.TypeRef{Kind: model.TypeStructure, Name: inc.Name}
		v.typeRef(ref, t.Owner, inc.Src, obj)
		if _, known := v.s.TypeOwner(inc.Name); !known || v.s.ArrayTypes[inc.Name] != nil || onCycle {
			continue
		}
		comps, ok := r.Components(inc.Name)
		if !ok {
			continue
		}
		for _, c := range comps {
			if c.Type.Elementary() {
				continue
			}
			if p, ok := c.Extension.Get(); ok {
				v.fail(rterr.CodeStructure, inc.Src, obj, "the extension of %s by %s makes it deep with component %s, but table %s includes it", c.Declared, p, c.Name, t.Name)
			} else {
				v.fail(rterr.CodeStructure, inc.Src, obj, "includes the deep structure %s (component %s); tables include only flat structures and tables", inc.Name, c.Name)
			}
			break
		}
		for _, c := range comps {
			if c.Name == model.TenantField {
				v.fail(rterr.CodeName, inc.Src, obj, "include %s brings the reserved name %s", inc.Name, model.TenantField)
			}
		}
	}
	if !onCycle {
		v.collisions(r, t.Name, obj, t.Src)
	}
}

// transformations checks the texts of transformations, which are
// optional.
func (v *validator) transformations() {
	for _, name := range slices.Sorted(maps.Keys(v.s.Transformations)) {
		t := v.s.Transformations[name]
		if len(t.Texts) == 0 {
			continue
		}
		v.labels("transformation "+name, t.Src, t.Texts, slices.Sorted(maps.Keys(t.Texts)))
	}
}

// httpServices checks the texts of service nodes and that an anonymous
// tenant belongs to a node with anonymous access.
func (v *validator) httpServices() {
	tree := httpsvc.Build(v.s)
	for _, path := range slices.Sorted(maps.Keys(v.s.HTTPServices)) {
		h := v.s.HTTPServices[path]
		v.labels("service node "+path, h.Src, h.Texts, slices.Sorted(maps.Keys(h.Texts)))
		if n := tree.Node(path); n != nil && h.Settings.AnonymousTenant.Valid() && !n.Settings.Anonymous {
			v.fail(rterr.CodeName, h.Src.At("/settings/anonymousTenant"), "service node "+path, "anonymousTenant needs anonymous access at the node")
		}
	}
}
