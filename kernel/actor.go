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

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Actor is the user on whose behalf an application calls the user and
// role functions of the library: with an actor, every function first
// checks the actor's authorizations in Tenant and fails with
// *rterr.NotAuthorized. Without actor, as on the command line, nothing is
// checked.
type Actor struct {
	User   string
	Tenant string
}

// Authorization objects of the kernel package TRBASE for administration.
const (
	UserAdminObject      = "/TRBASE/USER_ADMIN"
	RoleAdminObject      = "/TRBASE/ROLE_ADMIN"
	RoleAssignmentObject = "/TRBASE/ROLE_ASSIGNMENT"
)

// As returns the user functions on behalf of an actor.
func (u *Users) As(a Actor) *Users { return &Users{k: u.k, actor: &a} }

// As returns the role functions on behalf of an actor.
func (r *Roles) As(a Actor) *Roles { return &Roles{k: r.k, actor: &a} }

// effective returns the effective authorizations of an actor, nil
// without actor.
func (k *Kernel) actorAuthorizations(ctx context.Context, a *Actor) (*authorization.Effective, error) {
	if a == nil {
		return nil, nil
	}
	return k.EffectiveAuthorizations(ctx, a.User, a.Tenant, k.now())
}

// authorizeActor checks an activity of an actor on an object with the
// values of its other fields; without actor it passes.
func (k *Kernel) authorizeActor(ctx context.Context, a *Actor, object, activity string, values map[string]string, function string) error {
	if a == nil {
		return nil
	}
	e, err := k.actorAuthorizations(ctx, a)
	if err != nil {
		return err
	}
	c := authorization.Check{Object: object, Values: map[string]string{model.ActivityField: activity}}
	for f, v := range values {
		c.Values[f] = v
	}
	if e.Check(c) {
		return nil
	}
	k.rememberFailedCheck(ctx, a.User, a.Tenant, c, function)
	return &rterr.NotAuthorized{User: a.User, Object: object, Activity: activity}
}

// actorMay reports whether an actor may do an activity, for filtering
// lists; without actor everything is allowed.
func actorMay(e *authorization.Effective, a *Actor, object, activity string, values map[string]string) bool {
	if a == nil {
		return true
	}
	c := authorization.Check{Object: object, Values: map[string]string{model.ActivityField: activity}}
	for f, v := range values {
		c.Values[f] = v
	}
	return e.Check(c)
}

// by returns who makes a change for the history: the actor, else opts.By.
func (o RoleOptions) byActor(a *Actor) string {
	if a != nil && o.By == "" {
		return a.User
	}
	return o.by()
}
