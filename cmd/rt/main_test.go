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
)

func TestCLI(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ex := filepath.Join(testutil.ModuleRoot(t), "examples")
			steps := []struct {
				args     []string
				code     int
				contains string
			}{
				{[]string{"list"}, exitError, "run rt init"},
				{[]string{"init"}, exitOK, "Executed"},
				{[]string{"init"}, exitOK, "Nothing to do"},
				{[]string{"install", "--dry-run", filepath.Join(ex, "TRBASE")}, exitInvalid, "not set up: de"},
				{[]string{"language", "add", "de"}, exitOK, "German"},
				{[]string{"install", "--dry-run", filepath.Join(ex, "TRBASE")}, exitOK, "Dry run"},
				{[]string{"install", filepath.Join(ex, "TRFIN")}, exitInvalid, "requires TRBASE"},
				{[]string{"install", filepath.Join(ex, "TRBASE"), filepath.Join(ex, "TRFIN"), filepath.Join(ex, "TROTTO")}, exitOK, "Executed"},
				{[]string{"plan", filepath.Join(ex, "upgrade", "TROTTO")}, exitOK, "upgrade TROTTO 1.0.0 -> 1.1.0"},
				{[]string{"upgrade", filepath.Join(ex, "upgrade", "TROTTO")}, exitOK, "Executed"},
				{[]string{"list"}, exitOK, "TROTTO  1.1.0"},
				{[]string{"show", "/TRFIN/COMPANIES"}, exitOK, "/TROTTO/OTTO_ID"},
				{[]string{"show", "/TRFIN/COMPANY_STATUS"}, exitOK, `value "BLOCKED"`},
				{[]string{"show", "/TRBASE/COUNTRY"}, exitOK, "Länderschlüssel"},
				{[]string{"show", "/TRBASE/NAME"}, exitError, "domain:/TRBASE/NAME or data-element:/TRBASE/NAME"},
				{[]string{"show", "data-element:/TRBASE/NAME"}, exitOK, "used by /TRFIN/COMPANIES.NAME"},
				{[]string{"show", "TRFIN"}, exitOK, "required by [TROTTO]"},
				{[]string{"show", "NOPE"}, exitError, "not found"},
				{[]string{"check"}, exitOK, "match"},
				{[]string{"asset", "repos"}, exitOK, "TRFIN    templates"},
				{[]string{"asset", "repos", "TRBASE"}, exitOK, "Base styles and fonts"},
				{[]string{"asset", "list", "TRFIN", "templates"}, exitOK, "letter/"},
				{[]string{"asset", "list", "TRFIN", "templates", "invoice"}, exitOK, "de, en"},
				{[]string{"asset", "show", "TRFIN", "templates", "invoice/invoice.xsl"}, exitOK, "invoice/invoice_de.xsl"},
				{[]string{"asset", "show", "TRFIN", "web", "images"}, exitOK, "Folder images"},
				{[]string{"asset", "get", "TRFIN", "templates", "invoice/invoice.xsl", "--lang", "de", "-o", "-"}, exitOK, "Rechnung"},
				{[]string{"asset", "get", "TRFIN", "templates", "invoice/invoice.xsl", "--lang", "pl", "-o", "-"}, exitOK, "Invoice"},
				{[]string{"asset", "get", "TRFIN", "web", "images/logo.png", "-o", filepath.Join(t.TempDir(), "logo.png")}, exitOK, "image/png"},
				{[]string{"asset", "get", "TRFIN", "web", "nope.png", "-o", "-"}, exitError, "not found"},
				{[]string{"show", "/TROTTO/OTTO_SHOPS"}, exitOK, "setting /TRFIN/COMPANIES"},
				{[]string{"show", "/TRFIN/PAYMENTS"}, exitOK, "data class application"},
				{[]string{"setting", "list"}, exitOK, "/TRFIN/COMPANIES  TRFIN"},
				{[]string{"setting", "show", "/TRFIN/COMPANIES"}, exitOK, "parent /TROTTO/OTTO_CONNECTIONS through CONNECTION"},
				{[]string{"setting", "show", "/TRFIN/COMPANIES", "--json"}, exitOK, `"formatVersion": 3`},
				{[]string{"setting", "show", "NOPE"}, exitError, "not found"},
				{[]string{"show", "/TRBASE/ADDRESS"}, exitOK, "used by table /TRFIN/COMPANIES (include)"},
				{[]string{"show", "/TRFIN/COMPANY_GROUP"}, exitOK, "SEAT"},
				{[]string{"show", "/TRFIN/COMPANY_ARRAY", "--json"}, exitOK, `"kind": "array-type"`},
				{[]string{"show", "/TRFIN/COMPANIES_ARCHIVE"}, exitOK, "INCLUDE via /TRBASE/ADDRESS (include /TRFIN/COMPANIES)"},
				{[]string{"check"}, exitOK, "match"},
				{[]string{"language", "add", "xx"}, exitError, "ISO 639-1"},
				{[]string{"language", "list"}, exitOK, "English"},
				{[]string{"language", "remove", "en"}, exitError, "cannot be removed"},
				{[]string{"tenant", "create", "ACME", "--description", "Acme Corp"}, exitOK, "created"},
				{[]string{"tenant", "list"}, exitOK, "Acme Corp"},
				{[]string{"tenant", "delete", "ACME"}, exitNeedsPermit, "--yes"},
				{[]string{"tenant", "delete", "ACME", "--yes"}, exitOK, "Executed"},
				{[]string{"uninstall", "TRFIN", "--yes"}, exitError, "required by TROTTO"},
				{[]string{"uninstall", "TROTTO"}, exitNeedsPermit, "--yes"},
				{[]string{"uninstall", "TROTTO", "--yes"}, exitOK, "[DESTRUCTIVE]"},
				{[]string{"log", "--json"}, exitOK, `"operation": "UNINSTALL"`},
				{[]string{"bogus"}, exitUsage, "unknown command"},
			}
			for _, s := range steps {
				var out, errOut bytes.Buffer
				code := run(context.Background(), append([]string{"--db", db.DSN}, s.args...), &out, &errOut)
				all := out.String() + errOut.String()
				if code != s.code || !strings.Contains(all, s.contains) {
					t.Fatalf("rt %s: exit %d (want %d), output:\n%s", strings.Join(s.args, " "), code, s.code, all)
				}
			}
		})
	}
}

func TestFmt(t *testing.T) {
	dir := t.TempDir()
	messy := filepath.Join(dir, "src", "A.class")
	if err := os.MkdirAll(filepath.Dir(messy), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(messy, []byte("public class /X/A\n  public constant B:INT4=1\nend\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "BAD.class"), []byte("public class /X/BAD\n  public constant\nend\n"), 0o644)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"fmt", "--check", dir}, &out, &errOut); code != exitInvalid ||
		!strings.Contains(out.String(), "A.class") || !strings.Contains(errOut.String(), "BAD.class:2:18") {
		t.Fatalf("check: %d %s %s", code, out.String(), errOut.String())
	}
	os.Remove(filepath.Join(dir, "src", "BAD.class"))
	if code := run(context.Background(), []string{"fmt", "--check", dir}, &out, &errOut); code != exitCheck {
		t.Fatalf("check only: %d", code)
	}
	if code := run(context.Background(), []string{"fmt", dir}, &out, &errOut); code != exitOK {
		t.Fatalf("fmt: %d %s", code, errOut.String())
	}
	b, _ := os.ReadFile(messy)
	if string(b) != "public class /X/A\n\n  public constant B: INT4 = 1\n\nend\n" {
		t.Fatalf("formatted: %q", b)
	}
	out.Reset()
	if code := run(context.Background(), []string{"fmt", "--check", dir}, &out, &errOut); code != exitOK || out.Len() != 0 {
		t.Fatalf("canonical: %d %s", code, out.String())
	}
}

func TestBuild(t *testing.T) {
	var out, errOut bytes.Buffer
	dir := filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "TRFIN")
	if code := run(context.Background(), []string{"build", dir}, &out, &errOut); code != exitOK || !strings.Contains(out.String(), "build of TRFIN succeeded: 2 classes") {
		t.Fatalf("build: %d %s %s", code, out.String(), errOut.String())
	}
	broken := t.TempDir()
	testutil.WriteFile(t, broken, "manifest.json", []byte(`{"id":"TRB","version":"1.0.0","languageVersion":1}`))
	testutil.WriteFile(t, broken, "src/A.class", []byte("public class /TRB/A\n\n  public method M() returns INT4\n    return \"x\"\n  end\n\nend\n"))
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"build", broken}, &out, &errOut); code != exitInvalid || !strings.Contains(errOut.String(), "src/A.class:4:12: type:") {
		t.Fatalf("broken build: %d %s %s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"build", "--json", broken}, &out, &errOut); code != exitInvalid || !strings.Contains(out.String(), `"code": "type"`) {
		t.Fatalf("json: %d %s", code, out.String())
	}
}

func TestLanguageCommands(t *testing.T) {
	dir := filepath.Join(testutil.ModuleRoot(t), "examples", "TRORDER")
	for _, s := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"fmt", "--check", dir}, exitOK, ""},
		{[]string{"build", dir}, exitOK, "include /TRORDER/AUDIT does not exist and is skipped"},
		{[]string{"test", dir}, exitOK, "3 tests, 3 passed, 0 failed"},
		{[]string{"run", dir, "/TRORDER/ORDER_RATING", "RATE_ALL", `[[{"ID":"A1","AMOUNT":"1500.00","NOTE":"","PAID":false}]]`}, exitOK, `"A1": "HIGH"`},
		{[]string{"run", dir, "/TRORDER/ORDER_RATING", "TO_ROWS", `[[{"ID":"A1","AMOUNT":"1","NOTE":"","PAID":false}]]`}, exitOK, `"NOTE": "",
    "COUPON": null`},
		{[]string{"run", dir, "/TRORDER/ORDER_RATING", "RATE_ALL", `[[{"ID":"A3","AMOUNT":"-1","PAID":false}]]`}, exitError, "/TRORDER/CHECKS included from src/ORDER_RATING.class"},
		{[]string{"run", dir, "/TRORDER/ORDER_RATING", "RATE"}, exitError, "no public static method RATE"},
		{[]string{"run", dir, "/TRORDER/ORDER_RATING", "RATE_ALL", `[[{"ID":"","AMOUNT":"1","PAID":false}]]`}, exitError, "does not allow the empty string"},
	} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), s.args, &out, &errOut)
		all := out.String() + errOut.String()
		if code != s.code || !strings.Contains(all, s.contains) {
			t.Errorf("rt %s: exit %d (want %d):\n%s", strings.Join(s.args, " "), code, s.code, all)
		}
	}
	// The kernel installs the package; its sources are not stored yet.
	db := testutil.Databases(t)[0]
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--db", db.DSN, "init"}, &out, &errOut); code != exitOK {
		t.Fatalf("init: %s", errOut.String())
	}
	if code := run(context.Background(), []string{"--db", db.DSN, "language", "add", "de"}, &out, &errOut); code != exitOK {
		t.Fatalf("language: %s", errOut.String())
	}
	if code := run(context.Background(), []string{"--db", db.DSN, "install", dir}, &out, &errOut); code != exitOK || !strings.Contains(out.String(), "/TRORDER/ORDERS") {
		t.Fatalf("install: %d %s %s", code, out.String(), errOut.String())
	}
}
