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
	"encoding/json"
	"fmt"
	"slices"

	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/internal/semver"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/source"
)

// LockFormatVersion is the version of the lock file format.
const LockFormatVersion = 1

// Lock records the origins of the installed packages, to install exactly
// these commits and archives into another database.
type Lock struct {
	FormatVersion int         `json:"formatVersion"`
	Packages      []LockEntry `json:"packages"`
}

// LockEntry is a package of a lock.
type LockEntry struct {
	ID      model.PackageID `json:"id"`
	Version string          `json:"version"`
	Kind    string          `json:"kind"` // asset, archive or local
	Address string          `json:"address,omitempty"`
	Tag     string          `json:"tag,omitempty"`
	Commit  string          `json:"commit,omitempty"`
	Asset   string          `json:"asset,omitempty"`
	SHA256  string          `json:"sha256,omitempty"`
}

// ExportLock returns the origins of all installed packages except the
// kernel packages, in installation order.
func (k *Kernel) ExportLock(ctx context.Context) (*Lock, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	origins, err := k.repo.Origins(ctx, k.db)
	if err != nil {
		return nil, err
	}
	ids := s.PackageIDs()
	slices.SortStableFunc(ids, func(a, b model.PackageID) int { return s.Packages[a].InstalledAt.Compare(s.Packages[b].InstalledAt) })
	l := &Lock{FormatVersion: LockFormatVersion, Packages: []LockEntry{}}
	for _, id := range ids {
		if k.isKernelPackage(id) {
			continue
		}
		e := LockEntry{ID: id, Version: s.Packages[id].Manifest.Version, Kind: repo.OriginLocal}
		if o, ok := origins[id]; ok && o.Kind != repo.OriginLocal {
			e.Kind, e.Address, e.Tag, e.Commit, e.Asset, e.SHA256 = o.Kind, o.Address.Or(""), o.Tag.Or(""), o.Commit.Or(""), o.Asset.Or(""), o.SHA256.Or("")
		}
		l.Packages = append(l.Packages, e)
	}
	return l, nil
}

// ParseLock reads a lock file.
func ParseLock(data []byte) (*Lock, error) {
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, &rterr.InvalidArgument{Argument: "lock", Message: err.Error()}
	}
	if l.FormatVersion != LockFormatVersion {
		return nil, &rterr.InvalidArgument{Argument: "lock", Message: fmt.Sprintf("format version %d is not %d", l.FormatVersion, LockFormatVersion)}
	}
	return &l, nil
}

// ResolveLock resolves the packages of a lock: archives come from the
// cache, else from their source, and must have the recorded hash.
// Installed packages of the same version stay; local packages must be
// installed already.
func (k *Kernel) ResolveLock(ctx context.Context, l *Lock, req SourceRequest) (*Resolution, error) {
	r, err := k.newResolver(ctx, req)
	if err != nil {
		return nil, err
	}
	cache := source.Cache{Dir: k.sources.CacheDir}
	for _, e := range l.Packages {
		if k.isKernelPackage(e.ID) {
			continue
		}
		info, installed := r.cur.Packages[e.ID]
		if e.Kind == repo.OriginLocal {
			if !installed || info.Manifest.Version != e.Version {
				return nil, &rterr.InvalidArgument{Argument: "lock", Message: fmt.Sprintf("%s %s comes from a directory or ZIP file; install it first", e.ID, e.Version)}
			}
			continue
		}
		if installed && info.Manifest.Version == e.Version && r.origins[e.ID].SHA256.Or("") == e.SHA256 {
			continue
		}
		a, err := r.parse(e.Address)
		if err != nil {
			return nil, err
		}
		if e.Tag != "" {
			sp, _ := source.ParseSpec(e.Tag)
			a = a.WithSpec(sp)
		}
		res := source.Resolved{Address: a, Commit: e.Commit}
		kind := source.ArchiveKind(e.Kind)
		if kind == source.Asset {
			res.Tag, res.Version = e.Tag, e.Version
		}
		data, ok := cache.Get(e.SHA256)
		if !ok {
			src, err := r.sourceOf(a)
			if err != nil {
				return nil, err
			}
			arc, err := src.Fetch(ctx, res, string(e.ID), k.limits().Archive)
			if err != nil {
				return nil, err
			}
			if arc.SHA256 != e.SHA256 || arc.Kind != kind {
				return nil, &rterr.IntegrityError{Address: e.Address, Detail: fmt.Sprintf("the %s of %s has sha256 %s, the lock %s", arc.Kind, e.ID, arc.SHA256, e.SHA256)}
			}
			if _, err := cache.Put(arc.Data); err != nil {
				return nil, err
			}
			data = arc.Data
		}
		res.Tag, res.Version = e.Tag, e.Version
		n, err := r.load(a, res, source.Archive{Kind: kind, Name: e.Asset, Data: data, SHA256: e.SHA256}, string(e.ID))
		if err != nil {
			return nil, err
		}
		n.origin.Asset = model.None[string]()
		if e.Asset != "" {
			n.origin.Asset, n.source.Asset = model.Some(e.Asset), e.Asset
		}
		if installed {
			n.upgrade, n.from = true, info.Manifest.Version
		}
		r.add(n)
	}
	return &Resolution{nodes: r.order, allowUp: true}, nil
}

// Outdated is a package from a source with its versions.
type Outdated struct {
	ID        model.PackageID `json:"id"`
	Address   string          `json:"address"`
	Installed string          `json:"installed"`
	// Matching is the highest version that the constraint of the
	// installation and the requirements of the installed packages allow,
	// Latest the highest stable version; "" when there is none.
	Matching string `json:"matching"`
	Latest   string `json:"latest"`
}

// Outdated lists the packages from sources with the highest versions
// their tags offer.
func (k *Kernel) Outdated(ctx context.Context) ([]Outdated, error) {
	r, err := k.newResolver(ctx, SourceRequest{})
	if err != nil {
		return nil, err
	}
	out := []Outdated{}
	for _, id := range r.cur.PackageIDs() {
		o, ok := r.origins[id]
		if !ok || o.Kind == repo.OriginLocal {
			continue
		}
		a, err := r.parse(o.Address.Or(""))
		if err != nil {
			return nil, err
		}
		src, err := r.sourceOf(a)
		if err != nil {
			return nil, err
		}
		item := Outdated{ID: id, Address: a.Location(), Installed: r.cur.Packages[id].Manifest.Version}
		tags, err := src.Tags(ctx, a)
		if err != nil {
			return nil, err
		}
		rels := source.ReleasesOf(a, tags)
		if rel, err := source.Choose(a.Location(), rels, source.Spec{Kind: source.Latest}); err == nil {
			item.Latest = rel.Version
		}
		sp, _ := source.ParseSpec(o.Constraint.Or(""))
		if sp.Kind == source.Constraint || sp.Kind == source.Version {
			if rel, err := source.Choose(a.Location(), rels, sp, texts(r.installedBounds(id))...); err == nil {
				item.Matching = rel.Version
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// Newer reports whether an outdated entry has a higher matching or latest
// version than installed.
func (o Outdated) Newer() bool {
	inst, err := semver.Parse(o.Installed)
	if err != nil {
		return false
	}
	for _, v := range []string{o.Matching, o.Latest} {
		if nv, err := semver.Parse(v); err == nil && semver.Compare(nv, inst) > 0 {
			return true
		}
	}
	return false
}

