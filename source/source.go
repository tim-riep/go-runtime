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

package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tim-riep/go-runtime/rterr"
)

// ArchiveKind tells where a package archive comes from.
type ArchiveKind string

// Kinds of archives.
const (
	Asset         ArchiveKind = "asset"   // an asset <ID>-<version>.zip of the release of a tag
	SourceArchive ArchiveKind = "archive" // the archive of the repository at a commit
)

// Resolved is an address whose spec is resolved to a commit.
type Resolved struct {
	Address Address
	Tag     string // "" for commits and branches
	Version string // the version of the tag, "" without tag
	Commit  string
}

// Archive is a downloaded package archive.
type Archive struct {
	Kind ArchiveKind
	// Name is the name of the asset, or of the archive of the commit.
	Name   string
	Data   []byte
	SHA256 string
}

// Source is a host of package repositories, such as GitHub.
type Source interface {
	// Handles reports whether the source serves the address.
	Handles(a Address) bool
	// Tags returns the tags of the repository of an address.
	Tags(ctx context.Context, a Address) ([]Tag, error)
	// Commit returns the commit of a branch, or checks that a full commit
	// hash exists.
	Commit(ctx context.Context, a Address, ref string) (string, error)
	// Fetch downloads the package of a resolved address: the asset
	// <ID>-<version>.zip of the release of the tag if there is one, chosen
	// by pkg when there are several, else the archive of the commit. Archives
	// larger than max bytes are rejected.
	Fetch(ctx context.Context, r Resolved, pkg string, max int64) (Archive, error)
}

// Resolve resolves the spec of an address to a commit: a version or a
// constraint, together with the further constraints, through the tags, a
// commit as given, a branch only when allowBranch is set.
func Resolve(ctx context.Context, s Source, a Address, allowBranch bool, constraints ...string) (Resolved, error) {
	switch a.Spec.Kind {
	case Commit:
		c, err := s.Commit(ctx, a, a.Spec.Text)
		return Resolved{Address: a, Commit: c}, err
	case Branch:
		if !allowBranch {
			return Resolved{}, &rterr.InvalidArgument{Argument: "address", Message: fmt.Sprintf("%s names the branch %s, which may change; allow branches explicitly (--allow-branch)", a, a.Spec.Text)}
		}
		c, err := s.Commit(ctx, a, a.Spec.Text)
		return Resolved{Address: a, Commit: c}, err
	}
	tags, err := s.Tags(ctx, a)
	if err != nil {
		return Resolved{}, err
	}
	r, err := Choose(a.Location(), Releases(tags), a.Spec, constraints...)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Address: a, Tag: r.Tag, Version: r.Version, Commit: r.Commit}, nil
}

// assetPattern matches the names of package assets: <ID>-<version>.zip.
var assetPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]*)-(.+)\.zip$`)

// ChooseAsset picks the package asset of a release among the names of its
// assets: that of pkg, else the only one. It returns "" when the release
// has no package asset of the version.
func ChooseAsset(names []string, version, pkg string) (string, error) {
	var found []string
	for _, n := range names {
		m := assetPattern.FindStringSubmatch(n)
		if m == nil || m[2] != version {
			continue
		}
		if pkg != "" && m[1] == pkg {
			return n, nil
		}
		found = append(found, n)
	}
	switch {
	case len(found) == 0 || pkg != "":
		return "", nil
	case len(found) == 1:
		return found[0], nil
	}
	return "", &rterr.InvalidArgument{Argument: "package", Message: fmt.Sprintf("the release %s has the package assets %s; choose one with --package or the directory of the address", version, strings.Join(found, ", "))}
}

// PackageOf returns the package ID that the directory of an address
// names by its last segment, "" if it names none.
func PackageOf(a Address) string {
	last := a.Dir[strings.LastIndex(a.Dir, "/")+1:]
	if regexp.MustCompile(`^[A-Z][A-Z0-9]*$`).MatchString(last) {
		return last
	}
	return ""
}

// SHA256 returns the hex SHA-256 of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Allowed reports whether an address is in a list of allowed owners or
// owner/repo entries; an empty list allows all.
func Allowed(a Address, list []string) bool {
	if len(list) == 0 {
		return true
	}
	for _, e := range list {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == a.Owner || e == a.Owner+"/"+a.Repo || e == a.Host+"/"+a.Owner || e == a.Repository() {
			return true
		}
	}
	return false
}

// Cache keeps downloaded archives in a directory, named by their
// SHA-256.
type Cache struct{ Dir string }

func (c Cache) path(sha string) string { return filepath.Join(c.Dir, "archives", sha+".zip") }

// Get returns the archive with a SHA-256, if the cache has it and its
// content still has that hash.
func (c Cache) Get(sha string) ([]byte, bool) {
	if c.Dir == "" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sha) {
		return nil, false
	}
	b, err := os.ReadFile(c.path(sha))
	if err != nil || SHA256(b) != sha {
		return nil, false
	}
	return b, true
}

// Put stores an archive and returns its SHA-256.
func (c Cache) Put(data []byte) (string, error) {
	sha := SHA256(data)
	if c.Dir == "" {
		return sha, nil
	}
	p := c.path(sha)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return sha, err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return sha, err
	}
	return sha, os.Rename(tmp, p)
}
