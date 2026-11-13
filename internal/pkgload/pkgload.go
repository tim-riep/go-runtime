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

// Package pkgload reads packages from a file system.
//
// A package is a directory tree:
//
//	manifest.json
//	schema/domains/*.json
//	schema/data-elements/*.json
//	schema/tables/*.json
//	schema/extensions/*.json
//	assets/<repository>/asset.json and its files
//
// Every file must belong to a registered object type (see ObjectType).
// Loading validates each document against its JSON Schema, then checks the
// namespaces of all defined and referenced names of the whole package, and
// builds objects only if every check passed.
//
// The loader works on fs.FS, so a directory (os.DirFS) and an archive
// (zip.Reader) are read the same way.
package pkgload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/schemas"
	"github.com/tim-riep/go-runtime/source"
)

// Paths inside a package.
const (
	ManifestFile           = "manifest.json"
	SchemaDir              = "schema"
	DomainsDir             = "schema/domains"
	TablesDir              = "schema/tables"
	ExtensionsDir          = "schema/extensions"
	DataElementsDir        = "schema/data-elements"
	StructuresDir          = "schema/structures"
	ArrayTypesDir          = "schema/array-types"
	StructureExtensionsDir = "schema/structure-extensions"
	SourceDir              = "src"
)

// Options control loading.
type Options struct {
	// Kernel loads the kernel's own definitions: the package ID of the
	// kernel is allowed and the owner of all objects is the kernel.
	Kernel bool
}

// Load reads and validates the package rooted at fsys. origin describes the
// source for messages.
func Load(fsys fs.FS, origin string) (*model.Package, error) {
	return LoadWith(fsys, origin, Options{})
}

// LoadWith is Load with options.
func LoadWith(fsys fs.FS, origin string, opts Options) (*model.Package, error) {
	l := &loader{fsys: fsys, opts: opts}
	pkg := l.load()
	if err := l.errs.Err(); err != nil {
		return nil, err
	}
	pkg.Origin = origin
	return pkg, nil
}

type loader struct {
	fsys fs.FS
	opts Options
	id   string
	errs rterr.List
}

func (l *loader) loc(file string) rterr.Location {
	return rterr.Location{Package: l.id, File: file}
}

func (l *loader) fail(code rterr.Code, loc rterr.Location, format string, args ...any) {
	l.errs = append(l.errs, &rterr.ValidationError{Code: code, Location: loc, Message: fmt.Sprintf(format, args...)})
}

type manifestDoc struct {
	ID          string                 `json:"id"`
	Version     string                 `json:"version"`
	Description model.Optional[string] `json:"description"`
	Requires    []struct {
		ID      string                 `json:"id"`
		Version model.Optional[string] `json:"version"`
		Source  model.Optional[string] `json:"source"`
	} `json:"requires"`
	LanguageVersion int      `json:"languageVersion"`
	KernelPackage   bool     `json:"kernelPackage"`
	Capabilities    []string `json:"capabilities"`
}

type fieldDoc struct {
	Name        string                 `json:"name"`
	Description model.Optional[string] `json:"description"`
	Domain      string                 `json:"domain"`
	DataElement model.Optional[string] `json:"dataElement"`
	NotNull     bool                   `json:"notNull"`
	Default     json.RawMessage        `json:"default"`
	Included    model.Optional[string] `json:"include"` // set for include entries
}

type fkDoc struct {
	Name   string `json:"name"`
	Table  string `json:"table"`
	Fields []struct {
		Field      string `json:"field"`
		References string `json:"references"`
	} `json:"fields"`
}

type domainDoc struct {
	Name        string                 `json:"name"`
	Description model.Optional[string] `json:"description"`
	Type        string                 `json:"type"`
	Length      int                    `json:"length"`
	Decimals    int                    `json:"decimals"`
	AllowEmpty  model.Optional[bool]   `json:"allowEmpty"`
	Default     json.RawMessage        `json:"default"`
	Values      []struct {
		Value       json.RawMessage        `json:"value"`
		Description model.Optional[string] `json:"description"`
		Name        model.Optional[string] `json:"name"`
	} `json:"values"`
}

type dataElementDoc struct {
	Name       string                           `json:"name"`
	Domain     string                           `json:"domain"`
	Texts      map[string]model.DataElementText `json:"texts"`
	ObjectType model.Optional[string]           `json:"objectType"`
}

type tableDoc struct {
	Name           string                 `json:"name"`
	Description    model.Optional[string] `json:"description"`
	TenantSpecific bool                   `json:"tenantSpecific"`
	DataClass      string                 `json:"dataClass"`
	Fields         []fieldDoc             `json:"fields"`
	PrimaryKey     []string               `json:"primaryKey"`
	ForeignKeys    []fkDoc                `json:"foreignKeys"`
}

type extensionDoc struct {
	Table       string                 `json:"table"`
	Description model.Optional[string] `json:"description"`
	Fields      []fieldDoc             `json:"fields"`
	ForeignKeys []fkDoc                `json:"foreignKeys"`
}

func (l *loader) load() *model.Package {
	pkg := &model.Package{}
	var m manifestDoc
	if !l.readDoc(ManifestFile, schemas.Manifest, &m) {
		return pkg
	}
	l.id = m.ID
	if m.ID == model.KernelPackageID && !l.opts.Kernel {
		l.fail(rterr.CodeName, l.loc(ManifestFile).At("/id"), "package ID %s is reserved for the kernel", m.ID)
	}
	if m.ID == model.LocalPackageID {
		l.fail(rterr.CodeName, l.loc(ManifestFile).At("/id"), "package ID %s is reserved for local objects", m.ID)
	}
	pkg.Manifest = model.Manifest{ID: model.PackageID(m.ID), Version: m.Version, Description: m.Description, LanguageVersion: m.LanguageVersion,
		KernelPackage: m.KernelPackage, Capabilities: m.Capabilities}
	if l.opts.Kernel {
		pkg.Manifest.ID = model.Kernel
	}
	for i, r := range m.Requires {
		dep := model.Dependency{ID: model.PackageID(r.ID), Constraint: r.Version}
		if s, ok := r.Source.Get(); ok {
			host, _, _ := strings.Cut(strings.TrimPrefix(s, "https://"), "/")
			a, err := source.Parse(s, host)
			switch {
			case err != nil:
				l.fail(rterr.CodeSchema, l.loc(ManifestFile).At(fmt.Sprintf("/requires/%d/source", i)), "%v", err)
			case a.Spec.Kind != source.Latest:
				l.fail(rterr.CodeSchema, l.loc(ManifestFile).At(fmt.Sprintf("/requires/%d/source", i)), "the source %s names a version; the version belongs in \"version\"", s)
			default:
				dep.Source = model.Some(a.Location())
			}
		}
		pkg.Manifest.Requires = append(pkg.Manifest.Requires, dep)
	}
	l.checkLayout()

	// Every file must belong to a registered object type.
	var docs []loadedDoc
	_ = fs.WalkDir(l.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			l.fail(rterr.CodeFile, l.loc(p), "cannot read: %v", err)
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			l.fail(rterr.CodeFile, l.loc(p), "symbolic links are not allowed in packages")
			return nil
		case d.IsDir() || p == ManifestFile:
			return nil
		}
		t, isDoc := typeOf(p)
		switch {
		case t == nil && strings.HasPrefix(p, ObjectsDir+"/"):
			l.fail(rterr.CodeFile, l.loc(p), "objects belong in objects/<defining package>/<type without namespace>/<name without namespace>.json")
		case t == nil && path.Dir(p) == AssetsDir:
			l.fail(rterr.CodeAsset, l.loc(p), "files directly below assets/ are not allowed; every folder below assets/ is an asset repository")
		case t == nil:
			l.fail(rterr.CodeFile, l.loc(p), "the file does not belong to any known object type")
		case isDoc && t.Parse != nil:
			if doc, ok := l.readSource(t, p); ok {
				docs = append(docs, loadedDoc{typ: t, file: p, doc: doc})
			}
		case isDoc:
			doc := t.NewDoc()
			if od, ok := doc.(*objectDoc); ok {
				od.typ, _ = objectPath(p)
			}
			if l.readDoc(p, t.Schema, doc) {
				docs = append(docs, loadedDoc{typ: t, file: p, doc: doc})
			}
		}
		return nil
	})
	l.checkLanguageVersion(m.LanguageVersion, docs)
	for i := range objectTypes {
		if c := objectTypes[i].Check; c != nil {
			c(l)
		}
	}
	for _, d := range docs {
		if want, ok := d.typ.FileName(d.doc); ok && path.Base(d.file) != want {
			l.fail(rterr.CodeFile, l.loc(d.file), "a %s file must be named %s", d.typ.Kind, want)
		}
	}
	// The namespace check runs on all documents before any object is
	// built; a package with violations is not processed further.
	l.checkNamespaces(docs, pkg.Manifest.Requires)
	if len(l.errs) > 0 {
		return pkg
	}
	for i := range objectTypes {
		for _, d := range docs {
			if d.typ == &objectTypes[i] {
				d.typ.Build(l, d.file, d.doc, pkg)
			}
		}
	}
	return pkg
}

// checkLayout rejects unexpected entries below schema/, which are usually
// misspelled directories.
func (l *loader) checkLayout() {
	entries, err := fs.ReadDir(l.fsys, SchemaDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := path.Join(SchemaDir, e.Name())
		known := []string{DomainsDir, DataElementsDir, TablesDir, ExtensionsDir,
			StructuresDir, ArrayTypesDir, StructureExtensionsDir, HTTPServicesDir, AuthorizationObjectsDir, RolesDir,
			ExtensionPointsDir, ImplementationsDir, BusinessObjectsDir, BusinessObjectExtensionsDir, ServicesDir, ObjectTypesDir}
		if !e.IsDir() || !slices.Contains(known, p) {
			l.fail(rterr.CodeFile, l.loc(p), "unexpected entry; schema/ may only contain domains/, data-elements/, tables/, extensions/, structures/, array-types/, structure-extensions/, http-services/, authorization-objects/, roles/, extension-points/, extension-implementations/, business-objects/, business-object-extensions/, services/ and object-types/")
		}
	}
}

// readDoc reads file, validates it against the named schema and decodes it
// into v. It reports problems and returns false if the file is unusable.
func (l *loader) readDoc(file, schemaName string, v any) bool {
	data, err := fs.ReadFile(l.fsys, file)
	if err != nil {
		code := rterr.CodeFile
		if file == ManifestFile && errors.Is(err, fs.ErrNotExist) {
			code = rterr.CodeManifestMissing
		}
		l.fail(code, l.loc(file), "cannot read file: %v", err)
		return false
	}
	return l.decodeDoc(file, data, schemaName, v)
}

// decodeDoc validates a document against its JSON Schema and decodes it.
func (l *loader) decodeDoc(file string, data []byte, schemaName string, v any) bool {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		l.fail(rterr.CodeSyntax, l.loc(file), "invalid JSON: %v", err)
		return false
	}
	sch, err := compiled(schemaName)
	if err != nil {
		l.fail(rterr.CodeSchema, l.loc(file), "cannot compile schema %s: %v", schemaName, err)
		return false
	}
	if err := sch.Validate(inst); err != nil {
		var verr *jsonschema.ValidationError
		if !errors.As(err, &verr) {
			l.fail(rterr.CodeSchema, l.loc(file), "%v", err)
			return false
		}
		for _, leaf := range leaves(verr) {
			l.fail(rterr.CodeSchema, l.loc(file).At(pointer(leaf.InstanceLocation)), "%s", leaf.ErrorKind.LocalizedString(printer))
		}
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		l.fail(rterr.CodeSyntax, l.loc(file), "cannot decode: %v", err)
		return false
	}
	return true
}

var printer = message.NewPrinter(language.English)

func leaves(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return []*jsonschema.ValidationError{e}
	}
	var out []*jsonschema.ValidationError
	for _, c := range e.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}

func pointer(tokens []string) string {
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		t = strings.ReplaceAll(t, "~", "~0")
		sb.WriteString(strings.ReplaceAll(t, "/", "~1"))
	}
	return sb.String()
}

var (
	compileOnce sync.Once
	compiledMap map[string]*jsonschema.Schema
	compileErr  error
)

func compiled(name string) (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		entries, err := fs.ReadDir(schemas.FS, ".")
		if err != nil {
			compileErr = err
			return
		}
		for _, e := range entries {
			data, err := fs.ReadFile(schemas.FS, e.Name())
			if err != nil {
				compileErr = err
				return
			}
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if err != nil {
				compileErr = fmt.Errorf("%s: %w", e.Name(), err)
				return
			}
			if err := c.AddResource(schemas.BaseURL+e.Name(), doc); err != nil {
				compileErr = err
				return
			}
		}
		compiledMap = map[string]*jsonschema.Schema{}
		names := []string{schemas.Manifest}
		for _, t := range objectTypes {
			if t.Schema != "" {
				names = append(names, t.Schema)
			}
		}
		for _, n := range names {
			s, err := c.Compile(schemas.BaseURL + n)
			if err != nil {
				compileErr = err
				return
			}
			compiledMap[n] = s
		}
	})
	if compileErr != nil {
		return nil, compileErr
	}
	return compiledMap[name], nil
}

func (l *loader) owner() model.PackageID {
	if l.opts.Kernel {
		return model.Kernel
	}
	return model.PackageID(l.id)
}

func (l *loader) domain(file string, doc *domainDoc) model.Domain {
	loc := l.loc(file)
	d := model.Domain{
		Name: doc.Name, Owner: l.owner(), Description: doc.Description,
		Type: model.BaseType(doc.Type), Length: doc.Length, Decimals: doc.Decimals, AllowEmpty: doc.AllowEmpty,
		DefaultJSON: doc.Default, Src: loc,
	}
	for i, v := range doc.Values {
		canon, err := d.ParseJSON(v.Value)
		if err != nil {
			l.fail(rterr.CodeFixedValue, loc.At(fmt.Sprintf("/values/%d/value", i)), "%v", err)
			continue
		}
		d.Values = append(d.Values, model.FixedValue{Value: canon, Description: v.Description, Name: v.Name})
	}
	return d
}

func (l *loader) field(doc fieldDoc, ext bool, loc rterr.Location) model.Field {
	f := model.Field{
		Name: doc.Name, LocalName: doc.Name, Description: doc.Description, Owner: l.owner(),
		Origin: model.OriginTable, NotNull: doc.NotNull, Src: loc,
	}
	f.Domain, f.DataElement = doc.Domain, doc.DataElement
	if len(doc.Default) > 0 {
		f.DefaultJSON = doc.Default
	}
	if ext {
		f.Origin = model.OriginExtension
	}
	return f
}

func (l *loader) foreignKey(doc fkDoc, ext bool, loc rterr.Location) model.ForeignKey {
	fk := model.ForeignKey{Name: doc.Name, Owner: l.owner(), Origin: model.OriginTable, Target: doc.Table, Src: loc}
	if ext {
		fk.Origin = model.OriginExtension
	}
	for _, c := range doc.Fields {
		fk.Columns = append(fk.Columns, model.ColumnPair{Field: c.Field, References: c.References})
	}
	return fk
}

func (l *loader) table(file string, doc *tableDoc) model.Table {
	loc := l.loc(file)
	t := model.Table{
		Name: doc.Name, Owner: l.owner(), Description: doc.Description,
		TenantSpecific: doc.TenantSpecific, DataClass: model.DataClass(doc.DataClass), PrimaryKey: doc.PrimaryKey, Src: loc,
	}
	for i, f := range doc.Fields {
		floc := loc.At(fmt.Sprintf("/fields/%d", i))
		if name, ok := f.Included.Get(); ok {
			t.Includes = append(t.Includes, model.TableInclude{Name: name, After: len(t.Fields), Src: floc})
			continue
		}
		t.Fields = append(t.Fields, l.field(f, false, floc))
	}
	for i, fk := range doc.ForeignKeys {
		t.ForeignKeys = append(t.ForeignKeys, l.foreignKey(fk, false, loc.At(fmt.Sprintf("/foreignKeys/%d", i))))
	}
	return t
}

func (l *loader) extension(file string, doc *extensionDoc) model.Extension {
	loc := l.loc(file)
	e := model.Extension{Package: l.owner(), Table: doc.Table, Description: doc.Description, Src: loc}
	for i, f := range doc.Fields {
		e.Fields = append(e.Fields, l.field(f, true, loc.At(fmt.Sprintf("/fields/%d", i))))
	}
	for i, fk := range doc.ForeignKeys {
		e.ForeignKeys = append(e.ForeignKeys, l.foreignKey(fk, true, loc.At(fmt.Sprintf("/foreignKeys/%d", i))))
	}
	return e
}

// TopLevel are the entries a package has at its root; the archives of
// repositories may have other files beside them, such as README.md.
var TopLevel = []string{ManifestFile, SchemaDir, SourceDir, AssetsDir, TransformationsDir, ObjectsDir}
