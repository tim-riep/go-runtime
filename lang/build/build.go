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

// Package build turns the source files of a package into a checked
// program: it reads the package with its schema, requires the canonical
// form, inserts includes, adds the kernel classes and runs the type
// checker. It needs no database.
package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/kernelsrc"
	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Result is the outcome of a build.
type Result struct {
	Package     *model.Package
	Program     *types.Program // nil when the sources could not be checked
	Diagnostics []types.Diagnostic
}

// Errors returns the diagnostics with severity Error.
func (r *Result) Errors() []types.Diagnostic {
	var out []types.Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == types.Error {
			out = append(out, d)
		}
	}
	return out
}

// OK reports a build without errors.
func (r *Result) OK() bool { return len(r.Errors()) == 0 && r.Program != nil }

// Dir reads the package in dir and checks its sources. Packages it
// requires are looked up next to it, in a directory named after their ID;
// their schema is used to validate the schema of the package.
func Dir(dir string) (*Result, error) {
	pkg, err := pkgload.Load(os.DirFS(dir), dir)
	if err != nil {
		return nil, err
	}
	deps, err := dependencies(filepath.Dir(dir), pkg, map[model.PackageID]bool{pkg.Manifest.ID: true})
	if err != nil {
		return nil, err
	}
	return Package(pkg, deps), nil
}

func dependencies(parent string, pkg *model.Package, seen map[model.PackageID]bool) ([]*model.Package, error) {
	var out []*model.Package
	for _, r := range pkg.Manifest.Requires {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		dir := filepath.Join(parent, string(r.ID))
		if _, err := os.Stat(filepath.Join(dir, pkgload.ManifestFile)); err != nil {
			return nil, fmt.Errorf("package %s requires %s, which is expected in %s", pkg.Manifest.ID, r.ID, dir)
		}
		dep, err := pkgload.Load(os.DirFS(dir), dir)
		if err != nil {
			return nil, err
		}
		more, err := dependencies(parent, dep, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
		out = append(out, dep)
	}
	return out, nil
}

// includes finds includes of the package.
type includes map[string]*types.Unit

func (m includes) Include(name string) (*types.Unit, bool) {
	u, ok := m[name]
	return u, ok
}

// Package checks the sources of a loaded package. deps are the packages it
// requires, in installation order.
func Package(pkg *model.Package, deps []*model.Package) *Result {
	r := &Result{Package: pkg}
	schema, err := kernelschema.Schema(kernelschema.Latest())
	if err != nil {
		r.fail("", 0, 0, "schema", err.Error())
		return r
	}
	for _, p := range append(deps, pkg) {
		if err := schema.AddPackage(p, time.Time{}, time.Time{}); err != nil {
			r.fail("", 0, 0, "schema", err.Error())
			return r
		}
	}
	if err := validate.Schema(schema, validate.Options{}); err != nil {
		var list rterr.List
		if !errors.As(err, &list) {
			list = rterr.List{err}
		}
		for _, e := range list {
			r.fail("", 0, 0, "schema", e.Error())
		}
		return r
	}
	var units []*types.Unit
	incs := includes{}
	sources := append(kernelsrc.Sources(), pkg.Sources...)
	for _, s := range sources {
		f, errs := syntax.Parse(s.File, s.Text)
		if len(errs) > 0 {
			for _, e := range errs {
				r.fail(s.File, e.Pos.Line, e.Pos.Col, types.CodeSyntax, e.Msg)
			}
			continue
		}
		if canon := syntax.Print(f); canon != s.Text {
			line := firstDifference(s.Text, canon)
			r.fail(s.File, line, 1, types.CodeCanonical, "the file is not in canonical form from this line on; run rt fmt")
			continue
		}
		u := &types.Unit{File: s.File, Name: s.Name, Kind: s.Kind, Text: s.Text, AST: f}
		if s.Kind == model.SourceInclude {
			incs[s.Name] = u
			continue
		}
		units = append(units, u)
	}
	if len(r.Diagnostics) > 0 {
		return r
	}
	prog, diags := types.Check(types.Config{Package: pkg.Manifest.ID, Schema: schema, Units: units, Includes: incs})
	r.Program = prog
	r.Diagnostics = append(r.Diagnostics, diags...)
	for name, u := range incs {
		if !prog.Includes[name] {
			r.Diagnostics = append(r.Diagnostics, types.Diagnostic{Severity: types.Note, Level: "note", File: u.File, Code: types.CodeInclude,
				Message: fmt.Sprintf("include %s is not included anywhere; it is only parsed and formatted", name)})
		}
	}
	return r
}

func (r *Result) fail(file string, line, col int, code, msg string) {
	r.Diagnostics = append(r.Diagnostics, types.Diagnostic{Severity: types.Error, Level: "error", File: file, Line: line, Column: col, Code: code, Message: msg})
}

// firstDifference returns the first line in which two texts differ.
func firstDifference(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range min(len(al), len(bl)) {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	return min(len(al), len(bl))
}
