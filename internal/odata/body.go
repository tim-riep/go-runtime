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

package odata

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Kinds of changes of entities.
const (
	ChangeCreate  = "create"
	ChangePatch   = "patch"
	ChangeReplace = "replace"
)

// EntityBody is an entity read from the body of a request: its fields by
// technical name in the form of the transaction buffer, and for deep
// inserts the dependent entities by navigation.
type EntityBody struct {
	Type   *EntityType
	Fields map[string]any
	// Order lists the fields in the order of the body.
	Order    []string
	Children []ChildBodies
}

// ChildBodies are the dependent entities of a navigation in a deep insert.
type ChildBodies struct {
	Nav      *Navigation
	Entities []*EntityBody
}

// DecodeObject reads a JSON object and keeps the order of its members and
// the exact text of numbers.
func DecodeObject(body []byte) ([]string, map[string]json.RawMessage, *Error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the body is no JSON object")
	}
	var keys []string
	vals := map[string]json.RawMessage{}
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return nil, nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the body is no JSON object")
		}
		name, _ := k.(string)
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the body is no JSON object")
		}
		if _, dup := vals[name]; dup {
			return nil, nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, name+" appears twice in the body")
		}
		keys = append(keys, name)
		vals[name] = raw
	}
	if _, err := d.Token(); err != nil {
		return nil, nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the body is no JSON object")
	}
	return keys, vals, nil
}

// DecodeEntity reads an entity for a change. Unknown properties,
// properties the change may not set and values of the wrong type are
// rejected with 400 and the property as target, never ignored. Deep
// inserts follow the navigations to dependent entities.
func DecodeEntity(t *EntityType, body []byte, kind string) (*EntityBody, *Error) {
	keys, vals, e := DecodeObject(body)
	if e != nil {
		return nil, e
	}
	return decodeEntity(t, keys, vals, kind, "")
}

func decodeEntity(t *EntityType, keys []string, vals map[string]json.RawMessage, kind, prefix string) (*EntityBody, *Error) {
	out := &EntityBody{Type: t, Fields: map[string]any{}}
	for _, name := range keys {
		raw := vals[name]
		target := prefix + name
		if strings.Contains(name, "@") {
			if strings.HasSuffix(name, "@odata.bind") {
				return nil, notImplemented("@odata.bind").At(target)
			}
			continue // annotations such as @odata.etag or @odata.type
		}
		if nav := t.Navigation(name); nav != nil {
			if kind != ChangeCreate || !nav.Collection || len(nav.Reverse) == 0 {
				if kind == ChangeCreate {
					return nil, notImplemented("deep inserts along "+name).At(target)
				}
				return nil, Errorf(http.StatusBadRequest, CodeReadOnlyProperty, name).At(target)
			}
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, Errorf(http.StatusBadRequest, CodeInvalidValue, name, "expected an array of entities").At(target)
			}
			cb := ChildBodies{Nav: nav}
			for i, it := range items {
				ck, cv, e := DecodeObject(it)
				if e != nil {
					return nil, e.At(target)
				}
				child, e := decodeEntity(nav.Type, ck, cv, kind, target+"("+strconv.Itoa(i+1)+")/")
				if e != nil {
					return nil, e
				}
				for _, c := range nav.Reverse {
					if _, given := child.Fields[c.Field]; given {
						p := nav.Type.FieldProperty(c.Field)
						return nil, Errorf(http.StatusBadRequest, CodeReadOnlyProperty, p.Name).At(target + "(" + strconv.Itoa(i+1) + ")/" + p.Name)
					}
				}
				cb.Entities = append(cb.Entities, child)
			}
			out.Children = append(out.Children, cb)
			continue
		}
		p := t.Property(name)
		if p == nil {
			return nil, Errorf(http.StatusBadRequest, CodeUnknownProperty, t.Name, name).At(target)
		}
		if p.Computed || kind != ChangeCreate && (p.Immutable || p.Key) {
			return nil, Errorf(http.StatusBadRequest, CodeReadOnlyProperty, name).At(target)
		}
		v, e := DecodeValue(p.Domain, raw)
		if e != nil {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidValue, name, e.Error()).At(target)
		}
		if v == nil && !p.Nullable {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidValue, name, "it cannot be null").At(target)
		}
		out.Fields[p.Field.Name] = v
		out.Order = append(out.Order, p.Field.Name)
	}
	return out, nil
}

type valueError string

func (e valueError) Error() string { return string(e) }

// DecodeValue reads a value of a domain from JSON into the form of the
// transaction buffer: text for CHAR, STRING, DEC and UUID, int64, bool,
// time.Time and bytes. Int64 and decimals come as number or string and
// never pass through floating point.
func DecodeValue(d *model.Domain, raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	str := func() (string, error) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", valueError("expected a string")
		}
		return s, nil
	}
	number := func() (string, error) {
		if len(raw) > 0 && raw[0] == '"' {
			return str()
		}
		var n json.Number
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&n); err != nil {
			return "", valueError("expected a number")
		}
		return n.String(), nil
	}
	canonical := func(s string) (string, error) {
		c, err := d.Canonical(s)
		if err != nil {
			return "", valueError(err.Error())
		}
		return c, nil
	}
	switch d.Type {
	case model.Char, model.String:
		s, err := str()
		if err != nil {
			return nil, err
		}
		return canonical(s)
	case model.Int4, model.Int8:
		s, err := number()
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(s, ".eE") {
			return nil, valueError("expected an integer")
		}
		c, err := canonical(s)
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(c, 10, 64)
	case model.Dec:
		s, err := number()
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(s, "eE") {
			return nil, valueError("expected a decimal without exponent")
		}
		return canonical(s)
	case model.Bool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, valueError("expected true or false")
		}
		return b, nil
	case model.Date:
		s, err := str()
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(model.DateLayout, s)
		if err != nil {
			return nil, valueError("expected a date YYYY-MM-DD")
		}
		return t, nil
	case model.Timestamp:
		s, err := str()
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, valueError("expected a timestamp with time zone")
		}
		return t.UTC(), nil
	case model.UUID:
		s, err := str()
		if err != nil {
			return nil, err
		}
		if !guidPattern.MatchString(s) || len(s) != 36 {
			return nil, valueError("expected a GUID")
		}
		return strings.ToLower(s), nil
	case model.Blob:
		s, err := str()
		if err != nil {
			return nil, err
		}
		for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.RawURLEncoding, base64.StdEncoding} {
			if b, err := enc.DecodeString(s); err == nil {
				return b, nil
			}
		}
		return nil, valueError("expected base64url")
	}
	return nil, valueError("unsupported type")
}
