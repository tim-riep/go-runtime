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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/timriep/go-runtime/dialect"
	"github.com/timriep/go-runtime/internal/kernelschema"
	"github.com/timriep/go-runtime/model"
)

// Kernel table names.
const (
	TPackages      = model.KernelNamespace + "PACKAGES"
	TPackageDeps   = model.KernelNamespace + "PACKAGE_DEPS"
	TDomains       = model.KernelNamespace + "DOMAINS"
	TDomainValues  = model.KernelNamespace + "DOMAIN_VALUES"
	TTables        = model.KernelNamespace + "TABLES"
	TExtensions    = model.KernelNamespace + "EXTENSIONS"
	TFields        = model.KernelNamespace + "FIELDS"
	TForeignKeys   = model.KernelNamespace + "FOREIGN_KEYS"
	TFKFields      = model.KernelNamespace + "FK_FIELDS"
	TMigrations    = model.KernelNamespace + "MIGRATIONS"
	TLog           = model.KernelNamespace + "LOG"
	TLogPackages   = model.KernelNamespace + "LOG_PACKAGES"
	TLogStatements = model.KernelNamespace + "LOG_STATEMENTS"
)

// Repo accesses the kernel tables with a dialect.
type Repo struct {
	d dialect.Dialect
}

// New returns a Repo for the dialect.
func New(d dialect.Dialect) *Repo { return &Repo{d: d} }

var (
	layoutOnce sync.Once
	layout     *model.Physical
	layoutErr  error
)

// kernelLayout returns the physical layout of the newest kernel tables,
// used to encode and decode their values.
func kernelLayout() (*model.Physical, error) {
	layoutOnce.Do(func() {
		s, err := kernelschema.Schema(kernelschema.Latest())
		if err != nil {
			layoutErr = err
			return
		}
		layout = s.Physical()
	})
	return layout, layoutErr
}

type row map[string]any

func (r *Repo) domain(table, column string) (*model.Domain, error) {
	l, err := kernelLayout()
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

// Insert writes one row into a kernel table.
func (r *Repo) insert(ctx context.Context, q dialect.Querier, table string, values row) error {
	cols := make([]string, 0, len(values))
	for c := range values {
		cols = append(cols, c)
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
// values, in the given order.
func (r *Repo) selectRows(ctx context.Context, q dialect.Querier, table string, cols []string, orderBy ...string) ([]row, error) {
	stmt := "SELECT " + dialect.QuoteList(cols) + " FROM " + r.d.QuoteIdent(table)
	if len(orderBy) > 0 {
		stmt += " ORDER BY " + dialect.QuoteList(orderBy)
	}
	doms := make([]*model.Domain, len(cols))
	for i, c := range cols {
		d, err := r.domain(table, c)
		if err != nil {
			return nil, err
		}
		doms[i] = d
	}
	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", table, err)
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		rec := row{}
		for i, c := range cols {
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

func optStr(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
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

// nullable maps empty strings and the kernel owner to NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
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

	pkgs, err := r.selectRows(ctx, q, TPackages, []string{"PACKAGE_ID", "VERSION", "DESCRIPTION", "INSTALLED_AT", "UPDATED_AT"})
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		id := model.PackageID(str(p["PACKAGE_ID"]))
		s.Packages[id] = &model.PackageInfo{
			Manifest:    model.Manifest{ID: id, Version: str(p["VERSION"]), Description: str(p["DESCRIPTION"])},
			InstalledAt: tm(p["INSTALLED_AT"]), UpdatedAt: tm(p["UPDATED_AT"]),
		}
	}
	deps, err := r.selectRows(ctx, q, TPackageDeps, []string{"PACKAGE_ID", "REQUIRED_ID", "VERSION_CONSTRAINT"}, "PACKAGE_ID", "REQUIRED_ID")
	if err != nil {
		return nil, err
	}
	for _, d := range deps {
		if p := s.Packages[model.PackageID(str(d["PACKAGE_ID"]))]; p != nil {
			p.Manifest.Requires = append(p.Manifest.Requires, model.Dependency{ID: model.PackageID(str(d["REQUIRED_ID"])), Constraint: str(d["VERSION_CONSTRAINT"])})
		}
	}

	doms, err := r.selectRows(ctx, q, TDomains, []string{"DOMAIN_NAME", "PACKAGE_ID", "DESCRIPTION", "BASE_TYPE", "LENGTH", "DECIMALS"})
	if err != nil {
		return nil, err
	}
	for _, d := range doms {
		name := str(d["DOMAIN_NAME"])
		s.Domains[name] = &model.Domain{Name: name, Owner: model.PackageID(str(d["PACKAGE_ID"])), Description: str(d["DESCRIPTION"]),
			Type: model.BaseType(str(d["BASE_TYPE"])), Length: num(d["LENGTH"]), Decimals: num(d["DECIMALS"])}
	}
	vals, err := r.selectRows(ctx, q, TDomainValues, []string{"DOMAIN_NAME", "VALUE", "DESCRIPTION"}, "DOMAIN_NAME", "POSITION")
	if err != nil {
		return nil, err
	}
	for _, v := range vals {
		if d := s.Domains[str(v["DOMAIN_NAME"])]; d != nil {
			d.Values = append(d.Values, model.FixedValue{Value: str(v["VALUE"]), Description: str(v["DESCRIPTION"])})
		}
	}

	tabs, err := r.selectRows(ctx, q, TTables, []string{"TABLE_NAME", "PACKAGE_ID", "DESCRIPTION", "TENANT_SPECIFIC"})
	if err != nil {
		return nil, err
	}
	for _, t := range tabs {
		name := str(t["TABLE_NAME"])
		s.Tables[name] = &model.Table{Name: name, Owner: model.PackageID(str(t["PACKAGE_ID"])), Description: str(t["DESCRIPTION"]), TenantSpecific: flag(t["TENANT_SPECIFIC"])}
	}
	exts, err := r.selectRows(ctx, q, TExtensions, []string{"PACKAGE_ID", "TABLE_NAME", "DESCRIPTION"})
	if err != nil {
		return nil, err
	}
	for _, e := range exts {
		ext := &model.Extension{Package: model.PackageID(str(e["PACKAGE_ID"])), Table: str(e["TABLE_NAME"]), Description: str(e["DESCRIPTION"])}
		s.Extensions[ext.Key()] = ext
	}

	fields, err := r.selectRows(ctx, q, TFields, []string{"TABLE_NAME", "FIELD_NAME", "LOCAL_NAME", "DESCRIPTION", "PACKAGE_ID", "ORIGIN",
		"DOMAIN_NAME", "NOT_NULL", "DEFAULT_VALUE", "KEY_POSITION"}, "TABLE_NAME", "POSITION")
	if err != nil {
		return nil, err
	}
	keys := map[string][]struct {
		pos  int
		name string
	}{}
	for _, f := range fields {
		table := str(f["TABLE_NAME"])
		fld := model.Field{Name: str(f["FIELD_NAME"]), LocalName: str(f["LOCAL_NAME"]), Description: str(f["DESCRIPTION"]),
			Owner: model.PackageID(str(f["PACKAGE_ID"])), Origin: model.FieldOrigin(str(f["ORIGIN"])), Domain: str(f["DOMAIN_NAME"]),
			NotNull: flag(f["NOT_NULL"]), Default: optStr(f["DEFAULT_VALUE"])}
		switch fld.Origin {
		case model.OriginTable:
			t := s.Tables[table]
			if t == nil {
				return nil, fmt.Errorf("repository: field %s of unknown table %s", fld.Name, table)
			}
			t.Fields = append(t.Fields, fld)
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
		fk := model.ForeignKey{Name: str(f["FK_NAME"]), Owner: model.PackageID(str(f["PACKAGE_ID"])), Origin: model.FieldOrigin(str(f["ORIGIN"])),
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
	return s, nil
}

// deleteOrder lists the repository tables children first.
var deleteOrder = []string{TFKFields, TForeignKeys, TFields, TExtensions, TTables, TDomainValues, TDomains, TPackageDeps, TPackages}

// Write replaces the content of the repository with s.
func (r *Repo) Write(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, t := range deleteOrder {
		if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(t)); err != nil {
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	for _, id := range s.PackageIDs() {
		p := s.Packages[id]
		if err := r.insert(ctx, q, TPackages, row{"PACKAGE_ID": string(id), "VERSION": p.Manifest.Version,
			"DESCRIPTION": nullable(p.Manifest.Description), "INSTALLED_AT": p.InstalledAt, "UPDATED_AT": p.UpdatedAt}); err != nil {
			return err
		}
	}
	for _, id := range s.PackageIDs() {
		for _, d := range s.Packages[id].Manifest.Requires {
			if err := r.insert(ctx, q, TPackageDeps, row{"PACKAGE_ID": string(id), "REQUIRED_ID": string(d.ID), "VERSION_CONSTRAINT": nullable(d.Constraint)}); err != nil {
				return err
			}
		}
	}
	for _, name := range s.DomainNames() {
		d := s.Domains[name]
		if err := r.insert(ctx, q, TDomains, row{"DOMAIN_NAME": name, "PACKAGE_ID": nullable(string(d.Owner)), "DESCRIPTION": nullable(d.Description),
			"BASE_TYPE": string(d.Type), "LENGTH": nullableInt(d.Length, d.Type.HasLength()), "DECIMALS": nullableInt(d.Decimals, d.Type == model.Dec)}); err != nil {
			return err
		}
		for i, v := range d.Values {
			if err := r.insert(ctx, q, TDomainValues, row{"DOMAIN_NAME": name, "POSITION": int64(i + 1), "VALUE": v.Value, "DESCRIPTION": nullable(v.Description)}); err != nil {
				return err
			}
		}
	}
	for _, name := range s.TableNames() {
		t := s.Tables[name]
		if err := r.insert(ctx, q, TTables, row{"TABLE_NAME": name, "PACKAGE_ID": nullable(string(t.Owner)), "DESCRIPTION": nullable(t.Description), "TENANT_SPECIFIC": t.TenantSpecific}); err != nil {
			return err
		}
	}
	for _, e := range s.SortedExtensions() {
		if err := r.insert(ctx, q, TExtensions, row{"PACKAGE_ID": string(e.Package), "TABLE_NAME": e.Table, "DESCRIPTION": nullable(e.Description)}); err != nil {
			return err
		}
	}
	phys := s.Physical()
	for _, name := range phys.TableNames() {
		t := phys.Tables[name]
		for i, f := range t.Fields {
			key := slices.Index(t.PrimaryKey, f.Name)
			var def any
			if f.Default != nil {
				def = *f.Default
			}
			if err := r.insert(ctx, q, TFields, row{"TABLE_NAME": name, "FIELD_NAME": f.Name, "POSITION": int64(i + 1), "LOCAL_NAME": f.LocalName,
				"DESCRIPTION": nullable(f.Description), "PACKAGE_ID": nullable(string(f.Owner)), "ORIGIN": string(f.Origin), "DOMAIN_NAME": f.Domain,
				"NOT_NULL": f.NotNull, "DEFAULT_VALUE": def, "KEY_POSITION": nullableInt(key+1, key >= 0)}); err != nil {
				return err
			}
		}
		for _, fk := range t.ForeignKeys {
			if err := r.insert(ctx, q, TForeignKeys, row{"TABLE_NAME": name, "FK_NAME": fk.Name, "PACKAGE_ID": nullable(string(fk.Owner)),
				"ORIGIN": string(fk.Origin), "TARGET_TABLE": fk.Target}); err != nil {
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
	return nil
}

// LogPackage is a package affected by a logged operation.
type LogPackage struct {
	ID   string `json:"id"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// LogStatement is a statement executed by a logged operation.
type LogStatement struct {
	SQL         string `json:"sql"`
	Destructive bool   `json:"destructive"`
}

// LogEntry is one entry of the operation log.
type LogEntry struct {
	ID         string         `json:"id"`
	Operation  string         `json:"operation"`
	Target     string         `json:"target,omitempty"`
	StartedAt  time.Time      `json:"startedAt"`
	FinishedAt time.Time      `json:"finishedAt"`
	Status     string         `json:"status"`
	Message    string         `json:"message,omitempty"`
	Packages   []LogPackage   `json:"packages,omitempty"`
	Statements []LogStatement `json:"statements,omitempty"`
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
	if err := r.insert(ctx, q, TLog, row{"LOG_ID": e.ID, "OPERATION": e.Operation, "TARGET": nullable(e.Target), "STARTED_AT": e.StartedAt,
		"FINISHED_AT": e.FinishedAt, "STATUS": e.Status, "MESSAGE": nullable(e.Message)}); err != nil {
		return err
	}
	for _, p := range e.Packages {
		if err := r.insert(ctx, q, TLogPackages, row{"LOG_ID": e.ID, "PACKAGE_ID": p.ID, "VERSION_FROM": nullable(p.From), "VERSION_TO": nullable(p.To)}); err != nil {
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
		e := &LogEntry{ID: str(l["LOG_ID"]), Operation: str(l["OPERATION"]), Target: str(l["TARGET"]), StartedAt: tm(l["STARTED_AT"]),
			FinishedAt: tm(l["FINISHED_AT"]), Status: str(l["STATUS"]), Message: str(l["MESSAGE"])}
		byID[e.ID] = e
		out = append(out, e)
	}
	pkgs, err := r.selectRows(ctx, q, TLogPackages, []string{"LOG_ID", "PACKAGE_ID", "VERSION_FROM", "VERSION_TO"}, "LOG_ID", "PACKAGE_ID")
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		if e := byID[str(p["LOG_ID"])]; e != nil {
			e.Packages = append(e.Packages, LogPackage{ID: str(p["PACKAGE_ID"]), From: str(p["VERSION_FROM"]), To: str(p["VERSION_TO"])})
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
