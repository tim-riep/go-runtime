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
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/vm"
)

// maxRounds limits how often determinations trigger each other.
const maxRounds = 10

// triggered reports whether changes trigger a determination or a
// validation: one of its operations, and for update one of its fields
// when it names fields.
func triggered(t model.Trigger, ops map[model.EntityOperation]bool, fields map[string]bool) bool {
	for _, op := range t.On {
		if !ops[op] {
			continue
		}
		if op != model.OperationUpdate || len(t.Fields) == 0 {
			return true
		}
		for _, f := range t.Fields {
			if fields[f] {
				return true
			}
		}
	}
	return false
}

// keyMap returns the key fields of an instance.
func (b *entityBuffer) keyMap(in *instance) map[string]any {
	out := map[string]any{}
	for _, k := range b.keyFields(in.entity.Table) {
		out[k] = in.values[k]
	}
	return out
}

// behaviorCall is a call of a method of the behavior class.
type behaviorCall struct {
	bo     *model.BusinessObject
	entity *model.Entity
	method string
	ins    []*instance
	static bool   // static actions get no keys
	param  any    // the parameter of an action in Go form
	op     string // the operation in failures; the save operation when empty
	reason string // the reason of instances the response fails
}

// call runs a method of the behavior class for instances and records what
// the response says: failed instances with reason and the messages. A
// runtime error of the method fails all its instances with reason
// BEHAVIOR. It returns the result of the method in Go form.
func (b *entityBuffer) call(ctx context.Context, behavior vm.Behavior, c behaviorCall, r *result) (any, map[*instance]bool, error) {
	var keys []map[string]any
	if !c.static {
		keys = make([]map[string]any, len(c.ins))
		for i, in := range c.ins {
			keys[i] = b.keyMap(in)
		}
	}
	resp := native.NewEntityResponse(len(c.ins))
	out, err := behavior.CallBehavior(ctx, c.bo.BehaviorClass, c.method, keys, c.param, resp)
	failed := map[*instance]bool{}
	var exc *vm.Exception
	switch {
	case errors.As(err, &exc):
		for _, in := range c.ins {
			failed[in] = true
			r.fail(in.entity.Table, c.operation(in), reasonBehavior, strPtr(in.key), in.link)
		}
		if c.static {
			r.fail(c.entity.Table, c.op, reasonBehavior, nil, nil)
		}
		r.message("error", fmt.Sprintf("%s.%s: %s", c.bo.BehaviorClass, c.method, exc.Text), "", nil, nil)
		return nil, failed, nil
	case err != nil:
		return nil, nil, err
	}
	b.respond(resp, c, r, failed)
	return out, failed, nil
}

func (c behaviorCall) operation(in *instance) string {
	if c.op != "" {
		return c.op
	}
	return saveOperation(in)
}

// respond records the failed instances and messages of a response.
func (b *entityBuffer) respond(resp *native.EntityResponse, c behaviorCall, r *result, failed map[*instance]bool) {
	ins := c.ins
	for _, pos := range slices.Sorted(maps.Keys(resp.Failed)) {
		in := ins[pos-1]
		failed[in] = true
		r.fail(in.entity.Table, c.operation(in), c.reason, strPtr(in.key), in.link)
	}
	for _, m := range resp.Messages {
		msg := vm.EntityMessage{Severity: m.Severity, Text: m.Text}
		if m.Position > 0 {
			in := ins[m.Position-1]
			msg.Entity, msg.Key, msg.Link = strPtr(in.entity.Table), strPtr(in.key), in.link
		}
		if f, ok := m.Field.Get(); ok {
			msg.Field = strPtr(f)
		}
		r.Messages = append(r.Messages, msg)
	}
}

// determine runs the determinations of a time. On modify they run for the
// instances the statement changed, as its fields and operations trigger
// them; their own changes in local mode do not trigger them again. On
// save they run for every instance changed since the last save and then
// for instances that other determinations change, each determination at
// most once per instance and save, in at most maxRounds rounds.
func (b *entityBuffer) determine(ctx context.Context, behavior vm.Behavior, when model.When, touched []*instance, r *result) error {
	if behavior == nil || len(touched) == 0 {
		return nil
	}
	if b.running == nil {
		b.running = map[string]bool{}
	}
	done := map[string]map[*instance]bool{}
	for round := 0; ; round++ {
		ran := false
		for _, bo := range b.businessObjects(touched) {
			for i := range bo.Entities {
				e := &bo.Entities[i]
				for _, d := range e.Determinations {
					if d.When != when || b.running[bo.Name+"."+d.Name] {
						continue
					}
					var ins []*instance
					for _, in := range touched {
						ops, fields := in.ops, in.changed
						if when == model.WhenModify {
							ops, fields = in.recentOps, in.recent
						}
						if in.entity.Table != e.Table || in.bo != bo || done[d.Name][in] || in.state == stateDeleted && !slices.Contains(d.On, model.OperationDelete) {
							continue
						}
						if triggered(d.Trigger, ops, fields) {
							ins = append(ins, in)
						}
					}
					if len(ins) == 0 {
						continue
					}
					if round >= maxRounds {
						return &native.RuntimeError{Class: "/RT/VALUE_ERROR", Text: fmt.Sprintf("determination %s ran more than %d times in one save; determinations trigger each other", d.Name, maxRounds)}
					}
					if done[d.Name] == nil {
						done[d.Name] = map[*instance]bool{}
					}
					for _, in := range ins {
						done[d.Name][in] = true
					}
					key := bo.Name + "." + d.Name
					b.running[key] = true
					_, _, err := b.call(ctx, behavior, behaviorCall{bo: bo, entity: e, method: d.Name, ins: ins, reason: reasonBehavior}, r)
					delete(b.running, key)
					if err != nil {
						return err
					}
					ran = true
				}
			}
		}
		if !ran || when == model.WhenModify {
			return nil
		}
		touched = b.changedInstances()
	}
}

// businessObjects returns the business objects of instances in the order
// of their names.
func (b *entityBuffer) businessObjects(ins []*instance) []*model.BusinessObject {
	var out []*model.BusinessObject
	for _, in := range ins {
		if !slices.Contains(out, in.bo) {
			out = append(out, in.bo)
		}
	}
	slices.SortFunc(out, func(x, y *model.BusinessObject) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// validate runs the validations in the save sequence, once, for the
// instances changed since the last save. Failed instances fail the save.
func (b *entityBuffer) validate(ctx context.Context, behavior vm.Behavior, r *result) error {
	if behavior == nil {
		return nil
	}
	changed := b.changedInstances()
	for _, bo := range b.businessObjects(changed) {
		for i := range bo.Entities {
			e := &bo.Entities[i]
			for _, v := range e.Validations {
				var ins []*instance
				for _, in := range changed {
					if in.bo == bo && in.entity.Table == e.Table && triggered(v.Trigger, in.ops, in.changed) {
						ins = append(ins, in)
					}
				}
				if len(ins) == 0 {
					continue
				}
				if _, _, err := b.call(ctx, behavior, behaviorCall{bo: bo, entity: e, method: v.Name, ins: ins, reason: reasonValidation}, r); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// execute runs an action: an instance action for the instances of the
// keys that exist and the caller may run it on, a static action once.
// Changes of the action are statements in local mode, which run their own
// determinations. The result is the method's or, for result self, the
// instances after the action without those that failed.
func (b *entityBuffer) execute(ctx context.Context, bo *model.BusinessObject, e *model.Entity, st vm.ModifyStep, req vm.ModifyRequest, r *result) (any, []*instance, error) {
	a := e.Action(st.Action)
	if a == nil {
		return nil, nil, fmt.Errorf("%s has no action %s", e.Table, st.Action)
	}
	if req.Behavior == nil {
		return nil, nil, fmt.Errorf("action %s needs the behavior class of %s", a.Name, bo.Name)
	}
	c := behaviorCall{bo: bo, entity: e, method: a.Name, static: !a.Instance, param: st.Param, op: a.Name, reason: reasonBehavior}
	if a.Instance {
		seen := map[*instance]bool{}
		for _, k := range st.Rows {
			in, err := b.find(ctx, bo, e, k)
			if err != nil {
				return nil, nil, err
			}
			if in == nil || in.state == stateDeleted {
				key, _ := b.keyOf(e.Table, k)
				r.fail(e.Table, a.Name, reasonNotFound, strPtr(key), nil)
				continue
			}
			if seen[in] {
				continue
			}
			seen[in] = true
			if ok, err := b.authorizeChange(ctx, req, bo, e, a.Name, in, nil); err != nil {
				return nil, nil, err
			} else if !ok {
				r.fail(e.Table, a.Name, reasonNotAuthorized, strPtr(in.key), in.link)
				continue
			}
			c.ins = append(c.ins, in)
		}
		if len(c.ins) == 0 {
			return emptyResult(a), nil, nil
		}
	} else if ok, err := b.authorizeChange(ctx, req, bo, e, a.Name, nil, nil); err != nil {
		return nil, nil, err
	} else if !ok {
		r.fail(e.Table, a.Name, reasonNotAuthorized, nil, nil)
		return emptyResult(a), nil, nil
	}
	out, failed, err := b.call(ctx, req.Behavior, c, r)
	if err != nil {
		return nil, nil, err
	}
	res, ok := a.Result.Get()
	switch {
	case !ok:
		return nil, nil, nil
	case res == model.ResultSelf:
		list := []any{}
		for _, in := range c.ins {
			if !failed[in] && in.state != stateDeleted {
				list = append(list, mapsClone(in.values))
			}
		}
		return list, nil, nil
	case out == nil:
		return emptyResult(a), nil, nil
	}
	return out, nil, nil
}

// emptyResult is the result of an action that did not run: no rows of an
// instance action, null of a static one.
func emptyResult(a *model.Action) any {
	if a.Instance {
		return []any{}
	}
	return nil
}

// authorize checks the authorization to create an instance with values;
// parent is the parent instance of a dependent entity.
func (b *entityBuffer) authorize(ctx context.Context, local bool, bo *model.BusinessObject, e *model.Entity, op model.EntityOperation, values map[string]any, parent *instance, at native.CheckLocation) (bool, error) {
	if local {
		return true, nil
	}
	return b.check(ctx, bo, e, string(op), values, parent, at)
}

// authorizeChange checks the authorization of an operation, read or an
// action (op) on an instance, or of a static action when in is nil. An
// update is checked with the values before it and, when it changes a
// field of the authorization, also with the values after.
func (b *entityBuffer) authorizeChange(ctx context.Context, req vm.ModifyRequest, bo *model.BusinessObject, e *model.Entity, op any, in *instance, after map[string]any) (bool, error) {
	if req.Local {
		return true, nil
	}
	key := fmt.Sprint(op)
	if in == nil {
		return b.check(ctx, bo, e, key, nil, nil, req.Caller)
	}
	ok, err := b.check(ctx, bo, e, key, in.values, nil, req.Caller)
	if !ok || err != nil || after == nil {
		return ok, err
	}
	changed := false
	if au := e.Authorization; au != nil {
		for _, f := range au.Fields {
			if v, set := after[f]; set && jsonValue(nil, v) != jsonValue(nil, in.values[f]) {
				changed = true
			}
		}
	}
	if !changed {
		return true, nil
	}
	values := mapsClone(in.values)
	for f, v := range after {
		values[f] = v
	}
	return b.check(ctx, bo, e, key, values, nil, req.Caller)
}

// check checks the authorization of an entity for an operation, read or
// an action (key) with the values of an instance; values nil checks a
// static action for any values. A dependent entity without authorization
// of its own is checked with the authorization and values of the root
// instance: read with the activity for read, everything else with the
// activity for update of the root. Entities without authorization pass.
func (b *entityBuffer) check(ctx context.Context, bo *model.BusinessObject, e *model.Entity, key string, values map[string]any, parent *instance, at native.CheckLocation) (bool, error) {
	au := e.Authorization
	table := e.Table
	if au == nil {
		root := bo.Entity(bo.Root)
		if au = root.Authorization; au == nil {
			return true, nil
		}
		if key != model.ReadActivityKey {
			key = string(model.OperationUpdate)
		}
		if values != nil {
			var err error
			if values, err = b.rootValues(ctx, bo, e, values, parent); err != nil || values == nil {
				return false, err
			}
		}
		table = root.Table
	}
	c := authorization.Check{Object: au.Object, Values: map[string]string{}}
	if len(au.Activities) > 0 {
		act, ok := au.Activities[key]
		if !ok {
			return false, nil
		}
		c.Values[model.ActivityField] = act
	}
	for of, ef := range au.Fields {
		if values == nil {
			if c.Any == nil {
				c.Any = map[string]bool{}
			}
			c.Any[of] = true
			continue
		}
		c.Values[of] = checkText(b.domain(table, ef), values[ef])
	}
	return b.x.CheckAuthorization(c, at, native.CheckEntity)
}

// rootValues returns the values of the root instance of an instance of a
// dependent entity, found through the foreign keys; nil when a parent
// does not exist.
func (b *entityBuffer) rootValues(ctx context.Context, bo *model.BusinessObject, e *model.Entity, values map[string]any, parent *instance) (map[string]any, error) {
	for {
		p, ok := e.Parent.Get()
		if !ok {
			return values, nil
		}
		pe := bo.Entity(p)
		if parent == nil {
			pk := map[string]any{}
			for _, c := range b.foreignKey(e).Columns {
				pk[c.References] = values[c.Field]
			}
			var err error
			if parent, err = b.find(ctx, bo, pe, pk); err != nil || parent == nil {
				return nil, err
			}
		}
		e, values, parent = pe, parent.values, nil
	}
}

// checkText returns a value of the Go form as the text of an
// authorization check; null is "".
func checkText(d *model.Domain, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		if d != nil && d.Type == model.Date {
			return x.UTC().Format(model.DateLayout)
		}
		return x.UTC().Format(model.TimestampLayout)
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	}
	return fmt.Sprint(v)
}
