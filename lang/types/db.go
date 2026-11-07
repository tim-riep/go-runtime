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
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// FieldRef is a field of a table in a where condition, a set or an order.
type FieldRef struct {
	Table *Struct
	Field string
}

// sessionKey identifies the session of a database statement for the rule
// that a cursor must not be ended by commit or rollback of its session:
// "" is the default session, otherwise the name of the variable.
func sessionKey(using syntax.Expr) (string, bool) {
	if using == nil {
		return "", true
	}
	if id, ok := using.(*syntax.Ident); ok {
		return id.Name, true
	}
	return "", false
}

// table resolves the table of a database statement.
func (c *checker) table(n syntax.Name) *Struct {
	c.checkGlobalName(n)
	if !c.visibleNamespace(n) {
		return nil
	}
	if c.schema.s.Tables[n.Name] == nil {
		c.errorf(n.Pos, CodeUnknown, "%s is not a table", n.Name)
		return nil
	}
	c.use(n.Pos, "table", n.Name, "")
	return c.schema.structType(n.Name, n.Pos)
}

// session checks the session of using.
func (c *checker) session(f *fnCtx, sc *scope, x syntax.Expr, e env) {
	if x == nil {
		return
	}
	t := c.expr(f, sc, x, e, TypSession)
	if !IsKind(t, Session) && t != TypInvalid {
		c.errorf(x.Start(), CodeType, "using needs a SESSION, not %s", t)
	} else {
		c.notNull(x, t)
	}
}

// field resolves a field of a table written in a where condition, a set
// or an order: an upper-case name or the global name of an extension
// field.
func (c *checker) field(st *Struct, x syntax.Expr) (Type, bool) {
	var name string
	var pos syntax.Pos
	switch x := x.(type) {
	case *syntax.Ident:
		if syntax.CaseOf(x.Name) != syntax.UpperCase && !strings.HasPrefix(x.Name, "/") {
			return nil, false
		}
		name, pos = x.Name, x.Pos
	case *syntax.GlobalExpr:
		name, pos = x.Name.Name, x.Name.Pos
	default:
		return nil, false
	}
	fld := st.Field(name)
	if fld == nil {
		c.errorf(pos, CodeUnknown, "%s has no field %s", st.Name, name)
		c.info.Types[x] = TypInvalid
		return TypInvalid, true
	}
	c.fieldVisible(name, pos)
	c.useField(pos, st, name)
	c.info.Fields[x] = &FieldRef{Table: st, Field: name}
	c.info.Types[x] = fld.Type
	return fld.Type, true
}

// isFieldExpr reports names that are fields in a condition.
func isFieldExpr(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Ident:
		return syntax.CaseOf(x.Name) == syntax.UpperCase
	case *syntax.GlobalExpr:
		return true
	case *syntax.ParenExpr:
		return isFieldExpr(x.X)
	}
	return false
}

// where checks a condition on the fields of a table. Every comparison
// relates a field to a value of the code or to another field.
func (c *checker) where(f *fnCtx, sc *scope, st *Struct, x syntax.Expr, e env) {
	c.info.Types[x] = TypBool
	switch x := x.(type) {
	case *syntax.ParenExpr:
		c.where(f, sc, st, x.X, e)
		return
	case *syntax.UnaryExpr:
		if x.Op == "not" {
			c.where(f, sc, st, x.X, e)
			return
		}
	case *syntax.BinaryExpr:
		switch x.Op {
		case "and", "or":
			c.where(f, sc, st, x.X, e)
			c.where(f, sc, st, x.Y, e)
			return
		case "=", "<>", "<", ">", "<=", ">=", "in":
			c.comparison(f, sc, st, x, e)
			return
		}
	case *syntax.IsExpr:
		ft, ok := c.field(st, unparen(x.X))
		if !ok {
			c.errorf(x.X.Start(), CodeType, "is %s in a where condition applies to a field of %s", x.What, st.Name)
			return
		}
		if x.What == "empty" && !IsKind(NonOpt(ft), String) && ft != TypInvalid {
			c.errorf(x.OpPos, CodeType, "is empty applies to STRING fields, not %s", ft)
		}
		c.info.Types[x.X] = ft
		return
	}
	c.errorf(x.Start(), CodeType, "a where condition compares fields of %s with =, <>, <, >, <=, >=, in, is null and is empty, joined by and, or and not", st.Name)
}

func unparen(x syntax.Expr) syntax.Expr {
	for {
		p, ok := x.(*syntax.ParenExpr)
		if !ok {
			return x
		}
		x = p.X
	}
}

// comparison checks field op value, value op field or field op field.
func (c *checker) comparison(f *fnCtx, sc *scope, st *Struct, x *syntax.BinaryExpr, e env) {
	lf, rf := isFieldExpr(x.X), isFieldExpr(x.Y)
	if x.Op == "in" {
		lt, ok := c.field(st, unparen(x.X))
		if !ok {
			c.errorf(x.X.Start(), CodeType, "in needs a field of %s on its left side", st.Name)
			return
		}
		at := c.expr(f, sc, x.Y, e, &Array{Elem: NonOpt(lt)})
		c.notNull(x.Y, at)
		arr, ok := NonOpt(at).(*Array)
		if !ok || !assignable(NonOpt(arr.Elem), NonOpt(lt)) || IsOptional(arr.Elem) {
			if at != TypInvalid {
				c.errorf(x.Y.Start(), CodeType, "in needs an array of %s, not %s", NonOpt(lt), at)
			}
		}
		return
	}
	var lt, rt Type
	switch {
	case lf:
		lt, _ = c.field(st, unparen(x.X))
	default:
		lt = nil
	}
	if rf {
		rt, _ = c.field(st, unparen(x.Y))
	}
	if !lf && !rf {
		c.errorf(x.OpPos, CodeType, "the comparison needs a field of %s on one side", st.Name)
		return
	}
	if !lf {
		lt = c.expr(f, sc, x.X, e, NonOpt(rt))
		c.notNull(x.X, lt)
	}
	if !rf {
		if isNull(x.Y) {
			c.errorf(x.OpPos, CodeNullCheck, "compare with is null or is not null instead of %s null", x.Op)
			return
		}
		rt = c.expr(f, sc, x.Y, e, NonOpt(lt))
		c.notNull(x.Y, rt)
	}
	if lt == TypInvalid || rt == TypInvalid {
		return
	}
	// Fields may be null; a comparison with null is never true in the
	// database.
	c.compare(x, NonOpt(lt), NonOpt(rt))
	if !lf {
		c.convertValue(x.X, lt, NonOpt(rt))
	}
	if !rf {
		c.convertValue(x.Y, rt, NonOpt(lt))
	}
}

// convertValue records the conversion of a value to the type of the field
// it is compared with or stored in.
func (c *checker) convertValue(x syntax.Expr, from, to Type) {
	if assignable(NonOpt(from), NonOpt(to)) && needsConv(NonOpt(from), NonOpt(to)) {
		if _, named := to.(*Named); !named {
			c.info.Conv[x] = to
		}
	}
}

// query checks select.
func (c *checker) query(f *fnCtx, sc *scope, x *syntax.QueryExpr, e env) Type {
	st := c.table(x.Table)
	if x.Where != nil && st != nil {
		c.where(f, sc, st, x.Where, e)
	}
	if st != nil {
		for _, o := range x.Order {
			ft, _ := c.field(st, &o.Field)
			if ft != nil && ft != TypInvalid && !ordered(NonOpt(ft)) && !IsKind(NonOpt(ft), Bool) {
				c.errorf(o.Field.Pos, CodeType, "order by needs a field of a type that can be ordered, not %s", ft)
			}
		}
	}
	if x.Limit != nil {
		lt := c.expr(f, sc, x.Limit, e, TypInt8)
		c.assign(x.Limit, lt, TypInt8, "the limit")
	}
	c.session(f, sc, x.Using, e)
	if st == nil {
		return TypInvalid
	}
	c.info.Tables[x] = st
	switch x.Mode {
	case "single":
		if len(x.Order) > 0 || x.Limit != nil {
			c.errorf(x.Pos, CodeType, "select single reads one row and takes no order by or limit")
		}
		return Opt(st)
	case "count":
		if len(x.Order) > 0 {
			c.errorf(x.Pos, CodeType, "select count takes no order by")
		}
		return TypInt8
	}
	return &Array{Elem: st}
}

// update checks update /PKG/T set ... and delete from /PKG/T ....
func (c *checker) update(f *fnCtx, sc *scope, x *syntax.UpdateExpr, e env) Type {
	st := c.table(x.Table)
	if st != nil {
		seen := map[string]bool{}
		for _, s := range x.Sets {
			ft, _ := c.field(st, &s.Field)
			if seen[s.Field.Name] {
				c.errorf(s.Field.Pos, CodeDuplicate, "%s is set twice", s.Field.Name)
			}
			seen[s.Field.Name] = true
			vt := c.expr(f, sc, s.Value, e, ft)
			if ft != nil {
				c.assign(s.Value, vt, ft, "the value of "+s.Field.Name)
			}
		}
		if x.Where != nil {
			c.where(f, sc, st, x.Where, e)
		}
		c.info.Tables[x] = st
	}
	c.session(f, sc, x.Using, e)
	return TypInt8
}

func (c *checker) delete(f *fnCtx, sc *scope, x *syntax.DeleteExpr, e env) Type {
	st := c.table(x.Table)
	if st != nil {
		if x.Where != nil {
			c.where(f, sc, st, x.Where, e)
		}
		c.info.Tables[x] = st
	}
	c.session(f, sc, x.Using, e)
	return TypInt8
}

// rowStmt checks insert, update and delete of a row.
func (c *checker) rowStmt(f *fnCtx, sc *scope, s *syntax.RowStmt, e env) {
	t := c.expr(f, sc, s.Row, e, nil)
	c.notNull(s.Row, t)
	st, ok := NonOpt(t).(*Struct)
	if !ok || !st.Table {
		if t != TypInvalid {
			c.errorf(s.Row.Start(), CodeType, "%s needs a row of a table, not %s", s.Op, t)
		}
	} else {
		if !c.visiblePackage(st.Name) {
			c.visibleNamespace(syntax.Name{Pos: s.Row.Start(), Name: st.Name})
		}
		c.use(s.Row.Start(), "table", st.Name, "")
		c.info.RowTables[s] = st
	}
	c.session(f, sc, s.Using, e)
}

// sessionStmt checks commit and rollback.
func (c *checker) sessionStmt(f *fnCtx, sc *scope, s *syntax.DatabaseStmt, e env) {
	c.session(f, sc, s.Session, e)
	key, ok := sessionKey(s.Session)
	if !ok {
		return
	}
	for _, open := range f.cursors {
		if open == key {
			what := "the default session"
			if key != "" {
				what = "session " + key
			}
			c.errorf(s.Pos, CodeStatement, "%s would end the open cursor of the enclosing for ... in select on %s", s.Keyword, what)
			return
		}
	}
}
