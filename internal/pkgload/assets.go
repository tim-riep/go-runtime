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

package pkgload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/internal/mimetype"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Paths of asset repositories.
const (
	AssetsDir       = "assets"
	AssetDescriptor = "asset.json"
)

type assetDoc struct {
	Description map[string]string `json:"description"`
	Folders     map[string]struct {
		Description map[string]string `json:"description"`
	} `json:"folders"`
	Files map[string]struct {
		MimeType    string            `json:"mimeType"`
		Description map[string]string `json:"description"`
		Languages   map[string]string `json:"languages"`
	} `json:"files"`
}

// checkAssetLayout rejects folders below assets/ without asset.json.
func checkAssetLayout(l *loader) {
	entries, err := fs.ReadDir(l.fsys, AssetsDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.fail(rterr.CodeFile, l.loc(AssetsDir), "cannot read directory: %v", err)
		}
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue // reported as file directly below assets/
		}
		dir := path.Join(AssetsDir, e.Name())
		if _, err := fs.Stat(l.fsys, path.Join(dir, AssetDescriptor)); err != nil {
			l.fail(rterr.CodeAsset, l.loc(dir), "asset repository %s has no %s", e.Name(), AssetDescriptor)
		}
	}
}

func (l *loader) assetRepository(id, dir string, doc *assetDoc) *model.AssetRepository {
	descFile := path.Join(dir, AssetDescriptor)
	repo := &model.AssetRepository{Package: model.PackageID(l.id), ID: id, Description: doc.Description,
		Entries: map[string]*model.AssetEntry{}, Src: l.loc(descFile)}
	entryLoc := func(p string) rterr.Location { return l.loc(path.Join(dir, p)) }

	// Files and folders on disk.
	files := map[string]*model.AssetContent{}
	var folders []string
	_ = fs.WalkDir(l.fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			l.fail(rterr.CodeFile, l.loc(p), "cannot read: %v", err)
			return nil
		}
		if p == dir || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel := strings.TrimPrefix(p, dir+"/")
		switch {
		case d.IsDir():
			folders = append(folders, rel)
		case rel == AssetDescriptor:
		default:
			c, err := l.assetContent(p, rel)
			if err != nil {
				l.fail(rterr.CodeFile, l.loc(p), "cannot read: %v", err)
				return nil
			}
			files[rel] = c
		}
		return nil
	})

	addFolder := func(p string, loc rterr.Location) {
		for ; p != ""; p = model.AssetParent(p) {
			if _, ok := repo.Entries[p]; !ok {
				repo.Entries[p] = &model.AssetEntry{Path: p, Folder: true, Src: loc}
			}
		}
	}
	for _, f := range folders {
		addFolder(f, entryLoc(f))
	}
	for p, f := range doc.Folders {
		loc := repo.Src.At("/folders/" + pointerEscape(p))
		if _, isFile := files[p]; isFile {
			l.fail(rterr.CodeAsset, loc, "%s is a file, not a folder", p)
			continue
		}
		addFolder(p, loc)
		repo.Entries[p].Description = f.Description
		repo.Entries[p].Src = loc
	}

	// Language variants: their files are reachable only through the
	// logical path.
	used := map[string]string{} // file -> logical path that uses it
	for _, logical := range slices.Sorted(maps.Keys(doc.Files)) {
		info := doc.Files[logical]
		for _, lang := range slices.Sorted(maps.Keys(info.Languages)) {
			src := info.Languages[lang]
			loc := repo.Src.At("/files/" + pointerEscape(logical) + "/languages/" + lang)
			switch {
			case src == logical:
				l.fail(rterr.CodeAsset, loc, "variant %s of %s must be a different file", lang, logical)
			case files[src] == nil:
				l.fail(rterr.CodeAsset, loc, "variant %s of %s: file %s does not exist", lang, logical, src)
			case used[src] != "":
				l.fail(rterr.CodeAsset, loc, "file %s is already a variant of %s", src, used[src])
			case doc.Files[src].Languages != nil:
				l.fail(rterr.CodeAsset, loc, "file %s is itself language-dependent", src)
			default:
				used[src] = logical
			}
		}
	}
	for p, c := range files {
		if used[p] != "" {
			continue
		}
		repo.Entries[p] = &model.AssetEntry{Path: p, Content: c, Src: entryLoc(p)}
	}
	for logical, info := range doc.Files {
		loc := repo.Src.At("/files/" + pointerEscape(logical))
		e := repo.Entries[logical]
		switch {
		case used[logical] != "":
			l.fail(rterr.CodeAsset, loc, "file %s is a variant of %s and has no own entry", logical, used[logical])
			continue
		case e != nil && e.Folder:
			l.fail(rterr.CodeAsset, loc, "%s is a folder, not a file", logical)
			continue
		case e == nil && len(info.Languages) == 0:
			l.fail(rterr.CodeAsset, loc, "file %s does not exist", logical)
			continue
		case e == nil:
			e = &model.AssetEntry{Path: logical, Src: loc}
			repo.Entries[logical] = e
		}
		e.Description = info.Description
		e.MimeType = info.MimeType
		for lang, src := range info.Languages {
			if used[src] == logical {
				if e.Variants == nil {
					e.Variants = map[string]*model.AssetContent{}
				}
				e.Variants[lang] = files[src]
			}
		}
	}
	for _, e := range repo.Entries {
		if e.Folder {
			continue
		}
		if e.MimeType == "" {
			e.MimeType = mimetype.ForPath(e.Path)
		}
		if p := model.AssetParent(e.Path); p != "" {
			addFolder(p, e.Src)
		}
	}
	return repo
}

// assetContent reads a file once to determine its size and hash. The
// content is read again when it is stored.
func (l *loader) assetContent(p, rel string) (*model.AssetContent, error) {
	f, err := l.fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	fsys := l.fsys
	return &model.AssetContent{Source: rel, Size: n, SHA256: hex.EncodeToString(h.Sum(nil)),
		Open: func() (io.ReadCloser, error) { return fsys.Open(p) }}, nil
}

func pointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
