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
	"strings"

	"github.com/tim-riep/go-runtime/authorization"
	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// authCheck runs AUTH_CHECK: it hands the check to the host, raises
// /RT/NOT_AUTHORIZED for a failed check authorization and returns the
// result of authorized.
func (m *Machine) authCheck(fn *LinkedFunc, pc int, a *AuthCheck, values []Value) (Value, *thrown, error) {
	p := fn.Prog.P
	c := authorization.Check{Object: a.Object, Values: map[string]string{}}
	i := 0
	for _, f := range a.Fields {
		if f.Type == -1 {
			if c.Any == nil {
				c.Any = map[string]bool{}
			}
			c.Any[f.Name] = true
			continue
		}
		c.Values[f.Name] = m.xmlText(p, &p.Types[f.Type], values[i])
		i++
	}
	mode := native.CheckExpression
	if a.Statement {
		mode = native.CheckStatement
	}
	if m.opts.Host == nil {
		return Value{}, nil, errors.New("vm: the execution checks authorizations but has no host")
	}
	class, method, _ := strings.Cut(fn.Def.Name, ".")
	at := native.CheckLocation{Package: model.PackageID(p.Package), Class: class, Method: method}
	if l, ok := fn.Def.Position(pc); ok {
		at.Line = l.Line
	}
	ok, err := m.opts.Host.CheckAuthorization(c, at, mode)
	var denied *native.NotAuthorized
	switch {
	case errors.As(err, &denied):
		return Value{}, m.notAuthorized(denied.Check, denied.Text), nil
	case err != nil:
		return Value{}, nil, err
	case !ok && a.Statement:
		return Value{}, m.notAuthorized(c, "no authorization for "+c.Object), nil
	}
	return Bool(ok), nil, nil
}
