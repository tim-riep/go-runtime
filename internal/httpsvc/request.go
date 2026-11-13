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
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/model"
)

// Error is an answer of the kernel to a request it does not pass to the
// handlers: a status and a code for the text of the answer.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return e.Code }

// Codes of the answers of the kernel.
const (
	CodeNotFound       = "not-found"
	CodeUnauthorized   = "unauthorized"
	CodeForbidden      = "forbidden"
	CodeBadRequest     = "bad-request"
	CodeTooLarge       = "body-too-large"
	CodeInternal       = "internal-error"
	CodeUnavailable    = "unavailable"
	CodeNotHandled     = "method-not-handled"
	CodeSessionExpired = "session-expired"
	CodeCSRF           = "csrf-token-invalid"
	CodeSignInFailed   = "sign-in-failed"
	CodeMethod         = "method-not-allowed"
	CodeLanguage       = "language-not-set-up"
)

// TenantHeader is the header of the tenant of a request.
const TenantHeader = "X-Tenant"

// Tenant determines the tenant of a request: from the header X-Tenant,
// the query parameter tenant or the default tenant of the user, as far as
// the node allows these sources. A user may only work in tenants assigned
// to it; an anonymous request only in the tenant of the node, and without
// one in none.
func Tenant(r *http.Request, s Settings, u *auth.User) (model.Optional[string], error) {
	var asked model.Optional[string]
	for _, src := range s.TenantSources {
		switch src {
		case model.SourceHeader:
			if v := r.Header.Get(TenantHeader); v != "" && !asked.Valid() {
				asked = model.Some(v)
			}
		case model.SourceQuery:
			if v := r.URL.Query().Get("tenant"); v != "" && !asked.Valid() {
				asked = model.Some(v)
			}
		case model.SourceUser:
			if u != nil && !asked.Valid() {
				asked = u.DefaultTenant
				// A browser session has its own tenant.
				if u.Session != nil && u.Session.Tenant.Valid() {
					asked = u.Session.Tenant
				}
			}
		}
	}
	if u == nil {
		fixed := s.AnonymousTenant
		if a, ok := asked.Get(); ok && a != fixed.Or("") {
			return model.None[string](), &Error{Status: http.StatusForbidden, Code: CodeForbidden}
		}
		return fixed, nil
	}
	t, ok := asked.Get()
	if !ok {
		return model.None[string](), &Error{Status: http.StatusBadRequest, Code: CodeBadRequest}
	}
	if !slices.Contains(u.Tenants, t) {
		return model.None[string](), &Error{Status: http.StatusForbidden, Code: CodeForbidden}
	}
	return model.Some(t), nil
}

// Language determines the language of a request: from the query
// parameter lang, the header Accept-Language or the default language of
// the user, as far as the node allows these sources, among the languages
// that are set up; English otherwise.
func Language(r *http.Request, s Settings, u *auth.User, available map[string]bool) string {
	for _, src := range s.LanguageSources {
		switch src {
		case model.SourceQuery:
			if l := strings.ToLower(r.URL.Query().Get("lang")); available[l] {
				return l
			}
		case model.SourceHeader:
			for _, l := range acceptLanguages(r.Header.Get("Accept-Language")) {
				if available[l] {
					return l
				}
			}
		case model.SourceUser:
			if u != nil && u.Session != nil {
				if l, ok := u.Session.Language.Get(); ok && available[l] {
					return l
				}
			}
			if u != nil {
				if l, ok := u.Language.Get(); ok && available[l] {
					return l
				}
			}
		}
	}
	return model.DefaultLanguage
}

// acceptLanguages returns the primary language codes of Accept-Language
// by falling quality.
func acceptLanguages(h string) []string {
	type entry struct {
		lang string
		q    float64
		pos  int
	}
	var list []entry
	for i, part := range strings.Split(h, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		lang, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if lang != "" && lang != "*" && q > 0 {
			list = append(list, entry{lang, q, i})
		}
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].q > list[b].q })
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.lang
	}
	return out
}
