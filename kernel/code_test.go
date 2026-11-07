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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// orderPackage returns the example TRORDER with changed files.
func orderPackage(t *testing.T, files map[string]string) string {
	dir := testutil.CopyExample(t, "TRORDER")
	for p, s := range files {
		testutil.WriteFile(t, dir, p, []byte(s))
	}
	return dir
}

func generation(t *testing.T, k *kernel.Kernel) int64 {
	t.Helper()
	var n int64
	if err := k.DB().QueryRow(`SELECT COALESCE(MAX("GENERATION"), 0) FROM "/RT/CODE_GENERATION"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCompileOnInstall(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		pkg := testutil.LoadDir(t, orderPackage(t, nil))

		// A dry run compiles and changes nothing.
		dry, err := k.Install(ctx, nil, kernel.Options{DryRun: true})
		if err == nil {
			t.Fatal("no packages accepted")
		}
		dry, err = k.Plan(ctx, pkg)
		if err != nil || change(dry, plan.CompilePackage, "TRORDER") == nil || count(t, k, "/RT/BYTECODE") != 0 {
			t.Fatalf("dry run: %v %v", err, dry)
		}

		if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if count(t, k, "/RT/BYTECODE") != 1 || count(t, k, "/RT/SOURCES") != len(pkg.Sources) || generation(t, k) != 1 {
			t.Fatalf("stored: %d bytecode, %d sources, generation %d", count(t, k, "/RT/BYTECODE"), count(t, k, "/RT/SOURCES"), generation(t, k))
		}
		s, _ := k.Schema(ctx)
		if len(s.Sources["TRORDER"]) != len(pkg.Sources) || s.Packages["TRORDER"].Manifest.LanguageVersion != 1 {
			t.Fatalf("sources read back: %d", len(s.Sources["TRORDER"]))
		}
		consistent(t, k)

		// Applying the package again compiles it again.
		if _, err := k.Upgrade(ctx, pkg, kernel.Options{}); err != nil || generation(t, k) != 2 {
			t.Fatalf("upgrade: %v, generation %d", err, generation(t, k))
		}

		// An error in the code rejects the upgrade; nothing changes.
		broken := testutil.LoadDir(t, orderPackage(t, map[string]string{"src/BROKEN.class": "public class /TRORDER/BROKEN\n\n  public static method M() returns INT4\n    return \"x\"\n  end\n\nend\n"}))
		broken.Manifest.Version = "1.1.0"
		_, err = k.Upgrade(ctx, broken, kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || ce.Package != "TRORDER" || !strings.Contains(err.Error(), "src/BROKEN.class:4:12: type:") {
			t.Fatalf("compile error: %v", err)
		}
		if _, err := k.Upgrade(ctx, broken, kernel.Options{DryRun: true}); !errors.As(err, &ce) {
			t.Fatalf("dry run with error: %v", err)
		}
		s, _ = k.Schema(ctx)
		if s.Packages["TRORDER"].Manifest.Version != "1.0.0" || generation(t, k) != 2 {
			t.Fatal("the failed upgrade changed the repository")
		}

		// Files that are not in canonical form are rejected as well.
		messy := testutil.LoadDir(t, orderPackage(t, map[string]string{"src/MESSY.class": "public class /TRORDER/MESSY\nend\n\n"}))
		messy.Manifest.Version = "1.2.0"
		if _, err := k.Upgrade(ctx, messy, kernel.Options{}); !errors.As(err, &ce) || !strings.Contains(err.Error(), "canonical: the file is not in canonical form") {
			t.Fatalf("canonical form: %v", err)
		}

		if _, err := k.Uninstall(ctx, "TRORDER", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if count(t, k, "/RT/BYTECODE") != 0 || count(t, k, "/RT/SOURCES") != 0 {
			t.Fatal("code left after uninstall")
		}
		consistent(t, k)
	})
}
