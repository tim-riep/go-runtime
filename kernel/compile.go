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
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/types"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// compiled is the code an operation compiled.
type compiled struct {
	packages []model.PackageID // compiled packages, also those that have no code any more
	codes    []*repo.Code
}

// changedPackages returns the packages an operation installs, upgrades or
// removes.
func changedPackages(cur, want *model.Schema) []model.PackageID {
	var out []model.PackageID
	for id, w := range want.Packages {
		c, ok := cur.Packages[id]
		if !ok || !c.UpdatedAt.Equal(w.UpdatedAt) || c.Manifest.Version != w.Manifest.Version {
			out = append(out, id)
		}
	}
	for id := range cur.Packages {
		if _, ok := want.Packages[id]; !ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// installOrder sorts packages so that every package follows those it
// requires.
func installOrder(s *model.Schema, ids []model.PackageID) []model.PackageID {
	set := map[model.PackageID]bool{}
	for _, id := range ids {
		set[id] = true
	}
	var out []model.PackageID
	done := map[model.PackageID]bool{}
	var visit func(id model.PackageID)
	visit = func(id model.PackageID) {
		if done[id] {
			return
		}
		done[id] = true
		if p := s.Packages[id]; p != nil {
			for _, r := range p.Manifest.Requires {
				visit(r.ID)
			}
		}
		if set[id] {
			out = append(out, id)
		}
	}
	for _, id := range s.PackageIDs() {
		visit(id)
	}
	return out
}

// compile compiles the code of the packages an operation affects against
// the target schema. Any error rejects the whole operation.
func (k *Kernel) compile(ctx context.Context, q dialect.Querier, cur, want *model.Schema, now time.Time) (*compiled, []plan.Change, error) {
	if !k.repo.HasCode() {
		return &compiled{}, nil, nil
	}
	codes, err := k.repo.LoadCode(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	return k.compileSet(ctx, q, want, recompileSet(cur, want, codes), now)
}

// recompileSet returns the packages whose code may change through an
// operation, with the reason for each: the packages it installs or
// upgrades, the packages that require any changed package, the packages
// that include an include of a changed package with if found, and the
// packages that use an object of a changed package or a table or
// structure whose components change.
func recompileSet(cur, want *model.Schema, codes map[model.PackageID]*repo.Code) map[model.PackageID]string {
	changed := changedPackages(cur, want)
	isChanged := map[model.PackageID]bool{}
	for _, id := range changed {
		isChanged[id] = true
	}
	reasons := map[model.PackageID]string{}
	add := func(id model.PackageID, reason string) {
		if _, ok := want.Packages[id]; !ok {
			return
		}
		if _, done := reasons[id]; !done {
			reasons[id] = reason
		}
	}
	for _, id := range changed {
		if _, was := cur.Packages[id]; was {
			add(id, "upgraded")
		} else {
			add(id, "installed")
		}
	}
	for _, id := range want.PackageIDs() {
		for _, dep := range requiredBy(want, id) {
			if isChanged[dep] {
				add(id, "requires "+string(dep))
				break
			}
		}
	}
	objects := changedObjects(cur, want)
	for _, id := range sortedIDs(codes) {
		c := codes[id]
		for _, inc := range c.Includes {
			if isChanged[inc.Package] && inc.IfFound {
				add(id, "includes "+inc.Name+" if found")
			}
		}
		for _, u := range c.Uses {
			p, _ := u.ObjectPackage.Get()
			switch {
			case isChanged[p] && p != id:
				add(id, "uses "+u.Object)
			case objects[u.Object]:
				add(id, "uses "+u.Object)
			}
		}
	}
	return reasons
}

func sortedIDs(m map[model.PackageID]*repo.Code) []model.PackageID {
	out := make([]model.PackageID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// requiredBy returns the packages a package requires, also indirectly.
func requiredBy(s *model.Schema, id model.PackageID) []model.PackageID {
	var out []model.PackageID
	seen := map[model.PackageID]bool{}
	var visit func(p model.PackageID)
	visit = func(p model.PackageID) {
		info := s.Packages[p]
		if info == nil {
			return
		}
		for _, r := range info.Manifest.Requires {
			if !seen[r.ID] {
				seen[r.ID] = true
				out = append(out, r.ID)
				visit(r.ID)
			}
		}
	}
	visit(id)
	return out
}

// changedObjects returns the tables and structures whose resolved
// components differ, e.g. because an extension adds fields, which changes
// their memory layout in code.
func changedObjects(cur, want *model.Schema) map[string]bool {
	out := map[string]bool{}
	sig := func(s *model.Schema) map[string]string {
		m := map[string]string{}
		r := s.Resolver()
		for _, name := range append(s.TableNames(), s.StructureNames()...) {
			comps, _ := r.Components(name)
			var b strings.Builder
			for _, c := range comps {
				fmt.Fprintf(&b, "%s %s %s %v %v|", c.Name, c.Type.Kind, c.Type.Name, c.NotNull, c.Default)
			}
			if t := s.Tables[name]; t != nil {
				fmt.Fprintf(&b, "key %v", t.PrimaryKey)
			}
			m[name] = b.String()
		}
		return m
	}
	a, b := sig(cur), sig(want)
	for name, v := range a {
		if b[name] != v {
			out[name] = true
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			out[name] = true
		}
	}
	// Domains and data elements change the types of components as well.
	for name, d := range want.Domains {
		if c, ok := cur.Domains[name]; !ok || c.TypeString() != d.TypeString() || !slices.Equal(c.Values, d.Values) {
			out[name] = true
		}
	}
	for name, d := range want.DataElements {
		if c, ok := cur.DataElements[name]; !ok || c.Domain != d.Domain {
			out[name] = true
		}
	}
	return out
}

// compileSet compiles the given packages, with the reason for each, in
// installation order against the target schema.
func (k *Kernel) compileSet(ctx context.Context, q dialect.Querier, want *model.Schema, reasons map[model.PackageID]string, now time.Time) (*compiled, []plan.Change, error) {
	var ids []model.PackageID
	for id := range reasons {
		ids = append(ids, id)
	}
	programs, err := k.storedPrograms(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	out := &compiled{}
	var changes []plan.Change
	for _, id := range installOrder(want, ids) {
		delete(programs, id)
		out.packages = append(out.packages, id)
		sources := want.Sources[id]
		if len(sources) == 0 {
			continue
		}
		info := want.Packages[id]
		pkg := &model.Package{Manifest: info.Manifest, Sources: sources}
		r := build.CheckPackageOpts(pkg, want, build.Options{Natives: k.natives, Programs: programs})
		if !r.OK() {
			return nil, nil, compileError(id, reasons[id], r.Diagnostics)
		}
		prog, err := r.Compile(false, programs)
		if err != nil {
			return nil, nil, err
		}
		programs[id] = prog
		uses, includes := codeReferences(r)
		b := bytecode.Encode(prog)
		sum := sha256.Sum256(b)
		out.codes = append(out.codes, &repo.Code{Package: id, FormatVersion: bytecode.FormatVersion, LanguageVersion: info.Manifest.LanguageVersion,
			Bytes: b, SHA256: hex.EncodeToString(sum[:]), CompiledAt: now, Uses: uses, Includes: includes})
		changes = append(changes, plan.Change{Kind: plan.CompilePackage, Name: string(id), Detail: reasons[id]})
	}
	return out, changes, nil
}

func compileError(id model.PackageID, reason string, diags []types.Diagnostic) error {
	e := &rterr.CompileError{Package: string(id), Reason: reason}
	for _, d := range diags {
		if d.Severity != types.Error {
			continue
		}
		cd := rterr.CompileDiagnostic{File: d.File, Line: d.Line, Column: d.Column, Code: d.Code, Message: d.Message}
		for _, c := range d.Chain {
			cd.IncludedFrom = append(cd.IncludedFrom, c.Include+" included from "+c.File+":"+itoa(c.Line))
		}
		e.Diagnostics = append(e.Diagnostics, cd)
	}
	return e
}

func itoa(n int) string {
	return string(appendInt(nil, n))
}

func appendInt(b []byte, n int) []byte {
	if n < 0 {
		b = append(b, '-')
		n = -n
	}
	if n >= 10 {
		b = appendInt(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

// saveCode stores the compiled code and starts a new generation.
func (k *Kernel) saveCode(ctx context.Context, q dialect.Querier, c *compiled, now time.Time) error {
	if c == nil || len(c.packages) == 0 {
		return nil
	}
	gen, err := k.repo.NextGeneration(ctx, q, now)
	if err != nil {
		return err
	}
	for _, code := range c.codes {
		code.Generation = gen
	}
	return k.repo.ReplaceCode(ctx, q, c.packages, c.codes)
}

// storedPrograms decodes the compiled code in the repository.
func (k *Kernel) storedPrograms(ctx context.Context, q dialect.Querier) (map[model.PackageID]*bytecode.Program, error) {
	codes, err := k.repo.LoadCode(ctx, q)
	if err != nil {
		return nil, err
	}
	out := map[model.PackageID]*bytecode.Program{}
	for id, c := range codes {
		p, err := bytecode.Decode(c.Bytes)
		if err != nil {
			return nil, fmt.Errorf("the compiled code of %s: %w", id, err)
		}
		out[id] = p
	}
	return out, nil
}

// recompileOutdated compiles all packages again when stored code has an
// older bytecode format, e.g. after the kernel was updated.
func (k *Kernel) recompileOutdated(ctx context.Context) error {
	v, err := k.repo.KernelVersion(ctx, k.db)
	if err != nil || v != k.target || !k.repo.HasCode() {
		return err
	}
	codes, err := k.repo.LoadCode(ctx, k.db)
	if err != nil {
		return err
	}
	outdated := false
	for _, c := range codes {
		outdated = outdated || c.FormatVersion != bytecode.FormatVersion
	}
	if !outdated {
		return nil
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		// Old code cannot be decoded; everything is compiled from the sources.
		if err := k.repo.ReplaceCode(ctx, tx, s.PackageIDs(), nil); err != nil {
			return err
		}
		reasons := map[model.PackageID]string{}
		for id := range s.Sources {
			reasons[id] = "bytecode format changed"
		}
		now := k.timestamp()
		c, _, err := k.compileSet(ctx, tx, s, reasons, now)
		if err != nil {
			return err
		}
		return k.saveCode(ctx, tx, c, now)
	})
}

// codeReferences returns what the code of a package uses and includes.
func codeReferences(r *build.Result) ([]repo.CodeUse, []repo.CodeInclude) {
	var uses []repo.CodeUse
	seen := map[repo.CodeUse]bool{}
	for _, ref := range r.Program.Info.References {
		u := repo.CodeUse{File: ref.File, Line: ref.Line, Column: ref.Column, Kind: ref.Kind, Object: ref.Object}
		if ref.Member != "" {
			u.Member = model.Some(ref.Member)
		}
		if id, _, ok := model.SplitName(ref.Object); ok {
			u.ObjectPackage = model.Some(id)
		}
		if !seen[u] {
			seen[u] = true
			uses = append(uses, u)
		}
	}
	var includes []repo.CodeInclude
	byName := map[string]bool{}
	for _, inc := range r.Program.Info.IncludeRefs {
		if byName[inc.Name] {
			continue
		}
		byName[inc.Name] = true
		id, _, _ := model.SplitName(inc.Name)
		includes = append(includes, repo.CodeInclude{Name: inc.Name, Package: id, IfFound: inc.IfFound, Found: inc.Found})
	}
	return uses, includes
}
