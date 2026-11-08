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

package model

import (
	"cmp"
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/rterr"
)

// ExtensionUse tells how many implementations of an extension point run.
type ExtensionUse string

// Uses of extension points.
const (
	// UseSingle runs exactly one implementation.
	UseSingle ExtensionUse = "single"
	// UseMultiple runs all matching active implementations in order.
	UseMultiple ExtensionUse = "multiple"
)

// MaxExtensionFilters is the largest number of filter fields of an
// extension point.
const MaxExtensionFilters = 5

// ExtensionPoint is a place in the code of a package that other packages
// implement (schema/extension-points/*.json): an interface of the package,
// how many implementations run, filter fields that select them and an
// optional fallback class.
type ExtensionPoint struct {
	Name        string            `json:"name"`
	Owner       PackageID         `json:"owner"`
	File        string            `json:"file"`
	Description map[string]string `json:"description"`
	Interface   string            `json:"interface"`
	Use         ExtensionUse      `json:"use"`
	Filters     []ExtensionFilter `json:"filters"`
	// Fallback is the class that runs when no implementation matches.
	Fallback Optional[string] `json:"fallback"`
	// CommitAllowed lets implementations commit or roll back the default
	// session.
	CommitAllowed bool           `json:"commitAllowed"`
	Src           rterr.Location `json:"-"`
}

// ExtensionFilter is a filter field of an extension point. Like a field
// of an authorization object it has a data element or a domain.
type ExtensionFilter struct {
	Name        string           `json:"name"`
	DataElement Optional[string] `json:"dataElement"`
	Domain      Optional[string] `json:"domain"`
	// Default are the values of implementations that do not name the
	// field; without default every implementation names it.
	Default Optional[[]ValueEntry] `json:"default"`
}

// Filter returns the filter field with a name.
func (p *ExtensionPoint) Filter(name string) (ExtensionFilter, bool) {
	for _, f := range p.Filters {
		if f.Name == name {
			return f, true
		}
	}
	return ExtensionFilter{}, false
}

// Languages returns the languages of the description in sorted order.
func (p *ExtensionPoint) Languages() []string { return slices.Sorted(maps.Keys(p.Description)) }

// Implementation is a class of a package that an extension point runs
// (schema/extension-implementations/*.json), with the values of the filter
// fields it is meant for and its place in the order.
type Implementation struct {
	Name           string            `json:"name"`
	Owner          PackageID         `json:"owner"`
	File           string            `json:"file"`
	Description    map[string]string `json:"description"`
	ExtensionPoint string            `json:"extensionPoint"`
	Class          string            `json:"class"`
	// Filters are the values per filter field as written; fields with a
	// default may be left out, see Schema.ImplementationValues.
	Filters map[string][]ValueEntry `json:"filters"`
	Order   int32                   `json:"order"`
	Src     rterr.Location          `json:"-"`
}

// Languages returns the languages of the description in sorted order.
func (i *Implementation) Languages() []string { return slices.Sorted(maps.Keys(i.Description)) }

// FilterNames returns the names of the given filter fields in sorted
// order.
func (i *Implementation) FilterNames() []string { return slices.Sorted(maps.Keys(i.Filters)) }

// FilterDomain returns the domain of a filter field: that of its data
// element or its own. It returns "" for an unknown data element.
func (s *Schema) FilterDomain(f ExtensionFilter) string {
	return s.AuthorizationFieldDomain(AuthorizationField{Name: f.Name, DataElement: f.DataElement, Domain: f.Domain})
}

// ImplementationValues returns the values of every filter field of the
// extension point of an implementation: those it gives, else the default
// of the field. Fields without either are missing from the result.
func (s *Schema) ImplementationValues(impl *Implementation) map[string][]ValueEntry {
	out := map[string][]ValueEntry{}
	p := s.ExtensionPoints[impl.ExtensionPoint]
	if p == nil {
		return out
	}
	for _, f := range p.Filters {
		if v, ok := impl.Filters[f.Name]; ok {
			out[f.Name] = v
		} else if d, ok := f.Default.Get(); ok {
			out[f.Name] = d
		}
	}
	return out
}

// ExtensionPointNames returns the names of all extension points in sorted
// order.
func (s *Schema) ExtensionPointNames() []string { return slices.Sorted(maps.Keys(s.ExtensionPoints)) }

// ImplementationNames returns the names of all implementations in sorted
// order.
func (s *Schema) ImplementationNames() []string { return slices.Sorted(maps.Keys(s.Implementations)) }

// ImplementationsOf returns the implementations of an extension point in
// the order they run: by order, then package, then name.
func (s *Schema) ImplementationsOf(point string) []*Implementation {
	var out []*Implementation
	for _, i := range s.Implementations {
		if i.ExtensionPoint == point {
			out = append(out, i)
		}
	}
	slices.SortFunc(out, CompareImplementations)
	return out
}

// CompareImplementations orders implementations by order, package ID and
// name, so that the order is always unique.
func CompareImplementations(a, b *Implementation) int {
	return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.Name, b.Name))
}

// FilterOverlap is the overlap of two implementations in one filter
// field.
type FilterOverlap struct {
	Field  string `json:"field"`
	Values string `json:"values"` // the values both match, e.g. "DE" or "A to C"
}

// ImplementationsOverlap reports whether two implementations of the same
// extension point match a common set of filter values: in every filter
// field the values of both meet. It returns the common values per field.
// Implementations of a point without filter fields always overlap.
func (s *Schema) ImplementationsOverlap(a, b *Implementation) ([]FilterOverlap, bool) {
	p := s.ExtensionPoints[a.ExtensionPoint]
	if p == nil || a.ExtensionPoint != b.ExtensionPoint {
		return nil, false
	}
	av, bv := s.ImplementationValues(a), s.ImplementationValues(b)
	var out []FilterOverlap
	for _, f := range p.Filters {
		d := s.Domains[s.FilterDomain(f)]
		if d == nil {
			return nil, false
		}
		common, ok := EntriesOverlap(d, av[f.Name], bv[f.Name])
		if !ok {
			return nil, false
		}
		out = append(out, FilterOverlap{Field: f.Name, Values: common})
	}
	return out, true
}

func cloneExtensionPoint(p *ExtensionPoint) *ExtensionPoint {
	c := *p
	c.Description = maps.Clone(p.Description)
	c.Filters = make([]ExtensionFilter, len(p.Filters))
	for i, f := range p.Filters {
		if d, ok := f.Default.Get(); ok {
			f.Default = Some(slices.Clone(d))
		}
		c.Filters[i] = f
	}
	return &c
}

func cloneImplementation(i *Implementation) *Implementation {
	c := *i
	c.Description = maps.Clone(i.Description)
	c.Filters = make(map[string][]ValueEntry, len(i.Filters))
	for k, v := range i.Filters {
		c.Filters[k] = slices.Clone(v)
	}
	return &c
}
