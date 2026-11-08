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
	"errors"
	"fmt"
	"strings"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// extensionValue is the value of an extension point: an object of every
// selected implementation, created once when the expression ran, so that
// the implementations keep their state between the calls on the value.
type extensionValue struct {
	point         string
	objects       []*Record
	names         []string
	fallback      bool
	commitAllowed bool
}

// extGet runs EXT_GET: it asks the host for the implementations that the
// filter values select and creates their objects.
func (m *Machine) extGet(fn *LinkedFunc, pc int, e *Extension, values []Value) (Value, *thrown, error) {
	if m.opts.Host == nil {
		return Value{}, nil, errors.New("vm: the execution runs an extension point but has no host")
	}
	p := fn.Prog.P
	filters := map[string]string{}
	for i, f := range e.Fields {
		filters[f.Name] = m.xmlText(p, &p.Types[f.Type], values[i])
	}
	class, method, _ := strings.Cut(fn.Def.Name, ".")
	at := native.CheckLocation{Package: model.PackageID(p.Package), Class: class, Method: method}
	if l, ok := fn.Def.Position(pc); ok {
		at.Line = l.Line
	}
	sel, err := m.opts.Host.SelectImplementations(e.Name, filters, at)
	var xe *native.ExtensionError
	switch {
	case errors.As(err, &xe):
		return Value{}, m.extensionError(xe), nil
	case err != nil:
		return Value{}, nil, err
	}
	v := &extensionValue{point: e.Name, names: sel.Implementations, fallback: sel.Fallback, commitAllowed: sel.CommitAllowed}
	m.alloc(int64(64 + 16*len(sel.Classes)))
	for _, name := range sel.Classes {
		c := m.image.Class(name)
		if c == nil || c.Ctor == nil {
			return Value{}, nil, fmt.Errorf("vm: %s of extension point %s is no class with a constructor in generation %d", name, e.Name, m.image.Generation)
		}
		obj := m.newObject(c)
		if _, t, err := m.exec(c.Ctor, Value{R: obj}, nil, nil); err != nil || t != nil {
			return Value{}, t, err
		}
		v.objects = append(v.objects, obj)
	}
	return Value{R: v}, nil, nil
}

// extCall runs EXT_CALL: the method with index method of the interface on
// the object of every selected implementation, one after the other. With
// chain >= 0 the result of one implementation is the argument chain of the
// next, and the result of the last one is the result; without
// implementations it is the argument. An exception ends the calls.
func (m *Machine) extCall(pr *Prog, e *Extension, method, chain int, recv Value, args []Value) (Value, bool, *thrown, error) {
	v, ok := recv.R.(*extensionValue)
	if !ok {
		return Value{}, false, m.fail("/RT/VALUE_ERROR", "method called on null"), nil
	}
	iface := pr.class(e.Interface)
	if !v.commitAllowed {
		m.noCommit = append(m.noCommit, v.point)
		defer func() { m.noCommit = m.noCommit[:len(m.noCommit)-1] }()
	}
	var result Value
	has := false
	if chain >= 0 && chain < len(args) {
		result, has = args[chain], true
	}
	for _, obj := range v.objects {
		target := obj.Class.ITabs[iface][method]
		callArgs := make([]Value, len(args))
		for j, a := range args {
			if chain == j && has {
				a = result
			}
			// Every implementation gets its own copy of values.
			if j < len(target.Def.ParamTypes) {
				a = m.copyValue(target.Prog.P, target.Def.ParamTypes[j], a)
			}
			callArgs[j] = a
		}
		r, t, err := m.exec(target, Value{R: obj}, callArgs, nil)
		if err != nil || t != nil {
			return Value{}, false, t, err
		}
		if target.Def.Result >= 0 {
			result, has = r, true
		}
	}
	return result, has, nil, nil
}

// extInfo runs EXT_INFO: IS_IMPLEMENTED (0) or COUNT (1). The fallback
// class counts for neither.
func (m *Machine) extInfo(recv Value, which int) (Value, *thrown) {
	v, ok := recv.R.(*extensionValue)
	if !ok {
		return Value{}, m.fail("/RT/VALUE_ERROR", "method called on null")
	}
	n := len(v.names)
	if which == 1 {
		return Int(int64(n)), nil
	}
	return Bool(n > 0), nil
}

// extensionError raises /RT/NO_IMPLEMENTATION or
// /RT/IMPLEMENTATION_CONFLICT with the extension point and the
// implementations.
func (m *Machine) extensionError(xe *native.ExtensionError) *thrown {
	t := m.fail(xe.Class, "%s", xe.Text)
	r := t.obj
	l := r.Class.Layout
	if i := l.Field("EXTENSION_POINT"); i >= 0 {
		m.setField(r, l.Fields[i], Str(xe.ExtensionPoint))
	}
	if i := l.Field("IMPLEMENTATIONS"); i >= 0 {
		f := l.Fields[i]
		a := m.newArray(r.P, f.Type)
		for _, name := range xe.Implementations {
			a.Vals = append(a.Vals, Str(name))
		}
		m.setField(r, f, Value{R: a})
	}
	return t
}
