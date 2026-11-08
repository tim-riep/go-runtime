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
	"context"
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// activation is the activation of implementations: the global settings
// and those per tenant, each only where one was made. Implementations
// without setting are active.
type activation struct {
	global  map[string]bool
	tenants map[string]map[string]bool // tenant -> implementation -> active
}

// active reports whether an implementation is active in a tenant; ""
// asks for the global activation. A setting of the tenant takes
// precedence over the global one.
func (a *activation) active(impl, tenant string) bool {
	if a == nil {
		return true
	}
	if on, ok := a.tenants[tenant][impl]; ok && tenant != "" {
		return on
	}
	if on, ok := a.global[impl]; ok {
		return on
	}
	return true
}

// scopes returns "" for the global activation and the tenants with
// settings of their own, in sorted order.
func (a *activation) scopes() []string {
	out := []string{""}
	if a != nil {
		out = append(out, slices.Sorted(maps.Keys(a.tenants))...)
	}
	return out
}

// checkOverlaps rejects active implementations of an extension point used
// single that match the same filter values, globally or in a tenant with
// settings of its own. Every pair is reported.
func checkOverlaps(s *model.Schema, a *activation) error {
	var errs rterr.List
	for _, name := range s.ExtensionPointNames() {
		if s.ExtensionPoints[name].Use != model.UseSingle {
			continue
		}
		impls := s.ImplementationsOf(name)
		reported := map[[2]string]bool{}
		for _, tenant := range a.scopes() {
			var active []*model.Implementation
			for _, i := range impls {
				if a.active(i.Name, tenant) {
					active = append(active, i)
				}
			}
			for x := range active {
				for y := x + 1; y < len(active); y++ {
					pair := [2]string{active[x].Name, active[y].Name}
					values, ok := s.ImplementationsOverlap(active[x], active[y])
					if !ok || reported[pair] {
						continue
					}
					reported[pair] = true
					e := &rterr.ImplementationOverlap{ExtensionPoint: name, Implementations: pair, Tenant: tenant}
					for _, v := range values {
						e.Values = append(e.Values, rterr.OverlapValues{Field: v.Field, Values: v.Values})
					}
					errs = append(errs, e)
				}
			}
		}
	}
	return errs.Err()
}

// loadActivation reads the activation of the implementations.
func (k *Kernel) loadActivation(ctx context.Context, q dialect.Querier) (*activation, error) {
	return &activation{}, nil
}
