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

package kernel

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/internal/httpsvc"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
)

// Defaults of browser sessions.
const (
	DefaultSessionIdleTimeout = 30 * time.Minute
	DefaultSessionLifetime    = 12 * time.Hour
	// sessionTouch is how often the last use of a session is written.
	sessionTouch = time.Minute
)

// SessionSettings configure the browser sessions of /rt/session.
type SessionSettings struct {
	// IdleTimeout ends a session that was not used for this long; 0 means
	// DefaultSessionIdleTimeout.
	IdleTimeout time.Duration
	// Lifetime ends a session this long after the sign-in; 0 means
	// DefaultSessionLifetime.
	Lifetime time.Duration
	// InsecureCookie leaves out the attribute Secure of the cookie, for
	// development without TLS only.
	InsecureCookie bool
}

func (s SessionSettings) withDefaults() SessionSettings {
	if s.IdleTimeout <= 0 {
		s.IdleTimeout = DefaultSessionIdleTimeout
	}
	if s.Lifetime <= 0 {
		s.Lifetime = DefaultSessionLifetime
	}
	return s
}

// SetSessionSettings configures the browser sessions.
func (k *Kernel) SetSessionSettings(s SessionSettings) { k.sessions = s.withDefaults() }

func (s authStore) Session(ctx context.Context, hash string) (*auth.Session, error) {
	k := s.k
	ws, err := k.repo.WebSession(ctx, k.db, hash)
	if err != nil || ws == nil {
		return nil, err
	}
	now := k.timestamp()
	if !now.Before(ws.ExpiresAt) || now.Sub(ws.LastSeenAt) > k.sessions.IdleTimeout {
		return nil, k.userTx(ctx, func(q dialect.Querier) error { return k.repo.DeleteWebSession(ctx, q, hash) })
	}
	if now.Sub(ws.LastSeenAt) >= sessionTouch {
		ws.LastSeenAt = now
		if err := k.userTx(ctx, func(q dialect.Querier) error { return k.repo.UpdateWebSession(ctx, q, ws) }); err != nil {
			return nil, err
		}
	}
	return &auth.Session{Hash: ws.Hash, User: ws.User, Tenant: ws.Tenant, Language: ws.Language, ExpiresAt: ws.ExpiresAt}, nil
}

// SessionInfo is what GET /rt/session returns.
type SessionInfo struct {
	User string `json:"user"`
	// Name is the name of the user to show; users have none yet.
	Name      model.Optional[string] `json:"name"`
	Tenants   []string               `json:"tenants"`
	Tenant    model.Optional[string] `json:"tenant"`
	Language  string                 `json:"language"`
	Languages []SessionLanguage      `json:"languages"`
	CSRFToken string                 `json:"csrfToken"`
	ExpiresAt time.Time              `json:"expiresAt"`
}

// SessionLanguage is a language that is set up.
type SessionLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// WebSessions lists the browser sessions of a user, or of all for "".
func (k *Kernel) WebSessions(ctx context.Context, user string) ([]*repo.WebSession, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.repo.WebSessions(ctx, k.db, user)
}

// EndWebSessions ends all browser sessions of a user.
func (k *Kernel) EndWebSessions(ctx context.Context, user string) error {
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	return k.userTx(ctx, func(q dialect.Querier) error { return k.repo.DeleteWebSessions(ctx, q, user) })
}

// serveSession signs browsers in and out at /rt/session: POST signs in
// with user and password, GET describes the session, PATCH changes its
// tenant and language, DELETE signs out. It runs before the authentication
// of the request, because signing in needs none.
func (k *Kernel) serveSession(w http.ResponseWriter, r *http.Request, langs map[string]bool, entry *accessEntry) {
	ctx := r.Context()
	lang := httpsvc.Language(r, httpsvc.Defaults, nil, langs)
	fail := func(status int, code string) {
		entry.status = status
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		body, _ := json.Marshal(map[string]string{"code": code, "message": httpText(code, lang)})
		entry.size, _ = w.Write(body)
	}
	internal := func(err error) {
		slog.Error("browser session", "error", err)
		fail(http.StatusInternalServerError, httpsvc.CodeInternal)
	}
	store := authStore{k: k}
	current := func() (*auth.User, bool) {
		u, err := auth.Cookie{}.Authenticate(ctx, r, store)
		switch {
		case errors.Is(err, auth.ErrInvalid):
			fail(http.StatusUnauthorized, httpsvc.CodeSessionExpired)
			return nil, false
		case err != nil:
			internal(err)
			return nil, false
		}
		entry.user = u.Name
		return u, true
	}
	csrf := func(u *auth.User) bool {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(auth.CSRFHeader)), []byte(u.Session.CSRFToken)) != 1 {
			fail(http.StatusForbidden, httpsvc.CodeCSRF)
			return false
		}
		return true
	}
	var body struct {
		User     string                 `json:"user"`
		Password string                 `json:"password"`
		Tenant   model.Optional[string] `json:"tenant"`
		Language model.Optional[string] `json:"language"`
	}
	readBody := func() bool {
		mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		data, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if mt != "application/json" || err != nil || json.Unmarshal(data, &body) != nil {
			fail(http.StatusBadRequest, httpsvc.CodeBadRequest)
			return false
		}
		return true
	}
	switch r.Method {
	case http.MethodPost:
		if !readBody() {
			return
		}
		u, err := auth.Login(ctx, store, body.User, body.Password)
		switch {
		case errors.Is(err, auth.ErrInvalid):
			fail(http.StatusUnauthorized, httpsvc.CodeSignInFailed)
			return
		case err != nil:
			internal(err)
			return
		}
		entry.user = u.Name
		tenant := u.DefaultTenant
		if t, ok := body.Tenant.Get(); ok {
			if !slices.Contains(u.Tenants, t) {
				fail(http.StatusForbidden, httpsvc.CodeForbidden)
				return
			}
			tenant = body.Tenant
		}
		language := model.None[string]()
		if l, ok := body.Language.Get(); ok {
			if !langs[l] {
				fail(http.StatusBadRequest, httpsvc.CodeLanguage)
				return
			}
			language = body.Language
		}
		id, err := auth.NewSessionID()
		if err != nil {
			internal(err)
			return
		}
		now := k.timestamp()
		ws := &repo.WebSession{Hash: auth.SessionHash(id), User: u.Name, Tenant: tenant, Language: language, CreatedAt: now, LastSeenAt: now,
			ExpiresAt: now.Add(k.sessions.Lifetime)}
		err = k.userTx(ctx, func(q dialect.Querier) error {
			if err := k.repo.DeleteExpiredWebSessions(ctx, q, now, now.Add(-k.sessions.IdleTimeout)); err != nil {
				return err
			}
			return k.repo.InsertWebSession(ctx, q, ws)
		})
		if err != nil {
			internal(err)
			return
		}
		http.SetCookie(w, k.sessionCookie(id, ws.ExpiresAt))
		u.Session = &auth.Session{Hash: ws.Hash, User: u.Name, Tenant: tenant, Language: language, CSRFToken: auth.CSRFToken(id), ExpiresAt: ws.ExpiresAt}
		k.writeSessionInfo(w, r, u, langs, entry)
	case http.MethodGet, http.MethodHead:
		u, ok := current()
		if !ok {
			return
		}
		k.writeSessionInfo(w, r, u, langs, entry)
	case http.MethodPatch:
		u, ok := current()
		if !ok || !csrf(u) || !readBody() {
			return
		}
		sess := u.Session
		if t, ok := body.Tenant.Get(); ok {
			if !slices.Contains(u.Tenants, t) {
				fail(http.StatusForbidden, httpsvc.CodeForbidden)
				return
			}
			sess.Tenant = body.Tenant
		}
		if l, ok := body.Language.Get(); ok {
			if !langs[l] {
				fail(http.StatusBadRequest, httpsvc.CodeLanguage)
				return
			}
			sess.Language = body.Language
		}
		err := k.userTx(ctx, func(q dialect.Querier) error {
			ws, err := k.repo.WebSession(ctx, q, sess.Hash)
			if err != nil || ws == nil {
				return err
			}
			ws.Tenant, ws.Language, ws.LastSeenAt = sess.Tenant, sess.Language, k.timestamp()
			return k.repo.UpdateWebSession(ctx, q, ws)
		})
		if err != nil {
			internal(err)
			return
		}
		k.writeSessionInfo(w, r, u, langs, entry)
	case http.MethodDelete:
		u, ok := current()
		if !ok || !csrf(u) {
			return
		}
		if err := k.userTx(ctx, func(q dialect.Querier) error { return k.repo.DeleteWebSession(ctx, q, u.Session.Hash) }); err != nil {
			internal(err)
			return
		}
		c := k.sessionCookie("", time.Unix(0, 0))
		c.MaxAge = -1
		http.SetCookie(w, c)
		entry.status = http.StatusNoContent
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST, PATCH, DELETE")
		fail(http.StatusMethodNotAllowed, httpsvc.CodeMethod)
	}
}

// sessionCookie returns the cookie of a session: only for HTTP, sent only
// over TLS unless configured for development, only with requests of the
// same site, for all paths.
func (k *Kernel) sessionCookie(id string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: auth.SessionCookie, Value: id, Path: "/", Expires: expires, HttpOnly: true, Secure: !k.sessions.InsecureCookie,
		SameSite: http.SameSiteStrictMode}
}

func (k *Kernel) writeSessionInfo(w http.ResponseWriter, r *http.Request, u *auth.User, langs map[string]bool, entry *accessEntry) {
	info := SessionInfo{User: u.Name, Tenants: slices.Clone(u.Tenants), Tenant: u.Session.Tenant, CSRFToken: u.Session.CSRFToken,
		ExpiresAt: u.Session.ExpiresAt, Languages: []SessionLanguage{}}
	if info.Tenants == nil {
		info.Tenants = []string{}
	}
	slices.Sort(info.Tenants)
	info.Language = httpsvc.Language(r, httpsvc.Settings{LanguageSources: []string{model.SourceUser}}, u, langs)
	list, err := k.Languages().List(r.Context())
	if err != nil {
		slog.Error("languages of a browser session", "error", err)
	}
	for _, l := range list {
		info.Languages = append(info.Languages, SessionLanguage{Code: l.Code, Name: l.Name})
	}
	data, _ := json.Marshal(info)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	entry.status = http.StatusOK
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		entry.size, _ = w.Write(data)
	}
}
