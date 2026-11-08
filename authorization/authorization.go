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

// Package authorization resolves the effective authorizations of a user in
// a tenant on a day: the roles assigned to the user that are valid on the
// day, composite roles resolved, as grants per authorization object with
// their origin. Grants are compiled for fast checks: a check names a value
// for every field of an object and is allowed when one grant matches all
// of them.
//
// The types are stable within one FormatVersion.
package authorization

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// FormatVersion is the version of the structure of Effective.
const FormatVersion = 1

// Assignment is a role assigned to the user in the tenant.
type Assignment struct {
	ID        string
	Role      string
	ValidFrom model.Optional[time.Time] // first day, None for open
	ValidTo   model.Optional[time.Time] // last day, None for open
}

// ValidOn reports whether the assignment is valid on a day.
func (a Assignment) ValidOn(day time.Time) bool {
	if f, ok := a.ValidFrom.Get(); ok && day.Before(f) {
		return false
	}
	if t, ok := a.ValidTo.Get(); ok && day.After(t) {
		return false
	}
	return true
}

// Effective are the effective authorizations of a user in a tenant on a
// day.
type Effective struct {
	FormatVersion int       `json:"formatVersion"`
	User          string    `json:"user"`
	Tenant        string    `json:"tenant"`
	Date          time.Time `json:"date"`
	// Generation is the state of roles and assignments the result was
	// computed from.
	Generation int64 `json:"generation"`
	// Locked users have no effective authorizations.
	Locked bool `json:"locked"`
	// All tells that the role with all authorizations is assigned: every
	// authorization object with all values, also objects installed later.
	All        bool     `json:"all"`
	AllOrigins []Origin `json:"allOrigins,omitempty"`
	// Objects are the grants by authorization object.
	Objects map[string]*ObjectGrants `json:"objects"`
	// NotEffective are the authorizations of assigned roles that do not
	// fit their authorization objects and grant nothing.
	NotEffective []NotEffective `json:"notEffective"`
}

// ObjectGrants are the grants on one authorization object; one matching
// grant allows a check.
type ObjectGrants struct {
	Object string  `json:"object"`
	Grants []Grant `json:"grants"`
}

// Grant is an authorization of a role: a matcher for every field of the
// object, all of which must match.
type Grant struct {
	Fields map[string]*Matcher `json:"fields"`
	Origin Origin              `json:"origin"`
}

// Origin tells where a grant comes from.
type Origin struct {
	Role       string                    `json:"role"`
	Composite  model.Optional[string]    `json:"composite"` // the composite role, if the role is a member
	Assignment string                    `json:"assignment"`
	ValidFrom  model.Optional[time.Time] `json:"validFrom"`
	ValidTo    model.Optional[time.Time] `json:"validTo"`
}

// NotEffective is an authorization of an assigned role that grants
// nothing.
type NotEffective struct {
	Object   string                       `json:"object"`
	Origin   Origin                       `json:"origin"`
	Problems []model.AuthorizationProblem `json:"problems"`
}

// Matcher tests the values of one field: all values, single values,
// ranges and prefixes, any of which may match.
type Matcher struct {
	Any      bool
	Values   map[string]struct{}
	Ranges   []Range
	Prefixes []string
	integer  bool
}

// Range is a range of values, both bounds included.
type Range struct {
	Low, High       string
	lowInt, highInt int64
}

// Match reports whether a value in canonical text form matches.
func (m *Matcher) Match(v string) bool {
	if m.Any {
		return true
	}
	if _, ok := m.Values[v]; ok {
		return true
	}
	for _, p := range m.Prefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	if len(m.Ranges) == 0 {
		return false
	}
	if m.integer {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return false
		}
		for _, r := range m.Ranges {
			if n >= r.lowInt && n <= r.highInt {
				return true
			}
		}
		return false
	}
	for _, r := range m.Ranges {
		if v >= r.Low && v <= r.High {
			return true
		}
	}
	return false
}

// MarshalJSON writes the matcher as its entries.
func (m *Matcher) MarshalJSON() ([]byte, error) {
	var out []model.ValueEntry
	if m.Any {
		out = append(out, model.ValueEntry{Kind: model.ValuePattern, Low: model.AllValues})
	}
	for _, v := range slices.Sorted(maps.Keys(m.Values)) {
		out = append(out, model.ValueEntry{Kind: model.ValueSingle, Low: v})
	}
	for _, r := range m.Ranges {
		out = append(out, model.ValueEntry{Kind: model.ValueRange, Low: r.Low, High: r.High})
	}
	for _, p := range m.Prefixes {
		out = append(out, model.ValueEntry{Kind: model.ValuePattern, Low: p + "*"})
	}
	return json.Marshal(out)
}

// Allows reports whether one grant on the object matches the values of
// all its fields. A field without a value matches nothing.
func (e *Effective) Allows(object string, values map[string]string) bool {
	if e.Locked {
		return false
	}
	if e.All {
		return true
	}
	og := e.Objects[object]
	if og == nil {
		return false
	}
	for _, g := range og.Grants {
		ok := true
		for field, m := range g.Fields {
			v, given := values[field]
			if !given || !m.Match(v) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Resolve computes the effective authorizations from the schema and the
// assignments of the user in the tenant; locked users have none.
func Resolve(s *model.Schema, user, tenant string, date time.Time, locked bool, assignments []Assignment) *Effective {
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	e := &Effective{FormatVersion: FormatVersion, User: user, Tenant: tenant, Date: day, Locked: locked, Objects: map[string]*ObjectGrants{},
		NotEffective: []NotEffective{}}
	if locked {
		return e
	}
	for _, a := range assignments {
		if !a.ValidOn(day) {
			continue
		}
		origin := Origin{Role: a.Role, Assignment: a.ID, ValidFrom: a.ValidFrom, ValidTo: a.ValidTo}
		if a.Role == model.AllAuthorizationsRole {
			e.All = true
			e.AllOrigins = append(e.AllOrigins, origin)
			continue
		}
		role := s.Roles[a.Role]
		if role == nil {
			continue
		}
		if !role.Composite {
			e.add(s, role, origin)
			continue
		}
		for _, m := range role.Roles {
			if mr := s.Roles[m]; mr != nil && !mr.Composite {
				o := origin
				o.Role, o.Composite = m, model.Some(a.Role)
				e.add(s, mr, o)
			}
		}
	}
	return e
}

func (e *Effective) add(s *model.Schema, role *model.Role, origin Origin) {
	for _, a := range role.Authorizations {
		if problems := model.CheckAuthorization(s, a); len(problems) > 0 {
			e.NotEffective = append(e.NotEffective, NotEffective{Object: a.Object, Origin: origin, Problems: problems})
			continue
		}
		o := s.AuthorizationObjects[a.Object]
		g := Grant{Fields: map[string]*Matcher{}, Origin: origin}
		for _, f := range o.Fields {
			d := s.Domains[s.AuthorizationFieldDomain(f)]
			g.Fields[f.Name] = compile(o, f, d, a.Fields[f.Name])
		}
		og := e.Objects[a.Object]
		if og == nil {
			og = &ObjectGrants{Object: a.Object}
			e.Objects[a.Object] = og
		}
		og.Grants = append(og.Grants, g)
	}
}

// compile turns the entries of a field into a matcher. * on ACTIVITY
// becomes the activities the object allows now.
func compile(o *model.AuthorizationObject, f model.AuthorizationField, d *model.Domain, entries []model.ValueEntry) *Matcher {
	m := &Matcher{Values: map[string]struct{}{}, integer: d.Type == model.Int4 || d.Type == model.Int8}
	canonical := func(v string) string {
		if c, err := d.Canonical(v); err == nil {
			return c
		}
		return v
	}
	for _, e := range entries {
		switch e.Kind {
		case model.ValuePattern:
			switch {
			case e.Low != model.AllValues:
				m.Prefixes = append(m.Prefixes, strings.TrimSuffix(e.Low, "*"))
			case f.IsActivity():
				for _, act := range o.Activities {
					m.Values[act] = struct{}{}
				}
			default:
				m.Any = true
			}
		case model.ValueRange:
			r := Range{Low: canonical(e.Low), High: canonical(e.High)}
			if m.integer {
				r.lowInt, _ = strconv.ParseInt(r.Low, 10, 64)
				r.highInt, _ = strconv.ParseInt(r.High, 10, 64)
			}
			m.Ranges = append(m.Ranges, r)
		default:
			m.Values[canonical(e.Low)] = struct{}{}
		}
	}
	return m
}
