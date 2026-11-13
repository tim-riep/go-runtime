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

// session runs the commands on browser sessions.
func (c *cli) session(ctx context.Context, k *kernel.Kernel, args []string) int {
	switch {
	case len(args) >= 1 && len(args) <= 2 && args[0] == "list":
		user := ""
		if len(args) == 2 {
			user = args[1]
		}
		list, err := k.WebSessions(ctx, user)
		if err != nil {
			return c.fail(err)
		}
		if c.asJSON {
			c.json(list)
			return exitOK
		}
		w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "USER\tTENANT\tLANGUAGE\tSIGNED IN\tLAST USED\tEXPIRES")
		const layout = "2006-01-02 15:04:05"
		for _, s := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", s.User, s.Tenant.Or("-"), s.Language.Or("-"), s.CreatedAt.Format(layout),
				s.LastSeenAt.Format(layout), s.ExpiresAt.Format(layout))
		}
		w.Flush()
		return exitOK
	case len(args) == 2 && args[0] == "revoke":
		if err := k.EndWebSessions(ctx, args[1]); err != nil {
			return c.fail(err)
		}
		fmt.Fprintf(c.stdout, "The browser sessions of %s ended.\n", args[1])
		return exitOK
	}
	fmt.Fprintln(c.stderr, "usage: rt session list [user] | revoke <user>")
	return exitUsage
}
