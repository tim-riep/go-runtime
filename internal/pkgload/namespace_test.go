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

package pkgload

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/rterr"
)

func violations(t *testing.T, fsys fstest.MapFS) []*rterr.NamespaceViolation {
	t.Helper()
	pkg, err := Load(fsys, "mem")
	if pkg != nil {
		t.Fatal("a package with violations must not be built")
	}
	return rterr.All[*rterr.NamespaceViolation](err)
}

func expectViolation(t *testing.T, fsys fstest.MapFS, file, pointer, name string) {
	t.Helper()
	for _, v := range violations(t, fsys) {
		if v.Location.File == file && v.Location.Pointer == pointer && v.Name == name {
			return
		}
	}
	_, err := Load(fsys, "mem")
	t.Fatalf("no namespace violation for %s at %s%s in:\n%v", name, file, pointer, err)
}

func TestForeignNamespace(t *testing.T) {
	fsys := validFS()
	fsys["schema/domains/QTY.json"] = file(`{"name":"/TRBASE/QTY","type":"INT4"}`)
	expectViolation(t, fsys, "schema/domains/QTY.json", "/name", "/TRBASE/QTY")
	v := violations(t, fsys)[0]
	if v.Reference || !slices.Equal(v.Expected, []string{"/TRX/"}) || !strings.Contains(v.Error(), "expected namespace /TRX/") {
		t.Fatalf("violation: %+v", v)
	}
}

func TestMissingNamespace(t *testing.T) {
	fsys := validFS()
	fsys["schema/domains/QTY.json"] = file(`{"name":"QTY","type":"INT4"}`)
	expectViolation(t, fsys, "schema/domains/QTY.json", "/name", "QTY")
	fsys = validFS()
	fsys["schema/tables/ORDERS.json"] = file(`{"name":"/TRX/ORDERS","tenantSpecific":false,"dataClass":"application",
		"fields":[{"name":"ORDER_ID","domain":"STATUS"}],"primaryKey":["ORDER_ID"]}`)
	expectViolation(t, fsys, "schema/tables/ORDERS.json", "/fields/0/domain", "STATUS")
}

func TestReferenceToPackageNotRequired(t *testing.T) {
	fsys := validFS()
	fsys["schema/tables/ORDERS.json"] = file(`{"name":"/TRX/ORDERS","tenantSpecific":false,"dataClass":"application",
		"fields":[{"name":"ORDER_ID","dataElement":"/TRFIN/COMPANY"}],"primaryKey":["ORDER_ID"]}`)
	expectViolation(t, fsys, "schema/tables/ORDERS.json", "/fields/0/dataElement", "/TRFIN/COMPANY")
	v := violations(t, fsys)[0]
	if !v.Reference || !slices.Equal(v.Expected, []string{"/TRX/", "/TRBASE/", "/RT/"}) {
		t.Fatalf("violation: %+v", v)
	}
	// Kernel objects may always be referenced.
	fsys = validFS()
	fsys["schema/tables/ORDERS.json"] = file(`{"name":"/TRX/ORDERS","tenantSpecific":false,"dataClass":"application",
		"fields":[{"name":"OWNER","domain":"/RT/TENANT_ID"}],"primaryKey":["OWNER"]}`)
	if _, err := Load(fsys, "mem"); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionNamesNeedNamespace(t *testing.T) {
	fsys := validFS()
	fsys["schema/extensions/COMPANIES.json"] = file(`{"table":"/TRBASE/COMPANIES",
		"fields":[{"name":"EXT_ID","domain":"/TRBASE/ID"}],
		"foreignKeys":[{"name":"/TRBASE/EXT","table":"/TRX/ORDERS","fields":[{"field":"EXT_ID","references":"ORDER_ID"}]}]}`)
	expectViolation(t, fsys, "schema/extensions/COMPANIES.json", "/fields/0/name", "EXT_ID")
	expectViolation(t, fsys, "schema/extensions/COMPANIES.json", "/foreignKeys/0/name", "/TRBASE/EXT")
	expectViolation(t, fsys, "schema/extensions/COMPANIES.json", "/foreignKeys/0/fields/0/field", "EXT_ID")
}

// TestBusinessObjectExtensionNames requires the namespace of the package
// for the determinations, validations and actions of an extension.
func TestBusinessObjectExtensionNames(t *testing.T) {
	fsys := validFS()
	const f = "schema/business-object-extensions/ORDER.json"
	fsys[f] = file(`{"businessObject":"/TRBASE/ORDER","behaviorClass":"/TRX/ORDER_BEHAVIOR","entities":{"/TRBASE/ORDERS":{
		"determinations":[{"name":"FILL","when":"save","on":["create"]}],
		"validations":[{"name":"/TRBASE/CHECK","on":["create"]}],
		"actions":[{"name":"/TRX/SYNC","activity":"EXECUTE"}]}}}`)
	expectViolation(t, fsys, f, "/entities//TRBASE/ORDERS/validations/0/name", "/TRBASE/CHECK")
	for _, v := range violations(t, fsys) {
		if v.Name == "/TRX/SYNC" || v.Name == "/TRBASE/ORDER" {
			t.Errorf("violation of a valid name: %+v", v)
		}
	}
}

// TestServiceNames checks the names a service refers to, including
// actions, hidden fields and foreign keys of navigations with namespace.
func TestServiceNames(t *testing.T) {
	fsys := validFS()
	const f = "schema/services/SVC.json"
	fsys[f] = file(`{"name":"/TRX/SVC","description":{"en":"S"},"entitySets":{"Orders":{"businessObject":"/TRBASE/ORDER",
		"entity":"/TRBASE/ORDERS","actions":["/TRZ/SYNC"],"hiddenFields":["/TRX/NOTE"],"navigations":{"/TRZ/FK":"Z"}}}}`)
	expectViolation(t, fsys, f, "/entitySets/Orders/actions", "/TRZ/SYNC")
	expectViolation(t, fsys, f, "/entitySets/Orders/navigations//TRZ/FK", "/TRZ/FK")
	for _, v := range violations(t, fsys) {
		if v.Name == "/TRX/NOTE" || v.Name == "/TRBASE/ORDER" || v.Name == "/TRX/SVC" {
			t.Errorf("violation of a valid name: %+v", v)
		}
	}
}

func TestSeveralViolationsReportedTogether(t *testing.T) {
	fsys := validFS()
	fsys["schema/domains/QTY.json"] = file(`{"name":"/TRBASE/QTY","type":"INT4"}`)
	fsys["schema/domains/STATUS.json"] = file(`{"name":"STATUS","type":"CHAR","length":3}`)
	fsys["schema/business-objects/SETUP.json"] = file(`{"name":"/TRX/SETUP","description":{"en":"Setup"},"root":"/TRFIN/COMPANIES",
		"entities":{"/TRFIN/COMPANIES":{}}}`)
	if n := len(violations(t, fsys)); n != 4 {
		_, err := Load(fsys, "mem")
		t.Fatalf("expected 4 violations, got %d:\n%v", n, err)
	}
}

// TestSettingsRejected rejects the directories of settings, which
// business objects replace.
func TestSettingsRejected(t *testing.T) {
	for _, dir := range []string{"settings", "setting-extensions"} {
		fsys := validFS()
		p := "schema/" + dir + "/PARTNERS.json"
		fsys[p] = file(`{"id":"/TRX/PARTNERS"}`)
		_, err := Load(fsys, "mem")
		if err == nil || !strings.Contains(err.Error(), "schema/"+dir+": unexpected entry") {
			t.Errorf("%s: %v", dir, err)
		}
	}
}

func TestUnknownFiles(t *testing.T) {
	for _, p := range []string{"README.md", "notes.json", "schema/domains/QTY.txt", "schema/tables/sub/X.json", "assets/logo.png",
		"objects/X.json", "objects/TRX/X.json", "objects/TRX/T/sub/X.json", "objects/TRX/T/X.txt", "objects/trx/T/X.json"} {
		fsys := validFS()
		fsys[p] = file("x")
		_, err := Load(fsys, "mem")
		found := false
		for _, v := range rterr.All[*rterr.ValidationError](err) {
			if v.Location.File == p || strings.HasPrefix(p, v.Location.File+"/") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s not rejected: %v", p, err)
		}
	}
}

// TestEveryObjectTypeChecksNamespaces makes sure every registered object
// type takes part in the namespace check: each one must reject a document
// with a name in a foreign namespace. Only asset repositories, which have
// no names, and stylesheets, whose names come from their file names in the
// own namespace, are exempt.
func TestEveryObjectTypeChecksNamespaces(t *testing.T) {
	foreign := map[string]struct{ path, doc string }{
		"domain":       {"schema/domains/X.json", `{"name":"/TRZ/X","type":"BOOL"}`},
		"data element": {"schema/data-elements/X.json", `{"name":"/TRZ/X","domain":"/TRX/QTY","texts":{"en":{"short":"X","medium":"X","long":"X","heading":"X"}}}`},
		"table": {"schema/tables/X.json", `{"name":"/TRZ/X","tenantSpecific":false,"dataClass":"application",
			"fields":[{"name":"A","domain":"/TRX/QTY"}],"primaryKey":["A"]}`},
		"extension":                  {"schema/extensions/X.json", `{"table":"/TRZ/X","fields":[{"name":"/TRX/A","domain":"/TRX/QTY"}]}`},
		"structure":                  {"schema/structures/X.json", `{"name":"/TRZ/X","texts":{"en":{"label":"X"}},"components":[{"name":"A","domain":"/TRX/QTY"}]}`},
		"array type":                 {"schema/array-types/X.json", `{"name":"/TRZ/X","texts":{"en":{"label":"X"}},"rowType":{"domain":"/TRX/QTY"}}`},
		"structure extension":        {"schema/structure-extensions/X.json", `{"structure":"/TRZ/X","components":[{"name":"/TRX/A","domain":"/TRX/QTY"}]}`},
		"class":                      {"src/X.class", "public class /TRZ/X\nend\n"},
		"interface":                  {"src/X.interface", "public interface /TRZ/X\nend\n"},
		"include":                    {"src/X.include", "statements include /TRX/X\n  log info \"{/TRZ/Y.A}\"\nend\n"},
		"transformation description": {"transformations/X.json", `{"name":"/TRZ/X"}`},
		"authorization object":       {"schema/authorization-objects/X.json", `{"name":"/TRZ/X","texts":{"en":{"label":"X"}},"fields":[{"name":"A","domain":"/TRZ/Y"}]}`},
		"role":                       {"schema/roles/X.json", `{"name":"/TRZ/X","texts":{"en":{"label":"X"}},"roles":["/TRZ/Y"]}`},
		"extension point": {"schema/extension-points/X.json", `{"name":"/TRX/X","description":{"en":"X"},"interface":"/TRZ/I","use":"single",
			"filters":[{"name":"A","domain":"/TRX/QTY"}]}`},
		"implementation": {"schema/extension-implementations/X.json", `{"name":"/TRX/X","description":{"en":"X"},"extensionPoint":"/TRZ/P",
			"class":"/TRX/C"}`},
		"business object": {"schema/business-objects/X.json", `{"name":"/TRX/X","description":{"en":"X"},"root":"/TRZ/T",
			"behaviorClass":"/TRX/B","entities":{"/TRZ/T":{}}}`},
		"business object extension": {"schema/business-object-extensions/X.json", `{"businessObject":"/TRZ/X","entities":{"/TRZ/T":{}}}`},
		"service":                   {"schema/services/X.json", `{"name":"/TRX/X","description":{"en":"X"},"entitySets":{"Xs":{"businessObject":"/TRZ/X","entity":"/TRX/T"}}}`},
		"http service":              {"schema/http-services/X.json", `{"path":"x","texts":{"en":{"label":"X"}},"handlers":[{"class":"/TRZ/H"}]}`},
		"object type":               {"schema/object-types/X.json", `{"name":"/TRX/X","description":{"en":"X"},"content":"/TRZ/C"}`},
		"object":                    {"objects/TRZ/T/X.json", `{"name":"/TRX/X","content":{}}`},
	}
	for _, typ := range ObjectTypes() {
		if typ.Definitions == nil || typ.References == nil {
			t.Fatalf("%s has no namespace check", typ.Kind)
		}
		sample, ok := foreign[typ.Kind]
		if !ok {
			if typ.Kind != "asset repository" && typ.Kind != "transformation" {
				t.Errorf("object type %s has no namespace test; add a sample with a foreign namespace", typ.Kind)
			}
			continue
		}
		fsys := validFS()
		fsys[sample.path] = file(sample.doc)
		found := false
		for _, v := range violations(t, fsys) {
			if v.Location.File == sample.path && strings.HasPrefix(v.Name, "/TRZ/") {
				found = true
			}
		}
		if !found {
			_, err := Load(fsys, "mem")
			t.Errorf("object type %s accepted a foreign namespace: %v", typ.Kind, err)
		}
	}
}

func TestRegisterRejectsIncompleteTypes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("register accepted a type without namespace check")
		}
	}()
	register(ObjectType{Kind: "incomplete", Document: jsonIn("schema/x"), Schema: "x.schema.json", NewDoc: func() any { return nil }})
}
