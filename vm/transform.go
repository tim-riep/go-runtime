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
	"unicode/utf8"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
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
