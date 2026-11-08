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

// Package fakegithub is a GitHub for tests: repositories with tags,
// branches, commits, releases with assets and repository archives behind
// the REST API, private repositories that need a token, rate limits and
// failures.
package fakegithub

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/source/github"
)

// Asset is a file of a release.
type Asset struct {
	Name string
	Data []byte
}

// Repo is a repository.
type Repo struct {
	Private  bool
	Tags     map[string]string // tag → commit
	Branches map[string]string // branch → commit
	Archives map[string][]byte // commit → archive of the repository
	Releases map[string][]Asset
}

// Request is a request the server answered.
type Request struct {
	Path          string
	Authorization string
}

// Server is the fake.
type Server struct {
	*httptest.Server
	Token string // the token that private repositories need

	mu sync.Mutex
	// Repos are the repositories by owner/repo.
	Repos map[string]*Repo
	// RateLimited answers every API request with the rate limit until it is
	// reset to the zero time.
	RateLimited time.Time
	// Failures answers that many requests with 503 first.
	Failures int
	Requests []Request
	// NotModified counts the answers 304 to requests with If-None-Match.
	NotModified int
}

// New starts a server; it stops with the test.
func New(t testing.TB) *Server {
	s := &Server{Repos: map[string]*Repo{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Repo returns the repository owner/repo, created when missing.
func (s *Server) Repo(name string) *Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.Repos[name]
	if r == nil {
		r = &Repo{Tags: map[string]string{}, Branches: map[string]string{}, Archives: map[string][]byte{}, Releases: map[string][]Asset{}}
		s.Repos[name] = r
	}
	return r
}

// Commit adds an archive of the repository and returns its commit hash.
func (r *Repo) Commit(archive []byte) string {
	sum := sha1.Sum(archive)
	c := hex.EncodeToString(sum[:])
	r.Archives[c] = archive
	return c
}

// Config returns the configuration of a source that talks to the server.
func (s *Server) Config() github.Config {
	u, _ := url.Parse(s.URL)
	return github.Config{BaseURL: s.URL, APIURL: s.URL + "/api", Hosts: []string{u.Host}, Client: s.Client(), Sleep: func(time.Duration) {}}
}

// Host returns the host of the addresses the server serves.
func (s *Server) Host() string {
	u, _ := url.Parse(s.URL)
	return u.Host
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, Request{Path: r.URL.Path, Authorization: r.Header.Get("Authorization")})
	if s.Failures > 0 {
		s.Failures--
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	jsonOut := func(v any) {
		b, _ := json.Marshal(v)
		etag := fmt.Sprintf(`"%x"`, sha1.Sum(b))
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			s.NotModified++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
	notFound := func() {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}
	// Downloads behind redirects need no token.
	if rest, ok := strings.CutPrefix(r.URL.Path, "/download/"); ok {
		parts := strings.SplitN(rest, "/", 4) // owner/repo/kind/key
		repo := s.Repos[parts[0]+"/"+parts[1]]
		if repo == nil || len(parts) < 4 {
			notFound()
			return
		}
		switch parts[2] {
		case "archive":
			if b, ok := repo.Archives[parts[3]]; ok {
				w.Write(b)
				return
			}
		case "asset":
			tag, name, _ := strings.Cut(parts[3], "/")
			for _, a := range repo.Releases[tag] {
				if a.Name == name {
					w.Write(a.Data)
					return
				}
			}
		}
		notFound()
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/repos/")
	if !ok {
		notFound()
		return
	}
	if !s.RateLimited.IsZero() {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(s.RateLimited.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		return
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 {
		notFound()
		return
	}
	name := parts[0] + "/" + parts[1]
	repo := s.Repos[name]
	if repo == nil || repo.Private && r.Header.Get("Authorization") != "Bearer "+s.Token {
		notFound()
		return
	}
	op := parts[2]
	switch {
	case op == "tags":
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		type tag struct {
			Name   string `json:"name"`
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		var out []tag
		if page <= 1 {
			for _, n := range slices.Sorted(maps.Keys(repo.Tags)) {
				t := tag{Name: n}
				t.Commit.SHA = repo.Tags[n]
				out = append(out, t)
			}
		}
		jsonOut(out)
	case strings.HasPrefix(op, "commits/"):
		c := strings.TrimPrefix(op, "commits/")
		if _, ok := repo.Archives[c]; !ok {
			notFound()
			return
		}
		jsonOut(map[string]string{"sha": c})
	case strings.HasPrefix(op, "branches/"):
		c, ok := repo.Branches[strings.TrimPrefix(op, "branches/")]
		if !ok {
			notFound()
			return
		}
		jsonOut(map[string]any{"commit": map[string]string{"sha": c}})
	case strings.HasPrefix(op, "releases/tags/"):
		tag := strings.TrimPrefix(op, "releases/tags/")
		assets, ok := repo.Releases[tag]
		if !ok {
			notFound()
			return
		}
		out := []map[string]any{}
		for i, a := range repo.assets() {
			if a.tag == tag {
				out = append(out, map[string]any{"id": i + 1, "name": a.Name})
			}
		}
		_ = assets
		jsonOut(map[string]any{"tag_name": tag, "assets": out})
	case strings.HasPrefix(op, "releases/assets/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(op, "releases/assets/"))
		all := repo.assets()
		if id < 1 || id > len(all) || r.Header.Get("Accept") != "application/octet-stream" {
			notFound()
			return
		}
		http.Redirect(w, r, "/download/"+name+"/asset/"+all[id-1].tag+"/"+all[id-1].Name, http.StatusFound)
	case strings.HasPrefix(op, "zipball/"):
		c := strings.TrimPrefix(op, "zipball/")
		if t, ok := repo.Tags[c]; ok {
			c = t
		}
		if _, ok := repo.Archives[c]; !ok {
			notFound()
			return
		}
		http.Redirect(w, r, "/download/"+name+"/archive/"+c, http.StatusFound)
	default:
		notFound()
	}
}

type taggedAsset struct {
	tag string
	Asset
}

// assets lists the assets of all releases in the order of their tags; the
// position is the asset id.
func (r *Repo) assets() []taggedAsset {
	var out []taggedAsset
	for _, tag := range slices.Sorted(maps.Keys(r.Releases)) {
		for _, a := range r.Releases[tag] {
			out = append(out, taggedAsset{tag, a})
		}
	}
	return out
}
