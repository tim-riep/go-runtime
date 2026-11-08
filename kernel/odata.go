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
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/httpsvc"
	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/reflector"
)

// ODataServiceObject is the authorization object of the kernel package
// TRBASE that requests need to start a service.
const ODataServiceObject = "/TRBASE/ODATA_SERVICE"

// kernelRequest is a request to a service node of the kernel after sign-in,
// tenant, language and the check of the node.
type kernelRequest struct {
	g      *generation
	st     *httpState
	node   *httpsvc.Node
	rest   string
	set    httpsvc.Settings
	user   *Principal
	tenant model.Optional[string]
	lang   string
	entry  *accessEntry
}

// serveKernelNode runs the handler of a service node of the kernel in Go.
func (k *Kernel) serveKernelNode(w http.ResponseWriter, r *http.Request, kr *kernelRequest) {
	switch kr.node.Handlers()[0].Class {
	case httpsvc.ODataHandler:
		k.serveOData(w, r, kr)
	default:
		kr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, kr.node.Path))
	}
}

// fail answers a request with an error in the format of OData.
func (kr *kernelRequest) fail(w http.ResponseWriter, e *odata.Error) {
	kr.entry.status = e.Status
	kr.entry.size = e.Write(w, kr.lang)
}

// odataModels caches the models of the services of a generation.
type odataModels struct {
	mu     sync.Mutex
	models map[string]*odata.Model
}

// odataModel returns the model of a service of the generation. Validation
// guarantees that installed services have a complete model.
func (g *generation) odataModel(svc *model.Service) *odata.Model {
	g.odataOnce.Do(func() { g.odata = &odataModels{models: map[string]*odata.Model{}} })
	g.odata.mu.Lock()
	defer g.odata.mu.Unlock()
	if m, ok := g.odata.models[svc.Name]; ok {
		return m
	}
	m, problems := odata.Build(g.schema, g.phys, svc)
	if len(problems) > 0 {
		slog.Error("an installed service has problems", "service", svc.Name, "problems", odata.Problems(problems))
	}
	g.odata.models[svc.Name] = m
	return m
}

// serveOData serves /rt/odata: the list of services and the services.
func (k *Kernel) serveOData(w http.ResponseWriter, r *http.Request, kr *kernelRequest) {
	if v := r.Header.Get("OData-MaxVersion"); v != "" && !atLeast4(v) {
		kr.fail(w, odata.Errorf(http.StatusBadRequest, odata.CodeUnsupportedVersion, v))
		return
	}
	if kr.rest == "" {
		k.serveServiceList(w, r, kr)
		return
	}
	segs := strings.SplitN(kr.rest, "/", 3)
	if len(segs) < 2 {
		kr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, r.URL.Path))
		return
	}
	svc := kr.g.schema.ServiceAt(segs[0], segs[1])
	if svc == nil || !kr.st.services[svc.Name] {
		kr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, r.URL.Path))
		return
	}
	allowed, err := k.checkService(r.Context(), kr, svc, true)
	if err != nil {
		slog.Error("checking the start of a service", "service", svc.Name, "error", err)
		kr.fail(w, odata.Errorf(http.StatusInternalServerError, odata.CodeInternal))
		return
	}
	if !allowed {
		kr.fail(w, odata.Errorf(http.StatusForbidden, odata.CodeForbidden, svc.Name))
		return
	}
	resource := ""
	if len(segs) == 3 {
		resource = segs[2]
	}
	sr := &serviceRequest{kernelRequest: kr, svc: svc, m: kr.g.odataModel(svc), base: baseURL(r) + svc.Path(), resource: resource}
	k.serveService(w, r, sr)
}

// atLeast4 reports whether an OData version is 4.0 or later.
func atLeast4(v string) bool {
	major, _, _ := strings.Cut(strings.TrimSpace(v), ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 4
}

// baseURL returns scheme and host of a request.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

// checkService reports whether the user of a request may start a service.
// record writes failures to the trace and the last failure.
func (k *Kernel) checkService(ctx context.Context, kr *kernelRequest, svc *model.Service, record bool) (bool, error) {
	e, err := k.EffectiveAuthorizations(ctx, kr.user.Name, kr.tenant.Or(""), k.now())
	if err != nil {
		return false, err
	}
	c := authorization.Check{Object: ODataServiceObject, Values: map[string]string{"PACKAGE_ID": string(svc.Owner), "SERVICE_NAME": svc.Name,
		model.ActivityField: "EXECUTE"}}
	ok := e.Check(c)
	if record && (!ok || e.Tracing(k.now())) {
		k.recordKernelCheck(ctx, e, checkRecord{user: kr.user.Name, tenant: kr.tenant.Or(""), at: k.now(), mode: native.CheckStatement, check: c,
			allowed: ok, site: native.CheckLocation{Kernel: "odata " + svc.Name}})
	}
	return ok, nil
}

// serviceEntry is an entry of the list of services.
type serviceEntry struct {
	Package     model.PackageID `json:"package"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	URL         string          `json:"url"`
}

// serveServiceList answers GET /rt/odata/ with the active services the
// user may start.
func (k *Kernel) serveServiceList(w http.ResponseWriter, r *http.Request, kr *kernelRequest) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		kr.fail(w, odata.Errorf(http.StatusMethodNotAllowed, odata.CodeMethodNotAllowed, model.ODataPath, r.Method))
		return
	}
	list := []serviceEntry{}
	for _, name := range kr.g.schema.ServiceNames() {
		svc := kr.g.schema.Services[name]
		if !kr.st.services[name] {
			continue
		}
		ok, err := k.checkService(r.Context(), kr, svc, false)
		if err != nil {
			slog.Error("checking the start of a service", "service", name, "error", err)
			kr.fail(w, odata.Errorf(http.StatusInternalServerError, odata.CodeInternal))
			return
		}
		if ok {
			list = append(list, serviceEntry{Package: svc.Owner, Name: name, Description: reflector.In(svc.Description, kr.lang).Or(""), URL: svc.Path()})
		}
	}
	kr.writeJSON(w, r, http.StatusOK, map[string]any{"value": list})
}

// writeJSON writes a response in JSON.
func (kr *kernelRequest) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("encoding a response", "error", err)
		kr.fail(w, odata.Errorf(http.StatusInternalServerError, odata.CodeInternal))
		return
	}
	kr.writeBody(w, r, status, "application/json;odata.metadata=minimal", body)
}

// writeBody writes a response with the headers of OData.
func (kr *kernelRequest) writeBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("OData-Version", "4.0")
	h.Set("Content-Language", kr.lang)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	kr.entry.status = status
	if r.Method != http.MethodHead {
		kr.entry.size, _ = w.Write(body)
	}
}
