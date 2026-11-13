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

import "github.com/tim-riep/go-runtime/lang/syntax"

// ObjectTypeBehavior is the interface of the behavior classes of object
// types that packages define.
const ObjectTypeBehavior = "/TRBASE/OBJECT_TYPE_BEHAVIOR"

// objectTypeBehaviors checks the behavior classes of the object types of
// the package: a public class of the package that implements
// ObjectTypeBehavior and has a public constructor without parameters.
func (c *checker) objectTypeBehaviors() {
	if c.cfg.Schema == nil {
		return
	}
	for _, name := range c.cfg.Schema.ObjectTypeNames() {
		t := c.cfg.Schema.ObjectTypes[name]
		class, ok := t.Behavior.Get()
		if !ok || t.Owner != c.cfg.Package {
			continue
		}
		iface := c.classes[ObjectTypeBehavior]
		if iface == nil || !iface.IsInterface() {
			c.report(Error, t.File, syntax.Pos{}, nil, CodeObjectType, "/behavior: %s needs the package TRBASE with the interface %s", class, ObjectTypeBehavior)
			continue
		}
		if msg := c.implementingClass(class, iface, true); msg != "" {
			c.report(Error, t.File, syntax.Pos{}, nil, CodeObjectType, "/behavior: %s", msg)
			continue
		}
		c.useIn(t.File, "class", class)
	}
}
