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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/kernelsrc"
	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/kernelpkg"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/compile"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// Result is the outcome of a build.
type Result struct {
	Package     *model.Package
	Program     *types.Program // nil when the sources could not be checked
	Diagnostics []types.Diagnostic
	Schema      *model.Schema
	// Imports are the kernel and the packages the package requires, also
	// indirectly, in installation order.
	Imports []model.PackageID
	// Deps are the compiled programs of the required packages when the
	// build read them from directories.
	Deps     map[model.PackageID]*bytecode.Program
	includes includes
	natives  *native.Registry
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
	schema, err := Schema(pkg, deps)
	if err != nil {
		return Package(pkg, deps), nil
	}
	// The packages it requires are compiled first, in installation order,
	// so that the capabilities of kernel packages are known.
	programs := map[model.PackageID]*bytecode.Program{}
	for _, dep := range deps {
		if len(dep.Sources) == 0 {
			continue
		}
		dr := CheckPackageOpts(dep, schema, Options{Programs: programs})
		if !dr.OK() {
			return nil, fmt.Errorf("the required package %s does not compile: %v", dep.Manifest.ID, dr.Errors())
		}
		p, err := dr.Compile(false, programs)
		if err != nil {
			return nil, err
		}
		programs[dep.Manifest.ID] = p
	}
	r := CheckPackageOpts(pkg, schema, Options{Programs: programs})
	r.Deps = programs
	return r, nil
}

func dependencies(parent string, pkg *model.Package, seen map[model.PackageID]bool) ([]*model.Package, error) {
	var out []*model.Package
	for _, r := range pkg.Manifest.Requires {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		dir := filepath.Join(parent, string(r.ID))
		var dep *model.Package
		var err error
		switch _, statErr := os.Stat(filepath.Join(dir, pkgload.ManifestFile)); {
		case statErr == nil:
			dep, err = pkgload.Load(os.DirFS(dir), dir)
		case kernelpkg.Is(r.ID):
			// Kernel packages come from the kernel binary.
			dep, err = kernelpkg.Load(r.ID)
		default:
			return nil, fmt.Errorf("package %s requires %s, which is expected in %s", pkg.Manifest.ID, r.ID, dir)
		}
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

// includes finds includes of all packages.
type includes map[string]*types.Unit

func (m includes) Include(name string) (*types.Unit, bool) {
	u, ok := m[name]
	return u, ok
}

// Package checks the sources of a loaded package. deps are the packages it
// requires, in installation order.
func Package(pkg *model.Package, deps []*model.Package) *Result {
	return PackageWith(pkg, deps, native.Default)
}

// PackageWith is Package with the native methods of a registry.
func PackageWith(pkg *model.Package, deps []*model.Package, natives *native.Registry) *Result {
	r := &Result{Package: pkg, natives: natives}
	schema, err := Schema(pkg, deps)
	if err != nil {
		var list rterr.List
		if !errors.As(err, &list) {
			list = rterr.List{err}
		}
		for _, e := range list {
			r.fail("", 0, 0, "schema", e.Error())
		}
		return r
	}
	return CheckPackageWith(pkg, schema, natives)
}

// Schema returns the valid schema of the kernel with deps and pkg.
func Schema(pkg *model.Package, deps []*model.Package) (*model.Schema, error) {
	schema, err := kernelschema.Schema(kernelschema.Latest())
	if err != nil {
		return nil, err
	}
	for _, p := range append(slices.Clone(deps), pkg) {
		if err := schema.AddPackage(p, time.Time{}, time.Time{}); err != nil {
			return nil, err
		}
	}
	if err := validate.Schema(schema, validate.Options{}); err != nil {
		return nil, err
	}
	return schema, nil
}

// requiredPackages returns the packages pkg requires, also indirectly, in
// installation order.
func requiredPackages(s *model.Schema, id model.PackageID) []model.PackageID {
	var out []model.PackageID
	seen := map[model.PackageID]bool{id: true}
	var visit func(p model.PackageID)
	visit = func(p model.PackageID) {
		info := s.Packages[p]
		if info == nil {
			return
		}
		for _, r := range info.Manifest.Requires {
			if !seen[r.ID] {
				seen[r.ID] = true
				visit(r.ID)
				out = append(out, r.ID)
			}
		}
	}
	visit(id)
	return out
}

func parseUnit(pkg model.PackageID, file string, s model.Source) (*types.Unit, []syntax.Error) {
	f, errs := syntax.Parse(file, s.Text)
	if len(errs) > 0 {
		return nil, errs
	}
	return &types.Unit{Package: pkg, File: file, Name: s.Name, Kind: s.Kind, Text: s.Text, AST: f}, nil
}

// fileOf returns the name of a source file of another package in messages
// and line tables: "PKG:src/NAME.include".
func fileOf(pkg model.PackageID, file string) string { return string(pkg) + ":" + file }

var (
	kernelOnce sync.Once
	kernelProg *bytecode.Program
	kernelErr  error
	kernelLib  []*types.Unit
)

// KernelProgram returns the compiled classes of the kernel, which every
// image contains.
func KernelProgram() (*bytecode.Program, error) {
	kernelOnce.Do(func() {
		sources := map[string]string{}
		var units []*types.Unit
		for _, s := range kernelsrc.Sources() {
			u, errs := parseUnit(model.Kernel, s.File, s)
			if len(errs) > 0 {
				kernelErr = fmt.Errorf("kernel source %s: %v", s.File, errs)
				return
			}
			units = append(units, u)
			sources[s.File] = s.Text
		}
		kernelLib = units
		prog, diags := types.Check(types.Config{Package: model.Kernel, Units: units, Includes: includes{}})
		for _, d := range diags {
			if d.Severity == types.Error {
				kernelErr = fmt.Errorf("kernel source: %s", d)
				return
			}
		}
		kernelProg, kernelErr = compile.Compile(prog, compile.Options{Sources: sources, LanguageVersion: syntax.LanguageVersion})
		if kernelProg != nil {
			kernelProg.Package = string(model.Kernel)
		}
	})
	return kernelProg, kernelErr
}

// CheckPackage checks the sources of a package against a valid schema that
// contains the package and the packages it requires, also indirectly. The
// classes of required packages are taken from their sources in the
// schema, includes from all packages.
func CheckPackage(pkg *model.Package, schema *model.Schema) *Result {
	return CheckPackageWith(pkg, schema, native.Default)
}

// CheckPackageWith is CheckPackage with the native methods of a registry.
func CheckPackageWith(pkg *model.Package, schema *model.Schema, natives *native.Registry) *Result {
	return CheckPackageOpts(pkg, schema, Options{Natives: natives})
}

// Options control the check of a package.
type Options struct {
	// Natives are the native methods of the kernel; nil means
	// native.Default.
	Natives *native.Registry
	// Programs are compiled programs of required packages; the
	// capabilities of the methods of kernel packages come from them.
	Programs map[model.PackageID]*bytecode.Program
}

// CheckPackageOpts is CheckPackage with options.
func CheckPackageOpts(pkg *model.Package, schema *model.Schema, opts Options) *Result {
	natives := opts.Natives
	if natives == nil {
		natives = native.Default
	}
	r := &Result{Package: pkg, Schema: schema, natives: natives}
	id := pkg.Manifest.ID
	if _, err := KernelProgram(); err != nil {
		r.fail("", 0, 0, "kernel", err.Error())
		return r
	}
	var units []*types.Unit
	incs := includes{}
	own := map[string]bool{}
	for _, s := range pkg.Sources {
		u, errs := parseUnit(id, s.File, s)
		if len(errs) > 0 {
			for _, e := range errs {
				r.fail(s.File, e.Pos.Line, e.Pos.Col, types.CodeSyntax, e.Msg)
			}
			continue
		}
		if canon := syntax.Print(u.AST); canon != s.Text {
			line := firstDifference(s.Text, canon)
			r.fail(s.File, line, 1, types.CodeCanonical, "the file is not in canonical form from this line on; run rt fmt")
			continue
		}
		if s.Kind == model.SourceInclude {
			incs[s.Name] = u
			own[s.Name] = true
			continue
		}
		units = append(units, u)
	}
	if len(r.Diagnostics) > 0 {
		return r
	}
	libraries := slices.Clone(kernelLib)
	r.Imports = []model.PackageID{model.Kernel}
	for _, dep := range requiredPackages(schema, id) {
		r.Imports = append(r.Imports, dep)
		for _, s := range schema.Sources[dep] {
			if s.Kind == model.SourceInclude {
				continue
			}
			if u, errs := parseUnit(dep, fileOf(dep, s.File), s); len(errs) == 0 {
				libraries = append(libraries, u)
			}
		}
	}
	for other, sources := range schema.Sources {
		if other == id {
			continue
		}
		for _, s := range sources {
			if s.Kind != model.SourceInclude {
				continue
			}
			if u, errs := parseUnit(other, fileOf(other, s.File), s); len(errs) == 0 {
				incs[s.Name] = u
			}
		}
	}
	var visible []model.PackageID
	kernelPackage, caps := pkg.Manifest.KernelPackage, pkg.Manifest.Capabilities
	if info := schema.Packages[id]; info != nil {
		kernelPackage, caps = info.Manifest.KernelPackage, info.Manifest.Capabilities
		for _, req := range info.Manifest.Requires {
			visible = append(visible, req.ID)
		}
	} else {
		for _, req := range pkg.Manifest.Requires {
			visible = append(visible, req.ID)
		}
	}
	prog, diags := types.Check(types.Config{Package: id, Schema: schema, Units: units, Libraries: libraries, Visible: visible, Includes: incs,
		KernelPackage: kernelPackage, Natives: natives, Capabilities: caps, LibraryCapabilities: libraryCapabilities(schema, opts.Programs)})
	r.Program = prog
	r.Diagnostics = append(r.Diagnostics, diags...)
	for name, u := range incs {
		if own[name] && !prog.Includes[name] {
			r.Diagnostics = append(r.Diagnostics, types.Diagnostic{Severity: types.Note, Level: "note", File: u.File, Code: types.CodeInclude,
				Message: fmt.Sprintf("include %s is not included anywhere; it is only parsed and formatted", name)})
		}
	}
	r.includes = incs
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

// Compile compiles a successful build against the compiled programs of the
// packages it requires; a required package without code has none. Test
// classes are included only for tests.
func (r *Result) Compile(tests bool, programs map[model.PackageID]*bytecode.Program) (*bytecode.Program, error) {
	if !r.OK() {
		return nil, fmt.Errorf("the build of %s has errors", r.Package.Manifest.ID)
	}
	kernel, err := KernelProgram()
	if err != nil {
		return nil, err
	}
	imports := []*bytecode.Program{kernel}
	for _, id := range r.Imports[1:] {
		if p := programs[id]; p != nil {
			imports = append(imports, p)
		}
	}
	sources := map[string]string{}
	for _, s := range r.Package.Sources {
		sources[s.File] = s.Text
	}
	for _, u := range r.includes {
		sources[u.File] = u.Text
	}
	return compile.Compile(r.Program, compile.Options{Tests: tests, Sources: sources, LanguageVersion: r.Package.Manifest.LanguageVersion, Imports: imports})
}

// Image compiles a successful build and links it with the kernel and the
// packages it requires, for running it without a database.
func (r *Result) Image(tests bool) (*vm.Image, error) {
	p, err := r.Compile(tests, r.Deps)
	if err != nil {
		return nil, err
	}
	kernel, _ := KernelProgram()
	progs := []*bytecode.Program{kernel}
	for _, id := range r.Imports[1:] {
		if dp := r.Deps[id]; dp != nil {
			progs = append(progs, dp)
		}
	}
	return vm.LinkWith(0, r.natives, append(progs, p)...)
}

// libraryCapabilities returns the capabilities of the methods of the
// compiled kernel packages.
func libraryCapabilities(schema *model.Schema, programs map[model.PackageID]*bytecode.Program) func(class, method string) []string {
	return func(class, method string) []string {
		id, _, _ := model.SplitName(class)
		if info := schema.Packages[id]; info == nil || !info.Manifest.KernelPackage {
			return nil
		}
		p := programs[id]
		if p == nil {
			return nil
		}
		m, _ := p.Method(class, method)
		return m.Capabilities
	}
}
