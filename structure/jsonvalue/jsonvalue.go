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

// Package jsonvalue checks JSON values against resolved structures and
// writes them in canonical form.
//
// The format is that of the native transformation /TRBASE/JSON: structures
// are objects with their components, arrays are lists, DEC values are
// strings with all digits of their scale, integers are numbers, DATE and
// TIMESTAMP are ISO 8601 and components with fixed values hold the name of
// the value. null and "" are different values. The canonical form lists
// every component in the order of the structure: a missing component
// without notNull is null, a missing component with notNull takes its
// default and is an error without one.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
)

// Problem is a reason why a value does not fit its type.
type Problem struct {
	// Pointer locates the value (JSON Pointer).
	Pointer string
	Message string
}

func (p Problem) String() string {
	if p.Pointer == "" {
		return p.Message
	}
	return p.Pointer + ": " + p.Message
}

// Value is a checked value.
type Value struct {
	// Component is the type of the value: the component of a structure or
	// the row of an array type; nil for the root structure.
	Component *structure.Component
	// Definition is the structure of nested values and the root.
	Definition *structure.Definition
	Null       bool
	// Text is the canonical text form of an elementary value; for
	// components with fixed values the value, not its name.
	Text   string
	Fields []Field
	Items  []*Value
	// Pointer locates the value in the document.
	Pointer string
}

// Field is a component of a structure value.
type Field struct {
	Name  string
	Value *Value
}

// Field returns the value of a component, or nil.
func (v *Value) Field(name string) *Value {
	for _, f := range v.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

// Check checks raw against the structure def and returns the value with
// all components, or the problems found.
func Check(def *structure.Definition, raw json.RawMessage) (*Value, []Problem) {
	c := &checker{}
	v := c.structure(def, raw, "")
	if len(c.problems) > 0 {
		return nil, c.problems
	}
	return v, nil
}

type checker struct {
	problems []Problem
}

func (c *checker) fail(pointer, format string, args ...any) {
	c.problems = append(c.problems, Problem{Pointer: pointer, Message: fmt.Sprintf(format, args...)})
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (c *checker) structure(def *structure.Definition, raw json.RawMessage, ptr string) *Value {
	v := &Value{Definition: def, Pointer: ptr}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		c.fail(ptr, "expected an object for %s", def.Name)
		return v
	}
	for name := range members {
		if def.Component(name) == nil {
			c.fail(ptr+"/"+escape(name), "%s has no component %s", def.Name, name)
		}
	}
	for i := range def.Components {
		comp := &def.Components[i]
		at := ptr + "/" + escape(comp.Name)
		fr, ok := members[comp.Name]
		var fv *Value
		switch {
		case ok:
			fv = c.component(comp, fr, at)
		case comp.NotNull && comp.Default.Kind == model.DefaultValue:
			fv = &Value{Component: comp, Text: comp.Default.Value, Pointer: at}
		case comp.NotNull:
			c.fail(ptr, "%s needs %s", def.Name, comp.Name)
			continue
		default:
			fv = &Value{Component: comp, Null: true, Pointer: at}
		}
		v.Fields = append(v.Fields, Field{Name: comp.Name, Value: fv})
	}
	return v
}

func (c *checker) component(comp *structure.Component, raw json.RawMessage, ptr string) *Value {
	if isNull(raw) {
		if comp.NotNull {
			c.fail(ptr, "null is not allowed for %s", describe(comp))
		}
		return &Value{Component: comp, Null: true, Pointer: ptr}
	}
	switch comp.Kind {
	case structure.Nested:
		v := c.structure(comp.Structure, raw, ptr)
		v.Component = comp
		return v
	case structure.Array:
		return c.array(comp, raw, ptr)
	}
	return c.elementary(comp, raw, ptr)
}

func (c *checker) array(comp *structure.Component, raw json.RawMessage, ptr string) *Value {
	v := &Value{Component: comp, Definition: comp.ArrayType, Pointer: ptr}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		c.fail(ptr, "expected a list for %s", comp.ArrayType.Name)
		return v
	}
	row := &comp.ArrayType.Array.Row
	for i, it := range items {
		at := ptr + "/" + strconv.Itoa(i)
		if isNull(it) {
			c.fail(at, "rows of %s cannot be null", comp.ArrayType.Name)
			continue
		}
		v.Items = append(v.Items, c.component(row, it, at))
	}
	if key := comp.ArrayType.Array.Key; key != nil && key.Unique {
		seen := map[string]int{}
		for i, it := range v.Items {
			k := keyOf(it, key.Fields)
			if j, ok := seen[k]; ok {
				c.fail(it.Pointer, "row %d has the same key as row %d in %s", i+1, j+1, comp.ArrayType.Name)
				continue
			}
			seen[k] = i
		}
	}
	return v
}

// keyOf returns the key of a row: the values of the key fields, or the row
// itself for elementary rows.
func keyOf(row *Value, fields []string) string {
	if row.Component.Kind != structure.Nested {
		return text(row)
	}
	var parts []string
	for _, f := range fields {
		parts = append(parts, text(row.Field(f)))
	}
	return strings.Join(parts, "\x00")
}

func text(v *Value) string {
	if v == nil || v.Null {
		return "\x01null"
	}
	return v.Text
}

func (c *checker) elementary(comp *structure.Component, raw json.RawMessage, ptr string) *Value {
	v := &Value{Component: comp, Pointer: ptr}
	if len(comp.FixedValues) > 0 {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			c.fail(ptr, "expected the name of a fixed value of %s", describe(comp))
			return v
		}
		for _, fv := range comp.FixedValues {
			if n, ok := fv.CodeName(); ok && n == name {
				v.Text = fv.Value
				return v
			}
		}
		c.fail(ptr, "%q is no fixed value of %s; allowed are %s", name, describe(comp), strings.Join(names(comp.FixedValues), ", "))
		return v
	}
	if comp.Type == model.Blob {
		c.fail(ptr, "BLOB values cannot be written as JSON")
		return v
	}
	d := model.Domain{Name: comp.Domain, Type: comp.Type, Length: comp.Length, Decimals: comp.Decimals}
	t, err := d.ParseJSON(raw)
	if err != nil {
		c.fail(ptr, "%v", err)
		return v
	}
	if t == "" && comp.Type.IsString() && !comp.AllowEmpty.Or(true) {
		c.fail(ptr, "%s does not allow the empty string", describe(comp))
		return v
	}
	v.Text = t
	return v
}

func names(values []model.FixedValue) []string {
	var out []string
	for _, v := range values {
		if n, ok := v.CodeName(); ok {
			out = append(out, n)
		}
	}
	return out
}

func describe(comp *structure.Component) string {
	if comp.DataElement != nil {
		return comp.DataElement.Name
	}
	if comp.Domain != "" {
		return comp.Domain
	}
	if comp.Structure != nil {
		return comp.Structure.Name
	}
	if comp.ArrayType != nil {
		return comp.ArrayType.Name
	}
	return "the value"
}

func escape(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// MarshalJSON writes the canonical form.
func (v *Value) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	v.write(&b)
	return b.Bytes(), nil
}

// Canonical returns the canonical form.
func (v *Value) Canonical() json.RawMessage {
	b, _ := v.MarshalJSON()
	return b
}

func (v *Value) write(b *bytes.Buffer) {
	str := func(s string) {
		out, _ := json.Marshal(s)
		b.Write(out)
	}
	switch {
	case v.Null:
		b.WriteString("null")
	case v.Component == nil || v.Component.Kind == structure.Nested:
		b.WriteByte('{')
		for i, f := range v.Fields {
			if i > 0 {
				b.WriteByte(',')
			}
			str(f.Name)
			b.WriteByte(':')
			f.Value.write(b)
		}
		b.WriteByte('}')
	case v.Component.Kind == structure.Array:
		b.WriteByte('[')
		for i, it := range v.Items {
			if i > 0 {
				b.WriteByte(',')
			}
			it.write(b)
		}
		b.WriteByte(']')
	case len(v.Component.FixedValues) > 0:
		i := slices.IndexFunc(v.Component.FixedValues, func(f model.FixedValue) bool { return f.Value == v.Text })
		n, _ := v.Component.FixedValues[i].CodeName()
		str(n)
	case v.Component.Type == model.Int4 || v.Component.Type == model.Int8 || v.Component.Type == model.Bool:
		b.WriteString(v.Text)
	default:
		str(v.Text)
	}
}

// Walk calls fn for v and every value below it in document order, with the
// structure values that enclose it, innermost last.
func (v *Value) Walk(fn func(v *Value, enclosing []*Value)) {
	v.walk(fn, nil)
}

func (v *Value) walk(fn func(*Value, []*Value), enclosing []*Value) {
	fn(v, enclosing)
	if v.Null {
		return
	}
	if v.Component == nil || v.Component.Kind == structure.Nested {
		inner := append(slices.Clip(enclosing), v)
		for _, f := range v.Fields {
			f.Value.walk(fn, inner)
		}
	}
	for _, it := range v.Items {
		it.walk(fn, enclosing)
	}
}
