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
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/tim-riep/go-runtime/model"
)

// Expr is a boolean expression of $filter.
type Expr interface{ expr() }

// Operand is a value in an expression: a property, a literal or a call of
// tolower or toupper.
type Operand interface{ operand() }

type (
	// And and Or join two expressions.
	And struct{ L, R Expr }
	Or  struct{ L, R Expr }
	// Not negates an expression.
	Not struct{ X Expr }
	// Compare compares two operands with eq, ne, gt, ge, lt or le.
	Compare struct {
		Op   string
		L, R Operand
	}
	// In compares an operand with a list of literals.
	In struct {
		X      Operand
		Values []Literal
	}
	// Match is contains, startswith or endswith.
	Match struct {
		Func string
		X, Y Operand
	}
	// PropertyRef refers to a property.
	PropertyRef struct{ P *Property }
	// Call is tolower or toupper of an operand.
	Call struct {
		Func string
		X    Operand
	}
)

func (And) expr()     {}
func (Or) expr()      {}
func (Not) expr()     {}
func (Compare) expr() {}
func (In) expr()      {}
func (Match) expr()   {}

func (PropertyRef) operand() {}
func (Call) operand()        {}
func (Literal) operand()     {}

// Kinds of literals.
const (
	LitString         = "string"
	LitInt            = "int"
	LitDecimal        = "decimal"
	LitBool           = "bool"
	LitNull           = "null"
	LitDate           = "date"
	LitDateTimeOffset = "dateTimeOffset"
	LitGuid           = "guid"
)

// Literal is a literal in its canonical text form.
type Literal struct {
	Kind string
	Text string
}

// Comparison operators and their meaning.
var compareOps = map[string]bool{"eq": true, "ne": true, "gt": true, "ge": true, "lt": true, "le": true}

// Functions that are no part of this service.
var unsupportedFuncs = map[string]bool{"length": true, "indexof": true, "substring": true, "trim": true, "concat": true, "year": true,
	"month": true, "day": true, "hour": true, "minute": true, "second": true, "fractionalseconds": true, "date": true, "time": true,
	"totaloffsetminutes": true, "now": true, "maxdatetime": true, "mindatetime": true, "totalseconds": true, "round": true,
	"floor": true, "ceiling": true, "cast": true, "isof": true, "geo.distance": true, "geo.intersects": true, "geo.length": true,
	"matchespattern": true, "hassubset": true, "hassubsequence": true, "case": true}

// token is a token of an expression.
type token struct {
	kind string // ident, string, number, date, dateTimeOffset, guid, punct, end
	text string
	pos  int
}

var (
	guidPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)
	datePattern = regexp.MustCompile(`^-?[0-9]{4}-[0-9]{2}-[0-9]{2}(T[0-9]{2}:[0-9]{2}(:[0-9]{2}(\.[0-9]+)?)?(Z|[+-][0-9]{2}:[0-9]{2}))?`)
	numPattern  = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?`)
)

// lex splits an expression into tokens.
func lex(s string) ([]token, *Error) {
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '(' || c == ')' || c == ',' || c == '/' || c == ':':
			out = append(out, token{kind: "punct", text: string(c), pos: i})
			i++
		case c == '\'':
			var b strings.Builder
			j := i + 1
			for ; ; j++ {
				if j >= len(s) {
					return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("the string at position %d has no end", i+1))
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						b.WriteByte('\'')
						j++
						continue
					}
					break
				}
				b.WriteByte(s[j])
			}
			out = append(out, token{kind: "string", text: b.String(), pos: i})
			i = j + 1
		default:
			rest := s[i:]
			if m := guidPattern.FindString(rest); m != "" && (len(rest) == len(m) || !isIdent(rune(rest[len(m)]))) {
				out = append(out, token{kind: "guid", text: strings.ToLower(m), pos: i})
				i += len(m)
				continue
			}
			if m := datePattern.FindString(rest); m != "" {
				kind := "date"
				if strings.Contains(m, "T") {
					kind = "dateTimeOffset"
				}
				out = append(out, token{kind: kind, text: m, pos: i})
				i += len(m)
				continue
			}
			if m := numPattern.FindString(rest); m != "" {
				out = append(out, token{kind: "number", text: m, pos: i})
				i += len(m)
				continue
			}
			if isIdent(rune(c)) {
				j := i
				for j < len(s) && (isIdent(rune(s[j])) || s[j] == '.' || s[j] >= '0' && s[j] <= '9') {
					j++
				}
				out = append(out, token{kind: "ident", text: s[i:j], pos: i})
				i = j
				continue
			}
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("unexpected %q at position %d", c, i+1))
		}
	}
	return append(out, token{kind: "end", pos: len(s)}), nil
}

func isIdent(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) }

// parser reads an expression of $filter against an entity type.
type parser struct {
	toks []token
	i    int
	t    *EntityType
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

func (p *parser) bad(format string, args ...any) *Error {
	return Errorf(http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf(format, args...))
}

func notImplemented(what string) *Error {
	return Errorf(http.StatusNotImplemented, CodeNotImplemented, what)
}

func (p *parser) keyword(word string) bool {
	if t := p.peek(); t.kind == "ident" && t.text == word {
		p.i++
		return true
	}
	return false
}

func (p *parser) punct(c string) bool {
	if t := p.peek(); t.kind == "punct" && t.text == c {
		p.i++
		return true
	}
	return false
}

// ParseFilter parses $filter for an entity type.
func ParseFilter(s string, t *EntityType) (Expr, *Error) {
	toks, e := lex(s)
	if e != nil {
		return nil, e
	}
	p := &parser{toks: toks, t: t}
	x, e := p.or()
	if e != nil {
		return nil, e
	}
	if tok := p.peek(); tok.kind != "end" {
		return nil, p.bad("unexpected %q at position %d of $filter", tok.text, tok.pos+1)
	}
	return x, nil
}

func (p *parser) or() (Expr, *Error) {
	l, e := p.and()
	for e == nil && p.keyword("or") {
		var r Expr
		if r, e = p.and(); e == nil {
			l = Or{L: l, R: r}
		}
	}
	return l, e
}

func (p *parser) and() (Expr, *Error) {
	l, e := p.unary()
	for e == nil && p.keyword("and") {
		var r Expr
		if r, e = p.unary(); e == nil {
			l = And{L: l, R: r}
		}
	}
	return l, e
}

func (p *parser) unary() (Expr, *Error) {
	if p.keyword("not") {
		x, e := p.unary()
		return Not{X: x}, e
	}
	if t := p.peek(); t.kind == "punct" && t.text == "(" {
		// A parenthesized expression; an operand in parentheses is not
		// supported.
		p.i++
		x, e := p.or()
		if e != nil {
			return nil, e
		}
		if !p.punct(")") {
			return nil, p.bad("missing ) at position %d of $filter", p.peek().pos+1)
		}
		return x, nil
	}
	if t := p.peek(); t.kind == "ident" && (t.text == "contains" || t.text == "startswith" || t.text == "endswith") &&
		p.toks[p.i+1].kind == "punct" && p.toks[p.i+1].text == "(" {
		p.i += 2
		x, e := p.operand()
		if e != nil {
			return nil, e
		}
		if !p.punct(",") {
			return nil, p.bad("%s takes two arguments", t.text)
		}
		y, e := p.operand()
		if e != nil {
			return nil, e
		}
		if !p.punct(")") {
			return nil, p.bad("missing ) of %s", t.text)
		}
		if e := p.textual(x, t.text); e != nil {
			return nil, e
		}
		if e := p.textual(y, t.text); e != nil {
			return nil, e
		}
		return Match{Func: t.text, X: x, Y: y}, nil
	}
	l, e := p.operand()
	if e != nil {
		return nil, e
	}
	op := p.peek()
	switch {
	case op.kind == "ident" && compareOps[op.text]:
		p.i++
		r, e := p.operand()
		if e != nil {
			return nil, e
		}
		return p.compare(op.text, l, r)
	case op.kind == "ident" && op.text == "in":
		p.i++
		if !p.punct("(") {
			return nil, p.bad("in takes a list in parentheses")
		}
		var values []Literal
		for {
			o, e := p.operand()
			if e != nil {
				return nil, e
			}
			lit, ok := o.(Literal)
			if !ok {
				return nil, p.bad("in takes only literals")
			}
			values = append(values, lit)
			if p.punct(")") {
				break
			}
			if !p.punct(",") {
				return nil, p.bad("missing , or ) in the list of in")
			}
		}
		ref, ok := l.(PropertyRef)
		if !ok {
			return nil, p.bad("in compares a property")
		}
		for i, v := range values {
			c, e := literalFor(ref.P, v)
			if e != nil {
				return nil, e
			}
			values[i] = c
		}
		return In{X: l, Values: values}, nil
	case op.kind == "ident" && (op.text == "add" || op.text == "sub" || op.text == "mul" || op.text == "div" || op.text == "divby" || op.text == "mod" || op.text == "has"):
		return nil, notImplemented("the operator " + op.text + " in $filter")
	}
	return nil, p.bad("expected a comparison at position %d of $filter", op.pos+1)
}

// compare checks the operands of a comparison: one is a property, the
// literal fits its type, and null only goes with eq and ne.
func (p *parser) compare(op string, l, r Operand) (Expr, *Error) {
	prop := propertyOf(l)
	other := r
	if prop == nil {
		prop, other = propertyOf(r), l
	}
	if prop == nil {
		return nil, p.bad("a comparison needs a property")
	}
	if lit, ok := other.(Literal); ok {
		c, e := literalFor(prop, lit)
		if e != nil {
			return nil, e
		}
		if other == r {
			r = c
		} else {
			l = c
		}
		if c.Kind == LitNull && op != "eq" && op != "ne" {
			return nil, p.bad("only eq and ne compare with null")
		}
	} else if q := propertyOf(other); q != nil && q.Domain.Type != prop.Domain.Type {
		return nil, p.bad("%s and %s have different types", prop.Name, q.Name)
	}
	if prop.Domain.Type == model.Blob {
		return nil, p.bad("%s cannot be compared", prop.Name)
	}
	return Compare{Op: op, L: l, R: r}, nil
}

// propertyOf returns the property of an operand, also inside tolower and
// toupper.
func propertyOf(o Operand) *Property {
	switch x := o.(type) {
	case PropertyRef:
		return x.P
	case Call:
		return propertyOf(x.X)
	}
	return nil
}

// textual checks that an operand of a string function is text.
func (p *parser) textual(o Operand, fn string) *Error {
	switch x := o.(type) {
	case Literal:
		if x.Kind != LitString {
			return p.bad("%s takes text", fn)
		}
	default:
		if pr := propertyOf(o); pr == nil || !pr.Domain.Type.IsString() {
			return p.bad("%s takes text", fn)
		}
	}
	return nil
}

func (p *parser) operand() (Operand, *Error) {
	t := p.next()
	switch t.kind {
	case "string":
		return Literal{Kind: LitString, Text: t.text}, nil
	case "number":
		if strings.ContainsAny(t.text, "eE") {
			return nil, notImplemented("numbers with exponent in $filter")
		}
		if strings.Contains(t.text, ".") {
			return Literal{Kind: LitDecimal, Text: t.text}, nil
		}
		return Literal{Kind: LitInt, Text: t.text}, nil
	case "date":
		return Literal{Kind: LitDate, Text: t.text}, nil
	case "dateTimeOffset":
		return Literal{Kind: LitDateTimeOffset, Text: t.text}, nil
	case "guid":
		return Literal{Kind: LitGuid, Text: t.text}, nil
	case "ident":
		switch t.text {
		case "null":
			return Literal{Kind: LitNull}, nil
		case "true", "false":
			return Literal{Kind: LitBool, Text: t.text}, nil
		case "tolower", "toupper":
			if !p.punct("(") {
				return nil, p.bad("%s takes an argument in parentheses", t.text)
			}
			x, e := p.operand()
			if e != nil {
				return nil, e
			}
			if !p.punct(")") {
				return nil, p.bad("missing ) of %s", t.text)
			}
			if e := p.textual(x, t.text); e != nil {
				return nil, e
			}
			return Call{Func: t.text, X: x}, nil
		}
		if n := p.peek(); n.kind == "punct" && n.text == "(" {
			if unsupportedFuncs[t.text] {
				return nil, notImplemented("the function " + t.text + " in $filter")
			}
			return nil, p.bad("unknown function %s in $filter", t.text)
		}
		if n := p.peek(); n.kind == "punct" && n.text == "/" {
			return nil, notImplemented("paths along navigations and lambda operators in $filter")
		}
		if strings.HasPrefix(t.text, "$") || strings.Contains(t.text, ".") {
			return nil, notImplemented(t.text + " in $filter")
		}
		prop := p.t.Property(t.text)
		if prop == nil {
			if p.t.Navigation(t.text) != nil {
				return nil, notImplemented("navigations in $filter")
			}
			return nil, Errorf(http.StatusBadRequest, CodeUnknownProperty, p.t.Name, t.text)
		}
		return PropertyRef{P: prop}, nil
	case "punct":
		if t.text == "(" {
			return nil, notImplemented("operands in parentheses in $filter")
		}
	}
	return nil, p.bad("unexpected %q at position %d of $filter", t.text, t.pos+1)
}

// literalFor converts a literal into the canonical text of the type of a
// property, or rejects it.
func literalFor(p *Property, l Literal) (Literal, *Error) {
	if l.Kind == LitNull {
		return l, nil
	}
	d := p.Domain
	wrong := func() (Literal, *Error) {
		return Literal{}, Errorf(http.StatusBadRequest, CodeInvalidValue, p.Name, "a literal of kind "+l.Kind+" does not fit "+string(d.Type))
	}
	text := l.Text
	switch d.Type {
	case model.Char, model.String:
		if l.Kind != LitString {
			return wrong()
		}
	case model.Int4, model.Int8:
		if l.Kind != LitInt {
			return wrong()
		}
	case model.Dec:
		if l.Kind != LitInt && l.Kind != LitDecimal {
			return wrong()
		}
	case model.Bool:
		if l.Kind != LitBool {
			return wrong()
		}
	case model.Date:
		if l.Kind != LitDate {
			return wrong()
		}
	case model.Timestamp:
		if l.Kind != LitDateTimeOffset {
			return wrong()
		}
		t, err := time.Parse(time.RFC3339Nano, l.Text)
		if err != nil {
			if t, err = time.Parse("2006-01-02T15:04Z07:00", l.Text); err != nil {
				return Literal{}, Errorf(http.StatusBadRequest, CodeInvalidValue, p.Name, err.Error())
			}
		}
		text = t.UTC().Format(model.TimestampLayout)
	case model.UUID:
		if l.Kind != LitGuid {
			return wrong()
		}
	default:
		return wrong()
	}
	// Values outside the domain still compare, they only match no row;
	// a decimal with more places than the domain is rounded by nothing.
	if c, err := d.Canonical(text); err == nil {
		text = c
	}
	return Literal{Kind: l.Kind, Text: text}, nil
}
