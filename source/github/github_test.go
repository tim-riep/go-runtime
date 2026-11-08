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

package github_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil/fakegithub"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/source"
	"github.com/tim-riep/go-runtime/source/github"
)

const token = "ghp_secret_token_1234567890"

func setup(t *testing.T) (*fakegithub.Server, *github.GitHub, func(string) source.Address) {
	t.Helper()
	srv := fakegithub.New(t)
	fin := srv.Repo("tim-riep/trfin")
	c1, c2, c3 := fin.Commit([]byte("archive 1.0.0")), fin.Commit([]byte("archive 1.1.0")), fin.Commit([]byte("archive main"))
	fin.Tags["v1.0.0"], fin.Tags["v1.1.0"], fin.Tags["v2.0.0-rc.1"] = c1, c2, c3
	fin.Branches["main"] = c3
	fin.Releases["v1.1.0"] = []fakegithub.Asset{{Name: "TRFIN-1.1.0.zip", Data: []byte("asset 1.1.0")}, {Name: "notes.txt", Data: []byte("n")}}
	multi := srv.Repo("tim-riep/erp")
	m := multi.Commit([]byte("erp archive"))
	multi.Tags["v2.0.0"] = m
	multi.Releases["v2.0.0"] = []fakegithub.Asset{{Name: "TRFIN-2.0.0.zip", Data: []byte("fin")}, {Name: "TROTTO-2.0.0.zip", Data: []byte("otto")}}
	priv := srv.Repo("tim-riep/secret")
	priv.Private = true
	priv.Tags["v1.0.0"] = priv.Commit([]byte("secret"))
	srv.Token = token
	c := srv.Config()
	c.Token = token
	c.CacheDir = t.TempDir()
	g, err := github.New(c)
	if err != nil {
		t.Fatal(err)
	}
	addr := func(s string) source.Address {
		a, err := source.Parse(s, srv.Host())
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	return srv, g, addr
}

// TestResolveAndFetch resolves versions, constraints, commits and
// branches and fetches release assets before repository archives.
func TestResolveAndFetch(t *testing.T) {
	srv, g, addr := setup(t)
	ctx := context.Background()
	h := srv.Host()
	fetch := func(s string, allowBranch bool, pkg string) (source.Resolved, source.Archive) {
		t.Helper()
		r, err := source.Resolve(ctx, g, addr(s), allowBranch)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		a, err := g.Fetch(ctx, r, pkg, 1<<20)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		return r, a
	}
	r, a := fetch(h+"/tim-riep/trfin@^1.0", false, "")
	if r.Tag != "v1.1.0" || a.Kind != source.Asset || a.Name != "TRFIN-1.1.0.zip" || string(a.Data) != "asset 1.1.0" || a.SHA256 != source.SHA256(a.Data) {
		t.Errorf("asset: %+v %+v", r, a)
	}
	if r, a := fetch(h+"/tim-riep/trfin@v1.0.0", false, ""); r.Version != "1.0.0" || a.Kind != source.SourceArchive || string(a.Data) != "archive 1.0.0" {
		t.Errorf("archive: %+v %+v", r, a)
	}
	if r, _ := fetch(h+"/tim-riep/trfin", false, ""); r.Tag != "v1.1.0" {
		t.Errorf("latest stable: %+v", r)
	}
	if r, a := fetch(h+"/tim-riep/trfin@"+srv.Repo("tim-riep/trfin").Tags["v1.0.0"], false, ""); r.Tag != "" || string(a.Data) != "archive 1.0.0" {
		t.Errorf("commit: %+v %+v", r, a)
	}
	if _, err := source.Resolve(ctx, g, addr(h+"/tim-riep/trfin@main"), false); err == nil || !strings.Contains(err.Error(), "--allow-branch") {
		t.Errorf("branch without permission: %v", err)
	}
	if r, a := fetch(h+"/tim-riep/trfin@main", true, ""); string(a.Data) != "archive main" || r.Commit != srv.Repo("tim-riep/trfin").Branches["main"] {
		t.Errorf("branch: %+v", r)
	}
	// Several package assets: the directory or the package chooses.
	if _, a := fetch(h+"/tim-riep/erp/packages/TROTTO@v2.0.0", false, ""); string(a.Data) != "otto" {
		t.Errorf("by directory: %q", a.Data)
	}
	if _, a := fetch(h+"/tim-riep/erp@v2.0.0", false, "TRFIN"); string(a.Data) != "fin" {
		t.Errorf("by package: %q", a.Data)
	}
	r, _ = source.Resolve(ctx, g, addr(h+"/tim-riep/erp@v2.0.0"), false)
	if _, err := g.Fetch(ctx, r, "", 1<<20); err == nil || !strings.Contains(err.Error(), "TRFIN-2.0.0.zip, TROTTO-2.0.0.zip") {
		t.Errorf("ambiguous: %v", err)
	}
	// The private repository needs the token; downloads never get it.
	if r, a := fetch(h+"/tim-riep/secret@v1.0.0", false, ""); string(a.Data) != "secret" || r.Tag != "v1.0.0" {
		t.Errorf("private: %+v", a)
	}
	for _, q := range srv.Requests {
		if strings.HasPrefix(q.Path, "/download/") && q.Authorization != "" {
			t.Errorf("token sent to %s", q.Path)
		}
	}
	if _, err := g.Fetch(ctx, r, "", 5); err == nil {
		t.Error("archive above the limit")
	}
}

// TestFailures reports private repositories without token, rate limits
// with their reset, retries on 503 and keeps the token out of every error
// and log line.
func TestFailures(t *testing.T) {
	srv, g, addr := setup(t)
	ctx := context.Background()
	h := srv.Host()
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	anon := srv.Config()
	ga, _ := github.New(anon)
	_, err := source.Resolve(ctx, ga, addr(h+"/tim-riep/secret@v1.0.0"), false)
	var nf *rterr.NotFound
	if !errors.As(err, &nf) || !strings.Contains(err.Error(), "need a token") {
		t.Errorf("private without token: %v", err)
	}
	srv.Failures = 2
	if _, err := g.Tags(ctx, addr(h+"/tim-riep/trfin")); err != nil {
		t.Errorf("after two failures: %v", err)
	}
	srv.Failures = 10
	_, err = g.Tags(ctx, addr(h+"/tim-riep/trfin"))
	var se *rterr.SourceError
	if !errors.As(err, &se) || se.Status != 503 {
		t.Errorf("lasting failure: %v", err)
	}
	srv.Failures = 0
	reset := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	srv.RateLimited = reset
	_, err = g.Tags(ctx, addr(h+"/tim-riep/trfin"))
	var rl *rterr.RateLimited
	if !errors.As(err, &rl) || !rl.Reset.Equal(reset) || !strings.Contains(err.Error(), "2026-11-08T12:00:00Z") {
		t.Errorf("rate limit: %v", err)
	}
	srv.RateLimited = time.Time{}
	if _, err := g.Tags(ctx, addr(h+"/tim-riep/none")); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("missing repository: %v", err)
	}
	// Hosts that are not allowed are not reached.
	c := srv.Config()
	c.Hosts = []string{"example.com"}
	gb, _ := github.New(c)
	if _, err := gb.Tags(ctx, addr(h+"/tim-riep/trfin")); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("host: %v", err)
	}
	if strings.Contains(logs.String(), token) {
		t.Error("the token is in the log")
	}
	if _, err := github.New(github.Config{BaseURL: "http://github.com"}); err == nil {
		t.Error("http accepted")
	}
}

// TestETag answers the second request for tags from the cache with 304.
func TestETag(t *testing.T) {
	srv, g, addr := setup(t)
	ctx := context.Background()
	a := addr(srv.Host() + "/tim-riep/trfin")
	first, err := g.Tags(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	n := len(srv.Requests)
	second, err := g.Tags(ctx, a)
	if err != nil || len(second) != len(first) || len(srv.Requests) != n+1 || srv.NotModified != 1 {
		t.Fatalf("second: %v %v", second, err)
	}
}
