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
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
)

// ImplementationChange is an entry of the history of the activation of
// implementations.
type ImplementationChange = repo.ImplementationChange

// ImplementationState is a setting of the activation of an
// implementation, global for the tenant "".
type ImplementationState = repo.ImplementationState

// Kinds of changes in the history of the activation.
const (
	ChangeActivate              = "activate"
	ChangeDeactivate            = "deactivate"
	ChangeReset                 = "reset"
	ChangeImplementationAdded   = "installed"
	ChangeImplementationRemoved = "removed"
)

// States of implementations in the history.
const (
	stateActive   = "active"
	stateInactive = "inactive"
)

// activationCache is the activation read at a value of the counter of
// changes.
type activationCache struct {
	n int64
	a *activation
}

// readActivation reads the activation of the implementations.
func (k *Kernel) readActivation(ctx context.Context, q dialect.Querier) (*activation, error) {
	states, err := k.repo.ImplementationStates(ctx, q)
	if err != nil {
		return nil, err
	}
	traces, err := k.repo.ImplementationTraces(ctx, q)
	if err != nil {
		return nil, err
	}
	a := &activation{global: map[string]bool{}, tenants: map[string]map[string]bool{}, traces: traces}
	for _, s := range states {
		if s.Tenant == "" {
			a.global[s.Implementation] = s.Active
			continue
		}
		if a.tenants[s.Tenant] == nil {
			a.tenants[s.Tenant] = map[string]bool{}
		}
		a.tenants[s.Tenant][s.Implementation] = s.Active
	}
	return a, nil
}

// cachedActivation returns the activation for an execution. It reads the
// counter of changes, which every change increases in its transaction,
// and the settings only when the counter moved, so that kernels on the
// same database see the changes of each other with the next execution.
func (k *Kernel) cachedActivation(ctx context.Context) (*activation, error) {
	if !k.repo.HasActivation() {
		return &activation{}, nil
	}
	n, err := k.repo.ImplementationGeneration(ctx, k.db)
	if err != nil {
		return nil, err
	}
	if c := k.implementationStates.Load(); c != nil && c.n == n {
		return c.a, nil
	}
	a, err := k.readActivation(ctx, k.db)
	if err != nil {
		return nil, err
	}
	k.implementationStates.Store(&activationCache{n: n, a: a})
	return a, nil
}

// Implementations activates and deactivates implementations of extension
// points, globally and per tenant.
type Implementations struct{ k *Kernel }

// Implementations returns the manager of the activation.
func (k *Kernel) Implementations() *Implementations { return &Implementations{k: k} }

// ImplementationOptions say where a change of the activation applies and
// who makes it.
type ImplementationOptions struct {
	// Tenant restricts the change to a tenant; "" changes the global
	// activation. A setting of a tenant takes precedence over the global
	// one.
	Tenant string
	// By names who makes the change in the history: a user, or "cli".
	// Empty means "api".
	By string
}

func (o ImplementationOptions) by() string {
	if o.By == "" {
		return "api"
	}
	return o.By
}

// Activate activates an implementation. Activating an implementation of
// a single extension point that would match the same filter values as
// another active one is rejected.
func (m *Implementations) Activate(ctx context.Context, name string, opts ImplementationOptions) error {
	on := true
	return m.change(ctx, name, opts, ChangeActivate, &on)
}

// Deactivate deactivates an implementation.
func (m *Implementations) Deactivate(ctx context.Context, name string, opts ImplementationOptions) error {
	off := false
	return m.change(ctx, name, opts, ChangeDeactivate, &off)
}

// Reset removes the setting of an implementation for a tenant, so that
// the global activation applies, or with "" the global setting, so that
// it is active again.
func (m *Implementations) Reset(ctx context.Context, name string, opts ImplementationOptions) error {
	return m.change(ctx, name, opts, ChangeReset, nil)
}

// History returns the changes of the activation of an implementation, or
// of all for "", ordered by time.
func (m *Implementations) History(ctx context.Context, name string) ([]ImplementationChange, error) {
	if err := m.k.requireInit(ctx, m.k.db); err != nil {
		return nil, err
	}
	return m.k.repo.ImplementationChanges(ctx, m.k.db, name)
}

// States returns the settings of the activation of an implementation, or
// of all for "": the global one first, then those of tenants.
func (m *Implementations) States(ctx context.Context, name string) ([]ImplementationState, error) {
	if err := m.k.requireInit(ctx, m.k.db); err != nil {
		return nil, err
	}
	all, err := m.k.repo.ImplementationStates(ctx, m.k.db)
	if err != nil {
		return nil, err
	}
	out := []ImplementationState{}
	for _, s := range all {
		if name == "" || s.Implementation == name {
			out = append(out, s)
		}
	}
	return out, nil
}

// change sets the activation of an implementation in a tenant or
// globally, or removes the setting for set == nil, and records it.
func (m *Implementations) change(ctx context.Context, name string, opts ImplementationOptions, kind string, set *bool) error {
	k := m.k
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	if opts.Tenant != "" {
		tenants, err := k.Tenants().List(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(tenants, func(t Tenant) bool { return t.ID == opts.Tenant }) {
			return &rterr.NotFound{Kind: "tenant", Name: opts.Tenant}
		}
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		impl := s.Implementations[name]
		if impl == nil {
			return &rterr.NotFound{Kind: "implementation", Name: name}
		}
		a, err := k.readActivation(ctx, tx)
		if err != nil {
			return err
		}
		scope := a.global
		if opts.Tenant != "" {
			if a.tenants[opts.Tenant] == nil {
				a.tenants[opts.Tenant] = map[string]bool{}
			}
			scope = a.tenants[opts.Tenant]
		}
		old, had := scope[name]
		if set == nil && !had || set != nil && had && old == *set {
			return nil
		}
		if set == nil {
			delete(scope, name)
		} else {
			scope[name] = *set
		}
		if err := checkOverlaps(s, a); err != nil {
			return err
		}
		now := k.timestamp()
		if set == nil {
			err = k.repo.DeleteImplementationState(ctx, tx, name, opts.Tenant)
		} else {
			err = k.repo.PutImplementationState(ctx, tx, ImplementationState{Implementation: name, Package: impl.Owner, Tenant: opts.Tenant, Active: *set,
				ChangedAt: now, ChangedBy: opts.by()})
		}
		if err != nil {
			return err
		}
		c := ImplementationChange{ChangedAt: now, ChangedBy: opts.by(), Kind: kind, Implementation: name, ExtensionPoint: impl.ExtensionPoint,
			OldState: stateOf(old, had)}
		if set != nil {
			c.NewState = stateOf(*set, true)
		}
		if opts.Tenant != "" {
			c.Tenant = model.Some(opts.Tenant)
		}
		if err := k.repo.InsertImplementationChange(ctx, tx, c); err != nil {
			return err
		}
		return k.repo.NextImplementationGeneration(ctx, tx, now)
	})
}

func stateOf(active, set bool) model.Optional[string] {
	switch {
	case !set:
		return model.None[string]()
	case active:
		return model.Some(stateActive)
	}
	return model.Some(stateInactive)
}

// syncActivation keeps the activation in line with the implementations
// after an operation: new implementations are recorded as installed and
// active, removed ones lose their settings, which upgrades keep.
func (k *Kernel) syncActivation(ctx context.Context, q dialect.Querier, cur, want *model.Schema, now time.Time) error {
	if !k.repo.HasActivation() {
		return nil
	}
	changed := false
	for _, name := range cur.ImplementationNames() {
		if _, ok := want.Implementations[name]; ok {
			continue
		}
		if err := k.repo.DeleteImplementationStates(ctx, q, name); err != nil {
			return err
		}
		c := ImplementationChange{ChangedAt: now, ChangedBy: operationBy, Kind: ChangeImplementationRemoved, Implementation: name,
			ExtensionPoint: cur.Implementations[name].ExtensionPoint}
		if err := k.repo.InsertImplementationChange(ctx, q, c); err != nil {
			return err
		}
		changed = true
	}
	for _, name := range want.ImplementationNames() {
		if _, ok := cur.Implementations[name]; ok {
			continue
		}
		c := ImplementationChange{ChangedAt: now, ChangedBy: operationBy, Kind: ChangeImplementationAdded, Implementation: name,
			ExtensionPoint: want.Implementations[name].ExtensionPoint, NewState: model.Some(stateActive)}
		if err := k.repo.InsertImplementationChange(ctx, q, c); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return k.repo.NextImplementationGeneration(ctx, q, now)
}

// ExtensionPointDetail describes an extension point with its
// implementations, their activation globally and per tenant, and where
// code runs it.
type ExtensionPointDetail struct {
	*reflector.ExtensionPoint
	CodeUses []CodeUse `json:"codeUses"`
}

// ExtensionPoint returns the description of an extension point, or
// rterr.NotFound.
func (k *Kernel) ExtensionPoint(ctx context.Context, name string) (*ExtensionPointDetail, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	p, err := r.ExtensionPoint(name)
	if err != nil {
		return nil, err
	}
	if err := k.addActivation(ctx, p); err != nil {
		return nil, err
	}
	uses, err := k.CodeUses(ctx, name)
	if err != nil {
		return nil, err
	}
	d := &ExtensionPointDetail{ExtensionPoint: p, CodeUses: []CodeUse{}}
	for _, u := range uses {
		if u.Kind == "extension-point" {
			d.CodeUses = append(d.CodeUses, u)
		}
	}
	return d, nil
}

// ExtensionPoints returns the extension points of a package, or of all
// packages for "", ordered by name, with the activation of their
// implementations.
func (k *Kernel) ExtensionPoints(ctx context.Context, pkg model.PackageID) ([]*reflector.ExtensionPoint, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	if pkg != "" {
		if s, _ := k.Schema(ctx); s != nil && s.Packages[pkg] == nil {
			return nil, &rterr.NotInstalled{Package: string(pkg)}
		}
	}
	list := r.ExtensionPoints(pkg)
	for _, p := range list {
		if err := k.addActivation(ctx, p); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// addActivation adds the activation to the implementations of an
// extension point.
func (k *Kernel) addActivation(ctx context.Context, p *reflector.ExtensionPoint) error {
	a, err := k.readActivation(ctx, k.db)
	if err != nil {
		return err
	}
	for i := range p.Implementations {
		impl := &p.Implementations[i]
		act := &reflector.Activation{Active: a.active(impl.Name, ""), Tenants: map[string]bool{}}
		if on, ok := a.global[impl.Name]; ok {
			act.Global = model.Some(on)
		}
		for tenant, settings := range a.tenants {
			if on, ok := settings[impl.Name]; ok {
				act.Tenants[tenant] = on
			}
		}
		impl.Activation = act
	}
	return nil
}
