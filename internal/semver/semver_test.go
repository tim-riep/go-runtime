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

package semver

import "testing"

func TestCompare(t *testing.T) {
	ordered := []string{"0.9.0", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0"}
	for i := 0; i+1 < len(ordered); i++ {
		a, _ := Parse(ordered[i])
		b, _ := Parse(ordered[i+1])
		if Compare(a, b) >= 0 || Compare(b, a) <= 0 {
			t.Errorf("expected %s < %s", ordered[i], ordered[i+1])
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, s := range []string{"", "1", "1.0", "01.0.0", "1.0.0-", "v1.0.0", "1.0.0.0"} {
		if Valid(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestConstraints(t *testing.T) {
	cases := []struct {
		c    string
		v    string
		want bool
	}{
		{"", "3.4.5", true},
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "0.9.9", false},
		{">=1.0.0 <2.0.0", "1.9.9", true},
		{">=1.0.0, <2.0.0", "2.0.0", false},
		{"1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		{">1.2.3", "1.2.3", false},
		{"<=1.2.3", "1.2.3", true},
		{"^1.2.0", "1.9.0", true},
		{"^1.2.0", "2.0.0", false},
		{"^0.2.0", "0.3.0", false},
		{"^0.0.3", "0.0.4", false},
		{"~1.2.0", "1.2.9", true},
		{"~1.2.0", "1.3.0", false},
	}
	for _, c := range cases {
		got, err := Satisfies(c.v, c.c)
		if err != nil {
			t.Fatalf("%q %q: %v", c.c, c.v, err)
		}
		if got != c.want {
			t.Errorf("Satisfies(%q, %q) = %v, want %v", c.v, c.c, got, c.want)
		}
	}
	if _, err := ParseConstraint(">=x"); err == nil {
		t.Error("expected error for invalid constraint")
	}
}

// TestPartialVersions accepts versions without minor or patch in
// constraints and chooses the highest version, pre-releases only on
// request.
func TestPartialVersions(t *testing.T) {
	for _, c := range []struct {
		constraint, version string
		want              bool
	}{
		{"^1.0", "1.9.3", true}, {"^1.0", "2.0.0", false}, {"^1", "1.0.0", true}, {"~1.2", "1.2.9", true}, {"~1.2", "1.3.0", false},
		{"1.2", "1.2.7", true}, {"1.2", "1.3.0", false}, {"1", "1.99.0", true}, {"1", "2.0.0", false},
		{">=1.2 <2", "1.5.0", true}, {">=1.2 <2", "2.0.0", false}, {"1.2.3", "1.2.4", false},
	} {
		if got, err := Satisfies(c.version, c.constraint); err != nil || got != c.want {
			t.Errorf("%s %s: %v %v", c.constraint, c.version, got, err)
		}
	}
	if _, err := ParseConstraint("^1.2.3.4"); err == nil {
		t.Error("four parts")
	}
	var vs []Version
	for _, s := range []string{"1.0.0", "1.4.0", "1.5.0-rc.1", "2.0.0-beta", "0.9.0"} {
		v, _ := Parse(s)
		vs = append(vs, v)
	}
	for c, want := range map[string]string{"^1.0": "1.4.0", "": "1.4.0", "~1.5.0-rc.1": "1.5.0-rc.1", "^2": ""} {
		pc, _ := ParseConstraint(c)
		v, ok := pc.Highest(vs)
		if got := map[bool]string{true: v.String(), false: ""}[ok]; got != want {
			t.Errorf("highest %q = %q, want %q", c, got, want)
		}
	}
}
