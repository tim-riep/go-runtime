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

package kernel

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
)

// authFilter writes authorized in a where condition: the grants of the
// user on the object, joined with or. Fields with a value of the code are
// checked here and drop the grants that do not match; fields with a
// column become conditions on the column.
func (sb *sqlBuilder) authFilter(f *bytecode.AuthFilter, params []any) (string, error) {
	e := sb.auth
	switch {
	case e == nil || e.Locked:
		return sb.used(f, params, "(1 = 0)"), nil
	case e.All:
		return sb.used(f, params, "(1 = 1)"), nil
	}
	// Placeholders are bound in the order of the text: the arguments of a
	// grant that is left out are dropped again.
	start := len(sb.args)
	var grants []string
	all := false
	for _, g := range e.Grants(f.Object) {
		n := len(sb.args)
		cond, ok, err := sb.grantCondition(f, &g, params)
		if err != nil {
			return "", err
		}
		if !ok {
			sb.args = sb.args[:n]
			continue
		}
		if cond == "" {
			// A grant without conditions on columns allows every row.
			all = true
			break
		}
		grants = append(grants, cond)
	}
	var out string
	switch {
	case all:
		sb.args = sb.args[:start]
		out = "(1 = 1)"
	case len(grants) == 0:
		out = "(1 = 0)"
	default:
		out = "(" + strings.Join(grants, " OR ") + ")"
	}
	return sb.used(f, params, out), nil
}

// used logs a filter and keeps it for the trace.
func (sb *sqlBuilder) used(f *bytecode.AuthFilter, params []any, out string) string {
	slog.Debug("authorization filter", "object", f.Object, "table", sb.table.Name, "condition", out)
	use := filterUse{check: authorization.Check{Object: f.Object, Values: map[string]string{}}, columns: map[string]string{}, condition: out}
	for _, ff := range f.Fields {
		switch {
		case ff.Param >= 0:
			use.check.Values[ff.Name] = filterText(params[ff.Param])
		case ff.Column != "":
			use.columns[ff.Name] = ff.Column
		default:
			if use.check.Any == nil {
				use.check.Any = map[string]bool{}
			}
			use.check.Any[ff.Name] = true
		}
	}
	sb.filters = append(sb.filters, use)
	return out
}

// grantCondition returns the conditions of a grant on the columns, joined
// with and, or false when a value of the code does not match.
func (sb *sqlBuilder) grantCondition(f *bytecode.AuthFilter, g *authorization.Grant, params []any) (string, bool, error) {
	var conds []string
	for _, ff := range f.Fields {
		m := g.Fields[ff.Name]
		switch {
		case m == nil:
			// Grants have every field of the object; an older grant without
			// the field matches nothing, as in memory.
			return "", false, nil
		case ff.Param >= 0:
			if !m.Match(filterText(params[ff.Param])) {
				return "", false, nil
			}
		case ff.Column != "":
			c, err := sb.columnCondition(ff.Column, m)
			if err != nil {
				return "", false, err
			}
			conds = append(conds, c)
		}
	}
	if len(conds) == 0 {
		return "", true, nil
	}
	return "(" + strings.Join(conds, " AND ") + ")", true, nil
}

// columnCondition turns a matcher into a condition on a column. null
// matches no entry, not even *.
func (sb *sqlBuilder) columnCondition(column string, m *authorization.Matcher) (string, error) {
	d, err := sb.domain(column)
	if err != nil {
		return "", err
	}
	integer := d.Type == model.Int4 || d.Type == model.Int8
	col := sb.q(column)
	if m.Any {
		return col + " IS NOT NULL", nil
	}
	var parts []string
	if len(m.Values) > 0 {
		var values []any
		for _, v := range slices.Sorted(maps.Keys(m.Values)) {
			if !integer {
				values = append(values, v)
			} else if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				values = append(values, n)
			}
		}
		if len(values) > 0 {
			parts = append(parts, sb.d.InValues(column, integer, values, sb.bind))
		}
	}
	for _, r := range m.Ranges {
		if integer {
			lo, hi := r.Ints()
			parts = append(parts, col+" BETWEEN "+sb.bind(lo)+" AND "+sb.bind(hi))
		} else {
			parts = append(parts, col+" BETWEEN "+sb.bind(r.Low)+" AND "+sb.bind(r.High))
		}
	}
	for _, p := range m.Prefixes {
		parts = append(parts, sb.d.HasPrefix(column, integer, p, sb.bind))
	}
	switch len(parts) {
	case 0:
		return "(1 = 0)", nil
	case 1:
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

// filterText returns the canonical text of a value of the code, as the
// VM gives it to checks.
func filterText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return x.Format(model.DateLayout)
	}
	return fmt.Sprint(v)
}
