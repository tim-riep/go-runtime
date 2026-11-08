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

// Tables of the diagnosis of authorization checks.
const (
	TAuthLastFailed   = model.KernelNamespace + "AUTH_LAST_FAILED"
	TAuthTraces       = model.KernelNamespace + "AUTH_TRACES"
	TAuthTraceEntries = model.KernelNamespace + "AUTH_TRACE_ENTRIES"
)

// CheckSite is where a check was made: code of a package or a function of
// the kernel.
type CheckSite struct {
	Package model.Optional[string] `json:"package"`
	Class   model.Optional[string] `json:"class"`
	Method  model.Optional[string] `json:"method"`
	Line    model.Optional[int]    `json:"line"`
	Kernel  model.Optional[string] `json:"kernel"`
}

func (s CheckSite) row(r row) {
	r["PACKAGE_ID"], r["CLASS_NAME"], r["METHOD_NAME"], r["KERNEL_FUNCTION"] = val(s.Package), val(s.Class), val(s.Method), val(s.Kernel)
	r["LINE_NO"] = nil
	if l, ok := s.Line.Get(); ok {
		r["LINE_NO"] = int64(l)
	}
}

func siteOf(r row) CheckSite {
	s := CheckSite{Package: opt(r["PACKAGE_ID"]), Class: opt(r["CLASS_NAME"]), Method: opt(r["METHOD_NAME"]), Kernel: opt(r["KERNEL_FUNCTION"])}
	if _, ok := r["LINE_NO"].(int64); ok {
		s.Line = model.Some(num(r["LINE_NO"]))
	}
	return s
}

var siteColumns = []string{"PACKAGE_ID", "CLASS_NAME", "METHOD_NAME", "LINE_NO", "KERNEL_FUNCTION"}

// FailedCheck is the last failed check of a user in a tenant. Values and
// Grants are JSON.
type FailedCheck struct {
	User     string    `json:"user"`
	Tenant   string    `json:"tenant"`
	FailedAt time.Time `json:"failedAt"`
	Object   string    `json:"object"`
	Values   string    `json:"-"`
	Site     CheckSite `json:"site"`
	Grants   string    `json:"-"`
}

// HasAuthDiagnosis reports a kernel version with the diagnosis of checks.
func (r *Repo) HasAuthDiagnosis() bool { return r.has(TAuthLastFailed, "") }

// PutLastFailed replaces the last failed check of a user in a tenant.
func (r *Repo) PutLastFailed(ctx context.Context, q dialect.Querier, f FailedCheck) error {
	if !r.HasAuthDiagnosis() {
		return nil
	}
	if err := r.deleteWhere(ctx, q, TAuthLastFailed, row{"USER_NAME": f.User, "TENANT": f.Tenant}); err != nil {
		return err
	}
	values := row{"USER_NAME": f.User, "TENANT": f.Tenant, "FAILED_AT": f.FailedAt, "OBJECT_NAME": f.Object, "CHECK_VALUES": f.Values, "GRANTS": f.Grants}
	f.Site.row(values)
	return r.insert(ctx, q, TAuthLastFailed, values)
}

// LastFailed returns the last failed check of a user in a tenant, nil
// without one.
func (r *Repo) LastFailed(ctx context.Context, q dialect.Querier, user, tenant string) (*FailedCheck, error) {
	rows, err := r.selectWhere(ctx, q, TAuthLastFailed, append([]string{"USER_NAME", "TENANT", "FAILED_AT", "OBJECT_NAME", "CHECK_VALUES", "GRANTS"}, siteColumns...),
		row{"USER_NAME": user, "TENANT": tenant})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	x := rows[0]
	return &FailedCheck{User: str(x["USER_NAME"]), Tenant: str(x["TENANT"]), FailedAt: tm(x["FAILED_AT"]), Object: str(x["OBJECT_NAME"]),
		Values: str(x["CHECK_VALUES"]), Grants: str(x["GRANTS"]), Site: siteOf(x)}, nil
}

// AuthTrace is a running authorization trace of a user.
type AuthTrace struct {
	User       string                 `json:"user"`
	Tenant     model.Optional[string] `json:"tenant"`
	StartedAt  time.Time              `json:"startedAt"`
	Until      time.Time              `json:"until"`
	MaxEntries int                    `json:"maxEntries"`
	StartedBy  string                 `json:"startedBy"`
}

// PutAuthTrace starts or replaces the trace of a user and removes its
// entries.
func (r *Repo) PutAuthTrace(ctx context.Context, q dialect.Querier, t AuthTrace) error {
	if err := r.DeleteAuthTrace(ctx, q, t.User); err != nil {
		return err
	}
	if err := r.deleteWhere(ctx, q, TAuthTraceEntries, row{"USER_NAME": t.User}); err != nil {
		return err
	}
	return r.insert(ctx, q, TAuthTraces, row{"USER_NAME": t.User, "TENANT": val(t.Tenant), "STARTED_AT": t.StartedAt, "UNTIL": t.Until,
		"MAX_ENTRIES": int64(t.MaxEntries), "STARTED_BY": t.StartedBy})
}

// DeleteAuthTrace stops the trace of a user; its entries stay.
func (r *Repo) DeleteAuthTrace(ctx context.Context, q dialect.Querier, user string) error {
	if !r.HasAuthDiagnosis() {
		return nil
	}
	return r.deleteWhere(ctx, q, TAuthTraces, row{"USER_NAME": user})
}

// AuthTraceOf returns the trace of a user, nil without one.
func (r *Repo) AuthTraceOf(ctx context.Context, q dialect.Querier, user string) (*AuthTrace, error) {
	rows, err := r.selectWhere(ctx, q, TAuthTraces, []string{"USER_NAME", "TENANT", "STARTED_AT", "UNTIL", "MAX_ENTRIES", "STARTED_BY"}, row{"USER_NAME": user})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	x := rows[0]
	return &AuthTrace{User: str(x["USER_NAME"]), Tenant: opt(x["TENANT"]), StartedAt: tm(x["STARTED_AT"]), Until: tm(x["UNTIL"]),
		MaxEntries: num(x["MAX_ENTRIES"]), StartedBy: str(x["STARTED_BY"])}, nil
}

// TraceEntry is a check recorded by a trace. Values is JSON.
type TraceEntry struct {
	ID        string                 `json:"id"`
	At        time.Time              `json:"at"`
	User      string                 `json:"user"`
	Tenant    model.Optional[string] `json:"tenant"`
	Mode      string                 `json:"mode"`
	Object    string                 `json:"object"`
	Allowed   model.Optional[bool]   `json:"allowed"`
	Values    string                 `json:"-"`
	Site      CheckSite              `json:"site"`
	Condition model.Optional[string] `json:"condition"`
	Rows      model.Optional[int64]  `json:"rows"`
}

// InsertTraceEntries writes trace entries.
func (r *Repo) InsertTraceEntries(ctx context.Context, q dialect.Querier, entries []TraceEntry) error {
	for _, e := range entries {
		values := row{"ENTRY_ID": e.ID, "CHECKED_AT": e.At, "USER_NAME": e.User, "TENANT": val(e.Tenant), "CHECK_MODE": e.Mode, "OBJECT_NAME": e.Object,
			"ALLOWED": valBool(e.Allowed), "CHECK_VALUES": e.Values, "CONDITION_SQL": val(e.Condition), "ROW_COUNT": nil}
		if n, ok := e.Rows.Get(); ok {
			values["ROW_COUNT"] = n
		}
		e.Site.row(values)
		if err := r.insert(ctx, q, TAuthTraceEntries, values); err != nil {
			return err
		}
	}
	return nil
}

// TraceEntries returns the entries of a user in the order they were
// recorded, of one tenant or of all for "".
func (r *Repo) TraceEntries(ctx context.Context, q dialect.Querier, user, tenant string) ([]TraceEntry, error) {
	filter := row{"USER_NAME": user}
	if tenant != "" {
		filter["TENANT"] = tenant
	}
	rows, err := r.selectWhere(ctx, q, TAuthTraceEntries, append([]string{"ENTRY_ID", "CHECKED_AT", "USER_NAME", "TENANT", "CHECK_MODE", "OBJECT_NAME",
		"ALLOWED", "CHECK_VALUES", "CONDITION_SQL", "ROW_COUNT"}, siteColumns...), filter, "CHECKED_AT", "ENTRY_ID")
	if err != nil {
		return nil, err
	}
	out := []TraceEntry{}
	for _, x := range rows {
		e := TraceEntry{ID: str(x["ENTRY_ID"]), At: tm(x["CHECKED_AT"]), User: str(x["USER_NAME"]), Tenant: opt(x["TENANT"]), Mode: str(x["CHECK_MODE"]),
			Object: str(x["OBJECT_NAME"]), Allowed: optBool(x["ALLOWED"]), Values: str(x["CHECK_VALUES"]), Condition: opt(x["CONDITION_SQL"]),
			Rows: optInt64(x["ROW_COUNT"]), Site: siteOf(x)}
		out = append(out, e)
	}
	return out, nil
}

// CountTraceEntries returns the number of entries of a user.
func (r *Repo) CountTraceEntries(ctx context.Context, q dialect.Querier, user string) (int, error) {
	if !r.HasAuthDiagnosis() {
		return 0, nil
	}
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+r.d.QuoteIdent(TAuthTraceEntries)+" WHERE "+r.d.QuoteIdent("USER_NAME")+" = "+r.d.Placeholder(1), user).Scan(&n)
	return n, err
}
