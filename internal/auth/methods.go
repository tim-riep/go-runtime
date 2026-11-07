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
	"crypto/subtle"
	"errors"
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
	Now() time.Time
}

// ErrInvalid is the error of every failed authentication: it does not
// tell whether the user exists, is locked or gave a wrong secret.
var ErrInvalid = errors.New("auth: invalid credentials")

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
var Methods = map[string]Method{model.AuthBasic: Basic{}, model.AuthToken: Bearer{}}

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
		if m := Methods[name]; m != nil {
			out = append(out, m.Challenge())
		}
	}
	return out
}
