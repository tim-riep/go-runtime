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

// EntityOperation is an operation on the instances of an entity.
type EntityOperation string

// Operations of entities.
const (
	OperationCreate EntityOperation = "create"
	OperationUpdate EntityOperation = "update"
	OperationDelete EntityOperation = "delete"
)

// EntityOperations lists the operations in their order.
var EntityOperations = []EntityOperation{OperationCreate, OperationUpdate, OperationDelete}

// ReadActivityKey is the key of the activity for reading in the
// authorization of an entity.
const ReadActivityKey = "read"

// Numbering tells who gives the key of new instances.
type Numbering string

// Kinds of numbering.
const (
	// NumberingExternal: the caller gives the key.
	NumberingExternal Numbering = "external"
	// NumberingUUID: the kernel gives the single key field of type UUID a
	// new value when the instance is created.
	NumberingUUID Numbering = "uuid"
)

// When tells when a determination runs.
type When string

// Times of determinations.
const (
	// WhenModify runs at the end of the entity statement that changed the
	// instances.
	WhenModify When = "modify"
	// WhenSave runs in the save sequence.
	WhenSave When = "save"
)

// ResultSelf is the result of an action that returns the instances it
// acted on.
const ResultSelf = "self"

// Names of the fields that link new instances in the rows of create.
const (
	LinkField       = "LINK"
	ParentLinkField = "PARENT_LINK"
)

// BusinessObject combines a root table and dependent tables into one
// object with its behavior (schema/business-objects/*.json). Code changes
// its tables only through entity statements.
type BusinessObject struct {
	Name          string            `json:"name"`
	Owner         PackageID         `json:"owner"`
	File          string            `json:"file"`
	Description   map[string]string `json:"description"`
	Root          string            `json:"root"`
	BehaviorClass string            `json:"behaviorClass"`
	// Entities are the entities in the order of the tree: the root first,
	// then the children of every entity by name, depth first.
	Entities []Entity       `json:"entities"`
	Src      rterr.Location `json:"-"`
}

// Entity is a table of a business object with its behavior.
type Entity struct {
	Table string `json:"table"`
	// Parent and ForeignKey are set for dependent entities: the parent
	// entity and the foreign key of the table to it.
	Parent         Optional[string]           `json:"parent"`
	ForeignKey     Optional[string]           `json:"foreignKey"`
	Operations     []EntityOperation          `json:"operations"`
	Numbering      Numbering                  `json:"numbering"`
	Fields         map[string]FieldProperties `json:"fields"`
	Administrative AdministrativeFields       `json:"administrative"`
	ETag           Optional[string]           `json:"etag"`
	Determinations []Determination            `json:"determinations"`
	Validations    []Validation               `json:"validations"`
	Actions        []Action                   `json:"actions"`
	// Authorization is nil for a dependent entity that takes the
	// authorization of the root.
	Authorization *EntityAuthorization `json:"authorization"`
	Src           rterr.Location       `json:"-"`
}

// FieldProperties are the static properties of a field of an entity.
type FieldProperties struct {
	ReadOnly         bool `json:"readOnly,omitempty"`
	ReadOnlyOnUpdate bool `json:"readOnlyOnUpdate,omitempty"`
	Mandatory        bool `json:"mandatory,omitempty"`
}

// AdministrativeFields are the fields that the kernel fills when it
// saves an instance.
type AdministrativeFields struct {
	CreatedBy Optional[string] `json:"createdBy"`
	CreatedAt Optional[string] `json:"createdAt"`
	ChangedBy Optional[string] `json:"changedBy"`
	ChangedAt Optional[string] `json:"changedAt"`
}

// Fields returns the administrative fields that are set, in the order
// createdBy, createdAt, changedBy, changedAt.
func (a AdministrativeFields) Fields() []string {
	var out []string
	for _, f := range []Optional[string]{a.CreatedBy, a.CreatedAt, a.ChangedBy, a.ChangedAt} {
		if n, ok := f.Get(); ok {
			out = append(out, n)
		}
	}
	return out
}

// Trigger tells which changes of an instance run a determination or a
// validation: the operations, and for update optionally the fields whose
// change triggers it.
type Trigger struct {
	On     []EntityOperation `json:"on"`
	Fields []string          `json:"fields,omitempty"`
}

// Determination is a method of the behavior class that changes instances.
type Determination struct {
	Name string `json:"name"`
	When When   `json:"when"`
	Trigger
}

// Validation is a method of the behavior class that checks instances in
// the save sequence.
type Validation struct {
	Name string `json:"name"`
	Trigger
}

// Action is a method of the behavior class that code runs with execute.
type Action struct {
	Name string `json:"name"`
	// Instance actions run on instances given by their keys; static ones
	// on none.
	Instance bool `json:"instance"`
	// Parameter is a structure, if the action takes one.
	Parameter Optional[string] `json:"parameter"`
	// Result is ResultSelf, a structure or none.
	Result Optional[string] `json:"result"`
	// Modifies tells that the action changes instances.
	Modifies bool `json:"modifies"`
}

// EntityAuthorization checks the instances of an entity with an
// authorization object: its fields take the values of fields of the
// entity, and every operation, read and every action has an activity.
type EntityAuthorization struct {
	Object string `json:"object"`
	// Fields maps fields of the object to fields of the entity.
	Fields map[string]string `json:"fields"`
	// Activities maps operations, "read" and actions to activities.
	Activities map[string]string `json:"activities"`
}

// Entity returns the entity of a table.
func (b *BusinessObject) Entity(table string) *Entity {
	for i := range b.Entities {
		if b.Entities[i].Table == table {
			return &b.Entities[i]
		}
	}
	return nil
}

// Children returns the entities whose parent is the entity of a table.
func (b *BusinessObject) Children(table string) []*Entity {
	var out []*Entity
	for i := range b.Entities {
		if p, ok := b.Entities[i].Parent.Get(); ok && p == table {
			out = append(out, &b.Entities[i])
		}
	}
	return out
}

// Languages returns the languages of the description in sorted order.
func (b *BusinessObject) Languages() []string { return slices.Sorted(maps.Keys(b.Description)) }

// Allows reports whether the entity allows an operation.
func (e *Entity) Allows(op EntityOperation) bool { return slices.Contains(e.Operations, op) }

// Action returns the action with a name.
func (e *Entity) Action(name string) *Action {
	for i := range e.Actions {
		if e.Actions[i].Name == name {
			return &e.Actions[i]
		}
	}
	return nil
}

// Property returns the properties of a field.
func (e *Entity) Property(field string) FieldProperties { return e.Fields[field] }

// Administered reports whether the kernel fills a field: administrative
// fields and the ETag.
func (e *Entity) Administered(field string) bool {
	return slices.Contains(e.Administrative.Fields(), field) || e.ETag == Some(field)
}

// BusinessObjectNames returns the names of all business objects in
// sorted order.
func (s *Schema) BusinessObjectNames() []string { return slices.Sorted(maps.Keys(s.BusinessObjects)) }

// BusinessObjectOf returns the business object a table belongs to and
// its entity, or nil.
func (s *Schema) BusinessObjectOf(table string) (*BusinessObject, *Entity) {
	for _, name := range s.BusinessObjectNames() {
		b := s.BusinessObjects[name]
		if e := b.Entity(table); e != nil {
			return b, e
		}
	}
	return nil, nil
}

// SortEntities orders entities as a tree: the root first, then the
// children of every entity by name, depth first. Entities that are not
// reachable from the root follow by name, so that validation reports them.
func SortEntities(root string, entities []Entity) []Entity {
	byName := map[string]Entity{}
	for _, e := range entities {
		byName[e.Table] = e
	}
	var out []Entity
	done := map[string]bool{}
	var visit func(table string)
	visit = func(table string) {
		e, ok := byName[table]
		if !ok || done[table] {
			return
		}
		done[table] = true
		out = append(out, e)
		for _, name := range slices.Sorted(maps.Keys(byName)) {
			if p, ok := byName[name].Parent.Get(); ok && p == table {
				visit(name)
			}
		}
	}
	visit(root)
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		if !done[name] {
			done[name] = true
			out = append(out, byName[name])
		}
	}
	return out
}

func cloneBusinessObject(b *BusinessObject) *BusinessObject {
	c := *b
	c.Description = maps.Clone(b.Description)
	c.Entities = make([]Entity, len(b.Entities))
	for i, e := range b.Entities {
		e.Operations = slices.Clone(e.Operations)
		e.Fields = maps.Clone(e.Fields)
		e.Determinations = slices.Clone(e.Determinations)
		e.Validations = slices.Clone(e.Validations)
		e.Actions = slices.Clone(e.Actions)
		if e.Authorization != nil {
			a := *e.Authorization
			a.Fields, a.Activities = maps.Clone(a.Fields), maps.Clone(a.Activities)
			e.Authorization = &a
		}
		c.Entities[i] = e
	}
	return &c
}
