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
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

func compileReason(p *plan.Plan, pkg string) string {
	if c := change(p, plan.CompilePackage, pkg); c != nil {
		return c.Detail
	}
	return ""
}

func TestRecompilation(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{xpkg(t, "TRA")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		// TRB extends /TRA/ITEMS, whose layout changes in the code of TRA.
		p, err := k.Install(ctx, []*model.Package{xpkg(t, "TRB")}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r := compileReason(p, "TRA"); r != "uses /TRA/ITEMS" || compileReason(p, "TRB") != "installed" {
			t.Fatalf("plan:\n%s", p.Text())
		}
		if got := runJSON(t, k, "/TRB/SUB", "RUN"); !strings.HasSuffix(got, `"count 1"]`) {
			t.Fatalf("without TRC: %s", got)
		}

		// Installing TRC inserts its include into TRB, removing it takes it out.
		p, err = k.Install(ctx, []*model.Package{xpkg(t, "TRC")}, kernel.Options{})
		if err != nil || compileReason(p, "TRB") != "includes /TRC/HOOK if found" {
			t.Fatalf("install TRC: %v\n%v", err, p)
		}
		if got := runJSON(t, k, "/TRB/SUB", "RUN"); !strings.HasSuffix(got, `"count 101"]`) {
			t.Fatalf("with TRC: %s", got)
		}
		p, err = k.Uninstall(ctx, "TRC", kernel.Options{Confirm: true})
		if err != nil || compileReason(p, "TRB") != "includes /TRC/HOOK if found" {
			t.Fatalf("uninstall TRC: %v\n%v", err, p)
		}
		if got := runJSON(t, k, "/TRB/SUB", "RUN"); !strings.HasSuffix(got, `"count 1"]`) {
			t.Fatalf("after removing TRC: %s", got)
		}

		// An upgrade of TRA without the field NAME breaks the code of TRB.
		dir := testutil.CopyDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "xpkg", "TRA"))
		testutil.WriteFile(t, dir, "schema/tables/ITEMS.json", []byte(`{"name":"/TRA/ITEMS","tenantSpecific":true,"dataClass":"application",
			"fields":[{"name":"ITEM_ID","domain":"/TRA/CODE"}],"primaryKey":["ITEM_ID"]}`))
		base := testutil.LoadDir(t, xdir(t, "TRA")).Sources
		for i, s := range base {
			if s.File == "src/BASE.class" {
				base[i].Text = strings.Replace(s.Text, "    return item.NAME default \"\"\n", "    return item.ITEM_ID\n", 1)
				testutil.WriteFile(t, dir, s.File, []byte(base[i].Text))
			}
		}
		up := testutil.LoadDir(t, dir)
		up.Manifest.Version = "1.1.0"
		_, err = k.Upgrade(ctx, up, kernel.Options{AllowDestructive: true})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || ce.Package != "TRB" || ce.Reason != "requires TRA" || !strings.Contains(err.Error(), "TRB") ||
			!strings.Contains(err.Error(), "has no component NAME") {
			t.Fatalf("upgrade: %v", err)
		}
		s, _ := k.Schema(ctx)
		if s.Packages["TRA"].Manifest.Version != "1.0.0" {
			t.Fatal("the rejected upgrade changed the repository")
		}

		// Code uses of the table and its fields.
		uses, err := k.CodeUses(ctx, "/TRA/ITEMS")
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, u := range uses {
			found[string(u.Package)+" "+u.Kind+" "+u.Member.Or("")] = true
		}
		for _, want := range []string{"TRA table ", "TRA field NAME", "TRB table ", "TRB field NAME", "TRB field /TRB/ACTIVE", "TRB field ITEM_ID"} {
			if !found[want] {
				t.Errorf("missing use %q in %v", want, found)
			}
		}
		d, err := k.Class(ctx, "/TRA/BASE")
		if err != nil || d.Package != "TRA" || len(d.CodeUses) == 0 {
			t.Fatalf("class: %v %+v", err, d)
		}
		consistent(t, k)
	})
}

func xdir(t *testing.T, name string) string {
	return filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "xpkg", name)
}
