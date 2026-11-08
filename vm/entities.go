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

package vm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// EntityStore is the transaction buffer of the business objects of an
// execution; the kernel implements it. Rows are maps from field to value
// in the Go form of Database.
type EntityStore interface {
	// Modify runs the steps of modify business object and returns the
	// result and, for every step, the result of its action (nil for other
	// steps and actions without result).
	Modify(ctx context.Context, req ModifyRequest) (*EntityResult, []any, error)
	// Read returns the rows of the instances of read business object.
	Read(ctx context.Context, req ReadRequest) ([]map[string]any, *EntityResult, error)
	// Save runs the save sequence and reports whether it succeeded.
	Save(ctx context.Context, b Behavior) (*EntityResult, bool, error)
	// Discard drops the buffer and rolls back the default session.
	Discard(ctx context.Context) error
	// Pending reports whether the buffer holds changes.
	Pending() bool
}

// Behavior runs methods of behavior classes for the transaction buffer.
type Behavior interface {
	// CallBehavior runs a method of the behavior object of a class with
	// the keys of instances (nil for static actions), the parameter (nil
	// without) and a response, and returns its result in Go form.
	CallBehavior(ctx context.Context, class, method string, keys []map[string]any, param any, response *native.EntityResponse) (any, error)
}

// ModifyRequest describes modify business object.
type ModifyRequest struct {
	BusinessObject string
	Local          bool
	Steps          []ModifyStep
	Behavior       Behavior
	Caller         native.CheckLocation
}

// ModifyStep is a step of modify business object.
type ModifyStep struct {
	Op       string
	Entity   string
	ByParent bool
	Fields   []string
	Rows     []map[string]any // rows of create and update, keys of the others
	Action   string
	Param    any // the parameter of execute in Go form, nil without
}

// ReadRequest describes read business object.
type ReadRequest struct {
	BusinessObject string
	Local          bool
	Entity         string
	ByParent       bool
	Fields         []string
	Keys           []map[string]any
	Caller         native.CheckLocation
}

// EntityResult is the result of entity statements: new instances,
// failed instances and messages.
type EntityResult struct {
	Created  []EntityCreated
	Failures []EntityFailure
	Messages []EntityMessage
}

// EntityCreated is a new instance with its link and key as JSON.
type EntityCreated struct {
	Entity string
	Link   *string
	Key    string
}

// EntityFailure is an instance an entity statement failed for.
type EntityFailure struct {
	Entity    string
	Operation string
	Link      *string
	Key       *string
	Reason    string
	// Origin is the package of the behavior that failed the instance;
	// nil for failures of the kernel.
	Origin *string
}

// EntityMessage is a message of an entity statement.
type EntityMessage struct {
	Severity string
	Text     string
	Entity   *string
	Key      *string
	Link     *string
	Field    *string
	// Origin is the package of the behavior that gave the message; nil
	// for messages of the kernel.
	Origin *string
}

// Failed reports whether the result has failures or error messages.
func (r *EntityResult) Failed() bool {
	if len(r.Failures) > 0 {
		return true
	}
	for _, m := range r.Messages {
		if m.Severity == "error" {
			return true
		}
	}
	return false
}

// Summary writes the failures and messages for the text of an exception.
func (r *EntityResult) Summary() string {
	var parts []string
	for _, f := range r.Failures {
		s := f.Operation + " " + f.Entity
		if f.Key != nil {
			s += " " + *f.Key
		}
		parts = append(parts, s+": "+f.Reason)
	}
	for _, m := range r.Messages {
		if m.Severity == "error" {
			parts = append(parts, m.Text)
		}
	}
	return strings.Join(parts, "; ")
}

// goValue returns the result in the Go form of /TRBASE/ENTITY_RESULT.
func (r *EntityResult) goValue() map[string]any {
	str := func(s *string) any {
		if s == nil {
			return nil
		}
		return *s
	}
	created, failures, messages := []any{}, []any{}, []any{}
	for _, c := range r.Created {
		created = append(created, map[string]any{"ENTITY": c.Entity, "LINK": str(c.Link), "KEY": c.Key})
	}
	for _, f := range r.Failures {
		failures = append(failures, map[string]any{"ENTITY": f.Entity, "OPERATION": f.Operation, "LINK": str(f.Link), "KEY": str(f.Key), "REASON": f.Reason, "ORIGIN": str(f.Origin)})
	}
	for _, m := range r.Messages {
		messages = append(messages, map[string]any{"SEVERITY": m.Severity, "TEXT": m.Text, "ENTITY": str(m.Entity), "KEY": str(m.Key),
			"LINK": str(m.Link), "FIELD": str(m.Field), "MESSAGE": nil, "ARGUMENTS": []any{}, "ORIGIN": str(m.Origin)})
	}
	return map[string]any{"CREATED": created, "FAILURES": failures, "MESSAGES": messages}
}

// entity runs ENTITY. It returns the values to push.
func (m *Machine) entity(fn *LinkedFunc, pc int, es *EntityStmt, args []Value) ([]Value, *thrown, error) {
	store := m.opts.Entities
	if store == nil {
		return nil, nil, errors.New("vm: the execution has no transaction buffer for business objects")
	}
	p := fn.Prog.P
	class, method, _ := strings.Cut(fn.Def.Name, ".")
	caller := native.CheckLocation{Package: model.PackageID(fn.Prog.P.Package), Class: class, Method: method}
	if l, ok := fn.Def.Position(pc); ok {
		caller.Line = l.Line
	}
	result := func(r *EntityResult) (Value, error) {
		if es.Result < 0 {
			return Value{}, nil
		}
		return m.fromGoDeep(p, es.Result, r.goValue())
	}
	var out []Value
	switch es.Kind {
	case EntityModify:
		req := ModifyRequest{BusinessObject: es.BusinessObject, Local: es.Local, Behavior: m, Caller: caller}
		i := 0
		for _, st := range es.Steps {
			ms := ModifyStep{Op: st.Op, Entity: st.Entity, ByParent: st.ByParent, Fields: st.Fields, Action: st.Action}
			if st.Rows >= 0 {
				ms.Rows = rowMaps(m.toGoDeep(p, st.Rows, args[i]))
				i++
			}
			if st.Param >= 0 {
				ms.Param = m.toGoDeep(p, st.Param, args[i])
				i++
			}
			req.Steps = append(req.Steps, ms)
		}
		r, intos, err := store.Modify(m.ctx, req)
		if err != nil {
			th, ferr := m.storeError(err)
			return nil, th, ferr
		}
		for j, st := range es.Steps {
			if st.Into < 0 {
				continue
			}
			var x any
			if j < len(intos) {
				x = intos[j]
			}
			v, err := m.fromGoDeep(p, st.Into, x)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		}
		if es.Result >= 0 {
			v, err := result(r)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		}
	case EntityRead:
		st := es.Steps[0]
		rows, r, err := store.Read(m.ctx, ReadRequest{BusinessObject: es.BusinessObject, Local: es.Local, Entity: st.Entity, ByParent: st.ByParent,
			Fields: st.Fields, Keys: rowMaps(m.toGoDeep(p, st.Rows, args[0])), Caller: caller})
		if err != nil {
			th, ferr := m.storeError(err)
			return nil, th, ferr
		}
		list := make([]any, len(rows))
		for i, row := range rows {
			list[i] = row
		}
		v, err := m.fromGoDeep(p, st.Into, list)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, v)
		if es.Result >= 0 {
			rv, err := result(r)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, rv)
		}
	case EntitySave:
		if n := len(m.noCommit); n > 0 {
			return nil, m.fail("/RT/SESSION_ERROR", "commit business objects is not allowed in an implementation of extension point %s", m.noCommit[n-1]), nil
		}
		r, ok, err := store.Save(m.ctx, m)
		if err != nil {
			th, ferr := m.storeError(err)
			return nil, th, ferr
		}
		if es.Result >= 0 {
			v, err := result(r)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		} else if !ok {
			return nil, m.fail("/RT/SAVE_FAILED", "business objects could not be saved: %s", r.Summary()), nil
		}
	case EntityDiscard:
		if err := store.Discard(m.ctx); err != nil {
			th, ferr := m.storeError(err)
			return nil, th, ferr
		}
	}
	return out, nil, nil
}

// storeError turns an error of the transaction buffer into an exception
// or a fatal error.
func (m *Machine) storeError(err error) (*thrown, error) {
	var dbe *DatabaseError
	if errors.As(err, &dbe) {
		return m.dbError(err)
	}
	var re *native.RuntimeError
	if errors.As(err, &re) {
		return m.fail(re.Class, "%s", re.Text), nil
	}
	return nil, err
}

func rowMaps(x any) []map[string]any {
	list, _ := x.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		if row, ok := e.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

// toGoDeep converts a value into its Go form, structs into maps from
// component to value and arrays into lists.
func (m *Machine) toGoDeep(p *Program, t int, v Value) any {
	d := &p.Types[t]
	if v.IsNull() {
		return nil
	}
	switch d.Kind {
	case TStruct:
		r := v.record()
		l := &p.Layouts[d.Layout]
		out := make(map[string]any, len(l.Fields))
		for _, f := range l.Fields {
			out[f.Name] = m.toGoDeep(p, f.Type, m.getField(r, f))
		}
		return out
	case TArray:
		a := v.array()
		out := make([]any, a.Len())
		for i := range out {
			out[i] = m.toGoDeep(p, d.Elem, a.Get(i))
		}
		return out
	}
	return m.toGo(p, t, v)
}

// fromGoDeep converts a Go form into a value of type t: maps into structs,
// whose missing components take their zero value or null, and lists into
// arrays.
func (m *Machine) fromGoDeep(p *Program, t int, x any) (Value, error) {
	d := &p.Types[t]
	if x == nil {
		if d.Optional {
			return Null, nil
		}
		return m.zero(p, t), nil
	}
	switch d.Kind {
	case TStruct:
		row, ok := x.(map[string]any)
		if !ok {
			return Value{}, fmt.Errorf("vm: %T for a structure", x)
		}
		l := &p.Layouts[d.Layout]
		r := m.newRecord(nil, p, l)
		for _, f := range l.Fields {
			v, err := m.fromGoDeep(p, f.Type, row[f.Name])
			if err != nil {
				return Value{}, fmt.Errorf("%s: %w", f.Name, err)
			}
			m.setField(r, f, v)
		}
		return Value{R: r}, nil
	case TArray:
		list, ok := x.([]any)
		if !ok {
			return Value{}, fmt.Errorf("vm: %T for an array", x)
		}
		a := m.newArray(p, t)
		for _, e := range list {
			v, err := m.fromGoDeep(p, d.Elem, e)
			if err != nil {
				return Value{}, err
			}
			if th := m.appendElem(a, v); th != nil {
				return Value{}, errors.New(m.getField(th.obj, m.image.textField).str())
			}
		}
		return Value{R: a}, nil
	}
	return m.fromGo(p, t, x)
}

// CallBehavior runs a method of the behavior object of a class for the
// transaction buffer: one object per class and execution, created when it
// is first needed. The parameters are the keys (when keys is not nil), the
// parameter (when the method declares one) and the response.
func (m *Machine) CallBehavior(ctx context.Context, class, method string, keys []map[string]any, param any, response *native.EntityResponse) (any, error) {
	c := m.image.Class(class)
	if c == nil {
		return nil, fmt.Errorf("vm: the behavior class %s is not installed", class)
	}
	fn, ok := c.Methods[method]
	if !ok {
		return nil, fmt.Errorf("vm: %s has no method %s", class, method)
	}
	obj, ok := m.behaviors[c]
	if !ok {
		if c.Ctor == nil {
			return nil, fmt.Errorf("vm: %s has no constructor", class)
		}
		obj = Value{R: m.newObject(c)}
		if _, err := m.run(c.Ctor, obj, nil, nil); err != nil {
			return nil, err
		}
		if m.behaviors == nil {
			m.behaviors = map[*LinkedClass]Value{}
		}
		m.behaviors[c] = obj
	}
	p := fn.Prog.P
	types := fn.Def.ParamTypes
	if len(types) == 0 {
		return nil, fmt.Errorf("vm: %s.%s takes no response", class, method)
	}
	var args []Value
	i := 0
	if keys != nil {
		list := make([]any, len(keys))
		for j, k := range keys {
			list[j] = k
		}
		v, err := m.fromGoDeep(p, types[i], list)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		i++
	}
	if i < len(types)-1 {
		v, err := m.fromGoDeep(p, types[i], param)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	resp, err := m.NewNative(native.EntityResponseClass, response)
	if err != nil {
		return nil, err
	}
	args = append(args, resp)
	saved := m.ctx
	if ctx != nil {
		m.ctx = ctx
	}
	defer func() { m.ctx = saved }()
	v, err := m.run(fn, obj, args, nil)
	if err != nil {
		return nil, err
	}
	if fn.Def.Result < 0 {
		return nil, nil
	}
	return m.toGoDeep(p, fn.Def.Result, v), nil
}
