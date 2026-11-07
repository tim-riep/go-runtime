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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/testrun"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// Test runs the test classes of a package against the installed
// packages. With dir nil it tests the installed package id from its
// stored sources; otherwise it compiles dir without installing it, so
// that only tables of installed packages exist.
//
// Every test method gets a tenant of its own, which is created before and
// deleted with all its rows after the method. commit inside a test only
// sets a savepoint that rollback returns to; at the end every session is
// rolled back. So a session does not see what another one "committed",
// and under SQLite only one session of a test can write.
func (k *Kernel) Test(ctx context.Context, id model.PackageID, dir *model.Package, opts ExecOptions) ([]testrun.Result, error) {
	opts.defaults()
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	s, err := k.repo.Load(ctx, k.db)
	if err != nil {
		return nil, err
	}
	schema := s
	var pkg *model.Package
	if dir == nil {
		info, ok := s.Packages[id]
		if !ok {
			return nil, &rterr.NotFound{Kind: "package", Name: string(id)}
		}
		pkg = &model.Package{Manifest: info.Manifest, Sources: s.Sources[id]}
	} else {
		pkg, id = dir, dir.Manifest.ID
		if _, installed := s.Packages[id]; !installed {
			schema = s.Clone()
			now := k.timestamp()
			if err := schema.AddPackage(dir, now, now); err != nil {
				return nil, err
			}
		} else {
			// The installed schema stays; only the code is that of dir.
			pkg = &model.Package{Manifest: s.Packages[id].Manifest, Sources: dir.Sources}
		}
	}
	r := build.CheckPackageWith(pkg, schema, k.natives)
	if !r.OK() {
		return nil, compileError(id, "test", r.Diagnostics)
	}
	programs, err := k.storedPrograms(ctx, k.db)
	if err != nil {
		return nil, err
	}
	prog, err := r.Compile(true, programs)
	if err != nil {
		return nil, err
	}
	kernelProg, err := build.KernelProgram()
	if err != nil {
		return nil, err
	}
	progs := []*bytecode.Program{kernelProg}
	for _, dep := range r.Imports[1:] {
		if p := programs[dep]; p != nil {
			progs = append(progs, p)
		}
	}
	image, err := vm.LinkWith(0, k.natives, append(progs, prog)...)
	if err != nil {
		return nil, err
	}
	phys := schema.Physical()
	wrap := func(ctx context.Context, vo vm.Options, run func(vm.Options) error) error {
		tenant, err := k.testTenant(ctx)
		if err != nil {
			return err
		}
		x := &execution{k: k, d: k.d, phys: phys, tenant: tenant, opts: opts, test: true}
		x.sessions = []*session{{x: x, n: 1}}
		vo.Database = x
		vo.MaxRows = opts.MaxRows
		err = run(vo)
		err = errors.Join(err, x.finish(err))
		if _, derr := k.Tenants().Delete(context.Background(), tenant, Options{Confirm: true}); derr != nil {
			err = errors.Join(err, fmt.Errorf("deleting the test tenant %s: %w", tenant, derr))
		}
		return err
	}
	return testrun.RunEach(ctx, image, string(id), opts.VM, wrap)
}

// testTenant creates a tenant TEST_<random> for one test method.
func (k *Kernel) testTenant(ctx context.Context) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := "TEST_" + strings.ToUpper(hex.EncodeToString(b[:]))
	desc := model.Some("test run of " + time.Now().UTC().Format(time.RFC3339))
	if _, err := k.Tenants().Create(ctx, id, desc); err != nil {
		return "", err
	}
	return id, nil
}
