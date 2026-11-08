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
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// fnCtx is the method or function literal being checked.
type fnCtx struct {
	info     *FuncInfo
	result   Type
	raises   []*Class
	class    *Class
	method   *Method // the enclosing method
	static   bool
	ctor     bool
	closure  bool
	parent   *fnCtx
	scope    *scope // scope in the enclosing function where a closure was created
	handlers [][]*Class
	loops    int
	cursors  []string // sessions of the enclosing loops over select
}

type scope struct {
	vars   map[string]*Local
	parent *scope
	fn     *fnCtx
}

func (s *scope) lookup(name string) *Local {
	for x := s; x != nil; x = x.parent {
		if l, ok := x.vars[name]; ok {
			return l
		}
	}
	return nil
}

// env holds the paths known not to be null. The value tells whether the
// path may change through any call: attributes of objects.
type env map[string]bool

func (e env) clone() env { return maps.Clone(e) }

// meet keeps the paths known in both.
func meet(a, b env) env {
	out := env{}
	for k, v := range a {
		if _, ok := b[k]; ok {
			out[k] = v
		}
	}
	return out
}

// invalidate forgets a path and everything below it.
func (e env) invalidate(path string) {
	for k := range e {
		if k == path || strings.HasPrefix(k, path+".") {
			delete(e, k)
		}
	}
}

// call forgets the paths a call may change.
func (e env) call() {
	for k, v := range e {
		if v {
			delete(e, k)
		}
	}
}

// replace sets e to the content of o.
func (e env) replace(o env) {
	for k := range e {
		delete(e, k)
	}
	for k, v := range o {
		e[k] = v
	}
}

func (c *checker) bodies(k *Class) {
	c.loc = Site{File: k.File}
	defer func() { c.loc = Site{} }()
	for _, ct := range k.Constants {
		c.at(ct.Site, func() { c.constant(k, ct.Value, ct.Type) })
	}
	for _, a := range k.Attributes {
		if a.Init == nil {
			// Objects and functions have no zero value.
			switch a.Type.(type) {
			case *Class, *Func:
				c.at(a.Site, func() {
					c.errorf(a.Site.Pos, CodeNullCheck, "the attribute %s of type %s needs an initial value or must be optional", a.Name, a.Type)
				})
			}
			continue
		}
		c.at(a.Site, func() {
			if a.Static {
				c.constant(k, a.Init, a.Type)
				return
			}
			f := &fnCtx{info: &FuncInfo{}, result: TypVoid, class: k, ctor: true}
			e := env{}
			t := c.expr(f, &scope{vars: map[string]*Local{}, fn: f}, a.Init, e, a.Type)
			c.assign(a.Init, t, a.Type, "the initial value of "+a.Name)
		})
	}
	methods := slices.Clone(k.Methods)
	if k.Ctor != nil {
		methods = append(methods, k.Ctor)
	}
	for _, m := range methods {
		c.at(m.Site, func() { c.method(k, m) })
	}
}

// constant checks the value of a constant or static attribute: literals,
// enum constants and other constants only, so it can be computed when the
// program is compiled.
func (c *checker) constant(k *Class, x syntax.Expr, want Type) {
	f := &fnCtx{info: &FuncInfo{}, result: TypVoid, class: k, static: true}
	t := c.expr(f, &scope{vars: map[string]*Local{}, fn: f}, x, env{}, want)
	if !isConstExpr(c.info, x) {
		c.errorf(x.Start(), CodeConstant, "the value must be a literal, an enum constant or another constant")
		return
	}
	c.assign(x, t, want, "the value")
}

func isConstExpr(info *Info, x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.LiteralExpr:
		return true
	case *syntax.StringExpr:
		for _, p := range x.Parts {
			if p.Expr != nil {
				return false
			}
		}
		return true
	case *syntax.UnaryExpr:
		lit, ok := x.X.(*syntax.LiteralExpr)
		return x.Op == "-" && ok && (lit.Kind == syntax.INT || lit.Kind == syntax.DEC)
	case *syntax.ParenExpr:
		return isConstExpr(info, x.X)
	case *syntax.Ident:
		_, ok := info.Uses[x].(*Constant)
		return ok
	case *syntax.SelectExpr:
		s := info.Selects[x]
		return s != nil && (s.Kind == SelectConstant || s.Kind == SelectEnum)
	}
	return false
}

func (c *checker) method(k *Class, m *Method) {
	if m.Native {
		c.nativeMethod(k, m)
		return
	}
	if m.Decl == nil || m.Decl.Body == nil {
		return
	}
	f := &fnCtx{info: &FuncInfo{Type: m.Sig}, result: m.Sig.Result, raises: m.Sig.Raises, class: k, method: m, static: m.Static, ctor: m.Ctor}
	m.Func = f.info
	sc := &scope{vars: map[string]*Local{}, fn: f}
	for i, name := range m.Params {
		sc.vars[name] = f.info.newLocal(name, m.Sig.Params[i], true)
	}
	stmts := m.Decl.Body.Stmts
	if m.Ctor {
		stmts = c.superCall(k, f, sc, stmts)
	}
	if c.block(f, sc, stmts, env{}) || m.Sig.Result == TypVoid {
		return
	}
	c.errorf(m.Decl.Body.End.Pos, CodeReturn, "%s must return a %s at the end", m, m.Sig.Result)
}

// superCall checks the call of the base constructor, which must be the
// first statement when the base constructor takes parameters.
func (c *checker) superCall(k *Class, f *fnCtx, sc *scope, stmts []syntax.Stmt) []syntax.Stmt {
	base := k.baseCtor()
	if len(stmts) > 0 {
		if es, ok := stmts[0].(*syntax.ExprStmt); ok {
			if call, ok := es.X.(*syntax.CallExpr); ok {
				if _, ok := call.Fun.(*syntax.SuperExpr); ok {
					if k.Base == nil {
						c.errorf(call.Fun.Start(), CodeInherit, "%s has no base class", k.Name)
						return stmts[1:]
					}
					sig := &Func{Result: TypVoid}
					if base != nil {
						sig = base.Sig
					}
					e := env{}
					c.args(f, sc, call, sig, e, nil)
					c.info.Calls[call] = &Call{Kind: CallCtor, Method: base, Args: argValues(call)}
					c.info.Types[call] = TypVoid
					for _, r := range sig.Raises {
						c.raise(f, r, call.Fun.Start())
					}
					return stmts[1:]
				}
			}
		}
	}
	if base != nil && len(base.Sig.Params) > 0 {
		c.errorf(k.Ctor.Site.Pos, CodeInherit, "the constructor must start with super(...) for %s", base)
	}
	return stmts
}

// block checks statements in a new scope and reports whether the block
// always ends with return or raise.
func (c *checker) block(f *fnCtx, parent *scope, stmts []syntax.Stmt, e env) bool {
	sc := &scope{vars: map[string]*Local{}, parent: parent, fn: f}
	return c.stmts(f, sc, stmts, e)
}

func (c *checker) stmts(f *fnCtx, sc *scope, stmts []syntax.Stmt, e env) bool {
	terminates := false
	for _, s := range stmts {
		if terminates {
			c.errorf(s.Start(), CodeStatement, "this statement is never reached")
			break
		}
		terminates = c.stmt(f, sc, s, e)
	}
	return terminates
}

// declare adds a local variable to the scope.
func (c *checker) declare(f *fnCtx, sc *scope, id syntax.Ident, t Type) *Local {
	c.checkLocalName(id, "variable")
	if sc.lookup(id.Name) != nil || f.parent != nil && f.scope.lookup(id.Name) != nil {
		c.errorf(id.Pos, CodeDuplicate, "%s is already declared", id.Name)
	}
	l := f.info.newLocal(id.Name, t, false)
	sc.vars[id.Name] = l
	return l
}

func (c *checker) stmt(f *fnCtx, sc *scope, s syntax.Stmt, e env) bool {
	switch s := s.(type) {
	case *syntax.VarStmt:
		var want Type
		if s.Type != nil {
			want = c.resolveType(s.Type)
		}
		if s.Value == nil {
			// var x: T starts with the initial values of T.
			switch NonOpt(want).(type) {
			case *Class, *Func:
				if !IsOptional(want) {
					c.errorf(s.Name.Pos, CodeNullCheck, "%s of type %s needs an initial value or an optional type", s.Name.Name, want)
				}
			}
			if IsKind(want, Session) {
				c.errorf(s.Name.Pos, CodeType, "a SESSION comes from open session")
			}
			if p, ok := want.(*ExtensionPoint); ok {
				c.errorf(s.Name.Pos, CodeExtensionPoint, "the value of %s comes from extension point %s", p.Name, p.Name)
			}
			c.info.Vars[s] = []*Local{c.declare(f, sc, s.Name, want)}
			e.invalidate(s.Name.Name)
			break
		}
		t := c.expr(f, sc, s.Value, e, want)
		if want == nil {
			switch {
			case t == TypNull:
				c.errorf(s.Value.Start(), CodeType, "the type of %s cannot be derived from null; write var %s: T? = null", s.Name.Name, s.Name.Name)
				t = TypInvalid
			case t == TypVoid:
				c.errorf(s.Value.Start(), CodeType, "the call returns no value")
				t = TypInvalid
			}
			want = t
		} else {
			c.assign(s.Value, t, want, "the value of "+s.Name.Name)
		}
		l := c.declare(f, sc, s.Name, want)
		c.info.Vars[s] = []*Local{l}
		e.invalidate(s.Name.Name)
		if IsOptional(want) && !IsOptional(t) && t != TypInvalid {
			e[s.Name.Name] = false
		}
	case *syntax.AssignStmt:
		c.assignStmt(f, sc, s, e)
	case *syntax.ExprStmt:
		switch s.X.(type) {
		case *syntax.CallExpr, *syntax.UpdateExpr, *syntax.DeleteExpr:
		default:
			c.errorf(s.X.Start(), CodeStatement, "the value of the expression is not used")
		}
		c.expr(f, sc, s.X, e, nil)
	case *syntax.IfStmt:
		var outs []env
		cur := e.clone()
		all := true
		for _, cl := range s.Clauses {
			t, fl := c.cond(f, sc, cl.Cond, cur)
			if term := c.block(f, sc, cl.Body.Stmts, t); !term {
				all = false
				outs = append(outs, t)
			}
			cur = fl
		}
		if s.Else != nil {
			if term := c.block(f, sc, s.Else.Stmts, cur); !term {
				all = false
				outs = append(outs, cur)
			}
		} else {
			all = false
			outs = append(outs, cur)
		}
		if len(outs) > 0 {
			res := outs[0]
			for _, o := range outs[1:] {
				res = meet(res, o)
			}
			e.replace(res)
		}
		return all
	case *syntax.WhileStmt:
		c.loopEntry(s.Body.Stmts, e)
		t, fl := c.cond(f, sc, s.Cond, e)
		f.loops++
		c.block(f, sc, s.Body.Stmts, t)
		f.loops--
		e.replace(fl)
		// while true without break never ends normally.
		if lit, ok := s.Cond.(*syntax.LiteralExpr); ok && lit.Raw == "true" && !hasBreak(s.Body.Stmts) {
			return true
		}
	case *syntax.ForStmt:
		c.forStmt(f, sc, s, e)
	case *syntax.BreakStmt, *syntax.ContinueStmt:
		if f.loops == 0 {
			c.errorf(s.Start(), CodeStatement, "break and continue are only allowed in loops")
		}
		return true
	case *syntax.ReturnStmt:
		c.returnStmt(f, sc, s, e)
		return true
	case *syntax.RaiseStmt:
		c.raiseStmt(f, sc, s, e)
		return true
	case *syntax.TryStmt:
		return c.tryStmt(f, sc, s, e)
	case *syntax.AssertStmt:
		if !f.class.Test {
			c.errorf(s.Pos, CodeTest, "assert is only allowed in test classes")
		}
		t, _ := c.cond(f, sc, s.Cond, e)
		if s.Message != nil {
			mt := c.expr(f, sc, s.Message, t, TypString)
			c.assign(s.Message, mt, TypString, "the message")
		}
		e.replace(t)
	case *syntax.LogStmt:
		t := c.expr(f, sc, s.Value, e, nil)
		c.printable(s.Value, t)
	case *syntax.AuthCheckStmt:
		c.authCheck(f, sc, &s.AuthCheck, nil, e)
	case *syntax.ModifyStmt:
		c.modifyStmt(f, sc, s, e)
	case *syntax.SaveStmt:
		c.saveStmt(f, sc, s, e)
	case *syntax.IncludeStmt:
		return c.includeStmt(f, sc, s, e)
	case *syntax.DatabaseStmt:
		c.sessionStmt(f, sc, s, e)
	case *syntax.RowStmt:
		c.rowStmt(f, sc, s, e)
	}
	return false
}

// loopEntry forgets what the loop body may change before the condition is
// checked again.
func (c *checker) loopEntry(body []syntax.Stmt, e env) {
	all := false
	var walk func([]syntax.Stmt)
	walk = func(ss []syntax.Stmt) {
		for _, s := range ss {
			switch s := s.(type) {
			case *syntax.AssignStmt:
				if p, ok := syntacticPath(s.Target); ok {
					e.invalidate(p)
				}
			case *syntax.VarStmt:
				e.invalidate(s.Name.Name)
			case *syntax.IncludeStmt:
				all = true
			case *syntax.IfStmt:
				for _, cl := range s.Clauses {
					walk(cl.Body.Stmts)
				}
				if s.Else != nil {
					walk(s.Else.Stmts)
				}
			case *syntax.WhileStmt:
				walk(s.Body.Stmts)
			case *syntax.ForStmt:
				walk(s.Body.Stmts)
			case *syntax.TryStmt:
				walk(s.Body.Stmts)
				for _, cc := range s.Catches {
					walk(cc.Body.Stmts)
				}
				if s.Finally != nil {
					walk(s.Finally.Stmts)
				}
			}
		}
	}
	walk(body)
	if all {
		e.replace(env{})
	}
	// Calls in the loop may change attributes.
	e.call()
}

// syntacticPath returns the path of an assignment target as written.
func syntacticPath(x syntax.Expr) (string, bool) {
	switch x := x.(type) {
	case *syntax.Ident:
		if syntax.CaseOf(x.Name) == syntax.UpperCase {
			return "me." + x.Name, true
		}
		return x.Name, true
	case *syntax.MeExpr:
		return "me", true
	case *syntax.ParenExpr:
		return syntacticPath(x.X)
	case *syntax.SelectExpr:
		if g, ok := x.X.(*syntax.GlobalExpr); ok {
			return "static:" + g.Name.Name + "." + x.Name.Name, true
		}
		p, ok := syntacticPath(x.X)
		return p + "." + x.Name.Name, ok
	case *syntax.IndexExpr:
		return syntacticPath(x.X)
	}
	return "", false
}

func (c *checker) assignStmt(f *fnCtx, sc *scope, s *syntax.AssignStmt, e env) {
	target := c.place(f, sc, s.Target, e)
	t := c.expr(f, sc, s.Value, e, target)
	if target == nil {
		return
	}
	c.assign(s.Value, t, target, "the value")
	if p, ok := syntacticPath(s.Target); ok {
		e.invalidate(p)
		if _, isIndex := s.Target.(*syntax.IndexExpr); !isIndex && IsOptional(target) && !IsOptional(t) && t != TypInvalid {
			e[p] = c.volatile(s.Target)
		}
	}
}

// volatile reports paths that a call may change: attributes of objects.
func (c *checker) volatile(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Ident:
		_, isLocal := c.info.Uses[x].(*Local)
		return !isLocal
	case *syntax.ParenExpr:
		return c.volatile(x.X)
	case *syntax.SelectExpr:
		if s := c.info.Selects[x]; s != nil && s.Kind != SelectField {
			return true
		}
		return c.volatile(x.X)
	}
	return true
}

// place checks an assignment target and returns its type.
func (c *checker) place(f *fnCtx, sc *scope, x syntax.Expr, e env) Type {
	switch x := x.(type) {
	case *syntax.Ident:
		if syntax.CaseOf(x.Name) == syntax.LowerCase {
			l := sc.lookup(x.Name)
			if l == nil && f.closure {
				if outer := f.scope.lookup(x.Name); outer != nil {
					c.errorf(x.Pos, CodeStatement, "a function cannot assign %s of the enclosing method", x.Name)
					c.info.Uses[x] = outer
					return outer.Type
				}
			}
			if l == nil {
				c.errorf(x.Pos, CodeUnknown, "unknown variable %s", x.Name)
				return nil
			}
			c.info.Uses[x] = l
			c.info.Types[x] = l.Type
			return l.Type
		}
	case *syntax.SelectExpr, *syntax.IndexExpr:
	default:
		c.errorf(x.Start(), CodeStatement, "cannot assign to this expression")
		return nil
	}
	saved := e.clone()
	t := c.expr(f, sc, x, e, nil)
	e.replace(saved)
	switch u := c.info.Uses[identOf(x)].(type) {
	case *Constant:
		c.errorf(x.Start(), CodeStatement, "%s is a constant", u.Name)
		return nil
	case *Method:
		c.errorf(x.Start(), CodeStatement, "cannot assign to the method %s", u.Name)
		return nil
	}
	if s, ok := x.(*syntax.SelectExpr); ok {
		if sel := c.info.Selects[s]; sel != nil && (sel.Kind == SelectConstant || sel.Kind == SelectEnum || sel.Kind == SelectMethod || sel.Kind == SelectBuiltin) {
			c.errorf(x.Start(), CodeStatement, "cannot assign to %s", s.Name.Name)
			return nil
		}
	}
	// The declared type of the place, not the type known after null checks.
	return c.declaredType(x, t)
}

func identOf(x syntax.Expr) *syntax.Ident {
	id, _ := x.(*syntax.Ident)
	return id
}

// declaredType returns the type of a place without null checks.
func (c *checker) declaredType(x syntax.Expr, known Type) Type {
	switch x := x.(type) {
	case *syntax.Ident:
		switch u := c.info.Uses[x].(type) {
		case *Local:
			return u.Type
		case *Attribute:
			return u.Type
		}
	case *syntax.SelectExpr:
		if s := c.info.Selects[x]; s != nil {
			switch s.Kind {
			case SelectAttribute, SelectStatic:
				return s.Attribute.Type
			case SelectField:
				if st, ok := NonOpt(c.info.Types[x.X]).(*Struct); ok {
					return st.Fields[s.Field].Type
				}
			}
		}
	case *syntax.IndexExpr:
		switch col := NonOpt(c.info.Types[x.X]).(type) {
		case *Array:
			return col.Elem
		case *Map:
			return col.Elem
		}
	}
	return known
}

func (c *checker) forStmt(f *fnCtx, sc *scope, s *syntax.ForStmt, e env) {
	inner := &scope{vars: map[string]*Local{}, parent: sc, fn: f}
	var locals []*Local
	if q, ok := s.In.(*syntax.QueryExpr); ok && q.Mode == "" {
		// The loop streams the rows through a cursor.
		c.info.Cursors[s] = true
		if key, ok := sessionKey(q.Using); ok {
			f.cursors = append(f.cursors, key)
			defer func() { f.cursors = f.cursors[:len(f.cursors)-1] }()
		}
	}
	if s.In != nil {
		t := c.expr(f, sc, s.In, e, nil)
		c.notNull(s.In, t)
		var kt, vt Type = TypInvalid, TypInvalid
		switch col := NonOpt(t).(type) {
		case *Array:
			kt, vt = TypInt8, col.Elem
		case *Map:
			kt, vt = col.Key, col.Elem
			if s.Key == nil {
				c.errorf(s.Var.Pos, CodeType, "loops over maps name key and value: for key, value in ...")
			}
		default:
			if t != TypInvalid {
				c.errorf(s.In.Start(), CodeType, "cannot loop over %s; for ... in needs an array or a map", t)
			}
		}
		if s.Key != nil {
			locals = append(locals, c.declare(f, inner, *s.Key, kt))
		}
		locals = append(locals, c.declare(f, inner, s.Var, vt))
	} else {
		ft := c.expr(f, sc, s.From, e, nil)
		tt := c.expr(f, sc, s.To, e, nil)
		vt := Type(TypInt4)
		if IsKind(ft, Int8) || IsKind(tt, Int8) {
			vt = TypInt8
		}
		for _, x := range []syntax.Expr{s.From, s.To, s.Step} {
			if x == nil {
				continue
			}
			xt := c.info.Types[x]
			if x == s.Step {
				xt = c.expr(f, sc, s.Step, e, vt)
			}
			if !IsInteger(xt) && xt != TypInvalid {
				c.errorf(x.Start(), CodeType, "for ... from ... to counts with INT4 or INT8, not %s", xt)
				continue
			}
			c.assign(x, xt, vt, "the bound")
		}
		locals = append(locals, c.declare(f, inner, s.Var, vt))
	}
	c.info.Vars[s] = locals
	c.loopEntry(s.Body.Stmts, e)
	f.loops++
	c.stmts(f, inner, s.Body.Stmts, e.clone())
	f.loops--
}

func (c *checker) returnStmt(f *fnCtx, sc *scope, s *syntax.ReturnStmt, e env) {
	if s.Value == nil {
		if f.result != TypVoid {
			c.errorf(s.Pos, CodeReturn, "return needs a value of type %s", f.result)
		}
		return
	}
	if f.result == TypVoid {
		c.errorf(s.Pos, CodeReturn, "this method returns no value")
		c.expr(f, sc, s.Value, e, nil)
		return
	}
	t := c.expr(f, sc, s.Value, e, f.result)
	c.assign(s.Value, t, f.result, "the result")
}

func (c *checker) raiseStmt(f *fnCtx, sc *scope, s *syntax.RaiseStmt, e env) {
	c.checkGlobalName(s.Class)
	k := c.classRef(s.Class)
	if k == nil {
		for _, a := range s.Args {
			c.expr(f, sc, a, e, nil)
		}
		return
	}
	switch {
	case !k.IsException():
		c.errorf(s.Class.Pos, CodeException, "%s is not an exception class", k.Name)
	case k.IsRuntimeError() && !f.class.Kernel:
		c.errorf(s.Class.Pos, CodeException, "%s is a runtime error; only the kernel raises runtime errors", k.Name)
	}
	c.info.Raises[s] = k
	sig := &Func{Result: TypVoid}
	if k.Ctor != nil {
		sig = k.Ctor.Sig
	} else if b := k.baseCtor(); b != nil {
		sig = b.Sig
	}
	call := &syntax.CallExpr{Fun: &syntax.GlobalExpr{Name: s.Class}}
	for _, a := range s.Args {
		call.Args = append(call.Args, syntax.Arg{Value: a})
	}
	c.args(f, sc, call, sig, e, nil)
	for _, r := range sig.Raises {
		c.raise(f, r, s.Pos)
	}
	c.raise(f, k, s.Pos)
}

// raise requires that a checked exception is caught or declared.
func (c *checker) raise(f *fnCtx, k *Class, pos syntax.Pos) {
	if k.IsRuntimeError() {
		return
	}
	for i := len(f.handlers) - 1; i >= 0; i-- {
		for _, h := range f.handlers[i] {
			if k.Extends(h) {
				return
			}
		}
	}
	for _, r := range f.raises {
		if k.Extends(r) {
			return
		}
	}
	where := "the method"
	if f.closure {
		where = "the function"
	}
	c.errorf(pos, CodeException, "%s is neither caught nor declared with raises in %s", k.Name, where)
}

func (c *checker) tryStmt(f *fnCtx, sc *scope, s *syntax.TryStmt, e env) bool {
	var caught []*Class
	for _, cc := range s.Catches {
		c.checkGlobalName(cc.Class)
		k := c.classRef(cc.Class)
		if k == nil {
			continue
		}
		if !k.IsException() {
			c.errorf(cc.Class.Pos, CodeException, "%s is not an exception class", k.Name)
		}
		caught = append(caught, k)
	}
	before := e.clone()
	f.handlers = append(f.handlers, caught)
	body := e.clone()
	termBody := c.block(f, sc, s.Body.Stmts, body)
	f.handlers = f.handlers[:len(f.handlers)-1]
	// A handler may start after any statement of the body; only what was
	// known before the try is certain, minus what the body may change.
	handlerEnv := meet(before, body)
	handlerEnv.call()
	outs := []env{}
	if !termBody {
		outs = append(outs, body)
	}
	all := termBody
	i := 0
	for _, cc := range s.Catches {
		inner := &scope{vars: map[string]*Local{}, parent: sc, fn: f}
		var k *Class
		if i < len(caught) && caught[i].Name == cc.Class.Name {
			k = caught[i]
			i++
		}
		var t Type = TypInvalid
		if k != nil {
			t = k
		}
		l := c.declare(f, inner, cc.Var, t)
		c.info.Catches[cc] = l
		ce := handlerEnv.clone()
		if !c.stmts(f, inner, cc.Body.Stmts, ce) {
			all = false
			outs = append(outs, ce)
		}
	}
	res := handlerEnv
	if len(outs) > 0 {
		res = outs[0]
		for _, o := range outs[1:] {
			res = meet(res, o)
		}
	}
	if s.Finally != nil {
		fe := handlerEnv.clone()
		if c.block(f, sc, s.Finally.Stmts, fe) {
			return true
		}
		res = meet(res, fe)
	}
	e.replace(res)
	return all
}

// includeStmt inserts a statements include at the include statement and
// checks it in the scope of the including block.
func (c *checker) includeStmt(f *fnCtx, sc *scope, s *syntax.IncludeStmt, e env) bool {
	var stack []string
	for _, ch := range c.loc.Chain {
		stack = append([]string{ch.Include}, stack...)
	}
	u, ok := c.findInclude(s.Name, s.IfFound, stack)
	if !ok {
		return false
	}
	file, errs := syntax.Parse(u.File, u.Text)
	if len(errs) > 0 {
		return false
	}
	if file.Include == nil || file.Include.Kind != syntax.StatementsInclude {
		c.errorf(s.Name.Pos, CodeInclude, "%s is a members include and cannot be included among statements", s.Name.Name)
		return false
	}
	site := IncludeSite{Include: s.Name.Name, File: c.loc.File, Line: s.Name.Pos.Line}
	chain := append([]IncludeSite{site}, c.loc.Chain...)
	c.info.Expansions[s] = &Expansion{Name: s.Name.Name, File: u.File, Chain: chain, Stmts: file.Include.Body.Stmts}
	saved := c.loc
	c.loc = Site{File: u.File, Chain: chain}
	defer func() { c.loc = saved }()
	return c.stmts(f, sc, file.Include.Body.Stmts, e)
}

// hasBreak reports a break that leaves the loop with this body.
func hasBreak(ss []syntax.Stmt) bool {
	for _, s := range ss {
		switch s := s.(type) {
		case *syntax.BreakStmt:
			return true
		case *syntax.IfStmt:
			for _, cl := range s.Clauses {
				if hasBreak(cl.Body.Stmts) {
					return true
				}
			}
			if s.Else != nil && hasBreak(s.Else.Stmts) {
				return true
			}
		case *syntax.TryStmt:
			if hasBreak(s.Body.Stmts) || s.Finally != nil && hasBreak(s.Finally.Stmts) {
				return true
			}
			for _, cc := range s.Catches {
				if hasBreak(cc.Body.Stmts) {
					return true
				}
			}
		case *syntax.IncludeStmt:
			return true // conservative: an include may contain break
		}
	}
	return false
}
