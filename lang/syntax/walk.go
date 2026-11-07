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

package syntax

// Inspect calls fn for x and every expression inside it, parents first.
// It does not enter the statements of function bodies.
func Inspect(x Expr, fn func(Expr)) {
	if x == nil {
		return
	}
	fn(x)
	switch x := x.(type) {
	case *StringExpr:
		for _, p := range x.Parts {
			Inspect(p.Expr, fn)
		}
	case *ParenExpr:
		Inspect(x.X, fn)
	case *UnaryExpr:
		Inspect(x.X, fn)
	case *BinaryExpr:
		Inspect(x.X, fn)
		Inspect(x.Y, fn)
	case *IsExpr:
		Inspect(x.X, fn)
	case *AsExpr:
		Inspect(x.X, fn)
	case *CallExpr:
		Inspect(x.Fun, fn)
		for _, a := range x.Args {
			Inspect(a.Value, fn)
		}
	case *SelectExpr:
		Inspect(x.X, fn)
	case *IndexExpr:
		Inspect(x.X, fn)
		Inspect(x.Index, fn)
	case *NewExpr:
		for _, a := range x.Args {
			Inspect(a.Value, fn)
		}
	case *ArrayExpr:
		for _, e := range x.Elems {
			Inspect(e, fn)
		}
	case *MapExpr:
		for i := range x.Keys {
			Inspect(x.Keys[i], fn)
			Inspect(x.Values[i], fn)
		}
	case *FuncExpr:
		Inspect(x.Short, fn)
	case *QueryExpr:
		Inspect(x.Where, fn)
		Inspect(x.Limit, fn)
		Inspect(x.Using, fn)
	case *UpdateExpr:
		for _, s := range x.Sets {
			Inspect(s.Value, fn)
		}
		Inspect(x.Where, fn)
		Inspect(x.Using, fn)
	case *DeleteExpr:
		Inspect(x.Where, fn)
		Inspect(x.Using, fn)
	case *TransformExpr:
		Inspect(x.Source, fn)
		for _, pv := range x.Params {
			Inspect(pv.Value, fn)
		}
	}
}

// Includes calls fn for every include of a file: among members, in
// statements and in the bodies of function literals.
func Includes(f *File, fn func(name Name, ifFound bool)) {
	var members func(ms []Member)
	var stmts func(ss []Stmt)
	exprs := func(xs ...Expr) {
		for _, x := range xs {
			Inspect(x, func(e Expr) {
				if fe, ok := e.(*FuncExpr); ok && fe.Body != nil {
					stmts(fe.Body.Stmts)
				}
			})
		}
	}
	block := func(b *Block) {
		if b != nil {
			stmts(b.Stmts)
		}
	}
	stmts = func(ss []Stmt) {
		for _, s := range ss {
			switch s := s.(type) {
			case *IncludeStmt:
				fn(s.Name, s.IfFound)
			case *VarStmt:
				exprs(s.Value)
			case *RowStmt:
				exprs(s.Row, s.Using)
			case *DatabaseStmt:
				exprs(s.Session)
			case *AssignStmt:
				exprs(s.Target, s.Value)
			case *ExprStmt:
				exprs(s.X)
			case *ReturnStmt:
				exprs(s.Value)
			case *RaiseStmt:
				exprs(s.Args...)
			case *AssertStmt:
				exprs(s.Cond, s.Message)
			case *LogStmt:
				exprs(s.Value)
			case *IfStmt:
				for _, c := range s.Clauses {
					exprs(c.Cond)
					block(c.Body)
				}
				block(s.Else)
			case *ForStmt:
				exprs(s.In, s.From, s.To, s.Step)
				block(s.Body)
			case *WhileStmt:
				exprs(s.Cond)
				block(s.Body)
			case *TryStmt:
				block(s.Body)
				for _, c := range s.Catches {
					block(c.Body)
				}
				block(s.Finally)
			}
		}
	}
	members = func(ms []Member) {
		for _, m := range ms {
			switch m := m.(type) {
			case *IncludeMember:
				fn(m.Name, m.IfFound)
			case *MethodDecl:
				block(m.Body)
			case *ConstantDecl:
				exprs(m.Value)
			case *AttributeDecl:
				exprs(m.Value)
			}
		}
	}
	switch {
	case f.Class != nil:
		members(f.Class.Members)
	case f.Include != nil:
		members(f.Include.Members)
		block(f.Include.Body)
	}
}
