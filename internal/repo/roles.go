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

package repo

import (
	"context"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of roles.
const (
	TRoles              = model.KernelNamespace + "ROLES"
	TRoleTexts          = model.KernelNamespace + "ROLE_TEXTS"
	TRoleAuthorizations = model.KernelNamespace + "ROLE_AUTHORIZATIONS"
	TRoleAuthValues     = model.KernelNamespace + "ROLE_AUTH_VALUES"
	TCompositeRoles     = model.KernelNamespace + "COMPOSITE_ROLES"
	TRoleAssignments    = model.KernelNamespace + "ROLE_ASSIGNMENTS"
	TRoleHistory        = model.KernelNamespace + "ROLE_HISTORY"
	TAuthGeneration     = model.KernelNamespace + "AUTH_GENERATION"
)

// roleOwner reads the owner column of roles: NULL for the roles of the
// kernel, in its namespace, and for local roles.
func roleOwner(v any, name string) model.PackageID {
	if s, ok := v.(string); ok {
		return model.PackageID(s)
	}
	if model.IsKernelName(name) {
		return model.Kernel
	}
	return model.Local
}

func roleOwnerValue(id model.PackageID) any {
	if id == model.Local || id == model.Kernel {
		return nil
	}
	return string(id)
}

// Role reads one role as stored, or nil.
func (r *Repo) Role(ctx context.Context, q dialect.Querier, name string) (*model.Role, error) {
	s := model.NewSchema()
	if err := r.loadRolesWhere(ctx, q, s, row{"ROLE_NAME": name}); err != nil {
		return nil, err
	}
	return s.Roles[name], nil
}

func (r *Repo) loadRoles(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	return r.loadRolesWhere(ctx, q, s, nil)
}

// loadRolesWhere reads the roles that match the filter, nil for all.
func (r *Repo) loadRolesWhere(ctx context.Context, q dialect.Querier, s *model.Schema, filter row) error {
	roles, err := r.selectWhere(ctx, q, TRoles, []string{"ROLE_NAME", "PACKAGE_ID", "COMPOSITE", "FILE_PATH"}, filter, "ROLE_NAME")
	if err != nil {
		return err
	}
	for _, x := range roles {
		name := str(x["ROLE_NAME"])
		composite, _ := x["COMPOSITE"].(bool)
		s.Roles[name] = &model.Role{Name: name, Owner: roleOwner(x["PACKAGE_ID"], name), File: opt(x["FILE_PATH"]).Or(""), Composite: composite,
			Texts: map[string]model.Label{}}
	}
	texts, err := r.selectWhere(ctx, q, TRoleTexts, []string{"ROLE_NAME", "LANGUAGE", "LABEL", "DESCRIPTION"}, filter)
	if err != nil {
		return err
	}
	for _, t := range texts {
		if role := s.Roles[str(t["ROLE_NAME"])]; role != nil {
			role.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	auths, err := r.selectWhere(ctx, q, TRoleAuthorizations, []string{"ROLE_NAME", "POSITION", "OBJECT_NAME"}, filter, "ROLE_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, a := range auths {
		if role := s.Roles[str(a["ROLE_NAME"])]; role != nil {
			role.Authorizations = append(role.Authorizations, model.Authorization{Object: str(a["OBJECT_NAME"]), Fields: map[string][]model.ValueEntry{}})
		}
	}
	values, err := r.selectWhere(ctx, q, TRoleAuthValues, []string{"ROLE_NAME", "POSITION", "FIELD_NAME", "VALUE_NO", "VALUE_KIND", "LOW", "HIGH"},
		filter, "ROLE_NAME", "POSITION", "FIELD_NAME", "VALUE_NO")
	if err != nil {
		return err
	}
	for _, x := range values {
		role := s.Roles[str(x["ROLE_NAME"])]
		pos := num(x["POSITION"])
		if role == nil || pos < 1 || pos > len(role.Authorizations) {
			continue
		}
		a := role.Authorizations[pos-1]
		field := str(x["FIELD_NAME"])
		a.Fields[field] = append(a.Fields[field], model.ValueEntry{Kind: model.ValueKind(str(x["VALUE_KIND"])), Low: str(x["LOW"]), High: opt(x["HIGH"]).Or("")})
	}
	members, err := r.selectWhere(ctx, q, TCompositeRoles, []string{"ROLE_NAME", "POSITION", "MEMBER_ROLE"}, filter, "ROLE_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, m := range members {
		if role := s.Roles[str(m["ROLE_NAME"])]; role != nil {
			role.Roles = append(role.Roles, str(m["MEMBER_ROLE"]))
		}
	}
	return nil
}

func (r *Repo) writeRoles(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.RoleNames() {
		if err := r.writeRole(ctx, q, s.Roles[name]); err != nil {
			return err
		}
	}
	return nil
}

// writeRole inserts the rows of a role.
func (r *Repo) writeRole(ctx context.Context, q dialect.Querier, role *model.Role) error {
	file := model.None[string]()
	if role.File != "" {
		file = model.Some(role.File)
	}
	if err := r.insert(ctx, q, TRoles, row{"ROLE_NAME": role.Name, "PACKAGE_ID": roleOwnerValue(role.Owner), "COMPOSITE": role.Composite, "FILE_PATH": val(file)}); err != nil {
		return err
	}
	for _, lang := range role.Languages() {
		l := role.Texts[lang]
		if err := r.insert(ctx, q, TRoleTexts, row{"ROLE_NAME": role.Name, "LANGUAGE": lang, "LABEL": l.Label, "DESCRIPTION": val(l.Description)}); err != nil {
			return err
		}
	}
	for i, a := range role.Authorizations {
		pos := int64(i + 1)
		if err := r.insert(ctx, q, TRoleAuthorizations, row{"ROLE_NAME": role.Name, "POSITION": pos, "OBJECT_NAME": a.Object}); err != nil {
			return err
		}
		for _, field := range a.FieldNames() {
			for j, e := range a.Fields[field] {
				high := any(nil)
				if e.Kind == model.ValueRange {
					high = e.High
				}
				if err := r.insert(ctx, q, TRoleAuthValues, row{"ROLE_NAME": role.Name, "POSITION": pos, "FIELD_NAME": field, "VALUE_NO": int64(j + 1),
					"VALUE_KIND": string(e.Kind), "LOW": e.Low, "HIGH": high}); err != nil {
					return err
				}
			}
		}
	}
	for i, m := range role.Roles {
		if err := r.insert(ctx, q, TCompositeRoles, row{"ROLE_NAME": role.Name, "POSITION": int64(i + 1), "MEMBER_ROLE": m}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRole removes the rows of a role.
func (r *Repo) DeleteRole(ctx context.Context, q dialect.Querier, name string) error {
	for _, t := range []string{TRoleAuthValues, TRoleAuthorizations, TCompositeRoles, TRoleTexts, TRoles} {
		if err := r.deleteWhere(ctx, q, t, row{"ROLE_NAME": name}); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceRole writes a role in place of the one with its name.
func (r *Repo) ReplaceRole(ctx context.Context, q dialect.Querier, role *model.Role) error {
	if err := r.DeleteRole(ctx, q, role.Name); err != nil {
		return err
	}
	return r.writeRole(ctx, q, role)
}

// RoleAssignment is a role assigned to a user in a tenant.
type RoleAssignment struct {
	ID        string                    `json:"id"`
	Tenant    string                    `json:"tenant"`
	User      string                    `json:"user"`
	Role      string                    `json:"role"`
	ValidFrom model.Optional[time.Time] `json:"validFrom"` // first day, None for open
	ValidTo   model.Optional[time.Time] `json:"validTo"`   // last day, None for open
	CreatedAt time.Time                 `json:"createdAt"`
	CreatedBy string                    `json:"createdBy"`
}

// AssignmentFilter selects role assignments; empty fields select all.
type AssignmentFilter struct {
	ID, Tenant, User, Role string
}

func optDay(v any) model.Optional[time.Time] {
	if t, ok := v.(time.Time); ok {
		return model.Some(t.UTC())
	}
	return model.None[time.Time]()
}

func dayValue(d model.Optional[time.Time]) any {
	if t, ok := d.Get(); ok {
		return t
	}
	return nil
}

// RoleAssignments returns the role assignments that match the filter,
// ordered by tenant, user, role and the start of validity.
func (r *Repo) RoleAssignments(ctx context.Context, q dialect.Querier, f AssignmentFilter) ([]RoleAssignment, error) {
	filter := row{}
	for col, v := range map[string]string{"ASSIGNMENT_ID": f.ID, model.TenantField: f.Tenant, "USER_NAME": f.User, "ROLE_NAME": f.Role} {
		if v != "" {
			filter[col] = v
		}
	}
	rows, err := r.selectWhere(ctx, q, TRoleAssignments, []string{model.TenantField, "ASSIGNMENT_ID", "USER_NAME", "ROLE_NAME", "VALID_FROM", "VALID_TO",
		"CREATED_AT", "CREATED_BY"}, filter, model.TenantField, "USER_NAME", "ROLE_NAME", "VALID_FROM", "CREATED_AT")
	if err != nil {
		return nil, err
	}
	out := []RoleAssignment{}
	for _, x := range rows {
		out = append(out, RoleAssignment{ID: str(x["ASSIGNMENT_ID"]), Tenant: str(x[model.TenantField]), User: str(x["USER_NAME"]), Role: str(x["ROLE_NAME"]),
			ValidFrom: optDay(x["VALID_FROM"]), ValidTo: optDay(x["VALID_TO"]), CreatedAt: tm(x["CREATED_AT"]), CreatedBy: str(x["CREATED_BY"])})
	}
	return out, nil
}

// InsertRoleAssignment stores a role assignment.
func (r *Repo) InsertRoleAssignment(ctx context.Context, q dialect.Querier, a RoleAssignment) error {
	return r.insert(ctx, q, TRoleAssignments, row{model.TenantField: a.Tenant, "ASSIGNMENT_ID": a.ID, "USER_NAME": a.User, "ROLE_NAME": a.Role,
		"VALID_FROM": dayValue(a.ValidFrom), "VALID_TO": dayValue(a.ValidTo), "CREATED_AT": a.CreatedAt, "CREATED_BY": a.CreatedBy})
}

// DeleteRoleAssignment removes a role assignment.
func (r *Repo) DeleteRoleAssignment(ctx context.Context, q dialect.Querier, a RoleAssignment) error {
	return r.deleteWhere(ctx, q, TRoleAssignments, row{model.TenantField: a.Tenant, "ASSIGNMENT_ID": a.ID})
}

// RoleChange is an entry of the history of roles and role assignments.
type RoleChange struct {
	ID         string                 `json:"id"`
	ChangedAt  time.Time              `json:"changedAt"`
	ChangedBy  string                 `json:"changedBy"`
	Kind       string                 `json:"kind"`
	Role       model.Optional[string] `json:"role"`
	Tenant     model.Optional[string] `json:"tenant"`
	User       model.Optional[string] `json:"user"`
	Assignment model.Optional[string] `json:"assignment"`
	OldState   model.Optional[string] `json:"oldState"` // JSON
	NewState   model.Optional[string] `json:"newState"` // JSON
}

// InsertRoleChange adds an entry to the history. Entries are never
// changed or deleted.
func (r *Repo) InsertRoleChange(ctx context.Context, q dialect.Querier, c RoleChange) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	return r.insert(ctx, q, TRoleHistory, row{"CHANGE_ID": c.ID, "CHANGED_AT": c.ChangedAt, "CHANGED_BY": c.ChangedBy, "CHANGE_KIND": c.Kind,
		"ROLE_NAME": val(c.Role), "TENANT": val(c.Tenant), "USER_NAME": val(c.User), "ASSIGNMENT_ID": val(c.Assignment),
		"OLD_STATE": val(c.OldState), "NEW_STATE": val(c.NewState)})
}

// RoleChanges returns the history of a role, of a user, or all for "",
// ordered by time.
func (r *Repo) RoleChanges(ctx context.Context, q dialect.Querier, role, user string) ([]RoleChange, error) {
	filter := row{}
	if role != "" {
		filter["ROLE_NAME"] = role
	}
	if user != "" {
		filter["USER_NAME"] = user
	}
	rows, err := r.selectWhere(ctx, q, TRoleHistory, []string{"CHANGE_ID", "CHANGED_AT", "CHANGED_BY", "CHANGE_KIND", "ROLE_NAME", "TENANT", "USER_NAME",
		"ASSIGNMENT_ID", "OLD_STATE", "NEW_STATE"}, filter, "CHANGED_AT", "CHANGE_ID")
	if err != nil {
		return nil, err
	}
	out := []RoleChange{}
	for _, x := range rows {
		out = append(out, RoleChange{ID: str(x["CHANGE_ID"]), ChangedAt: tm(x["CHANGED_AT"]), ChangedBy: str(x["CHANGED_BY"]), Kind: str(x["CHANGE_KIND"]),
			Role: opt(x["ROLE_NAME"]), Tenant: opt(x["TENANT"]), User: opt(x["USER_NAME"]), Assignment: opt(x["ASSIGNMENT_ID"]),
			OldState: opt(x["OLD_STATE"]), NewState: opt(x["NEW_STATE"])})
	}
	return out, nil
}

// HasRoles reports a kernel version with roles.
func (r *Repo) HasRoles() bool { return r.has(TRoleAssignments, "") }

// AuthGeneration returns the counter of changes that affect effective
// authorizations.
func (r *Repo) AuthGeneration(ctx context.Context, q dialect.Querier) (int64, error) {
	rows, err := r.selectRows(ctx, q, TAuthGeneration, []string{"GENERATION"})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	n, _ := rows[0]["GENERATION"].(int64)
	return n, nil
}

// NextAuthGeneration increases the counter of changes that affect
// effective authorizations.
func (r *Repo) NextAuthGeneration(ctx context.Context, q dialect.Querier, at time.Time) error {
	if !r.HasRoles() {
		return nil
	}
	g, err := r.AuthGeneration(ctx, q)
	if err != nil {
		return err
	}
	if g == 0 {
		return r.insert(ctx, q, TAuthGeneration, row{"ID": int64(1), "GENERATION": int64(1), "CHANGED_AT": at})
	}
	// Increased in SQL, so that concurrent transactions never write the
	// same value: PostgreSQL makes the second wait for the first.
	gen, stamp := r.d.QuoteIdent("GENERATION"), r.d.QuoteIdent("CHANGED_AT")
	d, err := r.domain(TAuthGeneration, "CHANGED_AT")
	if err != nil {
		return err
	}
	ts, err := r.d.Encode(d, at)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "UPDATE "+r.d.QuoteIdent(TAuthGeneration)+" SET "+gen+" = "+gen+" + 1, "+stamp+" = "+r.d.Placeholder(1)+
		" WHERE "+r.d.QuoteIdent("ID")+" = 1", ts)
	return err
}

// InsertRoleText adds the label of a role in a language.
func (r *Repo) InsertRoleText(ctx context.Context, q dialect.Querier, name, lang string, l model.Label) error {
	return r.insert(ctx, q, TRoleTexts, row{"ROLE_NAME": name, "LANGUAGE": lang, "LABEL": l.Label, "DESCRIPTION": val(l.Description)})
}
