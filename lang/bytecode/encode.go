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

package bytecode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// magic starts every encoded program.
var magic = []byte("RTBC")

type writer struct{ b bytes.Buffer }

func (w *writer) int(n int) {
	var buf [binary.MaxVarintLen64]byte
	w.b.Write(buf[:binary.PutVarint(buf[:], int64(n))])
}
func (w *writer) u64(n uint64) {
	var buf [binary.MaxVarintLen64]byte
	w.b.Write(buf[:binary.PutUvarint(buf[:], n)])
}
func (w *writer) str(s string) { w.int(len(s)); w.b.WriteString(s) }
func (w *writer) bytes(b []byte) {
	w.int(len(b))
	w.b.Write(b)
}
func (w *writer) bool(v bool) {
	if v {
		w.int(1)
	} else {
		w.int(0)
	}
}
func (w *writer) strs(ss []string) {
	w.int(len(ss))
	for _, s := range ss {
		w.str(s)
	}
}
func (w *writer) ints(ns []int) {
	w.int(len(ns))
	for _, n := range ns {
		w.int(n)
	}
}

// Encode returns the binary form of a program. The same program always
// yields the same bytes.
func Encode(p *Program) []byte {
	w := &writer{}
	w.b.Write(magic)
	w.int(FormatVersion)
	w.str(p.Package)
	w.int(p.LanguageVersion)
	w.int(len(p.Imports))
	for _, i := range p.Imports {
		w.str(i)
	}
	w.bool(p.Tests)
	w.int(len(p.Files))
	for _, f := range p.Files {
		w.str(f.Path)
		w.str(f.SHA256)
	}
	w.int(len(p.Chains))
	for _, c := range p.Chains {
		w.int(len(c.Sites))
		for _, s := range c.Sites {
			w.str(s.Include)
			w.int(s.File)
			w.int(s.Line)
		}
	}
	w.int(len(p.Strings))
	for _, s := range p.Strings {
		w.str(s)
	}
	w.int(len(p.Bytes))
	for _, b := range p.Bytes {
		w.bytes(b)
	}
	w.int(len(p.Decs))
	for _, d := range p.Decs {
		w.u64(d[0])
		w.u64(d[1])
	}
	w.int(len(p.Types))
	for _, t := range p.Types {
		w.int(int(t.Kind))
		w.bool(t.Optional)
		w.str(t.Name)
		w.int(t.Precision)
		w.int(t.Scale)
		w.int(t.Elem)
		w.int(t.Key)
		w.int(t.Layout)
		w.int(t.Enum)
		w.int(t.Class)
		w.ints(t.KeyFields)
		w.bool(t.Unique)
		w.int(t.MaxLength)
		w.bool(t.NoEmpty)
		w.bool(t.Named)
	}
	w.int(len(p.Layouts))
	for _, l := range p.Layouts {
		w.str(l.Name)
		w.int(l.Size)
		w.int(l.Refs)
		w.int(len(l.Fields))
		for _, f := range l.Fields {
			w.str(f.Name)
			w.int(f.Type)
			w.int(int(f.Kind))
			w.int(f.Offset)
			w.int(f.Bit)
			w.int(f.Size)
			w.int(f.Present)
			w.int(f.PresBit)
			w.int(f.Ref)
			w.bool(f.Optional)
			w.str(f.Default)
			w.bool(f.HasDefault)
		}
	}
	w.int(len(p.Enums))
	for _, e := range p.Enums {
		w.str(e.Name)
		w.int(int(e.Base))
		w.int(len(e.Names))
		for i := range e.Names {
			w.str(e.Names[i])
			w.str(e.Values[i])
		}
	}
	w.int(len(p.Classes))
	for _, c := range p.Classes {
		w.str(c.Name)
		w.bool(c.Interface)
		w.bool(c.Kernel)
		w.bool(c.Test)
		w.bool(c.Exception)
		w.int(c.Base)
		w.int(c.Layout)
		w.int(c.Statics)
		w.int(c.Ctor)
		w.int(c.Init)
		w.ints(c.VTable)
		w.int(len(c.VSlots))
		for _, v := range c.VSlots {
			w.str(v)
		}
		w.int(len(c.ITables))
		for _, it := range c.ITables {
			w.int(it.Interface)
			w.ints(it.Funcs)
		}
		w.ints(c.Supers)
		w.int(len(c.Methods))
		for _, m := range c.Methods {
			w.str(m.Name)
			w.int(m.Func)
			w.bool(m.Static)
			w.bool(m.Public)
			w.bool(m.Test)
			w.strs(m.Capabilities)
		}
	}
	w.int(len(p.Funcs))
	for _, f := range p.Funcs {
		w.str(f.Name)
		w.int(f.Class)
		w.int(f.Params)
		w.int(f.Locals)
		w.bool(f.Instance)
		w.int(f.Captures)
		w.int(len(f.Code))
		for _, c := range f.Code {
			w.int(int(c))
		}
		w.int(len(f.Handlers))
		for _, h := range f.Handlers {
			w.int(h.Start)
			w.int(h.End)
			w.int(h.Target)
			w.int(h.Class)
		}
		w.int(len(f.Lines))
		for _, l := range f.Lines {
			w.int(l.PC)
			w.int(l.File)
			w.int(l.Line)
			w.int(l.Column)
			w.int(l.Chain)
		}
		w.ints(f.ParamTypes)
		w.int(f.Result)
		w.bool(f.Native)
		w.ints(f.Raises)
	}
	w.int(len(p.Queries))
	for _, q := range p.Queries {
		w.int(int(q.Kind))
		w.str(q.Table)
		w.int(q.Row)
		w.strs(q.Key)
		w.strs(q.Sets)
		w.int(q.Where)
		w.int(len(q.Conds))
		for _, c := range q.Conds {
			w.int(int(c.Op))
			w.ints(c.Kids)
			w.str(c.Field)
			w.str(c.Other)
			w.int(c.Param)
		}
		w.int(len(q.Order))
		for _, o := range q.Order {
			w.str(o.Field)
			w.bool(o.Descending)
		}
		w.bool(q.Limit)
		w.ints(q.Params)
	}
	return w.b.Bytes()
}

type reader struct {
	b   []byte
	err error
}

// maxCount limits counts read from untrusted input.
const maxCount = 1 << 24

func (r *reader) int() int {
	if r.err != nil {
		return 0
	}
	n, size := binary.Varint(r.b)
	if size <= 0 {
		r.err = errors.New("bytecode: truncated program")
		return 0
	}
	r.b = r.b[size:]
	return int(n)
}

func (r *reader) u64() uint64 {
	if r.err != nil {
		return 0
	}
	n, size := binary.Uvarint(r.b)
	if size <= 0 {
		r.err = errors.New("bytecode: truncated program")
		return 0
	}
	r.b = r.b[size:]
	return n
}

func (r *reader) count() int {
	n := r.int()
	if n < 0 || n > maxCount || n > len(r.b)+1 {
		if r.err == nil {
			r.err = errors.New("bytecode: invalid count")
		}
		return 0
	}
	return n
}

func (r *reader) raw() []byte {
	n := r.count()
	if r.err != nil {
		return nil
	}
	if n > len(r.b) {
		r.err = errors.New("bytecode: truncated program")
		return nil
	}
	out := append([]byte(nil), r.b[:n]...)
	r.b = r.b[n:]
	return out
}

func (r *reader) str() string { return string(r.raw()) }
func (r *reader) bool() bool  { return r.int() != 0 }
func (r *reader) strs() []string {
	n := r.count()
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := range out {
		out[i] = r.str()
	}
	return out
}
func (r *reader) ints() []int {
	n := r.count()
	if n == 0 {
		return nil
	}
	out := make([]int, n)
	for i := range out {
		out[i] = r.int()
	}
	return out
}

// Decode reads a program written by Encode.
func Decode(b []byte) (*Program, error) {
	if !bytes.HasPrefix(b, magic) {
		return nil, errors.New("bytecode: not a compiled program")
	}
	r := &reader{b: b[len(magic):]}
	if v := r.int(); v != FormatVersion {
		return nil, fmt.Errorf("bytecode: format version %d is not supported, expected %d", v, FormatVersion)
	}
	p := &Program{Package: r.str(), LanguageVersion: r.int()}
	for n := r.count(); n > 0; n-- {
		p.Imports = append(p.Imports, r.str())
	}
	p.Tests = r.bool()
	p.Files = make([]File, r.count())
	for i := range p.Files {
		p.Files[i] = File{Path: r.str(), SHA256: r.str()}
	}
	p.Chains = make([]Chain, r.count())
	for i := range p.Chains {
		n := r.count()
		for range n {
			p.Chains[i].Sites = append(p.Chains[i].Sites, ChainSite{Include: r.str(), File: r.int(), Line: r.int()})
		}
	}
	p.Strings = make([]string, r.count())
	for i := range p.Strings {
		p.Strings[i] = r.str()
	}
	p.Bytes = make([][]byte, r.count())
	for i := range p.Bytes {
		p.Bytes[i] = r.raw()
	}
	p.Decs = make([][2]uint64, r.count())
	for i := range p.Decs {
		p.Decs[i] = [2]uint64{r.u64(), r.u64()}
	}
	p.Types = make([]TypeDesc, r.count())
	for i := range p.Types {
		p.Types[i] = TypeDesc{Kind: TypeKind(r.int()), Optional: r.bool(), Name: r.str(), Precision: r.int(), Scale: r.int(),
			Elem: r.int(), Key: r.int(), Layout: r.int(), Enum: r.int(), Class: r.int(), KeyFields: r.ints(), Unique: r.bool(),
			MaxLength: r.int(), NoEmpty: r.bool(), Named: r.bool()}
	}
	p.Layouts = make([]Layout, r.count())
	for i := range p.Layouts {
		l := Layout{Name: r.str(), Size: r.int(), Refs: r.int()}
		l.Fields = make([]Field, r.count())
		for j := range l.Fields {
			l.Fields[j] = Field{Name: r.str(), Type: r.int(), Kind: FieldKind(r.int()), Offset: r.int(), Bit: r.int(), Size: r.int(),
				Present: r.int(), PresBit: r.int(), Ref: r.int(), Optional: r.bool(), Default: r.str(), HasDefault: r.bool()}
		}
		p.Layouts[i] = l
	}
	p.Enums = make([]Enum, r.count())
	for i := range p.Enums {
		e := Enum{Name: r.str(), Base: TypeKind(r.int())}
		n := r.count()
		for range n {
			e.Names = append(e.Names, r.str())
			e.Values = append(e.Values, r.str())
		}
		p.Enums[i] = e
	}
	p.Classes = make([]Class, r.count())
	for i := range p.Classes {
		c := Class{Name: r.str(), Interface: r.bool(), Kernel: r.bool(), Test: r.bool(), Exception: r.bool(), Base: r.int(), Layout: r.int(),
			Statics: r.int(), Ctor: r.int(), Init: r.int(), VTable: r.ints()}
		for n := r.count(); n > 0; n-- {
			c.VSlots = append(c.VSlots, r.str())
		}
		c.ITables = make([]ITable, r.count())
		for j := range c.ITables {
			c.ITables[j] = ITable{Interface: r.int(), Funcs: r.ints()}
		}
		c.Supers = r.ints()
		c.Methods = make([]MethodRef, r.count())
		for j := range c.Methods {
			c.Methods[j] = MethodRef{Name: r.str(), Func: r.int(), Static: r.bool(), Public: r.bool(), Test: r.bool(), Capabilities: r.strs()}
		}
		p.Classes[i] = c
	}
	p.Funcs = make([]Func, r.count())
	for i := range p.Funcs {
		f := Func{Name: r.str(), Class: r.int(), Params: r.int(), Locals: r.int(), Instance: r.bool(), Captures: r.int()}
		f.Code = make([]int32, r.count())
		for j := range f.Code {
			f.Code[j] = int32(r.int())
		}
		f.Handlers = make([]Handler, r.count())
		for j := range f.Handlers {
			f.Handlers[j] = Handler{Start: r.int(), End: r.int(), Target: r.int(), Class: r.int()}
		}
		f.Lines = make([]Line, r.count())
		for j := range f.Lines {
			f.Lines[j] = Line{PC: r.int(), File: r.int(), Line: r.int(), Column: r.int(), Chain: r.int()}
		}
		f.ParamTypes = r.ints()
		f.Result = r.int()
		f.Native = r.bool()
		f.Raises = r.ints()
		p.Funcs[i] = f
	}
	p.Queries = make([]Query, r.count())
	for i := range p.Queries {
		q := Query{Kind: QueryKind(r.int()), Table: r.str(), Row: r.int(), Key: r.strs(), Sets: r.strs(), Where: r.int()}
		q.Conds = make([]Cond, r.count())
		for j := range q.Conds {
			q.Conds[j] = Cond{Op: CondOp(r.int()), Kids: r.ints(), Field: r.str(), Other: r.str(), Param: r.int()}
		}
		q.Order = make([]OrderKey, r.count())
		for j := range q.Order {
			q.Order[j] = OrderKey{Field: r.str(), Descending: r.bool()}
		}
		q.Limit = r.bool()
		q.Params = r.ints()
		p.Queries[i] = q
	}
	if r.err != nil {
		return nil, r.err
	}
	if len(r.b) != 0 {
		return nil, errors.New("bytecode: unexpected data after the program")
	}
	if err := p.Verify(); err != nil {
		return nil, err
	}
	return p, nil
}

// Verify checks that instructions and the indexes in a program are valid,
// so that a decoded program cannot make the VM read outside its tables.
// References into imports are checked when programs are linked.
func (p *Program) Verify() error {
	in := func(i, n int) bool { return i >= 0 && i < n }
	ref := func(r Ref, n int) bool {
		if RefSlot(r) == 0 {
			return in(RefIndex(r), n)
		}
		return RefSlot(r) <= len(p.Imports) && r >= 0
	}
	for fi := range p.Funcs {
		f := &p.Funcs[fi]
		for pc := 0; pc < len(f.Code); {
			op := Op(f.Code[pc])
			if !op.Valid() || pc+op.Operands() >= len(f.Code) {
				return fmt.Errorf("bytecode: %s: invalid instruction at %d", f.Name, pc)
			}
			arg := func(i int) int { return int(f.Code[pc+1+i]) }
			ok := true
			switch op {
			case CONST_DEC, CONST_UUID:
				ok = in(arg(0), len(p.Decs))
			case CONST_STR:
				ok = in(arg(0), len(p.Strings))
			case CONST_BYTES:
				ok = in(arg(0), len(p.Bytes))
			case ZERO, COPY, EQ_VALUE, CHECK, NEW_ARRAY, NEW_MAP, ARRAY_LIT, MAP_LIT:
				ok = in(arg(0), len(p.Types))
			case CALL_BUILTIN:
				ok = in(arg(2), len(p.Types)) && arg(3) >= -1 && arg(3) < len(p.Types)
			case LOAD, STORE, LOAD_CELL, STORE_CELL, NEW_CELL, PUSH_CELL:
				ok = in(arg(0), f.Locals)
			case LOAD_UP, STORE_UP, PUSH_UP:
				ok = in(arg(0), f.Captures)
			case FOR_TEST, FOR_STEP:
				ok = in(arg(0), f.Locals) && in(arg(1), f.Locals) && in(arg(2), f.Locals) && in(arg(3), len(f.Code)+1)
			case JUMP, JUMP_FALSE, JUMP_TRUE, JUMP_NULL, JUMP_NOTNULL:
				ok = in(arg(0), len(f.Code)+1)
			case NEW_OBJ, STATICS, CALL_IFACE:
				ok = ref(arg(0), len(p.Classes))
			case NEW_STRUCT:
				ok = in(arg(0), len(p.Layouts))
			case CALL:
				ok = ref(arg(0), len(p.Funcs))
			case MAKE_FN:
				ok = in(arg(0), len(p.Funcs))
			case DB_QUERY, DB_CURSOR:
				ok = in(arg(0), len(p.Queries))
			case DB_NEXT:
				ok = in(arg(0), len(f.Code)+1)
			}
			if !ok {
				return fmt.Errorf("bytecode: %s: invalid operand of %s at %d", f.Name, op, pc)
			}
			pc += 1 + op.Operands()
		}
		for _, r := range f.Raises {
			if !ref(r, len(p.Classes)) {
				return fmt.Errorf("bytecode: %s: invalid exception class", f.Name)
			}
		}
		for _, h := range f.Handlers {
			if !in(h.Target, len(f.Code)) || h.Class != -1 && !ref(h.Class, len(p.Classes)) {
				return fmt.Errorf("bytecode: %s: invalid exception handler", f.Name)
			}
		}
	}
	for _, q := range p.Queries {
		ok := in(q.Row, len(p.Types)) && p.Types[q.Row].Kind == TStruct && !p.Types[q.Row].Optional && q.Kind >= QSelect && q.Kind <= QDeleteWhere && (q.Where == -1 || in(q.Where, len(q.Conds)))
		for _, c := range q.Conds {
			for _, k := range c.Kids {
				ok = ok && in(k, len(q.Conds))
			}
			ok = ok && (c.Param == -1 || in(c.Param, len(q.Params)))
		}
		for _, t := range q.Params {
			ok = ok && in(t, len(p.Types))
		}
		if !ok {
			return fmt.Errorf("bytecode: invalid query on %s", q.Table)
		}
	}
	for _, c := range p.Classes {
		for _, v := range c.VTable {
			if !ref(v, len(p.Funcs)) {
				return fmt.Errorf("bytecode: %s: invalid method table", c.Name)
			}
		}
		if len(c.VSlots) != len(c.VTable) {
			return fmt.Errorf("bytecode: %s: invalid method table", c.Name)
		}
	}
	return nil
}
