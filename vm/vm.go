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

// Package vm runs compiled programs: a stack machine with typed
// instructions, objects with a packed memory layout, checked arithmetic,
// exceptions with finally, and limits for instructions, call depth, memory
// and cancellation.
package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// Options are the limits and the log of an execution.
type Options struct {
	// Budget is the number of instructions an execution may run; 0 means
	// the default of 10 million.
	Budget int64
	// MaxCallDepth limits nested calls; 0 means 256.
	MaxCallDepth int
	// MaxMemory limits the bytes an execution may allocate; 0 means 256 MiB.
	MaxMemory int64
	// MaxCollectionLength limits the elements of one array, map or the
	// characters of one string; 0 means 10 million.
	MaxCollectionLength int
	// Log receives log statements; nil discards them.
	Log LogSink
	// Database gives access to the database; without it database
	// statements end the execution with an error.
	Database Database
	// MaxRows limits the rows a select reads into an array; 0 means
	// 10 000. Cursors of for ... in select have no limit.
	MaxRows int
	// Host gives native methods access to the kernel.
	Host native.Host
	// MaxNativeInput is the largest input of native methods in bytes; 0
	// means 16 MiB.
	MaxNativeInput int64
}

// LogSink receives log statements.
type LogSink interface {
	Log(level, text string)
}

// LogRecorder keeps log statements, for tests.
type LogRecorder struct{ Entries []LogEntry }

// LogEntry is one log statement.
type LogEntry struct{ Level, Text string }

// Log records an entry.
func (r *LogRecorder) Log(level, text string) { r.Entries = append(r.Entries, LogEntry{level, text}) }

// Exception is an exception that no catch handled.
type Exception struct {
	Class  string
	Text   string
	Object *Record
	Trace  []TraceFrame
}

// TraceFrame is a function on the stack when an exception was raised.
type TraceFrame struct {
	Func  string
	File  string
	Line  int
	Chain []string // "INCLUDE included from FILE:LINE", innermost first
}

func (e *Exception) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", e.Class, e.Text)
	for _, f := range e.Trace {
		fmt.Fprintf(&b, "\n    at %s (%s:%d)", f.Func, f.File, f.Line)
		for _, c := range f.Chain {
			fmt.Fprintf(&b, "\n        %s", c)
		}
	}
	return b.String()
}

// LimitError ends an execution that exceeded a limit. Code cannot catch it.
type LimitError struct {
	Limit string // budget, call depth, memory, collection length or canceled
	Msg   string
}

func (e *LimitError) Error() string { return e.Msg }

// Machine runs code of an image. It is not safe for concurrent use;
// images can be shared between machines. Static attributes belong to the
// machine.
type Machine struct {
	image   *Image
	opts    Options
	statics map[*LinkedClass]*Record
	budget  int64
	depth   int
	mem     int64
	ctx     context.Context
	steps   int

	defaultSession *session
	// callers are the methods that run, for the caller of native methods.
	callers []native.Caller
	// handles are the objects of native classes the execution created.
	handles []*Record
}

// New prepares an image for execution and runs the initial values of
// static attributes.
func New(image *Image, opts Options) (*Machine, error) {
	if opts.Budget <= 0 {
		opts.Budget = 10_000_000
	}
	if opts.MaxCallDepth <= 0 {
		opts.MaxCallDepth = 256
	}
	if opts.MaxMemory <= 0 {
		opts.MaxMemory = 256 << 20
	}
	if opts.MaxCollectionLength <= 0 {
		opts.MaxCollectionLength = 10_000_000
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = 10_000
	}
	if opts.MaxNativeInput <= 0 {
		opts.MaxNativeInput = 16 << 20
	}
	m := &Machine{image: image, opts: opts, statics: map[*LinkedClass]*Record{}, ctx: context.Background()}
	m.budget = opts.Budget
	for _, pr := range image.order {
		for _, c := range pr.Classes {
			if c.Statics != nil {
				m.statics[c] = m.newRecord(nil, pr.P, c.Statics)
			}
		}
	}
	for _, pr := range image.order {
		for _, c := range pr.Classes {
			if c.Init != nil {
				if _, err := m.run(c.Init, Value{}, nil, nil); err != nil {
					return nil, fmt.Errorf("initial values of %s: %w", c.Name, err)
				}
			}
		}
	}
	return m, nil
}

// Image returns the image of the machine.
func (m *Machine) Image() *Image { return m.image }

// Call runs a static method. The budget and memory limits start anew.
func (m *Machine) Call(ctx context.Context, class, method string, args ...Value) (Value, error) {
	c := m.image.Class(class)
	if c == nil {
		return Value{}, fmt.Errorf("vm: unknown class %s", class)
	}
	mi, ok := c.Method(method)
	if !ok {
		return Value{}, fmt.Errorf("vm: %s has no method %s", class, method)
	}
	f := mi.Func.Def
	if f.Instance {
		return Value{}, fmt.Errorf("vm: %s.%s is not static", class, method)
	}
	if len(args) != f.Params {
		return Value{}, fmt.Errorf("vm: %s.%s takes %d arguments, %d given", class, method, f.Params, len(args))
	}
	m.ctx, m.budget, m.mem, m.depth = ctx, m.opts.Budget, 0, 0
	return m.run(mi.Func, Value{}, args, nil)
}

// CallMethod creates an object of a class with its constructor without
// arguments and calls a method without arguments on it, for test methods.
func (m *Machine) CallMethod(ctx context.Context, class, method string) error {
	c := m.image.Class(class)
	if c == nil {
		return fmt.Errorf("vm: unknown class %s", class)
	}
	mi, ok := c.Method(method)
	if !ok {
		return fmt.Errorf("vm: %s has no method %s", class, method)
	}
	m.ctx, m.budget, m.mem, m.depth = ctx, m.opts.Budget, 0, 0
	obj := Value{R: m.newObject(c)}
	if _, err := m.run(c.Ctor, obj, nil, nil); err != nil {
		return err
	}
	_, err := m.run(mi.Func, obj, nil, nil)
	return err
}

// thrown is an exception on its way up the stack.
type thrown struct {
	obj   *Record
	trace []TraceFrame
	pc    int // position in the current function when raised again by finally, -1 otherwise
}

func (t *thrown) Error() string { return "exception" }

func (m *Machine) alloc(n int64) {
	m.mem += n
	if m.mem > m.opts.MaxMemory {
		panic(&LimitError{Limit: "memory", Msg: fmt.Sprintf("the execution allocated more than %d bytes", m.opts.MaxMemory)})
	}
}

// fail raises a runtime error of the VM.
func (m *Machine) fail(class, format string, args ...any) *thrown {
	r := m.newObject(m.image.rt[class])
	m.setField(r, m.image.textField, Str(fmt.Sprintf(format, args...)))
	return &thrown{obj: r, pc: -1}
}

// run executes a function and converts the result: an uncaught exception
// becomes *Exception at the outermost call.
func (m *Machine) run(fi *LinkedFunc, me Value, args []Value, cl *Closure) (v Value, err error) {
	v, t, err := m.exec(fi, me, args, cl)
	if err != nil {
		return Value{}, err
	}
	if t != nil {
		return Value{}, m.exception(t)
	}
	return v, nil
}

func (m *Machine) exception(t *thrown) *Exception {
	e := &Exception{Class: t.obj.Class.Name, Object: t.obj, Trace: t.trace}
	e.Text = m.getField(t.obj, m.image.textField).str()
	return e
}

// frameError adds the current function to the trace of an exception.
func (m *Machine) traceFrame(fn *LinkedFunc, pc int) TraceFrame {
	f, p := fn.Def, fn.Prog.P
	tf := TraceFrame{Func: f.Name}
	if l, ok := f.Position(pc); ok {
		tf.File, tf.Line = p.Files[l.File].Path, l.Line
		for _, s := range p.Chains[l.Chain].Sites {
			tf.Chain = append(tf.Chain, fmt.Sprintf("%s included from %s:%d", s.Include, p.Files[s.File].Path, s.Line))
		}
	}
	return tf
}

// isA reports whether an object is of class c or a subclass.
func (m *Machine) isA(obj *Record, c *LinkedClass) bool {
	return obj.Class != nil && obj.Class.Supers[c]
}

// exec runs a function. It returns the result, an exception that leaves
// the function, or a limit error.
func (m *Machine) exec(fn *LinkedFunc, me Value, args []Value, cl *Closure) (result Value, exc *thrown, err error) {
	m.depth++
	defer func() { m.depth-- }()
	if m.depth > m.opts.MaxCallDepth {
		return Value{}, nil, &LimitError{Limit: "call depth", Msg: fmt.Sprintf("more than %d nested calls", m.opts.MaxCallDepth)}
	}
	if fn.Def.Native {
		return m.callNative(fn, me, args)
	}
	class, method, _ := strings.Cut(fn.Def.Name, ".")
	m.callers = append(m.callers, native.Caller{Package: model.PackageID(fn.Prog.P.Package), Class: class, Method: method})
	defer func() { m.callers = m.callers[:len(m.callers)-1] }()
	f, pr := fn.Def, fn.Prog
	p := pr.P
	locals := make([]Value, f.Locals)
	copy(locals, args)
	stack := make([]Value, 0, 16)
	code := f.Code
	pc := 0
	start := 0 // pc of the current instruction

	// Cursors opened by this call are closed when it ends, also by return
	// or an exception.
	var cursors []*cursor
	defer func() {
		for _, c := range cursors {
			if cerr := c.close(); cerr != nil && err == nil {
				err = cerr
			}
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case *LimitError:
				result, exc, err = Value{}, nil, x
			default:
				panic(r)
			}
		}
	}()

	push := func(v Value) { stack = append(stack, v) }
	pop := func() Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	arg := func(i int) int { return int(code[start+1+i]) }

	// Exceptions caught by a finally handler keep their trace and position
	// when they are raised again.
	var pending map[*Record]*thrown
	// handle looks for a handler of an exception; it returns false when the
	// exception leaves the function.
	handle := func(t *thrown) bool {
		at := start
		if t.pc >= 0 {
			at = t.pc // the trace shows where the exception came from
		}
		for _, h := range f.Handlers {
			if start >= h.Start && start < h.End && (h.Class < 0 || m.isA(t.obj, pr.class(h.Class))) {
				if h.Class < 0 {
					if pending == nil {
						pending = map[*Record]*thrown{}
					}
					t.pc = at
					pending[t.obj] = t
				}
				// Cursors of loops inside the try end with them.
				for _, c := range cursors {
					if c.pc >= h.Start && c.pc < h.End {
						c.close()
					}
				}
				stack = stack[:0]
				push(Value{R: t.obj})
				pc = h.Target
				return true
			}
		}
		t.trace = append(t.trace, m.traceFrame(fn, at))
		t.pc = -1
		return false
	}
	call := func(target *LinkedFunc, argc int, closure *Closure) bool {
		callee := target.Def
		base := len(stack) - argc
		var recv Value
		cargs := stack[base:]
		if callee.Instance && closure == nil {
			recv = cargs[0]
			if recv.IsNull() {
				exc = m.fail("/RT/VALUE_ERROR", "method called on null")
				return false
			}
			cargs = cargs[1:]
		} else if closure != nil {
			recv = closure.Me
		}
		args := append([]Value(nil), cargs...)
		stack = stack[:base]
		v, t, e := m.exec(target, recv, args, closure)
		if e != nil {
			err = e
			return false
		}
		if t != nil {
			exc = t
			return false
		}
		if callee.Result >= 0 {
			push(v)
		}
		return true
	}

	for {
		if pc >= len(code) {
			return Value{}, nil, nil
		}
		m.budget--
		if m.budget < 0 {
			return Value{}, nil, &LimitError{Limit: "budget", Msg: fmt.Sprintf("the execution exceeded its budget of %d instructions", m.opts.Budget)}
		}
		if m.steps++; m.steps&1023 == 0 && m.ctx.Err() != nil {
			return Value{}, nil, &LimitError{Limit: "canceled", Msg: "the execution was canceled: " + m.ctx.Err().Error()}
		}
		start = pc
		op := Op(code[pc])
		pc += 1 + op.Operands()
		var t *thrown
		switch op {
		case NOP:
		case CONST_I:
			push(Int(int64(arg(0))))
		case CONST_I64:
			push(Int(int64(uint64(uint32(arg(0)))<<32 | uint64(uint32(arg(1))))))
		case CONST_DEC, CONST_UUID:
			d := p.Decs[arg(0)]
			push(Value{N: d[0], X: d[1]})
		case CONST_STR:
			push(Str(p.Strings[arg(0)]))
		case CONST_BYTES:
			push(Value{R: p.Bytes[arg(0)]})
		case TRUE:
			push(Bool(true))
		case FALSE:
			push(Bool(false))
		case NULL:
			push(Null)
		case ZERO:
			push(m.zero(p, arg(0)))
		case LOAD:
			push(locals[arg(0)])
		case STORE:
			locals[arg(0)] = pop()
		case LOAD_CELL:
			push(locals[arg(0)].R.(*Cell).V)
		case STORE_CELL:
			locals[arg(0)].R.(*Cell).V = pop()
		case NEW_CELL:
			locals[arg(0)] = Value{R: &Cell{V: pop()}}
		case PUSH_CELL:
			push(locals[arg(0)])
		case LOAD_UP:
			push(cl.Cells[arg(0)].V)
		case STORE_UP:
			cl.Cells[arg(0)].V = pop()
		case PUSH_UP:
			push(Value{R: cl.Cells[arg(0)]})
		case LOAD_ME:
			push(me)
		case DUP:
			push(stack[len(stack)-1])
		case POP:
			pop()
		case SWAP:
			n := len(stack)
			stack[n-1], stack[n-2] = stack[n-2], stack[n-1]
		case COPY:
			push(m.copyValue(p, arg(0), pop()))

		case ADD_I4, SUB_I4, MUL_I4, DIV_I4, MOD_I4, ADD_I8, SUB_I8, MUL_I8, DIV_I8, MOD_I8:
			b, a := pop().int(), pop().int()
			var r int64
			ok := true
			switch op {
			case ADD_I4, ADD_I8:
				r, ok = addInt(a, b)
			case SUB_I4, SUB_I8:
				r, ok = subInt(a, b)
			case MUL_I4, MUL_I8:
				r, ok = mulInt(a, b)
			case DIV_I4, DIV_I8, MOD_I4, MOD_I8:
				if b == 0 {
					t = m.fail("/RT/DIVISION_BY_ZERO", "division by zero")
					break
				}
				if a == -1<<63 && b == -1 {
					ok = false
					break
				}
				if op == DIV_I4 || op == DIV_I8 {
					r = a / b
				} else {
					r = a % b
				}
			}
			if t != nil {
				break
			}
			int4 := op <= NEG_I4
			if !ok || int4 && !fitsInt4(r) {
				typ := "INT8"
				if int4 {
					typ = "INT4"
				}
				t = m.fail("/RT/OVERFLOW", "the result of %d %s %d exceeds %s", a, opSymbol(op), b, typ)
				break
			}
			push(Int(r))
		case NEG_I4, NEG_I8:
			a := pop().int()
			if a == -1<<63 || op == NEG_I4 && !fitsInt4(-a) {
				t = m.fail("/RT/OVERFLOW", "-%d exceeds its type", a)
				break
			}
			push(Int(-a))
		case ADD_DEC, SUB_DEC, MOD_DEC:
			b, a := pop().dec(), pop().dec()
			sa, sb, sr := arg(0), arg(1), arg(2)
			x, e1 := dec.Rescale(a, sa, sr)
			y, e2 := dec.Rescale(b, sb, sr)
			var r dec.Dec
			var e3 error
			switch op {
			case ADD_DEC:
				r, e3 = dec.Add(x, y)
			case SUB_DEC:
				r, e3 = dec.Sub(x, y)
			default:
				r, e3 = dec.Mod(x, y)
			}
			t = m.decErr(errors.Join(e1, e2, e3))
			if t == nil {
				push(DecValue(r))
			}
		case MUL_DEC:
			b, a := pop().dec(), pop().dec()
			r, e := dec.Mul(a, b)
			if t = m.decErr(e); t == nil {
				push(DecValue(r))
			}
		case DIV_DEC:
			b, a := pop().dec(), pop().dec()
			r, e := dec.Div(a, arg(0), b, arg(1), arg(2))
			if t = m.decErr(e); t == nil {
				push(DecValue(r))
			}
		case NEG_DEC:
			push(DecValue(dec.Negate(pop().dec())))
		case DEC_RESCALE:
			r, e := dec.Rescale(pop().dec(), arg(0), arg(1))
			if t = m.decErr(e); t == nil {
				push(DecValue(r))
			}
		case DEC_FIT:
			v := stack[len(stack)-1]
			if !dec.Fits(v.dec(), arg(0), 0) {
				t = m.fail("/RT/OVERFLOW", "the number has more than %d digits", arg(0))
			}
		case I_TO_DEC:
			r, e := dec.Rescale(dec.FromInt64(pop().int()), 0, arg(0))
			if t = m.decErr(e); t == nil {
				push(DecValue(r))
			}
		case CONCAT:
			b, a := pop().str(), pop().str()
			t = m.checkLength(len(a) + len(b))
			if t == nil {
				m.alloc(int64(len(a) + len(b)))
				push(Str(a + b))
			}
		case CONCATN:
			n := arg(0)
			var sb strings.Builder
			for _, v := range stack[len(stack)-n:] {
				sb.WriteString(v.str())
			}
			stack = stack[:len(stack)-n]
			t = m.checkLength(sb.Len())
			if t == nil {
				m.alloc(int64(sb.Len()))
				push(Str(sb.String()))
			}
		case FMT:
			push(Str(m.format(p, TypeKind(arg(0)), arg(1), pop())))

		case CMP_I:
			b, a := pop().int(), pop().int()
			push(Int(int64(cmp3(a < b, a > b))))
		case CMP_STR:
			b, a := pop().str(), pop().str()
			push(Int(int64(strings.Compare(a, b))))
		case CMP_DEC:
			b, a := pop().dec(), pop().dec()
			s := max(arg(0), arg(1))
			x, e1 := dec.Rescale(a, arg(0), s)
			y, e2 := dec.Rescale(b, arg(1), s)
			if t = m.decErr(errors.Join(e1, e2)); t == nil {
				push(Int(int64(dec.Cmp(x, y))))
			}
		case CMP_BOOL:
			b, a := pop().N, pop().N
			push(Int(int64(cmp3(a < b, a > b))))
		case CMP_UUID:
			b, a := pop(), pop()
			push(Int(int64(cmp3(a.X < b.X || a.X == b.X && a.N < b.N, a.X > b.X || a.X == b.X && a.N > b.N))))
		case CMP_BYTES:
			b, a := pop(), pop()
			x, _ := a.R.([]byte)
			y, _ := b.R.([]byte)
			push(Int(int64(bytes.Compare(x, y))))
		case EQ_VALUE:
			b, a := pop(), pop()
			push(Bool(m.equal(p, arg(0), a, b)))
		case EQ_REF:
			b, a := pop(), pop()
			push(Bool(a.R == b.R))
		case TEST:
			c := pop().int()
			var r bool
			switch arg(0) {
			case 0:
				r = c == 0
			case 1:
				r = c != 0
			case 2:
				r = c < 0
			case 3:
				r = c <= 0
			case 4:
				r = c > 0
			case 5:
				r = c >= 0
			}
			push(Bool(r))
		case NOT:
			push(Bool(!pop().bool()))
		case IS_NULL:
			push(Bool(pop().IsNull()))
		case IS_EMPTY:
			v := pop()
			switch x := v.R.(type) {
			case string:
				push(Bool(x == ""))
			case []byte:
				push(Bool(len(x) == 0))
			case *Array:
				push(Bool(x.Len() == 0))
			case *Map:
				push(Bool(len(x.Keys) == 0))
			default:
				push(Bool(false))
			}
		case CONV:
			v, ct := m.convert(p, TypeKind(arg(0)), TypeKind(arg(1)), arg(2), arg(3), arg(4), pop())
			if t = ct; t == nil {
				push(v)
			}
		case CHECK:
			t = m.check(p, arg(0), stack[len(stack)-1])

		case JUMP:
			pc = arg(0)
		case JUMP_FALSE:
			if !pop().bool() {
				pc = arg(0)
			}
		case JUMP_TRUE:
			if pop().bool() {
				pc = arg(0)
			}
		case JUMP_NULL:
			if stack[len(stack)-1].IsNull() {
				pc = arg(0)
			}
		case JUMP_NOTNULL:
			if !stack[len(stack)-1].IsNull() {
				pc = arg(0)
			}

		case GETBOOL:
			r := pop().record()
			push(Bool(getBit(r.Data, arg(0), arg(1))))
		case SETBOOL:
			v := pop()
			setBit(pop().record().Data, arg(0), arg(1), v.bool())
		case GETOBOOL:
			r := pop().record()
			if !getBit(r.Data, arg(0), arg(1)) {
				push(Null)
			} else {
				push(Bool(getBit(r.Data, arg(0), arg(1)+1)))
			}
		case SETOBOOL:
			v := pop()
			r := pop().record()
			setBit(r.Data, arg(0), arg(1), !v.IsNull())
			setBit(r.Data, arg(0), arg(1)+1, !v.IsNull() && v.bool())
		case GETI4, GETI8, GETDEC, GETUUID, GETDATE, GETTS, GETENUM:
			r := pop().record()
			if pres := arg(1); pres >= 0 && !getBit(r.Data, pres, arg(2)) {
				push(Null)
				break
			}
			push(readFixed(r.Data, arg(0), fixedKind(op)))
		case SETI4, SETI8, SETDEC, SETUUID, SETDATE, SETTS, SETENUM:
			v := pop()
			r := pop().record()
			if pres := arg(1); pres >= 0 {
				setBit(r.Data, pres, arg(2), !v.IsNull())
				if v.IsNull() {
					break
				}
			}
			writeFixed(r.Data, arg(0), fixedKind(op), v)
		case GETREF:
			r := pop().record()
			if pres := arg(1); pres >= 0 && !getBit(r.Data, pres, arg(2)) {
				push(Null)
				break
			}
			push(refValue(r.Refs[arg(0)]))
		case SETREF:
			v := pop()
			r := pop().record()
			if pres := arg(1); pres >= 0 {
				setBit(r.Data, pres, arg(2), !v.IsNull())
			}
			if v.IsNull() {
				r.Refs[arg(0)] = nil
			} else {
				r.Refs[arg(0)] = v.R
			}
		case STATICS:
			push(Value{R: m.statics[pr.class(arg(0))]})

		case NEW_OBJ:
			push(Value{R: m.newObject(pr.class(arg(0)))})
		case NEW_STRUCT:
			push(Value{R: m.newRecord(nil, p, &p.Layouts[arg(0)])})
		case NEW_ARRAY:
			push(Value{R: m.newArray(p, arg(0))})
		case NEW_MAP:
			push(Value{R: m.newMap(p, arg(0))})
		case ARRAY_LIT:
			n := arg(1)
			a := m.newArray(p, arg(0))
			for _, v := range stack[len(stack)-n:] {
				if t = m.appendElem(a, v); t != nil {
					break
				}
			}
			stack = stack[:len(stack)-n]
			if t == nil {
				push(Value{R: a})
			}
		case MAP_LIT:
			n := arg(1)
			mp := m.newMap(p, arg(0))
			items := stack[len(stack)-2*n:]
			for i := 0; i < n && t == nil; i++ {
				t = m.mapSet(mp, items[2*i], items[2*i+1])
			}
			stack = stack[:len(stack)-2*n]
			if t == nil {
				push(Value{R: mp})
			}
		case INDEX_GET:
			i := pop().int()
			a := pop().array()
			if i < 1 || i > int64(a.Len()) {
				t = m.fail("/RT/INDEX_ERROR", "index %d is outside of 1 to %d", i, a.Len())
				break
			}
			push(a.Get(int(i - 1)))
		case INDEX_SET:
			v := pop()
			i := pop().int()
			a := pop().array()
			if i < 1 || i > int64(a.Len()) {
				t = m.fail("/RT/INDEX_ERROR", "index %d is outside of 1 to %d", i, a.Len())
				break
			}
			t = m.setElem(a, int(i-1), v)
		case MINDEX_GET:
			k := pop()
			mp := pop().mapv()
			kd := &mp.P.Types[mp.Key]
			i, ok := mp.index[keyOf(kd, k)]
			if !ok {
				t = m.fail("/RT/KEY_ERROR", "the map has no key %s", m.format(mp.P, kd.Kind, kd.Scale, k))
				break
			}
			push(mp.Vals[i])
		case MINDEX_SET:
			v := pop()
			k := pop()
			t = m.mapSet(pop().mapv(), k, v)

		case CALL:
			if !call(pr.fn(arg(0)), arg(1), nil) {
				t, exc = exc, nil
			}
		case CALL_VIRT:
			argc := arg(1)
			recv := stack[len(stack)-argc].record()
			if recv == nil {
				t = m.fail("/RT/VALUE_ERROR", "method called on null")
				break
			}
			if !call(recv.Class.VTable[arg(0)], argc, nil) {
				t, exc = exc, nil
			}
		case CALL_IFACE:
			argc := arg(2)
			recv := stack[len(stack)-argc].record()
			if recv == nil {
				t = m.fail("/RT/VALUE_ERROR", "method called on null")
				break
			}
			funcs := recv.Class.ITabs[pr.class(arg(0))]
			if !call(funcs[arg(1)], argc, nil) {
				t, exc = exc, nil
			}
		case CALL_FN:
			argc := arg(0)
			clo := stack[len(stack)-argc-1].R.(*Closure)
			callArgs := append([]Value(nil), stack[len(stack)-argc:]...)
			stack = stack[:len(stack)-argc-1]
			v, tt, e := m.exec(clo.Fn, clo.Me, callArgs, clo)
			if e != nil {
				return Value{}, nil, e
			}
			if tt != nil {
				t = tt
				break
			}
			if clo.Fn.Def.Result >= 0 {
				push(v)
			}
		case CALL_BUILTIN:
			argc := arg(1)
			n := argc
			if hasReceiver(arg(0)) {
				n++
			}
			vals := append([]Value(nil), stack[len(stack)-n:]...)
			stack = stack[:len(stack)-n]
			v, tt, e := m.builtin(p, arg(0), arg(2), arg(3), vals)
			if e != nil {
				return Value{}, nil, e
			}
			if tt != nil {
				t = tt
				break
			}
			if arg(3) >= 0 {
				push(v)
			}
		case MAKE_FN:
			n := arg(1)
			clo := &Closure{Fn: pr.Funcs[arg(0)], Me: me}
			for _, c := range stack[len(stack)-n:] {
				clo.Cells = append(clo.Cells, c.R.(*Cell))
			}
			stack = stack[:len(stack)-n]
			push(Value{R: clo})
		case RETURN:
			return Value{}, nil, nil
		case RETURN_VALUE:
			return pop(), nil, nil
		case RAISE:
			obj := pop().record()
			if prev, ok := pending[obj]; ok {
				delete(pending, obj)
				t = prev
			} else {
				t = &thrown{obj: obj, pc: -1}
			}
		case ASSERT:
			msg := "assertion failed"
			if arg(0) == 1 {
				msg = pop().str()
			}
			if !pop().bool() {
				t = m.fail("/RT/ASSERTION_FAILED", "%s", msg)
			}
		case LOG:
			text := pop().str()
			if m.opts.Log != nil {
				m.opts.Log.Log([]string{"debug", "info", "warning", "error"}[arg(0)], text)
			}
		case FOR_TEST:
			i, to, step := locals[arg(0)].int(), locals[arg(1)].int(), locals[arg(2)].int()
			switch {
			case step == 0:
				t = m.fail("/RT/VALUE_ERROR", "the step of a loop must not be 0")
			case step > 0 && i > to, step < 0 && i < to:
				pc = arg(3)
			}
		case FOR_STEP:
			n, ok := addInt(locals[arg(0)].int(), locals[arg(2)].int())
			if !ok || arg(4) == 1 && !fitsInt4(n) {
				pc = arg(3)
				break
			}
			locals[arg(0)] = Int(n)
		case DB_QUERY, DB_CURSOR:
			var res Value
			var ok bool
			var c *cursor
			stack, res, ok, c, t, err = m.dbQuery(p, &p.Queries[arg(0)], stack, op == DB_CURSOR)
			if c != nil {
				c.pc = start
				cursors = append(cursors, c)
			}
			if ok && t == nil && err == nil {
				push(res)
			}
		case DB_NEXT:
			c := pop().R.(*cursor)
			var row Value
			var ok bool
			row, ok, t, err = m.next(c)
			if ok {
				push(row)
			} else if t == nil {
				pc = arg(0)
			}
		case DB_CLOSE:
			err = pop().R.(*cursor).close()
		case DB_END:
			t, err = m.endSession(pop(), arg(0) == 1)
		case DB_OPEN:
			var s Value
			s, t, err = m.openSession()
			if t == nil && err == nil {
				push(s)
			}
		default:
			return Value{}, nil, fmt.Errorf("vm: unknown instruction %s at %d in %s", op, start, f.Name)
		}
		if err != nil {
			return Value{}, nil, err
		}
		if t != nil {
			if !handle(t) {
				return Value{}, t, nil
			}
		}
	}
}

func cmp3(lt, gt bool) int {
	switch {
	case lt:
		return -1
	case gt:
		return 1
	}
	return 0
}

func opSymbol(op Op) string {
	switch op {
	case ADD_I4, ADD_I8:
		return "+"
	case SUB_I4, SUB_I8:
		return "-"
	case MUL_I4, MUL_I8:
		return "*"
	case DIV_I4, DIV_I8:
		return "/"
	}
	return "mod"
}

func fixedKind(op Op) TypeKind {
	switch op {
	case GETI4, SETI4:
		return TInt4
	case GETI8, SETI8:
		return TInt8
	case GETDEC, SETDEC:
		return TDec
	case GETUUID, SETUUID:
		return TUUID
	case GETDATE, SETDATE:
		return TDate
	case GETTS, SETTS:
		return TTimestamp
	}
	return TEnum
}

func (m *Machine) decErr(err error) *thrown {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dec.ErrDivisionByZero):
		return m.fail("/RT/DIVISION_BY_ZERO", "division by zero")
	}
	return m.fail("/RT/OVERFLOW", "the number has more than %d digits", dec.MaxDigits)
}

func (m *Machine) checkLength(n int) *thrown {
	if n > m.opts.MaxCollectionLength {
		panic(&LimitError{Limit: "collection length", Msg: fmt.Sprintf("a string or collection exceeds %d elements", m.opts.MaxCollectionLength)})
	}
	return nil
}
