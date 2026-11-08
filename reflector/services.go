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

package reflector

import (
	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Service describes a service: its entity sets with the mapping of
// technical to public names and what they offer.
type Service struct {
	FormatVersion int               `json:"formatVersion"`
	Name          string            `json:"name"`
	Package       model.PackageID   `json:"package"`
	Description   map[string]string `json:"description"`
	// Path is where the kernel serves the service.
	Path string `json:"path"`
	// Namespace is the namespace of the schema in the metadata.
	Namespace         string              `json:"namespace"`
	Extensions        model.ExtensionRule `json:"extensions"`
	ExtensionPackages []model.PackageID   `json:"extensionPackages"`
	EntitySets        []ServiceEntitySet  `json:"entitySets"`
	// Contained are the entity types of dependent entities of extensions,
	// offered through navigations of their parents.
	Contained     []ServiceType   `json:"contained"`
	ActionImports []ServiceAction `json:"actionImports"`
	Languages     []string        `json:"languages"`
}

// ServiceEntitySet is an entity set of a service.
type ServiceEntitySet struct {
	Name           string                 `json:"name"`
	BusinessObject model.Optional[string] `json:"businessObject"`
	ReadOnly       bool                   `json:"readOnly"`
	Type           ServiceType            `json:"type"`
}

// ServiceType is an entity type of a service.
type ServiceType struct {
	Name       string                  `json:"name"`
	Table      string                  `json:"table"`
	Operations []model.EntityOperation `json:"operations"`
	Key        []string                `json:"key"`
	Properties []ServiceProperty       `json:"properties"`
	// Navigations are the navigations of the type.
	Navigations []ServiceNavigation `json:"navigations"`
	Actions     []ServiceAction     `json:"actions"`
}

// ServiceProperty maps a field to its public name.
type ServiceProperty struct {
	Name      string `json:"name"`
	Field     string `json:"field"`
	Nullable  bool   `json:"nullable"`
	Computed  bool   `json:"computed"`
	Immutable bool   `json:"immutable"`
	Mandatory bool   `json:"mandatory"`
	ETag      bool   `json:"etag"`
}

// ServiceNavigation is a navigation of an entity type.
type ServiceNavigation struct {
	Name string `json:"name"`
	// Target is the entity set it leads to, or none for a contained
	// navigation.
	Target     model.Optional[string] `json:"target"`
	Type       string                 `json:"type"`
	Collection bool                   `json:"collection"`
	Contained  bool                   `json:"contained"`
	ForeignKey string                 `json:"foreignKey"`
}

// ServiceAction maps an action to its public name.
type ServiceAction struct {
	Name   string          `json:"name"`
	Action string          `json:"action"`
	Origin model.PackageID `json:"origin"`
}

// Service returns the description of a service, or rterr.NotFound.
func (r *Reflector) Service(name string) (*Service, error) {
	svc := r.s.Services[name]
	if svc == nil {
		return nil, &rterr.NotFound{Kind: "service", Name: name}
	}
	m, problems := odata.Build(r.s, nil, svc)
	if len(problems) > 0 {
		return nil, &rterr.InvalidArgument{Argument: "service", Message: odata.Problems(problems)}
	}
	out := &Service{FormatVersion: FormatVersion, Name: svc.Name, Package: svc.Owner, Description: r.texts(svc.Description), Path: svc.Path(),
		Namespace: m.Namespace, Extensions: svc.Extensions, ExtensionPackages: append([]model.PackageID{}, svc.ExtensionPackages...),
		EntitySets: []ServiceEntitySet{}, Contained: []ServiceType{}, ActionImports: actionsOf(m.Imports), Languages: r.langs}
	for _, set := range m.Sets {
		out.EntitySets = append(out.EntitySets, ServiceEntitySet{Name: set.Name, BusinessObject: set.Def.BusinessObject, ReadOnly: set.Def.ReadOnly,
			Type: typeOf(set.Type)})
	}
	for _, t := range m.Contained {
		out.Contained = append(out.Contained, typeOf(t))
	}
	return out, nil
}

// Services returns the services of a package, or of all for an empty ID,
// ordered by name. Services that cannot be offered are left out.
func (r *Reflector) Services(pkg model.PackageID) []*Service {
	out := []*Service{}
	for _, name := range r.s.ServiceNames() {
		if pkg != "" && r.s.Services[name].Owner != pkg {
			continue
		}
		if svc, err := r.Service(name); err == nil {
			out = append(out, svc)
		}
	}
	return out
}

func typeOf(t *odata.EntityType) ServiceType {
	out := ServiceType{Name: t.Name, Table: t.Table.Name, Operations: append([]model.EntityOperation{}, t.Operations...), Key: []string{},
		Properties: []ServiceProperty{}, Navigations: []ServiceNavigation{}, Actions: actionsOf(t.Actions)}
	for _, k := range t.Key {
		out.Key = append(out.Key, k.Name)
	}
	for _, p := range t.Properties {
		out.Properties = append(out.Properties, ServiceProperty{Name: p.Name, Field: p.Field.Name, Nullable: p.Nullable, Computed: p.Computed,
			Immutable: p.Immutable, Mandatory: p.Mandatory, ETag: p == t.ETag})
	}
	for _, n := range t.Navigations {
		nav := ServiceNavigation{Name: n.Name, Type: n.Type.Name, Collection: n.Collection, Contained: n.Contained, ForeignKey: n.ForeignKey}
		if n.Target != nil {
			nav.Target = model.Some(n.Target.Name)
		}
		out.Navigations = append(out.Navigations, nav)
	}
	return out
}

func actionsOf(actions []*odata.Action) []ServiceAction {
	out := []ServiceAction{}
	for _, a := range actions {
		out = append(out, ServiceAction{Name: a.Name, Action: a.Action.Name, Origin: a.Action.Origin})
	}
	return out
}
