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
	"slices"

	"github.com/tim-riep/go-runtime/rterr"
)

// AuthorizationObject is an authorization object of a package
// (schema/authorization-objects/*.json): fields whose values grants will
// name, and with the field ACTIVITY the activities that can be granted.
type AuthorizationObject struct {
	Name   string               `json:"name"`
	Owner  PackageID            `json:"owner"`
	File   string               `json:"file"`
	Texts  map[string]Label     `json:"texts"`
	Fields []AuthorizationField `json:"fields"`
	// Activities are the codes of the activities that can be granted, in
	// order; empty without the field ACTIVITY.
	Activities []string       `json:"activities,omitempty"`
	Src        rterr.Location `json:"-"`
}

// AuthorizationField is a field of an authorization object. It has a data
// element or a domain, except the field ACTIVITY, whose type the kernel
// gives.
type AuthorizationField struct {
	Name        string           `json:"name"`
	DataElement Optional[string] `json:"dataElement"`
	Domain      Optional[string] `json:"domain"`
}

// IsActivity reports whether the field is the field ACTIVITY.
func (f AuthorizationField) IsActivity() bool { return f.Name == ActivityField }

// HasActivity reports whether the object has the field ACTIVITY.
func (o *AuthorizationObject) HasActivity() bool {
	return slices.ContainsFunc(o.Fields, AuthorizationField.IsActivity)
}

// Field returns the field with a name.
func (o *AuthorizationObject) Field(name string) (AuthorizationField, bool) {
	for _, f := range o.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return AuthorizationField{}, false
}

// Limits of authorization objects.
const (
	MaxAuthorizationFields    = 10
	MaxAuthorizationFieldName = 30
)

// AuthorizationFieldDomain returns the domain of a field: that of its data
// element, its own, or ActivityDomain for ACTIVITY. It returns "" for an
// unknown data element.
func (s *Schema) AuthorizationFieldDomain(f AuthorizationField) string {
	if f.IsActivity() && !f.DataElement.Valid() && !f.Domain.Valid() {
		return ActivityDomain
	}
	if de, ok := f.DataElement.Get(); ok {
		if d := s.DataElements[de]; d != nil {
			return d.Domain
		}
		return ""
	}
	return f.Domain.Or("")
}

// AuthorizationObjectNames returns the names of all authorization objects
// in sorted order.
func (s *Schema) AuthorizationObjectNames() []string {
	return slices.Sorted(maps.Keys(s.AuthorizationObjects))
}

func cloneAuthorizationObject(o *AuthorizationObject) *AuthorizationObject {
	c := *o
	c.Texts = maps.Clone(o.Texts)
	c.Fields = slices.Clone(o.Fields)
	c.Activities = slices.Clone(o.Activities)
	return &c
}

// Languages returns the languages of the texts in sorted order.
func (o *AuthorizationObject) Languages() []string { return slices.Sorted(maps.Keys(o.Texts)) }
