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

package kernel

import (
	"sync"

	"github.com/tim-riep/go-runtime/model"
)

// behaviorRegistry holds the business objects of a schema with all their
// installed extensions merged in. The code of a business object knows no
// extension; the transaction buffer finds their entities and behavior
// here, so that installing or removing an extension needs no compilation
// of the package of the business object.
type behaviorRegistry struct {
	mu      sync.Mutex
	schema  *model.Schema
	objects map[string]*model.BusinessObject
}

func newBehaviorRegistry(s *model.Schema) *behaviorRegistry {
	return &behaviorRegistry{schema: s, objects: map[string]*model.BusinessObject{}}
}

// businessObject returns a business object with its extensions, merged
// when it is first needed, or nil.
func (r *behaviorRegistry) businessObject(name string) *model.BusinessObject {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.objects[name]; ok {
		return b
	}
	b := r.schema.MergedBusinessObject(name, nil)
	r.objects[name] = b
	return b
}

// behaviorRegistry returns the registry of the generation.
func (g *generation) behaviorRegistry() *behaviorRegistry {
	g.behaviorsOnce.Do(func() { g.behaviors = newBehaviorRegistry(g.schema) })
	return g.behaviors
}

// businessObject returns a business object of the execution with its
// extensions, or nil.
func (x *execution) businessObject(name string) *model.BusinessObject {
	if x.behaviors == nil {
		x.behaviors = newBehaviorRegistry(x.schema)
	}
	return x.behaviors.businessObject(name)
}

// origins returns the packages whose behavior a business object runs, in
// order: its owner, then the extending packages.
func origins(b *model.BusinessObject) []model.PackageID {
	out := []model.PackageID{b.Owner}
	for _, x := range b.Extensions {
		out = append(out, x.Package)
	}
	return out
}
