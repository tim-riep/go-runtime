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

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// configurationFiles are the files of TRCONF: currencies with their
// texts as a business object of configuration without behavior class and
// authorization of its own, roles for its maintenance and a class that
// maintains it with entity statements.
func configurationFiles(t *testing.T, class string) map[string]string {
	t.Helper()
	api, errs := syntax.Format("API.class", class)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return map[string]string{
		"manifest.json": `{ "id": "TRCONF", "version": "1.0.0", "requires": [{ "id": "TRBASE" }], "languageVersion": 1 }`,
		"schema/tables/CURRENCIES.json": `{ "name": "/TRCONF/CURRENCIES", "tenantSpecific": true, "dataClass": "configuration", "fields": [
			{ "name": "CODE", "domain": "/TRBASE/NAME" }, { "name": "NAME", "domain": "/TRBASE/TEXT" } ], "primaryKey": ["CODE"] }`,
		"schema/tables/CURRENCY_NOTES.json": `{ "name": "/TRCONF/CURRENCY_NOTES", "tenantSpecific": true, "dataClass": "configuration", "fields": [
			{ "name": "CODE", "domain": "/TRBASE/NAME" }, { "name": "NOTE_NO", "domain": "/TRBASE/COUNTER" }, { "name": "TEXT", "domain": "/TRBASE/TEXT" } ],
			"primaryKey": ["CODE", "NOTE_NO"],
			"foreignKeys": [{ "name": "CURRENCY", "table": "/TRCONF/CURRENCIES", "fields": [{ "field": "CODE", "references": "CODE" }] }] }`,
		"schema/business-objects/CURRENCY.json": `{ "name": "/TRCONF/CURRENCY", "description": { "en": "Currency" }, "dataClass": "configuration",
			"root": "/TRCONF/CURRENCIES",
			"entities": {
				"/TRCONF/CURRENCIES": { "operations": ["create", "update", "delete"], "fields": { "NAME": { "mandatory": true } } },
				"/TRCONF/CURRENCY_NOTES": { "parent": "/TRCONF/CURRENCIES", "foreignKey": "CURRENCY", "operations": ["create", "delete"] }
			} }`,
		"schema/roles/CREATOR.json": `{ "name": "/TRCONF/CREATOR", "texts": { "en": { "label": "Creates currencies" } }, "authorizations": [
			{ "object": "/TRBASE/CONFIGURATION", "fields": { "BUSINESS_OBJECT": [{ "value": "/TRCONF/CURRENCY" }],
				"DATA_CLASS": [{ "value": "configuration" }], "ACTIVITY": [{ "value": "CREATE" }] } } ] }`,
		"schema/roles/RUNTIME.json": `{ "name": "/TRCONF/RUNTIME", "texts": { "en": { "label": "Maintains configuration at runtime" } }, "authorizations": [
			{ "object": "/TRBASE/CONFIGURATION", "fields": { "BUSINESS_OBJECT": [{ "pattern": "*" }],
				"DATA_CLASS": [{ "value": "runtimeConfiguration" }], "ACTIVITY": [{ "pattern": "*" }] } } ] }`,
		"src/API.class": api,
	}
}

const configurationAPI = `public class /TRCONF/API

public static method ADD(code: STRING, name: STRING) returns /TRBASE/ENTITY_RESULT
  var rows: array of /TRCONF/CURRENCIES = []
  var r: /TRCONF/CURRENCIES
  r.CODE = code
  r.NAME = name
  rows.APPEND(r)
  var notes: array of /TRCONF/CURRENCY_NOTES = []
  var n: /TRCONF/CURRENCY_NOTES
  n.CODE = code
  n.NOTE_NO = 1
  n.TEXT = "first"
  notes.APPEND(n)
  modify business object /TRCONF/CURRENCY
    create /TRCONF/CURRENCIES fields (CODE, NAME) from rows
    create /TRCONF/CURRENCY_NOTES by parent fields (NOTE_NO, TEXT) from notes
    result outcome
  end
  if outcome.FAILURES is not empty
    rollback business objects
    return outcome
  end
  commit business objects result saved
  return saved
end

public static method REMOVE(code: STRING) returns /TRBASE/ENTITY_RESULT
  var keys: array of key of /TRCONF/CURRENCIES = []
  var k: key of /TRCONF/CURRENCIES
  k.CODE = code
  keys.APPEND(k)
  modify business object /TRCONF/CURRENCY
    delete /TRCONF/CURRENCIES keys keys
    result outcome
  end
  return outcome
end

end
`

// TestConfigurationMaintenance maintains a business object of
// configuration with entity statements, checked with
// /TRBASE/CONFIGURATION for the name and data class of the business
// object, also for its dependent entities.
func TestConfigurationMaintenance(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, configurationFiles(t, configurationAPI))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		users := map[string]kernel.ExecOptions{}
		for user, role := range map[string]string{"creator": "/TRCONF/CREATOR", "runtime": "/TRCONF/RUNTIME"} {
			if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
			users[user] = kernel.ExecOptions{Tenant: "ACME", User: model.Some(user)}
		}
		const api = "/TRCONF/API"

		// Only the activity and data class of the role pass.
		r := result(t, mustCallAs(t, k, users["runtime"], api, "ADD", `"EUR"`, `"Euro"`))
		if len(r["FAILURES"]) != 2 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" {
			t.Errorf("runtime configuration only: %v", r)
		}
		r = result(t, mustCallAs(t, k, users["creator"], api, "ADD", `"EUR"`, `"Euro"`))
		if len(r["FAILURES"]) != 0 || count(t, k, "/TRCONF/CURRENCIES") != 1 || count(t, k, "/TRCONF/CURRENCY_NOTES") != 1 {
			t.Errorf("creator: %v", r)
		}
		r = result(t, mustCallAs(t, k, users["creator"], api, "REMOVE", `"EUR"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" {
			t.Errorf("delete without the activity: %v", r)
		}
		trace, err := k.LastFailedCheck(ctx, "creator", "ACME")
		if err != nil || trace == nil || trace.Object != model.ConfigurationObject ||
			string(trace.Values[model.ConfigurationBusinessObject]) != `"/TRCONF/CURRENCY"` || string(trace.Values[model.ConfigurationDataClass]) != `"configuration"` {
			t.Errorf("last failed check: %+v %v", trace, err)
		}

		d, err := k.BusinessObject(ctx, "/TRCONF/CURRENCY")
		if err != nil || d.DataClass != model.Configuration || d.BehaviorClass.Valid() ||
			d.Root.AuthorizationSource != model.Some(model.AuthorizationConfiguration) || d.Root.Authorization.Object != model.ConfigurationObject ||
			d.Root.AuthorizationConstants[model.ConfigurationBusinessObject] != "/TRCONF/CURRENCY" {
			t.Errorf("description: %+v %v", d, err)
		}
	})
}

// TestConfigurationTablesProtected rejects code that writes a table of a
// business object of configuration directly.
func TestConfigurationTablesProtected(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		class := configurationAPI[:strings.LastIndex(configurationAPI, "end\n")] + `public static method WRITE(code: STRING)
  var r: /TRCONF/CURRENCIES
  r.CODE = code
  r.NAME = code
  insert r
end

end
`
		_, err := k.Install(ctx, []*model.Package{writePackage(t, configurationFiles(t, class))}, kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "/TRCONF/CURRENCIES is an entity of business object /TRCONF/CURRENCY; change it with modify business object instead of insert") {
			t.Errorf("insert into a table of configuration: %v", err)
		}
	})
}

// TestNoSettingTables checks that a new database has no native tables of
// settings, which business objects of configuration replace.
func TestNoSettingTables(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		initialized(t, k)
		for _, table := range []string{"/RT/SETTINGS", "/RT/SETTING_TEXTS", "/RT/SETTING_TABLES", "/RT/SETTING_EXTENSIONS"} {
			var n int
			if err := k.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM "`+table+`"`).Scan(&n); err == nil {
				t.Errorf("%s exists", table)
			}
		}
	})
}
