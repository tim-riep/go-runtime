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
	"fmt"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/pkgzip"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/internal/resolve"
	"github.com/tim-riep/go-runtime/internal/semver"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/source"
)

// SourceConfig configures remote package sources.
type SourceConfig struct {
	// Sources serve package addresses, such as source/github.
	Sources []source.Source
	// Allow lists the owners or owner/repo entries that packages may come
	// from, also as dependencies; empty allows all.
	Allow []string
	// CacheDir keeps downloaded archives by SHA-256; "" keeps none.
	CacheDir string
	// Limits restrict archives; zero means DefaultArchiveLimits.
	Limits ArchiveLimits
}

// SetFormatLimits replaces the limits of native methods for zip, gzip
// and xlsx.
func (k *Kernel) SetFormatLimits(l native.Limits) { k.formats = l }

// SetSources replaces the remote package sources.
func (k *Kernel) SetSources(c SourceConfig) { k.sources = c }

// SourceHosts returns the hosts of the configured sources besides
// github.com, for parsing addresses.
func (k *Kernel) SourceHosts() []string {
	var out []string
	for _, s := range k.sources.Sources {
		if h, ok := s.(interface{ Host() string }); ok && h.Host() != source.DefaultHost {
			out = append(out, h.Host())
		}
	}
	return out
}

func (k *Kernel) limits() ArchiveLimits {
	l := k.sources.Limits
	if l == (ArchiveLimits{}) {
		l = DefaultArchiveLimits
	}
	l.File = max(l.File, k.maxAssetSize)
	return l
}

// SourceRequest asks for packages from addresses, together with packages
// from directories or ZIP files.
type SourceRequest struct {
	Addresses []string
	Local     []*model.Package
	// Package chooses the package among several assets of a release.
	Package string
	// AllowBranch permits addresses that name branches.
	AllowBranch bool
	// AllowRetag accepts a tag whose commit or archive changed since it
	// was installed last.
	AllowRetag bool
	// AllowDependencyUpgrades upgrades installed packages that do not
	// satisfy the requirements of the new ones.
	AllowDependencyUpgrades bool
	// ExpectSHA256 is the hash the archive of the only address must have.
	ExpectSHA256 string
}

// node is a package of a resolution.
type node struct {
	pkg     *model.Package
	origin  repo.Origin
	source  *plan.PackageSource
	upgrade bool
	from    string
}

// Resolution is the set of packages that an installation or upgrade
// brings, downloaded into the cache but not stored; ApplyResolution
// installs it.
type Resolution struct {
	nodes    []*node
	allowUp  bool
	upToDate []string
}

// UpToDate lists the packages an upgrade found up to date.
func (r *Resolution) UpToDate() []string { return r.upToDate }

// Empty reports a resolution without packages to install or upgrade.
func (r *Resolution) Empty() bool { return len(r.nodes) == 0 }

// resolver collects the packages of a resolution.
type resolver struct {
	k       *Kernel
	ctx     context.Context
	req     SourceRequest
	cur     *model.Schema
	origins map[model.PackageID]repo.Origin
	nodes   map[model.PackageID]*node
	order   []*node
	// bounds are the constraints on a package with the package that
	// states it.
	bounds map[model.PackageID][]bound
}

type bound struct {
	constraint string
	by         model.PackageID
}

func (k *Kernel) newResolver(ctx context.Context, req SourceRequest) (*resolver, error) {
	cur, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	origins, err := k.repo.Origins(ctx, k.db)
	if err != nil {
		return nil, err
	}
	return &resolver{k: k, ctx: ctx, req: req, cur: cur, origins: origins, nodes: map[model.PackageID]*node{}, bounds: map[model.PackageID][]bound{}}, nil
}

func (r *resolver) add(n *node) {
	r.nodes[n.pkg.Manifest.ID] = n
	r.order = append(r.order, n)
}

// sourceOf returns the source that serves an address.
func (r *resolver) sourceOf(a source.Address) (source.Source, error) {
	if !source.Allowed(a, r.k.sources.Allow) {
		return nil, &rterr.InvalidArgument{Argument: "source", Message: fmt.Sprintf("%s is not among the allowed sources (RT_SOURCE_ALLOW)", a.Location())}
	}
	for _, s := range r.k.sources.Sources {
		if s.Handles(a) {
			return s, nil
		}
	}
	return nil, &rterr.InvalidArgument{Argument: "source", Message: "no package source serves " + a.Host}
}

// parse reads an address with the hosts of the sources.
func (r *resolver) parse(s string) (source.Address, error) { return source.Parse(s, r.k.SourceHosts()...) }

// installedBounds returns the constraints that installed packages state
// on a package, except those that the resolution replaces.
func (r *resolver) installedBounds(id model.PackageID) []bound {
	var out []bound
	for _, pid := range r.cur.PackageIDs() {
		if _, replaced := r.nodes[pid]; replaced {
			continue
		}
		for _, d := range r.cur.Packages[pid].Manifest.Requires {
			if c, ok := d.Constraint.Get(); ok && d.ID == id {
				out = append(out, bound{c, pid})
			}
		}
	}
	return out
}

func texts(bs []bound) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.constraint)
	}
	return out
}

// fetch resolves an address under constraints, downloads the archive into
// the cache and loads the package.
func (r *resolver) fetch(a source.Address, constraints []string, pkgHint string, allowBranch bool) (*node, error) {
	src, err := r.sourceOf(a)
	if err != nil {
		return nil, err
	}
	res, err := source.Resolve(r.ctx, src, a, allowBranch || r.req.AllowBranch, constraints...)
	if err != nil {
		return nil, err
	}
	if pkgHint == "" {
		pkgHint = source.PackageOf(a)
	}
	limits := r.k.limits()
	arc, err := src.Fetch(r.ctx, res, pkgHint, limits.Archive)
	if err != nil {
		return nil, err
	}
	if _, err := (source.Cache{Dir: r.k.sources.CacheDir}).Put(arc.Data); err != nil {
		return nil, err
	}
	return r.load(a, res, arc, pkgHint)
}

// load checks an archive against what was recorded for its tag and loads
// its package.
func (r *resolver) load(a source.Address, res source.Resolved, arc source.Archive, pkgHint string) (*node, error) {
	loc := a.Location()
	if res.Tag != "" {
		seen, err := r.k.repo.SourceTagOf(r.ctx, r.k.db, loc, res.Tag, string(arc.Kind))
		if err != nil {
			return nil, err
		}
		if seen != nil && (seen.SHA256 != arc.SHA256 || seen.Commit != res.Commit) && !r.req.AllowRetag {
			return nil, &rterr.IntegrityError{Address: loc, Detail: fmt.Sprintf("the tag %s pointed to commit %s with archive sha256 %s on %s, now to commit %s with sha256 %s; the tag was moved (allow it with --allow-retag)",
				res.Tag, seen.Commit, seen.SHA256, seen.FirstSeen.Format(time.RFC3339), res.Commit, arc.SHA256)}
		}
	}
	opts := pkgzip.Options{}
	if arc.Kind == source.SourceArchive {
		// Of a repository only the package counts, not README.md and the
		// like beside it.
		opts = pkgzip.Options{StripTop: true, Dir: a.Dir, Keep: pkgload.TopLevel}
	}
	fsys, err := pkgzip.Open(a.String(), arc.Data, r.k.limits(), opts)
	if err != nil {
		return nil, err
	}
	pkg, err := pkgload.Load(fsys, a.String())
	if err != nil {
		return nil, err
	}
	id := pkg.Manifest.ID
	switch {
	case pkgHint != "" && string(id) != pkgHint:
		return nil, &rterr.InvalidArgument{Argument: "package", Message: fmt.Sprintf("%s holds package %s, not %s", a, id, pkgHint)}
	case res.Version != "" && pkg.Manifest.Version != res.Version:
		return nil, &rterr.InvalidArgument{Argument: "version", Message: fmt.Sprintf("the tag %s of %s holds %s with version %s in its manifest", res.Tag, loc, id, pkg.Manifest.Version)}
	}
	if err := r.k.rejectKernelPackage(pkg, "install"); err != nil {
		return nil, err
	}
	o := repo.Origin{Package: id, Kind: string(arc.Kind), Address: model.Some(loc), Commit: model.Some(res.Commit), SHA256: model.Some(arc.SHA256)}
	ps := &plan.PackageSource{Kind: string(arc.Kind), Address: loc, Commit: res.Commit, SHA256: arc.SHA256}
	if a.Spec.Kind != source.Latest {
		o.Requested, ps.Requested = model.Some(a.Spec.Text), a.Spec.Text
	}
	switch a.Spec.Kind {
	case source.Constraint:
		o.Constraint = model.Some(a.Spec.Text)
	case source.Branch:
		o.Constraint = model.Some(a.Spec.Text)
	default:
		o.Constraint = model.Some("^" + pkg.Manifest.Version)
	}
	if res.Tag != "" {
		o.Tag, ps.Tag = model.Some(res.Tag), res.Tag
	}
	if arc.Kind == source.Asset {
		o.Asset, ps.Asset = model.Some(arc.Name), arc.Name
	}
	return &node{pkg: pkg, origin: o, source: ps}, nil
}

// closure adds the packages that the packages of the resolution require
// and that are missing or too old, as long as their requirement names a
// source. It chooses for each the highest version that satisfies every
// constraint known at that time; a later constraint that excludes it is
// a conflict.
func (r *resolver) closure() error {
	for i := 0; i < len(r.order); i++ {
		n := r.order[i]
		for _, dep := range n.pkg.Manifest.Requires {
			if r.k.isKernelPackage(dep.ID) {
				if dep.Source.Valid() {
					return &rterr.InvalidArgument{Argument: "requires", Message: fmt.Sprintf("%s requires the kernel package %s with a source; kernel packages come only with the kernel", n.pkg.Manifest.ID, dep.ID)}
				}
				continue
			}
			c, constrained := dep.Constraint.Get()
			if constrained {
				r.bounds[dep.ID] = append(r.bounds[dep.ID], bound{c, n.pkg.Manifest.ID})
			}
			if m, ok := r.nodes[dep.ID]; ok {
				if constrained {
					if ok, _ := semver.Satisfies(m.pkg.Manifest.Version, c); !ok {
						return r.conflict(dep.ID, m)
					}
				}
				continue
			}
			all := append(texts(r.bounds[dep.ID]), texts(r.installedBounds(dep.ID))...)
			info, installed := r.cur.Packages[dep.ID]
			if installed && satisfiesAll(info.Manifest.Version, all) {
				continue
			}
			addr, ok := dep.Source.Get()
			if !ok && installed {
				if o, has := r.origins[dep.ID]; has && o.Kind != repo.OriginLocal {
					addr, ok = o.Address.Get()
				}
			}
			if !ok {
				continue // resolve.Resolve reports the missing or too old package
			}
			a, err := r.parse(addr)
			if err != nil {
				return err
			}
			m, err := r.fetch(a, all, string(dep.ID), false)
			if err != nil {
				return fmt.Errorf("%s, required by %s: %w", dep.ID, n.pkg.Manifest.ID, err)
			}
			m.source.Dependency = string(n.pkg.Manifest.ID)
			if installed {
				if err := upgradeOf(m, info.Manifest.Version); err != nil {
					return err
				}
			}
			r.add(m)
		}
	}
	return nil
}

func (r *resolver) conflict(id model.PackageID, m *node) error {
	var parts []string
	for _, b := range r.bounds[id] {
		parts = append(parts, b.constraint+" from "+string(b.by))
	}
	return &rterr.DependencyError{Kind: rterr.DependencyVersion, Package: string(id), Required: string(id),
		Constraint: strings.Join(parts, " and "), Found: m.pkg.Manifest.Version}
}

func satisfiesAll(version string, constraints []string) bool {
	for _, c := range constraints {
		if ok, err := semver.Satisfies(version, c); err != nil || !ok {
			return false
		}
	}
	return true
}

// upgradeOf marks a node as upgrade of an installed version, which must
// not be higher.
func upgradeOf(n *node, installed string) error {
	oldV, err1 := semver.Parse(installed)
	newV, err2 := semver.Parse(n.pkg.Manifest.Version)
	if err1 == nil && err2 == nil && semver.Compare(newV, oldV) < 0 {
		return &rterr.InvalidArgument{Argument: "version", Message: fmt.Sprintf("%s %s is lower than the installed version %s", n.pkg.Manifest.ID, n.pkg.Manifest.Version, installed)}
	}
	n.upgrade, n.from = true, installed
	return nil
}

// ResolveSources resolves the addresses of a request and the dependencies
// of all its packages. Archives are downloaded into the cache; the
// database stays unchanged.
func (k *Kernel) ResolveSources(ctx context.Context, req SourceRequest) (*Resolution, error) {
	if req.ExpectSHA256 != "" && len(req.Addresses) != 1 {
		return nil, &rterr.InvalidArgument{Argument: "sha256", Message: "an expected hash needs exactly one address"}
	}
	r, err := k.newResolver(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, p := range req.Local {
		r.add(&node{pkg: p, origin: repo.Origin{Package: p.Manifest.ID, Kind: repo.OriginLocal}})
	}
	for _, s := range req.Addresses {
		a, err := r.parse(s)
		if err != nil {
			return nil, err
		}
		n, err := r.fetch(a, nil, req.Package, false)
		if err != nil {
			return nil, err
		}
		if req.ExpectSHA256 != "" && !strings.EqualFold(req.ExpectSHA256, n.source.SHA256) {
			return nil, &rterr.IntegrityError{Address: a.String(), Detail: fmt.Sprintf("the archive has sha256 %s, not the expected %s", n.source.SHA256, req.ExpectSHA256)}
		}
		if _, dup := r.nodes[n.pkg.Manifest.ID]; dup {
			return nil, &rterr.DependencyError{Kind: rterr.DependencyConflict, Package: string(n.pkg.Manifest.ID)}
		}
		r.add(n)
	}
	if err := r.closure(); err != nil {
		return nil, err
	}
	return &Resolution{nodes: r.order, allowUp: req.AllowDependencyUpgrades}, nil
}

// UpgradeSource resolves an upgrade of a package from its source: the
// highest version that the constraint of its installation, or the given
// spec, and the requirements of the installed packages allow.
func (k *Kernel) UpgradeSource(ctx context.Context, id model.PackageID, spec string, req SourceRequest) (*Resolution, error) {
	r, err := k.newResolver(ctx, req)
	if err != nil {
		return nil, err
	}
	info := r.cur.Packages[id]
	if info == nil {
		return nil, &rterr.NotInstalled{Package: string(id)}
	}
	o, ok := r.origins[id]
	if !ok || o.Kind == repo.OriginLocal {
		return nil, &rterr.InvalidArgument{Argument: "package", Message: fmt.Sprintf("%s was installed from a directory or ZIP file; upgrade it with the new directory or file", id)}
	}
	a, err := r.parse(o.Address.Or(""))
	if err != nil {
		return nil, err
	}
	branch := false
	switch {
	case spec != "":
		sp, err := source.ParseSpec(spec)
		if err != nil {
			return nil, err
		}
		a = a.WithSpec(sp)
	default:
		if req, ok := o.Requested.Get(); ok {
			if sp, _ := source.ParseSpec(req); sp.Kind == source.Branch {
				a, branch = a.WithSpec(sp), true
				break
			}
		}
		sp, err := source.ParseSpec(o.Constraint.Or(""))
		if err != nil {
			return nil, err
		}
		a = a.WithSpec(sp)
	}
	n, err := r.fetch(a, texts(r.installedBounds(id)), string(id), branch)
	if err != nil {
		return nil, err
	}
	if spec == "" && a.Spec.Kind != source.Branch {
		// The constraint of the installation stays.
		n.origin.Requested, n.origin.Constraint = o.Requested, o.Constraint
		n.source.Requested = o.Requested.Or("")
	}
	if n.pkg.Manifest.Version == info.Manifest.Version && n.origin.Commit == o.Commit && n.origin.SHA256 == o.SHA256 {
		return &Resolution{upToDate: []string{string(id) + " " + info.Manifest.Version}}, nil
	}
	if err := upgradeOf(n, info.Manifest.Version); err != nil {
		return nil, err
	}
	r.add(n)
	if err := r.closure(); err != nil {
		return nil, err
	}
	return &Resolution{nodes: r.order, allowUp: true, upToDate: nil}, nil
}

// ApplyResolution installs and upgrades the packages of a resolution in
// one transaction in dependency order. Upgrades of installed packages that
// the request did not name need AllowDependencyUpgrades.
func (k *Kernel) ApplyResolution(ctx context.Context, res *Resolution, opts Options) (*plan.Plan, error) {
	if res.Empty() {
		return nil, &rterr.InvalidArgument{Argument: "packages", Message: "nothing to install or upgrade"}
	}
	var pkgs []*model.Package
	byID := map[model.PackageID]*node{}
	var proposed []string
	subject := ""
	for i, n := range res.nodes {
		pkgs = append(pkgs, n.pkg)
		byID[n.pkg.Manifest.ID] = n
		if i > 0 {
			subject += ", "
		}
		subject += string(n.pkg.Manifest.ID)
		if n.upgrade && n.source != nil && n.source.Dependency != "" {
			proposed = append(proposed, fmt.Sprintf("%s %s -> %s", n.pkg.Manifest.ID, n.from, n.pkg.Manifest.Version))
		}
	}
	var order []*model.Package
	destructive := rejectDestructive(subject, opts)
	return k.apply(ctx, operation{
		op: plan.Install,
		packages: func(*model.Schema) []plan.PackageChange {
			var out []plan.PackageChange
			for _, p := range order {
				n := byID[p.Manifest.ID]
				pc := plan.PackageChange{ID: string(p.Manifest.ID), To: model.Some(p.Manifest.Version), Source: n.source}
				if n.upgrade {
					pc.From = model.Some(n.from)
				}
				out = append(out, pc)
			}
			return out
		},
		target: func(cur *model.Schema, now time.Time) (*model.Schema, error) {
			var errs rterr.List
			for _, n := range res.nodes {
				info, ok := cur.Packages[n.pkg.Manifest.ID]
				switch {
				case ok && !n.upgrade:
					errs = append(errs, &rterr.AlreadyInstalled{Package: string(n.pkg.Manifest.ID), Version: info.Manifest.Version})
				case !ok && n.upgrade:
					errs = append(errs, &rterr.NotInstalled{Package: string(n.pkg.Manifest.ID)})
				}
				if err := k.rejectKernelSources(n.pkg); err != nil {
					errs = append(errs, err)
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
			installedAt := map[model.PackageID]time.Time{}
			for _, n := range res.nodes {
				if n.upgrade {
					installedAt[n.pkg.Manifest.ID] = cur.Packages[n.pkg.Manifest.ID].InstalledAt
					want.RemovePackage(n.pkg.Manifest.ID)
				}
			}
			for _, p := range order {
				at, ok := installedAt[p.Manifest.ID]
				if !ok {
					at = now
				}
				if err := want.AddPackage(p, at, now); err != nil {
					errs = append(errs, err)
				}
			}
			return want, errs.Err()
		},
		check: func(p *plan.Plan) error {
			if err := destructive(p); err != nil {
				return err
			}
			if len(proposed) > 0 && !res.allowUp && !opts.DryRun {
				return &rterr.ConfirmationRequired{Operation: "the upgrade of dependencies (--upgrade-dependencies)", Object: strings.Join(proposed, ", ")}
			}
			return nil
		},
		origins: func() []repo.Origin {
			var out []repo.Origin
			for _, n := range res.nodes {
				out = append(out, n.origin)
			}
			return out
		},
	}, opts)
}

// rejectKernelSources refuses a requirement of a kernel package that
// names a source.
func (k *Kernel) rejectKernelSources(p *model.Package) error {
	for _, d := range p.Manifest.Requires {
		if d.Source.Valid() && k.isKernelPackage(d.ID) {
			return &rterr.InvalidArgument{Argument: "requires", Message: fmt.Sprintf("%s requires the kernel package %s with a source; kernel packages come only with the kernel", p.Manifest.ID, d.ID)}
		}
	}
	return nil
}

// InstallSources resolves a request and installs its packages.
func (k *Kernel) InstallSources(ctx context.Context, req SourceRequest, opts Options) (*plan.Plan, error) {
	res, err := k.ResolveSources(ctx, req)
	if err != nil {
		return nil, err
	}
	return k.ApplyResolution(ctx, res, opts)
}
