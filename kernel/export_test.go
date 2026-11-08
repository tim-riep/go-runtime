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

package kernel

import (
	"context"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/vm"
)

// TargetVersion returns the kernel schema version the kernel works with.
func (k *Kernel) TargetVersion() int { return k.target }

// SetTargetVersion makes the kernel work with an older kernel schema
// version, so tests can create databases that need a migration.
func (k *Kernel) SetTargetVersion(v int) {
	k.target = v
	k.repo = k.repo.At(v)
	if v < 16 { // before the native transformation format xlsx
		pkgs := make([]*model.Package, len(k.kernelPkgs))
		for i, p := range k.kernelPkgs {
			c := *p
			c.Transformations = slices.DeleteFunc(slices.Clone(p.Transformations), func(t model.Transformation) bool {
				return t.Native.Or("") == model.TransformXLSX
			})
			pkgs[i] = &c
		}
		k.kernelPkgs = pkgs
	}
}

// ContentReads returns the number of asset contents that code read from
// the database.
func (k *Kernel) ContentReads() int64 { return k.contentReads.Load() }

// EffectiveComputed returns how often effective authorizations were
// computed rather than taken from the cache.
func (k *Kernel) EffectiveComputed() int64 { return k.effectiveComputed.Load() }

// SetHTTPLog replaces the sink for the log statements of requests.
func (k *Kernel) SetHTTPLog(s vm.LogSink) { k.httpLog = s }

// UpgradeKernelPackage upgrades a kernel package with any package, for
// tests that use TRBASE as an ordinary package.
func (k *Kernel) UpgradeKernelPackage(ctx context.Context, pkg *model.Package, opts Options) (*plan.Plan, error) {
	return k.upgrade(ctx, pkg, opts)
}
