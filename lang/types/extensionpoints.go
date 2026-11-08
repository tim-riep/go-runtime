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
	"strings"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// Methods that every value of an extension point has besides those of its
// interface.
const (
	IsImplementedMethod = "IS_IMPLEMENTED"
	CountMethod         = "COUNT"
)

// ExtensionExpr describes extension point NAME filters ...
type ExtensionExpr struct {
	Point *ExtensionPoint
	// Fields are the filter values in source order; they are evaluated in
	// this order.
	Fields []ExtensionField
}

// ExtensionField is the value of a filter field.
type ExtensionField struct {
	Name  string
	Type  Type // the type of the field
	Value syntax.Expr
}

// ChainParameter returns the parameter of a method of an extension point
// used multiple through which the implementations are chained: the only
// parameter whose type is identical to the result. Methods without result
// run one implementation after the other without chain.
func ChainParameter(m *Method) (int, bool) {
	if m.Sig.Result == TypVoid {
		return -1, false
	}
	found := -1
	for i, p := range m.Sig.Params {
		if Identical(p, m.Sig.Result) {
			if found >= 0 {
				return -1, false
			}
			found = i
		}
	}
	return found, found >= 0
}

// InterfaceMethods returns the methods of an interface and of the
// interfaces it extends, each name once.
func InterfaceMethods(iface *Class) []*Method {
	var out []*Method
	seen := map[string]bool{}
	var add func(i *Class)
	add = func(i *Class) {
		for _, m := range i.Methods {
			if !seen[m.Name] {
				seen[m.Name] = true
				out = append(out, m)
			}
		}
		for _, s := range i.Interfaces {
			add(s)
		}
	}
	add(iface)
	return out
}

// extensionPointType returns the type of the value of an extension point
// that the code may use, or nil.
func (c *checker) extensionPointType(n syntax.Name) *ExtensionPoint {
	c.checkGlobalName(n)
	var mp *model.ExtensionPoint
	if c.cfg.Schema != nil {
		mp = c.cfg.Schema.ExtensionPoints[n.Name]
	}
	if mp == nil {
		c.errorf(n.Pos, CodeExtensionPoint, "extension point %s does not exist", n.Name)
		return nil
	}
	if !c.visibleNamespace(n) {
		return nil
	}
	iface := c.classes[mp.Interface]
	if iface == nil || !iface.IsInterface() {
		// The definition of the extension point reports it.
		return nil
	}
	c.use(n.Pos, "extension-point", n.Name, "")
	if p := c.extensionTypes[n.Name]; p != nil {
		return p
	}
	if c.extensionTypes == nil {
		c.extensionTypes = map[string]*ExtensionPoint{}
	}
	p := &ExtensionPoint{Name: n.Name, Point: mp, Interface: iface}
	c.extensionTypes[n.Name] = p
	return p
}

// extensionPointExpr checks extension point NAME filters FIELD = value,
// ...: every filter field exactly once with a value of its type that is
// not null, as the fields of an authorization check, but without any.
func (c *checker) extensionPointExpr(f *fnCtx, sc *scope, x *syntax.ExtensionPointExpr, e env) Type {
	p := c.extensionPointType(x.Point)
	info := &ExtensionExpr{Point: p}
	c.info.ExtensionPoints[x] = info
	seen := map[string]bool{}
	for _, fv := range x.Filters {
		name := fv.Name.Name
		field := ExtensionField{Name: name, Type: TypInvalid, Value: fv.Value}
		if p != nil {
			def, ok := p.Point.Filter(name)
			switch {
			case !ok && len(p.Point.Filters) == 0:
				c.errorf(fv.Name.Pos, CodeExtensionPoint, "%s has no filter fields; write extension point %s without filters", p.Name, p.Name)
			case !ok:
				c.errorf(fv.Name.Pos, CodeUnknown, "%s has no filter field %s; its filter fields are %s", p.Name, name, filterNames(p.Point))
			case seen[name]:
				c.errorf(fv.Name.Pos, CodeDuplicate, "the filter field %s is given twice", name)
			default:
				c.use(fv.Name.Pos, "extension-point", p.Name, name)
				field.Type = c.filterType(def, fv.Name.Pos)
			}
			seen[name] = true
		}
		c.filterValue(f, sc, &field, fv, e)
		info.Fields = append(info.Fields, field)
	}
	if p != nil {
		for _, fd := range p.Point.Filters {
			if !seen[fd.Name] {
				c.errorf(x.Point.Pos, CodeExtensionPoint, "extension point %s needs a value for the filter field %s", p.Name, fd.Name)
			}
		}
	}
	// Selecting the implementations runs their constructors.
	e.call()
	if p == nil {
		return TypInvalid
	}
	return p
}

func filterNames(p *model.ExtensionPoint) string {
	var names []string
	for _, f := range p.Filters {
		names = append(names, f.Name)
	}
	return strings.Join(names, ", ")
}

// filterType returns the type of a filter field: that of its data element
// or domain.
func (c *checker) filterType(def model.ExtensionFilter, pos syntax.Pos) Type {
	name := def.Domain.Or("")
	if de, ok := def.DataElement.Get(); ok {
		name = de
	}
	if t := c.schema.lookup(name, pos); t != nil {
		return t
	}
	return TypInvalid
}

// filterValue checks the value of a filter field: of the type of the
// field and not null. Literals are checked against the length of the
// field now, other values when the expression runs.
func (c *checker) filterValue(f *fnCtx, sc *scope, field *ExtensionField, fv syntax.ParamValue, e env) {
	want := field.Type
	t := c.expr(f, sc, fv.Value, e, want)
	if t == TypNull || IsOptional(t) {
		c.notNull(fv.Value, t)
		return
	}
	if want == TypInvalid || !c.assign(fv.Value, t, want, "the value of "+fv.Name.Name) {
		return
	}
	if text, ok := literal(fv.Value); ok {
		if n, ok := want.(*Named); ok && n.Domain != nil && n.Domain.Length > 0 && utf8.RuneCountInString(text) > n.Domain.Length {
			c.errorf(fv.Value.Start(), CodeType, "%q is longer than the %d characters of %s", text, n.Domain.Length, fv.Name.Name)
		}
	}
}

// extensionCall checks a call of a method on the value of an extension
// point: a method of its interface, IS_IMPLEMENTED or COUNT.
func (c *checker) extensionCall(f *fnCtx, sc *scope, x *syntax.CallExpr, fun *syntax.SelectExpr, p *ExtensionPoint, e env) Type {
	name := fun.Name.Name
	switch name {
	case IsImplementedMethod, CountMethod:
		c.args(f, sc, x, &Func{}, e, nil)
		c.info.Calls[x] = &Call{Kind: CallExtension, Extension: p, Builtin: &Builtin{Name: name}, Receiver: fun.X, Args: argValues(x)}
		c.info.Selects[fun] = &Selection{Kind: SelectBuiltin}
		if name == CountMethod {
			return TypInt4
		}
		return TypBool
	}
	m, ok := p.Interface.Member(name).(*Method)
	if !ok {
		c.errorf(fun.Name.Pos, CodeUnknown, "%s has no method %s; it has the methods of %s, %s and %s", p.Name, name, p.Interface.Name, IsImplementedMethod, CountMethod)
		c.args(f, sc, x, &Func{}, e, nil)
		return TypInvalid
	}
	c.use(fun.Name.Pos, "extension-point", p.Name, name)
	c.useMember(fun.Name.Pos, m)
	c.args(f, sc, x, m.Sig, e, nil)
	for _, r := range m.Sig.Raises {
		c.raise(f, r, fun.Name.Pos)
	}
	c.info.Selects[fun] = &Selection{Kind: SelectMethod, Method: m}
	c.info.Calls[x] = &Call{Kind: CallExtension, Method: m, Extension: p, Receiver: fun.X, Args: argValues(x)}
	e.call()
	return m.Sig.Result
}

// extensionPointDefinitions checks the extension points of the package
// against its code: a public interface of the package whose methods suit
// the use, and a fallback class that implements it.
func (c *checker) extensionPointDefinitions() {
	if c.cfg.Schema == nil {
		return
	}
	for _, name := range c.cfg.Schema.ExtensionPointNames() {
		p := c.cfg.Schema.ExtensionPoints[name]
		if p.Owner != c.cfg.Package {
			continue
		}
		fail := func(pointer, format string, args ...any) {
			c.report(Error, p.File, syntax.Pos{}, nil, CodeExtensionPoint, "%s: "+format, append([]any{pointer}, args...)...)
		}
		iface := c.classes[p.Interface]
		switch {
		case iface == nil:
			fail("/interface", "interface %s does not exist", p.Interface)
			continue
		case !iface.IsInterface():
			fail("/interface", "%s is a class; an extension point needs an interface", p.Interface)
			continue
		case iface.Visibility != syntax.Public:
			fail("/interface", "%s is not public; the interface of an extension point is public", p.Interface)
		}
		c.useIn(p.File, "interface", p.Interface)
		for _, m := range InterfaceMethods(iface) {
			switch {
			case m.Name == IsImplementedMethod || m.Name == CountMethod:
				fail("/interface", "%s has the method %s, which every value of an extension point has already", p.Interface, m.Name)
			case p.Use == model.UseMultiple && m.Sig.Result != TypVoid:
				if _, ok := ChainParameter(m); !ok {
					fail("/use", "%s returns %s; in a multiple extension point a method returns nothing, or a value of the type of exactly one of its parameters, "+
						"which the implementations pass on one after the other. Return nothing and raise an exception instead, or use single", m, m.Sig.Result)
				}
			}
		}
		if fb, ok := p.Fallback.Get(); ok {
			if msg := c.implementingClass(fb, iface, false); msg != "" {
				fail("/fallback", "%s", msg)
			} else {
				c.useIn(p.File, "class", fb)
				c.noCommit(p, c.classes[fb], iface)
			}
		}
	}
}

// implementations checks the implementations of the package: a public
// class of the package that implements the interface of the extension
// point and has a public constructor without parameters.
func (c *checker) implementations() {
	if c.cfg.Schema == nil {
		return
	}
	for _, name := range c.cfg.Schema.ImplementationNames() {
		impl := c.cfg.Schema.Implementations[name]
		if impl.Owner != c.cfg.Package {
			continue
		}
		p := c.cfg.Schema.ExtensionPoints[impl.ExtensionPoint]
		if p == nil {
			continue // reported by the validation
		}
		iface := c.classes[p.Interface]
		if iface == nil || !iface.IsInterface() {
			continue // reported with the extension point
		}
		if msg := c.implementingClass(impl.Class, iface, true); msg != "" {
			c.report(Error, impl.File, syntax.Pos{}, nil, CodeExtensionPoint, "/class: %s", msg)
			continue
		}
		c.useIn(impl.File, "class", impl.Class)
		c.noCommit(p, c.classes[impl.Class], iface)
	}
}

// implementingClass checks a class that the kernel creates for an
// extension point and returns the problem, or "".
func (c *checker) implementingClass(name string, iface *Class, public bool) string {
	k := c.classes[name]
	switch {
	case k == nil:
		return name + " is not a class"
	case k.External:
		return name + " belongs to another package"
	case k.IsInterface():
		return name + " is an interface, not a class"
	case public && k.Visibility != syntax.Public:
		return name + " is not public"
	case k.Test:
		return name + " is a test class"
	case k.Native:
		return name + " is a native class and has no constructor"
	case !slices.Contains(allInterfaces(k), iface):
		return name + " does not implement " + iface.Name
	}
	for b := k; b != nil; b = b.Base {
		if b.Ctor == nil {
			continue
		}
		switch {
		case len(b.Ctor.Sig.Params) > 0 || public && b.Ctor.Visibility != syntax.Public:
			if public {
				return name + " needs a public constructor without parameters"
			}
			return name + " needs a constructor without parameters"
		case len(b.Ctor.Sig.Raises) > 0:
			return name + " has a constructor that raises " + b.Ctor.Sig.Raises[0].Name + "; the kernel creates it, so it raises no checked exceptions"
		}
		break
	}
	return ""
}

// useIn records the use of a class or an interface by a description file.
func (c *checker) useIn(file, kind, name string) {
	saved := c.loc
	c.loc = Site{File: file}
	c.use(syntax.Pos{Line: 1, Col: 1}, kind, name, "")
	c.loc = saved
}

// noCommit reports commit and rollback of the default session written
// directly in the methods with which a class of the package implements
// the interface of an extension point that does not allow commits. Calls
// of other methods are checked when they run.
func (c *checker) noCommit(p *model.ExtensionPoint, k *Class, iface *Class) {
	if p.CommitAllowed || k == nil {
		return
	}
	for _, im := range InterfaceMethods(iface) {
		m, ok := k.Member(im.Name).(*Method)
		if !ok || m.Decl == nil || m.Decl.Body == nil || m.Owner.External {
			continue
		}
		visitStmts(m.Decl.Body.Stmts, func(s syntax.Stmt) {
			if db, ok := s.(*syntax.DatabaseStmt); ok && db.Session == nil {
				c.at(m.Site, func() {
					c.errorf(db.Pos, CodeExtensionPoint, "%s implements %s of extension point %s, which does not allow implementations to %s the default session; "+
						"open a session of its own instead", m, im.Name, p.Name, db.Keyword)
				})
			}
		})
	}
}

// visitStmts calls fn for every statement in a list and in the blocks of
// its statements.
func visitStmts(ss []syntax.Stmt, fn func(syntax.Stmt)) {
	block := func(b *syntax.Block) {
		if b != nil {
			visitStmts(b.Stmts, fn)
		}
	}
	for _, s := range ss {
		fn(s)
		switch s := s.(type) {
		case *syntax.IfStmt:
			for _, cl := range s.Clauses {
				block(cl.Body)
			}
			block(s.Else)
		case *syntax.ForStmt:
			block(s.Body)
		case *syntax.WhileStmt:
			block(s.Body)
		case *syntax.TryStmt:
			block(s.Body)
			for _, cl := range s.Catches {
				block(cl.Body)
			}
			block(s.Finally)
		}
	}
}

// reflectedExtensionPoint checks the name that /TRBASE/REFLECTOR.
// EXTENSION_POINT gets as a string literal of an extension point of the
// own, a required or a kernel package, and records the use. Other names
// are looked up when the code runs.
func (c *checker) reflectedExtensionPoint(x *syntax.CallExpr) {
	if len(x.Args) != 1 || c.cfg.Schema == nil {
		return
	}
	name, ok := literal(x.Args[0].Value)
	if !ok {
		return
	}
	pos := x.Args[0].Value.Start()
	pkg, _, ok := model.SplitName(name)
	if !ok || pkg != c.cfg.Package && pkg != model.Kernel && !slices.Contains(c.cfg.Visible, pkg) {
		return
	}
	if c.cfg.Schema.ExtensionPoints[name] == nil {
		c.errorf(pos, CodeExtensionPoint, "extension point %s does not exist", name)
		return
	}
	c.use(pos, "extension-point", name, "")
}
