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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
)

// CallJSON calls a public static method with arguments in JSON and
// returns its result in JSON. DEC values are strings to stay exact; DATE
// and TIMESTAMP use ISO 8601, BYTES base64, enums the names of their
// constants; structs are objects, maps with STRING keys objects and other
// maps lists of [key, value] pairs. null and "" stay apart.
func (m *Machine) CallJSON(ctx context.Context, class, method string, args []json.RawMessage) ([]byte, error) {
	c := m.image.Class(class)
	if c == nil {
		return nil, fmt.Errorf("unknown class %s", class)
	}
	mi, ok := c.Method(method)
	if !ok || !mi.Static || !mi.Public || c.Def.Test {
		return nil, fmt.Errorf("%s has no public static method %s", class, method)
	}
	f, p := mi.Func.Def, mi.Func.Prog.P
	if len(args) != len(f.ParamTypes) {
		return nil, fmt.Errorf("%s.%s takes %d arguments, %d given", class, method, len(f.ParamTypes), len(args))
	}
	vals := make([]Value, len(args))
	for i, a := range args {
		v, err := m.FromJSON(p, f.ParamTypes[i], a)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i+1, err)
		}
		vals[i] = v
	}
	v, err := m.Call(ctx, class, method, vals...)
	if err != nil {
		return nil, err
	}
	if f.Result < 0 {
		return []byte("null"), nil
	}
	return m.ToJSON(p, f.Result, v)
}

// ToJSON encodes a value of type t.
func (m *Machine) ToJSON(p *Program, t int, v Value) ([]byte, error) {
	var b bytes.Buffer
	if err := m.writeJSON(&b, p, t, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m *Machine) writeJSON(b *bytes.Buffer, p *Program, t int, v Value) error {
	if v.IsNull() {
		b.WriteString("null")
		return nil
	}
	d := &p.Types[t]
	str := func(s string) {
		out, _ := json.Marshal(s)
		b.Write(out)
	}
	switch d.Kind {
	case TString:
		str(v.str())
	case TInt4, TInt8:
		b.WriteString(strconv.FormatInt(v.int(), 10))
	case TBool:
		b.WriteString(strconv.FormatBool(v.bool()))
	case TDec:
		str(dec.String(v.dec(), d.Scale))
	case TDate, TTimestamp, TUUID, TBytes:
		str(m.format(p, d.Kind, 0, v))
	case TEnum:
		str(p.Enums[d.Enum].Names[v.int()])
	case TStruct:
		r := v.record()
		b.WriteByte('{')
		for i, f := range r.Layout.Fields {
			if i > 0 {
				b.WriteByte(',')
			}
			str(f.Name)
			b.WriteByte(':')
			if err := m.writeJSON(b, r.P, f.Type, m.getField(r, f)); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case TArray:
		a := v.array()
		b.WriteByte('[')
		for i := range a.Len() {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := m.writeJSON(b, a.P, a.Elem, a.Get(i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case TMap:
		mp := v.mapv()
		obj := mp.P.Types[mp.Key].Kind == TString
		if obj {
			b.WriteByte('{')
		} else {
			b.WriteByte('[')
		}
		for i, k := range mp.Keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if obj {
				str(k.str())
				b.WriteByte(':')
			} else {
				b.WriteByte('[')
				if err := m.writeJSON(b, mp.P, mp.Key, k); err != nil {
					return err
				}
				b.WriteByte(',')
			}
			if err := m.writeJSON(b, mp.P, mp.Elem, mp.Vals[i]); err != nil {
				return err
			}
			if !obj {
				b.WriteByte(']')
			}
		}
		if obj {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	default:
		return fmt.Errorf("values of %s cannot be written as JSON", d.Name)
	}
	return nil
}

// FromJSON decodes a value of type t.
func (m *Machine) FromJSON(p *Program, t int, raw json.RawMessage) (Value, error) {
	d := &p.Types[t]
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !d.Optional {
			return Value{}, fmt.Errorf("null is not allowed for a value that is not optional")
		}
		return Null, nil
	}
	text := func() (string, error) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("expected a string, got %s", raw)
		}
		return s, nil
	}
	var v Value
	switch d.Kind {
	case TString:
		s, err := text()
		if err != nil {
			return Value{}, err
		}
		v = Str(s)
	case TInt4, TInt8:
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil || d.Kind == TInt4 && !fitsInt4(n) {
			return Value{}, fmt.Errorf("expected a whole number, got %s", raw)
		}
		v = Int(n)
	case TBool:
		var x bool
		if err := json.Unmarshal(raw, &x); err != nil {
			return Value{}, fmt.Errorf("expected true or false, got %s", raw)
		}
		v = Bool(x)
	case TDec:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(bytes.TrimSpace(raw))
		}
		x, err := dec.Parse(s, d.Scale)
		if err != nil || !dec.Fits(x, d.Precision, d.Scale) {
			return Value{}, fmt.Errorf("expected a decimal of DEC(%d,%d), got %s", d.Precision, d.Scale, raw)
		}
		v = DecValue(x)
	case TDate, TTimestamp, TUUID:
		s, err := text()
		if err != nil {
			return Value{}, err
		}
		var t *thrown
		v, t = m.convert(p, TString, d.Kind, 0, 0, 0, Str(s))
		if t != nil {
			return Value{}, fmt.Errorf("%s", m.getField(t.obj, m.image.textField).str())
		}
	case TBytes:
		s, err := text()
		if err != nil {
			return Value{}, err
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return Value{}, fmt.Errorf("expected base64, got %s", raw)
		}
		v = Value{R: b}
	case TEnum:
		s, err := text()
		if err != nil {
			return Value{}, err
		}
		e := &p.Enums[d.Enum]
		found := -1
		for i, n := range e.Names {
			if n == s {
				found = i
			}
		}
		if found < 0 {
			return Value{}, fmt.Errorf("%s has no constant %s", e.Name, s)
		}
		v = Int(int64(found))
	case TStruct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return Value{}, fmt.Errorf("expected an object for %s", d.Name)
		}
		l := &p.Layouts[d.Layout]
		r := m.newRecord(nil, p, l)
		for name := range fields {
			if l.Field(name) < 0 {
				return Value{}, fmt.Errorf("%s has no component %s", d.Name, name)
			}
		}
		for _, f := range l.Fields {
			fr, ok := fields[f.Name]
			if !ok {
				if !f.Optional {
					return Value{}, fmt.Errorf("%s needs %s", d.Name, f.Name)
				}
				continue
			}
			fv, err := m.FromJSON(p, f.Type, fr)
			if err != nil {
				return Value{}, fmt.Errorf("%s: %w", f.Name, err)
			}
			if t := m.check(p, f.Type, fv); t != nil {
				return Value{}, fmt.Errorf("%s: %s", f.Name, m.getField(t.obj, m.image.textField).str())
			}
			m.setField(r, f, fv)
		}
		v = Value{R: r}
	case TArray:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return Value{}, fmt.Errorf("expected a list")
		}
		a := m.newArray(p, t)
		for i, it := range items {
			ev, err := m.FromJSON(p, d.Elem, it)
			if err != nil {
				return Value{}, fmt.Errorf("element %d: %w", i+1, err)
			}
			if th := m.appendElem(a, ev); th != nil {
				return Value{}, fmt.Errorf("element %d: %s", i+1, m.getField(th.obj, m.image.textField).str())
			}
		}
		v = Value{R: a}
	case TMap:
		mp := m.newMap(p, t)
		if p.Types[d.Key].Kind == TString {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				return Value{}, fmt.Errorf("expected an object")
			}
			// Keep the order of the document.
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.Token()
			for dec.More() {
				kt, _ := dec.Token()
				var ignore json.RawMessage
				dec.Decode(&ignore)
				k := kt.(string)
				ev, err := m.FromJSON(p, d.Elem, obj[k])
				if err != nil {
					return Value{}, fmt.Errorf("%s: %w", k, err)
				}
				m.mapSet(mp, Str(k), ev)
			}
		} else {
			var pairs [][2]json.RawMessage
			if err := json.Unmarshal(raw, &pairs); err != nil {
				return Value{}, fmt.Errorf("expected a list of [key, value] pairs")
			}
			for _, pair := range pairs {
				kv, err := m.FromJSON(p, d.Key, pair[0])
				if err != nil {
					return Value{}, err
				}
				ev, err := m.FromJSON(p, d.Elem, pair[1])
				if err != nil {
					return Value{}, err
				}
				m.mapSet(mp, kv, ev)
			}
		}
		v = Value{R: mp}
	default:
		return Value{}, fmt.Errorf("values of this type cannot be read from JSON")
	}
	if d.Named {
		if t := m.check(p, t, v); t != nil {
			return Value{}, fmt.Errorf("%s", m.getField(t.obj, m.image.textField).str())
		}
	}
	return v, nil
}
