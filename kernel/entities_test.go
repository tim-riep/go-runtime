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
	"github.com/tim-riep/go-runtime/vm"
)

// orderAPI is the class /TRBO/API, which works on /TRBO/ORDER with entity
// statements.
const orderAPI = `public class /TRBO/API

public static method CREATE(id: STRING, name: STRING, note: STRING?) returns /TRBASE/ENTITY_RESULT
  var orders: array of new of /TRBO/ORDERS = []
  var o: new of /TRBO/ORDERS
  o.LINK = "o1"
  o.ORDER_ID = id
  o.NAME = name
  o.NOTE = note
  orders.APPEND(o)
  var notes: array of new of /TRBO/NOTES = []
  var n: new of /TRBO/NOTES
  n.PARENT_LINK = "o1"
  n.TEXT = "first note"
  notes.APPEND(n)
  var items: array of new of /TRBO/ITEMS = []
  var i: new of /TRBO/ITEMS
  i.PARENT_LINK = "o1"
  i.ITEM_NO = 1
  i.TEXT = "item"
  items.APPEND(i)
  modify business object /TRBO/ORDER
    create /TRBO/ORDERS fields (ORDER_ID, NAME, NOTE) from orders
    create /TRBO/NOTES by parent fields (TEXT) from notes
    create /TRBO/ITEMS by parent fields (ITEM_NO, TEXT) from items
    result outcome
  end
  if outcome.FAILURES is not empty
    return outcome
  end
  commit business objects result saved
  saved.CREATED = outcome.CREATED
  return saved
end

public static method ADD_ITEM(id: STRING, no: INT8) returns /TRBASE/ENTITY_RESULT
  var items: array of /TRBO/ITEMS = []
  var i: /TRBO/ITEMS
  i.ORDER_ID = id
  i.ITEM_NO = no
  i.TEXT = "added"
  items.APPEND(i)
  modify business object /TRBO/ORDER
    create /TRBO/ITEMS by parent fields (ITEM_NO, TEXT) from items
    result outcome
  end
  if outcome.FAILURES is not empty
    return outcome
  end
  return SAVE()
end

public static method STAGE_NOTE(id: STRING, note: STRING?)
  var rows: array of /TRBO/ORDERS = []
  var r: /TRBO/ORDERS
  r.ORDER_ID = id
  r.NOTE = note
  rows.APPEND(r)
  modify business object /TRBO/ORDER
    update /TRBO/ORDERS fields (NOTE) from rows
  end
end

public static method SAVE() returns /TRBASE/ENTITY_RESULT
  commit business objects result saved
  if saved.FAILURES is not empty
    rollback business objects
  end
  return saved
end

public static method SET_NOTE(id: STRING, note: STRING?) returns /TRBASE/ENTITY_RESULT
  STAGE_NOTE(id, note)
  return SAVE()
end

public static method RENAME(id: STRING, name: STRING) returns /TRBASE/ENTITY_RESULT
  var rows: array of /TRBO/ORDERS = []
  var r: /TRBO/ORDERS
  r.ORDER_ID = id
  r.NAME = name
  rows.APPEND(r)
  modify business object /TRBO/ORDER
    update /TRBO/ORDERS fields (NAME) from rows
    result outcome
  end
  if outcome.FAILURES is not empty
    return outcome
  end
  return SAVE()
end

public static method REMOVE(id: STRING) returns /TRBASE/ENTITY_RESULT
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  modify business object /TRBO/ORDER
    delete /TRBO/ORDERS keys keys
    result outcome
  end
  if outcome.FAILURES is not empty
    return outcome
  end
  return SAVE()
end

public static method READ(id: STRING) returns array of /TRBO/ORDERS
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  return read business object /TRBO/ORDER
    /TRBO/ORDERS fields all keys keys
  end
end

public static method NAMES(id: STRING) returns STRING
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  var rows = read business object /TRBO/ORDER
    /TRBO/ORDERS fields (NAME) keys keys
    result outcome
  end
  var notes = read business object /TRBO/ORDER
    /TRBO/NOTES by parent fields (TEXT) keys keys
  end
  var out = "{rows.LENGTH()} {outcome.FAILURES.LENGTH()} {notes.LENGTH()}"
  for row in rows
    out = out + " " + (row.NAME default "none")
  end
  return out
end

public static method COMMIT_PENDING(id: STRING)
  STAGE_NOTE(id, "pending")
  commit
end

public static method LEAVE_PENDING(id: STRING)
  STAGE_NOTE(id, "left")
end

public static method LEAVE_EMPTY_NAME(id: STRING)
  var rows: array of /TRBO/ORDERS = []
  var r: /TRBO/ORDERS
  r.ORDER_ID = id
  r.NAME = ""
  rows.APPEND(r)
  modify business object /TRBO/ORDER
    update /TRBO/ORDERS fields (NAME) from rows
  end
end

public static method DISCARD(id: STRING) returns STRING
  STAGE_NOTE(id, "discarded")
  rollback business objects
  return READ(id)[1].NOTE default "none"
end

public static method FAIL_SAVE(id: STRING)
  STAGE_NOTE(id, "x")
  var rows: array of /TRBO/ORDERS = []
  var r: /TRBO/ORDERS
  r.ORDER_ID = id
  r.NAME = ""
  rows.APPEND(r)
  modify business object /TRBO/ORDER
    update /TRBO/ORDERS fields (NAME) from rows
  end
  commit business objects
end

end
`

// boPackage returns TRBO with the business object, its behavior class and
// the class /TRBO/API.
func boPackage(t *testing.T, version string) *model.Package {
	files := boFiles(version, orderBO(orderRoot), orderBehavior)
	api, errs := syntax.Format("API.class", orderAPI)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	files["src/API.class"] = api
	return writePackage(t, files)
}

func result(t *testing.T, out string) map[string][]map[string]any {
	t.Helper()
	var r map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	return r
}

// TestEntityStatements creates instances with dependent instances in one
// statement, changes only the given fields, deletes with all dependent
// instances, checks mandatory fields and ETags, fills administrative
// fields and keeps the buffer apart from the default session.
func TestEntityStatements(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{boPackage(t, "1.0.0")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const api = "/TRBO/API"
		user := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
		out, err := call(k, user, api, "CREATE", `"A1"`, `"Alpha"`, "null")
		if err != nil {
			t.Fatal(err)
		}
		r := result(t, out)
		if len(r["CREATED"]) != 3 || r["CREATED"][0]["KEY"] != `{"ORDER_ID":"A1"}` || r["CREATED"][0]["LINK"] != "o1" ||
			!strings.HasPrefix(r["CREATED"][1]["KEY"].(string), `{"NOTE_ID":"`) || r["CREATED"][2]["KEY"] != `{"ORDER_ID":"A1","ITEM_NO":1}` ||
			len(r["FAILURES"]) != 0 {
			t.Fatalf("create: %s", out)
		}
		if n := count(t, k, "/TRBO/NOTES"); n != 1 {
			t.Fatalf("%d notes", n)
		}
		if by := scalar(t, k, "/TRBO/ORDERS", "CREATED_BY", where(k, "ORDER_ID"), "A1"); by != "anna" {
			t.Errorf("created by %v", by)
		}
		if at := scalar(t, k, "/TRBO/ORDERS", "CHANGED_AT", where(k, "ORDER_ID"), "A1"); at == nil {
			t.Error("no time of the change")
		}
		// The key exists, a parent that does not exist fails.
		r = result(t, mustCall(t, k, "ACME", api, "CREATE", `"A1"`, `"Again"`, "null"))
		if len(r["FAILURES"]) != 3 || r["FAILURES"][0]["REASON"] != "ALREADY_EXISTS" || r["FAILURES"][1]["REASON"] != "PARENT_NOT_FOUND" {
			t.Errorf("duplicate: %v", r)
		}
		r = result(t, mustCall(t, k, "ACME", api, "ADD_ITEM", `"NOPE"`, "2"))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "PARENT_NOT_FOUND" || r["FAILURES"][0]["ENTITY"] != "/TRBO/ITEMS" {
			t.Errorf("missing parent: %v", r)
		}
		mustCall(t, k, "ACME", api, "ADD_ITEM", `"A1"`, "2")

		// Only the given fields change; null and "" are different values.
		mustCall(t, k, "ACME", api, "SET_NOTE", `"A1"`, `""`)
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":""`) || !strings.Contains(got, `"NAME":"Alpha"`) {
			t.Errorf("after an empty note: %s", got)
		}
		mustCall(t, k, "ACME", api, "SET_NOTE", `"A1"`, "null")
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":null`) {
			t.Errorf("after a null note: %s", got)
		}
		expectCall(t, k, "ACME", api, "NAMES", `"1 0 1 Alpha"`, `"A1"`)
		expectCall(t, k, "ACME", api, "NAMES", `"0 1 0"`, `"NOPE"`)

		// A mandatory field without value fails the save; nothing is
		// written.
		r = result(t, mustCall(t, k, "ACME", api, "RENAME", `"A1"`, `""`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "MANDATORY" || !strings.Contains(r["MESSAGES"][0]["TEXT"].(string), "NAME of /TRBO/ORDERS needs a value") {
			t.Errorf("mandatory: %v", r)
		}
		_, err = call(k, kernel.ExecOptions{Tenant: "ACME"}, api, "FAIL_SAVE", `"A1"`)
		var exc *vm.Exception
		if !errors.As(err, &exc) || exc.Class != "/RT/SAVE_FAILED" {
			t.Errorf("save without result: %v", err)
		}

		// commit of the default session with changes in the buffer.
		_, err = call(k, kernel.ExecOptions{Tenant: "ACME"}, api, "COMMIT_PENDING", `"A1"`)
		if !errors.As(err, &exc) || exc.Class != "/RT/SESSION_ERROR" || !strings.Contains(exc.Text, "use commit business objects") {
			t.Errorf("commit with changes: %v", err)
		}
		// rollback business objects drops the changes.
		expectCall(t, k, "ACME", api, "DISCARD", `"none"`, `"A1"`)

		// The end of the execution saves, or drops with rollback.
		mustCall(t, k, "ACME", api, "LEAVE_PENDING", `"A1"`)
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":"left"`) {
			t.Errorf("saved at the end: %s", got)
		}
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME", End: kernel.RollbackUnlessCommitted}, api, "STAGE_NOTE", `"A1"`, `"dropped"`); err != nil {
			t.Fatal(err)
		}
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":"left"`) {
			t.Errorf("dropped at the end: %s", got)
		}
		_, err = call(k, kernel.ExecOptions{Tenant: "ACME"}, api, "LEAVE_EMPTY_NAME", `"A1"`)
		var ns *rterr.BusinessObjectsNotSaved
		if !errors.As(err, &ns) || !strings.Contains(err.Error(), "MANDATORY") {
			t.Errorf("failed save at the end: %v", err)
		}
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NAME":"Alpha"`) {
			t.Errorf("after the failed save: %s", got)
		}

		// An ETag conflict: another execution changes the instance after
		// this one read it.
		err = k.Execute(ctx, kernel.ExecOptions{Tenant: "ACME"}, func(ctx context.Context, m *vm.Machine) error {
			if _, err := m.CallJSON(ctx, api, "STAGE_NOTE", []json.RawMessage{json.RawMessage(`"A1"`), json.RawMessage(`"mine"`)}); err != nil {
				return err
			}
			if _, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, api, "SET_NOTE", `"A1"`, `"theirs"`); err != nil {
				return err
			}
			out, err := m.CallJSON(ctx, api, "SAVE", nil)
			if err != nil {
				return err
			}
			if r := result(t, string(out)); len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "ETAG" {
				t.Errorf("ETag: %s", out)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":"theirs"`) {
			t.Errorf("after the conflict: %s", got)
		}

		// Delete removes the dependent instances.
		r = result(t, mustCall(t, k, "ACME", api, "REMOVE", `"A1"`))
		if len(r["FAILURES"]) != 0 {
			t.Fatalf("remove: %v", r)
		}
		for _, table := range []string{"/TRBO/ORDERS", "/TRBO/ITEMS", "/TRBO/NOTES"} {
			if n := count(t, k, table); n != 0 {
				t.Errorf("%s has %d rows", table, n)
			}
		}
		r = result(t, mustCall(t, k, "ACME", api, "REMOVE", `"A1"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_FOUND" {
			t.Errorf("remove again: %v", r)
		}
	})
}

// TestEntityStatementRules rejects entity statements that the definition
// of the business object does not allow.
func TestEntityStatementRules(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		head := "public class /TRBO/BAD\n\npublic static method RUN(orders: array of /TRBO/ORDERS, notes: array of new of /TRBO/NOTES, keys: array of key of /TRBO/ORDERS)\n"
		for _, c := range []struct{ name, body, want string }{
			{"unknown field", "modify business object /TRBO/ORDER\nupdate /TRBO/ORDERS fields (COLOR) from orders\nend\n", "/TRBO/ORDERS has no field COLOR"},
			{"read-only field", "modify business object /TRBO/ORDER\nupdate /TRBO/ORDERS fields (STATUS) from orders\nend\n",
				"STATUS is read-only; only the behavior class of the business object sets it in local mode"},
			{"administrative field", "modify business object /TRBO/ORDER\nupdate /TRBO/ORDERS fields (CHANGED_BY) from orders\nend\n", "the kernel sets CHANGED_BY when it saves"},
			{"key on update", "modify business object /TRBO/ORDER\nupdate /TRBO/ORDERS fields (ORDER_ID) from orders\nend\n", "ORDER_ID is a key field and cannot change"},
			{"key of uuid", "modify business object /TRBO/ORDER\ncreate /TRBO/NOTES by parent fields (NOTE_ID, TEXT) from notes\nend\n",
				"the kernel gives the key NOTE_ID of /TRBO/NOTES, which has numbering uuid"},
			{"key missing", "modify business object /TRBO/ORDER\ncreate /TRBO/ORDERS fields (NAME) from orders\nend\n",
				"the key field ORDER_ID of /TRBO/ORDERS is missing in the field list"},
			{"root by parent", "modify business object /TRBO/ORDER\ncreate /TRBO/ORDERS by parent fields (ORDER_ID) from orders\nend\n", "/TRBO/ORDERS is the root"},
			{"dependent without parent", "modify business object /TRBO/ORDER\ncreate /TRBO/NOTES fields (TEXT) from notes\nend\n", "create it by parent"},
			{"operation not allowed", "modify business object /TRBO/ORDER\nupdate /TRBO/NOTES fields (TEXT) from notes\nend\n", "/TRBO/NOTES does not allow update"},
			{"no entity", "modify business object /TRBO/ORDER\ndelete /TRBASE/AUDIT_LOG keys keys\nend\n", "/TRBASE/AUDIT_LOG is no entity of /TRBO/ORDER"},
			{"rows without the field", "modify business object /TRBO/ORDER\nupdate /TRBO/ORDERS fields (NAME) from keys\nend\n",
				"the rows of key of /TRBO/ORDERS have no component NAME"},
			{"local mode outside", "modify business object /TRBO/ORDER in local mode\ndelete /TRBO/ORDERS keys keys\nend\n",
				"in local mode is only allowed in the behavior class /TRBO/ORDER_BEHAVIOR of /TRBO/ORDER"},
			{"unknown business object", "modify business object /TRBO/NOPE\nend\n", "business object /TRBO/NOPE does not exist"},
			{"read of an unknown field", "var rows = read business object /TRBO/ORDER\n/TRBO/ORDERS fields (COLOR) keys keys\nend\n", "/TRBO/ORDERS has no field COLOR"},
			{"projection", "var rows = read business object /TRBO/ORDER\n/TRBO/ORDERS fields (NAME) keys keys\nend\nvar note = rows[1].NOTE\n", "NOTE"},
		} {
			files := boFiles("1.0.0", orderBO(orderRoot), orderBehavior)
			src, errs := syntax.Format("BAD.class", head+c.body+"end\nend\n")
			if len(errs) > 0 {
				t.Fatalf("%s: %v", c.name, errs)
			}
			files["src/BAD.class"] = src
			_, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{})
			var ce *rterr.CompileError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// Behavior classes neither commit business objects nor change the
		// buffer in validations.
		for _, c := range []struct{ name, behavior, want string }{
			{"commit in the behavior class", strings.Replace(orderBehavior, "public method COUNT_OPEN(response: /TRBASE/ENTITY_RESPONSE)\n",
				"public method COUNT_OPEN(response: /TRBASE/ENTITY_RESPONSE)\ncommit business objects\n", 1),
				"the behavior class of /TRBO/ORDER does not commit or roll back business objects"},
			{"change in a validation", strings.Replace(orderBehavior, "public method CHECK_NAME(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\n",
				"public method CHECK_NAME(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\nmodify business object /TRBO/ORDER in local mode\ndelete /TRBO/ORDERS keys keys\nend\n", 1),
				"CHECK_NAME is a validation, which must not change the transaction buffer"},
		} {
			_, err := k.Install(ctx, []*model.Package{writePackage(t, boFiles("1.0.0", orderBO(orderRoot), c.behavior))}, kernel.Options{})
			var ce *rterr.CompileError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
	})
}

// determinationRoot is the root entity of /TRBO/ORDER with determinations
// on both times and a validation.
const determinationRoot = `, "numbering": "external",
	"fields": { "NAME": { "mandatory": true }, "STATUS": { "readOnly": true }, "NOTE": { "readOnly": true } },
	"determinations": [
		{ "name": "SET_STATUS", "when": "modify", "on": ["create"] },
		{ "name": "MARK_NAME", "when": "modify", "on": ["create", "update"], "fields": ["NAME"] },
		{ "name": "STAMP_NOTE", "when": "save", "on": ["create", "update"] } ],
	"validations": [{ "name": "CHECK_NAME", "on": ["create", "update"], "fields": ["NAME"] }]`

// determinationBehavior are the methods of /TRBO/ORDER_BEHAVIOR for
// determinationRoot. MARK_NAME changes the field that triggers it.
const determinationBehavior = `
public method SET_STATUS(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  var rows: array of /TRBO/ORDERS = []
  for key in keys
    var r: /TRBO/ORDERS
    r.ORDER_ID = key.ORDER_ID
    r.STATUS = "new"
    rows.APPEND(r)
  end
  modify business object /TRBO/ORDER in local mode
    update /TRBO/ORDERS fields (STATUS) from rows
  end
end

public method MARK_NAME(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  var rows = read business object /TRBO/ORDER in local mode
    /TRBO/ORDERS fields all keys keys
  end
  var changed: array of /TRBO/ORDERS = []
  for row in rows
    var r = row
    r.NAME = (r.NAME default "") + "!"
    changed.APPEND(r)
  end
  modify business object /TRBO/ORDER in local mode
    update /TRBO/ORDERS fields (NAME) from changed
  end
end

public method STAMP_NOTE(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  var rows = read business object /TRBO/ORDER in local mode
    /TRBO/ORDERS fields all keys keys
  end
  var changed: array of /TRBO/ORDERS = []
  for row in rows
    var r = row
    r.NOTE = "saved " + (r.NAME default "") + " " + (r.STATUS default "none")
    changed.APPEND(r)
  end
  modify business object /TRBO/ORDER in local mode
    update /TRBO/ORDERS fields (NOTE) from changed
  end
  response.GLOBAL_MESSAGE(/TRBASE/MESSAGE_SEVERITY.INFORMATION, "stamped {rows.LENGTH()}")
end

public method CHECK_NAME(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)
  var rows = read business object /TRBO/ORDER in local mode
    /TRBO/ORDERS fields (NAME) keys keys
  end
  for i, row in rows
    if (row.NAME default "").STARTS_WITH("x")
      response.FIELD_ERROR(i, "NAME", "the name must not start with x")
    end
    if (row.NAME default "") = "boom!"
      var zero = 0
      var x = 1 / zero
    end
  end
end
`

// determinationAPI is /TRBO/API for determinationRoot.
const determinationAPI = `public class /TRBO/API

public static method CREATE(id: STRING, name: STRING) returns /TRBASE/ENTITY_RESULT
  var orders: array of new of /TRBO/ORDERS = []
  var o: new of /TRBO/ORDERS
  o.ORDER_ID = id
  o.NAME = name
  orders.APPEND(o)
  modify business object /TRBO/ORDER
    create /TRBO/ORDERS fields (ORDER_ID, NAME) from orders
  end
  commit business objects result saved
  if saved.FAILURES is not empty
    rollback business objects
  end
  return saved
end

public static method STAGE(id: STRING, name: STRING) returns STRING
  var orders: array of new of /TRBO/ORDERS = []
  var o: new of /TRBO/ORDERS
  o.ORDER_ID = id
  o.NAME = name
  orders.APPEND(o)
  modify business object /TRBO/ORDER
    create /TRBO/ORDERS fields (ORDER_ID, NAME) from orders
  end
  var row = READ(id)[1]
  rollback business objects
  return "{row.NAME default "none"} {row.STATUS default "none"} {row.NOTE default "none"}"
end

public static method KEEP_FAILED(id: STRING) returns STRING
  var orders: array of new of /TRBO/ORDERS = []
  var o: new of /TRBO/ORDERS
  o.ORDER_ID = id
  o.NAME = "xray"
  orders.APPEND(o)
  modify business object /TRBO/ORDER
    create /TRBO/ORDERS fields (ORDER_ID, NAME) from orders
  end
  commit business objects result saved
  var out = "{saved.FAILURES.LENGTH()} {saved.FAILURES[1].REASON} {saved.MESSAGES[2].FIELD default "none"}"
  var rows: array of /TRBO/ORDERS = []
  var r: /TRBO/ORDERS
  r.ORDER_ID = id
  r.NAME = "yankee"
  rows.APPEND(r)
  modify business object /TRBO/ORDER
    update /TRBO/ORDERS fields (NAME) from rows
  end
  commit business objects result again
  return out + " " + "{again.FAILURES.LENGTH()} " + (READ(id)[1].NAME default "none")
end

public static method RENAME(id: STRING, name: STRING) returns /TRBASE/ENTITY_RESULT
  var rows: array of /TRBO/ORDERS = []
  var r: /TRBO/ORDERS
  r.ORDER_ID = id
  r.NAME = name
  rows.APPEND(r)
  modify business object /TRBO/ORDER
    update /TRBO/ORDERS fields (NAME) from rows
  end
  commit business objects result saved
  if saved.FAILURES is not empty
    rollback business objects
  end
  return saved
end

public static method READ(id: STRING) returns array of /TRBO/ORDERS
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  return read business object /TRBO/ORDER
    /TRBO/ORDERS fields all keys keys
  end
end

end
`

// TestDeterminations runs determinations on modify and on save as their
// operations and fields trigger them, lets their changes in local mode
// trigger other determinations but not themselves, and fails the save
// for instances a validation rejects while the buffer stays.
func TestDeterminations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		files := boFiles("1.0.0", orderBO(determinationRoot), determinationBehavior)
		src, errs := syntax.Format("API.class", determinationAPI)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		files["src/API.class"] = src
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const api = "/TRBO/API"
		// On modify: SET_STATUS for the creation and MARK_NAME once for the
		// name, although it changes the name itself; STAMP_NOTE waits for
		// the save.
		expectCall(t, k, "ACME", api, "STAGE", `"Alpha! new none"`, `"A1"`, `"Alpha"`)

		r := result(t, mustCall(t, k, "ACME", api, "CREATE", `"A1"`, `"Alpha"`))
		if len(r["FAILURES"]) != 0 || len(r["MESSAGES"]) != 1 || r["MESSAGES"][0]["TEXT"] != "stamped 1" {
			t.Fatalf("create: %v", r)
		}
		got := mustCall(t, k, "ACME", api, "READ", `"A1"`)
		if !strings.Contains(got, `"NAME":"Alpha!"`) || !strings.Contains(got, `"STATUS":"new"`) || !strings.Contains(got, `"NOTE":"saved Alpha! new"`) {
			t.Errorf("after create: %s", got)
		}
		r = result(t, mustCall(t, k, "ACME", api, "RENAME", `"A1"`, `"Beta"`))
		if len(r["FAILURES"]) != 0 {
			t.Fatalf("rename: %v", r)
		}
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NAME":"Beta!"`) || !strings.Contains(got, `"NOTE":"saved Beta! new"`) {
			t.Errorf("after rename: %s", got)
		}

		// A validation fails the save and keeps the buffer: a later change
		// in the same execution saves it.
		expectCall(t, k, "ACME", api, "KEEP_FAILED", `"1 VALIDATION NAME 0 yankee!"`, `"X1"`)

		// A rename the validation rejects leaves the database as it was.
		r = result(t, mustCall(t, k, "ACME", api, "RENAME", `"A1"`, `"xenon"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "VALIDATION" || r["MESSAGES"][1]["FIELD"] != "NAME" ||
			r["MESSAGES"][1]["KEY"] != `{"ORDER_ID":"A1"}` {
			t.Errorf("rejected rename: %v", r)
		}
		if got := mustCall(t, k, "ACME", api, "READ", `"A1"`); !strings.Contains(got, `"NAME":"Beta!"`) {
			t.Errorf("after the rejected rename: %s", got)
		}

		// A runtime error in a behavior method fails its instances.
		r = result(t, mustCall(t, k, "ACME", api, "RENAME", `"A1"`, `"boom"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "BEHAVIOR" ||
			!strings.Contains(r["MESSAGES"][1]["TEXT"].(string), "/TRBO/ORDER_BEHAVIOR.CHECK_NAME: ") {
			t.Errorf("runtime error: %v", r)
		}
	})
}
