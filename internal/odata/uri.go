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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// KeyValue is the value of a key property in a key predicate.
type KeyValue struct {
	P     *Property
	Value Literal
}

// Path is the resource path of a request below the root of a service.
type Path struct {
	Set *Set
	// Key addresses one entity of the set.
	Key []KeyValue
	// Nav follows a navigation of that entity; NavKey addresses one
	// entity of a collection navigation.
	Nav    *Navigation
	NavKey []KeyValue
	// Action is a bound action on the addressed entity; Import a static
	// action of the container.
	Action *Action
	Import *Action
	// Count asks for the number of entities.
	Count bool
}

// Type returns the type of the entities the path addresses.
func (p *Path) Type() *EntityType {
	if p.Nav != nil {
		return p.Nav.Type
	}
	if p.Set != nil {
		return p.Set.Type
	}
	return nil
}

// Single reports whether the path addresses one entity.
func (p *Path) Single() bool {
	if p.Nav != nil {
		return !p.Nav.Collection || p.NavKey != nil
	}
	return p.Key != nil
}

// ParsePath parses a resource path given in escaped form, such as
// Companies('C1')/Contacts or Items/$count.
func ParsePath(m *Model, escaped string) (*Path, *Error) {
	raw := splitSegments(escaped)
	segs := make([]string, len(raw))
	for i, s := range raw {
		u, err := url.PathUnescape(s)
		if err != nil {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the path is not escaped correctly")
		}
		segs[i] = u
	}
	p := &Path{}
	name, pred, e := splitPredicate(segs[0])
	if e != nil {
		return nil, e
	}
	if imp := m.Import(name); imp != nil && pred == "" && len(segs) == 1 {
		p.Import = imp
		return p, nil
	}
	p.Set = m.Set(name)
	if p.Set == nil {
		return nil, Errorf(http.StatusNotFound, CodeNotFound, name)
	}
	if pred != "" {
		if p.Key, e = parseKey(p.Set.Type, pred); e != nil {
			return nil, e
		}
	}
	for i := 1; i < len(segs); i++ {
		seg := segs[i]
		last := i == len(segs)-1
		switch {
		case seg == "$count" && last && !p.Single():
			p.Count = true
		case seg == "$count":
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "$count follows a collection")
		case seg == "$value" || seg == "$ref":
			return nil, notImplemented(seg)
		case strings.Contains(seg, ".") && last && p.Single() && p.Nav == nil:
			// A bound action: Namespace.Name.
			short := seg[strings.LastIndex(seg, ".")+1:]
			if seg[:strings.LastIndex(seg, ".")] != m.Namespace || p.Set.Type.Action(short) == nil {
				return nil, Errorf(http.StatusNotFound, CodeNotFound, seg)
			}
			p.Action = p.Set.Type.Action(short)
		case p.Single() && p.Nav == nil:
			name, pred, e := splitPredicate(seg)
			if e != nil {
				return nil, e
			}
			t := p.Set.Type
			if prop := t.Property(name); prop != nil {
				return nil, notImplemented("paths to properties such as " + name)
			}
			p.Nav = t.Navigation(name)
			if p.Nav == nil {
				return nil, Errorf(http.StatusNotFound, CodeNotFound, name)
			}
			if pred != "" {
				if !p.Nav.Collection {
					return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, name+" leads to one entity and takes no key")
				}
				if p.NavKey, e = parseKey(p.Nav.Type, pred); e != nil {
					return nil, e
				}
			}
		default:
			return nil, notImplemented("this path: " + strings.Join(segs, "/"))
		}
	}
	return p, nil
}

// splitSegments splits a path at slashes outside quotes.
func splitSegments(s string) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			quoted = !quoted
		case '/':
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// splitPredicate splits Name(predicate).
func splitPredicate(seg string) (string, string, *Error) {
	open := strings.IndexByte(seg, '(')
	if open < 0 {
		return seg, "", nil
	}
	if !strings.HasSuffix(seg, ")") {
		return "", "", Errorf(http.StatusBadRequest, CodeInvalidRequest, "the key of "+seg[:open]+" has no )")
	}
	return seg[:open], seg[open+1 : len(seg)-1], nil
}

// parseKey parses a key predicate: one value for a type with one key
// property, else name=value pairs for every key property.
func parseKey(t *EntityType, pred string) ([]KeyValue, *Error) {
	bad := func() ([]KeyValue, *Error) {
		return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the key ("+pred+") does not fit "+t.Name)
	}
	var out []KeyValue
	if len(t.Key) == 1 {
		if toks, e := lex(pred); e == nil {
			p := &parser{toks: toks, t: t}
			if o, e := p.operand(); e == nil && p.peek().kind == "end" {
				lit, ok := o.(Literal)
				if !ok {
					return bad()
				}
				c, e := literalFor(t.Key[0], lit)
				if e != nil {
					return nil, e
				}
				return []KeyValue{{P: t.Key[0], Value: c}}, nil
			}
		}
	}
	// Named values: the tokenizer reads = as an unexpected character, so
	// split the predicate at commas outside quotes first.
	for _, part := range splitOutside(pred, ',') {
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return bad()
		}
		prop := t.Property(strings.TrimSpace(name))
		if prop == nil || !prop.Key {
			return bad()
		}
		vt, e := lex(strings.TrimSpace(value))
		if e != nil {
			return nil, e
		}
		vp := &parser{toks: vt, t: t}
		o, e := vp.operand()
		lit, isLit := o.(Literal)
		if e != nil || !isLit || vp.peek().kind != "end" {
			return bad()
		}
		c, e := literalFor(prop, lit)
		if e != nil {
			return nil, e
		}
		out = append(out, KeyValue{P: prop, Value: c})
	}
	if len(out) != len(t.Key) {
		return bad()
	}
	return out, nil
}

// splitOutside splits at a separator outside quotes and parentheses.
func splitOutside(s string, sep byte) []string {
	var out []string
	start, depth, quoted := 0, 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// Order is an entry of $orderby.
type Order struct {
	P    *Property
	Desc bool
}

// Expand is an entry of $expand with the properties it selects.
type Expand struct {
	Nav    *Navigation
	Select []*Property
}

// Query holds the system query options of a request.
type Query struct {
	Filter  Expr
	OrderBy []Order
	Top     int64 // -1 without $top
	Skip    int64
	Count   bool
	// Select is nil for all properties.
	Select    []*Property
	Expand    []Expand
	SkipToken int64
}

// options that are no part of this service, and that answer 501.
var unsupportedOptions = map[string]bool{"$search": true, "$apply": true, "$levels": true, "$compute": true, "$index": true,
	"$schemaversion": true, "$deltatoken": true, "$id": true}

// ParseQuery parses the system query options for an entity type. Options
// without $ are custom options, such as tenant and lang, and stay unread.
func ParseQuery(values url.Values, t *EntityType) (*Query, *Error) {
	q := &Query{Top: -1}
	for name := range values {
		if !strings.HasPrefix(name, "$") {
			continue
		}
		switch name {
		case "$filter", "$orderby", "$top", "$skip", "$count", "$select", "$expand", "$format", "$skiptoken":
		default:
			if unsupportedOptions[name] {
				return nil, notImplemented(name)
			}
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "unknown system query option "+name)
		}
		if len(values[name]) > 1 {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, name+" appears twice")
		}
	}
	var e *Error
	if f := values.Get("$filter"); f != "" {
		if q.Filter, e = ParseFilter(f, t); e != nil {
			return nil, e
		}
	}
	if o := values.Get("$orderby"); o != "" {
		for _, part := range splitOutside(o, ',') {
			fields := strings.Fields(part)
			if len(fields) == 0 || len(fields) > 2 || len(fields) == 2 && fields[1] != "asc" && fields[1] != "desc" {
				return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "$orderby takes properties with asc or desc")
			}
			prop, e := selectable(t, fields[0], "$orderby")
			if e != nil {
				return nil, e
			}
			q.OrderBy = append(q.OrderBy, Order{P: prop, Desc: len(fields) == 2 && fields[1] == "desc"})
		}
	}
	number := func(name string) (int64, *Error) {
		v := values.Get(name)
		if v == "" {
			return -1, nil
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return 0, Errorf(http.StatusBadRequest, CodeInvalidRequest, name+" takes a number from 0")
		}
		return n, nil
	}
	if q.Top, e = number("$top"); e != nil {
		return nil, e
	}
	skip, e := number("$skip")
	if e != nil {
		return nil, e
	}
	q.Skip = max(skip, 0)
	token, e := number("$skiptoken")
	if e != nil {
		return nil, e
	}
	q.SkipToken = max(token, 0)
	switch c := values.Get("$count"); c {
	case "", "false":
	case "true":
		q.Count = true
	default:
		return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "$count is true or false")
	}
	if s := values.Get("$select"); s != "" {
		if q.Select, e = parseSelect(t, s); e != nil {
			return nil, e
		}
	}
	if x := values.Get("$expand"); x != "" {
		for _, part := range splitOutside(x, ',') {
			part = strings.TrimSpace(part)
			name, opts, _ := strings.Cut(part, "(")
			if name == "*" {
				return nil, notImplemented("$expand=*")
			}
			if strings.Contains(name, "/") {
				return nil, notImplemented("$expand over more than one level")
			}
			nav := t.Navigation(name)
			if nav == nil {
				return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, t.Name+" has no navigation "+name)
			}
			ex := Expand{Nav: nav}
			if opts != "" {
				if !strings.HasSuffix(opts, ")") {
					return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, "the options of $expand need )")
				}
				for _, o := range splitOutside(strings.TrimSuffix(opts, ")"), ';') {
					on, ov, _ := strings.Cut(o, "=")
					if on != "$select" {
						return nil, notImplemented(on + " inside $expand")
					}
					if ex.Select, e = parseSelect(nav.Type, ov); e != nil {
						return nil, e
					}
				}
			}
			q.Expand = append(q.Expand, ex)
		}
	}
	return q, nil
}

// parseSelect parses the properties of $select.
func parseSelect(t *EntityType, s string) ([]*Property, *Error) {
	var out []*Property
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "*" {
			return nil, nil
		}
		prop, e := selectable(t, name, "$select")
		if e != nil {
			return nil, e
		}
		out = append(out, prop)
	}
	return out, nil
}

// selectable returns a property named in $select or $orderby.
func selectable(t *EntityType, name, option string) (*Property, *Error) {
	if prop := t.Property(name); prop != nil {
		return prop, nil
	}
	if t.Navigation(name) != nil || strings.Contains(name, "/") {
		return nil, notImplemented(fmt.Sprintf("navigations in %s", option))
	}
	return nil, Errorf(http.StatusBadRequest, CodeUnknownProperty, t.Name, name)
}
