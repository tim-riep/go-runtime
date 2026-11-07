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
				{[]string{"show", "COMPANIES"}, exitOK, "/TROTTO/OTTO_ID"},
				{[]string{"show", "COMPANY_STATUS"}, exitOK, "value BLOCKED"},
				{[]string{"show", "COUNTRY"}, exitOK, "Länderschlüssel"},
				{[]string{"show", "NAME"}, exitError, "domain:NAME or data-element:NAME"},
				{[]string{"show", "data-element:NAME"}, exitOK, "used by COMPANIES.NAME"},
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
				{[]string{"show", "OTTO_SHOPS"}, exitOK, "setting TRFIN_COMPANIES"},
				{[]string{"show", "PAYMENTS"}, exitOK, "data class application"},
				{[]string{"setting", "list"}, exitOK, "TRFIN_COMPANIES  TRFIN"},
				{[]string{"setting", "show", "TRFIN_COMPANIES"}, exitOK, "parent OTTO_CONNECTIONS through CONNECTION"},
				{[]string{"setting", "show", "TRFIN_COMPANIES", "--json"}, exitOK, `"formatVersion": 1`},
				{[]string{"setting", "show", "NOPE"}, exitError, "not found"},
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
