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

package repo

import (
	"context"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of the activation of implementations.
const (
	TImplementationStates       = model.KernelNamespace + "IMPLEMENTATION_STATES"
	TImplementationTenantStates = model.KernelNamespace + "IMPLEMENTATION_TENANT_STATES"
	TImplementationGeneration   = model.KernelNamespace + "IMPLEMENTATION_GENERATION"
	TImplementationHistory      = model.KernelNamespace + "IMPLEMENTATION_HISTORY"
)

// HasActivation reports whether the kernel tables keep the activation of
// implementations.
func (r *Repo) HasActivation() bool { return r.has(TImplementationStates, "") }

// ImplementationState is a setting of the activation of an
// implementation: global for the tenant "", else for one tenant.
type ImplementationState struct {
	Implementation string          `json:"implementation"`
	Package        model.PackageID `json:"package"`
	Tenant         string          `json:"tenant,omitempty"`
	Active         bool            `json:"active"`
	ChangedAt      time.Time       `json:"changedAt"`
	ChangedBy      string          `json:"changedBy"`
}

var stateColumns = []string{"IMPLEMENTATION_NAME", "PACKAGE_ID", "ACTIVE", "CHANGED_AT", "CHANGED_BY"}

// ImplementationStates returns all settings, the global ones first, each
// ordered by implementation and tenant.
func (r *Repo) ImplementationStates(ctx context.Context, q dialect.Querier) ([]ImplementationState, error) {
	if !r.HasActivation() {
		return nil, nil
	}
	var out []ImplementationState
	read := func(table string, cols []string, order ...string) error {
		rows, err := r.selectRows(ctx, q, table, cols, order...)
		if err != nil {
			return err
		}
		for _, x := range rows {
			out = append(out, ImplementationState{Implementation: str(x["IMPLEMENTATION_NAME"]), Package: model.PackageID(str(x["PACKAGE_ID"])),
				Tenant: str(x[model.TenantField]), Active: flag(x["ACTIVE"]), ChangedAt: tm(x["CHANGED_AT"]), ChangedBy: str(x["CHANGED_BY"])})
		}
		return nil
	}
	if err := read(TImplementationStates, stateColumns, "IMPLEMENTATION_NAME"); err != nil {
		return nil, err
	}
	if err := read(TImplementationTenantStates, append([]string{model.TenantField}, stateColumns...), "IMPLEMENTATION_NAME", model.TenantField); err != nil {
		return nil, err
	}
	return out, nil
}

// PutImplementationState stores a setting in place of the one before.
func (r *Repo) PutImplementationState(ctx context.Context, q dialect.Querier, s ImplementationState) error {
	if err := r.DeleteImplementationState(ctx, q, s.Implementation, s.Tenant); err != nil {
		return err
	}
	x := row{"IMPLEMENTATION_NAME": s.Implementation, "PACKAGE_ID": string(s.Package), "ACTIVE": s.Active, "CHANGED_AT": s.ChangedAt, "CHANGED_BY": s.ChangedBy}
	if s.Tenant == "" {
		return r.insert(ctx, q, TImplementationStates, x)
	}
	x[model.TenantField] = s.Tenant
	return r.insert(ctx, q, TImplementationTenantStates, x)
}

// DeleteImplementationState removes the setting of an implementation for
// a tenant, or the global one for "".
func (r *Repo) DeleteImplementationState(ctx context.Context, q dialect.Querier, impl, tenant string) error {
	if tenant == "" {
		return r.deleteWhere(ctx, q, TImplementationStates, row{"IMPLEMENTATION_NAME": impl})
	}
	return r.deleteWhere(ctx, q, TImplementationTenantStates, row{model.TenantField: tenant, "IMPLEMENTATION_NAME": impl})
}

// DeleteImplementationStates removes all settings of an implementation.
func (r *Repo) DeleteImplementationStates(ctx context.Context, q dialect.Querier, impl string) error {
	if err := r.deleteWhere(ctx, q, TImplementationStates, row{"IMPLEMENTATION_NAME": impl}); err != nil {
		return err
	}
	return r.deleteWhere(ctx, q, TImplementationTenantStates, row{"IMPLEMENTATION_NAME": impl})
}

// ImplementationGeneration returns the counter of changes of the
// activation.
func (r *Repo) ImplementationGeneration(ctx context.Context, q dialect.Querier) (int64, error) {
	rows, err := r.selectRows(ctx, q, TImplementationGeneration, []string{"GENERATION"})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	n, _ := rows[0]["GENERATION"].(int64)
	return n, nil
}

// NextImplementationGeneration increases the counter of changes of the
// activation, so that every kernel on the database reads it again.
func (r *Repo) NextImplementationGeneration(ctx context.Context, q dialect.Querier, at time.Time) error {
	if !r.HasActivation() {
		return nil
	}
	g, err := r.ImplementationGeneration(ctx, q)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(TImplementationGeneration)); err != nil {
		return err
	}
	return r.insert(ctx, q, TImplementationGeneration, row{"ID": int64(1), "GENERATION": g + 1, "CHANGED_AT": at})
}

// ImplementationChange is an entry of the history of the activation.
type ImplementationChange struct {
	ID             string                 `json:"id"`
	ChangedAt      time.Time              `json:"changedAt"`
	ChangedBy      string                 `json:"changedBy"`
	Kind           string                 `json:"kind"`
	Implementation string                 `json:"implementation"`
	ExtensionPoint string                 `json:"extensionPoint"`
	Tenant         model.Optional[string] `json:"tenant"`
	OldState       model.Optional[string] `json:"oldState"` // active, inactive or null for no setting
	NewState       model.Optional[string] `json:"newState"`
}

// InsertImplementationChange adds an entry to the history.
func (r *Repo) InsertImplementationChange(ctx context.Context, q dialect.Querier, c ImplementationChange) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	return r.insert(ctx, q, TImplementationHistory, row{"CHANGE_ID": c.ID, "CHANGED_AT": c.ChangedAt, "CHANGED_BY": c.ChangedBy, "CHANGE_KIND": c.Kind,
		"IMPLEMENTATION_NAME": c.Implementation, "POINT_NAME": c.ExtensionPoint, "TENANT": val(c.Tenant), "OLD_STATE": val(c.OldState), "NEW_STATE": val(c.NewState)})
}

// ImplementationChanges returns the history of an implementation, or all
// for "", ordered by time.
func (r *Repo) ImplementationChanges(ctx context.Context, q dialect.Querier, impl string) ([]ImplementationChange, error) {
	filter := row{}
	if impl != "" {
		filter["IMPLEMENTATION_NAME"] = impl
	}
	rows, err := r.selectWhere(ctx, q, TImplementationHistory, []string{"CHANGE_ID", "CHANGED_AT", "CHANGED_BY", "CHANGE_KIND", "IMPLEMENTATION_NAME",
		"POINT_NAME", "TENANT", "OLD_STATE", "NEW_STATE"}, filter, "CHANGED_AT", "CHANGE_ID")
	if err != nil {
		return nil, err
	}
	out := []ImplementationChange{}
	for _, x := range rows {
		out = append(out, ImplementationChange{ID: str(x["CHANGE_ID"]), ChangedAt: tm(x["CHANGED_AT"]), ChangedBy: str(x["CHANGED_BY"]), Kind: str(x["CHANGE_KIND"]),
			Implementation: str(x["IMPLEMENTATION_NAME"]), ExtensionPoint: str(x["POINT_NAME"]), Tenant: opt(x["TENANT"]),
			OldState: opt(x["OLD_STATE"]), NewState: opt(x["NEW_STATE"])})
	}
	return out, nil
}

// Tables of traces of the selection of implementations.
const (
	TImplementationTraces       = model.KernelNamespace + "IMPLEMENTATION_TRACES"
	TImplementationTraceEntries = model.KernelNamespace + "IMPLEMENTATION_TRACE_ENTRIES"
)

// ImplementationTraces returns the running traces of the selection of
// implementations by user.
func (r *Repo) ImplementationTraces(ctx context.Context, q dialect.Querier) (map[string]AuthTrace, error) {
	rows, err := r.selectRows(ctx, q, TImplementationTraces, []string{"USER_NAME", "TENANT", "STARTED_AT", "UNTIL", "MAX_ENTRIES", "STARTED_BY"})
	if err != nil {
		return nil, err
	}
	out := map[string]AuthTrace{}
	for _, x := range rows {
		out[str(x["USER_NAME"])] = AuthTrace{User: str(x["USER_NAME"]), Tenant: opt(x["TENANT"]), StartedAt: tm(x["STARTED_AT"]), Until: tm(x["UNTIL"]),
			MaxEntries: num(x["MAX_ENTRIES"]), StartedBy: str(x["STARTED_BY"])}
	}
	return out, nil
}

// PutImplementationTrace starts or replaces the trace of a user and
// removes its entries.
func (r *Repo) PutImplementationTrace(ctx context.Context, q dialect.Querier, t AuthTrace) error {
	if err := r.DeleteImplementationTrace(ctx, q, t.User); err != nil {
		return err
	}
	if err := r.deleteWhere(ctx, q, TImplementationTraceEntries, row{"USER_NAME": t.User}); err != nil {
		return err
	}
	return r.insert(ctx, q, TImplementationTraces, row{"USER_NAME": t.User, "TENANT": val(t.Tenant), "STARTED_AT": t.StartedAt, "UNTIL": t.Until,
		"MAX_ENTRIES": int64(t.MaxEntries), "STARTED_BY": t.StartedBy})
}

// DeleteImplementationTrace stops the trace of a user; its entries stay.
func (r *Repo) DeleteImplementationTrace(ctx context.Context, q dialect.Querier, user string) error {
	return r.deleteWhere(ctx, q, TImplementationTraces, row{"USER_NAME": user})
}

// SelectionEntry is an evaluation of an extension point recorded by a
// trace. Filters and Selection are JSON.
type SelectionEntry struct {
	ID             string                 `json:"id"`
	At             time.Time              `json:"at"`
	User           string                 `json:"user"`
	Tenant         model.Optional[string] `json:"tenant"`
	ExtensionPoint string                 `json:"extensionPoint"`
	Filters        string                 `json:"-"`
	Selection      string                 `json:"-"`
	Site           CheckSite              `json:"site"`
}

// InsertSelectionEntries writes trace entries.
func (r *Repo) InsertSelectionEntries(ctx context.Context, q dialect.Querier, entries []SelectionEntry) error {
	for _, e := range entries {
		values := row{"ENTRY_ID": e.ID, "TRACED_AT": e.At, "USER_NAME": e.User, "TENANT": val(e.Tenant), "POINT_NAME": e.ExtensionPoint,
			"FILTER_VALUES": e.Filters, "SELECTION": e.Selection}
		e.Site.row(values)
		if err := r.insert(ctx, q, TImplementationTraceEntries, values); err != nil {
			return err
		}
	}
	return nil
}

// SelectionEntries returns the entries of a user in the order they were
// recorded, of one tenant or of all for "".
func (r *Repo) SelectionEntries(ctx context.Context, q dialect.Querier, user, tenant string) ([]SelectionEntry, error) {
	filter := row{"USER_NAME": user}
	if tenant != "" {
		filter["TENANT"] = tenant
	}
	rows, err := r.selectWhere(ctx, q, TImplementationTraceEntries, append([]string{"ENTRY_ID", "TRACED_AT", "USER_NAME", "TENANT", "POINT_NAME",
		"FILTER_VALUES", "SELECTION"}, siteColumns...), filter, "TRACED_AT", "ENTRY_ID")
	if err != nil {
		return nil, err
	}
	out := []SelectionEntry{}
	for _, x := range rows {
		out = append(out, SelectionEntry{ID: str(x["ENTRY_ID"]), At: tm(x["TRACED_AT"]), User: str(x["USER_NAME"]), Tenant: opt(x["TENANT"]),
			ExtensionPoint: str(x["POINT_NAME"]), Filters: str(x["FILTER_VALUES"]), Selection: str(x["SELECTION"]), Site: siteOf(x)})
	}
	return out, nil
}

// CountSelectionEntries returns the number of entries of a user.
func (r *Repo) CountSelectionEntries(ctx context.Context, q dialect.Querier, user string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+r.d.QuoteIdent(TImplementationTraceEntries)+" WHERE "+r.d.QuoteIdent("USER_NAME")+" = "+r.d.Placeholder(1), user).Scan(&n)
	return n, err
}
