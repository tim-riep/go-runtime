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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/testutil/fakegithub"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/source"
	"github.com/tim-riep/go-runtime/source/github"
)

// publishExample tags an example package in a repository of the fake,
// with its version replaced and its manifest patched.
func publishExample(t *testing.T, srv *fakegithub.Server, name, repo, tag, version string, asset bool, patch func(string) string) {
	t.Helper()
	dir := testutil.CopyExample(t, name)
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := strings.Replace(string(b), `"version": "1.0.0"`, `"version": "`+version+`"`, 1)
	if patch != nil {
		m = patch(m)
	}
	testutil.WriteFile(t, dir, "manifest.json", []byte(m))
	testutil.WriteFile(t, dir, "README.md", []byte("readme"))
	r := srv.Repo(repo)
	r.Tags[tag] = r.Commit(testutil.ZipDir(t, dir, strings.ReplaceAll(repo, "/", "-")+"-x/"))
	if asset {
		pkgOnly := testutil.CopyExample(t, name)
		testutil.WriteFile(t, pkgOnly, "manifest.json", []byte(m))
		r.Releases[tag] = []fakegithub.Asset{{Name: name + "-" + version + ".zip", Data: testutil.ZipDir(t, pkgOnly, "")}}
	}
}

// TestSourceCommands installs TROTTO from one repository with TRFIN from
// another as its dependency, lists outdated packages, upgrades TRFIN,
// installs the lock into a second database and uninstalls.
func TestSourceCommands(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			srv := fakegithub.New(t)
			cache := t.TempDir()
			old := newSources
			newSources = func() (kernel.SourceConfig, error) {
				g, err := github.New(srv.Config())
				return kernel.SourceConfig{Sources: []source.Source{g}, CacheDir: cache}, err
			}
			t.Cleanup(func() { newSources = old })
			input = strings.NewReader("")
			h := srv.Host()
			publishExample(t, srv, "TRFIN", "tim-riep/trfin", "v1.0.0", "1.0.0", true, nil)
			publishExample(t, srv, "TROTTO", "tim-riep/trotto", "v1.0.0", "1.0.0", false, func(m string) string {
				return strings.Replace(m, `"id": "TRFIN",
      "version": ">=1.0.0"`, `"id": "TRFIN",
      "version": "^1.0",
      "source": "`+h+`/tim-riep/trfin"`, 1)
			})
			lock := filepath.Join(t.TempDir(), "rt.lock")
			db2 := testutil.Another(t, db).DSN
			for _, s := range []struct {
				dsn      string
				args     []string
				code     int
				contains string
			}{
				{db.DSN, []string{"init"}, exitOK, ""},
				{db.DSN, []string{"language", "add", "de"}, exitOK, ""},
				{db.DSN, []string{"install", h + "/tim-riep/trotto", "--dry-run"}, exitOK, "TRFIN 1.0.0 from " + h + "/tim-riep/trfin, tag v1.0.0"},
				{db.DSN, []string{"install", h + "/tim-riep/trotto"}, exitNeedsPermit, "rerun with --yes"},
				{db.DSN, []string{"install", h + "/tim-riep/trotto", "--yes"}, exitOK, "Executed"},
				{db.DSN, []string{"show", "TROTTO"}, exitOK, "origin: " + h + "/tim-riep/trotto, archive, tag v1.0.0"},
				{db.DSN, []string{"show", "TROTTO"}, exitOK, `requires TRFIN "^1.0" from ` + h + "/tim-riep/trfin"},
				{db.DSN, []string{"show", "TRFIN"}, exitOK, "asset TRFIN-1.0.0.zip"},
				{db.DSN, []string{"outdated"}, exitOK, "TRFIN     1.0.0      1.0.0     1.0.0"},
			} {
				var out, errOut bytes.Buffer
				code := run(context.Background(), append([]string{"--db", s.dsn}, s.args...), &out, &errOut)
				all := out.String() + errOut.String()
				if code != s.code || !strings.Contains(norm(all), norm(s.contains)) {
					t.Fatalf("rt %s: exit %d (want %d), output:\n%s", strings.Join(s.args, " "), code, s.code, all)
				}
			}
			publishExample(t, srv, "TRFIN", "tim-riep/trfin", "v1.1.0", "1.1.0", false, nil)
			publishExample(t, srv, "TRFIN", "tim-riep/trfin", "v2.0.0", "2.0.0", false, nil)
			for _, s := range []struct {
				dsn      string
				args     []string
				code     int
				contains string
			}{
				{db.DSN, []string{"outdated"}, exitOK, "TRFIN *   1.0.0      1.1.0     2.0.0"},
				{db.DSN, []string{"upgrade", "TRFIN", "--yes"}, exitOK, "upgrade"},
				{db.DSN, []string{"show", "TRFIN"}, exitOK, "archive, tag v1.1.0"},
				{db.DSN, []string{"upgrade", "TRFIN", "--yes"}, exitOK, "TRFIN 1.1.0 is up to date."},
				{db.DSN, []string{"upgrade", "TRFIN@v2.0.0", "--yes"}, exitError, "matching ^1.0 and =2.0.0"},
				{db.DSN, []string{"export", "lock", "-o", lock}, exitOK, "Lock of 2 packages"},
				{db2, []string{"init"}, exitOK, ""},
				{db2, []string{"language", "add", "de"}, exitOK, ""},
				{db2, []string{"install", "--lock", lock, "--yes"}, exitOK, "Executed"},
				{db2, []string{"show", "TRFIN"}, exitOK, "tag v1.1.0"},
				{db.DSN, []string{"uninstall", "TROTTO", "--yes"}, exitOK, "Executed"},
				{db.DSN, []string{"uninstall", "TRFIN", "--yes"}, exitOK, "Executed"},
				{db.DSN, []string{"outdated"}, exitOK, "No packages from sources."},
			} {
				var out, errOut bytes.Buffer
				code := run(context.Background(), append([]string{"--db", s.dsn}, s.args...), &out, &errOut)
				all := out.String() + errOut.String()
				if code != s.code || !strings.Contains(norm(all), norm(s.contains)) {
					t.Fatalf("rt %s: exit %d (want %d), output:\n%s", strings.Join(s.args, " "), code, s.code, all)
				}
			}
		})
	}
}

// norm collapses runs of spaces, so that column widths do not matter.
func norm(s string) string { return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' }), " ") }
