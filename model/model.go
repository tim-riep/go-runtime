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

// Package model holds the data structures of the kernel: packages, domains,
// tables, extensions and the schema that combines all installed packages.
package model

import (
	"encoding/json"
	"time"

	"github.com/tim-riep/go-runtime/rterr"
)

// PackageID identifies a package. It consists of upper-case letters and
// digits and starts with a letter. The empty ID denotes the kernel.
type PackageID string

// Kernel is the owner of the kernel's native objects.
const Kernel PackageID = ""

// String returns the ID, or "kernel" for the kernel owner.
func (id PackageID) String() string {
	if id == Kernel {
		return "kernel"
	}
	return string(id)
}

// BaseType is the technical type of a domain.
type BaseType string

// Base types.
const (
	Char      BaseType = "CHAR"      // string with a maximum length
	String    BaseType = "STRING"    // string without length limit
	Int4      BaseType = "INT4"      // 32-bit integer
	Int8      BaseType = "INT8"      // 64-bit integer
	Dec       BaseType = "DEC"       // exact decimal with precision and scale
	Date      BaseType = "DATE"      // calendar date
	Timestamp BaseType = "TIMESTAMP" // point in time, stored in UTC
	Bool      BaseType = "BOOL"      // truth value
	UUID      BaseType = "UUID"      // UUID
	Blob      BaseType = "BLOB"      // binary data
)

// BaseTypes lists all base types.
var BaseTypes = []BaseType{Char, String, Int4, Int8, Dec, Date, Timestamp, Bool, UUID, Blob}

// Valid reports whether t is a known base type.
func (t BaseType) Valid() bool {
	for _, b := range BaseTypes {
		if t == b {
			return true
		}
	}
	return false
}

// HasLength reports whether the type takes a length.
func (t BaseType) HasLength() bool { return t == Char || t == Dec }

// AllowsFixedValues reports whether domains of this type may restrict their
// values to a fixed list.
func (t BaseType) AllowsFixedValues() bool { return t == Char || t == Int4 || t == Int8 }

// DataClass tells what kind of data a table holds.
type DataClass string

// Data classes.
const (
	// Configuration defines the behavior of the application. It is set up
	// once and rarely changes during operation.
	Configuration DataClass = "configuration"
	// Application holds master and transactional data of the running
	// business.
	Application DataClass = "application"
	// RuntimeConfiguration is configuration maintained directly during
	// operation, e.g. exchange rates or number range states.
	RuntimeConfiguration DataClass = "runtimeConfiguration"
	// System is reserved for the native tables of the kernel.
	System DataClass = "system"
)

// DataClasses lists all data classes.
var DataClasses = []DataClass{Configuration, Application, RuntimeConfiguration, System}

// Valid reports whether c is a known data class.
func (c DataClass) Valid() bool {
	for _, d := range DataClasses {
		if c == d {
			return true
		}
	}
	return false
}

// IsConfiguration reports whether tables of the class may form settings.
func (c DataClass) IsConfiguration() bool { return c == Configuration || c == RuntimeConfiguration }

// FixedValue is one allowed value of a domain.
type FixedValue struct {
	Value       string `json:"value"` // canonical text form
	Description string `json:"description,omitempty"`
}

// Domain describes a technical type.
type Domain struct {
	Name        string         `json:"name"`
	Owner       PackageID      `json:"owner"`
	Description string         `json:"description,omitempty"`
	Type        BaseType       `json:"type"`
	Length      int            `json:"length,omitempty"`   // CHAR: max characters, DEC: precision
	Decimals    int            `json:"decimals,omitempty"` // DEC: scale
	Values      []FixedValue   `json:"values,omitempty"`
	Src         rterr.Location `json:"-"`
}

// Maximum lengths of data element labels in characters.
const (
	MaxShortLabel  = 10
	MaxMediumLabel = 20
	MaxLongLabel   = 40
	MaxHeading     = 40
)

// DataElementText holds the labels of a data element in one language.
type DataElementText struct {
	Short       string `json:"short"`
	Medium      string `json:"medium"`
	Long        string `json:"long"`
	Heading     string `json:"heading"` // column heading
	Description string `json:"description,omitempty"`
}

// DataElement gives a domain a meaning: it carries the field labels in
// every language. Fields reference either a domain or a data element.
type DataElement struct {
	Name   string                     `json:"name"`
	Owner  PackageID                  `json:"owner"`
	Domain string                     `json:"domain"`
	Texts  map[string]DataElementText `json:"texts"` // by language code
	Src    rterr.Location             `json:"-"`
}

// Languages returns the languages of the texts in sorted order.
func (d *DataElement) Languages() []string { return sortedKeys(d.Texts) }

// FieldOrigin tells where a physical column comes from.
type FieldOrigin string

// Field origins.
const (
	OriginTable     FieldOrigin = "TABLE"     // declared by the table
	OriginExtension FieldOrigin = "EXTENSION" // added by an extension
	OriginTenant    FieldOrigin = "TENANT"    // added by the kernel for tenant-specific tables
)

// Field is a column of a table.
type Field struct {
	Name        string      `json:"name"`      // physical name, with namespace for extension fields
	LocalName   string      `json:"localName"` // name as declared in the package
	Description string      `json:"description,omitempty"`
	Owner       PackageID   `json:"owner"`
	Origin      FieldOrigin `json:"origin"`
	// Domain is the domain of the field. For fields with a data element it
	// is empty in Schema.Tables and set to the data element's domain by
	// Schema.Physical.
	Domain      string  `json:"domain"`
	DataElement string  `json:"dataElement,omitempty"`
	NotNull     bool    `json:"notNull"`
	Default     *string `json:"default,omitempty"` // canonical text form

	// DefaultJSON holds the default as written in the package until the
	// validator converts it into Default.
	DefaultJSON json.RawMessage `json:"-"`
	Src         rterr.Location  `json:"-"`
}

// ColumnPair maps a field of a foreign key to a field of the target table.
type ColumnPair struct {
	Field      string `json:"field"`
	References string `json:"references"`
}

// ForeignKey references the primary key of a target table.
type ForeignKey struct {
	Name    string         `json:"name"` // physical name, with namespace for extension keys
	Owner   PackageID      `json:"owner"`
	Origin  FieldOrigin    `json:"origin"`
	Target  string         `json:"target"`
	Columns []ColumnPair   `json:"columns"`
	Src     rterr.Location `json:"-"`
}

// FieldNames returns the source fields of the key.
func (fk *ForeignKey) FieldNames() []string {
	out := make([]string, len(fk.Columns))
	for i, c := range fk.Columns {
		out[i] = c.Field
	}
	return out
}

// Table is a table definition. In a Schema it holds only the fields declared
// by its owner; Schema.Physical merges extension and tenant fields.
type Table struct {
	Name           string         `json:"name"`
	Owner          PackageID      `json:"owner"`
	Description    string         `json:"description,omitempty"`
	TenantSpecific bool           `json:"tenantSpecific"`
	DataClass      DataClass      `json:"dataClass"`
	Fields         []Field        `json:"fields"`
	PrimaryKey     []string       `json:"primaryKey"`
	ForeignKeys    []ForeignKey   `json:"foreignKeys,omitempty"`
	Src            rterr.Location `json:"-"`
}

// Field returns the field with the given physical name.
func (t *Table) Field(name string) *Field {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// ForeignKey returns the foreign key with the given name.
func (t *Table) ForeignKey(name string) *ForeignKey {
	for i := range t.ForeignKeys {
		if t.ForeignKeys[i].Name == name {
			return &t.ForeignKeys[i]
		}
	}
	return nil
}

// IsKey reports whether the field is part of the primary key.
func (t *Table) IsKey(field string) bool {
	for _, k := range t.PrimaryKey {
		if k == field {
			return true
		}
	}
	return false
}

// Extension adds fields and foreign keys to a table of another package.
type Extension struct {
	Package     PackageID      `json:"package"`
	Table       string         `json:"table"`
	Description string         `json:"description,omitempty"`
	Fields      []Field        `json:"fields"`
	ForeignKeys []ForeignKey   `json:"foreignKeys,omitempty"`
	Src         rterr.Location `json:"-"`
}

// Key returns the identity of the extension.
func (e *Extension) Key() ExtensionKey { return ExtensionKey{Package: e.Package, Table: e.Table} }

// ExtensionKey identifies an extension: one package extends one table at
// most once.
type ExtensionKey struct {
	Package PackageID
	Table   string
}

// Dependency is a requirement of a package on another package.
type Dependency struct {
	ID         PackageID `json:"id"`
	Constraint string    `json:"version,omitempty"` // empty means any version
}

// Manifest describes a package.
type Manifest struct {
	ID          PackageID    `json:"id"`
	Version     string       `json:"version"`
	Description string       `json:"description,omitempty"`
	Requires    []Dependency `json:"requires,omitempty"`
}

// Package is a loaded package.
type Package struct {
	Manifest          Manifest
	Domains           []Domain
	DataElements      []DataElement
	Tables            []Table
	Extensions        []Extension
	Settings          []Setting
	SettingExtensions []SettingExtension
	Origin            string // where the package was loaded from
}

// PackageInfo is an installed package.
type PackageInfo struct {
	Manifest    Manifest  `json:"manifest"`
	InstalledAt time.Time `json:"installedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
