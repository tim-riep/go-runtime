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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
)

// objectType runs the commands on object types that packages define.
func (c *cli) objectType(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt object-type list [package] | show <name> | schema <name>")
		return exitUsage
	}
	switch {
	case len(args) >= 1 && len(args) <= 2 && args[0] == "list":
		pkg := model.PackageID("")
		if len(args) == 2 {
			pkg = model.PackageID(args[1])
		}
		list, err := k.ObjectTypes(ctx, pkg)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPACKAGE\tCONTENT\tBEHAVIOR\tOBJECTS\tDESCRIPTION")
		for _, t := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", t.Name, t.Package, t.Content, t.Behavior.Or("-"), t.Objects, reflector.In(t.Description, c.lang).Or(""))
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "show":
		return c.showObjectType(ctx, k, args[1])
	case len(args) == 2 && args[0] == "schema":
		lang := c.lang
		if lang == "" {
			lang = model.DefaultLanguage
		}
		s, err := k.ObjectTypeSchema(ctx, args[1], lang)
		if err != nil {
			return c.fail(err)
		}
		c.json(s)
		return exitOK
	}
	return usage()
}

func (c *cli) showObjectType(ctx context.Context, k *kernel.Kernel, name string) int {
	d, err := k.ObjectType(ctx, name)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(d)
		return exitOK
	}
	fmt.Fprintf(c.stdout, "Object type %s (package %s)\n  %s\n  content %s\n", d.Name, d.Package, reflector.In(d.Description, c.lang).Or(""), d.Content)
	if b, ok := d.Behavior.Get(); ok {
		fmt.Fprintf(c.stdout, "  behavior %s\n", b)
	}
	fmt.Fprintf(c.stdout, "  %d objects\n", d.Objects)
	if d.Definition != nil {
		fmt.Fprintln(c.stdout)
		printStructure(c.stdout, &kernel.StructureDetail{Definition: d.Definition})
	}
	return exitOK
}

// object runs the commands on objects of package-defined types.
func (c *cli) object(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt object list <type> [package] | show <name>")
		return exitUsage
	}
	switch {
	case len(args) >= 2 && len(args) <= 3 && args[0] == "list":
		pkg := model.PackageID("")
		if len(args) == 3 {
			pkg = model.PackageID(args[2])
		}
		list, err := k.Objects(ctx, args[1], pkg)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPACKAGE\tREFERENCES\tHASH")
		for _, o := range list {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", o.Name, o.Package, len(o.References), o.Hash[:12])
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "show":
		return c.showObject(ctx, k, args[1])
	}
	return usage()
}

func (c *cli) showObject(ctx context.Context, k *kernel.Kernel, name string) int {
	d, err := k.Object(ctx, name)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(d)
		return exitOK
	}
	fmt.Fprintf(c.stdout, "Object %s of type %s (package %s, %s)\n  hash %s\n", d.Name, d.Type, d.Package, d.File, d.Hash)
	for _, r := range d.References {
		target := r.Target
		if r.Part != "" {
			target += " " + r.Part
		}
		fmt.Fprintf(c.stdout, "  refers to %s %s at %s\n", r.Kind, target, r.Pointer)
	}
	var b bytes.Buffer
	if err := json.Indent(&b, d.Content, "  ", "  "); err == nil {
		fmt.Fprintf(c.stdout, "\n  %s\n", b.String())
	}
	return exitOK
}

// showObjectUses prints the objects whose content refers to name.
func (c *cli) showObjectUses(ctx context.Context, k *kernel.Kernel, name string) error {
	uses, err := k.ObjectUses(ctx, name)
	if err != nil {
		return err
	}
	for _, u := range uses {
		what := string(u.Reference.Kind)
		if u.Reference.Part != "" {
			what += " " + u.Reference.Part
		}
		fmt.Fprintf(c.stdout, "  used by object %s of package %s at %s (%s)\n", u.Object, u.Package, u.Reference.Pointer, what)
	}
	return nil
}
