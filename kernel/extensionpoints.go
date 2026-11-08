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
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/rterr"
)

// activation is the activation of implementations: the global settings
// and those per tenant, each only where one was made. Implementations
// without setting are active.
type activation struct {
	global  map[string]bool
	tenants map[string]map[string]bool // tenant -> implementation -> active
	// traces are the running traces of the selection by user.
	traces map[string]repo.AuthTrace
}

// where tells where the setting that decides the activation of an
// implementation in a tenant was made.
func (a *activation) where(impl, tenant string) string {
	if _, ok := a.tenants[tenant][impl]; ok && tenant != "" {
		return "in tenant " + tenant
	}
	return "globally"
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

// extensionRegistry holds the extension points of a schema with their
// implementations in the order they run. The defining code knows no
// implementation; every execution selects them here.
type extensionRegistry struct {
	points map[string]*registeredPoint
}

type registeredPoint struct {
	point   *model.ExtensionPoint
	domains map[string]*model.Domain // of the filter fields
	impls   []registeredImplementation
}

type registeredImplementation struct {
	impl   *model.Implementation
	values map[string][]model.ValueEntry // per filter field, with defaults
}

func newExtensionRegistry(s *model.Schema) *extensionRegistry {
	r := &extensionRegistry{points: map[string]*registeredPoint{}}
	for _, name := range s.ExtensionPointNames() {
		p := s.ExtensionPoints[name]
		rp := &registeredPoint{point: p, domains: map[string]*model.Domain{}}
		for _, f := range p.Filters {
			rp.domains[f.Name] = s.Domains[s.FilterDomain(f)]
		}
		for _, impl := range s.ImplementationsOf(name) {
			rp.impls = append(rp.impls, registeredImplementation{impl: impl, values: s.ImplementationValues(impl)})
		}
		r.points[name] = rp
	}
	return r
}

// extensionRegistry returns the registry of the generation.
func (g *generation) extensionRegistry() *extensionRegistry {
	g.extensionsOnce.Do(func() { g.extensions = newExtensionRegistry(g.schema) })
	return g.extensions
}

// matches reports whether the values of an implementation match the
// filter values, and otherwise the first field that does not.
func (rp *registeredPoint) matches(ri registeredImplementation, values map[string]string) (string, bool) {
	for _, f := range rp.point.Filters {
		d := rp.domains[f.Name]
		if d == nil || !model.EntriesMatch(d, ri.values[f.Name], values[f.Name]) {
			return f.Name, false
		}
	}
	return "", true
}

// SelectImplementations selects the active implementations of an
// extension point whose filter values match, in order. A single
// extension point runs exactly one: the matching implementation or the
// fallback class.
func (x *execution) SelectImplementations(point string, values map[string]string, at native.CheckLocation) (native.Selection, error) {
	if x.extensions == nil {
		x.extensions = newExtensionRegistry(x.schema)
	}
	rp := x.extensions.points[point]
	if rp == nil {
		return native.Selection{}, fmt.Errorf("extension point %s is not installed", point)
	}
	act, err := x.loadActivation()
	if err != nil {
		return native.Selection{}, err
	}
	p := rp.point
	sel := native.Selection{Use: p.Use, CommitAllowed: p.CommitAllowed}
	trace := x.selectionTrace(act)
	var reasons []SelectionReason
	note := func(impl, reason string) {
		if trace != nil {
			reasons = append(reasons, SelectionReason{Implementation: impl, Reason: reason})
		}
	}
	defer func() {
		if trace != nil {
			x.recordSelection(point, values, reasons, at)
		}
	}()
	for _, ri := range rp.impls {
		if !act.active(ri.impl.Name, x.tenant) {
			note(ri.impl.Name, "inactive "+act.where(ri.impl.Name, x.tenant))
			continue
		}
		if field, ok := rp.matches(ri, values); !ok {
			note(ri.impl.Name, "the filter field "+field+" does not match")
			continue
		}
		note(ri.impl.Name, "selected")
		sel.Classes = append(sel.Classes, ri.impl.Class)
		sel.Implementations = append(sel.Implementations, ri.impl.Name)
	}
	if len(sel.Implementations) == 0 && p.Fallback.Valid() {
		note(p.Fallback.Or(""), "fallback class, no implementation matches")
	}
	switch {
	case p.Use == model.UseSingle && len(sel.Implementations) > 1:
		return native.Selection{}, &native.ExtensionError{Class: model.KernelNamespace + "IMPLEMENTATION_CONFLICT", ExtensionPoint: point,
			Implementations: sel.Implementations[:2],
			Text: fmt.Sprintf("extension point %s runs a single implementation, but %s and %s both match %s", point, sel.Implementations[0],
				sel.Implementations[1], filterValuesText(p, values))}
	case len(sel.Implementations) > 0:
	case p.Fallback.Valid():
		sel.Classes, sel.Fallback = []string{p.Fallback.Or("")}, true
	case p.Use == model.UseSingle:
		return native.Selection{}, &native.ExtensionError{Class: model.KernelNamespace + "NO_IMPLEMENTATION", ExtensionPoint: point,
			Text: fmt.Sprintf("no active implementation of extension point %s matches %s, and it has no fallback class", point, filterValuesText(p, values))}
	}
	return sel, nil
}

// ImplementationActive reports whether an implementation is active in the
// tenant of the execution.
func (x *execution) ImplementationActive(name string) (bool, error) {
	a, err := x.loadActivation()
	if err != nil {
		return false, err
	}
	return a.active(name, x.tenant), nil
}

// filterValuesText writes filter values for messages, in the order of the
// fields.
func filterValuesText(p *model.ExtensionPoint, values map[string]string) string {
	if len(p.Filters) == 0 {
		return "without filter values"
	}
	var parts []string
	for _, f := range p.Filters {
		parts = append(parts, fmt.Sprintf("%s = %q", f.Name, values[f.Name]))
	}
	return strings.Join(parts, ", ")
}

// loadActivation returns the activation of the implementations for the
// execution, read once.
func (x *execution) loadActivation() (*activation, error) {
	if x.activation == nil {
		a, err := x.k.cachedActivation(x.ctx)
		if err != nil {
			return nil, err
		}
		x.activation = a
	}
	return x.activation, nil
}
