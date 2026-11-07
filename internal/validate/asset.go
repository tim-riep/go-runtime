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

package validate

import (
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// assetRepositories checks names, paths, sizes and languages of all asset
// repositories.
func (v *validator) assetRepositories() {
	for _, r := range v.s.SortedAssetRepositories() {
		v.assetRepository(r)
	}
}

func (v *validator) assetRepository(r *model.AssetRepository) {
	obj := "asset repository " + r.ID + " of package " + string(r.Package)
	if !model.ValidAssetRepositoryID(r.ID) {
		v.fail(rterr.CodeAsset, r.Src, obj, "repository IDs (folder names below assets/) consist of up to %d lower-case letters, digits, '_' and '-' and start with a letter", model.MaxAssetRepositoryIDLength)
	}
	v.languages(obj, r.Src.At("/description"), slices.Sorted(maps.Keys(r.Description)))

	lower := map[string]string{}
	for _, p := range r.Paths() {
		e := r.Entries[p]
		eobj := "asset " + p + " in repository " + r.ID + " of package " + string(r.Package)
		if e.Folder {
			eobj = "folder " + p + " in repository " + r.ID + " of package " + string(r.Package)
		}
		if why := model.CheckAssetPath(p); why != "" {
			v.fail(rterr.CodeAsset, e.Src, eobj, "invalid path: %s", why)
		}
		if other, ok := lower[strings.ToLower(p)]; ok {
			v.fail(rterr.CodeAsset, e.Src, eobj, "the path differs from %s only in upper and lower case", other)
		}
		lower[strings.ToLower(p)] = p
		if parent := model.AssetParent(p); parent != "" {
			if pe, ok := r.Entries[parent]; !ok || !pe.Folder {
				v.fail(rterr.CodeAsset, e.Src, eobj, "parent folder %s does not exist", parent)
			}
		}
		if len(e.Description) > 0 {
			v.languages(eobj, e.Src, slices.Sorted(maps.Keys(e.Description)))
		}
		if e.Folder {
			if e.Content != nil || len(e.Variants) > 0 || e.MimeType.Valid() {
				v.fail(rterr.CodeAsset, e.Src, eobj, "folders have no content")
			}
			continue
		}
		if mt, ok := e.MimeType.Get(); !ok || !model.ValidMimeType(mt) {
			v.fail(rterr.CodeAsset, e.Src, eobj, "invalid media type %s", e.MimeType)
		}
		if e.Content == nil && len(e.Variants) == 0 {
			v.fail(rterr.CodeAsset, e.Src, eobj, "the asset has neither content nor language variants")
		}
		v.knownLanguages(eobj, e.Languages())
		for _, c := range e.Contents() {
			if v.opts.MaxAssetSize > 0 && c.Size > v.opts.MaxAssetSize {
				v.fail(rterr.CodeAsset, e.Src, eobj, "file %s has %d bytes, the limit is %d", c.Source, c.Size, v.opts.MaxAssetSize)
			}
			if len(c.SHA256) != 64 {
				v.fail(rterr.CodeAsset, e.Src, eobj, "invalid content hash of %s", c.Source)
			}
		}
	}
}
