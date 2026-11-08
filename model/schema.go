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
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/rterr"
)

// Schema is the complete set of objects in one database: the kernel's native
// objects and those of all installed packages. Tables hold only the fields
// declared by their owner; extensions are kept separately so that each
// object has exactly one owner. Physical combines them.
type Schema struct {
	Packages            map[PackageID]*PackageInfo
	Domains             map[string]*Domain
	DataElements        map[string]*DataElement
	Tables              map[string]*Table
	Extensions          map[ExtensionKey]*Extension
	AssetRepositories   map[AssetRepositoryKey]*AssetRepository
	Structures          map[string]*Structure
	ArrayTypes          map[string]*ArrayType
	StructureExtensions map[StructureExtensionKey]*StructureExtension
	// Sources holds the source files of each package, the only source for
	// compiling its code.
	Sources map[PackageID][]Source
	// Transformations are the XSLT stylesheets of the packages by name.
	Transformations map[string]*Transformation
	// HTTPServices are the service nodes of the packages by full path.
	HTTPServices map[string]*HTTPService
	// AuthorizationObjects are the authorization objects by name.
	AuthorizationObjects map[string]*AuthorizationObject
	// Roles are the package roles and the local roles by name.
	Roles map[string]*Role
	// ExtensionPoints are the extension points by name.
	ExtensionPoints map[string]*ExtensionPoint
	// Implementations are the implementations of extension points by name.
	Implementations map[string]*Implementation
	// BusinessObjects are the business objects by name.
	BusinessObjects map[string]*BusinessObject
	// BusinessObjectExtensions are the extensions of business objects by
	// extending package and business object.
	BusinessObjectExtensions map[BusinessObjectExtensionKey]*BusinessObjectExtension
}

// NewSchema returns an empty schema.
func NewSchema() *Schema {
	return &Schema{
		Packages:                 map[PackageID]*PackageInfo{},
		Domains:                  map[string]*Domain{},
		DataElements:             map[string]*DataElement{},
		Tables:                   map[string]*Table{},
		Extensions:               map[ExtensionKey]*Extension{},
		AssetRepositories:        map[AssetRepositoryKey]*AssetRepository{},
		Structures:               map[string]*Structure{},
		ArrayTypes:               map[string]*ArrayType{},
		StructureExtensions:      map[StructureExtensionKey]*StructureExtension{},
		Sources:                  map[PackageID][]Source{},
		Transformations:          map[string]*Transformation{},
		HTTPServices:             map[string]*HTTPService{},
		AuthorizationObjects:     map[string]*AuthorizationObject{},
		Roles:                    map[string]*Role{},
		ExtensionPoints:          map[string]*ExtensionPoint{},
		Implementations:          map[string]*Implementation{},
		BusinessObjects:          map[string]*BusinessObject{},
		BusinessObjectExtensions: map[BusinessObjectExtensionKey]*BusinessObjectExtension{},
	}
}

// Clone returns a deep copy of s.
func (s *Schema) Clone() *Schema {
	c := NewSchema()
	for k, v := range s.Transformations {
		c.Transformations[k] = cloneTransformation(v)
	}
	for k, v := range s.HTTPServices {
		c.HTTPServices[k] = cloneHTTPService(v)
	}
	for k, v := range s.AuthorizationObjects {
		c.AuthorizationObjects[k] = cloneAuthorizationObject(v)
	}
	for k, v := range s.Roles {
		c.Roles[k] = cloneRole(v)
	}
	for k, v := range s.ExtensionPoints {
		c.ExtensionPoints[k] = cloneExtensionPoint(v)
	}
	for k, v := range s.Implementations {
		c.Implementations[k] = cloneImplementation(v)
	}
	for k, v := range s.BusinessObjects {
		c.BusinessObjects[k] = cloneBusinessObject(v)
	}
	for k, v := range s.BusinessObjectExtensions {
		c.BusinessObjectExtensions[k] = cloneBusinessObjectExtension(v)
	}
	for k, v := range s.Sources {
		c.Sources[k] = slices.Clone(v)
	}
	for k, v := range s.Packages {
		p := *v
		p.Manifest.Requires = slices.Clone(v.Manifest.Requires)
		p.Manifest.Capabilities = slices.Clone(v.Manifest.Capabilities)
		c.Packages[k] = &p
	}
	for k, v := range s.Domains {
		c.Domains[k] = cloneDomain(v)
	}
	for k, v := range s.DataElements {
		c.DataElements[k] = cloneDataElement(v)
	}
	for k, v := range s.Tables {
		c.Tables[k] = cloneTable(v)
	}
	for k, v := range s.Extensions {
		e := *v
		e.Fields = cloneFields(v.Fields)
		e.ForeignKeys = cloneFKs(v.ForeignKeys)
		c.Extensions[k] = &e
	}
	for k, v := range s.AssetRepositories {
		c.AssetRepositories[k] = cloneAssetRepository(v)
	}
	for k, v := range s.Structures {
		c.Structures[k] = cloneStructure(v)
	}
	for k, v := range s.ArrayTypes {
		c.ArrayTypes[k] = cloneArrayType(v)
	}
	for k, v := range s.StructureExtensions {
		e := *v
		e.Components = cloneComponents(v.Components)
		c.StructureExtensions[k] = &e
	}
	return c
}

func cloneDomain(d *Domain) *Domain {
	c := *d
	c.Values = slices.Clone(d.Values)
	return &c
}

func cloneDataElement(d *DataElement) *DataElement {
	c := *d
	c.Texts = make(map[string]DataElementText, len(d.Texts))
	for l, t := range d.Texts {
		c.Texts[l] = t
	}
	return &c
}

func cloneTable(t *Table) *Table {
	c := *t
	c.Fields = cloneFields(t.Fields)
	c.PrimaryKey = slices.Clone(t.PrimaryKey)
	c.ForeignKeys = cloneFKs(t.ForeignKeys)
	return &c
}

func cloneFields(fs []Field) []Field {
	out := slices.Clone(fs)
	for i := range out {
		out[i].DefaultJSON = slices.Clone(out[i].DefaultJSON)
	}
	return out
}

func cloneFKs(fks []ForeignKey) []ForeignKey {
	out := slices.Clone(fks)
	for i := range out {
		out[i].Columns = slices.Clone(out[i].Columns)
	}
	return out
}

// AddPackage adds the objects of p to the schema. Names that are already
// taken are reported as NameConflict errors and leave the schema unchanged
// for those objects.
func (s *Schema) AddPackage(p *Package, installedAt, updatedAt time.Time) error {
	var errs rterr.List
	id := p.Manifest.ID
	if len(p.Sources) > 0 {
		s.Sources[id] = slices.Clone(p.Sources)
	}
	for i := range p.Domains {
		d := cloneDomain(&p.Domains[i])
		d.Owner = id
		if old, ok := s.Domains[d.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "domain", Name: d.Name, Owner: string(old.Owner), Package: string(id), Location: d.Src})
			continue
		}
		s.Domains[d.Name] = d
	}
	for i := range p.DataElements {
		d := cloneDataElement(&p.DataElements[i])
		d.Owner = id
		if old, ok := s.DataElements[d.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "data element", Name: d.Name, Owner: string(old.Owner), Package: string(id), Location: d.Src})
			continue
		}
		s.DataElements[d.Name] = d
	}
	for i := range p.Tables {
		t := cloneTable(&p.Tables[i])
		t.Owner = id
		setOwner(t.Fields, t.ForeignKeys, id)
		if old, ok := s.TypeOwner(t.Name); ok {
			errs = append(errs, &rterr.NameConflict{Kind: "table", Name: t.Name, Owner: string(old), Package: string(id), Location: t.Src})
			continue
		}
		s.Tables[t.Name] = t
	}
	for i := range p.Extensions {
		e := p.Extensions[i]
		e.Package = id
		e.Fields = cloneFields(e.Fields)
		e.ForeignKeys = cloneFKs(e.ForeignKeys)
		setOwner(e.Fields, e.ForeignKeys, id)
		if _, ok := s.Extensions[e.Key()]; ok {
			errs = append(errs, &rterr.ValidationError{Code: rterr.CodeDuplicate, Location: e.Src, Object: "extension of " + e.Table, Message: "package extends this table more than once"})
			continue
		}
		s.Extensions[e.Key()] = &e
	}
	for i := range p.AssetRepositories {
		r := cloneAssetRepository(&p.AssetRepositories[i])
		r.Package = id
		s.AssetRepositories[r.Key()] = r
	}
	// Structures, tables and array types share one namespace.
	typeTaken := func(name string) (PackageID, bool) { return s.TypeOwner(name) }
	for i := range p.Structures {
		st := cloneStructure(&p.Structures[i])
		st.Owner = id
		for _, e := range st.Elements {
			if e.Component != nil {
				e.Component.Owner, e.Component.Origin = id, ComponentStructure
			}
		}
		if old, ok := typeTaken(st.Name); ok {
			errs = append(errs, &rterr.NameConflict{Kind: "structure", Name: st.Name, Owner: string(old), Package: string(id), Location: st.Src})
			continue
		}
		s.Structures[st.Name] = st
	}
	for i := range p.ArrayTypes {
		a := cloneArrayType(&p.ArrayTypes[i])
		a.Owner = id
		if old, ok := typeTaken(a.Name); ok {
			errs = append(errs, &rterr.NameConflict{Kind: "array type", Name: a.Name, Owner: string(old), Package: string(id), Location: a.Src})
			continue
		}
		s.ArrayTypes[a.Name] = a
	}
	for i := range p.Transformations {
		t := cloneTransformation(&p.Transformations[i])
		t.Owner = id
		if old, ok := s.Transformations[t.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "transformation", Name: t.Name, Owner: string(old.Owner), Package: string(id), Location: t.Src})
			continue
		}
		s.Transformations[t.Name] = t
	}
	for i := range p.HTTPServices {
		h := cloneHTTPService(&p.HTTPServices[i])
		h.Owner = id
		if _, ok := s.HTTPServices[h.FullPath()]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "service node", Name: h.FullPath(), Owner: string(id), Package: string(id), Location: h.Src})
			continue
		}
		s.HTTPServices[h.FullPath()] = h
	}
	for i := range p.AuthorizationObjects {
		o := cloneAuthorizationObject(&p.AuthorizationObjects[i])
		o.Owner = id
		if old, ok := s.AuthorizationObjects[o.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "authorization object", Name: o.Name, Owner: string(old.Owner), Package: string(id), Location: o.Src})
			continue
		}
		s.AuthorizationObjects[o.Name] = o
	}
	for i := range p.Roles {
		r := cloneRole(&p.Roles[i])
		r.Owner = id
		if old, ok := s.Roles[r.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "role", Name: r.Name, Owner: string(old.Owner), Package: string(id), Location: r.Src})
			continue
		}
		s.Roles[r.Name] = r
	}
	for i := range p.ExtensionPoints {
		e := cloneExtensionPoint(&p.ExtensionPoints[i])
		e.Owner = id
		if old, ok := s.ExtensionPoints[e.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "extension point", Name: e.Name, Owner: string(old.Owner), Package: string(id), Location: e.Src})
			continue
		}
		s.ExtensionPoints[e.Name] = e
	}
	for i := range p.Implementations {
		impl := cloneImplementation(&p.Implementations[i])
		impl.Owner = id
		if old, ok := s.Implementations[impl.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "implementation", Name: impl.Name, Owner: string(old.Owner), Package: string(id), Location: impl.Src})
			continue
		}
		s.Implementations[impl.Name] = impl
	}
	for i := range p.BusinessObjects {
		b := cloneBusinessObject(&p.BusinessObjects[i])
		b.Owner = id
		if old, ok := s.BusinessObjects[b.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "business object", Name: b.Name, Owner: string(old.Owner), Package: string(id), Location: b.Src})
			continue
		}
		s.BusinessObjects[b.Name] = b
	}
	for i := range p.BusinessObjectExtensions {
		x := cloneBusinessObjectExtension(&p.BusinessObjectExtensions[i])
		x.Package = id
		if _, ok := s.BusinessObjectExtensions[x.Key()]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "business object extension", Name: x.BusinessObject, Owner: string(id), Package: string(id), Location: x.Src})
			continue
		}
		s.BusinessObjectExtensions[x.Key()] = x
	}
	for i := range p.StructureExtensions {
		e := p.StructureExtensions[i]
		e.Package = id
		e.Components = cloneComponents(e.Components)
		for j := range e.Components {
			e.Components[j].Owner, e.Components[j].Origin = id, ComponentExtension
		}
		if _, ok := s.StructureExtensions[e.Key()]; ok {
			errs = append(errs, &rterr.ValidationError{Code: rterr.CodeDuplicate, Location: e.Src, Object: "extension of structure " + e.Structure, Message: "package extends this structure more than once"})
			continue
		}
		s.StructureExtensions[e.Key()] = &e
	}
	if id != Kernel {
		s.Packages[id] = &PackageInfo{Manifest: p.Manifest, InstalledAt: installedAt, UpdatedAt: updatedAt}
	}
	return errs.Err()
}

// setOwner makes the package that declares fields and foreign keys their
// owner.
func setOwner(fields []Field, fks []ForeignKey, id PackageID) {
	for i := range fields {
		fields[i].Owner = id
	}
	for i := range fks {
		fks[i].Owner = id
	}
}

// RemoveLanguage removes all texts in a language and returns how many were
// removed.
func (s *Schema) RemoveLanguage(code string) int {
	n := 0
	for _, d := range s.DataElements {
		if _, ok := d.Texts[code]; ok {
			delete(d.Texts, code)
			n++
		}
	}
	for _, st := range s.Structures {
		if _, ok := st.Texts[code]; ok {
			delete(st.Texts, code)
			n++
		}
	}
	for _, a := range s.ArrayTypes {
		if _, ok := a.Texts[code]; ok {
			delete(a.Texts, code)
			n++
		}
	}
	for _, t := range s.Transformations {
		if _, ok := t.Texts[code]; ok {
			delete(t.Texts, code)
			n++
		}
	}
	for _, h := range s.HTTPServices {
		if _, ok := h.Texts[code]; ok {
			delete(h.Texts, code)
			n++
		}
	}
	for _, o := range s.AuthorizationObjects {
		if _, ok := o.Texts[code]; ok {
			delete(o.Texts, code)
			n++
		}
	}
	for _, r := range s.Roles {
		if _, ok := r.Texts[code]; ok {
			delete(r.Texts, code)
			n++
		}
	}
	for _, e := range s.ExtensionPoints {
		if _, ok := e.Description[code]; ok {
			delete(e.Description, code)
			n++
		}
	}
	for _, i := range s.Implementations {
		if _, ok := i.Description[code]; ok {
			delete(i.Description, code)
			n++
		}
	}
	for _, b := range s.BusinessObjects {
		if _, ok := b.Description[code]; ok {
			delete(b.Description, code)
			n++
		}
	}
	for _, r := range s.AssetRepositories {
		if _, ok := r.Description[code]; ok {
			delete(r.Description, code)
			n++
		}
		for _, e := range r.Entries {
			if _, ok := e.Description[code]; ok {
				delete(e.Description, code)
				n++
			}
			if _, ok := e.Variants[code]; ok {
				delete(e.Variants, code)
				n++
				// An asset that existed only in this language is gone.
				if !e.Folder && e.Content == nil && len(e.Variants) == 0 {
					delete(r.Entries, e.Path)
				}
			}
		}
	}
	return n
}

// RemovePackage removes all objects owned by id.
func (s *Schema) RemovePackage(id PackageID) {
	delete(s.Sources, id)
	for name, t := range s.Transformations {
		if t.Owner == id {
			delete(s.Transformations, name)
		}
	}
	for path, h := range s.HTTPServices {
		if h.Owner == id {
			delete(s.HTTPServices, path)
		}
	}
	for name, o := range s.AuthorizationObjects {
		if o.Owner == id {
			delete(s.AuthorizationObjects, name)
		}
	}
	for name, r := range s.Roles {
		if r.Owner == id {
			delete(s.Roles, name)
		}
	}
	for name, e := range s.ExtensionPoints {
		if e.Owner == id {
			delete(s.ExtensionPoints, name)
		}
	}
	for name, i := range s.Implementations {
		if i.Owner == id {
			delete(s.Implementations, name)
		}
	}
	for name, b := range s.BusinessObjects {
		if b.Owner == id {
			delete(s.BusinessObjects, name)
		}
	}
	for k := range s.BusinessObjectExtensions {
		if k.Package == id {
			delete(s.BusinessObjectExtensions, k)
		}
	}
	for name, d := range s.Domains {
		if d.Owner == id {
			delete(s.Domains, name)
		}
	}
	for name, d := range s.DataElements {
		if d.Owner == id {
			delete(s.DataElements, name)
		}
	}
	for name, t := range s.Tables {
		if t.Owner == id {
			delete(s.Tables, name)
		}
	}
	for k := range s.Extensions {
		if k.Package == id {
			delete(s.Extensions, k)
		}
	}
	for n, st := range s.Structures {
		if st.Owner == id {
			delete(s.Structures, n)
		}
	}
	for n, a := range s.ArrayTypes {
		if a.Owner == id {
			delete(s.ArrayTypes, n)
		}
	}
	for k := range s.StructureExtensions {
		if k.Package == id {
			delete(s.StructureExtensions, k)
		}
	}
	for k := range s.AssetRepositories {
		if k.Package == id {
			delete(s.AssetRepositories, k)
		}
	}
	delete(s.Packages, id)
}

// Requires returns the packages that id requires directly or transitively.
// The result does not contain id itself.
func (s *Schema) Requires(id PackageID) map[PackageID]bool {
	seen := map[PackageID]bool{}
	var walk func(PackageID)
	walk = func(p PackageID) {
		info, ok := s.Packages[p]
		if !ok {
			return
		}
		for _, d := range info.Manifest.Requires {
			if !seen[d.ID] {
				seen[d.ID] = true
				walk(d.ID)
			}
		}
	}
	walk(id)
	delete(seen, id)
	return seen
}

// Visible reports whether objects owned by owner may be used by package id:
// its own objects, those of required packages and kernel objects.
func (s *Schema) Visible(id, owner PackageID) bool {
	return owner == id || owner == Kernel || s.Requires(id)[owner]
}

// Dependents returns the installed packages that require id directly.
func (s *Schema) Dependents(id PackageID) []PackageID {
	var out []PackageID
	for pid, info := range s.Packages {
		for _, d := range info.Manifest.Requires {
			if d.ID == id {
				out = append(out, pid)
			}
		}
	}
	slices.Sort(out)
	return out
}

// PackageIDs returns the installed package IDs in sorted order.
func (s *Schema) PackageIDs() []PackageID {
	ids := make([]PackageID, 0, len(s.Packages))
	for id := range s.Packages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// TableNames returns all table names in sorted order.
func (s *Schema) TableNames() []string { return sortedKeys(s.Tables) }

// DomainNames returns all domain names in sorted order.
func (s *Schema) DomainNames() []string { return sortedKeys(s.Domains) }

// DataElementNames returns all data element names in sorted order.
func (s *Schema) DataElementNames() []string { return sortedKeys(s.DataElements) }

// FieldDomain returns the name of the domain of a field, resolving its data
// element.
func (s *Schema) FieldDomain(f *Field) string {
	if name, ok := f.DataElement.Get(); ok {
		if d, ok := s.DataElements[name]; ok {
			return d.Domain
		}
		return ""
	}
	return f.Domain
}

// resolveDefault returns the effective default of a field: its own if it
// declares one, otherwise that of its domain.
func (s *Schema) resolveDefault(f *Field) Default {
	if f.Default.Kind != DefaultNone {
		return f.Default
	}
	if d, ok := s.Domains[s.FieldDomain(f)]; ok {
		return d.Default
	}
	return NoDefault()
}

// ExtensionsOf returns the extensions of a table ordered by package ID.
func (s *Schema) ExtensionsOf(table string) []*Extension {
	var out []*Extension
	for k, e := range s.Extensions {
		if k.Table == table {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b *Extension) int { return cmp.Compare(a.Package, b.Package) })
	return out
}

// SortedExtensions returns all extensions ordered by table and package.
func (s *Schema) SortedExtensions() []*Extension {
	out := make([]*Extension, 0, len(s.Extensions))
	for _, e := range s.Extensions {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b *Extension) int {
		return cmp.Or(cmp.Compare(a.Table, b.Table), cmp.Compare(a.Package, b.Package))
	})
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Physical is the database layout derived from a schema: every table with
// its tenant field, its own fields and all extension fields, and every
// foreign key including the tenant mapping.
type Physical struct {
	Tables       map[string]*Table
	Domains      map[string]*Domain
	DataElements map[string]*DataElement
}

// TableNames returns the physical table names in sorted order.
func (p *Physical) TableNames() []string { return sortedKeys(p.Tables) }

// Domain returns the domain of a field.
func (p *Physical) Domain(f *Field) *Domain { return p.Domains[f.Domain] }

// Physical computes the database layout of the schema.
func (s *Schema) Physical() *Physical {
	p := &Physical{Tables: map[string]*Table{}, Domains: s.Domains, DataElements: s.DataElements}
	r := s.Resolver()
	tenant := func(name string) bool {
		t, ok := s.Tables[name]
		return ok && t.TenantSpecific
	}
	augment := func(src *Table, fk ForeignKey) ForeignKey {
		fk.Columns = slices.Clone(fk.Columns)
		if src.TenantSpecific && tenant(fk.Target) {
			fk.Columns = append([]ColumnPair{{Field: TenantField, References: TenantField}}, fk.Columns...)
		}
		return fk
	}
	for name, base := range s.Tables {
		t := cloneTable(base)
		t.Fields = nil
		t.ForeignKeys = nil
		t.PrimaryKey = nil
		if base.TenantSpecific {
			t.Fields = append(t.Fields, Field{
				Name: TenantField, LocalName: TenantField, Owner: Kernel, Origin: OriginTenant,
				Domain: TenantDomain, NotNull: true,
			})
			t.PrimaryKey = append(t.PrimaryKey, TenantField)
			t.ForeignKeys = append(t.ForeignKeys, ForeignKey{
				Name: TenantForeignKey, Owner: Kernel, Origin: OriginTenant, Target: TenantTable,
				Columns: []ColumnPair{{Field: TenantField, References: TenantTableKey}},
			})
		}
		t.Fields = append(t.Fields, s.columns(r, base)...)
		t.PrimaryKey = append(t.PrimaryKey, base.PrimaryKey...)
		for _, fk := range base.ForeignKeys {
			t.ForeignKeys = append(t.ForeignKeys, augment(base, fk))
		}
		for _, e := range s.ExtensionsOf(name) {
			t.Fields = append(t.Fields, cloneFields(e.Fields)...)
			for _, fk := range e.ForeignKeys {
				t.ForeignKeys = append(t.ForeignKeys, augment(base, fk))
			}
		}
		for i := range t.Fields {
			if t.IsKey(t.Fields[i].Name) {
				t.Fields[i].NotNull = true
			}
			t.Fields[i].Domain = s.FieldDomain(&t.Fields[i])
			t.Fields[i].ResolvedDefault = s.resolveDefault(&t.Fields[i])
		}
		p.Tables[name] = t
	}
	return p
}

// columns returns the own fields of a table with the components of its
// includes at their places. Included components become columns of origin
// INCLUDE that remember the include and the declaring structure. Includes
// that cannot be resolved, e.g. because of a cycle, and non-elementary
// components are left out; the validation reports them.
func (s *Schema) columns(r *Resolver, t *Table) []Field {
	var out []Field
	includes := slices.Clone(t.Includes)
	slices.SortStableFunc(includes, func(a, b TableInclude) int { return a.After - b.After })
	next := 0
	add := func(upTo int) {
		for ; next < len(includes) && includes[next].After <= upTo; next++ {
			inc := includes[next].Name
			comps, ok := r.Components(inc)
			if !ok {
				continue
			}
			for _, c := range comps {
				if f, ok := includedField(c, inc); ok {
					out = append(out, f)
				}
			}
		}
	}
	for i, f := range cloneFields(t.Fields) {
		add(i)
		out = append(out, f)
	}
	add(len(t.Fields))
	return out
}

func includedField(c ResolvedComponent, inc string) (Field, bool) {
	f := Field{Name: c.Name, LocalName: c.Name, Description: c.Description, Owner: c.Owner, Origin: OriginInclude,
		NotNull: c.NotNull, Default: c.Default, DefaultJSON: slices.Clone(c.DefaultJSON),
		Included: Some(inc), Source: Some(c.Declared), Src: c.Src}
	switch c.Type.Kind {
	case TypeDataElement:
		f.DataElement = Some(c.Type.Name)
	case TypeDomain:
		f.Domain = c.Type.Name
	default:
		return Field{}, false
	}
	return f, true
}
