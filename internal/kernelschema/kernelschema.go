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

// Package kernelschema holds the definitions of the kernel's native tables.
//
// Each version of the kernel tables is a complete package definition in the
// regular package format, loaded in kernel mode so that every name lives in
// the kernel namespace. Moving from one version to the next is computed by
// the same diff that upgrades packages.
package kernelschema

import (
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/model"
)

//go:embed v1 v2 v3 v4 v5 v6 v7 v8 v9 v10 v11 v12 v13 v14
var files embed.FS

// Migration is one version of the kernel tables.
type Migration struct {
	Version int
	Dir     string
}

// Migrations lists all versions in ascending order. Append new versions;
// never change a released one.
var Migrations = []Migration{
	{Version: 1, Dir: "v1"},
	{Version: 2, Dir: "v2"},   // data classes, languages, data elements, settings
	{Version: 3, Dir: "v3"},   // asset repositories
	{Version: 4, Dir: "v4"},   // generated constraint names, defaults, empty strings
	{Version: 5, Dir: "v5"},   // structures, array types, includes
	{Version: 6, Dir: "v6"},   // names of fixed values in code
	{Version: 7, Dir: "v7"},   // source files, bytecode and uses of code
	{Version: 8, Dir: "v8"},   // kernel packages and capabilities
	{Version: 9, Dir: "v9"},   // http services and users
	{Version: 10, Dir: "v10"}, // uses of asset repositories
	{Version: 11, Dir: "v11"}, // authorization objects and activities
	{Version: 12, Dir: "v12"}, // roles and role assignments
	{Version: 13, Dir: "v13"}, // the role with all authorizations
	{Version: 14, Dir: "v14"}, // byte order for text columns, authorization checks
}

// Latest returns the newest version.
func Latest() int { return Migrations[len(Migrations)-1].Version }

// Package loads the kernel definitions of a version.
func Package(version int) (*model.Package, error) {
	for _, m := range Migrations {
		if m.Version != version {
			continue
		}
		sub, err := fs.Sub(files, m.Dir)
		if err != nil {
			return nil, err
		}
		return pkgload.LoadWith(sub, "kernel "+m.Dir, pkgload.Options{Kernel: true})
	}
	return nil, fmt.Errorf("unknown kernel schema version %d", version)
}

// Schema returns a schema containing only the kernel objects of a version.
func Schema(version int) (*model.Schema, error) {
	pkg, err := Package(version)
	if err != nil {
		return nil, err
	}
	s := model.NewSchema()
	if err := s.AddPackage(pkg, time.Time{}, time.Time{}); err != nil {
		return nil, err
	}
	return s, nil
}

// Replace returns a copy of s whose kernel objects are those of version,
// with texts in English and the languages keep accepts. Package objects
// are kept.
func Replace(s *model.Schema, version int, keep func(lang string) bool) (*model.Schema, error) {
	pkg, err := Package(version)
	if err != nil {
		return nil, err
	}
	pkg = pkg.WithLanguages(keep)
	out := s.Clone()
	out.RemovePackage(model.Kernel)
	if err := out.AddPackage(pkg, time.Time{}, time.Time{}); err != nil {
		return nil, err
	}
	return out, nil
}
