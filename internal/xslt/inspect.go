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

package xslt

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tim-riep/go-runtime/model"
)

// Namespace is the namespace of XSLT elements.
const Namespace = "http://www.w3.org/1999/XSL/Transform"

// Inspect reads the top-level parameters of a stylesheet and rejects what
// the kernel does not resolve: xsl:include and xsl:import.
func Inspect(text string) ([]model.TransformParam, error) {
	d := xml.NewDecoder(strings.NewReader(text))
	var params []model.TransformParam
	depth := 0
	line := func() int {
		l, _ := d.InputPos()
		return l
	}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var se *xml.SyntaxError
			if errors.As(err, &se) {
				return nil, &Error{Line: se.Line, Text: se.Msg}
			}
			return nil, &Error{Text: err.Error()}
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				if t.Name.Space != Namespace || t.Name.Local != "stylesheet" && t.Name.Local != "transform" {
					return nil, &Error{Line: line(), Text: "a transformation is an XSLT stylesheet: its root element is xsl:stylesheet or xsl:transform"}
				}
				continue
			}
			if t.Name.Space == Namespace && (t.Name.Local == "include" || t.Name.Local == "import") {
				return nil, &Error{Line: line(), Text: fmt.Sprintf("xsl:%s of %q is not supported; transformations cannot include or import other stylesheets yet", t.Name.Local, attr(t, "href"))}
			}
			if depth == 2 && t.Name.Space == Namespace && t.Name.Local == "param" {
				p := model.TransformParam{Name: attr(t, "name")}
				p.Required = attr(t, "required") == "yes"
				params = append(params, p)
			}
		case xml.EndElement:
			depth--
		}
	}
	return params, nil
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Space == "" && a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
