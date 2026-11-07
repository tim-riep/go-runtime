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
	"strings"
	"testing"
)

func TestHTTPServices(t *testing.T) {
	fsys := validFS()
	fsys["schema/http-services/root.json"] = file(`{"path":"","texts":{"en":{"label":"Root"}},"settings":{"anonymous":false,"timeoutSeconds":10}}`)
	fsys["schema/http-services/companies.json"] = file(`{"path":"companies/v1","texts":{"en":{"label":"Companies"}},
		"handlers":[{"class":"/TRX/API","parameters":[{"name":"mode","value":""}]},{"class":"/TRBASE/PING"}]}`)
	pkg, err := Load(fsys, "mem")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.HTTPServices) != 2 {
		t.Fatalf("nodes: %+v", pkg.HTTPServices)
	}
	for _, h := range pkg.HTTPServices {
		switch h.Path {
		case "":
			if v, _ := h.Settings.TimeoutSeconds.Get(); v != 10 || h.Settings.Anonymous.Or(true) || h.Settings.Budget.Valid() {
				t.Errorf("settings of the root: %+v", h.Settings)
			}
		case "companies/v1":
			if len(h.Handlers) != 2 || h.Handlers[0].Parameters[0].Value != "" || h.File != "schema/http-services/companies.json" {
				t.Errorf("node: %+v", h)
			}
		}
	}
	for _, c := range []struct{ doc, want string }{
		{`{"path":"a//b","texts":{"en":{"label":"X"}}}`, "no valid path"},
		{`{"path":"/a","texts":{"en":{"label":"X"}}}`, "no valid path"},
		{`{"path":"a/..","texts":{"en":{"label":"X"}}}`, "no valid path"},
		{`{"path":"a b","texts":{"en":{"label":"X"}}}`, "no valid path"},
		{`{"path":"x","texts":{"en":{"label":"X"}},"settings":{"authentication":["cookie"]}}`, "authentication"},
		{`{"path":"x","texts":{"en":{"label":"X"}},"handlers":[{"class":"/TROTHER/H"}]}`, "/TROTHER/H"},
	} {
		fsys := validFS()
		fsys["schema/http-services/x.json"] = file(c.doc)
		if _, err := Load(fsys, "mem"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", c.doc, err, c.want)
		}
	}
	fsys = validFS()
	fsys["schema/http-services/a.json"] = file(`{"path":"x","texts":{"en":{"label":"X"}}}`)
	fsys["schema/http-services/b.json"] = file(`{"path":"x","texts":{"en":{"label":"Y"}}}`)
	if _, err := Load(fsys, "mem"); err == nil || !strings.Contains(err.Error(), "defined in") {
		t.Errorf("duplicate path: %v", err)
	}
}
