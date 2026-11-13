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
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/rterr"
)

// ObjectType is a type of objects that a package defines
// (schema/object-types/*.json). Its content is a structure of the defining
// package; other packages ship objects of the type below
// objects/<DEFINING_PACKAGE>/<TYPE>/.
type ObjectType struct {
	Name        string            `json:"name"`
	Owner       PackageID         `json:"owner"`
	File        string            `json:"file"`
	Description map[string]string `json:"description"`
	// Content is the structure that describes the content of an object.
	Content string `json:"content"`
	// Behavior is the class that implements /TRBASE/OBJECT_TYPE_BEHAVIOR.
	Behavior Optional[string] `json:"behavior"`
	Src      rterr.Location   `json:"-"`
}

// Languages returns the languages of the description.
func (t *ObjectType) Languages() []string { return slices.Sorted(maps.Keys(t.Description)) }

// LocalName returns the name of the type without namespace.
func (t *ObjectType) LocalName() string {
	if _, local, ok := SplitName(t.Name); ok {
		return local
	}
	return t.Name
}

// Object is an object of a type that a package defines. Its content is
// JSON in the format of /TRBASE/JSON.
type Object struct {
	Name  string    `json:"name"`
	Owner PackageID `json:"owner"`
	Type  string    `json:"type"`
	File  string    `json:"file"`
	// Content is the content as written in the package until it is checked
	// against the type; then its canonical form.
	Content json.RawMessage `json:"content"`
	// Hash is the SHA-256 of the canonical content in hexadecimal; empty
	// until the content is checked.
	Hash string `json:"hash"`
	// References are the references found in the content; nil until the
	// content is checked.
	References []ObjectReference `json:"references"`
	Src        rterr.Location    `json:"-"`
}

// ReferenceKind is the kind of object a reference in the content of an
// object points to.
type ReferenceKind string

// Reference kinds. Each belongs to a data element of TRBASE; see
// ReferenceDataElements.
const (
	RefPackage         ReferenceKind = "package"
	RefBusinessObject  ReferenceKind = "businessObject"
	RefEntity          ReferenceKind = "entity"
	RefField           ReferenceKind = "field"
	RefAction          ReferenceKind = "action"
	RefService         ReferenceKind = "service"
	RefEntitySet       ReferenceKind = "entitySet"
	RefRole            ReferenceKind = "role"
	RefHTTPNode        ReferenceKind = "httpNode"
	RefAssetRepository ReferenceKind = "assetRepository"
	RefObject          ReferenceKind = "object"
)

// ReferenceDataElements are the data elements of TRBASE whose components
// are references, by their kind.
var ReferenceDataElements = map[string]ReferenceKind{
	"/TRBASE/REF_PACKAGE":          RefPackage,
	"/TRBASE/REF_BUSINESS_OBJECT":  RefBusinessObject,
	"/TRBASE/REF_ENTITY":           RefEntity,
	"/TRBASE/REF_FIELD":            RefField,
	"/TRBASE/REF_ACTION":           RefAction,
	"/TRBASE/REF_SERVICE":          RefService,
	"/TRBASE/REF_ENTITY_SET":       RefEntitySet,
	"/TRBASE/REF_ROLE":             RefRole,
	"/TRBASE/REF_HTTP_NODE":        RefHTTPNode,
	"/TRBASE/REF_ASSET_REPOSITORY": RefAssetRepository,
	"/TRBASE/REF_OBJECT":           RefObject,
}

// ObjectReferenceDomain is the domain of references to objects; data
// elements with this domain may name the type of the objects they refer to.
const ObjectReferenceDomain = "/TRBASE/REF_OBJECT"

// TextLanguageStructure is the structure of TRBASE that the rows of text
// arrays include: an array type whose rows include it and whose unique key
// is its component LANGUAGE holds texts per language.
const TextLanguageStructure = "/TRBASE/TEXT_LANGUAGE"

// TextLanguageComponent is the component of TextLanguageStructure.
const TextLanguageComponent = "LANGUAGE"

// Anchors returns the kinds a dependent reference kind refers to, nearest
// first; nil for kinds that stand alone.
func (k ReferenceKind) Anchors() []ReferenceKind {
	switch k {
	case RefField:
		return []ReferenceKind{RefEntity}
	case RefAction:
		return []ReferenceKind{RefBusinessObject, RefEntity}
	case RefEntitySet:
		return []ReferenceKind{RefService}
	case RefAssetRepository:
		return []ReferenceKind{RefPackage}
	}
	return nil
}

// ObjectReference is a reference in the content of an object.
type ObjectReference struct {
	Kind ReferenceKind `json:"kind"`
	// Target is the referenced object; for dependent kinds the anchor: the
	// entity of a field, the business object or entity of an action, the
	// service of an entity set, the package of an asset repository.
	Target string `json:"target"`
	// Part is the field, action, entity set or repository of dependent
	// kinds; empty otherwise.
	Part string `json:"part"`
	// ObjectType is the type of the referenced object for typed object
	// references.
	ObjectType string `json:"objectType"`
	// Pointer locates the reference in the content (JSON Pointer).
	Pointer string `json:"pointer"`
}

// Name returns the name that takes part in the namespace check: the
// target, or the part of dependent kinds if it carries a namespace.
func (r ObjectReference) Name() string {
	if r.Part != "" {
		return r.Part
	}
	return r.Target
}

// ObjectTypeNames returns the names of all object types in order.
func (s *Schema) ObjectTypeNames() []string { return slices.Sorted(maps.Keys(s.ObjectTypes)) }

// ObjectNames returns the names of all objects in order.
func (s *Schema) ObjectNames() []string { return slices.Sorted(maps.Keys(s.Objects)) }

// ObjectsOf returns the objects of a type in the dependency order of their
// packages, then by name.
func (s *Schema) ObjectsOf(typeName string) []*Object {
	rank := s.PackageRanks()
	var out []*Object
	for _, o := range s.Objects {
		if o.Type == typeName {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b *Object) int {
		return cmp.Or(cmp.Compare(rank[a.Owner], rank[b.Owner]), strings.Compare(a.Name, b.Name))
	})
	return out
}

// PackageRanks numbers the installed packages in dependency order: a
// package comes after all packages it requires, packages that do not
// depend on each other by ID. The kernel comes first.
func (s *Schema) PackageRanks() map[PackageID]int {
	rank := map[PackageID]int{Kernel: 0}
	ids := slices.Sorted(maps.Keys(s.Packages))
	var visit func(id PackageID)
	visiting := map[PackageID]bool{}
	visit = func(id PackageID) {
		if _, ok := rank[id]; ok || visiting[id] {
			return
		}
		visiting[id] = true
		if p, ok := s.Packages[id]; ok {
			deps := slices.Clone(p.Manifest.Requires)
			slices.SortFunc(deps, func(a, b Dependency) int { return strings.Compare(string(a.ID), string(b.ID)) })
			for _, d := range deps {
				visit(d.ID)
			}
		}
		rank[id] = len(rank)
	}
	for _, id := range ids {
		visit(id)
	}
	return rank
}

func cloneObjectType(t *ObjectType) *ObjectType {
	c := *t
	c.Description = maps.Clone(t.Description)
	return &c
}

func cloneObject(o *Object) *Object {
	c := *o
	c.Content = slices.Clone(o.Content)
	c.References = slices.Clone(o.References)
	return &c
}
