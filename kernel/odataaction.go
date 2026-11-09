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
	"bytes"
	"context"
	"net/http"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
	"github.com/tim-riep/go-runtime/vm"
)

// serveAction answers POST on a bound action of an entity or on an action
// import: the action runs as entity statement, then the save sequence.
func (k *Kernel) serveAction(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path) {
	ctx := r.Context()
	a := path.Action
	if a == nil {
		a = path.Import
	}
	f, e := responseFormat(r)
	if e != nil {
		sr.fail(w, e)
		return
	}
	body, e := sr.readBody(w, r)
	if e != nil {
		sr.fail(w, e)
		return
	}
	var param any
	if name, ok := a.Action.Parameter.Get(); ok {
		def, err := structure.Resolve(sr.g.schema, []string{sr.lang}, name)
		if err != nil {
			sr.internal(w, "resolving the parameter of an action", err)
			return
		}
		if len(bytes.TrimSpace(body)) == 0 {
			body = []byte("{}")
		}
		keys, vals, e := odata.DecodeObject(body)
		if e != nil {
			sr.fail(w, e)
			return
		}
		p, e := odata.DecodeComplex(def, keys, vals, "")
		if e != nil {
			sr.fail(w, e)
			return
		}
		param = p
	} else if t := bytes.TrimSpace(body); len(t) > 0 && !bytes.Equal(t, []byte("{}")) {
		sr.fail(w, odata.Errorf(http.StatusBadRequest, odata.CodeInvalidRequest, "the action "+a.Name+" takes no parameters"))
		return
	}
	owner := a.Owner
	step := vm.ModifyStep{Op: "execute", Entity: owner.Entity.Table, Action: a.Action.Name, Param: param}
	var result any
	var cs0 *changeSet
	run := func(ctx context.Context, cs *changeSet) error {
		cs0 = cs
		_, intos, err := cs.statements(ctx, owner, []vm.ModifyStep{step})
		if err != nil {
			return err
		}
		result = intos[0]
		return cs.save(ctx)
	}
	var key map[string]any
	var err error
	if a.Type != nil {
		key = entityKey(path)
		step.Rows = []map[string]any{key}
		err = k.changeEntity(ctx, sr, path, func(ctx context.Context, cs *changeSet, row map[string]any) error {
			if e := checkETag(r, a.Type, row, false); e != nil {
				return e
			}
			return run(ctx, cs)
		})
	} else {
		if err = sr.effective(ctx); err == nil {
			err = k.change(ctx, sr, run)
		}
	}
	if err != nil {
		sr.failWith(w, "running an action", err)
		return
	}
	res, ok := a.Action.Result.Get()
	switch {
	case !ok:
		w.Header().Set("OData-Version", "4.0")
		w.WriteHeader(http.StatusNoContent)
		sr.entry.status = http.StatusNoContent
	case res == model.ResultSelf:
		sr.answerChange(w, r, path, a.Type, key, f, cs0.messages, http.StatusOK)
	default:
		def, err := structure.Resolve(sr.g.schema, []string{sr.lang}, res)
		if err != nil {
			sr.internal(w, "resolving the result of an action", err)
			return
		}
		if list, isList := result.([]any); isList {
			result = nil
			if len(list) > 0 {
				result = list[0]
			}
		}
		value, _ := result.(map[string]any)
		o := &odata.Object{}
		if !f.NoMetadata {
			o.Set("@odata.context", sr.base+"$metadata#"+sr.m.Namespace+"."+model.PublicName(res))
		}
		o.Append(f.Complex(def, value))
		if msgs := sr.coreMessages(cs0.messages); len(msgs) > 0 {
			o.Set("@Core.Messages", msgs)
		}
		sr.writeOData(w, r, http.StatusOK, f, o)
	}
}
