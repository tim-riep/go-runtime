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
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// valuesText shows the values of a check in the order of the fields: a
// value in quotes, any, or the column of a filter.
func valuesText(values map[string]json.RawMessage) string {
	var parts []string
	for _, f := range slices.Sorted(maps.Keys(values)) {
		raw := values[f]
		var col struct {
			Column string `json:"column"`
		}
		switch {
		case string(raw) == "null":
			parts = append(parts, f+"=any")
		case json.Unmarshal(raw, &col) == nil && col.Column != "":
			parts = append(parts, f+"=column "+col.Column)
		default:
			var s string
			_ = json.Unmarshal(raw, &s)
			parts = append(parts, fmt.Sprintf("%s=%q", f, s))
		}
	}
	return strings.Join(parts, " ")
}

// siteText shows where a check was made.
func siteText(s repo.CheckSite) string {
	if k, ok := s.Kernel.Get(); ok {
		return "kernel: " + k
	}
	out := s.Class.Or("?") + "." + s.Method.Or("?")
	if p, ok := s.Package.Get(); ok {
		out += " (package " + p
		if l, ok := s.Line.Get(); ok {
			out += fmt.Sprintf(", line %d", l)
		}
		out += ")"
	}
	return out
}

// lastFailedCheck shows the last failed check of a user in a tenant.
func (c *cli) lastFailedCheck(ctx context.Context, k *kernel.Kernel, user string) int {
	if c.tenantID == "" {
		fmt.Fprintln(c.stderr, "usage: rt user last-failed-check <user> --tenant T")
		return exitUsage
	}
	f, err := k.LastFailedCheck(ctx, user, c.tenantID)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(f)
		return exitOK
	}
	fmt.Fprintf(c.stdout, "Last failed check of %s in tenant %s at %s\n", f.User, f.Tenant, f.FailedAt.Format(time.RFC3339))
	fmt.Fprintf(c.stdout, "  object: %s\n  values: %s\n  place:  %s\n", f.Object, valuesText(f.Values), siteText(f.Site))
	var grants []struct {
		Fields map[string][]model.ValueEntry `json:"fields"`
		Origin authorization.Origin          `json:"origin"`
	}
	_ = json.Unmarshal(f.Grants, &grants)
	if len(grants) == 0 {
		fmt.Fprintln(c.stdout, "  grants on the object: none")
		return exitOK
	}
	fmt.Fprintln(c.stdout, "  grants on the object:")
	for _, g := range grants {
		fmt.Fprintf(c.stdout, "    from %s (assignment %s)\n", g.Origin.Role, g.Origin.Assignment)
		for _, name := range slices.Sorted(maps.Keys(g.Fields)) {
			fmt.Fprintf(c.stdout, "      %-12s %s\n", name, entriesText(g.Fields[name]))
		}
	}
	return exitOK
}

// trace starts, stops and shows authorization traces.
func (c *cli) trace(ctx context.Context, k *kernel.Kernel, args []string) int {
	if len(args) == 3 && args[0] == "extension-points" {
		return c.selectionTrace(ctx, k, args[1], args[2])
	}
	if len(args) != 3 || args[0] != "authorizations" {
		fmt.Fprintln(c.stderr, "usage: rt trace authorizations|extension-points start|stop|show <user> [--tenant T] [--minutes 30] [--max 10000]")
		return exitUsage
	}
	user := args[2]
	switch args[1] {
	case "start":
		opts := kernel.TraceOptions{Duration: time.Duration(c.minutes) * time.Minute, MaxEntries: c.maxEntries, By: "cli"}
		if c.tenantID != "" {
			opts.Tenant = model.Some(c.tenantID)
		}
		t, err := k.StartTrace(ctx, user, opts)
		if err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Tracing the authorization checks of %s in %s until %s, at most %d.\n", t.User, t.Tenant.Or("all tenants"),
			t.Until.Format(time.RFC3339), t.MaxEntries)
		return exitOK
	case "stop":
		if err := k.StopTrace(ctx, user); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "Trace of %s stopped.\n", user)
		return exitOK
	case "show":
		r, err := k.Trace(ctx, user, c.tenantID)
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
			result := "allowed"
			if a, ok := e.Allowed.Get(); ok && !a {
				result = "denied"
			}
			if e.Mode == "filter" {
				result = "filter"
				if n, ok := e.Rows.Get(); ok {
					result += fmt.Sprintf(", %d rows", n)
				}
			}
			fmt.Fprintf(c.stdout, "  %s  %-8s %s  %s  %s\n    %s\n", e.At.Format(time.RFC3339), e.Tenant.Or("-"), e.Mode, e.Object, result, siteText(e.Site))
			fmt.Fprintf(c.stdout, "    %s\n", valuesText(e.Values))
			if cond, ok := e.Condition.Get(); ok {
				fmt.Fprintf(c.stdout, "    %s\n", cond)
			}
		}
		return exitOK
	}
	fmt.Fprintln(c.stderr, "usage: rt trace authorizations start|stop|show <user>")
	return exitUsage
}
