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
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/diff"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// NumberRangeObject is the authorization object of TRBASE for the
// maintenance of intervals.
const NumberRangeObject = "/TRBASE/NUMBER_RANGE"

// NumberInterval is an interval of a number range object in a tenant.
// Numbers have the length of the object, with leading zeros.
type NumberInterval struct {
	Object     string                 `json:"object"`
	ID         string                 `json:"id"`
	Subobject  model.Optional[string] `json:"subobject"`
	FiscalYear model.Optional[int64]  `json:"fiscalYear"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	// Current is the last number given, null before the first.
	Current  model.Optional[string] `json:"current"`
	External bool                   `json:"external"`
	// UsedPercent is the share of the interval given so far; set when
	// reading.
	UsedPercent int       `json:"usedPercent"`
	ChangedAt   time.Time `json:"changedAt"`
	ChangedBy   string    `json:"changedBy"`
}

// NumberRangeOptions control the maintenance of intervals.
type NumberRangeOptions struct {
	// Actor checks /TRBASE/NUMBER_RANGE for the object; without actor, as
	// on the command line, nothing is checked.
	Actor *Actor
	// Confirm allows moving the current number back and deleting an
	// interval that gave numbers.
	Confirm bool
	// By names who makes a change for the history; the actor by default.
	By string
}

func (o NumberRangeOptions) by() string {
	switch {
	case o.By != "":
		return o.By
	case o.Actor != nil:
		return o.Actor.User
	}
	return "api"
}

// NumberRangeObjects returns the number range objects of a package, or of
// all for "", ordered by name.
func (k *Kernel) NumberRangeObjects(ctx context.Context, pkg model.PackageID) ([]*model.NumberRange, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	out := []*model.NumberRange{}
	for _, name := range s.NumberRangeNames() {
		if n := s.NumberRanges[name]; pkg == "" || n.Owner == pkg {
			out = append(out, n)
		}
	}
	return out, nil
}

// NumberRangeObject returns a number range object.
func (k *Kernel) NumberRangeObject(ctx context.Context, name string) (*model.NumberRange, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	n := s.NumberRanges[name]
	if n == nil {
		return nil, &rterr.NotFound{Kind: "number range", Name: name}
	}
	return n, nil
}

// numberMaintenance maintains the intervals of a tenant in a transaction.
type numberMaintenance struct {
	k         *Kernel
	q         dialect.Querier
	s         *model.Schema
	tenant    string
	by        string
	confirm   bool
	authorize func(object, activity string) error
}

// maintain runs fn with the maintenance of intervals in a transaction of
// its own.
func (k *Kernel) maintain(ctx context.Context, tenant string, opts NumberRangeOptions, changes bool, fn func(m *numberMaintenance) error) error {
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	if !k.repo.HasNumberRanges() {
		return fmt.Errorf("the kernel tables of this database keep no number ranges")
	}
	tenants, err := k.repo.Tenants(ctx, k.db)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(tenants, func(t repo.Tenant) bool { return t.ID == tenant }) {
		return &rterr.NotFound{Kind: "tenant", Name: tenant}
	}
	s, err := k.Schema(ctx)
	if err != nil {
		return err
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		m := &numberMaintenance{k: k, q: tx, s: s, tenant: tenant, by: opts.by(), confirm: opts.Confirm,
			authorize: func(object, activity string) error {
				return k.authorizeActor(ctx, opts.Actor, NumberRangeObject, activity, map[string]string{"NUMBER_RANGE_OBJECT": object}, "number ranges")
			}}
		if changes {
			if err := k.repo.BumpNumberRangeGeneration(ctx, tx, k.now()); err != nil {
				return err
			}
		}
		return fn(m)
	})
}

// NumberIntervals returns the intervals of an object in a tenant.
func (k *Kernel) NumberIntervals(ctx context.Context, tenant, object string, opts NumberRangeOptions) ([]NumberInterval, error) {
	var out []NumberInterval
	err := k.maintain(ctx, tenant, opts, false, func(m *numberMaintenance) error {
		var err error
		out, err = m.list(ctx, object)
		return err
	})
	return out, err
}

// AddNumberInterval adds an interval in a tenant.
func (k *Kernel) AddNumberInterval(ctx context.Context, tenant string, iv NumberInterval, opts NumberRangeOptions) error {
	return k.maintain(ctx, tenant, opts, true, func(m *numberMaintenance) error { return m.add(ctx, iv) })
}

// ChangeNumberInterval changes the bounds, the current number or the kind
// of an interval, found by object, ID, subobject and year.
func (k *Kernel) ChangeNumberInterval(ctx context.Context, tenant string, iv NumberInterval, opts NumberRangeOptions) error {
	return k.maintain(ctx, tenant, opts, true, func(m *numberMaintenance) error { return m.change(ctx, iv) })
}

// DeleteNumberInterval deletes an interval.
func (k *Kernel) DeleteNumberInterval(ctx context.Context, tenant, object, id string, sub model.Optional[string], year model.Optional[int64], opts NumberRangeOptions) error {
	return k.maintain(ctx, tenant, opts, true, func(m *numberMaintenance) error { return m.delete(ctx, object, id, sub, year) })
}

// NumberRangeHistory returns the changes of intervals in a tenant, of an
// object or of all for "".
func (k *Kernel) NumberRangeHistory(ctx context.Context, tenant, object string, opts NumberRangeOptions) ([]repo.NumberRangeChange, error) {
	var out []repo.NumberRangeChange
	err := k.maintain(ctx, tenant, opts, false, func(m *numberMaintenance) error {
		var err error
		out, err = m.history(ctx, object)
		return err
	})
	return out, err
}

func numberError(kind rterr.NumberRangeErrorKind, object, format string, args ...any) error {
	return &rterr.NumberRangeError{Kind: kind, Object: object, Message: fmt.Sprintf(format, args...)}
}

// object returns a number range object of the schema.
func (m *numberMaintenance) object(name string) (*model.NumberRange, error) {
	n := m.s.NumberRanges[name]
	if n == nil {
		return nil, numberError(rterr.NumberRangeNotFound, name, "the number range object does not exist")
	}
	return n, nil
}

func (m *numberMaintenance) list(ctx context.Context, object string) ([]NumberInterval, error) {
	n, err := m.object(object)
	if err != nil {
		return nil, err
	}
	if err := m.authorize(object, "READ"); err != nil {
		return nil, err
	}
	rows, err := m.k.repo.NumberIntervals(ctx, m.q, m.tenant, object)
	if err != nil {
		return nil, err
	}
	out := []NumberInterval{}
	for _, r := range rows {
		out = append(out, external(n, r))
	}
	return out, nil
}

func (m *numberMaintenance) history(ctx context.Context, object string) ([]repo.NumberRangeChange, error) {
	if object != "" {
		if _, err := m.object(object); err != nil {
			return nil, err
		}
		if err := m.authorize(object, "READ"); err != nil {
			return nil, err
		}
		return m.k.repo.NumberRangeChanges(ctx, m.q, m.tenant, object)
	}
	all, err := m.k.repo.NumberRangeChanges(ctx, m.q, m.tenant, "")
	if err != nil {
		return nil, err
	}
	out := []repo.NumberRangeChange{}
	for _, c := range all {
		if m.authorize(c.Object, "READ") == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// external turns a stored interval into one with numbers of the length of
// the object.
func external(n *model.NumberRange, r repo.NumberInterval) NumberInterval {
	iv := NumberInterval{Object: r.Object, ID: r.ID, Subobject: r.Subobject, FiscalYear: r.FiscalYear, From: shorten(r.From, n.Length),
		To: shorten(r.To, n.Length), External: r.External, ChangedAt: r.ChangedAt, ChangedBy: r.ChangedBy}
	if c, ok := r.Current.Get(); ok {
		iv.Current = model.Some(shorten(c, n.Length))
		iv.UsedPercent = usedPercent(r.From, r.To, c)
	}
	return iv
}

var intervalIDPattern = regexp.MustCompile(`^[A-Z0-9]{2}$`)

// checkKey checks the interval ID, the subobject and the year of an
// interval against its object.
func (m *numberMaintenance) checkKey(n *model.NumberRange, id string, sub model.Optional[string], year model.Optional[int64]) error {
	if !intervalIDPattern.MatchString(id) {
		return numberError(rterr.NumberRangeArgument, n.Name, "the interval %q is not two upper-case letters or digits", id)
	}
	return checkSubobjectAndYear(m.s, n, sub, year)
}

// checkSubobjectAndYear checks that a subobject and a fiscal year are
// given exactly when the object has them and that they fit.
func checkSubobjectAndYear(s *model.Schema, n *model.NumberRange, sub model.Optional[string], year model.Optional[int64]) error {
	so, hasSub := n.Subobject.Get()
	v, given := sub.Get()
	switch {
	case hasSub && !given:
		return numberError(rterr.NumberRangeArgument, n.Name, "the number range object needs a value of its subobject %s", so.Name)
	case !hasSub && given:
		return numberError(rterr.NumberRangeArgument, n.Name, "the number range object has no subobject")
	case hasSub:
		if d := subobjectDomain(s, so); d != nil {
			if err := checkSubobjectValue(d, v); err != nil {
				return numberError(rterr.NumberRangeArgument, n.Name, "the subobject %s %q does not fit: %v", so.Name, v, err)
			}
		}
	}
	y, hasYear := year.Get()
	switch {
	case n.PerFiscalYear && !hasYear:
		return numberError(rterr.NumberRangeArgument, n.Name, "the number range object needs a fiscal year")
	case !n.PerFiscalYear && hasYear:
		return numberError(rterr.NumberRangeArgument, n.Name, "the number range object has no fiscal years")
	case hasYear && (y < 1 || y > 9999):
		return numberError(rterr.NumberRangeArgument, n.Name, "the fiscal year %d is not between 1 and 9999", y)
	}
	return nil
}

func subobjectDomain(s *model.Schema, so model.NumberRangeSubobject) *model.Domain {
	name := so.Domain.Or("")
	if de, ok := so.DataElement.Get(); ok {
		if d := s.DataElements[de]; d != nil {
			name = d.Domain
		}
	}
	return s.Domains[name]
}

// checkSubobjectValue checks a value of a subobject in text form: the
// length of CHAR, the decimal form of integers.
func checkSubobjectValue(d *model.Domain, v string) error {
	switch d.Type {
	case model.Char:
		if n := utf8.RuneCountInString(v); n > d.Length {
			return fmt.Errorf("%d characters, at most %d", n, d.Length)
		}
	case model.Int4, model.Int8:
		bits := 64
		if d.Type == model.Int4 {
			bits = 32
		}
		n, err := strconv.ParseInt(v, 10, bits)
		if err != nil || strconv.FormatInt(n, 10) != v {
			return fmt.Errorf("not a whole number")
		}
	}
	return nil
}

// stored finds the stored interval of a key.
func (m *numberMaintenance) stored(ctx context.Context, n *model.NumberRange, id string, sub model.Optional[string], year model.Optional[int64]) (repo.NumberInterval, []repo.NumberInterval, bool, error) {
	all, err := m.k.repo.NumberIntervals(ctx, m.q, m.tenant, n.Name)
	if err != nil {
		return repo.NumberInterval{}, nil, false, err
	}
	var same []repo.NumberInterval
	for _, r := range all {
		if r.Subobject == sub && r.FiscalYear == year {
			same = append(same, r)
		}
	}
	for _, r := range same {
		if r.ID == id {
			return r, same, true, nil
		}
	}
	return repo.NumberInterval{}, same, false, nil
}

// bounds checks and pads the numbers of an interval.
func bounds(n *model.NumberRange, iv NumberInterval) (from, to string, current model.Optional[string], err error) {
	pad := func(what, v string) (string, error) {
		if !isDigits(v) || len(v) > n.Length {
			return "", numberError(rterr.NumberRangeArgument, n.Name, "the %s %q is not a number of up to %d digits", what, v, n.Length)
		}
		return padNumber(v), nil
	}
	if from, err = pad("from number", iv.From); err != nil {
		return
	}
	if to, err = pad("to number", iv.To); err != nil {
		return
	}
	if from > to {
		err = numberError(rterr.NumberRangeArgument, n.Name, "the from number %s is greater than the to number %s", iv.From, iv.To)
		return
	}
	if c, ok := iv.Current.Get(); ok {
		var p string
		if p, err = pad("current number", c); err != nil {
			return
		}
		if iv.External {
			err = numberError(rterr.NumberRangeArgument, n.Name, "an external interval has no current number")
			return
		}
		if p < from || p > to {
			err = numberError(rterr.NumberRangeArgument, n.Name, "the current number %s lies outside of the interval %s to %s", c, iv.From, iv.To)
			return
		}
		current = model.Some(p)
	}
	return
}

// overlaps rejects an interval whose numbers overlap with another interval
// of the same subobject and year.
func overlaps(n *model.NumberRange, id, from, to string, others []repo.NumberInterval) error {
	for _, o := range others {
		if o.ID != id && from <= o.To && o.From <= to {
			return numberError(rterr.NumberRangeArgument, n.Name, "the interval %s overlaps with interval %s (%s to %s)", id, o.ID,
				shorten(o.From, n.Length), shorten(o.To, n.Length))
		}
	}
	return nil
}

func (m *numberMaintenance) add(ctx context.Context, iv NumberInterval) error {
	n, err := m.object(iv.Object)
	if err != nil {
		return err
	}
	if err := m.authorize(n.Name, "CREATE"); err != nil {
		return err
	}
	if err := m.checkKey(n, iv.ID, iv.Subobject, iv.FiscalYear); err != nil {
		return err
	}
	from, to, current, err := bounds(n, iv)
	if err != nil {
		return err
	}
	_, same, found, err := m.stored(ctx, n, iv.ID, iv.Subobject, iv.FiscalYear)
	if err != nil {
		return err
	}
	if found {
		return numberError(rterr.NumberRangeArgument, n.Name, "the interval %s exists already%s", iv.ID, keyText(iv.Subobject, iv.FiscalYear))
	}
	if err := overlaps(n, iv.ID, from, to, same); err != nil {
		return err
	}
	r := repo.NumberInterval{UUID: repo.NewID(), Tenant: m.tenant, Object: n.Name, ID: iv.ID, Subobject: iv.Subobject, FiscalYear: iv.FiscalYear,
		From: from, To: to, Current: current, External: iv.External, ChangedAt: m.k.now(), ChangedBy: m.by}
	r.Warned = warned(n, r)
	if err := m.k.repo.InsertNumberInterval(ctx, m.q, r); err != nil {
		return err
	}
	return m.record(ctx, "add", n, r, nil, &r, false)
}

func (m *numberMaintenance) change(ctx context.Context, iv NumberInterval) error {
	n, err := m.object(iv.Object)
	if err != nil {
		return err
	}
	if err := m.authorize(n.Name, "UPDATE"); err != nil {
		return err
	}
	if err := m.checkKey(n, iv.ID, iv.Subobject, iv.FiscalYear); err != nil {
		return err
	}
	from, to, current, err := bounds(n, iv)
	if err != nil {
		return err
	}
	old, same, found, err := m.stored(ctx, n, iv.ID, iv.Subobject, iv.FiscalYear)
	if err != nil {
		return err
	}
	if !found {
		return numberError(rterr.NumberRangeNotFound, n.Name, "the interval %s does not exist%s", iv.ID, keyText(iv.Subobject, iv.FiscalYear))
	}
	if err := overlaps(n, iv.ID, from, to, same); err != nil {
		return err
	}
	back := false
	if oc, ok := old.Current.Get(); ok {
		c, has := current.Get()
		back = !has || c < oc
	}
	if back && !m.confirm {
		return numberError(rterr.NumberRangeConfirmation, n.Name, "the change moves the current number of interval %s back, so numbers would be given again; confirm it", iv.ID)
	}
	r := old
	r.From, r.To, r.Current, r.External, r.ChangedAt, r.ChangedBy = from, to, current, iv.External, m.k.now(), m.by
	r.Warned = warned(n, r)
	if err := m.k.repo.UpdateNumberInterval(ctx, m.q, r); err != nil {
		return err
	}
	if back {
		m.k.auditNumberRange("current number of an interval moved back", m.tenant, n.Name, iv.ID, m.by)
	}
	return m.record(ctx, "change", n, r, &old, &r, back)
}

func (m *numberMaintenance) delete(ctx context.Context, object, id string, sub model.Optional[string], year model.Optional[int64]) error {
	n, err := m.object(object)
	if err != nil {
		return err
	}
	if err := m.authorize(n.Name, "DELETE"); err != nil {
		return err
	}
	if err := m.checkKey(n, id, sub, year); err != nil {
		return err
	}
	old, _, found, err := m.stored(ctx, n, id, sub, year)
	if err != nil {
		return err
	}
	if !found {
		return numberError(rterr.NumberRangeNotFound, n.Name, "the interval %s does not exist%s", id, keyText(sub, year))
	}
	used := old.Current.Valid()
	if used && !m.confirm {
		return numberError(rterr.NumberRangeConfirmation, n.Name, "interval %s gave numbers up to %s; deleting it lets them be given again; confirm it", id,
			shorten(old.Current.Or(""), n.Length))
	}
	if err := m.k.repo.DeleteNumberInterval(ctx, m.q, m.tenant, old.UUID); err != nil {
		return err
	}
	if used {
		m.k.auditNumberRange("used interval deleted", m.tenant, n.Name, id, m.by)
	}
	return m.record(ctx, "delete", n, old, &old, nil, used)
}

// record writes an entry of the history.
func (m *numberMaintenance) record(ctx context.Context, kind string, n *model.NumberRange, key repo.NumberInterval, old, new *repo.NumberInterval, confirmed bool) error {
	state := func(r *repo.NumberInterval) model.Optional[string] {
		if r == nil {
			return model.None[string]()
		}
		data, _ := json.Marshal(external(n, *r))
		return model.Some(string(data))
	}
	return m.k.repo.InsertNumberRangeChange(ctx, m.q, repo.NumberRangeChange{ChangedAt: m.k.now(), ChangedBy: m.by, Kind: kind, Tenant: m.tenant,
		Object: n.Name, Interval: key.ID, Subobject: key.Subobject, FiscalYear: key.FiscalYear, OldState: state(old), NewState: state(new), Confirmed: confirmed})
}

// auditNumberRange logs a confirmed change that lets numbers be given
// again.
func (k *Kernel) auditNumberRange(msg, tenant, object, interval, by string) {
	audit(context.Background(), msg, "tenant", tenant, "numberRange", object, "interval", interval, "by", by)
}

func keyText(sub model.Optional[string], year model.Optional[int64]) string {
	var parts []string
	if s, ok := sub.Get(); ok {
		parts = append(parts, fmt.Sprintf("subobject %q", s))
	}
	if y, ok := year.Get(); ok {
		parts = append(parts, fmt.Sprintf("year %d", y))
	}
	if len(parts) == 0 {
		return ""
	}
	return " for " + strings.Join(parts, " and ")
}

// Numbers are kept as text of model.MaxNumberLength digits, so that text
// order is number order.

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func padNumber(s string) string {
	return strings.Repeat("0", max(0, model.MaxNumberLength-len(s))) + s
}

// shorten gives a stored number with the length of its object.
func shorten(s string, length int) string {
	if len(s) > length {
		return s[len(s)-length:]
	}
	return s
}

func numberValue(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

func numberText(n *big.Int) string { return padNumber(n.String()) }

// usedPercent is the share of an interval up to the current number.
func usedPercent(from, to, current string) int {
	f, t, c := numberValue(from), numberValue(to), numberValue(current)
	total := new(big.Int).Add(new(big.Int).Sub(t, f), big.NewInt(1))
	used := new(big.Int).Add(new(big.Int).Sub(c, f), big.NewInt(1))
	if used.Sign() <= 0 {
		return 0
	}
	used.Mul(used, big.NewInt(100))
	return int(used.Div(used, total).Int64())
}

// warned reports whether the current number of an interval reached the
// warning threshold of its object.
func warned(n *model.NumberRange, r repo.NumberInterval) bool {
	c, ok := r.Current.Get()
	return ok && usedPercent(r.From, r.To, c) >= n.WarningPercent
}

// numberBuffers are the blocks of buffered number ranges a kernel
// reserved, per tenant and interval.
type numberBuffers struct {
	mu     sync.Mutex
	blocks map[string]*numberBlock
}

type numberBlock struct {
	next, last string // padded; next > last when the block is used up
	generation int64
}

// visibleNumberRange returns an object that code of the package reader
// may use: its own, of a required or of a kernel package.
func (x *execution) visibleNumberRange(reader model.PackageID, name string) (*model.NumberRange, error) {
	n := x.schema.NumberRanges[name]
	if n == nil || !x.mayRead(reader, n.Owner) {
		return nil, numberError(rterr.NumberRangeNotFound, name, "the number range object does not exist or package %s does not require its package", reader)
	}
	return n, nil
}

// interval finds the interval of a tenant for an object and its key.
func (k *Kernel) interval(ctx context.Context, q dialect.Querier, s *model.Schema, tenant string, n *model.NumberRange, id string, sub model.Optional[string], year model.Optional[int64]) (repo.NumberInterval, error) {
	if err := checkSubobjectAndYear(s, n, sub, year); err != nil {
		return repo.NumberInterval{}, err
	}
	all, err := k.repo.NumberIntervals(ctx, q, tenant, n.Name)
	if err != nil {
		return repo.NumberInterval{}, err
	}
	for _, r := range all {
		if r.ID == id && r.Subobject == sub && r.FiscalYear == year {
			return r, nil
		}
	}
	return repo.NumberInterval{}, numberError(rterr.NumberRangeNotFound, n.Name, "the interval %s does not exist in tenant %s%s", id, tenant, keyText(sub, year))
}

// nextNumber gives the next number of an internal interval in a session
// of the execution: without buffering in its transaction, with buffering
// from a block of the kernel.
func (x *execution) nextNumber(ctx context.Context, s *session, reader model.PackageID, object, id string, sub model.Optional[string], year model.Optional[int64]) (string, error) {
	if x.tenant == "" {
		return "", numberError(rterr.NumberRangeArgument, object, "numbers need a tenant")
	}
	if x.opts.ReadOnly {
		return "", errReadOnly
	}
	n, err := x.visibleNumberRange(reader, object)
	if err != nil {
		return "", err
	}
	if n.Buffering == model.BufferingBuffered && x.bound == nil && !x.test && !x.written() {
		return x.k.bufferedNumber(ctx, x.tenant, x.schema, n, id, sub, year)
	}
	// Without buffering, and with buffering when a session of the
	// execution holds the write lock of SQLite or the execution runs in a
	// test or in the transaction of the kernel: in the transaction of the
	// session, where a rollback returns the number.
	if s.sqlite() {
		for _, o := range x.sessions {
			if o != s && o.written {
				return "", fmt.Errorf("SQLite allows only one writing transaction; commit or roll back session %d first", o.n)
			}
		}
	}
	if err := s.begin(ctx); err != nil {
		return "", x.classify(err)
	}
	var number string
	err = s.guard(ctx, func() error {
		r, err := x.k.interval(ctx, s.conn, x.schema, x.tenant, n, id, sub, year)
		if err != nil {
			return err
		}
		first, _, err := x.k.reserve(ctx, s.conn, x.tenant, n, r, 1)
		number = first
		return err
	})
	if err != nil {
		return "", err
	}
	s.written = true
	return shorten(number, n.Length), nil
}

// written reports whether a session of the execution changed rows.
func (x *execution) written() bool {
	return slices.ContainsFunc(x.sessions, func(s *session) bool { return s.written })
}

// reserve gives count numbers of an internal interval in a transaction:
// it locks the interval, reads it again and writes the new current
// number. It returns the first and the last number, fewer than count at
// the end of the interval.
func (k *Kernel) reserve(ctx context.Context, q dialect.Querier, tenant string, n *model.NumberRange, r repo.NumberInterval, count int) (string, string, error) {
	if r.External {
		return "", "", numberError(rterr.NumberRangeArgument, n.Name, "interval %s is external; numbers come from outside", r.ID)
	}
	ok, err := k.repo.LockNumberInterval(ctx, q, tenant, r.UUID)
	if err != nil {
		return "", "", err
	}
	if ok {
		r, ok, err = k.repo.NumberIntervalByUUID(ctx, q, tenant, r.UUID)
	}
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", numberError(rterr.NumberRangeNotFound, n.Name, "the interval %s was deleted", r.ID)
	}
	next := numberValue(r.From)
	if c, ok := r.Current.Get(); ok {
		next = numberValue(c)
		next.Add(next, big.NewInt(1))
	}
	to := numberValue(r.To)
	if next.Cmp(to) > 0 {
		return "", "", numberError(rterr.NumberRangeExhausted, n.Name, "interval %s%s has given its last number %s", r.ID, keyText(r.Subobject, r.FiscalYear),
			shorten(r.To, n.Length))
	}
	last := new(big.Int).Add(next, big.NewInt(int64(count-1)))
	if last.Cmp(to) > 0 {
		last = to
	}
	first, lastText := numberText(next), numberText(last)
	r.Current = model.Some(lastText)
	warn := !r.Warned && warned(n, r)
	if err := k.repo.SetNumberIntervalCurrent(ctx, q, tenant, r.UUID, lastText, r.Warned || warn); err != nil {
		return "", "", err
	}
	if warn {
		slog.Warn("number range interval reached its warning threshold", "tenant", tenant, "numberRange", n.Name, "interval", r.ID,
			"subobject", r.Subobject.Or(""), "fiscalYear", r.FiscalYear.Or(0), "current", shorten(lastText, n.Length),
			"to", shorten(r.To, n.Length), "percent", usedPercent(r.From, r.To, lastText))
	}
	return first, lastText, nil
}

// bufferedNumber gives a number from the block of the kernel for an
// interval and reserves a new block in a transaction of its own when the
// block is used up or the intervals changed since.
func (k *Kernel) bufferedNumber(ctx context.Context, tenant string, s *model.Schema, n *model.NumberRange, id string, sub model.Optional[string], year model.Optional[int64]) (string, error) {
	if err := checkSubobjectAndYear(s, n, sub, year); err != nil {
		return "", err
	}
	gen, err := k.repo.NumberRangeGeneration(ctx, k.db)
	if err != nil {
		return "", err
	}
	key := strings.Join([]string{tenant, n.Name, id, sub.Or("\x00"), strconv.FormatInt(year.Or(0), 10)}, "\x01")
	k.numbers.mu.Lock()
	defer k.numbers.mu.Unlock()
	if k.numbers.blocks == nil {
		k.numbers.blocks = map[string]*numberBlock{}
	}
	b := k.numbers.blocks[key]
	if b == nil || b.generation != gen || b.next > b.last {
		b = nil
		err := k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
			r, err := k.interval(ctx, tx, s, tenant, n, id, sub, year)
			if err != nil {
				return err
			}
			first, last, err := k.reserve(ctx, tx, tenant, n, r, n.BlockSize)
			if err != nil {
				return err
			}
			b = &numberBlock{next: first, last: last, generation: gen}
			return nil
		})
		if err != nil {
			delete(k.numbers.blocks, key)
			return "", err
		}
		k.numbers.blocks[key] = b
	}
	number := b.next
	next := numberValue(b.next)
	b.next = numberText(next.Add(next, big.NewInt(1)))
	return shorten(number, n.Length), nil
}

// checkExternal checks a number given from outside against an external
// interval.
func (x *execution) checkExternal(ctx context.Context, reader model.PackageID, object, id string, sub model.Optional[string], year model.Optional[int64], number string) (bool, error) {
	if x.tenant == "" {
		return false, numberError(rterr.NumberRangeArgument, object, "numbers need a tenant")
	}
	n, err := x.visibleNumberRange(reader, object)
	if err != nil {
		return false, err
	}
	s := x.sessions[0]
	if err := s.begin(ctx); err != nil {
		return false, x.classify(err)
	}
	var ok bool
	err = s.guard(ctx, func() error {
		r, err := x.k.interval(ctx, s.conn, x.schema, x.tenant, n, id, sub, year)
		if err != nil {
			return err
		}
		if !r.External {
			return numberError(rterr.NumberRangeArgument, n.Name, "interval %s is internal; the kernel gives its numbers", r.ID)
		}
		if !isDigits(number) || len(number) != n.Length {
			return nil
		}
		p := padNumber(number)
		ok = p >= r.From && p <= r.To
		return nil
	})
	return ok, err
}

// syncNumberRanges removes the intervals of number range objects that are
// gone or whose change makes their intervals invalid, in all tenants, and
// records it in the history.
func (k *Kernel) syncNumberRanges(ctx context.Context, q dialect.Querier, cur, want *model.Schema, at time.Time) error {
	if !k.repo.HasNumberRanges() {
		return nil
	}
	var gone []*model.NumberRange
	for _, name := range cur.NumberRangeNames() {
		c := cur.NumberRanges[name]
		w, ok := want.NumberRanges[name]
		if !ok {
			gone = append(gone, c)
			continue
		}
		if _, destructive := diff.NumberRangeChange(cur, want, c, w); destructive {
			gone = append(gone, c)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	for _, n := range gone {
		all, err := k.repo.NumberIntervalsOf(ctx, q, n.Name)
		if err != nil {
			return err
		}
		for _, r := range all {
			if err := k.repo.DeleteNumberInterval(ctx, q, r.Tenant, r.UUID); err != nil {
				return err
			}
			data, _ := json.Marshal(external(n, r))
			if err := k.repo.InsertNumberRangeChange(ctx, q, repo.NumberRangeChange{ChangedAt: at, ChangedBy: "kernel", Kind: "removed",
				Tenant: r.Tenant, Object: n.Name, Interval: r.ID, Subobject: r.Subobject, FiscalYear: r.FiscalYear,
				OldState: model.Some(string(data)), Confirmed: r.Current.Valid()}); err != nil {
				return err
			}
		}
	}
	return k.repo.BumpNumberRangeGeneration(ctx, q, at)
}
