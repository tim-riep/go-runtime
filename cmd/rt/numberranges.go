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
	"strconv"
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// numberRange runs the commands on number range objects and their
// intervals.
func (c *cli) numberRange(ctx context.Context, k *kernel.Kernel, args []string) int {
	usage := func() int {
		fmt.Fprintln(c.stderr, "usage: rt number-range list [package] | show <name> | history [name] --tenant T\n"+
			"       rt number-range interval list <object> --tenant T\n"+
			"       rt number-range interval add|change <object> <interval> --tenant T --from N --to N [--current N] [--external] [--subobject V] [--year Y]\n"+
			"       rt number-range interval delete <object> <interval> --tenant T [--subobject V] [--year Y] [--yes]")
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	opts := kernel.NumberRangeOptions{By: cliUser, Confirm: c.yes}
	switch cmd, rest := args[0], args[1:]; {
	case cmd == "list" && len(rest) <= 1:
		pkg := ""
		if len(rest) == 1 {
			pkg = rest[0]
		}
		list, err := k.NumberRangeObjects(ctx, model.PackageID(pkg))
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPACKAGE\tLENGTH\tSUBOBJECT\tYEARS\tBUFFERING\tLABEL")
		for _, n := range list {
			sub := "-"
			if s, ok := n.Subobject.Get(); ok {
				sub = s.Name
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%t\t%s\t%s\n", n.Name, n.Owner, n.Length, sub, n.PerFiscalYear, buffering(n), labelText(c.lang, n.Texts))
		}
		w.Flush()
		return exitOK
	case cmd == "show" && len(rest) == 1:
		n, err := k.NumberRangeObject(ctx, rest[0])
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(n)
			return exitOK
		}
		fmt.Fprintf(c.stdout, "Number range %s of %s: %s\n", n.Name, n.Owner, labelText(c.lang, n.Texts))
		fmt.Fprintf(c.stdout, "  length         %d digits\n", n.Length)
		if s, ok := n.Subobject.Get(); ok {
			fmt.Fprintf(c.stdout, "  subobject      %s (%s)\n", s.Name, s.DataElement.Or(s.Domain.Or("")))
		}
		fmt.Fprintf(c.stdout, "  fiscal years   %t\n  buffering      %s\n  warning at     %d%%\n", n.PerFiscalYear, buffering(n), n.WarningPercent)
		return exitOK
	case cmd == "history" && len(rest) <= 1 && c.tenantID != "":
		name := ""
		if len(rest) == 1 {
			name = rest[0]
		}
		list, err := k.NumberRangeHistory(ctx, c.tenantID, name, opts)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tBY\tCHANGE\tOBJECT\tINTERVAL\tSUBOBJECT\tYEAR\tCONFIRMED")
		for _, h := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\n", h.ChangedAt.Format("2006-01-02 15:04:05"), h.ChangedBy, h.Kind, h.Object, h.Interval,
				optText(h.Subobject), optYearText(h.FiscalYear), h.Confirmed)
		}
		w.Flush()
		return exitOK
	case cmd == "interval" && len(rest) >= 2 && c.tenantID != "":
		return c.numberInterval(ctx, k, rest[0], rest[1:], opts, usage)
	}
	return usage()
}

func (c *cli) numberInterval(ctx context.Context, k *kernel.Kernel, cmd string, args []string, opts kernel.NumberRangeOptions, usage func() int) int {
	sub := model.None[string]()
	if c.set["subobject"] {
		sub = model.Some(c.subobject)
	}
	year := model.None[int64]()
	if c.set["year"] {
		y, err := strconv.ParseInt(c.year, 10, 32)
		if err != nil {
			fmt.Fprintf(c.stderr, "rt: --year %q is not a year\n", c.year)
			return exitUsage
		}
		year = model.Some(y)
	}
	switch {
	case cmd == "list" && len(args) == 1:
		list, err := k.NumberIntervals(ctx, c.tenantID, args[0], opts)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "INTERVAL\tSUBOBJECT\tYEAR\tFROM\tTO\tCURRENT\tUSED\tKIND")
		for _, iv := range list {
			kind := "internal"
			if iv.External {
				kind = "external"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d%%\t%s\n", iv.ID, optText(iv.Subobject), optYearText(iv.FiscalYear), iv.From, iv.To,
				iv.Current.Or("-"), iv.UsedPercent, kind)
		}
		w.Flush()
		return exitOK
	case (cmd == "add" || cmd == "change") && len(args) == 2:
		if c.from == "" || c.to == "" {
			return usage()
		}
		iv := kernel.NumberInterval{Object: args[0], ID: args[1], Subobject: sub, FiscalYear: year, From: c.from, To: c.to, External: c.external}
		if c.set["current"] {
			iv.Current = model.Some(c.current)
		}
		var err error
		if cmd == "add" {
			err = k.AddNumberInterval(ctx, c.tenantID, iv, opts)
		} else {
			err = k.ChangeNumberInterval(ctx, c.tenantID, iv, opts)
		}
		if err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Interval %s of %s %s in tenant %s.\n", iv.ID, iv.Object, map[string]string{"add": "added", "change": "changed"}[cmd], c.tenantID)
		return exitOK
	case cmd == "delete" && len(args) == 2:
		if err := k.DeleteNumberInterval(ctx, c.tenantID, args[0], args[1], sub, year, opts); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Interval %s of %s deleted in tenant %s.\n", args[1], args[0], c.tenantID)
		return exitOK
	}
	return usage()
}

func buffering(n *model.NumberRange) string {
	if n.Buffering == model.BufferingBuffered {
		return fmt.Sprintf("blocks of %d", n.BlockSize)
	}
	return "none"
}

func optText(o model.Optional[string]) string {
	if s, ok := o.Get(); ok {
		return strconv.Quote(s)
	}
	return "<null>"
}

func optYearText(o model.Optional[int64]) string {
	if y, ok := o.Get(); ok {
		return strconv.FormatInt(y, 10)
	}
	return "-"
}

// labelText is the label in a language, else in English.
func labelText(lang string, texts map[string]model.Label) string {
	if t, ok := texts[lang]; ok {
		return t.Label
	}
	return texts[model.DefaultLanguage].Label
}
