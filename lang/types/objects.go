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
	"fmt"
	"strings"

	"github.com/tim-riep/go-runtime/model"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// Kernel classes every program contains.
const (
	ExceptionClass    = "/RT/EXCEPTION"     // root of checked exceptions
	RuntimeErrorClass = "/RT/RUNTIME_ERROR" // root of the runtime errors of the VM
)

// RuntimeErrors are the runtime errors the VM raises.
var RuntimeErrors = []string{"/RT/OVERFLOW", "/RT/DIVISION_BY_ZERO", "/RT/INDEX_ERROR", "/RT/KEY_ERROR",
	"/RT/DUPLICATE_KEY", "/RT/CONVERSION_ERROR", "/RT/VALUE_ERROR", "/RT/ASSERTION_FAILED"}

// Site is where something is written: a file, a position and the chain of
// includes through which the file was inserted, innermost first.
type Site struct {
	File  string
	Pos   syntax.Pos
	Chain []IncludeSite
}

// IncludeSite is the place an include was inserted.
type IncludeSite struct {
	Include string // the include
	File    string // the including file
	Line    int
}

func (s Site) String() string {
	out := fmt.Sprintf("%s:%d:%d", s.File, s.Pos.Line, s.Pos.Col)
	for _, c := range s.Chain {
		out += fmt.Sprintf(" (%s included from %s:%d)", c.Include, c.File, c.Line)
	}
	return out
}

// ClassKind tells classes and interfaces apart.
type ClassKind int

// Class kinds.
const (
	KindClass ClassKind = iota
	KindInterface
)

// Class is a class or an interface.
type Class struct {
	Name       string
	Kind       ClassKind
	Visibility syntax.Visibility
	Open       bool
	Test       bool
	Kernel     bool // defined by the kernel
	Native     bool // objects carry a value of the kernel; executed by kernel
	Package    model.PackageID
	External   bool // a class of another package, known by its declaration
	Base       *Class
	Interfaces []*Class
	Attributes []*Attribute // own attributes in order
	Constants  []*Constant
	Methods    []*Method // own methods, without the constructor
	Ctor       *Method   // nil: the implicit constructor without parameters
	File       string
	Class      *syntax.ClassDecl
	Interface  *syntax.InterfaceDecl
	members    map[string]any
}

func (c *Class) String() string { return c.Name }

// IsInterface reports interfaces.
func (c *Class) IsInterface() bool { return c.Kind == KindInterface }

// Member returns the own or inherited member with the given name:
// *Attribute, *Constant or *Method.
func (c *Class) Member(name string) any {
	for k := c; k != nil; k = k.Base {
		if m, ok := k.members[name]; ok {
			return m
		}
	}
	if c.IsInterface() {
		for _, i := range c.Interfaces {
			if m := i.Member(name); m != nil {
				return m
			}
		}
	}
	return nil
}

// Extends reports whether c is d, a subclass of d or implements d.
func (c *Class) Extends(d *Class) bool {
	if c == nil || d == nil {
		return false
	}
	if c == d {
		return true
	}
	if c.Base.Extends(d) {
		return true
	}
	for _, i := range c.Interfaces {
		if i.Extends(d) {
			return true
		}
	}
	return false
}

// root returns the topmost base class.
func (c *Class) root() *Class {
	for c.Base != nil {
		c = c.Base
	}
	return c
}

// IsException reports exception classes: /RT/EXCEPTION and its
// subclasses, and the runtime errors.
func (c *Class) IsException() bool {
	r := c.root().Name
	return r == ExceptionClass || r == RuntimeErrorClass
}

// IsRuntimeError reports the unchecked runtime errors of the VM.
func (c *Class) IsRuntimeError() bool { return c.root().Name == RuntimeErrorClass }

// AllMethods returns the own and inherited instance and static methods,
// overriding ones replacing the overridden.
func (c *Class) AllMethods() []*Method {
	var out []*Method
	if c.Base != nil {
		out = c.Base.AllMethods()
	}
	for _, m := range c.Methods {
		replaced := false
		for i, o := range out {
			if o.Name == m.Name {
				out[i] = m
				replaced = true
			}
		}
		if !replaced {
			out = append(out, m)
		}
	}
	return out
}

// AllAttributes returns the instance attributes of the base classes and
// then the own ones.
func (c *Class) AllAttributes() []*Attribute {
	var out []*Attribute
	if c.Base != nil {
		out = c.Base.AllAttributes()
	}
	for _, a := range c.Attributes {
		if !a.Static {
			out = append(out, a)
		}
	}
	return out
}

// Attribute is an attribute of a class.
type Attribute struct {
	Name       string
	Type       Type
	Static     bool
	Visibility syntax.Visibility
	Owner      *Class
	Init       syntax.Expr // nil without initial value
	Site       Site
}

// Constant is a constant of a class. Its value is a constant expression.
type Constant struct {
	Name       string
	Type       Type
	Visibility syntax.Visibility
	Owner      *Class
	Value      syntax.Expr
	Site       Site
}

// Method is a method or a constructor.
type Method struct {
	Name       string // empty for constructors
	Owner      *Class
	Sig        *Func
	Params     []string
	Static     bool
	Open       bool
	Override   bool
	Test       bool
	Ctor       bool
	Native     bool
	Visibility syntax.Visibility
	Decl       *syntax.MethodDecl // nil for interface methods
	Site       Site
	Overrides  *Method // the base method this one overrides
	Func       *FuncInfo
	// Capabilities are the capabilities a method of a kernel package uses
	// directly or through the methods of kernel packages it calls.
	Capabilities []string
}

// Abstract reports interface methods.
func (m *Method) Abstract() bool { return m.Owner.IsInterface() }

func (m *Method) String() string {
	if m.Ctor {
		return "constructor of " + m.Owner.Name
	}
	return m.Owner.Name + "." + m.Name
}

// Local is a local variable or parameter.
type Local struct {
	Name     string
	Type     Type
	Index    int // slot in its function
	Param    bool
	Captured bool // used by a closure, so it lives in a cell
	Func     *FuncInfo
}

// FuncInfo describes a method body or a function literal.
type FuncInfo struct {
	Locals   []*Local
	Captures []*Local // locals of enclosing functions used by a closure
	Parent   *FuncInfo
	Type     *Func
}

func (f *FuncInfo) newLocal(name string, t Type, param bool) *Local {
	l := &Local{Name: name, Type: t, Index: len(f.Locals), Param: param, Func: f}
	f.Locals = append(f.Locals, l)
	return l
}

// CaptureIndex returns the position of a captured local, adding it.
func (f *FuncInfo) CaptureIndex(l *Local) int {
	for i, c := range f.Captures {
		if c == l {
			return i
		}
	}
	f.Captures = append(f.Captures, l)
	return len(f.Captures) - 1
}

// Severity of a diagnostic.
type Severity int

// Severities.
const (
	Error Severity = iota
	Note
)

// Diagnostic is an error or a note of the checker.
type Diagnostic struct {
	Severity Severity      `json:"-"`
	Level    string        `json:"severity"`
	File     string        `json:"file"`
	Line     int           `json:"line"`
	Column   int           `json:"column"`
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	Chain    []IncludeSite `json:"includedFrom,omitempty"`
}

func (d Diagnostic) String() string {
	var b strings.Builder
	if d.Line > 0 {
		fmt.Fprintf(&b, "%s:%d:%d: ", d.File, d.Line, d.Column)
	} else if d.File != "" {
		fmt.Fprintf(&b, "%s: ", d.File)
	}
	if d.Severity == Note {
		b.WriteString("note: ")
	}
	fmt.Fprintf(&b, "%s: %s", d.Code, d.Message)
	for _, c := range d.Chain {
		fmt.Fprintf(&b, "\n    %s included from %s:%d", c.Include, c.File, c.Line)
	}
	return b.String()
}

// Diagnostic codes.
const (
	CodeSyntax        = "syntax"
	CodeCanonical     = "canonical"
	CodeCase          = "case"
	CodeNamespace     = "namespace"
	CodeUnknown       = "unknown"
	CodeDuplicate     = "duplicate"
	CodeType          = "type"
	CodeNullCheck     = "null-check"
	CodeEmpty         = "empty-on-optional"
	CodeException     = "unhandled-exception"
	CodeOverride      = "override"
	CodeInterface     = "interface"
	CodeInherit       = "inheritance"
	CodeVisibility    = "visibility"
	CodeStatic        = "static"
	CodeReturn        = "return"
	CodeInclude       = "include"
	CodeNotYet        = "not-yet"
	CodeNative        = "native"
	CodeCapability    = "capability"
	CodeHandler       = "handler"
	CodeAsset         = "asset"
	CodeAuthorization = "authorization"
	CodeConstant      = "constant"
	CodeTest          = "test"
	CodeStatement     = "statement"
	CodeEnum          = "enum"
	CodeCycle         = "cycle"
)
