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
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

func xpkg(t *testing.T, name string) *model.Package {
	return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "xpkg", name))
}

func runJSON(t *testing.T, k *kernel.Kernel, class, method string) string {
	t.Helper()
	image, err := k.Image(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m, err := vm.New(image, vm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.CallJSON(context.Background(), class, method, nil)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestCodeAcrossPackages compiles code that uses classes, interfaces,
// exceptions, tables, extension fields and includes of a required package
// against the code stored in the repository.
func TestCodeAcrossPackages(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		for _, name := range []string{"TRA", "TRC", "TRB"} {
			if _, err := k.Install(ctx, []*model.Package{xpkg(t, name)}, kernel.Options{}); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		// TRC is installed, so include /TRC/HOOK if found inserts its code.
		got := runJSON(t, k, "/TRB/SUB", "RUN")
		if got != `["sub","TRB and TRA base","42","first true","negative -1","runtime error from TRA","count 101"]` {
			t.Fatalf("result: %s", got)
		}
		consistent(t, k)

		// Classes and members of other packages are only visible when public.
		for src, want := range map[string]string{
			"public class /TRB/USE\n\n  public static method M()\n    var x = new /TRA/INTERNAL()\n  end\n\nend\n":             "/TRA/INTERNAL is not public",
			"public class /TRB/USE\n\n  public static method M()\n    var b = new /TRA/BASE()\n    b.HIDDEN()\n  end\n\nend\n": "HIDDEN is only visible in the package of /TRA/BASE",
		} {
			dir := testutil.CopyDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "xpkg", "TRB"))
			testutil.WriteFile(t, dir, "src/USE.class", []byte(src))
			pkg := testutil.LoadDir(t, dir)
			pkg.Manifest.Version = "1.1.0"
			_, err := k.Upgrade(ctx, pkg, kernel.Options{})
			var ce *rterr.CompileError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), want) {
				t.Errorf("expected %q, got %v", want, err)
			}
		}
	})
}
