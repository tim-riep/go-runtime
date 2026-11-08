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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// Transformer runs transformations; the kernel implements it with the
// compiled stylesheets of the generation.
type Transformer interface {
	Transform(ctx context.Context, name string, source []byte, params map[string]string) ([]byte, error)
}

// TransformError is an error of a stylesheet that code catches as
// /TRBASE/XSLT_FAILED; Line is 0 when it is unknown.
type TransformError struct {
	Line int
	Text string
}

func (e *TransformError) Error() string { return e.Text }

// The exceptions of transform.
const (
	xsltFailed    = "/TRBASE/XSLT_FAILED"
	resultInvalid = "/TRBASE/TRANSFORM_RESULT_INVALID"
)

// transform runs XFORM: it serializes the source, runs the transformation
// and reads the result.
func (m *Machine) transform(p *Program, tr *Transform, src Value, params []Value) (Value, *thrown, error) {
	switch tr.Format {
	case "json":
		return m.transformJSON(p, tr, src)
	case "xml":
	default:
		return m.transformNative(p, tr, src, params)
	}
	if m.opts.Transformer == nil {
		return Value{}, nil, errors.New("vm: the execution has no transformations")
	}
	var data []byte
	switch {
	case !tr.SourceXML:
		data = m.EncodeXML(p, tr.Source, src)
	case p.Types[tr.Source].Kind == TBytes:
		data, _ = src.R.([]byte)
	default:
		data = []byte(src.str())
	}
	if int64(len(data)) > m.opts.MaxNativeInput {
		return Value{}, m.fail("/RT/INPUT_TOO_LARGE", "the source of transform %s has %d bytes, more than the limit of %d", tr.Name, len(data), m.opts.MaxNativeInput), nil
	}
	values := map[string]string{}
	for i, v := range params {
		if v.IsNull() {
			continue // the default of the stylesheet applies
		}
		values[tr.Params[i]] = m.xmlText(p, &p.Types[tr.ParamTypes[i]], v)
	}
	out, err := m.opts.Transformer.Transform(m.ctx, tr.Name, data, values)
	var te *TransformError
	switch {
	case errors.As(err, &te):
		line := model.None[int32]()
		if te.Line > 0 {
			line = model.Some(int32(te.Line))
		}
		return m.raiseNamed(xsltFailed, te.Text, line)
	case err != nil && m.ctx.Err() != nil:
		return Value{}, nil, &LimitError{Limit: "time limit", Msg: "the execution exceeded its time limit in transform " + tr.Name}
	case err != nil:
		return Value{}, nil, err
	}
	invalid := func(format string, args ...any) (Value, *thrown, error) {
		return m.raiseNamed(resultInvalid, fmt.Sprintf("the result of %s: %s", tr.Name, fmt.Sprintf(format, args...)))
	}
	switch p.Types[tr.Into].Kind {
	case TString:
		if !utf8.Valid(out) {
			return invalid("it is no UTF-8 text")
		}
		return Str(string(out)), nil, nil
	case TBytes:
		return Value{R: out}, nil, nil
	}
	v, err := m.DecodeXML(p, tr.Into, out)
	if err != nil {
		return invalid("%v", err)
	}
	return v, nil, nil
}

// raiseNamed raises an exception of a class of TRBASE with arguments of
// its constructor.
func (m *Machine) raiseNamed(class string, args ...any) (Value, *thrown, error) {
	c := m.image.classes[class]
	if c == nil {
		return Value{}, nil, fmt.Errorf("vm: the class %s is not linked", class)
	}
	obj, th, err := m.construct(c, args)
	if th != nil || err != nil {
		return Value{}, th, err
	}
	return Value{}, &thrown{obj: obj, pc: -1}, nil
}

// transformJSON runs the native transformation /TRBASE/JSON: a value to
// JSON in the format of rt run, or JSON text into a value.
func (m *Machine) transformJSON(p *Program, tr *Transform, src Value) (Value, *thrown, error) {
	var data []byte
	switch {
	case !tr.SourceXML:
		b, err := m.ToJSON(p, tr.Source, src)
		if err != nil {
			return m.raiseNamed(xsltFailed, err.Error(), model.None[int32]())
		}
		data = b
	case p.Types[tr.Source].Kind == TBytes:
		data, _ = src.R.([]byte)
	default:
		data = []byte(src.str())
	}
	if int64(len(data)) > m.opts.MaxNativeInput {
		return Value{}, m.fail("/RT/INPUT_TOO_LARGE", "the source of transform %s has %d bytes, more than the limit of %d", tr.Name, len(data), m.opts.MaxNativeInput), nil
	}
	invalid := func(format string, args ...any) (Value, *thrown, error) {
		return m.raiseNamed(resultInvalid, fmt.Sprintf("the result of %s: %s", tr.Name, fmt.Sprintf(format, args...)))
	}
	switch p.Types[tr.Into].Kind {
	case TString:
		if !utf8.Valid(data) {
			return invalid("it is no UTF-8 text")
		}
		return Str(string(data)), nil, nil
	case TBytes:
		return Value{R: data}, nil, nil
	}
	if !json.Valid(data) {
		return invalid("it is no JSON")
	}
	v, err := m.FromJSON(p, tr.Into, data)
	if err != nil {
		return invalid("%v", err)
	}
	return v, nil, nil
}

// transformNative runs a transformation that a kernel package implements
// in Go: arrays of structures go as tables of database values, the result
// comes back as bytes or as rows of the array type.
func (m *Machine) transformNative(p *Program, tr *Transform, src Value, params []Value) (Value, *thrown, error) {
	var t *native.Transform
	if m.image.natives != nil {
		t, _ = m.image.natives.LookupTransform(tr.Format)
	}
	if t == nil {
		return Value{}, nil, fmt.Errorf("vm: no native transformation of format %s is registered", tr.Format)
	}
	in := native.TransformInput{Name: tr.Name, Params: map[string]string{}}
	for i, v := range params {
		if !v.IsNull() {
			in.Params[tr.Params[i]] = m.xmlText(p, &p.Types[tr.ParamTypes[i]], v)
		}
	}
	table := func(arrayType int) (*native.Table, int) {
		elem := p.Types[arrayType].Elem
		l := &p.Layouts[p.Types[elem].Layout]
		tb := &native.Table{Struct: l.Name}
		for _, f := range l.Fields {
			tb.Fields = append(tb.Fields, f.Name)
		}
		return tb, elem
	}
	switch p.Types[tr.Source].Kind {
	case TBytes:
		in.Bytes, _ = src.R.([]byte)
		if int64(len(in.Bytes)) > m.opts.MaxNativeInput {
			return Value{}, m.fail("/RT/INPUT_TOO_LARGE", "the source of transform %s has %d bytes, more than the limit of %d", tr.Name, len(in.Bytes), m.opts.MaxNativeInput), nil
		}
	case TArray:
		tb, elem := table(tr.Source)
		a := src.array()
		l := &p.Layouts[p.Types[elem].Layout]
		for i := range a.Len() {
			r := a.Get(i).record()
			row := make([]any, len(l.Fields))
			for j, f := range l.Fields {
				row[j] = m.toGo(p, f.Type, m.getField(r, f))
			}
			tb.Rows = append(tb.Rows, row)
		}
		in.Rows = tb
	}
	var elem int
	if p.Types[tr.Into].Kind == TArray {
		in.Into, elem = table(tr.Into)
	}
	c := &native.Context{Context: m.ctx, Host: m.opts.Host, MaxInput: m.opts.MaxNativeInput, Method: tr.Name, Limits: m.opts.Limits}
	if len(m.callers) > 0 {
		c.Caller = m.callers[len(m.callers)-1]
		c.Callers = slices.Clone(m.callers)
	}
	out, err := t.Func(c, in)
	if err != nil {
		var ex *native.Exception
		var big *native.InputTooLarge
		switch {
		case errors.As(err, &ex):
			cl := m.image.classes[ex.Class]
			if cl == nil {
				return Value{}, nil, fmt.Errorf("vm: the class %s is not linked", ex.Class)
			}
			obj, th, cerr := m.construct(cl, ex.Args)
			if th != nil || cerr != nil {
				return Value{}, th, cerr
			}
			return Value{}, &thrown{obj: obj, pc: -1}, nil
		case errors.As(err, &big):
			return Value{}, m.fail("/RT/INPUT_TOO_LARGE", "%s: %s", tr.Name, big.Error()), nil
		case m.ctx.Err() != nil:
			return Value{}, nil, &LimitError{Limit: "time limit", Msg: "the execution exceeded its time limit in transform " + tr.Name}
		}
		return Value{}, nil, err
	}
	if p.Types[tr.Into].Kind == TBytes {
		if out.Bytes == nil {
			out.Bytes = []byte{}
		}
		return Value{R: out.Bytes}, nil, nil
	}
	a := m.newArrayOf(p, elem)
	for i, vals := range out.Rows {
		v, err := m.newRow(p, elem, vals)
		if err != nil {
			return m.raiseNamed(resultInvalid, fmt.Sprintf("the result of %s, row %d: %v", tr.Name, i+1, err))
		}
		if th := m.appendElem(a, v); th != nil {
			return Value{}, th, nil
		}
	}
	return Value{R: a}, nil, nil
}
