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

package natives

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// assetRepository is the Go value of /TRBASE/ASSET_REPOSITORY.
type assetRepository struct {
	src native.AssetSource
	r   *model.AssetRepository
}

const repoClass = "/TRBASE/ASSET_REPOSITORY"

// The exceptions of asset repositories.
const (
	errRepoNotFound    = "/TRBASE/ASSET_REPOSITORY_NOT_FOUND"
	errAccessDenied    = "/TRBASE/ASSET_ACCESS_DENIED"
	errAssetNotFound   = "/TRBASE/ASSET_NOT_FOUND"
	errPathInvalid     = "/TRBASE/ASSET_PATH_INVALID"
	errTooLarge        = "/TRBASE/ASSET_TOO_LARGE"
	errEncodingInvalid = "/TRBASE/ASSET_ENCODING_INVALID"
)

func init() {
	native.Default.RegisterClass(pkg, repoClass, (*assetRepository)(nil))
	static(repoClass, "OPEN", "(pkg: STRING, repository: STRING) returns /TRBASE/ASSET_REPOSITORY raises "+errRepoNotFound+", "+errAccessDenied,
		func(c *native.Context, owner, id string) (*assetRepository, error) {
			src, err := c.AssetRepository(model.PackageID(owner), id)
			var denied *native.AssetAccessDenied
			switch {
			case errors.As(err, &denied):
				return nil, native.Raise(errAccessDenied, string(denied.Reader), owner, id)
			case errors.Is(err, native.ErrAssetRepositoryNotFound):
				return nil, native.Raise(errRepoNotFound, owner, id)
			case err != nil:
				return nil, err
			}
			return &assetRepository{src: src, r: src.Repository()}, nil
		})
	static(repoClass, "LIST_REPOSITORIES", "(pkg: STRING) returns /TRBASE/ASSET_REPOSITORY_ARRAY raises "+errAccessDenied,
		func(c *native.Context, owner string) ([]AssetRepositoryInfo, error) {
			if c.Host == nil {
				return nil, errors.New("natives: assets need the kernel")
			}
			list, err := c.Host.AssetRepositories(c.Reader(), model.PackageID(owner))
			var denied *native.AssetAccessDenied
			if errors.As(err, &denied) {
				return nil, native.Raise(errAccessDenied, string(denied.Reader), owner, "")
			}
			if err != nil {
				return nil, err
			}
			out := make([]AssetRepositoryInfo, 0, len(list))
			for _, r := range list {
				label, _ := text(r.Description, c.Language())
				out = append(out, AssetRepositoryInfo{PackageID: string(r.Package), RepositoryID: r.ID, Label: label.Or("")})
			}
			return out, nil
		})

	method := func(name, sig string, fn any) {
		native.Default.Register(native.Method{Package: pkg, Class: repoClass, Method: name, Signature: sig, Func: fn})
	}
	notFound, invalid := errAssetNotFound, errPathInvalid
	method("PACKAGE_ID", "() returns STRING", func(c *native.Context, a *assetRepository) (string, error) {
		return string(a.r.Package), nil
	})
	method("ID", "() returns STRING", func(c *native.Context, a *assetRepository) (string, error) { return a.r.ID, nil })
	method("EXISTS", "(path: STRING) returns BOOL raises "+invalid, func(c *native.Context, a *assetRepository, p string) (bool, error) {
		if err := a.checkPath(p); err != nil {
			return false, err
		}
		return p == "" || a.r.Entries[p] != nil, nil
	})
	method("INFO", "(path: STRING) returns /TRBASE/ASSET_INFO raises "+notFound+", "+invalid, func(c *native.Context, a *assetRepository, p string) (AssetInfo, error) {
		if err := a.checkPath(p); err != nil {
			return AssetInfo{}, err
		}
		if p == "" {
			return folderInfo(""), nil
		}
		e := a.r.Entries[p]
		if e == nil {
			return AssetInfo{}, a.notFound(p)
		}
		return entryInfo(e, c.Language()), nil
	})
	list := func(tree bool) func(c *native.Context, a *assetRepository, p string) ([]AssetInfo, error) {
		return func(c *native.Context, a *assetRepository, p string) ([]AssetInfo, error) {
			if err := a.checkPath(p); err != nil {
				return nil, err
			}
			if e := a.r.Entries[p]; p != "" && (e == nil || !e.Folder) {
				return nil, native.Raise(notFound, string(a.r.Package), a.r.ID, p)
			}
			out := []AssetInfo{}
			for _, q := range slices.Sorted(maps.Keys(a.r.Entries)) {
				if tree && (p == "" || strings.HasPrefix(q, p+"/")) || !tree && model.AssetParent(q) == p {
					out = append(out, entryInfo(a.r.Entries[q], c.Language()))
				}
			}
			return out, nil
		}
	}
	method("LIST", "(path: STRING) returns /TRBASE/ASSET_ENTRY_ARRAY raises "+notFound+", "+invalid, list(false))
	method("LIST_TREE", "(path: STRING) returns /TRBASE/ASSET_ENTRY_ARRAY raises "+notFound+", "+invalid, list(true))
	getRaises := " raises " + notFound + ", " + invalid + ", " + errTooLarge
	method("GET", "(path: STRING) returns BYTES"+getRaises, func(c *native.Context, a *assetRepository, p string) ([]byte, error) {
		return a.get(c, p, c.Language())
	})
	method("GET_IN_LANGUAGE", "(path: STRING, language: STRING) returns BYTES"+getRaises, func(c *native.Context, a *assetRepository, p, lang string) ([]byte, error) {
		return a.get(c, p, lang)
	})
	method("GET_TEXT", "(path: STRING) returns STRING"+getRaises+", "+errEncodingInvalid, func(c *native.Context, a *assetRepository, p string) (string, error) {
		b, err := a.get(c, p, c.Language())
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", native.Raise(errEncodingInvalid, string(a.r.Package), a.r.ID, p)
		}
		return string(b), nil
	})
}

// checkPath raises /TRBASE/ASSET_PATH_INVALID for a path that breaks the
// rules of asset repositories; "" is the top level.
func (a *assetRepository) checkPath(p string) error {
	if p == "" {
		return nil
	}
	if why := model.CheckAssetPath(p); why != "" {
		return native.Raise(errPathInvalid, string(a.r.Package), a.r.ID, p, why)
	}
	return nil
}

func (a *assetRepository) notFound(p string) error {
	return native.Raise(errAssetNotFound, string(a.r.Package), a.r.ID, p)
}

// asset returns the version of an asset for a language.
func (a *assetRepository) asset(p, lang string) (*model.AssetContent, error) {
	if err := a.checkPath(p); err != nil {
		return nil, err
	}
	e := a.r.Entries[p]
	if e == nil || e.Folder {
		return nil, a.notFound(p)
	}
	content, _ := model.ChooseAssetContent(e, model.Some(lang))
	if content == nil {
		return nil, a.notFound(p)
	}
	return content, nil
}

// get loads the version of an asset for a language into memory.
func (a *assetRepository) get(c *native.Context, p, lang string) ([]byte, error) {
	content, err := a.asset(p, lang)
	if err != nil {
		return nil, err
	}
	if max := a.src.MaxRead(); content.Size > max {
		return nil, native.Raise(errTooLarge, string(a.r.Package), a.r.ID, p, content.Size, max)
	}
	return a.src.Read(c, content)
}

func folderInfo(p string) AssetInfo {
	return AssetInfo{Path: p, IsFolder: true, Languages: []string{}}
}

// entryInfo describes a folder, or an asset in the version GET returns
// for the language. The version fields are null for an asset without a
// version in the language.
func entryInfo(e *model.AssetEntry, lang string) AssetInfo {
	info := folderInfo(e.Path)
	info.IsFolder = e.Folder
	info.Description, _ = text(e.Description, lang)
	if e.Folder {
		return info
	}
	info.MimeType = e.MimeType
	info.Languages = e.Languages()
	info.HasNeutral = e.Content != nil
	if c, l := model.ChooseAssetContent(e, model.Some(lang)); c != nil {
		info.Size = c.Size
		info.Sha256 = model.Some(c.SHA256)
		info.Etag = model.Some(`"` + c.SHA256 + `"`)
		info.ModifiedAt = model.Some(c.ModifiedAt.UTC())
		info.Language = l
	}
	return info
}

// text returns the text in a language, else in English; "" is a text.
func text(texts map[string]string, lang string) (model.Optional[string], bool) {
	if t, ok := texts[lang]; ok {
		return model.Some(t), true
	}
	if t, ok := texts[model.DefaultLanguage]; ok {
		return model.Some(t), true
	}
	return model.None[string](), false
}
