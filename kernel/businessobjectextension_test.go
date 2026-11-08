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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// extensionJSON is the extension of /TRBO/ORDER by TRBX: a rating of its
// own that is read-only, a determination that rates new orders, a
// validation of the name, an action that rates again and labels of
// orders as dependent entity.
const extensionJSON = `{ "businessObject": "/TRBO/ORDER", "behaviorClass": "/TRBX/ORDER_BEHAVIOR",
	"entities": {
		"/TRBO/ORDERS": {
			"fields": { "/TRBX/RATING": { "readOnly": true } },
			"determinations": [{ "name": "/TRBX/RATE", "when": "save", "on": ["create"] }],
			"validations": [{ "name": "/TRBX/CHECK", "on": ["create", "update"], "fields": ["NAME"] }],
			"actions": [{ "name": "/TRBX/RERATE", "result": "self", "activity": "UPDATE" }]
		},
		"/TRBX/LABELS": { "parent": "/TRBO/ORDERS", "foreignKey": "ORDER", "operations": ["create", "delete"], "numbering": "uuid" }
	} }`

// extensionBehavior are the methods of /TRBX/ORDER_BEHAVIOR for
// extensionJSON: the methods carry the names without namespace.
const extensionBehavior = `
public method RATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
end
public method CHECK(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
end
public method RERATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
end
`

// extensionFiles are the files of TRBX, which extends /TRBO/ORDER.
func extensionFiles(t *testing.T, version, extension, members string, more map[string]string) map[string]string {
	t.Helper()
	class, errs := syntax.Format("ORDER_BEHAVIOR.class", "public class /TRBX/ORDER_BEHAVIOR\n"+members+"\nend\n")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	files := map[string]string{
		"manifest.json":                 `{ "id": "TRBX", "version": "` + version + `", "requires": [{ "id": "TRBASE" }, { "id": "TRBO" }], "languageVersion": 1 }`,
		"schema/extensions/ORDERS.json": `{ "table": "/TRBO/ORDERS", "fields": [{ "name": "/TRBX/RATING", "domain": "/TRBASE/COUNTER" }] }`,
		"schema/tables/LABELS.json": `{ "name": "/TRBX/LABELS", "tenantSpecific": true, "dataClass": "application", "fields": [
			{ "name": "LABEL_ID", "domain": "/TRBASE/GUID" }, { "name": "ORDER_ID", "domain": "/TRBASE/NAME", "notNull": true },
			{ "name": "TEXT", "domain": "/TRBASE/TEXT" } ], "primaryKey": ["LABEL_ID"],
			"foreignKeys": [{ "name": "ORDER", "table": "/TRBO/ORDERS", "fields": [{ "field": "ORDER_ID", "references": "ORDER_ID" }] }] }`,
		"src/ORDER_BEHAVIOR.class": class,
	}
	if extension != "" {
		files["schema/business-object-extensions/ORDER.json"] = extension
	}
	for k, v := range more {
		files[k] = v
	}
	return files
}

// TestBusinessObjectExtensionRepository installs an extension, reads it
// back and applies the rules for upgrades of the extension and of the
// business object it extends.
func TestBusinessObjectExtensionRepository(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, boFiles("1.0.0", orderBO(orderRoot), orderBehavior))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		files := extensionFiles(t, "1.0.0", extensionJSON, extensionBehavior, nil)
		p, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(planText(p), "add-business-object-extension /TRBO/ORDER by TRBX (2 entities)") {
			t.Errorf("plan:\n%s", planText(p))
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		x := s.BusinessObjectExtensions[model.BusinessObjectExtensionKey{Package: "TRBX", BusinessObject: "/TRBO/ORDER"}]
		loaded := writePackage(t, files).BusinessObjectExtensions[0]
		loaded.Package = "TRBX"
		got, _ := json.Marshal(x)
		want, _ := json.Marshal(loaded)
		if string(got) != string(want) {
			t.Errorf("read back:\n got %s\nwant %s", got, want)
		}
		if bo, e := s.BusinessObjectOf("/TRBX/LABELS"); bo == nil || bo.Name != "/TRBO/ORDER" || e.Table != "/TRBX/LABELS" {
			t.Errorf("BusinessObjectOf an entity of an extension: %v %v", bo, e)
		}

		// A field that becomes mandatory leaves the rows as they are.
		mandatory := strings.Replace(extensionJSON, `"/TRBX/RATING": { "readOnly": true }`, `"/TRBX/RATING": { "readOnly": true, "mandatory": true }`, 1)
		p, err = k.Upgrade(ctx, writePackage(t, extensionFiles(t, "1.1.0", mandatory, extensionBehavior, nil)), kernel.Options{})
		if err != nil || !strings.Contains(planText(p), "/TRBX/RATING becomes mandatory: existing instances of /TRBO/ORDERS without a value stay") {
			t.Errorf("mandatory: %v\n%s", err, planText(p))
		}

		// The business object cannot forbid what the extension does.
		closed := strings.Replace(orderBO(orderRoot), `"behaviorClass": "/TRBO/ORDER_BEHAVIOR",`,
			`"behaviorClass": "/TRBO/ORDER_BEHAVIOR", "extensible": ["fieldProperties", "determinations", "validations", "actions"],`, 1)
		_, err = k.Upgrade(ctx, writePackage(t, boFiles("1.1.0", closed, orderBehavior)), kernel.Options{})
		if err == nil || !strings.Contains(err.Error(), "business object extension /TRBO/ORDER by TRBX") ||
			!strings.Contains(err.Error(), "/TRBO/ORDER does not allow extensions with entities on /TRBO/ORDERS") {
			t.Errorf("upgrade that breaks the extension: %v", err)
		}
		// Nor remove a field a validation of the extension triggers on.
		renamed := strings.Replace(extensionJSON, `"fields": ["NAME"]`, `"fields": ["NOTE"]`, 1)
		if _, err := k.Upgrade(ctx, writePackage(t, extensionFiles(t, "1.2.0", renamed, extensionBehavior, nil)), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
	})
}
