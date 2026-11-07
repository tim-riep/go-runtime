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

package natives

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// HTTPRequest is the Go value of /TRBASE/HTTP_REQUEST.
type HTTPRequest struct {
	R    *http.Request
	Path string // full path
	Rest string // below the service node
	// Params are those of the handler entry that runs.
	Params  []model.HTTPParam
	MaxBody int64
	// TooLarge tells that the body was larger than MaxBody.
	TooLarge bool
	body     []byte
	read     bool
}

// Body reads the body once, at most MaxBody bytes.
func (r *HTTPRequest) Body() ([]byte, error) {
	if !r.read {
		r.read = true
		if r.R.Body != nil {
			b, err := io.ReadAll(io.LimitReader(r.R.Body, r.MaxBody+1))
			if err != nil {
				return nil, err
			}
			r.body = b
		}
		if int64(len(r.body)) > r.MaxBody {
			r.TooLarge = true
		}
	}
	if r.TooLarge {
		return nil, &native.InputTooLarge{What: "the request body", Size: int64(len(r.body)), Max: r.MaxBody}
	}
	return r.body, nil
}

// HTTPResponse is the Go value of /TRBASE/HTTP_RESPONSE. It buffers the
// body, so that status and headers can change until the request is done.
type HTTPResponse struct {
	Status int
	Header http.Header
	Body   bytes.Buffer
	Max    int64
	// Serve, when set, writes the response itself instead of Body, e.g. a
	// file of an asset repository that is streamed.
	Serve func(w http.ResponseWriter)
}

// NewHTTPResponse returns a response with status 200.
func NewHTTPResponse(max int64) *HTTPResponse {
	return &HTTPResponse{Status: http.StatusOK, Header: http.Header{}, Max: max}
}

func (r *HTTPResponse) write(b []byte) error {
	if int64(r.Body.Len()+len(b)) > r.Max {
		return &native.InputTooLarge{What: "the response", Size: int64(r.Body.Len() + len(b)), Max: r.Max}
	}
	r.Body.Write(b)
	return nil
}

func optional(vals []string) model.Optional[string] {
	if len(vals) == 0 {
		return model.None[string]()
	}
	return model.Some(vals[0])
}

func list(vals []string) []string {
	if vals == nil {
		return []string{}
	}
	return vals
}

func instance(class, method, sig string, fn any) {
	native.Default.Register(native.Method{Package: pkg, Class: class, Method: method, Signature: sig, Func: fn})
}

func init() {
	const req, resp = "/TRBASE/HTTP_REQUEST", "/TRBASE/HTTP_RESPONSE"
	native.Default.RegisterClass(pkg, req, (*HTTPRequest)(nil))
	native.Default.RegisterClass(pkg, resp, (*HTTPResponse)(nil))
	instance(req, "METHOD", "() returns STRING", func(c *native.Context, r *HTTPRequest) (string, error) { return r.R.Method, nil })
	instance(req, "PATH", "() returns STRING", func(c *native.Context, r *HTTPRequest) (string, error) { return r.Path, nil })
	instance(req, "REST_PATH", "() returns STRING", func(c *native.Context, r *HTTPRequest) (string, error) { return r.Rest, nil })
	instance(req, "QUERY", "(name: STRING) returns STRING?", func(c *native.Context, r *HTTPRequest, name string) (model.Optional[string], error) {
		return optional(r.R.URL.Query()[name]), nil
	})
	instance(req, "QUERY_ALL", "(name: STRING) returns array of STRING", func(c *native.Context, r *HTTPRequest, name string) ([]string, error) {
		return list(r.R.URL.Query()[name]), nil
	})
	instance(req, "HEADER", "(name: STRING) returns STRING?", func(c *native.Context, r *HTTPRequest, name string) (model.Optional[string], error) {
		return optional(r.R.Header.Values(name)), nil
	})
	instance(req, "HEADER_ALL", "(name: STRING) returns array of STRING", func(c *native.Context, r *HTTPRequest, name string) ([]string, error) {
		return list(r.R.Header.Values(name)), nil
	})
	instance(req, "BODY", "() returns BYTES", func(c *native.Context, r *HTTPRequest) ([]byte, error) { return r.Body() })
	instance(req, "BODY_TEXT", "() returns STRING", func(c *native.Context, r *HTTPRequest) (string, error) {
		b, err := r.Body()
		if err != nil {
			return "", err
		}
		if !utf8.Valid(b) {
			return "", errors.New("the request body is no UTF-8 text")
		}
		return string(b), nil
	})
	instance(req, "HANDLER_PARAMETER", "(name: STRING) returns STRING?", func(c *native.Context, r *HTTPRequest, name string) (model.Optional[string], error) {
		for _, p := range r.Params {
			if p.Name == name {
				return model.Some(p.Value), nil
			}
		}
		return model.None[string](), nil
	})

	instance(resp, "SET_STATUS", "(code: INT4)", func(c *native.Context, r *HTTPResponse, code int32) error {
		if code < 100 || code > 599 {
			return errors.New("an HTTP status is between 100 and 599")
		}
		r.Status = int(code)
		return nil
	})
	instance(resp, "STATUS", "() returns INT4", func(c *native.Context, r *HTTPResponse) (int32, error) { return int32(r.Status), nil })
	instance(resp, "SET_HEADER", "(name: STRING, value: STRING)", func(c *native.Context, r *HTTPResponse, name, value string) error {
		r.Header.Set(name, value)
		return nil
	})
	instance(resp, "ADD_HEADER", "(name: STRING, value: STRING)", func(c *native.Context, r *HTTPResponse, name, value string) error {
		r.Header.Add(name, value)
		return nil
	})
	instance(resp, "WRITE", "(data: BYTES)", func(c *native.Context, r *HTTPResponse, data []byte) error { return r.write(data) })
	instance(resp, "WRITE_TEXT", "(text: STRING)", func(c *native.Context, r *HTTPResponse, text string) error { return r.write([]byte(text)) })

	static("/TRBASE/CONTEXT", "USER", "() returns STRING?", func(c *native.Context) (model.Optional[string], error) { return c.User(), nil })
	static("/TRBASE/CONTEXT", "TENANT", "() returns STRING?", func(c *native.Context) (model.Optional[string], error) {
		if t := c.Tenant(); t != "" {
			return model.Some(t), nil
		}
		return model.None[string](), nil
	})
	static("/TRBASE/CONTEXT", "LANGUAGE", "() returns STRING", func(c *native.Context) (string, error) { return c.Language(), nil })
}
