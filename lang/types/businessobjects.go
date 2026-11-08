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

package types

import (
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// EntityResponseClass is the class of the response that methods of
// behavior classes get.
const EntityResponseClass = "/TRBASE/ENTITY_RESPONSE"

// BehaviorMethod is a method that the definition of a business object
// expects in its behavior class, with the signature derived from it.
type BehaviorMethod struct {
	Name   string
	Entity string
	Params []string // names
	Sig    *Func
}

// String writes the method as it is declared.
func (b BehaviorMethod) String() string {
	var params []string
	for i, p := range b.Sig.Params {
		params = append(params, b.Params[i]+": "+p.String())
	}
	out := "public method " + b.Name + "(" + strings.Join(params, ", ") + ")"
	if b.Sig.Result != TypVoid {
		out += " returns " + b.Sig.Result.String()
	}
	return out
}

// BehaviorMethods returns the methods of the behavior class of a business
// object: determinations and validations get the keys of the instances
// and a response, instance actions also their parameter and return the
// rows of their result structure, static actions get only the parameter
// and return their result structure. An action with result self returns
// nothing; the kernel reads the instances.
func (c *checker) behaviorMethods(b *model.BusinessObject) []BehaviorMethod {
	response := Type(TypInvalid)
	if k := c.classes[EntityResponseClass]; k != nil {
		response = k
	}
	var out []BehaviorMethod
	for _, e := range b.Entities {
		keys := Type(TypInvalid)
		if k := c.schema.keyOf(e.Table, syntax.Pos{}); k != nil {
			keys = &Array{Elem: k}
		}
		withKeys := func(name string) BehaviorMethod {
			return BehaviorMethod{Name: name, Entity: e.Table, Params: []string{"keys", "response"}, Sig: &Func{Params: []Type{keys, response}, Result: TypVoid}}
		}
		for _, d := range e.Determinations {
			out = append(out, withKeys(d.Name))
		}
		for _, v := range e.Validations {
			out = append(out, withKeys(v.Name))
		}
		for _, a := range e.Actions {
			m := BehaviorMethod{Name: a.Name, Entity: e.Table, Sig: &Func{Result: TypVoid}}
			if a.Instance {
				m.Params, m.Sig.Params = append(m.Params, "keys"), append(m.Sig.Params, keys)
			}
			if p, ok := a.Parameter.Get(); ok {
				m.Params, m.Sig.Params = append(m.Params, "parameter"), append(m.Sig.Params, c.structOrInvalid(p))
			}
			m.Params, m.Sig.Params = append(m.Params, "response"), append(m.Sig.Params, response)
			if r, ok := a.Result.Get(); ok && r != model.ResultSelf {
				m.Sig.Result = c.structOrInvalid(r)
				if a.Instance {
					m.Sig.Result = &Array{Elem: m.Sig.Result}
				}
			}
			out = append(out, m)
		}
	}
	return out
}

func (c *checker) structOrInvalid(name string) Type {
	if t := c.schema.lookup(name, syntax.Pos{}); t != nil {
		return t
	}
	return TypInvalid
}

// behaviorClasses checks the behavior classes of the business objects of
// the package: a class of the package with a public constructor without
// parameters that has every method the definition expects, public, not
// static, with exactly the derived signature and without checked
// exceptions.
func (c *checker) behaviorClasses() {
	if c.cfg.Schema == nil {
		return
	}
	for _, name := range c.cfg.Schema.BusinessObjectNames() {
		b := c.cfg.Schema.BusinessObjects[name]
		class, ok := b.BehaviorClass.Get()
		if b.Owner != c.cfg.Package || !ok {
			continue
		}
		fail := func(format string, args ...any) {
			c.report(Error, b.File, syntax.Pos{}, nil, CodeBusinessObject, "/behaviorClass: "+format, args...)
		}
		if c.classes[EntityResponseClass] == nil || !c.visiblePackage(EntityResponseClass) {
			fail("business objects need TRBASE for %s; add it to the requirements", EntityResponseClass)
			continue
		}
		k := c.classes[class]
		switch {
		case k == nil:
			fail("%s is not a class", class)
			continue
		case k.External:
			fail("%s belongs to another package", class)
			continue
		case k.IsInterface() || k.Test || k.Native:
			fail("%s is no plain class; the kernel creates it", class)
			continue
		}
		for x := k; x != nil; x = x.Base {
			if x.Ctor != nil {
				if len(x.Ctor.Sig.Params) > 0 || x.Ctor.Visibility != syntax.Public || len(x.Ctor.Sig.Raises) > 0 {
					fail("%s needs a public constructor without parameters that raises nothing", class)
				}
				break
			}
		}
		c.useIn(b.File, "class", class)
		for _, want := range c.behaviorMethods(b) {
			m, ok := k.Member(want.Name).(*Method)
			switch {
			case !ok:
				fail("%s needs the method %s for %s", class, want, want.Entity)
			case m.Static || m.Visibility != syntax.Public || !sameParams(m.Sig, want.Sig):
				fail("%s does not match: %s needs the method %s for %s", m, class, want, want.Entity)
			default:
				for _, r := range m.Sig.Raises {
					if !r.IsRuntimeError() {
						fail("%s raises %s; methods of behavior classes raise no checked exceptions", m, r.Name)
					}
				}
			}
		}
	}
}

// sameParams compares the parameters and the result of two signatures.
func sameParams(a, b *Func) bool {
	if len(a.Params) != len(b.Params) || !Identical(a.Result, b.Result) {
		return false
	}
	for i := range a.Params {
		if !Identical(a.Params[i], b.Params[i]) {
			return false
		}
	}
	return true
}
