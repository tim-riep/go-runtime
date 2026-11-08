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
	"os"
	"path/filepath"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestInstallZip installs packages from ZIP archives with the package at
// the root or in a top-level directory, and rejects archives beyond the
// limits.
func TestInstallZip(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		dir := t.TempDir()
		ex := filepath.Join(testutil.ModuleRoot(t), "examples")
		fin, otto := filepath.Join(dir, "TRFIN-1.0.0.zip"), filepath.Join(dir, "trotto.zip")
		if err := os.WriteFile(fin, testutil.ZipDir(t, filepath.Join(ex, "TRFIN"), ""), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(otto, testutil.ZipDir(t, filepath.Join(ex, "TROTTO"), "TROTTO/"), 0o644); err != nil {
			t.Fatal(err)
		}
		var pkgs []*model.Package
		for _, p := range []string{fin, otto} {
			pkg, err := kernel.LoadZip(p, kernel.DefaultArchiveLimits)
			if err != nil {
				t.Fatal(err)
			}
			pkgs = append(pkgs, pkg)
		}
		if _, err := k.Install(ctx, pkgs, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
		small := kernel.DefaultArchiveLimits
		small.Archive = 1000
		_, err := kernel.LoadZip(fin, small)
		var ua *rterr.UnsafeArchive
		if !errors.As(err, &ua) {
			t.Errorf("limit: %v", err)
		}
	})
}
