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
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/source"
	"github.com/tim-riep/go-runtime/source/github"
)

// newSources builds the package sources from the environment; tests
// replace it.
var newSources = func() (kernel.SourceConfig, error) {
	cfg := github.Config{BaseURL: os.Getenv("RT_GITHUB_URL"), Token: os.Getenv("RT_GITHUB_TOKEN")}
	if cfg.Token == "" {
		cfg.Token = os.Getenv("GITHUB_TOKEN")
	}
	if hosts := os.Getenv("RT_SOURCE_HOSTS"); hosts != "" {
		cfg.Hosts = splitList(hosts)
	}
	cache := os.Getenv("RT_CACHE_DIR")
	if cache == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			cache = filepath.Join(dir, "go-runtime")
		}
	}
	cfg.CacheDir = cache
	g, err := github.New(cfg)
	if err != nil {
		return kernel.SourceConfig{}, err
	}
	return kernel.SourceConfig{Sources: []source.Source{g}, Allow: splitList(os.Getenv("RT_SOURCE_ALLOW")), CacheDir: cache}, nil
}

func splitList(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func (c *cli) sourceRequest() kernel.SourceRequest {
	return kernel.SourceRequest{Package: c.pkg, AllowBranch: c.allowBranch, AllowRetag: c.allowRetag, AllowDependencyUpgrades: c.upgradeDeps, ExpectSHA256: c.expectSHA}
}

// applyResolution shows the plan of a resolution, asks for confirmation
// unless --yes and installs it.
func (c *cli) applyResolution(ctx context.Context, k *kernel.Kernel, res *kernel.Resolution) int {
	for _, u := range res.UpToDate() {
		fmt.Fprintf(c.stdout, "%s is up to date.\n", u)
	}
	if res.Empty() {
		return exitOK
	}
	opts := c.opts()
	opts.DryRun = true
	p, err := k.ApplyResolution(ctx, res, opts)
	if err != nil {
		return c.fail(err)
	}
	if c.dryRun {
		return c.printPlan(p)
	}
	if !c.yes {
		if c.asJSON || !terminal() {
			fmt.Fprint(c.stdout, p.Text())
			fmt.Fprintln(c.stderr, "error: the installation from a source needs confirmation; rerun with --yes")
			return exitNeedsPermit
		}
		fmt.Fprint(c.stdout, p.Text())
		fmt.Fprint(c.stdout, "\nProceed? [y/N] ")
		line, _ := readLine()
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(c.stdout, "Nothing was changed.")
			return exitNeedsPermit
		}
	}
	opts.DryRun = false
	p, err = k.ApplyResolution(ctx, res, opts)
	if err != nil {
		return c.fail(err)
	}
	return c.printPlan(p)
}

// installSources installs addresses together with directories and ZIP
// files, or the packages of a lock.
func (c *cli) installSources(ctx context.Context, k *kernel.Kernel, args []string) int {
	req := c.sourceRequest()
	for _, a := range args {
		if source.IsAddress(a, k.SourceHosts()...) {
			req.Addresses = append(req.Addresses, a)
			continue
		}
		pkg, err := c.loadPackage(a)
		if err != nil {
			return c.fail(err)
		}
		req.Local = append(req.Local, pkg)
	}
	var res *kernel.Resolution
	var err error
	if c.lockFile != "" {
		data, rerr := os.ReadFile(c.lockFile)
		if rerr != nil {
			return c.fail(rerr)
		}
		l, perr := kernel.ParseLock(data)
		if perr != nil {
			return c.fail(perr)
		}
		res, err = k.ResolveLock(ctx, l, req)
	} else {
		res, err = k.ResolveSources(ctx, req)
	}
	if err != nil {
		return c.fail(err)
	}
	return c.applyResolution(ctx, k, res)
}

// upgradeSource upgrades a package from its source: ID or ID@spec.
func (c *cli) upgradeSource(ctx context.Context, k *kernel.Kernel, arg string) int {
	id, spec, _ := strings.Cut(arg, "@")
	res, err := k.UpgradeSource(ctx, model.PackageID(id), spec, c.sourceRequest())
	if err != nil {
		return c.fail(err)
	}
	return c.applyResolution(ctx, k, res)
}

// isLocal reports whether an argument of upgrade is a directory or a ZIP
// file rather than a package ID.
func isLocal(arg string) bool {
	if strings.HasSuffix(strings.ToLower(arg), ".zip") {
		return true
	}
	fi, err := os.Stat(arg)
	return err == nil && fi.IsDir()
}

// outdated lists the packages from sources with their versions.
func (c *cli) outdated(ctx context.Context, k *kernel.Kernel) int {
	list, err := k.Outdated(ctx)
	if err != nil {
		return c.fail(err)
	}
	if c.asJSON {
		c.json(list)
		return exitOK
	}
	if len(list) == 0 {
		fmt.Fprintln(c.stdout, "No packages from sources.")
		return exitOK
	}
	w := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PACKAGE\tINSTALLED\tMATCHING\tLATEST\tSOURCE")
	for _, o := range list {
		mark := ""
		if o.Newer() {
			mark = " *"
		}
		fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\n", o.ID, mark, o.Installed, dash(o.Matching), dash(o.Latest), o.Address)
	}
	w.Flush()
	return exitOK
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// exportLock writes the origins of the installed packages.
func (c *cli) exportLock(ctx context.Context, k *kernel.Kernel) int {
	l, err := k.ExportLock(ctx)
	if err != nil {
		return c.fail(err)
	}
	b, _ := json.MarshalIndent(l, "", "  ")
	b = append(b, '\n')
	if c.output == "" || c.output == "-" {
		_, _ = c.stdout.Write(b)
		return exitOK
	}
	if err := os.WriteFile(c.output, b, 0o644); err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "Lock of %d packages written to %s.\n", len(l.Packages), c.output)
	return exitOK
}

// showOrigin prints where a package comes from.
func (c *cli) showOrigin(ctx context.Context, k *kernel.Kernel, id model.PackageID) {
	o, err := k.PackageOrigin(ctx, id)
	if err != nil {
		return
	}
	if o.Kind == "local" {
		fmt.Fprintf(c.stdout, "  origin: local (directory or ZIP file), installed %s by %s\n", o.InstalledAt.Format("2006-01-02"), o.InstalledBy)
		return
	}
	fmt.Fprintf(c.stdout, "  origin: %s, %s", o.Address.Or(""), o.Kind)
	if t, ok := o.Tag.Get(); ok {
		fmt.Fprintf(c.stdout, ", tag %s", t)
	}
	if a, ok := o.Asset.Get(); ok {
		fmt.Fprintf(c.stdout, ", asset %s", a)
	}
	fmt.Fprintf(c.stdout, "\n          commit %s, sha256 %s\n          requested %s, upgrades within %s, installed %s by %s\n",
		o.Commit.Or(""), o.SHA256.Or(""), o.Requested.Or("latest"), o.Constraint.Or("-"), o.InstalledAt.Format("2006-01-02"), o.InstalledBy)
}
