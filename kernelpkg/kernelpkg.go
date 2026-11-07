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

// Package kernelpkg embeds the kernel packages: packages that belong to
// one version of the kernel, are installed only from its binary and are
// the only packages with methods executed by kernel. The kernel installs
// them on initialization and upgrades them when a newer kernel opens a
// database.
package kernelpkg

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	// The implementations of the methods executed by kernel.
	_ "github.com/tim-riep/go-runtime/kernelpkg/natives"
	"github.com/tim-riep/go-runtime/model"
)

// Version is the version of the kernel packages of this kernel. Every
// kernel package has it as its version; a kernel that changes a kernel
// package increases it.
const Version = "1.1.0"

//go:embed all:TRBASE
var files embed.FS

// IDs are the kernel packages in installation order.
var IDs = []model.PackageID{"TRBASE"}

// Packages loads the kernel packages in installation order.
func Packages() ([]*model.Package, error) {
	var out []*model.Package
	for _, id := range IDs {
		p, err := Load(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Load loads one kernel package.
func Load(id model.PackageID) (*model.Package, error) {
	if !slices.Contains(IDs, id) {
		return nil, fmt.Errorf("kernelpkg: %s is no kernel package", id)
	}
	sub, err := fs.Sub(files, string(id))
	if err != nil {
		return nil, err
	}
	p, err := pkgload.Load(sub, "kernel:"+string(id))
	if err != nil {
		return nil, fmt.Errorf("kernel package %s: %w", id, err)
	}
	if !p.Manifest.KernelPackage || p.Manifest.Version != Version {
		return nil, fmt.Errorf("kernel package %s must be marked as kernel package and have version %s", id, Version)
	}
	return p, nil
}

// Is reports whether id is a kernel package.
func Is(id model.PackageID) bool { return slices.Contains(IDs, id) }
