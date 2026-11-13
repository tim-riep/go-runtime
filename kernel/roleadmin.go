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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/kernelpkg/natives"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/rterr"
)

// EffectiveRole is a role of a user in a tenant on a day: an assigned
// role, or a member of an assigned composite role.
type EffectiveRole struct {
	Role string `json:"role"`
	// Composite is the assigned composite role the role is a member of.
	Composite  model.Optional[string]    `json:"composite"`
	Assignment string                    `json:"assignment"`
	ValidFrom  model.Optional[time.Time] `json:"validFrom"`
	ValidTo    model.Optional[time.Time] `json:"validTo"`
}

// Effective returns the roles of a user in a tenant on a day: the
// assigned roles valid on the day, composite roles with their members (one
// level, as for authorizations), also roles without authorizations. A
// locked user has none. The list is ordered by role and composite role.
func (r *Roles) Effective(ctx context.Context, user, tenant string, day time.Time) ([]EffectiveRole, error) {
	k := r.k
	if err := k.requireInit(ctx, r.db()); err != nil {
		return nil, err
	}
	u, err := k.user(ctx, r.db(), user)
	if err != nil {
		return nil, err
	}
	out := []EffectiveRole{}
	if u.Status == "locked" {
		return out, nil
	}
	s, err := r.schema(ctx)
	if err != nil {
		return nil, err
	}
	list, err := k.repo.RoleAssignments(ctx, r.db(), repo.AssignmentFilter{User: user, Tenant: tenant})
	if err != nil {
		return nil, err
	}
	d := day.UTC().Truncate(24 * time.Hour)
	seen := map[[2]string]bool{}
	add := func(e EffectiveRole) {
		key := [2]string{e.Role, e.Composite.Or("")}
		if !seen[key] {
			seen[key] = true
			out = append(out, e)
		}
	}
	for _, a := range list {
		if f, ok := a.ValidFrom.Get(); ok && d.Before(f) {
			continue
		}
		if t, ok := a.ValidTo.Get(); ok && d.After(t) {
			continue
		}
		e := EffectiveRole{Role: a.Role, Assignment: a.ID, ValidFrom: a.ValidFrom, ValidTo: a.ValidTo}
		add(e)
		if role := s.Roles[a.Role]; role != nil && role.Composite {
			for _, m := range role.Roles {
				add(EffectiveRole{Role: m, Composite: model.Some(a.Role), Assignment: a.ID, ValidFrom: a.ValidFrom, ValidTo: a.ValidTo})
			}
		}
	}
	slices.SortFunc(out, func(a, b EffectiveRole) int {
		return cmp.Or(cmp.Compare(a.Role, b.Role), cmp.Compare(a.Composite.Or(""), b.Composite.Or("")))
	})
	return out, nil
}

// administration gives native methods the role and user functions of the
// kernel for the user of the execution, in the transaction of its default
// session. Without user every function is refused.
func (x *execution) administration(ctx context.Context, changes bool) (*Roles, *Users, func(error) error, error) {
	user, ok := x.opts.User.Get()
	if !ok || x.tenant == "" {
		return nil, nil, nil, &native.NotAuthorized{Text: "the role and user functions need a signed-in user and a tenant"}
	}
	if changes && x.opts.ReadOnly {
		return nil, nil, nil, errReadOnly
	}
	s := x.sessions[0]
	if err := s.begin(ctx); err != nil {
		return nil, nil, nil, x.classify(err)
	}
	// A savepoint keeps a failed change from ending the transaction of the
	// execution under PostgreSQL.
	if _, err := s.conn.ExecContext(ctx, "SAVEPOINT rt_admin"); err != nil {
		return nil, nil, nil, x.classify(err)
	}
	end := func(err error) error {
		if err != nil {
			if _, rerr := s.conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT rt_admin"); rerr != nil {
				return errors.Join(err, rerr)
			}
		} else if changes {
			s.written = true
		}
		if _, rerr := s.conn.ExecContext(ctx, "RELEASE SAVEPOINT rt_admin"); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	a := Actor{User: user, Tenant: x.tenant}
	return x.k.Roles().in(s.conn).As(a), (&Users{k: x.k}).As(a), end, nil
}

// adminError turns errors of the role functions into exceptions of TRBASE.
func adminError(err error) error {
	var na *rterr.NotAuthorized
	var cr *rterr.ConfirmationRequired
	var nf *rterr.NotFound
	var ia *rterr.InvalidArgument
	var ve *rterr.ValidationError
	var list rterr.List
	switch {
	case err == nil:
		return nil
	case errors.As(err, &na):
		return &native.NotAuthorized{Check: authorization.Check{Object: na.Object, Values: map[string]string{model.ActivityField: na.Activity}}, Text: na.Error()}
	case errors.As(err, &cr):
		return native.Raise("/TRBASE/ROLE_CONFIRMATION_REQUIRED", cr.Error()+"; pass confirm true")
	case errors.As(err, &nf), errors.As(err, &ia), errors.As(err, &ve), errors.As(err, &list):
		return native.Raise("/TRBASE/ROLE_ERROR", err.Error())
	}
	return err
}

func executionOf(c *native.Context) (*execution, error) {
	x, ok := c.Host.(*execution)
	if !ok {
		return nil, fmt.Errorf("%s needs the kernel", c.Method)
	}
	return x, nil
}

func dateOf(t model.Optional[time.Time]) model.Optional[native.Date] {
	if v, ok := t.Get(); ok {
		return model.Some(native.DateOf(v))
	}
	return model.None[native.Date]()
}

func timeOf(d model.Optional[native.Date]) model.Optional[time.Time] {
	if v, ok := d.Get(); ok {
		return model.Some(v.Time())
	}
	return model.None[time.Time]()
}

func roleAuthorizations(as []model.Authorization, role, composite model.Optional[string]) []natives.RoleAuthorization {
	out := []natives.RoleAuthorization{}
	for _, a := range as {
		ra := natives.RoleAuthorization{Object: a.Object, Fields: []natives.RoleField{}, RoleName: role, Composite: composite}
		for _, f := range a.FieldNames() {
			rf := natives.RoleField{Field: f, Values: []natives.RoleValue{}}
			for _, e := range a.Fields[f] {
				v := natives.RoleValue{Kind: natives.RoleValueKind(e.Kind), Low: e.Low}
				if e.Kind == model.ValueRange {
					v.High = model.Some(e.High)
				}
				rf.Values = append(rf.Values, v)
			}
			ra.Fields = append(ra.Fields, rf)
		}
		out = append(out, ra)
	}
	return out
}

func roleTexts(r *model.Role) []natives.RoleText {
	out := []natives.RoleText{}
	for _, l := range r.Languages() {
		out = append(out, natives.RoleText{Language: l, Label: r.Texts[l].Label, Description: r.Texts[l].Description})
	}
	return out
}

// roleOf builds a local role from its definition. Values are given in
// their canonical text form.
func roleOf(d natives.RoleDefinition) *model.Role {
	r := &model.Role{Name: d.Name, Owner: model.Local, Composite: d.Composite, Texts: map[string]model.Label{}, Roles: d.Roles.Or(nil)}
	for _, t := range d.Texts {
		r.Texts[t.Language] = model.Label{Label: t.Label, Description: t.Description}
	}
	for _, a := range d.Authorizations.Or(nil) {
		ma := model.Authorization{Object: a.Object, Fields: map[string][]model.ValueEntry{}}
		for _, f := range a.Fields {
			for _, v := range f.Values {
				ma.Fields[f.Field] = append(ma.Fields[f.Field], model.ValueEntry{Kind: model.ValueKind(v.Kind), Low: v.Low, High: v.High.Or("")})
			}
		}
		r.Authorizations = append(r.Authorizations, ma)
	}
	return r
}

func assignmentInfo(a RoleAssignment) natives.RoleAssignmentInfo {
	return natives.RoleAssignmentInfo{ID: a.ID, UserName: a.User, RoleName: a.Role, Tenant: a.Tenant, ValidFrom: dateOf(a.ValidFrom), ValidTo: dateOf(a.ValidTo),
		CreatedAt: a.CreatedAt, CreatedBy: a.CreatedBy}
}

func effectiveRoles(list []EffectiveRole) []natives.EffectiveRole {
	out := []natives.EffectiveRole{}
	for _, e := range list {
		out = append(out, natives.EffectiveRole{RoleName: e.Role, Composite: e.Composite, ValidFrom: dateOf(e.ValidFrom), ValidTo: dateOf(e.ValidTo)})
	}
	return out
}

// The natives of TRBASE that need the kernel: the roles of the context, the
// role administration and the users.
func init() {
	const pkg = "TRBASE"
	static := func(class, method, sig string, fn any) {
		native.Default.Register(native.Method{Package: pkg, Class: class, Method: method, Static: true, Signature: sig, Func: fn})
	}
	const admin = "/TRBASE/ROLE_ADMINISTRATION"
	const raises = " raises /TRBASE/ROLE_ERROR"

	static("/TRBASE/CONTEXT", "ROLES", "() returns /TRBASE/EFFECTIVE_ROLE_ARRAY", func(c *native.Context) ([]natives.EffectiveRole, error) {
		x, err := executionOf(c)
		if err != nil {
			return nil, err
		}
		user, ok := x.opts.User.Get()
		if !ok || x.tenant == "" {
			return []natives.EffectiveRole{}, nil
		}
		list, err := x.k.Roles().Effective(c, user, x.tenant, x.k.now())
		if err != nil {
			return nil, err
		}
		return effectiveRoles(list), nil
	})

	// call runs fn with the role functions of the execution.
	call := func(c *native.Context, changes bool, fn func(r *Roles, u *Users) error) error {
		x, err := executionOf(c)
		if err != nil {
			return err
		}
		r, u, end, err := x.administration(c, changes)
		if err != nil {
			return err
		}
		return adminError(end(fn(r, u)))
	}

	static(admin, "ROLES", "() returns /TRBASE/ROLE_INFO_ARRAY", func(c *native.Context) ([]natives.RoleInfo, error) {
		out := []natives.RoleInfo{}
		err := call(c, false, func(r *Roles, _ *Users) error {
			list, err := r.List(c)
			for _, s := range list {
				info := natives.RoleInfo{Name: s.Name, Composite: s.Composite, Incomplete: int64(s.Incomplete)}
				if s.Package != model.Local {
					info.PackageID = model.Some(string(s.Package))
				}
				if s.Label != "" {
					info.Label = model.Some(s.Label)
				}
				out = append(out, info)
			}
			return err
		})
		return out, err
	})
	static(admin, "ROLE", "(name: STRING) returns /TRBASE/ROLE_DETAIL"+raises, func(c *native.Context, name string) (natives.RoleDetail, error) {
		var out natives.RoleDetail
		err := call(c, false, func(r *Roles, _ *Users) error {
			d, err := r.Get(c, name)
			if err != nil {
				return err
			}
			role := d.Role
			out = natives.RoleDetail{Name: role.Name, Composite: role.Composite, Texts: roleTexts(role), All: d.All, UsedBy: d.UsedBy,
				Assignments: int64(d.Assignments), Effect: []natives.RoleAuthorization{}}
			if role.Owner != model.Local {
				out.PackageID = model.Some(string(role.Owner))
			}
			if role.Composite {
				out.Roles = model.Some(slices.Clone(role.Roles))
			} else {
				out.Authorizations = model.Some(roleAuthorizations(role.Authorizations, model.None[string](), model.None[string]()))
			}
			for _, a := range d.Authorizations {
				out.Effect = append(out.Effect, roleAuthorizations([]model.Authorization{a.Authorization}, model.Some(a.Role), model.None[string]())...)
			}
			return nil
		})
		return out, err
	})
	static(admin, "CREATE", "(role: /TRBASE/ROLE_DEFINITION)"+raises, func(c *native.Context, d natives.RoleDefinition) error {
		return call(c, true, func(r *Roles, _ *Users) error { return r.Create(c, roleOf(d), RoleOptions{}) })
	})
	static(admin, "UPDATE", "(role: /TRBASE/ROLE_DEFINITION)"+raises, func(c *native.Context, d natives.RoleDefinition) error {
		return call(c, true, func(r *Roles, _ *Users) error { return r.Update(c, roleOf(d), RoleOptions{}) })
	})
	static(admin, "DELETE", "(name: STRING, confirm: BOOL)"+raises, func(c *native.Context, name string, confirm bool) error {
		return call(c, true, func(r *Roles, _ *Users) error { return r.Delete(c, name, RoleOptions{Confirm: confirm}) })
	})
	static(admin, "ASSIGNMENTS", "(user: STRING?, role: STRING?, tenant: STRING?) returns /TRBASE/ROLE_ASSIGNMENT_ARRAY",
		func(c *native.Context, user, role, tenant model.Optional[string]) ([]natives.RoleAssignmentInfo, error) {
			out := []natives.RoleAssignmentInfo{}
			err := call(c, false, func(r *Roles, _ *Users) error {
				list, err := r.Assignments(c, user.Or(""), tenant.Or(""))
				for _, a := range list {
					if rn, ok := role.Get(); !ok || a.Role == rn {
						out = append(out, assignmentInfo(a))
					}
				}
				return err
			})
			return out, err
		})
	static(admin, "ASSIGN", "(user: STRING, role: STRING, tenant: STRING, first: DATE?, last: DATE?) returns /TRBASE/ROLE_ASSIGN_RESULT"+raises,
		func(c *native.Context, user, role, tenant string, first, last model.Optional[native.Date]) (natives.RoleAssignResult, error) {
			var out natives.RoleAssignResult
			err := call(c, true, func(r *Roles, _ *Users) error {
				a, overlaps, err := r.Assign(c, Assignment{User: user, Role: role, Tenant: tenant, ValidFrom: timeOf(first), ValidTo: timeOf(last)}, RoleOptions{})
				if err != nil {
					return err
				}
				out = natives.RoleAssignResult{Assignment: assignmentInfo(a), Overlaps: []natives.RoleAssignmentInfo{}}
				for _, o := range overlaps {
					out.Overlaps = append(out.Overlaps, assignmentInfo(o))
				}
				return nil
			})
			return out, err
		})
	static(admin, "UNASSIGN", "(id: STRING, confirm: BOOL)"+raises, func(c *native.Context, id string, confirm bool) error {
		return call(c, true, func(r *Roles, _ *Users) error { return r.Unassign(c, id, RoleOptions{Confirm: confirm}) })
	})
	static(admin, "AUTHORIZATIONS", "(user: STRING, tenant: STRING, day: DATE?) returns /TRBASE/EFFECTIVE_AUTHORIZATIONS"+raises,
		func(c *native.Context, user, tenant string, day model.Optional[native.Date]) (natives.EffectiveAuthorizations, error) {
			var out natives.EffectiveAuthorizations
			err := call(c, false, func(r *Roles, _ *Users) error {
				if err := r.authorizeAssignment(c, "READ", "", tenant, "ROLE_ADMINISTRATION.AUTHORIZATIONS"); err != nil {
					return err
				}
				at := r.k.now()
				if d, ok := day.Get(); ok {
					at = d.Time()
				}
				roles, err := r.Effective(c, user, tenant, at)
				if err != nil {
					return err
				}
				u, err := r.k.user(c, r.db(), user)
				if err != nil {
					return err
				}
				s, err := r.schema(c)
				if err != nil {
					return err
				}
				out = natives.EffectiveAuthorizations{UserName: user, Tenant: tenant, Day: native.DateOf(at), Locked: u.Status == "locked",
					Roles: effectiveRoles(roles), Authorizations: []natives.RoleAuthorization{}}
				for _, e := range roles {
					out.All = out.All || e.Role == model.AllAuthorizationsRole
					if role := s.Roles[e.Role]; role != nil && !role.Composite {
						out.Authorizations = append(out.Authorizations, roleAuthorizations(role.Authorizations, model.Some(e.Role), e.Composite)...)
					}
				}
				return nil
			})
			return out, err
		})
	static(admin, "HISTORY", "(role: STRING?, user: STRING?) returns /TRBASE/ROLE_CHANGE_ARRAY",
		func(c *native.Context, role, user model.Optional[string]) ([]natives.RoleChange, error) {
			out := []natives.RoleChange{}
			err := call(c, false, func(r *Roles, _ *Users) error {
				if err := r.authorize(c, "READ", role.Or(""), "ROLE_ADMINISTRATION.HISTORY"); err != nil {
					return err
				}
				list, err := r.History(c, role.Or(""), user.Or(""))
				for _, h := range list {
					out = append(out, natives.RoleChange{ID: h.ID, ChangedAt: h.ChangedAt, ChangedBy: h.ChangedBy, Kind: h.Kind, RoleName: h.Role,
						Tenant: h.Tenant, UserName: h.User, Assignment: h.Assignment, OldState: h.OldState, NewState: h.NewState})
				}
				return err
			})
			return out, err
		})
	static("/TRBASE/USER_ADMINISTRATION", "USERS", "() returns /TRBASE/USER_INFO_ARRAY", func(c *native.Context) ([]natives.UserInfo, error) {
		out := []natives.UserInfo{}
		err := call(c, false, func(_ *Roles, u *Users) error {
			list, err := u.List(c)
			for _, x := range list {
				tenants := slices.Clone(x.Tenants)
				if tenants == nil {
					tenants = []string{}
				}
				out = append(out, natives.UserInfo{UserName: x.Name, Status: natives.UserStatus(x.Status), Language: x.Language,
					DefaultTenant: x.DefaultTenant, Tenants: tenants})
			}
			return err
		})
		return out, err
	})
}
