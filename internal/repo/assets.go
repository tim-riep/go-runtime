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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Asset tables.
const (
	TAssetRepositories    = model.KernelNamespace + "ASSET_REPOSITORIES"
	TAssetRepositoryTexts = model.KernelNamespace + "ASSET_REPOSITORY_TEXTS"
	TAssetEntries         = model.KernelNamespace + "ASSET_ENTRIES"
	TAssetVariants        = model.KernelNamespace + "ASSET_VARIANTS"
	TAssetEntryTexts      = model.KernelNamespace + "ASSET_ENTRY_TEXTS"
	TAssetBlobs           = model.KernelNamespace + "ASSET_BLOBS"
	TAssetChunks          = model.KernelNamespace + "ASSET_CHUNKS"
)

// ChunkSize is the size of the parts in which contents are stored.
const ChunkSize = 256 << 10

// Entry kinds.
const (
	kindFolder = "FOLDER"
	kindFile   = "FILE"
)

func (r *Repo) loadAssets(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	return r.loadAssetsWhere(ctx, q, s, nil)
}

// AssetRepository reads the metadata of one asset repository, or returns
// nil if it does not exist.
func (r *Repo) AssetRepository(ctx context.Context, q dialect.Querier, pkg model.PackageID, id string) (*model.AssetRepository, error) {
	s := model.NewSchema()
	if err := r.loadAssetsWhere(ctx, q, s, row{"PACKAGE_ID": string(pkg), "REPOSITORY_ID": id}); err != nil {
		return nil, err
	}
	return s.AssetRepositories[model.AssetRepositoryKey{Package: pkg, ID: id}], nil
}

func (r *Repo) loadAssetsWhere(ctx context.Context, q dialect.Querier, s *model.Schema, filter row) error {
	key := func(rw row) model.AssetRepositoryKey {
		return model.AssetRepositoryKey{Package: model.PackageID(str(rw["PACKAGE_ID"])), ID: str(rw["REPOSITORY_ID"])}
	}
	repos, err := r.selectWhere(ctx, q, TAssetRepositories, []string{"PACKAGE_ID", "REPOSITORY_ID"}, filter)
	if err != nil {
		return err
	}
	for _, rw := range repos {
		k := key(rw)
		s.AssetRepositories[k] = &model.AssetRepository{Package: k.Package, ID: k.ID, Description: map[string]string{}, Entries: map[string]*model.AssetEntry{}}
	}
	texts, err := r.selectWhere(ctx, q, TAssetRepositoryTexts, []string{"PACKAGE_ID", "REPOSITORY_ID", "LANGUAGE", "DESCRIPTION"}, filter)
	if err != nil {
		return err
	}
	for _, rw := range texts {
		if ar := s.AssetRepositories[key(rw)]; ar != nil {
			ar.Description[str(rw["LANGUAGE"])] = str(rw["DESCRIPTION"])
		}
	}
	entries, err := r.selectWhere(ctx, q, TAssetEntries, []string{"PACKAGE_ID", "REPOSITORY_ID", "PATH", "KIND", "MIME_TYPE", "SHA256", "SIZE", "SOURCE_PATH", "MODIFIED_AT"}, filter)
	if err != nil {
		return err
	}
	for _, rw := range entries {
		ar := s.AssetRepositories[key(rw)]
		if ar == nil {
			return fmt.Errorf("repository: asset %s of unknown repository", str(rw["PATH"]))
		}
		e := &model.AssetEntry{Path: str(rw["PATH"]), Folder: str(rw["KIND"]) == kindFolder, MimeType: opt(rw["MIME_TYPE"])}
		if sha, ok := opt(rw["SHA256"]).Get(); ok {
			e.Content = &model.AssetContent{Source: str(rw["SOURCE_PATH"]), Size: int64(num(rw["SIZE"])), SHA256: sha, ModifiedAt: tm(rw["MODIFIED_AT"])}
		}
		ar.Entries[e.Path] = e
	}
	variants, err := r.selectWhere(ctx, q, TAssetVariants, []string{"PACKAGE_ID", "REPOSITORY_ID", "PATH", "LANGUAGE", "SOURCE_PATH", "SHA256", "SIZE", "MODIFIED_AT"}, filter)
	if err != nil {
		return err
	}
	for _, rw := range variants {
		if ar := s.AssetRepositories[key(rw)]; ar != nil {
			if e := ar.Entries[str(rw["PATH"])]; e != nil {
				if e.Variants == nil {
					e.Variants = map[string]*model.AssetContent{}
				}
				e.Variants[str(rw["LANGUAGE"])] = &model.AssetContent{Source: str(rw["SOURCE_PATH"]), Size: int64(num(rw["SIZE"])),
					SHA256: str(rw["SHA256"]), ModifiedAt: tm(rw["MODIFIED_AT"])}
			}
		}
	}
	etexts, err := r.selectWhere(ctx, q, TAssetEntryTexts, []string{"PACKAGE_ID", "REPOSITORY_ID", "PATH", "LANGUAGE", "DESCRIPTION"}, filter)
	if err != nil {
		return err
	}
	for _, rw := range etexts {
		if ar := s.AssetRepositories[key(rw)]; ar != nil {
			if e := ar.Entries[str(rw["PATH"])]; e != nil {
				if e.Description == nil {
					e.Description = map[string]string{}
				}
				e.Description[str(rw["LANGUAGE"])] = str(rw["DESCRIPTION"])
			}
		}
	}
	return nil
}

// writeAssets stores the metadata of all asset repositories. Contents must
// have been stored with StoreContent before. Paths are written in sorted
// order, so every folder comes before its entries.
func (r *Repo) writeAssets(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, ar := range s.SortedAssetRepositories() {
		k := row{"PACKAGE_ID": string(ar.Package), "REPOSITORY_ID": ar.ID}
		if err := r.insert(ctx, q, TAssetRepositories, k); err != nil {
			return err
		}
		for _, lang := range slices.Sorted(maps.Keys(ar.Description)) {
			if err := r.insert(ctx, q, TAssetRepositoryTexts, with(k, row{"LANGUAGE": lang, "DESCRIPTION": ar.Description[lang]})); err != nil {
				return err
			}
		}
		for _, p := range ar.Paths() {
			e := ar.Entries[p]
			ek := with(k, row{"PATH": p})
			var parent any // top-level entries have no parent
			if pp := model.AssetParent(p); pp != "" {
				parent = pp
			}
			vals := row{"KIND": kindFile, "PARENT_PATH": parent, "MIME_TYPE": val(e.MimeType)}
			if e.Folder {
				vals["KIND"] = kindFolder
			}
			if c := e.Content; c != nil {
				vals["SHA256"], vals["SIZE"], vals["SOURCE_PATH"], vals["MODIFIED_AT"] = c.SHA256, c.Size, c.Source, c.ModifiedAt
			}
			if err := r.insert(ctx, q, TAssetEntries, with(ek, vals)); err != nil {
				return err
			}
			for _, lang := range e.Languages() {
				c := e.Variants[lang]
				if err := r.insert(ctx, q, TAssetVariants, with(ek, row{"LANGUAGE": lang, "SOURCE_PATH": c.Source, "SHA256": c.SHA256,
					"SIZE": c.Size, "MODIFIED_AT": c.ModifiedAt})); err != nil {
					return err
				}
			}
			for _, lang := range slices.Sorted(maps.Keys(e.Description)) {
				if err := r.insert(ctx, q, TAssetEntryTexts, with(ek, row{"LANGUAGE": lang, "DESCRIPTION": e.Description[lang]})); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func with(a, b row) row {
	out := maps.Clone(a)
	maps.Copy(out, b)
	return out
}

// HasAssets reports whether the version has asset repositories.
func (r *Repo) HasAssets() bool { return r.has(TAssetBlobs, "") }

// HasContent reports whether a content is stored.
func (r *Repo) HasContent(ctx context.Context, q dialect.Querier, sha string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+r.d.QuoteIdent(TAssetBlobs)+" WHERE "+r.d.QuoteIdent("SHA256")+" = "+r.d.Placeholder(1), sha).Scan(&n)
	return n > 0, err
}

// ErrContentChanged reports that a file changed between loading the
// package and storing its content.
var ErrContentChanged = errors.New("the file changed after the package was loaded")

// StoreContent stores a content in chunks unless it is already stored. It
// streams the content and holds at most one chunk in memory.
func (r *Repo) StoreContent(ctx context.Context, q dialect.Querier, c *model.AssetContent) error {
	ok, err := r.HasContent(ctx, q, c.SHA256)
	if err != nil || ok {
		return err
	}
	if c.Open == nil {
		return fmt.Errorf("content %s of %s is not stored and cannot be read from the package", c.SHA256, c.Source)
	}
	chunks := (c.Size + ChunkSize - 1) / ChunkSize
	if err := r.insert(ctx, q, TAssetBlobs, row{"SHA256": c.SHA256, "SIZE": c.Size, "CHUNKS": chunks}); err != nil {
		return err
	}
	f, err := c.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, ChunkSize)
	var total int64
	for seq := int64(1); ; seq++ {
		n, err := io.ReadFull(f, buf)
		if n > 0 {
			h.Write(buf[:n])
			total += int64(n)
			if ierr := r.insert(ctx, q, TAssetChunks, row{"SHA256": c.SHA256, "SEQ": seq, "DATA": buf[:n]}); ierr != nil {
				return ierr
			}
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if total != c.Size || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return fmt.Errorf("%s: %w", c.Source, ErrContentChanged)
	}
	return nil
}

// DeleteUnusedContents removes stored contents that no asset or variant
// references.
func (r *Repo) DeleteUnusedContents(ctx context.Context, q dialect.Querier) error {
	if !r.HasAssets() {
		return nil
	}
	sha := r.d.QuoteIdent("SHA256")
	used := "SELECT " + sha + " FROM " + r.d.QuoteIdent(TAssetEntries) + " WHERE " + sha + " IS NOT NULL UNION SELECT " + sha + " FROM " + r.d.QuoteIdent(TAssetVariants)
	for _, t := range []string{TAssetChunks, TAssetBlobs} {
		if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(t)+" WHERE "+sha+" NOT IN ("+used+")"); err != nil {
			return fmt.Errorf("removing unused contents: %w", err)
		}
	}
	return nil
}

// AssetRepositoriesOf reads the asset repositories of a package, ordered
// by ID.
func (r *Repo) AssetRepositoriesOf(ctx context.Context, q dialect.Querier, pkg model.PackageID) ([]*model.AssetRepository, error) {
	s := model.NewSchema()
	if err := r.loadAssetsWhere(ctx, q, s, row{"PACKAGE_ID": string(pkg)}); err != nil {
		return nil, err
	}
	return s.SortedAssetRepositories(), nil
}

// ReadChunk returns one part of a stored content, numbered from 1.
func (r *Repo) ReadChunk(ctx context.Context, q dialect.Querier, sha string, seq int64) ([]byte, error) {
	var data []byte
	err := q.QueryRowContext(ctx, "SELECT "+r.d.QuoteIdent("DATA")+" FROM "+r.d.QuoteIdent(TAssetChunks)+" WHERE "+
		r.d.QuoteIdent("SHA256")+" = "+r.d.Placeholder(1)+" AND "+r.d.QuoteIdent("SEQ")+" = "+r.d.Placeholder(2), sha, seq).Scan(&data)
	return data, err
}

// ContentProblem is a stored content that does not match its hash, size
// or number of chunks, or a referenced content that is missing.
type ContentProblem struct {
	SHA256  string
	Problem string
}

// VerifyContents reads every stored content in chunks and compares it with
// its hash and size.
func (r *Repo) VerifyContents(ctx context.Context, q dialect.Querier) ([]ContentProblem, error) {
	if !r.HasAssets() {
		return nil, nil
	}
	blobs, err := r.selectRows(ctx, q, TAssetBlobs, []string{"SHA256", "SIZE", "CHUNKS"}, "SHA256")
	if err != nil {
		return nil, err
	}
	var out []ContentProblem
	stored := map[string]bool{}
	for _, b := range blobs {
		sha := str(b["SHA256"])
		stored[sha] = true
		h := sha256.New()
		var size int64
		seq := int64(0)
		for ; seq < int64(num(b["CHUNKS"])); seq++ {
			data, err := r.ReadChunk(ctx, q, sha, seq+1)
			if err != nil {
				out = append(out, ContentProblem{sha, fmt.Sprintf("chunk %d cannot be read: %v", seq+1, err)})
				break
			}
			h.Write(data)
			size += int64(len(data))
		}
		var chunks int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+r.d.QuoteIdent(TAssetChunks)+" WHERE "+r.d.QuoteIdent("SHA256")+" = "+r.d.Placeholder(1), sha).Scan(&chunks); err != nil {
			return nil, err
		}
		switch {
		case chunks != num(b["CHUNKS"]):
			out = append(out, ContentProblem{sha, fmt.Sprintf("has %d chunks, expected %d", chunks, num(b["CHUNKS"]))})
		case size != int64(num(b["SIZE"])):
			out = append(out, ContentProblem{sha, fmt.Sprintf("has %d bytes, expected %d", size, num(b["SIZE"]))})
		case hex.EncodeToString(h.Sum(nil)) != sha:
			out = append(out, ContentProblem{sha, "content does not match its SHA-256"})
		}
	}
	s := model.NewSchema()
	if err := r.loadAssets(ctx, q, s); err != nil {
		return nil, err
	}
	for sha := range s.AssetHashes() {
		if !stored[sha] {
			out = append(out, ContentProblem{sha, "referenced content is not stored"})
		}
	}
	for sha := range stored {
		if !s.AssetHashes()[sha] {
			out = append(out, ContentProblem{sha, "stored content is not referenced"})
		}
	}
	slices.SortFunc(out, func(a, b ContentProblem) int { return strings.Compare(a.SHA256+a.Problem, b.SHA256+b.Problem) })
	return out, nil
}
