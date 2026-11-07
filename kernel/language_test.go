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

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/rterr"
)

func TestLanguages(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
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
