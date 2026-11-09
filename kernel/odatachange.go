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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/vm"
)

// changeSet is the changes of a request in one execution with one
// transaction buffer: entity statements in order, then the save sequence.
// A later $batch bundles the requests of a change set in one of them.
type changeSet struct {
	sr     *serviceRequest
	x      *execution
	m      *vm.Machine
	caller native.CheckLocation
	// links map the links of new instances to the path of their entity in
	// the body, for the targets of messages.
	links map[string]string
	// messages are the warnings and informations of all statements.
	messages []vm.EntityMessage
}

// change runs fn in an execution of the request with a transaction buffer.
// Nothing is written unless fn saves; a failed save writes nothing.
func (k *Kernel) change(ctx context.Context, sr *serviceRequest, fn func(ctx context.Context, cs *changeSet) error) error {
	opts := ExecOptions{Tenant: sr.tenant.Or(""), Language: sr.lang, User: model.Some(sr.user.Name), Timeout: sr.set.Timeout,
		End: RollbackUnlessCommitted, VM: vm.Options{Budget: sr.set.Budget, Log: k.requestLog(model.ODataPath)}}
	return k.executeOn(ctx, sr.g, opts, func(ctx context.Context, m *vm.Machine, x *execution) error {
		cs := &changeSet{sr: sr, x: x, m: m, caller: native.CheckLocation{Kernel: "odata " + sr.svc.Name}, links: map[string]string{}}
		return fn(ctx, cs)
	})
}

// read reads an instance through the transaction buffer, as entity
// statements see it; nil when it does not exist or may not be read.
func (cs *changeSet) read(ctx context.Context, t *odata.EntityType, key map[string]any) (map[string]any, error) {
	rows, _, err := cs.x.buffer().Read(ctx, vm.ReadRequest{BusinessObject: t.BusinessObject.Name, Entity: t.Entity.Table,
		Keys: []map[string]any{key}, Caller: cs.caller})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// modify runs entity statements and fails with the error of OData that
// their result describes.
func (cs *changeSet) modify(ctx context.Context, t *odata.EntityType, steps []vm.ModifyStep) (*vm.EntityResult, error) {
	r, _, err := cs.x.buffer().Modify(ctx, vm.ModifyRequest{BusinessObject: t.BusinessObject.Name, Steps: steps, Behavior: cs.m, Caller: cs.caller})
	if err != nil {
		return nil, err
	}
	if e := cs.sr.resultError(r, cs.links); e != nil {
		return nil, e
	}
	cs.messages = append(cs.messages, r.Messages...)
	return r, nil
}

// save runs the save sequence and commits.
func (cs *changeSet) save(ctx context.Context) error {
	r, _, err := cs.x.buffer().Save(ctx, cs.m)
	if err != nil {
		return err
	}
	if e := cs.sr.resultError(r, cs.links); e != nil {
		return e
	}
	cs.messages = append(cs.messages, r.Messages...)
	return nil
}

// keyOf returns the key of a path as values of the buffer.
func keyOf(kv []odata.KeyValue) map[string]any {
	out := map[string]any{}
	for _, k := range kv {
		out[k.P.Field.Name] = odata.GoValue(k.P.Domain, k.Value.Text)
	}
	return out
}

// readBody reads the body within the limit of the node.
func (sr *serviceRequest) readBody(w http.ResponseWriter, r *http.Request) ([]byte, *odata.Error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sr.set.MaxBodySize))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return nil, odata.Errorf(http.StatusRequestEntityTooLarge, odata.CodeTooLarge)
	case err != nil:
		return nil, odata.Errorf(http.StatusBadRequest, odata.CodeInvalidRequest, "the body cannot be read")
	}
	return body, nil
}

// prefers reports whether Prefer asks for a preference.
func prefers(r *http.Request, pref string) bool {
	for _, p := range strings.Split(r.Header.Get("Prefer"), ",") {
		if strings.EqualFold(strings.TrimSpace(p), pref) {
			return true
		}
	}
	return false
}

// changeable checks that a path addresses an entity of a business object
// that allows an operation.
func changeable(t *odata.EntityType, op model.EntityOperation, method string) *odata.Error {
	if t.BusinessObject == nil || !t.Allows(op) {
		return odata.Errorf(http.StatusMethodNotAllowed, odata.CodeMethodNotAllowed, t.Name, method)
	}
	return nil
}

// serveCreate answers POST on a collection: a new instance with its
// dependent instances in one statement.
func (k *Kernel) serveCreate(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path) {
	ctx := r.Context()
	t := path.Type()
	if e := changeable(t, model.OperationCreate, r.Method); e != nil {
		sr.fail(w, e)
		return
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
	eb, e := odata.DecodeEntity(t, body, odata.ChangeCreate)
	if e != nil {
		sr.fail(w, e)
		return
	}
	if err := sr.effective(ctx); err != nil {
		sr.internal(w, "loading the authorizations", err)
		return
	}
	// A navigation to dependent entities gives the parent.
	parentFields := map[string]any{}
	if path.Nav != nil {
		parents, err := sr.parentOf(ctx, path)
		if err != nil {
			sr.failWith(w, "reading the parent", err)
			return
		}
		if parents == nil {
			sr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the entity"))
			return
		}
		for _, c := range path.Nav.Reverse {
			parentFields[c.Field] = parents[c.References]
		}
	}
	var key map[string]any
	var cs0 *changeSet
	err := k.change(ctx, sr, func(ctx context.Context, cs *changeSet) error {
		cs0 = cs
		var steps []vm.ModifyStep
		n := 0
		var add func(eb *odata.EntityBody, parentLink, target string, fixed map[string]any)
		add = func(eb *odata.EntityBody, parentLink, target string, fixed map[string]any) {
			n++
			link := "n" + strconv.Itoa(n)
			cs.links[link] = target
			e := eb.Type.Entity
			fkFields := map[string]bool{}
			if fk := cs.x.phys.Tables[e.Table].ForeignKey(e.ForeignKey.Or("")); fk != nil {
				for _, c := range fk.Columns {
					fkFields[c.Field] = true
				}
			}
			row := map[string]any{model.LinkField: link}
			var fields []string
			for _, fl := range eb.Order {
				row[fl] = eb.Fields[fl]
				if !fkFields[fl] {
					fields = append(fields, fl)
				}
			}
			for fl, v := range fixed {
				row[fl] = v
			}
			if parentLink != "" {
				row[model.ParentLinkField] = parentLink
			}
			steps = append(steps, vm.ModifyStep{Op: "create", Entity: e.Table, ByParent: e.Parent.Valid(), Fields: fields, Rows: []map[string]any{row}})
			for _, cb := range eb.Children {
				for i, child := range cb.Entities {
					add(child, link, target+cb.Nav.Name+"("+strconv.Itoa(i+1)+")/", nil)
				}
			}
		}
		add(eb, "", "", parentFields)
		r, err := cs.modify(ctx, t, steps)
		if err != nil {
			return err
		}
		if err := cs.save(ctx); err != nil {
			return err
		}
		key = map[string]any{}
		for _, c := range r.Created {
			if c.Link != nil && *c.Link == "n1" {
				var raw map[string]json.RawMessage
				if err := json.Unmarshal([]byte(c.Key), &raw); err != nil {
					return err
				}
				for _, p := range t.Key {
					if v, ok := raw[p.Field.Name]; ok {
						val, err := odata.DecodeValue(p.Domain, v)
						if err != nil {
							return err
						}
						key[p.Field.Name] = val
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		sr.failWith(w, "creating an entity", err)
		return
	}
	sr.answerChange(w, r, path, t, key, f, cs0.messages, http.StatusCreated)
}

// parentOf reads the entity a navigation starts from, or nil.
func (sr *serviceRequest) parentOf(ctx context.Context, path *odata.Path) (map[string]any, error) {
	q, err := sr.newQuery(path.Set.Type)
	if err != nil {
		return nil, err
	}
	if err := q.key(path.Key); err != nil {
		return nil, err
	}
	rows, err := sr.rows(ctx, q, q.selectText(nil, 1, 0))
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// entityKey returns the key of the entity a single path addresses.
func entityKey(path *odata.Path) map[string]any {
	if path.Nav != nil {
		return keyOf(path.NavKey)
	}
	return keyOf(path.Key)
}

// checkETag checks If-Match against the ETag of an instance: entities with
// an ETag need it for changes.
func checkETag(r *http.Request, t *odata.EntityType, row map[string]any, needed bool) *odata.Error {
	if t.ETag == nil {
		return nil
	}
	h := r.Header.Get("If-Match")
	if h == "" {
		if needed {
			return odata.Errorf(http.StatusPreconditionRequired, odata.CodePreconditionNeeded)
		}
		return nil
	}
	if !matchesETag(h, odata.ETag(t.ETag.Domain, row[t.ETag.Field.Name])) {
		return odata.Errorf(http.StatusPreconditionFailed, odata.CodePreconditionFailed)
	}
	return nil
}

// serveUpdate answers PATCH and PUT on an entity.
func (k *Kernel) serveUpdate(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path) {
	ctx := r.Context()
	t := path.Type()
	if e := changeable(t, model.OperationUpdate, r.Method); e != nil {
		sr.fail(w, e)
		return
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
	kind := odata.ChangePatch
	if r.Method == http.MethodPut {
		kind = odata.ChangeReplace
	}
	eb, e := odata.DecodeEntity(t, body, kind)
	if e != nil {
		sr.fail(w, e)
		return
	}
	fields := eb.Order
	if kind == odata.ChangeReplace {
		// PUT replaces every field the client may change: those it leaves
		// out become null.
		fields = nil
		for _, p := range t.Properties {
			if !p.Computed && !p.Immutable && !p.Key {
				fields = append(fields, p.Field.Name)
				if _, ok := eb.Fields[p.Field.Name]; !ok {
					if !p.Nullable {
						sr.fail(w, odata.Errorf(http.StatusBadRequest, odata.CodeInvalidValue, p.Name, "it cannot be null").At(p.Name))
						return
					}
					eb.Fields[p.Field.Name] = nil
				}
			}
		}
	}
	key := entityKey(path)
	var cs0 *changeSet
	err := k.changeEntity(ctx, sr, path, func(ctx context.Context, cs *changeSet, row map[string]any) error {
		cs0 = cs
		if e := checkETag(r, t, row, true); e != nil {
			return e
		}
		if len(fields) == 0 {
			return nil
		}
		values := map[string]any{}
		for fl, v := range key {
			values[fl] = v
		}
		for _, fl := range fields {
			values[fl] = eb.Fields[fl]
		}
		if _, err := cs.modify(ctx, t, []vm.ModifyStep{{Op: "update", Entity: t.Entity.Table, Fields: fields, Rows: []map[string]any{values}}}); err != nil {
			return err
		}
		return cs.save(ctx)
	})
	if err != nil {
		sr.failWith(w, "changing an entity", err)
		return
	}
	status := http.StatusNoContent
	if prefers(r, "return=representation") {
		status = http.StatusOK
	}
	sr.answerChange(w, r, path, t, key, f, cs0.messages, status)
}

// changeEntity runs fn on an entity a single path addresses, after the
// entity was found with the authorization for reading and read into the
// transaction buffer.
func (k *Kernel) changeEntity(ctx context.Context, sr *serviceRequest, path *odata.Path, fn func(ctx context.Context, cs *changeSet, row map[string]any) error) error {
	if !path.Single() || path.Nav != nil && path.NavKey == nil {
		return odata.Errorf(http.StatusMethodNotAllowed, odata.CodeMethodNotAllowed, "this path", "a change")
	}
	if err := sr.effective(ctx); err != nil {
		return err
	}
	q, found, err := sr.target(ctx, path)
	if err != nil {
		return err
	}
	if found {
		rows, err := sr.rows(ctx, q, q.selectText(nil, 1, 0))
		if err != nil {
			return err
		}
		found = len(rows) > 0
	}
	if !found {
		return odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the entity")
	}
	t := path.Type()
	return k.change(ctx, sr, func(ctx context.Context, cs *changeSet) error {
		row, err := cs.read(ctx, t, entityKey(path))
		if err != nil {
			return err
		}
		if row == nil {
			return odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the entity")
		}
		return fn(ctx, cs, row)
	})
}

// serveDelete answers DELETE on an entity: it deletes the instance with
// its dependent instances.
func (k *Kernel) serveDelete(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path) {
	ctx := r.Context()
	t := path.Type()
	if e := changeable(t, model.OperationDelete, r.Method); e != nil {
		sr.fail(w, e)
		return
	}
	key := entityKey(path)
	err := k.changeEntity(ctx, sr, path, func(ctx context.Context, cs *changeSet, row map[string]any) error {
		if e := checkETag(r, t, row, true); e != nil {
			return e
		}
		if _, err := cs.modify(ctx, t, []vm.ModifyStep{{Op: "delete", Entity: t.Entity.Table, Rows: []map[string]any{key}}}); err != nil {
			return err
		}
		return cs.save(ctx)
	})
	if err != nil {
		sr.failWith(w, "deleting an entity", err)
		return
	}
	w.Header().Set("OData-Version", "4.0")
	w.WriteHeader(http.StatusNoContent)
	sr.entry.status = http.StatusNoContent
}

// answerChange answers a change with the entity as the database has it
// now, with its location, ETag and the messages of the behavior; 204
// without body for status 204 or Prefer return=minimal.
func (sr *serviceRequest) answerChange(w http.ResponseWriter, r *http.Request, path *odata.Path, t *odata.EntityType, key map[string]any,
	f odata.Format, messages []vm.EntityMessage, status int) {
	ctx := r.Context()
	q, err := sr.newQuery(t)
	if err == nil {
		for _, p := range t.Key {
			if err = q.equal(p.Field.Name, key[p.Field.Name]); err != nil {
				break
			}
		}
	}
	var rows []map[string]any
	if err == nil {
		rows, err = sr.rows(ctx, q, q.selectText(nil, 1, 0))
	}
	if err != nil {
		sr.failWith(w, "reading the changed entity", err)
		return
	}
	h := w.Header()
	if len(rows) > 0 {
		location := sr.base + sr.locationOf(path, t, rows[0])
		if status == http.StatusCreated {
			h.Set("Location", location)
			h.Set("OData-EntityId", location)
		}
		if t.ETag != nil {
			h.Set("ETag", odata.ETag(t.ETag.Domain, rows[0][t.ETag.Field.Name]))
		}
	}
	if status == http.StatusNoContent || prefers(r, "return=minimal") || len(rows) == 0 {
		if prefers(r, "return=minimal") {
			h.Set("Preference-Applied", "return=minimal")
		}
		h.Set("OData-Version", "4.0")
		w.WriteHeader(http.StatusNoContent)
		sr.entry.status = http.StatusNoContent
		return
	}
	o := &odata.Object{}
	if !f.NoMetadata {
		target := path.Set.Name
		if path.Nav != nil {
			if path.Nav.Target != nil {
				target = path.Nav.Target.Name
			} else {
				target = sr.resourceBase(r, path)
			}
		}
		o.Set("@odata.context", sr.base+"$metadata#"+target+"/$entity")
	}
	o.Append(f.Entity(t, rows[0], nil))
	if msgs := sr.coreMessages(messages); len(msgs) > 0 {
		o.Set("@Core.Messages", msgs)
	}
	sr.writeOData(w, r, status, f, o)
}

// locationOf returns the path of an entity relative to the service.
func (sr *serviceRequest) locationOf(path *odata.Path, t *odata.EntityType, row map[string]any) string {
	for _, s := range sr.m.Sets {
		if s.Type == t {
			return s.Name + odata.KeyText(t, row)
		}
	}
	// A contained entity lives below its parent.
	segs := odataSegments(sr.resource)
	nav := path.Nav.Name
	return segs[0] + "/" + nav + odata.KeyText(t, row)
}

// coreMessages returns the warnings and informations of the behavior for
// the annotation Core.Messages.
func (sr *serviceRequest) coreMessages(messages []vm.EntityMessage) []any {
	var out []any
	for _, m := range messages {
		if m.Severity == "error" {
			continue
		}
		o := &odata.Object{}
		o.Set("code", strings.ToUpper(m.Severity))
		o.Set("message", m.Text)
		o.Set("severity", severityOf(m.Severity))
		if t := sr.messageTarget(m.Entity, m.Field, m.Link, nil); t != "" {
			o.Set("target", t)
		}
		if m.Origin != nil {
			o.Set("@RT.Origin", *m.Origin)
		}
		out = append(out, o)
	}
	return out
}

// severityOf maps a severity of messages to that of Core.Messages.
func severityOf(s string) string {
	if s == "information" {
		return "info"
	}
	return s
}

// messageTarget returns the target of a message: the property, below the
// path of the entity in the body of a deep insert.
func (sr *serviceRequest) messageTarget(entity, field, link *string, links map[string]string) string {
	if field == nil {
		return ""
	}
	name := *field
	for _, t := range sr.m.Types() {
		if entity != nil && t.Table.Name == *entity {
			if p := t.FieldProperty(*field); p != nil {
				name = p.Name
			}
		}
	}
	if link != nil && links != nil {
		return links[*link] + name
	}
	return name
}

// Reasons of failures with their status, the most severe first.
var failureStatus = []struct {
	reason string
	status int
	code   string
}{
	{"NOT_AUTHORIZED", http.StatusForbidden, odata.CodeForbidden},
	{"NOT_FOUND", http.StatusNotFound, odata.CodeNotFound},
	{"ALREADY_EXISTS", http.StatusConflict, odata.CodeAlreadyExists},
	{"ETAG", http.StatusPreconditionFailed, odata.CodePreconditionFailed},
	{"PARENT_NOT_FOUND", http.StatusBadRequest, odata.CodeParentNotFound},
	{"MANDATORY", http.StatusBadRequest, odata.CodeMandatory},
	{"VALIDATION", http.StatusBadRequest, odata.CodeValidation},
	{"BEHAVIOR", http.StatusBadRequest, odata.CodeBehavior},
	{"DATABASE", http.StatusInternalServerError, odata.CodeInternal},
}

// resultError returns the error a result of entity statements describes,
// or nil when nothing failed: its status comes from the most severe
// failure, its details from the messages with their targets.
func (sr *serviceRequest) resultError(r *vm.EntityResult, links map[string]string) *odata.Error {
	if r == nil || !r.Failed() {
		return nil
	}
	reasons := map[string]bool{}
	for _, f := range r.Failures {
		reasons[f.Reason] = true
	}
	e := odata.Errorf(http.StatusBadRequest, odata.CodeValidation)
	for _, fs := range failureStatus {
		if reasons[fs.reason] {
			e = odata.Errorf(fs.status, fs.code)
			if fs.code == odata.CodeNotFound || fs.code == odata.CodeForbidden {
				e.Args = []any{"the entity"}
			}
			if fs.code == odata.CodeMandatory {
				e = odata.Errorf(fs.status, odata.CodeValidation)
			}
			break
		}
	}
	if e.Status == http.StatusInternalServerError {
		return e
	}
	for _, m := range r.Messages {
		if m.Severity != "error" {
			continue
		}
		d := &odata.Error{Code: e.Code, Text: m.Text, Target: sr.messageTarget(m.Entity, m.Field, m.Link, links)}
		if m.Origin != nil {
			d.Origin = *m.Origin
		}
		e.Details = append(e.Details, d)
	}
	if len(e.Details) > 0 {
		e.Target = e.Details[0].Target
		if len(e.Details) == 1 {
			e.Text = e.Details[0].Text
		}
	}
	return e
}
