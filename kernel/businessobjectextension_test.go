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
	"slices"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
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

// TestBusinessObjectExtensionCompiler checks the behavior class of an
// extension, what it changes in local mode and who may use what the
// extension adds.
func TestBusinessObjectExtensionCompiler(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, boFiles("1.0.0", orderBO(orderRoot), orderBehavior))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		install := func(members string) error {
			_, err := k.Install(ctx, []*model.Package{writePackage(t, extensionFiles(t, "1.0.0", extensionJSON, members, nil))}, kernel.Options{DryRun: true})
			return err
		}
		const rate = "public method RATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\nend\n"
		body := func(statements string) string {
			return strings.Replace(extensionBehavior, rate, "public method RATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\n"+statements+"end\n", 1)
		}
		const rows = "var rows: array of /TRBO/ORDERS = []\nvar labels: array of /TRBX/LABELS = []\n"
		for _, c := range []struct{ name, members, want string }{
			{"missing method", strings.Replace(extensionBehavior, rate, "", 1),
				"/TRBX/ORDER_BEHAVIOR needs the method public method RATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE) for /TRBO/ORDERS"},
			{"wrong method", strings.Replace(extensionBehavior, "public method RERATE(keys", "public static method RERATE(keys", 1),
				"needs the method public method RERATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)"},
			{"field of the base in local mode", body(rows + "modify business object /TRBO/ORDER in local mode\nupdate /TRBO/ORDERS fields (NOTE) from rows\nend\n"),
				"in local mode the extension TRBX changes only its own fields; NOTE belongs to TRBO"},
			{"creation of the base in local mode", body(rows + "modify business object /TRBO/ORDER in local mode\ncreate /TRBO/ORDERS fields (ORDER_ID, NAME) from rows\nend\n"),
				"in local mode the extension TRBX does not create instances of /TRBO/ORDERS, which belongs to TRBO"},
			{"action of the base in local mode", body("var closing: array of key of /TRBO/ORDERS = []\nvar reason: /TRBO/REASON\n" +
				"modify business object /TRBO/ORDER in local mode\nexecute /TRBO/ORDERS.CLOSE keys closing parameter reason\nend\n"),
				"in local mode the extension TRBX runs only its own actions; CLOSE belongs to TRBO"},
			{"change in a validation", strings.Replace(extensionBehavior, "public method CHECK(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\n",
				"public method CHECK(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\n"+rows+
					"modify business object /TRBO/ORDER in local mode\nupdate /TRBO/ORDERS fields (/TRBX/RATING) from rows\nend\n", 1),
				"CHECK is a validation, which must not change the transaction buffer"},
		} {
			var ce *rterr.CompileError
			if err := install(c.members); !errors.As(err, &ce) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// Its own fields and entities it changes in local mode, the
		// actions of the base without.
		valid := body(rows + "modify business object /TRBO/ORDER in local mode\nupdate /TRBO/ORDERS fields (/TRBX/RATING) from rows\n" +
			"create /TRBX/LABELS by parent fields (TEXT) from labels\nend\n" +
			"var closing: array of key of /TRBO/ORDERS = []\nvar reason: /TRBO/REASON\n" +
			"modify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.CLOSE keys closing parameter reason\nend\n")
		if err := install(valid); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, extensionFiles(t, "1.0.0", extensionJSON, valid, nil))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}

		// Code of a package uses what the extension adds only when it
		// requires the extending package.
		user := func(id, requires string) map[string]string {
			src, errs := syntax.Format("USER.class", "public class /"+id+"/USER\n\npublic static method RUN(keys: array of key of /TRBO/ORDERS, labels: array of /TRBX/LABELS)\n"+
				"modify business object /TRBO/ORDER\nexecute /TRBO/ORDERS./TRBX/RERATE keys keys\ncreate /TRBX/LABELS by parent fields (TEXT) from labels\nresult outcome\nend\nend\n\nend\n")
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			return map[string]string{
				"manifest.json":  `{ "id": "` + id + `", "version": "1.0.0", "requires": [{ "id": "TRBASE" }, { "id": "TRBO" }` + requires + `], "languageVersion": 1 }`,
				"src/USER.class": src,
			}
		}
		dir := t.TempDir()
		for name, text := range user("TRBY", "") {
			testutil.WriteFile(t, dir, name, []byte(text))
		}
		_, err := kernel.LoadDir(dir)
		var nv *rterr.NamespaceViolation
		if !errors.As(err, &nv) || !strings.Contains(err.Error(), "reference /TRBX/RERATE") || !strings.Contains(err.Error(), "reference /TRBX/LABELS") {
			t.Errorf("use without the extending package: %v", err)
		}
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, user("TRBZ", `, { "id": "TRBX" }`))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		d, err := k.BusinessObject(ctx, "/TRBO/ORDER")
		if err != nil {
			t.Fatal(err)
		}
		uses := map[string]bool{}
		for _, u := range d.CodeUses {
			uses[string(u.Package)+" "+u.Kind+" "+u.Object+" "+u.Member.Or("")] = true
		}
		for _, want := range []string{"TRBZ action /TRBO/ORDERS /TRBX/RERATE", "TRBZ entity /TRBX/LABELS ", "TRBX business-object-extension /TRBO/ORDER "} {
			if !uses[want] {
				t.Errorf("use %q missing in %v", want, uses)
			}
		}
	})
}

// runtimeExtension is extensionJSON with an action that an authorization
// object of TRBX checks.
var runtimeExtension = strings.Replace(extensionJSON, `"actions": [{ "name": "/TRBX/RERATE", "result": "self", "activity": "UPDATE" }]`,
	`"actions": [{ "name": "/TRBX/RERATE", "result": "self", "activity": "UPDATE" },
		{ "name": "/TRBX/PROMOTE", "activity": "EXECUTE", "authorization": { "object": "/TRBX/PROMOTION", "fields": { "ORDER_NAME": "NAME" } } }]`, 1)

// runtimeBehavior rates new orders on save, rejects the name "bad" and
// rates again with its action, all in local mode.
const runtimeBehavior = `
public method RATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  SET_RATING(keys, 5)
  response.GLOBAL_MESSAGE(/TRBASE/MESSAGE_SEVERITY.INFORMATION, "rated by TRBX")
end
public method CHECK(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  var rows = read business object /TRBO/ORDER in local mode
    /TRBO/ORDERS fields (NAME) keys keys
  end
  for i, row in rows
    if (row.NAME default "") = "bad"
      response.ERROR(i, "TRBX rejects the name")
    end
  end
end
public method RERATE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  SET_RATING(keys, 9)
end
public method PROMOTE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  response.GLOBAL_MESSAGE(/TRBASE/MESSAGE_SEVERITY.SUCCESS, "promoted")
end
private method SET_RATING(keys: array of key of /TRBO/ORDERS, rating: INT4)
  var rows: array of /TRBO/ORDERS = []
  for key in keys
    var r: /TRBO/ORDERS
    r.ORDER_ID = key.ORDER_ID
    r./TRBX/RATING = rating
    rows.APPEND(r)
  end
  modify business object /TRBO/ORDER in local mode
    update /TRBO/ORDERS fields (/TRBX/RATING) from rows
  end
end
`

// runtimeAPI works on /TRBO/ORDER with what TRBX adds.
const runtimeAPI = `public class /TRBX/API

public static method RATING(id: STRING) returns STRING
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  var rows = read business object /TRBO/ORDER
    /TRBO/ORDERS fields (/TRBX/RATING) keys keys
  end
  for row in rows
    return "{row./TRBX/RATING default 0}"
  end
  return "none"
end

public static method RUN(id: STRING, action: STRING) returns /TRBASE/ENTITY_RESULT
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  if action = "RERATE"
    modify business object /TRBO/ORDER
      execute /TRBO/ORDERS./TRBX/RERATE keys keys
      result outcome
    end
    commit business objects result saved
    saved.FAILURES = outcome.FAILURES
    return saved
  end
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS./TRBX/PROMOTE keys keys
    result outcome
  end
  return outcome
end

public static method LABEL(id: STRING, text: STRING) returns /TRBASE/ENTITY_RESULT
  var labels: array of /TRBX/LABELS = []
  var l: /TRBX/LABELS
  l.ORDER_ID = id
  l.TEXT = text
  labels.APPEND(l)
  modify business object /TRBO/ORDER
    create /TRBX/LABELS by parent fields (TEXT) from labels
    result outcome
  end
  if outcome.FAILURES is not empty
    return outcome
  end
  commit business objects result saved
  return saved
end

end
`

// followerFiles are the files of TRBA, which extends /TRBO/ORDER after
// TRBX because it requires TRBX, although its ID comes first.
func followerFiles(t *testing.T) map[string]string {
	t.Helper()
	class, errs := syntax.Format("FOLLOW.class", `public class /TRBA/FOLLOW

public method FOLLOW(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  response.GLOBAL_MESSAGE(/TRBASE/MESSAGE_SEVERITY.INFORMATION, "followed by TRBA")
end

end
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return map[string]string{
		"manifest.json":    `{ "id": "TRBA", "version": "1.0.0", "requires": [{ "id": "TRBASE" }, { "id": "TRBO" }, { "id": "TRBX" }], "languageVersion": 1 }`,
		"src/FOLLOW.class": class,
		"schema/business-object-extensions/ORDER.json": `{ "businessObject": "/TRBO/ORDER", "behaviorClass": "/TRBA/FOLLOW", "entities": {
			"/TRBO/ORDERS": { "determinations": [{ "name": "/TRBA/FOLLOW", "when": "save", "on": ["create"] }] } } }`,
	}
}

// TestBusinessObjectExtensionRuntime runs the behavior of extensions with
// that of the business object: determinations of the extensions after
// those of the business object in the order of their packages,
// validations whose failures name the extending package, actions checked
// with the authorization of the entity or an authorization object of the
// extending package, and dependent entities that go with their parent.
func TestBusinessObjectExtensionRuntime(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{boPackage(t, "1.0.0", orderRoot)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		api, errs := syntax.Format("API.class", runtimeAPI)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		files := extensionFiles(t, "1.0.0", runtimeExtension, runtimeBehavior, map[string]string{
			"src/API.class": api,
			"schema/authorization-objects/PROMOTION.json": `{ "name": "/TRBX/PROMOTION", "texts": { "en": { "label": "Promotion" } },
				"fields": [{ "name": "ORDER_NAME", "domain": "/TRBASE/NAME" }, { "name": "ACTIVITY" }], "activities": ["EXECUTE"] }`,
			"schema/roles/RATER.json": `{ "name": "/TRBX/RATER", "texts": { "en": { "label": "Rater" } }, "authorizations": [
				{ "object": "/TRBASE/USER_ADMIN", "fields": { "USER_NAME": [{ "pattern": "*" }], "ACTIVITY": [{ "value": "READ" }, { "value": "UPDATE" }] } } ] }`,
			"schema/roles/PROMOTER.json": `{ "name": "/TRBX/PROMOTER", "texts": { "en": { "label": "Promoter" } }, "authorizations": [
				{ "object": "/TRBX/PROMOTION", "fields": { "ORDER_NAME": [{ "value": "Alpha" }], "ACTIVITY": [{ "value": "EXECUTE" }] } } ] }`,
		})
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, files), writePackage(t, followerFiles(t))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		all := allAuthorized(t, k, "ACME", "anna")
		users := map[string]kernel.ExecOptions{}
		for user, role := range map[string]string{"rita": "/TRBX/RATER", "paul": "/TRBX/PROMOTER"} {
			if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
			users[user] = kernel.ExecOptions{Tenant: "ACME", User: model.Some(user)}
		}

		// The determination of TRBX fills its read-only field on save,
		// then that of TRBA follows; both name their package.
		r := result(t, mustCallAs(t, k, all, "/TRBO/API", "CREATE", `"A1"`, `"Alpha"`, "null"))
		if len(r["FAILURES"]) != 0 || len(r["MESSAGES"]) != 2 || r["MESSAGES"][0]["TEXT"] != "rated by TRBX" || r["MESSAGES"][0]["ORIGIN"] != "TRBX" ||
			r["MESSAGES"][1]["TEXT"] != "followed by TRBA" || r["MESSAGES"][1]["ORIGIN"] != "TRBA" {
			t.Errorf("create: %v", r)
		}
		expectCallAs(t, k, all, "/TRBX/API", "RATING", `"5"`, `"A1"`)

		// A validation of TRBX fails the save with its package.
		dropping := all
		dropping.End = kernel.RollbackUnlessCommitted
		r = result(t, mustCallAs(t, k, dropping, "/TRBO/API", "CREATE", `"B1"`, `"bad"`, "null"))
		if len(r["FAILURES"]) == 0 || r["FAILURES"][0]["REASON"] != "VALIDATION" || r["FAILURES"][0]["ORIGIN"] != "TRBX" ||
			len(r["MESSAGES"]) == 0 || r["MESSAGES"][len(r["MESSAGES"])-1]["TEXT"] != "TRBX rejects the name" || r["MESSAGES"][len(r["MESSAGES"])-1]["ORIGIN"] != "TRBX" {
			t.Errorf("validation: %v", r)
		}

		// RERATE takes the activity UPDATE of the authorization of the
		// orders, PROMOTE the authorization object of TRBX.
		r = result(t, mustCallAs(t, k, users["rita"], "/TRBX/API", "RUN", `"A1"`, `"RERATE"`))
		if len(r["FAILURES"]) != 0 {
			t.Errorf("rerate with the authorization of the orders: %v", r)
		}
		expectCallAs(t, k, all, "/TRBX/API", "RATING", `"9"`, `"A1"`)
		r = result(t, mustCallAs(t, k, users["rita"], "/TRBX/API", "RUN", `"A1"`, `"PROMOTE"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" || r["FAILURES"][0]["OPERATION"] != "/TRBX/PROMOTE" {
			t.Errorf("promote without its authorization: %v", r)
		}
		r = result(t, mustCallAs(t, k, users["paul"], "/TRBX/API", "RUN", `"A1"`, `"PROMOTE"`))
		if len(r["FAILURES"]) != 0 || len(r["MESSAGES"]) != 1 || r["MESSAGES"][0]["ORIGIN"] != "TRBX" {
			t.Errorf("promote: %v", r)
		}
		r = result(t, mustCallAs(t, k, users["paul"], "/TRBX/API", "RUN", `"A1"`, `"RERATE"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" {
			t.Errorf("rerate without the authorization of the orders: %v", r)
		}

		// Labels are created below their order and deleted with it, also
		// by code of TRBO, which knows nothing of them.
		r = result(t, mustCallAs(t, k, all, "/TRBX/API", "LABEL", `"A1"`, `"urgent"`))
		if len(r["FAILURES"]) != 0 || count(t, k, "/TRBX/LABELS") != 1 {
			t.Errorf("label: %v", r)
		}
		r = result(t, mustCallAs(t, k, all, "/TRBO/API", "REMOVE", `"A1"`))
		if len(r["FAILURES"]) != 0 || count(t, k, "/TRBX/LABELS") != 0 || count(t, k, "/TRBO/ORDERS") != 0 {
			t.Errorf("remove: %v, %d labels", r, count(t, k, "/TRBX/LABELS"))
		}

		// Code sets the read-only field of the extension only through it.
		src, errs := syntax.Format("WRITER.class", "public class /TRBZ/WRITER\n\npublic static method RUN(rows: array of /TRBO/ORDERS)\n"+
			"modify business object /TRBO/ORDER\nupdate /TRBO/ORDERS fields (/TRBX/RATING) from rows\nend\nend\n\nend\n")
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		_, err := k.Install(ctx, []*model.Package{writePackage(t, map[string]string{
			"manifest.json":    `{ "id": "TRBZ", "version": "1.0.0", "requires": [{ "id": "TRBASE" }, { "id": "TRBO" }, { "id": "TRBX" }], "languageVersion": 1 }`,
			"src/WRITER.class": src,
		})}, kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "/TRBX/RATING is read-only") {
			t.Errorf("read-only field of the extension: %v", err)
		}
	})
}

// compiledPackages returns the packages a plan compiles.
func compiledPackages(p *plan.Plan) []string {
	var out []string
	if p == nil {
		return out
	}
	for _, c := range p.Changes {
		if c.Kind == plan.CompilePackage {
			out = append(out, c.Name)
		}
	}
	return out
}

// TestBusinessObjectExtensionLifecycle installs and removes extensions:
// without compiling the business object when the layout of its rows
// stays, with confirmation when the tables of its entities hold rows, and
// with a field that becomes mandatory for the instances saved after.
func TestBusinessObjectExtensionLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{boPackage(t, "1.0.0", orderRoot)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		all := allAuthorized(t, k, "ACME", "anna")
		mustCallAs(t, k, all, "/TRBO/API", "CREATE", `"A1"`, `"Alpha"`, "null")

		// Without a field for the orders the layout stays: only TRBX is
		// compiled, when it comes and when it goes.
		withoutField := strings.Replace(extensionJSON, `"fields": { "/TRBX/RATING": { "readOnly": true } },`, "", 1)
		files := extensionFiles(t, "1.0.0", withoutField, extensionBehavior, nil)
		delete(files, "schema/extensions/ORDERS.json")
		p, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := compiledPackages(p); len(got) != 1 || got[0] != "TRBX" {
			t.Errorf("compiled on install: %v\n%s", got, planText(p))
		}
		p, err = k.Uninstall(ctx, "TRBX", kernel.Options{Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := compiledPackages(p); len(got) != 0 {
			t.Errorf("compiled on uninstall: %v\n%s", got, planText(p))
		}
		// A field for the orders changes their layout: TRBO is compiled.
		p, err = k.Install(ctx, []*model.Package{writePackage(t, extensionFiles(t, "1.0.0", extensionJSON, extensionBehavior, nil))}, kernel.Options{})
		if err != nil || !slices.Contains(compiledPackages(p), "TRBO") {
			t.Fatalf("install with a field: %v %v", err, compiledPackages(p))
		}

		// The rating becomes mandatory: the order without one stays until
		// it is saved again.
		mandatory := strings.Replace(extensionJSON, `"/TRBX/RATING": { "readOnly": true }`, `"/TRBX/RATING": { "mandatory": true }`, 1)
		if _, err := k.Upgrade(ctx, writePackage(t, extensionFiles(t, "1.1.0", mandatory, extensionBehavior, nil)), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if n := count(t, k, "/TRBO/ORDERS"); n != 1 {
			t.Fatalf("%d orders", n)
		}
		r := result(t, mustCallAs(t, k, all, "/TRBO/API", "RENAME", `"A1"`, `"Alpha 2"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "MANDATORY" || !strings.Contains(r["MESSAGES"][0]["TEXT"].(string), "/TRBX/RATING") {
			t.Errorf("save without the mandatory field: %v", r)
		}

		// Uninstalling TRBX drops the labels, only when confirmed.
		if err := insert(t, k, "/TRBX/LABELS", values{model.TenantField: "ACME", "LABEL_ID": "0190b2a4-1f6e-7000-8000-000000000001", "ORDER_ID": "A1", "TEXT": "x"}); err != nil {
			t.Fatal(err)
		}
		var conf *rterr.ConfirmationRequired
		var dc *rterr.DestructiveChange
		if _, err := k.Uninstall(ctx, "TRBX", kernel.Options{}); !errors.As(err, &conf) && !errors.As(err, &dc) {
			t.Fatalf("uninstall with labels: %v", err)
		}
		if _, err := k.Uninstall(ctx, "TRBX", kernel.Options{Confirm: true, AllowDestructive: true}); err != nil {
			t.Fatal(err)
		}
		s, _ := k.Schema(ctx)
		if s.Tables["/TRBX/LABELS"] != nil || len(s.BusinessObjectExtensions) != 0 {
			t.Error("the extension is left")
		}
		r = result(t, mustCallAs(t, k, all, "/TRBO/API", "RENAME", `"A1"`, `"Alpha 3"`))
		if len(r["FAILURES"]) != 0 {
			t.Errorf("rename after the uninstallation: %v", r)
		}
		consistent(t, k)
	})
}
