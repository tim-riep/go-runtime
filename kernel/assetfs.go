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
	"io"
	"io/fs"
	"path"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// AssetFS returns an asset repository as file system, e.g. for
// html/template.ParseFS or http.FileServer. Every file is read in the
// version chosen for lang (requested language, then English, then the
// language-independent version; without a language the language-independent
// version first). Language-dependent assets appear under their logical
// path; their variant files do not appear. Assets without a version for
// lang are left out.
//
// The metadata of the repository is read once when the file system is
// created; contents are streamed from the database when files are read.
func (k *Kernel) AssetFS(ctx context.Context, pkg model.PackageID, repoID string, lang model.Optional[string]) (fs.FS, error) {
	r, err := k.assetRepository(ctx, pkg, repoID)
	if err != nil {
		return nil, err
	}
	return &assetFS{ctx: ctx, k: k, repo: r, lang: lang}, nil
}

type assetFS struct {
	ctx  context.Context
	k    *Kernel
	repo *model.AssetRepository
	lang model.Optional[string]
}

// WithContext returns the file system reading with another context, for
// reads after the execution that opened it has ended.
func (f *assetFS) WithContext(ctx context.Context) fs.FS {
	c := *f
	c.ctx = ctx
	return &c
}

var (
	_ fs.ReadDirFS  = (*assetFS)(nil)
	_ fs.StatFS     = (*assetFS)(nil)
	_ fs.ReadFileFS = (*assetFS)(nil)
)

// stat returns the entry for a name, "." being the root folder.
func (f *assetFS) stat(op, name string) (*AssetInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return &AssetInfo{Package: f.repo.Package, Repository: f.repo.ID, Folder: true}, nil
	}
	e := f.repo.Entries[name]
	if e == nil {
		return nil, &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	info := assetInfo(f.repo, e, f.lang)
	if info == nil {
		return nil, &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	return info, nil
}

func (f *assetFS) Open(name string) (fs.File, error) {
	info, err := f.stat("open", name)
	if err != nil {
		return nil, err
	}
	if info.Folder {
		return &assetDir{info: info, fsys: f, name: name}, nil
	}
	return &assetFile{AssetReader: &AssetReader{ctx: f.ctx, k: f.k, info: info, seq: -1}}, nil
}

func (f *assetFS) Stat(name string) (fs.FileInfo, error) {
	info, err := f.stat("stat", name)
	if err != nil {
		return nil, err
	}
	return fileInfo{info}, nil
}

func (f *assetFS) ReadDir(name string) ([]fs.DirEntry, error) {
	info, err := f.stat("readdir", name)
	if err != nil {
		return nil, err
	}
	if !info.Folder {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	dir := name
	if dir == "." {
		dir = ""
	}
	var out []fs.DirEntry
	for _, c := range children(f.repo, dir, f.lang) {
		out = append(out, fs.FileInfoToDirEntry(fileInfo{&c}))
	}
	return out, nil
}

func (f *assetFS) ReadFile(name string) ([]byte, error) {
	file, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if st, _ := file.Stat(); st.IsDir() {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	return io.ReadAll(file)
}

// fileInfo adapts AssetInfo to fs.FileInfo.
type fileInfo struct{ i *AssetInfo }

func (fi fileInfo) Name() string {
	if fi.i.Path == "" {
		return "."
	}
	return path.Base(fi.i.Path)
}
func (fi fileInfo) Size() int64        { return fi.i.Size }
func (fi fileInfo) ModTime() time.Time { return fi.i.ModifiedAt }
func (fi fileInfo) IsDir() bool        { return fi.i.Folder }
func (fi fileInfo) Sys() any           { return fi.i }
func (fi fileInfo) Mode() fs.FileMode {
	if fi.i.Folder {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

// assetFile is a file of an assetFS. It can seek, so http.FileServer serves
// range requests from it.
type assetFile struct{ *AssetReader }

func (f *assetFile) Stat() (fs.FileInfo, error) { return fileInfo{f.info}, nil }

// assetDir is a folder of an assetFS.
type assetDir struct {
	info    *AssetInfo
	fsys    *assetFS
	name    string
	entries []fs.DirEntry
	read    bool
}

func (d *assetDir) Stat() (fs.FileInfo, error) { return fileInfo{d.info}, nil }
func (d *assetDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}
func (d *assetDir) Close() error { return nil }

// ReadDir implements fs.ReadDirFile.
func (d *assetDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if !d.read {
		entries, err := d.fsys.ReadDir(d.name)
		if err != nil {
			return nil, err
		}
		d.entries, d.read = entries, true
	}
	if n <= 0 {
		out := d.entries
		d.entries = nil
		return out, nil
	}
	if len(d.entries) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(d.entries))
	out := d.entries[:n]
	d.entries = d.entries[n:]
	return out, nil
}
