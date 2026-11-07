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
	"math/big"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// expr checks an expression and returns its type, taking null checks into
// account. want is the expected type, if known; it lets literals adapt.
func (c *checker) expr(f *fnCtx, sc *scope, x syntax.Expr, e env, want Type) Type {
	t := c.expr0(f, sc, x, e, want)
	if t == nil {
		t = TypInvalid
	}
	// A path checked for null is not optional any more.
	if o, ok := t.(*Optional); ok {
		if p, ok := syntacticPath(x); ok {
			if _, known := e[p]; known {
				t = o.Elem
			}
		}
	}
	c.info.Types[x] = t
	return t
}

func (c *checker) expr0(f *fnCtx, sc *scope, x syntax.Expr, e env, want Type) Type {
	switch x := x.(type) {
	case *syntax.LiteralExpr:
		return c.literal(x, want)
	case *syntax.StringExpr:
		for _, p := range x.Parts {
			if p.Expr != nil {
				t := c.expr(f, sc, p.Expr, e, nil)
				c.printable(p.Expr, t)
			}
		}
		return TypString
	case *syntax.Ident:
		return c.ident(f, sc, x, e)
	case *syntax.GlobalExpr:
		c.checkGlobalName(x.Name)
		c.global(x)
		c.errorf(x.Name.Pos, CodeType, "%s is a type, not a value", x.Name.Name)
		return TypInvalid
	case *syntax.MeExpr:
		if f.static {
			c.errorf(x.Pos, CodeStatic, "me is not available in static methods")
			return TypInvalid
		}
		return f.class
	case *syntax.SuperExpr:
		c.errorf(x.Pos, CodeType, "super is only used as super(...) or super.METHOD(...)")
		return TypInvalid
	case *syntax.ParenExpr:
		t := c.expr(f, sc, x.X, e, want)
		if cv, ok := c.info.Conv[x.X]; ok {
			delete(c.info.Conv, x.X)
			c.info.Conv[x] = cv
		}
		return t
	case *syntax.UnaryExpr:
		if x.Op == "not" {
			t, fl := c.cond(f, sc, x.X, e)
			e.replace(meet(t, fl))
			return TypBool
		}
		t := c.expr(f, sc, x.X, e, want)
		c.notNull(x.X, t)
		if !IsNumeric(t) && t != TypInvalid {
			c.errorf(x.Pos, CodeType, "- needs a number, not %s", t)
			return TypInvalid
		}
		return Underlying(NonOpt(t))
	case *syntax.BinaryExpr:
		return c.binary(f, sc, x, e, want)
	case *syntax.IsExpr:
		t := c.expr(f, sc, x.X, e, nil)
		if x.What == "null" {
			if !IsOptional(t) && t != TypInvalid && !c.narrowed(x.X, e) {
				c.errorf(x.OpPos, CodeNullCheck, "%s is never null", syntax.PrintExpr(x.X))
			}
			return TypBool
		}
		switch {
		case IsOptional(t):
			c.errorf(x.OpPos, CodeEmpty, "%s may be null; check is not null first, is empty applies only to values", syntax.PrintExpr(x.X))
		case !emptyable(t) && t != TypInvalid:
			c.errorf(x.OpPos, CodeType, "is empty applies to STRING, BYTES, arrays and maps, not %s", t)
		}
		return TypBool
	case *syntax.AsExpr:
		to := c.resolveType(x.Type)
		from := c.expr(f, sc, x.X, e, nil)
		c.notNull(x.X, from)
		if IsOptional(to) {
			c.errorf(x.Type.Pos, CodeType, "convert to a type that is not optional")
			return TypInvalid
		}
		if from != TypInvalid && to != TypInvalid && !convertible(from, to) {
			c.errorf(x.Type.Pos, CodeType, "%s cannot be converted to %s", from, to)
		}
		return to
	case *syntax.SelectExpr:
		return c.selectExpr(f, sc, x, e)
	case *syntax.IndexExpr:
		t := c.expr(f, sc, x.X, e, nil)
		c.notNull(x.X, t)
		switch col := NonOpt(t).(type) {
		case *Array:
			it := c.expr(f, sc, x.Index, e, TypInt8)
			c.assign(x.Index, it, TypInt8, "the index")
			return col.Elem
		case *Map:
			kt := c.expr(f, sc, x.Index, e, col.Key)
			c.assign(x.Index, kt, col.Key, "the key")
			return col.Elem
		}
		c.expr(f, sc, x.Index, e, nil)
		if t != TypInvalid {
			c.errorf(x.Index.Start(), CodeType, "[...] applies to arrays and maps, not %s", t)
		}
		return TypInvalid
	case *syntax.CallExpr:
		return c.call(f, sc, x, e, want)
	case *syntax.NewExpr:
		return c.newExpr(f, sc, x, e)
	case *syntax.ArrayExpr:
		var elem Type
		if a, ok := NonOpt(want).(*Array); ok {
			elem = a.Elem
		}
		for _, el := range x.Elems {
			t := c.expr(f, sc, el, e, elem)
			if elem == nil {
				elem = t
			}
			c.assign(el, t, elem, "the element")
		}
		if elem == nil {
			c.errorf(x.Pos, CodeType, "the element type of [] cannot be derived; write var a: array of T = [] or new array of T")
			return TypInvalid
		}
		if a, ok := NonOpt(want).(*Array); ok && Identical(a.Elem, elem) {
			return a
		}
		return &Array{Elem: elem}
	case *syntax.MapExpr:
		var kt, vt Type
		if m, ok := NonOpt(want).(*Map); ok {
			kt, vt = m.Key, m.Elem
		}
		for i := range x.Keys {
			k := c.expr(f, sc, x.Keys[i], e, kt)
			if kt == nil {
				kt = k
			}
			c.assign(x.Keys[i], k, kt, "the key")
			v := c.expr(f, sc, x.Values[i], e, vt)
			if vt == nil {
				vt = v
			}
			c.assign(x.Values[i], v, vt, "the value")
		}
		if kt == nil {
			c.errorf(x.Pos, CodeType, "the types of [:] cannot be derived; write var m: map of K to V = [:] or new map of K to V")
			return TypInvalid
		}
		if !mapKey(kt) {
			c.errorf(x.Pos, CodeType, "%s cannot be a map key", kt)
		}
		return &Map{Key: kt, Elem: vt}
	case *syntax.FuncExpr:
		return c.funcExpr(f, sc, x, e, want)
	case *syntax.QueryExpr:
		return c.query(f, sc, x, e)
	case *syntax.UpdateExpr:
		return c.update(f, sc, x, e)
	case *syntax.DeleteExpr:
		return c.delete(f, sc, x, e)
	case *syntax.OpenSessionExpr:
		return TypSession
	}
	return TypInvalid
}

// narrowed reports whether a path is known not to be null.
func (c *checker) narrowed(x syntax.Expr, e env) bool {
	p, ok := syntacticPath(x)
	if !ok {
		return false
	}
	_, known := e[p]
	return known
}

func emptyable(t Type) bool {
	switch Underlying(t).(type) {
	case *Array, *Map:
		return true
	}
	return IsKind(t, String) || IsKind(t, Bytes)
}

// notNull reports the use of a value that may be null.
func (c *checker) notNull(x syntax.Expr, t Type) {
	if t == TypNull {
		c.errorf(x.Start(), CodeNullCheck, "null cannot be used here")
		return
	}
	if IsOptional(t) {
		c.errorf(x.Start(), CodeNullCheck, "%s may be null; check it with is not null or use default", syntax.PrintExpr(x))
	}
}

// printable reports values that cannot appear in a placeholder or log.
func (c *checker) printable(x syntax.Expr, t Type) {
	c.notNull(x, t)
	switch u := Underlying(NonOpt(t)).(type) {
	case *Basic:
		if u.Kind != Void && u.Kind != Invalid && u.Kind != Null {
			return
		}
		if u.Kind == Invalid {
			return
		}
	case *Enum:
		return
	}
	if !IsOptional(t) {
		c.errorf(x.Start(), CodeType, "%s cannot be shown as text", t)
	}
}

// convertible lists the explicit conversions with as.
func convertible(from, to Type) bool {
	if e, ok := from.(*Enum); ok {
		return IsKind(to, String) || Identical(e.Base, Underlying(to))
	}
	if _, ok := to.(*Enum); ok {
		return false
	}
	fb, tb := BasicOf(from), BasicOf(to)
	if fb == nil || tb == nil {
		return false
	}
	num := func(k BasicKind) bool { return k == Int4 || k == Int8 || k == Dec }
	switch {
	case fb.Kind == tb.Kind:
		return true
	case num(fb.Kind) && num(tb.Kind):
		return true
	case tb.Kind == String:
		return fb.Kind != Bytes
	case fb.Kind == String:
		return tb.Kind != Bytes
	case fb.Kind == Date && tb.Kind == Timestamp, fb.Kind == Timestamp && tb.Kind == Date:
		return true
	}
	return false
}

// assign checks that a value of type from can be stored where to is
// expected and records the implicit conversion.
func (c *checker) assign(x syntax.Expr, from, to Type, what string) bool {
	if from == TypInvalid || to == TypInvalid || to == nil {
		return true
	}
	if from == TypVoid {
		c.errorf(x.Start(), CodeType, "the call returns no value")
		return false
	}
	if from == TypNull {
		if IsOptional(to) {
			c.info.Conv[x] = to
			return true
		}
		c.errorf(x.Start(), CodeNullCheck, "%s cannot be null; it is %s", what, to)
		return false
	}
	if IsOptional(from) && !IsOptional(to) {
		c.errorf(x.Start(), CodeNullCheck, "%s may be null, but %s is not optional; check it with is not null or use default", syntax.PrintExpr(x), to)
		return false
	}
	if !assignable(NonOpt(from), NonOpt(to)) {
		c.errorf(x.Start(), CodeType, "%s is %s, but %s is needed", what, from, to)
		return false
	}
	if needsConv(from, to) {
		c.info.Conv[x] = to
	}
	return true
}

// assignable reports whether a value of type from can be stored as to.
func assignable(from, to Type) bool {
	if Identical(from, to) {
		return true
	}
	switch t := to.(type) {
	case *Class:
		k, ok := from.(*Class)
		return ok && k.Extends(t)
	case *Func:
		ff, ok := from.(*Func)
		if !ok || len(ff.Params) != len(t.Params) || !Identical(ff.Result, t.Result) {
			return false
		}
		for i := range ff.Params {
			if !Identical(ff.Params[i], t.Params[i]) {
				return false
			}
		}
		for _, r := range ff.Raises {
			if !slices.ContainsFunc(t.Raises, func(d *Class) bool { return r.Extends(d) }) {
				return false
			}
		}
		return true
	case *Enum, *Struct, *Array, *Map:
		return false
	}
	fb, tb := BasicOf(from), BasicOf(to)
	if fb == nil || tb == nil {
		return false
	}
	if _, isEnum := from.(*Enum); isEnum {
		return false
	}
	return widens(fb, tb)
}

// widens lists the lossless conversions between built-in types.
func widens(from, to *Basic) bool {
	switch {
	case from.Kind == to.Kind && from.Kind != Dec:
		return true
	case from.Kind == Int4 && to.Kind == Int8:
		return true
	case from.Kind == Int4 && to.Kind == Dec:
		return to.Precision-to.Scale >= 10
	case from.Kind == Int8 && to.Kind == Dec:
		return to.Precision-to.Scale >= 19
	case from.Kind == Dec && to.Kind == Dec:
		// The number of digits before the point is checked when the value
		// is stored.
		return to.Scale >= from.Scale
	}
	return false
}

// needsConv reports conversions that change the representation or need a
// check of the domain rules.
func needsConv(from, to Type) bool {
	if IsOptional(to) && !IsOptional(from) {
		return true
	}
	from, to = NonOpt(from), NonOpt(to)
	if _, ok := to.(*Named); ok {
		return true
	}
	fb, tb := BasicOf(from), BasicOf(to)
	if fb == nil || tb == nil {
		return false
	}
	return fb.Kind != tb.Kind || fb.Kind == Dec && (fb.Scale != tb.Scale || fb.Precision > tb.Precision)
}

// literal types a literal, adapting numbers to the expected type.
func (c *checker) literal(x *syntax.LiteralExpr, want Type) Type {
	switch x.Kind {
	case syntax.KEYWORD:
		switch x.Raw {
		case "true", "false":
			return TypBool
		case "null":
			return TypNull
		}
	case syntax.INT:
		return c.intLiteral(x, strings.ReplaceAll(x.Raw, "_", ""), want, false)
	case syntax.DEC:
		raw := strings.ReplaceAll(x.Raw, "_", "")
		ip, fp, _ := strings.Cut(raw, ".")
		ip = strings.TrimLeft(ip, "0")
		digits := len(ip) + len(fp)
		if digits > MaxPrecision {
			c.errorf(x.Pos, CodeType, "%s has more than %d digits", x.Raw, MaxPrecision)
			return TypInvalid
		}
		if b := BasicOf(NonOpt(want)); b != nil && b.Kind == Dec && len(fp) <= b.Scale && len(ip) <= b.Precision-b.Scale {
			return b
		}
		return DecType(max(digits, len(fp), 1), len(fp))
	}
	return TypInvalid
}

func (c *checker) intLiteral(x *syntax.LiteralExpr, digits string, want Type, negative bool) Type {
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return TypInvalid
	}
	if negative {
		n.Neg(n)
	}
	fitsInt4, fitsInt8 := n.IsInt64() && n.Int64() >= -1<<31 && n.Int64() < 1<<31, n.IsInt64()
	if b := BasicOf(NonOpt(want)); b != nil {
		switch b.Kind {
		case Int4:
			if fitsInt4 {
				return b
			}
		case Int8:
			if fitsInt8 {
				return b
			}
		case Dec:
			if len(strings.TrimLeft(digits, "0")) <= b.Precision-b.Scale {
				return b
			}
		}
	}
	switch {
	case fitsInt4:
		return TypInt4
	case fitsInt8:
		return TypInt8
	}
	c.errorf(x.Pos, CodeType, "%s does not fit into INT8; write it as decimal %s.0", x.Raw, x.Raw)
	return TypInvalid
}

func (c *checker) ident(f *fnCtx, sc *scope, x *syntax.Ident, e env) Type {
	switch syntax.CaseOf(x.Name) {
	case syntax.LowerCase:
		if l := sc.lookup(x.Name); l != nil {
			c.info.Uses[x] = l
			return l.Type
		}
		// A local of an enclosing method, used by a closure.
		for g := f; g.parent != nil; g = g.parent {
			if l := g.scope.lookup(x.Name); l != nil {
				l.Captured = true
				for h := f; h != g.parent; h = h.parent {
					h.info.CaptureIndex(l)
				}
				c.info.Uses[x] = l
				return l.Type
			}
		}
		c.errorf(x.Pos, CodeUnknown, "unknown variable %s", x.Name)
		return TypInvalid
	case syntax.UpperCase:
		return c.memberIdent(f, x)
	}
	c.errorf(x.Pos, CodeCase, "%s mixes upper and lower case; local names are lower case, member names upper case", x.Name)
	return TypInvalid
}

// memberIdent resolves an unqualified member name in the current class.
func (c *checker) memberIdent(f *fnCtx, x *syntax.Ident) Type {
	m := f.class.Member(x.Name)
	if m == nil {
		c.errorf(x.Pos, CodeUnknown, "%s has no member %s", f.class.Name, x.Name)
		return TypInvalid
	}
	c.info.Uses[x] = m
	c.useMember(x.Pos, m)
	switch m := m.(type) {
	case *Attribute:
		if !m.Static && f.static {
			c.errorf(x.Pos, CodeStatic, "the attribute %s needs an instance; static methods have none", x.Name)
		}
		c.visible(f, m.Owner, m.Visibility, x.Name, x.Pos)
		return m.Type
	case *Constant:
		c.visible(f, m.Owner, m.Visibility, x.Name, x.Pos)
		return m.Type
	case *Method:
		c.errorf(x.Pos, CodeType, "the method %s must be called: %s(...)", x.Name, x.Name)
	}
	return TypInvalid
}

// visible checks the visibility of a member.
func (c *checker) visible(f *fnCtx, owner *Class, v syntax.Visibility, name string, pos syntax.Pos) {
	switch v {
	case syntax.Private:
		if f.class != owner {
			c.errorf(pos, CodeVisibility, "%s is private to %s", name, owner.Name)
		}
	case syntax.Package:
		if owner.Package != f.class.Package {
			c.errorf(pos, CodeVisibility, "%s is only visible in the package of %s", name, owner.Name)
		}
	}
}

// global resolves a global name used in an expression: a class, enum or
// other type of the schema.
func (c *checker) global(x *syntax.GlobalExpr) any {
	if k, ok := c.classes[x.Name.Name]; ok {
		c.classVisible(k, x.Name.Pos)
		c.useName(x.Name)
		c.info.Globals[x] = k
		return k
	}
	if !c.visibleNamespace(x.Name) {
		return nil
	}
	if t := c.schema.lookup(x.Name.Name, x.Name.Pos); t != nil {
		c.useName(x.Name)
		c.info.Globals[x] = t
		return t
	}
	c.errorf(x.Name.Pos, CodeUnknown, "unknown class or type %s", x.Name.Name)
	return nil
}

func (c *checker) selectExpr(f *fnCtx, sc *scope, x *syntax.SelectExpr, e env) Type {
	name := x.Name.Name
	if g, ok := x.X.(*syntax.GlobalExpr); ok {
		c.checkGlobalName(g.Name)
		switch t := c.global(g).(type) {
		case *Class:
			m := t.Member(name)
			switch m := m.(type) {
			case *Attribute:
				if !m.Static {
					c.errorf(x.Name.Pos, CodeStatic, "%s is an instance attribute; use an object of %s", name, t.Name)
				}
				c.visible(f, m.Owner, m.Visibility, name, x.Name.Pos)
				c.info.Selects[x] = &Selection{Kind: SelectStatic, Attribute: m}
				c.useMember(x.Name.Pos, m)
				return m.Type
			case *Constant:
				c.visible(f, m.Owner, m.Visibility, name, x.Name.Pos)
				c.info.Selects[x] = &Selection{Kind: SelectConstant, Constant: m}
				c.useMember(x.Name.Pos, m)
				return m.Type
			case *Method:
				c.errorf(x.Name.Pos, CodeType, "the method %s must be called", name)
				return TypInvalid
			}
			c.errorf(x.Name.Pos, CodeUnknown, "%s has no member %s", t.Name, name)
		case *Enum:
			i := t.Index(name)
			if i < 0 {
				c.errorf(x.Name.Pos, CodeUnknown, "%s has no constant %s", t.Name, name)
				return TypInvalid
			}
			c.info.Selects[x] = &Selection{Kind: SelectEnum, Enum: t, EnumIndex: i}
			c.use(x.Name.Pos, "fixed-value", t.Name, name)
			return t
		case nil:
		default:
			c.errorf(x.Name.Pos, CodeType, "%s has no constants", g.Name.Name)
		}
		return TypInvalid
	}
	if _, ok := x.X.(*syntax.SuperExpr); ok {
		c.errorf(x.Name.Pos, CodeType, "super is only used to call methods: super.%s(...)", name)
		return TypInvalid
	}
	t := c.expr(f, sc, x.X, e, nil)
	if t == TypInvalid {
		return TypInvalid
	}
	c.notNull(x.X, t)
	switch r := NonOpt(t).(type) {
	case *Struct:
		i := r.FieldIndex(name)
		if i < 0 {
			c.errorf(x.Name.Pos, CodeUnknown, "%s has no component %s", r.Name, name)
			return TypInvalid
		}
		c.fieldVisible(name, x.Name.Pos)
		c.info.Selects[x] = &Selection{Kind: SelectField, Field: i}
		c.useField(x.Name.Pos, r, name)
		return r.Fields[i].Type
	case *Class:
		switch m := r.Member(name).(type) {
		case *Attribute:
			c.visible(f, m.Owner, m.Visibility, name, x.Name.Pos)
			kind := SelectAttribute
			if m.Static {
				kind = SelectStatic
			}
			c.info.Selects[x] = &Selection{Kind: kind, Attribute: m}
			c.useMember(x.Name.Pos, m)
			return m.Type
		case *Constant:
			c.visible(f, m.Owner, m.Visibility, name, x.Name.Pos)
			c.info.Selects[x] = &Selection{Kind: SelectConstant, Constant: m}
			c.useMember(x.Name.Pos, m)
			return m.Type
		case *Method:
			c.errorf(x.Name.Pos, CodeType, "the method %s must be called: %s(...)", name, name)
			return TypInvalid
		}
		c.errorf(x.Name.Pos, CodeUnknown, "%s has no member %s", r.Name, name)
		return TypInvalid
	}
	c.errorf(x.Name.Pos, CodeUnknown, "%s has no member %s; built-in methods are called with parentheses", t, name)
	return TypInvalid
}

// cond checks a condition and returns what is known when it is true and
// when it is false.
func (c *checker) cond(f *fnCtx, sc *scope, x syntax.Expr, e env) (env, env) {
	switch x := x.(type) {
	case *syntax.ParenExpr:
		t, fl := c.cond(f, sc, x.X, e)
		c.info.Types[x] = TypBool
		return t, fl
	case *syntax.UnaryExpr:
		if x.Op == "not" {
			t, fl := c.cond(f, sc, x.X, e)
			c.info.Types[x] = TypBool
			return fl, t
		}
	case *syntax.BinaryExpr:
		switch x.Op {
		case "and":
			ta, fa := c.cond(f, sc, x.X, e)
			tb, fb := c.cond(f, sc, x.Y, ta)
			c.info.Types[x] = TypBool
			return tb, meet(fa, fb)
		case "or":
			ta, fa := c.cond(f, sc, x.X, e)
			tb, fb := c.cond(f, sc, x.Y, fa)
			c.info.Types[x] = TypBool
			return meet(ta, tb), fb
		}
	case *syntax.IsExpr:
		c.expr(f, sc, x, e, nil)
		t, fl := e.clone(), e.clone()
		if x.What == "null" {
			if p, ok := syntacticPath(x.X); ok {
				v := c.volatile(x.X)
				if x.Not {
					t[p] = v
				} else {
					fl[p] = v
				}
			}
		}
		return t, fl
	}
	t := c.expr(f, sc, x, e, TypBool)
	if !IsKind(t, Bool) && t != TypInvalid {
		c.errorf(x.Start(), CodeType, "the condition is %s, but BOOL is needed", t)
	} else {
		c.notNull(x, t)
	}
	return e.clone(), e.clone()
}

func (c *checker) binary(f *fnCtx, sc *scope, x *syntax.BinaryExpr, e env, want Type) Type {
	switch x.Op {
	case "and", "or":
		t, fl := c.cond(f, sc, x, e)
		e.replace(meet(t, fl))
		return TypBool
	case "default":
		lt := c.expr(f, sc, x.X, e, want)
		if !IsOptional(lt) && lt != TypInvalid {
			c.errorf(x.OpPos, CodeNullCheck, "%s is never null; default is not needed", syntax.PrintExpr(x.X))
		}
		base := NonOpt(lt)
		rt := c.expr(f, sc, x.Y, e, base)
		if lt == TypNull {
			return rt
		}
		if rt == TypNull {
			return Opt(base)
		}
		if !assignable(NonOpt(rt), base) && rt != TypInvalid && base != TypInvalid {
			c.errorf(x.Y.Start(), CodeType, "the default is %s, but %s is needed", rt, base)
			return TypInvalid
		}
		if needsConv(NonOpt(rt), base) {
			c.info.Conv[x.Y] = base
		}
		if IsOptional(rt) {
			return Opt(base)
		}
		return base
	}
	if x.Op == "=" || x.Op == "<>" {
		if isNull(x.X) || isNull(x.Y) {
			c.errorf(x.OpPos, CodeNullCheck, "compare with is null or is not null instead of %s null", x.Op)
			return TypBool
		}
	}
	lt := c.expr(f, sc, x.X, e, nil)
	rt := c.expr(f, sc, x.Y, e, NonOpt(lt))
	if lit, ok := x.X.(*syntax.LiteralExpr); ok && lit.Kind != syntax.KEYWORD {
		lt = c.expr(f, sc, x.X, e, NonOpt(rt))
	}
	c.notNull(x.X, lt)
	c.notNull(x.Y, rt)
	if lt == TypInvalid || rt == TypInvalid {
		return TypInvalid
	}
	lt, rt = NonOpt(lt), NonOpt(rt)
	switch x.Op {
	case "+":
		if IsKind(lt, String) && IsKind(rt, String) {
			return TypString
		}
		if IsKind(lt, String) || IsKind(rt, String) {
			c.errorf(x.OpPos, CodeType, "+ joins two strings; show other values with a placeholder \"{x}\"")
			return TypInvalid
		}
		fallthrough
	case "-", "*", "/", "mod":
		res, ok := c.arith(x, lt, rt)
		if !ok {
			c.errorf(x.OpPos, CodeType, "%s needs numbers, not %s and %s", x.Op, lt, rt)
			return TypInvalid
		}
		return res
	case "=", "<>", "<", ">", "<=", ">=":
		c.compare(x, lt, rt)
		return TypBool
	}
	return TypInvalid
}

func isNull(x syntax.Expr) bool {
	l, ok := x.(*syntax.LiteralExpr)
	return ok && l.Raw == "null"
}

// arith types arithmetic and records the conversions of the operands to a
// common type.
func (c *checker) arith(x *syntax.BinaryExpr, lt, rt Type) (Type, bool) {
	a, b := BasicOf(lt), BasicOf(rt)
	if a == nil || b == nil || !IsNumeric(a) || !IsNumeric(b) {
		return nil, false
	}
	if _, ok := lt.(*Enum); ok {
		return nil, false
	}
	if _, ok := rt.(*Enum); ok {
		return nil, false
	}
	if a.Kind != Dec && b.Kind != Dec {
		if a.Kind == Int4 && b.Kind == Int4 {
			c.convTo(x.X, lt, TypInt4)
			c.convTo(x.Y, rt, TypInt4)
			return TypInt4, true
		}
		c.convTo(x.X, lt, TypInt8)
		c.convTo(x.Y, rt, TypInt8)
		return TypInt8, true
	}
	toDec := func(t *Basic, operand syntax.Expr, src Type) *Basic {
		switch t.Kind {
		case Int4:
			d := DecType(10, 0)
			c.info.Conv[operand] = d
			return d
		case Int8:
			d := DecType(19, 0)
			c.info.Conv[operand] = d
			return d
		}
		if _, named := src.(*Named); named {
			c.info.Conv[operand] = t
		}
		return t
	}
	a, b = toDec(a, x.X, lt), toDec(b, x.Y, rt)
	ia, ib := a.Precision-a.Scale, b.Precision-b.Scale
	switch x.Op {
	case "+", "-":
		s := max(a.Scale, b.Scale)
		return DecType(min(MaxPrecision, max(ia, ib)+1+s), s), true
	case "*":
		s := a.Scale + b.Scale
		if s > MaxPrecision {
			c.errorf(x.OpPos, CodeType, "the product has more than %d decimals; reduce the scale with as first", MaxPrecision)
			return TypInvalid, true
		}
		return DecType(min(MaxPrecision, a.Precision+b.Precision), s), true
	case "/":
		return DecType(MaxPrecision, max(a.Scale, b.Scale, 6)), true
	case "mod":
		s := max(a.Scale, b.Scale)
		return DecType(min(MaxPrecision, max(ia, ib)+s), s), true
	}
	return nil, false
}

// convTo records a conversion of an operand when its representation
// differs.
func (c *checker) convTo(x syntax.Expr, from Type, to *Basic) {
	if needsConv(from, to) {
		c.info.Conv[x] = to
	}
}

// compare checks the operand types of a comparison.
func (c *checker) compare(x *syntax.BinaryExpr, lt, rt Type) {
	ordered := x.Op != "=" && x.Op != "<>"
	if IsNumeric(lt) && IsNumeric(rt) {
		if _, ok := lt.(*Enum); !ok {
			if _, ok := rt.(*Enum); !ok {
				c.arith(&syntax.BinaryExpr{X: x.X, Y: x.Y, Op: "-", OpPos: x.OpPos}, lt, rt)
				return
			}
		}
	}
	ok := false
	switch l := lt.(type) {
	case *Enum:
		switch {
		case lt != rt:
			c.errorf(x.OpPos, CodeType, "%s cannot compare %s and %s", x.Op, lt, rt)
		case ordered:
			c.errorf(x.OpPos, CodeType, "enum constants are compared with = and <> only")
		}
		return
	case *Class:
		r, isClass := rt.(*Class)
		ok = isClass && (l.Extends(r) || r.Extends(l))
		if ordered {
			ok = false
		}
	case *Struct, *Array, *Map:
		ok = Identical(lt, rt) && !ordered
	default:
		lb, rb := BasicOf(lt), BasicOf(rt)
		if lb != nil && rb != nil && lb.Kind == rb.Kind {
			switch lb.Kind {
			case String, Date, Timestamp:
				ok = true
			case Bool, UUID, Bytes:
				ok = !ordered
			}
		}
	}
	if !ok {
		c.errorf(x.OpPos, CodeType, "%s cannot compare %s and %s", x.Op, lt, rt)
	}
}

func (c *checker) funcExpr(f *fnCtx, sc *scope, x *syntax.FuncExpr, e env, want Type) Type {
	ft := &Func{Result: TypVoid}
	inner := &fnCtx{info: &FuncInfo{Type: ft, Parent: f.info}, class: f.class, method: f.method, static: f.static, closure: true, parent: f, scope: sc}
	isc := &scope{vars: map[string]*Local{}, fn: inner}
	for _, p := range x.Params {
		c.checkLocalName(p.Name, "parameter")
		pt := c.resolveType(p.Type)
		ft.Params = append(ft.Params, pt)
		if sc.lookup(p.Name.Name) != nil {
			c.errorf(p.Name.Pos, CodeDuplicate, "%s is already declared", p.Name.Name)
		}
		isc.vars[p.Name.Name] = inner.info.newLocal(p.Name.Name, pt, true)
	}
	ft.Raises = c.raisesList(x.Raises)
	inner.raises = ft.Raises
	c.info.Funcs[x] = inner.info
	if x.Returns != nil {
		ft.Result = c.resolveType(x.Returns)
	}
	if x.Body == nil {
		var w Type
		if x.Returns != nil {
			w = ft.Result
		} else if wf, ok := NonOpt(want).(*Func); ok {
			w = wf.Result
		}
		t := c.expr(inner, isc, x.Short, env{}, w)
		if x.Returns == nil {
			if t == TypNull {
				c.errorf(x.Short.Start(), CodeType, "the result type cannot be derived from null; add returns T?")
			}
			ft.Result = t
			if w != nil && w != TypVoid && assignable(NonOpt(t), NonOpt(w)) && (IsOptional(w) || !IsOptional(t)) {
				ft.Result = w
				c.assign(x.Short, t, w, "the result")
			}
		} else {
			c.assign(x.Short, t, ft.Result, "the result")
		}
		return ft
	}
	inner.result = ft.Result
	if !c.block(inner, isc, x.Body.Stmts, env{}) && ft.Result != TypVoid {
		c.errorf(x.Body.End.Pos, CodeReturn, "the function must return a %s at the end", ft.Result)
	}
	return ft
}

func (c *checker) newExpr(f *fnCtx, sc *scope, x *syntax.NewExpr, e env) Type {
	if x.Type == nil {
		return TypInvalid
	}
	if !x.Call {
		return c.resolveType(x.Type)
	}
	n := syntax.Name{Pos: x.Type.Pos, Name: x.Type.Name}
	c.checkGlobalName(n)
	t := c.globalType(n)
	c.info.TypeExprs[x.Type] = t
	switch t := t.(type) {
	case *Class:
		switch {
		case t.IsInterface():
			c.errorf(n.Pos, CodeType, "%s is an interface; create an object of a class implementing it", t.Name)
		case t.Test && !f.class.Test:
			c.errorf(n.Pos, CodeTest, "test classes are only used by test classes")
		case t.IsRuntimeError() && !f.class.Kernel:
			c.errorf(n.Pos, CodeException, "only the kernel creates runtime errors")
		}
		sig := &Func{Result: TypVoid}
		if t.Ctor != nil {
			sig = t.Ctor.Sig
			c.visible(f, t, t.Ctor.Visibility, "the constructor", n.Pos)
		} else if b := t.baseCtor(); b != nil {
			sig = b.Sig
		}
		call := &syntax.CallExpr{Fun: &syntax.GlobalExpr{Name: n}, Args: x.Args, List: x.List}
		c.args(f, sc, call, sig, e, nil)
		for _, r := range sig.Raises {
			c.raise(f, r, n.Pos)
		}
		e.call()
		return t
	case *Struct:
		seen := map[string]bool{}
		for _, a := range x.Args {
			if a.Name == nil {
				c.errorf(a.Value.Start(), CodeType, "components of %s are given by name: COMPONENT: value", t.Name)
				c.expr(f, sc, a.Value, e, nil)
				continue
			}
			fld := t.Field(a.Name.Name)
			c.fieldVisible(a.Name.Name, a.Name.Pos)
			if fld != nil {
				c.useField(a.Name.Pos, t, fld.Name)
			}
			if fld == nil {
				c.errorf(a.Name.Pos, CodeUnknown, "%s has no component %s", t.Name, a.Name.Name)
				c.expr(f, sc, a.Value, e, nil)
				continue
			}
			if seen[fld.Name] {
				c.errorf(a.Name.Pos, CodeDuplicate, "%s is given twice", fld.Name)
			}
			seen[fld.Name] = true
			vt := c.expr(f, sc, a.Value, e, fld.Type)
			c.assign(a.Value, vt, fld.Type, fld.Name)
		}
		for _, fld := range t.Fields {
			if !seen[fld.Name] && !IsOptional(fld.Type) {
				c.errorf(n.Pos, CodeNullCheck, "%s is not optional and must be given", fld.Name)
			}
		}
		return t
	case nil:
		for _, a := range x.Args {
			c.expr(f, sc, a.Value, e, nil)
		}
		return TypInvalid
	}
	c.errorf(n.Pos, CodeType, "new creates objects of classes and values of structures, not %s", t)
	return TypInvalid
}

// args checks the arguments of a call against a signature. named allows
// named arguments and lists their allowed names.
func (c *checker) args(f *fnCtx, sc *scope, call *syntax.CallExpr, sig *Func, e env, named map[string]Type) {
	if named != nil {
		for _, a := range call.Args {
			if a.Name == nil {
				c.errorf(a.Value.Start(), CodeType, "arguments are given by name here")
				c.expr(f, sc, a.Value, e, nil)
				continue
			}
			pt, ok := named[a.Name.Name]
			if !ok {
				c.errorf(a.Name.Pos, CodeUnknown, "unknown argument %s", a.Name.Name)
				c.expr(f, sc, a.Value, e, nil)
				continue
			}
			t := c.expr(f, sc, a.Value, e, pt)
			c.assign(a.Value, t, pt, "argument "+a.Name.Name)
		}
		return
	}
	for i, a := range call.Args {
		if a.Name != nil {
			c.errorf(a.Name.Pos, CodeType, "arguments are given by position, not by name")
		}
		var pt Type
		if i < len(sig.Params) {
			pt = sig.Params[i]
		}
		t := c.expr(f, sc, a.Value, e, pt)
		if pt != nil {
			c.assign(a.Value, t, pt, "argument "+itoa(i+1))
		}
	}
	if len(call.Args) != len(sig.Params) {
		c.errorf(call.Fun.Start(), CodeType, "%d arguments given, %d expected", len(call.Args), len(sig.Params))
	}
}

func itoa(i int) string { return big.NewInt(int64(i)).String() }

func argValues(call *syntax.CallExpr) []syntax.Expr {
	out := make([]syntax.Expr, len(call.Args))
	for i, a := range call.Args {
		out[i] = a.Value
	}
	return out
}

// call checks a call of a method, a function value or a built-in method.
func (c *checker) call(f *fnCtx, sc *scope, x *syntax.CallExpr, e env, want Type) Type {
	switch fun := x.Fun.(type) {
	case *syntax.SuperExpr:
		c.errorf(fun.Pos, CodeInherit, "super(...) is only allowed as the first statement of a constructor")
		return TypVoid
	case *syntax.Ident:
		if syntax.CaseOf(fun.Name) == syntax.UpperCase {
			m, ok := f.class.Member(fun.Name).(*Method)
			if !ok {
				if f.class.Member(fun.Name) == nil {
					c.errorf(fun.Pos, CodeUnknown, "%s has no method %s", f.class.Name, fun.Name)
				} else {
					c.errorf(fun.Pos, CodeType, "%s is not a method", fun.Name)
				}
				c.args(f, sc, x, &Func{}, e, nil)
				return TypInvalid
			}
			c.info.Uses[fun] = m
			if !m.Static && f.static {
				c.errorf(fun.Pos, CodeStatic, "%s needs an instance; static methods have none", fun.Name)
			}
			return c.methodCall(f, sc, x, m, nil, e, false)
		}
	case *syntax.SelectExpr:
		name := fun.Name.Name
		if id, ok := fun.X.(*syntax.Ident); ok && id.Name == "UUID" && sc.lookup(id.Name) == nil && f.class.Member(id.Name) == nil {
			// UUID.RANDOM() returns a random UUID (version 4).
			if name != "RANDOM" {
				c.errorf(fun.Name.Pos, CodeUnknown, "UUID has no method %s", name)
				c.args(f, sc, x, &Func{}, e, nil)
				return TypInvalid
			}
			c.args(f, sc, x, &Func{}, e, nil)
			c.info.Calls[x] = &Call{Kind: CallBuiltin, Builtin: &Builtin{ID: UUIDRandom, Name: name}, Args: argValues(x)}
			c.info.Selects[fun] = &Selection{Kind: SelectBuiltin}
			return TypUUID
		}
		if g, ok := fun.X.(*syntax.GlobalExpr); ok {
			c.checkGlobalName(g.Name)
			switch t := c.global(g).(type) {
			case *Class:
				m, ok := t.Member(name).(*Method)
				if !ok {
					c.errorf(fun.Name.Pos, CodeUnknown, "%s has no method %s", t.Name, name)
					c.args(f, sc, x, &Func{}, e, nil)
					return TypInvalid
				}
				if !m.Static {
					c.errorf(fun.Name.Pos, CodeStatic, "%s is an instance method; call it on an object of %s", name, t.Name)
				}
				if t.Test && !f.class.Test {
					c.errorf(g.Name.Pos, CodeTest, "test classes are only used by test classes")
				}
				c.info.Selects[fun] = &Selection{Kind: SelectMethod, Method: m}
				return c.methodCall(f, sc, x, m, nil, e, false)
			case *Enum:
				return c.builtinCall(f, sc, x, nil, t, e, want)
			case nil:
				c.args(f, sc, x, &Func{}, e, nil)
				return TypInvalid
			}
			c.errorf(fun.Name.Pos, CodeType, "%s has no methods", g.Name.Name)
			return TypInvalid
		}
		if _, ok := fun.X.(*syntax.SuperExpr); ok {
			if f.static || f.class.Base == nil {
				c.errorf(fun.X.Start(), CodeInherit, "super needs a base class and an instance")
				return TypInvalid
			}
			m, ok := f.class.Base.Member(name).(*Method)
			if !ok || m.Abstract() {
				c.errorf(fun.Name.Pos, CodeUnknown, "%s has no method %s", f.class.Base.Name, name)
				return TypInvalid
			}
			c.info.Selects[fun] = &Selection{Kind: SelectMethod, Method: m}
			return c.methodCall(f, sc, x, m, nil, e, true)
		}
		rt := c.expr(f, sc, fun.X, e, nil)
		if rt == TypInvalid {
			c.args(f, sc, x, &Func{}, e, nil)
			return TypInvalid
		}
		c.notNull(fun.X, rt)
		if k, ok := NonOpt(rt).(*Class); ok {
			m, ok := k.Member(name).(*Method)
			if !ok {
				c.errorf(fun.Name.Pos, CodeUnknown, "%s has no method %s", k.Name, name)
				c.args(f, sc, x, &Func{}, e, nil)
				return TypInvalid
			}
			c.info.Selects[fun] = &Selection{Kind: SelectMethod, Method: m}
			return c.methodCall(f, sc, x, m, fun.X, e, false)
		}
		return c.builtinCall(f, sc, x, fun.X, NonOpt(rt), e, want)
	}
	// A function value.
	ft := c.expr(f, sc, x.Fun, e, nil)
	c.notNull(x.Fun, ft)
	fn, ok := NonOpt(ft).(*Func)
	if !ok {
		if ft != TypInvalid {
			c.errorf(x.Fun.Start(), CodeType, "%s is not a function", syntax.PrintExpr(x.Fun))
		}
		c.args(f, sc, x, &Func{}, e, nil)
		return TypInvalid
	}
	c.args(f, sc, x, fn, e, nil)
	for _, r := range fn.Raises {
		c.raise(f, r, x.Fun.Start())
	}
	c.info.Calls[x] = &Call{Kind: CallClosure, Args: argValues(x)}
	e.call()
	return fn.Result
}

// methodCall checks a call of a method on recv (nil: me or static).
func (c *checker) methodCall(f *fnCtx, sc *scope, x *syntax.CallExpr, m *Method, recv syntax.Expr, e env, super bool) Type {
	pos := x.Fun.Start()
	if sel, ok := x.Fun.(*syntax.SelectExpr); ok {
		pos = sel.Name.Pos
	}
	c.useMember(pos, m)
	c.visible(f, m.Owner, m.Visibility, m.Name, pos)
	c.args(f, sc, x, m.Sig, e, nil)
	for _, r := range m.Sig.Raises {
		c.raise(f, r, pos)
	}
	kind := CallStatic
	switch {
	case super:
		kind = CallSuper
	case m.Static:
	case m.Owner.IsInterface():
		kind = CallInterface
	case m.Open || m.Override:
		kind = CallVirtual
	}
	if m.Test && !f.class.Test {
		c.errorf(pos, CodeTest, "test methods are run by rt test")
	}
	c.info.Calls[x] = &Call{Kind: kind, Method: m, Receiver: recv, Args: argValues(x)}
	e.call()
	return m.Sig.Result
}

// ownPackage reports whether a name belongs to the package of the program.
func (c *checker) ownPackage(name string) bool {
	id, _, _ := model.SplitName(name)
	return id == c.cfg.Package
}
