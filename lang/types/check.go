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
	"slices"
	"sort"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// Unit is a source file to check.
type Unit struct {
	File string
	Name string
	Kind model.SourceKind
	Text string
	AST  *syntax.File
}

// IncludeSource finds includes by name. The package itself is the only
// source in this language part; later parts add more.
type IncludeSource interface {
	Include(name string) (*Unit, bool)
}

// Config is the input of Check.
type Config struct {
	Package  model.PackageID
	Schema   *model.Schema // valid schema with the package and the packages it requires
	Units    []*Unit       // classes and interfaces of the package and the kernel
	Includes IncludeSource
}

// Program is a checked program.
type Program struct {
	Package  model.PackageID
	Classes  []*Class // ordered by name
	Info     *Info
	Includes map[string]bool // includes that were inserted somewhere
}

// Class returns the class with the given name, or nil.
func (p *Program) Class(name string) *Class {
	for _, c := range p.Classes {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Info describes the checked program for the compiler.
type Info struct {
	Types      map[syntax.Expr]Type       // type of every expression, after null checks
	Conv       map[syntax.Expr]Type       // implicit conversion of an expression to this type
	Uses       map[*syntax.Ident]any      // *Local, *Attribute, *Constant or *Method
	Globals    map[*syntax.GlobalExpr]any // *Class, *Struct, *Enum, *Array or *Named
	Selects    map[*syntax.SelectExpr]*Selection
	Calls      map[*syntax.CallExpr]*Call
	TypeExprs  map[*syntax.Type]Type
	Funcs      map[*syntax.FuncExpr]*FuncInfo
	Vars       map[syntax.Stmt][]*Local // locals declared by var, for and catch (in source order)
	Catches    map[*syntax.CatchClause]*Local
	Raises     map[*syntax.RaiseStmt]*Class
	Expansions map[*syntax.IncludeStmt]*Expansion
}

// Expansion is a statements include inserted at an include statement.
type Expansion struct {
	Name  string
	File  string
	Chain []IncludeSite // innermost first, starting with this insertion
	Stmts []syntax.Stmt
}

// SelectionKind tells what x.NAME selects.
type SelectionKind int

// Selection kinds.
const (
	SelectField     SelectionKind = iota // field of a struct value
	SelectAttribute                      // instance attribute of an object
	SelectStatic                         // static attribute
	SelectConstant
	SelectEnum   // constant of an enum
	SelectMethod // method, only as callee
	SelectBuiltin
)

// Selection describes x.NAME.
type Selection struct {
	Kind      SelectionKind
	Field     int // SelectField
	Attribute *Attribute
	Constant  *Constant
	Enum      *Enum
	EnumIndex int
	Method    *Method
}

// CallKind tells how a call is made.
type CallKind int

// Call kinds.
const (
	CallStatic    CallKind = iota // a static method, or an instance method that cannot be overridden
	CallVirtual                   // an open or overriding instance method
	CallInterface                 // a method of an interface
	CallSuper                     // a base method through super, without dynamic dispatch
	CallCtor                      // super(...) in a constructor
	CallClosure                   // a value of a function type
	CallBuiltin                   // a built-in method of a type
)

// Call describes a call.
type Call struct {
	Kind     CallKind
	Method   *Method
	Builtin  *Builtin
	Receiver syntax.Expr // receiver of instance and built-in methods; nil for me
	Args     []syntax.Expr
	Named    []string // names of named arguments, aligned with Args
}

type checker struct {
	cfg      Config
	own      string // namespace of the package
	classes  map[string]*Class
	schema   *schemaTypes
	info     *Info
	diags    []Diagnostic
	loc      Site // file and include chain of the code being checked
	included map[string]bool
}

// Check checks a program. Diagnostics with severity Error make the
// program unusable.
func Check(cfg Config) (*Program, []Diagnostic) {
	c := &checker{cfg: cfg, own: model.Namespace(cfg.Package), classes: map[string]*Class{}, included: map[string]bool{}}
	c.info = &Info{Types: map[syntax.Expr]Type{}, Conv: map[syntax.Expr]Type{}, Uses: map[*syntax.Ident]any{},
		Globals: map[*syntax.GlobalExpr]any{}, Selects: map[*syntax.SelectExpr]*Selection{}, Calls: map[*syntax.CallExpr]*Call{},
		TypeExprs: map[*syntax.Type]Type{}, Funcs: map[*syntax.FuncExpr]*FuncInfo{}, Vars: map[syntax.Stmt][]*Local{},
		Catches: map[*syntax.CatchClause]*Local{}, Raises: map[*syntax.RaiseStmt]*Class{}, Expansions: map[*syntax.IncludeStmt]*Expansion{}}
	c.schema = newSchemaTypes(c, cfg.Schema, cfg.Package)
	c.declareClasses()
	c.hierarchy()
	for _, k := range c.sorted() {
		c.members(k)
	}
	for _, k := range c.sorted() {
		c.classRules(k)
	}
	for _, k := range c.sorted() {
		c.bodies(k)
	}
	p := &Program{Package: cfg.Package, Classes: c.sorted(), Info: c.info, Includes: c.included}
	sort.SliceStable(c.diags, func(i, j int) bool {
		a, b := c.diags[i], c.diags[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return p, c.diags
}

func (c *checker) sorted() []*Class {
	out := make([]*Class, 0, len(c.classes))
	for _, k := range c.classes {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// errorf reports an error at pos in the current file.
func (c *checker) errorf(pos syntax.Pos, code, format string, args ...any) {
	c.report(Error, c.loc.File, pos, slices.Clone(c.loc.Chain), code, format, args...)
}

func (c *checker) report(sev Severity, file string, pos syntax.Pos, chain []IncludeSite, code, format string, args ...any) {
	level := "error"
	if sev == Note {
		level = "note"
	}
	d := Diagnostic{Severity: sev, Level: level, File: file, Line: pos.Line, Column: pos.Col, Code: code, Message: fmt.Sprintf(format, args...), Chain: chain}
	for _, o := range c.diags {
		if o.File == d.File && o.Line == d.Line && o.Column == d.Column && o.Message == d.Message && slices.Equal(o.Chain, d.Chain) {
			return
		}
	}
	c.diags = append(c.diags, d)
}

func (c *checker) site(pos syntax.Pos) Site {
	return Site{File: c.loc.File, Pos: pos, Chain: c.loc.Chain}
}

// at switches the current location for the duration of fn.
func (c *checker) at(s Site, fn func()) {
	saved := c.loc
	c.loc = Site{File: s.File, Chain: s.Chain}
	fn()
	c.loc = saved
}

// checkGlobalName requires upper-case letters in the local part of a
// global name.
func (c *checker) checkGlobalName(n syntax.Name) {
	if _, local, ok := model.SplitName(n.Name); ok && syntax.CaseOf(local) != syntax.UpperCase {
		c.errorf(n.Pos, CodeCase, "global names are written in upper case: %s", strings.ToUpper(n.Name))
	}
}

func (c *checker) checkMemberName(id syntax.Ident, what string) {
	if syntax.CaseOf(id.Name) != syntax.UpperCase {
		c.errorf(id.Pos, CodeCase, "%s names are written in upper case: %s", what, strings.ToUpper(id.Name))
	}
}

func (c *checker) checkLocalName(id syntax.Ident, what string) {
	if syntax.CaseOf(id.Name) != syntax.LowerCase {
		c.errorf(id.Pos, CodeCase, "%s names are written in lower case: %s", what, strings.ToLower(id.Name))
	}
}

// declareClasses creates the classes and interfaces.
func (c *checker) declareClasses() {
	for _, u := range c.cfg.Units {
		f := u.AST
		c.loc = Site{File: u.File}
		var k *Class
		switch {
		case f.Class != nil:
			d := f.Class
			k = &Class{Name: d.Name.Name, Kind: KindClass, Visibility: d.Visibility, Open: d.Open, Test: d.Test, File: u.File, Class: d}
			c.checkGlobalName(d.Name)
			if d.Visibility == syntax.Private {
				c.errorf(d.Pos, CodeVisibility, "a class is public or package, not private")
			}
		case f.Interface != nil:
			d := f.Interface
			k = &Class{Name: d.Name.Name, Kind: KindInterface, Visibility: d.Visibility, File: u.File, Interface: d}
			c.checkGlobalName(d.Name)
			if d.Visibility == syntax.Private {
				c.errorf(d.Pos, CodeVisibility, "an interface is public or package, not private")
			}
		default:
			continue
		}
		k.Kernel = strings.HasPrefix(k.Name, model.KernelNamespace)
		k.members = map[string]any{}
		if _, dup := c.classes[k.Name]; dup {
			c.errorf(f.Name().Pos, CodeDuplicate, "%s is defined twice", k.Name)
			continue
		}
		if c.schema.exists(k.Name) {
			c.errorf(f.Name().Pos, CodeDuplicate, "%s is already the name of an object of the schema", k.Name)
		}
		c.classes[k.Name] = k
	}
	c.loc = Site{}
}

// classRef resolves a class or interface name used in a declaration.
func (c *checker) classRef(n syntax.Name) *Class {
	if k, ok := c.classes[n.Name]; ok {
		return k
	}
	if !c.visibleNamespace(n) {
		return nil
	}
	c.errorf(n.Pos, CodeUnknown, "unknown class or interface %s", n.Name)
	return nil
}

// visibleNamespace reports names of the package or the kernel and reports
// names of other packages, whose objects come with a later language part.
func (c *checker) visibleNamespace(n syntax.Name) bool {
	if strings.HasPrefix(n.Name, c.own) || strings.HasPrefix(n.Name, model.KernelNamespace) {
		return true
	}
	c.errorf(n.Pos, CodeNotYet, "%s belongs to another package; objects of other packages are not available in code yet", n.Name)
	return false
}

func (c *checker) hierarchy() {
	for _, k := range c.sorted() {
		c.loc = Site{File: k.File}
		if d := k.Class; d != nil {
			if d.Extends != nil {
				if b := c.classRef(*d.Extends); b != nil {
					switch {
					case b.IsInterface():
						c.errorf(d.Extends.Pos, CodeInherit, "%s is an interface; use implements", b.Name)
					case !b.Open:
						c.errorf(d.Extends.Pos, CodeInherit, "%s is not open and cannot be extended", b.Name)
					case b.Name == RuntimeErrorClass && !k.Kernel:
						c.errorf(d.Extends.Pos, CodeInherit, "only the kernel defines runtime errors")
					default:
						k.Base = b
					}
				}
			}
			for _, n := range d.Implements {
				if i := c.classRef(n); i != nil {
					if !i.IsInterface() {
						c.errorf(n.Pos, CodeInherit, "%s is a class; use extends", i.Name)
						continue
					}
					k.Interfaces = append(k.Interfaces, i)
				}
			}
		}
		if d := k.Interface; d != nil {
			for _, n := range d.Extends {
				if i := c.classRef(n); i != nil {
					if !i.IsInterface() {
						c.errorf(n.Pos, CodeInherit, "an interface can only extend interfaces, %s is a class", i.Name)
						continue
					}
					k.Interfaces = append(k.Interfaces, i)
				}
			}
		}
	}
	// Cycles: a class must not extend itself.
	for _, k := range c.sorted() {
		var chain []string
		seen := map[*Class]bool{}
		var visit func(x *Class) bool
		visit = func(x *Class) bool {
			if x == k && len(chain) > 0 {
				return true
			}
			if seen[x] {
				return false
			}
			seen[x] = true
			chain = append(chain, x.Name)
			var supers []*Class
			if x.Base != nil {
				supers = append(supers, x.Base)
			}
			supers = append(supers, x.Interfaces...)
			for _, s := range supers {
				if visit(s) {
					return true
				}
			}
			chain = chain[:len(chain)-1]
			return false
		}
		if visit(k) {
			c.report(Error, k.File, k.pos(), nil, CodeCycle, "%s extends itself: %s -> %s", k.Name, strings.Join(chain, " -> "), k.Name)
			k.Base, k.Interfaces = nil, nil
		}
	}
	c.loc = Site{}
}

func (k *Class) pos() syntax.Pos {
	if k.Class != nil {
		return k.Class.Name.Pos
	}
	if k.Interface != nil {
		return k.Interface.Name.Pos
	}
	return syntax.Pos{}
}

// resolveType resolves a type expression.
func (c *checker) resolveType(t *syntax.Type) Type {
	if t == nil {
		return TypVoid
	}
	out := c.typeOf(t)
	c.info.TypeExprs[t] = out
	return out
}

func (c *checker) typeOf(t *syntax.Type) Type {
	var out Type
	switch t.Kind {
	case syntax.BuiltinType:
		switch t.Name {
		case "STRING":
			out = TypString
		case "INT4":
			out = TypInt4
		case "INT8":
			out = TypInt8
		case "DATE":
			out = TypDate
		case "TIMESTAMP":
			out = TypTimestamp
		case "BOOL":
			out = TypBool
		case "UUID":
			out = TypUUID
		case "BYTES":
			out = TypBytes
		case "DEC":
			if t.Precision < 1 || t.Precision > MaxPrecision || t.Scale > t.Precision {
				c.errorf(t.Pos, CodeType, "DEC(%d,%d) is not valid: precision 1 to %d, scale at most the precision", t.Precision, t.Scale, MaxPrecision)
				return TypInvalid
			}
			out = DecType(t.Precision, t.Scale)
		}
	case syntax.NamedType:
		n := syntax.Name{Pos: t.Pos, Name: t.Name}
		out = c.globalType(n)
		if out == nil {
			return TypInvalid
		}
	case syntax.ArrayType:
		return &Array{Elem: c.resolveType(t.Elem)}
	case syntax.MapType:
		key := c.resolveType(t.Key)
		if !mapKey(key) {
			c.errorf(t.Key.Pos, CodeType, "%s cannot be a map key; keys are built-in types except BYTES, or enums, and never optional", key)
		}
		return &Map{Key: key, Elem: c.resolveType(t.Elem)}
	case syntax.FuncType:
		f := &Func{Result: TypVoid}
		for _, p := range t.Params {
			f.Params = append(f.Params, c.resolveType(p))
		}
		if t.Returns != nil {
			f.Result = c.resolveType(t.Returns)
		}
		f.Raises = c.raisesList(t.Raises)
		return f
	}
	if t.Optional {
		return Opt(out)
	}
	return out
}

func mapKey(t Type) bool {
	switch x := t.(type) {
	case *Enum:
		return true
	case *Named:
		return x.Base.Kind != Bytes
	case *Basic:
		return x.Kind != Bytes && x.Kind != Invalid && x.Kind != Null && x.Kind != Void
	}
	return false
}

// globalType resolves a global name used as type.
func (c *checker) globalType(n syntax.Name) Type {
	if k, ok := c.classes[n.Name]; ok {
		return k
	}
	if !c.visibleNamespace(n) {
		return nil
	}
	if t := c.schema.lookup(n.Name, n.Pos); t != nil {
		return t
	}
	c.errorf(n.Pos, CodeUnknown, "unknown type %s", n.Name)
	return nil
}

func (c *checker) raisesList(ns []syntax.Name) []*Class {
	var out []*Class
	for _, n := range ns {
		k := c.classRef(n)
		if k == nil {
			continue
		}
		if !k.IsException() {
			c.errorf(n.Pos, CodeException, "%s is not an exception class", k.Name)
			continue
		}
		if k.IsRuntimeError() {
			c.errorf(n.Pos, CodeException, "%s is a runtime error and needs no declaration", k.Name)
			continue
		}
		out = append(out, k)
	}
	return out
}

// members collects the members of a class, inserting members includes.
func (c *checker) members(k *Class) {
	c.loc = Site{File: k.File}
	defer func() { c.loc = Site{} }()
	if d := k.Interface; d != nil {
		for _, s := range d.Methods {
			m := &Method{Name: s.Name.Name, Owner: k, Visibility: syntax.Public, Site: c.site(s.Pos)}
			c.checkMemberName(s.Name, "method")
			m.Sig, m.Params = c.signature(s.Params, s.Returns, s.Raises)
			c.addMember(k, m.Name, m, s.Name.Pos)
			k.Methods = append(k.Methods, m)
		}
		return
	}
	c.classMembers(k, k.Class.Members, nil)
}

func (c *checker) classMembers(k *Class, ms []syntax.Member, stack []string) {
	for _, m := range ms {
		switch m := m.(type) {
		case *syntax.IncludeMember:
			c.memberInclude(k, m, stack)
		case *syntax.ConstantDecl:
			c.checkMemberName(m.Name, "constant")
			ct := &Constant{Name: m.Name.Name, Type: c.resolveType(m.Type), Visibility: m.Visibility, Owner: k, Value: m.Value, Site: c.site(m.Pos)}
			c.addMember(k, ct.Name, ct, m.Name.Pos)
			k.Constants = append(k.Constants, ct)
		case *syntax.AttributeDecl:
			c.checkMemberName(m.Name, "attribute")
			a := &Attribute{Name: m.Name.Name, Type: c.resolveType(m.Type), Static: m.Static, Visibility: m.Visibility, Owner: k, Init: m.Value, Site: c.site(m.Pos)}
			c.addMember(k, a.Name, a, m.Name.Pos)
			k.Attributes = append(k.Attributes, a)
		case *syntax.MethodDecl:
			mt := &Method{Owner: k, Static: m.Static, Open: m.Open, Override: m.Override, Test: m.Test, Ctor: m.Constructor,
				Native: m.Native, Visibility: m.Visibility, Decl: m, Site: c.site(m.Pos)}
			mt.Sig, mt.Params = c.signature(m.Sig.Params, m.Sig.Returns, m.Sig.Raises)
			if m.Constructor {
				if k.Ctor != nil {
					c.errorf(m.Pos, CodeDuplicate, "%s has more than one constructor", k.Name)
				}
				if m.Static || m.Open || m.Override || m.Test {
					c.errorf(m.Pos, CodeStatic, "a constructor is not static, open, override or test")
				}
				k.Ctor = mt
				continue
			}
			mt.Name = m.Sig.Name.Name
			c.checkMemberName(m.Sig.Name, "method")
			c.addMember(k, mt.Name, mt, m.Sig.Name.Pos)
			k.Methods = append(k.Methods, mt)
		}
	}
}

func (c *checker) memberInclude(k *Class, m *syntax.IncludeMember, stack []string) {
	u, ok := c.findInclude(m.Name, m.IfFound, stack)
	if !ok {
		return
	}
	f, errs := syntax.Parse(u.File, u.Text)
	if len(errs) > 0 {
		return // reported for the include file itself
	}
	if f.Include == nil || f.Include.Kind != syntax.MembersInclude {
		c.errorf(m.Name.Pos, CodeInclude, "%s is a statements include and cannot be included among members", m.Name.Name)
		return
	}
	site := IncludeSite{Include: m.Name.Name, File: c.loc.File, Line: m.Name.Pos.Line}
	saved := c.loc
	c.loc = Site{File: u.File, Chain: append([]IncludeSite{site}, saved.Chain...)}
	c.classMembers(k, f.Include.Members, append(stack, m.Name.Name))
	c.loc = saved
}

// findInclude resolves an include and checks for cycles. A missing include
// with "if found" is noted.
func (c *checker) findInclude(n syntax.Name, ifFound bool, stack []string) (*Unit, bool) {
	c.checkGlobalName(n)
	if !c.visibleNamespace(n) {
		return nil, false
	}
	if i := slices.Index(stack, n.Name); i >= 0 {
		c.errorf(n.Pos, CodeCycle, "include cycle: %s -> %s", strings.Join(stack[i:], " -> "), n.Name)
		return nil, false
	}
	u, ok := c.cfg.Includes.Include(n.Name)
	if !ok {
		if ifFound {
			c.report(Note, c.loc.File, n.Pos, slices.Clone(c.loc.Chain), CodeInclude, "include %s does not exist and is skipped", n.Name)
		} else {
			c.errorf(n.Pos, CodeInclude, "include %s does not exist", n.Name)
		}
		return nil, false
	}
	c.included[n.Name] = true
	return u, true
}

func (c *checker) signature(params []*syntax.Param, returns *syntax.Type, raises []syntax.Name) (*Func, []string) {
	f := &Func{Result: TypVoid}
	var names []string
	for _, p := range params {
		c.checkLocalName(p.Name, "parameter")
		if slices.Contains(names, p.Name.Name) {
			c.errorf(p.Name.Pos, CodeDuplicate, "parameter %s is declared twice", p.Name.Name)
		}
		names = append(names, p.Name.Name)
		f.Params = append(f.Params, c.resolveType(p.Type))
	}
	if returns != nil {
		f.Result = c.resolveType(returns)
	}
	f.Raises = c.raisesList(raises)
	return f, names
}

func (c *checker) addMember(k *Class, name string, m any, pos syntax.Pos) {
	if _, dup := k.members[name]; dup {
		c.errorf(pos, CodeDuplicate, "%s is declared twice in %s", name, k.Name)
		return
	}
	k.members[name] = m
}

// classRules checks inheritance, overriding, interfaces and the rules for
// exception and test classes.
func (c *checker) classRules(k *Class) {
	c.loc = Site{File: k.File}
	defer func() { c.loc = Site{} }()
	if k.IsInterface() {
		return
	}
	for name, m := range k.members {
		if k.Base == nil {
			continue
		}
		inherited := k.Base.Member(name)
		if inherited == nil {
			if mt, ok := m.(*Method); ok && mt.Override {
				c.at(mt.Site, func() { c.errorf(mt.Site.Pos, CodeOverride, "%s overrides nothing; remove override", mt) })
			}
			continue
		}
		mt, isMethod := m.(*Method)
		base, baseMethod := inherited.(*Method)
		if !isMethod || !baseMethod {
			c.at(siteOf(m), func() {
				c.errorf(siteOf(m).Pos, CodeDuplicate, "%s hides the member %s of %s", name, name, ownerOf(inherited).Name)
			})
			continue
		}
		c.at(mt.Site, func() { c.override(mt, base) })
	}
	for _, mt := range k.Methods {
		if k.Base == nil || k.Base.Member(mt.Name) == nil {
			if mt.Override {
				c.at(mt.Site, func() { c.errorf(mt.Site.Pos, CodeOverride, "%s overrides nothing; remove override", mt) })
			}
		}
		if (mt.Open || mt.Override) && mt.Static {
			c.at(mt.Site, func() { c.errorf(mt.Site.Pos, CodeStatic, "static methods cannot be open or override") })
		}
		if mt.Open && !k.Open {
			c.at(mt.Site, func() { c.errorf(mt.Site.Pos, CodeOverride, "%s is open, but %s is not", mt, k.Name) })
		}
		if mt.Test && !k.Test {
			c.at(mt.Site, func() { c.errorf(mt.Site.Pos, CodeTest, "test methods exist only in test classes") })
		}
		if mt.Test && (mt.Static || len(mt.Sig.Params) > 0 || mt.Sig.Result != TypVoid) {
			c.at(mt.Site, func() {
				c.errorf(mt.Site.Pos, CodeTest, "a test method has no parameters, no result and is not static")
			})
		}
	}
	// Every method of every interface, including the interfaces they
	// extend, needs a public instance method with the same signature.
	for _, i := range allInterfaces(k) {
		for _, im := range i.Methods {
			found, _ := k.Member(im.Name).(*Method)
			switch {
			case found == nil || found.Static:
				c.errorf(k.pos(), CodeInterface, "%s does not implement %s of %s", k.Name, im.Name, i.Name)
			case found.Visibility != syntax.Public:
				c.at(found.Site, func() { c.errorf(found.Site.Pos, CodeInterface, "%s implements %s and must be public", found, im) })
			default:
				c.at(found.Site, func() {
					if msg := sameSignature(found.Sig, im.Sig); msg != "" {
						c.errorf(found.Site.Pos, CodeInterface, "%s does not match %s: %s", found, im, msg)
					}
				})
			}
		}
	}
	if k.Test {
		if k.Base != nil || len(k.Interfaces) > 0 || k.Open {
			c.errorf(k.pos(), CodeTest, "test classes do not extend, implement or open")
		}
	}
	// A subclass needs a constructor when the base constructor takes
	// parameters.
	if b := k.baseCtor(); b != nil && len(b.Sig.Params) > 0 && k.Ctor == nil {
		c.errorf(k.pos(), CodeInherit, "%s needs a constructor that calls super with the arguments for %s", k.Name, b)
	}
}

// baseCtor returns the nearest constructor of a base class.
func (k *Class) baseCtor() *Method {
	for b := k.Base; b != nil; b = b.Base {
		if b.Ctor != nil {
			return b.Ctor
		}
	}
	return nil
}

func allInterfaces(k *Class) []*Class {
	var out []*Class
	var add func(i *Class)
	add = func(i *Class) {
		if slices.Contains(out, i) {
			return
		}
		out = append(out, i)
		for _, s := range i.Interfaces {
			add(s)
		}
	}
	for x := k; x != nil; x = x.Base {
		for _, i := range x.Interfaces {
			add(i)
		}
	}
	return out
}

func (c *checker) override(m, base *Method) {
	switch {
	case !m.Override:
		if base.Open || base.Override {
			c.errorf(m.Site.Pos, CodeOverride, "%s overrides %s and needs override", m, base)
		} else {
			c.errorf(m.Site.Pos, CodeOverride, "%s cannot override %s, which is not open", m, base)
		}
		return
	case !base.Open && !base.Override:
		c.errorf(m.Site.Pos, CodeOverride, "%s is not open and cannot be overridden", base)
		return
	case m.Visibility != base.Visibility:
		c.errorf(m.Site.Pos, CodeOverride, "%s must keep the visibility %s of %s", m, base.Visibility, base)
	}
	if msg := sameSignature(m.Sig, base.Sig); msg != "" {
		c.errorf(m.Site.Pos, CodeOverride, "%s does not match %s: %s", m, base, msg)
	}
	m.Overrides = base
}

// sameSignature compares an implementation with the method it overrides
// or implements. It may raise fewer exceptions, never more.
func sameSignature(impl, decl *Func) string {
	if len(impl.Params) != len(decl.Params) {
		return fmt.Sprintf("%d parameters instead of %d", len(impl.Params), len(decl.Params))
	}
	for i := range impl.Params {
		if !Identical(impl.Params[i], decl.Params[i]) {
			return fmt.Sprintf("parameter %d is %s instead of %s", i+1, impl.Params[i], decl.Params[i])
		}
	}
	if !Identical(impl.Result, decl.Result) {
		return fmt.Sprintf("result %s instead of %s", impl.Result, decl.Result)
	}
	for _, r := range impl.Raises {
		if !slices.ContainsFunc(decl.Raises, func(d *Class) bool { return r.Extends(d) }) {
			return "raises " + r.Name + ", which " + "the overridden method does not declare"
		}
	}
	return ""
}

func siteOf(m any) Site {
	switch m := m.(type) {
	case *Attribute:
		return m.Site
	case *Constant:
		return m.Site
	case *Method:
		return m.Site
	}
	return Site{}
}

func ownerOf(m any) *Class {
	switch m := m.(type) {
	case *Attribute:
		return m.Owner
	case *Constant:
		return m.Owner
	case *Method:
		return m.Owner
	}
	return nil
}
