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

package pkgload

import (
	"strings"
	"testing"
)

const stylesheet = `<xsl:stylesheet version="3.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:param name="LANG" select="'en'"/>
  <xsl:param name="MODE" required="yes"/>
  <xsl:template match="/"><xsl:param name="INNER"/><x/></xsl:template>
</xsl:stylesheet>
`

func TestTransformations(t *testing.T) {
	fsys := validFS()
	fsys["transformations/INVOICE_HTML.xsl"] = file(stylesheet)
	fsys["transformations/INVOICE_HTML.json"] = file(`{"name":"/TRX/INVOICE_HTML","texts":{"en":{"label":"Invoice"}},"output":"html"}`)
	pkg, err := Load(fsys, "mem")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Transformations) != 1 {
		t.Fatalf("transformations: %+v", pkg.Transformations)
	}
	tr := pkg.Transformations[0]
	if tr.Name != "/TRX/INVOICE_HTML" || len(tr.Params) != 2 || tr.Params[0].Name != "LANG" || tr.Params[0].Required || !tr.Params[1].Required ||
		tr.Texts["en"].Label != "Invoice" || tr.Output.Or("") != "html" {
		t.Errorf("transformation: %+v", tr)
	}

	for _, c := range []struct{ file, text, want string }{
		{"transformations/A.xsl", strings.Replace(stylesheet, `<xsl:param name="LANG" select="'en'"/>`, `<xsl:include href="/TRBASE/COMMON"/>`, 1), `line 2: xsl:include of "/TRBASE/COMMON" is not supported`},
		{"transformations/A.xsl", strings.Replace(stylesheet, `<xsl:param name="LANG" select="'en'"/>`, `<xsl:import href="common.xsl"/>`, 1), `xsl:import of "common.xsl"`},
		{"transformations/A.xsl", strings.Replace(stylesheet, `<xsl:param name="LANG" select="'en'"/>`, `<xsl:include href="https://example.com/x.xsl"/>`, 1), `https://example.com/x.xsl`},
		{"transformations/A.xsl", `<html/>`, "its root element is xsl:stylesheet or xsl:transform"},
		{"transformations/A.xsl", `<xsl:stylesheet`, "line 1"},
		{"transformations/B.json", `{"name":"/TRX/B"}`, "there is no transformations/B.xsl"},
	} {
		fsys := validFS()
		fsys[c.file] = file(c.text)
		_, err := Load(fsys, "mem")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", c.file, err, c.want)
		}
	}
}
