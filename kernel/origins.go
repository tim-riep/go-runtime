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

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Origin is where an installed package comes from.
type Origin = repo.Origin

// syncOrigins writes the origins of an operation and drops those of
// packages that are no longer installed; tags of sources are recorded so
// that a moved tag is noticed later.
func (k *Kernel) syncOrigins(ctx context.Context, q dialect.Querier, want *model.Schema, o operation, at time.Time, opts Options) error {
	if !k.repo.HasOrigins() {
		return nil
	}
	stored, err := k.repo.Origins(ctx, q)
	if err != nil {
		return err
	}
	for id := range stored {
		if want.Packages[id] == nil {
			if err := k.repo.DeleteOrigin(ctx, q, id); err != nil {
				return err
			}
		}
	}
	if o.origins == nil {
		return nil
	}
	for _, or := range o.origins() {
		if k.isKernelPackage(or.Package) {
			continue // kernel packages come with the kernel
		}
		or.InstalledAt, or.InstalledBy = at, opts.by()
		if old, ok := stored[or.Package]; ok && or.Kind == repo.OriginLocal && old.Kind == repo.OriginLocal {
			or.InstalledAt = old.InstalledAt
		}
		if err := k.repo.PutOrigin(ctx, q, or); err != nil {
			return err
		}
		if tag, ok := or.Tag.Get(); ok {
			if err := k.repo.PutSourceTag(ctx, q, repo.SourceTag{Address: or.Address.Or(""), Tag: tag, Kind: or.Kind, Commit: or.Commit.Or(""),
				SHA256: or.SHA256.Or(""), FirstSeen: at}); err != nil {
				return err
			}
		}
	}
	return nil
}

// localOrigins returns the origins of packages from directories or ZIP
// files.
func localOrigins(pkgs ...*model.Package) func() []repo.Origin {
	return func() []repo.Origin {
		var out []repo.Origin
		for _, p := range pkgs {
			out = append(out, repo.Origin{Package: p.Manifest.ID, Kind: repo.OriginLocal})
		}
		return out
	}
}

// PackageOrigin returns where an installed package comes from. Packages
// installed before origins were kept count as local.
func (k *Kernel) PackageOrigin(ctx context.Context, id model.PackageID) (*Origin, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	info := s.Packages[id]
	if info == nil {
		return nil, &rterr.NotInstalled{Package: string(id)}
	}
	origins, err := k.repo.Origins(ctx, k.db)
	if err != nil {
		return nil, err
	}
	if o, ok := origins[id]; ok {
		return &o, nil
	}
	return &Origin{Package: id, Kind: repo.OriginLocal, InstalledAt: info.InstalledAt, InstalledBy: "unknown"}, nil
}
