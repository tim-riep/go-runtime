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

package source_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/source"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

// TestParse reads every form of an address into its canonical form.
func TestParse(t *testing.T) {
	for _, c := range []struct {
		in, want string
		kind     source.SpecKind
		dir      string
	}{
		{"github.com/tim-riep/trfin@v1.2.0", "github.com/tim-riep/trfin@v1.2.0", source.Version, ""},
		{"github.com/tim-riep/trfin@1.2.0", "github.com/tim-riep/trfin@1.2.0", source.Version, ""},
		{"github.com/tim-riep/trotto@^1.0", "github.com/tim-riep/trotto@^1.0", source.Constraint, ""},
		{"github.com/Tim-Riep/TRFIN.git@>=1.2.0 <2.0.0", "github.com/tim-riep/trfin@>=1.2.0 <2.0.0", source.Constraint, ""},
		{"github.com/tim-riep/trfin", "github.com/tim-riep/trfin", source.Latest, ""},
		{"github.com/tim-riep/trfin@" + commit, "github.com/tim-riep/trfin@" + commit, source.Commit, ""},
		{"github.com/tim-riep/trfin@main", "github.com/tim-riep/trfin@main", source.Branch, ""},
		{"github.com/tim-riep/trfin@feature/x", "github.com/tim-riep/trfin@feature/x", source.Branch, ""},
		{"https://github.com/tim-riep/trfin", "github.com/tim-riep/trfin", source.Latest, ""},
		{"https://github.com/tim-riep/trfin/", "github.com/tim-riep/trfin", source.Latest, ""},
		{"https://github.com/tim-riep/erp-packages/tree/v2.0.0/packages/TRFIN", "github.com/tim-riep/erp-packages/packages/TRFIN@v2.0.0", source.Version, "packages/TRFIN"},
		{"https://github.com/tim-riep/erp-packages/tree/main", "github.com/tim-riep/erp-packages@main", source.Branch, ""},
		{"github.com/tim-riep/erp-packages/packages/TRFIN@^2", "github.com/tim-riep/erp-packages/packages/TRFIN@^2", source.Constraint, "packages/TRFIN"},
		{"ghe.example.com/team/trui@v1.0.0", "ghe.example.com/team/trui@v1.0.0", source.Version, ""},
	} {
		a, err := source.Parse(c.in, "ghe.example.com")
		if err != nil || a.String() != c.want || a.Spec.Kind != c.kind || a.Dir != c.dir {
			t.Errorf("%s: %+v %v", c.in, a, err)
			continue
		}
		if again, err := source.Parse(a.String(), "ghe.example.com"); err != nil || again != a {
			t.Errorf("%s: canonical form does not read back: %+v %v", c.in, again, err)
		}
	}
	for _, c := range []struct{ in, want string }{
		{"github.com/tim-riep", "has the form"},
		{"http://github.com/tim-riep/trfin", "only https"},
		{"gitlab.com/tim-riep/trfin", "not a configured source"},
		{"github.com/-bad/trfin", "no valid owner"},
		{"github.com/tim-riep/trfin/../x", "no valid directory"},
		{"github.com/tim-riep/trfin@", "nothing follows @"},
		{"github.com/tim-riep/trfin@bad..branch", "neither a version"},
		{"https://github.com/tim-riep/trfin/tree/v1.0.0/x@v2.0.0", "names a ref twice"},
	} {
		_, err := source.Parse(c.in)
		var ia *rterr.InvalidArgument
		if !errors.As(err, &ia) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.in, err)
		}
	}
	if !source.IsAddress("github.com/a/b") || !source.IsAddress("https://x/y") || source.IsAddress("examples/TRFIN") || !source.IsAddress("ghe.example.com/a/b", "ghe.example.com") {
		t.Error("IsAddress")
	}
}

// TestChoose picks the tag of a version, the highest release matching a
// constraint and the highest stable release, also under further
// constraints.
func TestChoose(t *testing.T) {
	releases := source.Releases([]source.Tag{{"v1.0.0", "a"}, {"v1.2.0", "b"}, {"1.2.0", "b2"}, {"v1.3.0-rc.1", "c"}, {"v2.0.0", "d"}, {"latest", "e"}, {"v0.9", "f"}})
	if len(releases) != 4 || releases[0].Tag != "v2.0.0" || releases[2].Tag != "v1.2.0" {
		t.Fatalf("releases: %+v", releases)
	}
	spec := func(s string) source.Spec {
		sp, err := source.ParseSpec(s)
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	for _, c := range []struct {
		spec  string
		extra []string
		want  string
	}{
		{"", nil, "v2.0.0"}, {"^1.0", nil, "v1.2.0"}, {"1.2.0", nil, "v1.2.0"}, {"v1.3.0-rc.1", nil, "v1.3.0-rc.1"},
		{"~1.3.0-rc.1", nil, "v1.3.0-rc.1"}, {"", []string{"<2.0.0"}, "v1.2.0"}, {"^1.0", []string{">=1.1.0"}, "v1.2.0"},
	} {
		r, err := source.Choose("github.com/a/b", releases, spec(c.spec), c.extra...)
		if err != nil || r.Tag != c.want {
			t.Errorf("%q %v: %+v %v", c.spec, c.extra, r, err)
		}
	}
	_, err := source.Choose("github.com/a/b", releases, spec("^3"))
	var nf *rterr.NotFound
	if !errors.As(err, &nf) || !strings.Contains(err.Error(), "matching ^3") {
		t.Errorf("no match: %v", err)
	}
	if _, err := source.Choose("github.com/a/b", releases, spec("^1.0"), "^2.0"); err == nil {
		t.Error("conflicting constraints")
	}
}

// TestCache keeps archives by their SHA-256 and ignores changed files.
func TestCache(t *testing.T) {
	c := source.Cache{Dir: t.TempDir()}
	sha, err := c.Put([]byte("archive"))
	if err != nil || sha != source.SHA256([]byte("archive")) {
		t.Fatal(sha, err)
	}
	if b, ok := c.Get(sha); !ok || string(b) != "archive" {
		t.Error("get")
	}
	if _, ok := c.Get(strings.Repeat("0", 64)); ok {
		t.Error("unknown hash")
	}
	if _, ok := c.Get("../x"); ok {
		t.Error("path")
	}
	if !source.Allowed(source.Address{Host: "github.com", Owner: "tim-riep", Repo: "trfin"}, []string{"acme", "tim-riep/trfin"}) ||
		source.Allowed(source.Address{Host: "github.com", Owner: "evil", Repo: "trfin"}, []string{"tim-riep"}) {
		t.Error("allowed")
	}
}
