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

// Package compile turns a checked program into bytecode: the memory
// layout of objects and structs, method tables, and the instructions of
// every method with exception and line tables.
package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
)

// Options control compilation.
type Options struct {
	// Tests includes test classes, for rt test. Shipped programs have none.
	Tests bool
	// Sources maps file paths to their text, for the hashes in the file
	// table.
	Sources map[string]string
	// LanguageVersion of the package.
	LanguageVersion int
}

type compiler struct {
	tp      *types.Program
	info    *types.Info
	opts    Options
	out     *bytecode.Program
	classes map[*types.Class]int
	ctors   map[*types.Class]int
	inits   map[*types.Class]int
	funcs   map[*types.Method]int
	vslot   map[*types.Method]int
	ifaces  map[*types.Class][]string // methods of an interface, including extended ones
	layouts map[*types.Struct]int
	inst    map[*types.Class]int // instance layout
	statics map[*types.Class]int
	enums   map[*types.Enum]int
	tdescs  map[string]int
	strs    map[string]int
	decs    map[[2]uint64]int
	files   map[string]int
	chains  map[string]int
	pending []pendingFunc
}

// pendingFunc is a function whose code is generated after all indexes
// exist.
type pendingFunc struct {
	idx   int
	gen   func(f *fn)
	class *types.Class
	site  types.Site
}

// Compile compiles a checked program without errors.
func Compile(tp *types.Program, opts Options) (prog *bytecode.Program, err error) {
	c := &compiler{tp: tp, info: tp.Info, opts: opts, classes: map[*types.Class]int{}, ctors: map[*types.Class]int{}, inits: map[*types.Class]int{},
		funcs: map[*types.Method]int{}, vslot: map[*types.Method]int{}, ifaces: map[*types.Class][]string{}, layouts: map[*types.Struct]int{},
		inst: map[*types.Class]int{}, statics: map[*types.Class]int{}, enums: map[*types.Enum]int{}, tdescs: map[string]int{},
		strs: map[string]int{}, decs: map[[2]uint64]int{}, files: map[string]int{}, chains: map[string]int{}}
	c.out = &bytecode.Program{Package: string(tp.Package), LanguageVersion: opts.LanguageVersion, Tests: opts.Tests,
		Chains: []bytecode.Chain{{}}}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(compileError); ok {
				prog, err = nil, e
				return
			}
			panic(r)
		}
	}()
	var classes []*types.Class
	for _, k := range tp.Classes {
		if k.Test && !opts.Tests {
			continue
		}
		classes = append(classes, k)
	}
	for i, k := range classes {
		c.classes[k] = i
		c.out.Classes = append(c.out.Classes, bytecode.Class{Name: k.Name, Interface: k.IsInterface(), Kernel: k.Kernel, Test: k.Test,
			Exception: k.IsException(), Base: -1, Layout: -1, Statics: -1, Ctor: -1, Init: -1})
	}
	// Bases first, so that subclasses extend the layouts and method tables
	// of their bases.
	ordered := slices.Clone(classes)
	sort.SliceStable(ordered, func(i, j int) bool { return depth(ordered[i]) < depth(ordered[j]) })
	for _, k := range ordered {
		c.declareClass(k)
	}
	for _, k := range ordered {
		c.itables(k)
	}
	for len(c.pending) > 0 {
		p := c.pending[0]
		c.pending = c.pending[1:]
		f := newFn(c, p.idx, p.class, p.site)
		p.gen(f)
		f.finish()
	}
	return c.out, nil
}

type compileError struct{ msg string }

func (e compileError) Error() string { return "compile: " + e.msg }

func (c *compiler) failf(format string, args ...any) {
	panic(compileError{fmt.Sprintf(format, args...)})
}

func depth(k *types.Class) int {
	n := 0
	for b := k.Base; b != nil; b = b.Base {
		n++
	}
	return n
}

// newFunc reserves a function and schedules its code.
func (c *compiler) newFunc(name string, class *types.Class, site types.Site, gen func(f *fn)) int {
	idx := len(c.out.Funcs)
	c.out.Funcs = append(c.out.Funcs, bytecode.Func{Name: name, Class: c.classes[class], Result: -1})
	c.pending = append(c.pending, pendingFunc{idx: idx, gen: gen, class: class, site: site})
	return idx
}

func (c *compiler) declareClass(k *types.Class) {
	ci := c.classes[k]
	bc := &c.out.Classes[ci]
	if k.Base != nil {
		bc.Base = c.classes[k.Base]
	}
	bc.Supers = append(bc.Supers, ci)
	for b := k.Base; b != nil; b = b.Base {
		bc.Supers = append(bc.Supers, c.classes[b])
	}
	for _, i := range allInterfaces(k) {
		if idx, ok := c.classes[i]; ok && !slices.Contains(bc.Supers, idx) {
			bc.Supers = append(bc.Supers, idx)
		}
	}
	if k.IsInterface() {
		c.ifaces[k] = interfaceMethods(k)
		return
	}
	// Instance layout: the fields of the base class keep their places.
	var base *bytecode.Layout
	if k.Base != nil {
		base = &c.out.Layouts[c.inst[k.Base]]
	}
	var own, statics []fieldSpec
	for _, a := range k.Attributes {
		if a.Static {
			statics = append(statics, fieldSpec{a.Name, a.Type})
		} else {
			own = append(own, fieldSpec{a.Name, a.Type})
		}
	}
	c.inst[k] = c.addLayout(buildLayout(c, k.Name, base, own))
	bc = &c.out.Classes[ci]
	bc.Layout = c.inst[k]
	if len(statics) > 0 {
		c.statics[k] = c.addLayout(buildLayout(c, k.Name+" static", nil, statics))
		c.out.Classes[ci].Statics = c.statics[k]
	}
	// Method table: inherited slots, overriding methods replace them, open
	// methods get new ones.
	var vt []int
	if k.Base != nil {
		vt = slices.Clone(c.out.Classes[c.classes[k.Base]].VTable)
	}
	for _, m := range k.Methods {
		idx := c.newFunc(k.Name+"."+m.Name, k, m.Site, func(f *fn) { f.method(m) })
		c.funcs[m] = idx
		c.out.Classes[ci].Methods = append(c.out.Classes[ci].Methods, bytecode.MethodRef{Name: m.Name, Func: idx, Static: m.Static,
			Public: m.Visibility == syntax.Public, Test: m.Test})
		switch {
		case m.Override && m.Overrides != nil:
			slot := c.vslot[rootMethod(m)]
			c.vslot[m] = slot
			vt[slot] = idx
		case m.Open:
			c.vslot[m] = len(vt)
			vt = append(vt, idx)
		}
	}
	// Inherited methods callable on this class by name.
	if k.Base != nil {
		for _, m := range c.out.Classes[c.classes[k.Base]].Methods {
			if !slices.ContainsFunc(c.out.Classes[ci].Methods, func(o bytecode.MethodRef) bool { return o.Name == m.Name }) {
				c.out.Classes[ci].Methods = append(c.out.Classes[ci].Methods, m)
			}
		}
	}
	c.out.Classes[ci].VTable = vt
	site := types.Site{File: k.File}
	ctorSite := site
	if k.Ctor != nil {
		ctorSite = k.Ctor.Site
	}
	c.ctors[k] = c.newFunc(k.Name+".constructor", k, ctorSite, func(f *fn) { f.ctor(k) })
	c.out.Classes[ci].Ctor = c.ctors[k]
	if k.Ctor != nil {
		c.funcs[k.Ctor] = c.ctors[k]
	}
	if slices.ContainsFunc(k.Attributes, func(a *types.Attribute) bool { return a.Static && a.Init != nil }) {
		c.inits[k] = c.newFunc(k.Name+".static", k, site, func(f *fn) { f.staticInit(k) })
		c.out.Classes[ci].Init = c.inits[k]
	}
}

// rootMethod returns the method that introduced the slot of an overriding
// method.
func rootMethod(m *types.Method) *types.Method {
	for m.Overrides != nil {
		m = m.Overrides
	}
	return m
}

func allInterfaces(k *types.Class) []*types.Class {
	var out []*types.Class
	var add func(i *types.Class)
	add = func(i *types.Class) {
		if slices.Contains(out, i) {
			return
		}
		out = append(out, i)
		for _, s := range i.Interfaces {
			add(s)
		}
	}
	for x := k; x != nil; x = x.Base {
		for _, i := range x.Interfaces {
			add(i)
		}
	}
	if k.IsInterface() {
		for _, s := range k.Interfaces {
			add(s)
		}
	}
	return out
}

// interfaceMethods lists the methods of an interface and the interfaces it
// extends, in a fixed order.
func interfaceMethods(k *types.Class) []string {
	var out []string
	for _, i := range append([]*types.Class{k}, allInterfaces(k)...) {
		for _, m := range i.Methods {
			if !slices.Contains(out, m.Name) {
				out = append(out, m.Name)
			}
		}
	}
	return out
}

// itables maps every interface a class implements to its functions.
func (c *compiler) itables(k *types.Class) {
	if k.IsInterface() {
		return
	}
	ci := c.classes[k]
	for _, i := range allInterfaces(k) {
		ii, ok := c.classes[i]
		if !ok {
			continue
		}
		it := bytecode.ITable{Interface: ii}
		for _, name := range c.ifaces[i] {
			m, _ := k.Member(name).(*types.Method)
			if m == nil {
				c.failf("%s does not implement %s", k.Name, name)
			}
			it.Funcs = append(it.Funcs, c.dispatch(k, m))
		}
		c.out.Classes[ci].ITables = append(c.out.Classes[ci].ITables, it)
	}
}

// dispatch returns the function a method resolves to for objects of k.
func (c *compiler) dispatch(k *types.Class, m *types.Method) int {
	if slot, ok := c.vslot[m]; ok {
		return c.out.Classes[c.classes[k]].VTable[slot]
	}
	return c.funcs[m]
}

// ---- tables ----

func (c *compiler) str(s string) int {
	if i, ok := c.strs[s]; ok {
		return i
	}
	c.strs[s] = len(c.out.Strings)
	c.out.Strings = append(c.out.Strings, s)
	return c.strs[s]
}

func (c *compiler) dec(lo, hi uint64) int {
	k := [2]uint64{lo, hi}
	if i, ok := c.decs[k]; ok {
		return i
	}
	c.decs[k] = len(c.out.Decs)
	c.out.Decs = append(c.out.Decs, k)
	return c.decs[k]
}

func (c *compiler) bytesConst(b []byte) int {
	c.out.Bytes = append(c.out.Bytes, b)
	return len(c.out.Bytes) - 1
}

func (c *compiler) file(path string) int {
	if i, ok := c.files[path]; ok {
		return i
	}
	sum := sha256.Sum256([]byte(c.opts.Sources[path]))
	c.files[path] = len(c.out.Files)
	c.out.Files = append(c.out.Files, bytecode.File{Path: path, SHA256: hex.EncodeToString(sum[:])})
	return c.files[path]
}

func (c *compiler) chain(sites []types.IncludeSite) int {
	if len(sites) == 0 {
		return 0
	}
	var key strings.Builder
	var ch bytecode.Chain
	for _, s := range sites {
		fmt.Fprintf(&key, "%s|%s|%d;", s.Include, s.File, s.Line)
		ch.Sites = append(ch.Sites, bytecode.ChainSite{Include: s.Include, File: c.file(s.File), Line: s.Line})
	}
	if i, ok := c.chains[key.String()]; ok {
		return i
	}
	c.chains[key.String()] = len(c.out.Chains)
	c.out.Chains = append(c.out.Chains, ch)
	return c.chains[key.String()]
}

func (c *compiler) addLayout(l bytecode.Layout) int {
	c.out.Layouts = append(c.out.Layouts, l)
	return len(c.out.Layouts) - 1
}

func (c *compiler) enum(e *types.Enum) int {
	if i, ok := c.enums[e]; ok {
		return i
	}
	be := bytecode.Enum{Name: e.Name, Base: basicKind(e.Base)}
	for _, v := range e.Values {
		be.Names = append(be.Names, v.Name)
		be.Values = append(be.Values, v.Value)
	}
	c.enums[e] = len(c.out.Enums)
	c.out.Enums = append(c.out.Enums, be)
	return c.enums[e]
}

// structLayout returns the layout of a struct type.
func (c *compiler) structLayout(s *types.Struct) int {
	if i, ok := c.layouts[s]; ok {
		return i
	}
	idx := c.addLayout(bytecode.Layout{Name: s.Name})
	c.layouts[s] = idx
	var fields []fieldSpec
	for _, f := range s.Fields {
		fields = append(fields, fieldSpec{f.Name, f.Type})
	}
	c.out.Layouts[idx] = buildLayout(c, s.Name, nil, fields)
	return idx
}

func basicKind(b *types.Basic) bytecode.TypeKind {
	switch b.Kind {
	case types.String:
		return bytecode.TString
	case types.Int4:
		return bytecode.TInt4
	case types.Int8:
		return bytecode.TInt8
	case types.Dec:
		return bytecode.TDec
	case types.Date:
		return bytecode.TDate
	case types.Timestamp:
		return bytecode.TTimestamp
	case types.Bool:
		return bytecode.TBool
	case types.UUID:
		return bytecode.TUUID
	case types.Bytes:
		return bytecode.TBytes
	}
	return bytecode.TVoid
}

// kindOf returns the type kind of a type without optional.
func kindOf(t types.Type) bytecode.TypeKind {
	switch x := types.NonOpt(t).(type) {
	case *types.Basic:
		return basicKind(x)
	case *types.Named:
		return basicKind(x.Base)
	case *types.Enum:
		return bytecode.TEnum
	case *types.Struct:
		return bytecode.TStruct
	case *types.Array:
		return bytecode.TArray
	case *types.Map:
		return bytecode.TMap
	case *types.Class:
		return bytecode.TObject
	case *types.Func:
		return bytecode.TFunc
	}
	return bytecode.TVoid
}

// typeDesc returns the index of the description of a type.
func (c *compiler) typeDesc(t types.Type) int {
	if t == nil || t == types.TypVoid {
		return -1
	}
	key := t.String()
	if n, ok := types.NonOpt(t).(*types.Named); ok {
		key += "|" + n.Base.String()
	}
	if i, ok := c.tdescs[key]; ok {
		return i
	}
	d := bytecode.TypeDesc{Kind: kindOf(t), Optional: types.IsOptional(t), Elem: -1, Key: -1, Layout: -1, Enum: -1, Class: -1}
	idx := len(c.out.Types)
	c.out.Types = append(c.out.Types, d)
	c.tdescs[key] = idx
	switch x := types.NonOpt(t).(type) {
	case *types.Basic:
		d.Precision, d.Scale = x.Precision, x.Scale
	case *types.Named:
		d.Name, d.Named = x.Name, true
		d.Precision, d.Scale = x.Base.Precision, x.Base.Scale
		if x.Domain != nil {
			if x.Domain.Type == "CHAR" {
				d.MaxLength = x.Domain.Length
			}
			d.NoEmpty = x.Base.Kind == types.String && !x.Domain.AllowsEmpty()
		}
	case *types.Enum:
		d.Name, d.Enum = x.Name, c.enum(x)
	case *types.Struct:
		d.Name, d.Layout = x.Name, c.structLayout(x)
	case *types.Array:
		d.Name = x.Name
		d.Elem = c.typeDesc(x.Elem)
		if st, ok := x.Elem.(*types.Struct); ok && x.Unique {
			for _, k := range x.Key {
				d.KeyFields = append(d.KeyFields, st.FieldIndex(k))
			}
			d.Unique = true
		}
	case *types.Map:
		d.Key, d.Elem = c.typeDesc(x.Key), c.typeDesc(x.Elem)
	case *types.Class:
		d.Name = x.Name
		if ci, ok := c.classes[x]; ok {
			d.Class = ci
		}
	}
	c.out.Types[idx] = d
	return idx
}

// ---- layouts ----

type fieldSpec struct {
	name string
	typ  types.Type
}

// fixedSize returns the size of values stored at fixed offsets, or 0 for
// references and BOOL.
func fixedSize(t types.Type) int {
	switch kindOf(t) {
	case bytecode.TDec, bytecode.TUUID:
		return 16
	case bytecode.TInt8, bytecode.TTimestamp:
		return 8
	case bytecode.TInt4, bytecode.TDate:
		return 4
	case bytecode.TEnum:
		return 2
	}
	return 0
}

// buildLayout appends fields to a base layout: present bits of optional
// fields, bits of BOOL fields, pairs of bits of optional BOOL fields, then
// fixed size values by decreasing size; references get slots.
func buildLayout(c *compiler, name string, base *bytecode.Layout, specs []fieldSpec) bytecode.Layout {
	l := bytecode.Layout{Name: name}
	if base != nil {
		l.Size, l.Refs = base.Size, base.Refs
		l.Fields = slices.Clone(base.Fields)
	}
	fields := make([]bytecode.Field, len(specs))
	var presence, bools, optBools []int
	var fixed []int
	for i, s := range specs {
		f := bytecode.Field{Name: s.name, Type: c.typeDesc(s.typ), Present: -1, Optional: types.IsOptional(s.typ)}
		switch {
		case kindOf(s.typ) == bytecode.TBool && f.Optional:
			f.Kind = bytecode.FOptBool
			optBools = append(optBools, i)
		case kindOf(s.typ) == bytecode.TBool:
			f.Kind = bytecode.FBool
			bools = append(bools, i)
		case fixedSize(s.typ) > 0:
			f.Kind, f.Size = bytecode.FFixed, fixedSize(s.typ)
			fixed = append(fixed, i)
			if f.Optional {
				presence = append(presence, i)
			}
		default:
			f.Kind = bytecode.FRef
			f.Ref = l.Refs
			l.Refs++
			if f.Optional {
				presence = append(presence, i)
			}
		}
		fields[i] = f
	}
	off := l.Size
	for n, i := range presence {
		fields[i].Present, fields[i].PresBit = off+n/8, n%8
	}
	off += (len(presence) + 7) / 8
	for n, i := range bools {
		fields[i].Offset, fields[i].Bit = off+n/8, n%8
	}
	off += (len(bools) + 7) / 8
	for n, i := range optBools {
		fields[i].Offset, fields[i].Bit = off+(2*n)/8, (2*n)%8
	}
	off += (2*len(optBools) + 7) / 8
	sort.SliceStable(fixed, func(a, b int) bool { return fields[fixed[a]].Size > fields[fixed[b]].Size })
	for _, i := range fixed {
		size := fields[i].Size
		if off%size != 0 {
			off += size - off%size
		}
		fields[i].Offset = off
		off += size
	}
	l.Size = off
	l.Fields = append(l.Fields, fields...)
	return l
}
