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
	Packages     map[PackageID]*PackageInfo
	Domains      map[string]*Domain
	DataElements map[string]*DataElement
	Tables       map[string]*Table
	Extensions   map[ExtensionKey]*Extension
}

// NewSchema returns an empty schema.
func NewSchema() *Schema {
	return &Schema{
		Packages:     map[PackageID]*PackageInfo{},
		Domains:      map[string]*Domain{},
		DataElements: map[string]*DataElement{},
		Tables:       map[string]*Table{},
		Extensions:   map[ExtensionKey]*Extension{},
	}
}

// Clone returns a deep copy of s.
func (s *Schema) Clone() *Schema {
	c := NewSchema()
	for k, v := range s.Packages {
		p := *v
		p.Manifest.Requires = slices.Clone(v.Manifest.Requires)
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
		if out[i].Default != nil {
			v := *out[i].Default
			out[i].Default = &v
		}
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
		if old, ok := s.Tables[t.Name]; ok {
			errs = append(errs, &rterr.NameConflict{Kind: "table", Name: t.Name, Owner: string(old.Owner), Package: string(id), Location: t.Src})
			continue
		}
		s.Tables[t.Name] = t
	}
	for i := range p.Extensions {
		e := p.Extensions[i]
		e.Package = id
		e.Fields = cloneFields(e.Fields)
		e.ForeignKeys = cloneFKs(e.ForeignKeys)
		if _, ok := s.Extensions[e.Key()]; ok {
			errs = append(errs, &rterr.ValidationError{Code: rterr.CodeDuplicate, Location: e.Src, Object: "extension of " + e.Table, Message: "package extends this table more than once"})
			continue
		}
		s.Extensions[e.Key()] = &e
	}
	if id != Kernel {
		s.Packages[id] = &PackageInfo{Manifest: p.Manifest, InstalledAt: installedAt, UpdatedAt: updatedAt}
	}
	return errs.Err()
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
	return n
}

// RemovePackage removes all objects owned by id.
func (s *Schema) RemovePackage(id PackageID) {
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
	if f.DataElement != "" {
		if d, ok := s.DataElements[f.DataElement]; ok {
			return d.Domain
		}
		return ""
	}
	return f.Domain
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
		t.Fields = append(t.Fields, cloneFields(base.Fields)...)
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
		}
		p.Tables[name] = t
	}
	return p
}
