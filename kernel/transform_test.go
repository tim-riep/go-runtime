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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

const invoice = `{"INVOICE_ID":"R-1","COMPANY_ID":"C1","ISSUED_ON":"2026-11-07","NOTE":"","REFERENCE":null,"TOTAL":"150.50",
	"ITEMS":[{"POSITION":1,"TEXT":"Consulting <hours>","AMOUNT":"100.00"},{"POSITION":2,"TEXT":"Travel","AMOUNT":"50.50"}]}`

// TestTransform runs the transformations of TRFIN: an invoice as HTML with
// a parameter, an import of XML into a structure that is stored, the
// canonical format with /TRBASE/IDENTITY, a result that does not fit and
// a template from an asset repository.
func TestTransform(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const R, O, T = "/TRFIN/INVOICE_RENDERER", "/TRFIN/ORDER_INTAKE", "/TRFIN/TEMPLATE_RENDERER"
		html := text(t, mustCall(t, k, "ACME", R, "RENDER", invoice))
		for _, part := range []string{`lang="de"`, "Rechnung R-1", "Consulting &lt;hours&gt;", "Summe: 150.50", `class="note"`} {
			if !strings.Contains(html, part) {
				t.Errorf("RENDER lacks %s:\n%s", part, html)
			}
		}
		// A parameter with null keeps the default of the stylesheet.
		if html := mustCall(t, k, "ACME", R, "RENDER_IN", invoice, "null"); !strings.Contains(html, "Invoice R-1") {
			t.Errorf("RENDER_IN null: %s", html)
		}

		xml := text(t, mustCall(t, k, "ACME", R, "AS_XML", invoice))
		for _, part := range []string{`<NOTE/>`, `<REFERENCE xsi:nil="true"/>`, `<TOTAL>150.50</TOTAL>`, `<ITEMS><item><POSITION>1</POSITION>`, `type="/TRFIN/INVOICE"`} {
			if !strings.Contains(xml, part) {
				t.Errorf("AS_XML lacks %s:\n%s", part, xml)
			}
		}
		round := mustCall(t, k, "ACME", R, "ROUNDTRIP", invoice)
		if !strings.Contains(round, `"NOTE":""`) || !strings.Contains(round, `"REFERENCE":null`) || !strings.Contains(round, `"TEXT":"Consulting \u003chours\u003e"`) {
			t.Errorf("ROUNDTRIP = %s", round)
		}

		// An order is read into a structure and stored.
		mustCall(t, k, "ACME", "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY", `"DE"`, `"Germany"`)
		mustCallAs(t, k, companyAdmin(t, k, "ACME"), "/TRFIN/COMPANY_SERVICE", "CREATE", `"C1"`, `"Acme"`, `"DE"`)
		row := mustCall(t, k, "ACME", O, "IMPORT", `"<order id=\"O-1\" company=\"C1\" date=\"2026-11-07\"><amount>12.5</amount><note/></order>"`)
		if !strings.Contains(row, `"AMOUNT":"12.50"`) || !strings.Contains(row, `"NOTE":""`) {
			t.Errorf("IMPORT = %s", row)
		}
		mustCall(t, k, "ACME", O, "IMPORT", `"<order id=\"O-2\" company=\"C1\" date=\"2026-11-08\"><amount>3</amount></order>"`)
		if got := mustCall(t, k, "ACME", O, "FIND", `"O-2"`); !strings.Contains(got, `"NOTE":null`) || !strings.Contains(got, `"ORDERED_ON":"2026-11-08"`) {
			t.Errorf("FIND O-2 = %s", got)
		}
		_, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, O, "IMPORT", `"<order id=\"O-3\"/>"`)
		if exceptionClass(err) != "/TRBASE/TRANSFORM_RESULT_INVALID" {
			t.Errorf("IMPORT of an incomplete order: %v", err)
		}
		if got := text(t, mustCall(t, k, "ACME", O, "WRONG", `"<order id=\"O-4\" company=\"C1\" date=\"2026-11-08\"><amount>3</amount></order>"`)); !strings.Contains(got, "/TRFIN/COMPANIES has no component <ORDER_ID>") &&
			!strings.Contains(got, "<NAME> is missing") {
			t.Errorf("WRONG = %s", got)
		}

		// A template from an asset repository, once and compiled.
		if got := mustCall(t, k, "ACME", T, "RENDER_ONCE", `"R-7"`, `"99.95"`, `"de"`); !strings.Contains(got, "Rechnung R-7") || !strings.Contains(got, "99.95") {
			t.Errorf("RENDER_ONCE = %s", got)
		}
		all := mustCall(t, k, "ACME", T, "RENDER_ALL", `["R-1","R-2","R-3"]`, `"en"`)
		if strings.Count(all, "R-") != 3 || !strings.Contains(all, "R-3") {
			t.Errorf("RENDER_ALL = %s", all)
		}
	})
}

// text decodes a JSON string.
func text(t *testing.T, s string) string {
	t.Helper()
	var out string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	return out
}

// TestTransformChecks rejects code with an unknown parameter and an
// upgrade that removes a transformation that code uses.
func TestTransformChecks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		dir := testutil.CopyExample(t, "TRFIN")
		renderer := filepath.Join(dir, "src", "INVOICE_RENDERER.class")
		src, _ := os.ReadFile(renderer)
		bad := strings.Replace(string(src), `parameters LANG = "de"`, `parameters LANGUAGE = "de"`, 1)
		testutil.WriteFile(t, dir, "src/INVOICE_RENDERER.class", []byte(bad))
		_, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, dir)}, kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "/TRFIN/INVOICE_HTML has no top-level xsl:param LANGUAGE; its parameters are LANG") {
			t.Fatalf("unknown parameter: %v", err)
		}

		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		without := testutil.CopyExample(t, "TRFIN")
		_ = os.Remove(filepath.Join(without, "transformations", "ORDER_IMPORT.xsl"))
		_ = os.Remove(filepath.Join(without, "transformations", "ORDER_IMPORT.json"))
		pkg := testutil.LoadDir(t, without)
		pkg.Manifest.Version = "1.1.0"
		_, err = k.Upgrade(ctx, pkg, kernel.Options{})
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "/TRFIN/ORDER_IMPORT is not a transformation") {
			t.Fatalf("upgrade without a used transformation: %v", err)
		}
	})
}

// TestJSON writes structures and arrays with null, "" and DEC as JSON with
// /TRBASE/JSON and reads them back.
func TestJSON(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.Example(t, "TRFIN")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const J = "/TRFIN/INVOICE_JSON"
		js := text(t, mustCall(t, k, "ACME", J, "TO_JSON", invoice))
		for _, part := range []string{`"NOTE":""`, `"REFERENCE":null`, `"TOTAL":"150.50"`, `"AMOUNT":"50.50"`, `"POSITION":1`} {
			if !strings.Contains(js, part) {
				t.Errorf("TO_JSON lacks %s: %s", part, js)
			}
		}
		back := mustCall(t, k, "ACME", J, "FROM_JSON", strconvQuote(js))
		if !strings.Contains(back, `"NOTE":""`) || !strings.Contains(back, `"REFERENCE":null`) || !strings.Contains(back, `"TOTAL":"150.50"`) {
			t.Errorf("FROM_JSON = %s", back)
		}
		all := mustCall(t, k, "ACME", J, "ROUNDTRIP", "["+invoice+","+strings.Replace(invoice, `"NOTE":""`, `"NOTE":null`, 1)+"]")
		if strings.Count(all, `"INVOICE_ID":"R-1"`) != 2 || !strings.Contains(all, `"NOTE":""`) || !strings.Contains(all, `"NOTE":null`) {
			t.Errorf("ROUNDTRIP = %s", all)
		}
		if got := text(t, mustCall(t, k, "ACME", J, "INVALID", `"{\"INVOICE_ID\":1}"`)); !strings.HasPrefix(got, "invalid:") {
			t.Errorf("INVALID = %s", got)
		}
		if got := text(t, mustCall(t, k, "ACME", J, "INVALID", `"not json"`)); !strings.Contains(got, "no JSON") {
			t.Errorf("INVALID of text = %s", got)
		}
	})
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
