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
	"github.com/tim-riep/go-runtime/model"
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
	// Imports are the compiled programs of the kernel and of all packages
	// the package requires, also indirectly, in installation order. Their
	// classes and functions are referred to by direct indexes.
	Imports []*bytecode.Program
}

// lib is an imported program.
type lib struct {
	prog *bytecode.Program
	slot int
}

type compiler struct {
	tp      *types.Program
	info    *types.Info
	opts    Options
	out     *bytecode.Program
	libs    map[string]*lib // by package
	classes map[*types.Class]int
	ctors   map[*types.Class]int
	inits   map[*types.Class]int
	funcs   map[*types.Method]int
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
	c := &compiler{tp: tp, info: tp.Info, opts: opts, libs: map[string]*lib{}, classes: map[*types.Class]int{}, ctors: map[*types.Class]int{},
		inits: map[*types.Class]int{}, funcs: map[*types.Method]int{}, ifaces: map[*types.Class][]string{}, layouts: map[*types.Struct]int{},
		inst: map[*types.Class]int{}, statics: map[*types.Class]int{}, enums: map[*types.Enum]int{}, tdescs: map[string]int{},
		strs: map[string]int{}, decs: map[[2]uint64]int{}, files: map[string]int{}, chains: map[string]int{}}
	c.out = &bytecode.Program{Package: string(tp.Package), LanguageVersion: opts.LanguageVersion, Tests: opts.Tests,
		Chains: []bytecode.Chain{{}}}
	for i, p := range opts.Imports {
		c.libs[p.Package] = &lib{prog: p, slot: i + 1}
		c.out.Imports = append(c.out.Imports, p.Package)
	}
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
	for _, k := range append(slices.Clone(tp.Libraries), classes...) {
		if k.IsInterface() {
			c.ifaces[k] = interfaceMethods(k)
		}
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

// ---- references into this program and its imports ----

// external returns the imported program and index of a class of another
// package.
func (c *compiler) external(k *types.Class) (*lib, int) {
	l := c.libs[string(k.Package)]
	if l == nil {
		c.failf("%s belongs to package %s, which is not imported", k.Name, k.Package)
	}
	i := l.prog.Class(k.Name)
	if i < 0 {
		c.failf("the compiled code of %s has no class %s", k.Package, k.Name)
	}
	return l, i
}

// translate turns a reference of an imported program into one of this
// program.
func (c *compiler) translate(l *lib, r bytecode.Ref) bytecode.Ref {
	if r < 0 {
		return r
	}
	pkg := l.prog.Package
	if s := bytecode.RefSlot(r); s > 0 {
		pkg = l.prog.Imports[s-1]
	}
	if pkg == c.out.Package {
		return bytecode.MakeRef(0, bytecode.RefIndex(r))
	}
	target := c.libs[pkg]
	if target == nil {
		c.failf("package %s is not imported", pkg)
	}
	return bytecode.MakeRef(target.slot, bytecode.RefIndex(r))
}

// classDef returns the compiled class of a class of this program or an
// import, with the import (nil for this program).
func (c *compiler) classDef(k *types.Class) (*bytecode.Class, *lib) {
	if i, ok := c.classes[k]; ok {
		return &c.out.Classes[i], nil
	}
	l, i := c.external(k)
	return &l.prog.Classes[i], l
}

func (c *compiler) classRef(k *types.Class) bytecode.Ref {
	if i, ok := c.classes[k]; ok {
		return bytecode.MakeRef(0, i)
	}
	l, i := c.external(k)
	return bytecode.MakeRef(l.slot, i)
}

func (c *compiler) ctorRef(k *types.Class) bytecode.Ref {
	if f, ok := c.ctors[k]; ok {
		return bytecode.MakeRef(0, f)
	}
	l, i := c.external(k)
	return bytecode.MakeRef(l.slot, l.prog.Classes[i].Ctor)
}

// funcRef returns the function of a method that is called without dynamic
// dispatch.
func (c *compiler) funcRef(m *types.Method) bytecode.Ref {
	if f, ok := c.funcs[m]; ok {
		return bytecode.MakeRef(0, f)
	}
	def, l := c.classDef(m.Owner)
	if m.Ctor {
		return c.ctorRef(m.Owner)
	}
	for _, mr := range def.Methods {
		if mr.Name == m.Name {
			if l == nil {
				return mr.Func
			}
			return c.translate(l, mr.Func)
		}
	}
	c.failf("no code for %s", m)
	return 0
}

// vslot returns the slot of an open or overriding method in the method
// table of its class.
func (c *compiler) vslot(m *types.Method) (int, bool) {
	if !m.Open && !m.Override {
		return 0, false
	}
	def, _ := c.classDef(m.Owner)
	i := slices.Index(def.VSlots, m.Name)
	return i, i >= 0
}

// instLayout returns the layout of the objects of a class.
func (c *compiler) instLayout(k *types.Class) *bytecode.Layout {
	if i, ok := c.inst[k]; ok {
		return &c.out.Layouts[i]
	}
	l, i := c.external(k)
	return &l.prog.Layouts[l.prog.Classes[i].Layout]
}

// staticLayout returns the layout of the static attributes of a class.
func (c *compiler) staticLayout(k *types.Class) *bytecode.Layout {
	if i, ok := c.statics[k]; ok {
		return &c.out.Layouts[i]
	}
	l, i := c.external(k)
	return &l.prog.Layouts[l.prog.Classes[i].Statics]
}

func (c *compiler) declareClass(k *types.Class) {
	ci := c.classes[k]
	bc := &c.out.Classes[ci]
	if k.Base != nil {
		bc.Base = c.classRef(k.Base)
	}
	bc.Supers = append(bc.Supers, bytecode.MakeRef(0, ci))
	for b := k.Base; b != nil; b = b.Base {
		bc.Supers = append(bc.Supers, c.classRef(b))
	}
	for _, i := range allInterfaces(k) {
		if r := c.classRef(i); !slices.Contains(bc.Supers, r) {
			bc.Supers = append(bc.Supers, r)
		}
	}
	if k.IsInterface() {
		return
	}
	// Instance layout: the fields of the base class keep their places.
	var base *bytecode.Layout
	if k.Base != nil {
		base = c.instLayout(k.Base)
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
	// methods get new ones. Methods callable by name include inherited ones.
	var vt []bytecode.Ref
	var names []string
	var methods []bytecode.MethodRef
	if k.Base != nil {
		def, l := c.classDef(k.Base)
		for _, r := range def.VTable {
			if l != nil {
				r = c.translate(l, r)
			}
			vt = append(vt, r)
		}
		names = slices.Clone(def.VSlots)
		for _, m := range def.Methods {
			if l != nil {
				m.Func = c.translate(l, m.Func)
			}
			methods = append(methods, m)
		}
	}
	for _, m := range k.Methods {
		idx := c.newFunc(k.Name+"."+m.Name, k, m.Site, func(f *fn) { f.method(m) })
		c.funcs[m] = idx
		ref := bytecode.MakeRef(0, idx)
		mr := bytecode.MethodRef{Name: m.Name, Func: ref, Static: m.Static, Public: m.Visibility == syntax.Public, Test: m.Test,
			Capabilities: m.Capabilities}
		if i := slices.IndexFunc(methods, func(o bytecode.MethodRef) bool { return o.Name == m.Name }); i >= 0 {
			methods[i] = mr
		} else {
			methods = append(methods, mr)
		}
		switch slot := slices.Index(names, m.Name); {
		case m.Override && slot >= 0:
			vt[slot] = ref
		case m.Open:
			names = append(names, m.Name)
			vt = append(vt, ref)
		}
	}
	c.out.Classes[ci].Methods = methods
	c.out.Classes[ci].VTable, c.out.Classes[ci].VSlots = vt, names
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
		it := bytecode.ITable{Interface: c.classRef(i)}
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
func (c *compiler) dispatch(k *types.Class, m *types.Method) bytecode.Ref {
	def := &c.out.Classes[c.classes[k]]
	if slot := slices.Index(def.VSlots, m.Name); slot >= 0 {
		return def.VTable[slot]
	}
	for _, mr := range def.Methods {
		if mr.Name == m.Name {
			return mr.Func
		}
	}
	return c.funcRef(m)
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
	l := buildLayout(c, s.Name, nil, fields)
	for i, f := range s.Fields {
		if f.Default.Kind == model.DefaultValue {
			l.Fields[i].Default, l.Fields[i].HasDefault = f.Default.Value, true
		}
	}
	c.out.Layouts[idx] = l
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
		d.Class = c.classRef(x)
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
