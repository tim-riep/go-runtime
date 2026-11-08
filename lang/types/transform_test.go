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

	"github.com/tim-riep/go-runtime/native"
)

// TestNativeTransform checks parameters, shapes and exceptions of a native
// transformation against what the registry states.
func TestNativeTransform(t *testing.T) {
	reg := native.NewRegistry()
	reg.RegisterTransform("xlsx", native.Transform{
		Params: []native.TransformParam{{Name: "MODE", Values: []string{"A", "B"}}, {Name: "SHEET"}},
		Shapes: []native.TransformShape{{Source: native.ShapeRows, Into: native.ShapeBytes}, {Source: native.ShapeBytes, Into: native.ShapeArrayType}},
		Raises: []string{"/TRT/FAILED"},
		Func: func(*native.Context, native.TransformInput) (native.TransformOutput, error) {
			return native.TransformOutput{}, nil
		},
	})
	files := func(body string) map[string]string {
		return map[string]string{
			"transformations/SHEET.json": `{"name":"/TRT/SHEET","native":"xlsx"}`,
			"schema/structures/DEEP.json": `{"name":"/TRT/DEEP","texts":{"en":{"label":"Deep"}},"components":[{"name":"ID","domain":"/TRT/ID"},
				{"name":"INFO","structure":"/TRT/ORDER_INFO"}]}`,
			"schema/array-types/DEEP_ARRAY.json": `{"name":"/TRT/DEEP_ARRAY","texts":{"en":{"label":"Deep"}},"rowType":{"structure":"/TRT/DEEP"}}`,
			"src/C.class":                        "public class /TRT/C\n" + body + "\nend\n",
		}
	}
	run := func(body, contains string) {
		t.Helper()
		r := compileWith(t, files(body), reg)
		if contains == "" {
			if !r.OK() {
				t.Errorf("%s: %v", body, r.Diagnostics)
			}
			return
		}
		for _, d := range r.Errors() {
			if strings.Contains(d.String(), contains) {
				return
			}
		}
		t.Errorf("%s: expected an error containing %q, got %v", body, contains, r.Diagnostics)
	}
	run(`public static method WRITE(orders: /TRT/ORDER_ARRAY, mode: STRING) returns /TRT/ORDER_ARRAY raises /TRT/FAILED
var plain: array of /TRT/ORDER_INFO = orders
var a = transform orders using /TRT/SHEET parameters MODE = "A", SHEET = "Orders" into BYTES
var b = transform plain using /TRT/SHEET parameters MODE = mode into BYTES
return transform b using /TRT/SHEET into /TRT/ORDER_ARRAY
end`, "")
	run(`public static method READ(data: BYTES) returns /TRT/ORDER_ARRAY raises /TRT/FAILED
return transform data using /TRT/SHEET parameters SHEETS = "x" into /TRT/ORDER_ARRAY
end`, "has no parameter SHEETS; its parameters are MODE, SHEET")
	run(`public static method READ(data: BYTES) returns /TRT/ORDER_ARRAY raises /TRT/FAILED
return transform data using /TRT/SHEET parameters MODE = "C" into /TRT/ORDER_ARRAY
end`, `takes A, B, not "C"`)
	run(`public static method READ(data: BYTES) returns array of /TRT/ORDER_INFO raises /TRT/FAILED
return transform data using /TRT/SHEET into array of /TRT/ORDER_INFO
end`, "bytes into array type")
	run(`public static method WRITE(deep: /TRT/DEEP_ARRAY) returns BYTES raises /TRT/FAILED
return transform deep using /TRT/SHEET into BYTES
end`, "rows into bytes")
	run(`public static method READ(data: BYTES) returns /TRT/DEEP_ARRAY raises /TRT/FAILED
return transform data using /TRT/SHEET into /TRT/DEEP_ARRAY
end`, "is not possible")
	run(`public static method WRITE(o: /TRT/ORDER_INFO) returns BYTES raises /TRT/FAILED
return transform o using /TRT/SHEET into BYTES
end`, "is not possible")
	run(`public static method WRITE(orders: /TRT/ORDER_ARRAY) returns BYTES
return transform orders using /TRT/SHEET into BYTES
end`, "/TRT/FAILED")
	if r := compileWith(t, files(`public static method WRITE(orders: /TRT/ORDER_ARRAY) returns BYTES raises /TRT/FAILED
return transform orders using /TRT/SHEET into BYTES
end`), native.NewRegistry()); r.OK() || !strings.Contains(r.Errors()[0].String(), "no native transformation of format xlsx") {
		t.Errorf("unregistered: %v", r.Diagnostics)
	}
}
