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
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/xslt"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of transformations.
const (
	TTransformations     = model.KernelNamespace + "TRANSFORMATIONS"
	TTransformationTexts = model.KernelNamespace + "TRANSFORMATION_TEXTS"
)

func (r *Repo) loadTransformations(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	rows, err := r.selectRows(ctx, q, TTransformations, []string{"TRANSFORMATION_NAME", "PACKAGE_ID", "FILE_PATH", "TEXT", "OUTPUT", "NATIVE"}, "TRANSFORMATION_NAME")
	if err != nil {
		return err
	}
	for _, t := range rows {
		name := str(t["TRANSFORMATION_NAME"])
		text := str(t["TEXT"])
		native := opt(t["NATIVE"])
		params := []model.TransformParam{}
		if !native.Valid() {
			// The parameters are read from the stylesheet itself.
			var err error
			if params, err = xslt.Inspect(text); err != nil {
				return fmt.Errorf("transformation %s: %w", name, err)
			}
		}
		s.Transformations[name] = &model.Transformation{Name: name, Owner: owner(t["PACKAGE_ID"]), File: str(t["FILE_PATH"]), Text: text,
			Params: params, Texts: map[string]model.Label{}, Output: opt(t["OUTPUT"]), Native: native}
	}
	texts, err := r.selectRows(ctx, q, TTransformationTexts, []string{"TRANSFORMATION_NAME", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if tr := s.Transformations[str(t["TRANSFORMATION_NAME"])]; tr != nil {
			tr.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	return nil
}

func (r *Repo) writeTransformations(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range slices.Sorted(maps.Keys(s.Transformations)) {
		t := s.Transformations[name]
		sum := sha256.Sum256([]byte(t.Text))
		if err := r.insert(ctx, q, TTransformations, row{"TRANSFORMATION_NAME": name, "PACKAGE_ID": string(t.Owner), "FILE_PATH": t.File,
			"TEXT": t.Text, "SHA256": hex.EncodeToString(sum[:]), "OUTPUT": val(t.Output), "NATIVE": val(t.Native)}); err != nil {
			return err
		}
		for _, lang := range slices.Sorted(maps.Keys(t.Texts)) {
			l := t.Texts[lang]
			if err := r.insert(ctx, q, TTransformationTexts, row{"TRANSFORMATION_NAME": name, "LANGUAGE": lang, "LABEL": l.Label,
				"DESCRIPTION": val(l.Description)}); err != nil {
				return err
			}
		}
	}
	return nil
}
