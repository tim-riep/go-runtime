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

package types

import (
	"slices"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// schemaTypes turns objects of the schema into types: structures and
// tables into structs, array types into arrays, data elements and domains
// into named types, domains with fixed values into enums.
type schemaTypes struct {
	c       *checker
	s       *model.Schema
	r       *model.Resolver
	own     model.PackageID
	structs map[string]*Struct
	arrays  map[string]*Array
	enums   map[string]*Enum
	named   map[string]*Named
}

func newSchemaTypes(c *checker, s *model.Schema, own model.PackageID) *schemaTypes {
	if s == nil {
		s = model.NewSchema()
	}
	return &schemaTypes{c: c, s: s, r: s.Resolver(), own: own, structs: map[string]*Struct{}, arrays: map[string]*Array{},
		enums: map[string]*Enum{}, named: map[string]*Named{}}
}

func (st *schemaTypes) exists(name string) bool {
	if _, ok := st.s.TypeOwner(name); ok {
		return true
	}
	_, de := st.s.DataElements[name]
	_, dom := st.s.Domains[name]
	return de || dom
}

// lookup returns the type for a schema object: a structure, table or array
// type, else a data element, else a domain. Data elements come before
// domains of the same name because they carry labels.
func (st *schemaTypes) lookup(name string, pos syntax.Pos) Type {
	if _, ok := st.s.Structures[name]; ok {
		return st.structType(name, pos)
	}
	if _, ok := st.s.Tables[name]; ok {
		return st.structType(name, pos)
	}
	if _, ok := st.s.ArrayTypes[name]; ok {
		return st.arrayType(name, pos)
	}
	if de, ok := st.s.DataElements[name]; ok {
		return st.dataElement(de, pos)
	}
	if d, ok := st.s.Domains[name]; ok {
		return st.domain(d, d.Name, pos)
	}
	return nil
}

func baseOf(d *model.Domain) *Basic {
	switch d.Type {
	case model.Char, model.String:
		return TypString
	case model.Int4:
		return TypInt4
	case model.Int8:
		return TypInt8
	case model.Dec:
		return DecType(d.Length, d.Decimals)
	case model.Date:
		return TypDate
	case model.Timestamp:
		return TypTimestamp
	case model.Bool:
		return TypBool
	case model.UUID:
		return TypUUID
	case model.Blob:
		return TypBytes
	}
	return TypInvalid
}

func (st *schemaTypes) dataElement(de *model.DataElement, pos syntax.Pos) Type {
	d := st.s.Domains[de.Domain]
	if d == nil {
		return TypInvalid
	}
	return st.domain(d, de.Name, pos)
}

// domain returns the enum of a domain with fixed values, or a named type
// with the given name.
func (st *schemaTypes) domain(d *model.Domain, name string, pos syntax.Pos) Type {
	if len(d.Values) > 0 {
		if e, ok := st.enums[d.Name]; ok {
			return e
		}
		e := &Enum{Name: d.Name, Base: baseOf(d), Domain: d}
		for _, v := range d.Values {
			n, ok := v.CodeName()
			if !ok {
				st.c.errorf(pos, CodeEnum, "fixed value %q of %s needs a name for code; add \"name\" to the value", v.Value, d.Name)
				continue
			}
			e.Values = append(e.Values, EnumValue{Name: n, Value: v.Value})
		}
		st.enums[d.Name] = e
		return e
	}
	if n, ok := st.named[name]; ok {
		return n
	}
	n := &Named{Name: name, Base: baseOf(d), Domain: d}
	st.named[name] = n
	return n
}

func (st *schemaTypes) typeRef(t model.TypeRef, pos syntax.Pos) Type {
	switch t.Kind {
	case model.TypeDataElement:
		if de, ok := st.s.DataElements[t.Name]; ok {
			return st.dataElement(de, pos)
		}
	case model.TypeDomain:
		if d, ok := st.s.Domains[t.Name]; ok {
			return st.domain(d, d.Name, pos)
		}
	case model.TypeStructure:
		return st.structType(t.Name, pos)
	case model.TypeArray:
		return st.arrayType(t.Name, pos)
	}
	return TypInvalid
}

// structType returns the struct of a structure or table. Fields without
// notNull are optional; the key fields of a table are never null.
func (st *schemaTypes) structType(name string, pos syntax.Pos) *Struct {
	if s, ok := st.structs[name]; ok {
		return s
	}
	s := &Struct{Name: name}
	st.structs[name] = s // before the fields, for nested references
	comps, ok := st.r.Components(name)
	if !ok {
		return s
	}
	var keys []string
	if t, ok := st.s.Tables[name]; ok {
		s.Table = true
		keys = t.PrimaryKey
		s.Key = slices.Clone(keys)
	}
	for _, comp := range comps {
		t := st.typeRef(comp.Type, pos)
		if !comp.NotNull && !slices.Contains(keys, comp.Name) {
			t = Opt(t)
		}
		def := comp.Default
		if def.IsZero() {
			switch x := NonOpt(t).(type) {
			case *Named:
				if x.Domain != nil {
					def = x.Domain.Default
				}
			case *Enum:
				if x.Domain != nil {
					def = x.Domain.Default
				}
			}
		}
		s.Fields = append(s.Fields, &Field{Name: comp.Name, Type: t, Default: def})
	}
	return s
}

func (st *schemaTypes) arrayType(name string, pos syntax.Pos) *Array {
	if a, ok := st.arrays[name]; ok {
		return a
	}
	at := st.s.ArrayTypes[name]
	a := &Array{Name: name}
	st.arrays[name] = a
	a.Elem = st.typeRef(at.Row, pos)
	if at.Key != nil {
		a.Key, a.Unique = slices.Clone(at.Key.Fields), at.Key.Unique
	}
	return a
}
