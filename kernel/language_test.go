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
	"testing"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func TestLanguages(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		langs := k.Languages()
		list, err := langs.List(ctx)
		if err != nil || len(list) != 1 || list[0].Code != "en" || list[0].Name != "English" {
			t.Fatalf("after init: %v %+v", err, list)
		}
		var ia *rterr.InvalidArgument
		for _, bad := range []string{"DE", "deu", "xx", ""} {
			if _, err := langs.Add(ctx, bad, ""); !errors.As(err, &ia) {
				t.Errorf("%q accepted: %v", bad, err)
			}
		}
		if l, err := langs.Add(ctx, "de", ""); err != nil || l.Name != "German" {
			t.Fatalf("add de: %v %+v", err, l)
		}
		if l, err := langs.Add(ctx, "pl", "Polski"); err != nil || l.Name != "Polski" {
			t.Fatalf("add pl: %v %+v", err, l)
		}
		violation(t, func() error { _, err := langs.Add(ctx, "de", ""); return err }(), rterr.ViolationUnique, "duplicate language")
		if _, err := langs.Remove(ctx, "en", kernel.Options{Confirm: true}); !errors.As(err, &ia) {
			t.Fatalf("English removed: %v", err)
		}
		if _, err := langs.Remove(ctx, "pl", kernel.Options{}); err != nil {
			t.Fatalf("removing a language without texts needs no confirmation: %v", err)
		}
		var nf *rterr.NotFound
		if _, err := langs.Remove(ctx, "pl", kernel.Options{}); !errors.As(err, &nf) {
			t.Fatalf("expected NotFound, got %v", err)
		}
		list, _ = langs.List(ctx)
		if len(list) != 2 || list[0].Code != "de" || list[1].Code != "en" {
			t.Fatalf("languages: %+v", list)
		}
	})
}

func TestLanguageMissingForPackage(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		_, err := k.Install(ctx, examples(t), kernel.Options{})
		var ml *rterr.MissingLanguages
		if !errors.As(err, &ml) || len(ml.Languages) != 1 || ml.Languages[0] != "de" {
			t.Fatalf("expected the missing language de, got %v", err)
		}
		if s, _ := k.Schema(ctx); len(s.Packages) != kernelPackages {
			t.Fatal("packages installed")
		}
	})
}

func TestLanguageRemoveWithTexts(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, examples(t), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		var conf *rterr.ConfirmationRequired
		if _, err := k.Languages().Remove(ctx, "de", kernel.Options{}); !errors.As(err, &conf) {
			t.Fatalf("expected ConfirmationRequired, got %v", err)
		}
		dry, err := k.Languages().Remove(ctx, "de", kernel.Options{DryRun: true})
		if err != nil || dry.Changes[0].Detail != "70 texts" {
			t.Fatalf("dry run: %v %+v", err, dry)
		}
		if _, err := k.Languages().Remove(ctx, "de", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		d, err := k.DataElement(ctx, "/TRBASE/COUNTRY")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := d.DataElement.Texts["de"]; ok || d.DataElement.Texts["en"].Long != "Country key" {
			t.Fatalf("texts: %+v", d.DataElement.Texts)
		}
		consistent(t, k)
		// Packages with German texts need the language again.
		var ml *rterr.MissingLanguages
		if _, err := k.Upgrade(ctx, testutil.Example(t, "TRFIN"), kernel.Options{}); !errors.As(err, &ml) {
			t.Fatalf("expected MissingLanguages, got %v", err)
		}
	})
}

// TestKernelTexts writes the German labels of the data element ACTIVITY of
// the kernel only when German is set up, also on a migration.
func TestKernelTexts(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			kernelTexts(t, db.DSN)
		})
	}
}

// kernelTexts runs TestKernelTexts on a database. The kernel has no
// kernel packages, since those of this kernel need the latest version.
func kernelTexts(t *testing.T, dsn string) {
	ctx := context.Background()
	k, err := kernel.OpenConfig(ctx, dsn, kernel.Config{KernelPackages: []*model.Package{}})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	{
		labels := func() map[string]model.DataElementText {
			s, err := k.Schema(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return s.DataElements[model.ActivityDomain].Texts
		}
		k.SetTargetVersion(10)
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Languages().Add(ctx, "fr", ""); err != nil {
			t.Fatal(err)
		}
		k.SetTargetVersion(kernelschema.Latest())
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if l := labels(); len(l) != 1 || l["en"].Medium != "Activity" {
			t.Fatalf("labels after the migration: %v", l)
		}
		if _, err := k.Languages().Add(ctx, "de", ""); err != nil {
			t.Fatal(err)
		}
		if l := labels(); l["de"].Medium != "Aktivität" {
			t.Fatalf("labels after adding German: %v", l)
		}
		if _, err := k.Languages().Remove(ctx, "de", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if l := labels(); len(l) != 1 {
			t.Fatalf("labels after removing German: %v", l)
		}
	}
}
