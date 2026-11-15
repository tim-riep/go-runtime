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
	"errors"
	"fmt"

	"github.com/tim-riep/go-runtime/kernelpkg/natives"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/rterr"
)

// numberException turns errors of number ranges into the exceptions of
// TRBASE.
func numberException(err error) error {
	var ne *rterr.NumberRangeError
	if !errors.As(err, &ne) {
		return err
	}
	class := map[rterr.NumberRangeErrorKind]string{
		rterr.NumberRangeNotFound:     "/TRBASE/NUMBER_RANGE_NOT_FOUND",
		rterr.NumberRangeExhausted:    "/TRBASE/NUMBER_RANGE_EXHAUSTED",
		rterr.NumberRangeArgument:     "/TRBASE/NUMBER_RANGE_ARGUMENT_INVALID",
		rterr.NumberRangeConfirmation: "/TRBASE/NUMBER_RANGE_CONFIRMATION_REQUIRED",
	}[ne.Kind]
	return native.Raise(class, ne.Error())
}

// numberMaintenance gives native methods the maintenance of intervals in
// the tenant of the execution, in the transaction of its default session.
func (x *execution) numberMaintenance(c *native.Context, changes, confirm bool) (*numberMaintenance, func(error) error, error) {
	user, ok := x.opts.User.Get()
	if !ok || x.tenant == "" {
		return nil, nil, &native.NotAuthorized{Text: "the maintenance of number ranges needs a signed-in user and a tenant"}
	}
	if changes && x.opts.ReadOnly {
		return nil, nil, errReadOnly
	}
	if !x.k.repo.HasNumberRanges() {
		return nil, nil, fmt.Errorf("the kernel tables of this database keep no number ranges")
	}
	s := x.sessions[0]
	if changes && s.sqlite() {
		for _, o := range x.sessions[1:] {
			if o.written {
				return nil, nil, fmt.Errorf("SQLite allows only one writing transaction; commit or roll back session %d first", o.n)
			}
		}
	}
	if err := s.begin(c); err != nil {
		return nil, nil, x.classify(err)
	}
	if _, err := s.conn.ExecContext(c, "SAVEPOINT rt_numbers"); err != nil {
		return nil, nil, x.classify(err)
	}
	end := func(err error) error {
		if err != nil {
			if _, rerr := s.conn.ExecContext(c, "ROLLBACK TO SAVEPOINT rt_numbers"); rerr != nil {
				return errors.Join(err, rerr)
			}
		} else if changes {
			s.written = true
		}
		if _, rerr := s.conn.ExecContext(c, "RELEASE SAVEPOINT rt_numbers"); rerr != nil {
			return errors.Join(err, rerr)
		}
		return numberException(err)
	}
	m := &numberMaintenance{k: x.k, q: s.conn, s: x.schema, tenant: x.tenant, by: user, confirm: confirm,
		authorize: func(object, activity string) error {
			return c.Authorize(NumberRangeObject, map[string]string{"NUMBER_RANGE_OBJECT": object, model.ActivityField: activity})
		}}
	if changes {
		if err := x.k.repo.BumpNumberRangeGeneration(c, s.conn, x.k.now()); err != nil {
			return nil, nil, end(err)
		}
	}
	return m, end, nil
}

func optYear(y model.Optional[int32]) model.Optional[int64] {
	if v, ok := y.Get(); ok {
		return model.Some(int64(v))
	}
	return model.None[int64]()
}

func intervalOfNative(iv natives.NumberInterval) NumberInterval {
	return NumberInterval{Object: iv.RangeName, ID: iv.IntervalID, Subobject: iv.Subobject, FiscalYear: optYear(iv.FiscalYear),
		From: iv.FromNo, To: iv.ToNo, Current: iv.CurrentNo, External: iv.External}
}

func nativeInterval(iv NumberInterval) natives.NumberInterval {
	out := natives.NumberInterval{RangeName: iv.Object, IntervalID: iv.ID, Subobject: iv.Subobject, FromNo: iv.From, ToNo: iv.To,
		CurrentNo: iv.Current, External: iv.External, UsedPercent: model.Some(int32(iv.UsedPercent)), ChangedAt: model.Some(iv.ChangedAt),
		ChangedBy: model.Some(iv.ChangedBy)}
	if y, ok := iv.FiscalYear.Get(); ok {
		out.FiscalYear = model.Some(int32(y))
	}
	return out
}

// The natives of TRBASE for number ranges.
func init() {
	const pkg = "TRBASE"
	static := func(class, method, sig string, fn any) {
		native.Default.Register(native.Method{Package: pkg, Class: class, Method: method, Static: true, Signature: sig, Func: fn})
	}
	const numbers = "/TRBASE/NUMBER_RANGE"
	const admin = "/TRBASE/NUMBER_RANGE_ADMINISTRATION"
	const raises = " raises /TRBASE/NUMBER_RANGE_ERROR"

	next := func(c *native.Context, session int, object, interval string, sub model.Optional[string], year model.Optional[int32]) (string, error) {
		x, err := executionOf(c)
		if err != nil {
			return "", err
		}
		if session < 1 || session > len(x.sessions) {
			return "", fmt.Errorf("the execution has no session %d", session)
		}
		n, err := x.nextNumber(c, x.sessions[session-1], c.Reader(), object, interval, sub, optYear(year))
		return n, numberException(err)
	}
	static(numbers, "NEXT", "(object: STRING, interval: STRING, subobject: STRING?, year: INT4?) returns STRING"+raises,
		func(c *native.Context, object, interval string, sub model.Optional[string], year model.Optional[int32]) (string, error) {
			return next(c, 1, object, interval, sub, year)
		})
	static(numbers, "NEXT_IN", "(target: SESSION, object: STRING, interval: STRING, subobject: STRING?, year: INT4?) returns STRING"+raises,
		func(c *native.Context, target native.Session, object, interval string, sub model.Optional[string], year model.Optional[int32]) (string, error) {
			return next(c, int(target), object, interval, sub, year)
		})
	static(numbers, "CHECK_EXTERNAL", "(object: STRING, interval: STRING, subobject: STRING?, year: INT4?, number: STRING) returns BOOL"+raises,
		func(c *native.Context, object, interval string, sub model.Optional[string], year model.Optional[int32], number string) (bool, error) {
			x, err := executionOf(c)
			if err != nil {
				return false, err
			}
			ok, err := x.checkExternal(c, c.Reader(), object, interval, sub, optYear(year), number)
			return ok, numberException(err)
		})

	// call runs fn with the maintenance of the execution.
	call := func(c *native.Context, changes, confirm bool, fn func(ctx context.Context, m *numberMaintenance) error) error {
		x, err := executionOf(c)
		if err != nil {
			return err
		}
		m, end, err := x.numberMaintenance(c, changes, confirm)
		if err != nil {
			return err
		}
		return end(fn(c, m))
	}
	static(admin, "OBJECTS", "(pkg: STRING?) returns /TRBASE/NUMBER_RANGE_INFO_ARRAY", func(c *native.Context, p model.Optional[string]) ([]natives.NumberRangeInfo, error) {
		out := []natives.NumberRangeInfo{}
		err := call(c, false, false, func(_ context.Context, m *numberMaintenance) error {
			for _, name := range m.s.NumberRangeNames() {
				n := m.s.NumberRanges[name]
				if id, ok := p.Get(); ok && string(n.Owner) != id {
					continue
				}
				if m.authorize(name, "READ") != nil {
					continue
				}
				info := natives.NumberRangeInfo{Name: name, PackageID: string(n.Owner), Length: int32(n.Length), PerFiscalYear: n.PerFiscalYear,
					Buffered: n.Buffering == model.BufferingBuffered, WarningPercent: int32(n.WarningPercent)}
				if l, ok := textIn(n.Texts, c.Host.Language()); ok {
					info.Label = model.Some(l.Label)
				}
				if s, ok := n.Subobject.Get(); ok {
					info.SubobjectName = model.Some(s.Name)
				}
				if n.BlockSize > 0 {
					info.BlockSize = model.Some(int32(n.BlockSize))
				}
				out = append(out, info)
			}
			return nil
		})
		return out, err
	})
	static(admin, "INTERVALS", "(object: STRING) returns /TRBASE/NUMBER_INTERVAL_ARRAY"+raises, func(c *native.Context, object string) ([]natives.NumberInterval, error) {
		out := []natives.NumberInterval{}
		err := call(c, false, false, func(ctx context.Context, m *numberMaintenance) error {
			list, err := m.list(ctx, object)
			for _, iv := range list {
				out = append(out, nativeInterval(iv))
			}
			return err
		})
		return out, err
	})
	static(admin, "ADD_INTERVAL", "(interval: /TRBASE/NUMBER_INTERVAL)"+raises, func(c *native.Context, iv natives.NumberInterval) error {
		return call(c, true, false, func(ctx context.Context, m *numberMaintenance) error { return m.add(ctx, intervalOfNative(iv)) })
	})
	static(admin, "CHANGE_INTERVAL", "(interval: /TRBASE/NUMBER_INTERVAL, confirm: BOOL)"+raises, func(c *native.Context, iv natives.NumberInterval, confirm bool) error {
		return call(c, true, confirm, func(ctx context.Context, m *numberMaintenance) error { return m.change(ctx, intervalOfNative(iv)) })
	})
	static(admin, "DELETE_INTERVAL", "(object: STRING, interval: STRING, subobject: STRING?, year: INT4?, confirm: BOOL)"+raises,
		func(c *native.Context, object, interval string, sub model.Optional[string], year model.Optional[int32], confirm bool) error {
			return call(c, true, confirm, func(ctx context.Context, m *numberMaintenance) error {
				return m.delete(ctx, object, interval, sub, optYear(year))
			})
		})
	static(admin, "HISTORY", "(object: STRING?) returns /TRBASE/NUMBER_RANGE_CHANGE_ARRAY"+raises, func(c *native.Context, object model.Optional[string]) ([]natives.NumberRangeChange, error) {
		out := []natives.NumberRangeChange{}
		err := call(c, false, false, func(ctx context.Context, m *numberMaintenance) error {
			list, err := m.history(ctx, object.Or(""))
			for _, ch := range list {
				nc := natives.NumberRangeChange{ChangedAt: ch.ChangedAt, ChangedBy: ch.ChangedBy, Kind: ch.Kind, RangeName: ch.Object,
					IntervalID: ch.Interval, Subobject: ch.Subobject, OldState: ch.OldState, NewState: ch.NewState, Confirmed: ch.Confirmed}
				if y, ok := ch.FiscalYear.Get(); ok {
					nc.FiscalYear = model.Some(int32(y))
				}
				out = append(out, nc)
			}
			return err
		})
		return out, err
	})
}

// textIn returns the text of a language, else the English one.
func textIn(texts map[string]model.Label, lang string) (model.Label, bool) {
	if l, ok := texts[lang]; ok {
		return l, true
	}
	l, ok := texts[model.DefaultLanguage]
	return l, ok
}
