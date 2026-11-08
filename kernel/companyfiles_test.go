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
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/xlsx"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

const companyFiles = "/TRFIN/COMPANY_FILES"

// unzip returns the entries of a zip archive.
func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		out[f.Name] = b
	}
	return out
}

// sheetRows reads the texts of the cells of a sheet.
func sheetRows(t *testing.T, data []byte, sheet string) [][]string {
	t.Helper()
	r, err := xlsx.NewReader(context.Background(), data, native.DefaultLimits.Workbook)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	if err := r.Rows(sheet, func(_ int, cells []xlsx.Cell) error {
		var row []string
		for _, c := range cells {
			s, _, _ := c.TextValue()
			row = append(row, s)
		}
		out = append(out, row)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// companiesSheet builds a workbook with the sheet Companies.
func companiesSheet(t *testing.T, rows ...[]string) []byte {
	t.Helper()
	w := xlsx.Create(native.DefaultLimits.Workbook)
	defer w.Close()
	if err := w.RenameSheet("Sheet1", "Companies"); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		for j, v := range row {
			col, _ := xlsx.ColumnName(j + 1)
			if v != "" {
				if err := w.SetText("Companies", col+itoa(i+1), v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	data, err := w.Save()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestCompanyFiles exports the companies of TRFIN as workbook in a zip
// archive with an invoice and imports them again, in code as rt run does
// and through the HTTP handlers for download and upload.
func TestCompanyFiles(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		boss := companyAdmin(t, k, "ACME")
		mustCallAs(t, k, boss, "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY", `"DE"`, `"Germany"`)
		mustCallAs(t, k, boss, "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY", `"FR"`, `"France"`)
		mustCallAs(t, k, boss, "/TRFIN/COMPANY_SERVICE", "CREATE", `"C1"`, `"Acme GmbH"`, `"DE"`)
		mustCallAs(t, k, boss, "/TRFIN/COMPANY_SERVICE", "CREATE", `"C2"`, `"Bleu SA"`, `"FR"`)

		if _, err := k.Users().Create(ctx, "anna", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Users().Create(ctx, "carl", "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, a := range []kernel.Assignment{{User: "anna", Role: "/TRFIN/SERVICE_USER"}, {User: "anna", Role: "/TRFIN/COMPANY_ADMIN"},
			{User: "carl", Role: "/TRFIN/SERVICE_USER"}, {User: "carl", Role: "/TRFIN/COMPANY_CLERK"}} {
			a.Tenant = "ACME"
			if _, _, err := k.Roles().Assign(ctx, a, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
		run := func(method string, args ...string) string {
			t.Helper()
			out, err := call(k, kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}, companyFiles, method, args...)
			if err != nil {
				t.Fatalf("%s: %v", method, err)
			}
			return out
		}
		// rt run TRFIN /TRFIN/COMPANY_FILES EXPORT '["2026-11-08"]' --tenant ACME --user anna
		archive := unb64(t, run("EXPORT", `"2026-11-08"`))
		files := unzip(t, archive)
		if len(files) != 2 {
			t.Fatalf("entries: %d", len(files))
		}
		rows := sheetRows(t, files["companies.xlsx"], "Companies")
		if len(rows) != 3 || rows[0][0] != "Company" || rows[1][0] != "C1" || rows[2][1] != "Bleu SA" {
			t.Errorf("sheet: %q", rows)
		}
		invoice := string(files["invoice.html"])
		for _, part := range []string{"Invoice EXPORT", "Company record C1 Acme GmbH", "Company record C2 Bleu SA", "Total: 0.20", "2026-11-08"} {
			if !strings.Contains(invoice, part) {
				t.Errorf("invoice lacks %q:\n%s", part, invoice)
			}
		}

		// The archive imports back; a workbook alone does as well, with
		// headings in German and a new company.
		if got := run("IMPORT", js(base64.StdEncoding.EncodeToString(archive))); got != "2" {
			t.Errorf("import: %s", got)
		}
		book := companiesSheet(t, []string{"Firma", "Name", "Land", "Status"}, []string{"C1", "Acme AG", "DE", "ACTIVE"}, []string{"C3", "Drei", "FR", ""})
		if got := run("IMPORT", js(base64.StdEncoding.EncodeToString(book))); got != "2" {
			t.Errorf("import workbook: %s", got)
		}
		if got := mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "FIND", `"C1"`); !strings.Contains(got, `"NAME":"Acme AG"`) {
			t.Errorf("C1 not updated: %s", got)
		}
		if got := mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "FIND", `"C3"`); !strings.Contains(got, `"STATUS":"ACTIVE"`) {
			t.Errorf("C3 not created with the default status: %s", got)
		}

		// Through HTTP.
		if err := k.ActivateHTTPService(ctx, "/trfin"); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(k.HTTPHandler())
		t.Cleanup(srv.Close)
		c := client{t, srv.URL}
		anna, carl := basicAuth("anna", "secret"), basicAuth("carl", "secret")

		a := c.do("GET", "/trfin/companies/export", anna, "")
		if a.status != 200 || a.header.Get("Content-Type") != "application/zip" || !strings.Contains(a.header.Get("Content-Disposition"), "companies.zip") {
			t.Fatalf("export: %d %v", a.status, a.header)
		}
		download := []byte(a.body)
		if rows := sheetRows(t, unzip(t, download)["companies.xlsx"], "Companies"); len(rows) != 4 {
			t.Errorf("download: %q", rows)
		}
		a = c.do("GET", "/trfin/companies/export?file=companies.xlsx", anna, "")
		if a.status != 200 || !strings.HasPrefix(a.header.Get("Content-Type"), "application/vnd.openxmlformats") || len(sheetRows(t, []byte(a.body), "Companies")) != 4 {
			t.Errorf("one file: %d %v", a.status, a.header)
		}
		if a := c.do("GET", "/trfin/companies/export?file=other.txt", anna, ""); a.status != 404 {
			t.Errorf("missing file: %d", a.status)
		}
		// The clerk may not export: the sheet has the headings only and
		// the archive no invoice.
		a = c.do("GET", "/trfin/companies/export", carl, "")
		if files := unzip(t, []byte(a.body)); a.status != 200 || len(files) != 1 || len(sheetRows(t, files["companies.xlsx"], "Companies")) != 1 {
			t.Errorf("clerk export: %d %d", a.status, len(files))
		}

		if a := c.do("POST", "/trfin/companies/import", anna, string(download)); a.status != 200 || a.body != `{"imported":3}` {
			t.Errorf("upload: %d %s", a.status, a.body)
		}
		bad := companiesSheet(t, []string{"Company", "Name", "Country"}, []string{"C4", "", "DE"}, []string{"C5", "Fünf", "XYZ"})
		a = c.do("POST", "/trfin/companies/import", anna, string(bad))
		var violations []map[string]any
		if err := json.Unmarshal([]byte(a.body), &violations); a.status != 422 || err != nil || len(violations) != 2 ||
			violations[0]["COMPONENT"] != "NAME" || violations[1]["COLUMN_NAME"] != "C" || violations[1]["RAW"] != "XYZ" {
			t.Errorf("violations: %d %s", a.status, a.body)
		}
		if a := c.do("POST", "/trfin/companies/import", anna, "no archive"); a.status != 400 {
			t.Errorf("no archive: %d %s", a.status, a.body)
		}
		// The clerk may update C1 in Germany but not create C9: nothing is
		// stored.
		upload := companiesSheet(t, []string{"Company", "Name", "Country"}, []string{"C1", "Acme SE", "DE"}, []string{"C9", "Neun", "DE"})
		if a := c.do("POST", "/trfin/companies/import", carl, string(upload)); a.status != http.StatusForbidden {
			t.Errorf("clerk upload: %d %s", a.status, a.body)
		}
		if got := mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "FIND", `"C1"`); !strings.Contains(got, `"NAME":"Acme AG"`) {
			t.Errorf("C1 after the rolled back upload: %s", got)
		}
	})
}
