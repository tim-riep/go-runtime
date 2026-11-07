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

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// HTTPHandlerInterface is the interface of handlers of http services.
const HTTPHandlerInterface = "/TRBASE/HTTP_HANDLER"

// HandlerRef is a reference of a service node to a handler class.
type HandlerRef struct {
	Class   string
	File    string // the file of the service node
	Pointer string // JSON Pointer of the reference in the file
}

// httpHandlers checks the handler classes of the service nodes of the
// package: a public class of the own or a required package that
// implements /TRBASE/HTTP_HANDLER and has a public constructor without
// parameters. The references are uses of the classes.
func (c *checker) httpHandlers() {
	for _, ref := range c.cfg.HTTPHandlers {
		fail := func(format string, args ...any) {
			c.report(Error, ref.File, syntax.Pos{}, nil, CodeHandler, "%s: "+format, append([]any{ref.Pointer}, args...)...)
		}
		k := c.classes[ref.Class]
		switch {
		case k == nil:
			fail("%s is not a class", ref.Class)
			continue
		case !c.visiblePackage(ref.Class):
			fail("%s belongs to a package that is not required", ref.Class)
			continue
		case k.IsInterface():
			fail("%s is an interface, not a class", ref.Class)
			continue
		case k.Visibility != syntax.Public:
			fail("%s is not public", ref.Class)
			continue
		case k.Test:
			fail("%s is a test class", ref.Class)
			continue
		}
		iface := c.classes[HTTPHandlerInterface]
		if iface == nil || !slices.Contains(allInterfaces(k), iface) {
			fail("%s does not implement %s", ref.Class, HTTPHandlerInterface)
			continue
		}
		if k.Native {
			fail("%s is a native class and has no constructor", ref.Class)
			continue
		}
		for b := k; b != nil; b = b.Base {
			if b.Ctor != nil {
				if len(b.Ctor.Params) > 0 || b.Ctor.Visibility != syntax.Public {
					fail("%s needs a public constructor without parameters", ref.Class)
				}
				break
			}
		}
		c.loc = Site{File: ref.File}
		c.use(syntax.Pos{Line: 1, Col: 1}, "class", ref.Class, "")
		c.loc = Site{}
	}
}
