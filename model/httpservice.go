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
	"bytes"
	"encoding/json"
	"errors"
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
	Authorization   HTTPAuthSetting    `json:"authorization,omitzero"`
}

// HTTPAuthorization is a check that a service node makes after the check
// of /TRBASE/HTTP_SERVICE: an authorization object with a value or any for
// every field.
type HTTPAuthorization struct {
	Object string                   `json:"object"`
	Fields map[string]HTTPAuthValue `json:"fields"`
}

// HTTPAuthValue is the value of a field of a node check in canonical text
// form, or any. JSON writes a string or {"any": true}.
type HTTPAuthValue struct {
	Any   bool
	Value string
}

// MarshalJSON writes the value as string or {"any": true}.
func (v HTTPAuthValue) MarshalJSON() ([]byte, error) {
	if v.Any {
		return []byte(`{"any":true}`), nil
	}
	return json.Marshal(v.Value)
}

// UnmarshalJSON reads a string, a number or {"any": true}.
func (v *HTTPAuthValue) UnmarshalJSON(data []byte) error {
	var obj struct {
		Any bool `json:"any"`
	}
	switch t := bytes.TrimSpace(data); {
	case len(t) > 0 && t[0] == '{':
		if err := json.Unmarshal(t, &obj); err != nil || !obj.Any {
			return errors.New(`a field of a node check is a value or {"any": true}`)
		}
		*v = HTTPAuthValue{Any: true}
	case len(t) > 0 && t[0] == '"':
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return err
		}
		*v = HTTPAuthValue{Value: s}
	default:
		var n json.Number
		if err := json.Unmarshal(t, &n); err != nil {
			return errors.New(`a field of a node check is a value or {"any": true}`)
		}
		*v = HTTPAuthValue{Value: n.String()}
	}
	return nil
}

// HTTPAuthSetting is the setting authorization of a node. Not set, the
// node inherits it; set to null (Check nil), the node and its subtree
// make no additional check.
type HTTPAuthSetting struct {
	Set   bool
	Check *HTTPAuthorization
}

// IsZero reports a setting that is not set.
func (s HTTPAuthSetting) IsZero() bool { return !s.Set }

// MarshalJSON writes null or the check.
func (s HTTPAuthSetting) MarshalJSON() ([]byte, error) {
	if s.Check == nil {
		return []byte("null"), nil
	}
	return json.Marshal(s.Check)
}

// UnmarshalJSON reads null or a check; a missing key leaves the setting
// unset.
func (s *HTTPAuthSetting) UnmarshalJSON(data []byte) error {
	*s = HTTPAuthSetting{Set: true}
	if string(bytes.TrimSpace(data)) == "null" {
		return nil
	}
	s.Check = &HTTPAuthorization{}
	return json.Unmarshal(data, s.Check)
}

// Authentication methods, tenant sources and language sources of
// service nodes.
const (
	AuthBasic   = "basic"
	AuthToken   = "token"
	AuthSession = "session"

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
	if a := s.Settings.Authorization.Check; a != nil {
		c.Settings.Authorization.Check = &HTTPAuthorization{Object: a.Object, Fields: maps.Clone(a.Fields)}
	}
	c.Handlers = make([]HTTPHandlerRef, len(s.Handlers))
	for i, h := range s.Handlers {
		c.Handlers[i] = HTTPHandlerRef{Class: h.Class, Parameters: slices.Clone(h.Parameters)}
	}
	return &c
}
