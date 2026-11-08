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

// businessObject runs the commands on business objects.
func (c *cli) businessObject(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt business-object list [package] [--data-class C] | show <name> [--base]")
		return exitUsage
	}
	switch {
	case len(args) >= 1 && len(args) <= 2 && args[0] == "list":
		pkg := model.PackageID("")
		if len(args) == 2 {
			pkg = model.PackageID(args[1])
		}
		list, err := k.BusinessObjects(ctx, reflector.BusinessObjectFilter{Package: pkg, DataClass: model.DataClass(c.dataClass)})
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPACKAGE\tDATA CLASS\tROOT\tENTITIES\tBEHAVIOR\tEXTENDED BY\tDESCRIPTION")
		for _, b := range list {
			var by []model.PackageID
			for _, x := range b.Extensions {
				by = append(by, x.Package)
			}
			ext := joinIDs(by)
			if ext == "" {
				ext = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n", b.Name, b.Package, b.DataClass, b.Root.Table, countEntities(b.Root), b.BehaviorClass.Or("-"),
				ext, reflector.In(b.Description, c.lang).Or(""))
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "show":
		d, err := k.BusinessObject(ctx, args[1], kernel.BusinessObjectOptions{Base: c.base})
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(d)
			return exitOK
		}
		c.printBusinessObject(d)
		return exitOK
	}
	return usage()
}

func countEntities(e *reflector.Entity) int {
	n := 1
	for _, c := range e.Children {
		n += countEntities(c)
	}
	return n
}

func (c *cli) printBusinessObject(d *kernel.BusinessObjectDetail) {
	out := c.stdout
	fmt.Fprintf(out, "Business object %s (package %s)\n", d.Name, d.Package)
	fmt.Fprintf(out, "  description: %s\n", reflector.In(d.Description, c.lang).Or("-"))
	fmt.Fprintf(out, "  data class: %s\n", d.DataClass)
	fmt.Fprintf(out, "  behavior class: %s\n", d.BehaviorClass.Or("-"))
	for _, x := range d.Extensions {
		fmt.Fprintf(out, "  extended by %s, behavior class %s\n", x.Package, x.BehaviorClass.Or("-"))
	}
	c.printEntity(d.Root, "  ", d.Package)
	if len(d.CodeUses) == 0 {
		fmt.Fprintln(out, "  not used by code")
	}
	for _, u := range d.CodeUses {
		member := ""
		if m, ok := u.Member.Get(); ok {
			member = " " + m
		}
		fmt.Fprintf(out, "  used by %s %s:%d %s %s%s\n", u.Package, u.File, u.Line, u.Kind, u.Object, member)
	}
}

// from names the package of a part that an extension adds.
func from(origin, owner model.PackageID) string {
	if origin == "" || origin == owner {
		return ""
	}
	return "  (" + string(origin) + ")"
}

func (c *cli) printEntity(e *reflector.Entity, indent string, owner model.PackageID) {
	out := c.stdout
	ops := make([]string, len(e.Operations))
	for i, op := range e.Operations {
		ops[i] = string(op)
	}
	fmt.Fprintf(out, "%sentity %s  key (%s)  operations %s  numbering %s%s\n", indent, e.Table, strings.Join(e.Key, ", "), strings.Join(ops, ", "), e.Numbering,
		from(e.Origin, owner))
	in := indent + "  "
	if p := e.ParentKey; p != nil {
		var pairs []string
		for _, f := range p.Fields {
			pairs = append(pairs, f.Field+" = "+f.References)
		}
		fmt.Fprintf(out, "%sparent %s through %s (%s)\n", in, p.Parent, p.ForeignKey, strings.Join(pairs, ", "))
	}
	for _, f := range e.Fields {
		var props []string
		for _, p := range []struct {
			on   bool
			name string
		}{{f.Key, "key"}, {f.ReadOnly, "read-only"}, {f.ReadOnlyOnUpdate, "read-only on update"}, {f.Mandatory, "mandatory"}, {f.ETag, "etag"}} {
			if p.on {
				props = append(props, p.name)
			}
		}
		if a, ok := f.Administrative.Get(); ok {
			props = append(props, a)
		}
		typ := string(f.Type)
		if f.Length > 0 {
			typ += fmt.Sprintf("(%d)", f.Length)
		}
		label := ""
		if f.DataElement != nil {
			if t, ok := reflector.In(f.DataElement.Texts, c.lang).Get(); ok {
				label = t.Medium
			}
		}
		by := ""
		if len(f.PropertiesBy) > 0 {
			by = "  (" + joinIDs(f.PropertiesBy) + ")"
		}
		fmt.Fprintf(out, "%sfield %s  %s  %s  %s%s\n", in, f.Name, typ, label, strings.Join(props, ", "), by)
	}
	for _, d := range e.Determinations {
		fmt.Fprintf(out, "%sdetermination %s on %s%s%s\n", in, d.Name, d.When, trigger(d.Trigger), from(d.Origin, owner))
	}
	for _, v := range e.Validations {
		fmt.Fprintf(out, "%svalidation %s%s%s\n", in, v.Name, trigger(v.Trigger), from(v.Origin, owner))
	}
	for _, a := range e.Actions {
		kind := "instance"
		if !a.Instance {
			kind = "static"
		}
		object := ""
		if au := a.Authorization; au != nil {
			object = "  authorization " + au.Object
		}
		fmt.Fprintf(out, "%saction %s  %s  parameter %s  result %s  modifies %v  activity %s%s%s\n", in, a.Name, kind, a.Parameter.Or("-"),
			a.Result.Or("-"), a.Modifies, a.Activity.Or("-"), object, from(a.Origin, owner))
	}
	switch src, _ := e.AuthorizationSource.Get(); src {
	case model.AuthorizationInherited:
		fmt.Fprintf(out, "%sauthorization of %s: %s\n", in, e.AuthorizationHolder.Or("-"), e.Authorization.Object)
	case model.AuthorizationConfiguration:
		fmt.Fprintf(out, "%sauthorization for configuration: %s\n", in, e.Authorization.Object)
	case model.AuthorizationOwn:
		fmt.Fprintf(out, "%sauthorization %s\n", in, e.Authorization.Object)
	}
	for _, ch := range e.Children {
		c.printEntity(ch, in, owner)
	}
}

// trigger describes the operations and fields of a trigger.
func trigger(t model.Trigger) string {
	ops := make([]string, len(t.On))
	for i, op := range t.On {
		ops[i] = string(op)
	}
	s := " (" + strings.Join(ops, ", ")
	if len(t.Fields) > 0 {
		s += "; fields " + strings.Join(t.Fields, ", ")
	}
	return s + ")"
}
