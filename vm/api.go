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
	"fmt"
	"reflect"
)

// The kernel runs handlers of http services through these methods. Unlike
// Call they keep the budget of the machine, so that all calls of one
// request share it.

// NewObject creates an object of a class with its constructor without
// arguments.
func (m *Machine) NewObject(ctx context.Context, class string) (Value, error) {
	c := m.image.Class(class)
	if c == nil || c.Ctor == nil {
		return Value{}, fmt.Errorf("vm: %s is no class with a constructor", class)
	}
	if len(c.Ctor.Def.ParamTypes) != 0 {
		return Value{}, fmt.Errorf("vm: the constructor of %s takes parameters", class)
	}
	m.ctx, m.depth = ctx, 0
	obj := Value{R: m.newObject(c)}
	if _, err := m.run(c.Ctor, obj, nil, nil); err != nil {
		return Value{}, err
	}
	return obj, nil
}

// NewNative creates an object of a native class that carries the Go
// value v.
func (m *Machine) NewNative(class string, v any) (Value, error) {
	c := m.image.Class(class)
	if c == nil {
		return Value{}, fmt.Errorf("vm: unknown class %s", class)
	}
	if want, ok := m.nativeType(class); !ok || reflect.TypeOf(v) != want {
		return Value{}, fmt.Errorf("vm: %s is no native class of %T", class, v)
	}
	r := m.newObject(c)
	r.Native = v
	return Value{R: r}, nil
}

func (m *Machine) nativeType(class string) (reflect.Type, bool) {
	for _, pr := range m.image.order {
		for _, f := range pr.Funcs {
			if nf := f.native; nf != nil && nf.class != nil && nf.class.Class == class {
				return nf.class.Type, true
			}
		}
	}
	return nil, false
}

// Invoke calls an instance method of an object with arguments, through
// the method table of its class.
func (m *Machine) Invoke(ctx context.Context, obj Value, method string, args ...Value) (Value, error) {
	r := obj.record()
	if r == nil || r.Class == nil {
		return Value{}, fmt.Errorf("vm: %s is called on no object", method)
	}
	mi, ok := r.Class.Method(method)
	if !ok || mi.Static {
		return Value{}, fmt.Errorf("vm: %s has no instance method %s", r.Class.Name, method)
	}
	m.ctx, m.depth = ctx, 0
	return m.run(mi.Func, obj, args, nil)
}

// AsBool returns the value of a BOOL.
func (v Value) AsBool() bool { return v.bool() }
