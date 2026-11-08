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
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// XML returns the metadata document in the XML format of CSDL 4.0.
func (md *Metadata) XML() []byte {
	w := &xmlWriter{}
	w.line(0, `<?xml version="1.0" encoding="utf-8"?>`)
	w.line(0, `<edmx:Edmx Version="4.0" xmlns:edmx="http://docs.oasis-open.org/odata/ns/edmx">`)
	for _, r := range md.References {
		w.line(1, `<edmx:Reference Uri=%s>`, attr(r.URI))
		alias := ""
		if r.Alias != "" {
			alias = " Alias=" + attr(r.Alias)
		}
		w.line(2, `<edmx:Include Namespace=%s%s/>`, attr(r.Namespace), alias)
		w.line(1, `</edmx:Reference>`)
	}
	w.line(1, `<edmx:DataServices>`)
	w.line(2, `<Schema Namespace=%s xmlns="http://docs.oasis-open.org/odata/ns/edm">`, attr(md.Namespace))
	for _, t := range md.EntityTypes {
		w.line(3, `<EntityType Name=%s>`, attr(t.Name))
		w.line(4, `<Key>`)
		for _, k := range t.Key {
			w.line(5, `<PropertyRef Name=%s/>`, attr(k))
		}
		w.line(4, `</Key>`)
		for _, p := range t.Properties {
			w.line(4, `<Property%s/>`, propertyAttrs(p, true))
		}
		for _, n := range t.Navigations {
			a := " Name=" + attr(n.Name) + " Type=" + attr(n.Type)
			if !n.Nullable && !strings.HasPrefix(n.Type, "Collection(") {
				a += ` Nullable="false"`
			}
			if n.Partner != "" {
				a += " Partner=" + attr(n.Partner)
			}
			if n.ContainsTarget {
				a += ` ContainsTarget="true"`
			}
			if len(n.Constraints) == 0 {
				w.line(4, `<NavigationProperty%s/>`, a)
				continue
			}
			w.line(4, `<NavigationProperty%s>`, a)
			for _, c := range n.Constraints {
				w.line(5, `<ReferentialConstraint Property=%s ReferencedProperty=%s/>`, attr(c.Property), attr(c.ReferencedProperty))
			}
			w.line(4, `</NavigationProperty>`)
		}
		w.line(3, `</EntityType>`)
	}
	for _, t := range md.ComplexTypes {
		w.line(3, `<ComplexType Name=%s>`, attr(t.Name))
		for _, p := range t.Properties {
			w.line(4, `<Property%s/>`, propertyAttrs(p, true))
		}
		w.line(3, `</ComplexType>`)
	}
	for _, a := range md.Actions {
		at := " Name=" + attr(a.Name)
		if a.BoundTo != "" {
			at += ` IsBound="true"`
		}
		if a.EntitySetPath != "" {
			at += " EntitySetPath=" + attr(a.EntitySetPath)
		}
		w.line(3, `<Action%s>`, at)
		for _, p := range a.Parameters {
			w.line(4, `<Parameter%s/>`, propertyAttrs(p, true))
		}
		if a.ReturnType != "" {
			rt := " Type=" + attr(a.ReturnType)
			if !a.ReturnNullable {
				rt += ` Nullable="false"`
			}
			w.line(4, `<ReturnType%s/>`, rt)
		}
		w.line(3, `</Action>`)
	}
	w.line(3, `<EntityContainer Name=%s>`, attr(containerName))
	for _, s := range md.EntitySets {
		if len(s.Bindings) == 0 {
			w.line(4, `<EntitySet Name=%s EntityType=%s/>`, attr(s.Name), attr(s.EntityType))
			continue
		}
		w.line(4, `<EntitySet Name=%s EntityType=%s>`, attr(s.Name), attr(s.EntityType))
		for _, b := range s.Bindings {
			w.line(5, `<NavigationPropertyBinding Path=%s Target=%s/>`, attr(b.Path), attr(b.Target))
		}
		w.line(4, `</EntitySet>`)
	}
	for _, a := range md.ActionImports {
		set := ""
		if a.EntitySet != "" {
			set = " EntitySet=" + attr(a.EntitySet)
		}
		w.line(4, `<ActionImport Name=%s Action=%s%s/>`, attr(a.Name), attr(a.Action), set)
	}
	w.line(3, `</EntityContainer>`)
	for _, g := range groupByTarget(md.Annotations) {
		w.line(3, `<Annotations Target=%s>`, attr(g[0].Target))
		for _, a := range g {
			q := ""
			if a.Qualifier != "" {
				q = " Qualifier=" + attr(a.Qualifier)
			}
			w.annotation(4, "Term="+attr(a.Term)+q, a.Value)
		}
		w.line(3, `</Annotations>`)
	}
	w.line(2, `</Schema>`)
	w.line(1, `</edmx:DataServices>`)
	w.line(0, `</edmx:Edmx>`)
	return w.buf.Bytes()
}

// propertyAttrs returns the attributes of a property or parameter; XML
// defaults Nullable to true.
func propertyAttrs(p MetaProperty, nullableDefault bool) string {
	a := " Name=" + attr(p.Name) + " Type=" + attr(p.Type)
	if !p.Nullable && nullableDefault {
		a += ` Nullable="false"`
	}
	if v, ok := p.MaxLength.Get(); ok {
		a += ` MaxLength="` + strconv.FormatInt(v, 10) + `"`
	}
	if v, ok := p.Precision.Get(); ok {
		a += ` Precision="` + strconv.FormatInt(v, 10) + `"`
	}
	if v, ok := p.Scale.Get(); ok {
		a += ` Scale="` + strconv.FormatInt(v, 10) + `"`
	}
	return a
}

// groupByTarget groups annotations by target in the order the targets
// first appear.
func groupByTarget(as []Annotation) [][]Annotation {
	var order []string
	groups := map[string][]Annotation{}
	for _, a := range as {
		if _, ok := groups[a.Target]; !ok {
			order = append(order, a.Target)
		}
		groups[a.Target] = append(groups[a.Target], a)
	}
	out := make([][]Annotation, len(order))
	for i, t := range order {
		out[i] = groups[t]
	}
	return out
}

type xmlWriter struct{ buf bytes.Buffer }

func (w *xmlWriter) line(depth int, format string, args ...any) {
	w.buf.WriteString(strings.Repeat("  ", depth))
	if len(args) == 0 {
		w.buf.WriteString(format)
	} else {
		w.buf.WriteString(fmt.Sprintf(format, args...))
	}
	w.buf.WriteByte('\n')
}

// attr quotes an attribute value.
func attr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	_ = xml.EscapeText(&b, []byte(s))
	b.WriteByte('"')
	return strings.ReplaceAll(b.String(), "&#xA;", "&#10;")
}

// xmlKinds are the attribute and element names of simple values.
var xmlKinds = map[string]string{KindString: "String", KindBoolean: "Bool", KindInteger: "Int", KindDecimal: "Decimal", KindDate: "Date",
	KindDateTimeOffset: "DateTimeOffset", KindGuid: "Guid", KindEnumMember: "EnumMember", KindPath: "Path", KindPropertyPath: "PropertyPath",
	KindNavigationPath: "NavigationPropertyPath", KindAnnotationPath: "AnnotationPath"}

// annotation writes an annotation or a property value: simple values as
// attribute, others as child element.
func (w *xmlWriter) annotation(depth int, head string, v Value) {
	element := "Annotation"
	if strings.HasPrefix(head, "Property=") {
		element = "PropertyValue"
	}
	if k, ok := xmlKinds[v.Kind]; ok {
		w.line(depth, "<%s %s %s=%s/>", element, head, k, attr(v.Text))
		return
	}
	w.line(depth, "<%s %s>", element, head)
	w.value(depth+1, v)
	w.line(depth, "</%s>", element)
}

// value writes a value as element.
func (w *xmlWriter) value(depth int, v Value) {
	switch v.Kind {
	case KindNull:
		w.line(depth, "<Null/>")
	case KindCollection:
		if len(v.Items) == 0 {
			w.line(depth, "<Collection/>")
			return
		}
		w.line(depth, "<Collection>")
		for _, it := range v.Items {
			w.value(depth+1, it)
		}
		w.line(depth, "</Collection>")
	case KindRecord:
		typ := ""
		if v.Type != "" {
			typ = " Type=" + attr(v.Type)
		}
		if len(v.Members) == 0 {
			w.line(depth, "<Record%s/>", typ)
			return
		}
		w.line(depth, "<Record%s>", typ)
		for _, m := range v.Members {
			if term, ok := strings.CutPrefix(m.Name, "@"); ok {
				w.annotation(depth+1, "Term="+attr(term), m.Value)
			} else {
				w.annotation(depth+1, "Property="+attr(m.Name), m.Value)
			}
		}
		w.line(depth, "</Record>")
	default:
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(v.Text))
		k := xmlKinds[v.Kind]
		w.line(depth, "<%s>%s</%s>", k, b.String(), k)
	}
}

// JSON returns the metadata document in the JSON format of CSDL 4.01.
func (md *Metadata) JSON() []byte {
	doc := &object{}
	doc.set("$Version", "4.01")
	doc.set("$EntityContainer", md.Container())
	refs := &object{}
	for _, r := range md.References {
		inc := &object{}
		inc.set("$Namespace", r.Namespace)
		if r.Alias != "" {
			inc.set("$Alias", r.Alias)
		}
		ref := &object{}
		ref.set("$Include", []any{inc})
		refs.set(r.URI, ref)
	}
	doc.set("$Reference", refs)
	schema := &object{}
	for _, t := range md.EntityTypes {
		o := &object{}
		o.set("$Kind", "EntityType")
		o.set("$Key", t.Key)
		for _, p := range t.Properties {
			o.set(p.Name, propertyJSON(p, false))
		}
		for _, n := range t.Navigations {
			no := &object{}
			no.set("$Kind", "NavigationProperty")
			typ, collection := strings.CutPrefix(n.Type, "Collection(")
			if collection {
				typ = strings.TrimSuffix(typ, ")")
				no.set("$Collection", true)
			}
			no.set("$Type", typ)
			if n.Nullable && !collection {
				no.set("$Nullable", true)
			}
			if n.Partner != "" {
				no.set("$Partner", n.Partner)
			}
			if n.ContainsTarget {
				no.set("$ContainsTarget", true)
			}
			if len(n.Constraints) > 0 {
				rc := &object{}
				for _, c := range n.Constraints {
					rc.set(c.Property, c.ReferencedProperty)
				}
				no.set("$ReferentialConstraint", rc)
			}
			o.set(n.Name, no)
		}
		schema.set(t.Name, o)
	}
	for _, t := range md.ComplexTypes {
		o := &object{}
		o.set("$Kind", "ComplexType")
		for _, p := range t.Properties {
			o.set(p.Name, propertyJSON(p, false))
		}
		schema.set(t.Name, o)
	}
	overloads := map[string][]any{}
	var names []string
	for _, a := range md.Actions {
		o := &object{}
		o.set("$Kind", "Action")
		if a.BoundTo != "" {
			o.set("$IsBound", true)
		}
		if a.EntitySetPath != "" {
			o.set("$EntitySetPath", a.EntitySetPath)
		}
		var params []any
		for _, p := range a.Parameters {
			po := propertyJSON(p, true)
			params = append(params, po)
		}
		if len(params) > 0 {
			o.set("$Parameter", params)
		}
		if a.ReturnType != "" {
			rt := &object{}
			typ, collection := strings.CutPrefix(a.ReturnType, "Collection(")
			if collection {
				rt.set("$Collection", true)
				typ = strings.TrimSuffix(typ, ")")
			}
			rt.set("$Type", typ)
			if a.ReturnNullable {
				rt.set("$Nullable", true)
			}
			o.set("$ReturnType", rt)
		}
		if _, ok := overloads[a.Name]; !ok {
			names = append(names, a.Name)
		}
		overloads[a.Name] = append(overloads[a.Name], o)
	}
	for _, n := range names {
		schema.set(n, overloads[n])
	}
	c := &object{}
	c.set("$Kind", "EntityContainer")
	for _, s := range md.EntitySets {
		o := &object{}
		o.set("$Collection", true)
		o.set("$Type", s.EntityType)
		if len(s.Bindings) > 0 {
			b := &object{}
			for _, x := range s.Bindings {
				b.set(x.Path, x.Target)
			}
			o.set("$NavigationPropertyBinding", b)
		}
		c.set(s.Name, o)
	}
	for _, a := range md.ActionImports {
		o := &object{}
		o.set("$Action", a.Action)
		if a.EntitySet != "" {
			o.set("$EntitySet", a.EntitySet)
		}
		c.set(a.Name, o)
	}
	schema.set(containerName, c)
	anns := &object{}
	for _, g := range groupByTarget(md.Annotations) {
		o := &object{}
		for _, a := range g {
			key := "@" + a.Term
			if a.Qualifier != "" {
				key += "#" + a.Qualifier
			}
			o.set(key, valueJSON(a.Value))
		}
		anns.set(g[0].Target, o)
	}
	if len(anns.keys) > 0 {
		schema.set("$Annotations", anns)
	}
	doc.set(md.Namespace, schema)
	out, _ := json.MarshalIndent(doc, "", "  ")
	return append(out, '\n')
}

// propertyJSON returns a property or parameter; JSON defaults Type to
// Edm.String and Nullable to false.
func propertyJSON(p MetaProperty, parameter bool) *object {
	o := &object{}
	if parameter {
		o.set("$Name", p.Name)
	}
	typ, collection := strings.CutPrefix(p.Type, "Collection(")
	if collection {
		typ = strings.TrimSuffix(typ, ")")
		o.set("$Collection", true)
	}
	if typ != "Edm.String" {
		o.set("$Type", typ)
	}
	if p.Nullable {
		o.set("$Nullable", true)
	}
	if v, ok := p.MaxLength.Get(); ok {
		o.set("$MaxLength", v)
	}
	if v, ok := p.Precision.Get(); ok {
		o.set("$Precision", v)
	}
	if v, ok := p.Scale.Get(); ok {
		o.set("$Scale", v)
	}
	return o
}

// jsonPaths are the keys of path values.
var jsonPaths = map[string]string{KindPath: "$Path", KindPropertyPath: "$PropertyPath", KindNavigationPath: "$NavigationPropertyPath",
	KindAnnotationPath: "$AnnotationPath"}

// valueJSON returns the value of an annotation in JSON.
func valueJSON(v Value) any {
	switch v.Kind {
	case KindBoolean:
		return v.Text == "true"
	case KindInteger, KindDecimal:
		return json.Number(v.Text)
	case KindNull:
		return nil
	case KindCollection:
		items := []any{}
		for _, it := range v.Items {
			items = append(items, valueJSON(it))
		}
		return items
	case KindRecord:
		o := &object{}
		if v.Type != "" {
			o.set("@type", "#"+v.Type)
		}
		for _, m := range v.Members {
			o.set(m.Name, valueJSON(m.Value))
		}
		return o
	}
	if key, ok := jsonPaths[v.Kind]; ok {
		o := &object{}
		o.set(key, v.Text)
		return o
	}
	return v.Text
}

// object is a JSON object that keeps the order of its members.
type object struct {
	keys   []string
	values []any
}

func (o *object) set(k string, v any) {
	o.keys = append(o.keys, k)
	o.values = append(o.values, v)
}

// MarshalJSON writes the members in their order.
func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.values[i])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
