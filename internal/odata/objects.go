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
	"slices"
	"strconv"
	"time"

	"github.com/tim-riep/go-runtime/internal/objects"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/structure"
	"github.com/tim-riep/go-runtime/structure/jsonvalue"
)

// Properties of entity sets of objects besides their content.
const (
	ObjectName    = "NAME"
	ObjectPackage = "PACKAGE_ID"
	ObjectHash    = "HASH"
)

// ObjectSource is the type of the objects an entity set offers with the
// resolved structure of their content.
type ObjectSource struct {
	Type *model.ObjectType
	Def  *structure.Definition
}

// objectSet builds the entity type of an entity set of objects: the name
// as key, the package and the hash, then the components of the content.
func (b *builder) objectSet(def *model.EntitySet, name string) *EntityType {
	ot := b.s.ObjectTypes[name]
	if ot == nil {
		b.fail(def.Src.At("/objectType"), "the object type %s does not exist", name)
		return nil
	}
	if len(def.Operations.Or(nil)) > 0 || len(def.Actions.Or(nil)) > 0 || len(def.HiddenFields) > 0 || len(def.Navigations) > 0 {
		b.fail(def.Src, "an entity set of objects is read only and offers all components without navigations")
		return nil
	}
	rt, problems := objects.Resolve(b.s, ot, nil)
	if len(problems) > 0 {
		b.fail(def.Src.At("/objectType"), "the object type %s is invalid", name)
		return nil
	}
	tab := &model.Table{Name: name, Owner: ot.Owner, Description: model.Some(ot.Description[model.DefaultLanguage]), PrimaryKey: []string{ObjectName}}
	t := &EntityType{Name: model.PublicName(name), Table: tab, Object: &ObjectSource{Type: ot, Def: rt.Def}}
	add := func(p *Property) {
		f := model.Field{Name: p.Field.Name, NotNull: !p.Nullable, Description: p.Field.Description}
		tab.Fields = append(tab.Fields, f)
		p.Field = &tab.Fields[len(tab.Fields)-1]
		t.Properties = append(t.Properties, p)
	}
	fixed := []struct {
		name, label string
		length      int
	}{{ObjectName, "Name of the object", 63}, {ObjectPackage, "Package that ships the object", 16}, {ObjectHash, "SHA-256 of the content", 64}}
	for _, x := range fixed {
		add(&Property{Name: x.name, Field: &model.Field{Name: x.name, Description: model.Some(x.label)},
			Domain: &model.Domain{Name: x.name, Type: model.Char, Length: x.length}, Key: x.name == ObjectName, Computed: true})
	}
	for i := range rt.Def.Components {
		c := &rt.Def.Components[i]
		if slices.ContainsFunc(fixed, func(x struct {
			name, label string
			length      int
		}) bool {
			return x.name == c.Name
		}) {
			b.fail(def.Src.At("/objectType"), "the component %s of %s has the name of a property of every object", c.Name, ot.Content)
			continue
		}
		p := &Property{Name: model.PublicName(c.Name), Field: &model.Field{Name: c.Name, Description: c.Description}, Nullable: !c.NotNull,
			Computed: true, Component: c, Domain: &model.Domain{Name: c.Domain, Type: model.String}}
		if c.Kind == structure.Elementary {
			p.Domain = &model.Domain{Name: c.Domain, Type: c.Type, Length: c.Length, Decimals: c.Decimals, Values: c.FixedValues}
			if c.DataElement != nil {
				p.DataElement = b.s.DataElements[c.DataElement.Name]
			}
		}
		add(p)
	}
	for _, p := range t.Properties {
		if p.Key {
			t.Key = append(t.Key, p)
		}
	}
	return t
}

// Elementary reports whether a property holds a single value, so that
// $filter and $orderby work on it.
func (p *Property) Elementary() bool {
	return p.Component == nil || p.Component.Kind == structure.Elementary
}

// ObjectRow converts an object into a row of its entity type: elementary
// values in the form of the transaction buffer, nested and array
// components as values that f writes as JSON.
func (t *EntityType) ObjectRow(o *model.Object, f Format) (map[string]any, error) {
	v, problems := jsonvalue.Check(t.Object.Def, o.Content)
	if len(problems) > 0 {
		return nil, &Error{Status: 500, Code: CodeInternal, Text: "object " + o.Name + ": " + problems[0].String()}
	}
	row := map[string]any{ObjectName: o.Name, ObjectPackage: string(o.Owner), ObjectHash: o.Hash}
	for _, fv := range v.Fields {
		// Elementary values stay in the form of the buffer for $filter
		// and $orderby; Format.Entity writes them.
		if x := fv.Value; !x.Null && x.Component.Kind == structure.Elementary {
			row[fv.Name] = elementary(x.Component.Type, x.Text)
			continue
		}
		row[fv.Name] = valueOf(fv.Value, f)
	}
	return row, nil
}

// valueOf converts a checked value.
func valueOf(v *jsonvalue.Value, f Format) any {
	switch {
	case v.Null:
		return nil
	case v.Component != nil && v.Component.Kind == structure.Array:
		list := []any{}
		for _, it := range v.Items {
			list = append(list, valueOf(it, f))
		}
		return list
	case v.Component == nil || v.Component.Kind == structure.Nested:
		o := &Object{}
		for _, fv := range v.Fields {
			o.Set(model.PublicName(fv.Name), valueOf(fv.Value, f))
		}
		return o
	}
	return f.JSONValue(domainOf(*v.Component), elementary(v.Component.Type, v.Text))
}

// elementary converts canonical text into the form of the transaction
// buffer.
func elementary(t model.BaseType, text string) any {
	switch t {
	case model.Int4, model.Int8:
		n, _ := strconv.ParseInt(text, 10, 64)
		return n
	case model.Bool:
		return text == "true"
	case model.Date:
		d, _ := time.Parse(model.DateLayout, text)
		return d
	case model.Timestamp:
		ts, _ := time.Parse(model.TimestampLayout, text)
		return ts
	}
	return text
}
