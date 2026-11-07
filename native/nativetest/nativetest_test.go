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

package nativetest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/native/gen"
	"github.com/tim-riep/go-runtime/vm"
)

func load(t *testing.T, reg *native.Registry) (*build.Result, *vm.Machine) {
	t.Helper()
	dir := filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "native", "TRTEST")
	pkg, err := pkgload.Load(os.DirFS(dir), dir)
	if err != nil {
		t.Fatal(err)
	}
	r := build.PackageWith(pkg, nil, reg)
	if !r.OK() {
		t.Fatal(r.Diagnostics)
	}
	image, err := r.Image(false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := vm.New(image, vm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r, m
}

func call(t *testing.T, m *vm.Machine, method string, args ...string) (string, error) {
	t.Helper()
	return callClass(t, m, "/TRTEST/NATIVES", method, args...)
}

func callClass(t *testing.T, m *vm.Machine, class, method string, args ...string) (string, error) {
	t.Helper()
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		raw[i] = json.RawMessage(a)
	}
	out, err := m.CallJSON(context.Background(), class, method, raw)
	return string(out), err
}

// TestBoundary passes structs, arrays, null, "" and DEC to native methods
// and back.
func TestBoundary(t *testing.T) {
	_, m := load(t, Registry)
	for _, c := range []struct {
		method string
		args   []string
		want   string
	}{
		{"ECHO", []string{`{"CODE":"A1","NAME":"abc","NOTE":"","AMOUNT":"1.50","DUE":null,"STATUS":"OPEN"}`},
			`{"CODE":"A1","NAME":"ABC","NOTE":"!","AMOUNT":"1.50","DUE":null,"STATUS":"CLOSED"}`},
		{"ECHO", []string{`{"CODE":"A2","NAME":"x","NOTE":null,"AMOUNT":"0.00","DUE":"2026-11-07","STATUS":"CLOSED"}`},
			`{"CODE":"A2","NAME":"X","NOTE":null,"AMOUNT":"0.00","DUE":"2026-11-07","STATUS":"CLOSED"}`},
		{"ECHO_ALL", []string{`[{"CODE":"A","NAME":"a","AMOUNT":"1.00","STATUS":"OPEN"},{"CODE":"B","NAME":"b","AMOUNT":"2.00","STATUS":"OPEN"}]`},
			`[{"CODE":"B","NAME":"b","NOTE":null,"AMOUNT":"2.00","DUE":null,"STATUS":"OPEN"},{"CODE":"A","NAME":"a","NOTE":null,"AMOUNT":"1.00","DUE":null,"STATUS":"OPEN"}]`},
		{"ECHO_ALL", []string{`[]`}, `[]`},
		{"TEXT", []string{`null`}, `null`},
		{"TEXT", []string{`""`}, `"[]"`},
		{"DOUBLE", []string{`"12.34"`}, `"24.68"`},
		{"FAIL_CODE", []string{`7`}, `7`},
	} {
		got, err := call(t, m, c.method, c.args...)
		if err != nil || got != c.want {
			t.Errorf("%s(%s) = %s, %v; want %s", c.method, strings.Join(c.args, ", "), got, err, c.want)
		}
	}
}

// TestErrors maps errors of implementations to exceptions; a panic does
// not stop the machine.
func TestErrors(t *testing.T) {
	_, m := load(t, Registry)
	for _, c := range []struct {
		method, class, text string
	}{
		{"FAIL", "/TRTEST/FAILED", "failed with code"},
		{"UNDECLARED", "/RT/NATIVE_ERROR", "raised /TRTEST/FAILED, which the method does not declare"},
		{"BROKEN", "/RT/NATIVE_ERROR", "/TRTEST/NATIVES.BROKEN: the disk is full"},
		{"PANIC", "/RT/NATIVE_ERROR", "panic: something impossible"},
		{"BAD_CODE", "/RT/VALUE_ERROR", ""},
	} {
		args := []string{}
		if c.method == "FAIL" {
			args = []string{"3"}
		}
		_, err := call(t, m, c.method, args...)
		var e *vm.Exception
		if !errors.As(err, &e) || e.Class != c.class || !strings.Contains(e.Text, c.text) {
			t.Errorf("%s: %v; want %s with %q", c.method, err, c.class, c.text)
		}
	}
	if got, err := call(t, m, "TEXT", `"after"`); err != nil || got != `"[after]"` {
		t.Errorf("after the panic: %s, %v", got, err)
	}
}

// TestLinkChecksGoTypes rejects an implementation whose Go types do not
// fit the declaration.
func TestLinkChecksGoTypes(t *testing.T) {
	reg := native.NewRegistry()
	reg.RegisterClass("TRTEST", "/TRTEST/COUNTER", (*Counter)(nil))
	for _, m := range Registry.Methods() {
		mm := *m
		if mm.Method == "DOUBLE" {
			mm.Func = func(c *native.Context, amount int64) (int64, error) { return amount, nil }
		}
		reg.Register(mm)
	}
	r, _ := load(t, Registry)
	pr, err := r.Compile(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	kernel, _ := build.KernelProgram()
	if _, err := vm.LinkWith(0, reg, kernel, pr); err == nil || !strings.Contains(err.Error(), "DOUBLE, parameter 1: needs dec.Dec, not int64") {
		t.Errorf("link: %v", err)
	}
}

// TestGeneratedTypes checks that types_gen.go is up to date.
func TestGeneratedTypes(t *testing.T) {
	r, _ := load(t, Registry)
	src, err := gen.Generate(r.Program, "nativetest")
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("types_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(have) != string(src) {
		t.Error("types_gen.go is outdated; run go generate ./native/nativetest")
	}
}

// TestNativeClass uses objects of a native class: code holds them and
// calls their methods; closing them frees them, and the objects left
// open are freed at the end.
func TestNativeClass(t *testing.T) {
	_, m := load(t, Registry)
	if got, err := callClass(t, m, "/TRTEST/COUNTER", "SUM", "10", "3"); err != nil || got != "33" {
		t.Errorf("SUM = %s, %v", got, err)
	}
	_, err := callClass(t, m, "/TRTEST/COUNTER", "AFTER_CLOSE")
	var e *vm.Exception
	if !errors.As(err, &e) || e.Class != "/RT/NATIVE_ERROR" || !strings.Contains(e.Text, "was released") {
		t.Errorf("AFTER_CLOSE: %v", err)
	}
	before := Closed.Load()
	if _, err := callClass(t, m, "/TRTEST/COUNTER", "LEAVE_OPEN", "3"); err != nil {
		t.Fatal(err)
	}
	if err := m.ReleaseHandles(); err != nil {
		t.Fatal(err)
	}
	// SUM left one counter open and LEAVE_OPEN three; AFTER_CLOSE closed
	// its own already.
	if n := Closed.Load() - before; n != 4 {
		t.Errorf("released %d counters", n)
	}
	// Objects of native classes cannot leave or enter through JSON.
	if _, err := callClass(t, m, "/TRTEST/COUNTER", "START", "1"); err == nil {
		t.Error("rt run returned an object of a native class")
	}
}
