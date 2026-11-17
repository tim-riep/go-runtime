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

package httpsvc

import (
	"slices"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

func schema() *model.Schema {
	s := model.NewSchema()
	s.Packages["TRDEMOFIN"] = &model.PackageInfo{Manifest: model.Manifest{ID: "TRDEMOFIN"}}
	s.Packages["TRBASE"] = &model.PackageInfo{Manifest: model.Manifest{ID: "TRBASE"}}
	add := func(owner model.PackageID, path string, set model.HTTPSettings, handlers ...string) {
		h := &model.HTTPService{Path: path, Owner: owner, Settings: set}
		for _, c := range handlers {
			h.Handlers = append(h.Handlers, model.HTTPHandlerRef{Class: c})
		}
		s.HTTPServices[h.FullPath()] = h
	}
	add("TRDEMOFIN", "", model.HTTPSettings{TimeoutSeconds: model.Some[int64](10)})
	add("TRDEMOFIN", "companies", model.HTTPSettings{}, "/TRDEMOFIN/API")
	add("TRDEMOFIN", "companies/export/csv", model.HTTPSettings{Anonymous: model.Some(true), AnonymousTenant: model.Some("ACME")}, "/TRDEMOFIN/CSV")
	add("TRBASE", "ping", model.HTTPSettings{Anonymous: model.Some(true)}, "/TRBASE/PING")
	return s
}

func TestMatch(t *testing.T) {
	tree := Build(schema())
	for _, c := range []struct{ path, node, rest string }{
		{"/trdemofin/companies", "/trdemofin/companies", ""},
		{"/trdemofin/companies/42", "/trdemofin/companies", "42"},
		{"/trdemofin/companies/export/csv/2026/11", "/trdemofin/companies/export/csv", "2026/11"},
		{"/trdemofin/companies/export", "/trdemofin/companies/export", ""},
		{"/trdemofin", "/trdemofin", ""},
		{"/trdemofin/Companies", "/trdemofin", "Companies"},
		{"/trbase/ping", "/trbase/ping", ""},
		{"/nope/x", "", ""},
		{"/TRDEMOFIN/companies", "", ""},
	} {
		n, rest := tree.Match(c.path)
		got := ""
		if n != nil {
			got = n.Path
		}
		if got != c.node || rest != c.rest {
			t.Errorf("Match(%s) = %s, %q; want %s, %q", c.path, got, rest, c.node, c.rest)
		}
	}
	if n := tree.Node("/trdemofin/companies/export"); n == nil || !n.Implicit() || len(n.Handlers()) != 0 {
		t.Errorf("implicit node: %+v", n)
	}
	if !slices.Equal(tree.Paths(), []string{"/rt", "/rt/odata", "/rt/session", "/trbase", "/trbase/ping", "/trdemofin", "/trdemofin/companies", "/trdemofin/companies/export", "/trdemofin/companies/export/csv"}) {
		t.Errorf("paths: %v", tree.Paths())
	}
}

func TestInheritance(t *testing.T) {
	tree := Build(schema())
	csv := tree.Node("/trdemofin/companies/export/csv").Settings
	if csv.Timeout != 10*time.Second || !csv.Anonymous || csv.AnonymousTenant.Or("") != "ACME" || csv.Budget != Defaults.Budget {
		t.Errorf("csv: %+v", csv)
	}
	export := tree.Node("/trdemofin/companies/export").Settings
	if export.Timeout != 10*time.Second || export.Anonymous {
		t.Errorf("export inherits from companies: %+v", export)
	}
	ping := tree.Node("/trbase/ping").Settings
	if ping.Timeout != Defaults.Timeout || !ping.Anonymous || ping.AnonymousTenant.Valid() {
		t.Errorf("ping: %+v", ping)
	}
	if root := tree.Node("/trbase").Settings; root.Anonymous || !slices.Equal(root.Authentication, Defaults.Authentication) {
		t.Errorf("kernel defaults: %+v", root)
	}
}

func TestActivation(t *testing.T) {
	tree := Build(schema())
	states := Sync(tree, nil)
	csv := tree.Node("/trdemofin/companies/export/csv")
	if Reachable(csv, states) || states[csv.Path] != StateNew {
		t.Fatal("a new node is reachable")
	}
	// Deactivate one node explicitly, then activate the package root.
	states = Deactivate(tree.Node("/trdemofin/companies/export"), states)
	states = Activate(tree.Node("/trdemofin"), states)
	if !Reachable(tree.Node("/trdemofin/companies"), states) {
		t.Error("companies is not reachable")
	}
	if Reachable(csv, states) || states[csv.Path] != StateActive {
		t.Errorf("csv below the deactivated export: %v reachable %v", states[csv.Path], Reachable(csv, states))
	}
	if states["/trdemofin/companies/export"] != StateInactive {
		t.Error("the deactivated node was activated with its parent")
	}
	states = Activate(tree.Node("/trdemofin/companies/export"), states)
	if !Reachable(csv, states) {
		t.Error("csv after activating export")
	}
	if Reachable(tree.Node("/trbase/ping"), states) {
		t.Error("another package was activated")
	}

	// An upgrade adds a node: it is new and not reachable, the others keep
	// their states.
	s := schema()
	s.HTTPServices["/trdemofin/companies/import"] = &model.HTTPService{Path: "companies/import", Owner: "TRDEMOFIN"}
	delete(s.HTTPServices, "/trdemofin/companies/export/csv")
	up := Build(s)
	states = Sync(up, states)
	if Reachable(up.Node("/trdemofin/companies/import"), states) || states["/trdemofin/companies/import"] != StateNew {
		t.Error("the new node of an upgrade is reachable")
	}
	if !Reachable(up.Node("/trdemofin/companies"), states) {
		t.Error("an existing node lost its state")
	}
	if _, ok := states["/trdemofin/companies/export/csv"]; ok {
		t.Error("the state of a removed node is left")
	}
}
