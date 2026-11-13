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

package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/testutil/fakegithub"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/source"
	"github.com/tim-riep/go-runtime/source/github"
)

// forge publishes small packages in repositories of a fake GitHub.
type forge struct {
	t   *testing.T
	srv *fakegithub.Server
}

// dep is a requirement of a published package: ID, constraint and the
// repository of its source ("" for none).
type dep struct{ id, version, repo string }

// manifest returns the manifest of a package without objects.
func (f *forge) manifest(id, version string, deps ...dep) []byte {
	var reqs []string
	for _, d := range deps {
		r := fmt.Sprintf(`{"id":%q,"version":%q`, d.id, d.version)
		if d.repo != "" {
			r += fmt.Sprintf(`,"source":%q`, f.srv.Host()+"/"+d.repo)
		}
		reqs = append(reqs, r+"}")
	}
	return []byte(fmt.Sprintf(`{"id":%q,"version":%q,"requires":[%s]}`, id, version, strings.Join(reqs, ",")))
}

// publish tags a version of a package in a repository, as release asset
// or as source archive in the directory dir of the repository.
func (f *forge) publish(repo, tag string, asset bool, dir string, manifest []byte) string {
	f.t.Helper()
	files := t2zip(f.t, map[string][]byte{"manifest.json": manifest}, "")
	r := f.srv.Repo(repo)
	prefix := strings.ReplaceAll(repo, "/", "-") + "-" + tag + "/"
	if dir != "" {
		prefix += dir + "/"
	}
	archive := t2zip(f.t, map[string][]byte{prefix + "manifest.json": manifest, strings.ReplaceAll(repo, "/", "-") + "-" + tag + "/README.md": []byte("readme")}, "")
	c := r.Commit(archive)
	r.Tags[tag] = c
	if asset {
		var m struct{ ID, Version string }
		_ = json.Unmarshal(manifest, &m)
		r.Releases[tag] = append(r.Releases[tag], fakegithub.Asset{Name: m.ID + "-" + m.Version + ".zip", Data: files})
	}
	return c
}

func t2zip(t *testing.T, files map[string][]byte, prefix string) []byte {
	t.Helper()
	dir := t.TempDir()
	for p, b := range files {
		testutil.WriteFile(t, dir, p, b)
	}
	return testutil.ZipDir(t, dir, prefix)
}

func newForge(t *testing.T) *forge { return &forge{t: t, srv: fakegithub.New(t)} }

// connect gives a kernel the fake as source with a cache.
func (f *forge) connect(k *kernel.Kernel, cache string, allow ...string) {
	f.t.Helper()
	g, err := github.New(f.srv.Config())
	if err != nil {
		f.t.Fatal(err)
	}
	k.SetSources(kernel.SourceConfig{Sources: []source.Source{g}, CacheDir: cache, Allow: allow})
}

func (f *forge) addr(s string) string { return f.srv.Host() + "/" + s }

func installSources(t *testing.T, k *kernel.Kernel, req kernel.SourceRequest, opts kernel.Options) error {
	t.Helper()
	_, err := k.InstallSources(context.Background(), req, opts)
	return err
}

func versionOf(t *testing.T, k *kernel.Kernel, id string) string {
	t.Helper()
	s, err := k.Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Packages[model.PackageID(id)]; p != nil {
		return p.Manifest.Version
	}
	return ""
}

// TestSourceChain installs a chain of three packages from three
// repositories, leaves a matching installed dependency alone and records
// the origins.
func TestSourceChain(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		f := newForge(t)
		f.connect(k, t.TempDir())
		f.publish("tim-riep/tra", "v1.0.0", true, "", f.manifest("TRA", "1.0.0"))
		f.publish("tim-riep/tra", "v1.1.0", false, "", f.manifest("TRA", "1.1.0"))
		f.publish("tim-riep/trb", "v1.0.0", false, "packages/TRB", f.manifest("TRB", "1.0.0", dep{"TRA", "^1.0", "tim-riep/tra"}))
		f.publish("tim-riep/trc", "v1.0.0", true, "", f.manifest("TRC", "1.0.0", dep{"TRB", "^1.0", "tim-riep/trb/packages/TRB"}))

		res, err := k.ResolveSources(ctx, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trc@^1")}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := k.ApplyResolution(ctx, res, kernel.Options{DryRun: true})
		if err != nil || len(p.Packages) != 3 || p.Packages[0].ID != "TRA" || p.Packages[0].To.Or("") != "1.1.0" || p.Packages[0].Source.Dependency != "TRB" ||
			p.Packages[2].Source.Asset != "TRC-1.0.0.zip" || p.Packages[1].Source.Kind != "archive" || !strings.Contains(p.Text(), "Sources:") {
			t.Fatalf("plan: %v\n%s", err, p.Text())
		}
		if versionOf(t, k, "TRA") != "" {
			t.Fatal("the dry run installed")
		}
		if _, err := k.ApplyResolution(ctx, res, kernel.Options{By: "cli"}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
		o, err := k.PackageOrigin(ctx, "TRB")
		if err != nil || o.Kind != "archive" || o.Address.Or("") != f.addr("tim-riep/trb/packages/TRB") || o.Constraint.Or("") != "^1.0.0" || o.InstalledBy != "cli" || o.Tag.Or("") != "v1.0.0" {
			t.Errorf("origin: %+v %v", o, err)
		}
		if o, _ := k.PackageOrigin(ctx, "TRC"); o.Requested.Or("") != "^1" || o.Constraint.Or("") != "^1" || o.Kind != "asset" {
			t.Errorf("origin of TRC: %+v", o)
		}
		s, _ := k.Schema(ctx)
		if d := s.Packages["TRB"].Manifest.Requires[0]; d.Source.Or("") != f.addr("tim-riep/tra") {
			t.Errorf("stored source: %+v", d)
		}

		// A package whose dependency is installed and matches leaves it.
		f.publish("tim-riep/trd", "v1.0.0", false, "", f.manifest("TRD", "1.0.0", dep{"TRA", "^1.0", "tim-riep/tra"}))
		n := len(f.srv.Requests)
		res, err = k.ResolveSources(ctx, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trd")}})
		if err != nil || res.Empty() {
			t.Fatal(err)
		}
		p, err = k.ApplyResolution(ctx, res, kernel.Options{})
		if err != nil || len(p.Packages) != 1 {
			t.Fatalf("TRD: %v %+v", err, p)
		}
		for _, q := range f.srv.Requests[n:] {
			if strings.Contains(q.Path, "/tra/") {
				t.Errorf("TRA fetched again: %s", q.Path)
			}
		}
		// The origins go with the package.
		if _, err := k.Uninstall(ctx, "TRD", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if o, _ := k.PackageOrigin(ctx, "TRA"); o.Kind != "asset" && o.Kind != "archive" {
			t.Errorf("TRA after uninstall of TRD: %+v", o)
		}
		var ni *rterr.NotInstalled
		if _, err := k.PackageOrigin(ctx, "TRD"); !errors.As(err, &ni) {
			t.Errorf("origin of TRD: %v", err)
		}
	})
}

// TestSourceDependencies proposes upgrades of installed dependencies and
// rejects conflicts, cycles, missing sources, sources of kernel packages,
// owners outside the allowed list and versions that differ from the tag.
func TestSourceDependencies(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		f := newForge(t)
		f.connect(k, t.TempDir())
		f.publish("tim-riep/tra", "v1.0.0", false, "", f.manifest("TRA", "1.0.0"))
		if err := installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/tra@v1.0.0")}}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		f.publish("tim-riep/tra", "v1.2.0", false, "", f.manifest("TRA", "1.2.0"))
		f.publish("tim-riep/trb", "v2.0.0", false, "", f.manifest("TRB", "2.0.0", dep{"TRA", "^1.2", "tim-riep/tra"}))
		req := kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trb")}}
		res, err := k.ResolveSources(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		p, err := k.ApplyResolution(ctx, res, kernel.Options{DryRun: true})
		if err != nil || p.Packages[0].ID != "TRA" || p.Packages[0].From.Or("") != "1.0.0" || p.Packages[0].To.Or("") != "1.2.0" {
			t.Fatalf("proposed upgrade: %v %+v", err, p)
		}
		var cr *rterr.ConfirmationRequired
		if _, err := k.ApplyResolution(ctx, res, kernel.Options{}); !errors.As(err, &cr) || !strings.Contains(err.Error(), "TRA 1.0.0 -> 1.2.0") {
			t.Fatalf("without permission: %v", err)
		}
		req.AllowDependencyUpgrades = true
		if err := installSources(t, k, req, kernel.Options{}); err != nil || versionOf(t, k, "TRA") != "1.2.0" || versionOf(t, k, "TRB") != "2.0.0" {
			t.Fatalf("with permission: %v", err)
		}
		consistent(t, k)

		expect := func(name string, req kernel.SourceRequest, want string) {
			t.Helper()
			err := installSources(t, k, req, kernel.Options{DryRun: true})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %v", name, err)
			}
		}
		// Two packages want different major versions.
		f.publish("tim-riep/trx", "v1.0.0", false, "", f.manifest("TRX", "1.0.0"))
		f.publish("tim-riep/trx", "v2.0.0", false, "", f.manifest("TRX", "2.0.0"))
		f.publish("tim-riep/tr1", "v1.0.0", false, "", f.manifest("TRONE", "1.0.0", dep{"TRX", "^1.0", "tim-riep/trx"}))
		f.publish("tim-riep/tr2", "v1.0.0", false, "", f.manifest("TRTWO", "1.0.0", dep{"TRX", "^2.0", "tim-riep/trx"}))
		expect("conflict", kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/tr1"), f.addr("tim-riep/tr2")}}, "^1.0 from TRONE and ^2.0 from TRTWO")
		// A cycle over two repositories.
		f.publish("tim-riep/cy1", "v1.0.0", false, "", f.manifest("TRCYA", "1.0.0", dep{"TRCYB", "^1", "tim-riep/cy2"}))
		f.publish("tim-riep/cy2", "v1.0.0", false, "", f.manifest("TRCYB", "1.0.0", dep{"TRCYA", "^1", "tim-riep/cy1"}))
		expect("cycle", kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/cy1")}}, "cycle")
		// A dependency without source stays missing.
		f.publish("tim-riep/nos", "v1.0.0", false, "", f.manifest("TRNOS", "1.0.0", dep{"TRMISSING", "^1", ""}))
		expect("missing", kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/nos")}}, "TRMISSING")
		// Kernel packages never come from a source.
		f.publish("tim-riep/kp", "v1.0.0", false, "", f.manifest("TRKP", "1.0.0", dep{"TRBASE", "^1", "tim-riep/trbase"}))
		expect("kernel package", kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/kp")}}, "kernel packages come only with the kernel")
		// The manifest must have the version of the tag.
		f.publish("tim-riep/bad", "v1.0.1", false, "", f.manifest("TRBAD", "1.0.0"))
		expect("version", kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/bad")}}, "with version 1.0.0 in its manifest")
		// Owners outside the list are rejected, also as dependency.
		f.publish("acme/tre", "v1.0.0", false, "", f.manifest("TRE", "1.0.0", dep{"TRX", "^1", "tim-riep/trx"}))
		f.connect(k, t.TempDir(), "acme")
		expect("allowed", kernel.SourceRequest{Addresses: []string{f.addr("acme/tre")}}, "tim-riep/trx is not among the allowed sources")
		expect("allowed root", kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trx")}}, "not among the allowed sources")
	})
}

// TestSourceIntegrity checks expected hashes, rejects moved tags unless
// allowed, exports a lock and installs it into a second database from the
// cache without network.
func TestSourceIntegrity(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ctx := context.Background()
			k, err := kernel.Open(ctx, db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			initialized(t, k)
			f := newForge(t)
			cache := t.TempDir()
			f.connect(k, cache)
			f.publish("tim-riep/tra", "v1.0.0", true, "", f.manifest("TRA", "1.0.0"))
			f.publish("tim-riep/trb", "v1.0.0", false, "", f.manifest("TRB", "1.0.0", dep{"TRA", "^1", "tim-riep/tra"}))
			sha := source.SHA256(f.srv.Repo("tim-riep/trb").Archives[f.srv.Repo("tim-riep/trb").Tags["v1.0.0"]])
			var ie *rterr.IntegrityError
			err = installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trb")}, ExpectSHA256: strings.Repeat("0", 64)}, kernel.Options{})
			if !errors.As(err, &ie) {
				t.Fatalf("wrong hash: %v", err)
			}
			if err := installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trb")}, ExpectSHA256: sha}, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			lock, err := k.ExportLock(ctx)
			if err != nil || len(lock.Packages) != 2 || lock.Packages[0].ID != "TRA" || lock.Packages[0].Kind != "asset" || lock.Packages[1].SHA256 != sha {
				t.Fatalf("lock: %+v %v", lock, err)
			}
			data, _ := json.Marshal(lock)

			// The tag v1.0.0 of TRB moves to another commit.
			if _, err := k.Uninstall(ctx, "TRB", kernel.Options{Confirm: true}); err != nil {
				t.Fatal(err)
			}
			f.publish("tim-riep/trb", "v1.0.0", false, "", []byte(strings.Replace(string(f.manifest("TRB", "1.0.0", dep{"TRA", "^1", "tim-riep/tra"})), `"requires"`, `"description":"moved","requires"`, 1)))
			err = installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trb@v1.0.0")}}, kernel.Options{})
			if !errors.As(err, &ie) || !strings.Contains(err.Error(), "the tag was moved") {
				t.Fatalf("moved tag: %v", err)
			}
			if err := installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trb@v1.0.0")}, AllowRetag: true}, kernel.Options{}); err != nil {
				t.Fatalf("with --allow-retag: %v", err)
			}

			// The lock installs the first commits into another database from
			// the cache, with the server down.
			k2, err := kernel.Open(ctx, testutil.Another(t, db).DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer k2.Close()
			initialized(t, k2)
			f.connect(k2, cache)
			f.srv.Close()
			l, err := kernel.ParseLock(data)
			if err != nil {
				t.Fatal(err)
			}
			res, err := k2.ResolveLock(ctx, l, kernel.SourceRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := k2.ApplyResolution(ctx, res, kernel.Options{}); err != nil {
				t.Fatal(err)
			}
			if o, err := k2.PackageOrigin(ctx, "TRB"); err != nil || o.SHA256.Or("") != sha || o.Kind != "archive" {
				t.Errorf("origin from lock: %+v %v", o, err)
			}
			if o, _ := k2.PackageOrigin(ctx, "TRA"); o.Asset.Or("") != "TRA-1.0.0.zip" {
				t.Errorf("asset from lock: %+v", o)
			}
			consistent(t, k2)
		})
	}
}

// TestSourceUpgrade lists outdated packages and upgrades within the
// constraint of the installation, or to an explicit version.
func TestSourceUpgrade(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		f := newForge(t)
		f.connect(k, t.TempDir())
		f.publish("tim-riep/tra", "v1.0.0", false, "", f.manifest("TRA", "1.0.0"))
		f.publish("tim-riep/trb", "v1.0.0", false, "", f.manifest("TRB", "1.0.0", dep{"TRA", "<1.5", "tim-riep/tra"}))
		if err := installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/trb")}}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		f.publish("tim-riep/tra", "v1.4.0", false, "", f.manifest("TRA", "1.4.0"))
		f.publish("tim-riep/tra", "v1.6.0", false, "", f.manifest("TRA", "1.6.0"))
		f.publish("tim-riep/tra", "v2.0.0", false, "", f.manifest("TRA", "2.0.0"))
		list, err := k.Outdated(ctx)
		if err != nil || len(list) != 2 || list[0].ID != "TRA" || list[0].Matching != "1.4.0" || list[0].Latest != "2.0.0" || !list[0].Newer() || list[1].Newer() {
			t.Fatalf("outdated: %+v %v", list, err)
		}
		res, err := k.UpgradeSource(ctx, "TRA", "", kernel.SourceRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := k.ApplyResolution(ctx, res, kernel.Options{}); err != nil || versionOf(t, k, "TRA") != "1.4.0" {
			t.Fatalf("upgrade: %v", err)
		}
		if res, err := k.UpgradeSource(ctx, "TRA", "", kernel.SourceRequest{}); err != nil || !res.Empty() || len(res.UpToDate()) != 1 {
			t.Errorf("up to date: %+v %v", res, err)
		}
		if _, err := k.UpgradeSource(ctx, "TRA", "v2.0.0", kernel.SourceRequest{}); err == nil || !strings.Contains(err.Error(), "<1.5") {
			t.Errorf("explicit version beyond a dependent: %v", err)
		}
		if _, err := k.UpgradeSource(ctx, "TRBASE", "", kernel.SourceRequest{}); err == nil {
			t.Error("kernel package upgraded")
		}
		// Uninstalling keeps nothing of the origins.
		if _, err := k.Uninstall(ctx, "TRB", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Uninstall(ctx, "TRA", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if list, _ := k.Outdated(ctx); len(list) != 0 {
			t.Errorf("after uninstall: %+v", list)
		}
	})
}

// TestSourcePrefixedTags installs packages from directories of one
// repository that releases each with tags of its own prefix.
func TestSourcePrefixedTags(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		f := newForge(t)
		f.connect(k, t.TempDir())
		f.publish("tim-riep/mono", "v9.0.0", false, "packages/TRP", f.manifest("TRP", "9.0.0"))
		f.publish("tim-riep/mono", "trp-v1.0.0", false, "packages/TRP", f.manifest("TRP", "1.0.0"))
		f.publish("tim-riep/mono", "trp-v1.1.0", true, "packages/TRP", f.manifest("TRP", "1.1.0"))
		f.publish("tim-riep/mono", "trq-v2.0.0", false, "packages/TRQ", f.manifest("TRQ", "2.0.0"))

		plan := func(addr string) *plan.Plan {
			t.Helper()
			res, err := k.ResolveSources(ctx, kernel.SourceRequest{Addresses: []string{f.addr(addr)}})
			if err != nil {
				t.Fatalf("%s: %v", addr, err)
			}
			p, err := k.ApplyResolution(ctx, res, kernel.Options{DryRun: true})
			if err != nil {
				t.Fatalf("%s: %v", addr, err)
			}
			return p
		}
		if p := plan("tim-riep/mono/packages/TRP"); p.Packages[0].To.Or("") != "1.1.0" || p.Packages[0].Source.Asset != "TRP-1.1.0.zip" {
			t.Errorf("latest: %s", p.Text())
		}
		if p := plan("tim-riep/mono/packages/TRP@trp-v1.0.0"); p.Packages[0].To.Or("") != "1.0.0" {
			t.Errorf("by tag: %s", p.Text())
		}
		if p := plan("tim-riep/mono/packages/TRP@1.0.0"); p.Packages[0].To.Or("") != "1.0.0" {
			t.Errorf("by version: %s", p.Text())
		}
		if err := installSources(t, k, kernel.SourceRequest{Addresses: []string{f.addr("tim-riep/mono/packages/TRQ@^2")}}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if o, err := k.PackageOrigin(ctx, "TRQ"); err != nil || o.Tag.Or("") != "trq-v2.0.0" {
			t.Errorf("origin: %+v %v", o, err)
		}
	})
}
