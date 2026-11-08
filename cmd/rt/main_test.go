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
				{[]string{"install", "--dry-run", filepath.Join(ex, "TRFIN")}, exitInvalid, "not set up: de"},
				{[]string{"language", "add", "de"}, exitOK, "German"},
				{[]string{"install", "--dry-run", filepath.Join(ex, "TRFIN")}, exitOK, "Dry run"},
				{[]string{"install", filepath.Join(ex, "TROTTO")}, exitInvalid, "requires TRFIN"},
				{[]string{"install", filepath.Join(testutil.ModuleRoot(t), "kernelpkg", "TRBASE")}, exitInvalid, "TRBASE is a kernel package"},
				{[]string{"natives", "list"}, exitOK, "TRBASE:/TRBASE/HASH.SHA256"},
				{[]string{"install", filepath.Join(ex, "TRFIN"), filepath.Join(ex, "TROTTO")}, exitOK, "Executed"},
				{[]string{"extension-point", "list", "TRFIN"}, exitOK, "/TRFIN/COMPANY_CHECKS  TRFIN    multiple  /TRFIN/COMPANY_CHECKS  COUNTRY  1"},
				{[]string{"extension-point", "show", "/TRFIN/COMPANY_CHECKS"}, exitOK, "implementation 1: /TROTTO/COMPANY_CHECKS (package TROTTO, class /TROTTO/COMPANY_CHECKS, order 100)"},
				{[]string{"extension-point", "show", "/TRFIN/COMPANY_CHECKS"}, exitOK, `COUNTRY: "DE" "AT"`},
				{[]string{"extension-point", "show", "/TRFIN/COMPANY_CHECKS"}, exitOK, "used by TRFIN src/COMPANY_SERVICE.class:"},
				{[]string{"extension-point", "show", "/TRFIN/COMPANY_CHECKS", "--json"}, exitOK, `"use": "multiple"`},
				{[]string{"extension-point", "show", "/TRFIN/NOPE"}, exitError, "not found"},
				{[]string{"implementation", "deactivate", "/TROTTO/COMPANY_CHECKS"}, exitOK, "Implementation /TROTTO/COMPANY_CHECKS deactivated globally."},
				{[]string{"extension-point", "show", "/TRFIN/COMPANY_CHECKS"}, exitOK, "globally: inactive"},
				{[]string{"implementation", "reset", "/TROTTO/COMPANY_CHECKS"}, exitOK, "is active again"},
				{[]string{"implementation", "history", "/TROTTO/COMPANY_CHECKS"}, exitOK, "cli     reset       /TROTTO/COMPANY_CHECKS  -       inactive  -"},
				{[]string{"implementation", "activate", "/TROTTO/NOPE"}, exitError, "not found"},
				{[]string{"implementation", "frobnicate"}, exitUsage, ""},
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
				{[]string{"show", "/TRFIN/COMPANY"}, exitError, "names a data-element and an authorization-object"},
				{[]string{"show", "authorization-object:/TRFIN/COMPANY"}, exitOK, "activity EXPORT: Export"},
				{[]string{"show", "authorization-object:/TRFIN/COMPANY"}, exitOK, "COUNTRY     /TRBASE/COUNTRY"},
				{[]string{"show", "--json", "authorization-object:/TROTTO/SYNC"}, exitOK, `"code": "SEND"`},
				{[]string{"authorization-objects", "list", "TROTTO"}, exitOK, "/TROTTO/SYNC  TROTTO   Connection to OTTO  COUNTRY, ACTIVITY"},
				{[]string{"authorization-objects", "list", "NOPE"}, exitError, "NOPE"},
				{[]string{"activities", "list", "--lang", "de"}, exitOK, "APPROVE     Genehmigen"},
				{[]string{"activities", "list"}, exitOK, "ADMINISTER  Administer"},
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
				{[]string{"run", "TRFIN", "/TRFIN/INVOICE_RENDERER", "RENDER", `[{"INVOICE_ID":"R-1","COMPANY_ID":"C1","ISSUED_ON":"2026-11-07","NOTE":null,
					"REFERENCE":null,"TOTAL":"10.00","ITEMS":[]}]`, "--tenant", "ACME"}, exitOK, "Rechnung R-1"},
				{[]string{"effects", "list"}, exitOK, "No failed effects."},
				{[]string{"user", "create", "anna", "--tenant", "ACME", "--lang", "de"}, exitOK, "User anna created."},
				{[]string{"user", "list"}, exitOK, "anna  active  de"},
				{[]string{"token", "create", "anna", "--label", "cli", "--expires", "2099-01-01"}, exitOK, "shown only now"},
				{[]string{"token", "list", "anna"}, exitOK, "2099-01-01"},
				{[]string{"user", "lock", "anna"}, exitOK, "locked"},
				{[]string{"user", "unlock", "anna"}, exitOK, "unlocked"},
				{[]string{"user", "tenant", "remove", "anna", "BETA"}, exitError, "not found"},
				{[]string{"http", "list", "TRFIN"}, exitOK, "/trfin"},
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
				input = strings.NewReader("secret\n")
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

func TestDisasm(t *testing.T) {
	dir := filepath.Join(testutil.ModuleRoot(t), "examples", "TRFIN")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"disasm", dir, "/TRFIN/TEMPLATE_RENDERER"}, &out, &errOut); code != exitOK {
		t.Fatalf("disasm: %d %s", code, errOut.String())
	}
	for _, want := range []string{
		"/TRFIN/TEMPLATE_RENDERER.TEMPLATE (static, 1 parameters, 2 locals)",
		`; src/TEMPLATE_RENDERER.class:6  var templates = /TRBASE/ASSET_REPOSITORY.OPEN("TRFIN", "templates")`,
		`; "templates"`,
		"; /TRBASE/ASSET_REPOSITORY.OPEN",
		"; /TRBASE/ASSET_REPOSITORY.GET_IN_LANGUAGE",
		"; /TRBASE/XSLT_STYLESHEET.COMPILE",
		"/TRFIN/TEMPLATE_RENDERER.RENDER_ALL (static, 2 parameters,",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "/TRFIN/COMPANY_SERVICE") {
		t.Error("the listing has functions of other classes")
	}
	out.Reset()
	if code := run(context.Background(), []string{"disasm", dir, "/TRFIN/COMPANY_SERVICE.BLOCK_COUNTRY"}, &out, &errOut); code != exitOK || !strings.Contains(out.String(), "; update set /TRFIN/COMPANIES") {
		t.Errorf("database statement: %d %s", code, out.String())
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"disasm", dir, "/TRFIN/NOPE"}, &out, &errOut); code != exitError || !strings.Contains(errOut.String(), "package TRFIN has no function /TRFIN/NOPE") {
		t.Errorf("unknown name: %d %s", code, errOut.String())
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
	} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), s.args, &out, &errOut)
		all := out.String() + errOut.String()
		if code != s.code || !strings.Contains(all, s.contains) {
			t.Errorf("rt %s: exit %d (want %d):\n%s", strings.Join(s.args, " "), code, s.code, all)
		}
	}
	// The kernel installs the package and runs its code for a tenant.
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
	if code := run(context.Background(), []string{"--db", db.DSN, "tenant", "create", "ACME"}, &out, &errOut); code != exitOK {
		t.Fatalf("tenant: %s", errOut.String())
	}
	for _, s := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"test", dir}, exitOK, "3 tests, 3 passed, 0 failed"},
		{[]string{"test", "TRORDER"}, exitOK, "3 tests, 3 passed, 0 failed"},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "RATE_ALL", `[[{"ID":"A1","AMOUNT":"1500.00","NOTE":"","PAID":false}]]`, "--tenant", "ACME"}, exitOK, `"A1": "HIGH"`},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "TO_ROWS", `[[{"ID":"A1","AMOUNT":"1","NOTE":"","PAID":false}]]`, "--tenant", "ACME"}, exitOK, `"NOTE": "",
    "COUPON": null`},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "RATE_ALL", `[[{"ID":"A3","AMOUNT":"-1","PAID":false}]]`, "--tenant", "ACME"}, exitError, "/TRORDER/CHECKS included from src/ORDER_RATING.class"},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "RATE", "--tenant", "ACME"}, exitError, "no public static method RATE"},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "RATE_ALL", `[[{"ID":"","AMOUNT":"1","PAID":false}]]`, "--tenant", "ACME"}, exitError, "does not allow the empty string"},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "RATE_ALL", "[[]]"}, exitUsage, "--tenant"},
		{[]string{"run", "TRORDER", "/TRORDER/ORDER_RATING", "RATE_ALL", "[[]]", "--tenant", "NOBODY"}, exitError, "tenant NOBODY not found"},
		{[]string{"run", "TRFIN", "/TRFIN/X", "Y", "--tenant", "ACME"}, exitError, "package TRFIN not found"},
	} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), append([]string{"--db", db.DSN}, s.args...), &out, &errOut)
		all := out.String() + errOut.String()
		if code != s.code || !strings.Contains(all, s.contains) {
			t.Errorf("rt %s: exit %d (want %d):\n%s", strings.Join(s.args, " "), code, s.code, all)
		}
	}
}

func TestRoleCommands(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			ex := filepath.Join(testutil.ModuleRoot(t), "examples")
			dir := t.TempDir()
			local := filepath.Join(dir, "ops.json")
			testutil.WriteFile(t, dir, "ops.json", []byte(`{ "name": "/LOCAL/OPS", "texts": { "en": { "label": "Operations" } },
				"roles": ["/TRFIN/COMPANY_CLERK", "/TROTTO/SYNC_OPERATOR"] }`))
			testutil.WriteFile(t, dir, "all.json", []byte(`{ "name": "/LOCAL/ALL", "texts": { "en": { "label": "All" } },
				"authorizations": [{ "object": "/TRFIN/COMPANY", "fields": { "COMPANY_ID": [{ "pattern": "*" }], "COUNTRY": [{ "pattern": "*" }], "ACTIVITY": [{ "pattern": "*" }] } }] }`))
			steps := []struct {
				args     []string
				code     int
				contains string
			}{
				{[]string{"init", "--admin", "root", "--tenant", "ACME"}, exitOK, "User root created with all authorizations in tenant ACME"},
				{[]string{"language", "add", "de"}, exitOK, "German"},
				{[]string{"install", filepath.Join(ex, "TRFIN"), filepath.Join(ex, "TROTTO")}, exitOK, "create-role /TRFIN/COMPANY_CLERK"},
				{[]string{"user", "create", "anna", "--tenant", "ACME"}, exitOK, "created"},
				{[]string{"role", "list"}, exitOK, "/TROTTO/SYNC_OPERATOR   TROTTO   single"},
				{[]string{"run", "TRFIN", "/TRFIN/ORDER_INTAKE", "FIND", `["X"]`, "--tenant", "ACME", "--user", "anna"}, exitOK, "null"},
				{[]string{"run", "TRFIN", "/TRFIN/ORDER_INTAKE", "FIND", `["X"]`, "--tenant", "ACME", "--user", "nobody"}, exitError, "user nobody not found"},
				{[]string{"tenant", "create", "BETA"}, exitOK, "BETA"},
				{[]string{"run", "TRFIN", "/TRFIN/ORDER_INTAKE", "FIND", `["X"]`, "--tenant", "BETA", "--user", "anna"}, exitUsage, "does not work in tenant BETA"},
				{[]string{"role", "show", "/RT/ALL_AUTHORIZATIONS"}, exitOK, "objects and activities installed later are included"},
				{[]string{"role", "delete", "/RT/ALL_AUTHORIZATIONS"}, exitError, "is a role of the kernel; it cannot change"},
				{[]string{"role", "show", "/TRFIN/COMPANY_CLERK"}, exitOK, `COUNTRY      "DE" "AT" "PL".."SK"`},
				{[]string{"role", "show", "/TRFIN/COMPANY_ADMIN"}, exitOK, "note: * on ACTIVITY includes activities the object allows later"},
				{[]string{"role", "import", local}, exitOK, "Role /LOCAL/OPS saved."},
				{[]string{"role", "create", filepath.Join(dir, "all.json")}, exitOK, "grants * on ACTIVITY of /TRFIN/COMPANY"},
				{[]string{"role", "create", local}, exitError, "exists already"},
				{[]string{"role", "show", "--json", "/LOCAL/OPS"}, exitOK, `"role": "/TROTTO/SYNC_OPERATOR"`},
				{[]string{"role", "list", "--package", "LOCAL"}, exitOK, "/LOCAL/OPS  LOCAL    composite"},
				{[]string{"role", "assign", "anna", "/LOCAL/OPS", "--tenant", "ACME", "--from", "2026-01-01"}, exitOK, "Role /LOCAL/OPS assigned to anna"},
				{[]string{"role", "assign", "anna", "/LOCAL/OPS", "--tenant", "ACME", "--to", "2026-03-01"}, exitOK, "overlaps (2026-01-01 to open)"},
				{[]string{"role", "assign", "anna", "/LOCAL/OPS", "--tenant", "BETA"}, exitError, "does not work in tenant BETA"},
				{[]string{"user", "roles", "anna"}, exitOK, "(overlaps)"},
				{[]string{"user", "authorizations", "anna", "--tenant", "ACME", "--date", "2026-02-01"}, exitOK, "from /TROTTO/SYNC_OPERATOR in /LOCAL/OPS"},
				{[]string{"user", "authorizations", "anna", "--tenant", "ACME", "--date", "2026-02-01"}, exitOK, `COUNTRY      "AT" "CH" D*`},
				{[]string{"user", "authorizations", "root", "--tenant", "ACME"}, exitOK, "ALL AUTHORIZATIONS from /RT/ALL_AUTHORIZATIONS"},
				{[]string{"user", "authorizations", "--json", "anna", "--tenant", "ACME", "--date", "2025-12-01"}, exitOK, `"/TRFIN/COMPANY": {`},
				{[]string{"install", filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "auth", "TRAUTH")}, exitOK, "compile-package TRAUTH"},
				{[]string{"run", "TRAUTH", "/TRAUTH/CHECKS", "UPDATE_COMPANY", `["C1", "AT"]`, "--tenant", "ACME", "--user", "anna"}, exitOK, "updated C1"},
				{[]string{"run", "TRAUTH", "/TRAUTH/CHECKS", "UPDATE_COMPANY", `["C1", "FR"]`, "--tenant", "ACME", "--user", "anna"}, exitNotAuthorized,
					`check: /TRFIN/COMPANY ACTIVITY="UPDATE" COMPANY_ID="C1" COUNTRY="FR"`},
				{[]string{"run", "TRAUTH", "/TRAUTH/CHECKS", "UPDATE_COMPANY", `["C1", "AT"]`, "--tenant", "ACME"}, exitNotAuthorized, "--user"},
				{[]string{"user", "last-failed-check", "anna", "--tenant", "ACME"}, exitOK, `values: ACTIVITY="UPDATE" COMPANY_ID="C1" COUNTRY="FR"`},
				{[]string{"user", "last-failed-check", "anna", "--tenant", "ACME"}, exitOK, "place:  /TRAUTH/CHECKS.UPDATE_COMPANY (package TRAUTH, line 6)"},
				{[]string{"user", "last-failed-check", "anna", "--tenant", "ACME"}, exitOK, `COUNTRY      "AT" "DE" "PL".."SK"`},
				{[]string{"user", "last-failed-check", "root", "--tenant", "ACME"}, exitError, "not found"},
				{[]string{"trace", "authorizations", "start", "anna", "--tenant", "ACME", "--max", "5"}, exitOK, "Tracing the authorization checks of anna in ACME"},
				{[]string{"run", "TRAUTH", "/TRAUTH/CHECKS", "CAN", `["FR", "READ"]`, "--tenant", "ACME", "--user", "anna"}, exitOK, "false"},
				{[]string{"trace", "authorizations", "show", "anna"}, exitOK, "expression  /TRFIN/COMPANY  denied"},
				{[]string{"trace", "authorizations", "show", "anna"}, exitOK, `ACTIVITY="READ" COMPANY_ID=any COUNTRY="FR"`},
				{[]string{"trace", "authorizations", "stop", "anna"}, exitOK, "Trace of anna stopped."},
				{[]string{"run", "TRFIN", "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY", `["DE", "Germany"]`, "--tenant", "ACME"}, exitOK, "null"},
				{[]string{"trace", "extension-points", "start", "anna", "--tenant", "ACME"}, exitOK, "Tracing the extension points that anna runs in ACME"},
				{[]string{"run", "TRFIN", "/TRFIN/COMPANY_SERVICE", "CREATE", `["C9", "Neu", "DE"]`, "--tenant", "ACME", "--user", "anna"}, exitOK, `"STATUS": "BLOCKED"`},
				{[]string{"trace", "extension-points", "show", "anna"}, exitOK, "/TRFIN/COMPANY_CHECKS  COUNTRY=\"DE\""},
				{[]string{"trace", "extension-points", "show", "anna"}, exitOK, "/TROTTO/COMPANY_CHECKS: selected"},
				{[]string{"trace", "extension-points", "stop", "anna"}, exitOK, "Trace of anna stopped."},
				{[]string{"implementation", "deactivate", "/TROTTO/COMPANY_CHECKS", "--tenant", "ACME"}, exitOK, "deactivated in tenant ACME"},
				{[]string{"run", "TRFIN", "/TRFIN/COMPANY_SERVICE", "CREATE", `["C10", "Neu", "DE"]`, "--tenant", "ACME"}, exitOK, `"STATUS": "ACTIVE"`},
				{[]string{"extension-point", "show", "/TRFIN/COMPANY_CHECKS"}, exitOK, "in tenant ACME: inactive"},
				{[]string{"trace", "authorizations", "show", "anna"}, exitOK, "No trace of anna runs."},
				{[]string{"show", "authorization-object:/TRFIN/COMPANY"}, exitOK, "Used in code:\n  TRAUTH src/CHECKS.class:6\n"},
				{[]string{"role", "export", "/TRFIN/COMPANY_CLERK"}, exitOK, `"from": "PL"`},
				{[]string{"role", "delete", "/TRFIN/COMPANY_CLERK"}, exitError, "package roles change with their package"},
				{[]string{"role", "delete", "/LOCAL/OPS"}, exitNeedsPermit, "--yes"},
				{[]string{"role", "delete", "/LOCAL/OPS", "--yes"}, exitOK, "deleted"},
				{[]string{"role", "history", "--user", "anna"}, exitOK, "assignment-removed  /LOCAL/OPS"},
				{[]string{"role", "history", "/RT/ALL_AUTHORIZATIONS"}, exitOK, "cli  assign"},
			}
			for _, s := range steps {
				var out, errOut bytes.Buffer
				input = strings.NewReader("secret\n")
				code := run(context.Background(), append([]string{"--db", db.DSN}, s.args...), &out, &errOut)
				all := out.String() + errOut.String()
				if code != s.code || !strings.Contains(all, s.contains) {
					t.Fatalf("rt %s: exit %d (want %d), output:\n%s", strings.Join(s.args, " "), code, s.code, all)
				}
			}
		})
	}
}

// TestInitWithAdmin creates the first user with all authorizations from the
// environment and on a terminal, and checks the users with the role.
func TestInitWithAdmin(t *testing.T) {
	for _, db := range testutil.Databases(t) {
		t.Run(db.Name, func(t *testing.T) {
			runCLI := func(stdin string, args ...string) (int, string) {
				var out, errOut bytes.Buffer
				input = strings.NewReader(stdin)
				code := run(context.Background(), append([]string{"--db", db.DSN}, args...), &out, &errOut)
				return code, out.String() + errOut.String()
			}
			t.Setenv("RT_ADMIN_USER", "root")
			t.Setenv("RT_ADMIN_TENANT", "MAIN")
			t.Setenv("RT_ADMIN_PASSWORD", "secret")
			if code, out := runCLI("", "init"); code != exitOK || !strings.Contains(out, "Tenant MAIN created.") || !strings.Contains(out, "User root created with all authorizations in tenant MAIN") {
				t.Fatalf("init from the environment: %d %s", code, out)
			}
			if code, out := runCLI("", "check"); code != exitOK || !strings.Contains(out, "root  MAIN  active  open to open  effective") || strings.Contains(out, "warning") {
				t.Errorf("check: %d %s", code, out)
			}
			code, out := runCLI("", "user", "roles", "root")
			id := ""
			for _, f := range strings.Fields(out) {
				if strings.Count(f, "-") == 4 {
					id = f
				}
			}
			if code != exitOK || id == "" {
				t.Fatalf("roles: %d %s", code, out)
			}
			if code, out := runCLI("", "role", "unassign", id); code != exitNeedsPermit || !strings.Contains(out, "the last user with /RT/ALL_AUTHORIZATIONS") {
				t.Errorf("unassign the last: %d %s", code, out)
			}
			if code, out := runCLI("", "user", "lock", "root"); code != exitNeedsPermit {
				t.Errorf("lock the last: %d %s", code, out)
			}
			if code, out := runCLI("", "role", "unassign", id, "--yes"); code != exitOK {
				t.Fatalf("unassign with --yes: %d %s", code, out)
			}
			if code, out := runCLI("", "check", "--json"); code != exitOK || !strings.Contains(out, `"warnings": [`) || !strings.Contains(out, "no active user has /RT/ALL_AUTHORIZATIONS") {
				t.Errorf("check without holder: %d %s", code, out)
			}
			if code, out := runCLI("", "role", "history", "/RT/ALL_AUTHORIZATIONS"); code != exitOK || !strings.Contains(out, "cli  unassign") {
				t.Errorf("history: %d %s", code, out)
			}
		})
	}
	// On a terminal of a new database, rt init asks.
	t.Run("terminal", func(t *testing.T) {
		old := terminal
		terminal = func() bool { return true }
		defer func() { terminal = old }()
		var out, errOut bytes.Buffer
		input = strings.NewReader("y\nadmin\nsecret\nFIRST\n")
		dsn := "sqlite:" + filepath.Join(t.TempDir(), "rt.db")
		if code := run(context.Background(), []string{"--db", dsn, "init"}, &out, &errOut); code != exitOK ||
			!strings.Contains(out.String(), "Create a first user with all authorizations? [y/N]") || !strings.Contains(out.String(), "User admin created with all authorizations in tenant FIRST") {
			t.Fatalf("interactive init: %d %s %s", code, out.String(), errOut.String())
		}
		out.Reset()
		input = strings.NewReader("n\n")
		if code := run(context.Background(), []string{"--db", dsn, "init"}, &out, &errOut); code != exitOK || strings.Contains(out.String(), "Create a first user") {
			t.Errorf("init with a user already: %d %s", code, out.String())
		}
	})
}
