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

package pkgload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/internal/objects"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/schemas"
)

// Directories of object types and objects.
const (
	ObjectTypesDir = SchemaDir + "/object-types"
	ObjectsDir     = "objects"
)

type objectTypeDoc struct {
	Name        string                 `json:"name"`
	Description map[string]string      `json:"description"`
	Content     string                 `json:"content"`
	Behavior    model.Optional[string] `json:"behavior"`
}

type objectDoc struct {
	Name    string          `json:"name"`
	Content json.RawMessage `json:"content"`
	// typ is the type the path of the file names.
	typ string
}

// objectPath splits objects/<PACKAGE>/<TYPE>/<NAME>.json into the type
// /<PACKAGE>/<TYPE>.
func objectPath(p string) (string, bool) {
	parts := strings.Split(p, "/")
	if len(parts) != 4 || parts[0] != ObjectsDir || !strings.HasSuffix(parts[3], ".json") {
		return "", false
	}
	if !model.ValidPackageID(parts[1]) || !model.ValidLocalName(parts[2]) {
		return "", false
	}
	return "/" + parts[1] + "/" + parts[2], true
}

func init() {
	register(ObjectType{
		Kind: "object type", Document: jsonIn(ObjectTypesDir), Schema: schemas.ObjectType,
		NewDoc:      func() any { return &objectTypeDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*objectTypeDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*objectTypeDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*objectTypeDoc)
			return append(ref(d.Content, "/content"), ref(d.Behavior.Or(""), "/behavior")...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*objectTypeDoc)
			pkg.ObjectTypes = append(pkg.ObjectTypes, model.ObjectType{Name: d.Name, Owner: l.owner(), File: file,
				Description: d.Description, Content: d.Content, Behavior: d.Behavior, Src: l.loc(file)})
		},
	})
	// Objects name their type by their path. The names they refer to in
	// their content depend on the type, which the loader does not know yet;
	// CheckObjects checks them once the types are known.
	register(ObjectType{
		Kind: "object",
		Document: func(p string) bool {
			_, ok := objectPath(p)
			return ok
		},
		Schema: schemas.Object,
		NewDoc: func() any { return &objectDoc{} },
		FileName: func(doc any) (string, bool) {
			return fileNameOf(doc.(*objectDoc).Name)
		},
		Definitions: func(doc any) []NameRef { return ref(doc.(*objectDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			return ref(doc.(*objectDoc).typ, "")
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*objectDoc)
			pkg.Objects = append(pkg.Objects, model.Object{Name: d.Name, Owner: l.owner(), Type: d.typ, File: file,
				Content: d.Content, Src: l.loc(file)})
		},
	})
}

// TypeOptions control CheckObjects.
type TypeOptions struct {
	// Languages limit the texts of resolved structures; nil keeps none.
	Languages []string
}

// CheckObjects checks the objects of package id in s, the schema that
// contains the package and everything it requires: their type must exist
// and belong to the package or to a package it requires directly, the
// content must fit the content structure, and the names it refers to must
// carry the namespace of the package, of a package it requires directly or
// of the kernel. This completes the namespace check of the loader for
// objects, whose references depend on their type. On success the objects
// hold their canonical content, hash and references.
func CheckObjects(s *model.Schema, id model.PackageID) error {
	info := s.Packages[id]
	allowed := map[model.PackageID]bool{id: true, model.Kernel: true}
	if info != nil {
		for _, r := range info.Manifest.Requires {
			allowed[r.ID] = true
		}
	}
	var errs rterr.List
	componentDefaults(s)
	types := map[string]*objects.Type{}
	for _, name := range s.ObjectNames() {
		o := s.Objects[name]
		if o.Owner != id {
			continue
		}
		loc := o.Src
		ot := s.ObjectTypes[o.Type]
		if ot == nil {
			errs = append(errs, &rterr.ValidationError{Code: rterr.CodeFile, Location: loc, Object: "object " + o.Name,
				Message: fmt.Sprintf("object type %s does not exist; the folder %s names it", o.Type, path.Dir(o.File))})
			continue
		}
		if !allowed[ot.Owner] {
			errs = append(errs, &rterr.NamespaceViolation{Location: loc, Name: o.Type, Expected: namespaces(allowed), Reference: true})
			continue
		}
		t, ok := types[o.Type]
		if !ok {
			var problems []objects.Problem
			t, problems = objects.Resolve(s, ot, nil)
			if len(problems) > 0 {
				t = nil
			}
			types[o.Type] = t
		}
		if t == nil {
			errs = append(errs, &rterr.ValidationError{Code: rterr.CodeObject, Location: loc, Object: "object " + o.Name,
				Message: fmt.Sprintf("object type %s is invalid", o.Type)})
			continue
		}
		checked, problems := t.Check(o.Content)
		for _, p := range problems {
			errs = append(errs, &rterr.ValidationError{Code: rterr.CodeObject, Location: loc.At("/content" + p.Pointer), Object: "object " + o.Name, Message: p.Message})
		}
		if checked == nil {
			continue
		}
		for _, r := range checked.References {
			for _, n := range referenceNames(r) {
				if !visibleName(n, allowed) {
					errs = append(errs, &rterr.NamespaceViolation{Location: loc.At("/content" + r.Pointer), Name: n, Expected: namespaces(allowed), Reference: true})
				}
			}
		}
		o.Content, o.Hash, o.References = checked.Content, checked.Hash, checked.References
	}
	return errs.Err()
}

// referenceNames returns the names of a reference that take part in the
// namespace check: names with namespace, package IDs as namespace and the
// package segment of node paths. Fields, actions and entity sets without
// namespace belong to their anchor.
func referenceNames(r model.ObjectReference) []string {
	var out []string
	switch r.Kind {
	case model.RefPackage:
		return []string{model.Namespace(model.PackageID(r.Target))}
	case model.RefHTTPNode:
		seg, _, _ := strings.Cut(strings.TrimPrefix(r.Target, "/"), "/")
		return []string{model.Namespace(model.PackageID(strings.ToUpper(seg)))}
	case model.RefAssetRepository:
		return []string{model.Namespace(model.PackageID(r.Target))}
	}
	if isGlobal(r.Target) && r.Kind.Anchors() == nil {
		out = append(out, r.Target)
	}
	if r.Kind == model.RefObject && r.ObjectType != "" {
		out = append(out, r.ObjectType)
	}
	if isGlobal(r.Part) {
		out = append(out, r.Part)
	}
	return out
}

func visibleName(name string, allowed map[model.PackageID]bool) bool {
	pkg, _, ok := model.SplitName(name)
	if !ok {
		id := model.PackageID(strings.Trim(name, "/"))
		return allowed[id]
	}
	return allowed[pkg]
}

func namespaces(allowed map[model.PackageID]bool) []string {
	var out []string
	for id := range allowed {
		out = append(out, model.Namespace(id))
	}
	slices.Sort(out)
	return out
}

// componentDefaults converts the defaults of structure components written
// in packages into their canonical form, as the validator does later, so
// that the content of objects gets the defaults of its type. Invalid
// defaults stay unconverted; the validator reports them.
func componentDefaults(s *model.Schema) {
	convert := func(c *model.Component) {
		if len(c.DefaultJSON) == 0 || c.Default.Kind != model.DefaultNone || !c.Type.Elementary() {
			return
		}
		domain := c.Type.Name
		if c.Type.Kind == model.TypeDataElement {
			de := s.DataElements[domain]
			if de == nil {
				return
			}
			domain = de.Domain
		}
		d := s.Domains[domain]
		if d == nil {
			return
		}
		if string(bytes.TrimSpace(c.DefaultJSON)) == "null" {
			c.Default = model.NullDefault()
			return
		}
		if v, err := d.ParseJSON(c.DefaultJSON); err == nil {
			c.Default = model.ValueDefault(v)
		}
	}
	for _, st := range s.Structures {
		for _, e := range st.Elements {
			if e.Component != nil {
				convert(e.Component)
			}
		}
	}
	for _, x := range s.StructureExtensions {
		for i := range x.Components {
			convert(&x.Components[i])
		}
	}
}
