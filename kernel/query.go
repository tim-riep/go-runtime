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

package kernel

import (
	"context"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/structure"
)

// LogEntry is one entry of the operation log.
type LogEntry = repo.LogEntry

// Log returns the operation log, newest entry first.
func (k *Kernel) Log(ctx context.Context) ([]*LogEntry, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.repo.ReadLog(ctx, k.db)
}

// Packages returns the installed packages ordered by ID.
func (k *Kernel) Packages(ctx context.Context) ([]*model.PackageInfo, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.PackageInfo, 0, len(s.Packages))
	for _, id := range s.PackageIDs() {
		out = append(out, s.Packages[id])
	}
	return out, nil
}

// PackageDetail describes an installed package and its objects.
type PackageDetail struct {
	Package    *model.PackageInfo `json:"package"`
	RequiredBy []model.PackageID  `json:"requiredBy,omitempty"`
	Domains    []*model.Domain    `json:"domains,omitempty"`
	Tables     []string           `json:"tables,omitempty"`
	Extensions []*model.Extension `json:"extensions,omitempty"`
}

// Package returns an installed package with its objects.
func (k *Kernel) Package(ctx context.Context, id model.PackageID) (*PackageDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	info, ok := s.Packages[id]
	if !ok {
		return nil, &rterr.NotInstalled{Package: string(id)}
	}
	d := &PackageDetail{Package: info, RequiredBy: s.Dependents(id)}
	for _, n := range s.DomainNames() {
		if s.Domains[n].Owner == id {
			d.Domains = append(d.Domains, s.Domains[n])
		}
	}
	for _, n := range s.TableNames() {
		if s.Tables[n].Owner == id {
			d.Tables = append(d.Tables, n)
		}
	}
	for _, e := range s.SortedExtensions() {
		if e.Package == id {
			d.Extensions = append(d.Extensions, e)
		}
	}
	return d, nil
}

// TableDetail describes a table with all its physical fields.
type TableDetail struct {
	Table        *model.Table           `json:"table"` // physical layout incl. tenant and extension fields
	Extensions   []*model.Extension     `json:"extensions,omitempty"`
	Includes     []model.TableInclude   `json:"includes,omitempty"`
	UsedBy       []model.Use            `json:"usedBy,omitempty"`       // structures, tables and array types using the table
	ReferencedBy []string               `json:"referencedBy,omitempty"` // "TABLE.FOREIGN_KEY"
	Setting      model.Optional[string] `json:"setting"`                // setting the table belongs to
	CodeUses     []CodeUse              `json:"codeUses"`
}

// Table returns the physical layout of a table.
func (k *Kernel) Table(ctx context.Context, name string) (*TableDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	p := s.Physical()
	t, ok := p.Tables[name]
	if !ok {
		return nil, &rterr.NotFound{Kind: "table", Name: name}
	}
	d := &TableDetail{Table: t, Extensions: s.ExtensionsOf(name), Includes: s.Tables[name].Includes, UsedBy: s.UsesOf(name), Setting: settingOf(s, name)}
	for _, other := range p.TableNames() {
		for _, fk := range p.Tables[other].ForeignKeys {
			if fk.Target == name {
				d.ReferencedBy = append(d.ReferencedBy, other+"."+fk.Name)
			}
		}
	}
	if d.CodeUses, err = k.CodeUses(ctx, name); err != nil {
		return nil, err
	}
	return d, nil
}

// DomainDetail describes a domain and where it is used.
type DomainDetail struct {
	Domain   *model.Domain `json:"domain"`
	UsedBy   []string      `json:"usedBy,omitempty"` // "TABLE.FIELD" or "AUTHORIZATION_OBJECT.FIELD"
	CodeUses []CodeUse     `json:"codeUses"`
}

// Domain returns a domain and the fields that use it.
func (k *Kernel) Domain(ctx context.Context, name string) (*DomainDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	dom, ok := s.Domains[name]
	if !ok {
		return nil, &rterr.NotFound{Kind: "domain", Name: name}
	}
	d := &DomainDetail{Domain: dom}
	p := s.Physical()
	for _, tn := range p.TableNames() {
		for _, f := range p.Tables[tn].Fields {
			if f.Domain == name {
				d.UsedBy = append(d.UsedBy, tn+"."+f.Name)
			}
		}
	}
	d.UsedBy = append(d.UsedBy, authorizationUses(s, func(f model.AuthorizationField) bool { return f.Domain == model.Some(name) })...)
	if d.CodeUses, err = k.CodeUses(ctx, name); err != nil {
		return nil, err
	}
	return d, nil
}

// authorizationUses returns the fields of authorization objects that use
// a data element or domain, as "OBJECT.FIELD".
func authorizationUses(s *model.Schema, uses func(f model.AuthorizationField) bool) []string {
	var out []string
	for _, name := range s.AuthorizationObjectNames() {
		for _, f := range s.AuthorizationObjects[name].Fields {
			if uses(f) {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	return out
}

// DataElementDetail describes a data element and where it is used.
type DataElementDetail struct {
	DataElement *model.DataElement `json:"dataElement"`
	Domain      *model.Domain      `json:"domain"`
	UsedBy      []string           `json:"usedBy,omitempty"` // "TABLE.FIELD" or "AUTHORIZATION_OBJECT.FIELD"
	CodeUses    []CodeUse          `json:"codeUses"`
}

// DataElement returns a data element with its domain and the fields that
// use it.
func (k *Kernel) DataElement(ctx context.Context, name string) (*DataElementDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	de, ok := s.DataElements[name]
	if !ok {
		return nil, &rterr.NotFound{Kind: "data element", Name: name}
	}
	d := &DataElementDetail{DataElement: de, Domain: s.Domains[de.Domain]}
	p := s.Physical()
	for _, tn := range p.TableNames() {
		for _, f := range p.Tables[tn].Fields {
			if f.DataElement == model.Some(name) {
				d.UsedBy = append(d.UsedBy, tn+"."+f.Name)
			}
		}
	}
	d.UsedBy = append(d.UsedBy, authorizationUses(s, func(f model.AuthorizationField) bool {
		return f.DataElement == model.Some(name) || name == model.ActivityDomain && f.IsActivity() && !f.DataElement.Valid() && !f.Domain.Valid()
	})...)
	if d.CodeUses, err = k.CodeUses(ctx, name); err != nil {
		return nil, err
	}
	return d, nil
}

// Object kinds understood by Resolve.
const (
	KindPackage     = "package"
	KindTable       = "table"
	KindDomain      = "domain"
	KindDataElement = "data-element"
	KindStructure   = "structure"
	KindArrayType   = "array-type"
	KindClass       = "class" // a class or interface in code

	KindAuthorizationObject = "authorization-object"
)

// Resolve determines which kind of object a name refers to. A prefix
// "package:", "table:", "domain:", "data-element:", "structure:",
// "array-type:", "class:" or "authorization-object:" selects the kind
// explicitly; a bare name
// that matches several kinds is reported as rterr.Ambiguous.
func (k *Kernel) Resolve(ctx context.Context, ref string) (kind, name string, err error) {
	if kd, n, ok := strings.Cut(ref, ":"); ok && slices.Contains([]string{KindPackage, KindTable, KindDomain, KindDataElement, KindStructure, KindArrayType, KindClass, KindAuthorizationObject}, kd) {
		return kd, n, nil
	}
	s, err := k.Schema(ctx)
	if err != nil {
		return "", "", err
	}
	var kinds []string
	if _, ok := s.Packages[model.PackageID(ref)]; ok {
		kinds = append(kinds, KindPackage)
	}
	if _, ok := s.Tables[ref]; ok {
		kinds = append(kinds, KindTable)
	}
	if _, ok := s.Domains[ref]; ok {
		kinds = append(kinds, KindDomain)
	}
	if _, ok := s.DataElements[ref]; ok {
		kinds = append(kinds, KindDataElement)
	}
	if _, ok := s.Structures[ref]; ok {
		kinds = append(kinds, KindStructure)
	}
	if _, ok := s.ArrayTypes[ref]; ok {
		kinds = append(kinds, KindArrayType)
	}
	if _, ok := s.AuthorizationObjects[ref]; ok {
		kinds = append(kinds, KindAuthorizationObject)
	}
	for _, sources := range s.Sources {
		for _, src := range sources {
			if src.Name == ref && src.Kind != model.SourceInclude {
				kinds = append(kinds, KindClass)
			}
		}
	}
	switch len(kinds) {
	case 0:
		return "", "", &rterr.NotFound{Kind: "object", Name: ref}
	case 1:
		return kinds[0], ref, nil
	}
	return "", "", &rterr.Ambiguous{Name: ref, Kinds: kinds}
}

// CodeUse is the use of an object by the code of a package.
type CodeUse struct {
	Package model.PackageID        `json:"package"`
	File    string                 `json:"file"`
	Line    int                    `json:"line"`
	Kind    string                 `json:"kind"`
	Object  string                 `json:"object"`
	Member  model.Optional[string] `json:"member"`
}

// CodeUses returns where code uses an object: a table, structure, array
// type, data element, domain, class, interface or include, including
// its fields, components and members.
func (k *Kernel) CodeUses(ctx context.Context, object string) ([]CodeUse, error) {
	codes, err := k.repo.LoadCode(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := []CodeUse{}
	for _, id := range sortedCodeIDs(codes) {
		for _, u := range codes[id].Uses {
			if u.Object == object {
				out = append(out, CodeUse{Package: id, File: u.File, Line: u.Line, Kind: u.Kind, Object: u.Object, Member: u.Member})
			}
		}
	}
	return out, nil
}

func sortedCodeIDs(m map[model.PackageID]*repo.Code) []model.PackageID {
	out := make([]model.PackageID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// StructureDetail is the resolved definition of a structure, table or
// array type with its direct uses.
type StructureDetail struct {
	Definition *structure.Definition `json:"definition"`
	UsedBy     []model.Use           `json:"usedBy"`
	CodeUses   []CodeUse             `json:"codeUses"`
}

// Structure returns the resolved definition of a structure, table or array
// type with texts in all languages set up in the database, and where it is
// used.
func (k *Kernel) Structure(ctx context.Context, name string) (*StructureDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	langs, err := k.languageCodes(ctx)
	if err != nil {
		return nil, err
	}
	d, err := structure.Resolve(s, langs, name)
	if err != nil {
		return nil, err
	}
	uses := s.UsesOf(name)
	if uses == nil {
		uses = []model.Use{}
	}
	codeUses, err := k.CodeUses(ctx, name)
	if err != nil {
		return nil, err
	}
	return &StructureDetail{Definition: d, UsedBy: uses, CodeUses: codeUses}, nil
}

func settingOf(s *model.Schema, table string) model.Optional[string] {
	if id := s.SettingOf(table); id != "" {
		return model.Some(id)
	}
	return model.None[string]()
}

// ClassDetail describes a class or interface of the code of a package.
type ClassDetail struct {
	Name     string           `json:"name"`
	Package  model.PackageID  `json:"package"`
	Kind     model.SourceKind `json:"kind"`
	File     string           `json:"file"`
	CodeUses []CodeUse        `json:"codeUses"`
}

// Class returns a class or interface of an installed package and where
// code uses it.
func (k *Kernel) Class(ctx context.Context, name string) (*ClassDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	for id, sources := range s.Sources {
		for _, src := range sources {
			if src.Name == name && src.Kind != model.SourceInclude {
				d := &ClassDetail{Name: name, Package: id, Kind: src.Kind, File: src.File}
				if d.CodeUses, err = k.CodeUses(ctx, name); err != nil {
					return nil, err
				}
				return d, nil
			}
		}
	}
	return nil, &rterr.NotFound{Kind: "class", Name: name}
}
