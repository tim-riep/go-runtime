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
	"slices"
	"strings"
)

// ResolvedComponent is a component of a structure or table after includes
// and extensions are resolved.
type ResolvedComponent struct {
	Component
	// Declared is the structure or table that declares the component.
	Declared string
	// Includes is the chain of includes from the resolved object down to
	// Declared; empty for components of the object itself.
	Includes []string
	// Extension is the package whose extension added the component.
	Extension Optional[PackageID]
}

// TypeEdge is a dependency between types, used to find cycles.
type TypeEdge struct {
	To  string
	Via string // e.g. "include", "component ADDRESS", "row type"
}

// Resolver resolves structures, tables and array types of a schema. It is
// cycle-safe: objects on a cycle resolve to nothing and are reported by
// Cycles.
type Resolver struct {
	s     *Schema
	memo  map[string][]ResolvedComponent
	state map[string]int // 0 unvisited, 1 resolving, 2 done
}

// Resolver returns a resolver for the schema.
func (s *Schema) Resolver() *Resolver {
	return &Resolver{s: s, memo: map[string][]ResolvedComponent{}, state: map[string]int{}}
}

// fieldComponent turns a table field into a component.
func fieldComponent(f Field, origin ComponentOrigin) Component {
	t := TypeRef{Kind: TypeDomain, Name: f.Domain}
	if de, ok := f.DataElement.Get(); ok {
		t = TypeRef{Kind: TypeDataElement, Name: de}
	}
	return Component{Name: f.Name, Owner: f.Owner, Origin: origin, Description: f.Description, Type: t,
		NotNull: f.NotNull, Default: f.Default, DefaultJSON: f.DefaultJSON, Src: f.Src}
}

// Components returns the resolved components of a structure or table in
// their final order: elements in order with includes replaced by the
// components of the included object, then the components of extensions
// ordered by package. A table yields its fields with its includes and
// table extensions, never the tenant field. It reports false for unknown
// objects and objects on a cycle.
func (r *Resolver) Components(name string) ([]ResolvedComponent, bool) {
	switch r.state[name] {
	case 1:
		return nil, false // cycle
	case 2:
		out, ok := r.memo[name]
		return out, ok
	}
	r.state[name] = 1
	out, ok := r.resolve(name)
	r.state[name] = 2
	if ok {
		r.memo[name] = out
	}
	return out, ok
}

func (r *Resolver) include(name, inc string) ([]ResolvedComponent, bool) {
	comps, ok := r.Components(inc)
	if !ok {
		return nil, false
	}
	out := make([]ResolvedComponent, len(comps))
	for i, c := range comps {
		c.Includes = append([]string{inc}, c.Includes...)
		out[i] = c
	}
	return out, true
}

func (r *Resolver) resolve(name string) ([]ResolvedComponent, bool) {
	if st, ok := r.s.Structures[name]; ok {
		var out []ResolvedComponent
		for _, e := range st.Elements {
			if inc, ok := e.Included.Get(); ok {
				comps, ok := r.include(name, inc)
				if !ok {
					return nil, false
				}
				out = append(out, comps...)
				continue
			}
			if e.Component != nil {
				out = append(out, ResolvedComponent{Component: *e.Component, Declared: name})
			}
		}
		for _, ext := range r.s.ExtensionsOfStructure(name) {
			for _, c := range ext.Components {
				out = append(out, ResolvedComponent{Component: c, Declared: name, Extension: Some(ext.Package)})
			}
		}
		return r.nested(out)
	}
	if t, ok := r.s.Tables[name]; ok {
		out, ok := r.tableComponents(t)
		if !ok {
			return nil, false
		}
		return r.nested(out)
	}
	return nil, false
}

// nested resolves the structures and array types used by components, so a
// cycle through them is detected.
func (r *Resolver) nested(comps []ResolvedComponent) ([]ResolvedComponent, bool) {
	for _, c := range comps {
		if !r.typeOK(c.Type) {
			return nil, false
		}
	}
	return comps, true
}

// typeOK resolves a non-elementary type; unknown types are left to the
// validation.
func (r *Resolver) typeOK(t TypeRef) bool {
	switch t.Kind {
	case TypeStructure:
		if _, known := r.s.TypeOwner(t.Name); !known {
			return true
		}
		_, ok := r.Components(t.Name)
		return ok
	case TypeArray:
		a, known := r.s.ArrayTypes[t.Name]
		if !known {
			return true
		}
		switch r.state[t.Name] {
		case 1:
			return false
		case 2:
			_, ok := r.memo[t.Name]
			return ok
		}
		r.state[t.Name] = 1
		ok := r.typeOK(a.Row)
		r.state[t.Name] = 2
		if ok {
			r.memo[t.Name] = nil
		}
		return ok
	}
	return true
}

// tableComponents expands a table: own fields with includes at their
// places, then table extension fields.
func (r *Resolver) tableComponents(t *Table) ([]ResolvedComponent, bool) {
	var out []ResolvedComponent
	includes := slices.Clone(t.Includes)
	slices.SortStableFunc(includes, func(a, b TableInclude) int { return a.After - b.After })
	next := 0
	addIncludes := func(upTo int) bool {
		for ; next < len(includes) && includes[next].After <= upTo; next++ {
			comps, ok := r.include(t.Name, includes[next].Name)
			if !ok {
				return false
			}
			out = append(out, comps...)
		}
		return true
	}
	for i, f := range t.Fields {
		if !addIncludes(i) {
			return nil, false
		}
		c := fieldComponent(f, ComponentStructure)
		// Key fields are NOT NULL because of the key, which is not taken
		// over; the repository does not keep a separate declaration.
		if t.IsKey(f.Name) {
			c.NotNull = false
		}
		out = append(out, ResolvedComponent{Component: c, Declared: t.Name})
	}
	if !addIncludes(len(t.Fields)) {
		return nil, false
	}
	for _, e := range r.s.ExtensionsOf(t.Name) {
		for _, f := range e.Fields {
			out = append(out, ResolvedComponent{Component: fieldComponent(f, ComponentExtension), Declared: t.Name, Extension: Some(e.Package)})
		}
	}
	return out, true
}

// Flat reports whether all resolved components of a structure or table are
// elementary. Tables with only flat includes are flat.
func (r *Resolver) Flat(name string) bool {
	comps, ok := r.Components(name)
	if !ok {
		return false
	}
	for _, c := range comps {
		if !c.Type.Elementary() {
			return false
		}
	}
	return true
}

// Edges returns the dependencies of a structure, table or array type.
func (s *Schema) Edges(name string) []TypeEdge {
	var out []TypeEdge
	typeEdge := func(t TypeRef, via string) {
		if t.Kind == TypeStructure || t.Kind == TypeArray {
			out = append(out, TypeEdge{To: t.Name, Via: via})
		}
	}
	if st, ok := s.Structures[name]; ok {
		for _, e := range st.Elements {
			if inc, ok := e.Included.Get(); ok {
				out = append(out, TypeEdge{To: inc, Via: "include"})
			} else if e.Component != nil {
				typeEdge(e.Component.Type, "component "+e.Component.Name)
			}
		}
		for _, ext := range s.ExtensionsOfStructure(name) {
			for _, c := range ext.Components {
				typeEdge(c.Type, "component "+c.Name)
			}
		}
	}
	if t, ok := s.Tables[name]; ok {
		for _, inc := range t.Includes {
			out = append(out, TypeEdge{To: inc.Name, Via: "include"})
		}
	}
	if a, ok := s.ArrayTypes[name]; ok {
		typeEdge(a.Row, "row type")
	}
	return out
}

// Cycles returns every cycle between structures, tables and array types,
// each as the full chain, e.g. "/X/A -> include /X/B -> component C /X/A".
func (s *Schema) Cycles() []string {
	var names []string
	names = append(names, s.StructureNames()...)
	names = append(names, s.TableNames()...)
	names = append(names, s.ArrayTypeNames()...)
	slices.Sort(names)
	state := map[string]int{}
	type step struct{ name, via string }
	var stack []step
	seen := map[string]bool{}
	var out []string
	var visit func(name string)
	visit = func(name string) {
		state[name] = 1
		for _, e := range s.Edges(name) {
			if _, known := s.TypeOwner(e.To); !known {
				continue
			}
			stack = append(stack, step{name: e.To, via: e.Via})
			switch state[e.To] {
			case 0:
				visit(e.To)
			case 1:
				// The cycle starts where e.To was entered.
				start := 0
				for i := len(stack) - 2; i >= 0; i-- {
					if stack[i].name == e.To {
						start = i + 1
						break
					}
				}
				var parts []string
				parts = append(parts, e.To)
				members := []string{e.To}
				for _, st := range stack[start:] {
					parts = append(parts, st.via+" "+st.name)
					members = append(members, st.name)
				}
				slices.Sort(members)
				key := strings.Join(members, "|")
				if !seen[key] {
					seen[key] = true
					out = append(out, strings.Join(parts, " -> "))
				}
			}
			stack = stack[:len(stack)-1]
		}
		state[name] = 2
	}
	for _, n := range names {
		if state[n] == 0 {
			stack = append(stack[:0], step{name: n})
			visit(n)
			stack = stack[:0]
		}
	}
	return out
}

// Use is a direct use of a structure, table or array type by another one.
type Use struct {
	Kind string `json:"kind"` // "structure", "table", "array-type" or "structure-extension"
	Name string `json:"name"` // the using object; the package for extensions
	Via  string `json:"via"`  // "include", "component SEAT", "row type", "extension"
}

// UsesOf lists the structures, tables and array types that include an
// object or use it as type, and the packages that extend it, ordered by
// kind and name.
func (s *Schema) UsesOf(name string) []Use {
	var out []Use
	add := func(kind string, names []string) {
		for _, n := range names {
			for _, e := range s.Edges(n) {
				if e.To == name {
					out = append(out, Use{Kind: kind, Name: n, Via: e.Via})
				}
			}
		}
	}
	add("array-type", s.ArrayTypeNames())
	add("structure", s.StructureNames())
	for _, e := range s.ExtensionsOfStructure(name) {
		out = append(out, Use{Kind: "structure-extension", Name: string(e.Package), Via: "extension"})
	}
	add("table", s.TableNames())
	return out
}
