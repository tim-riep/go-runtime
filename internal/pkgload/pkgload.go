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

// Package pkgload reads packages from a file system and validates every file
// against its JSON Schema.
//
// A package is a directory tree:
//
//	manifest.json
//	schema/domains/*.json
//	schema/tables/*.json
//	schema/extensions/*.json
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
)

// Paths inside a package.
const (
	ManifestFile         = "manifest.json"
	SchemaDir            = "schema"
	DomainsDir           = "schema/domains"
	TablesDir            = "schema/tables"
	ExtensionsDir        = "schema/extensions"
	DataElementsDir      = "schema/data-elements"
	SettingsDir          = "schema/settings"
	SettingExtensionsDir = "schema/setting-extensions"
)

// Options control loading.
type Options struct {
	// Kernel loads the kernel's own definitions: all table and domain names
	// and references receive the kernel namespace and the owner is the
	// kernel.
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
	ID          string `json:"id"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Requires    []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"requires"`
}

type fieldDoc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Domain      string          `json:"domain"`
	DataElement string          `json:"dataElement"`
	NotNull     bool            `json:"notNull"`
	Default     json.RawMessage `json:"default"`
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
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Length      int    `json:"length"`
	Decimals    int    `json:"decimals"`
	Values      []struct {
		Value       json.RawMessage `json:"value"`
		Description string          `json:"description"`
	} `json:"values"`
}

type dataElementDoc struct {
	Name   string                           `json:"name"`
	Domain string                           `json:"domain"`
	Texts  map[string]model.DataElementText `json:"texts"`
}

type tableDoc struct {
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	TenantSpecific bool       `json:"tenantSpecific"`
	DataClass      string     `json:"dataClass"`
	Fields         []fieldDoc `json:"fields"`
	PrimaryKey     []string   `json:"primaryKey"`
	ForeignKeys    []fkDoc    `json:"foreignKeys"`
}

type extensionDoc struct {
	Table       string     `json:"table"`
	Description string     `json:"description"`
	Fields      []fieldDoc `json:"fields"`
	ForeignKeys []fkDoc    `json:"foreignKeys"`
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
	pkg.Manifest = model.Manifest{ID: model.PackageID(m.ID), Version: m.Version, Description: m.Description}
	if l.opts.Kernel {
		pkg.Manifest.ID = model.Kernel
	}
	for _, r := range m.Requires {
		pkg.Manifest.Requires = append(pkg.Manifest.Requires, model.Dependency{ID: model.PackageID(r.ID), Constraint: r.Version})
	}
	l.checkLayout()

	for _, file := range l.files(DomainsDir) {
		var d domainDoc
		if l.readDoc(file, schemas.Domain, &d) {
			pkg.Domains = append(pkg.Domains, l.domain(file, &d))
		}
	}
	for _, file := range l.files(DataElementsDir) {
		var d dataElementDoc
		if l.readDoc(file, schemas.DataElement, &d) {
			l.checkFileName(file, d.Name, l.loc(file))
			pkg.DataElements = append(pkg.DataElements, model.DataElement{Name: d.Name, Owner: l.owner(), Domain: l.ref(d.Domain), Texts: d.Texts, Src: l.loc(file)})
		}
	}
	for _, file := range l.files(TablesDir) {
		var t tableDoc
		if l.readDoc(file, schemas.Table, &t) {
			pkg.Tables = append(pkg.Tables, l.table(file, &t))
		}
	}
	for _, file := range l.files(ExtensionsDir) {
		var e extensionDoc
		if l.readDoc(file, schemas.Extension, &e) {
			pkg.Extensions = append(pkg.Extensions, l.extension(file, &e))
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
		known := []string{DomainsDir, DataElementsDir, TablesDir, ExtensionsDir, SettingsDir, SettingExtensionsDir}
		if !e.IsDir() || !slices.Contains(known, p) {
			l.fail(rterr.CodeFile, l.loc(p), "unexpected entry; schema/ may only contain domains/, data-elements/, tables/, extensions/, settings/ and setting-extensions/")
		}
	}
}

func (l *loader) files(dir string) []string {
	entries, err := fs.ReadDir(l.fsys, dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.fail(rterr.CodeFile, l.loc(dir), "cannot read directory: %v", err)
		}
		return nil
	}
	var out []string
	for _, e := range entries {
		p := path.Join(dir, e.Name())
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			l.fail(rterr.CodeFile, l.loc(p), "unexpected entry; only *.json files are allowed here")
			continue
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return out
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
		for _, n := range []string{schemas.Manifest, schemas.Domain, schemas.DataElement, schemas.Table, schemas.Extension} {
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

// name qualifies a declared table or domain name in kernel mode.
func (l *loader) name(n string) string {
	if l.opts.Kernel {
		return model.Qualify(model.KernelPackageID, n)
	}
	return n
}

// ref qualifies a reference to a table or domain in kernel mode.
func (l *loader) ref(n string) string {
	if l.opts.Kernel && !strings.HasPrefix(n, "/") {
		return model.Qualify(model.KernelPackageID, n)
	}
	return n
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
		Name: l.name(doc.Name), Owner: l.owner(), Description: doc.Description,
		Type: model.BaseType(doc.Type), Length: doc.Length, Decimals: doc.Decimals, Src: loc,
	}
	l.checkFileName(file, doc.Name, loc)
	for i, v := range doc.Values {
		canon, err := d.ParseJSON(v.Value)
		if err != nil {
			l.fail(rterr.CodeFixedValue, loc.At(fmt.Sprintf("/values/%d/value", i)), "%v", err)
			continue
		}
		d.Values = append(d.Values, model.FixedValue{Value: canon, Description: v.Description})
	}
	return d
}

func (l *loader) checkFileName(file, name string, loc rterr.Location) {
	if want := name + ".json"; path.Base(file) != want {
		l.fail(rterr.CodeFile, loc.At("/name"), "file must be named %s", want)
	}
}

func (l *loader) field(doc fieldDoc, ext bool, loc rterr.Location) model.Field {
	f := model.Field{
		Name: doc.Name, LocalName: doc.Name, Description: doc.Description, Owner: l.owner(),
		Origin: model.OriginTable, NotNull: doc.NotNull, Src: loc,
	}
	if doc.Domain != "" {
		f.Domain = l.ref(doc.Domain)
	}
	f.DataElement = doc.DataElement
	if len(doc.Default) > 0 {
		f.DefaultJSON = doc.Default
	}
	if ext {
		f.Name = model.Qualify(model.PackageID(l.id), doc.Name)
		f.Origin = model.OriginExtension
	}
	return f
}

func (l *loader) foreignKey(doc fkDoc, ext bool, loc rterr.Location) model.ForeignKey {
	fk := model.ForeignKey{Name: doc.Name, Owner: l.owner(), Origin: model.OriginTable, Target: l.ref(doc.Table), Src: loc}
	if ext {
		fk.Name = model.Qualify(model.PackageID(l.id), doc.Name)
		fk.Origin = model.OriginExtension
	}
	for _, c := range doc.Fields {
		field := c.Field
		if ext {
			field = model.Qualify(model.PackageID(l.id), field)
		}
		fk.Columns = append(fk.Columns, model.ColumnPair{Field: field, References: c.References})
	}
	return fk
}

func (l *loader) table(file string, doc *tableDoc) model.Table {
	loc := l.loc(file)
	t := model.Table{
		Name: l.name(doc.Name), Owner: l.owner(), Description: doc.Description,
		TenantSpecific: doc.TenantSpecific, DataClass: model.DataClass(doc.DataClass), PrimaryKey: doc.PrimaryKey, Src: loc,
	}
	l.checkFileName(file, doc.Name, loc)
	for i, f := range doc.Fields {
		t.Fields = append(t.Fields, l.field(f, false, loc.At(fmt.Sprintf("/fields/%d", i))))
	}
	for i, fk := range doc.ForeignKeys {
		t.ForeignKeys = append(t.ForeignKeys, l.foreignKey(fk, false, loc.At(fmt.Sprintf("/foreignKeys/%d", i))))
	}
	return t
}

func (l *loader) extension(file string, doc *extensionDoc) model.Extension {
	loc := l.loc(file)
	e := model.Extension{Package: l.owner(), Table: l.ref(doc.Table), Description: doc.Description, Src: loc}
	for i, f := range doc.Fields {
		e.Fields = append(e.Fields, l.field(f, true, loc.At(fmt.Sprintf("/fields/%d", i))))
	}
	for i, fk := range doc.ForeignKeys {
		e.ForeignKeys = append(e.ForeignKeys, l.foreignKey(fk, true, loc.At(fmt.Sprintf("/foreignKeys/%d", i))))
	}
	return e
}
