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
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// statement is a database statement of code translated into SQL for one
// dialect, with its bound arguments.
type statement struct {
	text    string
	args    []any
	columns []string        // columns of the rows of select
	domains []*model.Domain // their domains, for decoding
}

// sqlBuilder writes SQL with numbered placeholders.
type sqlBuilder struct {
	d     dialect.Dialect
	b     strings.Builder
	args  []any
	table *model.Table
	phys  *model.Physical
	// auth are the effective authorizations for filters; nil without user.
	auth *authorization.Effective
}

func (sb *sqlBuilder) q(name string) string { return sb.d.QuoteIdent(name) }

func (sb *sqlBuilder) domain(column string) (*model.Domain, error) {
	for i := range sb.table.Fields {
		if f := &sb.table.Fields[i]; f.Name == column {
			if d := sb.phys.Domain(f); d != nil {
				return d, nil
			}
		}
	}
	return nil, fmt.Errorf("kernel: %s has no column %s", sb.table.Name, column)
}

// value binds a value written into a column. A value the column cannot
// hold is a check violation.
func (sb *sqlBuilder) value(column string, v any) (string, error) {
	d, err := sb.domain(column)
	if err != nil {
		return "", err
	}
	enc, err := sb.d.Encode(d, v)
	if err != nil {
		return "", &vm.DatabaseError{Class: "/RT/CHECK_VIOLATION", Text: fmt.Sprintf("%s.%s: %v", sb.table.Name, column, err), Table: sb.table.Name}
	}
	return sb.bind(enc), nil
}

// compared binds a value compared with a column. A value the column
// cannot hold is passed unchanged: it matches no row.
func (sb *sqlBuilder) compared(column string, v any) (string, error) {
	d, err := sb.domain(column)
	if err != nil {
		return "", err
	}
	enc, err := sb.d.Encode(d, v)
	if err != nil {
		enc = v
	}
	return sb.bind(enc), nil
}

func (sb *sqlBuilder) bind(v any) string {
	sb.args = append(sb.args, v)
	return sb.d.Placeholder(len(sb.args))
}

func (sb *sqlBuilder) write(parts ...string) {
	for _, p := range parts {
		sb.b.WriteString(p)
	}
}

// tenantFilter restricts statements on tables of tenants to the tenant of
// the execution.
func (sb *sqlBuilder) tenantFilter(tenant string) []string {
	if !sb.table.TenantSpecific {
		return nil
	}
	return []string{sb.q(model.TenantField) + " = " + sb.bind(tenant)}
}

var sqlOps = map[bytecode.CondOp]string{bytecode.CEq: " = ", bytecode.CNe: " <> ", bytecode.CLt: " < ", bytecode.CLe: " <= ", bytecode.CGt: " > ", bytecode.CGe: " >= "}

// cond writes a condition; null never satisfies a comparison.
func (sb *sqlBuilder) cond(q *bytecode.Query, i int, params []any) (string, error) {
	c := &q.Conds[i]
	switch c.Op {
	case bytecode.CAnd, bytecode.COr:
		a, err := sb.cond(q, c.Kids[0], params)
		if err != nil {
			return "", err
		}
		b, err := sb.cond(q, c.Kids[1], params)
		if err != nil {
			return "", err
		}
		op := " AND "
		if c.Op == bytecode.COr {
			op = " OR "
		}
		return "(" + a + op + b + ")", nil
	case bytecode.CNot:
		a, err := sb.cond(q, c.Kids[0], params)
		if err != nil {
			return "", err
		}
		return "NOT " + a, nil
	case bytecode.CAuth:
		return sb.authFilter(&q.Auths[c.Auth], params)
	case bytecode.CIsNull:
		return "(" + sb.q(c.Field) + " IS NULL)", nil
	case bytecode.CIsEmpty:
		ph, err := sb.compared(c.Field, "")
		return "(" + sb.q(c.Field) + " = " + ph + ")", err
	case bytecode.CIn:
		vals, _ := params[c.Param].([]any)
		if len(vals) == 0 {
			return "(1 = 0)", nil
		}
		var phs []string
		for _, v := range vals {
			ph, err := sb.compared(c.Field, v)
			if err != nil {
				return "", err
			}
			phs = append(phs, ph)
		}
		return "(" + sb.q(c.Field) + " IN (" + strings.Join(phs, ", ") + "))", nil
	}
	op := sqlOps[c.Op]
	if c.Param < 0 {
		return "(" + sb.q(c.Field) + op + sb.q(c.Other) + ")", nil
	}
	if params[c.Param] == nil {
		return "(1 = 0)", nil
	}
	ph, err := sb.compared(c.Field, params[c.Param])
	return "(" + sb.q(c.Field) + op + ph + ")", err
}

// where writes the condition. Placeholders are bound in the order of the
// text, as SQLite numbers them by position.
func (sb *sqlBuilder) where(q *bytecode.Query, tenant string, params []any, key []keyValue) error {
	conds := sb.tenantFilter(tenant)
	for _, k := range key {
		ph, err := sb.compared(k.column, k.value)
		if err != nil {
			return err
		}
		conds = append(conds, sb.q(k.column)+" = "+ph)
	}
	if q.Where >= 0 {
		c, err := sb.cond(q, q.Where, params)
		if err != nil {
			return err
		}
		conds = append(conds, c)
	}
	if len(conds) > 0 {
		sb.write(" WHERE ", strings.Join(conds, " AND "))
	}
	return nil
}

type keyValue struct {
	column string
	value  any
}

// buildStatement translates a statement of code. args are the parameters
// of the code, or the values of all fields for row statements.
func buildStatement(d dialect.Dialect, phys *model.Physical, tenant string, auth *authorization.Effective, p *bytecode.Program, q *bytecode.Query, args []any, limit int64) (*statement, error) {
	table := phys.Tables[q.Table]
	if table == nil {
		return nil, fmt.Errorf("kernel: table %s does not exist", q.Table)
	}
	if table.TenantSpecific && tenant == "" {
		return nil, &vm.DatabaseError{Class: "/RT/SESSION_ERROR", Text: fmt.Sprintf("the execution has no tenant, so it cannot access the table %s of tenants", q.Table)}
	}
	sb := &sqlBuilder{d: d, table: table, phys: phys, auth: auth}
	layout := &p.Layouts[p.Types[q.Row].Layout]
	var columns []string
	for _, f := range layout.Fields {
		columns = append(columns, f.Name)
	}
	quoted := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = sb.q(c)
	}
	st := &statement{}
	switch q.Kind {
	case bytecode.QSelect, bytecode.QSingle, bytecode.QCount:
		if q.Kind == bytecode.QCount {
			sb.write("SELECT COUNT(*) FROM ", sb.q(q.Table))
		} else {
			sb.write("SELECT ", strings.Join(quoted, ", "), " FROM ", sb.q(q.Table))
			st.columns = columns
			for _, c := range columns {
				dom, err := sb.domain(c)
				if err != nil {
					return nil, err
				}
				st.domains = append(st.domains, dom)
			}
		}
		if err := sb.where(q, tenant, args, nil); err != nil {
			return nil, err
		}
		if q.Kind != bytecode.QCount {
			// Without order by the rows come in the order of the primary key,
			// so that results do not depend on the database.
			var order []string
			for _, o := range q.Order {
				dir := ""
				if o.Descending {
					dir = " DESC"
				}
				order = append(order, sb.q(o.Field)+dir)
			}
			if len(order) == 0 {
				for _, k := range table.PrimaryKey {
					if k != model.TenantField {
						order = append(order, sb.q(k))
					}
				}
			}
			if len(order) > 0 {
				sb.write(" ORDER BY ", strings.Join(order, ", "))
			}
		}
		switch {
		case q.Kind == bytecode.QSingle:
			sb.write(" LIMIT 1")
		case q.Limit:
			sb.write(" LIMIT ", sb.bind(limit))
		}
	case bytecode.QInsert:
		cols, phs := []string{}, []string{}
		if table.TenantSpecific {
			cols = append(cols, sb.q(model.TenantField))
			phs = append(phs, sb.bind(tenant))
		}
		for i, c := range columns {
			ph, err := sb.value(c, args[i])
			if err != nil {
				return nil, err
			}
			cols = append(cols, quoted[i])
			phs = append(phs, ph)
		}
		sb.write("INSERT INTO ", sb.q(q.Table), " (", strings.Join(cols, ", "), ") VALUES (", strings.Join(phs, ", "), ")")
	case bytecode.QUpdateRow, bytecode.QDeleteRow:
		var sets []string
		var key []keyValue
		if q.Kind == bytecode.QUpdateRow {
			for i, c := range columns {
				if slices.Contains(q.Key, c) {
					continue
				}
				ph, err := sb.value(c, args[i])
				if err != nil {
					return nil, err
				}
				sets = append(sets, quoted[i]+" = "+ph)
			}
		}
		for i, c := range columns {
			if slices.Contains(q.Key, c) {
				key = append(key, keyValue{c, args[i]})
			}
		}
		if q.Kind == bytecode.QDeleteRow {
			sb.write("DELETE FROM ", sb.q(q.Table))
		} else {
			if len(sets) == 0 {
				// Only key fields: the statement still reports whether the
				// row exists.
				sets = append(sets, sb.q(q.Key[0])+" = "+sb.q(q.Key[0]))
			}
			sb.write("UPDATE ", sb.q(q.Table), " SET ", strings.Join(sets, ", "))
		}
		if err := sb.where(&bytecode.Query{Where: -1}, tenant, nil, key); err != nil {
			return nil, err
		}
	case bytecode.QUpdateWhere:
		var sets []string
		for i, c := range q.Sets {
			ph, err := sb.value(c, args[i])
			if err != nil {
				return nil, err
			}
			sets = append(sets, sb.q(c)+" = "+ph)
		}
		sb.write("UPDATE ", sb.q(q.Table), " SET ", strings.Join(sets, ", "))
		if err := sb.where(q, tenant, args, nil); err != nil {
			return nil, err
		}
	case bytecode.QDeleteWhere:
		sb.write("DELETE FROM ", sb.q(q.Table))
		if err := sb.where(q, tenant, args, nil); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("kernel: unknown statement %d", q.Kind)
	}
	st.text, st.args = sb.b.String(), sb.args
	return st, nil
}
