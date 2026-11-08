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
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
)

// service runs the commands on services.
func (c *cli) service(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt service list [package] | show <name> | activate <name> | deactivate <name>")
		return exitUsage
	}
	switch {
	case len(args) >= 1 && len(args) <= 2 && args[0] == "list":
		pkg := model.PackageID("")
		if len(args) == 2 {
			pkg = model.PackageID(args[1])
		}
		list, err := k.Services(ctx, pkg)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPACKAGE\tPATH\tACTIVE\tENTITY SETS\tDESCRIPTION")
		for _, svc := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", svc.Name, svc.Package, svc.Path, yesNo(svc.Active), len(svc.EntitySets),
				reflector.In(svc.Description, c.lang).Or(""))
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "show":
		d, err := k.Service(ctx, args[1])
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		c.printService(d)
		return exitOK
	case len(args) == 2 && (args[0] == "activate" || args[0] == "deactivate"):
		change := k.ActivateService
		if args[0] == "deactivate" {
			change = k.DeactivateService
		}
		if err := change(ctx, args[1], cliUser); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "%sd %s\n", args[0], args[1])
		return exitOK
	}
	return usage()
}

func (c *cli) printService(d *kernel.ServiceDetail) {
	out := c.stdout
	fmt.Fprintf(out, "service %s (%s)\n", d.Name, reflector.In(d.Description, c.lang).Or(""))
	fmt.Fprintf(out, "  package:    %s\n", d.Package)
	fmt.Fprintf(out, "  path:       %s\n", d.Path)
	fmt.Fprintf(out, "  active:     %s\n", yesNo(d.Active))
	ext := string(d.Extensions)
	if d.Extensions == model.ExtensionsListed {
		ext = joinIDs(d.ExtensionPackages)
	}
	fmt.Fprintf(out, "  extensions: %s\n", ext)
	for _, set := range d.EntitySets {
		source := set.Type.Table
		if bo, ok := set.BusinessObject.Get(); ok {
			source = bo + " " + source
		}
		ro := ""
		if set.ReadOnly {
			ro = ", read only"
		}
		fmt.Fprintf(out, "  entity set %s: %s%s\n", set.Name, source, ro)
		c.printServiceType(set.Type, "    ")
	}
	for _, t := range d.Contained {
		fmt.Fprintf(out, "  contained %s: %s\n", t.Name, t.Table)
		c.printServiceType(t, "    ")
	}
	for _, a := range d.ActionImports {
		fmt.Fprintf(out, "  action import %s: %s\n", a.Name, a.Action)
	}
}

func (c *cli) printServiceType(t reflector.ServiceType, indent string) {
	out := c.stdout
	var ops []string
	for _, op := range t.Operations {
		ops = append(ops, string(op))
	}
	fmt.Fprintf(out, "%stype %s, key %s, operations %s\n", indent, t.Name, strings.Join(t.Key, ", "), orDash(strings.Join(ops, ", ")))
	for _, p := range t.Properties {
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{{p.Nullable, "nullable"}, {p.Computed, "computed"}, {p.Immutable, "immutable"}, {p.Mandatory, "mandatory"}, {p.ETag, "etag"}} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		fmt.Fprintf(out, "%s  %s = %s %s\n", indent, p.Name, p.Field, strings.Join(flags, " "))
	}
	for _, n := range t.Navigations {
		kind := "to one"
		if n.Collection {
			kind = "to many"
		}
		if n.Contained {
			kind += ", contained"
		}
		fmt.Fprintf(out, "%s  -> %s (%s %s by %s)\n", indent, n.Name, kind, n.Target.Or(n.Type), n.ForeignKey)
	}
	for _, a := range t.Actions {
		fmt.Fprintf(out, "%s  action %s = %s\n", indent, a.Name, a.Action)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
