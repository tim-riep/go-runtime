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
	"fmt"
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

public static method CLOSE(id: STRING) returns STRING
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  var p: /TRBO/REASON
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS.CLOSE keys keys parameter p into closed
    execute /TRBO/ORDERS.COUNT_OPEN
    result outcome
  end
  var out = "{closed.LENGTH()}"
  for f in outcome.FAILURES
    out = out + " {f.OPERATION}:{f.REASON}"
  end
  return out
end

public static method COUNT() returns STRING
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS.COUNT_OPEN
    result outcome
  end
  commit business objects result saved
  return "{outcome.FAILURES.LENGTH()} {outcome.MESSAGES.LENGTH()} {saved.FAILURES.LENGTH()}"
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

// boPackage returns TRBO with the business object with the given root
// entity, its behavior class and the class /TRBO/API.
func boPackage(t *testing.T, version, root string) *model.Package {
	files := boFiles(version, orderBO(root), orderBehavior)
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
		if _, err := k.Install(ctx, []*model.Package{boPackage(t, "1.0.0", orderRoot)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const api = "/TRBO/API"
		user := allAuthorized(t, k, "ACME", "anna")
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
		r = result(t, mustCallAs(t, k, user, api, "CREATE", `"A1"`, `"Again"`, "null"))
		if len(r["FAILURES"]) != 3 || r["FAILURES"][0]["REASON"] != "ALREADY_EXISTS" || r["FAILURES"][1]["REASON"] != "PARENT_NOT_FOUND" {
			t.Errorf("duplicate: %v", r)
		}
		r = result(t, mustCallAs(t, k, user, api, "ADD_ITEM", `"NOPE"`, "2"))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "PARENT_NOT_FOUND" || r["FAILURES"][0]["ENTITY"] != "/TRBO/ITEMS" {
			t.Errorf("missing parent: %v", r)
		}
		mustCallAs(t, k, user, api, "ADD_ITEM", `"A1"`, "2")

		// Only the given fields change; null and "" are different values.
		mustCallAs(t, k, user, api, "SET_NOTE", `"A1"`, `""`)
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":""`) || !strings.Contains(got, `"NAME":"Alpha"`) {
			t.Errorf("after an empty note: %s", got)
		}
		mustCallAs(t, k, user, api, "SET_NOTE", `"A1"`, "null")
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":null`) {
			t.Errorf("after a null note: %s", got)
		}
		expectCallAs(t, k, user, api, "NAMES", `"1 0 1 Alpha"`, `"A1"`)
		expectCallAs(t, k, user, api, "NAMES", `"0 1 0"`, `"NOPE"`)

		// A mandatory field without value fails the save; nothing is
		// written.
		r = result(t, mustCallAs(t, k, user, api, "RENAME", `"A1"`, `""`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "MANDATORY" || !strings.Contains(r["MESSAGES"][0]["TEXT"].(string), "NAME of /TRBO/ORDERS needs a value") {
			t.Errorf("mandatory: %v", r)
		}
		_, err = call(k, user, api, "FAIL_SAVE", `"A1"`)
		var exc *vm.Exception
		if !errors.As(err, &exc) || exc.Class != "/RT/SAVE_FAILED" {
			t.Errorf("save without result: %v", err)
		}

		// commit of the default session with changes in the buffer.
		_, err = call(k, user, api, "COMMIT_PENDING", `"A1"`)
		if !errors.As(err, &exc) || exc.Class != "/RT/SESSION_ERROR" || !strings.Contains(exc.Text, "use commit business objects") {
			t.Errorf("commit with changes: %v", err)
		}
		// rollback business objects drops the changes.
		expectCallAs(t, k, user, api, "DISCARD", `"none"`, `"A1"`)

		// The end of the execution saves, or drops with rollback.
		mustCallAs(t, k, user, api, "LEAVE_PENDING", `"A1"`)
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":"left"`) {
			t.Errorf("saved at the end: %s", got)
		}
		if _, err := call(k, kernel.ExecOptions{Tenant: "ACME", User: user.User, End: kernel.RollbackUnlessCommitted}, api, "STAGE_NOTE", `"A1"`, `"dropped"`); err != nil {
			t.Fatal(err)
		}
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":"left"`) {
			t.Errorf("dropped at the end: %s", got)
		}
		_, err = call(k, user, api, "LEAVE_EMPTY_NAME", `"A1"`)
		var ns *rterr.BusinessObjectsNotSaved
		if !errors.As(err, &ns) || !strings.Contains(err.Error(), "MANDATORY") {
			t.Errorf("failed save at the end: %v", err)
		}
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NAME":"Alpha"`) {
			t.Errorf("after the failed save: %s", got)
		}

		// An ETag conflict: another execution changes the instance after
		// this one read it.
		err = k.Execute(ctx, user, func(ctx context.Context, m *vm.Machine) error {
			if _, err := m.CallJSON(ctx, api, "STAGE_NOTE", []json.RawMessage{json.RawMessage(`"A1"`), json.RawMessage(`"mine"`)}); err != nil {
				return err
			}
			if _, err := call(k, user, api, "SET_NOTE", `"A1"`, `"theirs"`); err != nil {
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
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NOTE":"theirs"`) {
			t.Errorf("after the conflict: %s", got)
		}

		// Delete removes the dependent instances.
		r = result(t, mustCallAs(t, k, user, api, "REMOVE", `"A1"`))
		if len(r["FAILURES"]) != 0 {
			t.Fatalf("remove: %v", r)
		}
		for _, table := range []string{"/TRBO/ORDERS", "/TRBO/ITEMS", "/TRBO/NOTES"} {
			if n := count(t, k, table); n != 0 {
				t.Errorf("%s has %d rows", table, n)
			}
		}
		r = result(t, mustCallAs(t, k, user, api, "REMOVE", `"A1"`))
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
			{"insert", "insert orders[1]\n", "/TRBO/ORDERS is an entity of business object /TRBO/ORDER; change it with modify business object instead of insert"},
			{"update row", "update orders[1]\n", "instead of update"},
			{"delete row", "delete orders[1]\n", "instead of delete"},
			{"update set", "var n = update /TRBO/ITEMS set TEXT = \"x\"\n", "/TRBO/ITEMS is an entity of business object /TRBO/ORDER"},
			{"delete from", "var n = delete from /TRBO/NOTES\n", "/TRBO/NOTES is an entity of business object /TRBO/ORDER"},
			{"unknown action", "modify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.OPEN keys keys\nend\n", "/TRBO/ORDERS has no action OPEN; its actions are CLOSE, COUNT_OPEN"},
			{"static action with keys", "modify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.COUNT_OPEN keys keys\nend\n", "COUNT_OPEN is a static action and takes no keys"},
			{"instance action without keys", "var p: /TRBO/REASON\nmodify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.CLOSE parameter p\nend\n", "CLOSE is an instance action and needs keys"},
			{"missing parameter", "modify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.CLOSE keys keys\nend\n", "CLOSE needs a parameter of type /TRBO/REASON"},
			{"needless parameter", "var p: /TRBO/REASON\nmodify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.COUNT_OPEN parameter p\nend\n", "COUNT_OPEN takes no parameter"},
			{"into without result", "modify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.COUNT_OPEN into x\nend\n", "COUNT_OPEN has no result"},
			{"result self", "var p: /TRBO/REASON\nmodify business object /TRBO/ORDER\nexecute /TRBO/ORDERS.CLOSE keys keys parameter p into closed\nend\nvar n: INT8 = closed\n", "array of /TRBO/ORDERS"},
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
			{"changing action in a validation", strings.Replace(orderBehavior, "public method CHECK_NAME(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\n",
				"public method CHECK_NAME(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE)\nvar p: /TRBO/REASON\nmodify business object /TRBO/ORDER in local mode\nexecute /TRBO/ORDERS.CLOSE keys keys parameter p\nend\n", 1),
				"CHECK_NAME is a validation, which must not run CLOSE, which changes instances"},
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
	"validations": [{ "name": "CHECK_NAME", "on": ["create", "update"], "fields": ["NAME"] }],
	"authorization": { "object": "/TRBASE/USER_ADMIN", "fields": { "USER_NAME": "NAME" },
		"activities": { "read": "READ", "create": "CREATE", "update": "UPDATE", "delete": "DELETE" } }`

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
		user := allAuthorized(t, k, "ACME", "anna")
		// On modify: SET_STATUS for the creation and MARK_NAME once for the
		// name, although it changes the name itself; STAMP_NOTE waits for
		// the save.
		expectCallAs(t, k, user, api, "STAGE", `"Alpha! new none"`, `"A1"`, `"Alpha"`)

		r := result(t, mustCallAs(t, k, user, api, "CREATE", `"A1"`, `"Alpha"`))
		if len(r["FAILURES"]) != 0 || len(r["MESSAGES"]) != 1 || r["MESSAGES"][0]["TEXT"] != "stamped 1" || r["MESSAGES"][0]["ORIGIN"] != "TRBO" {
			t.Fatalf("create: %v", r)
		}
		got := mustCallAs(t, k, user, api, "READ", `"A1"`)
		if !strings.Contains(got, `"NAME":"Alpha!"`) || !strings.Contains(got, `"STATUS":"new"`) || !strings.Contains(got, `"NOTE":"saved Alpha! new"`) {
			t.Errorf("after create: %s", got)
		}
		r = result(t, mustCallAs(t, k, user, api, "RENAME", `"A1"`, `"Beta"`))
		if len(r["FAILURES"]) != 0 {
			t.Fatalf("rename: %v", r)
		}
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NAME":"Beta!"`) || !strings.Contains(got, `"NOTE":"saved Beta! new"`) {
			t.Errorf("after rename: %s", got)
		}

		// A validation fails the save and keeps the buffer: a later change
		// in the same execution saves it.
		expectCallAs(t, k, user, api, "KEEP_FAILED", `"1 VALIDATION NAME 0 yankee!"`, `"X1"`)

		// A rename the validation rejects leaves the database as it was.
		r = result(t, mustCallAs(t, k, user, api, "RENAME", `"A1"`, `"xenon"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "VALIDATION" || r["MESSAGES"][1]["FIELD"] != "NAME" ||
			r["MESSAGES"][1]["KEY"] != `{"ORDER_ID":"A1"}` {
			t.Errorf("rejected rename: %v", r)
		}
		if got := mustCallAs(t, k, user, api, "READ", `"A1"`); !strings.Contains(got, `"NAME":"Beta!"`) {
			t.Errorf("after the rejected rename: %s", got)
		}

		// A runtime error in a behavior method fails its instances.
		r = result(t, mustCallAs(t, k, user, api, "RENAME", `"A1"`, `"boom"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "BEHAVIOR" ||
			!strings.Contains(r["MESSAGES"][1]["TEXT"].(string), "/TRBO/ORDER_BEHAVIOR.CHECK_NAME: ") {
			t.Errorf("runtime error: %v", r)
		}
	})
}

// actionRoot is the root entity of /TRBO/ORDER with actions.
const actionRoot = `, "numbering": "external",
	"fields": { "STATUS": { "readOnly": true }, "NOTE": { "readOnly": true } },
	"actions": [
		{ "name": "CLOSE", "parameter": "/TRBO/REASON", "result": "self" },
		{ "name": "LABELS", "result": "/TRBO/REASON", "modifies": false },
		{ "name": "COUNT_OPEN", "instance": false, "result": "/TRBO/REASON", "modifies": false },
		{ "name": "PING", "instance": false } ],
	"authorization": { "object": "/TRBASE/USER_ADMIN", "fields": { "USER_NAME": "NAME" },
		"activities": { "read": "READ", "create": "CREATE", "update": "UPDATE", "delete": "DELETE",
			"CLOSE": "UPDATE", "LABELS": "READ", "COUNT_OPEN": "READ", "PING": "READ" } }`

// actionBehavior are the methods of /TRBO/ORDER_BEHAVIOR for actionRoot.
const actionBehavior = `
public method CLOSE(keys: array of key of /TRBO/ORDERS, parameter: /TRBO/REASON, response: /TRBASE/ENTITY_RESPONSE)
  var rows = read business object /TRBO/ORDER in local mode
    /TRBO/ORDERS fields all keys keys
  end
  var changed: array of /TRBO/ORDERS = []
  for i, row in rows
    if (row.STATUS default "") = "closed"
      response.ERROR(i, "{row.ORDER_ID} is closed already")
    else
      var r = row
      r.STATUS = "closed"
      r.NOTE = parameter.TEXT
      changed.APPEND(r)
    end
  end
  modify business object /TRBO/ORDER in local mode
    update /TRBO/ORDERS fields (STATUS, NOTE) from changed
  end
end

public method LABELS(keys: array of key of /TRBO/ORDERS, response: /TRBASE/ENTITY_RESPONSE) returns array of /TRBO/REASON
  var out: array of /TRBO/REASON = []
  for key in keys
    var r: /TRBO/REASON
    r.TEXT = "order " + key.ORDER_ID
    out.APPEND(r)
  end
  return out
end

public method COUNT_OPEN(response: /TRBASE/ENTITY_RESPONSE) returns /TRBO/REASON
  var r: /TRBO/REASON
  r.TEXT = "counted"
  response.GLOBAL_MESSAGE(/TRBASE/MESSAGE_SEVERITY.SUCCESS, "done")
  return r
end

public method PING(response: /TRBASE/ENTITY_RESPONSE)
  var zero = 0
  var x = 1 / zero
end
`

// actionAPI is /TRBO/API for actionRoot.
const actionAPI = `public class /TRBO/API

public static method CREATE(id: STRING)
  var orders: array of new of /TRBO/ORDERS = []
  var o: new of /TRBO/ORDERS
  o.ORDER_ID = id
  o.NAME = id
  orders.APPEND(o)
  modify business object /TRBO/ORDER
    create /TRBO/ORDERS fields (ORDER_ID, NAME) from orders
  end
  commit business objects
end

public static method CLOSE(ids: array of STRING, reason: STRING) returns STRING
  var keys: array of key of /TRBO/ORDERS = []
  for id in ids
    var k: key of /TRBO/ORDERS
    k.ORDER_ID = id
    keys.APPEND(k)
  end
  var p: /TRBO/REASON
  p.TEXT = reason
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS.CLOSE keys keys parameter p into closed
    result outcome
  end
  commit business objects
  var out = "{closed.LENGTH()}"
  for f in outcome.FAILURES
    out = out + " {f.REASON}:{f.KEY default "none"}"
  end
  for row in closed
    out = out + " " + row.ORDER_ID + "=" + (row.STATUS default "none") + "/" + (row.NOTE default "none")
  end
  return out
end

public static method LABELS(id: STRING) returns STRING
  var keys: array of key of /TRBO/ORDERS = []
  var k: key of /TRBO/ORDERS
  k.ORDER_ID = id
  keys.APPEND(k)
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS.LABELS keys keys into labels
  end
  return labels[1].TEXT default "none"
end

public static method COUNT() returns STRING
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS.COUNT_OPEN into counted
    result outcome
  end
  return (counted.TEXT default "none") + " " + outcome.MESSAGES[1].TEXT
end

public static method PING() returns STRING
  modify business object /TRBO/ORDER
    execute /TRBO/ORDERS.PING
    result outcome
  end
  return "{outcome.FAILURES[1].REASON} {outcome.FAILURES[1].OPERATION}"
end

end
`

// TestActions runs instance actions with parameter and result self or a
// structure, and static actions, with failures of single instances.
func TestActions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		files := boFiles("1.0.0", orderBO(actionRoot), actionBehavior)
		src, errs := syntax.Format("API.class", actionAPI)
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
		user := allAuthorized(t, k, "ACME", "anna")
		mustCallAs(t, k, user, api, "CREATE", `"A1"`)
		mustCallAs(t, k, user, api, "CREATE", `"A2"`)
		expectCallAs(t, k, user, api, "CLOSE", `"1 NOT_FOUND:{\"ORDER_ID\":\"NOPE\"} A1=closed/done"`, `["A1","NOPE"]`, `"done"`)
		// The action fails for the closed instance and closes the other.
		expectCallAs(t, k, user, api, "CLOSE", `"1 BEHAVIOR:{\"ORDER_ID\":\"A1\"} A2=closed/again"`, `["A1","A2"]`, `"again"`)
		if got := mustCallAs(t, k, user, api, "LABELS", `"A2"`); got != `"order A2"` {
			t.Errorf("labels: %s", got)
		}
		expectCallAs(t, k, user, api, "COUNT", `"counted done"`)
		expectCallAs(t, k, user, api, "PING", `"BEHAVIOR PING"`)
	})
}

// countingBehavior is orderBehavior with COUNT_OPEN changing the status
// of A1 in local mode.
var countingBehavior = strings.Replace(orderBehavior, "public method COUNT_OPEN(response: /TRBASE/ENTITY_RESPONSE)\n", `public method COUNT_OPEN(response: /TRBASE/ENTITY_RESPONSE)
var rows: array of /TRBO/ORDERS = []
var r: /TRBO/ORDERS
r.ORDER_ID = "A1"
r.STATUS = "counted"
rows.APPEND(r)
modify business object /TRBO/ORDER in local mode
update /TRBO/ORDERS fields (STATUS) from rows
result outcome
end
if outcome.FAILURES is not empty
response.GLOBAL_MESSAGE(/TRBASE/MESSAGE_SEVERITY.ERROR, "not counted")
end
`, 1)

// orderReader is the role /TRBO/READER: reading all orders.
const orderReader = `{ "name": "/TRBO/READER", "texts": { "en": { "label": "Reader" } },
	"authorizations": [{ "object": "/TRBASE/USER_ADMIN", "fields": { "USER_NAME": [{ "pattern": "*" }], "ACTIVITY": [{ "value": "READ" }] } }] }`

// orderClerk is the role /TRBO/CLERK: all activities on orders whose name
// starts with A.
const orderClerk = `{ "name": "/TRBO/CLERK", "texts": { "en": { "label": "Clerk" } },
	"authorizations": [{ "object": "/TRBASE/USER_ADMIN", "fields": { "USER_NAME": [{ "pattern": "A*" }], "ACTIVITY": [{ "pattern": "*" }] } }] }`

// TestEntityAuthorizations checks the authorization of every instance:
// creations, changes before and after, dependent instances through the
// root, actions, the filter of read and the last failed check.
func TestEntityAuthorizations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		files := boFiles("1.0.0", orderBO(orderRoot), countingBehavior)
		src, errs := syntax.Format("API.class", orderAPI)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		files["src/API.class"] = src
		files["schema/roles/CLERK.json"] = orderClerk
		files["schema/roles/READER.json"] = orderReader
		pkg := writePackage(t, files)
		if _, err := k.Install(ctx, []*model.Package{pkg}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		for _, u := range []string{"anna", "rita", "root"} {
			if _, err := k.Users().Create(ctx, u, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TRBO/CLERK", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "rita", Role: "/TRBO/READER", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "root", Role: model.AllAuthorizationsRole, Tenant: "ACME"}, kernel.RoleOptions{AllAuthorizations: true}); err != nil {
			t.Fatal(err)
		}
		const api = "/TRBO/API"
		anna := kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}
		root := kernel.ExecOptions{Tenant: "ACME", User: model.Some("root")}
		run := func(opts kernel.ExecOptions, method string, args ...string) string {
			t.Helper()
			out, err := call(k, opts, api, method, args...)
			if err != nil {
				t.Fatalf("%s: %v", method, err)
			}
			return out
		}

		// anna creates orders named A..., not others; the dependent
		// instances of the failed one have no parent.
		r := result(t, run(anna, "CREATE", `"A1"`, `"Alpha"`, "null"))
		if len(r["FAILURES"]) != 0 || len(r["CREATED"]) != 3 {
			t.Fatalf("Alpha: %v", r)
		}
		r = result(t, run(anna, "CREATE", `"B1"`, `"Beta"`, "null"))
		if len(r["FAILURES"]) != 3 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" || r["FAILURES"][0]["OPERATION"] != "create" ||
			r["FAILURES"][1]["REASON"] != "PARENT_NOT_FOUND" {
			t.Fatalf("Beta: %v", r)
		}
		f, err := k.LastFailedCheck(ctx, "anna", "ACME")
		if err != nil || f == nil || f.Object != "/TRBASE/USER_ADMIN" || string(f.Values["USER_NAME"]) != `"Beta"` || string(f.Values["ACTIVITY"]) != `"CREATE"` {
			t.Errorf("last failed check: %+v, %v", f, err)
		}
		if r = result(t, run(root, "CREATE", `"B1"`, `"Beta"`, "null")); len(r["FAILURES"]) != 0 {
			t.Fatalf("Beta by root: %v", r)
		}

		// Reading filters what anna may not read, also the dependent
		// instances of it.
		if got := run(anna, "NAMES", `"A1"`); got != `"1 0 1 Alpha"` {
			t.Errorf("names of A1: %s", got)
		}
		if got := run(anna, "NAMES", `"B1"`); got != `"0 1 0"` {
			t.Errorf("names of B1: %s", got)
		}

		// A change is checked with the values before and after it.
		r = result(t, run(anna, "RENAME", `"A1"`, `"Bravo"`))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" || r["FAILURES"][0]["OPERATION"] != "update" {
			t.Errorf("rename to Bravo: %v", r)
		}
		if r = result(t, run(anna, "RENAME", `"A1"`, `"Able"`)); len(r["FAILURES"]) != 0 {
			t.Errorf("rename to Able: %v", r)
		}
		run(anna, "SET_NOTE", `"B1"`, `"mine"`)
		if got := run(root, "READ", `"B1"`); strings.Contains(got, "mine") {
			t.Errorf("note of B1: %s", got)
		}

		// Dependent instances need the activity for update of the root.
		if r = result(t, run(anna, "ADD_ITEM", `"A1"`, "2")); len(r["FAILURES"]) != 0 {
			t.Errorf("item of A1: %v", r)
		}
		r = result(t, run(anna, "ADD_ITEM", `"B1"`, "2"))
		if len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" || r["FAILURES"][0]["ENTITY"] != "/TRBO/ITEMS" {
			t.Errorf("item of B1: %v", r)
		}

		// Actions have their own activities; static actions are checked
		// for any value.
		if got := run(anna, "CLOSE", `"A1"`); got != `"1"` {
			t.Errorf("close A1: %s", got)
		}
		if got := run(anna, "CLOSE", `"B1"`); got != `"0 CLOSE:NOT_AUTHORIZED"` {
			t.Errorf("close B1: %s", got)
		}
		if got := run(kernel.ExecOptions{Tenant: "ACME"}, "CLOSE", `"A1"`); got != `"0 CLOSE:NOT_AUTHORIZED COUNT_OPEN:NOT_AUTHORIZED"` {
			t.Errorf("close without user: %s", got)
		}
		// Statements in local mode are not checked: rita may only read,
		// but the static action changes A1 for her.
		rita := kernel.ExecOptions{Tenant: "ACME", User: model.Some("rita")}
		if got := run(rita, "COUNT"); got != `"0 0 0"` {
			t.Errorf("count: %s", got)
		}
		if got := run(root, "READ", `"A1"`); !strings.Contains(got, `"STATUS":"counted"`) {
			t.Errorf("after count: %s", got)
		}
		if r = result(t, run(rita, "RENAME", `"A1"`, `"Abe"`)); len(r["FAILURES"]) != 1 || r["FAILURES"][0]["REASON"] != "NOT_AUTHORIZED" {
			t.Errorf("rename by rita: %v", r)
		}
		if r = result(t, run(anna, "REMOVE", `"B1"`)); len(r["FAILURES"]) != 1 || r["FAILURES"][0]["OPERATION"] != "delete" {
			t.Errorf("remove B1: %v", r)
		}
		if r = result(t, run(anna, "REMOVE", `"A1"`)); len(r["FAILURES"]) != 0 {
			t.Errorf("remove A1: %v", r)
		}
	})
}

// TestTableBecomesEntity upgrades a package whose code writes a table
// that becomes an entity: the upgrade fails and names every place.
func TestTableBecomesEntity(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		writer := "public class /TRBO/WRITER\n\npublic static method RUN(o: /TRBO/ORDERS)\ninsert o\nvar n = delete from /TRBO/ORDERS where NAME = \"x\"\nend\nend\n"
		src, errs := syntax.Format("WRITER.class", writer)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		files := map[string]string{
			"manifest.json":    `{ "id": "TRBO", "version": "1.0.0", "requires": [{ "id": "TRBASE" }], "languageVersion": 1 }`,
			"src/WRITER.class": src,
		}
		for name, v := range orderTables {
			files[name] = v
		}
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, files)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		next := boFiles("1.1.0", orderBO(orderRoot), orderBehavior)
		next["src/WRITER.class"] = src
		_, err := k.Upgrade(ctx, writePackage(t, next), kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "WRITER.class:4:") || !strings.Contains(err.Error(), "WRITER.class:5:") ||
			!strings.Contains(err.Error(), "instead of insert") {
			t.Errorf("upgrade: %v", err)
		}
	})
}

// TestBusinessObjectDescription describes a business object with its
// entities as a tree and the places of code that work on it.
func TestBusinessObjectDescription(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{boPackage(t, "1.0.0", orderRoot)}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		d, err := k.BusinessObject(ctx, "/TRBO/ORDER")
		if err != nil {
			t.Fatal(err)
		}
		root := d.Root
		if d.Package != "TRBO" || d.Description["de"] != "Auftrag" || root.Table != "/TRBO/ORDERS" || len(root.Children) != 2 ||
			root.Children[0].Table != "/TRBO/ITEMS" || root.Children[1].Numbering != model.NumberingUUID {
			t.Fatalf("tree: %+v", d.BusinessObject)
		}
		fields := map[string]string{}
		for _, f := range root.Fields {
			fields[f.Name] = fmt.Sprintf("%v %v %v %s %v", f.Key, f.ReadOnly, f.Mandatory, f.Administrative.Or("-"), f.ETag)
		}
		if fields["ORDER_ID"] != "true false false - false" || fields["STATUS"] != "false true false - false" || fields["NAME"] != "false false true - false" ||
			fields["CHANGED_AT"] != "false false false changedAt true" {
			t.Errorf("fields: %v", fields)
		}
		items := root.Children[0]
		if items.ParentKey == nil || items.ParentKey.Parent != "/TRBO/ORDERS" || items.AuthorizationSource != model.Some(model.AuthorizationInherited) || items.Authorization == nil ||
			len(root.Actions) != 2 || root.Actions[0].Activity.Or("") != "UPDATE" {
			t.Errorf("items: %+v, actions %+v", items, root.Actions)
		}
		kinds := map[string]bool{}
		for _, u := range d.CodeUses {
			if u.File != "src/API.class" {
				t.Errorf("use in %s", u.File)
			}
			kinds[u.Kind] = true
		}
		if !kinds["business-object"] || !kinds["entity"] || !kinds["action"] {
			t.Errorf("uses: %+v", d.CodeUses)
		}
		list, err := k.BusinessObjects(ctx, "TRBO")
		if err != nil || len(list) != 1 {
			t.Errorf("list: %v, %v", list, err)
		}
		if _, err := k.BusinessObjects(ctx, "NOPE"); err == nil {
			t.Error("list of a package that is not installed")
		}
	})
}
