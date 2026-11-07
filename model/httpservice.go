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

package model

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/rterr"
)

// HTTPService is a service node of a package (schema/http-services/*.json):
// a path below the package with handler classes and settings that its
// subtree inherits.
type HTTPService struct {
	// Path is relative to the package root, "" for the root itself.
	Path     string           `json:"path"`
	Owner    PackageID        `json:"owner"`
	File     string           `json:"file"`
	Texts    map[string]Label `json:"texts"`
	Handlers []HTTPHandlerRef `json:"handlers"`
	Settings HTTPSettings     `json:"settings"`
	Src      rterr.Location   `json:"-"`
}

// HTTPHandlerRef is an entry of the handler list of a service node.
type HTTPHandlerRef struct {
	Class      string      `json:"class"`
	Parameters []HTTPParam `json:"parameters,omitempty"`
}

// HTTPParam is a parameter of a handler entry.
type HTTPParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPSettings are the settings of a service node. A setting that is not
// set is inherited from the parent node.
type HTTPSettings struct {
	Authentication  Optional[[]string] `json:"authentication"`
	Anonymous       Optional[bool]     `json:"anonymous"`
	AnonymousTenant Optional[string]   `json:"anonymousTenant"`
	TenantSources   Optional[[]string] `json:"tenantSources"`
	LanguageSources Optional[[]string] `json:"languageSources"`
	MaxBodySize     Optional[int64]    `json:"maxBodySize"`
	MaxResponseSize Optional[int64]    `json:"maxResponseSize"`
	TimeoutSeconds  Optional[int64]    `json:"timeoutSeconds"`
	Budget          Optional[int64]    `json:"budget"`
}

// Authentication methods, tenant sources and language sources of
// service nodes.
const (
	AuthBasic = "basic"
	AuthToken = "token"

	SourceHeader = "header"
	SourceQuery  = "query"
	SourceUser   = "user"
)

var httpSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*$`)

// MaxHTTPPathLength limits the path of a service node below its package.
const MaxHTTPPathLength = 255

// ValidHTTPPath reports whether p is a valid path of a service node
// below its package: segments of letters, digits, ., _, ~ and -, starting
// with a letter or digit, separated by /. The empty path is the root.
func ValidHTTPPath(p string) bool {
	if p == "" {
		return true
	}
	if len(p) > MaxHTTPPathLength {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if !httpSegment.MatchString(s) {
			return false
		}
	}
	return true
}

// HTTPPackagePath returns the path of the root node of a package: its ID
// in lower case.
func HTTPPackagePath(id PackageID) string { return "/" + strings.ToLower(string(id)) }

// FullPath returns the path under which the node is reachable.
func (s *HTTPService) FullPath() string {
	if s.Path == "" {
		return HTTPPackagePath(s.Owner)
	}
	return HTTPPackagePath(s.Owner) + "/" + s.Path
}

func cloneHTTPService(s *HTTPService) *HTTPService {
	c := *s
	c.Texts = maps.Clone(s.Texts)
	c.Handlers = make([]HTTPHandlerRef, len(s.Handlers))
	for i, h := range s.Handlers {
		c.Handlers[i] = HTTPHandlerRef{Class: h.Class, Parameters: slices.Clone(h.Parameters)}
	}
	return &c
}
