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

package odata

import (
	"math/big"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Matches evaluates a filter on a row in memory, for entity sets that do
// not come from tables. Rows hold the values of the transaction buffer by
// field name. As in SQL, comparisons with null other than eq and ne are
// false.
func Matches(x Expr, row map[string]any) (bool, *Error) {
	switch e := x.(type) {
	case And:
		l, err := Matches(e.L, row)
		if err != nil || !l {
			return false, err
		}
		return Matches(e.R, row)
	case Or:
		l, err := Matches(e.L, row)
		if err != nil || l {
			return l, err
		}
		return Matches(e.R, row)
	case Not:
		v, err := Matches(e.X, row)
		return !v, err
	case Compare:
		l, err := operand(e.L, row)
		if err != nil {
			return false, err
		}
		r, err := operand(e.R, row)
		if err != nil {
			return false, err
		}
		if l == nil || r == nil {
			switch e.Op {
			case "eq":
				return l == nil && r == nil, nil
			case "ne":
				return (l == nil) != (r == nil), nil
			}
			return false, nil
		}
		c, ok := compareValues(l, r)
		if !ok {
			return false, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the operands of "+e.Op+" have different types")
		}
		switch e.Op {
		case "eq":
			return c == 0, nil
		case "ne":
			return c != 0, nil
		case "gt":
			return c > 0, nil
		case "ge":
			return c >= 0, nil
		case "lt":
			return c < 0, nil
		}
		return c <= 0, nil
	case In:
		v, err := operand(e.X, row)
		if err != nil || v == nil {
			return false, err
		}
		for _, lit := range e.Values {
			if c, ok := compareValues(v, literal(lit)); ok && c == 0 {
				return true, nil
			}
		}
		return false, nil
	case Match:
		a, err := operand(e.X, row)
		if err != nil {
			return false, err
		}
		b, err := operand(e.Y, row)
		if err != nil {
			return false, err
		}
		s, ok1 := a.(string)
		t, ok2 := b.(string)
		if !ok1 || !ok2 {
			return false, nil
		}
		switch e.Func {
		case "contains":
			return strings.Contains(s, t), nil
		case "startswith":
			return strings.HasPrefix(s, t), nil
		}
		return strings.HasSuffix(s, t), nil
	}
	return false, Errorf(http.StatusNotImplemented, CodeNotImplemented, "this filter")
}

// operand returns the value of an operand: strings, *big.Rat for numbers,
// bool, time.Time or nil.
func operand(o Operand, row map[string]any) (any, *Error) {
	switch x := o.(type) {
	case Literal:
		return literal(x), nil
	case PropertyRef:
		if !x.P.Elementary() {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, x.P.Name+" is no single value and cannot be filtered")
		}
		return normalize(x.P.Domain, row[x.P.Field.Name]), nil
	case Call:
		v, err := operand(x.X, row)
		if s, ok := v.(string); ok && err == nil {
			if x.Func == "tolower" {
				return strings.ToLower(s), nil
			}
			return strings.ToUpper(s), nil
		}
		return v, err
	}
	return nil, Errorf(http.StatusNotImplemented, CodeNotImplemented, "this operand")
}

func literal(l Literal) any {
	switch l.Kind {
	case LitNull:
		return nil
	case LitInt, LitDecimal:
		r, ok := new(big.Rat).SetString(l.Text)
		if !ok {
			return l.Text
		}
		return r
	case LitBool:
		return l.Text == "true"
	case LitDate:
		t, _ := time.Parse(model.DateLayout, l.Text)
		return t
	case LitDateTimeOffset:
		t, _ := time.Parse(time.RFC3339Nano, l.Text)
		return t.UTC()
	case LitGuid:
		return strings.ToLower(l.Text)
	}
	return l.Text
}

// normalize brings a value of the buffer into the form of operands.
func normalize(d *model.Domain, v any) any {
	switch x := v.(type) {
	case int64:
		return new(big.Rat).SetInt64(x)
	case string:
		if d.Type == model.Dec {
			if r, ok := new(big.Rat).SetString(x); ok {
				return r
			}
		}
	}
	return v
}

// compareValues compares two values of the same kind.
func compareValues(a, b any) (int, bool) {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return strings.Compare(x, y), ok
	case *big.Rat:
		y, ok := b.(*big.Rat)
		if !ok {
			return 0, false
		}
		return x.Cmp(y), true
	case bool:
		y, ok := b.(bool)
		if !ok {
			return 0, false
		}
		switch {
		case x == y:
			return 0, true
		case !x:
			return -1, true
		}
		return 1, true
	case time.Time:
		y, ok := b.(time.Time)
		return x.Compare(y), ok
	}
	return 0, false
}

// SortRows sorts rows in memory by $orderby, then by the key. null comes
// first in ascending order.
func SortRows(rows []map[string]any, order []Order, key []*Property) {
	all := slices.Clone(order)
	for _, k := range key {
		all = append(all, Order{P: k})
	}
	slices.SortStableFunc(rows, func(a, b map[string]any) int {
		for _, o := range all {
			x, y := normalize(o.P.Domain, a[o.P.Field.Name]), normalize(o.P.Domain, b[o.P.Field.Name])
			c := 0
			switch {
			case x == nil && y == nil:
			case x == nil:
				c = -1
			case y == nil:
				c = 1
			default:
				c, _ = compareValues(x, y)
			}
			if o.Desc {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		return 0
	})
}
