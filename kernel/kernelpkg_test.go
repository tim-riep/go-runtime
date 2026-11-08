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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/kernelpkg"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// TestKernelPackagesFromBinaryOnly installs TRBASE on initialization and
// rejects it, and packages that claim to be kernel packages, from
// directories.
func TestKernelPackagesFromBinaryOnly(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		p, err := k.Init(ctx, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(p.Text(), "TRBASE") {
			t.Errorf("init does not install TRBASE:\n%s", p.Text())
		}
		s, _ := k.Schema(ctx)
		if info := s.Packages["TRBASE"]; info == nil || !info.Manifest.KernelPackage || info.Manifest.Version != kernelpkg.Version {
			t.Fatalf("TRBASE: %+v", info)
		}
		var kp *rterr.KernelPackage
		if _, err := k.Upgrade(ctx, testutil.Example(t, "TRBASE"), kernel.Options{}); !errors.As(err, &kp) || kp.Operation != "upgrade" {
			t.Errorf("upgrade from a directory: %v", err)
		}
		if _, err := k.Uninstall(ctx, "TRBASE", kernel.Options{Confirm: true}); !errors.As(err, &kp) {
			t.Errorf("uninstall: %v", err)
		}
		fake := testutil.Example(t, "TRFIN")
		fake.Manifest.KernelPackage = true
		if _, err := k.Install(ctx, []*model.Package{fake}, kernel.Options{}); !errors.As(err, &kp) || kp.Package != "TRFIN" {
			t.Errorf("a package marked as kernel package: %v", err)
		}
		dir := testutil.CopyExample(t, "TRBASE")
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{}); !errors.As(err, &kp) {
			t.Errorf("install from a directory: %v", err)
		}
	})
}

// TestKernelPackageUpgradeOnOpen opens a database with a newer kernel:
// it upgrades TRBASE and compiles the packages that require it again. A
// kernel whose TRBASE breaks an installed package does not open.
func TestKernelPackageUpgradeOnOpen(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			initialized(t, k)
			if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			gen := generation(t, k)
			k.Close()

			newer := testutil.Example(t, "TRBASE")
			newer.Manifest.Version = "1.7.0"
			newer.Domains = append(newer.Domains, model.Domain{Name: "/TRBASE/NEWER", Type: model.Bool})
			k, err = kernel.OpenConfig(ctx, db.DSN, kernel.Config{KernelPackages: []*model.Package{newer}})
			if err != nil {
				t.Fatal(err)
			}
			s, _ := k.Schema(ctx)
			if s.Packages["TRBASE"].Manifest.Version != "1.7.0" || s.Domains["/TRBASE/NEWER"] == nil {
				t.Fatalf("TRBASE was not upgraded: %s", s.Packages["TRBASE"].Manifest.Version)
			}
			if g := generation(t, k); g <= gen {
				t.Errorf("TRFIN was not compiled again: generation %d -> %d", gen, g)
			}
			log, _ := k.Log(ctx)
			upgraded := false
			for _, e := range log {
				for _, p := range e.Packages {
					upgraded = upgraded || strings.EqualFold(e.Operation, "upgrade") && p.ID == "TRBASE" && p.To.Or("") == "1.7.0"
				}
			}
			if !upgraded {
				t.Errorf("no upgrade of TRBASE in the log")
			}
			k.Close()

			// A TRBASE without the domain the code of TRFIN uses.
			broken := testutil.Example(t, "TRBASE")
			broken.Manifest.Version = "1.8.0"
			for i, d := range broken.DataElements {
				if d.Name == "/TRBASE/NAME" {
					broken.DataElements = append(broken.DataElements[:i], broken.DataElements[i+1:]...)
					break
				}
			}
			_, err = kernel.OpenConfig(ctx, db.DSN, kernel.Config{KernelPackages: []*model.Package{broken}})
			var up *rterr.KernelPackageUpgrade
			if !errors.As(err, &up) || up.Packages[0] != "TRBASE" {
				t.Fatalf("expected KernelPackageUpgrade, got %v", err)
			}
			k, err = kernel.Open(ctx, db.DSN)
			if err == nil {
				defer k.Close()
				if s, _ := k.Schema(ctx); s.Packages["TRBASE"].Manifest.Version != "1.4.0" {
					t.Error("the failed upgrade changed TRBASE")
				}
			}
		})
	}
}
