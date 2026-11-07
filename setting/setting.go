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

// Package setting provides the resolved definition of a setting: the
// setting merged with all installed extensions, as a tree of tables with
// every field, its type, labels and derived value helps.
//
// The definition contains everything a user interface or a data access
// layer needs to work with a setting without reading the repository. The
// tenant field never appears in it; a tenant-specific setting is marked by
// TenantSpecific and its rows are always read and written for one tenant.
//
// The types are stable: fields are only added, never renamed or removed,
// within one FormatVersion.
package setting

import (
	"encoding/json"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// FormatVersion is the version of the structure of Definition.
const FormatVersion = 1

// Definition is the resolved definition of a setting.
type Definition struct {
	FormatVersion int `json:"formatVersion"`
	// ID is the setting ID.
	ID string `json:"id"`
	// Owner is the package that defines the setting.
	Owner model.PackageID `json:"owner"`
	// Texts holds the label of the setting by language code.
	Texts map[string]Text `json:"texts"`
	// DataClass is the class shared by all tables of the setting.
	DataClass model.DataClass `json:"dataClass"`
	// TenantSpecific is the tenant flag shared by all tables.
	TenantSpecific bool `json:"tenantSpecific"`
	// Extensions lists the packages that added tables, in package order.
	Extensions []model.PackageID `json:"extensions,omitempty"`
	// Languages are the languages set up in the database; texts exist only
	// in these.
	Languages []string `json:"languages"`
	// Root is the table the setting starts with.
	Root *Table `json:"root"`
}

// Text is the label of a setting in one language.
type Text struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Operations are the operations allowed on the rows of a table.
type Operations = model.Operations

// Table is a table of the setting with its dependent tables.
type Table struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Owner is the package that owns the table.
	Owner model.PackageID `json:"owner"`
	// Origin is the package that added the table to the setting: the owner
	// of the setting or an extending package.
	Origin         model.PackageID `json:"origin"`
	DataClass      model.DataClass `json:"dataClass"`
	TenantSpecific bool            `json:"tenantSpecific"`
	// ParentKey is the foreign key to the parent table; nil for the root.
	ParentKey  *ParentKey `json:"parentKey,omitempty"`
	Operations Operations `json:"operations"`
	// PrimaryKey lists the key fields in key order.
	PrimaryKey []string `json:"primaryKey"`
	// Fields lists all fields in their physical order, including the
	// extension fields of other packages.
	Fields []Field `json:"fields"`
	// Children are the dependent tables.
	Children []*Table `json:"children,omitempty"`
}

// ParentKey is the foreign key from a dependent table to its parent.
type ParentKey struct {
	Name   string             `json:"name"`
	Table  string             `json:"table"`
	Fields []model.ColumnPair `json:"fields"` // own field -> key field of the parent
}

// Field is a field of a table.
type Field struct {
	Name        string            `json:"name"`      // physical name
	LocalName   string            `json:"localName"` // name as declared
	Description string            `json:"description,omitempty"`
	Owner       model.PackageID   `json:"owner"`
	Origin      model.FieldOrigin `json:"origin"` // TABLE or EXTENSION
	// DataElement carries the labels; nil means the field has none and is
	// shown with its technical name.
	DataElement *DataElement       `json:"dataElement,omitempty"`
	Domain      string             `json:"domain"`
	Type        model.BaseType     `json:"type"`
	Length      int                `json:"length,omitempty"`
	Decimals    int                `json:"decimals,omitempty"`
	FixedValues []model.FixedValue `json:"fixedValues,omitempty"`
	NotNull     bool               `json:"notNull"`
	Key         bool               `json:"key"`
	Default     *string            `json:"default,omitempty"` // canonical text form
	// FromParent marks fields filled through the key to the parent table.
	FromParent bool `json:"fromParent,omitempty"`
	// ValueHelps are derived from the foreign keys the field takes part
	// in. Fixed values are a value help as well.
	ValueHelps []ValueHelp `json:"valueHelps,omitempty"`
}

// DataElement is the data element of a field with its labels.
type DataElement struct {
	Name  string                           `json:"name"`
	Owner model.PackageID                  `json:"owner"`
	Texts map[string]model.DataElementText `json:"texts"` // by language code
}

// ValueHelp offers the rows of a target table as values, derived from a
// foreign key.
type ValueHelp struct {
	ForeignKey string             `json:"foreignKey"`
	Table      string             `json:"table"`
	Fields     []model.ColumnPair `json:"fields"` // own field -> key field of the target
}

// JSON returns the definition as indented JSON.
func (d *Definition) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Walk calls fn for every table, parents before children.
func (d *Definition) Walk(fn func(t *Table, depth int)) {
	var walk func(*Table, int)
	walk = func(t *Table, depth int) {
		fn(t, depth)
		for _, c := range t.Children {
			walk(c, depth+1)
		}
	}
	if d.Root != nil {
		walk(d.Root, 0)
	}
}

// Table returns the table with the given name, or nil.
func (d *Definition) Table(name string) *Table {
	var out *Table
	d.Walk(func(t *Table, _ int) {
		if t.Name == name {
			out = t
		}
	})
	return out
}

// Field returns the field with the given physical name, or nil.
func (t *Table) Field(name string) *Field {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// Resolve builds the definition of a setting from a valid schema. Texts are
// limited to languages.
func Resolve(s *model.Schema, languages []string, id string) (*Definition, error) {
	st, ok := s.Settings[id]
	if !ok {
		return nil, &rterr.NotFound{Kind: "setting", Name: id}
	}
	langs := slices.Clone(languages)
	slices.Sort(langs)
	phys := s.Physical()
	d := &Definition{FormatVersion: FormatVersion, ID: id, Owner: st.Owner, Texts: map[string]Text{}, Languages: langs}
	for _, l := range langs {
		if t, ok := st.Texts[l]; ok {
			d.Texts[l] = Text{Label: t.Label, Description: t.Description}
		}
	}
	for _, e := range s.ExtensionsOfSetting(id) {
		d.Extensions = append(d.Extensions, e.Package)
	}
	tables := map[string]*Table{}
	for _, n := range s.SettingNodes(id) {
		pt := phys.Tables[n.Table]
		if pt == nil {
			return nil, &rterr.NotFound{Kind: "table", Name: n.Table}
		}
		t := resolveTable(phys, pt, n, langs)
		tables[n.Table] = t
		if n.Root {
			d.Root = t
			d.DataClass, d.TenantSpecific = pt.DataClass, pt.TenantSpecific
			continue
		}
		parent := tables[n.Parent]
		if parent == nil {
			return nil, &rterr.NotFound{Kind: "parent table", Name: n.Parent}
		}
		parent.Children = append(parent.Children, t)
	}
	return d, nil
}

// withoutTenant drops the tenant mapping from foreign key columns.
func withoutTenant(cols []model.ColumnPair) []model.ColumnPair {
	var out []model.ColumnPair
	for _, c := range cols {
		if c.Field != model.TenantField {
			out = append(out, c)
		}
	}
	return out
}

func resolveTable(phys *model.Physical, pt *model.Table, n model.SettingNode, langs []string) *Table {
	t := &Table{Name: pt.Name, Description: pt.Description, Owner: pt.Owner, Origin: n.Origin, DataClass: pt.DataClass,
		TenantSpecific: pt.TenantSpecific, Operations: n.Operations, PrimaryKey: []string{}, Fields: []Field{}}
	for _, k := range pt.PrimaryKey {
		if k != model.TenantField {
			t.PrimaryKey = append(t.PrimaryKey, k)
		}
	}
	fromParent := map[string]bool{}
	if !n.Root {
		if fk := pt.ForeignKey(n.ForeignKey); fk != nil {
			t.ParentKey = &ParentKey{Name: fk.Name, Table: fk.Target, Fields: withoutTenant(fk.Columns)}
			for _, c := range t.ParentKey.Fields {
				fromParent[c.Field] = true
			}
		}
	}
	for _, f := range pt.Fields {
		if f.Name == model.TenantField {
			continue
		}
		dom := phys.Domain(&f)
		rf := Field{Name: f.Name, LocalName: f.LocalName, Description: f.Description, Owner: f.Owner, Origin: f.Origin,
			Domain: f.Domain, NotNull: f.NotNull, Key: pt.IsKey(f.Name), Default: f.Default, FromParent: fromParent[f.Name]}
		if dom != nil {
			rf.Type, rf.FixedValues = dom.Type, slices.Clone(dom.Values)
			if dom.Type.HasLength() {
				rf.Length, rf.Decimals = dom.Length, dom.Decimals
			}
		}
		if de := phys.DataElements[f.DataElement]; de != nil {
			rf.DataElement = &DataElement{Name: de.Name, Owner: de.Owner, Texts: map[string]model.DataElementText{}}
			for _, l := range langs {
				if txt, ok := de.Texts[l]; ok {
					rf.DataElement.Texts[l] = txt
				}
			}
		}
		for _, fk := range pt.ForeignKeys {
			if fk.Origin == model.OriginTenant || !slices.Contains(fk.FieldNames(), f.Name) {
				continue
			}
			rf.ValueHelps = append(rf.ValueHelps, ValueHelp{ForeignKey: fk.Name, Table: fk.Target, Fields: withoutTenant(fk.Columns)})
		}
		t.Fields = append(t.Fields, rf)
	}
	return t
}
