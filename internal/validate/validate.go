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

// Package validate performs the semantic checks on a complete schema.
//
// The kernel validates the schema that an operation would produce as a
// whole. A change that leaves any object of any package invalid, including
// objects of packages that were not changed, is therefore rejected.
package validate

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Options control validation.
type Options struct {
	// Languages are the languages set up in the database. Texts in other
	// languages are reported as rterr.MissingLanguages. Nil skips the check.
	Languages map[string]bool
	// MaxAssetSize is the maximum size of one asset in bytes; 0 means no
	// limit.
	MaxAssetSize int64
}

// Schema checks s and converts field defaults written in packages into
// their canonical form (Field.DefaultJSON into Field.Default).
func Schema(s *model.Schema, opts Options) error {
	v := &validator{s: s, opts: opts, missing: map[string][]string{}}
	for _, name := range s.DomainNames() {
		v.domain(s.Domains[name])
	}
	for _, name := range s.DataElementNames() {
		v.dataElement(s.DataElements[name])
	}
	for _, name := range s.TableNames() {
		v.table(s.Tables[name])
	}
	for _, e := range s.SortedExtensions() {
		v.extension(e)
	}
	v.settings()
	v.assetRepositories()
	v.reportMissingLanguages()
	return v.errs.Err()
}

type validator struct {
	s       *model.Schema
	opts    Options
	errs    rterr.List
	missing map[string][]string // missing language -> objects using it
}

// languages checks the languages of an object's texts. English is
// mandatory.
func (v *validator) languages(object string, loc rterr.Location, langs []string) {
	if !slices.Contains(langs, model.DefaultLanguage) {
		v.fail(rterr.CodeText, loc.At("/texts"), object, "texts in English (%s) are mandatory", model.DefaultLanguage)
	}
	v.knownLanguages(object, langs)
}

// knownLanguages records languages of an object that are not set up.
func (v *validator) knownLanguages(object string, langs []string) {
	if v.opts.Languages == nil {
		return
	}
	for _, l := range langs {
		if !v.opts.Languages[l] {
			v.missing[l] = append(v.missing[l], object)
		}
	}
}

func (v *validator) reportMissingLanguages() {
	if len(v.missing) == 0 {
		return
	}
	err := &rterr.MissingLanguages{}
	seen := map[string]bool{}
	for l, objs := range v.missing {
		err.Languages = append(err.Languages, l)
		for _, o := range objs {
			if !seen[o] {
				seen[o] = true
				err.Objects = append(err.Objects, o)
			}
		}
	}
	slices.Sort(err.Languages)
	slices.Sort(err.Objects)
	v.errs = append(v.errs, err)
}

func (v *validator) fail(code rterr.Code, loc rterr.Location, object, format string, args ...any) {
	v.errs = append(v.errs, &rterr.ValidationError{Code: code, Location: loc, Object: object, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) identifier(name string, loc rterr.Location, object string) {
	if n := len(name); n > model.MaxIdentifierBytes {
		v.fail(rterr.CodeNameTooLong, loc, object, "identifier %q has %d bytes, the limit is %d", name, n, model.MaxIdentifierBytes)
	}
}

func (v *validator) domain(d *model.Domain) {
	obj := "domain " + d.Name
	v.identifier(d.Name, d.Src.At("/name"), obj)
	switch {
	case !d.Type.Valid():
		v.fail(rterr.CodeTypeParams, d.Src.At("/type"), obj, "unknown base type %q", d.Type)
	case d.Type == model.Char && (d.Length < 1 || d.Length > model.MaxCharLength):
		v.fail(rterr.CodeTypeParams, d.Src.At("/length"), obj, "CHAR length must be between 1 and %d", model.MaxCharLength)
	case d.Type == model.Char && d.Decimals != 0:
		v.fail(rterr.CodeTypeParams, d.Src.At("/decimals"), obj, "CHAR has no decimals")
	case d.Type == model.Dec && (d.Length < 1 || d.Length > model.MaxDecimalPrecision):
		v.fail(rterr.CodeTypeParams, d.Src.At("/length"), obj, "DEC length must be between 1 and %d", model.MaxDecimalPrecision)
	case d.Type == model.Dec && (d.Decimals < 0 || d.Decimals > d.Length):
		v.fail(rterr.CodeTypeParams, d.Src.At("/decimals"), obj, "DEC decimals must be between 0 and the length %d", d.Length)
	case !d.Type.HasLength() && (d.Length != 0 || d.Decimals != 0):
		v.fail(rterr.CodeTypeParams, d.Src, obj, "%s takes no length or decimals", d.Type)
	}
	if len(d.Values) > 0 && !d.Type.AllowsFixedValues() {
		v.fail(rterr.CodeFixedValue, d.Src.At("/values"), obj, "fixed values are only allowed for CHAR, INT4 and INT8")
		return
	}
	seen := map[string]bool{}
	for i, fv := range d.Values {
		loc := d.Src.At(fmt.Sprintf("/values/%d/value", i))
		if canon, err := d.Canonical(fv.Value); err != nil || canon != fv.Value {
			v.fail(rterr.CodeFixedValue, loc, obj, "invalid fixed value %q", fv.Value)
		}
		if seen[fv.Value] {
			v.fail(rterr.CodeFixedValue, loc, obj, "fixed value %q is listed twice", fv.Value)
		}
		seen[fv.Value] = true
	}
}

// fieldDomain resolves the domain of a field declared by owner, directly or
// through its data element, and reports problems on the way.
func (v *validator) fieldDomain(f *model.Field, owner model.PackageID, obj string) *model.Domain {
	switch {
	case f.Domain != "" && f.DataElement != "":
		v.fail(rterr.CodeDataElement, f.Src, obj, "a field references either a domain or a data element, not both")
		return nil
	case f.Domain == "" && f.DataElement == "":
		v.fail(rterr.CodeDataElement, f.Src, obj, "a field needs a domain or a data element")
		return nil
	case f.DataElement != "":
		de, ok := v.s.DataElements[f.DataElement]
		if !ok {
			v.fail(rterr.CodeDataElement, f.Src.At("/dataElement"), obj, "data element %s does not exist", f.DataElement)
			return nil
		}
		if !v.s.Visible(owner, de.Owner) {
			v.fail(rterr.CodeNotVisible, f.Src.At("/dataElement"), obj, "data element %s belongs to package %s, which %s does not require", de.Name, de.Owner, owner)
		}
		// Problems of the data element itself are reported for the data
		// element.
		return v.s.Domains[de.Domain]
	}
	d, ok := v.s.Domains[f.Domain]
	if !ok {
		v.fail(rterr.CodeUnknownDomain, f.Src.At("/domain"), obj, "domain %s does not exist", f.Domain)
		return nil
	}
	if !v.s.Visible(owner, d.Owner) {
		v.fail(rterr.CodeNotVisible, f.Src.At("/domain"), obj, "domain %s belongs to package %s, which %s does not require", d.Name, d.Owner, owner)
	}
	return d
}

// dataElement checks a data element and its texts.
func (v *validator) dataElement(de *model.DataElement) {
	obj := "data element " + de.Name
	v.identifier(de.Name, de.Src.At("/name"), obj)
	if model.IsKernelName(de.Name) != (de.Owner == model.Kernel) {
		v.fail(rterr.CodeName, de.Src.At("/name"), obj, "only kernel data elements may use the kernel namespace")
	}
	d, ok := v.s.Domains[de.Domain]
	switch {
	case !ok:
		v.fail(rterr.CodeUnknownDomain, de.Src.At("/domain"), obj, "domain %s does not exist", de.Domain)
	case !v.s.Visible(de.Owner, d.Owner):
		v.fail(rterr.CodeNotVisible, de.Src.At("/domain"), obj, "domain %s belongs to package %s, which %s does not require", d.Name, d.Owner, de.Owner)
	}
	for _, lang := range de.Languages() {
		t := de.Texts[lang]
		loc := de.Src.At("/texts/" + lang)
		for _, l := range []struct {
			name, text string
			max        int
		}{{"short", t.Short, model.MaxShortLabel}, {"medium", t.Medium, model.MaxMediumLabel}, {"long", t.Long, model.MaxLongLabel}, {"heading", t.Heading, model.MaxHeading}} {
			if n := utf8.RuneCountInString(l.text); n == 0 || n > l.max {
				v.fail(rterr.CodeText, loc.At("/"+l.name), obj, "%s label in %s must have 1 to %d characters", l.name, lang, l.max)
			}
		}
	}
	v.languages(obj, de.Src, de.Languages())
}

// field checks a field declared by owner and normalizes its default.
func (v *validator) field(table string, f *model.Field, owner model.PackageID) {
	obj := "field " + table + "." + f.Name
	v.identifier(f.Name, f.Src.At("/name"), obj)
	v.identifier(model.CheckName(table, f.Name), f.Src.At("/name"), obj+" (check constraint)")
	if model.IsKernelName(f.Name) && owner != model.Kernel {
		v.fail(rterr.CodeName, f.Src.At("/name"), obj, "names in the kernel namespace are reserved")
	}
	d := v.fieldDomain(f, owner, obj)
	if d == nil {
		return
	}
	if len(f.DefaultJSON) > 0 {
		canon, err := d.ParseJSON(f.DefaultJSON)
		if err != nil {
			v.fail(rterr.CodeDefault, f.Src.At("/default"), obj, "invalid default: %v", err)
			return
		}
		f.Default = &canon
		f.DefaultJSON = nil
	}
	if f.Default != nil {
		if canon, err := d.Canonical(*f.Default); err != nil || canon != *f.Default {
			v.fail(rterr.CodeDefault, f.Src.At("/default"), obj, "invalid default %q for domain %s", *f.Default, d.Name)
		} else if !d.HasValue(*f.Default) {
			v.fail(rterr.CodeDefault, f.Src.At("/default"), obj, "default %q is not a fixed value of domain %s", *f.Default, d.Name)
		}
	}
}

func (v *validator) table(t *model.Table) {
	obj := "table " + t.Name
	v.identifier(t.Name, t.Src.At("/name"), obj)
	v.identifier(model.PrimaryKeyName(t.Name), t.Src.At("/name"), obj+" (primary key constraint)")
	if model.IsKernelName(t.Name) != (t.Owner == model.Kernel) {
		v.fail(rterr.CodeName, t.Src.At("/name"), obj, "only kernel tables may use the kernel namespace")
	}
	switch {
	case !t.DataClass.Valid():
		v.fail(rterr.CodeDataClass, t.Src.At("/dataClass"), obj, "data class %q is not one of configuration, application, runtimeConfiguration", t.DataClass)
	case t.Owner == model.Kernel && t.DataClass != model.System:
		v.fail(rterr.CodeDataClass, t.Src.At("/dataClass"), obj, "kernel tables have the data class system")
	case t.Owner != model.Kernel && t.DataClass == model.System:
		v.fail(rterr.CodeDataClass, t.Src.At("/dataClass"), obj, "the data class system is reserved for the kernel")
	}
	if len(t.Fields) == 0 {
		v.fail(rterr.CodeUnknownField, t.Src.At("/fields"), obj, "table has no fields")
	}
	names := map[string]bool{}
	for i := range t.Fields {
		f := &t.Fields[i]
		if names[f.Name] {
			v.fail(rterr.CodeDuplicate, f.Src.At("/name"), obj, "field %s is declared twice", f.Name)
		}
		names[f.Name] = true
		v.field(t.Name, f, t.Owner)
	}
	if t.TenantSpecific {
		v.identifier(model.ForeignKeyName(t.Name, model.TenantForeignKey), t.Src.At("/name"), obj+" (tenant foreign key)")
		v.identifier(model.CheckName(t.Name, model.TenantField), t.Src.At("/name"), obj+" (tenant check constraint)")
	}
	if len(t.PrimaryKey) == 0 {
		v.fail(rterr.CodePrimaryKey, t.Src.At("/primaryKey"), obj, "table has no primary key")
	}
	seen := map[string]bool{}
	for i, k := range t.PrimaryKey {
		loc := t.Src.At(fmt.Sprintf("/primaryKey/%d", i))
		switch {
		case !names[k]:
			v.fail(rterr.CodePrimaryKey, loc, obj, "key field %s is not a field of the table", k)
		case seen[k]:
			v.fail(rterr.CodePrimaryKey, loc, obj, "key field %s is listed twice", k)
		}
		seen[k] = true
	}
	fkNames := map[string]bool{}
	for i := range t.ForeignKeys {
		fk := &t.ForeignKeys[i]
		if fkNames[fk.Name] {
			v.fail(rterr.CodeDuplicate, fk.Src.At("/name"), obj, "foreign key %s is declared twice", fk.Name)
		}
		fkNames[fk.Name] = true
		v.foreignKey(t, fk, t.Owner, t.Fields)
	}
}

func (v *validator) extension(e *model.Extension) {
	obj := "extension of " + e.Table + " by " + string(e.Package)
	t, ok := v.s.Tables[e.Table]
	if !ok {
		v.fail(rterr.CodeUnknownTable, e.Src.At("/table"), obj, "table %s does not exist", e.Table)
		return
	}
	switch {
	case t.Owner == e.Package:
		v.fail(rterr.CodeExtension, e.Src.At("/table"), obj, "a package cannot extend its own table")
	case t.Owner == model.Kernel && t.Name != model.TenantTable:
		v.fail(rterr.CodeReservedObject, e.Src.At("/table"), obj, "kernel table %s cannot be extended", t.Name)
	case !v.s.Visible(e.Package, t.Owner):
		v.fail(rterr.CodeNotVisible, e.Src.At("/table"), obj, "table %s belongs to package %s, which %s does not require", t.Name, t.Owner, e.Package)
	}
	names := map[string]bool{}
	for _, f := range t.Fields {
		names[f.Name] = true
	}
	for i := range e.Fields {
		f := &e.Fields[i]
		if names[f.Name] {
			v.fail(rterr.CodeDuplicate, f.Src.At("/name"), obj, "field %s is declared twice", f.Name)
		}
		names[f.Name] = true
		v.field(t.Name, f, e.Package)
		if f.NotNull && f.Default == nil && len(f.DefaultJSON) == 0 {
			v.fail(rterr.CodeExtension, f.Src.At("/notNull"), obj, "extension field %s must be nullable or have a default, the table may already contain rows", f.Name)
		}
	}
	for i := range e.ForeignKeys {
		v.foreignKey(t, &e.ForeignKeys[i], e.Package, e.Fields)
	}
}

// foreignKey checks a key declared by owner on table src. The key may use
// only the fields in own: the table's fields for table keys, the
// extension's fields for extension keys.
func (v *validator) foreignKey(src *model.Table, fk *model.ForeignKey, owner model.PackageID, own []model.Field) {
	obj := "foreign key " + src.Name + "." + fk.Name
	v.identifier(model.ForeignKeyName(src.Name, fk.Name), fk.Src.At("/name"), obj+" (constraint)")
	target, ok := v.s.Tables[fk.Target]
	if !ok {
		v.fail(rterr.CodeUnknownTable, fk.Src.At("/table"), obj, "target table %s does not exist", fk.Target)
		return
	}
	switch {
	case target.Owner == model.Kernel && owner != model.Kernel && target.Name != model.TenantTable:
		v.fail(rterr.CodeReservedObject, fk.Src.At("/table"), obj, "kernel table %s cannot be referenced", target.Name)
		return
	case !v.s.Visible(owner, target.Owner):
		v.fail(rterr.CodeNotVisible, fk.Src.At("/table"), obj, "table %s belongs to package %s, which %s does not require", target.Name, target.Owner, owner)
		return
	case !src.TenantSpecific && target.TenantSpecific:
		v.fail(rterr.CodeTenant, fk.Src.At("/table"), obj, "a table that is not tenant-specific cannot reference the tenant-specific table %s", target.Name)
		return
	}
	fields := map[string]*model.Field{}
	for i := range own {
		fields[own[i].Name] = &own[i]
	}
	used := map[string]bool{}
	refs := map[string]bool{}
	for i, c := range fk.Columns {
		loc := fk.Src.At(fmt.Sprintf("/fields/%d", i))
		f, ok := fields[c.Field]
		if !ok {
			v.fail(rterr.CodeUnknownField, loc.At("/field"), obj, "%s is not a field declared here", c.Field)
			continue
		}
		if used[c.Field] {
			v.fail(rterr.CodeForeignKey, loc.At("/field"), obj, "field %s is used twice", c.Field)
		}
		used[c.Field] = true
		tf := target.Field(c.References)
		if tf == nil || !target.IsKey(c.References) {
			v.fail(rterr.CodeForeignKey, loc.At("/references"), obj, "%s is not a key field of %s", c.References, target.Name)
			continue
		}
		if refs[c.References] {
			v.fail(rterr.CodeForeignKey, loc.At("/references"), obj, "key field %s is referenced twice", c.References)
		}
		refs[c.References] = true
		sd, td := v.s.Domains[v.s.FieldDomain(f)], v.s.Domains[v.s.FieldDomain(tf)]
		if sd != nil && td != nil && !Compatible(sd, td) {
			v.fail(rterr.CodeTypeMismatch, loc, obj, "%s has type %s, %s.%s has type %s", f.Name, sd.TypeString(), target.Name, tf.Name, td.TypeString())
		}
	}
	for _, k := range target.PrimaryKey {
		if !refs[k] && !slices.ContainsFunc(fk.Columns, func(c model.ColumnPair) bool { return c.References == k }) {
			v.fail(rterr.CodeForeignKey, fk.Src.At("/fields"), obj, "key field %s of %s is not mapped; a foreign key must cover the whole primary key", k, target.Name)
		}
	}
}

// Compatible reports whether a field of domain src may reference a field of
// domain dst: same base type, CHAR not longer than the target, DEC with the
// same precision and scale.
func Compatible(src, dst *model.Domain) bool {
	if src.Type != dst.Type {
		return false
	}
	switch src.Type {
	case model.Char:
		return src.Length <= dst.Length
	case model.Dec:
		return src.Length == dst.Length && src.Decimals == dst.Decimals
	}
	return true
}
