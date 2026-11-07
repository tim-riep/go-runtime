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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

type memStore struct {
	accounts map[string]*Account
	tokens   map[string]*Token
	failed   map[string]int
	now      time.Time
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
