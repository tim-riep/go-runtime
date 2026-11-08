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
	"log/slog"
	"net/http"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
)

// serviceRequest is a request to a service the user may start.
type serviceRequest struct {
	*kernelRequest
	svc *model.Service
	m   *odata.Model
	// base is the absolute URL of the service, ending with a slash;
	// resource the path below it.
	base     string
	resource string
}

// serveService serves the resources of a service.
func (k *Kernel) serveService(w http.ResponseWriter, r *http.Request, sr *serviceRequest) {
	switch sr.resource {
	case "":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			sr.fail(w, odata.Errorf(http.StatusMethodNotAllowed, odata.CodeMethodNotAllowed, sr.svc.Name, r.Method))
			return
		}
		sr.writeJSON(w, r, http.StatusOK, odata.ServiceDocument(sr.m, sr.base))
	case "$metadata":
		k.serveMetadata(w, r, sr)
	default:
		sr.fail(w, odata.Errorf(http.StatusNotImplemented, odata.CodeNotImplemented, sr.resource))
	}
}

// internal answers with an internal error and logs it.
func (kr *kernelRequest) internal(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "node", kr.node.Path, "user", kr.entry.user, "tenant", kr.entry.tenant, "error", err)
	kr.fail(w, odata.Errorf(http.StatusInternalServerError, odata.CodeInternal))
}
