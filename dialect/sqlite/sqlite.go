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

// Package sqlite implements the dialect for SQLite using modernc.org/sqlite.
//
// Storage of the base types:
//
//	CHAR, STRING     TEXT, CHAR length enforced by a check
//	INT4, INT8       INTEGER, INT4 range enforced by a check
//	DEC(p,s)         INTEGER holding value * 10^s, range enforced by a check
//	DATE             TEXT YYYY-MM-DD, validated by a check
//	TIMESTAMP        TEXT YYYY-MM-DDTHH:MM:SS.ffffffZ in UTC, validated by a check
//	BOOL             INTEGER 0 or 1
//	UUID             TEXT in lower case, validated by a check
//	BLOB             BLOB
//
// All tables are STRICT, so SQLite enforces the storage classes.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// Dialect is the SQLite dialect.
type Dialect struct{}

// New returns the SQLite dialect.
func New() *Dialect { return &Dialect{} }

var _ dialect.Dialect = (*Dialect)(nil)

// Name implements dialect.Dialect.
func (*Dialect) Name() string { return "sqlite" }

// Open opens the database file at path. Every connection enforces foreign
// keys, waits for locks and starts write transactions immediately.
func (*Dialect) Open(path string) (*sql.DB, error) {
	dsn := path
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate"
	return sql.Open("sqlite", dsn)
}

// QuoteIdent implements dialect.Dialect.
func (*Dialect) QuoteIdent(name string) string { return dialect.QuoteIdent(name) }

// Placeholder implements dialect.Dialect.
func (*Dialect) Placeholder(int) string { return "?" }

// ColumnType implements dialect.Dialect.
func (*Dialect) ColumnType(d *model.Domain) string {
	switch d.Type {
	case model.Int4, model.Int8, model.Dec, model.Bool:
		return "INTEGER"
	case model.Blob:
		return "BLOB"
	}
	return "TEXT"
}

var (
	timestampGlob = "'" + strings.Repeat("[0-9]", 4) + "-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]." + strings.Repeat("[0-9]", 6) + "Z'"
	uuidGlob      = "'" + hex(8) + "-" + hex(4) + "-" + hex(4) + "-" + hex(4) + "-" + hex(12) + "'"
)

func hex(n int) string { return strings.Repeat("[0-9a-f]", n) }

// ColumnCheck implements dialect.Dialect.
func (*Dialect) ColumnCheck(column string, d *model.Domain) string {
	c := dialect.QuoteIdent(column)
	var conds []string
	switch d.Type {
	case model.Char:
		conds = append(conds, fmt.Sprintf("length(%s) <= %d", c, d.Length))
	case model.Int4:
		conds = append(conds, c+" BETWEEN -2147483648 AND 2147483647")
	case model.Dec:
		limit := strings.Repeat("9", d.Length)
		conds = append(conds, fmt.Sprintf("%s BETWEEN -%s AND %s", c, limit, limit))
	case model.Date:
		conds = append(conds, c+" IS date("+c+")")
	case model.Timestamp:
		conds = append(conds, c+" GLOB "+timestampGlob,
			"strftime('%Y-%m-%dT%H:%M:%S', substr("+c+", 1, 19)) IS substr("+c+", 1, 19)")
	case model.Bool:
		conds = append(conds, c+" IN (0, 1)")
	case model.UUID:
		conds = append(conds, c+" GLOB "+uuidGlob)
	}
	if d.Type.IsString() && !d.AllowsEmpty() {
		conds = append(conds, c+" <> ''")
	}
	if len(d.Values) > 0 {
		conds = append(conds, c+" IN ("+dialect.ValueList(d)+")")
	}
	if len(conds) == 0 {
		return ""
	}
	return c + " IS NULL OR (" + strings.Join(conds, " AND ") + ")"
}

// Literal implements dialect.Dialect.
func (*Dialect) Literal(d *model.Domain, canonical string) (string, error) {
	switch d.Type {
	case model.Int4, model.Int8:
		return canonical, nil
	case model.Dec:
		v, err := model.ScaledDecimal(canonical)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(v, 10), nil
	case model.Bool:
		if canonical == "true" {
			return "1", nil
		}
		return "0", nil
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
	def, err := dialect.DefaultClause(d.Literal, dom, f.ResolvedDefault)
	if err != nil {
		return "", err
	}
	return s + def, nil
}

// createTable renders CREATE TABLE for t under the given name. Constraint
// names always derive from t.Name so a rebuilt table keeps them.
func (d *Dialect) createTable(p *model.Physical, t *model.Table, name string) (string, error) {
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
		fk := &t.ForeignKeys[i]
		lines = append(lines, "CONSTRAINT "+dialect.QuoteIdent(model.ForeignKeyName(t.Name, fk.Name))+" "+dialect.ForeignKeyClause(fk))
	}
	for i := range t.Fields {
		f := &t.Fields[i]
		if chk := d.ColumnCheck(f.Name, p.Domain(f)); chk != "" {
			lines = append(lines, "CONSTRAINT "+dialect.QuoteIdent(model.CheckName(t.Name, f.Name))+" CHECK ("+chk+")")
		}
	}
	return "CREATE TABLE " + dialect.QuoteIdent(name) + " (\n  " + strings.Join(lines, ",\n  ") + "\n) STRICT", nil
}

// PlanChanges implements dialect.Dialect. Foreign keys are not enforced
// while a plan runs, so the order of tables does not matter: tables are
// dropped first, then created, then changed. A change that ALTER TABLE
// cannot express rebuilds the table.
func (d *Dialect) PlanChanges(cur, want *model.Physical, changes []plan.Change) ([]plan.Step, error) {
	byTable := dialect.ChangesByTable(changes)
	names := make([]string, 0, len(byTable))
	for n := range byTable {
		names = append(names, n)
	}
	slices.Sort(names)

	var drops, creates, alters []plan.Step
	for _, name := range names {
		chs := byTable[name]
		desc := dialect.Describe(chs)
		destructive := dialect.HasDestructive(chs)
		switch {
		case chs[0].Kind == plan.CreateTable:
			stmt, err := d.createTable(want, want.Tables[name], name)
			if err != nil {
				return nil, err
			}
			creates = append(creates, plan.Step{Description: desc, Statements: []string{stmt}})
		case chs[0].Kind == plan.DropTable:
			drops = append(drops, plan.Step{Description: desc, Destructive: true, Statements: []string{"DROP TABLE " + dialect.QuoteIdent(name)}})
		case chs[0].Kind == plan.RecreateTable:
			stmt, err := d.createTable(want, want.Tables[name], name)
			if err != nil {
				return nil, err
			}
			drops = append(drops, plan.Step{Description: desc, Destructive: true, Statements: []string{"DROP TABLE " + dialect.QuoteIdent(name), stmt}})
		default:
			stmts, rebuilt, err := d.alter(cur, want, cur.Tables[name], want.Tables[name], chs)
			if err != nil {
				return nil, err
			}
			if rebuilt {
				desc = "rebuild table " + name + ": " + desc
			}
			alters = append(alters, plan.Step{Description: desc, Destructive: destructive, Statements: stmts})
		}
	}
	return slices.Concat(drops, creates, alters), nil
}

// alter returns ADD COLUMN statements when every change is a column that
// ALTER TABLE can add, and a rebuild of the table otherwise.
func (d *Dialect) alter(cur, want *model.Physical, c, w *model.Table, chs []plan.Change) (stmts []string, rebuilt bool, err error) {
	var adds []string
	for _, ch := range chs {
		if ch.Kind != plan.AddColumn || !d.canAdd(w, ch.Column) {
			stmts, err := d.rebuild(cur, want, c, w)
			return stmts, true, err
		}
		f := w.Field(ch.Column)
		def, err := d.columnDef(want, f)
		if err != nil {
			return nil, false, err
		}
		if chk := d.ColumnCheck(f.Name, want.Domain(f)); chk != "" {
			def += " CONSTRAINT " + dialect.QuoteIdent(model.CheckName(w.Name, f.Name)) + " CHECK (" + chk + ")"
		}
		adds = append(adds, "ALTER TABLE "+dialect.QuoteIdent(w.Name)+" ADD COLUMN "+def)
	}
	return adds, false, nil
}

// canAdd reports whether ALTER TABLE ADD COLUMN can create the column: it
// is no key field, no foreign key uses it, and NOT NULL comes with a default.
func (d *Dialect) canAdd(t *model.Table, column string) bool {
	f := t.Field(column)
	if f == nil || t.IsKey(column) || (f.NotNull && f.ResolvedDefault.Kind != model.DefaultValue) {
		return false
	}
	for _, fk := range t.ForeignKeys {
		if slices.Contains(fk.FieldNames(), column) {
			return false
		}
	}
	return true
}

func (d *Dialect) rebuild(cur, want *model.Physical, c, w *model.Table) ([]string, error) {
	tmp := model.RebuildName(w.Name)
	create, err := d.createTable(want, w, tmp)
	if err != nil {
		return nil, err
	}
	var cols, exprs []string
	for i := range w.Fields {
		wf := &w.Fields[i]
		cf := c.Field(wf.Name)
		if cf == nil {
			continue
		}
		cols = append(cols, dialect.QuoteIdent(wf.Name))
		exprs = append(exprs, convert(dialect.QuoteIdent(wf.Name), cur.Domain(cf), want.Domain(wf)))
	}
	stmts := []string{create}
	if len(cols) > 0 {
		stmts = append(stmts, "INSERT INTO "+dialect.QuoteIdent(tmp)+" ("+strings.Join(cols, ", ")+") SELECT "+strings.Join(exprs, ", ")+" FROM "+dialect.QuoteIdent(w.Name))
	}
	return append(stmts,
		"DROP TABLE "+dialect.QuoteIdent(w.Name),
		"ALTER TABLE "+dialect.QuoteIdent(tmp)+" RENAME TO "+dialect.QuoteIdent(w.Name),
	), nil
}

// convert returns the expression copying a column from domain from to
// domain to. Only a change of DEC scale needs arithmetic; reducing the scale
// truncates and is therefore a destructive change.
func convert(col string, from, to *model.Domain) string {
	if from.Type != model.Dec || to.Type != model.Dec || from.Decimals == to.Decimals {
		return col
	}
	if delta := to.Decimals - from.Decimals; delta > 0 {
		return col + " * 1" + strings.Repeat("0", delta)
	}
	return col + " / 1" + strings.Repeat("0", from.Decimals-to.Decimals)
}

// Transact implements dialect.Dialect. PRAGMA foreign_keys has no effect
// inside a transaction, so for DDL it is switched off on a dedicated
// connection before BEGIN, all keys are verified with foreign_key_check
// before COMMIT, and it is switched on again afterwards.
func (*Dialect) Transact(ctx context.Context, db *sql.DB, opts dialect.TxOptions, fn func(tx *sql.Tx) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if opts.DDL {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return err
		}
		defer func() {
			if _, rerr := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); rerr != nil {
				// Never hand a connection without foreign keys back to the pool.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				err = errors.Join(err, rerr)
			}
		}()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if opts.DeferForeignKeys {
		if _, err = tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	if opts.DDL {
		if err = foreignKeyCheck(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func foreignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		if !slices.Contains(tables, table) {
			tables = append(tables, table)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(tables) > 0 {
		return &rterr.ConstraintViolation{Kind: rterr.ViolationForeignKey, Table: strings.Join(tables, ", "),
			Err: errors.New("rows violate foreign keys after the change")}
	}
	return nil
}

// TableExists implements dialect.Dialect.
func (*Dialect) TableExists(ctx context.Context, q dialect.Querier, name string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", name).Scan(&n)
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
	case model.Dec:
		return model.ScaledDecimal(canon)
	case model.Bool:
		if canon == "true" {
			return int64(1), nil
		}
		return int64(0), nil
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
		if n, ok := v.(int64); ok {
			return n, nil
		}
	case model.Dec:
		if n, ok := v.(int64); ok {
			return model.UnscaleDecimal(n, d.Decimals), nil
		}
	case model.Bool:
		if n, ok := v.(int64); ok {
			return n != 0, nil
		}
	case model.Date, model.Timestamp:
		layout := model.DateLayout
		if d.Type == model.Timestamp {
			layout = model.TimestampLayout
		}
		if s, ok := v.(string); ok {
			return time.Parse(layout, s)
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

// Extended result codes of constraint violations.
const (
	codeCheck      = 275
	codeForeignKey = 787
	codeBusy       = 5
	codeNotNull    = 1299
	codePrimaryKey = 1555
	codeUnique     = 2067
)

// ClassifyError implements dialect.Dialect.
func (*Dialect) ClassifyError(err error) error {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return err
	}
	if se.Code()&0xff == codeBusy {
		return &rterr.LockTimeout{Err: err}
	}
	cv := &rterr.ConstraintViolation{Err: err}
	msg := se.Error()
	switch se.Code() {
	case codeForeignKey:
		cv.Kind = rterr.ViolationForeignKey
	case codeCheck:
		cv.Kind = rterr.ViolationCheck
		if _, after, ok := strings.Cut(msg, "CHECK constraint failed: "); ok {
			cv.Constraint = strings.TrimSpace(strings.Split(after, " (")[0])
			cv.Table, _, _ = strings.Cut(cv.Constraint, "#")
		}
	case codeNotNull:
		cv.Kind = rterr.ViolationNotNull
	case codePrimaryKey, codeUnique:
		cv.Kind = rterr.ViolationUnique
	default:
		return err
	}
	return cv
}
