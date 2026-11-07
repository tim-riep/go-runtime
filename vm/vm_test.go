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

package vm_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/vm"
)

var schemaFiles = map[string]string{
	"manifest.json": `{"id":"TRV","version":"1.0.0","languageVersion":1}`,
	"schema/domains/RATING.json": `{"name":"/TRV/RATING","type":"CHAR","length":1,
		"values":[{"value":"H","name":"HIGH"},{"value":"N","name":"NORMAL"}]}`,
	"schema/domains/AMOUNT.json": `{"name":"/TRV/AMOUNT","type":"DEC","length":15,"decimals":2}`,
	"schema/domains/TEXT.json":   `{"name":"/TRV/TEXT","type":"STRING"}`,
	"schema/domains/ID.json":     `{"name":"/TRV/ID","type":"CHAR","length":5,"allowEmpty":false}`,
	"schema/domains/FLAG.json":   `{"name":"/TRV/FLAG","type":"BOOL"}`,
	"schema/structures/ORDER_INFO.json": `{"name":"/TRV/ORDER_INFO","texts":{"en":{"label":"Order"}},"components":[
		{"name":"ID","domain":"/TRV/ID","notNull":true},{"name":"AMOUNT","domain":"/TRV/AMOUNT","notNull":true},
		{"name":"NOTE","domain":"/TRV/TEXT"},{"name":"COUPON","domain":"/TRV/TEXT"},{"name":"PAID","domain":"/TRV/FLAG","notNull":true}]}`,
	"schema/array-types/ORDER_ARRAY.json": `{"name":"/TRV/ORDER_ARRAY","texts":{"en":{"label":"Orders"}},
		"rowType":{"structure":"/TRV/ORDER_INFO"},"key":{"fields":["ID"],"unique":true}}`,
	"src/FAILED.class": "public class /TRV/FAILED extends /RT/EXCEPTION\npublic attribute CODE: INT4\npublic constructor(text: STRING, code: INT4)\nsuper(text)\nCODE = code\nend\nend\n",
}

// program builds and compiles a package with the schema above.
func program(t testing.TB, sources map[string]string) *vm.Image {
	t.Helper()
	image, _ := compiled(t, sources)
	return image
}

// compiled returns the image and the compiled program of the package.
func compiled(t testing.TB, sources map[string]string) (*vm.Image, *bytecode.Program) {
	t.Helper()
	fsys := fstest.MapFS{}
	all := map[string]string{}
	for p, s := range schemaFiles {
		all[p] = s
	}
	for p, s := range sources {
		all[p] = s
	}
	for p, s := range all {
		if len(p) > 4 && p[:4] == "src/" {
			out, errs := syntax.Format(p, s)
			if len(errs) > 0 {
				t.Fatalf("%s: %v", p, errs)
			}
			s = out
		}
		fsys[p] = &fstest.MapFile{Data: []byte(s)}
	}
	pkg, err := pkgload.Load(fsys, "mem")
	if err != nil {
		t.Fatal(err)
	}
	r := build.Package(pkg, nil)
	for _, d := range r.Errors() {
		t.Error(d)
	}
	if !r.OK() {
		t.FailNow()
	}
	p, err := r.Compile(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := build.KernelProgram()
	if err != nil {
		t.Fatal(err)
	}
	image, err := vm.Link(0, kernel, p)
	if err != nil {
		t.Fatal(err)
	}
	return image, p
}

// run calls the static method RUN of class /TRV/C.
func run(t testing.TB, members string, args ...vm.Value) (vm.Value, error) {
	t.Helper()
	p := program(t, map[string]string{"src/C.class": "public class /TRV/C\n" + members + "\nend\n"})
	m, err := vm.New(p, vm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return m.Call(context.Background(), "/TRV/C", "RUN", args...)
}

func TestSmoke(t *testing.T) {
	v, err := run(t, "public static method RUN(a: INT4) returns INT4\nreturn a * 2 + 1\nend", vm.Int(20))
	if err != nil || v.N != 41 {
		t.Fatalf("%v %v", v, err)
	}
}

// call runs a static method with JSON arguments and returns its JSON result.
func call(t testing.TB, sources map[string]string, class, method string, args ...string) (string, error) {
	t.Helper()
	p := program(t, sources)
	m, err := vm.New(p, vm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var raw []json.RawMessage
	for _, a := range args {
		raw = append(raw, json.RawMessage(a))
	}
	out, err := m.CallJSON(context.Background(), class, method, raw)
	return string(out), err
}

// expect runs RUN of /TRV/C and compares the JSON result.
func expect(t *testing.T, members, want string, args ...string) {
	t.Helper()
	got, err := call(t, map[string]string{"src/C.class": "public class /TRV/C\n" + members + "\nend\n"}, "/TRV/C", "RUN", args...)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// expectRaise runs RUN and expects an uncaught exception of class.
func expectRaise(t *testing.T, members, class, text string, args ...string) *vm.Exception {
	t.Helper()
	_, err := call(t, map[string]string{"src/C.class": "public class /TRV/C\n" + members + "\nend\n"}, "/TRV/C", "RUN", args...)
	var e *vm.Exception
	if !errors.As(err, &e) || e.Class != class || !strings.Contains(e.Text, text) {
		t.Fatalf("expected %s with %q, got %v", class, text, err)
	}
	return e
}

func TestStatements(t *testing.T) {
	expect(t, `
public static method RUN(n: INT4) returns array of STRING
  var out: array of STRING = []
  var i = 0
  while true
    i = i + 1
    if i > n
      break
    elseif i mod 2 = 0
      continue
    else
      out.APPEND("odd {i}")
    end
  end
  for k from 10 to 1 step -4
    out.APPEND("down {k}")
  end
  for pos, s in ["a", "b"]
    out.APPEND("{pos}:{s}")
  end
  var m: map of STRING to INT4 = ["x": 1, "y": 2]
  m.SET("z", 3)
  m["x"] = 10
  for key, value in m
    out.APPEND("{key}={value}")
  end
  return out
end`, `["odd 1","odd 3","odd 5","down 10","down 6","down 2","1:a","2:b","x=10","y=2","z=3"]`, "5")
}

func TestStringsAndText(t *testing.T) {
	expect(t, `
public static method RUN(a: STRING, b: STRING?) returns STRING
  var x = a.UPPER() + " " + (b default "none") + " " + "{a.LENGTH()} {a.SUBSTRING(2, 3)} {a.INDEX_OF("l") default 0}"
  return x + " " + ["p", "q"].JOIN("+") + " " + "a,b,c".SPLIT(",").REVERSE().JOIN("") + " \{literal\}"
end`, `"HELLO none 5 ell 3 p+q cba {literal}"`, `"hello"`, "null")
}

func TestNullAndEmpty(t *testing.T) {
	members := `
public static method RUN(o: /TRV/ORDER_INFO) returns array of STRING
  var out: array of STRING = []
  if o.NOTE is null
    out.APPEND("note null")
  elseif o.NOTE is empty
    out.APPEND("note empty")
  end
  if o.COUPON is not null and not (o.COUPON is empty)
    out.APPEND("coupon {o.COUPON}")
  end
  return out
end
public static method ECHO(o: /TRV/ORDER_INFO) returns /TRV/ORDER_INFO
  return o
end`
	expect(t, members, `["note empty","coupon X"]`, `{"ID":"A1","AMOUNT":"1.50","NOTE":"","COUPON":"X","PAID":false}`)
	expect(t, members, `["note null"]`, `{"ID":"A1","AMOUNT":"1.50","PAID":false}`)
	got, err := call(t, map[string]string{"src/C.class": "public class /TRV/C\n" + members + "\nend\n"}, "/TRV/C", "ECHO",
		`{"ID":"A1","AMOUNT":"1.5","NOTE":"","COUPON":null,"PAID":true}`)
	if err != nil || got != `{"ID":"A1","AMOUNT":"1.50","NOTE":"","COUPON":null,"PAID":true}` {
		t.Fatalf("echo: %s %v", got, err)
	}
	// The rules of the domain are checked: /TRV/ID has at most 5 characters
	// and no empty string.
	expectRaise(t, `
public static method RUN() returns STRING
  var o = new /TRV/ORDER_INFO(ID: "TOO_LONG", AMOUNT: 1, PAID: false)
  return o.ID
end`, "/RT/VALUE_ERROR", "allows 5 characters")
	expectRaise(t, `
public static method RUN() returns STRING
  var o = new /TRV/ORDER_INFO(ID: "", AMOUNT: 1, PAID: false)
  return o.ID
end`, "/RT/VALUE_ERROR", "does not allow the empty string")
}

func TestDecimals(t *testing.T) {
	expect(t, `
public static method RUN(a: DEC(15,2), b: DEC(15,2)) returns array of STRING
  var sum: DEC(15,2) = a + b
  var q = a / b
  var r = (a / b) as DEC(15,2)
  var p = a * b
  var i = 7 / 2
  var m = -7 mod 3
  return ["{sum}", "{q}", "{r}", "{p}", "{i}", "{m}", "{1.005 as DEC(5,2)}", "{-1.005 as DEC(5,2)}", "{a > b}"]
end`, `["13.33","3.258786","3.26","31.9260","3","-1","1.01","-1.01","true"]`, `"10.20"`, `"3.13"`)
	expectRaise(t, `
public static method RUN(a: DEC(15,2)) returns DEC(15,2)
  var x: DEC(15,2) = a + a
  return x
end`, "/RT/OVERFLOW", "more than 15 digits", `"9999999999999.99"`)
	expectRaise(t, "public static method RUN(a: DEC(15,2)) returns DEC(38,6)\nreturn a / 0.00\nend", "/RT/DIVISION_BY_ZERO", "division", `"1.00"`)
}

func TestIntegerErrors(t *testing.T) {
	expectRaise(t, "public static method RUN(a: INT4) returns INT4\nreturn a + 1\nend", "/RT/OVERFLOW", "exceeds INT4", "2147483647")
	expectRaise(t, "public static method RUN(a: INT8) returns INT8\nreturn a * 2\nend", "/RT/OVERFLOW", "exceeds INT8", "9223372036854775807")
	expectRaise(t, "public static method RUN(a: INT4) returns INT4\nreturn 10 / a\nend", "/RT/DIVISION_BY_ZERO", "division by zero", "0")
	expectRaise(t, "public static method RUN(a: array of INT4) returns INT4\nreturn a[3]\nend", "/RT/INDEX_ERROR", "index 3 is outside of 1 to 2", "[1, 2]")
	expectRaise(t, `public static method RUN() returns INT4
  var m: map of STRING to INT4 = [:]
  return m["x"]
end`, "/RT/KEY_ERROR", "no key x")
	expect(t, "public static method RUN(a: INT8) returns INT4\nreturn a as INT4\nend", "5", "5")
	expectRaise(t, "public static method RUN(a: INT8) returns INT4\nreturn a as INT4\nend", "/RT/OVERFLOW", "does not fit", "3000000000")
	expectRaise(t, "public static method RUN(a: STRING) returns INT4\nreturn a as INT4\nend", "/RT/CONVERSION_ERROR", "not a whole number", `"x"`)
}

func TestClosures(t *testing.T) {
	expect(t, `
public static method RUN() returns array of INT4
  var factor = 2
  var times = function (x: INT4) returns INT4
    return x * factor
  end
  factor = 3
  var fs: array of function (INT4) returns INT4 = []
  for i from 1 to 3
    fs.APPEND(function (x: INT4) => x + i)
  end
  var out: array of INT4 = [times(5)]
  for f in fs
    out.APPEND(f(10))
  end
  var nested = function (a: INT4) => function (b: INT4) => a * 100 + b + factor
  var inner = nested(7)
  out.APPEND(inner(1))
  out.APPEND([3, 1, 2].SORT_BY(function (v: INT4) => -v)[1])
  out.APPEND([1, 2, 3, 4].FILTER(function (v: INT4) => v mod 2 = 0).MAP(function (v: INT4) => v * v).LENGTH() as INT4)
  return out
end`, `[15,11,12,13,704,3,2]`)
}

func TestInheritanceAndInterfaces(t *testing.T) {
	sources := map[string]string{
		"src/SHAPE.interface": "public interface /TRV/SHAPE\nmethod AREA() returns INT8\nmethod NAME() returns STRING\nend\n",
		"src/BASE.class": `public open class /TRV/BASE implements /TRV/SHAPE
public attribute LOG: array of STRING = ["base init"]
public attribute SIDE: INT8
public constructor(side: INT8)
  LOG.APPEND("base ctor")
  SIDE = side
end
public open method AREA() returns INT8
  return SIDE * SIDE
end
public method NAME() returns STRING
  return "shape {DESCRIBE()}"
end
public open method DESCRIBE() returns STRING
  return "base"
end
end
`,
		"src/BOX.class": `public class /TRV/BOX extends /TRV/BASE
public attribute DEPTH: INT8 = 2
public constructor(side: INT8)
  super(side)
  LOG.APPEND("box ctor {DEPTH}")
end
public override method AREA() returns INT8
  return super.AREA() * DEPTH
end
public override method DESCRIBE() returns STRING
  return "box"
end
public static method RUN() returns array of STRING
  var shapes: array of /TRV/SHAPE = [new /TRV/BASE(3), new /TRV/BOX(3)]
  var out: array of STRING = []
  for s in shapes
    out.APPEND("{s.NAME()} {s.AREA()}")
  end
  var b = new /TRV/BOX(1)
  for l in b.LOG
    out.APPEND(l)
  end
  return out
end
end
`,
	}
	got, err := call(t, sources, "/TRV/BOX", "RUN")
	if err != nil || got != `["shape base 9","shape box 18","base init","base ctor","box ctor 2"]` {
		t.Fatalf("%s %v", got, err)
	}
}

func TestExceptionsAndFinally(t *testing.T) {
	expect(t, `
private static attribute TRACE: array of STRING
private static method RISKY(n: INT4) returns INT4 raises /TRV/FAILED
  if n > 2
    raise /TRV/FAILED with "too big {n}", n
  end
  return n
end
private static method WITH_RETURN() returns STRING
  try
    return "returned"
  finally
    TRACE.APPEND("finally after return")
  end
end
public static method RUN() returns array of STRING
  for i from 1 to 4
    try
      TRACE.APPEND("ok {RISKY(i)}")
      if i = 2
        continue
      end
      TRACE.APPEND("after {i}")
    catch e: /TRV/FAILED
      TRACE.APPEND("caught {e.MESSAGE()} code {e.CODE}")
      break
    finally
      TRACE.APPEND("finally {i}")
    end
  end
  TRACE.APPEND(WITH_RETURN())
  try
    var x = 1 / (TRACE.LENGTH() - TRACE.LENGTH())
  catch e: /RT/DIVISION_BY_ZERO
    TRACE.APPEND("runtime error {e.MESSAGE()}")
  end
  return TRACE
end`, `["ok 1","after 1","finally 1","ok 2","finally 2","caught too big 3 code 3","finally 3","finally after return","returned","runtime error division by zero"]`)
	// /RT/EXCEPTION does not catch runtime errors; finally still runs and
	// the exception leaves with a trace.
	e := expectRaise(t, `
public static method RUN() returns INT4
  try
    var z = 0
    return 5 / z
  catch e: /RT/EXCEPTION
    return -1
  finally
    log info "cleanup"
  end
end`, "/RT/DIVISION_BY_ZERO", "division")
	if len(e.Trace) == 0 || e.Trace[0].File != "src/C.class" || e.Trace[0].Line != 6 {
		t.Fatalf("trace: %+v", e.Trace)
	}
}

func TestValueSemantics(t *testing.T) {
	expect(t, `
public static method CHANGE(o: /TRV/ORDER_INFO, a: array of INT4)
  o.NOTE = "changed"
  a.APPEND(99)
end
public static method RUN() returns array of STRING
  var a = new /TRV/ORDER_INFO(ID: "A", AMOUNT: 1, PAID: false)
  var b = a
  b.NOTE = "b"
  var list = [1]
  var copy = list
  copy.APPEND(2)
  CHANGE(a, list)
  return ["{a.NOTE default "null"}", "{b.NOTE}", "{list.LENGTH()}", "{copy.LENGTH()}", "{a = b}"]
end`, `["null","b","1","2","false"]`)
}

func TestKeyedArrays(t *testing.T) {
	expect(t, `
public static method RUN(orders: /TRV/ORDER_ARRAY) returns array of STRING
  var out: array of STRING = []
  var o = orders.LOOKUP(ID: "B")
  if o is not null
    out.APPEND("found {o.AMOUNT}")
  end
  orders.APPEND(new /TRV/ORDER_INFO(ID: "C", AMOUNT: 3, PAID: true))
  out.APPEND("{orders.LOOKUP(ID: "C") is not null} {orders.LOOKUP(ID: "Z") is null}")
  return out
end`, `["found 2.00","true true"]`, `[{"ID":"A","AMOUNT":"1","PAID":false},{"ID":"B","AMOUNT":"2","PAID":true}]`)
	expectRaise(t, `
public static method RUN(orders: /TRV/ORDER_ARRAY) returns INT8
  orders.APPEND(new /TRV/ORDER_INFO(ID: "A", AMOUNT: 3, PAID: true))
  return orders.LENGTH()
end`, "/RT/DUPLICATE_KEY", "exists already", `[{"ID":"A","AMOUNT":"1","PAID":false}]`)
}

func TestLimits(t *testing.T) {
	p := program(t, map[string]string{"src/C.class": `public class /TRV/C
public static method LOOP() returns INT8
  var n: INT8 = 0
  while true
    n = n + 1
  end
end
public static method DEEP(n: INT8) returns INT8
  return DEEP(n + 1)
end
end
`})
	m, _ := vm.New(p, vm.Options{Budget: 10000})
	var le *vm.LimitError
	if _, err := m.Call(context.Background(), "/TRV/C", "LOOP"); !errors.As(err, &le) || le.Limit != "budget" {
		t.Fatalf("budget: %v", err)
	}
	m, _ = vm.New(p, vm.Options{MaxCallDepth: 50})
	if _, err := m.Call(context.Background(), "/TRV/C", "DEEP", vm.Int(0)); !errors.As(err, &le) || le.Limit != "call depth" {
		t.Fatalf("depth: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m, _ = vm.New(p, vm.Options{})
	if _, err := m.Call(ctx, "/TRV/C", "LOOP"); !errors.As(err, &le) || le.Limit != "canceled" {
		t.Fatalf("cancel: %v", err)
	}
}

func TestLayout(t *testing.T) {
	p := program(t, map[string]string{"src/FLAGS.class": `public class /TRV/FLAGS
public attribute A: BOOL?
public attribute B: BOOL?
public attribute C: BOOL?
public attribute D: BOOL?
public attribute E: BOOL?
public attribute F: BOOL?
public attribute G: BOOL?
public attribute H: BOOL?
public static method RUN() returns array of STRING
  var f = new /TRV/FLAGS()
  f.A = false
  f.H = true
  var t = new /TRV/TEXTS()
  t.TEXT = ""
  var u = new /TRV/TEXTS()
  return ["{f.A is null} {f.A default true}", "{f.B is null}", "{f.H default false}", "{t.TEXT is null} {t.TEXT is not null}", "{u.TEXT is null}"]
end
end
`, "src/TEXTS.class": "public class /TRV/TEXTS\npublic attribute TEXT: STRING?\nend\n"})
	l := *p.Class("/TRV/FLAGS").Layout
	// Eight optional BOOL take exactly two bytes.
	bools := 0
	for _, f := range l.Fields {
		if f.Kind == bytecode.FOptBool {
			bools++
			if f.Offset > 1 {
				t.Fatalf("optional BOOL %s at byte %d", f.Name, f.Offset)
			}
		}
	}
	if bools != 8 || l.Size != 2 {
		t.Fatalf("layout: %d optional BOOL, size %d: %+v", bools, l.Size, l.Fields)
	}
	m, _ := vm.New(p, vm.Options{})
	out, err := m.CallJSON(context.Background(), "/TRV/FLAGS", "RUN", nil)
	if err != nil || string(out) != `["false false","true","true","false true","true"]` {
		t.Fatalf("%s %v", out, err)
	}
}

func TestLogAndAssert(t *testing.T) {
	p := program(t, map[string]string{"src/T.class": `public test class /TRV/T
public test method PASSES()
  log info "running {1 + 1}"
  assert 1 + 1 = 2, "math"
end
public test method FAILS()
  assert 1 = 2, "one is not two"
end
end
`})
	rec := &vm.LogRecorder{}
	m, _ := vm.New(p, vm.Options{Log: rec})
	if err := m.CallMethod(context.Background(), "/TRV/T", "PASSES"); err != nil || len(rec.Entries) != 1 || rec.Entries[0] != (vm.LogEntry{Level: "info", Text: "running 2"}) {
		t.Fatalf("passes: %v %v", err, rec.Entries)
	}
	var e *vm.Exception
	if err := m.CallMethod(context.Background(), "/TRV/T", "FAILS"); !errors.As(err, &e) || e.Class != "/RT/ASSERTION_FAILED" || e.Text != "one is not two" {
		t.Fatalf("fails: %v", err)
	}
}

func TestIncludeLines(t *testing.T) {
	p := program(t, map[string]string{
		"src/STEP.include": "statements include /TRV/STEP\nlog info \"step {n}\"\nn = n / 0\nend\n",
		"src/C.class":      "public class /TRV/C\npublic static method RUN() returns INT4\nvar n = 1\ninclude /TRV/STEP\nreturn n\nend\nend\n",
	})
	m, _ := vm.New(p, vm.Options{})
	_, err := m.Call(context.Background(), "/TRV/C", "RUN")
	var e *vm.Exception
	if !errors.As(err, &e) || e.Trace[0].File != "src/STEP.include" || e.Trace[0].Line != 3 ||
		len(e.Trace[0].Chain) != 1 || e.Trace[0].Chain[0] != "/TRV/STEP included from src/C.class:5" {
		t.Fatalf("trace: %v", err)
	}
}

func TestBytecodeRoundTrip(t *testing.T) {
	_, p := compiled(t, map[string]string{"src/C.class": "public class /TRV/C\npublic static method RUN(n: INT4) returns STRING\nvar s = \"\"\nfor i from 1 to n\ns = s + \"{i}\"\nend\nreturn s\nend\nend\n"})
	b := bytecode.Encode(p)
	q, err := bytecode.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(bytecode.Encode(q), b) || len(q.Funcs) != len(p.Funcs) || q.Package != p.Package {
		t.Fatal("decoded program differs")
	}
	kernel, _ := build.KernelProgram()
	image, err := vm.Link(0, kernel, q)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := vm.New(image, vm.Options{})
	out, err := m.CallJSON(context.Background(), "/TRV/C", "RUN", []json.RawMessage{json.RawMessage("4")})
	if err != nil || string(out) != `"1234"` {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := bytecode.Decode(b[:len(b)/2]); err == nil {
		t.Fatal("truncated program accepted")
	}
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }

func BenchmarkLoop(b *testing.B) {
	p := program(b, map[string]string{"src/C.class": `public class /TRV/C
private static method STEP(a: INT8, i: INT8) returns INT8
  return (a + i * 3) mod 1000003
end
public static method RUN(n: INT8) returns INT8
  var a: INT8 = 0
  for i from 1 to n
    a = STEP(a, i)
  end
  return a
end
end
`})
	m, _ := vm.New(p, vm.Options{Budget: 1 << 40})
	b.ResetTimer()
	for range b.N {
		if _, err := m.Call(context.Background(), "/TRV/C", "RUN", vm.Int(10000)); err != nil {
			b.Fatal(err)
		}
	}
}
