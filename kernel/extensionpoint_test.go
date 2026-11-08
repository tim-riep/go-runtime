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
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// pointFiles are the files of a package TRPOINT with the interface
// /TRPOINT/CHECKS and the extension point /TRPOINT/CHECKS, whose JSON
// after the name and the interface is given.
func pointFiles(version, point string) map[string]string {
	return map[string]string{
		"manifest.json":        `{ "id": "TRPOINT", "version": "` + version + `", "requires": [{ "id": "TRBASE" }], "languageVersion": 1 }`,
		"src/CHECKS.interface": "public interface /TRPOINT/CHECKS\n\n  method CHECK(code: STRING) returns STRING\n\nend\n",
		"src/DEFAULT_CHECKS.class": "public open class /TRPOINT/DEFAULT_CHECKS implements /TRPOINT/CHECKS\n\n" +
			"  public open method CHECK(code: STRING) returns STRING\n    return code\n  end\n\nend\n",
		"schema/extension-points/CHECKS.json": `{ "name": "/TRPOINT/CHECKS", "description": { "en": "Checks", "de": "Prüfungen" },
			"interface": "/TRPOINT/CHECKS", ` + point + ` }`,
	}
}

// implFiles are the files of a package that requires TRPOINT and brings
// implementations of /TRPOINT/CHECKS: each one maps a name to the JSON of
// its implementation after the class, whose class /<ID>/<NAME>_CHECKS
// returns the name.
func implFiles(id, version string, impls map[string]string) map[string]string {
	files := map[string]string{
		"manifest.json": `{ "id": "` + id + `", "version": "` + version + `", "requires": [{ "id": "TRBASE" }, { "id": "TRPOINT" }], "languageVersion": 1 }`,
	}
	for name, rest := range impls {
		class := "/" + id + "/" + name + "_CHECKS"
		files["src/"+name+"_CHECKS.class"] = "public class " + class + " implements /TRPOINT/CHECKS\n\n" +
			"  public method CHECK(code: STRING) returns STRING\n    return \"" + name + "\"\n  end\n\nend\n"
		files["schema/extension-implementations/"+name+".json"] = `{ "name": "/` + id + `/` + name + `", "description": { "en": "` + name + `" },
			"extensionPoint": "/TRPOINT/CHECKS", "class": "` + class + `"` + rest + ` }`
	}
	return files
}

func writePackage(t *testing.T, files map[string]string) *model.Package {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		testutil.WriteFile(t, dir, name, []byte(text))
	}
	return testutil.LoadDir(t, dir)
}

func entryText(e model.ValueEntry) string { return string(e.Kind) + " " + e.Low + " " + e.High }

const countryFilter = `"use": "single", "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }]`

// TestExtensionPointRepository installs an extension point and
// implementations and reads them back from the repository.
func TestExtensionPointRepository(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		point := writePackage(t, pointFiles("1.0.0", `"use": "multiple", "fallback": "/TRPOINT/DEFAULT_CHECKS", "commitAllowed": true,
			"filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" },
			            { "name": "CLASS", "domain": "/TRBASE/COUNTER", "default": [{ "from": 1, "to": 5 }, { "pattern": "9*" }] }]`))
		impl := writePackage(t, implFiles("TRIMPL", "1.0.0", map[string]string{
			"A": `, "filters": { "COUNTRY": [{ "value": "DE" }, { "from": "AT", "to": "BE" }, { "pattern": "F*" }] }, "order": -5`,
			"B": `, "filters": { "COUNTRY": [{ "value": "" }], "CLASS": [{ "value": 7 }] }`,
		}))
		p, err := k.Install(ctx, []*model.Package{point, impl}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		text := planText(p)
		for _, want := range []string{"create-extension-point /TRPOINT/CHECKS (multiple, 2 filter fields)",
			"create-implementation /TRIMPL/A (of /TRPOINT/CHECKS by /TRIMPL/A_CHECKS)"} {
			if !strings.Contains(text, want) {
				t.Errorf("plan without %q:\n%s", want, text)
			}
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ep := s.ExtensionPoints["/TRPOINT/CHECKS"]
		if ep == nil || ep.Owner != "TRPOINT" || ep.Use != model.UseMultiple || ep.Fallback != model.Some("/TRPOINT/DEFAULT_CHECKS") || !ep.CommitAllowed ||
			ep.Description["de"] != "Prüfungen" || len(ep.Filters) != 2 || ep.Filters[0].Default.Valid() || ep.Filters[1].Domain != model.Some("/TRBASE/COUNTER") {
			t.Fatalf("extension point read back: %+v", ep)
		}
		if def, _ := ep.Filters[1].Default.Get(); len(def) != 2 || entryText(def[0]) != "range 1 5" || entryText(def[1]) != "pattern 9* " {
			t.Fatalf("default read back: %+v", def)
		}
		a, b := s.Implementations["/TRIMPL/A"], s.Implementations["/TRIMPL/B"]
		if a == nil || b == nil || a.Owner != "TRIMPL" || a.Class != "/TRIMPL/A_CHECKS" || a.Order != -5 || len(a.Filters["COUNTRY"]) != 3 ||
			entryText(a.Filters["COUNTRY"][1]) != "range AT BE" || b.Filters["COUNTRY"][0].Low != "" {
			t.Fatalf("implementations read back: %+v %+v", a, b)
		}
		if got := s.ImplementationsOf("/TRPOINT/CHECKS"); len(got) != 2 || got[0].Name != "/TRIMPL/A" {
			t.Fatalf("order: %v", got)
		}
		if v := s.ImplementationValues(a)["CLASS"]; len(v) != 2 {
			t.Fatalf("default for /TRIMPL/A: %v", v)
		}
		// Upgrading without changes keeps everything and shows nothing.
		if p, err := k.Upgrade(ctx, writePackage(t, implFiles("TRIMPL", "1.0.1", map[string]string{
			"A": `, "filters": { "COUNTRY": [{ "value": "DE" }, { "from": "AT", "to": "BE" }, { "pattern": "F*" }] }, "order": -5`,
			"B": `, "filters": { "COUNTRY": [{ "value": "" }], "CLASS": [{ "value": 7 }] }`,
		})), kernel.Options{}); err != nil || strings.Contains(planText(p), "implementation") {
			t.Fatalf("unchanged upgrade: %v\n%s", err, planText(p))
		}
		if _, err := k.Uninstall(ctx, "TRIMPL", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if s, _ := k.Schema(ctx); len(s.Implementations) != 0 {
			t.Fatal("implementations remain after uninstalling")
		}
		consistent(t, k)
	})
}

// TestImplementationOverlapOnInstall rejects a second implementation of a
// single extension point for the same filter values, naming both and the
// values.
func TestImplementationOverlapOnInstall(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, pointFiles("1.0.0", countryFilter)),
			writePackage(t, implFiles("TRIMPL", "1.0.0", map[string]string{"A": `, "filters": { "COUNTRY": [{ "value": "DE" }, { "value": "AT" }] }`}))},
			kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		other := func(country string) *model.Package {
			return writePackage(t, implFiles("TROTHER", "1.0.0", map[string]string{"B": `, "filters": { "COUNTRY": ` + country + ` }`}))
		}
		_, err := k.Install(ctx, []*model.Package{other(`[{ "pattern": "A*" }]`)}, kernel.Options{})
		var ov *rterr.ImplementationOverlap
		if !errors.As(err, &ov) || ov.Implementations != [2]string{"/TRIMPL/A", "/TROTHER/B"} ||
			!strings.Contains(err.Error(), "/TRIMPL/A and /TROTHER/B both match when active everywhere: COUNTRY = AT") {
			t.Fatalf("overlap: %v", err)
		}
		if _, err := k.Install(ctx, []*model.Package{other(`[{ "from": "E", "to": "Z" }]`)}, kernel.Options{}); err != nil {
			t.Fatalf("no overlap: %v", err)
		}
		// Two implementations of one package overlap as well.
		_, err = k.Upgrade(ctx, writePackage(t, implFiles("TRIMPL", "1.1.0", map[string]string{
			"A": `, "filters": { "COUNTRY": [{ "value": "DE" }] }`, "C": `, "filters": { "COUNTRY": [{ "pattern": "*" }] }`})), kernel.Options{})
		if !errors.As(err, &ov) || !strings.Contains(err.Error(), "COUNTRY = DE") {
			t.Fatalf("overlap within a package: %v", err)
		}
	})
}

// TestExtensionPointUpgradeRules applies the rules for upgrades of an
// extension point.
func TestExtensionPointUpgradeRules(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, pointFiles("1.0.0", countryFilter)),
			writePackage(t, implFiles("TRIMPL", "1.0.0", map[string]string{"A": `, "filters": { "COUNTRY": [{ "value": "DE" }] }`}))},
			kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		upgrade := func(version, point string, opts kernel.Options) (string, error) {
			p, err := k.Upgrade(ctx, writePackage(t, pointFiles(version, point)), opts)
			return planText(p), err
		}
		// A new filter field with a default, a fallback and commits.
		text, err := upgrade("1.1.0", `"use": "single", "fallback": "/TRPOINT/DEFAULT_CHECKS", "commitAllowed": true, "filters": [
			{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }, { "name": "CLASS", "domain": "/TRBASE/COUNTER", "default": [{ "pattern": "*" }] }]`, kernel.Options{})
		if err != nil || !strings.Contains(text, "alter-extension-point /TRPOINT/CHECKS (fallback none -> /TRPOINT/DEFAULT_CHECKS, commit allowed false -> true, filter field CLASS added)") {
			t.Fatalf("allowed upgrade: %v\n%s", err, text)
		}
		for _, c := range []struct{ name, point, want string }{
			{"multiple", `"use": "multiple", "commitAllowed": true, "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" },
				{ "name": "CLASS", "domain": "/TRBASE/COUNTER", "default": [{ "pattern": "*" }] }]`, "use single -> multiple"},
			{"commit forbidden", `"use": "single", "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" },
				{ "name": "CLASS", "domain": "/TRBASE/COUNTER", "default": [{ "pattern": "*" }] }]`, "commit allowed true -> false"},
			{"type changed", `"use": "single", "commitAllowed": true, "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" },
				{ "name": "CLASS", "domain": "/TRBASE/LENGTH", "default": [{ "pattern": "*" }] }]`, "filter field CLASS: type domain /TRBASE/COUNTER -> domain /TRBASE/LENGTH"},
			{"field removed", `"use": "single", "commitAllowed": true, "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }]`, "filter field CLASS removed"},
		} {
			_, err := upgrade("1.2.0", c.point, kernel.Options{})
			var dc *rterr.DestructiveChange
			if !errors.As(err, &dc) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// Changes that break the implementation of another package are
		// rejected even when destructive changes are allowed.
		for _, c := range []struct{ name, point, want string }{
			{"new field without default", `"use": "single", "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" },
				{ "name": "REGION", "domain": "/TRBASE/COUNTER" }]`, "the filter field REGION of /TRPOINT/CHECKS is missing"},
			{"field of the implementation removed", `"use": "single", "filters": []`, "/TRPOINT/CHECKS has no filter field COUNTRY"},
		} {
			_, err := upgrade("1.3.0", c.point, kernel.Options{AllowDestructive: true})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		dir := t.TempDir()
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRPOINT", "version": "2.0.0", "requires": [{ "id": "TRBASE" }] }`))
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{AllowDestructive: true}); err == nil ||
			!strings.Contains(err.Error(), "extension point /TRPOINT/CHECKS does not exist") {
			t.Fatalf("point removed with an implementation of another package: %v", err)
		}
		if _, err := k.Uninstall(ctx, "TRIMPL", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		_, err = k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) || !strings.Contains(err.Error(), "drop-extension-point /TRPOINT/CHECKS") {
			t.Fatalf("point removed: %v", err)
		}
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{AllowDestructive: true}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
	})
}
