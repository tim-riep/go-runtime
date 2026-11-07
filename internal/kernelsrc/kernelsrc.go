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

// Package kernelsrc holds the classes of the kernel written in the
// language itself, such as the roots of exceptions and the runtime errors.
// They are part of every program.
package kernelsrc

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/tim-riep/go-runtime/model"
)

//go:embed src/*.class
var files embed.FS

// Sources returns the kernel classes ordered by name.
func Sources() []model.Source {
	entries, _ := fs.ReadDir(files, "src")
	var out []model.Source
	for _, e := range entries {
		b, err := files.ReadFile(path.Join("src", e.Name()))
		if err != nil {
			continue
		}
		name := model.KernelNamespace + strings.TrimSuffix(e.Name(), ".class")
		out = append(out, model.Source{Name: name, Kind: model.SourceClass, File: "kernel/" + e.Name(), Text: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
