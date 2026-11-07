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
	"path/filepath"
	"slices"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/kernelpkg"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/native/nativetest"
)

// withTestKernel runs fn on every database with a kernel that has the
// test kernel package TRTEST and its natives, initialized and with the
// tenant ACME.
func withTestKernel(t *testing.T, fn func(t *testing.T, k *kernel.Kernel)) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			pkgs, err := kernelpkg.Packages()
			if err != nil {
				t.Fatal(err)
			}
			trtest := testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "native", "TRTEST"))
			k, err := kernel.OpenConfig(ctx, db.DSN, kernel.Config{Natives: native.Default.Merge(nativetest.Registry),
				KernelPackages: append(pkgs, trtest)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { k.Close() })
			if _, err := k.Init(ctx, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
				t.Fatal(err)
			}
			nativetest.Recorded()
			fn(t, k)
		})
	}
}

// TestEffects runs effects of native methods after the commit of their
// session, drops them on rollback and records failures for a retry.
func TestEffects(t *testing.T) {
	withTestKernel(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		const N = "/TRTEST/NATIVES"
		expect := func(want ...string) {
			t.Helper()
			if got := nativetest.Recorded(); !slices.Equal(got, want) {
				t.Errorf("effects ran: %q, want %q", got, want)
			}
		}
		mustCall(t, k, "ACME", N, "NOTIFY_COMMIT", `"committed"`)
		expect("committed")
		mustCall(t, k, "ACME", N, "NOTIFY_ROLLBACK", `"rolled back"`)
		expect()
		// The end of the execution commits the default session.
		mustCall(t, k, "ACME", N, "NOTIFY", `"at the end"`)
		expect("at the end")
		mustCall(t, k, "ACME", N, "NOTIFY_OTHER", `"other"`)
		expect("other", "other in the default session")

		nativetest.Failing.Store(true)
		mustCall(t, k, "ACME", N, "NOTIFY_COMMIT", `"lost"`)
		expect()
		failed, err := k.Effects(ctx)
		if err != nil || len(failed) != 1 || failed[0].Kind != "TRTEST.RECORD" || failed[0].Tenant.Or("") != "ACME" ||
			failed[0].Error != "the receiver is down" || failed[0].Attempts != 1 {
			t.Fatalf("failed effects: %+v, %v", failed, err)
		}
		if err := k.RetryEffect(ctx, failed[0].ID); err == nil {
			t.Fatal("the retry of a failing effect succeeded")
		}
		if again, _ := k.Effects(ctx); len(again) != 1 || again[0].Attempts != 2 || again[0].ID != failed[0].ID {
			t.Fatalf("after a failed retry: %+v", again)
		}
		nativetest.Failing.Store(false)
		if err := k.RetryEffect(ctx, failed[0].ID); err != nil {
			t.Fatal(err)
		}
		expect("lost")
		if left, _ := k.Effects(ctx); len(left) != 0 {
			t.Errorf("effects left: %+v", left)
		}
	})
}
