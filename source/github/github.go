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

// Package github is the package source for GitHub and GitHub Enterprise
// Server: tags, release assets and repository archives through the REST
// API, over HTTPS only, with a token for private repositories, retries,
// rate limits and an ETag cache for API answers.
package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/source"
)

// Config configures the source.
type Config struct {
	// BaseURL is the web address, https://github.com or that of a GitHub
	// Enterprise Server.
	BaseURL string
	// APIURL is the address of the REST API; empty derives it:
	// https://api.github.com for github.com, else <BaseURL>/api/v3.
	APIURL string
	// Token is sent to the API only, never to download hosts; it is never
	// stored, logged or shown.
	Token string
	// Hosts are the hosts that requests and redirects may reach; empty
	// derives them from BaseURL.
	Hosts []string
	// Client sends the requests; nil means a client with Timeout.
	Client *http.Client
	// CacheDir keeps API answers with their ETag; "" keeps none.
	CacheDir string
	// Timeout limits a request to the API, DownloadTimeout a download;
	// zero means 30 seconds and 5 minutes.
	Timeout, DownloadTimeout time.Duration
	// Retries are made on 502, 503, 504 and network errors, with a pause
	// that doubles from one second; zero means 3, a negative value none.
	Retries int
	// Sleep waits between retries; tests replace it.
	Sleep func(time.Duration)
}

// DefaultHosts are the hosts of github.com: web, API and downloads.
var DefaultHosts = []string{"github.com", "api.github.com", "codeload.github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// GitHub is the package source.
type GitHub struct {
	c        Config
	web, api *url.URL
}

// New returns the source for a configuration.
func New(c Config) (*GitHub, error) {
	if c.BaseURL == "" {
		c.BaseURL = "https://github.com"
	}
	web, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/"))
	if err != nil || web.Scheme != "https" || web.Host == "" {
		return nil, &rterr.InvalidArgument{Argument: "GitHub address", Message: fmt.Sprintf("%q is no https address", c.BaseURL)}
	}
	if c.APIURL == "" {
		if web.Host == "github.com" {
			c.APIURL = "https://api.github.com"
		} else {
			c.APIURL = web.String() + "/api/v3"
		}
	}
	api, err := url.Parse(strings.TrimSuffix(c.APIURL, "/"))
	if err != nil || api.Scheme != "https" {
		return nil, &rterr.InvalidArgument{Argument: "GitHub API address", Message: fmt.Sprintf("%q is no https address", c.APIURL)}
	}
	if len(c.Hosts) == 0 {
		if web.Host == "github.com" {
			c.Hosts = DefaultHosts
		} else {
			c.Hosts = []string{web.Host, api.Host}
		}
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.DownloadTimeout == 0 {
		c.DownloadTimeout = 5 * time.Minute
	}
	if c.Retries == 0 {
		c.Retries = 3
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	g := &GitHub{c: c, web: web, api: api}
	base := c.Client
	if base == nil {
		base = &http.Client{}
	}
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if err := g.allowed(req.URL); err != nil {
			return err
		}
		// Download hosts get signed addresses, not the token.
		req.Header.Del("Authorization")
		return nil
	}
	g.c.Client = &client
	return g, nil
}

// Host returns the host of the addresses the source serves.
func (g *GitHub) Host() string { return g.web.Host }

// Handles implements source.Source.
func (g *GitHub) Handles(a source.Address) bool { return a.Host == g.web.Host }

func (g *GitHub) allowed(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("only https is allowed, not %s", u.Scheme)
	}
	if !slices.Contains(g.c.Hosts, u.Hostname()) && !slices.Contains(g.c.Hosts, u.Host) {
		return fmt.Errorf("the host %s is not allowed", u.Host)
	}
	return nil
}

// redact removes the token from a text.
func (g *GitHub) redact(s string) string {
	if g.c.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, g.c.Token, "***")
}

// cached is an API answer with its ETag.
type cached struct {
	ETag string `json:"etag"`
	Body []byte `json:"body"`
}

func (g *GitHub) cacheFile(u string) string {
	sum := sha256.Sum256([]byte(u))
	return filepath.Join(g.c.CacheDir, "api", hex.EncodeToString(sum[:])+".json")
}

// get sends a GET request with retries. API requests carry the token and
// use the ETag cache; downloads read at most max bytes.
func (g *GitHub) get(ctx context.Context, u string, accept string, download bool, max int64) ([]byte, int, error) {
	target, err := url.Parse(u)
	if err != nil {
		return nil, 0, err
	}
	shown := target.Scheme + "://" + target.Host + target.Path
	fail := func(status int, detail string) error {
		return &rterr.SourceError{Method: "GET", URL: shown, Status: status, Detail: g.redact(detail)}
	}
	if err := g.allowed(target); err != nil {
		return nil, 0, fail(0, err.Error())
	}
	var etag *cached
	if !download && g.c.CacheDir != "" {
		if b, err := os.ReadFile(g.cacheFile(u)); err == nil {
			var c cached
			if json.Unmarshal(b, &c) == nil {
				etag = &c
			}
		}
	}
	timeout := g.c.Timeout
	if download {
		timeout = g.c.DownloadTimeout
	}
	pause := time.Second
	for attempt := 0; ; attempt++ {
		rctx, cancel := context.WithTimeout(ctx, timeout)
		req, _ := http.NewRequestWithContext(rctx, http.MethodGet, u, nil)
		req.Header.Set("Accept", accept)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if g.c.Token != "" && target.Host == g.api.Host && strings.HasPrefix(target.Path, g.api.Path) {
			req.Header.Set("Authorization", "Bearer "+g.c.Token)
		}
		if etag != nil {
			req.Header.Set("If-None-Match", etag.ETag)
		}
		resp, err := g.c.Client.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil || attempt >= g.c.Retries || g.c.Retries < 0 {
				return nil, 0, fail(0, g.redact(err.Error()))
			}
			g.c.Sleep(pause)
			pause *= 2
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, max+1))
		resp.Body.Close()
		cancel()
		switch {
		case resp.StatusCode == http.StatusNotModified && etag != nil:
			return etag.Body, http.StatusOK, nil
		case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout:
			if attempt < g.c.Retries && g.c.Retries > 0 {
				g.c.Sleep(pause)
				pause *= 2
				continue
			}
			return nil, resp.StatusCode, fail(resp.StatusCode, "")
		case limited(resp):
			return nil, resp.StatusCode, &rterr.RateLimited{Host: target.Host, Reset: resetOf(resp)}
		case rerr != nil:
			return nil, 0, fail(0, rerr.Error())
		case int64(len(body)) > max:
			return nil, resp.StatusCode, &rterr.UnsafeArchive{Archive: shown, Reason: fmt.Sprintf("it has more than the limit of %d bytes", max)}
		case resp.StatusCode != http.StatusOK:
			return nil, resp.StatusCode, fail(resp.StatusCode, message(body))
		}
		if !download && g.c.CacheDir != "" {
			if e := resp.Header.Get("ETag"); e != "" {
				if b, err := json.Marshal(cached{ETag: e, Body: body}); err == nil {
					p := g.cacheFile(u)
					if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
						_ = os.WriteFile(p, b, 0o644)
					}
				}
			}
		}
		return body, resp.StatusCode, nil
	}
}

// limited reports an answer of the rate limit.
func limited(resp *http.Response) bool {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return false
	}
	return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
}

func resetOf(resp *http.Response) time.Time {
	if s, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		return time.Unix(s, 0).UTC()
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		return time.Now().Add(time.Duration(s) * time.Second).UTC()
	}
	return time.Now().UTC()
}

// message returns the message of an error answer of the API.
func message(body []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &m) == nil {
		return m.Message
	}
	return ""
}

const maxAPI = 8 << 20

func (g *GitHub) repoURL(a source.Address, rest string) string {
	return g.api.String() + "/repos/" + url.PathEscape(a.Owner) + "/" + url.PathEscape(a.Repo) + rest
}

// apiJSON reads an API answer; 404 becomes NotFound with a hint on
// private repositories.
func (g *GitHub) apiJSON(ctx context.Context, a source.Address, rest string, v any) (bool, error) {
	body, status, err := g.get(ctx, g.repoURL(a, rest), "application/vnd.github+json", false, maxAPI)
	if status == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(body, v)
}

func (g *GitHub) notFound(a source.Address, what string) error {
	kind := what + " of"
	if g.c.Token == "" {
		kind = what + " (private repositories need a token in RT_GITHUB_TOKEN) of"
	}
	return &rterr.NotFound{Kind: kind, Name: a.Repository()}
}

// Tags implements source.Source.
func (g *GitHub) Tags(ctx context.Context, a source.Address) ([]source.Tag, error) {
	var out []source.Tag
	for page := 1; page <= 50; page++ {
		var tags []struct {
			Name   string `json:"name"`
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		found, err := g.apiJSON(ctx, a, fmt.Sprintf("/tags?per_page=100&page=%d", page), &tags)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, g.notFound(a, "repository")
		}
		for _, t := range tags {
			out = append(out, source.Tag{Name: t.Name, Commit: t.Commit.SHA})
		}
		if len(tags) < 100 {
			break
		}
	}
	return out, nil
}

// Commit implements source.Source.
func (g *GitHub) Commit(ctx context.Context, a source.Address, ref string) (string, error) {
	if a.Spec.Kind == source.Branch {
		var b struct {
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		found, err := g.apiJSON(ctx, a, "/branches/"+url.PathEscape(ref), &b)
		if err != nil {
			return "", err
		}
		if !found || b.Commit.SHA == "" {
			return "", g.notFound(a, "branch "+ref)
		}
		return b.Commit.SHA, nil
	}
	var c struct {
		SHA string `json:"sha"`
	}
	found, err := g.apiJSON(ctx, a, "/commits/"+url.PathEscape(ref), &c)
	if err != nil {
		return "", err
	}
	if !found || c.SHA != ref {
		return "", g.notFound(a, "commit "+ref)
	}
	return c.SHA, nil
}

// Fetch implements source.Source.
func (g *GitHub) Fetch(ctx context.Context, r source.Resolved, pkg string, max int64) (source.Archive, error) {
	a := r.Address
	if pkg == "" {
		pkg = source.PackageOf(a)
	}
	if r.Tag != "" {
		var rel struct {
			Assets []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"assets"`
		}
		found, err := g.apiJSON(ctx, a, "/releases/tags/"+url.PathEscape(r.Tag), &rel)
		if err != nil {
			return source.Archive{}, err
		}
		if found {
			var names []string
			for _, as := range rel.Assets {
				names = append(names, as.Name)
			}
			name, err := source.ChooseAsset(names, r.Version, pkg)
			if err != nil {
				return source.Archive{}, err
			}
			for _, as := range rel.Assets {
				if as.Name == name {
					data, _, err := g.get(ctx, g.repoURL(a, fmt.Sprintf("/releases/assets/%d", as.ID)), "application/octet-stream", true, max)
					if err != nil {
						return source.Archive{}, err
					}
					return source.Archive{Kind: source.Asset, Name: name, Data: data, SHA256: source.SHA256(data)}, nil
				}
			}
		}
	}
	data, status, err := g.get(ctx, g.repoURL(a, "/zipball/"+url.PathEscape(r.Commit)), "application/vnd.github+json", true, max)
	if status == http.StatusNotFound {
		return source.Archive{}, g.notFound(a, "commit "+r.Commit)
	}
	if err != nil {
		return source.Archive{}, err
	}
	return source.Archive{Kind: source.SourceArchive, Name: r.Commit + ".zip", Data: data, SHA256: source.SHA256(data)}, nil
}
