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

// Package objects checks the objects of package-defined types against the
// structure of their type: the content, the references it holds and its
// texts per language.
//
// References are components whose data element is one of the reference
// data elements of TRBASE (model.ReferenceDataElements) or has the domain
// model.ObjectReferenceDomain. A dependent reference (a field, an action,
// an entity set, an asset repository) refers to the nearest component of
// its anchor kind in the same structure, else in the structures that
// enclose it. Texts are arrays whose rows include model.TextLanguageStructure
// with the unique key LANGUAGE.
package objects

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
	"github.com/tim-riep/go-runtime/structure/jsonvalue"
)

// Problem is a reason why a type or an object is invalid.
type Problem struct {
	// Pointer locates the problem in the content of an object or in the
	// content structure of a type; empty for the whole document.
	Pointer string
	Message string
}

// Type is an object type with its resolved content structure.
type Type struct {
	Model *model.ObjectType
	Def   *structure.Definition
	s     *model.Schema
}

// Resolve resolves the content structure of an object type and checks
// it. Texts of the structure are limited to languages.
func Resolve(s *model.Schema, t *model.ObjectType, languages []string) (*Type, []Problem) {
	st := s.Structures[t.Content]
	if st == nil {
		if s.Tables[t.Content] != nil {
			return nil, []Problem{{Message: fmt.Sprintf("the content of an object type is a structure; %s is a table", t.Content)}}
		}
		return nil, []Problem{{Message: fmt.Sprintf("structure %s does not exist", t.Content)}}
	}
	if st.Owner != t.Owner {
		return nil, []Problem{{Message: fmt.Sprintf("the content structure %s belongs to %s; it must belong to %s, which defines the type", t.Content, st.Owner, t.Owner)}}
	}
	def, err := structure.Resolve(s, languages, t.Content)
	if err != nil {
		return nil, []Problem{{Message: err.Error()}}
	}
	ty := &Type{Model: t, Def: def, s: s}
	return ty, ty.check()
}

// Kind returns the reference kind of a component, and for references to
// objects the type they name, if any.
func (t *Type) Kind(c *structure.Component) (model.ReferenceKind, string, bool) {
	if c.Kind != structure.Elementary || c.DataElement == nil {
		return "", "", false
	}
	if k, ok := model.ReferenceDataElements[c.DataElement.Name]; ok {
		return k, "", true
	}
	if c.Domain == model.ObjectReferenceDomain {
		typ := ""
		if de := t.s.DataElements[c.DataElement.Name]; de != nil {
			typ = de.ObjectType.Or("")
		}
		return model.RefObject, typ, true
	}
	return "", "", false
}

// IsTextArray reports whether an array type holds texts per language.
func IsTextArray(at *structure.Definition) bool {
	row := at.Array.Row
	if row.Kind != structure.Nested {
		return false
	}
	c := row.Structure.Component(model.TextLanguageComponent)
	return c != nil && c.Origin != nil &&
		(c.Origin.Declared == model.TextLanguageStructure || slices.Contains(c.Origin.Includes, model.TextLanguageStructure))
}

// check checks the content structure: no BLOB, dependent references have
// an anchor in scope, text arrays have the unique key LANGUAGE.
func (t *Type) check() []Problem {
	var out []Problem
	var walk func(def *structure.Definition, ptr string, scope [][]model.ReferenceKind)
	walk = func(def *structure.Definition, ptr string, scope [][]model.ReferenceKind) {
		var here []model.ReferenceKind
		for i := range def.Components {
			if k, _, ok := t.Kind(&def.Components[i]); ok {
				here = append(here, k)
			}
		}
		scope = append(slices.Clip(scope), here)
		for i := range def.Components {
			c := &def.Components[i]
			at := ptr + "/" + c.Name
			t.component(c, at, scope, walk, &out)
		}
	}
	walk(t.Def, "", nil)
	return out
}

func (t *Type) component(c *structure.Component, at string, scope [][]model.ReferenceKind,
	walk func(*structure.Definition, string, [][]model.ReferenceKind), out *[]Problem) {
	switch c.Kind {
	case structure.Nested:
		walk(c.Structure, at, scope)
	case structure.Array:
		at2 := c.ArrayType
		if row := at2.Array.Row.Structure; row != nil && row.Component(model.TextLanguageComponent) != nil && IsTextArray(at2) {
			key := at2.Array.Key
			if key == nil || !key.Unique || !slices.Equal(key.Fields, []string{model.TextLanguageComponent}) {
				*out = append(*out, Problem{Pointer: at, Message: fmt.Sprintf("the array type %s holds texts and needs the unique key %s", at2.Name, model.TextLanguageComponent)})
			}
		}
		row := at2.Array.Row
		t.component(&row, at, scope, walk, out)
	case structure.Elementary:
		if c.Type == model.Blob {
			*out = append(*out, Problem{Pointer: at, Message: "BLOB components cannot be written as JSON"})
		}
		k, typ, ok := t.Kind(c)
		if !ok {
			return
		}
		if typ != "" && t.s.ObjectTypes[typ] == nil {
			*out = append(*out, Problem{Pointer: at, Message: fmt.Sprintf("data element %s refers to object type %s, which does not exist", c.DataElement.Name, typ)})
		}
		if anchors := k.Anchors(); anchors != nil && !inScope(scope, anchors) {
			*out = append(*out, Problem{Pointer: at, Message: fmt.Sprintf("%s refers to a %s, but no component of the structure or of an enclosing structure has the data element of a %s", c.DataElement.Name, k, anchorList(anchors))})
		}
	}
}

func inScope(scope [][]model.ReferenceKind, anchors []model.ReferenceKind) bool {
	for _, level := range scope {
		for _, a := range anchors {
			if slices.Contains(level, a) {
				return true
			}
		}
	}
	return false
}

func anchorList(anchors []model.ReferenceKind) string {
	s := string(anchors[0])
	for _, a := range anchors[1:] {
		s += " or " + string(a)
	}
	return s
}

// Checked is an object with its checked content.
type Checked struct {
	Value      *jsonvalue.Value
	Content    []byte
	Hash       string
	References []model.ObjectReference
	// Languages are the languages of all texts.
	Languages []string
}

// Check checks the content of an object against its type.
func (t *Type) Check(content []byte) (*Checked, []Problem) {
	v, problems := jsonvalue.Check(t.Def, content)
	if len(problems) > 0 {
		out := make([]Problem, len(problems))
		for i, p := range problems {
			out[i] = Problem{Pointer: p.Pointer, Message: p.Message}
		}
		return nil, out
	}
	c := &Checked{Value: v, Content: v.Canonical()}
	sum := sha256.Sum256(c.Content)
	c.Hash = hex.EncodeToString(sum[:])
	var out []Problem
	langs := map[string]bool{}
	v.Walk(func(x *jsonvalue.Value, enclosing []*jsonvalue.Value) {
		if x.Null || x.Component == nil {
			return
		}
		switch x.Component.Kind {
		case structure.Array:
			if IsTextArray(x.Definition) {
				t.texts(x, langs, &out)
			}
		case structure.Elementary:
			k, typ, ok := t.Kind(x.Component)
			if !ok {
				return
			}
			ref := model.ObjectReference{Kind: k, Target: x.Text, ObjectType: typ, Pointer: x.Pointer}
			if anchors := k.Anchors(); anchors != nil {
				a := t.anchor(enclosing, anchors)
				if a == nil || a.Null {
					out = append(out, Problem{Pointer: x.Pointer, Message: fmt.Sprintf("%s needs a %s in its structure or an enclosing one", k, anchorList(anchors))})
					return
				}
				ref.Target, ref.Part = a.Text, x.Text
				// An action anchored at an entity belongs to the business
				// object of the entity.
				if ak, _, _ := t.Kind(a.Component); ak == model.RefEntity && k == model.RefAction {
					if b, _ := t.s.BusinessObjectOf(a.Text); b != nil {
						ref.Target = b.Name
					}
				}
			}
			c.References = append(c.References, ref)
		}
	})
	if len(out) > 0 {
		return nil, out
	}
	c.Languages = slices.Sorted(func(yield func(string) bool) {
		for l := range langs {
			if !yield(l) {
				return
			}
		}
	})
	return c, nil
}

// anchor returns the nearest component value of one of the anchor kinds.
func (t *Type) anchor(enclosing []*jsonvalue.Value, anchors []model.ReferenceKind) *jsonvalue.Value {
	for i := len(enclosing) - 1; i >= 0; i-- {
		for _, a := range anchors {
			for _, f := range enclosing[i].Fields {
				if k, _, ok := t.Kind(f.Value.Component); ok && k == a {
					return f.Value
				}
			}
		}
	}
	return nil
}

func (t *Type) texts(arr *jsonvalue.Value, langs map[string]bool, out *[]Problem) {
	english := false
	for _, row := range arr.Items {
		l := row.Field(model.TextLanguageComponent)
		if l == nil || l.Null {
			continue
		}
		langs[l.Text] = true
		if l.Text == model.DefaultLanguage {
			english = true
		}
	}
	if !english {
		*out = append(*out, Problem{Pointer: arr.Pointer, Message: fmt.Sprintf("texts in English (%s) are mandatory", model.DefaultLanguage)})
	}
}

// RemoveLanguage removes the rows in language code from the text arrays
// of all objects of s and returns how many it removed. Objects whose type
// does not resolve stay as they are.
func RemoveLanguage(s *model.Schema, code string) int {
	n := 0
	types := map[string]*Type{}
	for _, name := range s.ObjectNames() {
		o := s.Objects[name]
		t, ok := types[o.Type]
		if !ok {
			if ot := s.ObjectTypes[o.Type]; ot != nil {
				t, _ = Resolve(s, ot, nil)
			}
			types[o.Type] = t
		}
		if t == nil {
			continue
		}
		v, problems := jsonvalue.Check(t.Def, o.Content)
		if len(problems) > 0 {
			continue
		}
		removed := 0
		v.Walk(func(x *jsonvalue.Value, _ []*jsonvalue.Value) {
			if x.Null || x.Component == nil || x.Component.Kind != structure.Array || !IsTextArray(x.Definition) {
				return
			}
			x.Items = slices.DeleteFunc(x.Items, func(row *jsonvalue.Value) bool {
				l := row.Field(model.TextLanguageComponent)
				if l != nil && !l.Null && l.Text == code {
					removed++
					return true
				}
				return false
			})
		})
		if removed == 0 {
			continue
		}
		n += removed
		o.Content = v.Canonical()
		sum := sha256.Sum256(o.Content)
		o.Hash = hex.EncodeToString(sum[:])
	}
	return n
}
