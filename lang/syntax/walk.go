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
	}
}
