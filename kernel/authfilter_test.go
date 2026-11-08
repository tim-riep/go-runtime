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
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// Codes with characters that SQL patterns treat specially, upper and lower
// case, umlauts and the empty string.
var filterCodes = []string{"", "A", "a", "AB", "Ab", "A%", "A_", `A\`, "%", "_", `\`, "Ä", "Äb", "B", "BC", "C", "c%d", "D_E", "[x", "*", "?", "Z", "z"}

var filterLevels = []int{-5, -1, 0, 1, 5, 9, 10, 12, 19, 20, 21, 100, 123}

type doc struct {
	id    int64
	code  *string
	level *int
}

// seedDocs adds documents with every code and level and some nulls.
func seedDocs(t *testing.T, k *kernel.Kernel, r *rand.Rand, n int) []doc {
	t.Helper()
	var docs []doc
	for i := range n {
		d := doc{id: int64(i + 1)}
		if r.IntN(10) > 0 {
			c := filterCodes[(i+r.IntN(3))%len(filterCodes)]
			d.code = &c
		}
		if r.IntN(10) > 0 {
			l := filterLevels[r.IntN(len(filterLevels))]
			d.level = &l
		}
		code, level := "null", "null"
		if d.code != nil {
			b, _ := json.Marshal(*d.code)
			code = string(b)
		}
		if d.level != nil {
			level = strconv.Itoa(*d.level)
		}
		mustCall(t, k, "ACME", "/TRAUTH/DOC_ACCESS", "ADD", strconv.FormatInt(d.id, 10), code, level)
		docs = append(docs, d)
	}
	return docs
}

// randomRole returns a role with one to three authorizations on
// /TRAUTH/DOC of values, ranges, prefixes and *.
func randomRole(r *rand.Rand) string {
	code := func() string {
		switch r.IntN(5) {
		case 0:
			return `{"pattern":"*"}`
		case 1:
			a, b := filterCodes[r.IntN(len(filterCodes))], filterCodes[r.IntN(len(filterCodes))]
			if b < a {
				a, b = b, a
			}
			return fmt.Sprintf(`{"from":%q,"to":%q}`, a, b)
		case 2:
			prefixes := []string{"A", "a", "A%", "A_", `A\`, "%", "_", `\`, "Ä", "B", "c%", "D_", "[", "?"}
			return fmt.Sprintf(`{"pattern":%q}`, prefixes[r.IntN(len(prefixes))]+"*")
		}
		return fmt.Sprintf(`{"value":%q}`, filterCodes[r.IntN(len(filterCodes))])
	}
	level := func() string {
		switch r.IntN(5) {
		case 0:
			return `{"pattern":"*"}`
		case 1:
			a, b := filterLevels[r.IntN(len(filterLevels))], filterLevels[r.IntN(len(filterLevels))]
			return fmt.Sprintf(`{"from":%d,"to":%d}`, min(a, b), max(a, b))
		case 2:
			return fmt.Sprintf(`{"pattern":"%d*"}`, 1+r.IntN(2))
		}
		return fmt.Sprintf(`{"value":%d}`, filterLevels[r.IntN(len(filterLevels))])
	}
	entries := func(f func() string) string {
		var out []string
		for range 1 + r.IntN(3) {
			out = append(out, f())
		}
		return "[" + strings.Join(out, ",") + "]"
	}
	var auths []string
	for range 1 + r.IntN(3) {
		act := `[{"value":"READ"}]`
		if r.IntN(4) == 0 {
			act = `[{"value":"UPDATE"}]`
		}
		auths = append(auths, fmt.Sprintf(`{"object":"/TRAUTH/DOC","fields":{"CODE":%s,"LEVEL":%s,"ACTIVITY":%s}}`, entries(code), entries(level), act))
	}
	return `{"name":"/LOCAL/DOCS","texts":{"en":{"label":"Documents"}},"authorizations":[` + strings.Join(auths, ",") + `]}`
}

func visible(t *testing.T, k *kernel.Kernel, opts kernel.ExecOptions) []int64 {
	t.Helper()
	out, err := call(k, opts, "/TRAUTH/DOC_ACCESS", "VISIBLE")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	var raw []json.Number
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		var strs []string
		if err := json.Unmarshal([]byte(out), &strs); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		for _, s := range strs {
			raw = append(raw, json.Number(s))
		}
	}
	for _, n := range raw {
		v, _ := n.Int64()
		ids = append(ids, v)
	}
	return ids
}

// TestFilterMatchesCheck compares authorized in a where condition with
// the check in memory for random roles and documents.
func TestFilterMatchesCheck(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k := openCheckKernel(t, db.DSN)
			r := rand.New(rand.NewPCG(7, 14))
			docs := seedDocs(t, k, r, 120)
			anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
			assigned, nonEmpty := false, 0
			for i := range 40 {
				role := randomRole(r)
				if _, err := k.Roles().Import(ctx, []byte(role), "docs.json", kernel.RoleOptions{By: "test"}); err != nil {
					t.Fatalf("%s: %v", role, err)
				}
				if !assigned {
					if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/LOCAL/DOCS", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
						t.Fatal(err)
					}
					assigned = true
				}
				e, err := k.EffectiveAuthorizations(ctx, "anna", "ACME", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				var want []int64
				for _, d := range docs {
					if d.code != nil && d.level != nil && e.Check(authorization.Check{Object: "/TRAUTH/DOC",
						Values: map[string]string{"CODE": *d.code, "LEVEL": strconv.Itoa(*d.level), "ACTIVITY": "READ"}}) {
						want = append(want, d.id)
					}
				}
				nonEmpty += min(len(want), 1)
				if got := visible(t, k, anna); !slices.Equal(got, want) {
					t.Fatalf("iteration %d, role %s:\ngot  %v\nwant %v", i, role, got, want)
				}
			}
			if nonEmpty < 20 {
				t.Errorf("only %d of 40 roles let documents through", nonEmpty)
			}
		})
	}
}

// TestFilters runs authorized in select with limit, select count, update
// and delete, for all authorizations, without user and with 5000 values.
func TestFilters(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k := openCheckKernel(t, db.DSN)
			const C = "/TRAUTH/DOC_ACCESS"
			for _, args := range [][]string{{"1", `"A"`, "1"}, {"2", `"B"`, "2"}, {"3", `"C"`, "3"}, {"4", "null", "4"}, {"5", `"A%"`, "5"}} {
				mustCall(t, k, "ACME", C, "ADD", args...)
			}
			role := func(codes []string) {
				t.Helper()
				var vs []string
				for _, c := range codes {
					vs = append(vs, fmt.Sprintf(`{"value":%q}`, c))
				}
				js := `{"name":"/LOCAL/DOCS","texts":{"en":{"label":"Documents"}},"authorizations":[
					{"object":"/TRAUTH/DOC","fields":{"CODE":[` + strings.Join(vs, ",") + `],"LEVEL":[{"pattern":"*"}],"ACTIVITY":[{"value":"READ"},{"value":"UPDATE"}]}},
					{"object":"/TRAUTH/DOC","fields":{"CODE":[{"value":"C"}],"LEVEL":[{"pattern":"*"}],"ACTIVITY":[{"value":"DELETE"}]}}]}`
				if _, err := k.Roles().Import(ctx, []byte(js), "docs.json", kernel.RoleOptions{By: "test"}); err != nil {
					t.Fatal(err)
				}
			}
			role([]string{"A", "A%"})
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/LOCAL/DOCS", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
			anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
			nobody := kernel.ExecOptions{Tenant: "ACME"}
			expect := func(opts kernel.ExecOptions, method, want string, args ...string) {
				t.Helper()
				got, err := call(k, opts, C, method, args...)
				if err != nil || got != want {
					t.Errorf("%s(%s) = %s, %v; want %s", method, strings.Join(args, ", "), got, err, want)
				}
			}
			if got := visible(t, k, anna); !slices.Equal(got, []int64{1, 5}) {
				t.Errorf("visible: %v", got)
			}
			expect(anna, "COUNT_AT", "1", "1")
			expect(anna, "COUNT_AT", "0", "2")
			if got, err := call(k, anna, C, "FIRST", "2"); err != nil || !strings.Contains(got, `"DOC_ID":1`) && !strings.Contains(got, `"DOC_ID":"1"`) ||
				!strings.Contains(got, `"CODE":null`) || strings.Contains(got, `"B"`) {
				t.Errorf("first: %s, %v", got, err)
			}
			expect(anna, "RAISE", "2", "9")
			expect(anna, "REMOVE", "0")
			if got := visible(t, k, nobody); len(got) != 0 {
				t.Errorf("without user: %v", got)
			}
			expect(nobody, "COUNT_AT", "0", "9")

			// The role with all authorizations needs no condition.
			if _, err := k.Users().Create(ctx, "root", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "root", Role: model.AllAuthorizationsRole, Tenant: "ACME"}, kernel.RoleOptions{By: "test", AllAuthorizations: true}); err != nil {
				t.Fatal(err)
			}
			if got := visible(t, k, kernel.ExecOptions{Tenant: "ACME", User: model.Some("root")}); !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
				t.Errorf("all authorizations: %v", got)
			}

			// 5000 values are one parameter.
			many := []string{"A"}
			for i := range 5000 {
				many = append(many, fmt.Sprintf("V%04d", i))
			}
			role(many)
			if got := visible(t, k, anna); !slices.Equal(got, []int64{1}) {
				t.Errorf("5000 values: %v", got)
			}
			expect(nobody, "REMOVE", "5")
		})
	}
}
