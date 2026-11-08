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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/server"
	"github.com/tim-riep/go-runtime/setting"
	"github.com/tim-riep/go-runtime/structure"
)

// Exit codes.
const (
	exitOK            = 0
	exitError         = 1
	exitUsage         = 2
	exitInvalid       = 3 // validation or dependency problems
	exitNeedsPermit   = 4 // destructive change or confirmation required
	exitCheck         = 5 // check found discrepancies
	exitExec          = 6 // execution failed and was rolled back
	exitNotAuthorized = 7 // an authorization check failed
)

const usage = `usage: rt [flags] <command> [arguments]

Commands:
  init [--admin U --tenant T]
                           create or migrate the kernel tables; a first user with all
                           authorizations is created with --admin or asked for on a terminal
  plan <package>           show the install or upgrade plan of a package directory
  install <package>...     install package directories
  upgrade <package>        upgrade an installed package
  uninstall <id>           remove a package and its data (needs --yes)
  list                     list installed packages
  show <object>            show a package, table, domain, data element, structure, array type
                           or authorization object (kind:name to disambiguate)
  check                    compare the repository with the database schema; list the users
                           with all authorizations and warn when no active user has them
  log                      show the operation log
  tenant create <id>       create a tenant (--description)
  tenant list              list tenants
  tenant delete <id>       delete a tenant and all its data (needs --yes)
  setting list             list settings
  setting show <id>        show the resolved definition of a setting (--json for JSON)
  asset repos [package]    list asset repositories
  asset list <package> <repository> [folder]
                           list a folder of an asset repository
  asset show <package> <repository> <path>
                           show an asset with its language variants
  asset get <package> <repository> <path> -o <file> [--lang code]
                           write an asset to a file, - for standard output
  language add <code>      set up a language (ISO 639-1, --name)
  language list            list languages
  language remove <code>   remove a language; texts in it need --yes
  natives list             list the methods executed by kernel that this kernel implements
  user create <name> [--tenant T] [--lang L]
                           create a user; the password is read from standard input
  user list                list users
  user lock|unlock <name>  lock a user or unlock it and reset its failed logins
  user passwd <name>       set the password of a user from standard input
  user tenant add|remove <name> <tenant>
                           let a user work in a tenant, or no longer
  token create <user> --label X [--expires 2027-01-31]
                           create an API token; it is shown only once
  token list [user]        list API tokens without their secrets
  token revoke <id>        delete an API token
  role list [--package P] [--incomplete]
                           list package roles and local roles
  role show <role>         show a role with its authorizations, their origin and problems
  role create|update|import <file.json>
                           create, replace or create-or-replace a local role (/LOCAL/<NAME>)
  role delete <role>       delete a local role; one in use needs --yes
  role export <role> [-o file]
                           write a role in the format of role files
  role assign <user> <role> --tenant T [--from D] [--to D]
                           assign a role to a user in a tenant, valid from and to a day
  role unassign <assignment>
                           remove a role assignment
  role history [role] [--user U]
                           show the changes of local roles and role assignments
  user roles <user> [--tenant T]
                           show the role assignments of a user
  user authorizations <user> --tenant T [--date D]
                           show the effective authorizations of a user with their origin
  user last-failed-check <user> --tenant T
                           show the last failed authorization check of a user in a tenant,
                           where it was made and the user's grants on the object
  trace authorizations start <user> [--tenant T] [--minutes 30] [--max 10000]
                           record the authorization checks of a user, in one tenant or all
  trace authorizations stop <user>
                           stop recording; the entries stay until the next start
  trace authorizations show <user> [--tenant T]
                           show the recorded checks with their result and place
  authorization-objects list [package]
                           list the authorization objects of a package or of all packages
  activities list          list the activities of the kernel (--lang for the texts)
  http list [package]      list the service nodes with their activation
  http show <path>         show a service node with its resolved settings and handlers
  http activate <path>     activate a service node and the never activated nodes below it
  http deactivate <path>   deactivate a service node; its subtree is not reachable
  serve [--addr :8080]     serve the reachable http services until SIGINT or SIGTERM
                           (--tls-cert/--tls-key, --read-timeout, --write-timeout,
                           --shutdown-timeout)
  effects list             list effects of native methods that failed after their commit
  effects retry <id>       run a failed effect again
  run <package> <class> <method> [json] --tenant <id> [--lang code] [--user U]
                           call a public static method of an installed package for a
                           tenant; arguments and result are JSON
  test <package|dir>       run the test classes of an installed package, or of a package
                           directory compiled against the installed packages; every test
                           method runs in a tenant of its own and nothing it writes stays

Commands for source files (no database needed):
  fmt [path...]            format source files in canonical form (--check only lists them)
  build <dir>              compile the sources of a package directory and show diagnostics;
                           required packages come from sibling directories
  disasm <dir> [name]      compile a package directory and list the bytecode of its
                           functions, or of a class or class.METHOD
  gen natives <dir> --go-package <name> [-o file]
                           generate the Go types of the structures, tables and enums that
                           the native methods of a kernel package use

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
	lang           string
	tenantID       string
	goPackage      string
	label          string
	expires        string
	set            map[string]bool // flags given on the command line
	output         string
	maxAssetSize   int64
	maxAssetRead   int64
	pkg            string
	incomplete     bool
	userFilter     string
	from, to, date string
	admin          string
	check          bool
	minutes        int
	maxEntries     int
	serve          server.Config
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
	c.fs.StringVar(&c.lang, "lang", "", "language of an asset to read, or of texts in run")
	c.fs.StringVar(&c.tenantID, "tenant", "", "run: the tenant whose data the code reads and writes")
	c.fs.StringVar(&c.goPackage, "go-package", "", "gen natives: the Go package of the generated file")
	c.fs.StringVar(&c.label, "label", "", "token create: the label of the token")
	c.fs.StringVar(&c.expires, "expires", "", "token create: the day the token expires, YYYY-MM-DD")
	c.fs.StringVar(&c.output, "o", "", "file to write an asset or a role to, - for standard output")
	c.fs.StringVar(&c.pkg, "package", "", "role list: only the roles of a package, LOCAL for local roles")
	c.fs.BoolVar(&c.incomplete, "incomplete", false, "role list: only roles with authorizations that grant nothing")
	c.fs.StringVar(&c.userFilter, "user", "", "run: the user of the execution; role history: only the changes of a user")
	c.fs.StringVar(&c.from, "from", "", "role assign: first day of validity, YYYY-MM-DD")
	c.fs.StringVar(&c.to, "to", "", "role assign: last day of validity, YYYY-MM-DD")
	c.fs.StringVar(&c.admin, "admin", "", "init: create this first user with all authorizations in --tenant; the password is read from standard input")
	c.fs.StringVar(&c.date, "date", "", "user authorizations: the day, YYYY-MM-DD (default today)")
	c.fs.BoolVar(&c.check, "check", false, "fmt: only list files that are not in canonical form")
	c.fs.IntVar(&c.minutes, "minutes", 30, "trace authorizations start: how long the trace runs")
	c.fs.IntVar(&c.maxEntries, "max", 10_000, "trace authorizations start: the most checks it records")
	c.fs.StringVar(&c.serve.Addr, "addr", server.DefaultAddr, "serve: the address to listen on")
	c.fs.StringVar(&c.serve.TLSCert, "tls-cert", "", "serve: PEM file of the TLS certificate")
	c.fs.StringVar(&c.serve.TLSKey, "tls-key", "", "serve: PEM file of the TLS key")
	c.fs.DurationVar(&c.serve.ReadTimeout, "read-timeout", server.DefaultReadTimeout, "serve: time to read a request with its body")
	c.fs.DurationVar(&c.serve.WriteTimeout, "write-timeout", server.DefaultWriteTimeout, "serve: time to write a response")
	c.fs.DurationVar(&c.serve.ShutdownTimeout, "shutdown-timeout", server.DefaultShutdownTimeout, "serve: time running requests get on shutdown")
	maxSize := int64(model.DefaultMaxAssetSize)
	if env := os.Getenv("RT_MAX_ASSET_SIZE"); env != "" {
		n, err := strconv.ParseInt(env, 10, 64)
		if err != nil || n <= 0 {
			fmt.Fprintln(stderr, "error: RT_MAX_ASSET_SIZE must be a positive number of bytes")
			return exitUsage
		}
		maxSize = n
	}
	c.fs.Int64Var(&c.maxAssetSize, "max-asset-size", maxSize, "maximum size of one asset in bytes (default $RT_MAX_ASSET_SIZE or 16 MiB)")
	maxRead := int64(kernel.DefaultMaxAssetRead)
	if env := os.Getenv("RT_MAX_ASSET_READ"); env != "" {
		n, err := strconv.ParseInt(env, 10, 64)
		if err != nil || n <= 0 {
			fmt.Fprintln(stderr, "error: RT_MAX_ASSET_READ must be a positive number of bytes")
			return exitUsage
		}
		maxRead = n
	}
	c.fs.Int64Var(&c.maxAssetRead, "max-asset-read", maxRead, "largest asset in bytes that code loads into memory (default $RT_MAX_ASSET_READ or 16 MiB)")
	c.fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		c.fs.PrintDefaults()
	}
	pos, err := c.parse(args)
	c.set = map[string]bool{}
	c.fs.Visit(func(f *flag.Flag) { c.set[f.Name] = true })
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
	if code, ok := c.dispatchLang(ctx, pos[0], pos[1:]); ok {
		return code
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
	if c.maxAssetSize <= 0 {
		fmt.Fprintln(stderr, "error: --max-asset-size must be positive")
		return exitUsage
	}
	k.SetMaxAssetSize(c.maxAssetSize)
	if c.maxAssetRead <= 0 {
		fmt.Fprintln(stderr, "error: --max-asset-read must be positive")
		return exitUsage
	}
	k.SetMaxAssetRead(c.maxAssetRead)
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
	case "run":
		if (len(args) != 3 && len(args) != 4) || c.tenantID == "" {
			fmt.Fprintln(c.stderr, "usage: rt run <package> <class> <method> [arguments as JSON array] --tenant <id> [--lang code]")
			return exitUsage
		}
		return c.run(ctx, k, args)
	case "test":
		if !c.need(args, 1, "test <package|directory>") {
			return exitUsage
		}
		return c.test(ctx, k, args[0])
	case "init":
		if !c.need(args, 0, "init") {
			return exitUsage
		}
		p, err := k.Init(ctx, c.opts())
		if err != nil {
			return c.fail(err)
		}
		if code := c.printPlan(p); code != exitOK || c.dryRun {
			return code
		}
		return c.firstAdmin(ctx, k)
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
	case "natives":
		if !c.need(args, 1, "natives list") || args[0] != "list" {
			return exitUsage
		}
		return c.natives(ctx, k)
	case "http":
		return c.http(ctx, k, args)
	case "authorization-objects":
		return c.authorizationObjects(ctx, k, args)
	case "role":
		return c.role(ctx, k, args)
	case "activities":
		return c.activities(ctx, k, args)
	case "serve":
		if !c.need(args, 0, "serve [--addr :8080] [--tls-cert file --tls-key file]") {
			return exitUsage
		}
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := server.Run(ctx, c.serve, k.HTTPHandler()); err != nil {
			return c.fail(err)
		}
		return exitOK
	case "user":
		return c.user(ctx, k, args)
	case "trace":
		return c.trace(ctx, k, args)
	case "token":
		return c.token(ctx, k, args)
	case "effects":
		switch {
		case len(args) == 1 && args[0] == "list":
			return c.effects(ctx, k)
		case len(args) == 2 && args[0] == "retry":
			if err := k.RetryEffect(ctx, args[1]); err != nil {
				return c.fail(err)
			}
			fmt.Fprintf(c.stdout, "Effect %s ran.\n", args[1])
			return exitOK
		}
		fmt.Fprintln(c.stderr, "usage: rt effects list | rt effects retry <id>")
		return exitUsage
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
		holders, warnings, err := c.allHolders(ctx, k)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(map[string]any{"discrepancies": append([]kernel.Discrepancy{}, d...), "allAuthorizations": holders, "warnings": warnings})
		} else {
			if len(d) == 0 {
				fmt.Fprintln(c.stdout, "Repository and database schema match.")
			}
			for _, x := range d {
				fmt.Fprintln(c.stdout, x)
			}
			c.printHolders(holders, warnings)
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
	case "setting":
		return c.setting(ctx, k, args)
	case "asset":
		return c.asset(ctx, k, args)
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
			if c, ok := r.Constraint.Get(); ok {
				req = append(req, string(r.ID)+" "+c)
			} else {
				req = append(req, string(r.ID))
			}
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
		fmt.Fprintf(c.stdout, "Table %s (owner %s, tenant-specific %v)\n  %s\n  data class %s\n", t.Name, t.Owner, t.TenantSpecific, t.Description, t.DataClass)
		if id, ok := d.Setting.Get(); ok {
			fmt.Fprintf(c.stdout, "  setting %s\n", id)
		} else {
			fmt.Fprintln(c.stdout, "  no setting")
		}
		fmt.Fprintln(c.stdout)
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "  FIELD\tDATA ELEMENT\tDOMAIN\tKEY\tNOT NULL\tDEFAULT\tOWNER\tORIGIN")
		for _, f := range t.Fields {
			key := ""
			if t.IsKey(f.Name) {
				key = "yes"
			}
			def := f.ResolvedDefault.String() // empty without default, <null>, or a quoted value
			origin := string(f.Origin)
			if src, ok := f.Source.Get(); ok {
				origin += " via " + src
				if inc := f.Included.Or(""); inc != src {
					origin += " (include " + inc + ")"
				}
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%v\t%s\t%s\t%s\n", f.Name, f.DataElement, f.Domain, key, f.NotNull, def, f.Owner, origin)
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
		for _, inc := range d.Includes {
			fmt.Fprintf(c.stdout, "  includes %s after %d fields\n", inc.Name, inc.After)
		}
		for _, u := range d.UsedBy {
			fmt.Fprintf(c.stdout, "  used by %s %s (%s)\n", u.Kind, u.Name, u.Via)
		}
	case kernel.KindAuthorizationObject:
		return c.showAuthorizationObject(ctx, k, name)
	case kernel.KindClass:
		d, err := k.Class(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		fmt.Fprintf(c.stdout, "%s %s (package %s, %s)\n", strings.ToUpper(string(d.Kind[:1]))+string(d.Kind[1:]), d.Name, d.Package, d.File)
	case kernel.KindStructure, kernel.KindArrayType:
		d, err := k.Structure(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		printStructure(c.stdout, d)
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
		if dom.Type.IsString() {
			fmt.Fprintf(c.stdout, "  allows empty string %v\n", dom.AllowsEmpty())
		}
		if !dom.Default.IsZero() {
			fmt.Fprintf(c.stdout, "  default %s\n", dom.Default)
		}
		for _, v := range dom.Values {
			fmt.Fprintf(c.stdout, "  value %q  %s\n", v.Value, v.Description)
		}
		for _, u := range d.UsedBy {
			fmt.Fprintf(c.stdout, "  used by %s\n", u)
		}
	}
	if kind != kernel.KindPackage {
		uses, err := k.CodeUses(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		for _, u := range uses {
			what := u.Kind
			if m, ok := u.Member.Get(); ok {
				what += " " + m
			}
			fmt.Fprintf(c.stdout, "  used in code by %s at %s:%d (%s)\n", u.Package, u.File, u.Line, what)
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
			from, hasFrom := p.From.Get()
			to, hasTo := p.To.Get()
			switch {
			case hasFrom && hasTo:
				objs = append(objs, p.ID+" "+from+"->"+to)
			case hasTo:
				objs = append(objs, p.ID+" "+to)
			default:
				objs = append(objs, p.ID+" "+from)
			}
		}
		if t, ok := e.Target.Get(); ok {
			objs = append(objs, t)
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
		desc := model.None[string]()
		if c.set["description"] {
			desc = model.Some(c.description)
		}
		t, err := ten.Create(ctx, args[1], desc)
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

func (c *cli) setting(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.stderr, "usage: rt setting list|show")
		return exitUsage
	}
	switch args[0] {
	case "list":
		list, err := k.Settings(ctx)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SETTING\tOWNER\tROOT\tTABLES\tCLASS\tTENANT\tEXTENDED BY\tLABEL")
		for _, s := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%v\t%s\t%s\n", s.ID, s.Owner, s.Root, s.Tables, s.DataClass, s.TenantSpecific, joinIDs(s.Extensions), s.Label)
		}
		w.Flush()
		return exitOK
	case "show":
		if !c.need(args[1:], 1, "setting show <id> [--json]") {
			return exitUsage
		}
		d, err := k.Setting(ctx, args[1])
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			b, err := d.JSON()
			if err != nil {
				return c.fail(err)
			}
			c.stdout.Write(b)
			return exitOK
		}
		printSetting(c.stdout, d)
		return exitOK
	}
	fmt.Fprintf(c.stderr, "error: unknown setting command %q\n", args[0])
	return exitUsage
}

func (c *cli) asset(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.stderr, "usage: rt asset repos|list|show|get")
		return exitUsage
	}
	switch args[0] {
	case "repos":
		if len(args) > 2 {
			fmt.Fprintln(c.stderr, "usage: rt asset repos [package]")
			return exitUsage
		}
		var pkg model.PackageID
		if len(args) == 2 {
			pkg = model.PackageID(args[1])
		}
		repos, err := k.AssetRepositories(ctx, pkg)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(repos)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PACKAGE\tREPOSITORY\tFILES\tFOLDERS\tBYTES\tLABEL")
		for _, r := range repos {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\n", r.Package, r.ID, r.Files, r.Folders, r.Size, r.Description[model.DefaultLanguage])
		}
		w.Flush()
		return exitOK
	case "list":
		if len(args) != 3 && len(args) != 4 {
			fmt.Fprintln(c.stderr, "usage: rt asset list <package> <repository> [folder]")
			return exitUsage
		}
		dir := ""
		if len(args) == 4 {
			dir = args[3]
		}
		list, err := k.ListAssets(ctx, model.PackageID(args[1]), args[2], dir)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tPATH\tBYTES\tMEDIA TYPE\tLANGUAGES")
		for _, a := range list {
			if a.Folder {
				fmt.Fprintf(w, "folder\t%s/\t\t\t\n", a.Path)
				continue
			}
			fmt.Fprintf(w, "file\t%s\t%d\t%s\t%s\n", a.Path, a.Size, a.MimeType.Or(""), strings.Join(a.Variants, ", "))
		}
		w.Flush()
		return exitOK
	case "show":
		if !c.need(args[1:], 3, "asset show <package> <repository> <path>") {
			return exitUsage
		}
		return c.showAsset(ctx, k, model.PackageID(args[1]), args[2], args[3])
	case "get":
		if !c.need(args[1:], 3, "asset get <package> <repository> <path> -o <file> [--lang code]") {
			return exitUsage
		}
		if c.output == "" {
			fmt.Fprintln(c.stderr, "error: name the target file with -o, or - for standard output")
			return exitUsage
		}
		r, err := k.OpenAsset(ctx, model.PackageID(args[1]), args[2], args[3], c.langOption())
		if err != nil {
			return c.fail(err)
		}
		defer r.Close()
		out := c.stdout
		if c.output != "-" {
			f, err := os.Create(c.output)
			if err != nil {
				return c.fail(err)
			}
			defer f.Close()
			out = f
		}
		if _, err := io.Copy(out, r); err != nil {
			return c.fail(err)
		}
		if c.output != "-" {
			info := r.Info()
			fmt.Fprintf(c.stderr, "Wrote %d bytes (%s, %s) to %s.\n", info.Size, info.MimeType.Or(""), versionName(info.Language), c.output)
		}
		return exitOK
	}
	fmt.Fprintf(c.stderr, "error: unknown asset command %q\n", args[0])
	return exitUsage
}

func (c *cli) showAsset(ctx context.Context, k *kernel.Kernel, pkg model.PackageID, repoID, path string) int {
	info, err := k.StatAsset(ctx, pkg, repoID, path, model.None[string]())
	var nf *rterr.AssetNotFound
	if errors.As(err, &nf) && nf.Path != "" {
		// Assets with variants only have no version without a language.
		info, err = k.StatAsset(ctx, pkg, repoID, path, model.Some(model.DefaultLanguage))
	}
	if err != nil {
		return c.fail(err)
	}
	type version struct {
		Language model.Optional[string] `json:"language"` // None for the language-independent version
		Source   model.Optional[string] `json:"source"`
		Size     int64                  `json:"size"`
		SHA256   model.Optional[string] `json:"sha256"`
		Modified string                 `json:"modifiedAt"`
	}
	var versions []version
	add := func(i *kernel.AssetInfo) {
		versions = append(versions, version{i.Language, i.Source, i.Size, i.SHA256, i.ModifiedAt.Format("2006-01-02 15:04:05")})
	}
	if !info.Folder {
		if info.HasNeutral {
			n, err := k.StatAsset(ctx, pkg, repoID, path, model.None[string]())
			if err != nil {
				return c.fail(err)
			}
			add(n)
		}
		for _, l := range info.Variants {
			v, err := k.StatAsset(ctx, pkg, repoID, path, model.Some(l))
			if err != nil {
				return c.fail(err)
			}
			add(v)
		}
	}
	if c.asJSON {
		c.json(struct {
			*kernel.AssetInfo
			Versions []version `json:"versions,omitempty"`
		}{info, versions})
		return exitOK
	}
	kind := "Asset"
	if info.Folder {
		kind = "Folder"
	}
	fmt.Fprintf(c.stdout, "%s %s\n  package %s, repository %s\n", kind, path, pkg, repoID)
	if !info.Folder {
		fmt.Fprintf(c.stdout, "  media type %s\n", info.MimeType.Or(""))
	}
	for _, l := range slices.Sorted(maps.Keys(info.Description)) {
		fmt.Fprintf(c.stdout, "  description (%s) %s\n", l, info.Description[l])
	}
	if len(versions) > 0 {
		fmt.Fprintln(c.stdout)
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "  VERSION\tSOURCE\tBYTES\tSHA-256\tMODIFIED")
		for _, v := range versions {
			fmt.Fprintf(w, "  %s\t%s\t%d\t%s\t%s\n", versionName(v.Language), v.Source.Or(""), v.Size, v.SHA256.Or(""), v.Modified)
		}
		w.Flush()
	}
	return exitOK
}

// versionName names the version of an asset for display.
func versionName(lang model.Optional[string]) string {
	return lang.Or("language-independent")
}

// langOption returns --lang if it was given.
func (c *cli) langOption() model.Optional[string] {
	if c.set["lang"] {
		return model.Some(c.lang)
	}
	return model.None[string]()
}

func joinIDs(ids []model.PackageID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = string(id)
	}
	return strings.Join(s, ", ")
}

func ops(o setting.Operations) string {
	var s []string
	for _, x := range []struct {
		on   bool
		name string
	}{{o.Create, "create"}, {o.Update, "update"}, {o.Delete, "delete"}} {
		if x.on {
			s = append(s, x.name)
		}
	}
	if len(s) == 0 {
		return "read only"
	}
	return strings.Join(s, ", ")
}

// printSetting renders a resolved setting as a tree with English labels.
// printStructure prints a resolved definition as a tree with the type,
// key data and origin of every component, then its uses.
func printStructure(out io.Writer, d *kernel.StructureDetail) {
	def := d.Definition
	shape := "flat"
	if !def.Flat {
		shape = "deep"
	}
	if def.Kind == structure.KindArrayType {
		shape = "array"
	}
	fmt.Fprintf(out, "%s %s (owner %s, %s)\n", strings.ToUpper(string(def.Kind[:1]))+strings.ReplaceAll(string(def.Kind[1:]), "-", " "), def.Name, def.Owner, shape)
	if t, ok := def.Texts[model.DefaultLanguage]; ok {
		fmt.Fprintf(out, "  %s\n", t.Label)
	}
	if len(def.Extensions) > 0 {
		fmt.Fprintf(out, "  extended by %s\n", joinIDs(def.Extensions))
	}
	fmt.Fprintln(out)
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "  COMPONENT\tTYPE\tNOT NULL\tDEFAULT\tORIGIN")
	var walk func(d *structure.Definition, indent string)
	row := func(c structure.Component, indent string) {
		typ := ""
		switch c.Kind {
		case structure.Elementary:
			typ = c.Domain + " " + string(c.Type)
			if c.Length > 0 {
				typ += fmt.Sprintf("(%d", c.Length)
				if c.Decimals > 0 {
					typ += fmt.Sprintf(",%d", c.Decimals)
				}
				typ += ")"
			}
			if c.DataElement != nil {
				typ = c.DataElement.Name + " = " + typ
			}
		case structure.Nested:
			typ = "structure " + c.Structure.Name
		case structure.Array:
			typ = "array " + c.ArrayType.Name
		}
		origin := ""
		if o := c.Origin; o != nil {
			origin = o.Declared
			if len(o.Includes) > 0 {
				origin = "include " + strings.Join(o.Includes, " > ")
			}
			if ext, ok := o.Extension.Get(); ok {
				origin += ", extension " + string(ext)
			}
		}
		name := c.Name
		if name == "" {
			name = "(row)"
		}
		fmt.Fprintf(w, "  %s%s\t%s\t%v\t%s\t%s\n", indent, name, typ, c.NotNull, c.Default, origin)
		switch c.Kind {
		case structure.Nested:
			walk(c.Structure, indent+"  ")
		case structure.Array:
			walk(c.ArrayType, indent+"  ")
		}
	}
	walk = func(d *structure.Definition, indent string) {
		if d.Array != nil {
			row(d.Array.Row, indent)
			return
		}
		for _, c := range d.Components {
			row(c, indent)
		}
	}
	walk(def, "")
	w.Flush()
	if a := def.Array; a != nil && a.Key != nil {
		kind := "non-unique"
		if a.Key.Unique {
			kind = "unique"
		}
		fmt.Fprintf(out, "  %s key %s\n", kind, strings.Join(a.Key.Fields, ", "))
	}
	for _, u := range d.UsedBy {
		fmt.Fprintf(out, "  used by %s %s (%s)\n", u.Kind, u.Name, u.Via)
	}
}

func printSetting(w io.Writer, d *setting.Definition) {
	fmt.Fprintf(w, "Setting %s: %s\n", d.ID, d.Texts[model.DefaultLanguage].Label)
	fmt.Fprintf(w, "  owner %s, data class %s, tenant-specific %v", d.Owner, d.DataClass, d.TenantSpecific)
	if len(d.Extensions) > 0 {
		fmt.Fprintf(w, ", extended by %s", joinIDs(d.Extensions))
	}
	fmt.Fprintf(w, "\n  languages %s\n", strings.Join(d.Languages, ", "))
	d.Walk(func(t *setting.Table, depth int) {
		indent := strings.Repeat("    ", depth)
		fmt.Fprintf(w, "\n%s%s (owner %s, added by %s; %s)\n", indent, t.Name, t.Owner, t.Origin, ops(t.Operations))
		if t.ParentKey != nil {
			var cols []string
			for _, p := range t.ParentKey.Fields {
				cols = append(cols, p.Field+" -> "+p.References)
			}
			fmt.Fprintf(w, "%s  parent %s through %s (%s)\n", indent, t.ParentKey.Table, t.ParentKey.Name, strings.Join(cols, ", "))
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, f := range t.Fields {
			label := ""
			if f.DataElement != nil {
				label = f.DataElement.Texts[model.DefaultLanguage].Medium
			}
			typ := string(f.Type)
			switch f.Type {
			case model.Char:
				typ = fmt.Sprintf("CHAR(%d)", f.Length)
			case model.Dec:
				typ = fmt.Sprintf("DEC(%d,%d)", f.Length, f.Decimals)
			}
			var flags []string
			if f.Key {
				flags = append(flags, "key")
			}
			if f.NotNull {
				flags = append(flags, "not null")
			}
			if f.FromParent {
				flags = append(flags, "from parent")
			}
			var help []string
			if len(f.FixedValues) > 0 {
				help = append(help, fmt.Sprintf("%d fixed values", len(f.FixedValues)))
			}
			for _, h := range f.ValueHelps {
				help = append(help, "values of "+h.Table)
			}
			fmt.Fprintf(tw, "%s  %s\t%s\t%s\t%s\t%s\n", indent, f.Name, label, typ, strings.Join(flags, ", "), strings.Join(help, "; "))
		}
		tw.Flush()
	})
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
		anf  *rterr.AssetNotFound
		nsv  *rterr.NamespaceViolation
		ce   *rterr.CompileError
		kp   *rterr.KernelPackage
	)
	switch {
	case errors.As(err, &dc):
		fmt.Fprintln(c.stderr, "hint: rerun with --allow-destructive, or with --dry-run to see the plan")
		return exitNeedsPermit
	case errors.As(err, &conf):
		fmt.Fprintln(c.stderr, "hint: rerun with --yes, or with --dry-run to see the plan")
		return exitNeedsPermit
	case errors.As(err, &anf):
		return exitError
	case errors.As(err, &ml):
		fmt.Fprintln(c.stderr, "hint: add the languages with rt language add")
		return exitInvalid
	case errors.As(err, &v), errors.As(err, &dep), errors.As(err, &nc), errors.As(err, &nsv), errors.As(err, &ce), errors.As(err, &kp):
		return exitInvalid
	case errors.As(err, &ee):
		return exitExec
	case errors.Is(err, rterr.ErrNotInitialized):
		fmt.Fprintln(c.stderr, "hint: run rt init")
	}
	return exitError
}

// natives lists the native methods of the kernel.
func (c *cli) natives(ctx context.Context, k *kernel.Kernel) int {
	list, err := k.Natives(ctx)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(list)
		return exitOK
	}
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "METHOD\tDECLARATION\tCAPABILITIES\tTIMEOUT\tDECLARED\tUSED")
	for _, n := range list {
		timeout := "-"
		if n.Timeout > 0 {
			timeout = n.Timeout.String()
		}
		caps := strings.Join(n.Capabilities, ", ")
		if caps == "" {
			caps = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", n.Key, n.Declaration, caps, timeout, yes(n.Declared), yes(n.Used))
	}
	w.Flush()
	return exitOK
}

// effects lists the effects that failed after their commit.
func (c *cli) effects(ctx context.Context, k *kernel.Kernel) int {
	list, err := k.Effects(ctx)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(list)
		return exitOK
	}
	if len(list) == 0 {
		fmt.Fprintln(c.stdout, "No failed effects.")
		return exitOK
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tFAILED\tTENANT\tKIND\tATTEMPTS\tERROR")
	for _, e := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", e.ID, e.FailedAt.Format("2006-01-02 15:04:05"), e.Tenant.Or("-"), e.Kind, e.Attempts, e.Error)
	}
	w.Flush()
	return exitOK
}

// http manages the service nodes.
func (c *cli) http(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt http list [package] | show <path> | activate <path> | deactivate <path>")
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	switch {
	case args[0] == "list" && len(args) <= 2:
		pkg := model.PackageID("")
		if len(args) == 2 {
			pkg = model.PackageID(args[1])
		}
		list, err := k.HTTPServices(ctx, pkg)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PATH\tSTATE\tREACHABLE\tHANDLERS")
		for _, n := range list {
			depth := strings.Count(n.Path, "/") - 1
			var handlers []string
			for _, h := range n.Handlers {
				handlers = append(handlers, h.Class)
			}
			reach := "no"
			if n.Reachable {
				reach = "yes"
			}
			fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\n", strings.Repeat("  ", depth), n.Path, n.State, reach, strings.Join(handlers, ", "))
		}
		w.Flush()
		return exitOK
	case args[0] == "show" && len(args) == 2:
		n, err := k.HTTPService(ctx, args[1])
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(n)
			return exitOK
		}
		s := n.Settings
		fmt.Fprintf(c.stdout, "Service node %s (package %s, %s)\n", n.Path, n.Package, n.File.Or("no file, a parent of other nodes"))
		if l, ok := n.Texts["en"]; ok {
			fmt.Fprintf(c.stdout, "  label: %s\n", l.Label)
		}
		reach := "not reachable"
		if n.Reachable {
			reach = "reachable"
		}
		fmt.Fprintf(c.stdout, "  state: %s, %s\n", n.State, reach)
		fmt.Fprintf(c.stdout, "  authentication: %s, anonymous: %v, anonymous tenant: %s\n", strings.Join(s.Authentication, ", "), s.Anonymous, s.AnonymousTenant.Or("-"))
		fmt.Fprintf(c.stdout, "  tenant from: %s, language from: %s\n", strings.Join(s.TenantSources, ", "), strings.Join(s.LanguageSources, ", "))
		fmt.Fprintf(c.stdout, "  max body: %d bytes, max response: %d bytes, timeout: %s, budget: %d\n", s.MaxBodySize, s.MaxResponseSize, s.Timeout, s.Budget)
		for i, h := range n.Handlers {
			fmt.Fprintf(c.stdout, "  handler %d: %s", i+1, h.Class)
			for _, p := range h.Parameters {
				fmt.Fprintf(c.stdout, " %s=%q", p.Name, p.Value)
			}
			fmt.Fprintln(c.stdout)
		}
		return exitOK
	case (args[0] == "activate" || args[0] == "deactivate") && len(args) == 2:
		var err error
		if args[0] == "activate" {
			err = k.ActivateHTTPService(ctx, args[1])
		} else {
			err = k.DeactivateHTTPService(ctx, args[1])
		}
		if err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Service node %s %sd.\n", args[1], args[0])
		return exitOK
	}
	return usage()
}

// input is standard input; tests replace it.
var input io.Reader = os.Stdin

// readSecret reads a password from standard input: its first line.
func (c *cli) readSecret() (string, error) {
	line, err := readLine()
	if err != nil {
		return "", errors.New("no password on standard input")
	}
	return line, nil
}

// lines reads standard input line by line; it is made again when tests
// replace input.
var lines struct {
	r    *bufio.Reader
	from io.Reader
}

// readLine reads a line of standard input without its end.
func readLine() (string, error) {
	if lines.r == nil || lines.from != input {
		lines.r, lines.from = bufio.NewReader(input), input
	}
	line, err := lines.r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// user manages users.
func (c *cli) user(ctx context.Context, k *kernel.Kernel, args []string) int {
	us := k.Users()
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt user create <name> [--tenant T] [--lang L] | list | lock <name> | unlock <name> | passwd <name> | tenant add|remove <name> <tenant> | "+
			"roles <name> [--tenant T] | authorizations <name> --tenant T [--date D]")
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	done := func(err error, format string, a ...any) int {
		if err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, format+"\n", a...)
		return exitOK
	}
	switch {
	case args[0] == "create" && len(args) == 2:
		pw, err := c.readSecret()
		if err != nil {
			return c.fail(err)
		}
		opts := kernel.UserOptions{}
		if c.tenantID != "" {
			opts.Tenant = model.Some(c.tenantID)
		}
		if c.lang != "" {
			opts.Language = model.Some(c.lang)
		}
		_, err = us.Create(ctx, args[1], pw, opts)
		return done(err, "User %s created.", args[1])
	case args[0] == "list" && len(args) == 1:
		list, err := us.List(ctx)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			if list == nil {
				list = []*kernel.User{}
			}
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "USER\tSTATUS\tLANGUAGE\tDEFAULT TENANT\tTENANTS\tFAILED LOGINS")
		for _, u := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n", u.Name, u.Status, u.Language.Or("-"), u.DefaultTenant.Or("-"), strings.Join(u.Tenants, ", "), u.FailedLogins)
		}
		w.Flush()
		return exitOK
	case args[0] == "lock" && len(args) == 2:
		return done(us.Lock(ctx, args[1], c.roleOpts()), "User %s locked.", args[1])
	case args[0] == "unlock" && len(args) == 2:
		return done(us.Unlock(ctx, args[1]), "User %s unlocked.", args[1])
	case args[0] == "passwd" && len(args) == 2:
		pw, err := c.readSecret()
		if err != nil {
			return c.fail(err)
		}
		return done(us.SetPassword(ctx, args[1], pw), "Password of %s set.", args[1])
	case args[0] == "tenant" && len(args) == 4 && args[1] == "add":
		return done(us.AddTenant(ctx, args[2], args[3]), "User %s may work in tenant %s.", args[2], args[3])
	case args[0] == "tenant" && len(args) == 4 && args[1] == "remove":
		return done(us.RemoveTenant(ctx, args[2], args[3], c.roleOpts()), "User %s no longer works in tenant %s.", args[2], args[3])
	case args[0] == "roles" && len(args) == 2:
		return c.userRoles(ctx, k, args[1])
	case args[0] == "authorizations" && len(args) == 2:
		return c.userAuthorizations(ctx, k, args[1])
	case args[0] == "last-failed-check" && len(args) == 2:
		return c.lastFailedCheck(ctx, k, args[1])
	}
	return usage()
}

// token manages API tokens.
func (c *cli) token(ctx context.Context, k *kernel.Kernel, args []string) int {
	ts := k.Tokens()
	switch {
	case len(args) == 2 && args[0] == "create":
		expires := model.None[time.Time]()
		if c.expires != "" {
			d, err := time.Parse(time.DateOnly, c.expires)
			if err != nil {
				fmt.Fprintln(c.stderr, "error: --expires needs a date YYYY-MM-DD")
				return exitUsage
			}
			expires = model.Some(d.UTC())
		}
		token, info, err := ts.Create(ctx, args[1], c.label, expires)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(map[string]any{"token": token, "info": info})
			return exitOK
		}
		fmt.Fprintf(c.stdout, "Token %s for %s created. It is shown only now:\n%s\n", info.ID, info.User, token)
		return exitOK
	case len(args) <= 2 && len(args) >= 1 && args[0] == "list":
		user := ""
		if len(args) == 2 {
			user = args[1]
		}
		list, err := ts.List(ctx, user)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			if list == nil {
				list = []*kernel.APIToken{}
			}
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tUSER\tLABEL\tEXPIRES\tCREATED")
		for _, t := range list {
			exp := "-"
			if e, ok := t.ExpiresAt.Get(); ok {
				exp = e.Format(time.DateOnly)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.User, t.Label, exp, t.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "revoke":
		if err := ts.Revoke(ctx, args[1]); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Token %s revoked.\n", args[1])
		return exitOK
	}
	fmt.Fprintln(c.stderr, "usage: rt token create <user> --label X [--expires YYYY-MM-DD] | list [user] | revoke <id>")
	return exitUsage
}

// showAuthorizationObject prints the resolved definition of an
// authorization object and where code uses it.
func (c *cli) showAuthorizationObject(ctx context.Context, k *kernel.Kernel, name string) int {
	o, err := k.AuthorizationObject(ctx, name)
	if err != nil {
		return c.fail(err)
	}
	uses, err := k.CodeUses(ctx, name)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(struct {
			Definition any              `json:"definition"`
			CodeUses   []kernel.CodeUse `json:"codeUses"`
		}{o, uses})
		return exitOK
	}
	fmt.Fprintf(c.stdout, "Authorization object %s (package %s)\n", o.Name, o.Package)
	for _, l := range o.Languages {
		if t, ok := o.Texts[l]; ok {
			fmt.Fprintf(c.stdout, "  %s: %s", l, t.Label)
			if d, ok := t.Description.Get(); ok {
				fmt.Fprintf(c.stdout, " - %s", d)
			}
			fmt.Fprintln(c.stdout)
		}
	}
	fmt.Fprintln(c.stdout)
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "  FIELD\tDATA ELEMENT\tDOMAIN\tTYPE\tLABEL\tVALUES")
	for _, f := range o.Fields {
		typ := string(f.Type)
		if n, ok := f.Length.Get(); ok {
			typ = fmt.Sprintf("%s(%d)", f.Type, n)
		}
		label := ""
		if t, ok := f.Labels[model.DefaultLanguage]; ok {
			label = t.Medium
		}
		var values []string
		for _, v := range f.Values {
			values = append(values, v.Value)
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n", f.Name, f.DataElement.Or("-"), f.Domain, typ, label, strings.Join(values, " "))
	}
	w.Flush()
	if len(o.Activities) > 0 {
		fmt.Fprintln(c.stdout)
		for _, a := range o.Activities {
			fmt.Fprintf(c.stdout, "  activity %s: %s\n", a.Code, a.Texts[model.DefaultLanguage].Label)
		}
	}
	// Every check and lookup in code once, without its fields.
	first := true
	for _, u := range uses {
		if u.Member.Valid() {
			continue
		}
		if first {
			fmt.Fprintln(c.stdout, "\nUsed in code:")
			first = false
		}
		fmt.Fprintf(c.stdout, "  %s %s:%d\n", u.Package, u.File, u.Line)
	}
	return exitOK
}

// authorizationObjects lists the authorization objects of a package or of
// all packages.
func (c *cli) authorizationObjects(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) == 0 || args[0] != "list" || len(args) > 2 {
		fmt.Fprintln(c.stderr, "usage: rt authorization-objects list [package]")
		return exitUsage
	}
	pkg := model.PackageID("")
	if len(args) == 2 {
		pkg = model.PackageID(args[1])
	}
	list, err := k.AuthorizationObjects(ctx, pkg)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(list)
		return exitOK
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPACKAGE\tLABEL\tFIELDS")
	for _, o := range list {
		var fields []string
		for _, f := range o.Fields {
			fields = append(fields, f.Name)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", o.Name, o.Package, o.Texts[model.DefaultLanguage].Label, strings.Join(fields, ", "))
	}
	w.Flush()
	return exitOK
}

// activities lists the activities of the kernel in the language of --lang.
func (c *cli) activities(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) != 1 || args[0] != "list" {
		fmt.Fprintln(c.stderr, "usage: rt activities list [--lang code]")
		return exitUsage
	}
	list, err := k.Activities(ctx)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(list)
		return exitOK
	}
	lang := c.lang
	if lang == "" {
		lang = model.DefaultLanguage
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CODE\tLABEL\tEXPLANATION")
	for _, a := range list {
		t := reflector.In(a.Texts, lang).Or(model.ActivityText{})
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.Code, t.Label, t.Description)
	}
	w.Flush()
	return exitOK
}
