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
	"flag"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
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

// nodeFlags are the flags of rt http configure; empty ones keep the
// setting.
type nodeFlags struct {
	authentication, tenantSources, languageSources                      string
	maxBody, maxResponse, timeout, budget, maxPageSize, metadataTimeout string
	reset                                                               bool
}

func (n *nodeFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&n.authentication, "authentication", "", "http configure: the methods of sign-in, basic and token, separated by commas")
	fs.StringVar(&n.tenantSources, "tenant-sources", "", "http configure: where the tenant comes from: header, query, user")
	fs.StringVar(&n.languageSources, "language-sources", "", "http configure: where the language comes from: query, header, user")
	fs.StringVar(&n.maxBody, "max-body-size", "", "http configure: the largest body of a request in bytes")
	fs.StringVar(&n.maxResponse, "max-response-size", "", "http configure: the largest response in bytes")
	fs.StringVar(&n.timeout, "timeout", "", "http configure: the time limit of a request in seconds")
	fs.StringVar(&n.budget, "budget", "", "http configure: the instruction budget of a request")
	fs.StringVar(&n.maxPageSize, "max-page-size", "", "http configure: the most entities a page of OData has")
	fs.StringVar(&n.metadataTimeout, "metadata-timeout", "", "http configure: the time limit of extensions of the metadata of OData in seconds")
	fs.BoolVar(&n.reset, "reset", false, "http configure: return to the defaults before applying the other settings")
}

// change returns the change the flags describe.
func (n *nodeFlags) change() (kernel.KernelNodeChange, error) {
	c := kernel.KernelNodeChange{Reset: n.reset}
	lists := []struct {
		flag string
		to   *model.Optional[[]string]
	}{{n.authentication, &c.Authentication}, {n.tenantSources, &c.TenantSources}, {n.languageSources, &c.LanguageSources}}
	for _, l := range lists {
		if l.flag != "" {
			*l.to = model.Some(strings.Split(l.flag, ","))
		}
	}
	numbers := []struct {
		name, flag string
		to         *model.Optional[int64]
	}{{"max-body-size", n.maxBody, &c.MaxBodySize}, {"max-response-size", n.maxResponse, &c.MaxResponseSize}, {"timeout", n.timeout, &c.TimeoutSeconds},
		{"budget", n.budget, &c.Budget}, {"max-page-size", n.maxPageSize, &c.MaxPageSize}, {"metadata-timeout", n.metadataTimeout, &c.MetadataTimeoutSeconds}}
	for _, x := range numbers {
		if x.flag == "" {
			continue
		}
		v, err := strconv.ParseInt(x.flag, 10, 64)
		if err != nil {
			return c, &rterr.InvalidArgument{Argument: "--" + x.name, Message: "not a number: " + x.flag}
		}
		*x.to = model.Some(v)
	}
	return c, nil
}
