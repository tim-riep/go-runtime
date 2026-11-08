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
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/rterr"
)

// ExtensionRule tells which extensions of business objects by other
// packages a service offers.
type ExtensionRule string

// Rules for extensions in services.
const (
	// ExtensionsAll offers the extensions of all packages.
	ExtensionsAll ExtensionRule = "all"
	// ExtensionsNone offers none: the service shows the business objects
	// and tables as their owners define them.
	ExtensionsNone ExtensionRule = "none"
	// ExtensionsListed offers the extensions of the packages in
	// Service.ExtensionPackages.
	ExtensionsListed ExtensionRule = "listed"
)

// Service offers entities of business objects and tables without
// business object under public names over OData
// (schema/services/*.json). The kernel serves it below
// /rt/odata/<package>/<service>/ and derives everything else from the
// business objects.
type Service struct {
	Name        string            `json:"name"`
	Owner       PackageID         `json:"owner"`
	File        string            `json:"file"`
	Description map[string]string `json:"description"`
	// Extensions tells which extensions of other packages the service
	// offers; ExtensionPackages lists them for ExtensionsListed.
	Extensions        ExtensionRule `json:"extensions"`
	ExtensionPackages []PackageID   `json:"extensionPackages,omitempty"`
	// EntitySets are the entity sets in the order of their names.
	EntitySets []EntitySet    `json:"entitySets"`
	Src        rterr.Location `json:"-"`
}

// EntitySet is an entity set of a service: an entity of a business object
// or a table without business object, which is read only.
type EntitySet struct {
	// Name is the public name.
	Name string `json:"name"`
	// BusinessObject and Entity name the entity of a business object;
	// Table names a table without business object. Exactly one of both is
	// set.
	BusinessObject Optional[string] `json:"businessObject"`
	Entity         Optional[string] `json:"entity"`
	Table          Optional[string] `json:"table"`
	ReadOnly       bool             `json:"readOnly,omitempty"`
	// Operations and Actions narrow those of the entity; none offers all.
	Operations Optional[[]EntityOperation] `json:"operations"`
	Actions    Optional[[]string]          `json:"actions"`
	// HiddenFields are fields the service does not offer.
	HiddenFields []string `json:"hiddenFields,omitempty"`
	// Navigations name the navigations along foreign keys, by the name of
	// the foreign key.
	Navigations map[string]string `json:"navigations,omitempty"`
	Src         rterr.Location    `json:"-"`
}

// Source returns the table of the entity set: the entity or the table.
func (e *EntitySet) Source() string {
	if t, ok := e.Entity.Get(); ok {
		return t
	}
	return e.Table.Or("")
}

// Offers reports whether the service offers the extensions of a package
// other than its own.
func (s *Service) Offers(pkg PackageID) bool {
	switch {
	case pkg == s.Owner:
		return true
	case s.Extensions == ExtensionsNone:
		return false
	case s.Extensions == ExtensionsListed:
		return slices.Contains(s.ExtensionPackages, pkg)
	}
	return true
}

// LocalName returns the name of the service without namespace.
func (s *Service) LocalName() string {
	if _, local, ok := SplitName(s.Name); ok {
		return local
	}
	return s.Name
}

// Path returns the path the kernel serves the service at, ending with a
// slash: /rt/odata/<package>/<service>/ in lower case.
func (s *Service) Path() string {
	return ODataPath + "/" + strings.ToLower(string(s.Owner)) + "/" + strings.ToLower(s.LocalName()) + "/"
}

// Languages returns the languages of the description.
func (s *Service) Languages() []string { return slices.Sorted(maps.Keys(s.Description)) }

// EntitySet returns the entity set with the given public name.
func (s *Service) EntitySet(name string) *EntitySet {
	for i := range s.EntitySets {
		if s.EntitySets[i].Name == name {
			return &s.EntitySets[i]
		}
	}
	return nil
}

// ServiceNames returns the names of all services in order.
func (s *Schema) ServiceNames() []string { return slices.Sorted(maps.Keys(s.Services)) }

// ServiceAt returns the service of a package with the given local name,
// ignoring case, or nil.
func (s *Schema) ServiceAt(pkg, local string) *Service {
	for _, svc := range s.Services {
		if strings.EqualFold(string(svc.Owner), pkg) && strings.EqualFold(svc.LocalName(), local) {
			return svc
		}
	}
	return nil
}

// ODataPath is the path of the node of the kernel that serves all
// services.
const ODataPath = "/rt/odata"

var odataIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ValidODataIdentifier reports whether name is a simple identifier of
// OData.
func ValidODataIdentifier(name string) bool { return odataIdentifier.MatchString(name) }

// PublicName maps a technical name to a name of OData: a name without
// namespace stays, /P/N becomes P_N. Package IDs have no underscore, so
// the first underscore of a mapped name separates package and name again.
func PublicName(technical string) string {
	if pkg, local, ok := SplitName(technical); ok {
		return string(pkg) + "_" + local
	}
	return technical
}

func cloneService(s *Service) *Service {
	c := *s
	c.Description = maps.Clone(s.Description)
	c.ExtensionPackages = slices.Clone(s.ExtensionPackages)
	c.EntitySets = make([]EntitySet, len(s.EntitySets))
	for i, e := range s.EntitySets {
		e.Operations = cloneOptionalSlice(e.Operations)
		e.Actions = cloneOptionalSlice(e.Actions)
		e.HiddenFields = slices.Clone(e.HiddenFields)
		e.Navigations = maps.Clone(e.Navigations)
		c.EntitySets[i] = e
	}
	return &c
}

func cloneOptionalSlice[T any](o Optional[[]T]) Optional[[]T] {
	if v, ok := o.Get(); ok {
		return Some(slices.Clone(v))
	}
	return o
}
