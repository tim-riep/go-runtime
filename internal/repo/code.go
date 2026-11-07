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

package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Kernel tables of source files and compiled code.
const (
	TSources        = model.KernelNamespace + "SOURCES"
	TBytecode       = model.KernelNamespace + "BYTECODE"
	TCodeGeneration = model.KernelNamespace + "CODE_GENERATION"
	TCodeUses       = model.KernelNamespace + "CODE_USES"
	TCodeIncludes   = model.KernelNamespace + "CODE_INCLUDES"
)

// HasCode reports whether the version stores source files and code.
func (r *Repo) HasCode() bool { return r.has(TBytecode, "") }

// Code is the compiled code of a package with what it uses.
type Code struct {
	Package         model.PackageID
	FormatVersion   int
	LanguageVersion int
	Bytes           []byte
	SHA256          string
	Generation      int64
	CompiledAt      time.Time
	Uses            []CodeUse
	Includes        []CodeInclude
}

// CodeUse is an object used by code.
type CodeUse struct {
	File          string                          `json:"file"`
	Line          int                             `json:"line"`
	Column        int                             `json:"column"`
	Kind          string                          `json:"kind"` // table, field, structure, component, ...
	Object        string                          `json:"object"`
	Member        model.Optional[string]          `json:"member"`
	ObjectPackage model.Optional[model.PackageID] `json:"objectPackage"`
}

// CodeInclude is an include inserted by code, or skipped because it does
// not exist (include ... if found).
type CodeInclude struct {
	Name    string
	Package model.PackageID // the package of its namespace
	IfFound bool
	Found   bool
}

func (r *Repo) loadSources(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	rows, err := r.selectRows(ctx, q, TSources, []string{"PACKAGE_ID", "FILE_PATH", "OBJECT_NAME", "KIND", "TEXT"}, "PACKAGE_ID", "FILE_PATH")
	if err != nil {
		return err
	}
	for _, src := range rows {
		id := owner(src["PACKAGE_ID"])
		s.Sources[id] = append(s.Sources[id], model.Source{Name: str(src["OBJECT_NAME"]), Kind: model.SourceKind(str(src["KIND"])),
			File: str(src["FILE_PATH"]), Text: str(src["TEXT"])})
	}
	return nil
}

func (r *Repo) writeSources(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, id := range s.PackageIDs() {
		for _, src := range s.Sources[id] {
			sum := sha256.Sum256([]byte(src.Text))
			if err := r.insert(ctx, q, TSources, row{"PACKAGE_ID": string(id), "FILE_PATH": src.File, "OBJECT_NAME": src.Name,
				"KIND": string(src.Kind), "TEXT": src.Text, "SHA256": hex.EncodeToString(sum[:])}); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadCode reads the compiled code of all packages, by package.
func (r *Repo) LoadCode(ctx context.Context, q dialect.Querier) (map[model.PackageID]*Code, error) {
	out := map[model.PackageID]*Code{}
	if !r.HasCode() {
		return out, nil
	}
	rows, err := r.selectRows(ctx, q, TBytecode, []string{"PACKAGE_ID", "FORMAT_VERSION", "LANGUAGE_VERSION", "CODE", "SHA256", "GENERATION", "COMPILED_AT"}, "PACKAGE_ID")
	if err != nil {
		return nil, err
	}
	for _, b := range rows {
		code, _ := b["CODE"].([]byte)
		id := owner(b["PACKAGE_ID"])
		out[id] = &Code{Package: id, FormatVersion: num(b["FORMAT_VERSION"]), LanguageVersion: num(b["LANGUAGE_VERSION"]), Bytes: code,
			SHA256: str(b["SHA256"]), Generation: int64(num(b["GENERATION"])), CompiledAt: tm(b["COMPILED_AT"])}
	}
	uses, err := r.selectRows(ctx, q, TCodeUses, []string{"PACKAGE_ID", "FILE_PATH", "LINE_NO", "COLUMN_NO", "OBJECT_KIND", "OBJECT_NAME", "MEMBER_NAME", "OBJECT_PACKAGE"}, "PACKAGE_ID", "POSITION")
	if err != nil {
		return nil, err
	}
	for _, u := range uses {
		if c := out[owner(u["PACKAGE_ID"])]; c != nil {
			use := CodeUse{File: str(u["FILE_PATH"]), Line: num(u["LINE_NO"]), Column: num(u["COLUMN_NO"]), Kind: str(u["OBJECT_KIND"]),
				Object: str(u["OBJECT_NAME"]), Member: opt(u["MEMBER_NAME"])}
			if p, ok := opt(u["OBJECT_PACKAGE"]).Get(); ok {
				use.ObjectPackage = model.Some(model.PackageID(p))
			}
			c.Uses = append(c.Uses, use)
		}
	}
	incs, err := r.selectRows(ctx, q, TCodeIncludes, []string{"PACKAGE_ID", "INCLUDE_NAME", "INCLUDE_PACKAGE", "IF_FOUND", "FOUND"}, "PACKAGE_ID", "INCLUDE_NAME")
	if err != nil {
		return nil, err
	}
	for _, i := range incs {
		if c := out[owner(i["PACKAGE_ID"])]; c != nil {
			c.Includes = append(c.Includes, CodeInclude{Name: str(i["INCLUDE_NAME"]), Package: model.PackageID(str(i["INCLUDE_PACKAGE"])),
				IfFound: flag(i["IF_FOUND"]), Found: flag(i["FOUND"])})
		}
	}
	return out, nil
}

// SaveCode writes the compiled code of packages; their earlier rows must
// be gone, which Write ensures.
func (r *Repo) SaveCode(ctx context.Context, q dialect.Querier, codes []*Code) error {
	if !r.HasCode() {
		return nil
	}
	for _, c := range codes {
		if err := r.insert(ctx, q, TBytecode, row{"PACKAGE_ID": string(c.Package), "FORMAT_VERSION": int64(c.FormatVersion),
			"LANGUAGE_VERSION": int64(c.LanguageVersion), "CODE": c.Bytes, "SHA256": c.SHA256, "GENERATION": c.Generation,
			"COMPILED_AT": c.CompiledAt}); err != nil {
			return err
		}
		for i, u := range c.Uses {
			var pkg any
			if p, ok := u.ObjectPackage.Get(); ok {
				pkg = ownerValue(p)
			}
			if err := r.insert(ctx, q, TCodeUses, row{"PACKAGE_ID": string(c.Package), "POSITION": int64(i + 1), "FILE_PATH": u.File,
				"LINE_NO": int64(u.Line), "COLUMN_NO": int64(u.Column), "OBJECT_KIND": u.Kind, "OBJECT_NAME": u.Object,
				"MEMBER_NAME": val(u.Member), "OBJECT_PACKAGE": pkg}); err != nil {
				return err
			}
		}
		for _, inc := range c.Includes {
			if err := r.insert(ctx, q, TCodeIncludes, row{"PACKAGE_ID": string(c.Package), "INCLUDE_NAME": inc.Name,
				"INCLUDE_PACKAGE": string(inc.Package), "IF_FOUND": inc.IfFound, "FOUND": inc.Found}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Generation returns the current generation of the compiled code, 0 if
// there has been none.
func (r *Repo) Generation(ctx context.Context, q dialect.Querier) (int64, error) {
	if !r.HasCode() {
		return 0, nil
	}
	rows, err := r.selectRows(ctx, q, TCodeGeneration, []string{"GENERATION"})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	n, _ := rows[0]["GENERATION"].(int64)
	return n, nil
}

// NextGeneration increases the generation and returns it.
func (r *Repo) NextGeneration(ctx context.Context, q dialect.Querier, at time.Time) (int64, error) {
	g, err := r.Generation(ctx, q)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(TCodeGeneration)); err != nil {
		return 0, fmt.Errorf("writing %s: %w", TCodeGeneration, err)
	}
	return g + 1, r.insert(ctx, q, TCodeGeneration, row{"ID": int64(1), "GENERATION": g + 1, "CHANGED_AT": at})
}

// ReplaceCode removes the code of the given packages and writes the new
// code; packages in codes are replaced, the others only removed.
func (r *Repo) ReplaceCode(ctx context.Context, q dialect.Querier, packages []model.PackageID, codes []*Code) error {
	if !r.HasCode() {
		return nil
	}
	for _, id := range packages {
		for _, t := range []string{TCodeIncludes, TCodeUses, TBytecode} {
			if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(t)+" WHERE "+r.d.QuoteIdent("PACKAGE_ID")+" = "+r.d.Placeholder(1), string(id)); err != nil {
				return fmt.Errorf("writing %s: %w", t, err)
			}
		}
	}
	return r.SaveCode(ctx, q, codes)
}
