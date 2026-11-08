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
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// Save runs the save sequence: determinations on save, validations,
// mandatory fields, administrative fields and ETags, writing every change
// in a savepoint of the default session and its commit. When anything
// fails, nothing is written and the buffer stays.
func (b *entityBuffer) Save(ctx context.Context, behavior vm.Behavior) (*vm.EntityResult, bool, error) {
	r := &result{}
	if err := b.determine(ctx, behavior, model.WhenSave, b.changedInstances(), r); err != nil {
		return nil, false, err
	}
	if err := b.validate(ctx, behavior, r); err != nil {
		return nil, false, err
	}
	b.checkMandatory(r)
	if r.Failed() {
		return &r.EntityResult, false, nil
	}
	ok, err := b.write(ctx, r)
	if err != nil || !ok {
		return &r.EntityResult, false, err
	}
	if err := b.x.sessions[0].Commit(ctx); err != nil {
		return nil, false, err
	}
	b.instances, b.order = map[string]*instance{}, nil
	return &r.EntityResult, true, nil
}

// changedInstances returns the instances changed since the last save.
func (b *entityBuffer) changedInstances() []*instance {
	var out []*instance
	for _, in := range b.order {
		if in.state != stateRead {
			out = append(out, in)
		}
	}
	return out
}

// checkMandatory fails instances whose mandatory fields or key fields have
// no value: null, or "" for text.
func (b *entityBuffer) checkMandatory(r *result) {
	for _, in := range b.order {
		if in.state != stateCreated && in.state != stateUpdated {
			continue
		}
		var fields []string
		fields = append(fields, b.keyFields(in.entity.Table)...)
		for _, f := range slicesSorted(in.entity.Fields) {
			if in.entity.Fields[f].Mandatory && !slices.Contains(fields, f) {
				fields = append(fields, f)
			}
		}
		for _, f := range fields {
			if v := in.values[f]; v == nil || v == "" && b.isText(in.entity.Table, f) {
				r.fail(in.entity.Table, saveOperation(in), reasonMandatory, strPtr(in.key), in.link)
				r.message("error", fmt.Sprintf("%s of %s needs a value", f, in.entity.Table), in.entity.Table, strPtr(in.key), strPtr(f))
			}
		}
	}
}

func (b *entityBuffer) isText(table, field string) bool {
	d := b.domain(table, field)
	return d != nil && (d.Type == model.Char || d.Type == model.String)
}

func slicesSorted(m map[string]model.FieldProperties) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// saveOperation names the operation of an instance in the result of a
// save.
func saveOperation(in *instance) string {
	switch in.state {
	case stateCreated:
		return "create"
	case stateDeleted:
		return "delete"
	}
	return "update"
}

// depth returns the depth of an entity in the tree of its business object.
func depth(bo *model.BusinessObject, e *model.Entity) int {
	n := 0
	for p, ok := e.Parent.Get(); ok; p, ok = bo.Entity(p).Parent.Get() {
		n++
	}
	return n
}

// write writes the changes in a savepoint of the default session:
// deletions of children first, creations of parents first, then updates
// of the changed fields. Updates and deletions of entities with ETag
// check that the instance did not change since it was read. On a failure
// everything is rolled back to the savepoint.
func (b *entityBuffer) write(ctx context.Context, r *result) (bool, error) {
	s := b.x.sessions[0]
	now := b.x.k.timestamp()
	var user any
	if u, ok := b.x.opts.User.Get(); ok {
		user = u
	}
	var deletes, creates, updates []*instance
	for _, in := range b.order {
		switch in.state {
		case stateDeleted:
			deletes = append(deletes, in)
		case stateCreated:
			creates = append(creates, in)
		case stateUpdated:
			updates = append(updates, in)
		}
	}
	slices.SortStableFunc(deletes, func(x, y *instance) int { return depth(y.bo, y.entity) - depth(x.bo, x.entity) })
	slices.SortStableFunc(creates, func(x, y *instance) int { return depth(x.bo, x.entity) - depth(y.bo, y.entity) })
	if err := s.savepoint(ctx, "rt_save"); err != nil {
		return false, err
	}
	// written are the values with administrative fields and ETags, which
	// the buffer takes over only when everything is written.
	written := map[*instance]map[string]any{}
	failed := false
	fail := func(in *instance, reason string, err error) {
		failed = true
		r.fail(in.entity.Table, saveOperation(in), reason, strPtr(in.key), in.link)
		if err != nil {
			r.message("error", err.Error(), in.entity.Table, strPtr(in.key), nil)
		}
	}
	for _, in := range deletes {
		n, err := b.exec(ctx, in, "delete", nil)
		switch {
		case isDatabaseError(err):
			fail(in, reasonDatabase, err)
		case err != nil:
			return false, err
		case n == 0:
			fail(in, b.lost(in), nil)
		}
	}
	for _, in := range creates {
		values := b.stamped(in, now, user, true)
		written[in] = values
		if _, err := b.exec(ctx, in, "insert", values); isDatabaseError(err) {
			fail(in, reasonDatabase, err)
		} else if err != nil {
			return false, err
		}
	}
	for _, in := range updates {
		values := b.stamped(in, now, user, false)
		written[in] = values
		n, err := b.exec(ctx, in, "update", values)
		switch {
		case isDatabaseError(err):
			fail(in, reasonDatabase, err)
		case err != nil:
			return false, err
		case n == 0:
			fail(in, b.lost(in), nil)
		}
	}
	if failed {
		return false, s.rollbackTo(ctx, "rt_save")
	}
	for in, values := range written {
		in.values = values
	}
	return true, s.release(ctx, "rt_save")
}

// lost tells why an update or deletion found no row: another execution
// changed it, as its ETag tells, or deleted it.
func (b *entityBuffer) lost(in *instance) string {
	if in.entity.ETag.Valid() {
		return reasonETag
	}
	return reasonNotFound
}

// stamped returns the values of an instance with the administrative
// fields and a new ETag.
func (b *entityBuffer) stamped(in *instance, now any, user any, created bool) map[string]any {
	out := mapsClone(in.values)
	a := in.entity.Administrative
	set := func(f model.Optional[string], v any) {
		if name, ok := f.Get(); ok {
			out[name] = v
			in.changed[name] = true
		}
	}
	if created {
		set(a.CreatedBy, user)
		set(a.CreatedAt, now)
	}
	set(a.ChangedBy, user)
	set(a.ChangedAt, now)
	if f, ok := in.entity.ETag.Get(); ok {
		switch b.domain(in.entity.Table, f).Type {
		case model.Timestamp:
			out[f] = now
		case model.UUID:
			out[f] = newUUID()
		case model.Int8:
			n, _ := in.values[f].(int64)
			out[f] = n + 1
		}
		in.changed[f] = true
	}
	return out
}

func isDatabaseError(err error) bool {
	var dbe *vm.DatabaseError
	return errors.As(err, &dbe)
}

// exec writes one instance: insert all columns, update the changed ones,
// or delete; updates and deletions match the key and the ETag read.
func (b *entityBuffer) exec(ctx context.Context, in *instance, op string, values map[string]any) (int64, error) {
	d := b.x.d
	table := in.entity.Table
	var args []any
	bind := func(field string, v any) (string, error) {
		enc, err := d.Encode(b.domain(table, field), v)
		if err != nil {
			return "", &vm.DatabaseError{Class: "/RT/CHECK_VIOLATION", Text: fmt.Sprintf("%s.%s: %v", table, field, err), Table: table}
		}
		args = append(args, enc)
		return d.Placeholder(len(args)), nil
	}
	var where []string
	match := func() error {
		if b.table(table).TenantSpecific {
			ph, _ := bind(model.TenantField, b.x.tenant)
			where = append(where, d.QuoteIdent(model.TenantField)+" = "+ph)
		}
		for _, k := range b.keyFields(table) {
			ph, err := bind(k, in.values[k])
			if err != nil {
				return err
			}
			where = append(where, d.QuoteIdent(k)+" = "+ph)
		}
		if f, ok := in.entity.ETag.Get(); ok {
			if in.etag == nil {
				where = append(where, d.QuoteIdent(f)+" IS NULL")
			} else {
				ph, err := bind(f, in.etag)
				if err != nil {
					return err
				}
				where = append(where, d.QuoteIdent(f)+" = "+ph)
			}
		}
		return nil
	}
	var text string
	switch op {
	case "insert":
		var cols, phs []string
		if b.table(table).TenantSpecific {
			ph, _ := bind(model.TenantField, b.x.tenant)
			cols, phs = append(cols, d.QuoteIdent(model.TenantField)), append(phs, ph)
		}
		for _, c := range b.columns(table) {
			ph, err := bind(c.Name, values[c.Name])
			if err != nil {
				return 0, err
			}
			cols, phs = append(cols, d.QuoteIdent(c.Name)), append(phs, ph)
		}
		text = "INSERT INTO " + d.QuoteIdent(table) + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(phs, ", ") + ")"
	case "update":
		var sets []string
		for _, c := range b.columns(table) {
			if !in.changed[c.Name] {
				continue
			}
			ph, err := bind(c.Name, values[c.Name])
			if err != nil {
				return 0, err
			}
			sets = append(sets, d.QuoteIdent(c.Name)+" = "+ph)
		}
		if len(sets) == 0 {
			return 1, nil
		}
		if err := match(); err != nil {
			return 0, err
		}
		text = "UPDATE " + d.QuoteIdent(table) + " SET " + strings.Join(sets, ", ") + " WHERE " + strings.Join(where, " AND ")
	case "delete":
		if err := match(); err != nil {
			return 0, err
		}
		text = "DELETE FROM " + d.QuoteIdent(table) + " WHERE " + strings.Join(where, " AND ")
	}
	return b.x.sessions[0].rawExec(ctx, text, args)
}
