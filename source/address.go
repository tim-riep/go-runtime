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

// Package source fetches packages from remote repositories: it parses
// package addresses, chooses versions from the tags of a repository and
// defines the interface that sources such as GitHub implement.
package source

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tim-riep/go-runtime/internal/semver"
	"github.com/tim-riep/go-runtime/rterr"
)

// DefaultHost is the host of public GitHub.
const DefaultHost = "github.com"

// SpecKind tells what the part after @ of an address names.
type SpecKind int

// Kinds of specs.
const (
	Latest     SpecKind = iota // nothing: the highest stable version
	Version                    // a version, the tag v1.2.0 or 1.2.0
	Constraint                 // a version constraint
	Commit                     // a full commit hash
	Branch                     // a branch, which may change
)

var specKinds = [...]string{"latest", "version", "constraint", "commit", "branch"}

func (k SpecKind) String() string { return specKinds[k] }

// Spec is the part after @ of an address.
type Spec struct {
	Kind SpecKind
	Text string // as given; "" for Latest
}

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	branchPattern = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/-]*$`)
	ownerPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	repoPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	segment       = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

// ParseSpec reads the part after @: a version with or without v, a full
// commit hash, a version constraint, else a branch.
func ParseSpec(s string) (Spec, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return Spec{Kind: Latest}, nil
	case semver.Valid(strings.TrimPrefix(s, "v")):
		return Spec{Kind: Version, Text: s}, nil
	case commitPattern.MatchString(s):
		return Spec{Kind: Commit, Text: s}, nil
	}
	if _, err := semver.ParseConstraint(s); err == nil {
		return Spec{Kind: Constraint, Text: s}, nil
	}
	if !branchPattern.MatchString(s) || strings.Contains(s, "..") || strings.HasSuffix(s, "/") || strings.Contains(s, "//") {
		return Spec{}, invalid("%q is neither a version, a version constraint, a commit nor a branch", s)
	}
	return Spec{Kind: Branch, Text: s}, nil
}

func (s Spec) String() string { return s.Text }

// VersionOf returns the version a Version spec names, without v.
func (s Spec) VersionOf() string { return strings.TrimPrefix(s.Text, "v") }

// Address is the address of a package: a repository on a host, a
// directory in it ("" for the root) and a spec.
type Address struct {
	Host  string
	Owner string
	Repo  string
	Dir   string
	Spec  Spec
}

// Location returns the address without spec in canonical form, such as
// github.com/tim-riep/trfin or github.com/tim-riep/erp/packages/TRFIN.
func (a Address) Location() string {
	s := a.Host + "/" + a.Owner + "/" + a.Repo
	if a.Dir != "" {
		s += "/" + a.Dir
	}
	return s
}

// Repository returns host/owner/repo.
func (a Address) Repository() string { return a.Host + "/" + a.Owner + "/" + a.Repo }

// String returns the canonical form, with @spec unless it is Latest.
func (a Address) String() string {
	if a.Spec.Kind == Latest {
		return a.Location()
	}
	return a.Location() + "@" + a.Spec.Text
}

// WithSpec returns the address with another spec.
func (a Address) WithSpec(s Spec) Address { a.Spec = s; return a }

func invalid(format string, args ...any) error {
	return &rterr.InvalidArgument{Argument: "address", Message: fmt.Sprintf(format, args...)}
}

// IsAddress reports whether s looks like a package address rather than a
// directory: it starts with https:// or one of the hosts.
func IsAddress(s string, hosts ...string) bool {
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return true
	}
	for _, h := range append([]string{DefaultHost}, hosts...) {
		if strings.HasPrefix(s, h+"/") {
			return true
		}
	}
	return false
}

// Parse reads an address in canonical form, host/owner/repo[/dir][@spec],
// or as URL https://host/owner/repo[/tree/ref[/dir]]. The hosts are
// accepted besides github.com, e.g. that of a GitHub Enterprise Server.
// Owner and repository are compared in lower case, as GitHub does; a
// trailing .git is dropped.
func Parse(s string, hosts ...string) (Address, error) {
	rest := strings.TrimSpace(s)
	url := false
	switch {
	case strings.HasPrefix(rest, "http://"):
		return Address{}, invalid("%q: only https is allowed", s)
	case strings.HasPrefix(rest, "https://"):
		rest, url = strings.TrimPrefix(rest, "https://"), true
	}
	var spec string
	hasSpec := false
	if i := strings.Index(rest, "@"); i >= 0 {
		rest, spec, hasSpec = rest[:i], rest[i+1:], true
	}
	parts := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	if len(parts) < 3 {
		return Address{}, invalid("%q: an address has the form github.com/<owner>/<repo>[/<dir>][@<version>]", s)
	}
	a := Address{Host: strings.ToLower(parts[0]), Owner: strings.ToLower(parts[1]), Repo: strings.ToLower(strings.TrimSuffix(parts[2], ".git"))}
	known := a.Host == DefaultHost
	for _, h := range hosts {
		known = known || a.Host == strings.ToLower(h)
	}
	if !known {
		return Address{}, invalid("%q: the host %s is not a configured source", s, a.Host)
	}
	if !ownerPattern.MatchString(a.Owner) || !repoPattern.MatchString(a.Repo) || a.Repo == "." || a.Repo == ".." {
		return Address{}, invalid("%q: %s/%s is no valid owner and repository", s, a.Owner, a.Repo)
	}
	dir := parts[3:]
	// https://host/owner/repo/tree/<ref>/<dir>
	if url && len(dir) >= 2 && dir[0] == "tree" {
		if hasSpec {
			return Address{}, invalid("%q names a ref twice, with /tree/ and with @", s)
		}
		spec, hasSpec, dir = dir[1], true, dir[2:]
	}
	for _, seg := range dir {
		if seg == "." || seg == ".." || !segment.MatchString(seg) {
			return Address{}, invalid("%q: %q is no valid directory", s, strings.Join(dir, "/"))
		}
	}
	a.Dir = strings.Join(dir, "/")
	if hasSpec && strings.TrimSpace(spec) == "" {
		return Address{}, invalid("%q: nothing follows @", s)
	}
	sp, err := ParseSpec(spec)
	if err != nil {
		return Address{}, err
	}
	a.Spec = sp
	return a, nil
}
