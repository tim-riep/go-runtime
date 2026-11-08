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
	"log/slog"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// SelectionReason tells why an implementation was selected or not.
type SelectionReason struct {
	Implementation string `json:"implementation"`
	Reason         string `json:"reason"`
}

// selectionTrace returns the running trace of the user of the execution
// in its tenant, or nil. A trace that is off costs nothing: whether one
// runs is part of the activation the execution reads anyway.
func (x *execution) selectionTrace(a *activation) *repo.AuthTrace {
	user, ok := x.opts.User.Get()
	if !ok || a == nil {
		return nil
	}
	t, ok := a.traces[user]
	if !ok || !x.k.now().Before(t.Until) {
		return nil
	}
	if tenant, ok := t.Tenant.Get(); ok && tenant != x.tenant {
		return nil
	}
	return &t
}

// recordSelection keeps an evaluation of an extension point for the
// trace.
func (x *execution) recordSelection(point string, values map[string]string, reasons []SelectionReason, at native.CheckLocation) {
	filters, _ := json.Marshal(values)
	if reasons == nil {
		reasons = []SelectionReason{}
	}
	selection, _ := json.Marshal(reasons)
	e := repo.SelectionEntry{ID: repo.NewID(), At: x.k.timestamp(), User: x.opts.User.Or(""), ExtensionPoint: point, Filters: string(filters),
		Selection: string(selection), Site: siteOf(at)}
	if x.tenant != "" {
		e.Tenant = model.Some(x.tenant)
	}
	x.selections = append(x.selections, e)
	slog.Debug("extension point", "point", point, "filters", string(filters), "selection", string(selection))
}

// flushSelections writes the traced evaluations of an execution after its
// sessions ended, up to the maximum of the trace.
func (x *execution) flushSelections() {
	if len(x.selections) == 0 || x.activation == nil {
		return
	}
	ctx := context.Background()
	t := x.selectionTrace(x.activation)
	if t == nil {
		return
	}
	n, err := x.k.repo.CountSelectionEntries(ctx, x.k.db, t.User)
	if err == nil {
		entries := x.selections
		if room := t.MaxEntries - n; len(entries) > room {
			entries = entries[:max(room, 0)]
		}
		err = x.k.repo.InsertSelectionEntries(ctx, x.k.db, entries)
	}
	if err != nil {
		slog.Warn("storing a trace of extension points", "user", t.User, "error", err)
	}
}

// StartImplementationTrace records which implementations the extension
// points that a user runs select, and why, from now on, replacing a
// running trace and its entries. It takes effect with the next execution.
func (k *Kernel) StartImplementationTrace(ctx context.Context, user string, opts TraceOptions) (*AuthTrace, error) {
	if opts.Duration <= 0 {
		opts.Duration = 30 * time.Minute
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 10_000
	}
	if opts.By == "" {
		opts.By = "api"
	}
	now := k.timestamp()
	t := AuthTrace{User: user, Tenant: opts.Tenant, StartedAt: now, Until: now.Add(opts.Duration), MaxEntries: opts.MaxEntries, StartedBy: opts.By}
	err := k.userTx(ctx, func(q dialect.Querier) error {
		if _, err := k.user(ctx, q, user); err != nil {
			return err
		}
		if err := k.repo.PutImplementationTrace(ctx, q, t); err != nil {
			return err
		}
		return k.repo.NextImplementationGeneration(ctx, q, now)
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// StopImplementationTrace ends the trace of a user; the entries stay
// until the next start.
func (k *Kernel) StopImplementationTrace(ctx context.Context, user string) error {
	return k.userTx(ctx, func(q dialect.Querier) error {
		if err := k.repo.DeleteImplementationTrace(ctx, q, user); err != nil {
			return err
		}
		return k.repo.NextImplementationGeneration(ctx, q, k.timestamp())
	})
}

// SelectionTraceResult is a trace of the selection of implementations
// with its entries; Trace is nil when no trace runs.
type SelectionTraceResult struct {
	Trace   *AuthTrace           `json:"trace"`
	Ended   bool                 `json:"ended"`
	Entries []SelectionEntryView `json:"entries"`
}

// SelectionEntryView is a recorded evaluation of an extension point: the
// filter values and every implementation with the reason why it was
// selected or not.
type SelectionEntryView struct {
	repo.SelectionEntry
	Filters   map[string]string `json:"filters"`
	Selection []SelectionReason `json:"selection"`
}

// ImplementationTrace returns the trace of the selection of a user and
// its entries, of one tenant or of all for "".
func (k *Kernel) ImplementationTrace(ctx context.Context, user, tenant string) (*SelectionTraceResult, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	traces, err := k.repo.ImplementationTraces(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := &SelectionTraceResult{Ended: true, Entries: []SelectionEntryView{}}
	if t, ok := traces[user]; ok {
		out.Trace, out.Ended = &t, !k.now().Before(t.Until)
	}
	list, err := k.repo.SelectionEntries(ctx, k.db, user, tenant)
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		v := SelectionEntryView{SelectionEntry: e}
		_ = json.Unmarshal([]byte(e.Filters), &v.Filters)
		_ = json.Unmarshal([]byte(e.Selection), &v.Selection)
		out.Entries = append(out.Entries, v)
	}
	return out, nil
}
