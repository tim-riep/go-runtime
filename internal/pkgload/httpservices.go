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
	"fmt"
	"path"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/schemas"
)

// HTTPServicesDir holds the service nodes of a package, one per file.
const HTTPServicesDir = SchemaDir + "/http-services"

func init() {
	register(ObjectType{
		Kind:     "http service",
		Document: func(p string) bool { return path.Dir(p) == HTTPServicesDir && strings.HasSuffix(p, ".json") },
		Schema:   schemas.HTTPService,
		NewDoc:   func() any { return &model.HTTPService{} },
		FileName: func(doc any) (string, bool) { return "", false },
		// Paths belong to the package through its prefix; the handler
		// classes are references.
		Definitions: func(doc any) []NameRef { return nil },
		References: func(doc any) []NameRef {
			var out []NameRef
			for i, h := range doc.(*model.HTTPService).Handlers {
				out = append(out, NameRef{Name: h.Class, Pointer: fmt.Sprintf("/handlers/%d/class", i)})
			}
			return out
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			h := *doc.(*model.HTTPService)
			h.File, h.Src = file, l.loc(file)
			if !model.ValidHTTPPath(h.Path) {
				l.fail(rterr.CodeName, l.loc(file).At("/path"), "%q is no valid path of a service node: segments of letters, digits, '.', '_', '~' and '-' that start with a letter or digit, separated by '/'", h.Path)
				return
			}
			for _, other := range pkg.HTTPServices {
				if other.Path == h.Path {
					l.fail(rterr.CodeName, l.loc(file).At("/path"), "the service node %q is defined in %s already", h.Path, other.File)
					return
				}
			}
			pkg.HTTPServices = append(pkg.HTTPServices, h)
		},
	})
}
