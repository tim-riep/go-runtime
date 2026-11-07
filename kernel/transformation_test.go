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
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

const greeting = `<xsl:stylesheet version="3.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:param name="GREETING" select="'Hello'"/>
  <xsl:output method="text"/>
  <xsl:template match="/"><xsl:value-of select="$GREETING"/>, <xsl:value-of select="/value"/></xsl:template>
</xsl:stylesheet>
`

// transformationPackage writes a package TRXF with the transformation
// /TRXF/GREETING.
func transformationPackage(t *testing.T, version, sheet string) string {
	dir := filepath.Join(t.TempDir(), "TRXF")
	testutil.WriteFile(t, dir, "manifest.json", []byte(`{"id":"TRXF","version":"`+version+`"}`))
	testutil.WriteFile(t, dir, "transformations/GREETING.xsl", []byte(sheet))
	testutil.WriteFile(t, dir, "transformations/GREETING.json", []byte(`{"name":"/TRXF/GREETING","texts":{"en":{"label":"Greeting"},"de":{"label":"Gruß"}},"output":"text"}`))
	return dir
}

// TestTransformationLifecycle installs, upgrades and removes a package
// with a transformation.
func TestTransformationLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		p, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, transformationPackage(t, "1.0.0", greeting))}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if change(p, plan.CompileTransformation, "/TRXF/GREETING") == nil {
			t.Errorf("plan:\n%s", p.Text())
		}
		s, _ := k.Schema(ctx)
		tr := s.Transformations["/TRXF/GREETING"]
		if tr == nil || tr.Owner != "TRXF" || len(tr.Params) != 1 || tr.Params[0].Name != "GREETING" || tr.Texts["de"].Label != "Gruß" ||
			tr.Output.Or("") != "text" || tr.Text != greeting {
			t.Fatalf("stored transformation: %+v", tr)
		}
		consistent(t, k)

		// A stylesheet that does not compile stops the upgrade.
		broken := strings.Replace(greeting, `select="/value"`, `select="(("`, 1)
		_, err = k.Upgrade(ctx, testutil.LoadDir(t, transformationPackage(t, "1.1.0", broken)), kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || len(ce.Diagnostics) != 1 || ce.Diagnostics[0].File != "transformations/GREETING.xsl" || ce.Diagnostics[0].Line != 4 ||
			ce.Diagnostics[0].Code != "xslt" {
			t.Fatalf("broken stylesheet: %v", err)
		}
		if s, _ := k.Schema(ctx); s.Transformations["/TRXF/GREETING"].Text != greeting {
			t.Error("the failed upgrade changed the transformation")
		}

		// An include of another stylesheet is rejected when loading.
		withInclude := strings.Replace(greeting, `<xsl:output method="text"/>`, `<xsl:include href="/TRBASE/IDENTITY"/>`, 1)
		if _, err := kernel.LoadDir(transformationPackage(t, "1.1.0", withInclude)); err == nil || !strings.Contains(err.Error(), "xsl:include") {
			t.Errorf("include: %v", err)
		}

		if _, err := k.Uninstall(ctx, "TRXF", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		if s, _ := k.Schema(ctx); s.Transformations["/TRXF/GREETING"] != nil {
			t.Error("the transformation is left")
		}
		consistent(t, k)
	})
}
