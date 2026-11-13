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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
)

// The execution is the host of native methods.

func (x *execution) Tenant() string { return x.tenant }

func (x *execution) User() model.Optional[string] { return x.opts.User }

func (x *execution) Language() string {
	if x.opts.Language == "" {
		return model.DefaultLanguage
	}
	return x.opts.Language
}

func (x *execution) Now() time.Time { return x.k.now().UTC() }

// AfterCommit registers an effect that runs after the next commit of a
// session and is discarded by its rollback. In test runs, whose commits
// are savepoints, effects never run.
func (x *execution) AfterCommit(session int, e native.Effect) error {
	fn, ok := x.k.natives.Effect(e.Kind)
	if !ok {
		return fmt.Errorf("kernel: no effect %s is registered", e.Kind)
	}
	if session <= 0 {
		session = 1
	}
	if session > len(x.sessions) {
		return fmt.Errorf("kernel: the execution has no session %d", session)
	}
	if x.test {
		return nil
	}
	tenant := x.tenant
	x.sessions[session-1].AfterCommit(func() { x.k.runEffect(tenant, e, fn, 0, "") })
	return nil
}

// runEffect runs an effect after a commit. A failure is logged and
// recorded in /RT/EFFECT_FAILURES, where rt effects retry finds it; the
// transaction is complete already.
func (k *Kernel) runEffect(tenant string, e native.Effect, fn native.EffectFunc, attempts int, id string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), effectTimeout)
	defer cancel()
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
				slog.Error("panic in an effect", "kind", e.Kind, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		err = fn(ctx, e.Payload)
	}()
	if err == nil {
		return nil
	}
	slog.Error("an effect failed after the commit of its session", "kind", e.Kind, "tenant", tenant, "error", err)
	if id == "" {
		id = repo.NewID()
	}
	f := repo.EffectFailure{ID: id, FailedAt: k.timestamp(), Kind: e.Kind, Payload: string(e.Payload), Error: err.Error(), Attempts: attempts + 1}
	if tenant != "" {
		f.Tenant = model.Some(tenant)
	}
	if rerr := k.repo.InsertEffectFailure(context.Background(), k.db, f); rerr != nil {
		slog.Error("recording a failed effect", "kind", e.Kind, "error", rerr)
	}
	return err
}

// effectTimeout limits one run of an effect.
const effectTimeout = 30 * time.Second

// EffectFailure is an effect that failed after a commit.
type EffectFailure = repo.EffectFailure

// Effects returns the effects that failed after the commit of their
// session, oldest first.
func (k *Kernel) Effects(ctx context.Context) ([]EffectFailure, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.repo.EffectFailures(ctx, k.db)
}

// RetryEffect runs a failed effect again. On success it is removed;
// otherwise its error and number of attempts are updated.
func (k *Kernel) RetryEffect(ctx context.Context, id string) error {
	list, err := k.Effects(ctx)
	if err != nil {
		return err
	}
	for _, f := range list {
		if f.ID != id {
			continue
		}
		fn, ok := k.natives.Effect(f.Kind)
		if !ok {
			return fmt.Errorf("kernel: no effect %s is registered", f.Kind)
		}
		if err := k.repo.DeleteEffectFailure(ctx, k.db, id); err != nil {
			return err
		}
		return k.runEffect(f.Tenant.Or(""), native.Effect{Kind: f.Kind, Payload: json.RawMessage(f.Payload)}, fn, f.Attempts, id)
	}
	return &rterr.NotFound{Kind: "effect", Name: id}
}

func (x *execution) KernelPackage(id model.PackageID) bool {
	info := x.schema.Packages[id]
	return info != nil && info.Manifest.KernelPackage
}

// mayRead reports whether code of a package may read the asset
// repositories of another: its own, those of the packages it requires,
// and all for kernel packages.
func (x *execution) mayRead(reader, pkg model.PackageID) bool {
	if reader == pkg || x.KernelPackage(reader) {
		return true
	}
	if info := x.schema.Packages[reader]; info != nil {
		for _, r := range info.Manifest.Requires {
			if r.ID == pkg {
				return true
			}
		}
	}
	return false
}

func (x *execution) AssetRepository(reader, pkg model.PackageID, repoID string) (native.AssetSource, error) {
	if !x.mayRead(reader, pkg) {
		return nil, &native.AssetAccessDenied{Reader: reader, Package: pkg}
	}
	r, err := x.k.assetRepository(x.ctx, pkg, repoID)
	var anf *rterr.AssetNotFound
	if errors.As(err, &anf) {
		return nil, native.ErrAssetRepositoryNotFound
	}
	if err != nil {
		return nil, err
	}
	return &assetSource{x: x, r: r}, nil
}

func (x *execution) AssetRepositories(reader, pkg model.PackageID) ([]*model.AssetRepository, error) {
	if !x.mayRead(reader, pkg) {
		return nil, &native.AssetAccessDenied{Reader: reader, Package: pkg}
	}
	if err := x.k.requireInit(x.ctx, x.k.db); err != nil {
		return nil, err
	}
	all, err := x.k.repo.AssetRepositoriesOf(x.ctx, x.k.db, pkg)
	if err != nil {
		return nil, err
	}
	return all, nil
}

// SetClock replaces the clock of the kernel, for tests of code with the
// capability clock.
func (k *Kernel) SetClock(now func() time.Time) { k.now = now }

func (x *execution) Reflector() (*reflector.Reflector, error) {
	if x.reflector == nil {
		langs, err := x.k.languageCodes(x.ctx)
		if err != nil {
			return nil, err
		}
		x.reflector = reflector.New(x.schema, langs)
	}
	return x.reflector, nil
}

// Components returns the components of a structure or table for native
// transformations: domain, the headings of the data element by language,
// notNull and the default, of the component or else of its domain.
func (x *execution) Components(structure string) ([]native.Component, error) {
	comps, ok := x.schema.Resolver().Components(structure)
	if !ok {
		return nil, fmt.Errorf("kernel: %s is no structure or table", structure)
	}
	var out []native.Component
	for _, c := range comps {
		if c.Name == model.TenantField {
			continue
		}
		nc := native.Component{Name: c.Name, NotNull: c.NotNull, Labels: map[string]string{}}
		switch c.Type.Kind {
		case model.TypeDataElement:
			de := x.schema.DataElements[c.Type.Name]
			if de == nil {
				return nil, fmt.Errorf("kernel: data element %s of %s.%s is missing", c.Type.Name, structure, c.Name)
			}
			nc.Domain = x.schema.Domains[de.Domain]
			for lang, t := range de.Texts {
				nc.Labels[lang] = t.Heading
			}
		case model.TypeDomain:
			nc.Domain = x.schema.Domains[c.Type.Name]
		default:
			return nil, fmt.Errorf("kernel: %s.%s is no simple component", structure, c.Name)
		}
		if nc.Domain == nil {
			return nil, fmt.Errorf("kernel: the domain of %s.%s is missing", structure, c.Name)
		}
		def := c.Default
		if def.IsZero() {
			def = nc.Domain.Default
		}
		if def.Kind == model.DefaultValue {
			nc.Default = model.Some(def.Value)
		}
		out = append(out, nc)
	}
	return out, nil
}

// objectType returns a package-defined type whose objects code of reader
// may read: the defining package, packages that require it and kernel
// packages.
func (x *execution) objectType(reader model.PackageID, typeName string) (*model.ObjectType, error) {
	t := x.schema.ObjectTypes[typeName]
	if t == nil {
		return nil, &native.ObjectTypeNotVisible{Reader: reader, Type: typeName}
	}
	if !x.KernelPackage(reader) && !x.schema.Visible(reader, t.Owner) {
		return nil, &native.ObjectTypeNotVisible{Reader: reader, Type: typeName, Exists: true}
	}
	return t, nil
}

func (x *execution) Objects(reader model.PackageID, typeName string, pkg model.PackageID, ref *model.ObjectReference) ([]*model.Object, error) {
	if _, err := x.objectType(reader, typeName); err != nil {
		return nil, err
	}
	var out []*model.Object
	for _, o := range x.schema.ObjectsOf(typeName) {
		if pkg != "" && o.Owner != pkg {
			continue
		}
		if ref != nil && !slices.ContainsFunc(o.References, func(r model.ObjectReference) bool {
			return r.Kind == ref.Kind && r.Target == ref.Target && (ref.Part == "" || r.Part == ref.Part)
		}) {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func (x *execution) Object(reader model.PackageID, name string) (*model.Object, error) {
	o := x.schema.Objects[name]
	if o == nil {
		return nil, nil
	}
	if _, err := x.objectType(reader, o.Type); err != nil {
		return nil, err
	}
	return o, nil
}
