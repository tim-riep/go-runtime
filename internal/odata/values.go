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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Format is the format of a response in JSON.
type Format struct {
	// IEEE754 writes Edm.Int64 and Edm.Decimal as strings.
	IEEE754 bool
	// NoMetadata leaves out @odata.context, @odata.etag and the other
	// annotations of control.
	NoMetadata bool
}

// ContentType returns the media type of responses in the format.
func (f Format) ContentType() string {
	ct := "application/json;odata.metadata=minimal"
	if f.NoMetadata {
		ct = "application/json;odata.metadata=none"
	}
	if f.IEEE754 {
		ct += ";IEEE754Compatible=true"
	}
	return ct
}

// JSONValue returns the value of a field as JSON: numbers of Edm.Int64 and
// Edm.Decimal with their exact text, as strings for IEEE754.
func (f Format) JSONValue(d *model.Domain, v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case int64:
		if d.Type == model.Int8 && f.IEEE754 {
			return strconv.FormatInt(x, 10)
		}
		return x
	case string:
		if d.Type == model.Dec {
			if f.IEEE754 {
				return x
			}
			return json.Number(x)
		}
		return x
	case time.Time:
		if d.Type == model.Date {
			return x.Format(model.DateLayout)
		}
		return x.UTC().Format("2006-01-02T15:04:05.999999Z07:00")
	case []byte:
		return base64.URLEncoding.EncodeToString(x)
	}
	return v
}

// ETag returns the weak ETag of an entity from the value of its ETag
// field, or "" without one.
func ETag(d *model.Domain, v any) string {
	text := canonicalText(d, v)
	if text == "" {
		return ""
	}
	return `W/"` + base64.RawURLEncoding.EncodeToString([]byte(text)) + `"`
}

// canonicalText returns the canonical text of a value of the database.
func canonicalText(d *model.Domain, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case time.Time:
		if d.Type == model.Date {
			return x.Format(model.DateLayout)
		}
		return x.UTC().Format(model.TimestampLayout)
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	}
	return fmt.Sprint(v)
}

// KeyText returns the key predicate of an entity, such as ('C1') or
// (A='x',B=1), for URLs.
func KeyText(t *EntityType, row map[string]any) string {
	var parts []string
	for _, k := range t.Key {
		lit := literalText(k.Domain, row[k.Field.Name])
		if len(t.Key) == 1 {
			return "(" + lit + ")"
		}
		parts = append(parts, k.Name+"="+lit)
	}
	return "(" + strings.Join(parts, ",") + ")"
}

// literalText writes a value as literal of a URL.
func literalText(d *model.Domain, v any) string {
	text := canonicalText(d, v)
	switch d.Type {
	case model.Char, model.String:
		return "'" + strings.ReplaceAll(text, "'", "''") + "'"
	}
	return text
}

// Entity writes the properties of an entity in their order, those of sel
// only if sel is not nil, with its ETag unless the format has no metadata.
func (f Format) Entity(t *EntityType, row map[string]any, sel []*Property) *Object {
	o := &Object{}
	if !f.NoMetadata && t.ETag != nil {
		if tag := ETag(t.ETag.Domain, row[t.ETag.Field.Name]); tag != "" {
			o.Set("@odata.etag", tag)
		}
	}
	for _, p := range t.Properties {
		if sel != nil && !containsProperty(sel, p) {
			continue
		}
		o.Set(p.Name, f.JSONValue(p.Domain, row[p.Field.Name]))
	}
	return o
}

func containsProperty(ps []*Property, p *Property) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}

// SelectText returns the list of selected properties for context URLs.
func SelectText(sel []*Property) string {
	names := make([]string, len(sel))
	for i, p := range sel {
		names[i] = p.Name
	}
	return strings.Join(names, ",")
}

// CanonicalText returns a value of the database as text that identifies
// it, for grouping rows by fields.
func CanonicalText(v any) string {
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	}
	return fmt.Sprint(v)
}
