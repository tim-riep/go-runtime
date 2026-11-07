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
	"errors"
	"io"
	"maps"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// AssetRepositoryInfo describes an asset repository.
type AssetRepositoryInfo struct {
	Package     model.PackageID   `json:"package"`
	ID          string            `json:"id"`
	Description map[string]string `json:"description"` // label by language
	Files       int               `json:"files"`
	Folders     int               `json:"folders"`
	Size        int64             `json:"size"` // bytes of all contents, variants included
}

// AssetInfo describes a folder or an asset. For an asset read with a
// language it describes the version that was chosen. It holds everything an
// HTTP handler needs for Content-Type, Content-Length, ETag and
// Last-Modified.
type AssetInfo struct {
	Package     model.PackageID        `json:"package"`
	Repository  string                 `json:"repository"`
	Path        string                 `json:"path"`
	Folder      bool                   `json:"folder,omitempty"`
	MimeType    model.Optional[string] `json:"mimeType"` // None for folders
	Size        int64                  `json:"size"`
	SHA256      model.Optional[string] `json:"sha256"` // None for folders
	ModifiedAt  time.Time              `json:"modifiedAt"`
	Language    model.Optional[string] `json:"language"` // chosen variant, None for the language-independent version
	Source      model.Optional[string] `json:"source"`   // file in the package the content came from, None for folders
	Description map[string]string      `json:"description,omitempty"`
	Variants    []string               `json:"variants,omitempty"` // languages with a variant
	HasNeutral  bool                   `json:"hasNeutral,omitempty"`
}

// ETag returns a strong entity tag derived from the content hash.
// ETag returns "" for folders, which have no content.
func (i *AssetInfo) ETag() string {
	sha, ok := i.SHA256.Get()
	if !ok {
		return ""
	}
	return `"` + sha + `"`
}

// AssetRepositories lists the asset repositories of a package, or of all
// packages if pkg is empty, ordered by package and ID.
func (k *Kernel) AssetRepositories(ctx context.Context, pkg model.PackageID) ([]AssetRepositoryInfo, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	if pkg != "" && s.Packages[pkg] == nil {
		return nil, &rterr.NotInstalled{Package: string(pkg)}
	}
	out := []AssetRepositoryInfo{}
	for _, r := range s.SortedAssetRepositories() {
		if pkg != "" && r.Package != pkg {
			continue
		}
		info := AssetRepositoryInfo{Package: r.Package, ID: r.ID, Description: r.Description}
		for _, e := range r.Entries {
			if e.Folder {
				info.Folders++
				continue
			}
			info.Files++
			for _, c := range e.Contents() {
				info.Size += c.Size
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// assetRepository reads the metadata of a repository.
func (k *Kernel) assetRepository(ctx context.Context, pkg model.PackageID, id string) (*model.AssetRepository, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	r, err := k.repo.AssetRepository(ctx, k.db, pkg, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, &rterr.AssetNotFound{Package: string(pkg), Repository: id}
	}
	return r, nil
}

// chooseContent picks the version of an asset for a language: the
// requested language, then English, then the language-independent version.
// Without a language it prefers the language-independent version, then
// English.
func chooseContent(e *model.AssetEntry, lang model.Optional[string]) (*model.AssetContent, model.Optional[string]) {
	en := model.Some(model.DefaultLanguage)
	if l, ok := lang.Get(); ok {
		if v := e.Variants[l]; v != nil {
			return v, lang
		}
		if v := e.Variants[model.DefaultLanguage]; v != nil {
			return v, en
		}
		return e.Content, model.None[string]()
	}
	if e.Content != nil {
		return e.Content, model.None[string]()
	}
	if v := e.Variants[model.DefaultLanguage]; v != nil {
		return v, en
	}
	return nil, model.None[string]()
}

// assetInfo describes an entry; for assets in the version chosen for lang.
// It returns nil if the asset has no version for lang.
func assetInfo(r *model.AssetRepository, e *model.AssetEntry, lang model.Optional[string]) *AssetInfo {
	info := &AssetInfo{Package: r.Package, Repository: r.ID, Path: e.Path, Folder: e.Folder, MimeType: e.MimeType,
		Description: e.Description, Variants: e.Languages(), HasNeutral: e.Content != nil}
	if e.Folder {
		return info
	}
	c, l := chooseContent(e, lang)
	if c == nil {
		return nil
	}
	info.Size, info.SHA256, info.ModifiedAt, info.Language, info.Source = c.Size, model.Some(c.SHA256), c.ModifiedAt, l, model.Some(c.Source)
	return info
}

func notFound(pkg model.PackageID, repoID, path string, lang model.Optional[string]) error {
	return &rterr.AssetNotFound{Package: string(pkg), Repository: repoID, Path: path, Language: lang.Or("")}
}

// StatAsset returns the metadata of a folder or of the version of an asset
// chosen for lang.
// A lang of None asks for the language-independent version first.
func (k *Kernel) StatAsset(ctx context.Context, pkg model.PackageID, repoID, path string, lang model.Optional[string]) (*AssetInfo, error) {
	r, err := k.assetRepository(ctx, pkg, repoID)
	if err != nil {
		return nil, err
	}
	e := r.Entries[path]
	if e == nil {
		return nil, notFound(pkg, repoID, path, model.None[string]())
	}
	info := assetInfo(r, e, lang)
	if info == nil {
		return nil, notFound(pkg, repoID, path, lang)
	}
	return info, nil
}

// AssetExists reports whether a folder or asset exists.
func (k *Kernel) AssetExists(ctx context.Context, pkg model.PackageID, repoID, path string) (bool, error) {
	_, err := k.StatAsset(ctx, pkg, repoID, path, model.None[string]())
	var nf *rterr.AssetNotFound
	if errors.As(err, &nf) && nf.Path != "" {
		return false, nil
	}
	return err == nil, err
}

// ListAssets lists the entries of a folder, "" for the top level, ordered
// by path. Assets are described in their language-independent version, or
// in English if they have none.
func (k *Kernel) ListAssets(ctx context.Context, pkg model.PackageID, repoID, dir string) ([]AssetInfo, error) {
	r, err := k.assetRepository(ctx, pkg, repoID)
	if err != nil {
		return nil, err
	}
	if dir != "" {
		if e := r.Entries[dir]; e == nil || !e.Folder {
			return nil, notFound(pkg, repoID, dir, model.None[string]())
		}
	}
	return children(r, dir, model.None[string]()), nil
}

func children(r *model.AssetRepository, dir string, lang model.Optional[string]) []AssetInfo {
	out := []AssetInfo{}
	for _, p := range slices.Sorted(maps.Keys(r.Entries)) {
		if model.AssetParent(p) != dir {
			continue
		}
		if info := assetInfo(r, r.Entries[p], lang); info != nil {
			out = append(out, *info)
		}
	}
	return out
}

// OpenAsset opens the version of an asset chosen for lang for reading. The
// content is streamed from the database chunk by chunk.
func (k *Kernel) OpenAsset(ctx context.Context, pkg model.PackageID, repoID, path string, lang model.Optional[string]) (*AssetReader, error) {
	info, err := k.StatAsset(ctx, pkg, repoID, path, lang)
	if err != nil {
		return nil, err
	}
	if info.Folder {
		return nil, notFound(pkg, repoID, path, lang)
	}
	return &AssetReader{ctx: ctx, k: k, info: info, seq: -1}, nil
}

// AssetReader streams the content of an asset. It holds at most one chunk
// in memory and supports seeking, e.g. for range requests.
type AssetReader struct {
	ctx    context.Context
	k      *Kernel
	info   *AssetInfo
	pos    int64
	seq    int64 // number of the chunk in buf, starting at 0; -1 for none
	buf    []byte
	closed bool
}

// Info returns the metadata of the version being read.
func (r *AssetReader) Info() *AssetInfo { return r.info }

// Read implements io.Reader.
func (r *AssetReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("asset reader is closed")
	}
	if r.pos >= r.info.Size {
		return 0, io.EOF
	}
	seq := r.pos / repo.ChunkSize
	if seq != r.seq {
		data, err := r.k.repo.ReadChunk(r.ctx, r.k.db, r.info.SHA256.Or(""), seq+1)
		if err != nil {
			return 0, err
		}
		r.buf, r.seq = data, seq
	}
	off := r.pos - seq*repo.ChunkSize
	if off >= int64(len(r.buf)) {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, r.buf[off:])
	r.pos += int64(n)
	return n, nil
}

// Seek implements io.Seeker.
func (r *AssetReader) Seek(offset int64, whence int) (int64, error) {
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = r.pos + offset
	case io.SeekEnd:
		pos = r.info.Size + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if pos < 0 {
		return 0, errors.New("negative position")
	}
	r.pos = pos
	return pos, nil
}

// Close implements io.Closer.
func (r *AssetReader) Close() error {
	r.closed, r.buf = true, nil
	return nil
}
