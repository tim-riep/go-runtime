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
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// nativeFunc is a native method bound to its implementation.
type nativeFunc struct {
	m      *native.Method
	fn     reflect.Value
	in     []reflect.Type // Go types of the parameters after the context
	out    reflect.Type   // Go type of the result, nil without result
	raises []*LinkedClass
	class  *native.Class // the native class of instance methods
}

var (
	typString    = reflect.TypeOf("")
	typInt32     = reflect.TypeOf(int32(0))
	typInt64     = reflect.TypeOf(int64(0))
	typDec       = reflect.TypeOf(dec.Dec{})
	typDate      = reflect.TypeOf(native.Date(0))
	typTimestamp = reflect.TypeOf(time.Time{})
	typBool      = reflect.TypeOf(false)
	typUUID      = reflect.TypeOf(native.UUID{})
	typBytes     = reflect.TypeOf([]byte(nil))
	typSession   = reflect.TypeOf(native.Session(0))
)

// optional is implemented by model.Optional.
type optional interface {
	AnyValue() (any, bool)
}

type settableOptional interface {
	SetAny(v any, valid bool) bool
}

var optionalType = reflect.TypeOf((*optional)(nil)).Elem()

// optionalElem returns T of model.Optional[T].
func optionalElem(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() != reflect.Struct || !t.Implements(optionalType) || t.NumField() != 2 || t.PkgPath() != reflect.TypeOf(model.Optional[int]{}).PkgPath() {
		return nil, false
	}
	return t.Field(0).Type, true
}

// bindNatives binds the native functions of the linked programs.
func (image *Image) bindNatives(reg *native.Registry) error {
	for _, pr := range image.order {
		for _, f := range pr.Funcs {
			if !f.Def.Native {
				continue
			}
			class, method, _ := strings.Cut(f.Def.Name, ".")
			m, ok := reg.Lookup(model.PackageID(pr.P.Package), class, method)
			if !ok {
				return fmt.Errorf("vm: no implementation of %s is registered in the kernel", f.Def.Name)
			}
			nf := &nativeFunc{m: m, fn: reflect.ValueOf(m.Func)}
			ft := nf.fn.Type()
			first := 1
			if f.Def.Instance {
				c, ok := reg.LookupClass(class)
				if !ok {
					return fmt.Errorf("vm: %s is an instance method of %s, which is no native class", f.Def.Name, class)
				}
				if ft.In(1) != c.Type {
					return fmt.Errorf("vm: %s: the Go value of %s is %s, not %s", f.Def.Name, class, c.Type, ft.In(1))
				}
				nf.class = c
				first = 2
			}
			if ft.NumIn()-first != len(f.Def.ParamTypes) {
				return fmt.Errorf("vm: %s takes %d parameters, its implementation %d", f.Def.Name, len(f.Def.ParamTypes), ft.NumIn()-first)
			}
			for i, t := range f.Def.ParamTypes {
				gt := ft.In(first + i)
				if err := image.compatible(reg, pr.P, t, gt); err != nil {
					return fmt.Errorf("vm: %s, parameter %d: %w", f.Def.Name, i+1, err)
				}
				nf.in = append(nf.in, gt)
			}
			switch {
			case f.Def.Result < 0 && ft.NumOut() != 1:
				return fmt.Errorf("vm: %s has no result; its implementation must return only error", f.Def.Name)
			case f.Def.Result >= 0 && ft.NumOut() != 2:
				return fmt.Errorf("vm: %s has a result; its implementation must return it and error", f.Def.Name)
			case f.Def.Result >= 0:
				nf.out = ft.Out(0)
				if err := image.compatible(reg, pr.P, f.Def.Result, nf.out); err != nil {
					return fmt.Errorf("vm: %s, result: %w", f.Def.Name, err)
				}
			}
			for _, r := range f.Def.Raises {
				nf.raises = append(nf.raises, pr.class(r))
			}
			f.native = nf
		}
	}
	return nil
}

// compatible checks that Go values of type gt represent values of type t.
func (image *Image) compatible(reg *native.Registry, p *Program, t int, gt reflect.Type) error {
	d := &p.Types[t]
	if d.Optional {
		elem, ok := optionalElem(gt)
		if !ok && d.Kind != TObject {
			return fmt.Errorf("an optional value needs model.Optional, not %s", gt)
		}
		if d.Kind == TObject {
			elem = gt
		}
		nd := *d
		nd.Optional = false
		return image.compatibleDesc(reg, p, &nd, elem)
	}
	return image.compatibleDesc(reg, p, d, gt)
}

func (image *Image) compatibleDesc(reg *native.Registry, p *Program, d *TypeDesc, gt reflect.Type) error {
	want := map[TypeKind]reflect.Type{TString: typString, TInt4: typInt32, TInt8: typInt64, TDec: typDec, TDate: typDate,
		TTimestamp: typTimestamp, TBool: typBool, TUUID: typUUID, TBytes: typBytes, TSession: typSession}[d.Kind]
	switch d.Kind {
	case TEnum:
		base := reflect.String
		if p.Enums[d.Enum].Base != TString {
			base = reflect.Int64
		}
		if gt.Kind() != base {
			return fmt.Errorf("the enum %s needs a Go type of kind %s, not %s", d.Name, base, gt)
		}
		return nil
	case TStruct:
		if gt.Kind() != reflect.Struct {
			return fmt.Errorf("%s needs a Go struct, not %s", d.Name, gt)
		}
		l := &p.Layouts[d.Layout]
		fields := structFields(gt)
		if len(fields) != len(l.Fields) {
			return fmt.Errorf("%s has %d fields, the Go type %s %d; generate it with rt gen natives", d.Name, len(l.Fields), gt, len(fields))
		}
		for _, f := range l.Fields {
			i, ok := fields[f.Name]
			if !ok {
				return fmt.Errorf("the Go type %s has no field with the tag rt:%q", gt, f.Name)
			}
			if err := image.compatible(reg, p, f.Type, gt.Field(i).Type); err != nil {
				return fmt.Errorf("%s.%s: %w", d.Name, f.Name, err)
			}
		}
		return nil
	case TArray:
		if gt.Kind() != reflect.Slice || gt == typBytes {
			return fmt.Errorf("an array needs a Go slice, not %s", gt)
		}
		return image.compatible(reg, p, d.Elem, gt.Elem())
	case TObject:
		c, ok := reg.LookupClass(d.Name)
		if !ok {
			return fmt.Errorf("%s is no native class; native methods take only objects of native classes", d.Name)
		}
		if gt != c.Type {
			return fmt.Errorf("objects of %s are %s in Go, not %s", d.Name, c.Type, gt)
		}
		return nil
	}
	if want == nil {
		return fmt.Errorf("values of this type cannot be passed to native methods")
	}
	if gt != want {
		return fmt.Errorf("needs %s, not %s", want, gt)
	}
	return nil
}

// structFields maps the rt tags of a Go struct to field indexes.
func structFields(t reflect.Type) map[string]int {
	out := map[string]int{}
	for i := range t.NumField() {
		if tag, ok := t.Field(i).Tag.Lookup("rt"); ok {
			out[tag] = i
		}
	}
	return out
}

// toNative converts a value of type t into a Go value of type gt.
func (m *Machine) toNative(p *Program, t int, v Value, gt reflect.Type) reflect.Value {
	d := &p.Types[t]
	if d.Optional && d.Kind != TObject {
		out := reflect.New(gt).Elem()
		so := out.Addr().Interface().(settableOptional)
		if v.IsNull() {
			so.SetAny(nil, false)
			return out
		}
		elem, _ := optionalElem(gt)
		nd := *d
		nd.Optional = false
		so.SetAny(m.toNativeDesc(p, &nd, v, elem).Interface(), true)
		return out
	}
	return m.toNativeDesc(p, d, v, gt)
}

func (m *Machine) toNativeDesc(p *Program, d *TypeDesc, v Value, gt reflect.Type) reflect.Value {
	switch d.Kind {
	case TString:
		return reflect.ValueOf(v.str())
	case TInt4:
		return reflect.ValueOf(int32(v.int()))
	case TInt8:
		return reflect.ValueOf(v.int())
	case TDec:
		return reflect.ValueOf(v.dec())
	case TDate:
		return reflect.ValueOf(native.Date(v.int()))
	case TTimestamp:
		return reflect.ValueOf(time.UnixMicro(v.int()).UTC())
	case TBool:
		return reflect.ValueOf(v.bool())
	case TUUID:
		var u native.UUID
		putUUID(u[:], v)
		return reflect.ValueOf(u)
	case TBytes:
		b, _ := v.R.([]byte)
		return reflect.ValueOf(append([]byte{}, b...))
	case TEnum:
		s := p.Enums[d.Enum].Values[v.int()]
		out := reflect.New(gt).Elem()
		if gt.Kind() == reflect.String {
			out.SetString(s)
		} else {
			n, _ := strconv.ParseInt(s, 10, 64)
			out.SetInt(n)
		}
		return out
	case TStruct:
		r := v.record()
		out := reflect.New(gt).Elem()
		fields := structFields(gt)
		for _, f := range r.Layout.Fields {
			out.Field(fields[f.Name]).Set(m.toNative(p, f.Type, m.getField(r, f), gt.Field(fields[f.Name]).Type))
		}
		return out
	case TArray:
		a := v.array()
		out := reflect.MakeSlice(gt, a.Len(), a.Len())
		for i := range a.Len() {
			out.Index(i).Set(m.toNative(p, d.Elem, a.Get(i), gt.Elem()))
		}
		return out
	case TObject:
		if v.IsNull() {
			return reflect.Zero(gt)
		}
		return reflect.ValueOf(v.record().Native)
	case TSession:
		n := 1
		if s, ok := v.R.(*session); ok {
			n = s.s.Number()
		}
		return reflect.ValueOf(native.Session(n))
	}
	panic(fmt.Sprintf("vm: no Go value for type kind %d", d.Kind))
}

// fromNative converts a Go value into a value of type t and checks the
// rules of named types.
func (m *Machine) fromNative(p *Program, t int, rv reflect.Value) (Value, *thrown) {
	d := &p.Types[t]
	if d.Optional && d.Kind != TObject {
		x, ok := rv.Interface().(optional).AnyValue()
		if !ok {
			return Null, nil
		}
		nd := *d
		nd.Optional = false
		v, th := m.fromNativeDesc(p, &nd, reflect.ValueOf(x))
		if th == nil && d.Named {
			th = m.check(p, t, v)
		}
		return v, th
	}
	v, th := m.fromNativeDesc(p, d, rv)
	if th == nil && d.Named {
		th = m.check(p, t, v)
	}
	return v, th
}

func (m *Machine) fromNativeDesc(p *Program, d *TypeDesc, rv reflect.Value) (Value, *thrown) {
	switch d.Kind {
	case TString:
		return Str(rv.String()), nil
	case TInt4, TInt8, TDate:
		return Int(rv.Int()), nil
	case TDec:
		x := rv.Interface().(dec.Dec)
		if !dec.Fits(x, d.Precision, d.Scale) {
			return Value{}, m.fail("/RT/OVERFLOW", "the result has more digits than DEC(%d,%d)", d.Precision, d.Scale)
		}
		return DecValue(x), nil
	case TTimestamp:
		return Int(rv.Interface().(time.Time).UTC().UnixMicro()), nil
	case TBool:
		return Bool(rv.Bool()), nil
	case TUUID:
		u := rv.Interface().(native.UUID)
		return uuidValue(u[:]), nil
	case TBytes:
		b := rv.Bytes()
		if b == nil {
			b = []byte{}
		}
		return Value{R: append([]byte{}, b...)}, nil
	case TEnum:
		s := rv.String()
		if rv.Kind() != reflect.String {
			s = strconv.FormatInt(rv.Int(), 10)
		}
		e := &p.Enums[d.Enum]
		for i, val := range e.Values {
			if val == s {
				return Int(int64(i)), nil
			}
		}
		return Value{}, m.fail("/RT/VALUE_ERROR", "%s has no fixed value %q", e.Name, s)
	case TStruct:
		l := &p.Layouts[d.Layout]
		r := &Record{Layout: l, P: p, Data: make([]byte, l.Size), Refs: make([]any, l.Refs)}
		m.alloc(int64(l.Size + 16*l.Refs + 48))
		fields := structFields(rv.Type())
		for _, f := range l.Fields {
			fv, th := m.fromNative(p, f.Type, rv.Field(fields[f.Name]))
			if th != nil {
				return Value{}, th
			}
			m.setField(r, f, fv)
		}
		return Value{R: r}, nil
	case TArray:
		a := m.newArrayOf(p, d.Elem)
		for i := range rv.Len() {
			ev, th := m.fromNative(p, d.Elem, rv.Index(i))
			if th != nil {
				return Value{}, th
			}
			if th := m.appendElem(a, ev); th != nil {
				return Value{}, th
			}
		}
		return Value{R: a}, nil
	case TObject:
		if rv.IsNil() {
			if d.Optional {
				return Null, nil
			}
			return Value{}, m.fail("/RT/VALUE_ERROR", "a native method returned no object of %s", d.Name)
		}
		c := m.image.classes[d.Name]
		r := m.newObject(c)
		r.Native = rv.Interface()
		m.handles = append(m.handles, r)
		return Value{R: r}, nil
	}
	return Value{}, m.fail("/RT/VALUE_ERROR", "no value for type kind %d", d.Kind)
}

// callNative runs a native function.
func (m *Machine) callNative(fn *LinkedFunc, me Value, args []Value) (result Value, exc *thrown, err error) {
	nf := fn.native
	if nf == nil {
		return Value{}, nil, fmt.Errorf("vm: %s is executed by kernel but not linked to an implementation", fn.Def.Name)
	}
	p := fn.Prog.P
	ctx := m.ctx
	if nf.m.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, nf.m.Timeout)
		defer cancel()
	}
	c := &native.Context{Context: ctx, Host: m.opts.Host, MaxInput: m.opts.MaxNativeInput, Method: fn.Def.Name, Limits: m.opts.Limits}
	if len(m.callers) > 0 {
		c.Caller = m.callers[len(m.callers)-1]
		c.Callers = slices.Clone(m.callers)
	}
	in := []reflect.Value{reflect.ValueOf(c)}
	if fn.Def.Instance {
		r := me.record()
		if r.released {
			return Value{}, m.nativeError(fn, "the object of %s was released", r.Class.Name), nil
		}
		in = append(in, reflect.ValueOf(r.Native))
	}
	for i, a := range args {
		in = append(in, m.toNative(p, fn.Def.ParamTypes[i], a, nf.in[i]))
	}
	var out []reflect.Value
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in a native method", "method", nf.m.Key(), "panic", r, "stack", string(debug.Stack()))
				exc = m.nativeError(fn, "panic: %v", r)
			}
		}()
		out = nf.fn.Call(in)
	}()
	if exc != nil {
		return Value{}, exc, nil
	}
	if e, _ := out[len(out)-1].Interface().(error); e != nil {
		return m.nativeFailure(fn, ctx, e)
	}
	if m.ctx.Err() != nil {
		return Value{}, nil, &LimitError{Limit: "time limit", Msg: "the execution exceeded its time limit in " + fn.Def.Name}
	}
	if nf.m.Releases && fn.Def.Instance {
		if err := m.release(me.record()); err != nil {
			return Value{}, m.nativeError(fn, "releasing the object: %v", err), nil
		}
	}
	if nf.out == nil {
		return Value{}, nil, nil
	}
	v, th := m.fromNative(p, fn.Def.Result, out[0])
	return v, th, nil
}

// release frees the Go value of an object of a native class.
func (m *Machine) release(r *Record) error {
	if r.released {
		return nil
	}
	r.released = true
	if c, ok := r.Native.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// ReleaseHandles frees the objects of native classes that the machine
// created and that are still open. The kernel calls it at the end of
// every execution.
func (m *Machine) ReleaseHandles() error {
	var errs []error
	for _, r := range m.handles {
		if err := m.release(r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Class.Name, err))
		}
	}
	m.handles = nil
	return errors.Join(errs...)
}

// nativeFailure turns the error of a native method into an exception.
func (m *Machine) nativeFailure(fn *LinkedFunc, ctx context.Context, e error) (Value, *thrown, error) {
	var ex *native.Exception
	var big *native.InputTooLarge
	var denied *native.NotAuthorized
	switch {
	case errors.As(e, &ex):
		c := m.image.classes[ex.Class]
		declared := false
		for _, r := range fn.native.raises {
			declared = declared || c != nil && c.Supers[r]
		}
		if !declared {
			return Value{}, m.nativeError(fn, "the implementation raised %s, which the method does not declare", ex.Class), nil
		}
		obj, th, err := m.construct(c, ex.Args)
		if th != nil || err != nil {
			return Value{}, th, err
		}
		return Value{}, &thrown{obj: obj, pc: -1}, nil
	case errors.As(e, &big):
		return Value{}, m.fail("/RT/INPUT_TOO_LARGE", "%s: %s", fn.Def.Name, big.Error()), nil
	case errors.As(e, &denied):
		return Value{}, m.notAuthorized(denied.Check, denied.Text), nil
	case m.ctx.Err() != nil:
		return Value{}, nil, &LimitError{Limit: "time limit", Msg: "the execution exceeded its time limit in " + fn.Def.Name}
	case ctx.Err() != nil && errors.Is(e, ctx.Err()):
		return Value{}, m.nativeError(fn, "the method exceeded its time limit of %s", fn.native.m.Timeout), nil
	}
	return Value{}, m.nativeError(fn, "%v", e), nil
}

// nativeError returns /RT/NATIVE_ERROR for a native function.
func (m *Machine) nativeError(fn *LinkedFunc, format string, args ...any) *thrown {
	t := m.fail("/RT/NATIVE_ERROR", "%s: %s", fn.Def.Name, fmt.Sprintf(format, args...))
	class, method, _ := strings.Cut(fn.Def.Name, ".")
	l := t.obj.Class.Layout
	for name, v := range map[string]string{"CLASS_NAME": class, "METHOD_NAME": method} {
		if i := l.Field(name); i >= 0 {
			m.setField(t.obj, l.Fields[i], Str(v))
		}
	}
	return t
}

// construct creates an exception object with its constructor from Go
// arguments.
func (m *Machine) construct(c *LinkedClass, args []any) (*Record, *thrown, error) {
	obj := m.newObject(c)
	ctor := c.Ctor
	if ctor == nil || len(ctor.Def.ParamTypes) != len(args) {
		return nil, nil, fmt.Errorf("vm: the constructor of %s does not take %d arguments", c.Name, len(args))
	}
	p := ctor.Prog.P
	vals := make([]Value, len(args))
	for i, a := range args {
		v, err := m.goArg(p, ctor.Def.ParamTypes[i], a)
		if err != nil {
			return nil, nil, fmt.Errorf("vm: argument %d of %s: %w", i+1, c.Name, err)
		}
		vals[i] = v
	}
	if _, t, err := m.exec(ctor, Value{R: obj}, vals, nil); t != nil || err != nil {
		return nil, t, err
	}
	return obj, nil, nil
}

// goArg converts a Go argument of an exception into a value of type t.
func (m *Machine) goArg(p *Program, t int, a any) (Value, error) {
	d := &p.Types[t]
	if a == nil {
		if !d.Optional {
			return Value{}, errors.New("nil for a value that is not optional")
		}
		return Null, nil
	}
	if o, ok := a.(optional); ok {
		x, valid := o.AnyValue()
		if !valid {
			return m.goArg(p, t, nil)
		}
		a = x
	}
	switch x := a.(type) {
	case int:
		a = int64(x)
	case int32:
		a = int64(x)
	}
	// Arrays and structures come as Go types with rt tags, like the
	// results of native methods.
	if rv := reflect.ValueOf(a); (rv.Kind() == reflect.Slice && rv.Type() != reflect.TypeFor[[]byte]()) || (rv.Kind() == reflect.Struct && rv.Type() != typTimestamp) {
		if err := m.image.compatible(nil, p, t, rv.Type()); err != nil {
			return Value{}, err
		}
		v, th := m.fromNative(p, t, rv)
		if th != nil {
			return Value{}, m.exception(th)
		}
		return v, nil
	}
	return m.fromGo(p, t, a)
}

// notAuthorized returns /RT/NOT_AUTHORIZED for a failed check, with the
// object and the checked values, null for fields checked with any.
func (m *Machine) notAuthorized(c authorization.Check, text string) *thrown {
	t := m.fail("/RT/NOT_AUTHORIZED", "%s", text)
	t.check = &c
	r := t.obj
	l := r.Class.Layout
	if i := l.Field("OBJECT"); i >= 0 {
		m.setField(r, l.Fields[i], Str(c.Object))
	}
	if i := l.Field("VALUES"); i >= 0 {
		f := l.Fields[i]
		mp := m.newMap(r.P, f.Type)
		// Fields in order of their names, any as null.
		var fields []string
		for field := range c.Values {
			fields = append(fields, field)
		}
		for field := range c.Any {
			if _, ok := c.Values[field]; !ok {
				fields = append(fields, field)
			}
		}
		slices.Sort(fields)
		for _, field := range fields {
			if v, ok := c.Values[field]; ok && !c.Any[field] {
				m.mapSet(mp, Str(field), Str(v))
			} else {
				m.mapSet(mp, Str(field), Null)
			}
		}
		m.setField(r, f, Value{R: mp})
	}
	return t
}
