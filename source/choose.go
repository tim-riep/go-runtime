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
	"fmt"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/internal/semver"
	"github.com/tim-riep/go-runtime/rterr"
)

// Tag is a tag of a repository with the commit it points to.
type Tag struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// Release is a tag whose name is a version, v1.2.0 or 1.2.0.
type Release struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Releases returns the tags that name versions, the highest first. When
// both v1.2.0 and 1.2.0 exist, the one with v counts.
func Releases(tags []Tag) []Release {
	byVersion := map[string]Release{}
	for _, t := range tags {
		v := strings.TrimPrefix(t.Name, "v")
		if !semver.Valid(v) {
			continue
		}
		if old, ok := byVersion[v]; ok && strings.HasPrefix(old.Tag, "v") {
			continue
		}
		byVersion[v] = Release{Tag: t.Name, Version: v, Commit: t.Commit}
	}
	out := make([]Release, 0, len(byVersion))
	for _, r := range byVersion {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Release) int {
		va, _ := semver.Parse(a.Version)
		vb, _ := semver.Parse(b.Version)
		return semver.Compare(vb, va)
	})
	return out
}

// Choose returns the release that a spec of kind Latest, Version or
// Constraint names, among those that also satisfy the further
// constraints, such as those of packages that require it: the tag of the
// version, else the highest release that matches, stable ones only unless
// a constraint names a pre-release.
func Choose(location string, releases []Release, spec Spec, constraints ...string) (Release, error) {
	all := slices.Clone(constraints)
	switch spec.Kind {
	case Version:
		all = append(all, "="+spec.VersionOf())
	case Constraint:
		all = append(all, spec.Text)
	case Latest:
	default:
		return Release{}, fmt.Errorf("source: a %s is not chosen from releases", spec.Kind)
	}
	var cs []semver.Constraint
	pre := false
	for _, c := range all {
		pc, err := semver.ParseConstraint(c)
		if err != nil {
			return Release{}, &rterr.InvalidArgument{Argument: "version", Message: err.Error()}
		}
		cs = append(cs, pc)
		pre = pre || pc.Prerelease()
	}
	for _, r := range releases {
		v, _ := semver.Parse(r.Version)
		if len(v.Pre) > 0 && !pre {
			continue
		}
		ok := true
		for _, c := range cs {
			ok = ok && c.Match(v)
		}
		if ok {
			return r, nil
		}
	}
	what := "a stable version"
	if len(all) > 0 {
		what = "a version matching " + strings.Join(all, " and ")
	}
	return Release{}, &rterr.NotFound{Kind: what + " among the tags of", Name: location}
}
