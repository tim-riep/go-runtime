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

package model

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/rterr"
)

// Limits of asset repositories.
const (
	// MaxAssetRepositoryIDLength is the maximum length of a repository ID.
	MaxAssetRepositoryIDLength = 32
	// MaxAssetSegmentBytes is the maximum length of one path segment.
	MaxAssetSegmentBytes = 100
	// MaxAssetPathBytes is the maximum length of an asset path.
	MaxAssetPathBytes = 255
	// DefaultMaxAssetSize is the default maximum size of one asset.
	DefaultMaxAssetSize = 16 << 20
)

var (
	assetRepositoryIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	assetSegmentPattern      = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)
	mimeTypePattern          = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
)

// ValidAssetRepositoryID reports whether id is a valid repository ID:
// lower-case letters, digits, underscores and hyphens, starting with a
// letter.
func ValidAssetRepositoryID(id string) bool {
	return len(id) <= MaxAssetRepositoryIDLength && assetRepositoryIDPattern.MatchString(id)
}

// ValidMimeType reports whether t has the form type/subtype in lower case.
func ValidMimeType(t string) bool { return len(t) <= 127 && mimeTypePattern.MatchString(t) }

// CheckAssetPath returns why p is not a valid asset path, or "". Paths are
// segments separated by "/" without leading, trailing or double slashes.
// Segments consist of ASCII letters, digits, ".", "_" and "-" and do not
// start with "."; this excludes "." and "..", hidden files, spaces and
// control characters.
func CheckAssetPath(p string) string {
	switch {
	case p == "":
		return "the path is empty"
	case len(p) > MaxAssetPathBytes:
		return fmt.Sprintf("the path has %d bytes, the limit is %d", len(p), MaxAssetPathBytes)
	case strings.HasPrefix(p, "/"), strings.HasSuffix(p, "/"), strings.Contains(p, "//"):
		return "paths must not have leading, trailing or double slashes"
	}
	for _, seg := range strings.Split(p, "/") {
		switch {
		case len(seg) > MaxAssetSegmentBytes:
			return fmt.Sprintf("segment %q has %d bytes, the limit is %d", seg, len(seg), MaxAssetSegmentBytes)
		case strings.HasPrefix(seg, "."):
			return fmt.Sprintf("segment %q starts with a dot", seg)
		case !assetSegmentPattern.MatchString(seg):
			return fmt.Sprintf("segment %q may only contain ASCII letters, digits, '.', '_' and '-'", seg)
		}
	}
	return ""
}

// AssetParent returns the path of the folder containing p, or "" for
// entries on the top level.
func AssetParent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// AssetRepositoryKey identifies an asset repository: the package that
// brings it and the repository ID, which is its folder name.
type AssetRepositoryKey struct {
	Package PackageID
	ID      string
}

// AssetRepository is a folder of files a package brings.
type AssetRepository struct {
	Package     PackageID              `json:"package"`
	ID          string                 `json:"id"`
	Description map[string]string      `json:"description"` // label by language
	Entries     map[string]*AssetEntry `json:"entries"`     // by path
	Src         rterr.Location         `json:"-"`
}

// Key returns the identity of the repository.
func (r *AssetRepository) Key() AssetRepositoryKey {
	return AssetRepositoryKey{Package: r.Package, ID: r.ID}
}

// Paths returns the paths of all entries in sorted order.
func (r *AssetRepository) Paths() []string { return slices.Sorted(maps.Keys(r.Entries)) }

// AssetEntry is a folder or an asset of a repository.
type AssetEntry struct {
	Path        string            `json:"path"`
	Folder      bool              `json:"folder,omitempty"`
	Description map[string]string `json:"description,omitempty"` // by language
	MimeType    Optional[string]  `json:"mimeType"`              // None for folders
	// Content is the language-independent version. It is nil for folders
	// and for language-dependent assets without such a version.
	Content *AssetContent `json:"content,omitempty"`
	// Variants holds the versions per language of a language-dependent
	// asset.
	Variants map[string]*AssetContent `json:"variants,omitempty"`
	Src      rterr.Location           `json:"-"`
}

// Languages returns the languages of the variants in sorted order.
func (e *AssetEntry) Languages() []string { return slices.Sorted(maps.Keys(e.Variants)) }

// AssetContent describes the stored content of an asset or variant.
type AssetContent struct {
	// Source is the path of the file in the repository folder of the
	// package, e.g. "invoice/invoice_de.xsl" for a variant.
	Source     string    `json:"source"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"` // hex, lower case
	ModifiedAt time.Time `json:"modifiedAt"`
	// Open reads the content from the package. It is set only for
	// packages loaded from files.
	Open func() (io.ReadCloser, error) `json:"-"`
}

// Contents returns the language-independent content and the variants.
func (e *AssetEntry) Contents() []*AssetContent {
	var out []*AssetContent
	if e.Content != nil {
		out = append(out, e.Content)
	}
	for _, l := range e.Languages() {
		out = append(out, e.Variants[l])
	}
	return out
}

func cloneContent(c *AssetContent) *AssetContent {
	if c == nil {
		return nil
	}
	d := *c
	return &d
}

func cloneAssetRepository(r *AssetRepository) *AssetRepository {
	c := *r
	c.Description = maps.Clone(r.Description)
	c.Entries = make(map[string]*AssetEntry, len(r.Entries))
	for p, e := range r.Entries {
		ce := *e
		ce.Description = maps.Clone(e.Description)
		ce.Content = cloneContent(e.Content)
		if e.Variants != nil {
			ce.Variants = make(map[string]*AssetContent, len(e.Variants))
			for l, v := range e.Variants {
				ce.Variants[l] = cloneContent(v)
			}
		}
		c.Entries[p] = &ce
	}
	return &c
}

// SortedAssetRepositories returns all repositories ordered by package and
// ID.
func (s *Schema) SortedAssetRepositories() []*AssetRepository {
	out := make([]*AssetRepository, 0, len(s.AssetRepositories))
	for _, r := range s.AssetRepositories {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *AssetRepository) int {
		return cmp.Or(cmp.Compare(a.Package, b.Package), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// AssetHashes returns the hashes of all stored contents.
func (s *Schema) AssetHashes() map[string]bool {
	out := map[string]bool{}
	for _, r := range s.AssetRepositories {
		for _, e := range r.Entries {
			for _, c := range e.Contents() {
				out[c.SHA256] = true
			}
		}
	}
	return out
}
