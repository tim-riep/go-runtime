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
	"encoding/binary"
	"fmt"
	"math/bits"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
)

// Value is a value on the operand stack or in a local. Integers, dates,
// timestamps, enum constants and BOOL are kept in N; DEC and UUID use N
// and X; strings, BYTES, records, collections, functions and cells are
// in R. null is a distinct R, so "" and null stay different.
type Value struct {
	N, X uint64
	R    any
}

type nullType struct{}

var nullRef = &nullType{}

// Null is the value null.
var Null = Value{R: nullRef}

// IsNull reports null.
func (v Value) IsNull() bool { return v.R == nullRef }

// Int returns an integer value.
func Int(n int64) Value { return Value{N: uint64(n)} }

// Bool returns a BOOL value.
func Bool(b bool) Value {
	if b {
		return Value{N: 1}
	}
	return Value{}
}

// Str returns a STRING value.
func Str(s string) Value { return Value{R: s} }

// DecValue returns a DEC value from its unscaled representation.
func DecValue(d dec.Dec) Value { return Value{N: d.Lo, X: d.Hi} }

func (v Value) int() int64      { return int64(v.N) }
func (v Value) bool() bool      { return v.N != 0 }
func (v Value) str() string     { s, _ := v.R.(string); return s }
func (v Value) dec() dec.Dec    { return dec.Dec{Lo: v.N, Hi: v.X} }
func (v Value) record() *Record { r, _ := v.R.(*Record); return r }
func (v Value) array() *Array   { a, _ := v.R.(*Array); return a }
func (v Value) mapv() *Map      { m, _ := v.R.(*Map); return m }

// Record is an object of a class or a value of a struct: fixed size
// values and bits in Data, references in Refs, both laid out by the
// compiler.
type Record struct {
	Class  *LinkedClass // nil for structs
	Layout *bytecode.Layout
	P      *bytecode.Program // the program whose types the layout refers to
	Data   []byte
	Refs   []any
	// Native is the Go value of an object of a native class; released
	// tells that it was freed.
	Native   any
	released bool
}

// Cell holds a local variable captured by a function literal.
type Cell struct{ V Value }

// Closure is a function value.
type Closure struct {
	Fn    *LinkedFunc
	Cells []*Cell
	Me    Value
}

// Array is an array. Arrays of BOOL are bit sets. Arrays of array types
// with a unique key keep an index of the positions by key.
type Array struct {
	P      *bytecode.Program
	Elem   int // TypeDesc of the elements in P
	Vals   []Value
	Bits   []uint64 // BOOL: values
	Nulls  []uint64 // BOOL?: set bit means present
	N      int      // length of bit sets
	IsBits bool
	Key    []int // key fields of struct elements
	Unique bool
	index  map[string]int
	dirty  bool
}

// Map is a map that keeps the order of insertion.
type Map struct {
	P         *bytecode.Program
	Key, Elem int
	Keys      []Value
	Vals      []Value
	index     map[string]int
}

// ---- data access ----

func getBit(data []byte, off, bit int) bool { return data[off]&(1<<bit) != 0 }

func setBit(data []byte, off, bit int, on bool) {
	if on {
		data[off] |= 1 << bit
	} else {
		data[off] &^= 1 << bit
	}
}

// ---- arrays ----

// Len returns the number of elements.
func (a *Array) Len() int {
	if a.IsBits {
		return a.N
	}
	return len(a.Vals)
}

// Get returns element i (0-based).
func (a *Array) Get(i int) Value {
	if !a.IsBits {
		return a.Vals[i]
	}
	if a.Nulls != nil && a.Nulls[i/64]&(1<<(i%64)) == 0 {
		return Null
	}
	return Bool(a.Bits[i/64]&(1<<(i%64)) != 0)
}

// set stores element i without index maintenance.
func (a *Array) set(i int, v Value) {
	if !a.IsBits {
		a.Vals[i] = v
		return
	}
	w, b := i/64, uint64(1)<<(i%64)
	if a.Nulls != nil {
		if v.IsNull() {
			a.Nulls[w] &^= b
			a.Bits[w] &^= b
			return
		}
		a.Nulls[w] |= b
	}
	if v.bool() {
		a.Bits[w] |= b
	} else {
		a.Bits[w] &^= b
	}
}

func (a *Array) grow() {
	if a.IsBits {
		if a.N%64 == 0 {
			a.Bits = append(a.Bits, 0)
			if a.Nulls != nil {
				a.Nulls = append(a.Nulls, 0)
			}
		}
		a.N++
		return
	}
	a.Vals = append(a.Vals, Value{})
}

func (a *Array) shrink() {
	if a.IsBits {
		a.N--
		return
	}
	a.Vals = a.Vals[:len(a.Vals)-1]
}

// ---- keys ----

// keyOf encodes values of elementary types and enums as map keys.
func keyOf(d *bytecode.TypeDesc, v Value) string {
	if v.IsNull() {
		return "n"
	}
	switch d.Kind {
	case bytecode.TString:
		return "s" + v.str()
	case bytecode.TDec, bytecode.TUUID:
		return "d" + strconv.FormatUint(v.N, 16) + ":" + strconv.FormatUint(v.X, 16)
	case bytecode.TBytes:
		b, _ := v.R.([]byte)
		return "b" + string(b)
	}
	return "i" + strconv.FormatInt(v.int(), 10)
}

// ---- machine helpers for values ----

// zero returns the zero value of type t of program p: "" for STRING, 0
// for numbers, false, empty collections and structs of zero values.
// Optional types and objects have null.
func (m *Machine) zero(p *bytecode.Program, t int) Value {
	d := &p.Types[t]
	if d.Optional {
		return Null
	}
	switch d.Kind {
	case bytecode.TString:
		return Str("")
	case bytecode.TBytes:
		return Value{R: []byte{}}
	case bytecode.TStruct:
		return Value{R: m.newRecord(nil, p, &p.Layouts[d.Layout])}
	case bytecode.TArray:
		return Value{R: m.newArray(p, t)}
	case bytecode.TMap:
		return Value{R: m.newMap(p, t)}
	case bytecode.TObject, bytecode.TFunc:
		return Null
	}
	return Value{}
}

// newRecord creates an object of a class or a struct value with zero
// values; l and its field types belong to program p.
func (m *Machine) newRecord(class *LinkedClass, p *bytecode.Program, l *bytecode.Layout) *Record {
	r := &Record{Class: class, Layout: l, P: p, Data: make([]byte, l.Size), Refs: make([]any, l.Refs)}
	m.alloc(int64(l.Size + 16*l.Refs + 48))
	for _, f := range l.Fields {
		if f.Kind == bytecode.FRef && !f.Optional {
			r.Refs[f.Ref] = m.zero(p, f.Type).R
		}
		if f.HasDefault {
			if v, err := m.fromCanonical(p, f.Type, f.Default); err == nil {
				m.setField(r, f, v)
			}
		}
	}
	return r
}

// newObject creates an object of a class.
func (m *Machine) newObject(c *LinkedClass) *Record { return m.newRecord(c, c.Prog.P, c.Layout) }

func (m *Machine) newArray(p *bytecode.Program, t int) *Array {
	d := &p.Types[t]
	a := m.newArrayOf(p, d.Elem)
	a.Key, a.Unique = d.KeyFields, d.Unique
	m.alloc(64)
	return a
}

// newArrayOf creates an array with elements of type elem of program p.
func (m *Machine) newArrayOf(p *bytecode.Program, elem int) *Array {
	a := &Array{P: p, Elem: elem}
	if elem >= 0 && p.Types[elem].Kind == bytecode.TBool {
		a.IsBits = true
		if p.Types[elem].Optional {
			a.Nulls = []uint64{}
		}
	}
	return a
}

func (m *Machine) newMap(p *bytecode.Program, t int) *Map {
	d := &p.Types[t]
	m.alloc(64)
	return &Map{P: p, Key: d.Key, Elem: d.Elem, index: map[string]int{}}
}

// refValue turns a reference slot into a value.
func refValue(r any) Value {
	if r == nil {
		return Null
	}
	return Value{R: r}
}

// copyValue returns a deep copy of a value of a struct, array or map
// type t of program p; other values are returned unchanged.
func (m *Machine) copyValue(p *bytecode.Program, t int, v Value) Value {
	if v.IsNull() || t < 0 {
		return v
	}
	switch p.Types[t].Kind {
	case bytecode.TStruct:
		return m.copyRecord(v)
	case bytecode.TArray:
		src := v.array()
		a := &Array{P: src.P, Elem: src.Elem, IsBits: src.IsBits, N: src.N, Key: src.Key, Unique: src.Unique, dirty: true}
		if src.IsBits {
			a.Bits = append([]uint64(nil), src.Bits...)
			if src.Nulls != nil {
				a.Nulls = append([]uint64{}, src.Nulls...)
			}
		} else {
			a.Vals = make([]Value, len(src.Vals))
			for i, e := range src.Vals {
				a.Vals[i] = m.copyValue(src.P, src.Elem, e)
			}
		}
		m.alloc(int64(32 * a.Len()))
		return Value{R: a}
	case bytecode.TMap:
		src := v.mapv()
		mp := &Map{P: src.P, Key: src.Key, Elem: src.Elem, Keys: append([]Value(nil), src.Keys...), Vals: make([]Value, len(src.Vals)), index: make(map[string]int, len(src.index))}
		for i, e := range src.Vals {
			mp.Vals[i] = m.copyValue(src.P, src.Elem, e)
		}
		for k, i := range src.index {
			mp.index[k] = i
		}
		m.alloc(int64(64 * len(mp.Keys)))
		return Value{R: mp}
	}
	return v
}

func (m *Machine) copyRecord(v Value) Value {
	src := v.record()
	r := &Record{Layout: src.Layout, P: src.P, Data: append([]byte(nil), src.Data...), Refs: make([]any, len(src.Refs))}
	m.alloc(int64(len(r.Data) + 16*len(r.Refs) + 48))
	for _, f := range src.Layout.Fields {
		if f.Kind == bytecode.FRef && src.Refs[f.Ref] != nil {
			r.Refs[f.Ref] = m.copyValue(src.P, f.Type, Value{R: src.Refs[f.Ref]}).R
		}
	}
	return Value{R: r}
}

// equal compares two values of type t of program p.
func (m *Machine) equal(p *bytecode.Program, t int, a, b Value) bool {
	if a.IsNull() || b.IsNull() {
		return a.IsNull() == b.IsNull()
	}
	switch p.Types[t].Kind {
	case bytecode.TString:
		return a.str() == b.str()
	case bytecode.TBytes:
		x, _ := a.R.([]byte)
		y, _ := b.R.([]byte)
		return bytes.Equal(x, y)
	case bytecode.TDec, bytecode.TUUID:
		return a.N == b.N && a.X == b.X
	case bytecode.TStruct:
		x, y := a.record(), b.record()
		for _, f := range x.Layout.Fields {
			if !m.equal(x.P, f.Type, m.getField(x, f), m.getField(y, f)) {
				return false
			}
		}
		return true
	case bytecode.TArray:
		x, y := a.array(), b.array()
		if x.Len() != y.Len() {
			return false
		}
		for i := range x.Len() {
			if !m.equal(x.P, x.Elem, x.Get(i), y.Get(i)) {
				return false
			}
		}
		return true
	case bytecode.TMap:
		x, y := a.mapv(), b.mapv()
		if len(x.Keys) != len(y.Keys) {
			return false
		}
		kd := &x.P.Types[x.Key]
		for i, k := range x.Keys {
			j, ok := y.index[keyOf(kd, k)]
			if !ok || !m.equal(x.P, x.Elem, x.Vals[i], y.Vals[j]) {
				return false
			}
		}
		return true
	case bytecode.TObject, bytecode.TFunc:
		return a.R == b.R
	}
	return a.N == b.N
}

// getField reads a field of a record as value.
func (m *Machine) getField(r *Record, f bytecode.Field) Value {
	switch f.Kind {
	case bytecode.FBool:
		return Bool(getBit(r.Data, f.Offset, f.Bit))
	case bytecode.FOptBool:
		if !getBit(r.Data, f.Offset, f.Bit) {
			return Null
		}
		return Bool(getBit(r.Data, f.Offset, f.Bit+1))
	case bytecode.FRef:
		if f.Present >= 0 && !getBit(r.Data, f.Present, f.PresBit) {
			return Null
		}
		return refValue(r.Refs[f.Ref])
	}
	if f.Present >= 0 && !getBit(r.Data, f.Present, f.PresBit) {
		return Null
	}
	return readFixed(r.Data, f.Offset, r.P.Types[f.Type].Kind)
}

// setField writes a field of a record.
func (m *Machine) setField(r *Record, f bytecode.Field, v Value) {
	switch f.Kind {
	case bytecode.FBool:
		setBit(r.Data, f.Offset, f.Bit, v.bool())
		return
	case bytecode.FOptBool:
		setBit(r.Data, f.Offset, f.Bit, !v.IsNull())
		setBit(r.Data, f.Offset, f.Bit+1, !v.IsNull() && v.bool())
		return
	case bytecode.FRef:
		if f.Present >= 0 {
			setBit(r.Data, f.Present, f.PresBit, !v.IsNull())
		}
		if v.IsNull() {
			r.Refs[f.Ref] = nil
		} else {
			r.Refs[f.Ref] = v.R
		}
		return
	}
	if f.Present >= 0 {
		setBit(r.Data, f.Present, f.PresBit, !v.IsNull())
		if v.IsNull() {
			return
		}
	}
	writeFixed(r.Data, f.Offset, r.P.Types[f.Type].Kind, v)
}

func readFixed(data []byte, off int, k bytecode.TypeKind) Value {
	le := binary.LittleEndian
	switch k {
	case bytecode.TInt4, bytecode.TDate:
		return Int(int64(int32(le.Uint32(data[off:]))))
	case bytecode.TInt8, bytecode.TTimestamp:
		return Int(int64(le.Uint64(data[off:])))
	case bytecode.TDec, bytecode.TUUID:
		return Value{N: le.Uint64(data[off:]), X: le.Uint64(data[off+8:])}
	case bytecode.TEnum:
		return Int(int64(le.Uint16(data[off:])))
	}
	return Value{}
}

func writeFixed(data []byte, off int, k bytecode.TypeKind, v Value) {
	le := binary.LittleEndian
	switch k {
	case bytecode.TInt4, bytecode.TDate:
		le.PutUint32(data[off:], uint32(int32(v.int())))
	case bytecode.TInt8, bytecode.TTimestamp:
		le.PutUint64(data[off:], v.N)
	case bytecode.TDec, bytecode.TUUID:
		le.PutUint64(data[off:], v.N)
		le.PutUint64(data[off+8:], v.X)
	case bytecode.TEnum:
		le.PutUint16(data[off:], uint16(v.N))
	}
}

// ---- integer arithmetic with range checks ----

func addInt(a, b int64) (int64, bool) {
	s := a + b
	return s, (s > a) == (b > 0)
}

func subInt(a, b int64) (int64, bool) {
	s := a - b
	return s, (s < a) == (b > 0)
}

func mulInt(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	hi, lo := bits.Mul64(uint64(abs(a)), uint64(abs(b)))
	neg := (a < 0) != (b < 0)
	if hi != 0 || lo > 1<<63 || lo == 1<<63 && !neg {
		return 0, false
	}
	if neg {
		return -int64(lo), true
	}
	return int64(lo), true
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func fitsInt4(n int64) bool { return n >= -1<<31 && n < 1<<31 }

// ---- formatting ----

func formatUUID(v Value) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], v.X)
	binary.BigEndian.PutUint64(b[8:], v.N)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func putUUID(b []byte, v Value) {
	binary.BigEndian.PutUint64(b[:8], v.X)
	binary.BigEndian.PutUint64(b[8:], v.N)
}

func uuidValue(b []byte) Value {
	return Value{X: binary.BigEndian.Uint64(b[:8]), N: binary.BigEndian.Uint64(b[8:])}
}

func parseUUID(s string) (Value, bool) {
	h := strings.ReplaceAll(s, "-", "")
	if len(h) != 32 || len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return Value{}, false
	}
	hi, err1 := strconv.ParseUint(h[:16], 16, 64)
	lo, err2 := strconv.ParseUint(h[16:], 16, 64)
	if err1 != nil || err2 != nil {
		return Value{}, false
	}
	return Value{N: lo, X: hi}, true
}
