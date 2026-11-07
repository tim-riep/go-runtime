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
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

const tValues = "/TRNULL/VALUES"

// nullPackage writes a package whose fields cover null, "", allowEmpty and
// the three kinds of defaults.
func nullPackage(t *testing.T) *model.Package {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "TRNULL")
	w := func(rel, doc string) { testutil.WriteFile(t, dir, rel, []byte(doc)) }
	w("manifest.json", `{"id":"TRNULL","version":"1.0.0"}`)
	w("schema/domains/ID.json", `{"name":"/TRNULL/ID","type":"INT4"}`)
	w("schema/domains/TEXT.json", `{"name":"/TRNULL/TEXT","type":"STRING"}`)
	w("schema/domains/LABEL.json", `{"name":"/TRNULL/LABEL","type":"CHAR","length":10}`)
	w("schema/domains/CODE.json", `{"name":"/TRNULL/CODE","type":"CHAR","length":5,"allowEmpty":false}`)
	w("schema/domains/WITH_DEFAULT.json", `{"name":"/TRNULL/WITH_DEFAULT","type":"CHAR","length":10,"default":"X"}`)
	w("schema/tables/VALUES.json", `{"name":"/TRNULL/VALUES","tenantSpecific":false,"dataClass":"configuration",
		"fields":[
			{"name":"ID","domain":"/TRNULL/ID"},
			{"name":"OPT","domain":"/TRNULL/TEXT"},
			{"name":"REQ","domain":"/TRNULL/TEXT","notNull":true,"default":""},
			{"name":"CODE","domain":"/TRNULL/CODE"},
			{"name":"TRAIL","domain":"/TRNULL/LABEL"},
			{"name":"D_NONE","domain":"/TRNULL/LABEL"},
			{"name":"D_NULL","domain":"/TRNULL/LABEL","default":null},
			{"name":"D_EMPTY","domain":"/TRNULL/LABEL","default":""},
			{"name":"D_DOMAIN","domain":"/TRNULL/WITH_DEFAULT"},
			{"name":"D_OVERRIDE","domain":"/TRNULL/WITH_DEFAULT","default":null}],
		"primaryKey":["ID"]}`)
	w("schema/settings/VALUES.json", `{"id":"/TRNULL/VALUES","texts":{"en":{"label":"Values","description":""}},
		"root":{"table":"/TRNULL/VALUES","operations":{"create":true,"update":true,"delete":true}}}`)
	return testutil.LoadDir(t, dir)
}

func TestNullAndEmptyString(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		pkg := nullPackage(t)

		// DDL: no default, DEFAULT NULL and DEFAULT '' are three cases.
		dry, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		ddl := dry.SQL()
		for col, want := range map[string]string{
			"D_NONE": `^\s*"D_NONE" (TEXT|VARCHAR\(10\)),$`, "D_NULL": `"D_NULL" (TEXT|VARCHAR\(10\)) DEFAULT NULL,`,
			"D_EMPTY": `"D_EMPTY" (TEXT|VARCHAR\(10\)) DEFAULT '',`, "D_DOMAIN": `"D_DOMAIN" (TEXT|VARCHAR\(10\)) DEFAULT 'X',`,
			"D_OVERRIDE": `"D_OVERRIDE" (TEXT|VARCHAR\(10\)) DEFAULT NULL,`, "REQ": `"REQ" TEXT NOT NULL DEFAULT '',`,
		} {
			if !regexp.MustCompile(`(?m)` + want).MatchString(ddl) {
				t.Errorf("column %s: no match for %s in\n%s", col, want, ddl)
			}
		}
		if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)

		// Repository: declared defaults keep their kind.
		s, _ := k.Schema(ctx)
		tab := s.Tables[tValues]
		for field, want := range map[string]model.Default{
			"D_NONE": model.NoDefault(), "D_NULL": model.NullDefault(), "D_EMPTY": model.ValueDefault(""),
			"D_DOMAIN": model.NoDefault(), "D_OVERRIDE": model.NullDefault(), "REQ": model.ValueDefault(""),
		} {
			if got := tab.Field(field).Default; got != want {
				t.Errorf("declared default of %s: %+v, want %+v", field, got, want)
			}
		}
		if d := s.Domains["/TRNULL/WITH_DEFAULT"]; d.Default != model.ValueDefault("X") {
			t.Errorf("domain default: %+v", d.Default)
		}
		if d := s.Domains["/TRNULL/CODE"]; d.AllowEmpty != model.Some(false) || s.Domains["/TRNULL/TEXT"].AllowEmpty.Valid() {
			t.Errorf("allowEmpty: %+v", d.AllowEmpty)
		}
		if got := s.Physical().Tables[tValues].Field("D_DOMAIN").ResolvedDefault; got != model.ValueDefault("X") {
			t.Errorf("effective default of D_DOMAIN: %+v", got)
		}

		// Defaults applied by the database.
		mustInsert(t, k, tValues, values{"ID": 1})
		w := where(k, "ID")
		for field, want := range map[string]any{"D_NONE": nil, "D_NULL": nil, "D_EMPTY": "", "D_DOMAIN": "X", "D_OVERRIDE": nil, "REQ": "", "OPT": nil} {
			if got := scalar(t, k, tValues, field, w, 1); got != want {
				t.Errorf("%s after insert: %#v, want %#v", field, got, want)
			}
		}

		// null and "" in the same nullable column.
		mustInsert(t, k, tValues, values{"ID": 2, "OPT": nil})
		mustInsert(t, k, tValues, values{"ID": 3, "OPT": ""})
		if got := scalar(t, k, tValues, "OPT", w, 2); got != nil {
			t.Errorf("null read back as %#v", got)
		}
		if got := scalar(t, k, tValues, "OPT", w, 3); got != "" {
			t.Errorf(`"" read back as %#v`, got)
		}

		// notNull forbids null but allows "".
		mustInsert(t, k, tValues, values{"ID": 4, "REQ": ""})
		violation(t, insert(t, k, tValues, values{"ID": 5, "REQ": nil}), rterr.ViolationNotNull, "null in a notNull column")

		// allowEmpty false forbids "".
		violation(t, insert(t, k, tValues, values{"ID": 6, "CODE": ""}), rterr.ViolationCheck, `"" with allowEmpty false`)
		mustInsert(t, k, tValues, values{"ID": 7, "CODE": nil})

		// Trailing spaces are kept and not ignored in comparisons.
		mustInsert(t, k, tValues, values{"ID": 8, "TRAIL": "ab  "})
		if got := scalar(t, k, tValues, "TRAIL", w, 8); got != "ab  " {
			t.Errorf("trailing spaces: %q", got)
		}
		var n int
		if err := k.DB().QueryRowContext(ctx, `SELECT count(*) FROM "/TRNULL/VALUES" WHERE "TRAIL" = 'ab'`).Scan(&n); err != nil || n != 0 {
			t.Errorf("'ab' matched 'ab  ': %d %v", n, err)
		}

		// The resolved definition distinguishes the three defaults.
		def, err := k.Setting(ctx, tValues)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := def.JSON()
		js := string(b)
		for _, want := range []string{`"default": null`, `"default": ""`, `"default": "X"`, `"description": ""`} {
			if !strings.Contains(js, want) {
				t.Errorf("definition lacks %s", want)
			}
		}
		if f := def.Root.Field("D_NONE"); f.Default.Kind != model.DefaultNone || f.AllowEmpty != model.Some(true) {
			t.Errorf("D_NONE: %+v", f)
		}
		if f := def.Root.Field("CODE"); f.AllowEmpty != model.Some(false) {
			t.Errorf("CODE: %+v", f)
		}
	})
}

// TestUniqueNullAndEmpty checks the value encoding of the kernel against a
// UNIQUE column: several nulls are allowed, "" only once. Packages cannot
// declare unique constraints yet, so the table is created with SQL.
func TestUniqueNullAndEmpty(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.DB().ExecContext(ctx, `CREATE TABLE "UNIQUE_TEST" ("ID" INTEGER PRIMARY KEY, "V" VARCHAR(10) UNIQUE)`); err != nil {
			t.Fatal(err)
		}
		dom := &model.Domain{Name: "/TEST/V", Type: model.Char, Length: 10}
		put := func(id int, v any) error {
			enc, err := k.Dialect().Encode(dom, v)
			if err != nil {
				t.Fatal(err)
			}
			_, err = k.DB().ExecContext(ctx, `INSERT INTO "UNIQUE_TEST" ("ID", "V") VALUES (`+k.Dialect().Placeholder(1)+`, `+k.Dialect().Placeholder(2)+`)`, id, enc)
			return k.Dialect().ClassifyError(err)
		}
		for i := 1; i <= 3; i++ {
			if err := put(i, nil); err != nil {
				t.Fatalf("null %d: %v", i, err)
			}
		}
		if err := put(4, ""); err != nil {
			t.Fatal(err)
		}
		violation(t, put(5, ""), rterr.ViolationUnique, `second ""`)
	})
}
