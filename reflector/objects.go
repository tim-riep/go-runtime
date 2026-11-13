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
	"encoding/json"
	"strings"

	"github.com/tim-riep/go-runtime/internal/objects"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/structure"
	"github.com/tim-riep/go-runtime/structure/jsonvalue"
)

// ObjectType describes a type of objects that a package defines.
type ObjectType struct {
	FormatVersion int                    `json:"formatVersion"`
	Name          string                 `json:"name"`
	Package       model.PackageID        `json:"package"`
	Description   map[string]string      `json:"description"`
	Content       string                 `json:"content"`
	Behavior      model.Optional[string] `json:"behavior"`
	// Definition is the resolved content structure.
	Definition *structure.Definition `json:"definition"`
	// Objects is the number of installed objects of the type.
	Objects int `json:"objects"`
}

// Object describes an object of a package-defined type with its content.
type Object struct {
	FormatVersion int                     `json:"formatVersion"`
	Name          string                  `json:"name"`
	Type          string                  `json:"type"`
	Package       model.PackageID         `json:"package"`
	File          string                  `json:"file"`
	Hash          string                  `json:"hash"`
	Content       json.RawMessage         `json:"content"`
	References    []model.ObjectReference `json:"references"`
}

// ObjectUse is a reference in the content of an object to another object.
type ObjectUse struct {
	Object    string                `json:"object"`
	Type      string                `json:"type"`
	Package   model.PackageID       `json:"package"`
	Reference model.ObjectReference `json:"reference"`
}

// ObjectTypes describes the object types of a package, or of all for "",
// ordered by name.
func (r *Reflector) ObjectTypes(pkg model.PackageID) []*ObjectType {
	out := []*ObjectType{}
	for _, name := range r.s.ObjectTypeNames() {
		if t := r.s.ObjectTypes[name]; pkg == "" || t.Owner == pkg {
			d, _ := r.ObjectType(name)
			out = append(out, d)
		}
	}
	return out
}

// ObjectType describes an object type.
func (r *Reflector) ObjectType(name string) (*ObjectType, error) {
	t := r.s.ObjectTypes[name]
	if t == nil {
		return nil, &rterr.NotFound{Kind: "object type", Name: name}
	}
	d := &ObjectType{FormatVersion: FormatVersion, Name: t.Name, Package: t.Owner, Description: r.texts(t.Description), Content: t.Content,
		Behavior: t.Behavior, Objects: len(r.s.ObjectsOf(name))}
	if rt, problems := objects.Resolve(r.s, t, r.langs); len(problems) == 0 {
		d.Definition = rt.Def
	}
	return d, nil
}

// ObjectTypeSchema returns a JSON Schema of the object files of a type for
// editors, with descriptions in language.
func (r *Reflector) ObjectTypeSchema(name, language string) (map[string]any, error) {
	d, err := r.ObjectType(name)
	if err != nil {
		return nil, err
	}
	if d.Definition == nil {
		return nil, &rterr.NotFound{Kind: "valid object type", Name: name}
	}
	content := jsonvalue.Schema(d.Definition, language)
	defs := content["$defs"]
	delete(content, "$defs")
	delete(content, "$schema")
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"title":                "Object of type " + name,
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "content"},
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "pattern": `^/[A-Z][A-Z0-9]*/[A-Z][A-Z0-9_]*$`},
			"content": content,
		},
		"$defs": defs,
	}, nil
}

// Objects describes the objects of a type, of a package or both; "" means
// all. They come in the dependency order of their packages, then by name.
func (r *Reflector) Objects(typeName string, pkg model.PackageID) []*Object {
	out := []*Object{}
	var list []*model.Object
	if typeName != "" {
		list = r.s.ObjectsOf(typeName)
	} else {
		for _, name := range r.s.ObjectNames() {
			list = append(list, r.s.Objects[name])
		}
	}
	for _, o := range list {
		if pkg == "" || o.Owner == pkg {
			out = append(out, object(o))
		}
	}
	return out
}

// Object describes an object.
func (r *Reflector) Object(name string) (*Object, error) {
	o := r.s.Objects[name]
	if o == nil {
		return nil, &rterr.NotFound{Kind: "object", Name: name}
	}
	return object(o), nil
}

func object(o *model.Object) *Object {
	refs := o.References
	if refs == nil {
		refs = []model.ObjectReference{}
	}
	return &Object{FormatVersion: FormatVersion, Name: o.Name, Type: o.Type, Package: o.Owner, File: o.File, Hash: o.Hash,
		Content: o.Content, References: refs}
}

// ObjectUses returns the references in the content of objects to name: a
// package, a business object, an entity and its fields, an action, a
// service and its entity sets, a role, a node, an object, or an object type
// whose objects typed references name.
func (r *Reflector) ObjectUses(name string) []ObjectUse {
	out := []ObjectUse{}
	for _, on := range r.s.ObjectNames() {
		o := r.s.Objects[on]
		for _, ref := range o.References {
			if ref.Target == name || ref.Part == name && strings.HasPrefix(name, "/") || ref.ObjectType == name {
				out = append(out, ObjectUse{Object: o.Name, Type: o.Type, Package: o.Owner, Reference: ref})
			}
		}
	}
	return out
}
