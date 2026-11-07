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

package postgres

import (
	"context"
	"strings"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

const tablesQuery = `
SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull, a.atthasdef
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p')
ORDER BY c.relname, a.attnum`

const constraintsQuery = `
SELECT cl.relname, con.conname, con.contype, COALESCE(tcl.relname, ''), k.ord, a.attname, COALESCE(fa.attname, '')
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class cl ON cl.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = cl.relnamespace
LEFT JOIN pg_catalog.pg_class tcl ON tcl.oid = con.confrelid
CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
LEFT JOIN pg_catalog.pg_attribute fa ON fa.attrelid = con.confrelid AND fa.attnum = con.confkey[k.ord]
WHERE n.nspname = current_schema() AND con.contype IN ('p', 'f', 'c')
ORDER BY cl.relname, con.conname, k.ord`

// Introspect implements dialect.Dialect.
func (d *Dialect) Introspect(ctx context.Context, q dialect.Querier) (*dialect.Catalog, error) {
	cat := &dialect.Catalog{Tables: map[string]*dialect.CatalogTable{}}
	table := func(name string) *dialect.CatalogTable {
		t, ok := cat.Tables[name]
		if !ok {
			t = &dialect.CatalogTable{Columns: map[string]dialect.CatalogColumn{}, Constraints: map[string]bool{}}
			cat.Tables[name] = t
		}
		return t
	}
	rows, err := q.QueryContext(ctx, tablesQuery)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tname, col, typ string
		var notNull, hasDef bool
		if err := rows.Scan(&tname, &col, &typ, &notNull, &hasDef); err != nil {
			rows.Close()
			return nil, err
		}
		table(tname).Columns[col] = dialect.CatalogColumn{Type: normalizeType(typ), NotNull: notNull, HasDefault: hasDef}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.QueryContext(ctx, constraintsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fks := map[string]*dialect.CatalogForeignKey{}
	var fkOrder []string
	for rows.Next() {
		var tname, cname, ctype, target, col, ref string
		var ord int
		if err := rows.Scan(&tname, &cname, &ctype, &target, &ord, &col, &ref); err != nil {
			return nil, err
		}
		t := table(tname)
		t.Constraints[cname] = true
		switch ctype {
		case "p":
			t.PrimaryKey = append(t.PrimaryKey, col)
		case "f":
			key := tname + "\x00" + cname
			fk, ok := fks[key]
			if !ok {
				fk = &dialect.CatalogForeignKey{Target: target}
				fks[key] = fk
				fkOrder = append(fkOrder, key)
			}
			fk.Columns = append(fk.Columns, model.ColumnPair{Field: col, References: ref})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, key := range fkOrder {
		tname, _, _ := strings.Cut(key, "\x00")
		cat.Tables[tname].ForeignKeys = append(cat.Tables[tname].ForeignKeys, *fks[key])
	}
	return cat, nil
}

// normalizeType maps format_type output to the spelling of ColumnType.
func normalizeType(t string) string {
	for from, to := range map[string]string{
		"character varying":        "VARCHAR",
		"numeric":                  "NUMERIC",
		"timestamp with time zone": "TIMESTAMPTZ",
	} {
		if rest, ok := strings.CutPrefix(t, from); ok {
			return to + rest
		}
	}
	return strings.ToUpper(t)
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
			ct.Columns[f.Name] = dialect.CatalogColumn{Type: d.ColumnType(dom), NotNull: f.NotNull, HasDefault: f.Default != nil}
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
