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

// Package bytecode defines the compiled form of a package: classes with
// the memory layout of their objects, functions with typed stack
// instructions, exception and line tables, and a binary encoding with a
// format version so that compiled programs can be stored.
package bytecode

// FormatVersion is the version of the binary encoding.
const FormatVersion = 3

// Ref is a direct reference to a class or function of a program or one of
// its imports: the slot of the program (0 for the program itself, n for
// Imports[n-1]) and the index in its table.
type Ref = int

// RefBits is the number of bits of the index in a Ref.
const RefBits = 20

// MakeRef returns the reference to entry idx of the program in slot.
func MakeRef(slot, idx int) Ref { return slot<<RefBits | idx }

// RefSlot returns the slot of a reference.
func RefSlot(r Ref) int { return r >> RefBits }

// RefIndex returns the index of a reference.
func RefIndex(r Ref) int { return r & (1<<RefBits - 1) }

// Program is a compiled package.
type Program struct {
	Package         string
	LanguageVersion int
	// Imports are the packages whose classes and functions the program
	// refers to: the kernel and all packages it requires, also indirectly,
	// in installation order.
	Imports []string
	Files   []File      // source files, referenced by index
	Chains  []Chain     // include chains, referenced by index; 0 is the empty chain
	Strings []string    // string constants
	Bytes   [][]byte    // BYTES constants
	Decs    [][2]uint64 // DEC constants: low and high word of the unscaled value
	Types   []TypeDesc  // type descriptions, referenced by index
	Layouts []Layout
	Enums   []Enum
	Classes []Class
	Funcs   []Func
	Queries []Query // database statements
	Tests   bool    // contains test classes
}

// File is a source file.
type File struct {
	Path   string
	SHA256 string
}

// Chain is the chain of includes through which code was inserted,
// innermost first.
type Chain struct {
	Sites []ChainSite
}

// ChainSite is one insertion of an include.
type ChainSite struct {
	Include string
	File    int
	Line    int
}

// TypeKind is the kind of a type description.
type TypeKind uint8

// Type kinds.
const (
	TString TypeKind = iota + 1
	TInt4
	TInt8
	TDec
	TDate
	TTimestamp
	TBool
	TUUID
	TBytes
	TEnum
	TStruct
	TArray
	TMap
	TObject
	TFunc
	TVoid
)

// TypeDesc describes a type for zero values, copies, comparison, keys and
// the conversion from and to JSON.
type TypeDesc struct {
	Kind      TypeKind
	Optional  bool
	Name      string // enum, struct, class or array type
	Precision int    // DEC
	Scale     int
	Elem      int // array element, map value
	Key       int // map key
	Layout    int // struct
	Enum      int
	Class     Ref
	// Arrays of array types with a unique key keep an index over these
	// fields of their struct elements.
	KeyFields []int
	Unique    bool
	// Rules of named types, checked when a value is stored: the maximum
	// number of characters (0: none) and whether "" is rejected.
	MaxLength int
	NoEmpty   bool
	Named     bool
}

// FieldKind tells how a field is stored.
type FieldKind uint8

// Field kinds.
const (
	FBool    FieldKind = iota + 1 // one bit
	FOptBool                      // two bits: present and value
	FFixed                        // fixed size bytes at an offset
	FRef                          // a slot in the reference slice
)

// Field is a field of a layout.
type Field struct {
	Name     string
	Type     int // TypeDesc
	Kind     FieldKind
	Offset   int // byte offset of fixed values and bits
	Bit      int // bit of FBool and FOptBool (the present bit for FOptBool; the value bit follows)
	Size     int // FFixed
	Present  int // byte offset of the present bit of optional FFixed and FRef fields, -1 if not optional
	PresBit  int
	Ref      int // FRef slot
	Optional bool
	// Default is the initial value of fields of tables and structures
	// with a default in the schema, in canonical text form.
	Default    string
	HasDefault bool
}

// Layout is the memory layout of objects of a class or values of a struct.
type Layout struct {
	Name   string
	Size   int // bytes of data
	Refs   int // reference slots
	Fields []Field
}

// Field returns the index of a field, or -1.
func (l *Layout) Field(name string) int {
	for i, f := range l.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// Enum is an enum with its constants.
type Enum struct {
	Name   string
	Base   TypeKind // TString, TInt4 or TInt8
	Names  []string
	Values []string
}

// Class is a compiled class or interface.
type Class struct {
	Name      string
	Interface bool
	Kernel    bool
	Test      bool
	Exception bool // extends /RT/EXCEPTION or is a runtime error
	Base      Ref  // -1 without base class
	Layout    int  // instance layout, -1 for interfaces
	Statics   int  // layout of the static attributes, -1 if none
	Ctor      int  // constructor function, -1 for interfaces
	Init      int  // function setting the static attributes, -1 if none
	VTable    []Ref
	VSlots    []string // the method of every slot of VTable
	ITables   []ITable
	Supers    []Ref // the class itself, its base classes and all interfaces, for type tests
	Methods   []MethodRef
}

// ITable maps the methods of an interface to the functions of a class.
type ITable struct {
	Interface Ref
	Funcs     []Ref
}

// MethodRef names a method of a class for calls from outside.
type MethodRef struct {
	Name   string
	Func   Ref
	Static bool
	Public bool
	Test   bool
}

// Func is a compiled method, constructor or function literal.
type Func struct {
	Name     string
	Class    int
	Params   int // including the receiver of instance methods
	Locals   int // slots including parameters
	Instance bool
	Captures int // number of captured cells of a function literal
	Code     []int32
	Handlers []Handler
	Lines    []Line
	// ParamTypes and Result describe the signature for calls from outside.
	ParamTypes []int
	Result     int // TypeDesc, -1 without result
}

// Handler catches exceptions raised between Start and End.
type Handler struct {
	Start, End int
	Target     int
	Class      Ref // -1 catches everything (finally)
}

// Line maps code from PC on to a source position.
type Line struct {
	PC     int
	File   int
	Line   int
	Column int
	Chain  int
}

// Position returns the source position of a program counter.
func (f *Func) Position(pc int) (Line, bool) {
	var out Line
	found := false
	for _, l := range f.Lines {
		if l.PC > pc {
			break
		}
		out, found = l, true
	}
	return out, found
}

// Class returns the index of a class by name, or -1.
func (p *Program) Class(name string) int {
	for i := range p.Classes {
		if p.Classes[i].Name == name {
			return i
		}
	}
	return -1
}

// Method returns the function of a method of a class, or -1.
func (p *Program) Method(class, method string) (MethodRef, bool) {
	c := p.Class(class)
	if c < 0 {
		return MethodRef{}, false
	}
	for _, m := range p.Classes[c].Methods {
		if m.Name == method {
			return m, true
		}
	}
	return MethodRef{}, false
}
