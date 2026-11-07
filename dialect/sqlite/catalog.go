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

package sqlite

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

var constraintName = regexp.MustCompile(`CONSTRAINT\s+"((?:[^"]|"")*)"`)

// Introspect implements dialect.Dialect. SQLite does not report constraint
// names through its pragmas, so they are read from the stored CREATE
// statements.
func (d *Dialect) Introspect(ctx context.Context, q dialect.Querier) (*dialect.Catalog, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, sql FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`)
	if err != nil {
		return nil, err
	}
	defs := map[string]string{}
	for rows.Next() {
		var name string
		var def sql.NullString
		if err := rows.Scan(&name, &def); err != nil {
			rows.Close()
			return nil, err
		}
		defs[name] = def.String
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cat := &dialect.Catalog{Tables: map[string]*dialect.CatalogTable{}}
	for name, def := range defs {
		t := &dialect.CatalogTable{Columns: map[string]dialect.CatalogColumn{}, Constraints: map[string]bool{}}
		for _, m := range constraintName.FindAllStringSubmatch(def, -1) {
			t.Constraints[strings.ReplaceAll(m[1], `""`, `"`)] = true
		}
		if err := columns(ctx, q, name, t); err != nil {
			return nil, err
		}
		if err := foreignKeys(ctx, q, name, t); err != nil {
			return nil, err
		}
		cat.Tables[name] = t
	}
	return cat, nil
}

func columns(ctx context.Context, q dialect.Querier, table string, t *dialect.CatalogTable) error {
	rows, err := q.QueryContext(ctx, `SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	keys := map[int]string{}
	for rows.Next() {
		var name, typ string
		var notNull bool
		var def sql.NullString
		var pk int
		if err := rows.Scan(&name, &typ, &notNull, &def, &pk); err != nil {
			return err
		}
		kind := model.DefaultNone
		switch {
		case def.Valid && strings.EqualFold(def.String, "NULL"):
			kind = model.DefaultNull
		case def.Valid:
			kind = model.DefaultValue
		}
		t.Columns[name] = dialect.CatalogColumn{Type: strings.ToUpper(typ), NotNull: notNull, Default: kind}
		if pk > 0 {
			keys[pk] = name
		}
	}
	for i := 1; i <= len(keys); i++ {
		t.PrimaryKey = append(t.PrimaryKey, keys[i])
	}
	return rows.Err()
}

func foreignKeys(ctx context.Context, q dialect.Querier, table string, t *dialect.CatalogTable) error {
	rows, err := q.QueryContext(ctx, `SELECT id, seq, "table", "from", "to" FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[int]*dialect.CatalogForeignKey{}
	var ids []int
	for rows.Next() {
		var id, seq int
		var target, from, to string
		if err := rows.Scan(&id, &seq, &target, &from, &to); err != nil {
			return err
		}
		fk, ok := byID[id]
		if !ok {
			fk = &dialect.CatalogForeignKey{Target: target}
			byID[id] = fk
			ids = append(ids, id)
		}
		fk.Columns = append(fk.Columns, model.ColumnPair{Field: from, References: to})
	}
	sort.Ints(ids)
	for _, id := range ids {
		t.ForeignKeys = append(t.ForeignKeys, *byID[id])
	}
	return rows.Err()
}

// Expected implements dialect.Dialect.
func (d *Dialect) Expected(p *model.Physical) *dialect.Catalog {
	cat := &dialect.Catalog{Tables: map[string]*dialect.CatalogTable{}}
	for name, t := range p.Tables {
		ct := &dialect.CatalogTable{Columns: map[string]dialect.CatalogColumn{}, Constraints: map[string]bool{}, PrimaryKey: t.PrimaryKey}
		ct.Constraints[model.PrimaryKeyName(name)] = true
		for i := range t.Fields {
			f := &t.Fields[i]
			dom := p.Domain(f)
			ct.Columns[f.Name] = dialect.CatalogColumn{Type: d.ColumnType(dom), NotNull: f.NotNull, Default: f.ResolvedDefault.Kind}
			if d.ColumnCheck(f.Name, dom) != "" {
				ct.Constraints[model.CheckName(name, f.Name)] = true
			}
		}
		for _, fk := range t.ForeignKeys {
			ct.Constraints[model.ForeignKeyName(name, fk.Name)] = true
			ct.ForeignKeys = append(ct.ForeignKeys, dialect.CatalogForeignKey{Target: fk.Target, Columns: fk.Columns})
		}
		cat.Tables[name] = ct
	}
	return cat
}
