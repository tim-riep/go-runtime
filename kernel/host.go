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
	"io/fs"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
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

func (x *execution) Assets(caller, pkg model.PackageID, repo string, lang model.Optional[string]) (fs.FS, error) {
	allowed := caller == pkg
	if info := x.schema.Packages[caller]; info != nil {
		for _, r := range info.Manifest.Requires {
			allowed = allowed || r.ID == pkg
		}
	}
	if !allowed {
		return nil, fs.ErrNotExist
	}
	fsys, err := x.k.AssetFS(x.ctx, pkg, repo, lang)
	var nf *rterr.NotFound
	var anf *rterr.AssetNotFound
	if errors.As(err, &nf) || errors.As(err, &anf) {
		return nil, fs.ErrNotExist
	}
	return fsys, err
}

// SetClock replaces the clock of the kernel, for tests of code with the
// capability clock.
func (k *Kernel) SetClock(now func() time.Time) { k.now = now }
