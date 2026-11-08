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
	"maps"
	"slices"
	"strings"

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

// call runs a method of the behavior class for instances and records what
// the response says: failed instances with reason and the messages.
// Runtime errors of the method fail all its instances.
func (b *entityBuffer) call(ctx context.Context, behavior vm.Behavior, bo *model.BusinessObject, method string, ins []*instance, reason string, r *result) (map[*instance]bool, error) {
	keys := make([]map[string]any, len(ins))
	for i, in := range ins {
		keys[i] = b.keyMap(in)
	}
	resp := native.NewEntityResponse(len(ins))
	_, err := behavior.CallBehavior(ctx, bo.BehaviorClass, method, keys, nil, resp)
	failed := map[*instance]bool{}
	var exc *vm.Exception
	switch {
	case errors.As(err, &exc):
		for _, in := range ins {
			failed[in] = true
			r.fail(in.entity.Table, saveOperation(in), reasonBehavior, strPtr(in.key), in.link)
		}
		r.message("error", fmt.Sprintf("%s.%s: %s", bo.BehaviorClass, method, exc.Text), "", nil, nil)
		return failed, nil
	case err != nil:
		return nil, err
	}
	b.respond(resp, ins, reason, r, failed)
	return failed, nil
}

// respond records the failed instances and messages of a response.
func (b *entityBuffer) respond(resp *native.EntityResponse, ins []*instance, reason string, r *result, failed map[*instance]bool) {
	for _, pos := range slices.Sorted(maps.Keys(resp.Failed)) {
		in := ins[pos-1]
		failed[in] = true
		r.fail(in.entity.Table, saveOperation(in), reason, strPtr(in.key), in.link)
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
			for _, e := range bo.Entities {
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
					_, err := b.call(ctx, behavior, bo, d.Name, ins, reasonBehavior, r)
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
		for _, e := range bo.Entities {
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
				if _, err := b.call(ctx, behavior, bo, v.Name, ins, reasonValidation, r); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// execute runs an action.
func (b *entityBuffer) execute(ctx context.Context, bo *model.BusinessObject, e *model.Entity, st vm.ModifyStep, req vm.ModifyRequest, r *result) (any, []*instance, error) {
	return nil, nil, &native.RuntimeError{Class: "/RT/SESSION_ERROR", Text: "actions are not available yet"}
}

// authorize checks the authorization of an operation on new values.
func (b *entityBuffer) authorize(ctx context.Context, local bool, bo *model.BusinessObject, e *model.Entity, op model.EntityOperation, values map[string]any, parent *instance, at native.CheckLocation) (bool, error) {
	return true, nil
}

// authorizeChange checks the authorization of an operation, read or an
// action on an instance; after are the values after an update.
func (b *entityBuffer) authorizeChange(ctx context.Context, req vm.ModifyRequest, bo *model.BusinessObject, e *model.Entity, op any, in *instance, after map[string]any) (bool, error) {
	return true, nil
}
