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

// Package semver implements semantic versions and version constraints.
//
// A constraint is a list of comparisons separated by commas or spaces, all of
// which must hold: "=1.2.3", "1.2.3", ">1.0.0", ">=1.0.0", "<2.0.0",
// "<=1.4.0", "^1.2.0" (same major version) and "~1.2.0" (same minor
// version). An empty constraint matches every version. Versions in
// constraints may leave out the minor and patch version, which count as 0:
// "^1.0" is "^1.0.0"; without operator "1.2" is every 1.2.x and "1" every
// 1.x.x.
package semver

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version.
type Version struct {
	Major, Minor, Patch int
	Pre                 []string // pre-release identifiers
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// Parse parses a version such as "1.2.3" or "1.2.3-rc.1".
func Parse(s string) (Version, error) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a semantic version", s)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.Pre = strings.Split(m[4], ".")
	}
	return v, nil
}

// Valid reports whether s is a semantic version.
func Valid(s string) bool { _, err := Parse(s); return err == nil }

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Compare returns -1, 0 or +1 following semantic version precedence.
func Compare(a, b Version) int {
	if c := cmp.Or(cmp.Compare(a.Major, b.Major), cmp.Compare(a.Minor, b.Minor), cmp.Compare(a.Patch, b.Patch)); c != 0 {
		return c
	}
	switch {
	case len(a.Pre) == 0 && len(b.Pre) == 0:
		return 0
	case len(a.Pre) == 0:
		return 1
	case len(b.Pre) == 0:
		return -1
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		if c := comparePre(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.Pre), len(b.Pre))
}

func comparePre(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return cmp.Compare(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

type op int

const (
	opEq op = iota
	opGt
	opGe
	opLt
	opLe
)

type comparison struct {
	op op
	v  Version
}

func (c comparison) match(v Version) bool {
	r := Compare(v, c.v)
	switch c.op {
	case opEq:
		return r == 0
	case opGt:
		return r > 0
	case opGe:
		return r >= 0
	case opLt:
		return r < 0
	case opLe:
		return r <= 0
	}
	return false
}

// Constraint is a parsed version constraint.
type Constraint struct {
	text string
	cmps []comparison
}

// ParseConstraint parses a constraint expression.
func ParseConstraint(s string) (Constraint, error) {
	c := Constraint{text: strings.TrimSpace(s)}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		cmps, err := parseComparison(tok)
		if err != nil {
			return Constraint{}, err
		}
		c.cmps = append(c.cmps, cmps...)
	}
	return c, nil
}

// partial parses a version that may lack minor and patch, which count as
// 0, and returns how many parts were given.
func partial(s string) (Version, int, error) {
	if v, err := Parse(s); err == nil {
		return v, 3, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return Version{}, 0, fmt.Errorf("%q is not a semantic version", s)
	}
	full := strings.Join(append(parts, []string{"0", "0"}[:3-len(parts)]...), ".")
	v, err := Parse(full)
	if err != nil || len(v.Pre) > 0 {
		return Version{}, 0, fmt.Errorf("%q is not a semantic version", s)
	}
	return v, len(parts), nil
}

func parseComparison(tok string) ([]comparison, error) {
	prefixes := []struct {
		p  string
		op op
	}{{">=", opGe}, {"<=", opLe}, {">", opGt}, {"<", opLt}, {"=", opEq}}
	for _, p := range prefixes {
		if rest, ok := strings.CutPrefix(tok, p.p); ok {
			v, _, err := partial(rest)
			if err != nil {
				return nil, err
			}
			return []comparison{{p.op, v}}, nil
		}
	}
	if rest, ok := strings.CutPrefix(tok, "^"); ok {
		v, _, err := partial(rest)
		if err != nil {
			return nil, err
		}
		upper := Version{Major: v.Major + 1}
		switch {
		case v.Major == 0 && v.Minor == 0:
			upper = Version{Patch: v.Patch + 1}
		case v.Major == 0:
			upper = Version{Minor: v.Minor + 1}
		}
		return []comparison{{opGe, v}, {opLt, upper}}, nil
	}
	if rest, ok := strings.CutPrefix(tok, "~"); ok {
		v, _, err := partial(rest)
		if err != nil {
			return nil, err
		}
		return []comparison{{opGe, v}, {opLt, Version{Major: v.Major, Minor: v.Minor + 1}}}, nil
	}
	v, n, err := partial(tok)
	switch {
	case err != nil:
		return nil, err
	case n == 1:
		return []comparison{{opGe, v}, {opLt, Version{Major: v.Major + 1}}}, nil
	case n == 2:
		return []comparison{{opGe, v}, {opLt, Version{Major: v.Major, Minor: v.Minor + 1}}}, nil
	}
	return []comparison{{opEq, v}}, nil
}

// Prerelease reports whether the constraint names a pre-release version;
// only then do pre-releases take part when a version is chosen.
func (c Constraint) Prerelease() bool {
	for _, cmp := range c.cmps {
		if len(cmp.v.Pre) > 0 {
			return true
		}
	}
	return false
}

// Highest returns the highest of the versions that satisfy the
// constraint, pre-releases only if the constraint names one.
func (c Constraint) Highest(versions []Version) (Version, bool) {
	var best Version
	found := false
	for _, v := range versions {
		if len(v.Pre) > 0 && !c.Prerelease() || !c.Match(v) {
			continue
		}
		if !found || Compare(v, best) > 0 {
			best, found = v, true
		}
	}
	return best, found
}

// Match reports whether v satisfies the constraint.
func (c Constraint) Match(v Version) bool {
	for _, cmp := range c.cmps {
		if !cmp.match(v) {
			return false
		}
	}
	return true
}

func (c Constraint) String() string { return c.text }

// Satisfies parses version and constraint and reports whether the version
// satisfies the constraint.
func Satisfies(version, constraint string) (bool, error) {
	v, err := Parse(version)
	if err != nil {
		return false, err
	}
	c, err := ParseConstraint(constraint)
	if err != nil {
		return false, err
	}
	return c.Match(v), nil
}
