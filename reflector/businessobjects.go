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
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/structure"
)

// BusinessObject is the resolved definition of a business object: its
// entities as a tree with their fields, behavior and authorization.
type BusinessObject struct {
	FormatVersion int             `json:"formatVersion"`
	Name          string          `json:"name"`
	Package       model.PackageID `json:"package"`
	// Description holds the description by language.
	Description map[string]string `json:"description"`
	// DataClass is application, configuration or runtimeConfiguration.
	DataClass model.DataClass `json:"dataClass"`
	// BehaviorClass is null for a business object without behavior.
	BehaviorClass model.Optional[string] `json:"behaviorClass"`
	Root          *Entity                `json:"root"`
	// Extensions are the installed extensions merged into the
	// description, in the order their behavior runs.
	Extensions []model.ExtensionRef `json:"extensions"`
	// Languages are the languages of the texts.
	Languages []string `json:"languages"`
}

// Entity is an entity of a business object with its dependent entities.
type Entity struct {
	Table string `json:"table"`
	// Origin is the package that defines the entity: the owner of the
	// business object or an extending package.
	Origin model.PackageID `json:"origin"`
	Texts  map[string]Text `json:"texts"`
	Key    []string        `json:"key"`
	// ParentKey links a dependent entity to its parent; null for the root.
	ParentKey      *ParentKey              `json:"parentKey"`
	Operations     []model.EntityOperation `json:"operations"`
	Numbering      model.Numbering         `json:"numbering"`
	Fields         []EntityField           `json:"fields"`
	Determinations []model.Determination   `json:"determinations"`
	Validations    []model.Validation      `json:"validations"`
	Actions        []EntityAction          `json:"actions"`
	// Authorization is the authorization that checks the entity, null
	// for none. AuthorizationSource tells whether it is the entity's own,
	// inherited from AuthorizationHolder, a parent entity, or the
	// configuration object with the values of AuthorizationConstants.
	Authorization          *model.EntityAuthorization                `json:"authorization"`
	AuthorizationSource    model.Optional[model.AuthorizationSource] `json:"authorizationSource"`
	AuthorizationHolder    model.Optional[string]                    `json:"authorizationHolder"`
	AuthorizationConstants map[string]string                         `json:"authorizationConstants"`
	Children               []*Entity                                 `json:"children"`
}

// ParentKey is the foreign key of a dependent entity to its parent with
// the fields of the entity and of the parent.
type ParentKey struct {
	Parent     string             `json:"parent"`
	ForeignKey string             `json:"foreignKey"`
	Fields     []model.ColumnPair `json:"fields"`
}

// EntityField is a field of an entity with its type and texts, as in the
// definition of the table, and its properties in the business object.
type EntityField struct {
	structure.Component
	Key              bool `json:"key"`
	ReadOnly         bool `json:"readOnly"`
	ReadOnlyOnUpdate bool `json:"readOnlyOnUpdate"`
	Mandatory        bool `json:"mandatory"`
	// Administrative names what the kernel writes into the field when it
	// saves: createdBy, createdAt, changedBy or changedAt; null for none.
	Administrative model.Optional[string] `json:"administrative"`
	ETag           bool                   `json:"etag"`
	// PropertiesBy are the extending packages that set properties of the
	// field.
	PropertiesBy []model.PackageID `json:"propertiesBy"`
}

// EntityAction is an action with its parameter and result.
type EntityAction struct {
	model.Action
	// Activity is the activity of the authorization; null without.
	Activity model.Optional[string] `json:"activity"`
}

// BusinessObject returns the definition of a business object, or
// rterr.NotFound.
func (r *Reflector) BusinessObject(name string) (*BusinessObject, error) {
	b := r.s.MergedBusinessObject(name, nil)
	if b == nil {
		return nil, &rterr.NotFound{Kind: "business object", Name: name}
	}
	out := &BusinessObject{FormatVersion: FormatVersion, Name: b.Name, Package: b.Owner, Description: r.texts(b.Description),
		DataClass: b.DataClass, BehaviorClass: b.BehaviorClass, Extensions: b.Extensions, Languages: r.langs}
	if out.Extensions == nil {
		out.Extensions = []model.ExtensionRef{}
	}
	entities := map[string]*Entity{}
	for i := range b.Entities {
		e := &b.Entities[i]
		re, err := r.entity(b, e)
		if err != nil {
			return nil, err
		}
		entities[e.Table] = re
		if p, ok := e.Parent.Get(); ok {
			entities[p].Children = append(entities[p].Children, re)
		} else {
			out.Root = re
		}
	}
	return out, nil
}

// entity describes an entity without its children.
func (r *Reflector) entity(b *model.BusinessObject, e *model.Entity) (*Entity, error) {
	d, err := structure.Resolve(r.s, r.langs, e.Table)
	if err != nil {
		return nil, err
	}
	t := r.s.Tables[e.Table]
	out := &Entity{Table: e.Table, Origin: e.Origin, Texts: map[string]Text{}, Operations: e.Operations, Numbering: e.Numbering, Fields: []EntityField{},
		Determinations: e.Determinations, Validations: e.Validations, Actions: []EntityAction{}, AuthorizationConstants: map[string]string{}, Children: []*Entity{}}
	for l, tx := range d.Texts {
		out.Texts[l] = Text{Label: tx.Label, Description: tx.Description}
	}
	for _, k := range t.PrimaryKey {
		if k != model.TenantField {
			out.Key = append(out.Key, k)
		}
	}
	if p, ok := e.Parent.Get(); ok {
		fk := t.ForeignKey(e.ForeignKey.Or(""))
		out.ParentKey = &ParentKey{Parent: p, ForeignKey: fk.Name, Fields: fk.Columns}
	}
	var auth *model.EntityAuthorization
	if ra, ok := b.AuthorizationOf(e); ok {
		auth = ra.Authorization
		out.Authorization, out.AuthorizationSource = auth, model.Some(ra.Source)
		out.AuthorizationHolder = model.Some(ra.Holder.Table)
		if ra.Constants != nil {
			out.AuthorizationConstants = ra.Constants
		}
		if ra.Source == model.AuthorizationInherited {
			auth = nil
		}
	}
	admin := map[string]string{}
	for kind, f := range map[string]model.Optional[string]{"createdBy": e.Administrative.CreatedBy, "createdAt": e.Administrative.CreatedAt,
		"changedBy": e.Administrative.ChangedBy, "changedAt": e.Administrative.ChangedAt} {
		if n, ok := f.Get(); ok {
			admin[n] = kind
		}
	}
	for _, c := range d.Components {
		p := e.Fields[c.Name]
		f := EntityField{Component: c, Key: slices.Contains(out.Key, c.Name), ReadOnly: p.ReadOnly, ReadOnlyOnUpdate: p.ReadOnlyOnUpdate,
			Mandatory: p.Mandatory, ETag: e.ETag.Or("") == c.Name, PropertiesBy: e.FieldOrigins[c.Name]}
		if f.PropertiesBy == nil {
			f.PropertiesBy = []model.PackageID{}
		}
		if kind, ok := admin[c.Name]; ok {
			f.Administrative = model.Some(kind)
		}
		out.Fields = append(out.Fields, f)
	}
	for _, a := range e.Actions {
		ra := EntityAction{Action: a}
		if act, ok := a.Activity.Get(); ok {
			ra.Activity = model.Some(act)
		} else if auth != nil {
			if act, ok := auth.Activities[a.Name]; ok {
				ra.Activity = model.Some(act)
			}
		} else if out.Authorization != nil {
			if act, ok := out.Authorization.Activities[string(model.OperationUpdate)]; ok {
				ra.Activity = model.Some(act)
			}
		}
		out.Actions = append(out.Actions, ra)
	}
	return out, nil
}

// BusinessObjects returns the business objects of a package, or of all
// packages for "", ordered by name.
func (r *Reflector) BusinessObjects(pkg model.PackageID) []*BusinessObject {
	out := []*BusinessObject{}
	for _, name := range r.s.BusinessObjectNames() {
		if pkg != "" && r.s.BusinessObjects[name].Owner != pkg {
			continue
		}
		if b, err := r.BusinessObject(name); err == nil {
			out = append(out, b)
		}
	}
	return out
}
