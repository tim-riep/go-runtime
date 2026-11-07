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

	"github.com/tim-riep/go-runtime/dialect"
)

// Language is a row of the language table.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// InsertLanguage adds a language.
func (r *Repo) InsertLanguage(ctx context.Context, q dialect.Querier, l Language) error {
	return r.insert(ctx, q, TLanguages, row{"LANGUAGE": l.Code, "NAME": l.Name})
}

// Languages returns all languages ordered by code.
func (r *Repo) Languages(ctx context.Context, q dialect.Querier) ([]Language, error) {
	rows, err := r.selectRows(ctx, q, TLanguages, []string{"LANGUAGE", "NAME"}, "LANGUAGE")
	if err != nil {
		return nil, err
	}
	out := make([]Language, 0, len(rows))
	for _, l := range rows {
		out = append(out, Language{Code: str(l["LANGUAGE"]), Name: str(l["NAME"])})
	}
	return out, nil
}

// LanguageCodes returns the codes of all languages.
func (r *Repo) LanguageCodes(ctx context.Context, q dialect.Querier) (map[string]bool, error) {
	langs, err := r.Languages(ctx, q)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range langs {
		out[l.Code] = true
	}
	return out, nil
}

// DeleteLanguage removes a language row.
func (r *Repo) DeleteLanguage(ctx context.Context, q dialect.Querier, code string) error {
	_, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(TLanguages)+" WHERE "+r.d.QuoteIdent("LANGUAGE")+" = "+r.d.Placeholder(1), code)
	return err
}
