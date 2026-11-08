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
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/internal/httpsvc"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/kernelpkg/natives"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/vm"
)

// httpState is the activation of the service nodes at one generation of
// the activation.
type httpState struct {
	n      int64
	states map[string]httpsvc.State
	// services are the active services; kernel the settings operators
	// gave the service nodes of the kernel.
	services map[string]bool
	kernel   map[string]repo.KernelNodeSettings
}

// HTTPHandler returns the http.Handler of the http services of the kernel.
// Every request reads the generations of code and activation, so
// installations, upgrades and activations take effect without restart; a
// request keeps the generation it started with.
func (k *Kernel) HTTPHandler() http.Handler {
	return http.HandlerFunc(k.serveHTTP)
}

// treeOf returns the tree of service nodes of a generation.
func (g *generation) treeOf() *httpsvc.Tree {
	g.treeOnce.Do(func() { g.tree = httpsvc.Build(g.schema) })
	return g.tree
}

// activation returns the activation of service nodes and services and the
// settings of the nodes of the kernel, cached until the next change.
func (k *Kernel) activation(ctx context.Context) (*httpState, error) {
	n, err := k.repo.HTTPGeneration(ctx, k.db)
	if err != nil {
		return nil, err
	}
	if s := k.httpStates.Load(); s != nil && s.n == n {
		return s, nil
	}
	stored, err := k.repo.HTTPStates(ctx, k.db)
	if err != nil {
		return nil, err
	}
	svcs, err := k.repo.ServiceStates(ctx, k.db)
	if err != nil {
		return nil, err
	}
	kernel, err := k.repo.KernelNodeSettings(ctx, k.db)
	if err != nil {
		return nil, err
	}
	s := &httpState{n: n, states: states(stored), services: map[string]bool{}, kernel: kernel}
	for name, st := range svcs {
		s.services[name] = st.Active
	}
	k.httpStates.Store(s)
	return s, nil
}

// settingsOf returns the settings of a node: for nodes of the kernel with
// the changes of operators, never anonymous.
func (s *httpState) settingsOf(n *httpsvc.Node) httpsvc.Settings {
	set := n.Settings
	if n.Package == model.Kernel {
		if ks, ok := s.kernel[n.Path]; ok {
			set = httpsvc.Override(set, ks.Settings)
		}
		set.Anonymous, set.AnonymousTenant = false, model.None[string]()
	}
	return set
}

// accessEntry is the line of the access log of a request.
type accessEntry struct {
	user, tenant, node, handler string
	status                      int
	size                        int
}

func (k *Kernel) serveHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	entry := &accessEntry{}
	defer func() {
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "user", entry.user, "tenant", entry.tenant, "node", entry.node,
			"handler", entry.handler, "status", entry.status, "duration", time.Since(start), "bytes", entry.size)
	}()
	ctx := r.Context()
	lang := model.DefaultLanguage
	fail := func(status int, code string) {
		entry.status = status
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(status)
		body, _ := json.Marshal(map[string]string{"code": code, "message": httpText(code, lang)})
		entry.size, _ = w.Write(body)
	}
	if err := k.requireInit(ctx, k.db); err != nil {
		slog.Error("http services are not available", "error", err)
		fail(http.StatusServiceUnavailable, httpsvc.CodeUnavailable)
		return
	}
	g, err := k.generation(ctx)
	if err != nil {
		slog.Error("loading the code", "error", err)
		fail(http.StatusInternalServerError, httpsvc.CodeInternal)
		return
	}
	st, err := k.activation(ctx)
	if err != nil {
		slog.Error("loading the activation of service nodes", "error", err)
		fail(http.StatusInternalServerError, httpsvc.CodeInternal)
		return
	}
	langs, err := k.repo.LanguageCodes(ctx, k.db)
	if err != nil {
		fail(http.StatusInternalServerError, httpsvc.CodeInternal)
		return
	}
	node, rest := g.treeOf().Match(r.URL.Path)
	if node != nil {
		lang = httpsvc.Language(r, node.Settings, nil, langs)
	}
	if node == nil || !httpsvc.Reachable(node, st.states) || len(node.Handlers()) == 0 {
		fail(http.StatusNotFound, httpsvc.CodeNotFound)
		return
	}
	entry.node = node.Path
	set := st.settingsOf(node)

	user, err := k.Authenticate(ctx, r, set.Authentication)
	if err != nil && !errors.Is(err, auth.ErrInvalid) {
		slog.Error("authentication", "error", err)
		fail(http.StatusInternalServerError, httpsvc.CodeInternal)
		return
	}
	if err != nil || user == nil && !set.Anonymous {
		for _, c := range auth.Challenges(set.Authentication) {
			w.Header().Add("WWW-Authenticate", c)
		}
		fail(http.StatusUnauthorized, httpsvc.CodeUnauthorized)
		return
	}
	userName := model.None[string]()
	if user != nil {
		userName = model.Some(user.Name)
		entry.user = user.Name
	}
	lang = httpsvc.Language(r, set, user, langs)
	tenant, err := httpsvc.Tenant(r, set, user)
	var he *httpsvc.Error
	if errors.As(err, &he) {
		fail(he.Status, he.Code)
		return
	}
	entry.tenant = tenant.Or("")
	if u, ok := userName.Get(); ok && tenant.Valid() {
		k.noteAllAuthorizations(ctx, u, tenant.Or(""))
	}
	// Nodes with sign-in check the node and their additional check before
	// the first handler.
	if user != nil && !set.Anonymous {
		allowed, err := k.checkNode(ctx, node, user.Name, tenant.Or(""))
		if err != nil {
			slog.Error("checking the authorization of a request", "node", node.Path, "error", err)
			fail(http.StatusInternalServerError, httpsvc.CodeInternal)
			return
		}
		if !allowed {
			fail(http.StatusForbidden, httpsvc.CodeForbidden)
			return
		}
	}

	if node.Package == model.Kernel {
		entry.handler = node.Handlers()[0].Class
		k.serveKernelNode(w, r, &kernelRequest{g: g, st: st, node: node, rest: rest, set: set, user: user, tenant: tenant, lang: lang, entry: entry})
		return
	}
	req := &natives.HTTPRequest{R: r, Path: node.Path, Rest: rest, Package: node.Package, MaxBody: set.MaxBodySize}
	resp := natives.NewHTTPResponse(set.MaxResponseSize)
	handled := false
	opts := ExecOptions{Tenant: tenant.Or(""), Language: lang, User: userName, Timeout: set.Timeout, VM: vm.Options{Budget: set.Budget, Log: k.requestLog(node.Path)},
		Rollback: func() bool { return !handled || resp.Status >= 400 }}
	err = k.executeWith(ctx, g, opts, func(ctx context.Context, m *vm.Machine) error {
		reqV, err := m.NewNative("/TRBASE/HTTP_REQUEST", req)
		if err != nil {
			return err
		}
		respV, err := m.NewNative("/TRBASE/HTTP_RESPONSE", resp)
		if err != nil {
			return err
		}
		for _, h := range node.Handlers() {
			entry.handler = h.Class
			req.Params = h.Parameters
			obj, err := m.NewObject(ctx, h.Class)
			if err != nil {
				return err
			}
			v, err := m.Invoke(ctx, obj, "HANDLE", reqV, respV)
			if err != nil {
				return err
			}
			if v.AsBool() {
				handled = true
				return nil
			}
		}
		return nil
	})
	var ex *vm.Exception
	var le *vm.LimitError
	switch {
	case errors.As(err, &ex) && ex.Class == model.NotAuthorizedClass:
		fail(http.StatusForbidden, httpsvc.CodeForbidden)
		return
	case errors.As(err, &ex) && ex.Class == "/RT/INPUT_TOO_LARGE" && req.TooLarge:
		fail(http.StatusRequestEntityTooLarge, httpsvc.CodeTooLarge)
		return
	case errors.As(err, &le) && (le.Limit == "time limit" || le.Limit == "canceled"):
		slog.Warn("a request exceeded its time limit", "node", node.Path, "handler", entry.handler, "error", err)
		fail(http.StatusServiceUnavailable, httpsvc.CodeUnavailable)
		return
	case err != nil:
		slog.Error("a request failed", "node", node.Path, "handler", entry.handler, "user", entry.user, "tenant", entry.tenant, "error", err)
		fail(http.StatusInternalServerError, httpsvc.CodeInternal)
		return
	case !handled:
		fail(http.StatusNotFound, httpsvc.CodeNotHandled)
		return
	}
	entry.status = resp.Status
	for name, vals := range resp.Header {
		w.Header()[name] = vals
	}
	if resp.Serve != nil {
		entry.size = int(resp.Serve(r.Context(), w))
		return
	}
	if w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", strconv.Itoa(resp.Body.Len()))
	}
	w.WriteHeader(resp.Status)
	if r.Method != http.MethodHead {
		entry.size, _ = w.Write(resp.Body.Bytes())
	}
}

// requestLog returns the sink for the log statements of a request.
func (k *Kernel) requestLog(node string) vm.LogSink {
	if k.httpLog != nil {
		return k.httpLog
	}
	return slogSink{node}
}

// slogSink writes log statements to slog with the service node.
type slogSink struct{ node string }

func (s slogSink) Log(level, text string) {
	l := slog.LevelInfo
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	}
	slog.Log(context.Background(), l, text, "node", s.node)
}

// HTTPServiceObject is the authorization object of the kernel package
// TRBASE that requests with sign-in need for a service node.
const HTTPServiceObject = "/TRBASE/HTTP_SERVICE"

// nodeChecks returns the checks of a request to a node: EXECUTE on the
// node, then its additional check.
func nodeChecks(node *httpsvc.Node) []authorization.Check {
	out := []authorization.Check{{Object: HTTPServiceObject,
		Values: map[string]string{"PACKAGE_ID": string(node.Package), "SERVICE_PATH": node.Path, model.ActivityField: "EXECUTE"}}}
	if a := node.Settings.Authorization; a != nil {
		c := authorization.Check{Object: a.Object, Values: map[string]string{}}
		for name, v := range a.Fields {
			if v.Any {
				if c.Any == nil {
					c.Any = map[string]bool{}
				}
				c.Any[name] = true
			} else {
				c.Values[name] = v.Value
			}
		}
		out = append(out, c)
	}
	return out
}

// checkNode reports whether a user may call a node in a tenant.
func (k *Kernel) checkNode(ctx context.Context, node *httpsvc.Node, user, tenant string) (bool, error) {
	e, err := k.EffectiveAuthorizations(ctx, user, tenant, k.now())
	if err != nil {
		return false, err
	}
	for _, c := range nodeChecks(node) {
		ok := e.Check(c)
		if !ok || e.Tracing(k.now()) {
			k.recordKernelCheck(ctx, e, checkRecord{user: user, tenant: tenant, at: k.now(), mode: native.CheckStatement, check: c, allowed: ok,
				site: native.CheckLocation{Kernel: "http " + node.Path}})
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
