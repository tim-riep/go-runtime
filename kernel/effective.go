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
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
)

// effectiveCacheSize limits the cached effective authorizations.
const effectiveCacheSize = 10000

// EffectiveAuthorizations returns the authorizations of a user in a tenant
// on a day, from the role assignments valid on the day. Results are kept
// per user, tenant and day until /RT/AUTH_GENERATION changes, which every
// change of roles, assignments, users and packages does in its
// transaction; each call reads the counter, so that kernels on the same
// database see changes of each other at once.
func (k *Kernel) EffectiveAuthorizations(ctx context.Context, user, tenant string, date time.Time) (*authorization.Effective, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	gen, err := k.repo.AuthGeneration(ctx, k.db)
	if err != nil {
		return nil, err
	}
	key := effectiveKey{user, tenant, date.UTC().Format("2006-01-02")}
	if e, ok := k.effective.get(key, gen); ok {
		return e, nil
	}
	u, err := k.user(ctx, k.db, user)
	if err != nil {
		return nil, err
	}
	s, err := k.repo.Load(ctx, k.db)
	if err != nil {
		return nil, err
	}
	list, err := k.repo.RoleAssignments(ctx, k.db, repo.AssignmentFilter{User: user, Tenant: tenant})
	if err != nil {
		return nil, err
	}
	assignments := make([]authorization.Assignment, len(list))
	for i, a := range list {
		assignments[i] = authorization.Assignment{ID: a.ID, Role: a.Role, ValidFrom: a.ValidFrom, ValidTo: a.ValidTo}
	}
	k.effectiveComputed.Add(1)
	e := authorization.Resolve(s, user, tenant, date.UTC(), u.Status == "locked", assignments)
	e.Generation = gen
	k.effective.put(key, e)
	return e, nil
}

type effectiveKey struct{ user, tenant, day string }

// effectiveCache keeps effective authorizations, the least recently used
// going first; an entry of an older generation is a miss.
type effectiveCache struct {
	mu    sync.Mutex
	order *list.List // of *effectiveEntry, the most recent first
	byKey map[effectiveKey]*list.Element
}

type effectiveEntry struct {
	key effectiveKey
	e   *authorization.Effective
}

func newEffectiveCache() *effectiveCache {
	return &effectiveCache{order: list.New(), byKey: map[effectiveKey]*list.Element{}}
}

func (c *effectiveCache) get(key effectiveKey, gen int64) (*authorization.Effective, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.byKey[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*effectiveEntry).e
	if e.Generation != gen {
		c.order.Remove(el)
		delete(c.byKey, key)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e, true
}

func (c *effectiveCache) put(key effectiveKey, e *authorization.Effective) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		el.Value.(*effectiveEntry).e = e
		c.order.MoveToFront(el)
		return
	}
	c.byKey[key] = c.order.PushFront(&effectiveEntry{key, e})
	for c.order.Len() > effectiveCacheSize {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.byKey, last.Value.(*effectiveEntry).key)
	}
}

// noteAllAuthorizations records a sign-in of a user who has all
// authorizations in the tenant, in the history and the log.
func (k *Kernel) noteAllAuthorizations(ctx context.Context, user, tenant string) {
	if !k.repo.HasRoles() {
		return
	}
	e, err := k.EffectiveAuthorizations(ctx, user, tenant, k.now())
	if err != nil {
		slog.Error("reading effective authorizations", "user", user, "tenant", tenant, "error", err)
		return
	}
	if !e.All {
		return
	}
	audit(ctx, "sign-in with all authorizations", "user", user, "tenant", tenant)
	err = k.repo.InsertRoleChange(ctx, k.db, RoleChange{ChangedAt: k.timestamp(), ChangedBy: user, Kind: ChangeAllSignIn,
		Role: model.Some(model.AllAuthorizationsRole), Tenant: model.Some(tenant), User: model.Some(user)})
	if err != nil {
		slog.Error("recording a sign-in with all authorizations", "user", user, "tenant", tenant, "error", err)
	}
}
