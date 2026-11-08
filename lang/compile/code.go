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

package compile

import (
	"math/big"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
)

// fn generates the code of one function.
type fn struct {
	c      *compiler
	idx    int
	class  *types.Class
	info   *types.FuncInfo
	code   []int32
	lines  []bytecode.Line
	nslots int
	file   string
	chain  []types.IncludeSite
	loops  []*loop
	tries  []*tryCtx
	result types.Type
}

type loop struct {
	breaks, continues []int
	tries             int // number of enclosing tries when the loop started
}

// tryCtx is a try statement being compiled. Its handlers cover ranges of
// code that leave out finally blocks inserted for return, break and
// continue.
type tryCtx struct {
	finally  *syntax.Block
	catching bool  // compiling the catch blocks
	body     []int // pairs of start and end of the ranges covered by the catch blocks
	all      []int // ranges covered by the handler that runs finally
	open     int   // start of the current range, -1 if closed
	allOpen  int
	finFile  string
	finChain []types.IncludeSite
}

func newFn(c *compiler, idx int, class *types.Class, site types.Site) *fn {
	return &fn{c: c, idx: idx, class: class, file: site.File, chain: site.Chain, result: types.TypVoid}
}

func (f *fn) out() *bytecode.Func { return &f.c.out.Funcs[f.idx] }

func (f *fn) emit(op Op, args ...int) int {
	pc := len(f.code)
	f.code = append(f.code, int32(op))
	for _, a := range args {
		f.code = append(f.code, int32(a))
	}
	return pc
}

// jump emits a jump with an operand patched later.
func (f *fn) jump(op Op) int {
	f.emit(op, 0)
	return len(f.code) - 1
}

func (f *fn) patch(at int) { f.code[at] = int32(len(f.code)) }

func (f *fn) temp() int {
	f.nslots++
	return f.nslots - 1
}

// line records the position of the code that follows.
func (f *fn) line(p syntax.Pos) {
	l := bytecode.Line{PC: len(f.code), File: f.c.file(f.file), Line: p.Line, Column: p.Col, Chain: f.c.chain(f.chain)}
	if n := len(f.lines); n > 0 {
		last := f.lines[n-1]
		if last.PC == l.PC {
			f.lines[n-1] = l
			return
		}
		if last.File == l.File && last.Line == l.Line && last.Chain == l.Chain {
			return
		}
	}
	f.lines = append(f.lines, l)
}

func (f *fn) finish() {
	o := f.out()
	o.Code = f.code
	o.Lines = f.lines
	o.Locals = max(f.nslots, o.Params)
}

func (f *fn) setLocals(info *types.FuncInfo) {
	f.info = info
	f.nslots = len(info.Locals)
}

// boxParams moves captured parameters into cells.
func (f *fn) boxParams() {
	for _, l := range f.info.Locals {
		if l.Param && l.Captured {
			f.emit(LOAD, l.Index)
			f.emit(NEW_CELL, l.Index)
		}
	}
}

func (f *fn) method(m *types.Method) {
	o := f.out()
	o.Instance = !m.Static
	o.Params = len(m.Params)
	for _, p := range m.Sig.Params {
		o.ParamTypes = append(o.ParamTypes, f.c.typeDesc(p))
	}
	o.Result = f.c.typeDesc(m.Sig.Result)
	f.result = m.Sig.Result
	if m.Native {
		o.Native = true
		for _, r := range m.Sig.Raises {
			o.Raises = append(o.Raises, f.c.classRef(r))
		}
		return
	}
	if m.Func == nil {
		// Abstract methods have no code.
		f.emit(RETURN)
		return
	}
	f.setLocals(m.Func)
	f.line(m.Site.Pos)
	f.boxParams()
	f.stmts(m.Decl.Body.Stmts)
	f.emit(RETURN)
}

// ctor generates the constructor: the base constructor, the initial values
// of the own attributes, then the body.
func (f *fn) ctor(k *types.Class) {
	o := f.out()
	o.Instance = true
	m := k.Ctor
	var body []syntax.Stmt
	if m != nil {
		o.Params = len(m.Params)
		for _, p := range m.Sig.Params {
			o.ParamTypes = append(o.ParamTypes, f.c.typeDesc(p))
		}
		f.setLocals(m.Func)
		f.line(m.Site.Pos)
		f.boxParams()
		body = m.Decl.Body.Stmts
	} else {
		f.info = &types.FuncInfo{}
	}
	superDone := false
	if len(body) > 0 {
		if es, ok := body[0].(*syntax.ExprStmt); ok {
			if call, ok := es.X.(*syntax.CallExpr); ok {
				if ci := f.c.info.Calls[call]; ci != nil && ci.Kind == types.CallCtor {
					f.line(es.Pos)
					f.emit(LOAD_ME)
					f.args(ci.Args)
					f.emit(CALL, f.c.ctorRef(k.Base), len(ci.Args)+1)
					body = body[1:]
					superDone = true
				}
			}
		}
	}
	if !superDone && k.Base != nil {
		f.emit(LOAD_ME)
		f.emit(CALL, f.c.ctorRef(k.Base), 1)
	}
	layout := f.c.instLayout(k)
	for _, a := range k.Attributes {
		if a.Static || a.Init == nil {
			continue
		}
		saved := f.file
		f.file, f.chain = a.Site.File, a.Site.Chain
		f.line(a.Site.Pos)
		f.emit(LOAD_ME)
		f.valueOwn(a.Init)
		f.setField(layout.Fields[layout.Field(a.Name)])
		f.file = saved
	}
	if m != nil {
		f.file, f.chain = m.Site.File, m.Site.Chain
	}
	f.stmts(body)
	f.emit(RETURN)
}

func (f *fn) staticInit(k *types.Class) {
	f.info = &types.FuncInfo{}
	layout := f.c.staticLayout(k)
	for _, a := range k.Attributes {
		if !a.Static || a.Init == nil {
			continue
		}
		f.file, f.chain = a.Site.File, a.Site.Chain
		f.line(a.Site.Pos)
		f.emit(STATICS, f.c.classRef(k))
		f.valueOwn(a.Init)
		f.setField(layout.Fields[layout.Field(a.Name)])
	}
	f.emit(RETURN)
}

// ---- statements ----

func (f *fn) stmts(ss []syntax.Stmt) {
	for _, s := range ss {
		f.stmt(s)
	}
}

func (f *fn) stmt(s syntax.Stmt) {
	f.line(s.Start())
	info := f.c.info
	switch s := s.(type) {
	case *syntax.VarStmt:
		l := info.Vars[s][0]
		if s.Value == nil {
			f.emit(ZERO, f.c.typeDesc(l.Type))
		} else {
			f.valueOwn(s.Value)
		}
		f.storeLocal(l, true)
	case *syntax.RowStmt:
		f.rowStmt(s)
	case *syntax.ModifyStmt:
		f.modifyStmt(s)
	case *syntax.SaveStmt:
		f.saveStmt(s)
	case *syntax.DatabaseStmt:
		f.sessionStmt(s)
	case *syntax.AssignStmt:
		f.assign(s)
	case *syntax.ExprStmt:
		f.expr(s.X)
		if t := info.Types[s.X]; t != types.TypVoid && t != nil {
			f.emit(POP)
		}
	case *syntax.IfStmt:
		var ends []int
		for _, cl := range s.Clauses {
			f.line(cl.Pos)
			f.expr(cl.Cond)
			next := f.jump(JUMP_FALSE)
			f.stmts(cl.Body.Stmts)
			ends = append(ends, f.jump(JUMP))
			f.patch(next)
		}
		if s.Else != nil {
			f.stmts(s.Else.Stmts)
		}
		for _, e := range ends {
			f.patch(e)
		}
	case *syntax.WhileStmt:
		top := len(f.code)
		f.expr(s.Cond)
		exit := f.jump(JUMP_FALSE)
		l := f.pushLoop()
		f.stmts(s.Body.Stmts)
		f.emit(JUMP, top)
		f.patch(exit)
		f.popLoop(l, top)
	case *syntax.ForStmt:
		f.forStmt(s)
	case *syntax.BreakStmt:
		l := f.loops[len(f.loops)-1]
		f.leaveTries(l.tries)
		l.breaks = append(l.breaks, f.jump(JUMP))
	case *syntax.ContinueStmt:
		l := f.loops[len(f.loops)-1]
		f.leaveTries(l.tries)
		l.continues = append(l.continues, f.jump(JUMP))
	case *syntax.ReturnStmt:
		if s.Value != nil {
			f.valueOwn(s.Value)
			if len(f.tries) > 0 && f.anyFinally(0) {
				t := f.temp()
				f.emit(STORE, t)
				f.leaveTries(0)
				f.emit(LOAD, t)
			}
			f.emit(RETURN_VALUE)
			return
		}
		f.leaveTries(0)
		f.emit(RETURN)
	case *syntax.RaiseStmt:
		k := info.Raises[s]
		f.emit(NEW_OBJ, f.c.classRef(k))
		f.emit(DUP)
		f.args(s.Args)
		f.emit(CALL, f.c.ctorRef(k), len(s.Args)+1)
		f.line(s.Pos)
		f.emit(RAISE)
	case *syntax.TryStmt:
		f.tryStmt(s)
	case *syntax.AssertStmt:
		f.expr(s.Cond)
		if s.Message != nil {
			f.expr(s.Message)
			f.emit(ASSERT, 1)
		} else {
			f.emit(ASSERT, 0)
		}
	case *syntax.AuthCheckStmt:
		f.authCheck(s.Pos, &s.AuthCheck, true)
	case *syntax.LogStmt:
		f.expr(s.Value)
		f.toText(info.Types[s.Value])
		f.emit(LOG, slices.Index(syntax.LogLevels, s.Level))
	case *syntax.IncludeStmt:
		e := info.Expansions[s]
		if e == nil {
			return // skipped include ... if found
		}
		file, chain := f.file, f.chain
		f.file, f.chain = e.File, e.Chain
		f.stmts(e.Stmts)
		f.file, f.chain = file, chain
	}
}

func (f *fn) pushLoop() *loop {
	l := &loop{tries: len(f.tries)}
	f.loops = append(f.loops, l)
	return l
}

func (f *fn) popLoop(l *loop, cont int) {
	for _, b := range l.breaks {
		f.patch(b)
	}
	for _, c := range l.continues {
		f.code[c] = int32(cont)
	}
	f.loops = f.loops[:len(f.loops)-1]
}

// storeLocal stores the value on the stack into a local; declare creates
// the cell of a captured local.
func (f *fn) storeLocal(l *types.Local, declare bool) {
	switch {
	case l.Func != f.info:
		f.emit(STORE_UP, slices.Index(f.info.Captures, l))
	case l.Captured && declare:
		f.emit(NEW_CELL, l.Index)
	case l.Captured:
		f.emit(STORE_CELL, l.Index)
	default:
		f.emit(STORE, l.Index)
	}
}

func (f *fn) loadLocal(l *types.Local) {
	switch {
	case l.Func != f.info:
		f.emit(LOAD_UP, slices.Index(f.info.Captures, l))
	case l.Captured:
		f.emit(LOAD_CELL, l.Index)
	default:
		f.emit(LOAD, l.Index)
	}
}

func (f *fn) assign(s *syntax.AssignStmt) {
	info := f.c.info
	switch t := s.Target.(type) {
	case *syntax.Ident:
		switch u := info.Uses[t].(type) {
		case *types.Local:
			f.valueOwn(s.Value)
			f.storeLocal(u, false)
			return
		case *types.Attribute:
			f.attrTarget(u)
			f.valueOwn(s.Value)
			f.setAttr(u)
			return
		}
	case *syntax.SelectExpr:
		sel := info.Selects[t]
		switch sel.Kind {
		case types.SelectField:
			f.expr(t.X)
			f.valueOwn(s.Value)
			st := types.NonOpt(info.Types[t.X]).(*types.Struct)
			f.setField(f.c.out.Layouts[f.c.structLayout(st)].Fields[sel.Field])
			return
		case types.SelectAttribute:
			f.expr(t.X)
			f.valueOwn(s.Value)
			f.setAttr(sel.Attribute)
			return
		case types.SelectStatic:
			f.emit(STATICS, f.c.classRef(sel.Attribute.Owner))
			f.valueOwn(s.Value)
			f.setAttr(sel.Attribute)
			return
		}
	case *syntax.IndexExpr:
		f.expr(t.X)
		f.expr(t.Index)
		f.valueOwn(s.Value)
		if _, isMap := types.NonOpt(info.Types[t.X]).(*types.Map); isMap {
			f.emit(MINDEX_SET)
		} else {
			f.emit(INDEX_SET)
		}
		return
	case *syntax.ParenExpr:
		f.assign(&syntax.AssignStmt{Target: t.X, Value: s.Value})
		return
	}
	f.c.failf("cannot assign to %s", syntax.PrintExpr(s.Target))
}

// attrTarget pushes the object that holds an attribute.
func (f *fn) attrTarget(a *types.Attribute) {
	if a.Static {
		f.emit(STATICS, f.c.classRef(a.Owner))
	} else {
		f.emit(LOAD_ME)
	}
}

func (f *fn) attrField(a *types.Attribute) bytecode.Field {
	var l *bytecode.Layout
	if a.Static {
		l = f.c.staticLayout(a.Owner)
	} else {
		l = f.c.instLayout(a.Owner)
	}
	fd := l.Fields[l.Field(a.Name)]
	// The layout of a class of another package refers to the types of its
	// program; the code uses those of this one.
	fd.Type = f.c.typeDesc(a.Type)
	return fd
}

func (f *fn) setAttr(a *types.Attribute) { f.setField(f.attrField(a)) }
func (f *fn) getAttr(a *types.Attribute) { f.getField(f.attrField(a)) }

// fixedOps maps the type kinds of fixed size fields to their instructions.
var fixedOps = map[TypeKind][2]Op{
	TInt4: {GETI4, SETI4}, TInt8: {GETI8, SETI8}, TDec: {GETDEC, SETDEC}, TUUID: {GETUUID, SETUUID},
	TDate: {GETDATE, SETDATE}, TTimestamp: {GETTS, SETTS}, TEnum: {GETENUM, SETENUM},
}

func (f *fn) getField(fd bytecode.Field) { f.fieldOp(fd, 0) }
func (f *fn) setField(fd bytecode.Field) { f.fieldOp(fd, 1) }

func (f *fn) fieldOp(fd bytecode.Field, set int) {
	switch fd.Kind {
	case FBool:
		f.emit([2]Op{GETBOOL, SETBOOL}[set], fd.Offset, fd.Bit)
	case FOptBool:
		f.emit([2]Op{GETOBOOL, SETOBOOL}[set], fd.Offset, fd.Bit)
	case FFixed:
		op := fixedOps[f.c.out.Types[fd.Type].Kind][set]
		f.emit(op, fd.Offset, fd.Present, fd.PresBit)
	case FRef:
		f.emit([2]Op{GETREF, SETREF}[set], fd.Ref, fd.Present, fd.PresBit)
	}
}

func (f *fn) forStmt(s *syntax.ForStmt) {
	info := f.c.info
	if info.Cursors[s] {
		f.cursorLoop(s)
		return
	}
	vars := info.Vars[s]
	if s.In != nil {
		t := types.NonOpt(info.Types[s.In])
		col, idx, n := f.temp(), f.temp(), f.temp()
		f.valueOwn(s.In)
		var vals int
		if m, isMap := t.(*types.Map); isMap {
			vals = f.temp()
			f.emit(STORE, col)
			f.emit(LOAD, col)
			f.emit(CALL_BUILTIN, int(types.MapValues), 0, f.c.typeDesc(m), f.c.typeDesc(&types.Array{Elem: m.Elem}))
			f.emit(STORE, vals)
			f.emit(LOAD, col)
			f.emit(CALL_BUILTIN, int(types.MapKeys), 0, f.c.typeDesc(m), f.c.typeDesc(&types.Array{Elem: m.Key}))
			f.emit(STORE, col)
		} else {
			f.emit(STORE, col)
		}
		f.emit(LOAD, col)
		f.emit(CALL_BUILTIN, int(types.ArrLength), 0, f.c.typeDesc(&types.Array{Elem: types.TypInt8}), f.c.typeDesc(types.TypInt8))
		f.emit(STORE, n)
		f.emit(CONST_I, 1)
		f.emit(STORE, idx)
		top := len(f.code)
		f.emit(LOAD, idx)
		f.emit(LOAD, n)
		f.emit(CMP_I)
		f.emit(TEST, 3)
		exit := f.jump(JUMP_FALSE)
		elem := vars[len(vars)-1]
		if _, isMap := t.(*types.Map); isMap {
			f.emit(LOAD, col)
			f.emit(LOAD, idx)
			f.emit(INDEX_GET)
			f.storeLocal(vars[0], true)
			f.emit(LOAD, vals)
		} else {
			if len(vars) == 2 {
				f.emit(LOAD, idx)
				f.storeLocal(vars[0], true)
			}
			f.emit(LOAD, col)
		}
		f.emit(LOAD, idx)
		f.emit(INDEX_GET)
		f.copyIfValue(elem.Type)
		f.storeLocal(elem, true)
		l := f.pushLoop()
		f.stmts(s.Body.Stmts)
		cont := len(f.code)
		f.emit(LOAD, idx)
		f.emit(CONST_I, 1)
		f.emit(ADD_I8)
		f.emit(STORE, idx)
		f.emit(JUMP, top)
		f.patch(exit)
		f.popLoop(l, cont)
		return
	}
	v := vars[0]
	int4 := 0
	if types.IsKind(v.Type, types.Int4) {
		int4 = 1
	}
	i, to, step := f.temp(), f.temp(), f.temp()
	f.expr(s.From)
	f.emit(STORE, i)
	f.expr(s.To)
	f.emit(STORE, to)
	if s.Step != nil {
		f.expr(s.Step)
	} else {
		f.emit(CONST_I, 1)
	}
	f.emit(STORE, step)
	top := len(f.code)
	exitTest := len(f.code) + 4
	f.emit(FOR_TEST, i, to, step, 0)
	f.emit(LOAD, i)
	f.storeLocal(v, true)
	l := f.pushLoop()
	f.stmts(s.Body.Stmts)
	cont := len(f.code)
	exitStep := len(f.code) + 4
	f.emit(FOR_STEP, i, to, step, 0, int4)
	f.emit(JUMP, top)
	f.code[exitTest] = int32(len(f.code))
	f.code[exitStep] = int32(len(f.code))
	f.popLoop(l, cont)
}

// ---- try and finally ----

func (f *fn) tryStmt(s *syntax.TryStmt) {
	info := f.c.info
	t := &tryCtx{finally: s.Finally, open: len(f.code), allOpen: len(f.code), finFile: f.file, finChain: f.chain}
	f.tries = append(f.tries, t)
	f.stmts(s.Body.Stmts)
	t.close(len(f.code), true)
	f.inlineFinally(t)
	ends := []int{f.jump(JUMP)}
	t.catching = true
	type handler struct{ pc, class int }
	var handlers []handler
	for _, cc := range s.Catches {
		l := info.Catches[cc]
		k, _ := l.Type.(*types.Class)
		if k == nil {
			continue
		}
		pc := len(f.code)
		t.allOpen = pc
		f.line(cc.Pos)
		f.storeLocal(l, true)
		f.stmts(cc.Body.Stmts)
		handlers = append(handlers, handler{pc, f.c.classRef(k)})
		t.closeAll(len(f.code))
		f.inlineFinally(t)
		ends = append(ends, f.jump(JUMP))
	}
	f.tries = f.tries[:len(f.tries)-1]
	o := f.out()
	for _, h := range handlers {
		for i := 0; i < len(t.body); i += 2 {
			o.Handlers = append(o.Handlers, bytecode.Handler{Start: t.body[i], End: t.body[i+1], Target: h.pc, Class: h.class})
		}
	}
	if s.Finally != nil {
		// The handler for everything runs finally and raises again.
		pc := len(f.code)
		ex := f.temp()
		f.emit(STORE, ex)
		f.inlineFinally(t)
		f.emit(LOAD, ex)
		f.emit(RAISE)
		for i := 0; i < len(t.all); i += 2 {
			o.Handlers = append(o.Handlers, bytecode.Handler{Start: t.all[i], End: t.all[i+1], Target: pc, Class: -1})
		}
	}
	for _, e := range ends {
		f.patch(e)
	}
}

// close ends the current ranges at pc.
func (t *tryCtx) close(pc int, body bool) {
	if body && t.open >= 0 {
		if pc > t.open {
			t.body = append(t.body, t.open, pc)
		}
		t.open = -1
	}
	t.closeAll(pc)
	if !body {
		return
	}
	t.allOpen = -1
}

func (t *tryCtx) closeAll(pc int) {
	if t.allOpen >= 0 && pc > t.allOpen {
		t.all = append(t.all, t.allOpen, pc)
	}
	t.allOpen = -1
}

func (t *tryCtx) reopen(pc int) {
	if !t.catching {
		t.open = pc
	}
	t.allOpen = pc
}

// anyFinally reports whether a try from level on has a finally block.
func (f *fn) anyFinally(level int) bool {
	for _, t := range f.tries[level:] {
		if t.finally != nil {
			return true
		}
	}
	return false
}

// leaveTries inserts the finally blocks of the tries from level on, the
// innermost first, before return, break or continue leaves them.
func (f *fn) leaveTries(level int) {
	if !f.anyFinally(level) {
		return
	}
	saved := slices.Clone(f.tries)
	for i := len(f.tries) - 1; i >= level; i-- {
		t := f.tries[i]
		// The inserted code is not protected by this try and the inner ones.
		for _, inner := range f.tries[i:] {
			inner.close(len(f.code), true)
		}
		if t.finally != nil {
			f.tries = f.tries[:i]
			f.inlineFinally(t)
			f.tries = saved
		}
	}
	// Code after return, break and continue in the same block is
	// unreachable, but the ranges continue for the code after an if.
	for _, t := range f.tries[level:] {
		t.reopen(len(f.code))
	}
}

func (f *fn) inlineFinally(t *tryCtx) {
	if t.finally == nil {
		return
	}
	file, chain := f.file, f.chain
	f.file, f.chain = t.finFile, t.finChain
	f.stmts(t.finally.Stmts)
	f.file, f.chain = file, chain
}

// ---- expressions ----

// valueOwn pushes the value of an expression to be stored or passed on:
// values of structs, arrays and maps read from a place are copied.
func (f *fn) valueOwn(x syntax.Expr) {
	f.expr(x)
	if isPlace(x) {
		t := f.c.info.Types[x]
		if cv, ok := f.c.info.Conv[x]; ok {
			t = cv
		}
		f.copyIfValue(t)
	}
}

func isPlace(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Ident, *syntax.SelectExpr, *syntax.IndexExpr:
		return true
	case *syntax.ParenExpr:
		return isPlace(x.X)
	case *syntax.BinaryExpr:
		return x.Op == "default" && (isPlace(x.X) || isPlace(x.Y))
	}
	return false
}

func (f *fn) copyIfValue(t types.Type) {
	switch types.NonOpt(t).(type) {
	case *types.Struct, *types.Array, *types.Map:
		f.emit(COPY, f.c.typeDesc(t))
	}
}

func (f *fn) args(xs []syntax.Expr) {
	for _, x := range xs {
		f.valueOwn(x)
	}
}

// expr pushes the value of an expression with its implicit conversion.
func (f *fn) expr(x syntax.Expr) {
	f.expr0(x)
	if to, ok := f.c.info.Conv[x]; ok {
		f.convert(f.c.info.Types[x], to)
	}
}

func scaleOf(t types.Type) int {
	if b := types.BasicOf(types.NonOpt(t)); b != nil && b.Kind == types.Dec {
		return b.Scale
	}
	return 0
}

// operandType returns the type of an operand after its conversion.
func (f *fn) operandType(x syntax.Expr) types.Type {
	if t, ok := f.c.info.Conv[x]; ok {
		return t
	}
	return f.c.info.Types[x]
}

func (f *fn) expr0(x syntax.Expr) {
	info := f.c.info
	t := info.Types[x]
	switch x := x.(type) {
	case *syntax.LiteralExpr:
		f.literal(x, t)
	case *syntax.StringExpr:
		if len(x.Parts) == 0 {
			f.emit(CONST_STR, f.c.str(""))
			return
		}
		for _, p := range x.Parts {
			if p.Expr == nil {
				f.emit(CONST_STR, f.c.str(p.Text))
				continue
			}
			f.expr(p.Expr)
			f.toText(info.Types[p.Expr])
		}
		if len(x.Parts) > 1 {
			f.emit(CONCATN, len(x.Parts))
		}
	case *syntax.Ident:
		switch u := info.Uses[x].(type) {
		case *types.Local:
			f.loadLocal(u)
		case *types.Attribute:
			f.attrTarget(u)
			f.getAttr(u)
		case *types.Constant:
			f.constant(u)
		default:
			f.c.failf("unresolved name %s", x.Name)
		}
	case *syntax.MeExpr:
		f.emit(LOAD_ME)
	case *syntax.ParenExpr:
		f.expr(x.X)
	case *syntax.UnaryExpr:
		f.expr(x.X)
		if x.Op == "not" {
			f.emit(NOT)
			return
		}
		switch kindOf(t) {
		case TInt4:
			f.emit(NEG_I4)
		case TInt8:
			f.emit(NEG_I8)
		case TDec:
			f.emit(NEG_DEC)
		}
	case *syntax.BinaryExpr:
		f.binary(x, t)
	case *syntax.IsExpr:
		f.expr(x.X)
		if x.What == "null" {
			f.emit(IS_NULL)
		} else {
			f.emit(IS_EMPTY, int(kindOf(info.Types[x.X])))
		}
		if x.Not {
			f.emit(NOT)
		}
	case *syntax.AsExpr:
		f.expr(x.X)
		f.conv(info.Types[x.X], t)
	case *syntax.SelectExpr:
		f.selectExpr(x)
	case *syntax.IndexExpr:
		f.expr(x.X)
		f.expr(x.Index)
		if _, isMap := types.NonOpt(info.Types[x.X]).(*types.Map); isMap {
			f.emit(MINDEX_GET)
		} else {
			f.emit(INDEX_GET)
		}
	case *syntax.CallExpr:
		f.call(x)
	case *syntax.NewExpr:
		f.newExpr(x, t)
	case *syntax.ArrayExpr:
		for _, e := range x.Elems {
			f.valueOwn(e)
		}
		f.emit(ARRAY_LIT, f.c.typeDesc(t), len(x.Elems))
	case *syntax.MapExpr:
		for i := range x.Keys {
			f.valueOwn(x.Keys[i])
			f.valueOwn(x.Values[i])
		}
		f.emit(MAP_LIT, f.c.typeDesc(t), len(x.Keys))
	case *syntax.FuncExpr:
		f.funcExpr(x)
	case *syntax.QueryExpr, *syntax.UpdateExpr, *syntax.DeleteExpr, *syntax.OpenSessionExpr:
		f.dbExpr(x)
	case *syntax.TransformExpr:
		f.transform(x)
	case *syntax.AuthorizedExpr:
		f.authCheck(x.Pos, &x.AuthCheck, false)
	case *syntax.ExtensionPointExpr:
		f.extensionPoint(x)
	case *syntax.ReadExpr:
		f.readExpr(x)
	default:
		f.c.failf("cannot compile %T", x)
	}
}

// constant pushes the value of a constant; constants are literals, enum
// constants or other constants.
func (f *fn) constant(k *types.Constant) {
	f.expr(k.Value)
}

func (f *fn) literal(x *syntax.LiteralExpr, t types.Type) {
	switch x.Kind {
	case syntax.KEYWORD:
		switch x.Raw {
		case "true":
			f.emit(TRUE)
		case "false":
			f.emit(FALSE)
		default:
			f.emit(NULL)
		}
		return
	}
	raw := strings.ReplaceAll(x.Raw, "_", "")
	if kindOf(t) == TDec {
		d, err := dec.Parse(raw, scaleOf(t))
		if err != nil {
			f.c.failf("%v", err)
		}
		f.emit(CONST_DEC, f.c.dec(d.Lo, d.Hi))
		return
	}
	n, _ := new(big.Int).SetString(raw, 10)
	f.int64(n.Int64())
}

func (f *fn) int64(n int64) {
	if n >= -1<<31 && n < 1<<31 {
		f.emit(CONST_I, int(n))
		return
	}
	f.emit(CONST_I64, int(int32(uint64(n)>>32)), int(int32(uint32(n))))
}

// toText formats the value on the stack as text for placeholders and log.
func (f *fn) toText(t types.Type) {
	switch k := kindOf(t); k {
	case TString:
	case TDec:
		f.emit(FMT, int(k), scaleOf(t))
	case TEnum:
		f.emit(FMT, int(k), f.c.enum(types.NonOpt(t).(*types.Enum)))
	default:
		f.emit(FMT, int(k), 0)
	}
}

func (f *fn) binary(x *syntax.BinaryExpr, t types.Type) {
	info := f.c.info
	switch x.Op {
	case "and", "or":
		f.expr(x.X)
		f.emit(DUP)
		op := JUMP_FALSE
		if x.Op == "or" {
			op = JUMP_TRUE
		}
		end := f.jump(op)
		f.emit(POP)
		f.expr(x.Y)
		f.patch(end)
		return
	case "default":
		f.expr(x.X)
		end := f.jump(JUMP_NOTNULL)
		f.emit(POP)
		f.expr(x.Y)
		f.patch(end)
		return
	}
	f.expr(x.X)
	f.expr(x.Y)
	lt, rt := f.operandType(x.X), f.operandType(x.Y)
	switch x.Op {
	case "+":
		if kindOf(t) == TString {
			f.emit(CONCAT)
			return
		}
		f.arith(t, lt, rt, [3]Op{ADD_I4, ADD_I8, ADD_DEC})
	case "-":
		f.arith(t, lt, rt, [3]Op{SUB_I4, SUB_I8, SUB_DEC})
	case "*":
		f.arith(t, lt, rt, [3]Op{MUL_I4, MUL_I8, MUL_DEC})
	case "/":
		f.arith(t, lt, rt, [3]Op{DIV_I4, DIV_I8, DIV_DEC})
	case "mod":
		f.arith(t, lt, rt, [3]Op{MOD_I4, MOD_I8, MOD_DEC})
	default:
		f.compare(x.Op, lt, rt)
		_ = info
	}
}

func (f *fn) arith(t, lt, rt types.Type, ops [3]Op) {
	switch kindOf(t) {
	case TInt4:
		f.emit(ops[0])
	case TInt8:
		f.emit(ops[1])
	case TDec:
		if ops[2] == MUL_DEC || ops[2] == NEG_DEC {
			f.emit(ops[2])
			return
		}
		f.emit(ops[2], scaleOf(lt), scaleOf(rt), scaleOf(t))
	}
}

var conds = map[string]int{"=": 0, "<>": 1, "<": 2, "<=": 3, ">": 4, ">=": 5}

func (f *fn) compare(op string, lt, rt types.Type) {
	k := kindOf(lt)
	if kindOf(rt) == TDec {
		k = TDec
	}
	switch k {
	case TInt4, TInt8, TDate, TTimestamp, TEnum:
		f.emit(CMP_I)
	case TDec:
		f.emit(CMP_DEC, scaleOf(lt), scaleOf(rt))
	case TString:
		f.emit(CMP_STR)
	case TBool:
		f.emit(CMP_BOOL)
	case TUUID:
		f.emit(CMP_UUID)
	case TBytes:
		f.emit(CMP_BYTES)
	case TStruct, TArray, TMap:
		f.emit(EQ_VALUE, f.c.typeDesc(lt))
		if op == "<>" {
			f.emit(NOT)
		}
		return
	default:
		f.emit(EQ_REF)
		if op == "<>" {
			f.emit(NOT)
		}
		return
	}
	f.emit(TEST, conds[op])
}

// convert emits an implicit conversion; values that are null stay null.
func (f *fn) convert(from, to types.Type) {
	fb, tb := types.BasicOf(types.NonOpt(from)), types.BasicOf(types.NonOpt(to))
	_, named := types.NonOpt(to).(*types.Named)
	if fb == nil || tb == nil || from == types.TypNull {
		return
	}
	needs := fb.Kind != tb.Kind && tb.Kind == types.Dec || fb.Kind == types.Dec && tb.Kind == types.Dec && (fb.Scale != tb.Scale || tb.Precision < types.MaxPrecision) || named
	if !needs {
		return
	}
	skip := -1
	if types.IsOptional(from) {
		skip = f.jump(JUMP_NULL)
	}
	switch {
	case fb.Kind != types.Dec && tb.Kind == types.Dec:
		f.emit(I_TO_DEC, tb.Scale)
		f.emit(DEC_FIT, tb.Precision)
	case fb.Kind == types.Dec && tb.Kind == types.Dec:
		if fb.Scale != tb.Scale {
			f.emit(DEC_RESCALE, fb.Scale, tb.Scale)
		}
		if tb.Precision < types.MaxPrecision {
			f.emit(DEC_FIT, tb.Precision)
		}
	}
	if named {
		f.emit(CHECK, f.c.typeDesc(types.NonOpt(to)))
	}
	if skip >= 0 {
		f.patch(skip)
	}
}

// conv emits an explicit conversion with as.
func (f *fn) conv(from, to types.Type) {
	fk, tk := kindOf(from), kindOf(to)
	extra := scaleOf(from)
	if e, ok := types.NonOpt(from).(*types.Enum); ok {
		extra = f.c.enum(e)
	}
	p := 0
	if b := types.BasicOf(to); b != nil {
		p = b.Precision
	}
	f.emit(CONV, int(fk), int(tk), p, scaleOf(to), extra)
	if _, named := to.(*types.Named); named {
		f.emit(CHECK, f.c.typeDesc(to))
	}
}

func (f *fn) selectExpr(x *syntax.SelectExpr) {
	info := f.c.info
	sel := info.Selects[x]
	switch sel.Kind {
	case types.SelectField:
		f.expr(x.X)
		st := types.NonOpt(info.Types[x.X]).(*types.Struct)
		f.getField(f.c.out.Layouts[f.c.structLayout(st)].Fields[sel.Field])
	case types.SelectAttribute:
		f.expr(x.X)
		f.getAttr(sel.Attribute)
	case types.SelectStatic:
		f.emit(STATICS, f.c.classRef(sel.Attribute.Owner))
		f.getAttr(sel.Attribute)
	case types.SelectConstant:
		f.constant(sel.Constant)
	case types.SelectEnum:
		f.c.enum(sel.Enum)
		f.emit(CONST_I, sel.EnumIndex)
	default:
		f.c.failf("cannot compile %s", syntax.PrintExpr(x))
	}
}

func (f *fn) call(x *syntax.CallExpr) {
	ci := f.c.info.Calls[x]
	if ci == nil {
		f.c.failf("unresolved call %s", syntax.PrintExpr(x.Fun))
	}
	f.line(x.Fun.Start())
	switch ci.Kind {
	case types.CallExtension:
		f.extensionCall(x, ci)
	case types.CallClosure:
		f.expr(x.Fun)
		f.args(ci.Args)
		f.emit(CALL_FN, len(ci.Args))
	case types.CallBuiltin:
		var recvType types.Type
		if ci.Receiver != nil {
			f.expr(ci.Receiver)
			recvType = types.NonOpt(f.c.info.Types[ci.Receiver])
		} else if g, ok := x.Fun.(*syntax.SelectExpr).X.(*syntax.GlobalExpr); ok {
			// Methods of an enum type, such as PARSE.
			recvType = f.c.info.Globals[g].(types.Type)
		} else {
			recvType = types.TypUUID // UUID.RANDOM()
		}
		f.args(ci.Args)
		f.emit(CALL_BUILTIN, int(ci.Builtin.ID), len(ci.Args), f.c.typeDesc(recvType), f.c.typeDesc(f.c.info.Types[x]))
	default:
		m := ci.Method
		argc := len(ci.Args)
		if !m.Static {
			if ci.Receiver != nil {
				f.expr(ci.Receiver)
			} else {
				f.emit(LOAD_ME)
			}
			argc++
		}
		f.args(ci.Args)
		switch ci.Kind {
		case types.CallVirtual:
			slot, _ := f.c.vslot(m)
			f.emit(CALL_VIRT, slot, argc)
		case types.CallInterface:
			iface := m.Owner
			if ci.Receiver != nil {
				if k, ok := types.NonOpt(f.c.info.Types[ci.Receiver]).(*types.Class); ok && k.IsInterface() {
					iface = k
				}
			}
			f.emit(CALL_IFACE, f.c.classRef(iface), slices.Index(f.c.ifaces[iface], m.Name), argc)
		default:
			f.emit(CALL, f.c.funcRef(m), argc)
		}
	}
}

func (f *fn) newExpr(x *syntax.NewExpr, t types.Type) {
	switch tt := types.NonOpt(t).(type) {
	case *types.Class:
		f.emit(NEW_OBJ, f.c.classRef(tt))
		f.emit(DUP)
		var args []syntax.Expr
		for _, a := range x.Args {
			args = append(args, a.Value)
		}
		f.args(args)
		f.emit(CALL, f.c.ctorRef(tt), len(args)+1)
	case *types.Struct:
		li := f.c.structLayout(tt)
		f.emit(NEW_STRUCT, li)
		for _, a := range x.Args {
			f.emit(DUP)
			f.valueOwn(a.Value)
			l := &f.c.out.Layouts[li]
			f.setField(l.Fields[l.Field(a.Name.Name)])
		}
	case *types.Array:
		f.emit(NEW_ARRAY, f.c.typeDesc(tt))
	case *types.Map:
		f.emit(NEW_MAP, f.c.typeDesc(tt))
	}
}

func (f *fn) funcExpr(x *syntax.FuncExpr) {
	info := f.c.info.Funcs[x]
	ft := info.Type
	file, chain := f.file, f.chain
	idx := f.c.newFunc(f.out().Name+".function", f.class, types.Site{File: file, Pos: x.Pos, Chain: chain}, func(g *fn) {
		o := g.out()
		o.Params = len(ft.Params)
		o.Captures = len(info.Captures)
		o.Instance = f.out().Instance
		for _, p := range ft.Params {
			o.ParamTypes = append(o.ParamTypes, g.c.typeDesc(p))
		}
		o.Result = g.c.typeDesc(ft.Result)
		g.result = ft.Result
		g.setLocals(info)
		g.line(x.Pos)
		g.boxParams()
		if x.Body != nil {
			g.stmts(x.Body.Stmts)
			g.emit(RETURN)
			return
		}
		g.valueOwn(x.Short)
		if ft.Result == types.TypVoid {
			g.emit(RETURN)
			return
		}
		g.emit(RETURN_VALUE)
	})
	for _, l := range info.Captures {
		if l.Func == f.info {
			f.emit(PUSH_CELL, l.Index)
		} else {
			f.emit(PUSH_UP, slices.Index(f.info.Captures, l))
		}
	}
	f.emit(MAKE_FN, idx, len(info.Captures))
}
