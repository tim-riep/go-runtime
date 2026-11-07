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

package kernel_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/timriep/go-runtime/dialect"
	"github.com/timriep/go-runtime/kernel"
	"github.com/timriep/go-runtime/rterr"
)

type values map[string]any

// raw is passed to the database without encoding, to test constraints
// that the encoder would already reject.
type raw struct{ v any }

// insert writes a row with the dialect's value encoding and returns
// constraint violations as rterr.ConstraintViolation.
func insert(t *testing.T, k *kernel.Kernel, table string, row values) error {
	t.Helper()
	ctx := context.Background()
	s, err := k.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Physical()
	tab := p.Tables[table]
	if tab == nil {
		t.Fatalf("unknown table %s", table)
	}
	d := k.Dialect()
	var cols, marks []string
	var args []any
	for c := range row {
		cols = append(cols, c)
	}
	slices.Sort(cols)
	for i, c := range cols {
		f := tab.Field(c)
		if f == nil {
			t.Fatalf("unknown field %s.%s", table, c)
		}
		if r, ok := row[c].(raw); ok {
			args = append(args, r.v)
		} else {
			v, err := d.Encode(p.Domain(f), row[c])
			if err != nil {
				t.Fatalf("encoding %s: %v", c, err)
			}
			args = append(args, v)
		}
		marks = append(marks, d.Placeholder(i+1))
	}
	stmt := "INSERT INTO " + d.QuoteIdent(table) + " (" + dialect.QuoteList(cols) + ") VALUES (" + strings.Join(marks, ", ") + ")"
	_, err = k.DB().ExecContext(ctx, stmt, args...)
	return d.ClassifyError(err)
}

func mustInsert(t *testing.T, k *kernel.Kernel, table string, row values) {
	t.Helper()
	if err := insert(t, k, table, row); err != nil {
		t.Fatalf("insert into %s: %v", table, err)
	}
}

// violation fails unless err is a constraint violation of the given kind.
func violation(t *testing.T, err error, kind rterr.ViolationKind, what string) {
	t.Helper()
	var cv *rterr.ConstraintViolation
	if !errors.As(err, &cv) || cv.Kind != kind {
		t.Fatalf("%s: expected %s violation, got %v", what, kind, err)
	}
}

// scalar reads one value with the dialect's decoding.
func scalar(t *testing.T, k *kernel.Kernel, table, field, where string, args ...any) any {
	t.Helper()
	ctx := context.Background()
	s, err := k.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Physical()
	f := p.Tables[table].Field(field)
	d := k.Dialect()
	var raw any
	q := "SELECT " + d.QuoteIdent(field) + " FROM " + d.QuoteIdent(table) + " WHERE " + where
	if err := k.DB().QueryRowContext(ctx, q, args...).Scan(&raw); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	v, err := d.Decode(p.Domain(f), raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func count(t *testing.T, k *kernel.Kernel, table string) int {
	t.Helper()
	var n int
	if err := k.DB().QueryRow("SELECT count(*) FROM " + k.Dialect().QuoteIdent(table)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// where builds "a = ? AND b = ?" with the dialect's placeholders.
func where(k *kernel.Kernel, cols ...string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = k.Dialect().QuoteIdent(c) + " = " + k.Dialect().Placeholder(i+1)
	}
	return strings.Join(parts, " AND ")
}
