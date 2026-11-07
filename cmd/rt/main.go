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

// Command rt manages packages and tenants of a go-runtime database.
//
//	rt [flags] <command> [arguments]
//
// The database is selected with --db or the RT_DATABASE environment
// variable: sqlite:<path> or postgres://...
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// Exit codes.
const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 2
	exitInvalid     = 3 // validation or dependency problems
	exitNeedsPermit = 4 // destructive change or confirmation required
	exitCheck       = 5 // check found discrepancies
	exitExec        = 6 // execution failed and was rolled back
)

const usage = `usage: rt [flags] <command> [arguments]

Commands:
  init                     create or migrate the kernel tables
  plan <package>           show the install or upgrade plan of a package directory
  install <package>...     install package directories
  upgrade <package>        upgrade an installed package
  uninstall <id>           remove a package and its data (needs --yes)
  list                     list installed packages
  show <object>            show a package, table, domain or data element (kind:name to disambiguate)
  check                    compare the repository with the database schema
  log                      show the operation log
  tenant create <id>       create a tenant (--description)
  tenant list              list tenants
  tenant delete <id>       delete a tenant and all its data (needs --yes)
  language add <code>      set up a language (ISO 639-1, --name)
  language list            list languages
  language remove <code>   remove a language; texts in it need --yes

Flags (anywhere on the command line):
`

type cli struct {
	stdout, stderr io.Writer
	fs             *flag.FlagSet
	db             string
	asJSON         bool
	dryRun         bool
	allowDestr     bool
	yes            bool
	description    string
	name           string
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	c := &cli{stdout: stdout, stderr: stderr, fs: flag.NewFlagSet("rt", flag.ContinueOnError)}
	c.fs.SetOutput(stderr)
	c.fs.StringVar(&c.db, "db", os.Getenv("RT_DATABASE"), "database: sqlite:<path> or postgres://... (default $RT_DATABASE)")
	c.fs.BoolVar(&c.asJSON, "json", false, "print JSON")
	c.fs.BoolVar(&c.dryRun, "dry-run", false, "only print the plan")
	c.fs.BoolVar(&c.allowDestr, "allow-destructive", false, "permit destructive changes")
	c.fs.BoolVar(&c.yes, "yes", false, "confirm deleting data")
	c.fs.StringVar(&c.description, "description", "", "description of a new tenant")
	c.fs.StringVar(&c.name, "name", "", "name of a new language (default: English name of the code)")
	c.fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		c.fs.PrintDefaults()
	}
	pos, err := c.parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if len(pos) == 0 {
		c.fs.Usage()
		return exitUsage
	}
	if c.db == "" {
		fmt.Fprintln(stderr, "error: no database; use --db or set RT_DATABASE")
		return exitUsage
	}
	k, err := kernel.Open(ctx, c.db)
	if err != nil {
		return c.fail(err)
	}
	defer k.Close()
	return c.dispatch(ctx, k, pos[0], pos[1:])
}

// parse accepts flags before, between and after positional arguments.
func (c *cli) parse(args []string) ([]string, error) {
	var pos []string
	for {
		if err := c.fs.Parse(args); err != nil {
			return nil, err
		}
		rest := c.fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func (c *cli) need(args []string, n int, what string) bool {
	if len(args) != n && !(n < 0 && len(args) >= -n) {
		fmt.Fprintf(c.stderr, "usage: rt %s\n", what)
		return false
	}
	return true
}

func (c *cli) opts() kernel.Options {
	return kernel.Options{DryRun: c.dryRun, AllowDestructive: c.allowDestr, Confirm: c.yes}
}

func (c *cli) dispatch(ctx context.Context, k *kernel.Kernel, cmd string, args []string) int {
	switch cmd {
	case "init":
		if !c.need(args, 0, "init") {
			return exitUsage
		}
		p, err := k.Init(ctx, c.opts())
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	case "plan":
		if !c.need(args, 1, "plan <package>") {
			return exitUsage
		}
		pkg, err := kernel.LoadDir(args[0])
		if err != nil {
			return c.fail(err)
		}
		p, err := k.Plan(ctx, pkg)
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	case "install":
		if !c.need(args, -1, "install <package>...") {
			return exitUsage
		}
		var pkgs []*model.Package
		for _, a := range args {
			pkg, err := kernel.LoadDir(a)
			if err != nil {
				return c.fail(err)
			}
			pkgs = append(pkgs, pkg)
		}
		p, err := k.Install(ctx, pkgs, c.opts())
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	case "upgrade":
		if !c.need(args, 1, "upgrade <package>") {
			return exitUsage
		}
		pkg, err := kernel.LoadDir(args[0])
		if err != nil {
			return c.fail(err)
		}
		p, err := k.Upgrade(ctx, pkg, c.opts())
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	case "uninstall":
		if !c.need(args, 1, "uninstall <id>") {
			return exitUsage
		}
		p, err := k.Uninstall(ctx, model.PackageID(args[0]), c.opts())
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	case "list":
		return c.list(ctx, k)
	case "show":
		if !c.need(args, 1, "show <object>") {
			return exitUsage
		}
		return c.show(ctx, k, args[0])
	case "check":
		d, err := k.Check(ctx)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
		} else if len(d) == 0 {
			fmt.Fprintln(c.stdout, "Repository and database schema match.")
		} else {
			for _, x := range d {
				fmt.Fprintln(c.stdout, x)
			}
		}
		if len(d) > 0 {
			return exitCheck
		}
		return exitOK
	case "log":
		return c.log(ctx, k)
	case "tenant":
		return c.tenant(ctx, k, args)
	case "language":
		return c.language(ctx, k, args)
	}
	fmt.Fprintf(c.stderr, "error: unknown command %q\n", cmd)
	c.fs.Usage()
	return exitUsage
}

func (c *cli) printPlan(p *plan.Plan) int {
	if c.asJSON {
		c.json(p)
		return exitOK
	}
	fmt.Fprint(c.stdout, p.Text())
	switch {
	case p.Executed:
		fmt.Fprintf(c.stdout, "\nExecuted (log %s).\n", p.LogID)
	case len(p.Changes) == 0:
		fmt.Fprintln(c.stdout, "\nNothing to do.")
	default:
		fmt.Fprintln(c.stdout, "\nDry run, nothing was changed.")
	}
	return exitOK
}

func (c *cli) list(ctx context.Context, k *kernel.Kernel) int {
	pkgs, err := k.Packages(ctx)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(pkgs)
		return exitOK
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tVERSION\tINSTALLED\tUPDATED\tREQUIRES\tDESCRIPTION")
	for _, p := range pkgs {
		var req []string
		for _, r := range p.Manifest.Requires {
			req = append(req, strings.TrimSpace(string(r.ID)+" "+r.Constraint))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Manifest.ID, p.Manifest.Version, p.InstalledAt.Format("2006-01-02 15:04:05"),
			p.UpdatedAt.Format("2006-01-02 15:04:05"), strings.Join(req, ", "), p.Manifest.Description)
	}
	w.Flush()
	return exitOK
}

func (c *cli) show(ctx context.Context, k *kernel.Kernel, ref string) int {
	kind, name, err := k.Resolve(ctx, ref)
	if err != nil {
		return c.fail(err)
	}
	switch kind {
	case kernel.KindPackage:
		d, err := k.Package(ctx, model.PackageID(name))
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		m := d.Package.Manifest
		fmt.Fprintf(c.stdout, "Package %s %s\n  %s\n", m.ID, m.Version, m.Description)
		for _, r := range m.Requires {
			fmt.Fprintf(c.stdout, "  requires %s %s\n", r.ID, r.Constraint)
		}
		if len(d.RequiredBy) > 0 {
			fmt.Fprintf(c.stdout, "  required by %v\n", d.RequiredBy)
		}
		for _, dom := range d.Domains {
			fmt.Fprintf(c.stdout, "  domain %s %s\n", dom.Name, dom.TypeString())
		}
		for _, t := range d.Tables {
			fmt.Fprintf(c.stdout, "  table %s\n", t)
		}
		for _, e := range d.Extensions {
			fmt.Fprintf(c.stdout, "  extension of %s (%d fields)\n", e.Table, len(e.Fields))
		}
	case kernel.KindTable:
		d, err := k.Table(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		t := d.Table
		fmt.Fprintf(c.stdout, "Table %s (owner %s, tenant-specific %v)\n  %s\n\n", t.Name, t.Owner, t.TenantSpecific, t.Description)
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "  FIELD\tDATA ELEMENT\tDOMAIN\tKEY\tNOT NULL\tDEFAULT\tOWNER\tORIGIN")
		for _, f := range t.Fields {
			key := ""
			if t.IsKey(f.Name) {
				key = "yes"
			}
			def := ""
			if f.Default != nil {
				def = *f.Default
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%v\t%s\t%s\t%s\n", f.Name, f.DataElement, f.Domain, key, f.NotNull, def, f.Owner, f.Origin)
		}
		w.Flush()
		for _, fk := range t.ForeignKeys {
			var cols []string
			for _, p := range fk.Columns {
				cols = append(cols, p.Field+" -> "+p.References)
			}
			fmt.Fprintf(c.stdout, "  foreign key %s to %s (%s)\n", fk.Name, fk.Target, strings.Join(cols, ", "))
		}
		for _, r := range d.ReferencedBy {
			fmt.Fprintf(c.stdout, "  referenced by %s\n", r)
		}
	case kernel.KindDataElement:
		d, err := k.DataElement(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		de := d.DataElement
		typ := ""
		if d.Domain != nil {
			typ = " " + d.Domain.TypeString()
		}
		fmt.Fprintf(c.stdout, "Data element %s (owner %s)\n  domain %s%s\n\n", de.Name, de.Owner, de.Domain, typ)
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "  LANGUAGE\tSHORT\tMEDIUM\tLONG\tHEADING\tDESCRIPTION")
		for _, lang := range de.Languages() {
			t := de.Texts[lang]
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n", lang, t.Short, t.Medium, t.Long, t.Heading, t.Description)
		}
		w.Flush()
		for _, u := range d.UsedBy {
			fmt.Fprintf(c.stdout, "  used by %s\n", u)
		}
	case kernel.KindDomain:
		d, err := k.Domain(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		dom := d.Domain
		fmt.Fprintf(c.stdout, "Domain %s %s (owner %s)\n  %s\n", dom.Name, dom.TypeString(), dom.Owner, dom.Description)
		for _, v := range dom.Values {
			fmt.Fprintf(c.stdout, "  value %s  %s\n", v.Value, v.Description)
		}
		for _, u := range d.UsedBy {
			fmt.Fprintf(c.stdout, "  used by %s\n", u)
		}
	}
	return exitOK
}

func (c *cli) log(ctx context.Context, k *kernel.Kernel) int {
	entries, err := k.Log(ctx)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(entries)
		return exitOK
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "STARTED\tOPERATION\tSTATUS\tOBJECTS\tSTATEMENTS\tID")
	for _, e := range entries {
		var objs []string
		for _, p := range e.Packages {
			objs = append(objs, strings.TrimSpace(p.ID+" "+strings.Trim(p.From+"->"+p.To, "->")))
		}
		if e.Target != "" {
			objs = append(objs, e.Target)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", e.StartedAt.Format("2006-01-02 15:04:05"), e.Operation, e.Status, strings.Join(objs, ", "), len(e.Statements), e.ID)
	}
	w.Flush()
	return exitOK
}

func (c *cli) tenant(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.stderr, "usage: rt tenant create|list|delete")
		return exitUsage
	}
	ten := k.Tenants()
	switch args[0] {
	case "create":
		if !c.need(args[1:], 1, "tenant create <id> [--description text]") {
			return exitUsage
		}
		t, err := ten.Create(ctx, args[1], c.description)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(t)
		} else {
			fmt.Fprintf(c.stdout, "Tenant %s created.\n", t.ID)
		}
		return exitOK
	case "list":
		list, err := ten.List(ctx)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TENANT\tCREATED\tDESCRIPTION")
		for _, t := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.CreatedAt.Format("2006-01-02 15:04:05"), t.Description)
		}
		w.Flush()
		return exitOK
	case "delete":
		if !c.need(args[1:], 1, "tenant delete <id> [--yes]") {
			return exitUsage
		}
		p, err := ten.Delete(ctx, args[1], c.opts())
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	}
	fmt.Fprintf(c.stderr, "error: unknown tenant command %q\n", args[0])
	return exitUsage
}

func (c *cli) language(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.stderr, "usage: rt language add|list|remove")
		return exitUsage
	}
	langs := k.Languages()
	switch args[0] {
	case "add":
		if !c.need(args[1:], 1, "language add <code> [--name text]") {
			return exitUsage
		}
		l, err := langs.Add(ctx, args[1], c.name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(l)
		} else {
			fmt.Fprintf(c.stdout, "Language %s (%s) added.\n", l.Code, l.Name)
		}
		return exitOK
	case "list":
		list, err := langs.List(ctx)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "LANGUAGE\tNAME")
		for _, l := range list {
			fmt.Fprintf(w, "%s\t%s\n", l.Code, l.Name)
		}
		w.Flush()
		return exitOK
	case "remove":
		if !c.need(args[1:], 1, "language remove <code> [--yes]") {
			return exitUsage
		}
		p, err := langs.Remove(ctx, args[1], c.opts())
		if err != nil {
			return c.fail(err)
		}
		return c.printPlan(p)
	}
	fmt.Fprintf(c.stderr, "error: unknown language command %q\n", args[0])
	return exitUsage
}

func (c *cli) json(v any) {
	enc := json.NewEncoder(c.stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// fail prints err and returns the exit code for its class.
func (c *cli) fail(err error) int {
	fmt.Fprintln(c.stderr, "error: "+strings.ReplaceAll(err.Error(), "\n", "\n  "))
	var (
		v    *rterr.ValidationError
		dep  *rterr.DependencyError
		nc   *rterr.NameConflict
		dc   *rterr.DestructiveChange
		conf *rterr.ConfirmationRequired
		ee   *rterr.ExecError
		ml   *rterr.MissingLanguages
	)
	switch {
	case errors.As(err, &dc):
		fmt.Fprintln(c.stderr, "hint: rerun with --allow-destructive, or with --dry-run to see the plan")
		return exitNeedsPermit
	case errors.As(err, &conf):
		fmt.Fprintln(c.stderr, "hint: rerun with --yes, or with --dry-run to see the plan")
		return exitNeedsPermit
	case errors.As(err, &ml):
		fmt.Fprintln(c.stderr, "hint: add the languages with rt language add")
		return exitInvalid
	case errors.As(err, &v), errors.As(err, &dep), errors.As(err, &nc):
		return exitInvalid
	case errors.As(err, &ee):
		return exitExec
	case errors.Is(err, rterr.ErrNotInitialized):
		fmt.Fprintln(c.stderr, "hint: run rt init")
	}
	return exitError
}
