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
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// stepsFiles are the files of TRSTEPS: the multiple extension point
// /TRSTEPS/STEPS filtered by COUNTRY and NO, whose implementations
// remember a text and append to a text one after the other, the single
// extension points /TRSTEPS/PRICE with a fallback class and
// /TRSTEPS/STRICT without, and /TRSTEPS/RUN that runs them.
func stepsFiles(version string) map[string]string {
	return map[string]string{
		"manifest.json": `{ "id": "TRSTEPS", "version": "` + version + `", "requires": [{ "id": "TRBASE" }], "languageVersion": 1 }`,
		"schema/extension-points/STEPS.json": `{ "name": "/TRSTEPS/STEPS", "description": { "en": "Steps", "de": "Schritte" }, "interface": "/TRSTEPS/STEPS",
			"use": "multiple", "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }, { "name": "NO", "domain": "/TRBASE/COUNTER" }] }`,
		"src/STEPS.interface": `public interface /TRSTEPS/STEPS

  method REMEMBER(text: STRING)
  method APPEND(text: STRING) returns STRING raises /TRSTEPS/STOPPED

end
`,
		"src/PRICING.interface": `public interface /TRSTEPS/PRICING

  method PRICE(amount: INT4) returns STRING

end
`,
		"src/STOPPED.class": `# The chain was stopped.
public class /TRSTEPS/STOPPED extends /RT/EXCEPTION

  public constructor(text: STRING)
    super(text)
  end

end
`,
		"src/DEFAULT_PRICE.class": `public class /TRSTEPS/DEFAULT_PRICE implements /TRSTEPS/PRICING

  public method PRICE(amount: INT4) returns STRING
    return "default {amount}"
  end

end
`,
		"src/RUN.class": `public class /TRSTEPS/RUN

  public static method STEPS(country: /TRBASE/COUNTRY, no: INT8, text: STRING) returns STRING
    var steps = extension point /TRSTEPS/STEPS filters COUNTRY = country, NO = no
    steps.REMEMBER("r")
    var count = steps.COUNT()
    var implemented = steps.IS_IMPLEMENTED()
    try
      return "{count} {implemented} " + steps.APPEND(text)
    catch e: /TRSTEPS/STOPPED
      return "stopped: " + e.MESSAGE()
    end
  end

  public static method PRICE(country: /TRBASE/COUNTRY) returns STRING
    var pricing = extension point /TRSTEPS/PRICE filters COUNTRY = country
    return USE(pricing)
  end

  private static method USE(pricing: extension point /TRSTEPS/PRICE) returns STRING
    return "{pricing.IS_IMPLEMENTED()} " + pricing.PRICE(7)
  end

  public static method STRICT(country: /TRBASE/COUNTRY) returns STRING
    return (extension point /TRSTEPS/STRICT filters COUNTRY = country).PRICE(1)
  end

  public static method REFLECT() returns STRING raises /TRBASE/EXTENSION_POINT_NOT_FOUND
    var p = /TRBASE/REFLECTOR.EXTENSION_POINT("/TRSTEPS/STEPS")
    var out = "{p.USE.NAME()} {p.INTERFACE_NAME} {p.FILTERS.LENGTH()} {p.FILTERS[2].TYPE} {p.DESCRIPTION default "-"}"
    for impl in p.IMPLEMENTATIONS
      out = out + " {impl.NAME}:{impl.ACTIVE}:{impl.FILTERS.LENGTH()}"
    end
    return out
  end

end
`,
		"schema/extension-points/PRICE.json": `{ "name": "/TRSTEPS/PRICE", "description": { "en": "Price" }, "interface": "/TRSTEPS/PRICING",
			"use": "single", "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }], "fallback": "/TRSTEPS/DEFAULT_PRICE" }`,
		"schema/extension-points/STRICT.json": `{ "name": "/TRSTEPS/STRICT", "description": { "en": "Strict" }, "interface": "/TRSTEPS/PRICING",
			"use": "single", "filters": [{ "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }] }`,
	}
}

// stepClass is a class of TRSTEPIMPL that implements /TRSTEPS/STEPS: it
// remembers texts and appends "[NAME:remembered]"; extra is code at the
// start of APPEND.
func stepClass(name, extra string) string {
	return `public class /TRSTEPIMPL/` + name + ` implements /TRSTEPS/STEPS

  private attribute SEEN: STRING

  public method REMEMBER(text: STRING)
    SEEN = SEEN + text
  end

  public method APPEND(text: STRING) returns STRING raises /TRSTEPS/STOPPED
` + extra + `    return text + "[` + name + `:{SEEN}]"
  end

end
`
}

func priceClass(name string) string {
	return `public class /TRSTEPIMPL/` + name + ` implements /TRSTEPS/PRICING

  public method PRICE(amount: INT4) returns STRING
    return "` + name + ` {amount}"
  end

end
`
}

func implementation(name, point, class, rest string) string {
	return `{ "name": "/TRSTEPIMPL/` + name + `", "description": { "en": "` + name + `" }, "extensionPoint": "` + point + `",
		"class": "/TRSTEPIMPL/` + class + `"` + rest + ` }`
}

// stepImplFiles are the files of TRSTEPIMPL with implementations of the
// extension points of TRSTEPS.
func stepImplFiles(version string) map[string]string {
	return map[string]string{
		"manifest.json":   `{ "id": "TRSTEPIMPL", "version": "` + version + `", "requires": [{ "id": "TRBASE" }, { "id": "TRSTEPS" }], "languageVersion": 1 }`,
		"src/A.class":     stepClass("A", ""),
		"src/B.class":     stepClass("B", "    if text = \"stop\"\n      raise /TRSTEPS/STOPPED with \"by B\"\n    end\n"),
		"src/C.class":     stepClass("C", ""),
		"src/DE.class":    priceClass("DE"),
		"src/OTHER.class": priceClass("OTHER"),
		// B runs first by its order, then A and C with the same order by
		// name.
		"schema/extension-implementations/A.json": implementation("A", "/TRSTEPS/STEPS", "A",
			`, "filters": { "COUNTRY": [{ "value": "DE" }, { "from": "FR", "to": "IT" }], "NO": [{ "pattern": "*" }] }, "order": 10`),
		"schema/extension-implementations/B.json": implementation("B", "/TRSTEPS/STEPS", "B",
			`, "filters": { "COUNTRY": [{ "pattern": "D*" }], "NO": [{ "from": 1, "to": 5 }] }, "order": 5`),
		"schema/extension-implementations/C.json": implementation("C", "/TRSTEPS/STEPS", "C",
			`, "filters": { "COUNTRY": [{ "pattern": "*" }], "NO": [{ "pattern": "1*" }] }, "order": 10`),
		"schema/extension-implementations/PRICE_DE.json":     implementation("PRICE_DE", "/TRSTEPS/PRICE", "DE", `, "filters": { "COUNTRY": [{ "value": "DE" }] }`),
		"schema/extension-implementations/STRICT_DE.json":    implementation("STRICT_DE", "/TRSTEPS/STRICT", "DE", `, "filters": { "COUNTRY": [{ "value": "DE" }] }`),
		"schema/extension-implementations/STRICT_OTHER.json": implementation("STRICT_OTHER", "/TRSTEPS/STRICT", "OTHER", `, "filters": { "COUNTRY": [{ "value": "AT" }] }`),
	}
}

// TestExtensionPointRuntime runs extension points: selection by values,
// ranges, patterns and *, the order, the chain, the state between two
// calls, an exception that ends the chain, the fallback class and the
// errors without implementation.
func TestExtensionPointRuntime(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0")), writePackage(t, stepImplFiles("1.0.0"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		const run = "/TRSTEPS/RUN"
		for _, c := range []struct{ country, no, text, want string }{
			// B (order 5) for D* and 1 to 5, then A and C (order 10) by name.
			{"DE", "1", "x", `"3 true x[B:r][A:r][C:r]"`},
			{"DE", "3", "x", `"2 true x[B:r][A:r]"`},
			{"DK", "12", "x", `"1 true x[C:r]"`},
			{"GB", "12", "x", `"2 true x[A:r][C:r]"`},
			{"IT", "7", "x", `"1 true x[A:r]"`},
			{"US", "7", "x", `"0 false x"`},
			{"DE", "1", "stop", `"stopped: by B"`},
		} {
			if got := mustCall(t, k, "ACME", run, "STEPS", `"`+c.country+`"`, c.no, `"`+c.text+`"`); got != c.want {
				t.Errorf("STEPS(%s, %s, %s) = %s, want %s", c.country, c.no, c.text, got, c.want)
			}
		}
		expectCall(t, k, "ACME", run, "PRICE", `"true DE 7"`, `"DE"`)
		expectCall(t, k, "ACME", run, "PRICE", `"false default 7"`, `"FR"`)
		expectCall(t, k, "ACME", run, "STRICT", `"OTHER 1"`, `"AT"`)
		_, err := call(k, kernel.ExecOptions{Tenant: "ACME"}, run, "STRICT", `"FR"`)
		var exc *vm.Exception
		if !errors.As(err, &exc) || exc.Class != "/RT/NO_IMPLEMENTATION" ||
			!strings.Contains(exc.Text, `no active implementation of extension point /TRSTEPS/STRICT matches COUNTRY = "FR", and it has no fallback class`) {
			t.Fatalf("no implementation: %v", err)
		}
	})
}

// TestExtensionPointCommit rejects commit and rollback of the default
// session in implementations of an extension point that does not allow
// them: written in the implementing method when the code is compiled,
// elsewhere when it runs. Sessions of their own are not affected.
func TestExtensionPointCommit(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, stepsFiles("1.0.0"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		impl := func(extra string) map[string]string {
			return map[string]string{
				"manifest.json": `{ "id": "TRSTEPIMPL", "version": "1.0.0", "requires": [{ "id": "TRBASE" }, { "id": "TRSTEPS" }], "languageVersion": 1 }`,
				"src/A.class":   stepClass("A", extra),
				"src/HELPER.class": "public class /TRSTEPIMPL/HELPER\n\n  public static method SAVE()\n    commit\n  end\n\n" +
					"  public static method OWN()\n    var s = open session\n    commit s\n  end\n\nend\n",
				"schema/extension-implementations/A.json": implementation("A", "/TRSTEPS/STEPS", "A", `, "filters": { "COUNTRY": [{ "pattern": "*" }], "NO": [{ "pattern": "*" }] }`),
			}
		}
		_, err := k.Install(ctx, []*model.Package{writePackage(t, impl("    commit\n"))}, kernel.Options{})
		var ce *rterr.CompileError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "implements APPEND of extension point /TRSTEPS/STEPS, which does not allow implementations to commit the default session") {
			t.Fatalf("commit in the implementing method: %v", err)
		}
		if _, err := k.Install(ctx, []*model.Package{writePackage(t, impl("    /TRSTEPIMPL/HELPER.SAVE()\n"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		_, err = call(k, kernel.ExecOptions{Tenant: "ACME"}, "/TRSTEPS/RUN", "STEPS", `"DE"`, "1", `"x"`)
		var exc *vm.Exception
		if !errors.As(err, &exc) || exc.Class != "/RT/SESSION_ERROR" ||
			!strings.Contains(exc.Text, "commit of the default session is not allowed in an implementation of extension point /TRSTEPS/STEPS") {
			t.Fatalf("commit in a called method: %v", err)
		}
		if _, err := k.Upgrade(ctx, writePackage(t, impl("    /TRSTEPIMPL/HELPER.OWN()\n")), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", "/TRSTEPS/RUN", "STEPS", `"1 true x[A:r]"`, `"DE"`, "1", `"x"`)
	})
}
