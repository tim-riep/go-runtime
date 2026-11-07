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

// Package types checks programs of the go-runtime language: names and their
// spelling, types including the types from the schema of the package, null
// checks along the control flow, checked exceptions, classes, interfaces
// and includes. The result describes every expression for the compiler.
package types

import (
	"fmt"
	"strings"

	"github.com/tim-riep/go-runtime/model"
)

// Type is a type of the language.
type Type interface {
	String() string
}

// BasicKind is the kind of a built-in type.
type BasicKind int

// Built-in types. Null is the type of the literal null, Void the result of
// methods without result.
const (
	Invalid BasicKind = iota
	String
	Int4
	Int8
	Dec
	Date
	Timestamp
	Bool
	UUID
	Bytes
	Null
	Void
	Session // a database session
)

var basicNames = [...]string{"invalid", "STRING", "INT4", "INT8", "DEC", "DATE", "TIMESTAMP", "BOOL", "UUID", "BYTES", "null", "no result", "SESSION"}

// MaxPrecision is the largest precision of DEC.
const MaxPrecision = 38

// Basic is a built-in type. Precision and Scale apply to DEC.
type Basic struct {
	Kind      BasicKind
	Precision int
	Scale     int
}

func (b *Basic) String() string {
	if b.Kind == Dec {
		return fmt.Sprintf("DEC(%d,%d)", b.Precision, b.Scale)
	}
	return basicNames[b.Kind]
}

// Predefined built-in types.
var (
	TypInvalid   = &Basic{Kind: Invalid}
	TypString    = &Basic{Kind: String}
	TypInt4      = &Basic{Kind: Int4}
	TypInt8      = &Basic{Kind: Int8}
	TypDate      = &Basic{Kind: Date}
	TypTimestamp = &Basic{Kind: Timestamp}
	TypBool      = &Basic{Kind: Bool}
	TypUUID      = &Basic{Kind: UUID}
	TypBytes     = &Basic{Kind: Bytes}
	TypNull      = &Basic{Kind: Null}
	TypVoid      = &Basic{Kind: Void}
	TypSession   = &Basic{Kind: Session}
)

// DecType returns DEC(p,s).
func DecType(p, s int) *Basic { return &Basic{Kind: Dec, Precision: p, Scale: s} }

// Named is a data element or a domain without fixed values: a built-in
// type with the rules of the domain, which the VM checks when a value is
// stored in a place of this type.
type Named struct {
	Name   string // data element or domain
	Base   *Basic
	Domain *model.Domain
}

func (n *Named) String() string { return n.Name }

// Enum is a domain with fixed values; its constants are the values.
type Enum struct {
	Name   string
	Base   *Basic // STRING, INT4 or INT8
	Domain *model.Domain
	Values []EnumValue
}

// EnumValue is a constant of an enum.
type EnumValue struct {
	Name  string // name in code
	Value string // fixed value in canonical text form
}

func (e *Enum) String() string { return e.Name }

// Index returns the position of a constant, or -1.
func (e *Enum) Index(name string) int {
	for i, v := range e.Values {
		if v.Name == name {
			return i
		}
	}
	return -1
}

// Struct is a structure or a table of the schema.
type Struct struct {
	Name   string
	Table  bool
	Key    []string // primary key of a table
	Fields []*Field
}

// Field is a component of a struct.
type Field struct {
	Name    string
	Type    Type          // optional types are *Optional
	Default model.Default // of the component or its domain
}

func (s *Struct) String() string { return s.Name }

// Field returns the field with the given name, or nil.
func (s *Struct) Field(name string) *Field {
	for _, f := range s.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// FieldIndex returns the position of a field, or -1.
func (s *Struct) FieldIndex(name string) int {
	for i, f := range s.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// Array is an array. Arrays from array types of the schema have a name and
// possibly a key.
type Array struct {
	Elem   Type
	Name   string   // array type of the schema, empty otherwise
	Key    []string // key fields of a struct element type
	Unique bool
}

func (a *Array) String() string {
	if a.Name != "" {
		return a.Name
	}
	return "array of " + a.Elem.String()
}

// Map is a map.
type Map struct {
	Key, Elem Type
}

func (m *Map) String() string { return "map of " + m.Key.String() + " to " + m.Elem.String() }

// Func is a function type.
type Func struct {
	Params []Type
	Result Type // TypVoid without result
	Raises []*Class
}

func (f *Func) String() string {
	var b strings.Builder
	b.WriteString("function (")
	for i, p := range f.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.String())
	}
	b.WriteString(")")
	if f.Result != TypVoid {
		b.WriteString(" returns " + f.Result.String())
	}
	if len(f.Raises) > 0 {
		b.WriteString(" raises ")
		for i, r := range f.Raises {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(r.Name)
		}
	}
	return b.String()
}

// Optional is T?.
type Optional struct {
	Elem Type
}

func (o *Optional) String() string { return o.Elem.String() + "?" }

// Opt returns t? unless t is optional already.
func Opt(t Type) Type {
	if _, ok := t.(*Optional); ok || t == TypNull {
		return t
	}
	return &Optional{Elem: t}
}

// NonOpt returns T for T? and t otherwise.
func NonOpt(t Type) Type {
	if o, ok := t.(*Optional); ok {
		return o.Elem
	}
	return t
}

// IsOptional reports whether t may be null.
func IsOptional(t Type) bool {
	_, ok := t.(*Optional)
	return ok || t == TypNull
}

// Underlying returns the built-in type of named types and t otherwise.
func Underlying(t Type) Type {
	if n, ok := t.(*Named); ok {
		return n.Base
	}
	return t
}

// BasicOf returns the built-in type of t, also for named types, or nil.
func BasicOf(t Type) *Basic {
	b, _ := Underlying(t).(*Basic)
	return b
}

// IsKind reports whether t is (a named type of) the built-in kind k.
func IsKind(t Type, k BasicKind) bool {
	b := BasicOf(t)
	return b != nil && b.Kind == k
}

// IsInteger reports INT4 and INT8.
func IsInteger(t Type) bool { return IsKind(t, Int4) || IsKind(t, Int8) }

// IsNumeric reports INT4, INT8 and DEC.
func IsNumeric(t Type) bool { return IsInteger(t) || IsKind(t, Dec) }

// Identical reports whether two types are the same. Named types are
// identical to their built-in type in collections and function types.
func Identical(a, b Type) bool {
	switch x := a.(type) {
	case *Basic:
		y, ok := Underlying(b).(*Basic)
		return ok && x.Kind == y.Kind && (x.Kind != Dec || x.Precision == y.Precision && x.Scale == y.Scale)
	case *Named:
		return Identical(x.Base, b)
	case *Optional:
		y, ok := b.(*Optional)
		return ok && Identical(x.Elem, y.Elem)
	case *Array:
		y, ok := b.(*Array)
		return ok && Identical(x.Elem, y.Elem)
	case *Map:
		y, ok := b.(*Map)
		return ok && Identical(x.Key, y.Key) && Identical(x.Elem, y.Elem)
	case *Func:
		y, ok := b.(*Func)
		if !ok || len(x.Params) != len(y.Params) || !Identical(x.Result, y.Result) || len(x.Raises) != len(y.Raises) {
			return false
		}
		for i := range x.Params {
			if !Identical(x.Params[i], y.Params[i]) {
				return false
			}
		}
		for i := range x.Raises {
			if x.Raises[i] != y.Raises[i] {
				return false
			}
		}
		return true
	}
	return a == b
}

// Serializable reports whether values of t can be passed to and returned
// from the command line: built-in types, structs, enums and collections of
// them.
func Serializable(t Type) bool {
	switch x := NonOpt(t).(type) {
	case *Basic:
		return x.Kind != Void && x.Kind != Invalid && x.Kind != Session
	case *Named, *Enum:
		return true
	case *Struct:
		for _, f := range x.Fields {
			if !Serializable(f.Type) {
				return false
			}
		}
		return true
	case *Array:
		return Serializable(x.Elem)
	case *Map:
		return Serializable(x.Key) && Serializable(x.Elem)
	}
	return false
}
