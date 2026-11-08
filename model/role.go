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

package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/rterr"
)

// NotAuthorizedClass is the runtime error of a failed authorization
// check.
const NotAuthorizedClass = KernelNamespace + "NOT_AUTHORIZED"

// AllAuthorizationsRole is the role of the kernel that grants every
// authorization object with all values, also objects installed later. It
// is not stored, cannot change and is no member of composite roles.
const AllAuthorizationsRole = KernelNamespace + "ALL_AUTHORIZATIONS"

// Role bundles authorizations on authorization objects, or with Composite
// other roles. Package roles come with a package (schema/roles/*.json);
// local roles have the owner Local and are maintained at runtime.
type Role struct {
	Name      string           `json:"name"`
	Owner     PackageID        `json:"owner"`
	File      string           `json:"file,omitempty"`
	Texts     map[string]Label `json:"texts"`
	Composite bool             `json:"composite"`
	// Authorizations are those of a single role, in order.
	Authorizations []Authorization `json:"authorizations,omitempty"`
	// Roles are the members of a composite role, in order.
	Roles []string       `json:"roles,omitempty"`
	Src   rterr.Location `json:"-"`
}

// Languages returns the languages of the texts in sorted order.
func (r *Role) Languages() []string { return slices.Sorted(maps.Keys(r.Texts)) }

// Authorization grants values for the fields of an authorization object.
// The fields of an authorization combine with and, the entries of a field
// with or.
type Authorization struct {
	Object string                  `json:"object"`
	Fields map[string][]ValueEntry `json:"fields"`
}

// FieldNames returns the names of the fields in sorted order.
func (a Authorization) FieldNames() []string { return slices.Sorted(maps.Keys(a.Fields)) }

// ValueKind is the kind of an entry of granted values.
type ValueKind string

// Kinds of entries.
const (
	ValueSingle  ValueKind = "value"   // one value
	ValueRange   ValueKind = "range"   // Low to High, both included
	ValuePattern ValueKind = "pattern" // * for all values, or a prefix and *
)

// AllValues is the pattern for all values.
const AllValues = "*"

// ValueEntry is a value, a range or a pattern granted for a field. Low and
// High are in the canonical text form of the field's type; a pattern is
// in Low. LowJSON and HighJSON keep the values as written in JSON, to
// check their JSON types; they are empty for entries read back from the
// repository.
type ValueEntry struct {
	Kind     ValueKind       `json:"kind"`
	Low      string          `json:"low"`
	High     string          `json:"high,omitempty"`
	LowJSON  json.RawMessage `json:"-"`
	HighJSON json.RawMessage `json:"-"`
}

// UnmarshalJSON reads {"value": v}, {"from": a, "to": b} or {"pattern":
// p}. null is never a value.
func (e *ValueEntry) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	text := func(key string) (string, json.RawMessage, error) {
		v := raw[key]
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return "", nil, fmt.Errorf("%s is null; null is never granted", key)
		}
		dec := json.NewDecoder(bytes.NewReader(v))
		dec.UseNumber()
		var x any
		if err := dec.Decode(&x); err != nil {
			return "", nil, err
		}
		switch y := x.(type) {
		case string:
			return y, v, nil
		case json.Number:
			return y.String(), v, nil
		case bool:
			return strconv.FormatBool(y), v, nil
		}
		return "", nil, fmt.Errorf("%s must be a string, a number or a boolean", key)
	}
	var err error
	switch _, value := raw["value"]; {
	case value && len(raw) == 1:
		e.Kind = ValueSingle
		e.Low, e.LowJSON, err = text("value")
	case raw["pattern"] != nil && len(raw) == 1:
		e.Kind = ValuePattern
		var p string
		if err := json.Unmarshal(raw["pattern"], &p); err != nil {
			return errors.New("a pattern is a string")
		}
		e.Low = p
	case raw["from"] != nil && raw["to"] != nil && len(raw) == 2:
		e.Kind = ValueRange
		if e.Low, e.LowJSON, err = text("from"); err == nil {
			e.High, e.HighJSON, err = text("to")
		}
	default:
		return errors.New(`an entry is {"value": v}, {"from": a, "to": b} or {"pattern": p}`)
	}
	return err
}

// MarshalJSON writes the entry in the format of role files; values are
// strings, see JSON for typed values.
func (e ValueEntry) MarshalJSON() ([]byte, error) {
	switch e.Kind {
	case ValueRange:
		return json.Marshal(map[string]string{"from": e.Low, "to": e.High})
	case ValuePattern:
		return json.Marshal(map[string]string{"pattern": e.Low})
	}
	return json.Marshal(map[string]string{"value": e.Low})
}

// AuthorizationProblem is a reason why an authorization does not fit its
// authorization object.
type AuthorizationProblem struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (p AuthorizationProblem) String() string {
	if p.Field == "" {
		return p.Message
	}
	return "field " + p.Field + ": " + p.Message
}

// CheckAuthorization returns the problems of an authorization against its
// authorization object in s: every field of the object given and no other,
// values of the field's type and length, fixed values only, ranges and
// prefixes only for CHAR and integers, and for ACTIVITY only single allowed
// activities or *. An authorization with problems grants nothing.
func CheckAuthorization(s *Schema, a Authorization) []AuthorizationProblem {
	o := s.AuthorizationObjects[a.Object]
	if o == nil {
		return []AuthorizationProblem{{Message: "authorization object " + a.Object + " does not exist"}}
	}
	var out []AuthorizationProblem
	for _, f := range o.Fields {
		entries, ok := a.Fields[f.Name]
		if !ok {
			out = append(out, AuthorizationProblem{Field: f.Name, Message: "is missing; write * to grant all values"})
			continue
		}
		if len(entries) == 0 {
			out = append(out, AuthorizationProblem{Field: f.Name, Message: "has no values"})
		}
		d := s.Domains[s.AuthorizationFieldDomain(f)]
		for _, e := range entries {
			if msg := checkEntry(o, f, d, e); msg != "" {
				out = append(out, AuthorizationProblem{Field: f.Name, Message: msg})
			}
		}
	}
	for _, name := range a.FieldNames() {
		if _, ok := o.Field(name); !ok {
			out = append(out, AuthorizationProblem{Field: name, Message: "is no field of " + o.Name})
		}
	}
	return out
}

func checkEntry(o *AuthorizationObject, f AuthorizationField, d *Domain, e ValueEntry) string {
	if !f.IsActivity() {
		o = nil
	}
	return valueEntry(o, d, e)
}

// CheckValueEntry checks an entry of values against the domain of a field
// with the rules of role files: values of the type and length of the
// field, fixed values only, ranges and prefixes only for CHAR and
// integers. It returns "" for a valid entry and the reason otherwise.
func CheckValueEntry(d *Domain, e ValueEntry) string { return valueEntry(nil, d, e) }

// valueEntry checks an entry; o is the authorization object of the field
// ACTIVITY, nil for every other field.
func valueEntry(o *AuthorizationObject, d *Domain, e ValueEntry) string {
	if d == nil {
		return "the type of the field is unknown"
	}
	ordered := d.Type == Char || d.Type == Int4 || d.Type == Int8
	switch e.Kind {
	case ValuePattern:
		p := e.Low
		switch {
		case p == AllValues:
			return ""
		case !strings.HasSuffix(p, "*") || strings.Count(p, "*") != 1:
			return fmt.Sprintf("pattern %q is no pattern; patterns are * or a prefix followed by *", p)
		case o != nil:
			return "ACTIVITY takes single activities or *"
		case !ordered:
			return fmt.Sprintf("patterns other than * need CHAR, INT4 or INT8, not %s", d.TypeString())
		}
		prefix := strings.TrimSuffix(p, "*")
		if d.Type == Char {
			if _, err := d.Canonical(prefix); err != nil {
				return fmt.Sprintf("pattern %q: %v", p, err)
			}
			return ""
		}
		if strings.TrimLeft(prefix, "-0123456789") != "" || strings.Contains(prefix[min(1, len(prefix)):], "-") {
			return fmt.Sprintf("pattern %q is no prefix of an integer", p)
		}
		return ""
	case ValueRange:
		if o != nil {
			return "ACTIVITY takes single activities or *"
		}
		if !ordered {
			return fmt.Sprintf("ranges need CHAR, INT4 or INT8, not %s", d.TypeString())
		}
		lo, msg := entryValue(d, e.Low, e.LowJSON)
		if msg != "" {
			return "from: " + msg
		}
		hi, msg := entryValue(d, e.High, e.HighJSON)
		if msg != "" {
			return "to: " + msg
		}
		if CompareValues(d.Type, lo, hi) > 0 {
			return fmt.Sprintf("range %q to %q is empty; from must not be greater than to", lo, hi)
		}
		return ""
	}
	v, msg := entryValue(d, e.Low, e.LowJSON)
	if msg != "" {
		return msg
	}
	if o != nil && !slices.Contains(o.Activities, v) {
		return fmt.Sprintf("%s is not allowed for %s; it allows %s", v, o.Name, strings.Join(o.Activities, ", "))
	}
	return ""
}

// entryValue checks a value against the domain: JSON type, length or
// range, empty string and fixed values.
func entryValue(d *Domain, text string, raw json.RawMessage) (string, string) {
	var v string
	var err error
	if len(raw) > 0 {
		v, err = d.ParseJSON(raw)
	} else {
		v, err = d.Canonical(text)
	}
	if err != nil {
		return "", err.Error()
	}
	if v == "" && d.Type == Char && !d.AllowsEmpty() {
		return "", "the domain does not allow the empty string"
	}
	if len(d.Values) > 0 && !slices.ContainsFunc(d.Values, func(fv FixedValue) bool { return fv.Value == v }) {
		var vals []string
		for _, fv := range d.Values {
			vals = append(vals, strconv.Quote(fv.Value))
		}
		return "", fmt.Sprintf("%q is no fixed value of %s; they are %s", v, d.Name, strings.Join(vals, ", "))
	}
	return v, ""
}

// CompareValues compares two canonical values of an ordered type: integers
// by number, CHAR by code points.
func CompareValues(t BaseType, a, b string) int {
	if t == Int4 || t == Int8 {
		x, _ := strconv.ParseInt(a, 10, 64)
		y, _ := strconv.ParseInt(b, 10, 64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
}

// RoleNames returns the names of all roles in sorted order.
func (s *Schema) RoleNames() []string { return slices.Sorted(maps.Keys(s.Roles)) }

func cloneRole(r *Role) *Role {
	c := *r
	c.Texts = maps.Clone(r.Texts)
	c.Roles = slices.Clone(r.Roles)
	c.Authorizations = make([]Authorization, len(r.Authorizations))
	for i, a := range r.Authorizations {
		fields := make(map[string][]ValueEntry, len(a.Fields))
		for k, v := range a.Fields {
			fields[k] = slices.Clone(v)
		}
		c.Authorizations[i] = Authorization{Object: a.Object, Fields: fields}
	}
	return &c
}

// CloneRole returns a deep copy of a role.
func CloneRole(r *Role) *Role { return cloneRole(r) }

// DetachLocalRoles removes from the local roles what refers to package id,
// before the package is uninstalled: authorizations on its authorization
// objects and members that are its roles. A composite role without
// members left is removed.
func (s *Schema) DetachLocalRoles(id PackageID) {
	for _, name := range s.RoleNames() {
		r := s.Roles[name]
		if r.Owner != Local {
			continue
		}
		r.Authorizations = slices.DeleteFunc(r.Authorizations, func(a Authorization) bool {
			o := s.AuthorizationObjects[a.Object]
			return o != nil && o.Owner == id
		})
		if !r.Composite {
			continue
		}
		r.Roles = slices.DeleteFunc(r.Roles, func(m string) bool {
			mr := s.Roles[m]
			return mr != nil && mr.Owner == id
		})
		if len(r.Roles) == 0 {
			delete(s.Roles, name)
		}
	}
}
