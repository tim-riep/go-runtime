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
	"database/sql"
	"maps"
	"strings"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/httpsvc"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// HTTPServiceInfo describes a service node with its resolved settings.
type HTTPServiceInfo struct {
	Path     string                 `json:"path"`
	Package  model.PackageID        `json:"package"`
	File     model.Optional[string] `json:"file"` // null for nodes no file defines
	Texts    map[string]model.Label `json:"texts"`
	Handlers []model.HTTPHandlerRef `json:"handlers"`
	Settings httpsvc.Settings       `json:"settings"`
	// State is new, active or inactive; Reachable tells whether the node
	// and all its ancestors are active.
	State     string `json:"state"`
	Reachable bool   `json:"reachable"`
}

func states(stored map[string]repo.HTTPState) map[string]httpsvc.State {
	out := map[string]httpsvc.State{}
	for p, s := range stored {
		out[p] = httpsvc.State(s.State)
	}
	return out
}

func info(n *httpsvc.Node, st map[string]httpsvc.State) HTTPServiceInfo {
	i := HTTPServiceInfo{Path: n.Path, Package: n.Package, Texts: map[string]model.Label{}, Handlers: n.Handlers(), Settings: n.Settings,
		State: string(st[n.Path]), Reachable: httpsvc.Reachable(n, st)}
	if i.Handlers == nil {
		i.Handlers = []model.HTTPHandlerRef{}
	}
	if n.Service != nil {
		i.File, i.Texts = model.Some(n.Service.File), n.Service.Texts
	}
	return i
}

// HTTPServices lists the service nodes of a package, or of all packages
// for "", ordered by path.
func (k *Kernel) HTTPServices(ctx context.Context, pkg model.PackageID) ([]HTTPServiceInfo, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := k.repo.HTTPStates(ctx, k.db)
	if err != nil {
		return nil, err
	}
	tree, st := httpsvc.Build(s), states(stored)
	out := []HTTPServiceInfo{}
	for _, p := range tree.Paths() {
		n := tree.Node(p)
		if pkg == "" || n.Package == pkg {
			out = append(out, info(n, st))
		}
	}
	return out, nil
}

// HTTPService returns a service node with its resolved settings.
func (k *Kernel) HTTPService(ctx context.Context, path string) (*HTTPServiceInfo, error) {
	list, err := k.HTTPServices(ctx, "")
	if err != nil {
		return nil, err
	}
	path = normalizePath(path)
	for _, i := range list {
		if i.Path == path {
			return &i, nil
		}
	}
	return nil, &rterr.NotFound{Kind: "service node", Name: path}
}

func normalizePath(p string) string { return "/" + strings.Trim(p, "/") }

// ActivateHTTPService activates a service node and the nodes of its
// subtree that were never activated; deactivated nodes stay inactive.
func (k *Kernel) ActivateHTTPService(ctx context.Context, path string) error {
	return k.changeHTTPState(ctx, path, httpsvc.Activate)
}

// DeactivateHTTPService deactivates a service node; its subtree is not
// reachable while it is inactive.
func (k *Kernel) DeactivateHTTPService(ctx context.Context, path string) error {
	return k.changeHTTPState(ctx, path, httpsvc.Deactivate)
}

func (k *Kernel) changeHTTPState(ctx context.Context, path string, change func(*httpsvc.Node, map[string]httpsvc.State) map[string]httpsvc.State) error {
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		tree := httpsvc.Build(s)
		n := tree.Node(normalizePath(path))
		if n == nil {
			return &rterr.NotFound{Kind: "service node", Name: normalizePath(path)}
		}
		stored, err := k.repo.HTTPStates(ctx, tx)
		if err != nil {
			return err
		}
		next := change(n, httpsvc.Sync(tree, states(stored)))
		return k.writeHTTPStates(ctx, tx, tree, stored, next)
	})
}

// writeHTTPStates stores changed states and starts a new generation of
// the activation.
func (k *Kernel) writeHTTPStates(ctx context.Context, q dialect.Querier, tree *httpsvc.Tree, stored map[string]repo.HTTPState, next map[string]httpsvc.State) error {
	now := k.timestamp()
	out := map[string]repo.HTTPState{}
	for p, st := range next {
		n := tree.Node(p)
		old, ok := stored[p]
		if ok && old.State == string(st) {
			out[p] = old
			continue
		}
		out[p] = repo.HTTPState{Path: p, Package: n.Package, State: string(st), ChangedAt: now}
	}
	if maps.EqualFunc(out, stored, func(a, b repo.HTTPState) bool { return a == b }) {
		return nil
	}
	if err := k.repo.ReplaceHTTPStates(ctx, q, out); err != nil {
		return err
	}
	return k.repo.NextHTTPGeneration(ctx, q, now)
}

// syncHTTPStates keeps the activation states in line with the service
// nodes after an operation: new nodes are new, removed ones lose their
// state.
func (k *Kernel) syncHTTPStates(ctx context.Context, q dialect.Querier, want *model.Schema) error {
	if !k.repo.HasHTTPServices() {
		return nil
	}
	stored, err := k.repo.HTTPStates(ctx, q)
	if err != nil {
		return err
	}
	tree := httpsvc.Build(want)
	return k.writeHTTPStates(ctx, q, tree, stored, httpsvc.Sync(tree, states(stored)))
}
