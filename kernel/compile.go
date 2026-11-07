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
	reasons := map[model.PackageID]string{}
	for _, id := range changedPackages(cur, want) {
		if _, ok := want.Packages[id]; ok {
			reasons[id] = "installed"
			if _, was := cur.Packages[id]; was {
				reasons[id] = "upgraded"
			}
		}
	}
	return k.compileSet(ctx, q, want, reasons, now)
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
		r := build.CheckPackage(pkg, want)
		if !r.OK() {
			return nil, nil, compileError(id, reasons[id], r.Diagnostics)
		}
		prog, err := r.Compile(false, programs)
		if err != nil {
			return nil, nil, err
		}
		programs[id] = prog
		b := bytecode.Encode(prog)
		sum := sha256.Sum256(b)
		out.codes = append(out.codes, &repo.Code{Package: id, FormatVersion: bytecode.FormatVersion, LanguageVersion: info.Manifest.LanguageVersion,
			Bytes: b, SHA256: hex.EncodeToString(sum[:]), CompiledAt: now})
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
