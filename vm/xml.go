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

package vm

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
)

// The canonical XML format of values, which transform writes and reads:
//
//	<value xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" type="/PKG/STRUCT">
//	  <NAME>text</NAME>            a component; /PKG/FIELD of an extension is PKG.FIELD
//	  <NOTE xsi:nil="true"/>       null
//	  <COUPON/>                    the empty string
//	  <ITEMS><item>...</item></ITEMS>
//	</value>
//
// Simple values are the text of <value>. Array elements are <item>, map
// entries <entry><key>...</key><value>...</value></entry>. DEC has all
// digits of its scale, DATE is YYYY-MM-DD, TIMESTAMP YYYY-MM-DDThh:mm:ss
// with six fraction digits and Z, BOOL true or false, UUID lower-case
// hex, BYTES base64 and enum constants their fixed value.

const xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"

const xmlTimestampLayout = "2006-01-02T15:04:05.000000Z"

// xmlName returns the element name of a field: /PKG/FIELD becomes
// PKG.FIELD, since / is not allowed in XML names.
func xmlName(field string) string {
	if !strings.HasPrefix(field, "/") {
		return field
	}
	return strings.ReplaceAll(strings.TrimPrefix(field, "/"), "/", ".")
}

// EncodeXML writes a value of type t in the canonical XML format.
func (m *Machine) EncodeXML(p *Program, t int, v Value) []byte {
	var b bytes.Buffer
	d := &p.Types[t]
	b.WriteString(`<value xmlns:xsi="` + xsiNamespace + `"`)
	if d.Kind == TStruct {
		b.WriteString(` type="` + d.Name + `"`)
	}
	m.writeXMLContent(&b, p, t, v, "value", true)
	return b.Bytes()
}

// writeXMLContent writes the attributes rest, content and end tag of an
// element whose start tag "<name" is written already.
func (m *Machine) writeXMLContent(b *bytes.Buffer, p *Program, t int, v Value, name string, open bool) {
	d := &p.Types[t]
	if !open {
		b.WriteString("<" + name)
	}
	if v.IsNull() {
		b.WriteString(` xsi:nil="true"/>`)
		return
	}
	switch d.Kind {
	case TStruct:
		r := v.record()
		b.WriteString(">")
		for _, f := range r.Layout.Fields {
			m.writeXMLContent(b, p, f.Type, m.getField(r, f), xmlName(f.Name), false)
		}
	case TArray:
		a := v.array()
		b.WriteString(">")
		for i := range a.Len() {
			m.writeXMLContent(b, p, d.Elem, a.Get(i), "item", false)
		}
	case TMap:
		mp := v.mapv()
		b.WriteString(">")
		for i := range mp.Keys {
			b.WriteString("<entry>")
			m.writeXMLContent(b, p, d.Key, mp.Keys[i], "key", false)
			m.writeXMLContent(b, p, d.Elem, mp.Vals[i], "value", false)
			b.WriteString("</entry>")
		}
	default:
		text := m.xmlText(p, d, v)
		if text == "" {
			b.WriteString("/>")
			return
		}
		b.WriteString(">")
		_ = xml.EscapeText(b, []byte(text))
	}
	b.WriteString("</" + name + ">")
}

// xmlText returns the text form of a simple value.
func (m *Machine) xmlText(p *Program, d *TypeDesc, v Value) string {
	switch d.Kind {
	case TString:
		return v.str()
	case TInt4, TInt8:
		return strconv.FormatInt(v.int(), 10)
	case TDec:
		return dec.String(v.dec(), d.Scale)
	case TDate:
		return formatDate(v.int())
	case TTimestamp:
		return time.UnixMicro(v.int()).UTC().Format(xmlTimestampLayout)
	case TBool:
		return strconv.FormatBool(v.bool())
	case TUUID:
		return formatUUID(v)
	case TBytes:
		b, _ := v.R.([]byte)
		return base64.StdEncoding.EncodeToString(b)
	case TEnum:
		return p.Enums[d.Enum].Values[v.int()]
	}
	return ""
}

// xmlNode is an element of a document read back.
type xmlNode struct {
	name     string
	nil      bool
	text     strings.Builder
	children []*xmlNode
}

func parseXML(data []byte) (*xmlNode, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{name: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Space == xsiNamespace && a.Name.Local == "nil" {
					n.nil = a.Value == "true" || a.Value == "1"
				}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			} else if root == nil {
				root = n
			} else {
				return nil, errors.New("more than one root element")
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("the result is no XML document")
	}
	return root, nil
}

// DecodeXML reads a value of type t from the canonical XML format.
func (m *Machine) DecodeXML(p *Program, t int, data []byte) (Value, error) {
	root, err := parseXML(data)
	if err != nil {
		return Value{}, err
	}
	if root.name != "value" {
		return Value{}, fmt.Errorf("the root element is <%s>, not <value>", root.name)
	}
	return m.decodeNode(p, t, root, "/value")
}

func (m *Machine) decodeNode(p *Program, t int, n *xmlNode, path string) (Value, error) {
	d := &p.Types[t]
	fail := func(format string, args ...any) (Value, error) {
		return Value{}, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...))
	}
	if n.nil {
		if !d.Optional {
			return fail("null for a value that is not optional")
		}
		return Null, nil
	}
	var v Value
	switch d.Kind {
	case TStruct:
		l := &p.Layouts[d.Layout]
		r := &Record{Layout: l, P: p, Data: make([]byte, l.Size), Refs: make([]any, l.Refs)}
		m.alloc(int64(l.Size + 16*l.Refs + 48))
		byName := map[string]*xmlNode{}
		for _, c := range n.children {
			if _, dup := byName[c.name]; dup {
				return fail("<%s> appears twice", c.name)
			}
			byName[c.name] = c
		}
		for _, f := range l.Fields {
			c, ok := byName[xmlName(f.Name)]
			if !ok {
				if !p.Types[f.Type].Optional {
					return fail("<%s> is missing", xmlName(f.Name))
				}
				m.setField(r, f, Null)
				continue
			}
			delete(byName, xmlName(f.Name))
			fv, err := m.decodeNode(p, f.Type, c, path+"/"+c.name)
			if err != nil {
				return Value{}, err
			}
			m.setField(r, f, fv)
		}
		for name := range byName {
			return fail("%s has no component <%s>", d.Name, name)
		}
		v = Value{R: r}
	case TArray:
		a := m.newArray(p, t)
		for i, c := range n.children {
			if c.name != "item" {
				return fail("an array has <item> elements, not <%s>", c.name)
			}
			ev, err := m.decodeNode(p, d.Elem, c, fmt.Sprintf("%s/item[%d]", path, i+1))
			if err != nil {
				return Value{}, err
			}
			if th := m.appendElem(a, ev); th != nil {
				return fail("%s", m.getField(th.obj, m.image.textField).str())
			}
		}
		v = Value{R: a}
	case TMap:
		mp := m.newMap(p, t)
		for i, c := range n.children {
			if c.name != "entry" || len(c.children) != 2 || c.children[0].name != "key" || c.children[1].name != "value" {
				return fail("a map has <entry> elements with <key> and <value>")
			}
			ep := fmt.Sprintf("%s/entry[%d]", path, i+1)
			k, err := m.decodeNode(p, d.Key, c.children[0], ep+"/key")
			if err != nil {
				return Value{}, err
			}
			ev, err := m.decodeNode(p, d.Elem, c.children[1], ep+"/value")
			if err != nil {
				return Value{}, err
			}
			if th := m.mapSet(mp, k, ev); th != nil {
				return fail("%s", m.getField(th.obj, m.image.textField).str())
			}
		}
		v = Value{R: mp}
	default:
		if len(n.children) > 0 {
			return fail("a simple value has no elements, found <%s>", n.children[0].name)
		}
		var err error
		if v, err = m.parseXMLText(p, d, n.text.String()); err != nil {
			return fail("%v", err)
		}
	}
	if d.Named {
		if th := m.check(p, t, v); th != nil {
			return fail("%s", m.getField(th.obj, m.image.textField).str())
		}
	}
	return v, nil
}

// parseXMLText reads the text form of a simple value.
func (m *Machine) parseXMLText(p *Program, d *TypeDesc, s string) (Value, error) {
	switch d.Kind {
	case TString:
		return Str(s), nil
	case TInt4, TInt8:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || d.Kind == TInt4 && !fitsInt4(n) {
			return Value{}, fmt.Errorf("%q is no whole number of the type", s)
		}
		return Int(n), nil
	case TDec:
		x, err := dec.Parse(strings.TrimSpace(s), d.Scale)
		if err != nil || !dec.Fits(x, d.Precision, d.Scale) {
			return Value{}, fmt.Errorf("%q is no decimal of DEC(%d,%d)", s, d.Precision, d.Scale)
		}
		return DecValue(x), nil
	case TBool:
		switch strings.TrimSpace(s) {
		case "true":
			return Bool(true), nil
		case "false":
			return Bool(false), nil
		}
		return Value{}, fmt.Errorf("%q is neither true nor false", s)
	case TBytes:
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return Value{}, fmt.Errorf("the bytes are no base64")
		}
		return Value{R: b}, nil
	case TEnum:
		e := &p.Enums[d.Enum]
		for i, val := range e.Values {
			if val == s {
				return Int(int64(i)), nil
			}
		}
		return Value{}, fmt.Errorf("%s has no fixed value %q", e.Name, s)
	case TDate, TTimestamp, TUUID:
		v, th := m.convert(p, TString, d.Kind, 0, 0, 0, Str(strings.TrimSpace(s)))
		if th != nil {
			return Value{}, errors.New(m.getField(th.obj, m.image.textField).str())
		}
		return v, nil
	}
	return Value{}, fmt.Errorf("values of this type have no text form")
}
