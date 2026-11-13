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

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// User is a signed-in user.
type User struct {
	Name          string
	Language      model.Optional[string]
	DefaultTenant model.Optional[string]
	Tenants       []string // tenants the user may work in
	// Session is the browser session the user signed in with; nil for
	// other methods.
	Session *Session
}

// Session is a session of a browser as the store keeps it.
type Session struct {
	// Hash is the hash of the session ID, the key in the store.
	Hash     string
	User     string
	Tenant   model.Optional[string]
	Language model.Optional[string]
	// CSRFToken is the token that changing requests of the session send.
	CSRFToken string
	ExpiresAt time.Time
}

// Account is a user as the store keeps it.
type Account struct {
	User
	Locked       bool
	PasswordHash model.Optional[string]
}

// Token is an API token as the store keeps it.
type Token struct {
	ID        string
	User      string
	Hash      string
	ExpiresAt model.Optional[time.Time]
}

// Store reads users and tokens and records failed attempts.
type Store interface {
	Account(ctx context.Context, name string) (*Account, error) // nil without such user
	Token(ctx context.Context, id string) (*Token, error)       // nil without such token
	// Failed records a failed attempt for a user; the store locks the user
	// after too many of them.
	Failed(ctx context.Context, name string) error
	// Succeeded resets the failed attempts of a user.
	Succeeded(ctx context.Context, name string) error
	// Session returns a session by the hash of its ID, nil if there is
	// none or it expired.
	Session(ctx context.Context, hash string) (*Session, error)
	Now() time.Time
}

// ErrInvalid is the error of every failed authentication: it does not
// tell whether the user exists, is locked or gave a wrong secret.
var ErrInvalid = errors.New("auth: invalid credentials")

// ErrSessionExpired is the error of a session cookie whose session does
// not exist or expired; it is an ErrInvalid.
var ErrSessionExpired = fmt.Errorf("%w: the session expired", ErrInvalid)

// Method is an authentication method of http services.
type Method interface {
	Name() string // as in the settings of service nodes
	// Applies reports whether a request carries credentials of the method.
	Applies(r *http.Request) bool
	Authenticate(ctx context.Context, r *http.Request, s Store) (*User, error)
	// Challenge is the value of WWW-Authenticate for a request without
	// valid credentials.
	Challenge() string
}

// Methods are the authentication methods by name.
var Methods = map[string]Method{model.AuthBasic: Basic{}, model.AuthToken: Bearer{}, model.AuthSession: Cookie{}}

// Basic is HTTP Basic authentication with user name and password.
type Basic struct{}

func (Basic) Name() string      { return model.AuthBasic }
func (Basic) Challenge() string { return `Basic realm="go-runtime", charset="UTF-8"` }

func (Basic) Applies(r *http.Request) bool {
	_, _, ok := r.BasicAuth()
	return ok
}

func (Basic) Authenticate(ctx context.Context, r *http.Request, s Store) (*User, error) {
	name, password, ok := r.BasicAuth()
	if !ok {
		return nil, ErrInvalid
	}
	return Login(ctx, s, name, password)
}

// Login checks the password of a user. Failed attempts count against the
// user, a success resets them.
func Login(ctx context.Context, s Store, name, password string) (*User, error) {
	a, err := s.Account(ctx, name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		// Spend the time of a check, so that the answer does not tell
		// whether the user exists.
		CheckPassword(dummyHash, password)
		return nil, ErrInvalid
	}
	hash, has := a.PasswordHash.Get()
	if a.Locked || !has || !CheckPassword(hash, password) {
		if !a.Locked {
			if err := s.Failed(ctx, name); err != nil {
				return nil, err
			}
		}
		return nil, ErrInvalid
	}
	if err := s.Succeeded(ctx, name); err != nil {
		return nil, err
	}
	return &a.User, nil
}

var dummyHash, _ = HashPassword("go-runtime")

// Bearer is authentication with an API token: Authorization: Bearer rt_...
type Bearer struct{}

func (Bearer) Name() string      { return model.AuthToken }
func (Bearer) Challenge() string { return `Bearer realm="go-runtime"` }

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(h, " ")
	return strings.TrimSpace(token), ok && strings.EqualFold(scheme, "Bearer")
}

func (Bearer) Applies(r *http.Request) bool {
	_, ok := bearer(r)
	return ok
}

func (Bearer) Authenticate(ctx context.Context, r *http.Request, s Store) (*User, error) {
	token, _ := bearer(r)
	id, err := ParseToken(token)
	if err != nil {
		return nil, ErrInvalid
	}
	t, err := s.Token(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil || subtle.ConstantTimeCompare([]byte(t.Hash), []byte(TokenHash(token))) != 1 {
		return nil, ErrInvalid
	}
	if exp, ok := t.ExpiresAt.Get(); ok && !s.Now().Before(exp) {
		return nil, ErrInvalid
	}
	a, err := s.Account(ctx, t.User)
	if err != nil {
		return nil, err
	}
	if a == nil || a.Locked {
		return nil, ErrInvalid
	}
	return &a.User, nil
}

// SessionCookie is the name of the cookie of browser sessions.
const SessionCookie = "rt_session"

// CSRFHeader is the header with the token of the session that requests
// other than GET, HEAD and OPTIONS send.
const CSRFHeader = "X-CSRF-Token"

// NewSessionID returns a new random session ID for the cookie.
func NewSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SessionHash returns the stored hash of a session ID.
func SessionHash(id string) string { return TokenHash("session:" + id) }

// CSRFToken returns the token of a session. It derives from the session
// ID, which only the cookie holds, so it needs no storage.
func CSRFToken(id string) string {
	sum := sha256.Sum256([]byte("csrf\x00" + id))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Cookie is authentication with the cookie of a browser session.
type Cookie struct{}

func (Cookie) Name() string { return model.AuthSession }

// Challenge is empty: browsers sign in through /rt/session, not through a
// dialog.
func (Cookie) Challenge() string { return "" }

func (Cookie) Applies(r *http.Request) bool {
	c, err := r.Cookie(SessionCookie)
	return err == nil && c.Value != ""
}

func (Cookie) Authenticate(ctx context.Context, r *http.Request, s Store) (*User, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil, ErrSessionExpired
	}
	sess, err := s.Session(ctx, SessionHash(c.Value))
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrSessionExpired
	}
	a, err := s.Account(ctx, sess.User)
	if err != nil {
		return nil, err
	}
	if a == nil || a.Locked {
		return nil, ErrSessionExpired
	}
	u := a.User
	sess.CSRFToken = CSRFToken(c.Value)
	u.Session = sess
	return &u, nil
}

// Authenticate signs in a request with the first allowed method whose
// credentials the request carries. Without credentials it returns nil and
// no error.
func Authenticate(ctx context.Context, r *http.Request, allowed []string, s Store) (*User, error) {
	for _, name := range allowed {
		m := Methods[name]
		if m != nil && m.Applies(r) {
			return m.Authenticate(ctx, r, s)
		}
	}
	return nil, nil
}

// Challenges returns the WWW-Authenticate values of the allowed methods.
func Challenges(allowed []string) []string {
	var out []string
	for _, name := range allowed {
		if m := Methods[name]; m != nil && m.Challenge() != "" {
			out = append(out, m.Challenge())
		}
	}
	return out
}
