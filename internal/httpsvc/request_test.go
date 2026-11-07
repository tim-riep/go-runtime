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
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/model"
)

func TestTenant(t *testing.T) {
	anna := &auth.User{Name: "anna", DefaultTenant: model.Some("ACME"), Tenants: []string{"ACME", "BETA"}}
	for _, c := range []struct {
		name, url, header string
		set               Settings
		user              *auth.User
		want              string
		status            int
	}{
		{"default tenant", "/x", "", Defaults, anna, "ACME", 0},
		{"header", "/x?tenant=ACME", "BETA", Defaults, anna, "BETA", 0},
		{"query", "/x?tenant=BETA", "", Defaults, anna, "BETA", 0},
		{"foreign tenant", "/x", "GAMMA", Defaults, anna, "", 403},
		{"header not allowed", "/x", "BETA", Settings{TenantSources: []string{model.SourceUser}}, anna, "ACME", 0},
		{"no tenant", "/x", "", Defaults, &auth.User{Name: "bob", Tenants: []string{"ACME"}}, "", 400},
		{"anonymous", "/x", "", Settings{TenantSources: Defaults.TenantSources, AnonymousTenant: model.Some("ACME")}, nil, "ACME", 0},
		{"anonymous foreign", "/x", "BETA", Settings{TenantSources: Defaults.TenantSources, AnonymousTenant: model.Some("ACME")}, nil, "", 403},
		{"anonymous without tenant", "/x", "", Defaults, nil, "", 0},
	} {
		r := httptest.NewRequest("GET", c.url, nil)
		if c.header != "" {
			r.Header.Set(TenantHeader, c.header)
		}
		got, err := Tenant(r, c.set, c.user)
		var e *Error
		switch {
		case c.status != 0:
			if !errors.As(err, &e) || e.Status != c.status {
				t.Errorf("%s: %v, want %d", c.name, err, c.status)
			}
		case err != nil || got.Or("") != c.want:
			t.Errorf("%s: %v %v, want %s", c.name, got, err, c.want)
		}
	}
}

func TestLanguage(t *testing.T) {
	available := map[string]bool{"en": true, "de": true}
	anna := &auth.User{Name: "anna", Language: model.Some("de")}
	for _, c := range []struct {
		url, accept string
		user        *auth.User
		want        string
	}{
		{"/x?lang=de", "", nil, "de"},
		{"/x?lang=fr", "fr;q=1, de-CH;q=0.8, en;q=0.5", nil, "de"},
		{"/x", "en-US, de;q=0.9", anna, "en"},
		{"/x", "", anna, "de"},
		{"/x", "fr", nil, "en"},
	} {
		r := httptest.NewRequest("GET", c.url, nil)
		if c.accept != "" {
			r.Header.Set("Accept-Language", c.accept)
		}
		if got := Language(r, Defaults, c.user, available); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.url, c.accept, got, c.want)
		}
	}
}
