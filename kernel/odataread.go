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
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
)

// odataQuery builds a statement on the table of an entity type: the
// tenant of the request and the authorization for reading are always part
// of it, so that counts and pages hold only rows the user may read.
type odataQuery struct {
	sb    *sqlBuilder
	t     *odata.EntityType
	conds []string
}

// newQuery starts a statement on the table of a type.
func (sr *serviceRequest) newQuery(t *odata.EntityType) (*odataQuery, error) {
	q := &odataQuery{sb: &sqlBuilder{d: sr.k.d, table: t.Table, phys: sr.g.phys, auth: sr.auth}, t: t}
	if t.Table.TenantSpecific {
		tenant, ok := sr.tenant.Get()
		if !ok {
			return nil, odata.Errorf(http.StatusBadRequest, odata.CodeInvalidRequest, "the request names no tenant")
		}
		q.conds = append(q.conds, q.sb.tenantFilter(tenant)...)
	}
	auth, err := q.readAuthorization()
	if err != nil {
		return nil, err
	}
	q.conds = append(q.conds, auth)
	return q, nil
}

// readAuthorization returns the condition of the authorization for
// reading: that of the entity, of its nearest parent entity with one, or
// of configuration for business objects and tables of configuration.
func (q *odataQuery) readAuthorization() (string, error) {
	t, sb := q.t, q.sb
	if t.BusinessObject == nil {
		return sb.authFilter(&bytecode.AuthFilter{Object: model.ConfigurationObject, Fields: []bytecode.AuthFilterField{
			{Name: model.ConfigurationBusinessObject, Param: 0}, {Name: model.ConfigurationDataClass, Param: 1}, {Name: model.ActivityField, Param: 2}}},
			[]any{t.Table.Name, string(t.Table.DataClass), "READ"})
	}
	ra, ok := t.BusinessObject.AuthorizationOf(t.Entity)
	if !ok {
		return "(1 = 1)", nil
	}
	au := ra.Authorization
	f := &bytecode.AuthFilter{Object: au.Object}
	var params []any
	param := func(name string, v any) {
		f.Fields = append(f.Fields, bytecode.AuthFilterField{Name: name, Param: len(params), Column: ""})
		params = append(params, v)
	}
	for name, v := range ra.Constants {
		param(name, v)
	}
	for objField, column := range au.Fields {
		f.Fields = append(f.Fields, bytecode.AuthFilterField{Name: objField, Param: -1, Column: column})
	}
	if act, ok := au.Activities[model.ReadActivityKey]; ok {
		param(model.ActivityField, act)
	}
	if ra.Source != model.AuthorizationInherited || ra.Holder == t.Entity {
		return sb.authFilter(f, params)
	}
	// The values come from the nearest parent with an authorization:
	// EXISTS along the foreign keys up to it, with the condition on its
	// columns innermost.
	var b strings.Builder
	outer, cur, depth := sb.q(t.Table.Name), t.Entity, 0
	for cur != ra.Holder {
		parent := t.BusinessObject.Entity(cur.Parent.Or(""))
		fk := sb.phys.Tables[cur.Table].ForeignKey(cur.ForeignKey.Or(""))
		if parent == nil || fk == nil {
			return "(1 = 0)", nil
		}
		depth++
		alias := sb.q(fmt.Sprintf("p%d", depth))
		var join []string
		for _, c := range fk.Columns {
			if c.Field == model.TenantField {
				continue
			}
			join = append(join, alias+"."+sb.q(c.References)+" = "+outer+"."+sb.q(c.Field))
		}
		if sb.phys.Tables[parent.Table].TenantSpecific && sb.phys.Tables[cur.Table].TenantSpecific {
			join = append(join, alias+"."+sb.q(model.TenantField)+" = "+outer+"."+sb.q(model.TenantField))
		}
		b.WriteString("EXISTS (SELECT 1 FROM " + sb.q(parent.Table) + " " + alias + " WHERE " + strings.Join(join, " AND ") + " AND ")
		outer, cur = alias, parent
	}
	own := sb.table
	sb.table = sb.phys.Tables[ra.Holder.Table]
	cond, err := sb.authFilter(f, params)
	sb.table = own
	if err != nil {
		return "", err
	}
	b.WriteString(cond + strings.Repeat(")", depth))
	return b.String(), nil
}

// col returns the quoted column of a property.
func (q *odataQuery) col(p *odata.Property) string { return q.sb.q(p.Field.Name) }

// keys adds conditions on the key or other fields of the table.
func (q *odataQuery) equal(field string, v any) error {
	ph, err := q.sb.compared(field, v)
	if err != nil {
		return err
	}
	q.conds = append(q.conds, q.sb.q(field)+" = "+ph)
	return nil
}

// key adds the values of a key predicate.
func (q *odataQuery) key(kv []odata.KeyValue) error {
	for _, k := range kv {
		if err := q.equal(k.P.Field.Name, k.Value.Text); err != nil {
			return err
		}
	}
	return nil
}

// tuples adds the condition that the fields have one of the given rows
// of values.
func (q *odataQuery) tuples(fields []string, rows [][]any) error {
	if len(rows) == 0 {
		q.conds = append(q.conds, "(1 = 0)")
		return nil
	}
	var alts []string
	for _, r := range rows {
		var parts []string
		for i, f := range fields {
			ph, err := q.sb.compared(f, r[i])
			if err != nil {
				return err
			}
			parts = append(parts, q.sb.q(f)+" = "+ph)
		}
		alts = append(alts, "("+strings.Join(parts, " AND ")+")")
	}
	q.conds = append(q.conds, "("+strings.Join(alts, " OR ")+")")
	return nil
}

// filter adds $filter.
func (q *odataQuery) filter(x odata.Expr) error {
	if x == nil {
		return nil
	}
	c, err := q.expr(x)
	if err != nil {
		return err
	}
	q.conds = append(q.conds, c)
	return nil
}

// falseValue is false in the dialect.
func (q *odataQuery) falseValue() string {
	if q.sb.d.Name() == "sqlite" {
		return "0"
	}
	return "FALSE"
}

// trueValue is true in the dialect.
func (q *odataQuery) trueValue() string {
	if q.sb.d.Name() == "sqlite" {
		return "1"
	}
	return "TRUE"
}

// twoValued makes a condition false where SQL would give unknown, so that
// not and ne follow the two-valued logic of OData: null ne 'X' is true.
func (q *odataQuery) twoValued(cond string, nullable bool) string {
	if !nullable {
		return cond
	}
	return "COALESCE(" + cond + ", " + q.falseValue() + ")"
}

var odataOps = map[string]string{"eq": " = ", "ne": " <> ", "gt": " > ", "ge": " >= ", "lt": " < ", "le": " <= "}

func (q *odataQuery) expr(x odata.Expr) (string, error) {
	switch e := x.(type) {
	case odata.And:
		return q.join(e.L, e.R, " AND ")
	case odata.Or:
		return q.join(e.L, e.R, " OR ")
	case odata.Not:
		c, err := q.expr(e.X)
		return "(NOT " + c + ")", err
	case odata.Compare:
		return q.compare(e)
	case odata.In:
		p := odataPropertyOf(e.X)
		col, err := q.operand(e.X, p)
		if err != nil {
			return "", err
		}
		var phs []string
		null := false
		for _, v := range e.Values {
			if v.Kind == odata.LitNull {
				null = true
				continue
			}
			ph, err := q.sb.compared(p.Field.Name, v.Text)
			if err != nil {
				return "", err
			}
			phs = append(phs, ph)
		}
		cond := "(1 = 0)"
		if len(phs) > 0 {
			cond = q.twoValued(col+" IN ("+strings.Join(phs, ", ")+")", p.Nullable)
		}
		if null {
			cond = "(" + cond + " OR " + col + " IS NULL)"
		}
		return cond, nil
	case odata.Match:
		return q.match(e)
	}
	return "", fmt.Errorf("kernel: unknown expression %T", x)
}

func (q *odataQuery) join(l, r odata.Expr, op string) (string, error) {
	a, err := q.expr(l)
	if err != nil {
		return "", err
	}
	b, err := q.expr(r)
	return "(" + a + op + b + ")", err
}

// odataPropertyOf returns the property of an operand.
func odataPropertyOf(o odata.Operand) *odata.Property {
	switch x := o.(type) {
	case odata.PropertyRef:
		return x.P
	case odata.Call:
		return odataPropertyOf(x.X)
	}
	return nil
}

// operand writes an operand; literals take the domain of the property
// they are compared with.
func (q *odataQuery) operand(o odata.Operand, with *odata.Property) (string, error) {
	switch x := o.(type) {
	case odata.PropertyRef:
		return q.col(x.P), nil
	case odata.Call:
		inner, err := q.operand(x.X, with)
		if err != nil {
			return "", err
		}
		if x.Func == "tolower" {
			return "LOWER(" + inner + ")", nil
		}
		return "UPPER(" + inner + ")", nil
	case odata.Literal:
		if with == nil {
			return q.sb.bind(x.Text), nil
		}
		return q.sb.compared(with.Field.Name, x.Text)
	}
	return "", fmt.Errorf("kernel: unknown operand %T", o)
}

func (q *odataQuery) compare(c odata.Compare) (string, error) {
	lp, rp := odataPropertyOf(c.L), odataPropertyOf(c.R)
	with := lp
	if with == nil {
		with = rp
	}
	for _, side := range []struct {
		lit  odata.Operand
		prop odata.Operand
	}{{c.R, c.L}, {c.L, c.R}} {
		if lit, ok := side.lit.(odata.Literal); ok && lit.Kind == odata.LitNull {
			col, err := q.operand(side.prop, with)
			if err != nil {
				return "", err
			}
			if c.Op == "eq" {
				return col + " IS NULL", nil
			}
			return col + " IS NOT NULL", nil
		}
	}
	l, err := q.operand(c.L, with)
	if err != nil {
		return "", err
	}
	r, err := q.operand(c.R, with)
	if err != nil {
		return "", err
	}
	nullable := lp != nil && lp.Nullable || rp != nil && rp.Nullable
	if !nullable {
		return l + odataOps[c.Op] + r, nil
	}
	// OData compares null as a value: null eq null is true, null ne 'X'
	// is true, and orders with null are false.
	if lp != nil && rp != nil && (c.Op == "eq" || c.Op == "ne") {
		l2, err := q.operand(c.L, with)
		if err != nil {
			return "", err
		}
		r2, err := q.operand(c.R, with)
		if err != nil {
			return "", err
		}
		same := "(COALESCE(" + l + " = " + r + ", " + q.falseValue() + ") OR (" + l2 + " IS NULL AND " + r2 + " IS NULL))"
		if c.Op == "ne" {
			return "(NOT " + same + ")", nil
		}
		return same, nil
	}
	if c.Op == "ne" {
		return "COALESCE(" + l + odataOps[c.Op] + r + ", " + q.trueValue() + ")", nil
	}
	return q.twoValued(l+odataOps[c.Op]+r, true), nil
}

// match writes contains, startswith and endswith by position, without
// LIKE, so that no character needs escaping.
func (q *odataQuery) match(m odata.Match) (string, error) {
	p := odataPropertyOf(m.X)
	if p == nil {
		p = odataPropertyOf(m.Y)
	}
	nullable := p != nil && p.Nullable
	if lit, ok := m.Y.(odata.Literal); ok && lit.Text == "" {
		x, err := q.operand(m.X, p)
		return x + " IS NOT NULL", err
	}
	x, err := q.operand(m.X, p)
	if err != nil {
		return "", err
	}
	y, err := q.operand(m.Y, p)
	if err != nil {
		return "", err
	}
	find := "instr(" + x + ", " + y + ")"
	if q.sb.d.Name() != "sqlite" {
		find = "strpos(" + x + ", " + y + ")"
	}
	var cond string
	switch m.Func {
	case "contains":
		cond = find + " > 0"
	case "startswith":
		cond = find + " = 1"
	default:
		y2, err := q.operand(m.Y, p)
		if err != nil {
			return "", err
		}
		if q.sb.d.Name() == "sqlite" {
			cond = "substr(" + x + ", -length(" + y + ")) = " + y2
		} else {
			cond = "right(" + x + ", length(" + y + ")) = " + y2
		}
	}
	return q.twoValued(cond, nullable), nil
}

// where returns the conditions joined with AND.
func (q *odataQuery) where() string {
	if len(q.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(q.conds, " AND ")
}

// selectText returns the statement that reads the properties of the
// type, ordered by the given order and then by the key, with a limit
// and an offset; limit < 0 reads all rows.
func (q *odataQuery) selectText(order []odata.Order, limit, offset int64) string {
	var cols []string
	for _, p := range q.t.Properties {
		cols = append(cols, q.col(p))
	}
	text := "SELECT " + strings.Join(cols, ", ") + " FROM " + q.sb.q(q.t.Table.Name) + q.where()
	var keys []string
	sorted := map[*odata.Property]bool{}
	for _, o := range order {
		dir := " ASC"
		if o.Desc {
			dir = " DESC"
		}
		// Null comes last ascending and first descending on both
		// databases.
		if o.P.Nullable {
			keys = append(keys, "("+q.col(o.P)+" IS NULL)"+dir)
		}
		keys = append(keys, q.col(o.P)+dir)
		sorted[o.P] = true
	}
	for _, k := range q.t.Key {
		if !sorted[k] {
			keys = append(keys, q.col(k)+" ASC")
		}
	}
	text += " ORDER BY " + strings.Join(keys, ", ")
	if limit >= 0 {
		text += " LIMIT " + q.sb.bind(limit)
	}
	if offset > 0 {
		if limit < 0 {
			text += " LIMIT -1"
			if q.sb.d.Name() != "sqlite" {
				text = strings.TrimSuffix(text, " LIMIT -1") + " LIMIT ALL"
			}
		}
		text += " OFFSET " + q.sb.bind(offset)
	}
	return text
}

// rows runs a statement of the query and returns the rows by field.
func (sr *serviceRequest) rows(ctx context.Context, q *odataQuery, text string) ([]map[string]any, error) {
	ds := make([]*model.Domain, len(q.t.Properties))
	for i, p := range q.t.Properties {
		ds[i] = p.Domain
	}
	raw, err := sr.k.query(ctx, text, q.sb.args, ds)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		m := make(map[string]any, len(r))
		for j, p := range q.t.Properties {
			m[p.Field.Name] = r[j]
		}
		out[i] = m
	}
	return out, nil
}

// count returns the number of rows of the query.
func (sr *serviceRequest) count(ctx context.Context, q *odataQuery) (int64, error) {
	text := "SELECT COUNT(*) FROM " + q.sb.q(q.t.Table.Name) + q.where()
	raw, err := sr.k.query(ctx, text, q.sb.args, []*model.Domain{{Type: model.Int8}})
	if err != nil {
		return 0, err
	}
	n, _ := raw[0][0].(int64)
	return n, nil
}

// query runs a statement of the kernel on the database and decodes the
// columns with their domains.
func (k *Kernel) query(ctx context.Context, text string, args []any, domains []*model.Domain) ([][]any, error) {
	rows, err := k.db.QueryContext(ctx, text, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, text)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(domains))
		ptrs := make([]any, len(domains))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			dv, err := k.d.Decode(domains[i], v)
			if err != nil {
				return nil, err
			}
			vals[i] = dv
		}
		out = append(out, vals)
	}
	return out, rows.Err()
}

// effective loads the authorizations of the user of the request.
func (sr *serviceRequest) effective(ctx context.Context) error {
	if sr.authLoaded {
		return nil
	}
	e, err := sr.k.EffectiveAuthorizations(ctx, sr.user.Name, sr.tenant.Or(""), sr.k.now())
	if err != nil {
		return err
	}
	sr.auth, sr.authLoaded = e, true
	return nil
}
