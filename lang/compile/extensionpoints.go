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
	"slices"

	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
	"github.com/tim-riep/go-runtime/model"
)

// extension returns the index of an extension point in the table of the
// program, with the fields of x in the order of their values.
func (f *fn) extension(p *types.ExtensionPoint, fields []bytecode.AuthField) int {
	e := bytecode.Extension{Name: p.Name, Interface: f.c.classRef(p.Interface), Fields: fields}
	f.c.out.Extensions = append(f.c.out.Extensions, e)
	return len(f.c.out.Extensions) - 1
}

// extensionPoint pushes the filter values in source order and selects
// the implementations.
func (f *fn) extensionPoint(x *syntax.ExtensionPointExpr) {
	info := f.c.info.ExtensionPoints[x]
	var fields []bytecode.AuthField
	for _, fd := range info.Fields {
		f.expr(fd.Value)
		fields = append(fields, bytecode.AuthField{Name: fd.Name, Type: f.c.typeDesc(fd.Type)})
	}
	f.line(x.Pos)
	f.emit(bytecode.EXT_GET, f.extension(info.Point, fields))
}

// extensionCall calls a method on the value of an extension point.
func (f *fn) extensionCall(x *syntax.CallExpr, ci *types.Call) {
	p := ci.Extension
	e, ok := f.c.exts[p.Name]
	if !ok {
		e = f.extension(p, nil)
		f.c.exts[p.Name] = e
	}
	f.expr(ci.Receiver)
	if ci.Method == nil {
		which := 0
		if ci.Builtin.Name == types.CountMethod {
			which = 1
		}
		f.emit(bytecode.EXT_INFO, e, which)
		return
	}
	f.args(ci.Args)
	// A single implementation returns its result; several pass it on
	// through the chained parameter.
	chain := bytecode.ExtCallEach
	if i, ok := types.ChainParameter(ci.Method); ok && p.Point.Use == model.UseMultiple {
		chain = i
	}
	f.emit(bytecode.EXT_CALL, e, slices.Index(f.c.ifaces[p.Interface], ci.Method.Name), len(ci.Args)+1, chain)
}
