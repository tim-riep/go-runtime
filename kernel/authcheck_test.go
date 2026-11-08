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
	"strconv"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/kernelpkg"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/native/nativetest"
)

// openCheckKernel opens a kernel with the test natives and the examples,
// and the user anna with the role /TRFIN/COMPANY_CLERK in ACME.
func openCheckKernel(tb testing.TB, dsn string) *kernel.Kernel {
	ctx := context.Background()
	pkgs, err := kernelpkg.Packages()
	if err != nil {
		tb.Fatal(err)
	}
	trtest := testutil.LoadDir(tb, filepath.Join(testutil.ModuleRoot(tb), "testdata", "lang", "native", "TRTEST"))
	k, err := kernel.OpenConfig(ctx, dsn, kernel.Config{Natives: native.Default.Merge(nativetest.Registry), KernelPackages: append(pkgs, trtest)})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { k.Close() })
	if _, err := k.Init(ctx, kernel.Options{}); err != nil {
		tb.Fatal(err)
	}
	if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
		tb.Fatal(err)
	}
	if _, err := k.Languages().Add(ctx, "de", ""); err != nil {
		tb.Fatal(err)
	}
	if _, err := k.Install(ctx, []*model.Package{testutil.Example(tb, "TROTTO"), testutil.Example(tb, "TRFIN")}, kernel.Options{}); err != nil {
		tb.Fatal(err)
	}
	if _, err := k.Users().Create(ctx, "anna", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
		tb.Fatal(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRFIN/COMPANY_CLERK", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
		tb.Fatal(err)
	}
	return k
}

// TestAuthorize checks authorizations from a native method: success, any,
// /RT/NOT_AUTHORIZED with its object and values, the text in the language
// of the execution and the hint without a user.
func TestAuthorize(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			k := openCheckKernel(t, db.DSN)
			const N = "/TRTEST/NATIVES"
			anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
			for _, c := range []struct {
				country, any, want string
			}{{"DE", "false", `"authorized"`}, {"DE", "true", `"authorized"`}, {"PT", "false", `"authorized"`}} {
				if got, err := call(k, anna, N, "AUTHORIZE", `"`+c.country+`"`, c.any); err != nil || got != c.want {
					t.Errorf("AUTHORIZE(%s, %s) = %s, %v", c.country, c.any, got, err)
				}
			}
			_, err := call(k, anna, N, "AUTHORIZE", `"FR"`, "false")
			if exceptionClass(err) != model.NotAuthorizedClass || !strings.Contains(err.Error(), "no authorization for /TRFIN/COMPANY") ||
				strings.Contains(err.Error(), "--user") {
				t.Errorf("FR: %v", err)
			}
			if got, err := call(k, anna, N, "CAUGHT", `"FR"`); err != nil || got != `"/TRFIN/COMPANY ACTIVITY=READ COMPANY_ID=any COUNTRY=FR"` {
				t.Errorf("caught: %s, %v", got, err)
			}
			de := anna
			de.Language = "de"
			if _, err := call(k, de, N, "AUTHORIZE", `"FR"`, "false"); !strings.Contains(err.Error(), "keine Berechtigung für /TRFIN/COMPANY") {
				t.Errorf("German: %v", err)
			}
			if _, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, N, "AUTHORIZE", `"DE"`, "false"); exceptionClass(err) != model.NotAuthorizedClass ||
				!strings.Contains(err.Error(), "--user") {
				t.Errorf("without user: %v", err)
			}
		})
	}
}

// BenchmarkCheckInExecution measures checks of an execution with a warm
// cache of effective authorizations: one execution runs b.N checks.
func BenchmarkCheckInExecution(b *testing.B) {
	k := openCheckKernel(b, testutil.Databases(b)[0].DSN)
	anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
	if _, err := call(k, anna, "/TRTEST/NATIVES", "AUTHORIZE_MANY", "1"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	if _, err := call(k, anna, "/TRTEST/NATIVES", "AUTHORIZE_MANY", strconv.Itoa(b.N)); err != nil {
		b.Fatal(err)
	}
}
