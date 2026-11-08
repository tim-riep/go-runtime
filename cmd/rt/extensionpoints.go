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
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
)

// implementation runs the commands on implementations of extension
// points.
func (c *cli) implementation(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt implementation activate|deactivate|reset <name> [--tenant T] | history [name]")
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	impls := k.Implementations()
	opts := kernel.ImplementationOptions{Tenant: c.tenantID, By: cliUser}
	where := "globally"
	if c.tenantID != "" {
		where = "in tenant " + c.tenantID
	}
	switch cmd, rest := args[0], args[1:]; {
	case cmd == "activate" && len(rest) == 1:
		if err := impls.Activate(ctx, rest[0], opts); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Implementation %s activated %s.\n", rest[0], where)
		return exitOK
	case cmd == "deactivate" && len(rest) == 1:
		if err := impls.Deactivate(ctx, rest[0], opts); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Implementation %s deactivated %s.\n", rest[0], where)
		return exitOK
	case cmd == "reset" && len(rest) == 1:
		if err := impls.Reset(ctx, rest[0], opts); err != nil {
			return c.fail(err)
		}
		if c.tenantID != "" {
			fmt.Fprintf(c.stdout, "Implementation %s follows the global activation in tenant %s.\n", rest[0], c.tenantID)
		} else {
			fmt.Fprintf(c.stdout, "Implementation %s is active again where no tenant decides otherwise.\n", rest[0])
		}
		return exitOK
	case cmd == "history" && len(rest) <= 1:
		name := ""
		if len(rest) == 1 {
			name = rest[0]
		}
		list, err := impls.History(ctx, name)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tBY\tCHANGE\tIMPLEMENTATION\tTENANT\tOLD\tNEW")
		for _, h := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", h.ChangedAt.Format("2006-01-02 15:04:05"), h.ChangedBy, h.Kind, h.Implementation,
				h.Tenant.Or("-"), h.OldState.Or("-"), h.NewState.Or("-"))
		}
		w.Flush()
		return exitOK
	}
	return usage()
}

// extensionPoint runs the commands on extension points.
func (c *cli) extensionPoint(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt extension-point list [package] | show <name>")
		return exitUsage
	}
	switch {
	case len(args) >= 1 && len(args) <= 2 && args[0] == "list":
		pkg := model.PackageID("")
		if len(args) == 2 {
			pkg = model.PackageID(args[1])
		}
		list, err := k.ExtensionPoints(ctx, pkg)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPACKAGE\tUSE\tINTERFACE\tFILTERS\tIMPLEMENTATIONS\tDESCRIPTION")
		for _, p := range list {
			var filters []string
			for _, f := range p.Filters {
				filters = append(filters, f.Name)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", p.Name, p.Package, p.Use, p.Interface, strings.Join(filters, ", "), len(p.Implementations),
				reflector.In(p.Description, c.lang).Or(""))
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "show":
		d, err := k.ExtensionPoint(ctx, args[1])
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		c.printExtensionPoint(d)
		return exitOK
	}
	return usage()
}

func (c *cli) printExtensionPoint(d *kernel.ExtensionPointDetail) {
	out := c.stdout
	fmt.Fprintf(out, "Extension point %s (package %s)\n", d.Name, d.Package)
	fmt.Fprintf(out, "  description: %s\n", reflector.In(d.Description, c.lang).Or("-"))
	fmt.Fprintf(out, "  interface: %s, use: %s, fallback: %s, commit allowed: %v\n", d.Interface, d.Use, d.Fallback.Or("none"), d.CommitAllowed)
	for _, f := range d.Filters {
		typ := string(f.Type)
		if n, ok := f.Length.Get(); ok {
			typ += fmt.Sprintf("(%d)", n)
		}
		fmt.Fprintf(out, "  filter %s  %s  %s", f.Name, f.DataElement.Or(f.Domain), typ)
		if def, ok := f.Default.Get(); ok {
			fmt.Fprintf(out, "  default %s", entriesText(def))
		}
		fmt.Fprintln(out)
	}
	if len(d.Implementations) == 0 {
		fmt.Fprintln(out, "  no implementations")
	}
	for i, impl := range d.Implementations {
		fmt.Fprintf(out, "  implementation %d: %s (package %s, class %s, order %d)\n", i+1, impl.Name, impl.Package, impl.Class, impl.Order)
		for _, f := range d.Filters {
			fmt.Fprintf(out, "    %s: %s\n", f.Name, entriesText(impl.Filters[f.Name]))
		}
		if a := impl.Activation; a != nil {
			global := "active"
			if !a.Active {
				global = "inactive"
			}
			if _, ok := a.Global.Get(); !ok {
				global += " (no setting)"
			}
			fmt.Fprintf(out, "    globally: %s\n", global)
			for _, tenant := range slices.Sorted(maps.Keys(a.Tenants)) {
				state := "active"
				if !a.Tenants[tenant] {
					state = "inactive"
				}
				fmt.Fprintf(out, "    in tenant %s: %s\n", tenant, state)
			}
		}
	}
	if len(d.CodeUses) == 0 {
		fmt.Fprintln(out, "  not run by code")
	}
	for _, u := range d.CodeUses {
		member := ""
		if m, ok := u.Member.Get(); ok {
			member = " " + m
		}
		fmt.Fprintf(out, "  used by %s %s:%d%s\n", u.Package, u.File, u.Line, member)
	}
}

// selectionTrace runs rt trace extension-points start|stop|show.
func (c *cli) selectionTrace(ctx context.Context, k *kernel.Kernel, cmd, user string) int {
	switch cmd {
	case "start":
		opts := kernel.TraceOptions{Duration: time.Duration(c.minutes) * time.Minute, MaxEntries: c.maxEntries, By: cliUser}
		if c.tenantID != "" {
			opts.Tenant = model.Some(c.tenantID)
		}
		t, err := k.StartImplementationTrace(ctx, user, opts)
		if err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Tracing the extension points that %s runs in %s until %s, at most %d.\n", t.User, t.Tenant.Or("all tenants"),
			t.Until.Format(time.RFC3339), t.MaxEntries)
		return exitOK
	case "stop":
		if err := k.StopImplementationTrace(ctx, user); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Trace of %s stopped.\n", user)
		return exitOK
	case "show":
		r, err := k.ImplementationTrace(ctx, user, c.tenantID)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(r)
			return exitOK
		}
		switch {
		case r.Trace == nil:
			fmt.Fprintf(c.stdout, "No trace of %s runs.\n", user)
		case r.Ended:
			fmt.Fprintf(c.stdout, "The trace of %s ended at %s.\n", user, r.Trace.Until.Format(time.RFC3339))
		default:
			fmt.Fprintf(c.stdout, "The trace of %s runs until %s.\n", user, r.Trace.Until.Format(time.RFC3339))
		}
		for _, e := range r.Entries {
			fmt.Fprintf(c.stdout, "  %s  %-8s %s  %s\n    %s\n", e.At.Format(time.RFC3339), e.Tenant.Or("-"), e.ExtensionPoint, filtersText(e.Filters), siteText(e.Site))
			for _, s := range e.Selection {
				fmt.Fprintf(c.stdout, "    %s: %s\n", s.Implementation, s.Reason)
			}
		}
		return exitOK
	}
	fmt.Fprintln(c.stderr, "usage: rt trace extension-points start|stop|show <user>")
	return exitUsage
}

func filtersText(values map[string]string) string {
	var parts []string
	for _, f := range slices.Sorted(maps.Keys(values)) {
		parts = append(parts, fmt.Sprintf("%s=%q", f, values[f]))
	}
	return strings.Join(parts, " ")
}
