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

//go:embed v1
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

// Replace returns a copy of s whose kernel objects are those of version.
// Package objects are kept.
func Replace(s *model.Schema, version int) (*model.Schema, error) {
	pkg, err := Package(version)
	if err != nil {
		return nil, err
	}
	out := s.Clone()
	out.RemovePackage(model.Kernel)
	if err := out.AddPackage(pkg, time.Time{}, time.Time{}); err != nil {
		return nil, err
	}
	return out, nil
}
