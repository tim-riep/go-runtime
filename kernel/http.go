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
	"fmt"
	"maps"
	"strings"
	"time"

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
	// OData has the settings of the node of OData, nil for other nodes.
	OData *ODataSettings `json:"odata,omitempty"`
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
	ks, err := k.repo.KernelNodeSettings(ctx, k.db)
	if err != nil {
		return nil, err
	}
	tree, st := httpsvc.Build(s), states(stored)
	hs := &httpState{states: st, kernel: ks}
	out := []HTTPServiceInfo{}
	for _, p := range tree.Paths() {
		n := tree.Node(p)
		if pkg == "" || n.Package == pkg {
			i := info(n, st)
			i.Settings = hs.settingsOf(n)
			if p == model.ODataPath {
				o := odataSettings(ks)
				i.OData = &o
			}
			out = append(out, i)
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

// ODataSettings are the settings of the node of OData beyond those of
// every node.
type ODataSettings struct {
	MaxPageSize     int64         `json:"maxPageSize"`
	MetadataTimeout time.Duration `json:"metadataTimeout"`
}

// Defaults of the node of OData.
const (
	DefaultMaxPageSize     = 200
	DefaultMetadataTimeout = 5 * time.Second
)

// odataSettings returns the settings of the node of OData.
func odataSettings(ks map[string]repo.KernelNodeSettings) ODataSettings {
	s := ODataSettings{MaxPageSize: DefaultMaxPageSize, MetadataTimeout: DefaultMetadataTimeout}
	if c, ok := ks[model.ODataPath]; ok {
		s.MaxPageSize = c.MaxPageSize.Or(s.MaxPageSize)
		if v, ok := c.MetadataTimeoutSeconds.Get(); ok {
			s.MetadataTimeout = time.Duration(v) * time.Second
		}
	}
	return s
}

// KernelNodeChange changes the settings of a service node of the kernel;
// absent values stay as they are.
type KernelNodeChange struct {
	Authentication  model.Optional[[]string]
	TenantSources   model.Optional[[]string]
	LanguageSources model.Optional[[]string]
	MaxBodySize     model.Optional[int64]
	MaxResponseSize model.Optional[int64]
	TimeoutSeconds  model.Optional[int64]
	Budget          model.Optional[int64]
	// MaxPageSize and MetadataTimeoutSeconds apply to the node of OData.
	MaxPageSize            model.Optional[int64]
	MetadataTimeoutSeconds model.Optional[int64]
	// Reset drops all changes first, so the defaults apply again.
	Reset bool
}

// ConfigureHTTPService changes the settings of a service node of the
// kernel. Nodes of packages take their settings from their package, and no
// node of the kernel allows anonymous access. by names who changes them.
func (k *Kernel) ConfigureHTTPService(ctx context.Context, path string, c KernelNodeChange, by string) error {
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	path = normalizePath(path)
	known := false
	for _, n := range httpsvc.KernelNodes {
		known = known || n.FullPath() == path
	}
	if !known {
		return &rterr.InvalidArgument{Argument: "service node", Message: path + " is no node of the kernel; nodes of packages take their settings from their package"}
	}
	if err := checkNodeChange(path, c); err != nil {
		return err
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		all, err := k.repo.KernelNodeSettings(ctx, tx)
		if err != nil {
			return err
		}
		cur := all[path]
		if c.Reset {
			cur = repo.KernelNodeSettings{}
		}
		cur.Path, cur.ChangedAt, cur.ChangedBy = path, k.timestamp(), optionalUser(by)
		set := &cur.Settings
		for _, x := range []struct {
			to   *model.Optional[[]string]
			from model.Optional[[]string]
		}{{&set.Authentication, c.Authentication}, {&set.TenantSources, c.TenantSources}, {&set.LanguageSources, c.LanguageSources}} {
			if x.from.Valid() {
				*x.to = x.from
			}
		}
		for _, x := range []struct {
			to   *model.Optional[int64]
			from model.Optional[int64]
		}{{&set.MaxBodySize, c.MaxBodySize}, {&set.MaxResponseSize, c.MaxResponseSize}, {&set.TimeoutSeconds, c.TimeoutSeconds},
			{&set.Budget, c.Budget}, {&cur.MaxPageSize, c.MaxPageSize}, {&cur.MetadataTimeoutSeconds, c.MetadataTimeoutSeconds}} {
			if x.from.Valid() {
				*x.to = x.from
			}
		}
		if err := k.repo.WriteKernelNodeSettings(ctx, tx, cur); err != nil {
			return err
		}
		return k.repo.NextHTTPGeneration(ctx, tx, cur.ChangedAt)
	})
}

// checkNodeChange checks the values of a change of the settings of a node
// of the kernel.
func checkNodeChange(path string, c KernelNodeChange) error {
	bad := func(format string, args ...any) error {
		return &rterr.InvalidArgument{Argument: "setting", Message: fmt.Sprintf(format, args...)}
	}
	for _, m := range c.Authentication.Or(nil) {
		if m != model.AuthBasic && m != model.AuthToken {
			return bad("the authentication is basic or token, not %q", m)
		}
	}
	if v, ok := c.Authentication.Get(); ok && len(v) == 0 {
		return bad("a node of the kernel needs an authentication; it never allows anonymous access")
	}
	for _, list := range []model.Optional[[]string]{c.TenantSources, c.LanguageSources} {
		for _, s := range list.Or(nil) {
			if s != model.SourceHeader && s != model.SourceQuery && s != model.SourceUser {
				return bad("a source is header, query or user, not %q", s)
			}
		}
	}
	limits := []struct {
		name     string
		v        model.Optional[int64]
		min, max int64
	}{{"the maximal body size", c.MaxBodySize, 1, 1 << 40}, {"the maximal response size", c.MaxResponseSize, 1, 1 << 40},
		{"the timeout", c.TimeoutSeconds, 1, 3600}, {"the budget", c.Budget, 1, 1 << 50},
		{"the maximal page size", c.MaxPageSize, 1, 10_000}, {"the timeout of the metadata", c.MetadataTimeoutSeconds, 1, 60}}
	for _, l := range limits {
		if v, ok := l.v.Get(); ok && (v < l.min || v > l.max) {
			return bad("%s is between %d and %d, not %d", l.name, l.min, l.max, v)
		}
	}
	if path != model.ODataPath && (c.MaxPageSize.Valid() || c.MetadataTimeoutSeconds.Valid()) {
		return bad("the page size and the timeout of the metadata are settings of %s", model.ODataPath)
	}
	return nil
}
