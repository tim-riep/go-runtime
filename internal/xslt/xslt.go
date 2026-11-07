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

// Package xslt compiles and runs XSLT stylesheets with go-xslt for the
// kernel. Stylesheets read nothing outside: there is no base directory
// and no network, so xsl:include, xsl:import and document() find only the
// stylesheet itself.
package xslt

import (
	"context"
	"fmt"
	"strings"

	"github.com/tim-riep/go-xslt/engine"
)

// Error is an error of a stylesheet or a transformation, with the line in
// the stylesheet where known.
type Error struct {
	Line int // 0 when unknown
	Text string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Text)
	}
	return e.Text
}

func diagError(diags []engine.Diagnostic) *Error {
	for _, d := range diags {
		if d.Severity == engine.SeverityError {
			text := d.Message
			if d.Code != "" && !strings.Contains(text, d.Code) {
				text = d.Code + ": " + text
			}
			return &Error{Line: d.Line, Text: text}
		}
	}
	return &Error{Text: "the transformation failed"}
}

// Stylesheet is a compiled stylesheet; it is safe for concurrent use.
type Stylesheet struct{ s *engine.Stylesheet }

// Compile compiles a stylesheet.
func Compile(text string) (*Stylesheet, error) {
	s, diags := engine.Compile(engine.Request{Stylesheet: text})
	if s == nil {
		return nil, diagError(diags)
	}
	return &Stylesheet{s: s}, nil
}

// Transform runs the stylesheet on an XML document with top-level
// parameters. maxOutput limits the size of the result; 0 means none.
func (s *Stylesheet) Transform(ctx context.Context, source string, params map[string]string, maxOutput int64) ([]byte, error) {
	res := s.s.TransformContext(ctx, engine.TransformRequest{Source: source, Params: params, Limits: engine.Limits{MaxOutputBytes: maxOutput}})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if res.HasErrors() {
		return nil, diagError(res.Diagnostics)
	}
	return []byte(res.Output), nil
}
