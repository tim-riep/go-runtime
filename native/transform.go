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

package native

import (
	"fmt"

	"github.com/tim-riep/go-runtime/model"
)

// Shape is a kind of source or result of a native transformation that the
// compiler checks.
type Shape string

// Shapes.
const (
	// ShapeBytes is BYTES.
	ShapeBytes Shape = "bytes"
	// ShapeRows is an array of a structure or table with simple
	// components only.
	ShapeRows Shape = "rows"
	// ShapeArrayType is an array type of the schema whose row type has
	// simple components only.
	ShapeArrayType Shape = "array type"
)

// TransformParam is a parameter of a native transformation; Values are
// the texts it allows, empty for any.
type TransformParam struct {
	Name   string
	Values []string
}

// TransformShape is a pair of source and result a transformation takes.
type TransformShape struct{ Source, Into Shape }

// Table is an array of rows of a structure: the values of its fields in
// order, as the database uses them (string, int64, bool, time.Time,
// decimal text, enum codes; nil for null).
type Table struct {
	Struct string
	Fields []string
	Rows   [][]any
}

// TransformInput is what a native transformation gets: the parameters
// as text, and the source as bytes or as table. Into names the structure
// and fields of the rows of a table it returns.
type TransformInput struct {
	Name   string
	Params map[string]string
	Bytes  []byte
	Rows   *Table
	Into   *Table
}

// TransformOutput is the result: bytes, or rows of Into whose values may
// also be canonical text.
type TransformOutput struct {
	Bytes []byte
	Rows  [][]any
}

// Transform is a native transformation that a kernel package provides,
// such as xlsx: its parameters, the shapes of source and result, the
// checked exceptions it raises and its implementation.
type Transform struct {
	Params []TransformParam
	Shapes []TransformShape
	Raises []string
	Func   func(c *Context, in TransformInput) (TransformOutput, error)
}

// RegisterTransform adds the native transformation of a format. It panics
// when the format is registered twice.
func (r *Registry) RegisterTransform(format string, t Transform) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.transforms[format]; dup {
		panic(fmt.Sprintf("native: transformation format %s registered twice", format))
	}
	r.transforms[format] = &t
}

// LookupTransform returns the native transformation of a format.
func (r *Registry) LookupTransform(format string) (*Transform, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.transforms[format]
	return t, ok
}

// NativeTransform implements the lookup of the compiler: the parameters
// with their allowed values, the pairs of source and result shapes and
// the exceptions.
func (r *Registry) NativeTransform(format string) (params map[string][]string, shapes [][2]string, raises []string, ok bool) {
	t, ok := r.LookupTransform(format)
	if !ok {
		return nil, nil, nil, false
	}
	params = map[string][]string{}
	for _, p := range t.Params {
		params[p.Name] = p.Values
	}
	for _, s := range t.Shapes {
		shapes = append(shapes, [2]string{string(s.Source), string(s.Into)})
	}
	return params, shapes, t.Raises, true
}

// Component is a component of a structure or table, with what native
// transformations need to read and write its values.
type Component struct {
	Name string
	// Labels are the headings of the data element by language, empty for
	// components with a domain.
	Labels  map[string]string
	Domain  *model.Domain
	NotNull bool
	// Default is the default in canonical text form, if any.
	Default model.Optional[string]
}
