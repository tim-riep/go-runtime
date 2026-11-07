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

// Package postgres implements the dialect for PostgreSQL using pgx.
//
// The kernel works in the first schema of the connection's search_path and
// never qualifies names. All foreign keys are DEFERRABLE INITIALLY
// IMMEDIATE, so operations such as deleting a tenant can defer them.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"github.com/timriep/go-runtime/dialect"
	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/plan"
	"github.com/timriep/go-runtime/rterr"
)

// lockKey identifies the advisory lock that serializes kernel operations.
const lockKey = 0x7274_6b72 // "rtkr"

// Dialect is the PostgreSQL dialect.
type Dialect struct{}

// New returns the PostgreSQL dialect.
func New() *Dialect { return &Dialect{} }

var _ dialect.Dialect = (*Dialect)(nil)

// Name implements dialect.Dialect.
func (*Dialect) Name() string { return "postgres" }

// Open implements dialect.Dialect. dsn is a pgx connection string.
func (*Dialect) Open(dsn string) (*sql.DB, error) { return sql.Open("pgx", dsn) }

// QuoteIdent implements dialect.Dialect.
func (*Dialect) QuoteIdent(name string) string { return dialect.QuoteIdent(name) }

// Placeholder implements dialect.Dialect.
func (*Dialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }

// ColumnType implements dialect.Dialect.
func (*Dialect) ColumnType(d *model.Domain) string {
	switch d.Type {
	case model.Char:
		return fmt.Sprintf("VARCHAR(%d)", d.Length)
	case model.String:
		return "TEXT"
	case model.Int4:
		return "INTEGER"
	case model.Int8:
		return "BIGINT"
	case model.Dec:
		return fmt.Sprintf("NUMERIC(%d,%d)", d.Length, d.Decimals)
	case model.Date:
		return "DATE"
	case model.Timestamp:
		return "TIMESTAMPTZ"
	case model.Bool:
		return "BOOLEAN"
	case model.UUID:
		return "UUID"
	case model.Blob:
		return "BYTEA"
	}
	return "TEXT"
}

// ColumnCheck implements dialect.Dialect. Native types cover everything but
// fixed values.
func (*Dialect) ColumnCheck(column string, d *model.Domain) string {
	if len(d.Values) == 0 {
		return ""
	}
	return dialect.QuoteIdent(column) + " IN (" + dialect.ValueList(d) + ")"
}

// Literal implements dialect.Dialect.
func (*Dialect) Literal(d *model.Domain, canonical string) (string, error) {
	switch d.Type {
	case model.Int4, model.Int8, model.Dec:
		return canonical, nil
	case model.Bool:
		return strings.ToUpper(canonical), nil
	case model.Blob:
		return "", errors.New("BLOB has no literal form")
	}
	return dialect.QuoteString(canonical), nil
}

func (d *Dialect) columnDef(p *model.Physical, f *model.Field) (string, error) {
	dom := p.Domain(f)
	s := dialect.QuoteIdent(f.Name) + " " + d.ColumnType(dom)
	if f.NotNull {
		s += " NOT NULL"
	}
	if f.Default != nil {
		lit, err := d.Literal(dom, *f.Default)
		if err != nil {
			return "", err
		}
		s += " DEFAULT " + lit
	}
	return s, nil
}

func fkConstraint(table string, fk *model.ForeignKey) string {
	return "CONSTRAINT " + dialect.QuoteIdent(model.ForeignKeyName(table, fk.Name)) + " " +
		dialect.ForeignKeyClause(fk) + " DEFERRABLE INITIALLY IMMEDIATE"
}

func checkConstraint(table, column, cond string) string {
	return "CONSTRAINT " + dialect.QuoteIdent(model.CheckName(table, column)) + " CHECK (" + cond + ")"
}

// createTable renders CREATE TABLE with all foreign keys except those in
// skip, which are added once their targets exist.
func (d *Dialect) createTable(p *model.Physical, t *model.Table, skip func(fk string) bool) (string, error) {
	var lines []string
	for i := range t.Fields {
		def, err := d.columnDef(p, &t.Fields[i])
		if err != nil {
			return "", err
		}
		lines = append(lines, def)
	}
	lines = append(lines, "CONSTRAINT "+dialect.QuoteIdent(model.PrimaryKeyName(t.Name))+" PRIMARY KEY ("+dialect.QuoteList(t.PrimaryKey)+")")
	for i := range t.ForeignKeys {
		if !skip(t.ForeignKeys[i].Name) {
			lines = append(lines, fkConstraint(t.Name, &t.ForeignKeys[i]))
		}
	}
	for i := range t.Fields {
		f := &t.Fields[i]
		if chk := d.ColumnCheck(f.Name, p.Domain(f)); chk != "" {
			lines = append(lines, checkConstraint(t.Name, f.Name, chk))
		}
	}
	return "CREATE TABLE " + dialect.QuoteIdent(t.Name) + " (\n  " + strings.Join(lines, ",\n  ") + "\n)", nil
}

func alterTable(table, action string) string {
	return "ALTER TABLE " + dialect.QuoteIdent(table) + " " + action
}

// PlanChanges implements dialect.Dialect. The order respects dependencies:
// removed foreign keys first, then dropped tables, created tables, column
// changes and finally new foreign keys.
func (d *Dialect) PlanChanges(cur, want *model.Physical, changes []plan.Change) ([]plan.Step, error) {
	var dropFKs, dropTables, createTables, alters, addFKs []plan.Step
	var created, dropped []string
	lateAdds := map[string]bool{}

	for _, ch := range changes {
		switch ch.Kind {
		case plan.CreateTable:
			created = append(created, ch.Table)
		case plan.DropTable:
			dropped = append(dropped, ch.Table)
		case plan.RecreateTable:
			created = append(created, ch.Table)
			dropped = append(dropped, ch.Table)
		}
	}

	if len(dropped) > 0 {
		slices.Sort(dropped)
		dropTables = append(dropTables, plan.Step{
			Description: string(plan.DropTable) + " " + strings.Join(dropped, ", "), Destructive: true,
			Statements: []string{"DROP TABLE " + dialect.QuoteList(dropped)},
		})
	}
	order, late := dialect.CreationOrder(want, created)
	for _, name := range order {
		t := want.Tables[name]
		stmt, err := d.createTable(want, t, func(fk string) bool { return late[name+"|"+fk] })
		if err != nil {
			return nil, err
		}
		desc := string(plan.CreateTable) + " " + name
		if slices.Contains(dropped, name) {
			desc = string(plan.RecreateTable) + " " + name
		}
		createTables = append(createTables, plan.Step{Description: desc, Statements: []string{stmt}})
		for _, fk := range t.ForeignKeys {
			if late[name+"|"+fk.Name] {
				lateAdds[name+"|"+fk.Name] = true
				addFKs = append(addFKs, plan.Step{
					Description: string(plan.AddForeignKey) + " " + name + "." + fk.Name,
					Statements:  []string{alterTable(name, "ADD "+fkConstraint(name, &fk))},
				})
			}
		}
	}

	for _, ch := range changes {
		step := plan.Step{Description: ch.String(), Destructive: ch.Destructive}
		t := ch.Table
		switch ch.Kind {
		case plan.DropForeignKey:
			step.Statements = []string{alterTable(t, "DROP CONSTRAINT "+dialect.QuoteIdent(model.ForeignKeyName(t, ch.Name)))}
			dropFKs = append(dropFKs, step)
		case plan.AddForeignKey:
			fk := want.Tables[t].ForeignKey(ch.Name)
			step.Statements = []string{alterTable(t, "ADD "+fkConstraint(t, fk))}
			addFKs = append(addFKs, step)
		case plan.AddColumn:
			f := want.Tables[t].Field(ch.Column)
			def, err := d.columnDef(want, f)
			if err != nil {
				return nil, err
			}
			if chk := d.ColumnCheck(f.Name, want.Domain(f)); chk != "" {
				def += " " + checkConstraint(t, f.Name, chk)
			}
			step.Statements = []string{alterTable(t, "ADD COLUMN "+def)}
			alters = append(alters, step)
		case plan.DropColumn:
			step.Statements = []string{alterTable(t, "DROP COLUMN "+dialect.QuoteIdent(ch.Column))}
			alters = append(alters, step)
		case plan.AlterType:
			f := want.Tables[t].Field(ch.Column)
			typ := d.ColumnType(want.Domain(f))
			col := dialect.QuoteIdent(ch.Column)
			action := "ALTER COLUMN " + col + " TYPE " + typ
			// An explicit cast would silently truncate strings, so USING is
			// only added when the base type changes.
			if cur.Domain(cur.Tables[t].Field(ch.Column)).Type != want.Domain(f).Type {
				action += " USING " + col + "::" + typ
			}
			step.Statements = []string{alterTable(t, action)}
			alters = append(alters, step)
		case plan.AlterValues:
			f := want.Tables[t].Field(ch.Column)
			step.Statements = []string{alterTable(t, "DROP CONSTRAINT IF EXISTS "+dialect.QuoteIdent(model.CheckName(t, f.Name)))}
			if chk := d.ColumnCheck(f.Name, want.Domain(f)); chk != "" {
				step.Statements = append(step.Statements, alterTable(t, "ADD "+checkConstraint(t, f.Name, chk)))
			}
			alters = append(alters, step)
		case plan.SetNotNull:
			step.Statements = []string{alterTable(t, "ALTER COLUMN "+dialect.QuoteIdent(ch.Column)+" SET NOT NULL")}
			alters = append(alters, step)
		case plan.DropNotNull:
			step.Statements = []string{alterTable(t, "ALTER COLUMN "+dialect.QuoteIdent(ch.Column)+" DROP NOT NULL")}
			alters = append(alters, step)
		case plan.SetDefault:
			f := want.Tables[t].Field(ch.Column)
			action := "DROP DEFAULT"
			if f.Default != nil {
				lit, err := d.Literal(want.Domain(f), *f.Default)
				if err != nil {
					return nil, err
				}
				action = "SET DEFAULT " + lit
			}
			step.Statements = []string{alterTable(t, "ALTER COLUMN "+dialect.QuoteIdent(ch.Column)+" "+action)}
			alters = append(alters, step)
		case plan.AlterPrimaryKey:
			pk := dialect.QuoteIdent(model.PrimaryKeyName(t))
			step.Statements = []string{
				alterTable(t, "DROP CONSTRAINT "+pk),
				alterTable(t, "ADD CONSTRAINT "+pk+" PRIMARY KEY ("+dialect.QuoteList(want.Tables[t].PrimaryKey)+")"),
			}
			alters = append(alters, step)
		}
	}
	return slices.Concat(dropFKs, dropTables, createTables, alters, addFKs), nil
}

// Transact implements dialect.Dialect.
func (*Dialect) Transact(ctx context.Context, db *sql.DB, opts dialect.TxOptions, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return err
	}
	if opts.DeferForeignKeys {
		if _, err = tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// TableExists implements dialect.Dialect.
func (*Dialect) TableExists(ctx context.Context, q dialect.Querier, name string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = current_schema() AND tablename = $1", name).Scan(&n)
	return n > 0, err
}

// Encode implements dialect.Dialect.
func (*Dialect) Encode(d *model.Domain, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if d.Type == model.Blob {
		return dialect.EncodeBlob(v)
	}
	canon, err := dialect.Canonicalize(d, v)
	if err != nil {
		return nil, err
	}
	switch d.Type {
	case model.Int4, model.Int8:
		return strconv.ParseInt(canon, 10, 64)
	case model.Bool:
		return canon == "true", nil
	case model.Timestamp:
		return time.Parse(model.TimestampLayout, canon)
	}
	return canon, nil
}

// Decode implements dialect.Dialect.
func (*Dialect) Decode(d *model.Domain, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch d.Type {
	case model.Int4, model.Int8:
		switch n := v.(type) {
		case int64:
			return n, nil
		case int32:
			return int64(n), nil
		}
	case model.Dec:
		switch x := v.(type) {
		case string:
			return d.Canonical(x)
		case []byte:
			return d.Canonical(string(x))
		case float64:
			return d.Canonical(strconv.FormatFloat(x, 'f', d.Decimals, 64))
		}
	case model.Bool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case model.Date, model.Timestamp:
		if t, ok := v.(time.Time); ok {
			if d.Type == model.Timestamp {
				return t.UTC(), nil
			}
			return t, nil
		}
	case model.UUID:
		switch x := v.(type) {
		case string:
			return x, nil
		case [16]byte:
			return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16]), nil
		}
	case model.Blob:
		if b, ok := v.([]byte); ok {
			return b, nil
		}
	default:
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unexpected value %T for %s", v, d.TypeString())
}

// ClassifyError implements dialect.Dialect.
func (*Dialect) ClassifyError(err error) error {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	cv := &rterr.ConstraintViolation{Table: pe.TableName, Constraint: pe.ConstraintName, Err: err}
	switch pe.Code {
	case "23503":
		cv.Kind = rterr.ViolationForeignKey
	case "23514":
		cv.Kind = rterr.ViolationCheck
	case "23502":
		cv.Kind = rterr.ViolationNotNull
	case "23505":
		cv.Kind = rterr.ViolationUnique
	case "22001", "22003", "22007", "22008": // too long, numeric overflow, invalid date or time
		cv.Kind = rterr.ViolationCheck
	default:
		return err
	}
	return cv
}
