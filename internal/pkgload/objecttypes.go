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
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// NameRef is a name found in a document with its JSON Pointer.
type NameRef struct {
	Name    string
	Pointer string
}

// ObjectType describes how the loader reads the files of one kind of
// object. Every kind of object the kernel knows registers here; the loader
// processes only files of registered types and rejects all others, so no
// object reaches the kernel without the namespace check.
type ObjectType struct {
	// Kind names the type in messages, e.g. "table".
	Kind string
	// Document reports whether a path in the package is a document of
	// this type. Documents are validated against Schema and decoded.
	Document func(p string) bool
	// Content reports whether a path is a plain file that belongs to an
	// object of this type, such as an asset. Nil means the type has none.
	Content func(p string) bool
	// Schema is the JSON Schema file of the documents.
	Schema string
	// NewDoc returns the value a document is decoded into.
	NewDoc func() any
	// FileName returns the file name a document must have, if any.
	FileName func(doc any) (string, bool)
	// Definitions returns the names of the objects a document defines.
	Definitions func(doc any) []NameRef
	// References returns the names of the objects a document refers to.
	References func(doc any) []NameRef
	// Check runs structural checks of the type on the whole package before
	// anything is built. Nil means none.
	Check func(l *loader)
	// Build adds the objects of a document to the package. It runs only if
	// the whole package passed all checks.
	Build func(l *loader, file string, doc any, pkg *model.Package)
}

var objectTypes []ObjectType

// register adds an object type. It panics if the type cannot take part in
// the namespace check.
func register(t ObjectType) {
	if t.Kind == "" || t.Document == nil || t.Schema == "" || t.NewDoc == nil || t.Definitions == nil || t.References == nil || t.Build == nil {
		panic(fmt.Sprintf("pkgload: object type %q is incomplete; Definitions and References are required for the namespace check", t.Kind))
	}
	objectTypes = append(objectTypes, t)
}

// ObjectTypes returns the registered object types.
func ObjectTypes() []ObjectType { return slices.Clone(objectTypes) }

// typeOf returns the type a path belongs to and whether it is a document.
func typeOf(p string) (*ObjectType, bool) {
	for i := range objectTypes {
		t := &objectTypes[i]
		if t.Document(p) {
			return t, true
		}
		if t.Content != nil && t.Content(p) {
			return t, false
		}
	}
	return nil, false
}

// jsonIn returns a matcher for *.json files directly in dir.
func jsonIn(dir string) func(string) bool {
	return func(p string) bool { return path.Dir(p) == dir && strings.HasSuffix(p, ".json") }
}

// fileNameOf requires a file named after the part of the name after the
// namespace.
func fileNameOf(name string) (string, bool) {
	if _, local, ok := model.SplitName(name); ok {
		return local + ".json", true
	}
	return name + ".json", true
}

func ref(name, pointer string) []NameRef {
	if name == "" {
		return nil
	}
	return []NameRef{{Name: name, Pointer: pointer}}
}

// checkNamespaces checks every name of every document: definitions must
// carry the namespace of the package, references that of the package, of a
// package it requires directly, or of the kernel. All violations are
// reported together.
func (l *loader) checkNamespaces(docs []loadedDoc, requires []model.Dependency) {
	own := model.Namespace(model.PackageID(l.id))
	allowed := []string{own}
	for _, r := range requires {
		allowed = append(allowed, model.Namespace(r.ID))
	}
	if !slices.Contains(allowed, model.KernelNamespace) {
		allowed = append(allowed, model.KernelNamespace)
	}
	for _, d := range docs {
		loc := l.loc(d.file)
		for _, n := range d.typ.Definitions(d.doc) {
			if !strings.HasPrefix(n.Name, own) {
				l.errs = append(l.errs, &rterr.NamespaceViolation{Location: loc.At(n.Pointer), Name: n.Name, Expected: []string{own}})
			}
		}
		for _, n := range d.typ.References(d.doc) {
			if !slices.ContainsFunc(allowed, func(ns string) bool { return strings.HasPrefix(n.Name, ns) }) {
				l.errs = append(l.errs, &rterr.NamespaceViolation{Location: loc.At(n.Pointer), Name: n.Name, Expected: allowed, Reference: true})
			}
		}
	}
}

type loadedDoc struct {
	typ  *ObjectType
	file string
	doc  any
}

func init() {
	register(ObjectType{
		Kind: "domain", Document: jsonIn(DomainsDir), Schema: "domain.schema.json",
		NewDoc:      func() any { return &domainDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*domainDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*domainDoc).Name, "/name") },
		References:  func(any) []NameRef { return nil },
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			pkg.Domains = append(pkg.Domains, l.domain(file, doc.(*domainDoc)))
		},
	})
	register(ObjectType{
		Kind: "data element", Document: jsonIn(DataElementsDir), Schema: "data-element.schema.json",
		NewDoc:      func() any { return &dataElementDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*dataElementDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*dataElementDoc).Name, "/name") },
		References:  func(doc any) []NameRef { return ref(doc.(*dataElementDoc).Domain, "/domain") },
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*dataElementDoc)
			pkg.DataElements = append(pkg.DataElements, model.DataElement{Name: d.Name, Owner: l.owner(), Domain: d.Domain, Texts: d.Texts, Src: l.loc(file)})
		},
	})
	register(ObjectType{
		Kind: "table", Document: jsonIn(TablesDir), Schema: "table.schema.json",
		NewDoc:      func() any { return &tableDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*tableDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*tableDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			t := doc.(*tableDoc)
			return append(fieldRefs(t.Fields), fkRefs(t.ForeignKeys, false)...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			pkg.Tables = append(pkg.Tables, l.table(file, doc.(*tableDoc)))
		},
	})
	register(ObjectType{
		Kind: "extension", Document: jsonIn(ExtensionsDir), Schema: "extension.schema.json",
		NewDoc:   func() any { return &extensionDoc{} },
		FileName: func(any) (string, bool) { return "", false },
		Definitions: func(doc any) []NameRef {
			e := doc.(*extensionDoc)
			var out []NameRef
			for i, f := range e.Fields {
				out = append(out, ref(f.Name, fmt.Sprintf("/fields/%d/name", i))...)
			}
			for i, fk := range e.ForeignKeys {
				out = append(out, ref(fk.Name, fmt.Sprintf("/foreignKeys/%d/name", i))...)
			}
			return out
		},
		References: func(doc any) []NameRef {
			e := doc.(*extensionDoc)
			out := ref(e.Table, "/table")
			out = append(out, fieldRefs(e.Fields)...)
			return append(out, fkRefs(e.ForeignKeys, true)...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			pkg.Extensions = append(pkg.Extensions, l.extension(file, doc.(*extensionDoc)))
		},
	})
	register(ObjectType{
		Kind: "setting", Document: jsonIn(SettingsDir), Schema: "setting.schema.json",
		NewDoc:      func() any { return &settingDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*settingDoc).ID) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*settingDoc).ID, "/id") },
		References: func(doc any) []NameRef {
			s := doc.(*settingDoc)
			return append(ref(s.Root.Table, "/root/table"), nodeRefs(s.Tables)...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*settingDoc)
			loc := l.loc(file)
			st := model.Setting{ID: d.ID, Owner: l.owner(), Texts: d.Texts, Root: settingTable(d.Root, loc.At("/root")), Src: loc}
			for i, t := range d.Tables {
				st.Tables = append(st.Tables, settingTable(t, loc.At(fmt.Sprintf("/tables/%d", i))))
			}
			pkg.Settings = append(pkg.Settings, st)
		},
	})
	register(ObjectType{
		Kind: "setting extension", Document: jsonIn(SettingExtensionsDir), Schema: "setting-extension.schema.json",
		NewDoc:      func() any { return &settingExtensionDoc{} },
		FileName:    func(any) (string, bool) { return "", false },
		Definitions: func(any) []NameRef { return nil },
		References: func(doc any) []NameRef {
			s := doc.(*settingExtensionDoc)
			return append(ref(s.Setting, "/setting"), nodeRefs(s.Tables)...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*settingExtensionDoc)
			loc := l.loc(file)
			e := model.SettingExtension{Package: l.owner(), Setting: d.Setting, Src: loc}
			for i, t := range d.Tables {
				e.Tables = append(e.Tables, settingTable(t, loc.At(fmt.Sprintf("/tables/%d", i))))
			}
			pkg.SettingExtensions = append(pkg.SettingExtensions, e)
		},
	})
	register(ObjectType{
		Kind: "structure", Document: jsonIn(StructuresDir), Schema: "structure.schema.json",
		NewDoc:      func() any { return &structureDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*structureDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*structureDoc).Name, "/name") },
		References:  func(doc any) []NameRef { return componentRefs(doc.(*structureDoc).Components, "/components") },
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*structureDoc)
			loc := l.loc(file)
			st := model.Structure{Name: d.Name, Owner: l.owner(), Texts: d.Texts, Src: loc}
			for i, c := range d.Components {
				cloc := loc.At(fmt.Sprintf("/components/%d", i))
				if inc, ok := c.Included.Get(); ok {
					st.Elements = append(st.Elements, model.Element{Included: model.Some(inc), Src: cloc})
					continue
				}
				comp := c.component(l.owner(), model.ComponentStructure, cloc)
				st.Elements = append(st.Elements, model.Element{Component: &comp, Src: cloc})
			}
			pkg.Structures = append(pkg.Structures, st)
		},
	})
	register(ObjectType{
		Kind: "array type", Document: jsonIn(ArrayTypesDir), Schema: "array-type.schema.json",
		NewDoc:      func() any { return &arrayTypeDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*arrayTypeDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*arrayTypeDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			t := doc.(*arrayTypeDoc).RowType.typeRef()
			return ref(t.Name, "/rowType/"+string(t.Kind))
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*arrayTypeDoc)
			a := model.ArrayType{Name: d.Name, Owner: l.owner(), Texts: d.Texts, Row: d.RowType.typeRef(), Key: d.Key, Src: l.loc(file)}
			pkg.ArrayTypes = append(pkg.ArrayTypes, a)
		},
	})
	register(ObjectType{
		Kind: "structure extension", Document: jsonIn(StructureExtensionsDir), Schema: "structure-extension.schema.json",
		NewDoc:   func() any { return &structureExtensionDoc{} },
		FileName: func(any) (string, bool) { return "", false },
		Definitions: func(doc any) []NameRef {
			var out []NameRef
			for i, c := range doc.(*structureExtensionDoc).Components {
				out = append(out, ref(c.Name, fmt.Sprintf("/components/%d/name", i))...)
			}
			return out
		},
		References: func(doc any) []NameRef {
			d := doc.(*structureExtensionDoc)
			return append(ref(d.Structure, "/structure"), componentRefs(d.Components, "/components")...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*structureExtensionDoc)
			loc := l.loc(file)
			e := model.StructureExtension{Package: l.owner(), Structure: d.Structure, Src: loc}
			for i, c := range d.Components {
				e.Components = append(e.Components, c.component(l.owner(), model.ComponentExtension, loc.At(fmt.Sprintf("/components/%d", i))))
			}
			pkg.StructureExtensions = append(pkg.StructureExtensions, e)
		},
	})
	// Asset repositories are identified by package and folder name; they
	// define and reference no names, which the empty lists state
	// explicitly.
	register(ObjectType{
		Kind: "asset repository", Schema: "asset.schema.json",
		Document: func(p string) bool {
			parts := strings.Split(p, "/")
			return len(parts) == 3 && parts[0] == AssetsDir && parts[2] == AssetDescriptor
		},
		Content: func(p string) bool {
			parts := strings.Split(p, "/")
			return len(parts) >= 3 && parts[0] == AssetsDir
		},
		NewDoc:      func() any { return &assetDoc{} },
		FileName:    func(any) (string, bool) { return "", false },
		Definitions: func(any) []NameRef { return nil },
		References:  func(any) []NameRef { return nil },
		Check:       checkAssetLayout,
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			dir := path.Dir(file)
			if r := l.assetRepository(path.Base(dir), dir, doc.(*assetDoc)); r != nil {
				pkg.AssetRepositories = append(pkg.AssetRepositories, *r)
			}
		},
	})
}

func fieldRefs(fs []fieldDoc) []NameRef {
	var out []NameRef
	for i, f := range fs {
		if inc, ok := f.Included.Get(); ok {
			out = append(out, ref(inc, fmt.Sprintf("/fields/%d/include", i))...)
		}
		out = append(out, ref(f.Domain, fmt.Sprintf("/fields/%d/domain", i))...)
		if de, ok := f.DataElement.Get(); ok {
			out = append(out, ref(de, fmt.Sprintf("/fields/%d/dataElement", i))...)
		}
	}
	return out
}

// fkRefs lists the target tables of foreign keys and, for extension keys,
// the extension fields they use.
func fkRefs(fks []fkDoc, ext bool) []NameRef {
	var out []NameRef
	for i, fk := range fks {
		out = append(out, ref(fk.Table, fmt.Sprintf("/foreignKeys/%d/table", i))...)
		if ext {
			for j, c := range fk.Fields {
				out = append(out, ref(c.Field, fmt.Sprintf("/foreignKeys/%d/fields/%d/field", i, j))...)
			}
		}
	}
	return out
}

func nodeRefs(ts []settingTableDoc) []NameRef {
	var out []NameRef
	for i, t := range ts {
		out = append(out, ref(t.Table, fmt.Sprintf("/tables/%d/table", i))...)
		if parent, ok := t.Parent.Get(); ok {
			out = append(out, ref(parent, fmt.Sprintf("/tables/%d/parent", i))...)
		}
	}
	return out
}

func settingTable(d settingTableDoc, loc rterr.Location) model.SettingTable {
	return model.SettingTable{Table: d.Table, Parent: d.Parent, ForeignKey: d.ForeignKey, Operations: d.Operations, Src: loc}
}

// typeDoc holds the type keys of a component or row type; exactly one is set.
type typeDoc struct {
	DataElement model.Optional[string] `json:"dataElement"`
	Domain      model.Optional[string] `json:"domain"`
	Structure   model.Optional[string] `json:"structure"`
	ArrayType   model.Optional[string] `json:"arrayType"`
}

func (t typeDoc) typeRef() model.TypeRef {
	for _, c := range []struct {
		o    model.Optional[string]
		kind model.TypeKind
	}{{t.DataElement, model.TypeDataElement}, {t.Domain, model.TypeDomain}, {t.Structure, model.TypeStructure}, {t.ArrayType, model.TypeArray}} {
		if n, ok := c.o.Get(); ok {
			return model.TypeRef{Kind: c.kind, Name: n}
		}
	}
	return model.TypeRef{}
}

type componentDoc struct {
	typeDoc
	Name        string                 `json:"name"`
	Description model.Optional[string] `json:"description"`
	NotNull     bool                   `json:"notNull"`
	Default     json.RawMessage        `json:"default"`
	Included    model.Optional[string] `json:"include"`
}

func (c componentDoc) component(owner model.PackageID, origin model.ComponentOrigin, loc rterr.Location) model.Component {
	return model.Component{Name: c.Name, Owner: owner, Origin: origin, Description: c.Description, Type: c.typeRef(),
		NotNull: c.NotNull, DefaultJSON: c.Default, Src: loc}
}

type structureDoc struct {
	Name       string                 `json:"name"`
	Texts      map[string]model.Label `json:"texts"`
	Components []componentDoc         `json:"components"`
}

type arrayTypeDoc struct {
	Name    string                 `json:"name"`
	Texts   map[string]model.Label `json:"texts"`
	RowType typeDoc                `json:"rowType"`
	Key     *model.ArrayKey        `json:"key"`
}

type structureExtensionDoc struct {
	Structure  string         `json:"structure"`
	Components []componentDoc `json:"components"`
}

// componentRefs lists the types and includes of components.
func componentRefs(cs []componentDoc, base string) []NameRef {
	var out []NameRef
	for i, c := range cs {
		p := fmt.Sprintf("%s/%d", base, i)
		if inc, ok := c.Included.Get(); ok {
			out = append(out, ref(inc, p+"/include")...)
			continue
		}
		t := c.typeRef()
		out = append(out, ref(t.Name, p+"/"+string(t.Kind))...)
	}
	return out
}
