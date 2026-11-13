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
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// kernelPackagesVersion is the kernel schema version that introduced
// kernel packages.
const kernelPackagesVersion = 8

// languagesVersion is the first kernel schema version with languages.
const languagesVersion = 2

// Config configures a kernel beyond its database.
type Config struct {
	// Natives are the native methods; nil means native.Default, those of
	// the kernel binary.
	Natives *native.Registry
	// KernelPackages are the kernel packages in installation order; nil
	// means those embedded in the kernel binary.
	KernelPackages []*model.Package
	// MaxFailedLogins locks a user after this many failed logins; 0 means
	// 5.
	MaxFailedLogins int
	// MaxAssetRead is the largest asset in bytes that code loads into the
	// memory of an execution; larger ones raise /TRBASE/ASSET_TOO_LARGE.
	// 0 means DefaultMaxAssetRead. Streaming to an HTTP response has no
	// limit.
	MaxAssetRead int64
	// Sources are the remote package sources, such as GitHub; without
	// them only directories and ZIP files are installed.
	Sources SourceConfig
	// Formats are the limits of native methods for zip, gzip and xlsx;
	// zero fields mean native.DefaultLimits.
	Formats native.Limits
	// ObjectBehaviorTimeout limits each call of the behavior class of an
	// object type; 0 means DefaultObjectBehaviorTimeout.
	ObjectBehaviorTimeout time.Duration
}

// syncKernelPackages installs the kernel packages that are missing and
// upgrades those whose version differs from the binary, that were
// installed from a directory by a kernel before kernel packages, or all
// of them with force, when a language was added. Kernel packages are
// installed with the texts of the languages that are set up.
func (k *Kernel) syncKernelPackages(ctx context.Context, force bool) (*plan.Plan, error) {
	if k.target < kernelPackagesVersion || len(k.kernelPkgs) == 0 {
		return nil, nil
	}
	s, err := k.repo.Load(ctx, k.db)
	if err != nil {
		return nil, err
	}
	langs, err := k.repo.LanguageCodes(ctx, k.db)
	if err != nil {
		return nil, err
	}
	keep := func(l string) bool { return langs[l] }
	var missing []*model.Package
	var result *plan.Plan
	var ids []string
	for _, p := range k.kernelPkgs {
		info, installed := s.Packages[p.Manifest.ID]
		switch {
		case !installed:
			missing = append(missing, p.WithLanguages(keep))
			ids = append(ids, string(p.Manifest.ID))
		case force || info.Manifest.Version != p.Manifest.Version || !info.Manifest.KernelPackage:
			ids = append(ids, string(p.Manifest.ID))
			up, err := k.upgrade(ctx, p.WithLanguages(keep), Options{})
			if err != nil {
				return nil, &rterr.KernelPackageUpgrade{Packages: ids, Err: err}
			}
			result = merge(result, up)
		}
	}
	if len(missing) > 0 {
		in, err := k.install(ctx, missing, Options{})
		if err != nil {
			return nil, &rterr.KernelPackageUpgrade{Packages: ids, Err: err}
		}
		result = merge(result, in)
	}
	return result, nil
}

func merge(a, b *plan.Plan) *plan.Plan {
	if a == nil {
		return b
	}
	a.Packages = append(a.Packages, b.Packages...)
	a.Changes = append(a.Changes, b.Changes...)
	a.Steps = append(a.Steps, b.Steps...)
	return a
}

// NativeInfo describes a registered native method.
type NativeInfo struct {
	Key          string        `json:"key"`
	Declaration  string        `json:"declaration"`
	Capabilities []string      `json:"capabilities"`
	Timeout      time.Duration `json:"timeout"`
	// Declared tells whether the installed kernel package declares the
	// method; Used whether code of installed packages calls it.
	Declared bool `json:"declared"`
	Used     bool `json:"used"`
}

// Natives lists the native methods registered in the kernel.
func (k *Kernel) Natives(ctx context.Context) ([]NativeInfo, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	declared, err := k.declaredNatives(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := k.repo.LoadCode(ctx, k.db)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, c := range codes {
		for _, u := range c.Uses {
			if u.Kind == "method" && u.Member.Valid() {
				used[u.Object+"."+u.Member.Or("")] = true
			}
		}
	}
	var out []NativeInfo
	for _, m := range k.natives.Methods() {
		caps := slices.Clone(m.Capabilities)
		if caps == nil {
			caps = []string{}
		}
		out = append(out, NativeInfo{Key: m.Key(), Declaration: m.Declaration(), Capabilities: caps, Timeout: m.Timeout,
			Declared: declared[m.Key()], Used: used[m.Class+"."+m.Method]})
	}
	return out, nil
}

// declaredNatives returns the keys of the native methods that the stored
// code of the installed packages declares.
func (k *Kernel) declaredNatives(ctx context.Context) (map[string]bool, error) {
	programs, err := k.storedPrograms(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for id, p := range programs {
		for _, f := range p.Funcs {
			if f.Native {
				class, method, _ := strings.Cut(f.Name, ".")
				out[native.Key(id, class, method)] = true
			}
		}
	}
	return out, nil
}

// warnUndeclaredNatives logs registered native methods that no installed
// kernel package declares.
func (k *Kernel) warnUndeclaredNatives(ctx context.Context) {
	declared, err := k.declaredNatives(ctx)
	if err != nil {
		return
	}
	for _, m := range k.natives.Methods() {
		if !declared[m.Key()] {
			slog.Warn("a native method is registered, but no installed kernel package declares it", "method", m.Key())
		}
	}
}
