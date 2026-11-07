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
	"testing"

	"github.com/tim-riep/go-runtime/internal/diff"
	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// forEachDB runs fn against a fresh database of every available dialect.
func forEachDB(t *testing.T, fn func(t *testing.T, k *kernel.Kernel)) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			k, err := kernel.Open(context.Background(), db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { k.Close() })
			fn(t, k)
		})
	}
}

// initialized initializes the kernel and sets up German, the second
// language of the examples. Kernel versions without languages skip that.
func initialized(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ctx := context.Background()
	if _, err := k.Init(ctx, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Languages().List(ctx); err == nil {
		if _, err := k.Languages().Add(ctx, "de", ""); err != nil {
			t.Fatal(err)
		}
	}
}

func examples(t *testing.T) []*model.Package {
	return []*model.Package{testutil.Example(t, "TROTTO"), testutil.Example(t, "TRFIN")}
}

// legacyExample loads an example package without structures, array types
// and includes, for kernel versions before 5: the address included into
// /TRFIN/COMPANIES becomes its former COUNTRY field.
func legacyExample(t *testing.T, name string) *model.Package {
	p := testutil.Example(t, name)
	p.Structures, p.ArrayTypes, p.StructureExtensions, p.AuthorizationObjects, p.Roles = nil, nil, nil, nil, nil
	var tables []model.Table
	for _, tb := range p.Tables {
		if tb.Name == "/TRFIN/COMPANIES_ARCHIVE" {
			continue
		}
		for _, inc := range tb.Includes {
			country := model.Field{Name: "COUNTRY", LocalName: "COUNTRY", Owner: p.Manifest.ID, Origin: model.OriginTable,
				DataElement: model.Some("/TRBASE/COUNTRY"), NotNull: true}
			if inc.Name == "/TRBASE/ADDRESS" {
				tb.Fields = slices.Insert(slices.Clone(tb.Fields), inc.After, country)
			}
		}
		tb.Includes = nil
		tables = append(tables, tb)
	}
	p.Tables = tables
	return p
}

func legacyExamples(t *testing.T) []*model.Package {
	return []*model.Package{legacyExample(t, "TROTTO"), legacyExample(t, "TRBASE"), legacyExample(t, "TRFIN")}
}

// sameSchema fails if the repository differs from want.
// kernelPackages is the number of kernel packages, which the kernel
// installs on initialization.
const kernelPackages = 1

// baseSchema is the kernel schema with the kernel packages as the kernel
// installs them, with the texts of the languages set up in k.
func baseSchema(t *testing.T, k *kernel.Kernel) *model.Schema {
	t.Helper()
	langs, err := k.Languages().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, l := range langs {
		set[l.Code] = true
	}
	keep := func(l string) bool { return set[l] }
	kernelOnly, err := kernelschema.Replace(model.NewSchema(), kernelschema.Latest(), keep)
	if err != nil {
		t.Fatal(err)
	}
	return testutil.AddPackages(t, kernelOnly, testutil.Example(t, "TRBASE").WithLanguages(keep))
}

// upgrade upgrades a package; TRBASE, a kernel package, goes through the
// test hook for kernel packages.
func upgrade(k *kernel.Kernel, ctx context.Context, pkg *model.Package, opts kernel.Options) (*plan.Plan, error) {
	if pkg.Manifest.ID == "TRBASE" {
		return k.UpgradeKernelPackage(ctx, pkg, opts)
	}
	return k.Upgrade(ctx, pkg, opts)
}

func sameSchema(t *testing.T, k *kernel.Kernel, want *model.Schema) {
	t.Helper()
	got, err := k.Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := validate.Schema(want, validate.Options{}); err != nil {
		t.Fatal(err)
	}
	if ch := diff.Diff(got, want); len(ch) > 0 {
		t.Fatalf("repository differs from expected schema: %v", ch)
	}
	if len(got.Packages) != len(want.Packages) {
		t.Fatalf("packages: got %d, want %d", len(got.Packages), len(want.Packages))
	}
}

func TestInit(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); !errors.Is(err, rterr.ErrNotInitialized) {
			t.Fatalf("expected ErrNotInitialized, got %v", err)
		}
		dry, err := k.Init(ctx, kernel.Options{DryRun: true})
		if err != nil || dry.Executed || len(dry.Steps) == 0 {
			t.Fatalf("dry run: %v %+v", err, dry)
		}
		p, err := k.Init(ctx, kernel.Options{})
		if err != nil || !p.Executed {
			t.Fatalf("init: %v", err)
		}
		again, err := k.Init(ctx, kernel.Options{})
		if err != nil || again.Executed || len(again.Changes) != 0 {
			t.Fatalf("second init must be a no-op: %v %+v", err, again)
		}
		sameSchema(t, k, baseSchema(t, k))
	})
}

func TestInstall(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		dry, err := k.Install(ctx, examples(t), kernel.Options{DryRun: true})
		if err != nil || dry.Executed {
			t.Fatalf("dry run: %v", err)
		}
		if ids, _ := k.Schema(ctx); len(ids.Packages) != kernelPackages {
			t.Fatal("dry run installed packages")
		}
		p, err := k.Install(ctx, examples(t), kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Summary(); got != "install TRFIN 1.0.0, TROTTO 1.0.0 ("+k.Dialect().Name()+")" {
			t.Errorf("summary: %s", got)
		}
		if p.LogID == "" {
			t.Error("no log entry")
		}
		sameSchema(t, k, testutil.AddPackages(t, baseSchema(t, k), testutil.Example(t, "TRFIN"), testutil.Example(t, "TROTTO")))

		var already *rterr.AlreadyInstalled
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); !errors.As(err, &already) {
			t.Fatalf("expected AlreadyInstalled, got %v", err)
		}
	})
}

func TestInstallMissingDependency(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		initialized(t, k)
		_, err := k.Install(context.Background(), []*model.Package{testutil.Example(t, "TROTTO")}, kernel.Options{})
		var dep *rterr.DependencyError
		if !errors.As(err, &dep) || dep.Kind != rterr.DependencyMissing || dep.Required != "TRFIN" {
			t.Fatalf("got %v", err)
		}
	})
}

func TestFailedInstallRollsBack(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		// A table that already exists outside the repository makes CREATE
		// TABLE fail after other statements have run.
		if _, err := k.DB().ExecContext(ctx, `CREATE TABLE "/TRFIN/COUNTRIES" ("X" INTEGER)`); err != nil {
			t.Fatal(err)
		}
		_, err := k.Install(ctx, examples(t), kernel.Options{})
		var ee *rterr.ExecError
		if !errors.As(err, &ee) {
			t.Fatalf("expected ExecError, got %v", err)
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Packages) != kernelPackages {
			t.Fatal("packages recorded after failed install")
		}
		for _, name := range []string{"/TRFIN/BANK_ACCOUNTS", "/TRFIN/COMPANIES"} {
			var n int
			if err := k.DB().QueryRowContext(ctx, `SELECT count(*) FROM "`+name+`"`).Scan(&n); err == nil {
				t.Errorf("table %s survived the rollback", name)
			}
		}
		log, err := k.Log(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) < 2 || log[0].Status != "FAILED" {
			t.Fatalf("failure not logged: %+v", log)
		}
	})
}
