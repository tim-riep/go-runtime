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

// Package repo reads and writes the repository: the kernel tables that
// describe all installed packages and their objects.
//
// The repository stores the physical layout, i.e. every column including
// tenant and extension fields with its owner and origin, so it can be
// queried directly. Loading it reconstructs the per-owner definitions of
// model.Schema.
package repo

import (
	"cmp"
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/model"
)

// Kernel table names.
const (
	TPackages          = model.KernelNamespace + "PACKAGES"
	TPackageDeps       = model.KernelNamespace + "PACKAGE_DEPS"
	TPackageCaps       = model.KernelNamespace + "PACKAGE_CAPABILITIES"
	TDomains           = model.KernelNamespace + "DOMAINS"
	TDomainValues      = model.KernelNamespace + "DOMAIN_VALUES"
	TTables            = model.KernelNamespace + "TABLES"
	TExtensions        = model.KernelNamespace + "EXTENSIONS"
	TFields            = model.KernelNamespace + "FIELDS"
	TForeignKeys       = model.KernelNamespace + "FOREIGN_KEYS"
	TFKFields          = model.KernelNamespace + "FK_FIELDS"
	TMigrations        = model.KernelNamespace + "MIGRATIONS"
	TLog               = model.KernelNamespace + "LOG"
	TLogPackages       = model.KernelNamespace + "LOG_PACKAGES"
	TLogStatements     = model.KernelNamespace + "LOG_STATEMENTS"
	TLanguages         = model.KernelNamespace + "LANGUAGES"
	TDataElements      = model.KernelNamespace + "DATA_ELEMENTS"
	TDataElementTexts  = model.KernelNamespace + "DATA_ELEMENT_TEXTS"
	TSettings          = model.KernelNamespace + "SETTINGS"
	TSettingTexts      = model.KernelNamespace + "SETTING_TEXTS"
	TSettingExtensions = model.KernelNamespace + "SETTING_EXTENSIONS"
	TSettingTables     = model.KernelNamespace + "SETTING_TABLES"
)

// Repo accesses the kernel tables of one kernel schema version with a
// dialect. Tables and columns that the version does not have are skipped
// when reading and writing, so older repositories can be read during a
// migration.
type Repo struct {
	d       dialect.Dialect
	version int
}

// New returns a Repo for the newest kernel version.
func New(d dialect.Dialect) *Repo { return &Repo{d: d, version: kernelschema.Latest()} }

// At returns a Repo for the given kernel version.
func (r *Repo) At(version int) *Repo { return &Repo{d: r.d, version: version} }

var (
	layoutMu sync.Mutex
	layouts  = map[int]*model.Physical{}
)

// layout returns the physical layout of the kernel tables of r's version,
// used to encode and decode their values.
func (r *Repo) layout() (*model.Physical, error) {
	layoutMu.Lock()
	defer layoutMu.Unlock()
	if l, ok := layouts[r.version]; ok {
		return l, nil
	}
	s, err := kernelschema.Schema(r.version)
	if err != nil {
		return nil, err
	}
	layouts[r.version] = s.Physical()
	return layouts[r.version], nil
}

// has reports whether the version has the table and, if given, the column.
func (r *Repo) has(table, column string) bool {
	l, err := r.layout()
	if err != nil {
		return false
	}
	t, ok := l.Tables[table]
	return ok && (column == "" || t.Field(column) != nil)
}

type row map[string]any

func (r *Repo) domain(table, column string) (*model.Domain, error) {
	l, err := r.layout()
	if err != nil {
		return nil, err
	}
	t, ok := l.Tables[table]
	if !ok {
		return nil, fmt.Errorf("unknown kernel table %s", table)
	}
	f := t.Field(column)
	if f == nil {
		return nil, fmt.Errorf("unknown column %s.%s", table, column)
	}
	return l.Domain(f), nil
}

// insert writes one row into a kernel table. Rows of tables and values of
// columns that the version does not have are dropped.
func (r *Repo) insert(ctx context.Context, q dialect.Querier, table string, values row) error {
	if !r.has(table, "") {
		return nil
	}
	cols := make([]string, 0, len(values))
	for c := range values {
		if r.has(table, c) {
			cols = append(cols, c)
		}
	}
	slices.Sort(cols)
	args := make([]any, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		d, err := r.domain(table, c)
		if err != nil {
			return err
		}
		if args[i], err = r.d.Encode(d, values[c]); err != nil {
			return fmt.Errorf("%s.%s: %w", table, c, err)
		}
		marks[i] = r.d.Placeholder(i + 1)
	}
	stmt := "INSERT INTO " + r.d.QuoteIdent(table) + " (" + dialect.QuoteList(cols) + ") VALUES (" + strings.Join(marks, ", ") + ")"
	if _, err := q.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("writing %s: %w", table, r.d.ClassifyError(err))
	}
	return nil
}

// selectRows reads the given columns of a kernel table, decoded into Go
// values, in the given order. Columns the version does not have are
// returned as nil; a table it does not have yields no rows.
func (r *Repo) selectRows(ctx context.Context, q dialect.Querier, table string, cols []string, orderBy ...string) ([]row, error) {
	return r.selectWhere(ctx, q, table, cols, nil, orderBy...)
}

// selectWhere is selectRows restricted to rows whose columns equal the
// values of filter.
func (r *Repo) selectWhere(ctx context.Context, q dialect.Querier, table string, cols []string, filter row, orderBy ...string) ([]row, error) {
	if !r.has(table, "") {
		return nil, nil
	}
	var present []string
	for _, c := range cols {
		if r.has(table, c) {
			present = append(present, c)
		}
	}
	stmt := "SELECT " + dialect.QuoteList(present) + " FROM " + r.d.QuoteIdent(table)
	var args []any
	if len(filter) > 0 {
		var conds []string
		for _, c := range slices.Sorted(maps.Keys(filter)) {
			args = append(args, filter[c])
			conds = append(conds, r.d.QuoteIdent(c)+" = "+r.d.Placeholder(len(args)))
		}
		stmt += " WHERE " + strings.Join(conds, " AND ")
	}
	if len(orderBy) > 0 {
		stmt += " ORDER BY " + dialect.QuoteList(orderBy)
	}
	doms := make([]*model.Domain, len(present))
	for i, c := range present {
		d, err := r.domain(table, c)
		if err != nil {
			return nil, err
		}
		doms[i] = d
	}
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", table, err)
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		raw := make([]any, len(present))
		ptrs := make([]any, len(present))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		rec := row{}
		for i, c := range present {
			v, err := r.d.Decode(doms[i], raw[i])
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", table, c, err)
			}
			rec[c] = v
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// opt reads a nullable text column: NULL is None, "" is Some("").
func opt(v any) model.Optional[string] {
	if s, ok := v.(string); ok {
		return model.Some(s)
	}
	return model.None[string]()
}

// val writes an optional text: None is NULL, Some("") is "".
func val(o model.Optional[string]) any {
	if s, ok := o.Get(); ok {
		return s
	}
	return nil
}

func optBool(v any) model.Optional[bool] {
	if b, ok := v.(bool); ok {
		return model.Some(b)
	}
	return model.None[bool]()
}

func valBool(o model.Optional[bool]) any {
	if b, ok := o.Get(); ok {
		return b
	}
	return nil
}

// owner reads an owner column, which is NULL for kernel objects.
func owner(v any) model.PackageID {
	if s, ok := v.(string); ok {
		return model.PackageID(s)
	}
	return model.Kernel
}

// ownerValue writes an owner column: NULL for the kernel.
func ownerValue(id model.PackageID) any {
	if id == model.Kernel {
		return nil
	}
	return string(id)
}

// readDefault reads DEFAULT_KIND and DEFAULT_VALUE. Repositories without
// DEFAULT_KIND knew only values.
func readDefault(kind, value any) model.Default {
	switch k, _ := kind.(string); k {
	case "NULL":
		return model.NullDefault()
	case "VALUE":
		v, _ := value.(string)
		return model.ValueDefault(v)
	case "":
		if v, ok := value.(string); ok {
			return model.ValueDefault(v)
		}
	}
	return model.NoDefault()
}

// writeDefault returns the values of DEFAULT_KIND and DEFAULT_VALUE.
func writeDefault(d model.Default) (any, any) {
	if d.Kind == model.DefaultValue {
		return d.Kind.String(), d.Value
	}
	return d.Kind.String(), nil
}

func num(v any) int {
	n, _ := v.(int64)
	return int(n)
}

func flag(v any) bool {
	b, _ := v.(bool)
	return b
}

func tm(v any) time.Time {
	t, _ := v.(time.Time)
	return t
}

func nullableInt(n int, ok bool) any {
	if !ok {
		return nil
	}
	return int64(n)
}

// KernelVersion returns the applied version of the kernel tables, or 0 if
// the database has not been initialized.
func (r *Repo) KernelVersion(ctx context.Context, q dialect.Querier) (int, error) {
	ok, err := r.d.TableExists(ctx, q, TMigrations)
	if err != nil || !ok {
		return 0, err
	}
	var v int
	err = q.QueryRowContext(ctx, "SELECT COALESCE(MAX("+r.d.QuoteIdent("VERSION")+"), 0) FROM "+r.d.QuoteIdent(TMigrations)).Scan(&v)
	return v, err
}

// AddMigration records an applied kernel version.
func (r *Repo) AddMigration(ctx context.Context, q dialect.Querier, version int, at time.Time) error {
	return r.insert(ctx, q, TMigrations, row{"VERSION": int64(version), "APPLIED_AT": at})
}

// Load reads the complete schema from the repository.
func (r *Repo) Load(ctx context.Context, q dialect.Querier) (*model.Schema, error) {
	s := model.NewSchema()

	pkgs, err := r.selectRows(ctx, q, TPackages, []string{"PACKAGE_ID", "VERSION", "DESCRIPTION", "INSTALLED_AT", "UPDATED_AT", "LANGUAGE_VERSION", "KERNEL_PACKAGE"})
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		id := owner(p["PACKAGE_ID"])
		s.Packages[id] = &model.PackageInfo{
			Manifest: model.Manifest{ID: id, Version: str(p["VERSION"]), Description: opt(p["DESCRIPTION"]), LanguageVersion: num(p["LANGUAGE_VERSION"]),
				KernelPackage: flag(p["KERNEL_PACKAGE"])},
			InstalledAt: tm(p["INSTALLED_AT"]), UpdatedAt: tm(p["UPDATED_AT"]),
		}
	}
	deps, err := r.selectRows(ctx, q, TPackageDeps, []string{"PACKAGE_ID", "REQUIRED_ID", "VERSION_CONSTRAINT"}, "PACKAGE_ID", "REQUIRED_ID")
	if err != nil {
		return nil, err
	}
	for _, d := range deps {
		if p := s.Packages[owner(d["PACKAGE_ID"])]; p != nil {
			p.Manifest.Requires = append(p.Manifest.Requires, model.Dependency{ID: model.PackageID(str(d["REQUIRED_ID"])), Constraint: opt(d["VERSION_CONSTRAINT"])})
		}
	}
	caps, err := r.selectRows(ctx, q, TPackageCaps, []string{"PACKAGE_ID", "CAPABILITY"}, "PACKAGE_ID", "CAPABILITY")
	if err != nil {
		return nil, err
	}
	for _, c := range caps {
		if p := s.Packages[owner(c["PACKAGE_ID"])]; p != nil {
			p.Manifest.Capabilities = append(p.Manifest.Capabilities, str(c["CAPABILITY"]))
		}
	}

	doms, err := r.selectRows(ctx, q, TDomains, []string{"DOMAIN_NAME", "PACKAGE_ID", "DESCRIPTION", "BASE_TYPE", "LENGTH", "DECIMALS",
		"DEFAULT_KIND", "DEFAULT_VALUE", "ALLOW_EMPTY"})
	if err != nil {
		return nil, err
	}
	for _, d := range doms {
		name := str(d["DOMAIN_NAME"])
		s.Domains[name] = &model.Domain{Name: name, Owner: owner(d["PACKAGE_ID"]), Description: opt(d["DESCRIPTION"]),
			Type: model.BaseType(str(d["BASE_TYPE"])), Length: num(d["LENGTH"]), Decimals: num(d["DECIMALS"]),
			Default: readDefault(d["DEFAULT_KIND"], d["DEFAULT_VALUE"]), AllowEmpty: optBool(d["ALLOW_EMPTY"])}
	}
	vals, err := r.selectRows(ctx, q, TDomainValues, []string{"DOMAIN_NAME", "VALUE", "DESCRIPTION", "VALUE_NAME"}, "DOMAIN_NAME", "POSITION")
	if err != nil {
		return nil, err
	}
	for _, v := range vals {
		if d := s.Domains[str(v["DOMAIN_NAME"])]; d != nil {
			d.Values = append(d.Values, model.FixedValue{Value: str(v["VALUE"]), Description: opt(v["DESCRIPTION"]), Name: opt(v["VALUE_NAME"])})
		}
	}

	des, err := r.selectRows(ctx, q, TDataElements, []string{"DATA_ELEMENT", "PACKAGE_ID", "DOMAIN_NAME"})
	if err != nil {
		return nil, err
	}
	for _, d := range des {
		name := str(d["DATA_ELEMENT"])
		s.DataElements[name] = &model.DataElement{Name: name, Owner: owner(d["PACKAGE_ID"]), Domain: str(d["DOMAIN_NAME"]),
			Texts: map[string]model.DataElementText{}}
	}
	deTexts, err := r.selectRows(ctx, q, TDataElementTexts, []string{"DATA_ELEMENT", "LANGUAGE", "SHORT_LABEL", "MEDIUM_LABEL", "LONG_LABEL", "HEADING", "DESCRIPTION"})
	if err != nil {
		return nil, err
	}
	for _, t := range deTexts {
		if d := s.DataElements[str(t["DATA_ELEMENT"])]; d != nil {
			d.Texts[str(t["LANGUAGE"])] = model.DataElementText{Short: str(t["SHORT_LABEL"]), Medium: str(t["MEDIUM_LABEL"]),
				Long: str(t["LONG_LABEL"]), Heading: str(t["HEADING"]), Description: opt(t["DESCRIPTION"])}
		}
	}

	tabs, err := r.selectRows(ctx, q, TTables, []string{"TABLE_NAME", "PACKAGE_ID", "DESCRIPTION", "TENANT_SPECIFIC", "DATA_CLASS"})
	if err != nil {
		return nil, err
	}
	provisional := !r.has(TTables, "DATA_CLASS")
	for _, t := range tabs {
		name := str(t["TABLE_NAME"])
		if provisional {
			t["DATA_CLASS"] = string(ProvisionalClass(owner(t["PACKAGE_ID"])))
		}
		s.Tables[name] = &model.Table{Name: name, Owner: owner(t["PACKAGE_ID"]), Description: opt(t["DESCRIPTION"]),
			TenantSpecific: flag(t["TENANT_SPECIFIC"]), DataClass: model.DataClass(str(t["DATA_CLASS"]))}
	}
	exts, err := r.selectRows(ctx, q, TExtensions, []string{"PACKAGE_ID", "TABLE_NAME", "DESCRIPTION"})
	if err != nil {
		return nil, err
	}
	for _, e := range exts {
		ext := &model.Extension{Package: owner(e["PACKAGE_ID"]), Table: str(e["TABLE_NAME"]), Description: opt(e["DESCRIPTION"])}
		s.Extensions[ext.Key()] = ext
	}

	fields, err := r.selectRows(ctx, q, TFields, []string{"TABLE_NAME", "FIELD_NAME", "LOCAL_NAME", "DESCRIPTION", "PACKAGE_ID", "ORIGIN",
		"DOMAIN_NAME", "NOT_NULL", "DEFAULT_KIND", "DEFAULT_VALUE", "KEY_POSITION", "DATA_ELEMENT"}, "TABLE_NAME", "POSITION")
	if err != nil {
		return nil, err
	}
	keys := map[string][]struct {
		pos  int
		name string
	}{}
	for _, f := range fields {
		table := str(f["TABLE_NAME"])
		fld := model.Field{Name: str(f["FIELD_NAME"]), LocalName: str(f["LOCAL_NAME"]), Description: opt(f["DESCRIPTION"]),
			Owner: owner(f["PACKAGE_ID"]), Origin: model.FieldOrigin(str(f["ORIGIN"])), Domain: str(f["DOMAIN_NAME"]),
			NotNull: flag(f["NOT_NULL"]), Default: readDefault(f["DEFAULT_KIND"], f["DEFAULT_VALUE"]),
			DataElement: opt(f["DATA_ELEMENT"])}
		// The repository stores the effective domain; the declaration
		// references only the data element.
		if fld.DataElement.Valid() {
			fld.Domain = ""
		}
		switch fld.Origin {
		case model.OriginTable, model.OriginInclude:
			t := s.Tables[table]
			if t == nil {
				return nil, fmt.Errorf("repository: field %s of unknown table %s", fld.Name, table)
			}
			// Included columns come from the includes of the table; only
			// their key position is part of the declaration.
			if fld.Origin == model.OriginTable {
				t.Fields = append(t.Fields, fld)
			}
			if pos := num(f["KEY_POSITION"]); pos > 0 {
				keys[table] = append(keys[table], struct {
					pos  int
					name string
				}{pos, fld.Name})
			}
		case model.OriginExtension:
			e := s.Extensions[model.ExtensionKey{Package: fld.Owner, Table: table}]
			if e == nil {
				return nil, fmt.Errorf("repository: extension field %s without extension", fld.Name)
			}
			e.Fields = append(e.Fields, fld)
		}
	}
	for table, ks := range keys {
		slices.SortFunc(ks, func(a, b struct {
			pos  int
			name string
		}) int {
			return cmp.Compare(a.pos, b.pos)
		})
		for _, k := range ks {
			s.Tables[table].PrimaryKey = append(s.Tables[table].PrimaryKey, k.name)
		}
	}

	fks, err := r.selectRows(ctx, q, TForeignKeys, []string{"TABLE_NAME", "FK_NAME", "PACKAGE_ID", "ORIGIN", "TARGET_TABLE"}, "TABLE_NAME", "FK_NAME")
	if err != nil {
		return nil, err
	}
	fkCols, err := r.selectRows(ctx, q, TFKFields, []string{"TABLE_NAME", "FK_NAME", "FIELD_NAME", "TARGET_FIELD"}, "TABLE_NAME", "FK_NAME", "POSITION")
	if err != nil {
		return nil, err
	}
	cols := map[[2]string][]model.ColumnPair{}
	for _, c := range fkCols {
		// The tenant mapping is derived from the tenant flags and not part
		// of the declaration.
		if str(c["FIELD_NAME"]) == model.TenantField {
			continue
		}
		key := [2]string{str(c["TABLE_NAME"]), str(c["FK_NAME"])}
		cols[key] = append(cols[key], model.ColumnPair{Field: str(c["FIELD_NAME"]), References: str(c["TARGET_FIELD"])})
	}
	for _, f := range fks {
		table := str(f["TABLE_NAME"])
		fk := model.ForeignKey{Name: str(f["FK_NAME"]), Owner: owner(f["PACKAGE_ID"]), Origin: model.FieldOrigin(str(f["ORIGIN"])),
			Target: str(f["TARGET_TABLE"]), Columns: cols[[2]string{table, str(f["FK_NAME"])}]}
		switch fk.Origin {
		case model.OriginTable:
			if t := s.Tables[table]; t != nil {
				t.ForeignKeys = append(t.ForeignKeys, fk)
			}
		case model.OriginExtension:
			if e := s.Extensions[model.ExtensionKey{Package: fk.Owner, Table: table}]; e != nil {
				e.ForeignKeys = append(e.ForeignKeys, fk)
			}
		}
	}
	if err := r.loadStructures(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadTransformations(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadHTTPServices(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadAuthorizationObjects(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadRoles(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadSources(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadSettings(ctx, q, s); err != nil {
		return nil, err
	}
	if err := r.loadAssets(ctx, q, s); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *Repo) loadSettings(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	sets, err := r.selectRows(ctx, q, TSettings, []string{"SETTING_ID", "PACKAGE_ID", "ROOT_TABLE"})
	if err != nil {
		return err
	}
	for _, st := range sets {
		id := str(st["SETTING_ID"])
		s.Settings[id] = &model.Setting{ID: id, Owner: owner(st["PACKAGE_ID"]), Texts: map[string]model.SettingText{},
			Root: model.SettingTable{Table: str(st["ROOT_TABLE"])}}
	}
	texts, err := r.selectRows(ctx, q, TSettingTexts, []string{"SETTING_ID", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if st := s.Settings[str(t["SETTING_ID"])]; st != nil {
			st.Texts[str(t["LANGUAGE"])] = model.SettingText{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	exts, err := r.selectRows(ctx, q, TSettingExtensions, []string{"PACKAGE_ID", "SETTING_ID"})
	if err != nil {
		return err
	}
	for _, e := range exts {
		ext := &model.SettingExtension{Package: owner(e["PACKAGE_ID"]), Setting: str(e["SETTING_ID"])}
		s.SettingExtensions[ext.Key()] = ext
	}
	nodes, err := r.selectRows(ctx, q, TSettingTables, []string{"TABLE_NAME", "SETTING_ID", "PACKAGE_ID", "PARENT_TABLE", "FK_NAME",
		"ALLOW_CREATE", "ALLOW_UPDATE", "ALLOW_DELETE"}, "SETTING_ID", "POSITION")
	if err != nil {
		return err
	}
	for _, n := range nodes {
		st := s.Settings[str(n["SETTING_ID"])]
		if st == nil {
			return fmt.Errorf("repository: table %s of unknown setting %s", str(n["TABLE_NAME"]), str(n["SETTING_ID"]))
		}
		t := model.SettingTable{Table: str(n["TABLE_NAME"]), Parent: opt(n["PARENT_TABLE"]), ForeignKey: opt(n["FK_NAME"]),
			Operations: model.Operations{Create: flag(n["ALLOW_CREATE"]), Update: flag(n["ALLOW_UPDATE"]), Delete: flag(n["ALLOW_DELETE"])}}
		origin := owner(n["PACKAGE_ID"])
		switch {
		case t.Table == st.Root.Table:
			st.Root = t
		case origin == st.Owner:
			st.Tables = append(st.Tables, t)
		default:
			e := s.SettingExtensions[model.SettingExtensionKey{Package: origin, Setting: st.ID}]
			if e == nil {
				return fmt.Errorf("repository: table %s added to setting %s by %s without extension", t.Table, st.ID, origin)
			}
			e.Tables = append(e.Tables, t)
		}
	}
	return nil
}

// writeSettings stores settings and their extensions. Tables are written
// parents first because of the parent reference.
func (r *Repo) writeSettings(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, id := range s.SettingIDs() {
		st := s.Settings[id]
		if err := r.insert(ctx, q, TSettings, row{"SETTING_ID": id, "PACKAGE_ID": string(st.Owner), "ROOT_TABLE": st.Root.Table}); err != nil {
			return err
		}
		for _, lang := range st.Languages() {
			t := st.Texts[lang]
			if err := r.insert(ctx, q, TSettingTexts, row{"SETTING_ID": id, "LANGUAGE": lang, "LABEL": t.Label, "DESCRIPTION": val(t.Description)}); err != nil {
				return err
			}
		}
	}
	for _, e := range s.SortedSettingExtensions() {
		if err := r.insert(ctx, q, TSettingExtensions, row{"PACKAGE_ID": string(e.Package), "SETTING_ID": e.Setting}); err != nil {
			return err
		}
	}
	for _, id := range s.SettingIDs() {
		nodes := s.SettingNodes(id)
		written := map[string]bool{}
		for pass := 0; pass <= len(nodes); pass++ {
			for i, n := range nodes {
				if written[n.Table] || (!n.Root && !written[n.Parent.Or("")]) {
					continue
				}
				written[n.Table] = true
				if err := r.insert(ctx, q, TSettingTables, row{"TABLE_NAME": n.Table, "SETTING_ID": id, "PACKAGE_ID": string(n.Origin),
					"POSITION": int64(i + 1), "PARENT_TABLE": val(n.Parent), "FK_NAME": val(n.ForeignKey),
					"ALLOW_CREATE": n.Operations.Create, "ALLOW_UPDATE": n.Operations.Update, "ALLOW_DELETE": n.Operations.Delete}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ProvisionalClass is the data class of tables in repositories written
// before data classes existed: system for kernel tables and application
// for package tables until their package is applied again.
func ProvisionalClass(owner model.PackageID) model.DataClass {
	if owner == model.Kernel {
		return model.System
	}
	return model.Application
}

// HasDataClasses reports whether the version stores data classes.
func (r *Repo) HasDataClasses() bool { return r.has(TTables, "DATA_CLASS") }

// deleteOrder lists the repository tables children first.
var deleteOrder = []string{TRoleAuthValues, TRoleAuthorizations, TCompositeRoles, TRoleTexts, TRoles, TAuthObjectActivities, TAuthObjectFields, TAuthObjectTexts, TAuthObjects, THTTPHandlerParams, THTTPHandlers, THTTPServiceTexts, THTTPServices, TTransformationTexts, TTransformations, TCodeIncludes, TCodeUses, TBytecode, TSources, TTableIncludes, TArrayTypeKeys, TArrayTypeTexts, TArrayTypes, TStructureElements, TStructureExtensions,
	TStructureTexts, TStructures, TAssetEntryTexts, TAssetVariants, TAssetEntries, TAssetRepositoryTexts, TAssetRepositories, TSettingTables, TSettingExtensions, TSettingTexts, TSettings, TFKFields, TForeignKeys, TFields,
	TDataElementTexts, TDataElements, TExtensions, TTables, TDomainValues, TDomains, TPackageCaps, TPackageDeps, TPackages}

// Clear deletes all rows that describe the schema. Log, migrations,
// languages and tenants are kept.
func (r *Repo) Clear(ctx context.Context, q dialect.Querier) error {
	for _, t := range deleteOrder {
		if !r.has(t, "") {
			continue
		}
		if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(t)); err != nil {
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	return nil
}

// Write replaces the content of the repository with s. The compiled code
// of packages that stay installed is kept.
func (r *Repo) Write(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	code, err := r.LoadCode(ctx, q)
	if err != nil {
		return err
	}
	if err := r.Clear(ctx, q); err != nil {
		return err
	}
	for _, id := range s.PackageIDs() {
		p := s.Packages[id]
		if err := r.insert(ctx, q, TPackages, row{"PACKAGE_ID": string(id), "VERSION": p.Manifest.Version,
			"DESCRIPTION": val(p.Manifest.Description), "INSTALLED_AT": p.InstalledAt, "UPDATED_AT": p.UpdatedAt,
			"LANGUAGE_VERSION": nullableInt(p.Manifest.LanguageVersion, p.Manifest.LanguageVersion > 0), "KERNEL_PACKAGE": p.Manifest.KernelPackage}); err != nil {
			return err
		}
		if r.has(TPackageCaps, "") {
			for _, c := range slices.Sorted(slices.Values(p.Manifest.Capabilities)) {
				if err := r.insert(ctx, q, TPackageCaps, row{"PACKAGE_ID": string(id), "CAPABILITY": c}); err != nil {
					return err
				}
			}
		}
	}
	for _, id := range s.PackageIDs() {
		for _, d := range s.Packages[id].Manifest.Requires {
			if err := r.insert(ctx, q, TPackageDeps, row{"PACKAGE_ID": string(id), "REQUIRED_ID": string(d.ID), "VERSION_CONSTRAINT": val(d.Constraint)}); err != nil {
				return err
			}
		}
	}
	for _, name := range s.DomainNames() {
		d := s.Domains[name]
		domKind, domValue := writeDefault(d.Default)
		if err := r.insert(ctx, q, TDomains, row{"DEFAULT_KIND": domKind, "DEFAULT_VALUE": domValue, "ALLOW_EMPTY": valBool(d.AllowEmpty), "DOMAIN_NAME": name, "PACKAGE_ID": ownerValue(d.Owner), "DESCRIPTION": val(d.Description),
			"BASE_TYPE": string(d.Type), "LENGTH": nullableInt(d.Length, d.Type.HasLength()), "DECIMALS": nullableInt(d.Decimals, d.Type == model.Dec)}); err != nil {
			return err
		}
		for i, v := range d.Values {
			if err := r.insert(ctx, q, TDomainValues, row{"DOMAIN_NAME": name, "POSITION": int64(i + 1), "VALUE": v.Value, "DESCRIPTION": val(v.Description), "VALUE_NAME": val(v.Name)}); err != nil {
				return err
			}
		}
	}
	for _, name := range s.DataElementNames() {
		d := s.DataElements[name]
		if err := r.insert(ctx, q, TDataElements, row{"DATA_ELEMENT": name, "PACKAGE_ID": ownerValue(d.Owner), "DOMAIN_NAME": d.Domain}); err != nil {
			return err
		}
		for _, lang := range d.Languages() {
			t := d.Texts[lang]
			if err := r.insert(ctx, q, TDataElementTexts, row{"DATA_ELEMENT": name, "LANGUAGE": lang, "SHORT_LABEL": t.Short, "MEDIUM_LABEL": t.Medium,
				"LONG_LABEL": t.Long, "HEADING": t.Heading, "DESCRIPTION": val(t.Description)}); err != nil {
				return err
			}
		}
	}
	for _, name := range s.TableNames() {
		t := s.Tables[name]
		if err := r.insert(ctx, q, TTables, row{"TABLE_NAME": name, "PACKAGE_ID": ownerValue(t.Owner), "DESCRIPTION": val(t.Description),
			"TENANT_SPECIFIC": t.TenantSpecific, "DATA_CLASS": string(t.DataClass), "PK_NAME": model.PrimaryKeyName(name)}); err != nil {
			return err
		}
	}
	for _, e := range s.SortedExtensions() {
		if err := r.insert(ctx, q, TExtensions, row{"PACKAGE_ID": string(e.Package), "TABLE_NAME": e.Table, "DESCRIPTION": val(e.Description)}); err != nil {
			return err
		}
	}
	if err := r.writeStructures(ctx, q, s); err != nil {
		return err
	}
	phys := s.Physical()
	for _, name := range phys.TableNames() {
		t := phys.Tables[name]
		for i, f := range t.Fields {
			key := slices.Index(t.PrimaryKey, f.Name)
			defKind, defValue := writeDefault(f.Default)
			if err := r.insert(ctx, q, TFields, row{"TABLE_NAME": name, "FIELD_NAME": f.Name, "POSITION": int64(i + 1), "LOCAL_NAME": f.LocalName,
				"DESCRIPTION": val(f.Description), "PACKAGE_ID": ownerValue(f.Owner), "ORIGIN": string(f.Origin), "DOMAIN_NAME": f.Domain,
				"NOT_NULL": f.NotNull, "DEFAULT_KIND": defKind, "DEFAULT_VALUE": defValue, "KEY_POSITION": nullableInt(key+1, key >= 0), "DATA_ELEMENT": val(f.DataElement),
				"CHECK_NAME": model.CheckName(name, f.Name), "INCLUDE_NAME": val(f.Included), "SOURCE_NAME": val(f.Source)}); err != nil {
				return err
			}
		}
		for _, fk := range t.ForeignKeys {
			if err := r.insert(ctx, q, TForeignKeys, row{"TABLE_NAME": name, "FK_NAME": fk.Name, "PACKAGE_ID": ownerValue(fk.Owner),
				"ORIGIN": string(fk.Origin), "TARGET_TABLE": fk.Target, "CONSTRAINT_NAME": model.ForeignKeyName(name, fk.Name)}); err != nil {
				return err
			}
			for i, c := range fk.Columns {
				if err := r.insert(ctx, q, TFKFields, row{"TABLE_NAME": name, "FK_NAME": fk.Name, "POSITION": int64(i + 1),
					"FIELD_NAME": c.Field, "TARGET_FIELD": c.References}); err != nil {
					return err
				}
			}
		}
	}
	if err := r.writeSettings(ctx, q, s); err != nil {
		return err
	}
	if err := r.writeTransformations(ctx, q, s); err != nil {
		return err
	}
	if err := r.writeHTTPServices(ctx, q, s); err != nil {
		return err
	}
	if err := r.writeAuthorizationObjects(ctx, q, s); err != nil {
		return err
	}
	if err := r.writeRoles(ctx, q, s); err != nil {
		return err
	}
	if err := r.writeSources(ctx, q, s); err != nil {
		return err
	}
	if err := r.writeAssets(ctx, q, s); err != nil {
		return err
	}
	var kept []*Code
	for _, id := range s.PackageIDs() {
		if c := code[id]; c != nil {
			kept = append(kept, c)
		}
	}
	return r.SaveCode(ctx, q, kept)
}

// LogPackage is a package affected by a logged operation.
type LogPackage struct {
	ID   string                 `json:"id"`
	From model.Optional[string] `json:"from"` // None for installations
	To   model.Optional[string] `json:"to"`   // None for removals
}

// LogStatement is a statement executed by a logged operation.
type LogStatement struct {
	SQL         string `json:"sql"`
	Destructive bool   `json:"destructive"`
}

// LogEntry is one entry of the operation log.
type LogEntry struct {
	ID         string                 `json:"id"`
	Operation  string                 `json:"operation"`
	Target     model.Optional[string] `json:"target"`
	StartedAt  time.Time              `json:"startedAt"`
	FinishedAt time.Time              `json:"finishedAt"`
	Status     string                 `json:"status"`
	Message    model.Optional[string] `json:"message"`
	Packages   []LogPackage           `json:"packages,omitempty"`
	Statements []LogStatement         `json:"statements,omitempty"`
}

// Log statuses.
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
)

// WriteLog stores a log entry. An empty ID is replaced by a new one.
func (r *Repo) WriteLog(ctx context.Context, q dialect.Querier, e *LogEntry) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if err := r.insert(ctx, q, TLog, row{"LOG_ID": e.ID, "OPERATION": e.Operation, "TARGET": val(e.Target), "STARTED_AT": e.StartedAt,
		"FINISHED_AT": e.FinishedAt, "STATUS": e.Status, "MESSAGE": val(e.Message)}); err != nil {
		return err
	}
	for _, p := range e.Packages {
		if err := r.insert(ctx, q, TLogPackages, row{"LOG_ID": e.ID, "PACKAGE_ID": p.ID, "VERSION_FROM": val(p.From), "VERSION_TO": val(p.To)}); err != nil {
			return err
		}
	}
	for i, st := range e.Statements {
		if err := r.insert(ctx, q, TLogStatements, row{"LOG_ID": e.ID, "POSITION": int64(i + 1), "STATEMENT": st.SQL, "DESTRUCTIVE": st.Destructive}); err != nil {
			return err
		}
	}
	return nil
}

// ReadLog returns all log entries, newest first.
func (r *Repo) ReadLog(ctx context.Context, q dialect.Querier) ([]*LogEntry, error) {
	rows, err := r.selectRows(ctx, q, TLog, []string{"LOG_ID", "OPERATION", "TARGET", "STARTED_AT", "FINISHED_AT", "STATUS", "MESSAGE"}, "STARTED_AT", "LOG_ID")
	if err != nil {
		return nil, err
	}
	byID := map[string]*LogEntry{}
	var out []*LogEntry
	for _, l := range rows {
		e := &LogEntry{ID: str(l["LOG_ID"]), Operation: str(l["OPERATION"]), Target: opt(l["TARGET"]), StartedAt: tm(l["STARTED_AT"]),
			FinishedAt: tm(l["FINISHED_AT"]), Status: str(l["STATUS"]), Message: opt(l["MESSAGE"])}
		byID[e.ID] = e
		out = append(out, e)
	}
	pkgs, err := r.selectRows(ctx, q, TLogPackages, []string{"LOG_ID", "PACKAGE_ID", "VERSION_FROM", "VERSION_TO"}, "LOG_ID", "PACKAGE_ID")
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		if e := byID[str(p["LOG_ID"])]; e != nil {
			e.Packages = append(e.Packages, LogPackage{ID: str(p["PACKAGE_ID"]), From: opt(p["VERSION_FROM"]), To: opt(p["VERSION_TO"])})
		}
	}
	stmts, err := r.selectRows(ctx, q, TLogStatements, []string{"LOG_ID", "STATEMENT", "DESTRUCTIVE"}, "LOG_ID", "POSITION")
	if err != nil {
		return nil, err
	}
	for _, st := range stmts {
		if e := byID[str(st["LOG_ID"])]; e != nil {
			e.Statements = append(e.Statements, LogStatement{SQL: str(st["STATEMENT"]), Destructive: flag(st["DESTRUCTIVE"])})
		}
	}
	slices.Reverse(out)
	return out, nil
}

// NewID returns a version 7 UUID: time-ordered and random.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(time.Now().UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Tenant is a row of the tenant table.
type Tenant struct {
	ID          string                 `json:"id"`
	Description model.Optional[string] `json:"description"`
	CreatedAt   time.Time              `json:"createdAt"`
}

// InsertTenant creates a tenant.
func (r *Repo) InsertTenant(ctx context.Context, q dialect.Querier, t Tenant) error {
	return r.insert(ctx, q, model.TenantTable, row{model.TenantTableKey: t.ID, "DESCRIPTION": val(t.Description), "CREATED_AT": t.CreatedAt})
}

// Tenants returns all tenants ordered by key.
func (r *Repo) Tenants(ctx context.Context, q dialect.Querier) ([]Tenant, error) {
	rows, err := r.selectRows(ctx, q, model.TenantTable, []string{model.TenantTableKey, "DESCRIPTION", "CREATED_AT"}, model.TenantTableKey)
	if err != nil {
		return nil, err
	}
	out := make([]Tenant, 0, len(rows))
	for _, t := range rows {
		out = append(out, Tenant{ID: str(t[model.TenantTableKey]), Description: opt(t["DESCRIPTION"]), CreatedAt: tm(t["CREATED_AT"])})
	}
	return out, nil
}

// NameProblem is a generated name in the repository that differs from the
// name the kernel computes.
type NameProblem struct {
	Object   string
	Stored   string
	Computed string
}

// CheckGeneratedNames compares the stored generated names of primary keys,
// foreign keys and checks with the computed ones.
func (r *Repo) CheckGeneratedNames(ctx context.Context, q dialect.Querier) ([]NameProblem, error) {
	var out []NameProblem
	cmp := func(object, stored, computed string) {
		if stored != computed {
			out = append(out, NameProblem{object, stored, computed})
		}
	}
	if r.has(TTables, "PK_NAME") {
		rows, err := r.selectRows(ctx, q, TTables, []string{"TABLE_NAME", "PK_NAME"}, "TABLE_NAME")
		if err != nil {
			return nil, err
		}
		for _, t := range rows {
			cmp("table "+str(t["TABLE_NAME"]), str(t["PK_NAME"]), model.PrimaryKeyName(str(t["TABLE_NAME"])))
		}
	}
	if r.has(TForeignKeys, "CONSTRAINT_NAME") {
		rows, err := r.selectRows(ctx, q, TForeignKeys, []string{"TABLE_NAME", "FK_NAME", "CONSTRAINT_NAME"}, "TABLE_NAME", "FK_NAME")
		if err != nil {
			return nil, err
		}
		for _, f := range rows {
			cmp("foreign key "+str(f["TABLE_NAME"])+"."+str(f["FK_NAME"]), str(f["CONSTRAINT_NAME"]), model.ForeignKeyName(str(f["TABLE_NAME"]), str(f["FK_NAME"])))
		}
	}
	if r.has(TFields, "CHECK_NAME") {
		rows, err := r.selectRows(ctx, q, TFields, []string{"TABLE_NAME", "FIELD_NAME", "CHECK_NAME"}, "TABLE_NAME", "FIELD_NAME")
		if err != nil {
			return nil, err
		}
		for _, f := range rows {
			cmp("field "+str(f["TABLE_NAME"])+"."+str(f["FIELD_NAME"]), str(f["CHECK_NAME"]), model.CheckName(str(f["TABLE_NAME"]), str(f["FIELD_NAME"])))
		}
	}
	return out, nil
}

// InsertDataElementText adds the labels of a data element in a language.
func (r *Repo) InsertDataElementText(ctx context.Context, q dialect.Querier, name, lang string, t model.DataElementText) error {
	return r.insert(ctx, q, TDataElementTexts, row{"DATA_ELEMENT": name, "LANGUAGE": lang, "SHORT_LABEL": t.Short, "MEDIUM_LABEL": t.Medium,
		"LONG_LABEL": t.Long, "HEADING": t.Heading, "DESCRIPTION": val(t.Description)})
}
