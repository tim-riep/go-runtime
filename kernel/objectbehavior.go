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
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// DefaultObjectBehaviorTimeout limits each call of the behavior of an
// object type.
const DefaultObjectBehaviorTimeout = 5 * time.Second

// Kinds of changes of objects as /TRBASE/OBJECT_CHANGE_KIND names them.
const (
	objectCreated = "CREATED"
	objectChanged = "CHANGED"
	objectDeleted = "DELETED"
)

// objectChange is a row of /TRBASE/OBJECT_CHANGE.
type objectChange struct {
	Type      string                 `json:"TYPE"`
	Name      string                 `json:"NAME"`
	PackageID string                 `json:"PACKAGE_ID"`
	Kind      string                 `json:"KIND"`
	OldHash   model.Optional[string] `json:"OLD_HASH"`
	NewHash   model.Optional[string] `json:"NEW_HASH"`
}

// objectMessage is a row of /TRBASE/OBJECT_MESSAGE.
type objectMessage struct {
	Name     string                 `json:"NAME"`
	Severity string                 `json:"SEVERITY"`
	Text     string                 `json:"TEXT"`
	Pointer  model.Optional[string] `json:"POINTER"`
}

// objectChanges returns the changes of objects from cur to want by type,
// for the types whose behavior class exists in want.
func objectChanges(cur, want *model.Schema) (map[string][]objectChange, []string) {
	out := map[string][]objectChange{}
	add := func(o *model.Object, kind string, old, new model.Optional[string]) {
		t := want.ObjectTypes[o.Type]
		if t == nil || !t.Behavior.Valid() {
			return
		}
		out[o.Type] = append(out[o.Type], objectChange{Type: o.Type, Name: o.Name, PackageID: string(o.Owner), Kind: kind, OldHash: old, NewHash: new})
	}
	for _, name := range want.ObjectNames() {
		w := want.Objects[name]
		c, ok := cur.Objects[name]
		switch {
		case !ok:
			add(w, objectCreated, model.None[string](), model.Some(w.Hash))
		case c.Hash != w.Hash:
			add(w, objectChanged, model.Some(c.Hash), model.Some(w.Hash))
		}
	}
	for _, name := range cur.ObjectNames() {
		if _, ok := want.Objects[name]; !ok {
			add(cur.Objects[name], objectDeleted, model.Some(cur.Objects[name].Hash), model.None[string]())
		}
	}
	var types []string
	rank := want.PackageRanks()
	for _, name := range want.ObjectTypeNames() {
		if len(out[name]) > 0 {
			types = append(types, name)
		}
	}
	// Types in the dependency order of their packages.
	slices.SortStableFunc(types, func(a, b string) int {
		return cmp.Compare(rank[want.ObjectTypes[a].Owner], rank[want.ObjectTypes[b].Owner])
	})
	return out, types
}

// runObjectBehaviors calls CHECK and then CHANGED of the behavior classes
// of the object types whose objects an operation changed, in the
// transaction q of the operation after the repository and the code were
// written. It returns the warnings as notes of the plan; an error message
// or an exception rejects the operation.
func (k *Kernel) runObjectBehaviors(ctx context.Context, q dialect.Querier, cur, want *model.Schema) ([]plan.Change, error) {
	changes, types := objectChanges(cur, want)
	if len(types) == 0 {
		return nil, nil
	}
	image, err := k.loadImage(ctx, q)
	if err != nil {
		return nil, err
	}
	g := &generation{n: -1, image: image, phys: want.Physical(), schema: want, sheets: newSheets(want)}
	timeout := k.objectBehaviorTimeout
	if timeout <= 0 {
		timeout = DefaultObjectBehaviorTimeout
	}
	var notes []plan.Change
	var errs rterr.List
	call := func(t *model.ObjectType, method string, arg json.RawMessage) (json.RawMessage, error) {
		class, _ := t.Behavior.Get()
		opts := ExecOptions{Language: model.DefaultLanguage, ReadOnly: true, End: RollbackUnlessCommitted, Timeout: timeout,
			VM: vm.Options{Log: k.requestLog("object type " + t.Name)}, bound: q}
		var out json.RawMessage
		err := k.executeWith(ctx, g, opts, func(ctx context.Context, m *vm.Machine) error {
			var err error
			out, err = m.CallObjectJSON(ctx, class, method, arg)
			return err
		})
		if err != nil {
			return nil, &rterr.ValidationError{Code: rterr.CodeObjectType, Location: t.Src, Object: "object type " + t.Name,
				Message: fmt.Sprintf("%s.%s failed: %s", class, method, strings.TrimSpace(err.Error()))}
		}
		return out, nil
	}
	for _, name := range types {
		t := want.ObjectTypes[name]
		arg, err := json.Marshal(changes[name])
		if err != nil {
			return nil, err
		}
		out, err := call(t, "CHECK", arg)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var msgs []objectMessage
		if err := json.Unmarshal(out, &msgs); err != nil {
			return nil, err
		}
		for _, m := range msgs {
			o := want.Objects[m.Name]
			if o == nil {
				o = cur.Objects[m.Name]
			}
			loc := rterr.Location{}
			obj := "object " + m.Name
			if o != nil {
				loc = o.Src
				obj = fmt.Sprintf("object %s of package %s", o.Name, o.Owner)
				if p, ok := m.Pointer.Get(); ok {
					loc = loc.At("/content" + p)
				}
			}
			if m.Severity == "ERROR" {
				errs = append(errs, &rterr.ValidationError{Code: rterr.CodeObject, Location: loc, Object: obj, Message: m.Text})
				continue
			}
			notes = append(notes, plan.Change{Kind: plan.ObjectMessage, Name: m.Name, Detail: strings.ToLower(m.Severity) + ": " + m.Text})
		}
	}
	if err := errs.Err(); err != nil {
		return nil, err
	}
	for _, name := range types {
		arg, _ := json.Marshal(changes[name])
		if _, err := call(want.ObjectTypes[name], "CHANGED", arg); err != nil {
			errs = append(errs, err)
		}
	}
	return notes, errs.Err()
}
