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

package types_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/syntax"
)

// schemaFiles is the schema of the test package TRT.
var schemaFiles = map[string]string{
	"manifest.json": `{"id":"TRT","version":"1.0.0","languageVersion":1}`,
	"schema/domains/RATING.json": `{"name":"/TRT/RATING","type":"CHAR","length":1,
		"values":[{"value":"H","name":"HIGH"},{"value":"N","name":"NORMAL"}]}`,
	"schema/domains/LEVEL.json":  `{"name":"/TRT/LEVEL","type":"CHAR","length":3,"values":[{"value":"low"},{"value":"MID"}]}`,
	"schema/domains/CODE.json":   `{"name":"/TRT/CODE","type":"CHAR","length":3,"values":[{"value":"A"},{"value":"B"}]}`,
	"schema/domains/AMOUNT.json": `{"name":"/TRT/AMOUNT","type":"DEC","length":15,"decimals":2}`,
	"schema/domains/TEXT.json":   `{"name":"/TRT/TEXT","type":"STRING"}`,
	"schema/domains/ID.json":     `{"name":"/TRT/ID","type":"CHAR","length":10,"allowEmpty":false}`,
	"schema/domains/FLAG.json":   `{"name":"/TRT/FLAG","type":"BOOL"}`,
	"schema/structures/ORDER_INFO.json": `{"name":"/TRT/ORDER_INFO","texts":{"en":{"label":"Order"}},"components":[
		{"name":"ID","domain":"/TRT/ID","notNull":true},{"name":"AMOUNT","domain":"/TRT/AMOUNT","notNull":true},
		{"name":"NOTE","domain":"/TRT/TEXT"},{"name":"PAID","domain":"/TRT/FLAG","notNull":true},{"name":"CODE","domain":"/TRT/CODE"}]}`,
	"schema/array-types/ORDER_ARRAY.json": `{"name":"/TRT/ORDER_ARRAY","texts":{"en":{"label":"Orders"}},
		"rowType":{"structure":"/TRT/ORDER_INFO"},"key":{"fields":["ID"],"unique":true}}`,
	"schema/tables/ORDERS.json": `{"name":"/TRT/ORDERS","tenantSpecific":true,"dataClass":"application","fields":[
		{"name":"ID","domain":"/TRT/ID"},{"name":"AMOUNT","domain":"/TRT/AMOUNT","notNull":true},
		{"name":"NOTE","domain":"/TRT/TEXT"},{"name":"PAID","domain":"/TRT/FLAG","notNull":true}],"primaryKey":["ID"]}`,
	"src/FAILED.class": "public class /TRT/FAILED extends /RT/EXCEPTION\npublic constructor(text: STRING)\nsuper(text)\nend\nend\n",
}

// compile builds a package with the schema above and the given sources,
// which are formatted first.
func compile(t *testing.T, sources map[string]string) *build.Result {
	t.Helper()
	fsys := fstest.MapFS{}
	for p, s := range schemaFiles {
		fsys[p] = &fstest.MapFile{Data: []byte(s)}
	}
	for p, s := range sources {
		if out, errs := syntax.Format(p, s); len(errs) == 0 {
			s = out
		}
		fsys[p] = &fstest.MapFile{Data: []byte(s)}
	}
	for p, f := range fsys {
		if strings.HasPrefix(p, "src/") {
			if out, errs := syntax.Format(p, string(f.Data)); len(errs) == 0 {
				f.Data = []byte(out)
			}
		}
	}
	pkg, err := pkgload.Load(fsys, "mem")
	if err != nil {
		t.Fatal(err)
	}
	return build.Package(pkg, nil)
}

// class wraps method bodies into a class /TRT/C.
func class(members string) map[string]string {
	return map[string]string{"src/C.class": "public class /TRT/C\n" + members + "\nend\n"}
}

func expectOK(t *testing.T, sources map[string]string) *build.Result {
	t.Helper()
	r := compile(t, sources)
	if !r.OK() {
		for _, d := range r.Errors() {
			t.Error(d)
		}
		t.FailNow()
	}
	return r
}

func expectError(t *testing.T, sources map[string]string, code, contains string) {
	t.Helper()
	r := compile(t, sources)
	for _, d := range r.Errors() {
		if d.Code == code && strings.Contains(d.String(), contains) {
			return
		}
	}
	var all []string
	for _, d := range r.Diagnostics {
		all = append(all, d.String())
	}
	t.Fatalf("expected %s error containing %q, got:\n%s", code, contains, strings.Join(all, "\n"))
}

func TestValidProgram(t *testing.T) {
	expectOK(t, class(`
public constant LIMIT: DEC(15,2) = 1000.00
public constant LEVEL: /TRT/RATING = /TRT/RATING.HIGH
private attribute HISTORY: array of /TRT/ORDER_INFO
private attribute LAST: /TRT/ORDER_INFO?

public method RATE(order: /TRT/ORDER_INFO) returns /TRT/RATING raises /TRT/FAILED
  if order.AMOUNT < 0
    raise /TRT/FAILED with "negative {order.ID}"
  end
  var note = order.NOTE default ""
  if order.NOTE is not null and not (order.NOTE is empty)
    note = "{note} and {order.NOTE}"
  end
  HISTORY.APPEND(order)
  LAST = order
  if order.AMOUNT > LIMIT
    return /TRT/RATING.HIGH
  end
  return /TRT/RATING.NORMAL
end

public method OPEN_TOTAL() returns DEC(15,2)
  var total: DEC(15,2) = 0
  for info in HISTORY.FILTER(function (o: /TRT/ORDER_INFO) returns BOOL
    return not o.PAID
  end)
    total = total + info.AMOUNT
  end
  var count = HISTORY.FILTER(function (o: /TRT/ORDER_INFO) => o.PAID).LENGTH()
  var ids = HISTORY.MAP(function (o: /TRT/ORDER_INFO) => o.ID)
  var names: map of STRING to INT4 = ["a": 1]
  for key, value in names
    log info "{key} = {value} of {count} {ids.JOIN(", ")}"
  end
  return total
end

public method EARLY(text: STRING?) returns INT8
  if text is null
    return 0
  end
  return text.LENGTH()
end

public method ELSE_BRANCH(text: STRING?) returns INT8
  if text is null
    return -1
  else
    return text.LENGTH()
  end
end

public method OR_CHECK(text: STRING?) returns BOOL
  return text is null or text.LENGTH() > 3
end

public method DIVIDE(a: DEC(15,2), b: DEC(15,2)) returns DEC(15,2)
  return (a / b) as DEC(15,2)
end

public method INTS(a: INT4, b: INT8) returns INT8
  var c = a + b
  var d: INT8 = a
  for i from 1 to 10 step 2
    d = d + i * c mod 3
  end
  return d
end

public method SAFE() returns INT4
  try
    RATE(new /TRT/ORDER_INFO(ID: "1", AMOUNT: 1.00, PAID: false))
  catch e: /TRT/FAILED
    log warning e.MESSAGE()
    return 1
  catch e: /RT/DIVISION_BY_ZERO
    return 2
  finally
    log debug "done"
  end
  return 0
end

public method FIND_ONE(orders: /TRT/ORDER_ARRAY) returns STRING
  var found = orders.LOOKUP(ID: "4711")
  if found is not null
    return found.ID
  end
  return orders.FIND(function (o: /TRT/ORDER_INFO) => o.PAID) is null as STRING
end
`))
}

func TestCase(t *testing.T) {
	expectError(t, class("public method M()\nvar Total = 1\nend"), "case", "variable names are written in lower case: total")
	expectError(t, class("public method rate()\nend"), "case", "method names are written in upper case: RATE")
	expectError(t, class("private attribute history: INT4"), "case", "attribute names are written in upper case: HISTORY")
	expectError(t, class("public method M(Order: INT4)\nend"), "case", "parameter names are written in lower case: order")
	expectError(t, class("public method M()\nvar x = Value\nend"), "case", "mixes upper and lower case")
	expectError(t, map[string]string{"src/LOWER.class": "public class /TRT/LOWER\npublic method M()\nvar x = /TRT/Lower.A\nend\nend\n"},
		"case", "global names are written in upper case: /TRT/LOWER")
}

func TestNamespace(t *testing.T) {
	// A package that is not required: rejected by the loader.
	fsys := fstest.MapFS{}
	for p, s := range schemaFiles {
		fsys[p] = &fstest.MapFile{Data: []byte(s)}
	}
	fsys["src/C.class"] = &fstest.MapFile{Data: []byte("public class /TRT/C\n\n  public attribute A: /TRFIN/X?\n\nend\n")}
	if _, err := pkgload.Load(fsys, "mem"); err == nil || !strings.Contains(err.Error(), "reference /TRFIN/X: expected namespace /TRT/") {
		t.Fatalf("foreign namespace: %v", err)
	}
	// A definition in a foreign namespace.
	fsys["src/C.class"] = &fstest.MapFile{Data: []byte("public class /TRZ/C\nend\n")}
	if _, err := pkgload.Load(fsys, "mem"); err == nil || !strings.Contains(err.Error(), "definition /TRZ/C") {
		t.Fatalf("foreign definition: %v", err)
	}
}

func TestNullChecks(t *testing.T) {
	expectError(t, class("public method M(o: /TRT/ORDER_INFO) returns INT8\nreturn o.NOTE.LENGTH()\nend"),
		"null-check", "o.NOTE may be null")
	expectError(t, class("public method M(o: /TRT/ORDER_INFO) returns BOOL\nreturn o.NOTE is empty\nend"),
		"empty-on-optional", "o.NOTE may be null")
	expectError(t, class("public method M(s: STRING?) returns STRING\nreturn s\nend"), "null-check", "s may be null, but STRING is not optional")
	expectError(t, class("public method M(s: STRING?) returns STRING\nreturn \"{s}\"\nend"), "null-check", "s may be null")
	expectError(t, class("public method M(s: STRING?) returns BOOL\nreturn s = null\nend"), "null-check", "compare with is null")
	expectError(t, class("public method M(s: STRING) returns STRING\nreturn s default \"x\"\nend"), "null-check", "s is never null")
	expectError(t, class("public method M(s: STRING?) returns INT8\nif s is not null or s = \"x\"\nreturn 1\nend\nreturn s.LENGTH()\nend"),
		"null-check", "s may be null")
	// A call may change an attribute that was checked.
	expectError(t, class(`
private attribute NAME: STRING?
private method TOUCH()
end
public method M() returns INT8
  if NAME is not null
    TOUCH()
    return NAME.LENGTH()
  end
  return 0
end`), "null-check", "NAME may be null")
	// An assignment in the loop forgets the check.
	expectError(t, class(`
public method M(s: STRING?) returns INT8
  var n: INT8 = 0
  if s is not null
    while n < 3
      n = n + s.LENGTH()
      s = null
    end
  end
  return n
end`), "null-check", "s may be null")
	// A struct without a required component.
	expectError(t, class("public method M() returns /TRT/ORDER_INFO\nreturn new /TRT/ORDER_INFO(ID: \"1\")\nend"),
		"null-check", "AMOUNT is not optional and must be given")
}

func TestExceptions(t *testing.T) {
	expectError(t, class("public method M()\nraise /TRT/FAILED with \"x\"\nend"), "unhandled-exception", "/TRT/FAILED is neither caught nor declared")
	expectError(t, class("public method A() raises /TRT/FAILED\nend\npublic method B()\nA()\nend"),
		"unhandled-exception", "/TRT/FAILED is neither caught nor declared")
	expectError(t, class("public method M()\nvar f = function () => 1\nraise /RT/OVERFLOW with \"x\"\nend"), "unhandled-exception", "only the kernel raises")
	expectError(t, class("public method A() raises /TRT/FAILED\nend\npublic method B()\nvar f = function ()\nA()\nend\nend"),
		"unhandled-exception", "declared with raises in the function")
	expectError(t, class("public method M() raises /TRT/NOPE\nend"), "unknown", "unknown class or interface /TRT/NOPE")
	expectOK(t, class("public method A() raises /TRT/FAILED\nend\npublic method B() raises /RT/EXCEPTION\nA()\nend"))
}

func TestInheritance(t *testing.T) {
	base := "public open class /TRT/BASE\npublic method FIXED()\nend\npublic open method FREE()\nend\nend\n"
	expectError(t, map[string]string{"src/BASE.class": base, "src/SUB.class": "public class /TRT/SUB extends /TRT/BASE\npublic override method FIXED()\nend\nend\n"},
		"override", "/TRT/BASE.FIXED is not open")
	expectError(t, map[string]string{"src/BASE.class": base, "src/SUB.class": "public class /TRT/SUB extends /TRT/BASE\npublic method FREE()\nend\nend\n"},
		"override", "needs override")
	expectError(t, map[string]string{"src/BASE.class": base, "src/SUB.class": "public class /TRT/SUB extends /TRT/BASE\npublic override method OTHER()\nend\nend\n"},
		"override", "overrides nothing")
	expectError(t, map[string]string{"src/FINAL.class": "public class /TRT/FINAL\nend\n", "src/SUB.class": "public class /TRT/SUB extends /TRT/FINAL\nend\n"},
		"inheritance", "/TRT/FINAL is not open")
	expectError(t, map[string]string{"src/A.class": "public open class /TRT/A extends /TRT/B\nend\n", "src/B.class": "public open class /TRT/B extends /TRT/A\nend\n"},
		"cycle", "extends itself")
	expectOK(t, map[string]string{"src/BASE.class": base, "src/SUB.class": "public class /TRT/SUB extends /TRT/BASE\npublic override method FREE()\nsuper.FREE()\nend\nend\n"})
}

func TestInterfaces(t *testing.T) {
	iface := "public interface /TRT/RATER\nmethod RATE(o: /TRT/ORDER_INFO) returns /TRT/RATING\nmethod RESET()\nend\n"
	expectError(t, map[string]string{"src/RATER.interface": iface,
		"src/IMPL.class": "public class /TRT/IMPL implements /TRT/RATER\npublic method RESET()\nend\nend\n"},
		"interface", "/TRT/IMPL does not implement RATE of /TRT/RATER")
	expectError(t, map[string]string{"src/RATER.interface": iface,
		"src/IMPL.class": "public class /TRT/IMPL implements /TRT/RATER\npublic method RESET()\nend\npublic method RATE(o: /TRT/ORDER_INFO) returns STRING\nreturn \"\"\nend\nend\n"},
		"interface", "result STRING instead of /TRT/RATING")
	expectOK(t, map[string]string{"src/RATER.interface": iface,
		"src/IMPL.class": "public class /TRT/IMPL implements /TRT/RATER\npublic method RESET()\nend\npublic method RATE(o: /TRT/ORDER_INFO) returns /TRT/RATING\nreturn /TRT/RATING.HIGH\nend\npublic static method USE(r: /TRT/RATER)\nr.RESET()\nend\nend\n"})
}

func TestNotYet(t *testing.T) {
	expectError(t, class("public method NOW() returns TIMESTAMP executed by kernel"), "not-yet", "methods executed by kernel are not available yet")
}

func TestDatabase(t *testing.T) {
	expectOK(t, class(`public static method M(id: STRING, ids: array of STRING) returns INT8
  var s = open session
  var total = 0
  for o in select from /TRT/ORDERS where AMOUNT > 1.00 and NOTE is null order by AMOUNT descending limit 10 using s
    o.PAID = true
    update o
    total = total + 1
  end
  var one = select single from /TRT/ORDERS where ID = id
  var n = select count from /TRT/ORDERS where ID in ids
  var row: /TRT/ORDERS
  row.ID = "1"
  insert row using s
  delete row
  commit s
  var changed = update /TRT/ORDERS set NOTE = null, PAID = false where NOTE is empty
  delete from /TRT/ORDERS where ID <> ""
  rollback
  if one is not null
    return n + changed
  end
  return total
end`))
	expectError(t, class("public static method M()\nvar x = select from /TRT/ORDER_INFO\nend"), "unknown", "/TRT/ORDER_INFO is not a table")
	expectError(t, class("public static method M()\nvar x = select from /TRT/ORDERS where COLOR = 1\nend"), "unknown", "/TRT/ORDERS has no field COLOR")
	expectError(t, class("public static method M(n: STRING?)\nvar x = select from /TRT/ORDERS where NOTE = n\nend"), "null-check", "")
	expectError(t, class("public static method M()\nvar x = select from /TRT/ORDERS where NOTE = null\nend"), "null-check", "is null")
	expectError(t, class("public static method M()\nvar x = select from /TRT/ORDERS where PAID is empty\nend"), "type", "is empty applies to STRING fields")
	expectError(t, class("public static method M()\nvar x = select from /TRT/ORDERS where 1 = 1\nend"), "type", "needs a field")
	expectError(t, class("public static method M(a: array of INT4)\nvar x = select from /TRT/ORDERS where ID in a\nend"), "type", "in needs an array of")
	expectError(t, class("public static method M()\nvar x = select single from /TRT/ORDERS limit 1\nend"), "type", "takes no order by or limit")
	expectError(t, class("public static method M()\nvar x = update /TRT/ORDERS set PAID = 1\nend"), "type", "")
	expectError(t, class("public static method M(o: /TRT/ORDER_INFO)\ninsert o\nend"), "type", "insert needs a row of a table")
	expectError(t, class("public static method M()\ninsert new /TRT/ORDERS(ID: \"1\", AMOUNT: 1.00, PAID: false) using 1\nend"), "type", "using needs a SESSION")
	expectError(t, class("public static method M()\nfor o in select from /TRT/ORDERS\ncommit\nend\nend"), "statement", "open cursor")
	expectError(t, class("public static method M()\nvar s = open session\nfor o in select from /TRT/ORDERS using s\nrollback s\nend\nend"), "statement", "open cursor")
	expectOK(t, class("public static method M()\nvar s = open session\nfor o in select from /TRT/ORDERS using s\ncommit\nend\nend"))
	expectError(t, class("private attribute S: SESSION?"), "type", "SESSION")
	expectError(t, class("public static method M()\nvar a = new array of SESSION\nend"), "type", "SESSION")
}

func TestTypes(t *testing.T) {
	expectError(t, class("public method M(a: DEC(15,2), b: DEC(15,2)) returns DEC(15,2)\nreturn a / b\nend"), "type", "DEC(38,6), but DEC(15,2) is needed")
	expectError(t, class("public method M(a: INT8) returns INT4\nreturn a\nend"), "type", "INT8, but INT4 is needed")
	expectError(t, class("public method M(s: STRING) returns STRING\nreturn s + 1\nend"), "type", "+ joins two strings")
	expectError(t, class("public method M() returns /TRT/RATING\nreturn \"H\"\nend"), "type", "STRING, but /TRT/RATING is needed")
	expectError(t, class("public method M() returns INT4\nend"), "return", "must return a INT4 at the end")
	expectError(t, class("public method M()\nreturn\nvar x = 1\nend"), "statement", "never reached")
	expectError(t, class("public method M()\nassert true\nend"), "test", "assert is only allowed in test classes")
	expectError(t, class("public method M(a: array of INT4)\na.FOO()\nend"), "unknown", "has no method FOO")
	expectError(t, class("public method M() returns BOOL\nreturn /TRT/CODE.A = /TRT/RATING.HIGH\nend"), "type", "cannot compare")
	expectError(t, class("public constant A: INT4 = B()\npublic static method B() returns INT4\nreturn 1\nend"), "constant", "must be a literal")
	expectError(t, class("public method M() returns /TRT/RATING\nreturn /TRT/RATING.NONE\nend"), "unknown", "has no constant NONE")
	expectError(t, class("public method M(r: /TRT/LEVEL) returns STRING\nreturn r.VALUE()\nend"), "enum", `fixed value "low" of /TRT/LEVEL needs a name`)
	expectError(t, class("private attribute A: INT4\npublic static method M() returns INT4\nreturn A\nend"), "static", "needs an instance")
	expectError(t, class("public method M()\nvar f = function () => 1\nvar x = 1\nvar g = function ()\nx = 2\nend\nend"), "statement", "cannot assign x of the enclosing method")
	expectError(t, class("private attribute OTHER: /TRT/C"), "null-check", "needs an initial value or must be optional")
	expectError(t, map[string]string{"src/C.class": "public class /TRT/C\npublic method M()\nvar o = new /TRT/D()\no.SECRET()\nend\nend\n",
		"src/D.class": "public class /TRT/D\nprivate method SECRET()\nend\nend\n"}, "visibility", "SECRET is private to /TRT/D")
}

func TestCanonicalFormRequired(t *testing.T) {
	fsys := fstest.MapFS{}
	for p, s := range schemaFiles {
		fsys[p] = &fstest.MapFile{Data: []byte(s)}
	}
	fsys["src/FAILED.class"] = &fstest.MapFile{Data: []byte(mustFormat(t, schemaFiles["src/FAILED.class"]))}
	fsys["src/C.class"] = &fstest.MapFile{Data: []byte("public class /TRT/C\n\n  public constant A: INT4 =  1\n\nend\n")}
	pkg, err := pkgload.Load(fsys, "mem")
	if err != nil {
		t.Fatal(err)
	}
	r := build.Package(pkg, nil)
	if r.OK() || len(r.Errors()) != 1 || !strings.Contains(r.Errors()[0].String(), "src/C.class:3:1: canonical: the file is not in canonical form from this line on; run rt fmt") {
		t.Fatalf("canonical form: %v", r.Diagnostics)
	}
}

func mustFormat(t *testing.T, s string) string {
	out, errs := syntax.Format("x", s)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return out
}

func notes(r *build.Result) []string {
	var out []string
	for _, d := range r.Diagnostics {
		if d.Level == "note" {
			out = append(out, d.String())
		}
	}
	return out
}

func TestIncludes(t *testing.T) {
	checks := "members include /TRT/CHECKS\nprivate constant MIN: INT8 = 1\nprivate method CHECK(v: INT8) returns BOOL\nreturn v >= MIN\nend\nend\n"
	step := "statements include /TRT/STEP\nlog info \"step {step}\"\nstep = step + 1\nend\n"
	user := "public class /TRT/C\ninclude /TRT/CHECKS\npublic method M() returns BOOL\nvar step: INT8 = 1\ninclude /TRT/STEP\ninclude /TRT/OPTIONAL if found\nreturn CHECK(step)\nend\nend\n"
	r := expectOK(t, map[string]string{"src/CHECKS.include": checks, "src/STEP.include": step, "src/C.class": user})
	if n := notes(r); len(n) != 1 || !strings.Contains(n[0], "include /TRT/OPTIONAL does not exist and is skipped") {
		t.Fatalf("notes: %v", n)
	}
	// An include with if found that exists is inserted like any other.
	optional := "statements include /TRT/OPTIONAL\nstep = step + MISSING\nend\n"
	expectError(t, map[string]string{"src/CHECKS.include": checks, "src/STEP.include": step, "src/OPTIONAL.include": optional, "src/C.class": user},
		"unknown", "src/OPTIONAL.include:2:17: unknown: /TRT/C has no member MISSING\n    /TRT/OPTIONAL included from src/C.class:8")
	// The include kind must fit the place.
	expectError(t, map[string]string{"src/STEP.include": step, "src/C.class": "public class /TRT/C\ninclude /TRT/STEP\nend\n"},
		"include", "/TRT/STEP is a statements include and cannot be included among members")
	expectError(t, map[string]string{"src/CHECKS.include": checks, "src/C.class": "public class /TRT/C\npublic method M()\ninclude /TRT/CHECKS\nend\nend\n"},
		"include", "/TRT/CHECKS is a members include and cannot be included among statements")
	expectError(t, class("public method M()\ninclude /TRT/NOPE\nend"), "include", "include /TRT/NOPE does not exist")
	// Cycles show the chain.
	a := "statements include /TRT/A\ninclude /TRT/B\nend\n"
	b := "statements include /TRT/B\ninclude /TRT/A\nend\n"
	expectError(t, map[string]string{"src/A.include": a, "src/B.include": b, "src/C.class": "public class /TRT/C\npublic method M()\ninclude /TRT/A\nend\nend\n"},
		"cycle", "include cycle: /TRT/A -> /TRT/B -> /TRT/A")
	// Errors in nested includes name the whole chain.
	inner := "statements include /TRT/INNER\nvar x: INT4 = \"text\"\nend\n"
	outer := "statements include /TRT/OUTER\ninclude /TRT/INNER\nend\n"
	expectError(t, map[string]string{"src/INNER.include": inner, "src/OUTER.include": outer, "src/C.class": "public class /TRT/C\npublic method M()\ninclude /TRT/OUTER\nend\nend\n"},
		"type", "src/INNER.include:2:17: type: the value of x is STRING, but INT4 is needed\n    /TRT/INNER included from src/OUTER.include:2\n    /TRT/OUTER included from src/C.class:4")
	// An include used nowhere is only noted.
	r = expectOK(t, map[string]string{"src/STEP.include": step, "src/C.class": "public class /TRT/C\nend\n"})
	if n := notes(r); len(n) != 1 || !strings.Contains(n[0], "include /TRT/STEP is not included anywhere") {
		t.Fatalf("notes: %v", n)
	}
}
