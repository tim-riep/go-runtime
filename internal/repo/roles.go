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

// roleOwner reads the owner column of roles: NULL for local roles.
func roleOwner(v any) model.PackageID {
	if s, ok := v.(string); ok {
		return model.PackageID(s)
	}
	return model.Local
}

func roleOwnerValue(id model.PackageID) any {
	if id == model.Local {
		return nil
	}
	return string(id)
}

func (r *Repo) loadRoles(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	roles, err := r.selectRows(ctx, q, TRoles, []string{"ROLE_NAME", "PACKAGE_ID", "COMPOSITE", "FILE_PATH"}, "ROLE_NAME")
	if err != nil {
		return err
	}
	for _, x := range roles {
		name := str(x["ROLE_NAME"])
		composite, _ := x["COMPOSITE"].(bool)
		s.Roles[name] = &model.Role{Name: name, Owner: roleOwner(x["PACKAGE_ID"]), File: opt(x["FILE_PATH"]).Or(""), Composite: composite,
			Texts: map[string]model.Label{}}
	}
	texts, err := r.selectRows(ctx, q, TRoleTexts, []string{"ROLE_NAME", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if role := s.Roles[str(t["ROLE_NAME"])]; role != nil {
			role.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	auths, err := r.selectRows(ctx, q, TRoleAuthorizations, []string{"ROLE_NAME", "POSITION", "OBJECT_NAME"}, "ROLE_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, a := range auths {
		if role := s.Roles[str(a["ROLE_NAME"])]; role != nil {
			role.Authorizations = append(role.Authorizations, model.Authorization{Object: str(a["OBJECT_NAME"]), Fields: map[string][]model.ValueEntry{}})
		}
	}
	values, err := r.selectRows(ctx, q, TRoleAuthValues, []string{"ROLE_NAME", "POSITION", "FIELD_NAME", "VALUE_NO", "VALUE_KIND", "LOW", "HIGH"},
		"ROLE_NAME", "POSITION", "FIELD_NAME", "VALUE_NO")
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
	members, err := r.selectRows(ctx, q, TCompositeRoles, []string{"ROLE_NAME", "POSITION", "MEMBER_ROLE"}, "ROLE_NAME", "POSITION")
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
