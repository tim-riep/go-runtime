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

package kernel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// browser is a client with the cookie of a session.
type browser struct {
	c      client
	cookie *http.Cookie
	csrf   string
}

func (b *browser) do(method, path, body string, csrf bool) answer {
	b.c.t.Helper()
	return b.c.do(method, path, func(r *http.Request) {
		if b.cookie != nil {
			r.AddCookie(b.cookie)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", b.csrf)
		}
	}, body)
}

// signIn signs a browser in and keeps the cookie and the CSRF token.
func signIn(t *testing.T, c client, body string) (*browser, answer) {
	t.Helper()
	b := &browser{c: c}
	a := b.do("POST", "/rt/session", body, false)
	for _, ck := range (&http.Response{Header: a.header}).Cookies() {
		if ck.Name == "rt_session" {
			b.cookie = &http.Cookie{Name: ck.Name, Value: ck.Value}
		}
	}
	var info kernel.SessionInfo
	if a.status == 200 {
		if err := json.Unmarshal([]byte(a.body), &info); err != nil {
			t.Fatal(err)
		}
		b.csrf = info.CSRFToken
	}
	return b, a
}

func sessionInfo(t *testing.T, a answer) kernel.SessionInfo {
	t.Helper()
	var info kernel.SessionInfo
	if a.status != 200 || json.Unmarshal([]byte(a.body), &info) != nil {
		t.Fatalf("session: %d %s", a.status, a.body)
	}
	return info
}

func TestWebSessions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		var mu sync.Mutex
		now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
		k.SetClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return now })
		advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
		k.SetSessionSettings(kernel.SessionSettings{IdleTimeout: 30 * time.Minute, Lifetime: 2 * time.Hour})
		c := httpKernel(t, k)
		if err := k.ActivateHTTPService(ctx, "/trhttp"); err != nil {
			t.Fatal(err)
		}
		if err := k.Users().AddTenant(ctx, "anna", "BETA"); err != nil {
			t.Fatal(err)
		}

		// Wrong password, then the sign-in with its cookie.
		if _, a := signIn(t, c, `{"user":"anna","password":"wrong"}`); a.status != 401 || a.header.Get("WWW-Authenticate") != "" {
			t.Fatalf("wrong password: %d %v", a.status, a.header)
		}
		if u, _ := k.Users().Get(ctx, "anna"); u.FailedLogins != 1 {
			t.Errorf("failed logins: %d", u.FailedLogins)
		}
		if _, a := signIn(t, c, `{"user":"anna","password":"secret","tenant":"GAMMA"}`); a.status != 403 {
			t.Errorf("tenant without assignment: %d", a.status)
		}
		b, a := signIn(t, c, `{"user":"anna","password":"secret"}`)
		info := sessionInfo(t, a)
		if info.User != "anna" || info.Tenant.Or("") != "ACME" || info.Language != "en" || info.Name.Valid() || len(info.Languages) != 2 ||
			strings.Join(info.Tenants, ",") != "ACME,BETA" || info.CSRFToken == "" {
			t.Errorf("info: %+v", info)
		}
		set := a.header.Get("Set-Cookie")
		for _, attr := range []string{"HttpOnly", "Secure", "SameSite=Strict", "Path=/"} {
			if !strings.Contains(set, attr) {
				t.Errorf("cookie lacks %s: %s", attr, set)
			}
		}
		if _, a := signIn(t, c, `{"user":"anna","password":"secret"}`); a.status != 200 {
			t.Fatal("second session")
		}
		if u, _ := k.Users().Get(ctx, "anna"); u.FailedLogins != 0 {
			t.Errorf("failed logins after the sign-in: %d", u.FailedLogins)
		}
		if a := b.c.do("POST", "/rt/session", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, `{"user":"anna","password":"secret"}`); a.status != 400 {
			t.Errorf("sign-in without JSON: %d", a.status)
		}

		// The cookie signs in requests; tenant and language come from the
		// session unless the request names them.
		a = b.do("GET", "/trhttp/echo", "", false)
		if a.status != 200 || !strings.Contains(a.body, "user=anna tenant=ACME lang=en") {
			t.Fatalf("echo: %d %s", a.status, a.body)
		}
		// CSRF: changing requests need the token of the session.
		if a := b.do("POST", "/trhttp/echo", "", false); a.status != 403 || !strings.Contains(a.body, "csrf-token-invalid") {
			t.Errorf("POST without token: %d %s", a.status, a.body)
		}
		b2 := *b
		b2.csrf = "wrong"
		if a := b2.do("POST", "/trhttp/echo", "", true); a.status != 403 {
			t.Errorf("POST with a wrong token: %d", a.status)
		}
		if a := b.do("POST", "/trhttp/echo", "", true); a.status != 200 {
			t.Errorf("POST with the token: %d %s", a.status, a.body)
		}
		// Basic authentication needs no token.
		if a := c.do("POST", "/trhttp/echo", basicAuth("anna", "secret"), ""); a.status != 200 {
			t.Errorf("POST with basic: %d", a.status)
		}

		// Tenant and language change within the session.
		if a := b.do("PATCH", "/rt/session", `{"tenant":"BETA","language":"de"}`, false); a.status != 403 {
			t.Errorf("PATCH without token: %d", a.status)
		}
		if a := b.do("PATCH", "/rt/session", `{"tenant":"GAMMA"}`, true); a.status != 403 {
			t.Errorf("tenant without assignment: %d", a.status)
		}
		if a := b.do("PATCH", "/rt/session", `{"language":"fr"}`, true); a.status != 400 {
			t.Errorf("language not set up: %d", a.status)
		}
		info = sessionInfo(t, b.do("PATCH", "/rt/session", `{"tenant":"BETA","language":"de"}`, true))
		if info.Tenant.Or("") != "BETA" || info.Language != "de" {
			t.Errorf("after PATCH: %+v", info)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRHTTP/CALLER", Tenant: "BETA"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		if a := b.do("GET", "/trhttp/echo", "", false); !strings.Contains(a.body, "tenant=BETA lang=de") {
			t.Errorf("echo after PATCH: %s", a.body)
		}
		if a := b.do("GET", "/trhttp/echo?lang=en", "", false); !strings.Contains(a.body, "tenant=BETA lang=en") {
			t.Errorf("language of the request: %s", a.body)
		}

		// Inactivity ends a session; use within the idle time keeps it.
		advance(20 * time.Minute)
		sessionInfo(t, b.do("GET", "/rt/session", "", false))
		advance(20 * time.Minute)
		sessionInfo(t, b.do("GET", "/rt/session", "", false))
		advance(31 * time.Minute)
		a = b.do("GET", "/trhttp/echo", "", false)
		if a.status != 401 || !strings.Contains(a.body, "session-expired") {
			t.Errorf("idle: %d %s", a.status, a.body)
		}
		if a := b.do("GET", "/rt/session", "", false); a.status != 401 {
			t.Errorf("session after idle: %d", a.status)
		}

		// The absolute lifetime ends a session that is in use.
		b, _ = signIn(t, c, `{"user":"anna","password":"secret"}`)
		for range 4 {
			advance(25 * time.Minute)
			sessionInfo(t, b.do("GET", "/rt/session", "", false))
		}
		advance(25 * time.Minute)
		if a := b.do("GET", "/rt/session", "", false); a.status != 401 {
			t.Errorf("after the lifetime: %d", a.status)
		}

		// Sign-out ends the session and expires the cookie.
		b, _ = signIn(t, c, `{"user":"anna","password":"secret"}`)
		if a := b.do("DELETE", "/rt/session", "", false); a.status != 403 {
			t.Errorf("DELETE without token: %d", a.status)
		}
		a = b.do("DELETE", "/rt/session", "", true)
		if a.status != 204 || !strings.Contains(a.header.Get("Set-Cookie"), "Max-Age=0") {
			t.Errorf("sign-out: %d %v", a.status, a.header)
		}
		if a := b.do("GET", "/rt/session", "", false); a.status != 401 {
			t.Errorf("after sign-out: %d", a.status)
		}

		// Locking the user ends its sessions.
		b, _ = signIn(t, c, `{"user":"anna","password":"secret"}`)
		list, err := k.WebSessions(ctx, "anna")
		if err != nil || len(list) == 0 {
			t.Fatalf("sessions: %v %v", list, err)
		}
		if err := k.Users().Lock(ctx, "anna"); err != nil {
			t.Fatal(err)
		}
		if a := b.do("GET", "/trhttp/echo", "", false); a.status != 401 {
			t.Errorf("after the lock: %d", a.status)
		}
		if list, _ := k.WebSessions(ctx, "anna"); len(list) != 0 {
			t.Errorf("sessions after the lock: %d", len(list))
		}
		if err := k.Users().Unlock(ctx, "anna"); err != nil {
			t.Fatal(err)
		}
		// A password change ends them as well.
		b, _ = signIn(t, c, `{"user":"anna","password":"secret"}`)
		if err := k.Users().SetPassword(ctx, "anna", "other"); err != nil {
			t.Fatal(err)
		}
		if a := b.do("GET", "/rt/session", "", false); a.status != 401 {
			t.Errorf("after the password change: %d", a.status)
		}
		if a := b.do("PUT", "/rt/session", "", false); a.status != 405 {
			t.Errorf("PUT: %d", a.status)
		}
	})
}

func TestWebSessionInsecureCookie(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		k.SetSessionSettings(kernel.SessionSettings{InsecureCookie: true})
		c := httpKernel(t, k)
		_, a := signIn(t, c, `{"user":"anna","password":"secret","language":"de"}`)
		if a.status != 200 || strings.Contains(a.header.Get("Set-Cookie"), "Secure") {
			t.Errorf("insecure cookie: %d %s", a.status, a.header.Get("Set-Cookie"))
		}
		if info := sessionInfo(t, a); info.Language != "de" || info.Tenant != model.Some("ACME") {
			t.Errorf("language of the sign-in: %+v", info)
		}
	})
}
