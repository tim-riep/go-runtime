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
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func trusePackage(t *testing.T) string {
	return filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "native", "TRUSE")
}

// TestTRBASENatives calls the methods executed by kernel of TRBASE from a
// package that requires it.
func TestTRBASENatives(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, trusePackage(t)), testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const U = "/TRUSE/USE"
		sum := sha256.Sum256([]byte("abc"))
		expectCall(t, k, "ACME", U, "HASH", `"`+base64.StdEncoding.EncodeToString(sum[:])+`"`, `"`+base64.StdEncoding.EncodeToString([]byte("abc"))+`"`)
		expectCall(t, k, "ACME", U, "IDS", "true")

		k.SetClock(func() time.Time { return time.Date(2026, 11, 7, 10, 30, 0, 123456789, time.UTC) })
		expectCall(t, k, "ACME", U, "NOW", `"2026-11-07T10:30:00.123456Z"`)
		expectCall(t, k, "ACME", U, "TODAY", `"2026-11-07"`)
		k.SetClock(time.Now)

		if got := mustCall(t, k, "ACME", U, "READ", `"TRUSE"`, `"upper.xsl"`); !strings.Contains(got, "upper:") {
			t.Errorf("READ = %s", got)
		}
		if got := mustCall(t, k, "ACME", U, "READ_DE", `"upper.xsl"`); !strings.Contains(got, "gross:") {
			t.Errorf("READ_DE = %s", got)
		}
		expectCall(t, k, "ACME", U, "READ", `"not found: missing.xsl"`, `"TRUSE"`, `"missing.xsl"`)
		// TRUSE does not require TRFIN, so it cannot read its assets.
		expectCall(t, k, "ACME", U, "READ", `"denied: TRUSE"`, `"TRFIN"`, `"invoice/invoice.xsl"`)

		expectCall(t, k, "ACME", U, "RUN", `"upper:ACME"`, `"acme"`)
		expectCall(t, k, "ACME", U, "MANY", `["upper:A","upper:B","upper:C"]`, `["a","b","c"]`)
		expectCall(t, k, "ACME", U, "BROKEN", `"line 2"`)

		// Without the capability clock in the manifest, the package does not
		// compile.
		dir := testutil.CopyDir(t, trusePackage(t))
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRUSE", "version": "1.1.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }] }`))
		_, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "/TRBASE/CLOCK.NOW needs the capability clock") {
			t.Errorf("upgrade without capability: %v", err)
		}
	})
}
