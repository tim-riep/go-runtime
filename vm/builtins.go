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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/lang/types"
)

func hasReceiver(id int) bool { return types.BuiltinID(id) != types.EnumParse }

// callFn calls a function value.
func (m *Machine) callFn(c *Closure, args ...Value) (Value, *thrown, error) {
	return m.exec(c.Fn, c.Me, args, c)
}

// ---- arrays with keys ----

func (m *Machine) recordKey(a *Array, v Value) string {
	r := v.record()
	var b strings.Builder
	for _, fi := range a.Key {
		f := r.Layout.Fields[fi]
		b.WriteString(keyOf(&r.P.Types[f.Type], m.getField(r, f)))
		b.WriteByte(0)
	}
	return b.String()
}

// reindex rebuilds the key index and reports duplicate keys.
func (m *Machine) reindex(a *Array) *thrown {
	if !a.Unique || !a.dirty && a.index != nil {
		return nil
	}
	a.index = make(map[string]int, a.Len())
	a.dirty = false
	for i := range a.Len() {
		k := m.recordKey(a, a.Get(i))
		if _, dup := a.index[k]; dup {
			a.dirty = true
			return m.fail("/RT/DUPLICATE_KEY", "the key of element %d exists twice", i+1)
		}
		a.index[k] = i
	}
	return nil
}

func (m *Machine) appendElem(a *Array, v Value) *thrown {
	if a.Len() >= m.opts.MaxCollectionLength {
		return m.checkLength(a.Len() + 1)
	}
	if a.Unique && !v.IsNull() {
		if t := m.reindex(a); t != nil {
			return t
		}
		k := m.recordKey(a, v)
		if _, dup := a.index[k]; dup {
			return m.fail("/RT/DUPLICATE_KEY", "an element with this key exists already")
		}
		a.index[k] = a.Len()
	}
	m.alloc(32)
	a.grow()
	a.set(a.Len()-1, v)
	return nil
}

func (m *Machine) setElem(a *Array, i int, v Value) *thrown {
	if a.Unique {
		if t := m.reindex(a); t != nil {
			return t
		}
		k := m.recordKey(a, v)
		if j, dup := a.index[k]; dup && j != i {
			return m.fail("/RT/DUPLICATE_KEY", "an element with this key exists already")
		}
		a.dirty = true
	}
	a.set(i, v)
	return nil
}

func (m *Machine) mapSet(mp *Map, k, v Value) *thrown {
	key := keyOf(&mp.P.Types[mp.Key], k)
	if i, ok := mp.index[key]; ok {
		mp.Vals[i] = v
		return nil
	}
	if len(mp.Keys) >= m.opts.MaxCollectionLength {
		return m.checkLength(len(mp.Keys) + 1)
	}
	m.alloc(64)
	mp.index[key] = len(mp.Keys)
	mp.Keys = append(mp.Keys, k)
	mp.Vals = append(mp.Vals, v)
	return nil
}

// compareKind orders two values of an orderable kind; nulls come first.
func (m *Machine) compareKind(d *TypeDesc, a, b Value) int {
	switch {
	case a.IsNull() && b.IsNull():
		return 0
	case a.IsNull():
		return -1
	case b.IsNull():
		return 1
	}
	switch d.Kind {
	case TString:
		return strings.Compare(a.str(), b.str())
	case TDec:
		return dec.Cmp(a.dec(), b.dec())
	}
	return cmp3(a.int() < b.int(), a.int() > b.int())
}

func (m *Machine) elems(a *Array) []Value {
	out := make([]Value, a.Len())
	for i := range out {
		out[i] = a.Get(i)
	}
	return out
}

func (m *Machine) fromElems(like *Array, elem int, vals []Value) *Array {
	a := m.newArrayOf(like.P, elem)
	a.Key, a.Unique, a.dirty = like.Key, like.Unique, true
	for _, v := range vals {
		a.grow()
		a.set(a.Len()-1, v)
	}
	m.alloc(int64(32 * len(vals)))
	return a
}

// builtin runs a built-in method. vals holds the receiver, if any, and the
// arguments.
func (m *Machine) builtin(p *Program, id, recvT, resT int, vals []Value) (Value, *thrown, error) {
	b := types.BuiltinID(id)
	var recv Value
	args := vals
	if hasReceiver(id) {
		recv, args = vals[0], vals[1:]
		if recv.IsNull() {
			return Value{}, m.fail("/RT/VALUE_ERROR", "method called on null"), nil
		}
	}
	switch b {
	case types.ArrLength:
		return Int(int64(recv.array().Len())), nil, nil
	case types.ArrAppend:
		return Value{}, m.appendElem(recv.array(), args[0]), nil
	case types.ArrInsert:
		a := recv.array()
		i := args[0].int()
		if i < 1 || i > int64(a.Len())+1 {
			return Value{}, m.fail("/RT/INDEX_ERROR", "position %d is outside of 1 to %d", i, a.Len()+1), nil
		}
		vs := m.elems(a)
		vs = append(vs[:i-1], append([]Value{args[1]}, vs[i-1:]...)...)
		na := m.fromElems(a, a.Elem, vs)
		if t := m.reindex(na); t != nil {
			return Value{}, t, nil
		}
		*a = *na
		return Value{}, nil, nil
	case types.ArrRemove:
		a := recv.array()
		i := args[0].int()
		if i < 1 || i > int64(a.Len()) {
			return Value{}, m.fail("/RT/INDEX_ERROR", "index %d is outside of 1 to %d", i, a.Len()), nil
		}
		vs := m.elems(a)
		*a = *m.fromElems(a, a.Elem, append(vs[:i-1], vs[i:]...))
		return Value{}, nil, nil
	case types.ArrClear:
		a := recv.array()
		*a = *m.fromElems(a, a.Elem, nil)
		return Value{}, nil, nil
	case types.ArrContains, types.ArrIndexOf:
		a := recv.array()
		for i := range a.Len() {
			if m.equal(a.P, a.Elem, a.Get(i), args[0]) {
				if b == types.ArrContains {
					return Bool(true), nil, nil
				}
				return Int(int64(i + 1)), nil, nil
			}
		}
		if b == types.ArrContains {
			return Bool(false), nil, nil
		}
		return Null, nil, nil
	case types.ArrFind, types.ArrFilter, types.ArrAny, types.ArrAll:
		a := recv.array()
		f := args[0].R.(*Closure)
		var keep []Value
		for _, v := range m.elems(a) {
			r, t, err := m.callFn(f, m.copyValue(a.P, a.Elem, v))
			if t != nil || err != nil {
				return Value{}, t, err
			}
			switch {
			case b == types.ArrFind && r.bool():
				return m.copyValue(a.P, a.Elem, v), nil, nil
			case b == types.ArrAny && r.bool():
				return Bool(true), nil, nil
			case b == types.ArrAll && !r.bool():
				return Bool(false), nil, nil
			case b == types.ArrFilter && r.bool():
				keep = append(keep, m.copyValue(a.P, a.Elem, v))
			}
		}
		switch b {
		case types.ArrFind:
			return Null, nil, nil
		case types.ArrAny:
			return Bool(false), nil, nil
		case types.ArrAll:
			return Bool(true), nil, nil
		}
		na := m.fromElems(a, a.Elem, keep)
		return Value{R: na}, nil, nil
	case types.ArrMap:
		a := recv.array()
		f := args[0].R.(*Closure)
		out := m.newArrayOf(p, p.Types[resT].Elem)
		for _, v := range m.elems(a) {
			r, t, err := m.callFn(f, m.copyValue(a.P, a.Elem, v))
			if t != nil || err != nil {
				return Value{}, t, err
			}
			if t := m.appendElem(out, r); t != nil {
				return Value{}, t, nil
			}
		}
		return Value{R: out}, nil, nil
	case types.ArrSort, types.ArrSortBy:
		a := recv.array()
		vs := m.elems(a)
		keys := vs
		kd := &a.P.Types[a.Elem]
		if b == types.ArrSortBy {
			f := args[0].R.(*Closure)
			keys = make([]Value, len(vs))
			for i, v := range vs {
				r, t, err := m.callFn(f, m.copyValue(a.P, a.Elem, v))
				if t != nil || err != nil {
					return Value{}, t, err
				}
				keys[i] = r
			}
			kd = &f.Fn.Prog.P.Types[f.Fn.Def.Result]
		}
		idx := make([]int, len(vs))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { return m.compareKind(kd, keys[idx[i]], keys[idx[j]]) < 0 })
		sorted := make([]Value, len(vs))
		for i, k := range idx {
			sorted[i] = m.copyValue(a.P, a.Elem, vs[k])
		}
		return Value{R: m.fromElems(a, a.Elem, sorted)}, nil, nil
	case types.ArrReverse:
		a := recv.array()
		vs := m.elems(a)
		out := make([]Value, len(vs))
		for i, v := range vs {
			out[len(vs)-1-i] = m.copyValue(a.P, a.Elem, v)
		}
		return Value{R: m.fromElems(a, a.Elem, out)}, nil, nil
	case types.ArrSlice:
		a := recv.array()
		from, to := args[0].int(), args[1].int()
		if from < 1 || from > int64(a.Len())+1 || to < from-1 || to > int64(a.Len()) {
			return Value{}, m.fail("/RT/INDEX_ERROR", "SLICE(%d, %d) is outside of 1 to %d", from, to, a.Len()), nil
		}
		vs := m.elems(a)[from-1 : to]
		out := make([]Value, len(vs))
		for i, v := range vs {
			out[i] = m.copyValue(a.P, a.Elem, v)
		}
		return Value{R: m.fromElems(a, a.Elem, out)}, nil, nil
	case types.ArrJoin:
		a := recv.array()
		parts := make([]string, a.Len())
		for i := range parts {
			parts[i] = a.Get(i).str()
		}
		return Str(strings.Join(parts, args[0].str())), nil, nil
	case types.ArrLookup:
		a := recv.array()
		if t := m.reindex(a); t != nil {
			return Value{}, t, nil
		}
		l := &a.P.Layouts[a.P.Types[a.Elem].Layout]
		var k strings.Builder
		for i, fi := range a.Key {
			k.WriteString(keyOf(&a.P.Types[l.Fields[fi].Type], args[i]))
			k.WriteByte(0)
		}
		i, ok := a.index[k.String()]
		if !ok {
			return Null, nil, nil
		}
		return m.copyValue(a.P, a.Elem, a.Get(i)), nil, nil

	case types.MapLength:
		return Int(int64(len(recv.mapv().Keys))), nil, nil
	case types.MapGet, types.MapContains:
		mp := recv.mapv()
		i, ok := mp.index[keyOf(&mp.P.Types[mp.Key], args[0])]
		if b == types.MapContains {
			return Bool(ok), nil, nil
		}
		if !ok {
			return Null, nil, nil
		}
		return m.copyValue(mp.P, mp.Elem, mp.Vals[i]), nil, nil
	case types.MapSet:
		return Value{}, m.mapSet(recv.mapv(), args[0], args[1]), nil
	case types.MapRemove:
		mp := recv.mapv()
		key := keyOf(&mp.P.Types[mp.Key], args[0])
		i, ok := mp.index[key]
		if !ok {
			return Value{}, nil, nil
		}
		mp.Keys = append(mp.Keys[:i], mp.Keys[i+1:]...)
		mp.Vals = append(mp.Vals[:i], mp.Vals[i+1:]...)
		mp.index = make(map[string]int, len(mp.Keys))
		for j, k := range mp.Keys {
			mp.index[keyOf(&mp.P.Types[mp.Key], k)] = j
		}
		return Value{}, nil, nil
	case types.MapKeys, types.MapValues:
		mp := recv.mapv()
		src, elem := mp.Keys, mp.Key
		if b == types.MapValues {
			src, elem = mp.Vals, mp.Elem
		}
		out := m.newArrayOf(mp.P, elem)
		for _, v := range src {
			out.grow()
			out.set(out.Len()-1, m.copyValue(mp.P, elem, v))
		}
		return Value{R: out}, nil, nil
	case types.MapClear:
		mp := recv.mapv()
		mp.Keys, mp.Vals, mp.index = nil, nil, map[string]int{}
		return Value{}, nil, nil

	case types.StrLength:
		return Int(int64(utf8.RuneCountInString(recv.str()))), nil, nil
	case types.StrUpper:
		return Str(strings.ToUpper(recv.str())), nil, nil
	case types.StrLower:
		return Str(strings.ToLower(recv.str())), nil, nil
	case types.StrTrim:
		return Str(strings.TrimSpace(recv.str())), nil, nil
	case types.StrContains:
		return Bool(strings.Contains(recv.str(), args[0].str())), nil, nil
	case types.StrStartsWith:
		return Bool(strings.HasPrefix(recv.str(), args[0].str())), nil, nil
	case types.StrEndsWith:
		return Bool(strings.HasSuffix(recv.str(), args[0].str())), nil, nil
	case types.StrIndexOf:
		s := recv.str()
		i := strings.Index(s, args[0].str())
		if i < 0 {
			return Null, nil, nil
		}
		return Int(int64(utf8.RuneCountInString(s[:i]) + 1)), nil, nil
	case types.StrSubstring:
		r := []rune(recv.str())
		start, n := args[0].int(), args[1].int()
		if start < 1 || n < 0 || start-1+n > int64(len(r)) {
			return Value{}, m.fail("/RT/INDEX_ERROR", "SUBSTRING(%d, %d) is outside of a string of %d characters", start, n, len(r)), nil
		}
		return Str(string(r[start-1 : start-1+n])), nil, nil
	case types.StrReplace:
		if args[0].str() == "" {
			return Value{}, m.fail("/RT/VALUE_ERROR", "REPLACE needs a text to search for"), nil
		}
		out := strings.ReplaceAll(recv.str(), args[0].str(), args[1].str())
		if t := m.checkLength(len(out)); t != nil {
			return Value{}, t, nil
		}
		return Str(out), nil, nil
	case types.StrSplit:
		if args[0].str() == "" {
			return Value{}, m.fail("/RT/VALUE_ERROR", "SPLIT needs a separator"), nil
		}
		out := m.newArrayOf(p, p.Types[resT].Elem)
		for _, part := range strings.Split(recv.str(), args[0].str()) {
			out.grow()
			out.set(out.Len()-1, Str(part))
		}
		return Value{R: out}, nil, nil
	case types.BytesLength:
		bs, _ := recv.R.([]byte)
		return Int(int64(len(bs))), nil, nil

	case types.DateAddDays:
		n, ok := addInt(recv.int(), args[0].int())
		if !ok || !fitsInt4(n) {
			return Value{}, m.fail("/RT/OVERFLOW", "the date is out of range"), nil
		}
		return Int(n), nil, nil
	case types.DateDaysUntil:
		return Int(args[0].int() - recv.int()), nil, nil
	case types.DateYear, types.DateMonth, types.DateDay:
		t := time.Unix(recv.int()*86400, 0).UTC()
		switch b {
		case types.DateYear:
			return Int(int64(t.Year())), nil, nil
		case types.DateMonth:
			return Int(int64(t.Month())), nil, nil
		}
		return Int(int64(t.Day())), nil, nil
	case types.TsDate:
		us := recv.int()
		days := us / 86_400_000_000
		if us < 0 && us%86_400_000_000 != 0 {
			days--
		}
		return Int(days), nil, nil

	case types.EnumParse:
		e := &p.Enums[p.Types[recvT].Enum]
		text := m.format(p, e.Base, 0, args[0])
		for i, v := range e.Values {
			if v == text {
				return Int(int64(i)), nil, nil
			}
		}
		return Null, nil, nil
	case types.EnumValueOf:
		e := &p.Enums[p.Types[recvT].Enum]
		v := e.Values[recv.int()]
		if e.Base == TString {
			return Str(v), nil, nil
		}
		n, _ := parseInt(v)
		return Int(n), nil, nil
	case types.EnumName:
		e := &p.Enums[p.Types[recvT].Enum]
		return Str(e.Names[recv.int()]), nil, nil
	}
	return Value{}, nil, errUnknownBuiltin(id)
}
