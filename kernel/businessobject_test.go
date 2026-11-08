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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// orderTables are the tables of the package TRBO: orders with items and
// notes, which have a UUID as key.
var orderTables = map[string]string{
	"schema/tables/ORDERS.json": `{ "name": "/TRBO/ORDERS", "tenantSpecific": true, "dataClass": "application", "fields": [
		{ "name": "ORDER_ID", "domain": "/TRBASE/NAME" }, { "name": "NAME", "domain": "/TRBASE/NAME" },
		{ "name": "STATUS", "domain": "/TRBASE/NAME" }, { "name": "NOTE", "domain": "/TRBASE/TEXT" },
		{ "name": "CREATED_BY", "domain": "/TRBASE/NAME" }, { "name": "CREATED_AT", "domain": "/TRBASE/TIMESTAMP" },
		{ "name": "CHANGED_BY", "domain": "/TRBASE/NAME" }, { "name": "CHANGED_AT", "domain": "/TRBASE/TIMESTAMP" } ],
		"primaryKey": ["ORDER_ID"] }`,
	"schema/tables/ITEMS.json": `{ "name": "/TRBO/ITEMS", "tenantSpecific": true, "dataClass": "application", "fields": [
		{ "name": "ORDER_ID", "domain": "/TRBASE/NAME" }, { "name": "ITEM_NO", "domain": "/TRBASE/COUNTER" },
		{ "name": "TEXT", "domain": "/TRBASE/TEXT" } ],
		"primaryKey": ["ORDER_ID", "ITEM_NO"],
		"foreignKeys": [{ "name": "ORDER", "table": "/TRBO/ORDERS", "fields": [{ "field": "ORDER_ID", "references": "ORDER_ID" }] }] }`,
	"schema/tables/NOTES.json": `{ "name": "/TRBO/NOTES", "tenantSpecific": true, "dataClass": "application", "fields": [
		{ "name": "NOTE_ID", "domain": "/TRBASE/GUID" }, { "name": "ORDER_ID", "domain": "/TRBASE/NAME", "notNull": true },
		{ "name": "TEXT", "domain": "/TRBASE/TEXT" } ],
		"primaryKey": ["NOTE_ID"],
		"foreignKeys": [{ "name": "ORDER", "table": "/TRBO/ORDERS", "fields": [{ "field": "ORDER_ID", "references": "ORDER_ID" }] }] }`,
	"schema/structures/REASON.json": `{ "name": "/TRBO/REASON", "texts": { "en": { "label": "Reason" } }, "components": [
		{ "name": "TEXT", "domain": "/TRBASE/TEXT" } ] }`,
}

// orderBO is the definition of /TRBO/ORDER with the JSON of the root
// entity after its operations.
func orderBO(root string) string {
	return `{ "name": "/TRBO/ORDER", "description": { "en": "Order", "de": "Auftrag" }, "root": "/TRBO/ORDERS",
		"behaviorClass": "/TRBO/ORDER_BEHAVIOR",
		"entities": {
			"/TRBO/ORDERS": { "operations": ["create", "update", "delete"]` + root + ` },
			"/TRBO/ITEMS": { "parent": "/TRBO/ORDERS", "foreignKey": "ORDER", "operations": ["create", "update", "delete"] },
			"/TRBO/NOTES": { "parent": "/TRBO/ORDERS", "foreignKey": "ORDER", "operations": ["create", "delete"], "numbering": "uuid" }
		} }`
}

const orderRoot = `, "numbering": "external",
	"fields": { "NAME": { "mandatory": true }, "STATUS": { "readOnly": true } },
	"administrative": { "createdBy": "CREATED_BY", "createdAt": "CREATED_AT", "changedBy": "CHANGED_BY", "changedAt": "CHANGED_AT" },
	"etag": "CHANGED_AT",
	"determinations": [{ "name": "SET_STATUS", "when": "modify", "on": ["create"] }],
	"validations": [{ "name": "CHECK_NAME", "on": ["create", "update"], "fields": ["NAME"] }],
	"actions": [{ "name": "CLOSE", "parameter": "/TRBO/REASON", "result": "self" }, { "name": "COUNT_OPEN", "instance": false, "modifies": false }],
	"authorization": { "object": "/TRBASE/USER_ADMIN", "fields": { "USER_NAME": "NAME" },
		"activities": { "read": "READ", "create": "CREATE", "update": "UPDATE", "delete": "DELETE", "CLOSE": "UPDATE", "COUNT_OPEN": "READ" } }`

// boFiles are the files of TRBO with the business object and a behavior
// class with the given members.
func boFiles(version, bo, members string) map[string]string {
	class := "public class /TRBO/ORDER_BEHAVIOR\n" + members + "\nend\n"
	if out, errs := syntax.Format("ORDER_BEHAVIOR.class", class); len(errs) == 0 {
		class = out
	}
	files := map[string]string{
		"manifest.json":                      `{ "id": "TRBO", "version": "` + version + `", "requires": [{ "id": "TRBASE" }], "languageVersion": 1 }`,
		"src/ORDER_BEHAVIOR.class":           class,
		"schema/business-objects/ORDER.json": bo,
	}
	for k, v := range orderTables {
		files[k] = v
	}
	return files
}

// TestBusinessObjectRepository installs a business object, reads it back
// and applies the rules for upgrades.
func TestBusinessObjectRepository(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		p, err := k.Install(ctx, []*model.Package{writePackage(t, boFiles("1.0.0", orderBO(orderRoot), ""))}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(planText(p), "create-business-object /TRBO/ORDER (root /TRBO/ORDERS, 3 entities)") {
			t.Errorf("plan:\n%s", planText(p))
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b := s.BusinessObjects["/TRBO/ORDER"]
		if b == nil || b.Owner != "TRBO" || b.Description["de"] != "Auftrag" || len(b.Entities) != 3 || b.Entities[0].Table != "/TRBO/ORDERS" ||
			b.Entities[1].Table != "/TRBO/ITEMS" || b.Entities[2].Numbering != model.NumberingUUID {
			t.Fatalf("read back: %+v", b)
		}
		// The definition reads back as it was loaded.
		loaded := writePackage(t, boFiles("1.0.0", orderBO(orderRoot), "")).BusinessObjects[0]
		for i := range loaded.Entities {
			got, _ := json.Marshal(b.Entities[i])
			want, _ := json.Marshal(loaded.Entities[i])
			if string(got) != string(want) {
				t.Errorf("entity %d:\n got %s\nwant %s", i, got, want)
			}
		}
		if bo, e := s.BusinessObjectOf("/TRBO/NOTES"); bo != b || e.Table != "/TRBO/NOTES" {
			t.Error("BusinessObjectOf")
		}

		// Allowed: another action and a stricter field.
		grown := strings.Replace(orderRoot, `"STATUS": { "readOnly": true }`, `"STATUS": { "readOnly": true }, "NOTE": { "readOnly": true }`, 1)
		grown = strings.Replace(grown, `{ "name": "COUNT_OPEN"`, `{ "name": "REOPEN" }, { "name": "COUNT_OPEN"`, 1)
		grown = strings.Replace(grown, `"COUNT_OPEN": "READ"`, `"COUNT_OPEN": "READ", "REOPEN": "UPDATE"`, 1)
		p, err = k.Upgrade(ctx, writePackage(t, boFiles("1.1.0", orderBO(grown), "")), kernel.Options{})
		if err != nil || !strings.Contains(planText(p), "alter-business-object /TRBO/ORDER (entity /TRBO/ORDERS: behavior)") {
			t.Fatalf("allowed upgrade: %v\n%s", err, planText(p))
		}
		for _, c := range []struct{ name, bo, want string }{
			{"numbering", strings.Replace(orderBO(grown), `"/TRBO/NOTES": { "parent": "/TRBO/ORDERS", "foreignKey": "ORDER", "operations": ["create", "delete"], "numbering": "uuid" }`,
				`"/TRBO/NOTES": { "parent": "/TRBO/ORDERS", "foreignKey": "ORDER", "operations": ["create", "delete"] }`, 1), "entity /TRBO/NOTES: numbering uuid -> external"},
		} {
			_, err := k.Upgrade(ctx, writePackage(t, boFiles("1.2.0", c.bo, "")), kernel.Options{})
			var dc *rterr.DestructiveChange
			if !errors.As(err, &dc) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// Removing the business object is destructive.
		files := boFiles("2.0.0", "", "")
		delete(files, "schema/business-objects/ORDER.json")
		_, err = k.Upgrade(ctx, writePackage(t, files), kernel.Options{})
		var dc *rterr.DestructiveChange
		if !errors.As(err, &dc) || !strings.Contains(err.Error(), "drop-business-object /TRBO/ORDER") {
			t.Fatalf("removal: %v", err)
		}
		if _, err := k.Upgrade(ctx, writePackage(t, files), kernel.Options{AllowDestructive: true}); err != nil {
			t.Fatal(err)
		}
		consistent(t, k)
	})
}
