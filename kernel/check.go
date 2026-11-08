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

package kernel

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/rterr"
)

// Discrepancy is a difference between the repository and the actual
// database schema.
type Discrepancy struct {
	Object  string `json:"object"`
	Problem string `json:"problem"`
}

func (d Discrepancy) String() string { return d.Object + ": " + d.Problem }

// Check compares the layout described by the repository with the tables
// that actually exist and reports every difference. It also validates the
// repository itself, so business objects and other definitions that became invalid
// are reported. The repository is never changed by this.
func (k *Kernel) Check(ctx context.Context) ([]Discrepancy, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	actual, err := k.d.Introspect(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := compareCatalogs(k.d.Expected(s.Physical()), actual)
	err = k.validate(ctx, k.db, s)
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		out = append(out, Discrepancy{Object: v.Object, Problem: v.Message})
	}
	for _, m := range rterr.All[*rterr.MissingLanguages](err) {
		out = append(out, Discrepancy{Object: "languages", Problem: m.Error()})
	}
	if err != nil && len(out) == 0 {
		return nil, err
	}
	names, err := k.repo.CheckGeneratedNames(ctx, k.db)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		out = append(out, Discrepancy{Object: n.Object, Problem: "stored generated name " + n.Stored + " differs from " + n.Computed})
	}
	cols, err := k.repo.CheckColumns(ctx, k.db, s.Physical())
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		out = append(out, Discrepancy{Object: c.Object, Problem: c.Problem})
	}
	problems, err := k.repo.VerifyContents(ctx, k.db)
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		out = append(out, Discrepancy{Object: "asset content " + p.SHA256, Problem: p.Problem})
	}
	return out, nil
}

func compareCatalogs(want, got *dialect.Catalog) []Discrepancy {
	var out []Discrepancy
	add := func(object, format string, args ...any) {
		out = append(out, Discrepancy{Object: object, Problem: fmt.Sprintf(format, args...)})
	}
	for name, wt := range want.Tables {
		gt, ok := got.Tables[name]
		if !ok {
			add(name, "table is missing in the database")
			continue
		}
		for col, wc := range wt.Columns {
			gc, ok := gt.Columns[col]
			switch {
			case !ok:
				add(name+"."+col, "column is missing")
			case gc.Type != wc.Type:
				add(name+"."+col, "type is %s, expected %s", gc.Type, wc.Type)
			case gc.NotNull != wc.NotNull:
				add(name+"."+col, "NOT NULL is %v, expected %v", gc.NotNull, wc.NotNull)
			case gc.Default != wc.Default:
				add(name+"."+col, "default is %s, expected %s", gc.Default, wc.Default)
			}
		}
		for col := range gt.Columns {
			if _, ok := wt.Columns[col]; !ok {
				add(name+"."+col, "column is not in the repository")
			}
		}
		if !slices.Equal(gt.PrimaryKey, wt.PrimaryKey) {
			add(name, "primary key is (%s), expected (%s)", strings.Join(gt.PrimaryKey, ", "), strings.Join(wt.PrimaryKey, ", "))
		}
		gotFKs := map[string]bool{}
		for _, fk := range gt.ForeignKeys {
			gotFKs[fk.Key()] = true
		}
		wantFKs := map[string]bool{}
		for _, fk := range wt.ForeignKeys {
			wantFKs[fk.Key()] = true
			if !gotFKs[fk.Key()] {
				add(name, "foreign key to %s on (%s) is missing", fk.Target, strings.Join(fieldsOf(fk), ", "))
			}
		}
		for _, fk := range gt.ForeignKeys {
			if !wantFKs[fk.Key()] {
				add(name, "foreign key to %s on (%s) is not in the repository", fk.Target, strings.Join(fieldsOf(fk), ", "))
			}
		}
		for c := range wt.Constraints {
			if !gt.Constraints[c] {
				add(name, "constraint %s is missing", c)
			}
		}
		for c := range gt.Constraints {
			if !wt.Constraints[c] {
				add(name, "constraint %s is not in the repository", c)
			}
		}
	}
	for name := range got.Tables {
		if _, ok := want.Tables[name]; !ok {
			add(name, "table is not in the repository")
		}
	}
	slices.SortFunc(out, func(a, b Discrepancy) int {
		return cmp.Or(cmp.Compare(a.Object, b.Object), cmp.Compare(a.Problem, b.Problem))
	})
	return out
}

func fieldsOf(fk dialect.CatalogForeignKey) []string {
	out := make([]string, len(fk.Columns))
	for i, c := range fk.Columns {
		out[i] = c.Field
	}
	return out
}
