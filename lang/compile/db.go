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

package compile

import (
	"slices"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
)

// query collects a database statement while the code pushes its
// parameters.
type query struct {
	f *fn
	q bytecode.Query
}

func (f *fn) newQuery(kind QueryKind, st *types.Struct) *query {
	return &query{f: f, q: bytecode.Query{Kind: kind, Table: st.Name, Row: f.c.typeDesc(st), Where: -1}}
}

// param pushes a value of the code and records its type.
func (q *query) param(x syntax.Expr) int {
	q.f.expr(x)
	q.q.Params = append(q.q.Params, q.f.c.typeDesc(types.NonOpt(q.f.operandType(x))))
	return len(q.q.Params) - 1
}

func (q *query) add(c bytecode.Cond) int {
	q.q.Conds = append(q.q.Conds, c)
	return len(q.q.Conds) - 1
}

func (q *query) fieldName(x syntax.Expr) (string, bool) {
	for {
		p, ok := x.(*syntax.ParenExpr)
		if !ok {
			break
		}
		x = p.X
	}
	if fr, ok := q.f.c.info.Fields[x]; ok {
		return fr.Field, true
	}
	return "", false
}

var condOps = map[string]CondOp{"=": CEq, "<>": CNe, "<": CLt, "<=": CLe, ">": CGt, ">=": CGe}

// mirrored turns value op FIELD into FIELD op value.
var mirrored = map[CondOp]CondOp{CEq: CEq, CNe: CNe, CLt: CGt, CLe: CGe, CGt: CLt, CGe: CLe}

// cond translates a where condition; values of the code are pushed in the
// order of the condition.
func (q *query) cond(x syntax.Expr) int {
	switch x := x.(type) {
	case *syntax.ParenExpr:
		return q.cond(x.X)
	case *syntax.UnaryExpr:
		k := q.cond(x.X)
		return q.add(bytecode.Cond{Op: CNot, Kids: []int{k}, Param: -1})
	case *syntax.IsExpr:
		name, _ := q.fieldName(x.X)
		op := CIsNull
		if x.What == "empty" {
			op = CIsEmpty
		}
		n := q.add(bytecode.Cond{Op: op, Field: name, Param: -1})
		if x.Not {
			n = q.add(bytecode.Cond{Op: CNot, Kids: []int{n}, Param: -1})
		}
		return n
	case *syntax.BinaryExpr:
		switch x.Op {
		case "and", "or":
			op := CAnd
			if x.Op == "or" {
				op = COr
			}
			a := q.cond(x.X)
			b := q.cond(x.Y)
			return q.add(bytecode.Cond{Op: op, Kids: []int{a, b}, Param: -1})
		case "in":
			name, _ := q.fieldName(x.X)
			p := q.param(x.Y)
			return q.add(bytecode.Cond{Op: CIn, Field: name, Param: p})
		}
		op := condOps[x.Op]
		lf, lok := q.fieldName(x.X)
		rf, rok := q.fieldName(x.Y)
		switch {
		case lok && rok:
			return q.add(bytecode.Cond{Op: op, Field: lf, Other: rf, Param: -1})
		case lok:
			p := q.param(x.Y)
			return q.add(bytecode.Cond{Op: op, Field: lf, Param: p})
		default:
			p := q.param(x.X)
			return q.add(bytecode.Cond{Op: mirrored[op], Field: rf, Param: p})
		}
	}
	q.f.c.failf("unexpected condition %T", x)
	return -1
}

func (q *query) where(x syntax.Expr) {
	if x != nil {
		q.q.Where = q.cond(x)
	}
}

// session pushes the session of using, null for the default session.
func (f *fn) session(x syntax.Expr) {
	if x == nil {
		f.emit(NULL)
		return
	}
	f.expr(x)
}

// finish adds the statement to the program and returns its index.
func (q *query) finish() int {
	q.f.c.out.Queries = append(q.f.c.out.Queries, q.q)
	return len(q.f.c.out.Queries) - 1
}

// selectQuery pushes the parameters, the limit and the session of select.
func (f *fn) selectQuery(x *syntax.QueryExpr) int {
	st := f.c.info.Tables[x]
	kind := QSelect
	switch x.Mode {
	case "single":
		kind = QSingle
	case "count":
		kind = QCount
	}
	q := f.newQuery(kind, st)
	q.where(x.Where)
	for _, o := range x.Order {
		name := o.Field.Name
		if fr, ok := f.c.info.Fields[&o.Field]; ok {
			name = fr.Field
		}
		q.q.Order = append(q.q.Order, bytecode.OrderKey{Field: name, Descending: o.Descending})
	}
	if x.Limit != nil {
		f.expr(x.Limit)
		q.q.Limit = true
	}
	f.session(x.Using)
	return q.finish()
}

func (f *fn) dbExpr(x syntax.Expr) {
	info := f.c.info
	switch x := x.(type) {
	case *syntax.QueryExpr:
		f.emit(DB_QUERY, f.selectQuery(x))
	case *syntax.UpdateExpr:
		q := f.newQuery(QUpdateWhere, info.Tables[x])
		for _, s := range x.Sets {
			q.q.Sets = append(q.q.Sets, s.Field.Name)
			f.valueOwn(s.Value)
			q.q.Params = append(q.q.Params, f.c.typeDesc(types.NonOpt(f.operandType(s.Value))))
		}
		q.where(x.Where)
		f.session(x.Using)
		f.emit(DB_QUERY, q.finish())
	case *syntax.DeleteExpr:
		q := f.newQuery(QDeleteWhere, info.Tables[x])
		q.where(x.Where)
		f.session(x.Using)
		f.emit(DB_QUERY, q.finish())
	case *syntax.OpenSessionExpr:
		f.emit(DB_OPEN)
	}
}

// rowStmt is insert, update or delete of a row.
func (f *fn) rowStmt(s *syntax.RowStmt) {
	st := f.c.info.RowTables[s]
	kind := map[string]QueryKind{"insert": QInsert, "update": QUpdateRow, "delete": QDeleteRow}[s.Op]
	q := f.newQuery(kind, st)
	q.q.Key = slices.Clone(st.Key)
	f.expr(s.Row)
	f.session(s.Using)
	f.emit(DB_QUERY, q.finish())
}

// sessionStmt is commit or rollback.
func (f *fn) sessionStmt(s *syntax.DatabaseStmt) {
	f.session(s.Session)
	rollback := 0
	if s.Keyword == "rollback" {
		rollback = 1
	}
	f.emit(DB_END, rollback)
}

// cursorLoop is for ... in select: the rows come one by one from a cursor,
// which is closed when the loop ends or is left with break.
func (f *fn) cursorLoop(s *syntax.ForStmt) {
	vars := f.c.info.Vars[s]
	cur, idx := f.temp(), f.temp()
	f.emit(DB_CURSOR, f.selectQuery(s.In.(*syntax.QueryExpr)))
	f.emit(STORE, cur)
	f.emit(CONST_I, 0)
	f.emit(STORE, idx)
	top := len(f.code)
	f.emit(LOAD, cur)
	exit := f.jump(DB_NEXT)
	f.storeLocal(vars[len(vars)-1], true)
	if len(vars) == 2 {
		f.emit(LOAD, idx)
		f.emit(CONST_I, 1)
		f.emit(ADD_I8)
		f.emit(DUP)
		f.emit(STORE, idx)
		f.storeLocal(vars[0], true)
	}
	l := f.pushLoop()
	f.stmts(s.Body.Stmts)
	f.emit(JUMP, top)
	f.popLoop(l, top)
	// break leaves the loop here
	f.emit(LOAD, cur)
	f.emit(DB_CLOSE)
	f.patch(exit)
}

// transform runs a transformation: the source, then the values of the
// parameters.
func (f *fn) transform(x *syntax.TransformExpr) {
	t := f.c.info.Transforms[x]
	f.expr(x.Source)
	bt := bytecode.Transform{Name: t.Name, Params: t.Params, Source: f.c.typeDesc(t.Source), SourceXML: t.SourceXML, Into: f.c.typeDesc(t.Into)}
	for _, pv := range x.Params {
		f.expr(pv.Value)
		bt.ParamTypes = append(bt.ParamTypes, f.c.typeDesc(f.operandType(pv.Value)))
	}
	f.c.out.Transforms = append(f.c.out.Transforms, bt)
	f.line(x.Pos)
	f.emit(XFORM, len(f.c.out.Transforms)-1)
}
