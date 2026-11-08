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
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
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
