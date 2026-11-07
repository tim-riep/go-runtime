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

// Package testutil provides helpers shared by the tests.
package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/model"
)

// ModuleRoot returns the directory containing go.mod.
func ModuleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// Example loads a package from the examples directory, e.g. "TRFIN" or
// "upgrade/TROTTO".
func Example(t testing.TB, name string) *model.Package {
	t.Helper()
	dir := filepath.Join(ModuleRoot(t), "examples", name)
	pkg, err := pkgload.Load(os.DirFS(dir), dir)
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	return pkg
}

// KernelSchema returns the newest kernel schema.
func KernelSchema(t testing.TB) *model.Schema {
	t.Helper()
	s, err := kernelschema.Schema(kernelschema.Latest())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Fixed timestamp used for packages in tests.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// AddPackages adds packages to s and fails the test on name conflicts.
func AddPackages(t testing.TB, s *model.Schema, pkgs ...*model.Package) *model.Schema {
	t.Helper()
	for _, p := range pkgs {
		if err := s.AddPackage(p, Epoch, Epoch); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

var update = flag.Bool("update", false, "rewrite golden files")

// Golden compares got with testdata/golden/<name> in the module root and
// rewrites the file when the test runs with -update.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	path := filepath.Join(ModuleRoot(t), "testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run the test with -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("output differs from %s (run with -update to accept):\n%s", name, got)
	}
}

// CopyExample copies an example package into a temporary directory, so a
// test can change its files, and returns the directory.
func CopyExample(t testing.TB, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), filepath.Base(name))
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(ModuleRoot(t), "examples", name))); err != nil {
		t.Fatal(err)
	}
	return dir
}

// WriteFile writes a file below dir, creating its folders.
func WriteFile(t testing.TB, dir, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// LoadDir loads a package from a directory.
func LoadDir(t testing.TB, dir string) *model.Package {
	t.Helper()
	pkg, err := pkgload.Load(os.DirFS(dir), dir)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}
