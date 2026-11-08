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

package repo

import (
	"context"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of the origins of packages.
const (
	TPackageOrigins = model.KernelNamespace + "PACKAGE_ORIGINS"
	TSourceTags     = model.KernelNamespace + "SOURCE_TAGS"
)

// Kinds of origins.
const (
	OriginAsset   = "asset"
	OriginArchive = "archive"
	OriginLocal   = "local"
)

// Origin is where an installed package comes from.
type Origin struct {
	Package     model.PackageID        `json:"package"`
	Kind        string                 `json:"kind"`
	Address     model.Optional[string] `json:"address"`
	Requested   model.Optional[string] `json:"requested"`
	Constraint  model.Optional[string] `json:"constraint"`
	Tag         model.Optional[string] `json:"tag"`
	Commit      model.Optional[string] `json:"commit"`
	Asset       model.Optional[string] `json:"asset"`
	SHA256      model.Optional[string] `json:"sha256"`
	InstalledAt time.Time              `json:"installedAt"`
	InstalledBy string                 `json:"installedBy"`
}

// HasOrigins reports a kernel version with origins of packages.
func (r *Repo) HasOrigins() bool { return r.has(TPackageOrigins, "") }

var originColumns = []string{"PACKAGE_ID", "KIND", "ADDRESS", "REQUESTED", "CONSTRAINT_TEXT", "TAG", "COMMIT_SHA", "ASSET_NAME", "ARCHIVE_SHA256", "INSTALLED_AT", "INSTALLED_BY"}

// Origins returns the origins of the installed packages by ID.
func (r *Repo) Origins(ctx context.Context, q dialect.Querier) (map[model.PackageID]Origin, error) {
	rows, err := r.selectRows(ctx, q, TPackageOrigins, originColumns, "PACKAGE_ID")
	if err != nil {
		return nil, err
	}
	out := map[model.PackageID]Origin{}
	for _, x := range rows {
		o := Origin{Package: model.PackageID(str(x["PACKAGE_ID"])), Kind: str(x["KIND"]), Address: opt(x["ADDRESS"]), Requested: opt(x["REQUESTED"]),
			Constraint: opt(x["CONSTRAINT_TEXT"]), Tag: opt(x["TAG"]), Commit: opt(x["COMMIT_SHA"]), Asset: opt(x["ASSET_NAME"]), SHA256: opt(x["ARCHIVE_SHA256"]),
			InstalledAt: tm(x["INSTALLED_AT"]), InstalledBy: str(x["INSTALLED_BY"])}
		out[o.Package] = o
	}
	return out, nil
}

// PutOrigin replaces the origin of a package.
func (r *Repo) PutOrigin(ctx context.Context, q dialect.Querier, o Origin) error {
	if !r.HasOrigins() {
		return nil
	}
	if err := r.DeleteOrigin(ctx, q, o.Package); err != nil {
		return err
	}
	return r.insert(ctx, q, TPackageOrigins, row{"PACKAGE_ID": string(o.Package), "KIND": o.Kind, "ADDRESS": val(o.Address), "REQUESTED": val(o.Requested),
		"CONSTRAINT_TEXT": val(o.Constraint), "TAG": val(o.Tag), "COMMIT_SHA": val(o.Commit), "ASSET_NAME": val(o.Asset), "ARCHIVE_SHA256": val(o.SHA256),
		"INSTALLED_AT": o.InstalledAt, "INSTALLED_BY": o.InstalledBy})
}

// DeleteOrigin removes the origin of a package.
func (r *Repo) DeleteOrigin(ctx context.Context, q dialect.Querier, id model.PackageID) error {
	if !r.HasOrigins() {
		return nil
	}
	return r.deleteWhere(ctx, q, TPackageOrigins, row{"PACKAGE_ID": string(id)})
}

// SourceTag is a commit and archive hash seen for a tag.
type SourceTag struct {
	Address   string
	Tag       string
	Kind      string
	Commit    string
	SHA256    string
	FirstSeen time.Time
}

// SourceTagOf returns what was seen for a tag, nil if nothing.
func (r *Repo) SourceTagOf(ctx context.Context, q dialect.Querier, address, tag, kind string) (*SourceTag, error) {
	rows, err := r.selectWhere(ctx, q, TSourceTags, []string{"ADDRESS", "TAG", "KIND", "COMMIT_SHA", "ARCHIVE_SHA256", "FIRST_SEEN_AT"},
		row{"ADDRESS": address, "TAG": tag, "KIND": kind})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	x := rows[0]
	return &SourceTag{Address: str(x["ADDRESS"]), Tag: str(x["TAG"]), Kind: str(x["KIND"]), Commit: str(x["COMMIT_SHA"]), SHA256: str(x["ARCHIVE_SHA256"]),
		FirstSeen: tm(x["FIRST_SEEN_AT"])}, nil
}

// PutSourceTag records what was seen for a tag, replacing an earlier
// record.
func (r *Repo) PutSourceTag(ctx context.Context, q dialect.Querier, t SourceTag) error {
	if !r.HasOrigins() {
		return nil
	}
	if err := r.deleteWhere(ctx, q, TSourceTags, row{"ADDRESS": t.Address, "TAG": t.Tag, "KIND": t.Kind}); err != nil {
		return err
	}
	return r.insert(ctx, q, TSourceTags, row{"ADDRESS": t.Address, "TAG": t.Tag, "KIND": t.Kind, "COMMIT_SHA": t.Commit, "ARCHIVE_SHA256": t.SHA256,
		"FIRST_SEEN_AT": t.FirstSeen})
}
