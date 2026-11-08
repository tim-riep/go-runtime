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
	"maps"
	"strings"
	"testing"
)

const checksInterface = `public interface /TRT/CHECKS
method VALIDATE(order: /TRT/ORDER_INFO) raises /TRT/FAILED
method ADJUST(order: /TRT/ORDER_INFO) returns /TRT/ORDER_INFO
end
`

const checksImpl = `public class /TRT/IMPL implements /TRT/CHECKS
private attribute COUNT: INT4
public method VALIDATE(order: /TRT/ORDER_INFO) raises /TRT/FAILED
COUNT = COUNT + 1
end
public method ADJUST(order: /TRT/ORDER_INFO) returns /TRT/ORDER_INFO
order.NOTE = "{COUNT}"
return order
end
end
`

// points returns the sources of TRT with the multiple extension point
// /TRT/CHECKS filtered by CODE and ID, its implementation /TRT/IMPL and
// the class /TRT/C with members, after applying the replacements.
func points(members string, replace ...string) map[string]string {
	files := map[string]string{
		"src/CHECKS.interface": checksInterface,
		"src/IMPL.class":       checksImpl,
		"schema/extension-points/CHECKS.json": `{"name":"/TRT/CHECKS","description":{"en":"Checks"},"interface":"/TRT/CHECKS","use":"multiple",
			"filters":[{"name":"CODE","domain":"/TRT/CODE"},{"name":"ID","domain":"/TRT/ID"}]}`,
		"schema/extension-implementations/IMPL.json": `{"name":"/TRT/IMPL","description":{"en":"Impl"},"extensionPoint":"/TRT/CHECKS",
			"class":"/TRT/IMPL","filters":{"CODE":[{"value":"A"}],"ID":[{"pattern":"*"}]}}`,
	}
	maps.Copy(files, class(members))
	r := strings.NewReplacer(replace...)
	for k, v := range files {
		files[k] = r.Replace(v)
	}
	return files
}

const runChecks = `
public static method RUN(order: /TRT/ORDER_INFO) returns /TRT/ORDER_INFO raises /TRT/FAILED
  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = order.ID
  checks.VALIDATE(order)
  if checks.IS_IMPLEMENTED() and checks.COUNT() > 0
    return PASS(checks).ADJUST(order)
  end
  return order
end
private static method PASS(checks: extension point /TRT/CHECKS) returns extension point /TRT/CHECKS
  return checks
end
`

func TestExtensionPointProgram(t *testing.T) {
	expectOK(t, points(runChecks))
}

func TestExtensionPointRules(t *testing.T) {
	for _, c := range []struct {
		name, members string
		replace       []string
		code, msg     string
	}{
		{"interface not public", runChecks, []string{"public interface /TRT/CHECKS", "package interface /TRT/CHECKS"},
			"extension-point", "/interface: /TRT/CHECKS is not public; the interface of an extension point is public"},
		{"class does not implement the interface", runChecks, []string{"public class /TRT/IMPL implements /TRT/CHECKS", "public class /TRT/IMPL"},
			"extension-point", "/class: /TRT/IMPL does not implement /TRT/CHECKS"},
		{"class not public", runChecks, []string{"public class /TRT/IMPL", "package class /TRT/IMPL"},
			"extension-point", "/class: /TRT/IMPL is not public"},
		{"constructor with parameters", runChecks, []string{"private attribute COUNT: INT4\n", "private attribute COUNT: INT4\npublic constructor(n: INT4)\nCOUNT = n\nend\n"},
			"extension-point", "/class: /TRT/IMPL needs a public constructor without parameters"},
		{"constructor that raises", runChecks, []string{"private attribute COUNT: INT4\n", "private attribute COUNT: INT4\npublic constructor() raises /TRT/FAILED\nend\n"},
			"extension-point", "/TRT/IMPL has a constructor that raises /TRT/FAILED"},
		{"fallback does not implement", runChecks, []string{`"use":"multiple",`, `"use":"multiple","fallback":"/TRT/C",`},
			"extension-point", "/fallback: /TRT/C does not implement /TRT/CHECKS"},
		{"result without chain", runChecks, []string{"returns /TRT/ORDER_INFO\nend\n", "returns /TRT/ORDER_INFO\nmethod SCORE() returns INT4\nend\n"},
			"extension-point", "/use: /TRT/CHECKS.SCORE returns INT4; in a multiple extension point a method returns nothing"},
		{"reserved method", runChecks, []string{"returns /TRT/ORDER_INFO\nend\n", "returns /TRT/ORDER_INFO\nmethod COUNT()\nend\n"},
			"extension-point", "/TRT/CHECKS has the method COUNT, which every value of an extension point has already"},
		{"missing filter field", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A\nend\n", nil,
			"extension-point", "extension point /TRT/CHECKS needs a value for the filter field ID"},
		{"no filters at all", "public static method RUN()\n  var checks = extension point /TRT/CHECKS\nend\n", nil,
			"extension-point", "extension point /TRT/CHECKS needs a value for the filter field CODE"},
		{"unknown filter field", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"X\", NAME = \"N\"\nend\n", nil,
			"unknown", "/TRT/CHECKS has no filter field NAME; its filter fields are CODE, ID"},
		{"filter field twice", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"X\", ID = \"Y\"\nend\n", nil,
			"duplicate", "the filter field ID is given twice"},
		{"value of the wrong type", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = 7\nend\n", nil,
			"type", "the value of ID is INT4"},
		{"value that may be null", "public static method RUN(order: /TRT/ORDER_INFO)\n  var checks = extension point /TRT/CHECKS filters CODE = order.CODE, ID = \"X\"\nend\n", nil,
			"null-check", "order.CODE may be null"},
		{"literal too long", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"ABCDEFGHIJK\"\nend\n", nil,
			"type", `"ABCDEFGHIJK" is longer than the 10 characters of ID`},
		{"unknown extension point", "public static method RUN()\n  var checks = extension point /TRT/NOPE\nend\n", nil,
			"extension-point", "extension point /TRT/NOPE does not exist"},
		{"attribute", "private attribute CHECKS: extension point /TRT/CHECKS\n", nil,
			"extension-point", "the value of /TRT/CHECKS cannot be kept in an attribute"},
		{"array", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"X\"\n  var all = [checks]\nend\n", nil,
			"extension-point", "the value of /TRT/CHECKS cannot be kept in an array"},
		{"array type", "public static method RUN(all: array of extension point /TRT/CHECKS)\nend\n", nil,
			"extension-point", "the value of /TRT/CHECKS cannot be kept in an array"},
		{"map", "public static method RUN(all: map of STRING to extension point /TRT/CHECKS)\nend\n", nil,
			"extension-point", "the value of /TRT/CHECKS cannot be kept in a map"},
		{"variable without value", "public static method RUN()\n  var checks: extension point /TRT/CHECKS\nend\n", nil,
			"extension-point", "the value of /TRT/CHECKS comes from extension point /TRT/CHECKS"},
		{"optional", "public static method RUN(checks: extension point /TRT/CHECKS?)\nend\n", nil,
			"type", "the value of an extension point is never null"},
		{"unknown method", "public static method RUN()\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"X\"\n  checks.PRINT()\nend\n", nil,
			"unknown", "/TRT/CHECKS has no method PRINT; it has the methods of /TRT/CHECKS, IS_IMPLEMENTED and COUNT"},
		{"exception not handled", "public static method RUN(order: /TRT/ORDER_INFO)\n  var checks = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"X\"\n  checks.VALIDATE(order)\nend\n", nil,
			"unhandled-exception", "/TRT/FAILED"},
		{"undeclared exception in the implementation", runChecks, []string{"public method ADJUST(order: /TRT/ORDER_INFO) returns /TRT/ORDER_INFO\n",
			"public method ADJUST(order: /TRT/ORDER_INFO) returns /TRT/ORDER_INFO raises /TRT/FAILED\n"},
			"interface", "raises /TRT/FAILED, which the overridden method does not declare"},
		{"commit in the implementation", runChecks, []string{"COUNT = COUNT + 1\n", "COUNT = COUNT + 1\ncommit\n"},
			"extension-point", "/TRT/IMPL.VALIDATE implements VALIDATE of extension point /TRT/CHECKS, which does not allow implementations to commit the default session"},
		{"compared", "public static method RUN()\n  var a = extension point /TRT/CHECKS filters CODE = /TRT/CODE.A, ID = \"X\"\n  var same = a = a\nend\n", nil,
			"type", "= cannot compare extension point /TRT/CHECKS and extension point /TRT/CHECKS"},
	} {
		t.Run(c.name, func(t *testing.T) { expectError(t, points(c.members, c.replace...), c.code, c.msg) })
	}
}

// TestExtensionPointCommitAllowed accepts commit in implementations of an
// extension point that allows it, and a session of its own always.
func TestExtensionPointCommitAllowed(t *testing.T) {
	expectOK(t, points(runChecks, "COUNT = COUNT + 1\n", "COUNT = COUNT + 1\ncommit\n", `"use":"multiple",`, `"use":"multiple","commitAllowed":true,`))
	expectOK(t, points(runChecks, "COUNT = COUNT + 1\n", "COUNT = COUNT + 1\nvar s = open session\ncommit s\n"))
}

// TestExtensionPointSingle allows every kind of result in a single
// extension point.
func TestExtensionPointSingle(t *testing.T) {
	expectOK(t, points(runChecks, `"use":"multiple"`, `"use":"single"`, "returns /TRT/ORDER_INFO\nend\n", "returns /TRT/ORDER_INFO\nmethod SCORE() returns INT4\nend\n",
		"order.NOTE = \"{COUNT}\"\nreturn order\nend\n", "order.NOTE = \"{COUNT}\"\nreturn order\nend\npublic method SCORE() returns INT4\nreturn COUNT\nend\n"))
}
