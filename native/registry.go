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

// Package native holds the implementations of methods executed by the
// kernel: Go functions behind methods of kernel packages that are declared
// with "executed by kernel". They are part of the kernel binary and are
// registered here with the signature they implement; the compiler checks
// every declaration against its registration, and the VM calls them with
// values converted to Go types.
package native

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Method is the registration of a native method.
type Method struct {
	Package model.PackageID // kernel package, e.g. TRBASE
	Class   string          // e.g. /TRBASE/HASH
	Method  string          // e.g. SHA256
	Static  bool
	// Signature is the part of the declaration after the method name, in
	// the syntax of the language: "(data: BYTES) returns BYTES raises
	// /TRBASE/X". The compiler compares it with the declaration.
	Signature string
	// Capabilities are the effects beyond computing and the database the
	// method has, such as model.CapabilityClock.
	Capabilities []string
	// Timeout limits one call in addition to the time limit of the
	// execution; 0 means none.
	Timeout time.Duration
	// Func is the implementation:
	//
	//	func(c *Context, p1 T1, ...) (R, error)        static method
	//	func(c *Context, self *G, p1 T1, ...) (R, error) method of native class G
	//
	// Without result it returns only error. The VM converts the values of
	// the parameters to the Go types and back for the result.
	Func any
}

// Key returns PACKAGE:/CLASS.METHOD.
func (m *Method) Key() string { return Key(m.Package, m.Class, m.Method) }

// Declaration returns the method as it is declared in a class.
func (m *Method) Declaration() string {
	s := ""
	if m.Static {
		s = "static "
	}
	return s + "method " + m.Method + m.Signature
}

// Key identifies a native method.
func Key(pkg model.PackageID, class, method string) string {
	return string(pkg) + ":" + class + "." + method
}

// Class is the registration of a native class: its instances carry a Go
// value of type Type, a pointer type, that only its native methods see.
type Class struct {
	Package model.PackageID
	Class   string
	Type    reflect.Type
}

// Registry holds native methods and classes.
type Registry struct {
	mu      sync.RWMutex
	methods map[string]*Method
	classes map[string]*Class
	effects map[string]EffectFunc
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{methods: map[string]*Method{}, classes: map[string]*Class{}, effects: map[string]EffectFunc{}}
}

// Default is the registry of the kernel binary. Kernel packages register
// their implementations in it from init functions.
var Default = NewRegistry()

var (
	contextType = reflect.TypeOf((*Context)(nil))
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

// Register adds a native method. It panics when the registration is
// malformed or registered twice: that is a programming error of the
// kernel, found by its tests.
func (r *Registry) Register(m Method) {
	if m.Package == "" || !strings.HasPrefix(m.Class, model.Namespace(m.Package)) || m.Method == "" || m.Func == nil {
		panic(fmt.Sprintf("native: incomplete registration %s", m.Key()))
	}
	for _, c := range m.Capabilities {
		if !slices.Contains(model.KnownCapabilities, c) {
			panic(fmt.Sprintf("native: %s declares the unknown capability %q", m.Key(), c))
		}
	}
	ft := reflect.TypeOf(m.Func)
	if ft.Kind() != reflect.Func || ft.NumIn() < 1 || ft.In(0) != contextType || ft.IsVariadic() ||
		ft.NumOut() < 1 || ft.NumOut() > 2 || ft.Out(ft.NumOut()-1) != errorType {
		panic(fmt.Sprintf("native: %s must be a func(*native.Context, ...) (result, error) or func(*native.Context, ...) error", m.Key()))
	}
	if !m.Static && (ft.NumIn() < 2 || ft.In(1).Kind() != reflect.Pointer) {
		panic(fmt.Sprintf("native: %s is an instance method; its second parameter must be the Go value of the native class", m.Key()))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.methods[m.Key()]; dup {
		panic(fmt.Sprintf("native: %s is registered twice", m.Key()))
	}
	mm := m
	r.methods[m.Key()] = &mm
}

// RegisterClass declares a native class whose instances carry Go values
// of the type of example, a typed nil pointer such as (*stylesheet)(nil).
func (r *Registry) RegisterClass(pkg model.PackageID, class string, example any) {
	t := reflect.TypeOf(example)
	if t == nil || t.Kind() != reflect.Pointer || !strings.HasPrefix(class, model.Namespace(pkg)) {
		panic(fmt.Sprintf("native: class %s needs a pointer type", class))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.classes[class]; dup {
		panic(fmt.Sprintf("native: class %s is registered twice", class))
	}
	r.classes[class] = &Class{Package: pkg, Class: class, Type: t}
}

// Lookup returns the registration of a method.
func (r *Registry) Lookup(pkg model.PackageID, class, method string) (*Method, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.methods[Key(pkg, class, method)]
	return m, ok
}

// LookupClass returns the registration of a native class.
func (r *Registry) LookupClass(class string) (*Class, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.classes[class]
	return c, ok
}

// Methods returns all registered methods ordered by key.
func (r *Registry) Methods() []*Method {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Method, 0, len(r.methods))
	for _, m := range r.methods {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b *Method) int { return strings.Compare(a.Key(), b.Key()) })
	return out
}

// Merge returns a registry with the entries of r and of others, for tests
// that add kernel packages of their own.
func (r *Registry) Merge(others ...*Registry) *Registry {
	out := NewRegistry()
	for _, src := range append([]*Registry{r}, others...) {
		src.mu.RLock()
		for k, m := range src.methods {
			out.methods[k] = m
		}
		for k, c := range src.classes {
			out.classes[k] = c
		}
		for k, e := range src.effects {
			out.effects[k] = e
		}
		src.mu.RUnlock()
	}
	return out
}

// NativeMethod implements the lookup of the compiler.
func (r *Registry) NativeMethod(pkg model.PackageID, class, method string) (bool, string, bool) {
	m, ok := r.Lookup(pkg, class, method)
	if !ok {
		return false, "", false
	}
	return m.Static, m.Signature, true
}
