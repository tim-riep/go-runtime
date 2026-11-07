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

// Package dialect defines the interface that hides the differences between
// the supported databases, and helpers shared by the implementations.
package dialect

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/plan"
)

// Querier is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TxOptions control a transaction started by Dialect.Transact.
type TxOptions struct {
	// DDL prepares the transaction for schema changes. On SQLite, foreign
	// key enforcement is switched off for the transaction so tables can be
	// rebuilt, and all keys are verified before commit.
	DDL bool
	// DeferForeignKeys checks foreign keys at commit instead of after each
	// statement.
	DeferForeignKeys bool
}

// Dialect encapsulates everything that differs between databases.
type Dialect interface {
	// Name returns "sqlite" or "postgres".
	Name() string
	// Open opens a connection pool for a dialect-specific connection string.
	Open(dsn string) (*sql.DB, error)

	// QuoteIdent quotes an identifier.
	QuoteIdent(name string) string
	// Placeholder returns the bind parameter for position n, starting at 1.
	Placeholder(n int) string
	// ColumnType returns the SQL column type of a domain.
	ColumnType(d *model.Domain) string
	// ColumnCheck returns the condition that a column of the domain must
	// satisfy, or "" if the column type alone is sufficient.
	ColumnCheck(column string, d *model.Domain) string
	// Literal renders a canonical value of the domain as SQL literal.
	Literal(d *model.Domain, canonical string) (string, error)

	// PlanChanges turns logical changes into executable steps. The dialect
	// decides how each change is carried out, e.g. ALTER TABLE or a rebuild.
	PlanChanges(cur, want *model.Physical, changes []plan.Change) ([]plan.Step, error)

	// Transact runs fn in a transaction that is committed if fn returns
	// nil and rolled back otherwise. It also serializes concurrent kernel
	// operations.
	Transact(ctx context.Context, db *sql.DB, opts TxOptions, fn func(tx *sql.Tx) error) error

	// TableExists reports whether a table exists.
	TableExists(ctx context.Context, q Querier, name string) (bool, error)
	// Introspect reads the actual layout of all tables.
	Introspect(ctx context.Context, q Querier) (*Catalog, error)
	// Expected returns the catalog that Introspect should report for p.
	Expected(p *model.Physical) *Catalog

	// Encode converts a Go value into the driver value stored for the
	// domain: string, int64, bool, []byte, time.Time or nil.
	Encode(d *model.Domain, v any) (any, error)
	// Decode converts a value read from a column of the domain into its Go
	// representation: string for CHAR, STRING, DEC and UUID, int64, bool,
	// time.Time for DATE and TIMESTAMP, []byte, or nil.
	Decode(d *model.Domain, v any) (any, error)
	// ClassifyError converts constraint violations into
	// *rterr.ConstraintViolation and returns other errors unchanged.
	ClassifyError(err error) error
}

// Catalog is the layout of the tables in a database, reduced to what the
// kernel compares.
type Catalog struct {
	Tables map[string]*CatalogTable
}

// CatalogTable is one table of a Catalog.
type CatalogTable struct {
	Columns     map[string]CatalogColumn
	PrimaryKey  []string
	ForeignKeys []CatalogForeignKey
	Constraints map[string]bool // names of constraints
}

// CatalogColumn is one column of a CatalogTable.
type CatalogColumn struct {
	Type       string
	NotNull    bool
	HasDefault bool
}

// CatalogForeignKey is one foreign key of a CatalogTable.
type CatalogForeignKey struct {
	Target  string
	Columns []model.ColumnPair
}

// Key returns a comparable representation of the key.
func (fk CatalogForeignKey) Key() string {
	parts := []string{fk.Target}
	for _, c := range fk.Columns {
		parts = append(parts, c.Field+"="+c.References)
	}
	return strings.Join(parts, "|")
}

// QuoteIdent quotes an identifier with double quotes.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// QuoteString renders s as SQL string literal.
func QuoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// QuoteList quotes and joins identifiers.
func QuoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = QuoteIdent(n)
	}
	return strings.Join(q, ", ")
}

// ValueList renders the fixed values of a domain for an IN list.
func ValueList(d *model.Domain) string {
	vals := make([]string, len(d.Values))
	for i, v := range d.Values {
		if d.Type == model.Char {
			vals[i] = QuoteString(v.Value)
		} else {
			vals[i] = v.Value
		}
	}
	return strings.Join(vals, ", ")
}

// ForeignKeyClause renders a FOREIGN KEY table constraint without the
// leading CONSTRAINT name.
func ForeignKeyClause(fk *model.ForeignKey) string {
	src := make([]string, len(fk.Columns))
	dst := make([]string, len(fk.Columns))
	for i, c := range fk.Columns {
		src[i], dst[i] = c.Field, c.References
	}
	return "FOREIGN KEY (" + QuoteList(src) + ") REFERENCES " + QuoteIdent(fk.Target) + " (" + QuoteList(dst) + ")"
}

// CreationOrder orders the given tables so that targets of foreign keys
// come before the tables referencing them, as far as possible. Keys that
// form a cycle cannot be satisfied by any order; the returned set holds the
// "table|key" pairs whose targets are created later.
func CreationOrder(p *model.Physical, names []string) ([]string, map[string]bool) {
	pending := map[string]bool{}
	for _, n := range names {
		pending[n] = true
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	var order []string
	done := map[string]bool{}
	visiting := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if done[n] || visiting[n] {
			return
		}
		visiting[n] = true
		for _, fk := range p.Tables[n].ForeignKeys {
			if pending[fk.Target] && fk.Target != n {
				visit(fk.Target)
			}
		}
		visiting[n] = false
		done[n] = true
		order = append(order, n)
	}
	for _, n := range sorted {
		visit(n)
	}
	late := map[string]bool{}
	created := map[string]bool{}
	for _, n := range order {
		for _, fk := range p.Tables[n].ForeignKeys {
			if pending[fk.Target] && !created[fk.Target] && fk.Target != n {
				late[n+"|"+fk.Name] = true
			}
		}
		created[n] = true
	}
	return order, late
}

// ChangesByTable groups table-level changes by table name.
func ChangesByTable(changes []plan.Change) map[string][]plan.Change {
	out := map[string][]plan.Change{}
	for _, c := range changes {
		if c.Table != "" {
			out[c.Table] = append(out[c.Table], c)
		}
	}
	return out
}

// HasDestructive reports whether any of the changes is destructive.
func HasDestructive(changes []plan.Change) bool {
	return slices.ContainsFunc(changes, func(c plan.Change) bool { return c.Destructive })
}

// Describe joins the descriptions of changes for a step.
func Describe(changes []plan.Change) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = c.String()
	}
	return strings.Join(parts, "; ")
}
