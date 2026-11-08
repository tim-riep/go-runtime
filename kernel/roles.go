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
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// Kinds of entries in the history of roles and role assignments.
const (
	ChangeCreateRole        = "create-role"
	ChangeUpdateRole        = "update-role"
	ChangeDeleteRole        = "delete-role"
	ChangeAssign            = "assign"
	ChangeUnassign          = "unassign"
	ChangeAssignmentRemoved = "assignment-removed"
	ChangeRoleChanged       = "role-changed"
	ChangeAllSignIn         = "all-authorizations-sign-in"
	ChangeAllRestored       = "all-authorizations-restored"
)

// RoleChange is an entry of the history of roles and role assignments.
type RoleChange = repo.RoleChange

// RoleAssignment is a role assigned to a user in a tenant.
type RoleAssignment = repo.RoleAssignment

// operationBy is the user named in the history for operations on
// packages.
const operationBy = "kernel"

// checkRoles checks the roles of the target schema of an operation on
// packages. The roles of the packages the operation installs or upgrades
// must fit their authorization objects; authorizations of other roles
// that stop fitting are reported as notices, since they grant nothing
// until they are maintained. Local roles that the operation changes and
// roles that are assigned but gone are listed; only an uninstall may
// remove assigned roles.
func (k *Kernel) checkRoles(ctx context.Context, q dialect.Querier, op plan.Operation, cur, want *model.Schema) ([]plan.Change, error) {
	strict := map[model.PackageID]bool{}
	for _, id := range changedPackages(cur, want) {
		strict[id] = true
	}
	var errs rterr.List
	var notes []plan.Change
	for _, name := range want.RoleNames() {
		r := want.Roles[name]
		for i, a := range r.Authorizations {
			problems := model.CheckAuthorization(want, a)
			if len(problems) == 0 {
				continue
			}
			if strict[r.Owner] {
				for _, p := range problems {
					errs = append(errs, &rterr.ValidationError{Code: rterr.CodeRole, Location: r.Src.At(fmt.Sprintf("/authorizations/%d", i)),
						Object: "role " + name + ", authorization on " + a.Object, Message: p.String()})
				}
				continue
			}
			if c := cur.Roles[name]; c != nil && i < len(c.Authorizations) && c.Authorizations[i].Object == a.Object &&
				slices.Equal(problems, model.CheckAuthorization(cur, c.Authorizations[i])) {
				continue // known before
			}
			var texts []string
			for _, p := range problems {
				texts = append(texts, p.String())
			}
			notes = append(notes, plan.Change{Kind: plan.IncompleteAuthorization, Name: name, Notice: true,
				Detail: fmt.Sprintf("authorization %d on %s grants nothing: %s", i+1, a.Object, strings.Join(texts, "; "))})
		}
	}
	if len(errs) > 0 {
		return nil, errs.Err()
	}
	for _, name := range cur.RoleNames() {
		c := cur.Roles[name]
		if c.Owner != model.Local {
			continue
		}
		switch w := want.Roles[name]; {
		case w == nil:
			notes = append(notes, plan.Change{Kind: plan.DropRole, Name: name, Detail: "no member left"})
		case !sameRole(c, w):
			notes = append(notes, plan.Change{Kind: plan.AlterRole, Name: name, Detail: "references to removed objects and roles removed"})
		}
	}
	removed, err := k.removedAssignments(ctx, q, want)
	if err != nil {
		return nil, err
	}
	if len(removed) > 0 {
		if op != plan.Uninstall {
			counts := map[string]int{}
			for _, a := range removed {
				counts[a.Role]++
			}
			var msgs []string
			for _, role := range slices.Sorted(maps.Keys(counts)) {
				msgs = append(msgs, fmt.Sprintf("%s has %d assignments", role, counts[role]))
			}
			return nil, &rterr.InvalidArgument{Argument: "roles", Message: "the operation removes roles that are assigned to users: " + strings.Join(msgs, ", ")}
		}
		notes = append(notes, plan.Change{Kind: plan.DeleteRows, Table: repo.TRoleAssignments, Destructive: true,
			Detail: fmt.Sprintf("%d role assignments", len(removed))})
	}
	return notes, nil
}

func sameRole(a, b *model.Role) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// removedAssignments returns the assignments of roles that want no longer
// has.
func (k *Kernel) removedAssignments(ctx context.Context, q dialect.Querier, want *model.Schema) ([]RoleAssignment, error) {
	if !k.repo.HasRoles() {
		return nil, nil
	}
	all, err := k.repo.RoleAssignments(ctx, q, repo.AssignmentFilter{})
	if err != nil {
		return nil, err
	}
	var out []RoleAssignment
	for _, a := range all {
		if a.Role != model.AllAuthorizationsRole && want.Roles[a.Role] == nil {
			out = append(out, a)
		}
	}
	return out, nil
}

// afterRoles records what an operation on packages did to local roles and
// role assignments, and marks the effective authorizations as changed.
func (k *Kernel) afterRoles(ctx context.Context, q dialect.Querier, cur, want *model.Schema, at time.Time) error {
	if !k.repo.HasRoles() {
		return nil
	}
	removed, err := k.removedAssignments(ctx, q, want)
	if err != nil {
		return err
	}
	for _, a := range removed {
		if err := k.removeAssignment(ctx, q, a, ChangeAssignmentRemoved, operationBy, at); err != nil {
			return err
		}
	}
	for _, name := range cur.RoleNames() {
		c := cur.Roles[name]
		if c.Owner != model.Local {
			continue
		}
		w := want.Roles[name]
		switch {
		case w == nil:
			err = k.repo.InsertRoleChange(ctx, q, RoleChange{ChangedAt: at, ChangedBy: operationBy, Kind: ChangeDeleteRole, Role: model.Some(name),
				OldState: roleState(cur, c)})
		case !sameRole(c, w):
			err = k.repo.InsertRoleChange(ctx, q, RoleChange{ChangedAt: at, ChangedBy: operationBy, Kind: ChangeRoleChanged, Role: model.Some(name),
				OldState: roleState(cur, c), NewState: roleState(want, w)})
		}
		if err != nil {
			return err
		}
	}
	return k.repo.NextAuthGeneration(ctx, q, at)
}

// removeAssignment deletes an assignment and records it; for the role with
// all authorizations also in the audit log.
func (k *Kernel) removeAssignment(ctx context.Context, q dialect.Querier, a RoleAssignment, kind, by string, at time.Time) error {
	if err := k.repo.DeleteRoleAssignment(ctx, q, a); err != nil {
		return err
	}
	if a.Role == model.AllAuthorizationsRole {
		audit(ctx, "all authorizations unassigned", "user", a.User, "tenant", a.Tenant, "by", by, "assignment", a.ID, "change", kind)
	}
	return k.repo.InsertRoleChange(ctx, q, RoleChange{ChangedAt: at, ChangedBy: by, Kind: kind, Role: model.Some(a.Role), Tenant: model.Some(a.Tenant),
		User: model.Some(a.User), Assignment: model.Some(a.ID), OldState: assignmentState(a)})
}

func assignmentState(a RoleAssignment) model.Optional[string] {
	b, _ := json.Marshal(a)
	return model.Some(string(b))
}

func roleState(s *model.Schema, r *model.Role) model.Optional[string] {
	b, err := RoleJSON(s, r)
	if err != nil {
		return model.None[string]()
	}
	return model.Some(string(b))
}

// RoleJSON writes a role in the format of role files: values of integer
// fields as numbers, of BOOL fields as booleans, others as strings.
func RoleJSON(s *model.Schema, r *model.Role) ([]byte, error) {
	type doc struct {
		Name           string                 `json:"name"`
		Texts          map[string]model.Label `json:"texts"`
		Authorizations []map[string]any       `json:"authorizations,omitempty"`
		Roles          []string               `json:"roles,omitempty"`
	}
	d := doc{Name: r.Name, Texts: r.Texts, Roles: r.Roles}
	if !r.Composite {
		d.Authorizations = []map[string]any{}
	}
	for _, a := range r.Authorizations {
		fields := map[string][]map[string]any{}
		var o *model.AuthorizationObject
		if s != nil {
			o = s.AuthorizationObjects[a.Object]
		}
		for _, name := range a.FieldNames() {
			var typ model.BaseType
			if o != nil {
				if f, ok := o.Field(name); ok {
					if dom := s.Domains[s.AuthorizationFieldDomain(f)]; dom != nil {
						typ = dom.Type
					}
				}
			}
			typed := func(v string) any {
				switch typ {
				case model.Int4, model.Int8:
					if n, err := strconv.ParseInt(v, 10, 64); err == nil {
						return n
					}
				case model.Bool:
					if b, err := strconv.ParseBool(v); err == nil {
						return b
					}
				}
				return v
			}
			for _, e := range a.Fields[name] {
				switch e.Kind {
				case model.ValueRange:
					fields[name] = append(fields[name], map[string]any{"from": typed(e.Low), "to": typed(e.High)})
				case model.ValuePattern:
					fields[name] = append(fields[name], map[string]any{"pattern": e.Low})
				default:
					fields[name] = append(fields[name], map[string]any{"value": typed(e.Low)})
				}
			}
		}
		d.Authorizations = append(d.Authorizations, map[string]any{"object": a.Object, "fields": fields})
	}
	return json.MarshalIndent(d, "", "  ")
}

// Roles manages roles: package roles are read only, local roles are
// maintained at runtime.
type Roles struct {
	k     *Kernel
	actor *Actor // nil: unchecked
}

// authorize checks an activity of the actor on a role.
func (r *Roles) authorize(ctx context.Context, activity, role, function string) error {
	return r.k.authorizeActor(ctx, r.actor, RoleAdminObject, activity, map[string]string{"ROLE_NAME": role}, function)
}

// authorizeAssignment checks an activity of the actor on the assignments
// of a role in a tenant.
func (r *Roles) authorizeAssignment(ctx context.Context, activity, role, tenant, function string) error {
	return r.k.authorizeActor(ctx, r.actor, RoleAssignmentObject, activity, map[string]string{"ROLE_NAME": role, "TENANT": tenant}, function)
}

// Roles returns the role manager.
func (k *Kernel) Roles() *Roles { return &Roles{k: k} }

// RoleOptions control changes of roles and assignments.
type RoleOptions struct {
	// By names who makes the change in the history: a user, or "cli".
	// Empty means "api".
	By string
	// Confirm permits removing what uses a deleted role, and removing or
	// locking the last user with all authorizations.
	Confirm bool
	// AllAuthorizations permits assigning the role with all
	// authorizations; the command line sets it, packages never can.
	AllAuthorizations bool
}

func (o RoleOptions) by() string {
	if o.By == "" {
		return "api"
	}
	return o.By
}

// RoleSummary is a role in a list.
type RoleSummary struct {
	Name       string          `json:"name"`
	Package    model.PackageID `json:"package"` // LOCAL for local roles
	Composite  bool            `json:"composite"`
	Label      string          `json:"label"` // English
	Incomplete int             `json:"incomplete"`
}

// List returns the roles ordered by name.
func (r *Roles) List(ctx context.Context) ([]RoleSummary, error) {
	s, err := r.k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	e, err := r.k.actorAuthorizations(ctx, r.actor)
	if err != nil {
		return nil, err
	}
	out := []RoleSummary{}
	for _, name := range s.RoleNames() {
		if !actorMay(e, r.actor, RoleAdminObject, "READ", map[string]string{"ROLE_NAME": name}) {
			continue
		}
		role := s.Roles[name]
		sum := RoleSummary{Name: name, Package: role.Owner, Composite: role.Composite, Label: role.Texts[model.DefaultLanguage].Label}
		for _, a := range role.Authorizations {
			if len(model.CheckAuthorization(s, a)) > 0 {
				sum.Incomplete++
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// RoleDetail is a role with its authorizations resolved.
type RoleDetail struct {
	Role *model.Role `json:"role"`
	// Authorizations are those of the role, or of its members for a
	// composite role, with their origin and problems.
	Authorizations []RoleAuthorization `json:"authorizations"`
	// UsedBy are the composite roles with the role.
	UsedBy []string `json:"usedBy"`
	// Assignments is the number of assignments of the role.
	Assignments int `json:"assignments"`
	// All marks the role with all authorizations: Authorizations are the
	// objects it covers now; objects and activities installed later are
	// covered as well.
	All bool `json:"all"`
}

// RoleAuthorization is an authorization of a role with its origin.
type RoleAuthorization struct {
	Role          string                       `json:"role"` // the single role that has it
	Authorization model.Authorization          `json:"authorization"`
	Problems      []model.AuthorizationProblem `json:"problems"` // empty if effective
	// AllActivities tells that it grants * on ACTIVITY, which includes
	// activities the object allows later.
	AllActivities bool `json:"allActivities"`
}

// Get returns a role with its resolved authorizations.
func (r *Roles) Get(ctx context.Context, name string) (*RoleDetail, error) {
	if err := r.authorize(ctx, "READ", name, "Roles.Get"); err != nil {
		return nil, err
	}
	s, err := r.k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	role := s.Roles[name]
	if role == nil {
		return nil, &rterr.NotFound{Kind: "role", Name: name}
	}
	d := &RoleDetail{Role: role, Authorizations: []RoleAuthorization{}, UsedBy: []string{}, All: name == model.AllAuthorizationsRole}
	if d.All {
		// The role grants every object; it stores nothing, so what it
		// covers now is derived from the installed objects.
		for _, obj := range s.AuthorizationObjectNames() {
			o := s.AuthorizationObjects[obj]
			a := model.Authorization{Object: obj, Fields: map[string][]model.ValueEntry{}}
			for _, f := range o.Fields {
				a.Fields[f.Name] = []model.ValueEntry{{Kind: model.ValuePattern, Low: model.AllValues}}
			}
			d.Authorizations = append(d.Authorizations, RoleAuthorization{Role: name, Authorization: a, Problems: []model.AuthorizationProblem{},
				AllActivities: o.HasActivity()})
		}
	}
	members := []*model.Role{role}
	if role.Composite {
		members = nil
		for _, m := range role.Roles {
			if mr := s.Roles[m]; mr != nil {
				members = append(members, mr)
			}
		}
	}
	for _, m := range members {
		for _, a := range m.Authorizations {
			ra := RoleAuthorization{Role: m.Name, Authorization: a, Problems: model.CheckAuthorization(s, a)}
			if ra.Problems == nil {
				ra.Problems = []model.AuthorizationProblem{}
			}
			for _, e := range a.Fields[model.ActivityField] {
				ra.AllActivities = ra.AllActivities || e.Kind == model.ValuePattern && e.Low == model.AllValues
			}
			d.Authorizations = append(d.Authorizations, ra)
		}
	}
	for _, other := range s.RoleNames() {
		if slices.Contains(s.Roles[other].Roles, name) {
			d.UsedBy = append(d.UsedBy, other)
		}
	}
	as, err := r.k.repo.RoleAssignments(ctx, r.k.db, repo.AssignmentFilter{Role: name})
	if err != nil {
		return nil, err
	}
	d.Assignments = len(as)
	return d, nil
}

// Export writes a role in the format of role files.
func (r *Roles) Export(ctx context.Context, name string) ([]byte, error) {
	if err := r.authorize(ctx, "EXPORT", name, "Roles.Export"); err != nil {
		return nil, err
	}
	s, err := r.k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	role := s.Roles[name]
	if role == nil {
		return nil, &rterr.NotFound{Kind: "role", Name: name}
	}
	return RoleJSON(s, role)
}

// ParseRole reads a local role in the format of role files.
func ParseRole(data []byte, file string) (*model.Role, error) { return pkgload.ParseRole(data, file) }

// Create creates a local role.
func (r *Roles) Create(ctx context.Context, role *model.Role, opts RoleOptions) error {
	if err := r.authorize(ctx, "CREATE", role.Name, "Roles.Create"); err != nil {
		return err
	}
	return r.save(ctx, role, opts, 1)
}

// Update replaces a local role.
func (r *Roles) Update(ctx context.Context, role *model.Role, opts RoleOptions) error {
	if err := r.authorize(ctx, "UPDATE", role.Name, "Roles.Update"); err != nil {
		return err
	}
	return r.save(ctx, role, opts, -1)
}

// Import creates or replaces a local role from a role file.
func (r *Roles) Import(ctx context.Context, data []byte, file string, opts RoleOptions) (*model.Role, error) {
	role, err := ParseRole(data, file)
	if err != nil {
		return nil, err
	}
	if err := r.authorize(ctx, "IMPORT", role.Name, "Roles.Import"); err != nil {
		return nil, err
	}
	return role, r.save(ctx, role, opts, 0)
}

// save writes a local role: mode 1 creates, -1 replaces, 0 does either.
// The role must be valid and fit its authorization objects.
func (r *Roles) save(ctx context.Context, role *model.Role, opts RoleOptions, mode int) error {
	k := r.k
	role = model.CloneRole(role)
	role.Owner, role.File = model.Local, ""
	if !strings.HasPrefix(role.Name, model.LocalNamespace) {
		return &rterr.InvalidArgument{Argument: "role", Message: "local roles are named " + model.LocalNamespace + "<NAME>, not " + role.Name}
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		if err := k.requireInit(ctx, tx); err != nil {
			return err
		}
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		old := s.Roles[role.Name]
		switch {
		case mode > 0 && old != nil:
			return &rterr.InvalidArgument{Argument: "role", Message: "role " + role.Name + " exists already"}
		case mode < 0 && old == nil:
			return &rterr.NotFound{Kind: "role", Name: role.Name}
		}
		before := s.Clone()
		s.Roles[role.Name] = role
		if err := k.validate(ctx, tx, s); err != nil {
			return err
		}
		var errs rterr.List
		for i, a := range role.Authorizations {
			for _, p := range model.CheckAuthorization(s, a) {
				errs = append(errs, &rterr.ValidationError{Code: rterr.CodeRole, Location: role.Src.At(fmt.Sprintf("/authorizations/%d", i)),
					Object: "role " + role.Name + ", authorization on " + a.Object, Message: p.String()})
			}
		}
		if len(errs) > 0 {
			return errs.Err()
		}
		if err := k.repo.ReplaceRole(ctx, tx, role); err != nil {
			return err
		}
		at := k.timestamp()
		c := RoleChange{ChangedAt: at, ChangedBy: opts.byActor(r.actor), Kind: ChangeCreateRole, Role: model.Some(role.Name), NewState: roleState(s, role)}
		if old != nil {
			c.Kind, c.OldState = ChangeUpdateRole, roleState(before, old)
		}
		if err := k.repo.InsertRoleChange(ctx, tx, c); err != nil {
			return err
		}
		return k.repo.NextAuthGeneration(ctx, tx, at)
	})
}

// Delete deletes a local role. A role in composite roles or assignments
// needs Confirm, which removes it from the composite roles and removes
// its assignments; a composite role without members left is deleted too.
func (r *Roles) Delete(ctx context.Context, name string, opts RoleOptions) error {
	if err := r.authorize(ctx, "DELETE", name, "Roles.Delete"); err != nil {
		return err
	}
	opts.By = opts.byActor(r.actor)
	k := r.k
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		if err := k.requireInit(ctx, tx); err != nil {
			return err
		}
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		role := s.Roles[name]
		switch {
		case role == nil:
			return &rterr.NotFound{Kind: "role", Name: name}
		case role.Owner == model.Kernel:
			return &rterr.InvalidArgument{Argument: "role", Message: name + " is a role of the kernel; it cannot change"}
		case role.Owner != model.Local:
			return &rterr.InvalidArgument{Argument: "role", Message: name + " belongs to package " + string(role.Owner) + "; package roles change with their package"}
		}
		assignments, err := k.repo.RoleAssignments(ctx, tx, repo.AssignmentFilter{Role: name})
		if err != nil {
			return err
		}
		var composites []*model.Role
		for _, other := range s.RoleNames() {
			if o := s.Roles[other]; slices.Contains(o.Roles, name) {
				composites = append(composites, o)
			}
		}
		if (len(assignments) > 0 || len(composites) > 0) && !opts.Confirm {
			return &rterr.ConfirmationRequired{Operation: "deletion", Object: fmt.Sprintf("role %s with %d assignments in %d composite roles", name, len(assignments), len(composites))}
		}
		at := k.timestamp()
		for _, a := range assignments {
			if err := k.removeAssignment(ctx, tx, a, ChangeAssignmentRemoved, opts.by(), at); err != nil {
				return err
			}
		}
		for _, c := range composites {
			old := roleState(s, c)
			c.Roles = slices.DeleteFunc(c.Roles, func(m string) bool { return m == name })
			if len(c.Roles) == 0 {
				if err := r.deleteRole(ctx, tx, s, c, opts, at); err != nil {
					return err
				}
				continue
			}
			if err := k.repo.ReplaceRole(ctx, tx, c); err != nil {
				return err
			}
			if err := k.repo.InsertRoleChange(ctx, tx, RoleChange{ChangedAt: at, ChangedBy: opts.by(), Kind: ChangeUpdateRole, Role: model.Some(c.Name),
				OldState: old, NewState: roleState(s, c)}); err != nil {
				return err
			}
		}
		if err := r.deleteRole(ctx, tx, s, role, opts, at); err != nil {
			return err
		}
		return k.repo.NextAuthGeneration(ctx, tx, at)
	})
}

func (r *Roles) deleteRole(ctx context.Context, q dialect.Querier, s *model.Schema, role *model.Role, opts RoleOptions, at time.Time) error {
	k := r.k
	as, err := k.repo.RoleAssignments(ctx, q, repo.AssignmentFilter{Role: role.Name})
	if err != nil {
		return err
	}
	for _, a := range as {
		if err := k.removeAssignment(ctx, q, a, ChangeAssignmentRemoved, opts.by(), at); err != nil {
			return err
		}
	}
	if err := k.repo.DeleteRole(ctx, q, role.Name); err != nil {
		return err
	}
	return k.repo.InsertRoleChange(ctx, q, RoleChange{ChangedAt: at, ChangedBy: opts.by(), Kind: ChangeDeleteRole, Role: model.Some(role.Name),
		OldState: roleState(s, role)})
}

// History returns the changes of a role, of a user, or all for "", by time.
func (r *Roles) History(ctx context.Context, role, user string) ([]RoleChange, error) {
	if err := r.k.requireInit(ctx, r.k.db); err != nil {
		return nil, err
	}
	return r.k.repo.RoleChanges(ctx, r.k.db, role, user)
}

// Assignment describes a role assignment to create.
type Assignment struct {
	User, Role, Tenant string
	// ValidFrom and ValidTo are the first and last day of validity, both
	// included; None is open.
	ValidFrom, ValidTo model.Optional[time.Time]
}

// Assign assigns a role or composite role to a user in a tenant, which
// the user must work in. It returns the assignment and the other
// assignments of the same role to the user in the tenant whose validity
// overlaps; overlaps do no harm.
func (r *Roles) Assign(ctx context.Context, a Assignment, opts RoleOptions) (RoleAssignment, []RoleAssignment, error) {
	k := r.k
	var out RoleAssignment
	var overlaps []RoleAssignment
	if a.Role == model.AllAuthorizationsRole && !opts.AllAuthorizations {
		return out, nil, &rterr.InvalidArgument{Argument: "role", Message: a.Role + " is assigned only on the command line, with rt role assign"}
	}
	if err := r.authorizeAssignment(ctx, "CREATE", a.Role, a.Tenant, "Roles.Assign"); err != nil {
		return out, nil, err
	}
	opts.By = opts.byActor(r.actor)
	from, to := dayOf(a.ValidFrom), dayOf(a.ValidTo)
	if f, ok := from.Get(); ok {
		if t, ok := to.Get(); ok && t.Before(f) {
			return out, nil, &rterr.InvalidArgument{Argument: "validity", Message: "the last day is before the first day"}
		}
	}
	err := k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		if err := k.requireInit(ctx, tx); err != nil {
			return err
		}
		user, err := k.user(ctx, tx, a.User)
		if err != nil {
			return err
		}
		if !slices.Contains(user.Tenants, a.Tenant) {
			return &rterr.InvalidArgument{Argument: "tenant", Message: fmt.Sprintf("user %s does not work in tenant %s; add the tenant to the user first", a.User, a.Tenant)}
		}
		if a.Role != model.AllAuthorizationsRole {
			s, err := k.repo.Load(ctx, tx)
			if err != nil {
				return err
			}
			if s.Roles[a.Role] == nil {
				return &rterr.NotFound{Kind: "role", Name: a.Role}
			}
		}
		existing, err := k.repo.RoleAssignments(ctx, tx, repo.AssignmentFilter{Tenant: a.Tenant, User: a.User, Role: a.Role})
		if err != nil {
			return err
		}
		for _, e := range existing {
			if overlap(e.ValidFrom, e.ValidTo, from, to) {
				overlaps = append(overlaps, e)
			}
		}
		at := k.timestamp()
		out = RoleAssignment{ID: repo.NewID(), Tenant: a.Tenant, User: a.User, Role: a.Role, ValidFrom: from, ValidTo: to, CreatedAt: at, CreatedBy: opts.by()}
		if err := k.repo.InsertRoleAssignment(ctx, tx, out); err != nil {
			return err
		}
		if err := k.repo.InsertRoleChange(ctx, tx, RoleChange{ChangedAt: at, ChangedBy: opts.by(), Kind: ChangeAssign, Role: model.Some(a.Role),
			Tenant: model.Some(a.Tenant), User: model.Some(a.User), Assignment: model.Some(out.ID), NewState: assignmentState(out)}); err != nil {
			return err
		}
		return k.repo.NextAuthGeneration(ctx, tx, at)
	})
	if err == nil && a.Role == model.AllAuthorizationsRole {
		audit(ctx, "all authorizations assigned", "user", a.User, "tenant", a.Tenant, "by", opts.by(), "assignment", out.ID)
	}
	return out, overlaps, err
}

// Unassign removes a role assignment.
func (r *Roles) Unassign(ctx context.Context, id string, opts RoleOptions) error {
	k := r.k
	if r.actor != nil {
		if err := k.requireInit(ctx, k.db); err != nil {
			return err
		}
		list, err := k.repo.RoleAssignments(ctx, k.db, repo.AssignmentFilter{ID: id})
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return &rterr.NotFound{Kind: "role assignment", Name: id}
		}
		if err := r.authorizeAssignment(ctx, "DELETE", list[0].Role, list[0].Tenant, "Roles.Unassign"); err != nil {
			return err
		}
		opts.By = opts.byActor(r.actor)
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		if err := k.requireInit(ctx, tx); err != nil {
			return err
		}
		list, err := k.repo.RoleAssignments(ctx, tx, repo.AssignmentFilter{ID: id})
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return &rterr.NotFound{Kind: "role assignment", Name: id}
		}
		if err := k.keepAllHolder(ctx, tx, opts, "removal", func(h AllAuthorizationsHolder) bool { return h.Assignment == id }); err != nil {
			return err
		}
		at := k.timestamp()
		if err := k.removeAssignment(ctx, tx, list[0], ChangeUnassign, opts.by(), at); err != nil {
			return err
		}
		return k.repo.NextAuthGeneration(ctx, tx, at)
	})
}

// Assignments returns the role assignments of a user, in one tenant or
// in all for "".
func (r *Roles) Assignments(ctx context.Context, user, tenant string) ([]RoleAssignment, error) {
	if err := r.k.requireInit(ctx, r.k.db); err != nil {
		return nil, err
	}
	list, err := r.k.repo.RoleAssignments(ctx, r.k.db, repo.AssignmentFilter{User: user, Tenant: tenant})
	if err != nil || r.actor == nil {
		return list, err
	}
	e, err := r.k.actorAuthorizations(ctx, r.actor)
	if err != nil {
		return nil, err
	}
	out := []RoleAssignment{}
	for _, a := range list {
		if actorMay(e, r.actor, RoleAssignmentObject, "READ", map[string]string{"ROLE_NAME": a.Role, "TENANT": a.Tenant}) {
			out = append(out, a)
		}
	}
	return out, nil
}

// removeTenantAssignments removes the assignments of a user, or of all
// users for "", in a tenant, and records them.
func (k *Kernel) removeTenantAssignments(ctx context.Context, q dialect.Querier, tenant, user, by string) error {
	if !k.repo.HasRoles() {
		return nil
	}
	list, err := k.repo.RoleAssignments(ctx, q, repo.AssignmentFilter{Tenant: tenant, User: user})
	if err != nil {
		return err
	}
	at := k.timestamp()
	for _, a := range list {
		if err := k.removeAssignment(ctx, q, a, ChangeAssignmentRemoved, by, at); err != nil {
			return err
		}
	}
	return k.repo.NextAuthGeneration(ctx, q, at)
}

// dayOf cuts a time to its day in UTC.
func dayOf(t model.Optional[time.Time]) model.Optional[time.Time] {
	if v, ok := t.Get(); ok {
		return model.Some(time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC))
	}
	return t
}

// overlap reports whether two periods of days, None being open, share a
// day.
func overlap(af, at, bf, bt model.Optional[time.Time]) bool {
	before := func(to, from model.Optional[time.Time]) bool {
		t, ok1 := to.Get()
		f, ok2 := from.Get()
		return ok1 && ok2 && t.Before(f)
	}
	return !before(at, bf) && !before(bt, af)
}

// validOn reports whether an assignment is valid on a day.
func validOn(a RoleAssignment, day time.Time) bool {
	if f, ok := a.ValidFrom.Get(); ok && day.Before(f) {
		return false
	}
	if t, ok := a.ValidTo.Get(); ok && day.After(t) {
		return false
	}
	return true
}
