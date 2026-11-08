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
	"time"

	"github.com/tim-riep/go-runtime/internal/resolve"
	"github.com/tim-riep/go-runtime/internal/semver"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

func manifests(s *model.Schema) map[model.PackageID]model.Manifest {
	out := make(map[model.PackageID]model.Manifest, len(s.Packages))
	for id, p := range s.Packages {
		out[id] = p.Manifest
	}
	return out
}

// rejectDestructive fails if the plan contains destructive changes that
// opts does not allow.
func rejectDestructive(subject string, opts Options) func(p *plan.Plan) error {
	return func(p *plan.Plan) error {
		d := p.Destructive()
		if len(d) == 0 || opts.AllowDestructive {
			return nil
		}
		err := &rterr.DestructiveChange{Package: subject}
		for _, c := range d {
			err.Changes = append(err.Changes, rterr.Change{Object: c.Object(), Description: c.String()})
		}
		return err
	}
}

// Install installs packages that are not installed yet. Packages given
// together are installed in dependency order in one transaction.
func (k *Kernel) Install(ctx context.Context, pkgs []*model.Package, opts Options) (*plan.Plan, error) {
	for _, p := range pkgs {
		if err := k.rejectKernelPackage(p, "install"); err != nil {
			return nil, err
		}
	}
	return k.install(ctx, pkgs, opts)
}

// rejectKernelPackage refuses a kernel package, or a package that claims
// to be one, from outside the kernel binary. Kernels before the version
// with kernel packages, which tests simulate, accept them.
func (k *Kernel) rejectKernelPackage(p *model.Package, operation string) error {
	if k.target < kernelPackagesVersion {
		return nil
	}
	if p.Manifest.KernelPackage || k.isKernelPackage(p.Manifest.ID) {
		return &rterr.KernelPackage{Package: string(p.Manifest.ID), Operation: operation}
	}
	return nil
}

func (k *Kernel) isKernelPackage(id model.PackageID) bool {
	for _, p := range k.kernelPkgs {
		if p.Manifest.ID == id {
			return true
		}
	}
	return false
}

func (k *Kernel) install(ctx context.Context, pkgs []*model.Package, opts Options) (*plan.Plan, error) {
	if len(pkgs) == 0 {
		return nil, &rterr.InvalidArgument{Argument: "packages", Message: "no package given"}
	}
	var order []*model.Package
	subject := ""
	for i, p := range pkgs {
		if i > 0 {
			subject += ", "
		}
		subject += string(p.Manifest.ID)
	}
	return k.apply(ctx, operation{
		op: plan.Install,
		packages: func(*model.Schema) []plan.PackageChange {
			var out []plan.PackageChange
			for _, p := range order {
				out = append(out, plan.PackageChange{ID: string(p.Manifest.ID), To: model.Some(p.Manifest.Version)})
			}
			return out
		},
		target: func(cur *model.Schema, now time.Time) (*model.Schema, error) {
			var errs rterr.List
			for _, p := range pkgs {
				if info, ok := cur.Packages[p.Manifest.ID]; ok {
					errs = append(errs, &rterr.AlreadyInstalled{Package: string(p.Manifest.ID), Version: info.Manifest.Version})
				}
			}
			if err := errs.Err(); err != nil {
				return nil, err
			}
			var err error
			if order, err = resolve.Resolve(manifests(cur), pkgs); err != nil {
				return nil, err
			}
			want := cur.Clone()
			for _, p := range order {
				if err := want.AddPackage(p, now, now); err != nil {
					errs = append(errs, err)
				}
			}
			return want, errs.Err()
		},
		check: rejectDestructive(subject, opts),
	}, opts)
}

// Upgrade replaces an installed package with a new version. Destructive
// changes need Options.AllowDestructive. Changes that break another
// installed package are always rejected. Lower versions are rejected; the
// same version may be applied again.
func (k *Kernel) Upgrade(ctx context.Context, pkg *model.Package, opts Options) (*plan.Plan, error) {
	if err := k.rejectKernelPackage(pkg, "upgrade"); err != nil {
		return nil, err
	}
	return k.upgrade(ctx, pkg, opts)
}

func (k *Kernel) upgrade(ctx context.Context, pkg *model.Package, opts Options) (*plan.Plan, error) {
	id := pkg.Manifest.ID
	var from string
	return k.apply(ctx, operation{
		op: plan.Upgrade,
		packages: func(*model.Schema) []plan.PackageChange {
			return []plan.PackageChange{{ID: string(id), From: model.Some(from), To: model.Some(pkg.Manifest.Version)}}
		},
		target: func(cur *model.Schema, now time.Time) (*model.Schema, error) {
			info, ok := cur.Packages[id]
			if !ok {
				return nil, &rterr.NotInstalled{Package: string(id)}
			}
			from = info.Manifest.Version
			oldV, err1 := semver.Parse(from)
			newV, err2 := semver.Parse(pkg.Manifest.Version)
			if err1 == nil && err2 == nil && semver.Compare(newV, oldV) < 0 {
				return nil, &rterr.InvalidArgument{Argument: "version", Message: "version " + pkg.Manifest.Version + " is lower than the installed version " + from}
			}
			if _, err := resolve.Resolve(manifests(cur), []*model.Package{pkg}); err != nil {
				return nil, err
			}
			want := cur.Clone()
			want.RemovePackage(id)
			return want, want.AddPackage(pkg, info.InstalledAt, now)
		},
		check: rejectDestructive(string(id), opts),
	}, opts)
}

// Uninstall removes a package with its tables, domains and extension
// fields. All data in them is lost, so it needs Options.Confirm unless it is
// a dry run. Packages that others require cannot be removed.
func (k *Kernel) Uninstall(ctx context.Context, id model.PackageID, opts Options) (*plan.Plan, error) {
	if k.target >= kernelPackagesVersion && k.isKernelPackage(id) {
		return nil, &rterr.KernelPackage{Package: string(id), Operation: "uninstall"}
	}
	var from string
	return k.apply(ctx, operation{
		op: plan.Uninstall,
		packages: func(*model.Schema) []plan.PackageChange {
			return []plan.PackageChange{{ID: string(id), From: model.Some(from)}}
		},
		target: func(cur *model.Schema, _ time.Time) (*model.Schema, error) {
			info, ok := cur.Packages[id]
			if !ok {
				return nil, &rterr.NotInstalled{Package: string(id)}
			}
			from = info.Manifest.Version
			if deps := cur.Dependents(id); len(deps) > 0 {
				err := &rterr.InUse{Package: string(id)}
				for _, d := range deps {
					err.RequiredBy = append(err.RequiredBy, string(d))
				}
				return nil, err
			}
			if !opts.Confirm && !opts.DryRun {
				return nil, &rterr.ConfirmationRequired{Operation: "uninstall", Object: "package " + string(id)}
			}
			want := cur.Clone()
			want.DetachLocalRoles(id)
			want.RemovePackage(id)
			return want, nil
		},
	}, opts)
}

// Plan returns the plan for installing pkg, or for upgrading it if it is
// installed, without changing the database.
func (k *Kernel) Plan(ctx context.Context, pkg *model.Package) (*plan.Plan, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	opts := Options{DryRun: true, AllowDestructive: true}
	if _, ok := s.Packages[pkg.Manifest.ID]; ok {
		return k.Upgrade(ctx, pkg, opts)
	}
	return k.Install(ctx, []*model.Package{pkg}, opts)
}
