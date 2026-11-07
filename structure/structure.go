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

// Package structure provides the resolved definition of a structure, a
// table seen as structure, or an array type: includes replaced by their
// components, extensions applied, and every component with its type,
// labels and origin.
//
// Tables appear as flat structures: their fields, included components and
// extension fields, never the tenant field, keys or foreign keys.
//
// The types are stable: fields are only added, never renamed or removed,
// within one FormatVersion. null and "" are different values throughout,
// as in package setting.
package structure

import (
	"encoding/json"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// FormatVersion is the version of the structure of Definition.
const FormatVersion = 1

// Kind is the kind of a resolved object.
type Kind string

// Kinds.
const (
	KindStructure Kind = "structure"
	KindTable     Kind = "table"
	KindArrayType Kind = "array-type"
)

// ComponentKind tells what a component holds.
type ComponentKind string

// Component kinds.
const (
	Elementary ComponentKind = "elementary" // a data element or domain
	Nested     ComponentKind = "structure"  // a structure or table
	Array      ComponentKind = "array"      // an array type
)

// Definition is the resolved definition of a structure, table or array
// type.
type Definition struct {
	FormatVersion int             `json:"formatVersion"`
	Kind          Kind            `json:"kind"`
	Name          string          `json:"name"`
	Owner         model.PackageID `json:"owner"`
	// Texts holds the label by language code; tables have none.
	Texts map[string]Text `json:"texts"`
	// Flat tells whether all components are elementary; always false for
	// array types.
	Flat bool `json:"flat"`
	// Components lists the components of structures and tables in their
	// final order.
	Components []Component `json:"components,omitempty"`
	// Array describes the rows of an array type.
	Array *ArrayDef `json:"array,omitempty"`
	// Extensions lists the packages that extend the object, in package
	// order.
	Extensions []model.PackageID `json:"extensions,omitempty"`
}

// Text is a label in one language.
type Text struct {
	Label       string                 `json:"label"`
	Description model.Optional[string] `json:"description"`
}

// ArrayDef describes the rows and key of an array type.
type ArrayDef struct {
	// Row is the row type; it has no name and no origin.
	Row Component `json:"row"`
	Key *Key      `json:"key"` // nil: no key
}

// Key is the key of an array type.
type Key struct {
	Fields []string `json:"fields"`
	Unique bool     `json:"unique"`
}

// Origin tells where a component comes from.
type Origin struct {
	// Declared is the structure or table that declares the component.
	Declared string `json:"declared"`
	// Includes is the chain of includes from the resolved object down to
	// Declared; empty for components of the object itself.
	Includes []string `json:"includes"`
	// Extension is the package whose extension added the component.
	Extension model.Optional[model.PackageID] `json:"extension"`
	// Package is the owner of the component.
	Package model.PackageID `json:"package"`
}

// Component is a resolved component.
type Component struct {
	Name        string                 `json:"name,omitempty"`
	Description model.Optional[string] `json:"description"`
	Origin      *Origin                `json:"origin,omitempty"`
	Kind        ComponentKind          `json:"kind"`

	// Elementary components.
	DataElement *DataElement       `json:"dataElement,omitempty"`
	Domain      string             `json:"domain,omitempty"`
	Type        model.BaseType     `json:"type,omitempty"`
	Length      int                `json:"length,omitempty"`
	Decimals    int                `json:"decimals,omitempty"`
	FixedValues []model.FixedValue `json:"fixedValues,omitempty"`
	// AllowEmpty tells whether the component accepts "" (CHAR and STRING
	// only).
	AllowEmpty model.Optional[bool] `json:"allowEmpty"`
	NotNull    bool                 `json:"notNull"`
	// Default is the effective default: none (left out), null, or a value
	// in canonical text form, including "".
	Default model.Default `json:"default,omitzero"`

	// Structure is the resolved nested structure or table.
	Structure *Definition `json:"structure,omitempty"`
	// ArrayType is the resolved array type.
	ArrayType *Definition `json:"arrayType,omitempty"`
}

// DataElement is the data element of a component with its labels.
type DataElement struct {
	Name  string                           `json:"name"`
	Owner model.PackageID                  `json:"owner"`
	Texts map[string]model.DataElementText `json:"texts"`
}

// JSON returns the definition as indented JSON.
func (d *Definition) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Component returns the component with the given name, or nil.
func (d *Definition) Component(name string) *Component {
	for i := range d.Components {
		if d.Components[i].Name == name {
			return &d.Components[i]
		}
	}
	return nil
}

// Resolve builds the definition of a structure, table or array type from a
// valid schema. Texts are limited to languages.
func Resolve(s *model.Schema, languages []string, name string) (*Definition, error) {
	langs := slices.Clone(languages)
	slices.Sort(langs)
	r := &resolver{s: s, res: s.Resolver(), langs: langs, active: map[string]bool{}}
	return r.definition(name)
}

type resolver struct {
	s      *model.Schema
	res    *model.Resolver
	langs  []string
	active map[string]bool
}

func (r *resolver) texts(t map[string]model.Label) map[string]Text {
	out := map[string]Text{}
	for _, l := range r.langs {
		if x, ok := t[l]; ok {
			out[l] = Text{Label: x.Label, Description: x.Description}
		}
	}
	return out
}

func (r *resolver) definition(name string) (*Definition, error) {
	if r.active[name] {
		return nil, &rterr.ValidationError{Code: rterr.CodeStructure, Object: "structure " + name, Message: "contains itself"}
	}
	r.active[name] = true
	defer delete(r.active, name)

	if a, ok := r.s.ArrayTypes[name]; ok {
		d := &Definition{FormatVersion: FormatVersion, Kind: KindArrayType, Name: name, Owner: a.Owner, Texts: r.texts(a.Texts)}
		row, err := r.component(model.ResolvedComponent{Component: model.Component{Type: a.Row}})
		if err != nil {
			return nil, err
		}
		row.Origin = nil
		d.Array = &ArrayDef{Row: row}
		if a.Key != nil {
			d.Array.Key = &Key{Fields: slices.Clone(a.Key.Fields), Unique: a.Key.Unique}
		}
		return d, nil
	}
	d := &Definition{FormatVersion: FormatVersion, Name: name, Texts: map[string]Text{}}
	switch {
	case r.s.Structures[name] != nil:
		st := r.s.Structures[name]
		d.Kind, d.Owner, d.Texts = KindStructure, st.Owner, r.texts(st.Texts)
		for _, e := range r.s.ExtensionsOfStructure(name) {
			d.Extensions = append(d.Extensions, e.Package)
		}
	case r.s.Tables[name] != nil:
		d.Kind, d.Owner = KindTable, r.s.Tables[name].Owner
		for _, e := range r.s.ExtensionsOf(name) {
			d.Extensions = append(d.Extensions, e.Package)
		}
	default:
		return nil, &rterr.NotFound{Kind: "structure or array type", Name: name}
	}
	comps, ok := r.res.Components(name)
	if !ok {
		return nil, &rterr.ValidationError{Code: rterr.CodeStructure, Object: "structure " + name, Message: "cannot be resolved"}
	}
	d.Flat = r.res.Flat(name)
	d.Components = []Component{}
	for _, c := range comps {
		rc, err := r.component(c)
		if err != nil {
			return nil, err
		}
		d.Components = append(d.Components, rc)
	}
	return d, nil
}

func (r *resolver) component(c model.ResolvedComponent) (Component, error) {
	out := Component{Name: c.Name, Description: c.Description, NotNull: c.NotNull,
		Origin: &Origin{Declared: c.Declared, Includes: slices.Clone(c.Includes), Extension: c.Extension, Package: c.Owner}}
	if out.Origin.Includes == nil {
		out.Origin.Includes = []string{}
	}
	switch c.Type.Kind {
	case model.TypeStructure, model.TypeArray:
		def, err := r.definition(c.Type.Name)
		if err != nil {
			return Component{}, err
		}
		if c.Type.Kind == model.TypeStructure {
			out.Kind, out.Structure = Nested, def
		} else {
			out.Kind, out.ArrayType = Array, def
		}
		return out, nil
	}
	out.Kind = Elementary
	domain := c.Type.Name
	if c.Type.Kind == model.TypeDataElement {
		de := r.s.DataElements[c.Type.Name]
		if de == nil {
			return Component{}, &rterr.NotFound{Kind: "data element", Name: c.Type.Name}
		}
		domain = de.Domain
		out.DataElement = &DataElement{Name: de.Name, Owner: de.Owner, Texts: map[string]model.DataElementText{}}
		for _, l := range r.langs {
			if t, ok := de.Texts[l]; ok {
				out.DataElement.Texts[l] = t
			}
		}
	}
	dom := r.s.Domains[domain]
	if dom == nil {
		return Component{}, &rterr.NotFound{Kind: "domain", Name: domain}
	}
	out.Domain, out.Type, out.FixedValues = dom.Name, dom.Type, slices.Clone(dom.Values)
	if dom.Type.HasLength() {
		out.Length, out.Decimals = dom.Length, dom.Decimals
	}
	if dom.Type.IsString() {
		out.AllowEmpty = model.Some(dom.AllowsEmpty())
	}
	out.Default = c.Default
	if out.Default.Kind == model.DefaultNone {
		out.Default = dom.Default
	}
	return out, nil
}
