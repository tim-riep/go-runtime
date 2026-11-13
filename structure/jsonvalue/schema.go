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

package jsonvalue

import (
	"fmt"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
)

// Schema returns a JSON Schema (draft 2020-12) for values of the structure
// def, for editors. Structures and array types become definitions in
// $defs. Labels of data elements in language become descriptions.
func Schema(def *structure.Definition, language string) map[string]any {
	g := &schemaGen{defs: map[string]any{}, lang: language}
	root := g.structure(def)
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$defs"] = g.defs
	return root
}

type schemaGen struct {
	defs map[string]any
	lang string
}

func (g *schemaGen) ref(def *structure.Definition, build func() map[string]any) map[string]any {
	if _, ok := g.defs[def.Name]; !ok {
		g.defs[def.Name] = map[string]any{} // placeholder against repetition
		g.defs[def.Name] = build()
	}
	return map[string]any{"$ref": "#/$defs/" + escape(def.Name)}
}

func (g *schemaGen) structure(def *structure.Definition) map[string]any {
	props := map[string]any{}
	var required []string
	for i := range def.Components {
		c := &def.Components[i]
		props[c.Name] = g.component(c)
		if c.NotNull && c.Default.Kind != model.DefaultValue {
			required = append(required, c.Name)
		}
	}
	out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	if t, ok := def.Texts[g.lang]; ok {
		out["title"] = t.Label
	}
	return out
}

func (g *schemaGen) component(c *structure.Component) map[string]any {
	var s map[string]any
	switch c.Kind {
	case structure.Nested:
		s = g.ref(c.Structure, func() map[string]any { return g.structure(c.Structure) })
	case structure.Array:
		at := c.ArrayType
		s = g.ref(at, func() map[string]any {
			row := at.Array.Row
			return map[string]any{"type": "array", "items": g.component(&row)}
		})
	default:
		s = g.elementary(c)
	}
	if c.DataElement != nil {
		if t, ok := c.DataElement.Texts[g.lang]; ok {
			s = map[string]any{"allOf": []any{s}, "description": t.Medium}
		}
	}
	if !c.NotNull && c.Name != "" {
		return map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
	}
	return s
}

func (g *schemaGen) elementary(c *structure.Component) map[string]any {
	if len(c.FixedValues) > 0 {
		return map[string]any{"enum": names(c.FixedValues)}
	}
	switch c.Type {
	case model.Char:
		s := map[string]any{"type": "string", "maxLength": c.Length}
		if !c.AllowEmpty.Or(true) {
			s["minLength"] = 1
		}
		return s
	case model.String:
		s := map[string]any{"type": "string"}
		if !c.AllowEmpty.Or(true) {
			s["minLength"] = 1
		}
		return s
	case model.Int4, model.Int8:
		return map[string]any{"type": "integer"}
	case model.Bool:
		return map[string]any{"type": "boolean"}
	case model.Dec:
		return map[string]any{"type": "string", "pattern": fmt.Sprintf(`^[+-]?[0-9]+(\.[0-9]{1,%d})?$`, max(c.Decimals, 1))}
	case model.Date:
		return map[string]any{"type": "string", "format": "date"}
	case model.Timestamp:
		return map[string]any{"type": "string", "format": "date-time"}
	case model.UUID:
		return map[string]any{"type": "string", "format": "uuid"}
	}
	return map[string]any{}
}
