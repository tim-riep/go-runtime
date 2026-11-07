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
	"database/sql"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/iso639"
	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// Language is a language in which translatable texts can exist.
type Language = repo.Language

// MaxLanguageNameLength is the maximum length of a language name.
const MaxLanguageNameLength = 60

// Languages manages the languages of a kernel.
type Languages struct{ k *Kernel }

// Languages returns the language operations.
func (k *Kernel) Languages() *Languages { return &Languages{k: k} }

var languagePattern = regexp.MustCompile(`^[a-z]{2}$`)

func checkLanguage(code string) (string, error) {
	name, ok := iso639.Name(code)
	if !languagePattern.MatchString(code) || !ok {
		return "", &rterr.InvalidArgument{Argument: "language", Message: fmt.Sprintf("%q is not an ISO 639-1 code in lower case", code)}
	}
	return name, nil
}

// Add sets up a language. An empty name takes the English name of the
// ISO 639-1 code.
func (l *Languages) Add(ctx context.Context, code, name string) (*Language, error) {
	isoName, err := checkLanguage(code)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = isoName
	}
	if utf8.RuneCountInString(name) > MaxLanguageNameLength {
		return nil, &rterr.InvalidArgument{Argument: "name", Message: fmt.Sprintf("at most %d characters", MaxLanguageNameLength)}
	}
	k := l.k
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	lang := Language{Code: code, Name: name}
	if err := k.repo.InsertLanguage(ctx, k.db, lang); err != nil {
		return nil, err
	}
	// The kernel and the kernel packages bring their texts in the new
	// language.
	if err := k.addKernelTexts(ctx, code); err != nil {
		return nil, err
	}
	if _, err := k.syncKernelPackages(ctx, true); err != nil {
		return nil, err
	}
	return &lang, nil
}

// List returns all languages ordered by code.
func (l *Languages) List(ctx context.Context) ([]Language, error) {
	if err := l.k.requireInit(ctx, l.k.db); err != nil {
		return nil, err
	}
	return l.k.repo.Languages(ctx, l.k.db)
}

// Remove deletes a language. If texts exist in it, Options.Confirm is
// required and the texts are removed as well. English cannot be removed.
func (l *Languages) Remove(ctx context.Context, code string, opts Options) (*plan.Plan, error) {
	if _, err := checkLanguage(code); err != nil {
		return nil, err
	}
	if code == model.DefaultLanguage {
		return nil, &rterr.InvalidArgument{Argument: "language", Message: "English always exists and cannot be removed"}
	}
	k := l.k
	var result *plan.Plan
	started := k.timestamp()
	run := func(q dialect.Querier, execute bool) error {
		if err := k.requireInit(ctx, q); err != nil {
			return err
		}
		langs, err := k.repo.LanguageCodes(ctx, q)
		if err != nil {
			return err
		}
		if !langs[code] {
			return &rterr.NotFound{Kind: "language", Name: code}
		}
		counts, err := k.repo.CountTexts(ctx, q, code)
		if err != nil {
			return err
		}
		p := &plan.Plan{Operation: plan.LanguageRemove, Dialect: k.d.Name(), Target: model.Some(code)}
		texts := 0
		for _, table := range repo.TextTables {
			if n := counts[table]; n > 0 {
				texts += n
				p.Changes = append(p.Changes, plan.Change{Kind: plan.DeleteRows, Table: table, Destructive: true, Detail: fmt.Sprintf("%d texts", n)})
			}
		}
		p.Changes = append(p.Changes, plan.Change{Kind: plan.DeleteRows, Table: repo.TLanguages, Detail: "language " + code})
		result = p
		if !execute {
			return nil
		}
		if texts > 0 && !opts.Confirm {
			return &rterr.ConfirmationRequired{Operation: "removal", Object: fmt.Sprintf("language %s with %d texts", code, texts)}
		}
		if texts > 0 {
			s, err := k.repo.Load(ctx, q)
			if err != nil {
				return err
			}
			s.RemoveLanguage(code)
			if err := k.persist(ctx, q, s); err != nil {
				return err
			}
		}
		if err := k.repo.DeleteLanguage(ctx, q, code); err != nil {
			return err
		}
		return k.writeLog(ctx, q, p, started, repo.StatusSuccess, model.None[string]())
	}
	if opts.DryRun {
		if err := run(k.db, false); err != nil {
			return nil, err
		}
		return result, nil
	}
	if err := k.d.Transact(ctx, k.db, dialect.TxOptions{DeferForeignKeys: true}, func(tx *sql.Tx) error { return run(tx, true) }); err != nil {
		return nil, err
	}
	result.Executed = true
	return result, nil
}

// addKernelTexts writes the labels of the data elements of the kernel in a
// language that was set up.
func (k *Kernel) addKernelTexts(ctx context.Context, lang string) error {
	pkg, err := kernelschema.Package(k.target)
	if err != nil {
		return err
	}
	for _, d := range pkg.DataElements {
		if t, ok := d.Texts[lang]; ok {
			if err := k.repo.InsertDataElementText(ctx, k.db, d.Name, lang, t); err != nil {
				return err
			}
		}
	}
	return nil
}
