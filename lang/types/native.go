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
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// nativeMethod checks a method executed by kernel against the
// implementation registered in the kernel.
func (c *checker) nativeMethod(k *Class, m *Method) {
	pos := m.Site.Pos
	if !c.cfg.KernelPackage {
		c.errorf(pos, CodeNative, "methods executed by kernel are only allowed in kernel packages")
		return
	}
	if m.Open {
		c.errorf(pos, CodeNative, "%s is executed by kernel and cannot be open", m)
	}
	var static bool
	var sig string
	ok := false
	if c.cfg.Natives != nil {
		static, sig, ok = c.cfg.Natives.NativeMethod(c.cfg.Package, k.Name, m.Name)
	}
	if !ok {
		c.errorf(pos, CodeNative, "no implementation of %s.%s is registered in the kernel", k.Name, m.Name)
		return
	}
	declared := signatureText(m.Static, m.Sig)
	reg, ok := c.registeredSignature(k, m.Name, static, sig)
	if !ok {
		c.errorf(pos, CodeNative, "the registered signature of %s.%s cannot be read: %s", k.Name, m.Name, sig)
		return
	}
	if registered := signatureText(static, reg); registered != declared {
		c.errorf(pos, CodeNative, "%s.%s does not match its implementation: declared %s; registered %s", k.Name, m.Name, declared, registered)
	}
}

// registeredSignature resolves a registered signature in the context of
// the class.
func (c *checker) registeredSignature(k *Class, name string, static bool, sig string) (*Func, bool) {
	text := "public class " + k.Name + "\n\n  public method " + name + sig + " executed by kernel\n\nend\n"
	f, errs := syntax.Parse("registration", text)
	if len(errs) > 0 || f.Class == nil || len(f.Class.Members) != 1 {
		return nil, false
	}
	md, ok := f.Class.Members[0].(*syntax.MethodDecl)
	if !ok {
		return nil, false
	}
	quiet, before := c.quiet, len(c.diags)
	c.quiet = false
	fn, _ := c.signature(md.Sig.Params, md.Sig.Returns, md.Sig.Raises)
	c.quiet = quiet
	if len(c.diags) > before {
		c.diags = c.diags[:before]
		return nil, false
	}
	return fn, true
}

// signatureText writes a signature for messages and comparison: static,
// the parameter types with ?, the result and the exceptions in order of
// their names.
func signatureText(static bool, f *Func) string {
	var b strings.Builder
	if static {
		b.WriteString("static ")
	}
	b.WriteString("(")
	for i, p := range f.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.String())
	}
	b.WriteString(")")
	if f.Result != TypVoid {
		b.WriteString(" returns " + f.Result.String())
	}
	if len(f.Raises) > 0 {
		var names []string
		for _, r := range f.Raises {
			names = append(names, r.Name)
		}
		slices.Sort(names)
		b.WriteString(" raises " + strings.Join(names, ", "))
	}
	return b.String()
}
