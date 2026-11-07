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

package pkgload

import (
	"errors"
	"path"
	"strings"

	"github.com/tim-riep/go-runtime/internal/xslt"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/schemas"
)

// TransformationsDir holds the XSLT stylesheets of a package, one
// transformation per file, with an optional description next to it.
const TransformationsDir = "transformations"

// stylesheetDoc is a parsed stylesheet.
type stylesheetDoc struct {
	file   string
	text   string
	params []model.TransformParam
	pkg    string
}

func (d *stylesheetDoc) setPackage(id string) { d.pkg = id }

func (d *stylesheetDoc) name() string {
	return model.Namespace(model.PackageID(d.pkg)) + strings.TrimSuffix(path.Base(d.file), ".xsl")
}

// transformationDoc is the description of a transformation.
type transformationDoc struct {
	Name   string                 `json:"name"`
	Texts  map[string]model.Label `json:"texts"`
	Output model.Optional[string] `json:"output"`
}

func init() {
	register(ObjectType{
		Kind:     "transformation",
		Document: func(p string) bool { return path.Dir(p) == TransformationsDir && strings.HasSuffix(p, ".xsl") },
		Parse: func(file string, data []byte) (any, []*rterr.ValidationError) {
			text := string(data)
			params, err := xslt.Inspect(text)
			if err != nil {
				loc := rterr.Location{File: file}
				var xe *xslt.Error
				if errors.As(err, &xe) {
					loc.Line = xe.Line
				}
				return nil, []*rterr.ValidationError{{Code: rterr.CodeSource, Location: loc, Message: err.Error()}}
			}
			return &stylesheetDoc{file: file, text: text, params: params}, nil
		},
		FileName: func(doc any) (string, bool) { return "", false },
		Definitions: func(doc any) []NameRef {
			return []NameRef{{Name: doc.(*stylesheetDoc).name(), Line: 1, Column: 1}}
		},
		References: func(doc any) []NameRef { return nil },
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*stylesheetDoc)
			pkg.Transformations = append(pkg.Transformations, model.Transformation{Name: d.name(), File: file, Text: d.text,
				Params: d.params, Texts: map[string]model.Label{}, Src: l.loc(file)})
		},
	})
	register(ObjectType{
		Kind:     "transformation description",
		Document: func(p string) bool { return path.Dir(p) == TransformationsDir && strings.HasSuffix(p, ".json") },
		Schema:   schemas.Transformation,
		NewDoc:   func() any { return &transformationDoc{} },
		FileName: func(doc any) (string, bool) {
			_, local, ok := model.SplitName(doc.(*transformationDoc).Name)
			return local + ".json", ok
		},
		// The description belongs to the transformation of the same name
		// in the own package.
		Definitions: func(doc any) []NameRef { return nil },
		References: func(doc any) []NameRef {
			return []NameRef{{Name: doc.(*transformationDoc).Name, Pointer: "/name"}}
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*transformationDoc)
			for i := range pkg.Transformations {
				if t := &pkg.Transformations[i]; t.Name == d.Name {
					t.Texts, t.Output = d.Texts, d.Output
					return
				}
			}
			l.fail(rterr.CodeFile, l.loc(file), "%s describes the transformation %s, but there is no %s", file, d.Name,
				path.Join(TransformationsDir, strings.TrimSuffix(path.Base(file), ".json")+".xsl"))
		},
	})
}
