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
	"fmt"
	"strconv"
)

// Optional holds a value or no value. The kernel never represents "no
// value" by a zero value: Some("") is the empty string, None is null.
type Optional[T any] struct {
	value T
	valid bool
}

// Some returns an Optional holding v.
func Some[T any](v T) Optional[T] { return Optional[T]{value: v, valid: true} }

// None returns an Optional without value.
func None[T any]() Optional[T] { return Optional[T]{} }

// Get returns the value and whether there is one.
func (o Optional[T]) Get() (T, bool) { return o.value, o.valid }

// Valid reports whether there is a value.
func (o Optional[T]) Valid() bool { return o.valid }

// Or returns the value, or def if there is none.
func (o Optional[T]) Or(def T) T {
	if o.valid {
		return o.value
	}
	return def
}

// MarshalJSON writes null for None and the value otherwise.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.valid {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON reads null as None and every other value as Some.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*o = None[T]()
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*o = Some(v)
	return nil
}

// String renders the value for display: <null> for None, strings quoted,
// so null and "" stay distinguishable.
func (o Optional[T]) String() string {
	if !o.valid {
		return "<null>"
	}
	if s, ok := any(o.value).(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(o.value)
}

// DefaultKind distinguishes the three kinds of defaults.
type DefaultKind uint8

// Default kinds.
const (
	DefaultNone  DefaultKind = iota // no default declared
	DefaultNull                     // default null, declared explicitly
	DefaultValue                    // default value, possibly ""
)

func (k DefaultKind) String() string {
	switch k {
	case DefaultNull:
		return "NULL"
	case DefaultValue:
		return "VALUE"
	}
	return "NONE"
}

// Default is the default of a domain or field.
type Default struct {
	Kind  DefaultKind
	Value string // canonical text form; set only for DefaultValue
}

// NoDefault returns the absent default.
func NoDefault() Default { return Default{} }

// NullDefault returns the explicit default null.
func NullDefault() Default { return Default{Kind: DefaultNull} }

// ValueDefault returns a default value in canonical text form.
func ValueDefault(v string) Default { return Default{Kind: DefaultValue, Value: v} }

// IsZero reports whether no default is declared, so `json:",omitzero"`
// leaves the default out instead of writing null.
func (d Default) IsZero() bool { return d.Kind == DefaultNone }

// MarshalJSON writes null for DefaultNull and the value for DefaultValue.
// DefaultNone is meant to be left out with omitzero; it is written as an
// empty object if it is not.
func (d Default) MarshalJSON() ([]byte, error) {
	switch d.Kind {
	case DefaultNull:
		return []byte("null"), nil
	case DefaultValue:
		return json.Marshal(d.Value)
	}
	return []byte("{}"), nil
}

// UnmarshalJSON reads null as DefaultNull and a string as DefaultValue.
func (d *Default) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*d = NullDefault()
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("{}")) {
		*d = NoDefault()
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*d = ValueDefault(s)
	return nil
}

// String renders the default for display.
func (d Default) String() string {
	switch d.Kind {
	case DefaultNull:
		return "<null>"
	case DefaultValue:
		return strconv.Quote(d.Value)
	}
	return ""
}
