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
	"crypto/rand"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// Reasons why an entity statement fails for an instance.
const (
	reasonNotAuthorized  = "NOT_AUTHORIZED"
	reasonNotFound       = "NOT_FOUND"
	reasonAlreadyExists  = "ALREADY_EXISTS"
	reasonParentNotFound = "PARENT_NOT_FOUND"
	reasonValidation     = "VALIDATION"
	reasonMandatory      = "MANDATORY"
	reasonETag           = "ETAG"
	reasonDatabase       = "DATABASE"
	reasonBehavior       = "BEHAVIOR"
)

// instanceState is the state of an instance in the transaction buffer.
type instanceState int

const (
	stateRead    instanceState = iota // as in the database
	stateCreated                      // new, not in the database
	stateUpdated                      // changed
	stateDeleted                      // deleted, in the database until the save
)

// instance is an instance of an entity in the transaction buffer.
type instance struct {
	bo     *model.BusinessObject
	entity *model.Entity
	key    string // the key as JSON
	values map[string]any
	state  instanceState
	// etag is the value of the ETag field when the instance was read.
	etag any
	// changed are the fields changed since the last save, ops the
	// operations since the last save; both trigger determinations and
	// validations.
	changed map[string]bool
	ops     map[model.EntityOperation]bool
	// recent and recentOps are the fields and operations of the running
	// entity statement, which trigger determinations on modify.
	recent    map[string]bool
	recentOps map[model.EntityOperation]bool
	link      *string
}

// entityBuffer is the transaction buffer of the business objects of an
// execution. It belongs to the default session: entity statements read
// and change it, commit business objects writes it.
type entityBuffer struct {
	x         *execution
	instances map[string]*instance // by table and key
	order     []*instance          // in the order they entered the buffer
	// running are the determinations that run, which their own changes do
	// not trigger again; depth counts nested entity statements of them.
	running map[string]bool
	depth   int
}

func (x *execution) buffer() *entityBuffer {
	if x.entities == nil {
		x.entities = &entityBuffer{x: x, instances: map[string]*instance{}}
	}
	return x.entities
}

func bufferKey(table, key string) string { return table + "\x00" + key }

// Pending reports whether the buffer holds changes.
func (b *entityBuffer) Pending() bool {
	for _, in := range b.order {
		if in.state != stateRead {
			return true
		}
	}
	return false
}

// table returns the physical table of an entity.
func (b *entityBuffer) table(name string) *model.Table { return b.x.phys.Tables[name] }

// columns returns the columns of a table without the tenant field.
func (b *entityBuffer) columns(name string) []*model.Field {
	var out []*model.Field
	t := b.table(name)
	for i := range t.Fields {
		if t.Fields[i].Name != model.TenantField {
			out = append(out, &t.Fields[i])
		}
	}
	return out
}

// keyFields returns the key of a table without the tenant field.
func (b *entityBuffer) keyFields(name string) []string {
	return slices.DeleteFunc(slices.Clone(b.table(name).PrimaryKey), func(f string) bool { return f == model.TenantField })
}

func (b *entityBuffer) domain(table, field string) *model.Domain {
	t := b.table(table)
	if f := t.Field(field); f != nil {
		return b.x.phys.Domain(f)
	}
	return nil
}

// keyOf returns the key of a row as JSON in the format of rt run, and
// whether all key fields have a value.
func (b *entityBuffer) keyOf(table string, row map[string]any) (string, bool) {
	var parts []string
	for _, k := range b.keyFields(table) {
		v := row[k]
		if v == nil {
			return "", false
		}
		parts = append(parts, strconv.Quote(k)+":"+jsonValue(b.domain(table, k), v))
	}
	return "{" + strings.Join(parts, ",") + "}", true
}

// jsonValue writes a value of the Go form as JSON in the format of rt run.
func jsonValue(d *model.Domain, v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		if d != nil && d.Type == model.Date {
			return strconv.Quote(x.UTC().Format(model.DateLayout))
		}
		return strconv.Quote(x.UTC().Format(model.TimestampLayout))
	case []byte:
		return strconv.Quote(string(x))
	case string:
		return strconv.Quote(x)
	}
	return strconv.Quote(fmt.Sprint(v))
}

// goValue converts the canonical text of a default into the Go form.
func goValue(d *model.Domain, text string) any {
	switch d.Type {
	case model.Int4, model.Int8:
		n, _ := strconv.ParseInt(text, 10, 64)
		return n
	case model.Bool:
		return text == "true"
	case model.Date:
		t, _ := time.Parse(model.DateLayout, text)
		return t
	case model.Timestamp:
		t, _ := time.Parse(model.TimestampLayout, text)
		return t
	case model.Blob:
		return []byte(text)
	}
	return text
}

// zeroValue returns the zero value of a domain in Go form, as code gives
// new rows.
func zeroValue(d *model.Domain) any {
	switch d.Type {
	case model.Int4, model.Int8:
		return int64(0)
	case model.Dec:
		return "0"
	case model.Bool:
		return false
	case model.Date, model.Timestamp:
		return time.Unix(0, 0).UTC()
	case model.UUID:
		return "00000000-0000-0000-0000-000000000000"
	case model.Blob:
		return []byte{}
	}
	return ""
}

// initialValue returns the value of a field of a new instance that the
// statement does not set: its default, else null when it may be null,
// else the zero value.
func (b *entityBuffer) initialValue(f *model.Field) any {
	d := b.x.phys.Domain(f)
	def := f.Default
	if def.IsZero() && d != nil {
		def = d.Default
	}
	switch {
	case def.Kind == model.DefaultValue && d != nil:
		return goValue(d, def.Value)
	case !f.NotNull || d == nil:
		return nil
	}
	return zeroValue(d)
}

func newUUID() string {
	var u native.UUID
	_, _ = rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return u.String()
}

// needTenant fails statements on tenant-specific tables without tenant.
func (b *entityBuffer) needTenant(table string) error {
	if b.table(table).TenantSpecific && b.x.tenant == "" {
		return &native.RuntimeError{Class: "/RT/SESSION_ERROR", Text: "business object tables of tenants need an execution with a tenant"}
	}
	return nil
}

// selectRows reads rows of a table in the default session whose fields
// equal the given values.
func (b *entityBuffer) selectRows(ctx context.Context, table string, where map[string]any) ([]map[string]any, error) {
	if err := b.needTenant(table); err != nil {
		return nil, err
	}
	d := b.x.d
	cols := b.columns(table)
	var names []string
	for _, c := range cols {
		names = append(names, d.QuoteIdent(c.Name))
	}
	var conds []string
	var args []any
	bind := func(field string, v any) error {
		enc, err := d.Encode(b.domain(table, field), v)
		if err != nil {
			enc = v
		}
		args = append(args, enc)
		conds = append(conds, d.QuoteIdent(field)+" = "+d.Placeholder(len(args)))
		return nil
	}
	if b.table(table).TenantSpecific {
		_ = bind(model.TenantField, b.x.tenant)
	}
	for _, f := range slices.Sorted(mapsKeys(where)) {
		_ = bind(f, where[f])
	}
	text := "SELECT " + strings.Join(names, ", ") + " FROM " + d.QuoteIdent(table)
	if len(conds) > 0 {
		text += " WHERE " + strings.Join(conds, " AND ")
	}
	var domains []*model.Domain
	for _, c := range cols {
		domains = append(domains, b.x.phys.Domain(c))
	}
	var rows [][]any
	var err error
	if s := b.x.sessions[0]; s.open || b.x.test {
		rows, err = s.rawQuery(ctx, text, args, domains)
	} else {
		// Outside a transaction the rows come from the pool, so that no
		// snapshot of a reading transaction outlives them.
		rows, err = queryRows(ctx, b.x.k.db, b.x, text, args, domains)
	}
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		m := make(map[string]any, len(cols))
		for j, c := range cols {
			m[c.Name] = r[j]
		}
		out[i] = m
	}
	return out, nil
}

func mapsKeys(m map[string]any) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// find returns the instance of an entity with the key of a row: from the
// buffer, else read from the database into the buffer. Deleted instances
// and instances that do not exist give nil.
func (b *entityBuffer) find(ctx context.Context, bo *model.BusinessObject, e *model.Entity, row map[string]any) (*instance, error) {
	key, ok := b.keyOf(e.Table, row)
	if !ok {
		return nil, nil
	}
	if in := b.instances[bufferKey(e.Table, key)]; in != nil {
		if in.state == stateDeleted {
			return nil, nil
		}
		return in, nil
	}
	where := map[string]any{}
	for _, k := range b.keyFields(e.Table) {
		where[k] = row[k]
	}
	rows, err := b.selectRows(ctx, e.Table, where)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return b.add(bo, e, rows[0], stateRead), nil
}

// add puts an instance into the buffer.
func (b *entityBuffer) add(bo *model.BusinessObject, e *model.Entity, values map[string]any, state instanceState) *instance {
	key, _ := b.keyOf(e.Table, values)
	in := &instance{bo: bo, entity: e, key: key, values: values, state: state, changed: map[string]bool{}, ops: map[model.EntityOperation]bool{},
		recent: map[string]bool{}, recentOps: map[model.EntityOperation]bool{}}
	if f, ok := e.ETag.Get(); ok && state == stateRead {
		in.etag = values[f]
	}
	b.instances[bufferKey(e.Table, key)] = in
	b.order = append(b.order, in)
	return in
}

// children returns the instances of a child entity of a parent instance:
// those in the database, as the buffer has them, and the new ones.
func (b *entityBuffer) children(ctx context.Context, bo *model.BusinessObject, child *model.Entity, parent *instance) ([]*instance, error) {
	fk := b.foreignKey(child)
	where := map[string]any{}
	for _, c := range fk.Columns {
		if c.Field != model.TenantField {
			where[c.Field] = parent.values[c.References]
		}
	}
	var out []*instance
	seen := map[string]bool{}
	if parent.state != stateCreated {
		rows, err := b.selectRows(ctx, child.Table, where)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			key, _ := b.keyOf(child.Table, r)
			in := b.instances[bufferKey(child.Table, key)]
			if in == nil {
				in = b.add(bo, child, r, stateRead)
			}
			seen[key] = true
			if in.state != stateDeleted && matches(in.values, where) {
				out = append(out, in)
			}
		}
	}
	for _, in := range b.order {
		if in.entity.Table == child.Table && !seen[in.key] && in.state != stateDeleted && matches(in.values, where) {
			out = append(out, in)
		}
	}
	return out, nil
}

func matches(values, where map[string]any) bool {
	for k, v := range where {
		if fmt.Sprint(values[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// foreignKey returns the foreign key of a dependent entity to its parent
// in the physical table, with the tenant field.
func (b *entityBuffer) foreignKey(e *model.Entity) *model.ForeignKey {
	return b.table(e.Table).ForeignKey(e.ForeignKey.Or(""))
}

// result collects the result of an entity statement.
type result struct{ vm.EntityResult }

func strPtr(s string) *string { return &s }

func (r *result) fail(entity, op, reason string, key *string, link *string) {
	r.Failures = append(r.Failures, vm.EntityFailure{Entity: entity, Operation: op, Key: key, Link: link, Reason: reason})
}

func (r *result) message(severity, text, entity string, key *string, field *string) {
	m := vm.EntityMessage{Severity: severity, Text: text, Key: key, Field: field}
	if entity != "" {
		m.Entity = strPtr(entity)
	}
	r.Messages = append(r.Messages, m)
}

// Modify runs modify business object.
func (b *entityBuffer) Modify(ctx context.Context, req vm.ModifyRequest) (*vm.EntityResult, []any, error) {
	bo := b.x.schema.BusinessObjects[req.BusinessObject]
	if bo == nil {
		return nil, nil, fmt.Errorf("business object %s is not installed", req.BusinessObject)
	}
	r := &result{}
	b.depth++
	defer func() { b.depth-- }()
	if b.depth > maxRounds {
		return nil, nil, &native.RuntimeError{Class: "/RT/VALUE_ERROR", Text: fmt.Sprintf("determinations changed %s more than %d times in a row; they trigger each other", bo.Name, maxRounds)}
	}
	// The changes of a statement in a determination trigger determinations
	// on their own; the running statement keeps its changes.
	type recentChanges struct {
		fields map[string]bool
		ops    map[model.EntityOperation]bool
	}
	outer := map[*instance]recentChanges{}
	for _, in := range b.order {
		outer[in] = recentChanges{in.recent, in.recentOps}
		in.recent, in.recentOps = map[string]bool{}, map[model.EntityOperation]bool{}
	}
	defer func() {
		for in, c := range outer {
			in.recent, in.recentOps = c.fields, c.ops
		}
	}()
	links := map[string]*instance{}
	intos := make([]any, len(req.Steps))
	var touched []*instance
	for i, st := range req.Steps {
		e := bo.Entity(st.Entity)
		if e == nil {
			return nil, nil, fmt.Errorf("%s is no entity of %s", st.Entity, bo.Name)
		}
		if err := b.needTenant(e.Table); err != nil {
			return nil, nil, err
		}
		var err error
		var done []*instance
		switch st.Op {
		case "create":
			done, err = b.create(ctx, bo, e, st, req, links, r)
		case "update":
			done, err = b.update(ctx, bo, e, st, req, r)
		case "delete":
			done, err = b.delete(ctx, bo, e, st, req, r)
		case "execute":
			intos[i], done, err = b.execute(ctx, bo, e, st, req, r)
		}
		if err != nil {
			return nil, nil, err
		}
		touched = append(touched, done...)
	}
	if err := b.determine(ctx, req.Behavior, model.WhenModify, touched, r); err != nil {
		return nil, nil, err
	}
	return &r.EntityResult, intos, nil
}

// create runs a create step: new instances with the given fields, their
// key from the row or a new UUID, and for dependent entities the key of
// the parent from the instance PARENT_LINK names or from the foreign key
// fields of the row.
func (b *entityBuffer) create(ctx context.Context, bo *model.BusinessObject, e *model.Entity, st vm.ModifyStep, req vm.ModifyRequest, links map[string]*instance, r *result) ([]*instance, error) {
	var out []*instance
	for _, row := range st.Rows {
		values := map[string]any{}
		for _, c := range b.columns(e.Table) {
			values[c.Name] = b.initialValue(c)
		}
		for _, f := range st.Fields {
			values[f] = row[f]
		}
		var link *string
		if l, ok := row[model.LinkField].(string); ok {
			link = strPtr(l)
		}
		if e.Numbering == model.NumberingUUID {
			values[b.keyFields(e.Table)[0]] = newUUID()
		}
		var parent *instance
		if p, ok := e.Parent.Get(); ok {
			pe := bo.Entity(p)
			if l, ok := row[model.ParentLinkField].(string); ok {
				parent = links[l]
				if parent != nil && parent.entity != pe {
					parent = nil
				}
			} else {
				fk := b.foreignKey(e)
				pk := map[string]any{}
				for _, c := range fk.Columns {
					pk[c.References] = row[c.Field]
				}
				var err error
				if parent, err = b.find(ctx, bo, pe, pk); err != nil {
					return nil, err
				}
			}
			if parent == nil {
				r.fail(e.Table, "create", reasonParentNotFound, nil, link)
				continue
			}
			for _, c := range b.foreignKey(e).Columns {
				if c.Field != model.TenantField {
					values[c.Field] = parent.values[c.References]
				}
			}
		}
		key, ok := b.keyOf(e.Table, values)
		if !ok {
			r.fail(e.Table, "create", reasonMandatory, nil, link)
			r.message("error", "the key of a new instance of "+e.Table+" has no value", e.Table, nil, nil)
			continue
		}
		existing := b.instances[bufferKey(e.Table, key)]
		if existing == nil || existing.state != stateDeleted {
			found, err := b.find(ctx, bo, e, values)
			if err != nil {
				return nil, err
			}
			if found != nil {
				r.fail(e.Table, "create", reasonAlreadyExists, strPtr(key), link)
				continue
			}
		}
		if ok, err := b.authorize(ctx, req.Local, bo, e, model.OperationCreate, values, parent, req.Caller); err != nil {
			return nil, err
		} else if !ok {
			r.fail(e.Table, "create", reasonNotAuthorized, strPtr(key), link)
			continue
		}
		var in *instance
		if existing != nil && existing.state == stateDeleted {
			in = existing
			in.values, in.state = values, stateUpdated
			for _, c := range b.columns(e.Table) {
				in.changed[c.Name] = true
			}
			in.ops[model.OperationUpdate] = true
			in.recentOps[model.OperationUpdate] = true
		} else {
			in = b.add(bo, e, values, stateCreated)
			in.ops[model.OperationCreate] = true
			in.recentOps[model.OperationCreate] = true
		}
		in.link = link
		for _, f := range st.Fields {
			in.changed[f] = true
			in.recent[f] = true
		}
		if link != nil {
			links[*link] = in
		}
		r.Created = append(r.Created, vm.EntityCreated{Entity: e.Table, Link: link, Key: key})
		out = append(out, in)
	}
	return out, nil
}

// update runs an update step: only the given fields change.
func (b *entityBuffer) update(ctx context.Context, bo *model.BusinessObject, e *model.Entity, st vm.ModifyStep, req vm.ModifyRequest, r *result) ([]*instance, error) {
	var out []*instance
	for _, row := range st.Rows {
		in, err := b.find(ctx, bo, e, row)
		if err != nil {
			return nil, err
		}
		key, _ := b.keyOf(e.Table, row)
		if in == nil {
			r.fail(e.Table, "update", reasonNotFound, strPtr(key), nil)
			continue
		}
		after := mapsClone(in.values)
		for _, f := range st.Fields {
			after[f] = row[f]
		}
		if ok, err := b.authorizeChange(ctx, req, bo, e, model.OperationUpdate, in, after); err != nil {
			return nil, err
		} else if !ok {
			r.fail(e.Table, "update", reasonNotAuthorized, strPtr(key), nil)
			continue
		}
		for _, f := range st.Fields {
			if fmt.Sprint(in.values[f]) != fmt.Sprint(row[f]) || in.values[f] == nil != (row[f] == nil) {
				in.changed[f] = true
				in.recent[f] = true
			}
			in.values[f] = row[f]
		}
		if in.state == stateRead {
			in.state = stateUpdated
		}
		// A new instance stays a creation for the save; on modify the
		// statement updated it.
		if in.state != stateCreated {
			in.ops[model.OperationUpdate] = true
		}
		in.recentOps[model.OperationUpdate] = true
		out = append(out, in)
	}
	return out, nil
}

func mapsClone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// delete runs a delete step: the instances and all their dependent
// instances.
func (b *entityBuffer) delete(ctx context.Context, bo *model.BusinessObject, e *model.Entity, st vm.ModifyStep, req vm.ModifyRequest, r *result) ([]*instance, error) {
	var out []*instance
	for _, row := range st.Rows {
		in, err := b.find(ctx, bo, e, row)
		if err != nil {
			return nil, err
		}
		key, _ := b.keyOf(e.Table, row)
		if in == nil {
			r.fail(e.Table, "delete", reasonNotFound, strPtr(key), nil)
			continue
		}
		if ok, err := b.authorizeChange(ctx, req, bo, e, model.OperationDelete, in, nil); err != nil {
			return nil, err
		} else if !ok {
			r.fail(e.Table, "delete", reasonNotAuthorized, strPtr(key), nil)
			continue
		}
		done, err := b.remove(ctx, bo, in)
		if err != nil {
			return nil, err
		}
		out = append(out, done...)
	}
	return out, nil
}

// remove deletes an instance and its dependent instances in the buffer.
func (b *entityBuffer) remove(ctx context.Context, bo *model.BusinessObject, in *instance) ([]*instance, error) {
	var out []*instance
	for _, child := range bo.Children(in.entity.Table) {
		kids, err := b.children(ctx, bo, child, in)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			done, err := b.remove(ctx, bo, k)
			if err != nil {
				return nil, err
			}
			out = append(out, done...)
		}
	}
	if in.state == stateCreated {
		delete(b.instances, bufferKey(in.entity.Table, in.key))
		b.order = slices.DeleteFunc(b.order, func(x *instance) bool { return x == in })
		return out, nil
	}
	in.state = stateDeleted
	in.ops[model.OperationDelete] = true
	in.recentOps[model.OperationDelete] = true
	return append(out, in), nil
}

// Read runs read business object.
func (b *entityBuffer) Read(ctx context.Context, req vm.ReadRequest) ([]map[string]any, *vm.EntityResult, error) {
	bo := b.x.schema.BusinessObjects[req.BusinessObject]
	if bo == nil {
		return nil, nil, fmt.Errorf("business object %s is not installed", req.BusinessObject)
	}
	e := bo.Entity(req.Entity)
	if e == nil {
		return nil, nil, fmt.Errorf("%s is no entity of %s", req.Entity, bo.Name)
	}
	r := &result{}
	var found []*instance
	if req.ByParent {
		pe := bo.Entity(e.Parent.Or(""))
		for _, k := range req.Keys {
			parent, err := b.find(ctx, bo, pe, k)
			if err != nil {
				return nil, nil, err
			}
			if parent == nil {
				key, _ := b.keyOf(pe.Table, k)
				r.fail(pe.Table, "read", reasonNotFound, strPtr(key), nil)
				continue
			}
			kids, err := b.children(ctx, bo, e, parent)
			if err != nil {
				return nil, nil, err
			}
			found = append(found, kids...)
		}
	} else {
		for _, k := range req.Keys {
			in, err := b.find(ctx, bo, e, k)
			if err != nil {
				return nil, nil, err
			}
			if in == nil {
				key, _ := b.keyOf(e.Table, k)
				r.fail(e.Table, "read", reasonNotFound, strPtr(key), nil)
				continue
			}
			found = append(found, in)
		}
	}
	var out []map[string]any
	for _, in := range found {
		ok, err := b.authorizeChange(ctx, vm.ModifyRequest{Local: req.Local, Caller: req.Caller}, bo, in.entity, model.ReadActivityKey, in, nil)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			r.fail(in.entity.Table, "read", reasonNotAuthorized, strPtr(in.key), nil)
			continue
		}
		out = append(out, mapsClone(in.values))
	}
	return out, &r.EntityResult, nil
}

// Discard drops the buffer and rolls back the default session.
func (b *entityBuffer) Discard(ctx context.Context) error {
	b.instances, b.order = map[string]*instance{}, nil
	return b.x.sessions[0].Rollback(ctx)
}

// entityStore gives the VM the transaction buffer of an execution, made
// when an entity statement first needs it.
type entityStore struct{ x *execution }

func (s *entityStore) Modify(ctx context.Context, req vm.ModifyRequest) (*vm.EntityResult, []any, error) {
	return s.x.buffer().Modify(ctx, req)
}

func (s *entityStore) Read(ctx context.Context, req vm.ReadRequest) ([]map[string]any, *vm.EntityResult, error) {
	return s.x.buffer().Read(ctx, req)
}

func (s *entityStore) Save(ctx context.Context, b vm.Behavior) (*vm.EntityResult, bool, error) {
	return s.x.buffer().Save(ctx, b)
}

func (s *entityStore) Discard(ctx context.Context) error { return s.x.buffer().Discard(ctx) }

func (s *entityStore) Pending() bool { return s.x.entities != nil && s.x.entities.Pending() }

// endBuffer ends the transaction buffer with the execution: a buffer with
// changes is saved when the execution commits at its end, and a failed
// save fails the execution, which rolls back. Otherwise the changes are
// dropped.
func (x *execution) endBuffer(ctx context.Context, m *vm.Machine) error {
	if x.entities == nil || !x.entities.Pending() {
		return nil
	}
	if x.test || x.opts.End != CommitOnSuccess || x.opts.Rollback != nil && x.opts.Rollback() {
		x.entities.instances, x.entities.order = map[string]*instance{}, nil
		return nil
	}
	r, ok, err := x.entities.Save(ctx, m)
	if err != nil {
		return err
	}
	if !ok {
		return &rterr.BusinessObjectsNotSaved{Summary: r.Summary()}
	}
	return nil
}
