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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// cliUser names the command line in the history of roles.
const cliUser = "cli"

func (c *cli) roleOpts() kernel.RoleOptions {
	return kernel.RoleOptions{By: cliUser, Confirm: c.yes, AllAuthorizations: true}
}

// role runs the role commands.
func (c *cli) role(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt role list [--package P] [--incomplete] | show <role> | create|update|import <file.json> | delete <role> [--yes] | "+
			"export <role> [-o file] | history [role] [--user U] | assign <user> <role> --tenant T [--from D] [--to D] | unassign <assignment>")
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	roles := k.Roles()
	switch cmd, rest := args[0], args[1:]; {
	case cmd == "list" && len(rest) == 0:
		list, err := roles.List(ctx)
		if err != nil {
			return c.fail(err)
		}
		var shown []kernel.RoleSummary
		for _, r := range list {
			if (c.pkg == "" || string(r.Package) == c.pkg) && (!c.incomplete || r.Incomplete > 0) {
				shown = append(shown, r)
			}
		}
		if c.asJSON {
			c.json(append([]kernel.RoleSummary{}, shown...))
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ROLE\tPACKAGE\tKIND\tLABEL\tINCOMPLETE")
		for _, r := range shown {
			kind := "single"
			if r.Composite {
				kind = "composite"
			}
			inc := ""
			if r.Incomplete > 0 {
				inc = fmt.Sprintf("%d authorizations", r.Incomplete)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Package, kind, r.Label, inc)
		}
		w.Flush()
		return exitOK
	case cmd == "show" && len(rest) == 1:
		return c.showRole(ctx, k, rest[0])
	case (cmd == "create" || cmd == "update" || cmd == "import") && len(rest) == 1:
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return c.fail(err)
		}
		role, err := kernel.ParseRole(data, rest[0])
		if err != nil {
			return c.fail(err)
		}
		switch cmd {
		case "create":
			err = roles.Create(ctx, role, c.roleOpts())
		case "update":
			err = roles.Update(ctx, role, c.roleOpts())
		default:
			_, err = roles.Import(ctx, data, rest[0], c.roleOpts())
		}
		if err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Role %s saved.\n", role.Name)
		c.activityHint(ctx, roles, role.Name)
		return exitOK
	case cmd == "delete" && len(rest) == 1:
		if err := roles.Delete(ctx, rest[0], c.roleOpts()); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Role %s deleted.\n", rest[0])
		return exitOK
	case cmd == "export" && len(rest) == 1:
		data, err := roles.Export(ctx, rest[0])
		if err != nil {
			return c.fail(err)
		}
		data = append(data, '\n')
		if c.output == "" || c.output == "-" {
			_, err = c.stdout.Write(data)
		} else {
			err = os.WriteFile(c.output, data, 0o644)
		}
		if err != nil {
			return c.fail(err)
		}
		return exitOK
	case cmd == "history" && len(rest) <= 1:
		role := ""
		if len(rest) == 1 {
			role = rest[0]
		}
		list, err := roles.History(ctx, role, c.userFilter)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tBY\tCHANGE\tROLE\tTENANT\tUSER")
		for _, h := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", h.ChangedAt.Format("2006-01-02 15:04:05"), h.ChangedBy, h.Kind, h.Role.Or(""), h.Tenant.Or(""), h.User.Or(""))
		}
		w.Flush()
		return exitOK
	case cmd == "assign" && len(rest) == 2:
		return c.assign(ctx, k, rest[0], rest[1])
	case cmd == "unassign" && len(rest) == 1:
		if err := roles.Unassign(ctx, rest[0], c.roleOpts()); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Assignment %s removed.\n", rest[0])
		return exitOK
	}
	return usage()
}

// activityHint tells that * on ACTIVITY includes activities the object
// allows later.
func (c *cli) activityHint(ctx context.Context, roles *kernel.Roles, name string) {
	d, err := roles.Get(ctx, name)
	if err != nil {
		return
	}
	for _, a := range d.Authorizations {
		if a.AllActivities {
			fmt.Fprintf(c.stdout, "Note: %s grants * on ACTIVITY of %s, which includes activities the object allows later.\n", a.Role, a.Authorization.Object)
		}
	}
}

func (c *cli) showRole(ctx context.Context, k *kernel.Kernel, name string) int {
	d, err := k.Roles().Get(ctx, name)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(d)
		return exitOK
	}
	r := d.Role
	kind := "Role"
	if r.Composite {
		kind = "Composite role"
	}
	fmt.Fprintf(c.stdout, "%s %s (%s)\n", kind, r.Name, r.Owner)
	for _, l := range r.Languages() {
		t := r.Texts[l]
		fmt.Fprintf(c.stdout, "  %s: %s", l, t.Label)
		if desc, ok := t.Description.Get(); ok {
			fmt.Fprintf(c.stdout, " - %s", desc)
		}
		fmt.Fprintln(c.stdout)
	}
	if r.Composite {
		fmt.Fprintf(c.stdout, "  roles: %s\n", strings.Join(r.Roles, ", "))
	}
	if d.All {
		fmt.Fprintf(c.stdout, "  all values of every authorization object; objects and activities installed later are included.\n  It covers now:\n")
	}
	for _, a := range d.Authorizations {
		fmt.Fprintln(c.stdout)
		origin := ""
		if a.Role != r.Name {
			origin = " from " + a.Role
		}
		state := ""
		if len(a.Problems) > 0 {
			state = " [INCOMPLETE, grants nothing]"
		}
		fmt.Fprintf(c.stdout, "  %s%s%s\n", a.Authorization.Object, origin, state)
		for _, f := range a.Authorization.FieldNames() {
			var vals []string
			for _, e := range a.Authorization.Fields[f] {
				switch e.Kind {
				case model.ValueRange:
					vals = append(vals, fmt.Sprintf("%q..%q", e.Low, e.High))
				case model.ValuePattern:
					vals = append(vals, e.Low)
				default:
					vals = append(vals, fmt.Sprintf("%q", e.Low))
				}
			}
			fmt.Fprintf(c.stdout, "    %-12s %s\n", f, strings.Join(vals, " "))
		}
		for _, p := range a.Problems {
			fmt.Fprintf(c.stdout, "    ! %s\n", p)
		}
		if a.AllActivities {
			fmt.Fprintln(c.stdout, "    note: * on ACTIVITY includes activities the object allows later")
		}
	}
	if len(d.UsedBy) > 0 {
		fmt.Fprintf(c.stdout, "\n  used by %s\n", strings.Join(d.UsedBy, ", "))
	}
	fmt.Fprintf(c.stdout, "  %d assignments\n", d.Assignments)
	return exitOK
}

// day reads an optional day of a flag.
func day(s string) (model.Optional[time.Time], error) {
	if s == "" {
		return model.None[time.Time](), nil
	}
	t, err := time.Parse(model.DateLayout, s)
	if err != nil {
		return model.None[time.Time](), fmt.Errorf("%q is no day in the form YYYY-MM-DD", s)
	}
	return model.Some(t), nil
}

func (c *cli) assign(ctx context.Context, k *kernel.Kernel, user, role string) int {
	if c.tenantID == "" {
		fmt.Fprintln(c.stderr, "usage: rt role assign <user> <role> --tenant T [--from D] [--to D]")
		return exitUsage
	}
	from, err := day(c.from)
	if err != nil {
		return c.fail(err)
	}
	to, err := day(c.to)
	if err != nil {
		return c.fail(err)
	}
	a, overlaps, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: c.tenantID, ValidFrom: from, ValidTo: to}, c.roleOpts())
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(map[string]any{"assignment": a, "overlaps": overlaps})
		return exitOK
	}
	fmt.Fprintf(c.stdout, "Role %s assigned to %s in tenant %s (assignment %s).\n", role, user, c.tenantID, a.ID)
	for _, o := range overlaps {
		fmt.Fprintf(c.stdout, "Note: assignment %s of the same role overlaps (%s).\n", o.ID, period(o))
	}
	if role == model.AllAuthorizationsRole {
		fmt.Fprintln(c.stdout, "Note: the user has all authorizations in this tenant; every sign-in is recorded.")
	}
	c.activityHint(ctx, k.Roles(), role)
	return exitOK
}

func period(a kernel.RoleAssignment) string {
	f, t := "open", "open"
	if v, ok := a.ValidFrom.Get(); ok {
		f = v.Format(model.DateLayout)
	}
	if v, ok := a.ValidTo.Get(); ok {
		t = v.Format(model.DateLayout)
	}
	return f + " to " + t
}

func (c *cli) userRoles(ctx context.Context, k *kernel.Kernel, user string) int {
	list, err := k.Roles().Assignments(ctx, user, c.tenantID)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(list)
		return exitOK
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TENANT\tROLE\tVALID\tASSIGNMENT\tCREATED BY")
	for i, a := range list {
		note := ""
		for j, b := range list {
			if i != j && a.Tenant == b.Tenant && a.Role == b.Role && overlaps(a, b) {
				note = " (overlaps)"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\t%s\t%s\n", a.Tenant, a.Role, period(a), note, a.ID, a.CreatedBy)
	}
	w.Flush()
	return exitOK
}

func overlaps(a, b kernel.RoleAssignment) bool {
	before := func(to, from model.Optional[time.Time]) bool {
		t, ok1 := to.Get()
		f, ok2 := from.Get()
		return ok1 && ok2 && t.Before(f)
	}
	return !before(a.ValidTo, b.ValidFrom) && !before(b.ValidTo, a.ValidFrom)
}

// userAuthorizations shows the effective authorizations of a user in a
// tenant on a day with their origin.
func (c *cli) userAuthorizations(ctx context.Context, k *kernel.Kernel, user string) int {
	if c.tenantID == "" {
		fmt.Fprintln(c.stderr, "usage: rt user authorizations <user> --tenant T [--date D]")
		return exitUsage
	}
	date := time.Now()
	if c.date != "" {
		d, err := day(c.date)
		if err != nil {
			return c.fail(err)
		}
		date = d.Or(date)
	}
	e, err := k.EffectiveAuthorizations(ctx, user, c.tenantID, date)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(e)
		return exitOK
	}
	fmt.Fprintf(c.stdout, "Effective authorizations of %s in tenant %s on %s\n", e.User, e.Tenant, e.Date.Format(model.DateLayout))
	if e.Locked {
		fmt.Fprintln(c.stdout, "  none: the user is locked")
		return exitOK
	}
	for _, o := range e.AllOrigins {
		fmt.Fprintf(c.stdout, "  ALL AUTHORIZATIONS from %s (assignment %s)\n", o.Role, o.Assignment)
	}
	for _, name := range slices.Sorted(maps.Keys(e.Objects)) {
		fmt.Fprintf(c.stdout, "\n  %s\n", name)
		for _, g := range e.Objects[name].Grants {
			origin := g.Origin.Role
			if comp, ok := g.Origin.Composite.Get(); ok {
				origin += " in " + comp
			}
			fmt.Fprintf(c.stdout, "    from %s (assignment %s, %s)\n", origin, g.Origin.Assignment,
				period(kernel.RoleAssignment{ValidFrom: g.Origin.ValidFrom, ValidTo: g.Origin.ValidTo}))
			for _, f := range slices.Sorted(maps.Keys(g.Fields)) {
				b, _ := g.Fields[f].MarshalJSON()
				var entries []model.ValueEntry
				_ = json.Unmarshal(b, &entries)
				var vals []string
				for _, en := range entries {
					switch en.Kind {
					case model.ValueRange:
						vals = append(vals, fmt.Sprintf("%q..%q", en.Low, en.High))
					case model.ValuePattern:
						vals = append(vals, en.Low)
					default:
						vals = append(vals, fmt.Sprintf("%q", en.Low))
					}
				}
				fmt.Fprintf(c.stdout, "      %-12s %s\n", f, strings.Join(vals, " "))
			}
		}
	}
	if len(e.NotEffective) > 0 {
		fmt.Fprintln(c.stdout, "\n  not effective:")
		for _, n := range e.NotEffective {
			var probs []string
			for _, p := range n.Problems {
				probs = append(probs, p.String())
			}
			fmt.Fprintf(c.stdout, "    %s from %s: %s\n", n.Object, n.Origin.Role, strings.Join(probs, "; "))
		}
	}
	return exitOK
}

// terminal reports whether standard input is a terminal; tests replace it.
var terminal = func() bool {
	f, ok := input.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// firstAdmin creates a first user with all authorizations after init:
// with --admin and --tenant or RT_ADMIN_USER and RT_ADMIN_TENANT, the
// password from RT_ADMIN_PASSWORD or standard input, or after asking on
// a terminal when there is no user yet. Flags win over the environment.
// A missing tenant is created.
func (c *cli) firstAdmin(ctx context.Context, k *kernel.Kernel) int {
	or := func(flag, env string) string {
		if flag != "" {
			return flag
		}
		return os.Getenv(env)
	}
	name, tenant := or(c.admin, "RT_ADMIN_USER"), or(c.tenantID, "RT_ADMIN_TENANT")
	var password string
	switch {
	case name != "":
		if tenant == "" {
			fmt.Fprintln(c.stderr, "usage: rt init --admin <user> --tenant <tenant> (or RT_ADMIN_USER and RT_ADMIN_TENANT), the password in RT_ADMIN_PASSWORD or on standard input")
			return exitUsage
		}
		password = os.Getenv("RT_ADMIN_PASSWORD")
		if password == "" {
			pw, err := c.readSecret()
			if err != nil {
				return c.fail(err)
			}
			password = pw
		}
	case terminal():
		users, err := k.Users().List(ctx)
		if err != nil || len(users) > 0 {
			return exitOK
		}
		ask := func(q string) string {
			fmt.Fprint(c.stdout, q)
			line, _ := readLine()
			return strings.TrimSpace(line)
		}
		if a := strings.ToLower(ask("Create a first user with all authorizations? [y/N] ")); a != "y" && a != "yes" {
			return exitOK
		}
		name = ask("User name: ")
		password = ask("Password (shown as typed): ")
		tenant = ask("Tenant (created if missing): ")
	default:
		return exitOK
	}
	tenants, err := k.Tenants().List(ctx)
	if err != nil {
		return c.fail(err)
	}
	if !slices.ContainsFunc(tenants, func(t kernel.Tenant) bool { return t.ID == tenant }) {
		if _, err := k.Tenants().Create(ctx, tenant, model.None[string]()); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Tenant %s created.\n", tenant)
	}
	if _, err := k.Users().Create(ctx, name, password, kernel.UserOptions{Tenant: model.Some(tenant)}); err != nil {
		return c.fail(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: name, Role: model.AllAuthorizationsRole, Tenant: tenant}, c.roleOpts()); err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "User %s created with all authorizations in tenant %s; every sign-in is recorded.\n", name, tenant)
	return exitOK
}

// allHolders returns the assignments of the role with all authorizations
// and a warning when no active user has it today.
func (c *cli) allHolders(ctx context.Context, k *kernel.Kernel) ([]kernel.AllAuthorizationsHolder, []string, error) {
	holders, err := k.AllAuthorizationsHolders(ctx)
	if err != nil {
		return nil, nil, err
	}
	warnings := []string{}
	if !slices.ContainsFunc(holders, func(h kernel.AllAuthorizationsHolder) bool { return h.Effective }) {
		warnings = append(warnings, "no active user has "+model.AllAuthorizationsRole+" in any tenant; nobody can administer everything")
	}
	return holders, warnings, nil
}

func (c *cli) printHolders(holders []kernel.AllAuthorizationsHolder, warnings []string) {
	if len(holders) > 0 {
		fmt.Fprintf(c.stdout, "Users with %s:\n", model.AllAuthorizationsRole)
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		for _, h := range holders {
			state := "effective"
			if !h.Effective {
				state = "not effective"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", h.User, h.Tenant, h.Status, period(kernel.RoleAssignment{ValidFrom: h.ValidFrom, ValidTo: h.ValidTo}), state)
		}
		w.Flush()
	}
	for _, x := range warnings {
		fmt.Fprintln(c.stdout, "warning: "+x)
	}
}
