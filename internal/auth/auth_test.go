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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

type memStore struct {
	accounts map[string]*Account
	tokens   map[string]*Token
	failed   map[string]int
	sessions map[string]*Session
	now      time.Time
}

func (s *memStore) Session(_ context.Context, hash string) (*Session, error) {
	return s.sessions[hash], nil
}

func (s *memStore) Account(_ context.Context, name string) (*Account, error) {
	return s.accounts[name], nil
}
func (s *memStore) Token(_ context.Context, id string) (*Token, error) { return s.tokens[id], nil }
func (s *memStore) Now() time.Time                                     { return s.now }
func (s *memStore) Failed(_ context.Context, name string) error {
	s.failed[name]++
	if s.failed[name] >= 3 {
		s.accounts[name].Locked = true
	}
	return nil
}
func (s *memStore) Succeeded(_ context.Context, name string) error {
	s.failed[name] = 0
	return nil
}

func TestPasswords(t *testing.T) {
	h, err := HashPassword("secret")
	if err != nil || !CheckPassword(h, "secret") || CheckPassword(h, "Secret") || CheckPassword("plain", "plain") {
		t.Fatalf("hash %s: %v", h, err)
	}
	h2, _ := HashPassword("secret")
	if h == h2 {
		t.Error("the salt is not random")
	}
}

func TestBasicAndBearer(t *testing.T) {
	ctx := context.Background()
	hash, _ := HashPassword("pw")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s := &memStore{accounts: map[string]*Account{"anna": {User: User{Name: "anna"}, PasswordHash: model.Some(hash)}},
		tokens: map[string]*Token{}, failed: map[string]int{}, now: now}
	basic := func(user, pw string) (*User, error) {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth(user, pw)
		return Authenticate(ctx, r, []string{model.AuthBasic, model.AuthToken}, s)
	}
	if u, err := basic("anna", "pw"); err != nil || u.Name != "anna" {
		t.Fatalf("basic: %v %v", u, err)
	}
	if _, err := basic("nobody", "pw"); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown user: %v", err)
	}
	for range 3 {
		if _, err := basic("anna", "wrong"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("wrong password: %v", err)
		}
	}
	if _, err := basic("anna", "pw"); !errors.Is(err, ErrInvalid) {
		t.Error("a locked user signed in")
	}
	s.accounts["anna"].Locked = false

	id := "0199a0b1-2c3d-7e4f-8a9b-0c1d2e3f4a5b"
	token, th, err := NewToken(id)
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := ParseToken(token); err != nil || parsed != id {
		t.Fatalf("parse: %s %v", parsed, err)
	}
	s.tokens[id] = &Token{ID: id, User: "anna", Hash: th, ExpiresAt: model.Some(now.Add(time.Hour))}
	bearer := func(tok string, allowed ...string) (*User, error) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		return Authenticate(ctx, r, allowed, s)
	}
	if u, err := bearer(token, model.AuthToken); err != nil || u.Name != "anna" {
		t.Fatalf("bearer: %v %v", u, err)
	}
	if u, err := bearer(token, model.AuthBasic); u != nil || err != nil {
		t.Errorf("token where only basic is allowed: %v %v", u, err)
	}
	if _, err := bearer(token+"x", model.AuthToken); !errors.Is(err, ErrInvalid) {
		t.Errorf("wrong secret: %v", err)
	}
	s.now = now.Add(2 * time.Hour)
	if _, err := bearer(token, model.AuthToken); !errors.Is(err, ErrInvalid) {
		t.Errorf("expired token: %v", err)
	}
	s.now = now
	delete(s.tokens, id)
	if _, err := bearer(token, model.AuthToken); !errors.Is(err, ErrInvalid) {
		t.Errorf("revoked token: %v", err)
	}
	if u, err := Authenticate(ctx, httptest.NewRequest("GET", "/", nil), []string{model.AuthBasic}, s); u != nil || err != nil {
		t.Errorf("no credentials: %v %v", u, err)
	}
}

func TestSessionCookie(t *testing.T) {
	hash, _ := HashPassword("pw")
	s := &memStore{accounts: map[string]*Account{"anna": {User: User{Name: "anna", Tenants: []string{"ACME"}}, PasswordHash: model.Some(hash)}},
		failed: map[string]int{}, sessions: map[string]*Session{}}
	id, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	s.sessions[SessionHash(id)] = &Session{User: "anna", Tenant: model.Some("ACME")}
	r := httptest.NewRequest("GET", "/x", nil)
	if (Cookie{}).Applies(r) {
		t.Error("applies without cookie")
	}
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: id})
	u, err := Authenticate(context.Background(), r, []string{model.AuthBasic, model.AuthSession}, s)
	if err != nil || u.Name != "anna" || u.Session == nil || u.Session.CSRFToken != CSRFToken(id) || u.Session.Tenant.Or("") != "ACME" {
		t.Fatalf("session: %+v %v", u, err)
	}
	if CSRFToken(id) == CSRFToken(id+"x") || SessionHash(id) == id {
		t.Error("token or hash does not depend on the ID")
	}
	// Basic wins when listed first and the request carries both.
	r.SetBasicAuth("anna", "pw")
	if u, err := Authenticate(context.Background(), r, []string{model.AuthBasic, model.AuthSession}, s); err != nil || u.Session != nil {
		t.Errorf("basic first: %+v %v", u, err)
	}
	unknown := httptest.NewRequest("GET", "/x", nil)
	unknown.AddCookie(&http.Cookie{Name: SessionCookie, Value: "nope"})
	if _, err := Authenticate(context.Background(), unknown, []string{model.AuthSession}, s); !errors.Is(err, ErrSessionExpired) || !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown session: %v", err)
	}
	s.accounts["anna"].Locked = true
	if _, err := Authenticate(context.Background(), httptest.NewRequest("GET", "/x", nil), []string{model.AuthSession}, s); err != nil {
		t.Errorf("without cookie: %v", err)
	}
	locked := httptest.NewRequest("GET", "/x", nil)
	locked.AddCookie(&http.Cookie{Name: SessionCookie, Value: id})
	if _, err := Authenticate(context.Background(), locked, []string{model.AuthSession}, s); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("locked user: %v", err)
	}
	if got := Challenges([]string{model.AuthBasic, model.AuthSession}); len(got) != 1 {
		t.Errorf("challenges: %v", got)
	}
}
