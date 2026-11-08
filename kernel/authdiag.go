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

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/rterr"
)

// checkRecord is a check of a user, kept for the last failed check and
// the authorization trace.
type checkRecord struct {
	user, tenant string
	at           time.Time
	mode         native.CheckMode
	check        authorization.Check
	allowed      bool
	site         native.CheckLocation
	// Filters: the columns by field, the condition and the rows the
	// statement read or changed.
	columns   map[string]string
	condition string
	rows      model.Optional[int64]
}

// valuesJSON writes the values of a check by field, null for any and
// {"column": NAME} for the columns of filters.
func (r *checkRecord) valuesJSON() string {
	out := map[string]any{}
	for f, v := range r.check.Values {
		out[f] = v
	}
	for f := range r.check.Any {
		out[f] = nil
	}
	for f, c := range r.columns {
		out[f] = map[string]string{"column": c}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func siteOf(at native.CheckLocation) repo.CheckSite {
	s := repo.CheckSite{}
	if at.Package != "" {
		s.Package = model.Some(string(at.Package))
	}
	if at.Class != "" {
		s.Class = model.Some(at.Class)
	}
	if at.Method != "" {
		s.Method = model.Some(at.Method)
	}
	if at.Line > 0 {
		s.Line = model.Some(at.Line)
	}
	if at.Kernel != "" {
		s.Kernel = model.Some(at.Kernel)
	}
	return s
}

// writeFailed stores a failed check as the last failed check of the user,
// with the grants of the user on the object.
func (k *Kernel) writeFailed(ctx context.Context, e *authorization.Effective, r checkRecord) {
	grants := e.Grants(r.check.Object)
	if grants == nil {
		grants = []authorization.Grant{}
	}
	g, _ := json.Marshal(grants)
	f := repo.FailedCheck{User: r.user, Tenant: r.tenant, FailedAt: r.at, Object: r.check.Object, Values: r.valuesJSON(), Site: siteOf(r.site), Grants: string(g)}
	if err := k.repo.PutLastFailed(ctx, k.db, f); err != nil {
		slog.Warn("storing the last failed authorization check", "user", r.user, "tenant", r.tenant, "error", err)
	}
}

// writeTrace stores checks of a traced user, up to the maximum of the
// trace.
func (k *Kernel) writeTrace(ctx context.Context, e *authorization.Effective, records []checkRecord) {
	if len(records) == 0 || e.Trace == nil {
		return
	}
	n, err := k.repo.CountTraceEntries(ctx, k.db, records[0].user)
	if err == nil {
		var entries []repo.TraceEntry
		for _, r := range records {
			if n+len(entries) >= e.Trace.MaxEntries || !r.at.Before(e.Trace.Until) {
				break
			}
			te := repo.TraceEntry{ID: repo.NewID(), At: r.at, User: r.user, Mode: string(r.mode), Object: r.check.Object, Values: r.valuesJSON(),
				Site: siteOf(r.site), Rows: r.rows}
			if r.tenant != "" {
				te.Tenant = model.Some(r.tenant)
			}
			if r.mode == native.CheckFilter {
				te.Condition = model.Some(r.condition)
			} else {
				te.Allowed = model.Some(r.allowed)
			}
			entries = append(entries, te)
		}
		err = k.repo.InsertTraceEntries(ctx, k.db, entries)
	}
	if err != nil {
		slog.Warn("storing an authorization trace", "user", records[0].user, "error", err)
	}
}

// recordKernelCheck stores a check that the kernel made outside an
// execution: as last failed check and in the trace.
func (k *Kernel) recordKernelCheck(ctx context.Context, e *authorization.Effective, r checkRecord) {
	if !r.allowed {
		k.writeFailed(ctx, e, r)
	}
	if e.Tracing(r.at) {
		k.writeTrace(ctx, e, []checkRecord{r})
	}
}

// rememberFailedCheck records a failed check of the kernel for an actor.
func (k *Kernel) rememberFailedCheck(ctx context.Context, e *authorization.Effective, user, tenant string, c authorization.Check, allowed bool, function string) {
	k.recordKernelCheck(ctx, e, checkRecord{user: user, tenant: tenant, at: k.now(), mode: native.CheckStatement, check: c, allowed: allowed,
		site: native.CheckLocation{Kernel: function}})
}

// flushDiagnosis writes the last failed check and the traced checks of an
// execution after its sessions ended, so that a rollback keeps them.
func (x *execution) flushDiagnosis() {
	if x.auth == nil || x.failed == nil && len(x.traced) == 0 {
		return
	}
	ctx := context.Background()
	if x.failed != nil {
		x.k.writeFailed(ctx, x.auth, *x.failed)
	}
	x.k.writeTrace(ctx, x.auth, x.traced)
}

// LastFailedCheck is the last failed authorization check of a user in a
// tenant, with the grants of the user on the object at that time.
type LastFailedCheck struct {
	User     string                     `json:"user"`
	Tenant   string                     `json:"tenant"`
	FailedAt time.Time                  `json:"failedAt"`
	Object   string                     `json:"object"`
	Values   map[string]json.RawMessage `json:"values"` // null for any
	Site     repo.CheckSite             `json:"site"`
	Grants   json.RawMessage            `json:"grants"`
}

// LastFailedCheck returns the last failed check of a user in a tenant, or
// rterr.NotFound.
func (k *Kernel) LastFailedCheck(ctx context.Context, user, tenant string) (*LastFailedCheck, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	f, err := k.repo.LastFailed(ctx, k.db, user, tenant)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, &rterr.NotFound{Kind: "failed check of user " + user + " in tenant", Name: tenant}
	}
	out := &LastFailedCheck{User: f.User, Tenant: f.Tenant, FailedAt: f.FailedAt, Object: f.Object, Site: f.Site, Grants: json.RawMessage(f.Grants)}
	_ = json.Unmarshal([]byte(f.Values), &out.Values)
	return out, nil
}

// AuthTrace is a running authorization trace.
type AuthTrace = repo.AuthTrace

// TraceOptions start an authorization trace.
type TraceOptions struct {
	Tenant     model.Optional[string] // only checks in this tenant
	Duration   time.Duration          // default 30 minutes
	MaxEntries int                    // default 10 000
	By         string                 // who starts it
}

// StartTrace records the checks of a user from now on, replacing a
// running trace and its entries.
func (k *Kernel) StartTrace(ctx context.Context, user string, opts TraceOptions) (*AuthTrace, error) {
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
		if err := k.repo.PutAuthTrace(ctx, q, t); err != nil {
			return err
		}
		// The effective authorizations carry the trace.
		return k.repo.NextAuthGeneration(ctx, q, now)
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// StopTrace ends the trace of a user; the entries stay until the next
// start.
func (k *Kernel) StopTrace(ctx context.Context, user string) error {
	return k.userTx(ctx, func(q dialect.Querier) error {
		if err := k.repo.DeleteAuthTrace(ctx, q, user); err != nil {
			return err
		}
		return k.repo.NextAuthGeneration(ctx, q, k.timestamp())
	})
}

// TraceResult is a trace with its entries; Trace is nil when no trace
// runs, Ended when its time is over or it was stopped.
type TraceResult struct {
	Trace   *AuthTrace       `json:"trace"`
	Ended   bool             `json:"ended"`
	Entries []TraceEntryView `json:"entries"`
}

// TraceEntryView is a recorded check with its values.
type TraceEntryView struct {
	repo.TraceEntry
	Values map[string]json.RawMessage `json:"values"`
}

// Trace returns the trace of a user and its entries, of one tenant or of
// all for "".
func (k *Kernel) Trace(ctx context.Context, user, tenant string) (*TraceResult, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	tr, err := k.repo.AuthTraceOf(ctx, k.db, user)
	if err != nil {
		return nil, err
	}
	list, err := k.repo.TraceEntries(ctx, k.db, user, tenant)
	if err != nil {
		return nil, err
	}
	out := &TraceResult{Trace: tr, Ended: tr == nil || !k.now().Before(tr.Until), Entries: []TraceEntryView{}}
	for _, e := range list {
		v := TraceEntryView{TraceEntry: e}
		_ = json.Unmarshal([]byte(e.Values), &v.Values)
		out.Entries = append(out.Entries, v)
	}
	return out, nil
}
