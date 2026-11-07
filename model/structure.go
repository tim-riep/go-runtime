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
	"encoding/json"
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/rterr"
)

// TypeKind tells what a type reference points to.
type TypeKind string

// Type kinds.
const (
	TypeDataElement TypeKind = "dataElement"
	TypeDomain      TypeKind = "domain"
	TypeStructure   TypeKind = "structure" // a structure or a table
	TypeArray       TypeKind = "arrayType"
)

// TypeRef is the type of a component or of the rows of an array type.
type TypeRef struct {
	Kind TypeKind `json:"kind"`
	Name string   `json:"name"`
}

// Elementary reports whether the type is a data element or a domain.
func (t TypeRef) Elementary() bool { return t.Kind == TypeDataElement || t.Kind == TypeDomain }

// ComponentOrigin tells who declared a component.
type ComponentOrigin string

// Component origins.
const (
	ComponentStructure ComponentOrigin = "STRUCTURE" // declared by the structure
	ComponentExtension ComponentOrigin = "EXTENSION" // added by a structure extension
)

// Component is a component of a structure.
type Component struct {
	Name        string           `json:"name"`
	Owner       PackageID        `json:"owner"`
	Origin      ComponentOrigin  `json:"origin"`
	Description Optional[string] `json:"description"`
	Type        TypeRef          `json:"type"`
	NotNull     bool             `json:"notNull"`
	Default     Default          `json:"default,omitzero"`
	// DefaultJSON holds the default as written in the package until the
	// validator converts it into Default.
	DefaultJSON json.RawMessage `json:"-"`
	Src         rterr.Location  `json:"-"`
}

// Element is an entry of a structure: a component or the include of a
// structure or table, whose components take its place.
type Element struct {
	Component *Component       `json:"component,omitempty"`
	Included  Optional[string] `json:"include"`
	Src       rterr.Location   `json:"-"`
}

// Label is the label of an object in one language.
type Label struct {
	Label       string           `json:"label"`
	Description Optional[string] `json:"description"`
}

// Structure is a data structure without database table. Tables are
// structures as well; see Schema.TypeOf.
type Structure struct {
	Name     string           `json:"name"`
	Owner    PackageID        `json:"owner"`
	Texts    map[string]Label `json:"texts"` // by language code
	Elements []Element        `json:"elements"`
	Src      rterr.Location   `json:"-"`
}

// Languages returns the languages of the texts in sorted order.
func (s *Structure) Languages() []string { return slices.Sorted(maps.Keys(s.Texts)) }

// ArrayKey is the key of an array type.
type ArrayKey struct {
	Fields []string `json:"fields"`
	Unique bool     `json:"unique"`
}

// ArrayType describes an array of rows of dynamic length.
type ArrayType struct {
	Name  string           `json:"name"`
	Owner PackageID        `json:"owner"`
	Texts map[string]Label `json:"texts"`
	Row   TypeRef          `json:"rowType"`
	Key   *ArrayKey        `json:"key"` // nil: no key
	Src   rterr.Location   `json:"-"`
}

// Languages returns the languages of the texts in sorted order.
func (a *ArrayType) Languages() []string { return slices.Sorted(maps.Keys(a.Texts)) }

// StructureExtension adds components to a structure of another package.
type StructureExtension struct {
	Package    PackageID      `json:"package"`
	Structure  string         `json:"structure"`
	Components []Component    `json:"components"`
	Src        rterr.Location `json:"-"`
}

// Key returns the identity of the extension.
func (e *StructureExtension) Key() StructureExtensionKey {
	return StructureExtensionKey{Package: e.Package, Structure: e.Structure}
}

// StructureExtensionKey identifies a structure extension.
type StructureExtensionKey struct {
	Package   PackageID
	Structure string
}

// TableInclude includes a structure or table into a table. Its components
// become columns after the first After fields of the table.
type TableInclude struct {
	Name  string         `json:"name"`
	After int            `json:"after"`
	Src   rterr.Location `json:"-"`
}

func cloneComponents(cs []Component) []Component {
	out := slices.Clone(cs)
	for i := range out {
		out[i].DefaultJSON = slices.Clone(out[i].DefaultJSON)
	}
	return out
}

func cloneStructure(s *Structure) *Structure {
	c := *s
	c.Texts = maps.Clone(s.Texts)
	c.Elements = slices.Clone(s.Elements)
	for i, e := range c.Elements {
		if e.Component != nil {
			comp := *e.Component
			comp.DefaultJSON = slices.Clone(comp.DefaultJSON)
			c.Elements[i].Component = &comp
		}
	}
	return &c
}

func cloneArrayType(a *ArrayType) *ArrayType {
	c := *a
	c.Texts = maps.Clone(a.Texts)
	if a.Key != nil {
		k := *a.Key
		k.Fields = slices.Clone(a.Key.Fields)
		c.Key = &k
	}
	return &c
}

// StructureNames returns all structure names in sorted order.
func (s *Schema) StructureNames() []string { return sortedKeys(s.Structures) }

// ArrayTypeNames returns all array type names in sorted order.
func (s *Schema) ArrayTypeNames() []string { return sortedKeys(s.ArrayTypes) }

// ExtensionsOfStructure returns the extensions of a structure ordered by
// package ID.
func (s *Schema) ExtensionsOfStructure(name string) []*StructureExtension {
	var out []*StructureExtension
	for k, e := range s.StructureExtensions {
		if k.Structure == name {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b *StructureExtension) int { return cmp.Compare(a.Package, b.Package) })
	return out
}

// SortedStructureExtensions returns all structure extensions ordered by
// structure and package.
func (s *Schema) SortedStructureExtensions() []*StructureExtension {
	out := make([]*StructureExtension, 0, len(s.StructureExtensions))
	for _, e := range s.StructureExtensions {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b *StructureExtension) int {
		return cmp.Or(cmp.Compare(a.Structure, b.Structure), cmp.Compare(a.Package, b.Package))
	})
	return out
}

// TypeOwner returns the owner of a structure, table or array type and
// whether it exists.
func (s *Schema) TypeOwner(name string) (PackageID, bool) {
	if st, ok := s.Structures[name]; ok {
		return st.Owner, true
	}
	if t, ok := s.Tables[name]; ok {
		return t.Owner, true
	}
	if a, ok := s.ArrayTypes[name]; ok {
		return a.Owner, true
	}
	return "", false
}
