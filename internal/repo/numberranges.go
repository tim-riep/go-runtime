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
	"maps"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of number ranges.
const (
	TNumberRanges          = model.KernelNamespace + "NUMBER_RANGES"
	TNumberRangeTexts      = model.KernelNamespace + "NUMBER_RANGE_TEXTS"
	TNumberRangeIntervals  = model.KernelNamespace + "NUMBER_RANGE_INTERVALS"
	TNumberRangeHistory    = model.KernelNamespace + "NUMBER_RANGE_HISTORY"
	TNumberRangeGeneration = model.KernelNamespace + "NUMBER_RANGE_GENERATION"
)

// HasNumberRanges reports whether the kernel tables keep number ranges.
func (r *Repo) HasNumberRanges() bool { return r.has(TNumberRanges, "") }

func (r *Repo) loadNumberRanges(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	rows, err := r.selectRows(ctx, q, TNumberRanges, []string{"RANGE_NAME", "PACKAGE_ID", "FILE_PATH", "NUMBER_LENGTH", "SUBOBJECT_NAME",
		"SUBOBJECT_DATA_ELEMENT", "SUBOBJECT_DOMAIN", "PER_FISCAL_YEAR", "BUFFERING", "BLOCK_SIZE", "WARNING_PERCENT"}, "RANGE_NAME")
	if err != nil {
		return err
	}
	for _, x := range rows {
		name := str(x["RANGE_NAME"])
		n := &model.NumberRange{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]), Texts: map[string]model.Label{},
			Length: num(x["NUMBER_LENGTH"]), PerFiscalYear: flag(x["PER_FISCAL_YEAR"]), Buffering: model.NumberRangeBuffering(str(x["BUFFERING"])),
			BlockSize: num(x["BLOCK_SIZE"]), WarningPercent: num(x["WARNING_PERCENT"])}
		if sub, ok := opt(x["SUBOBJECT_NAME"]).Get(); ok {
			n.Subobject = model.Some(model.NumberRangeSubobject{Name: sub, DataElement: opt(x["SUBOBJECT_DATA_ELEMENT"]), Domain: opt(x["SUBOBJECT_DOMAIN"])})
		}
		s.NumberRanges[name] = n
	}
	texts, err := r.selectRows(ctx, q, TNumberRangeTexts, []string{"RANGE_NAME", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if n := s.NumberRanges[str(t["RANGE_NAME"])]; n != nil {
			n.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	return nil
}

func (r *Repo) writeNumberRanges(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.NumberRangeNames() {
		n := s.NumberRanges[name]
		x := row{"RANGE_NAME": name, "PACKAGE_ID": ownerValue(n.Owner), "FILE_PATH": n.File, "NUMBER_LENGTH": int64(n.Length),
			"PER_FISCAL_YEAR": n.PerFiscalYear, "BUFFERING": string(n.Buffering), "BLOCK_SIZE": nullableInt(n.BlockSize, n.BlockSize > 0),
			"WARNING_PERCENT": int64(n.WarningPercent), "SUBOBJECT_NAME": nil, "SUBOBJECT_DATA_ELEMENT": nil, "SUBOBJECT_DOMAIN": nil}
		if sub, ok := n.Subobject.Get(); ok {
			x["SUBOBJECT_NAME"], x["SUBOBJECT_DATA_ELEMENT"], x["SUBOBJECT_DOMAIN"] = sub.Name, val(sub.DataElement), val(sub.Domain)
		}
		if err := r.insert(ctx, q, TNumberRanges, x); err != nil {
			return err
		}
		for _, lang := range slices.Sorted(maps.Keys(n.Texts)) {
			l := n.Texts[lang]
			if err := r.insert(ctx, q, TNumberRangeTexts, row{"RANGE_NAME": name, "LANGUAGE": lang, "LABEL": l.Label, "DESCRIPTION": val(l.Description)}); err != nil {
				return err
			}
		}
	}
	return nil
}

// NumberInterval is an interval of a number range object in a tenant.
// Numbers are kept padded with zeros to model.MaxNumberLength digits.
type NumberInterval struct {
	UUID       string                 `json:"uuid"`
	Tenant     string                 `json:"tenant"`
	Object     string                 `json:"object"`
	ID         string                 `json:"id"`
	Subobject  model.Optional[string] `json:"subobject"`
	FiscalYear model.Optional[int64]  `json:"fiscalYear"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	// Current is the last number given or reserved, null before the
	// first.
	Current   model.Optional[string] `json:"current"`
	External  bool                   `json:"external"`
	Warned    bool                   `json:"warned"`
	ChangedAt time.Time              `json:"changedAt"`
	ChangedBy string                 `json:"changedBy"`
}

var intervalColumns = []string{model.TenantField, "INTERVAL_UUID", "RANGE_NAME", "INTERVAL_ID", "SUBOBJECT", "FISCAL_YEAR", "FROM_NO", "TO_NO",
	"CURRENT_NO", "EXTERNAL", "WARNED", "CHANGED_AT", "CHANGED_BY"}

func intervalOf(x row) NumberInterval {
	return NumberInterval{UUID: str(x["INTERVAL_UUID"]), Tenant: str(x[model.TenantField]), Object: str(x["RANGE_NAME"]), ID: str(x["INTERVAL_ID"]),
		Subobject: opt(x["SUBOBJECT"]), FiscalYear: optInt64(x["FISCAL_YEAR"]), From: str(x["FROM_NO"]), To: str(x["TO_NO"]),
		Current: opt(x["CURRENT_NO"]), External: flag(x["EXTERNAL"]), Warned: flag(x["WARNED"]), ChangedAt: tm(x["CHANGED_AT"]),
		ChangedBy: str(x["CHANGED_BY"])}
}

func intervalRow(i NumberInterval) row {
	return row{model.TenantField: i.Tenant, "INTERVAL_UUID": i.UUID, "RANGE_NAME": i.Object, "INTERVAL_ID": i.ID, "SUBOBJECT": val(i.Subobject),
		"FISCAL_YEAR": optValue(i.FiscalYear), "FROM_NO": i.From, "TO_NO": i.To, "CURRENT_NO": val(i.Current), "EXTERNAL": i.External,
		"WARNED": i.Warned, "CHANGED_AT": i.ChangedAt, "CHANGED_BY": i.ChangedBy}
}

// NumberIntervals returns the intervals of a tenant, of one object or of
// all for "", ordered by object, subobject, year and interval.
func (r *Repo) NumberIntervals(ctx context.Context, q dialect.Querier, tenant, object string) ([]NumberInterval, error) {
	filter := row{model.TenantField: tenant}
	if object != "" {
		filter["RANGE_NAME"] = object
	}
	rows, err := r.selectWhere(ctx, q, TNumberRangeIntervals, intervalColumns, filter, "RANGE_NAME", "SUBOBJECT", "FISCAL_YEAR", "INTERVAL_ID")
	if err != nil {
		return nil, err
	}
	out := []NumberInterval{}
	for _, x := range rows {
		out = append(out, intervalOf(x))
	}
	return out, nil
}

// NumberIntervalsOf returns the intervals of an object in all tenants.
func (r *Repo) NumberIntervalsOf(ctx context.Context, q dialect.Querier, object string) ([]NumberInterval, error) {
	rows, err := r.selectWhere(ctx, q, TNumberRangeIntervals, intervalColumns, row{"RANGE_NAME": object}, model.TenantField, "INTERVAL_ID")
	if err != nil {
		return nil, err
	}
	var out []NumberInterval
	for _, x := range rows {
		out = append(out, intervalOf(x))
	}
	return out, nil
}

// NumberIntervalByUUID returns an interval of a tenant.
func (r *Repo) NumberIntervalByUUID(ctx context.Context, q dialect.Querier, tenant, uuid string) (NumberInterval, bool, error) {
	rows, err := r.selectWhere(ctx, q, TNumberRangeIntervals, intervalColumns, row{model.TenantField: tenant, "INTERVAL_UUID": uuid})
	if err != nil || len(rows) == 0 {
		return NumberInterval{}, false, err
	}
	return intervalOf(rows[0]), true, nil
}

// InsertNumberInterval adds an interval.
func (r *Repo) InsertNumberInterval(ctx context.Context, q dialect.Querier, i NumberInterval) error {
	return r.insert(ctx, q, TNumberRangeIntervals, intervalRow(i))
}

// UpdateNumberInterval writes all columns of an interval.
func (r *Repo) UpdateNumberInterval(ctx context.Context, q dialect.Querier, i NumberInterval) error {
	x := intervalRow(i)
	delete(x, model.TenantField)
	delete(x, "INTERVAL_UUID")
	return r.update(ctx, q, TNumberRangeIntervals, row{model.TenantField: i.Tenant, "INTERVAL_UUID": i.UUID}, x)
}

// DeleteNumberInterval removes an interval.
func (r *Repo) DeleteNumberInterval(ctx context.Context, q dialect.Querier, tenant, uuid string) error {
	return r.deleteWhere(ctx, q, TNumberRangeIntervals, row{model.TenantField: tenant, "INTERVAL_UUID": uuid})
}

// NumberRangeChange is an entry of the history of intervals.
type NumberRangeChange struct {
	ID         string                 `json:"id"`
	ChangedAt  time.Time              `json:"changedAt"`
	ChangedBy  string                 `json:"changedBy"`
	Kind       string                 `json:"kind"`
	Tenant     string                 `json:"tenant"`
	Object     string                 `json:"object"`
	Interval   string                 `json:"interval"`
	Subobject  model.Optional[string] `json:"subobject"`
	FiscalYear model.Optional[int64]  `json:"fiscalYear"`
	OldState   model.Optional[string] `json:"oldState"` // the interval as JSON, null when added
	NewState   model.Optional[string] `json:"newState"` // null when deleted
	Confirmed  bool                   `json:"confirmed"`
}

var changeColumns = []string{"CHANGE_ID", "CHANGED_AT", "CHANGED_BY", "CHANGE_KIND", "TENANT", "RANGE_NAME", "INTERVAL_ID", "SUBOBJECT",
	"FISCAL_YEAR", "OLD_STATE", "NEW_STATE", "CONFIRMED"}

// InsertNumberRangeChange adds an entry to the history.
func (r *Repo) InsertNumberRangeChange(ctx context.Context, q dialect.Querier, c NumberRangeChange) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	return r.insert(ctx, q, TNumberRangeHistory, row{"CHANGE_ID": c.ID, "CHANGED_AT": c.ChangedAt, "CHANGED_BY": c.ChangedBy, "CHANGE_KIND": c.Kind,
		"TENANT": c.Tenant, "RANGE_NAME": c.Object, "INTERVAL_ID": c.Interval, "SUBOBJECT": val(c.Subobject), "FISCAL_YEAR": optValue(c.FiscalYear),
		"OLD_STATE": val(c.OldState), "NEW_STATE": val(c.NewState), "CONFIRMED": c.Confirmed})
}

// NumberRangeChanges returns the history of a tenant, of an object or of
// all for "", ordered by time.
func (r *Repo) NumberRangeChanges(ctx context.Context, q dialect.Querier, tenant, object string) ([]NumberRangeChange, error) {
	filter := row{}
	if tenant != "" {
		filter["TENANT"] = tenant
	}
	if object != "" {
		filter["RANGE_NAME"] = object
	}
	rows, err := r.selectWhere(ctx, q, TNumberRangeHistory, changeColumns, filter, "CHANGED_AT", "CHANGE_ID")
	if err != nil {
		return nil, err
	}
	out := []NumberRangeChange{}
	for _, x := range rows {
		out = append(out, NumberRangeChange{ID: str(x["CHANGE_ID"]), ChangedAt: tm(x["CHANGED_AT"]), ChangedBy: str(x["CHANGED_BY"]),
			Kind: str(x["CHANGE_KIND"]), Tenant: str(x["TENANT"]), Object: str(x["RANGE_NAME"]), Interval: str(x["INTERVAL_ID"]),
			Subobject: opt(x["SUBOBJECT"]), FiscalYear: optInt64(x["FISCAL_YEAR"]), OldState: opt(x["OLD_STATE"]), NewState: opt(x["NEW_STATE"]),
			Confirmed: flag(x["CONFIRMED"])})
	}
	return out, nil
}

// NumberRangeGeneration returns the counter of changes of intervals.
func (r *Repo) NumberRangeGeneration(ctx context.Context, q dialect.Querier) (int64, error) {
	rows, err := r.selectRows(ctx, q, TNumberRangeGeneration, []string{"GENERATION"})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	n, _ := rows[0]["GENERATION"].(int64)
	return n, nil
}

// LockNumberInterval locks an interval for the rest of the transaction by
// writing its row unchanged: PostgreSQL holds the lock of the row until the
// commit, SQLite takes the lock for writing the database. Writing first,
// not reading, keeps two transactions from both reading and then waiting
// for each other. It reports whether the interval exists.
func (r *Repo) LockNumberInterval(ctx context.Context, q dialect.Querier, tenant, uuid string) (bool, error) {
	where, args, err := r.encodeConds(TNumberRangeIntervals, row{model.TenantField: tenant, "INTERVAL_UUID": uuid}, 0, " AND ")
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx, "UPDATE "+r.d.QuoteIdent(TNumberRangeIntervals)+" SET "+r.d.QuoteIdent("CURRENT_NO")+" = "+
		r.d.QuoteIdent("CURRENT_NO")+" WHERE "+where, args...)
	if err != nil {
		return false, r.d.ClassifyError(err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetNumberIntervalCurrent writes the current number of an interval and
// whether the kernel warned about its level.
func (r *Repo) SetNumberIntervalCurrent(ctx context.Context, q dialect.Querier, tenant, uuid, current string, warned bool) error {
	return r.update(ctx, q, TNumberRangeIntervals, row{model.TenantField: tenant, "INTERVAL_UUID": uuid}, row{"CURRENT_NO": current, "WARNED": warned})
}

// BumpNumberRangeGeneration increases the counter of changes of intervals
// in place. Under PostgreSQL the row stays locked until the commit, so
// maintenance of intervals runs one at a time.
func (r *Repo) BumpNumberRangeGeneration(ctx context.Context, q dialect.Querier, at time.Time) error {
	if !r.HasNumberRanges() {
		return nil
	}
	gen, now := r.d.QuoteIdent("GENERATION"), r.d.Placeholder(1)
	at2, err := r.encodeValue(TNumberRangeGeneration, "CHANGED_AT", at)
	if err != nil {
		return err
	}
	res, err := q.ExecContext(ctx, "UPDATE "+r.d.QuoteIdent(TNumberRangeGeneration)+" SET "+gen+" = "+gen+" + 1, "+r.d.QuoteIdent("CHANGED_AT")+" = "+now, at2)
	if err != nil {
		return r.d.ClassifyError(err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return r.insert(ctx, q, TNumberRangeGeneration, row{"ID": int64(1), "GENERATION": int64(1), "CHANGED_AT": at})
}

func (r *Repo) encodeValue(table, column string, v any) (any, error) {
	d, err := r.domain(table, column)
	if err != nil {
		return nil, err
	}
	return r.d.Encode(d, v)
}
