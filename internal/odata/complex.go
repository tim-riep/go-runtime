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
	"encoding/json"
	"net/http"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
)

// domainOf returns the domain of an elementary component.
func domainOf(c structure.Component) *model.Domain {
	return &model.Domain{Type: c.Type, Length: c.Length, Decimals: c.Decimals}
}

// DecodeComplex reads the parameters of an action: the components of a
// structure by their public names, in the form of the transaction buffer.
// Unknown members and wrong values answer 400.
func DecodeComplex(def *structure.Definition, keys []string, vals map[string]json.RawMessage, prefix string) (map[string]any, *Error) {
	out := map[string]any{}
	byName := map[string]structure.Component{}
	for _, c := range def.Components {
		byName[model.PublicName(c.Name)] = c
		out[c.Name] = nil
	}
	for _, k := range keys {
		c, ok := byName[k]
		if !ok {
			return nil, Errorf(http.StatusBadRequest, CodeUnknownProperty, model.PublicName(def.Name), k).At(prefix + k)
		}
		v, e := decodeComponent(c, vals[k], prefix+k)
		if e != nil {
			return nil, e
		}
		out[c.Name] = v
	}
	for _, c := range def.Components {
		if c.NotNull && out[c.Name] == nil {
			name := model.PublicName(c.Name)
			return nil, Errorf(http.StatusBadRequest, CodeInvalidValue, name, "it cannot be null").At(prefix + name)
		}
	}
	return out, nil
}

func decodeComponent(c structure.Component, raw json.RawMessage, target string) (any, *Error) {
	switch {
	case c.Structure != nil:
		if string(raw) == "null" {
			return nil, nil
		}
		keys, vals, e := DecodeObject(raw)
		if e != nil {
			return nil, e.At(target)
		}
		return DecodeComplex(c.Structure, keys, vals, target+"/")
	case c.ArrayType != nil:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidValue, target, "expected an array").At(target)
		}
		list := []any{}
		for _, it := range items {
			v, e := decodeComponent(c.ArrayType.Array.Row, it, target)
			if e != nil {
				return nil, e
			}
			list = append(list, v)
		}
		return list, nil
	}
	v, err := DecodeValue(domainOf(c), raw)
	if err != nil {
		return nil, Errorf(http.StatusBadRequest, CodeInvalidValue, target, err.Error()).At(target)
	}
	return v, nil
}

// Complex writes a value of a structure with the public names of its
// components.
func (f Format) Complex(def *structure.Definition, v map[string]any) *Object {
	o := &Object{}
	for _, c := range def.Components {
		o.Set(model.PublicName(c.Name), f.component(c, v[c.Name]))
	}
	return o
}

func (f Format) component(c structure.Component, v any) any {
	switch {
	case v == nil:
		return nil
	case c.Structure != nil:
		m, _ := v.(map[string]any)
		return f.Complex(c.Structure, m)
	case c.ArrayType != nil:
		list := []any{}
		items, _ := v.([]any)
		for _, it := range items {
			list = append(list, f.component(c.ArrayType.Array.Row, it))
		}
		return list
	}
	return f.JSONValue(domainOf(c), v)
}
