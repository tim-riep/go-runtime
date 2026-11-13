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
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
)

// The package TRDEF defines the object type /TRDEF/VIEW, whose content is
// deep: texts, columns and groups of fields of an entity, and a reference
// to another view. It owns the business object /TRDEF/NOTE.
var trdefFiles = map[string]string{
	"manifest.json": `{"id":"TRDEF","version":"1.0.0","requires":[{"id":"TRBASE"}]}`,
	"schema/object-types/VIEW.json": `{"name":"/TRDEF/VIEW","description":{"en":"View","de":"Ansicht"},
		"content":"/TRDEF/VIEW"}`,
	"schema/data-elements/VIEW_NAME.json": `{"name":"/TRDEF/VIEW_NAME","domain":"/TRBASE/REF_OBJECT","objectType":"/TRDEF/VIEW",
		"texts":{"en":{"short":"View","medium":"View","long":"View","heading":"View"}}}`,
	"schema/domains/IMPORTANCE.json": `{"name":"/TRDEF/IMPORTANCE","type":"CHAR","length":6,"values":[{"value":"HIGH"},{"value":"low","name":"LOW"}]}`,
	"schema/structures/VIEW.json": `{"name":"/TRDEF/VIEW","texts":{"en":{"label":"View"}},"components":[
		{"name":"ENTITY","dataElement":"/TRBASE/REF_ENTITY","notNull":true},
		{"name":"TEXTS","arrayType":"/TRDEF/VIEW_TEXTS"},
		{"name":"COLUMNS","arrayType":"/TRDEF/COLUMNS"},
		{"name":"GROUPS","arrayType":"/TRDEF/GROUPS"},
		{"name":"NEXT","dataElement":"/TRDEF/VIEW_NAME"}]}`,
	"schema/structures/VIEW_TEXT.json": `{"name":"/TRDEF/VIEW_TEXT","texts":{"en":{"label":"Text"}},"components":[
		{"include":"/TRBASE/TEXT_LANGUAGE"},{"name":"TITLE","domain":"/TRBASE/NAME","notNull":true}]}`,
	"schema/array-types/VIEW_TEXTS.json": `{"name":"/TRDEF/VIEW_TEXTS","texts":{"en":{"label":"Texts"}},"rowType":{"structure":"/TRDEF/VIEW_TEXT"},
		"key":{"fields":["LANGUAGE"],"unique":true}}`,
	"schema/structures/COLUMN.json": `{"name":"/TRDEF/COLUMN","texts":{"en":{"label":"Column"}},"components":[
		{"name":"FIELD","dataElement":"/TRBASE/REF_FIELD","notNull":true},
		{"name":"WIDTH","domain":"/TRBASE/COUNTER"},
		{"name":"IMPORTANCE","domain":"/TRDEF/IMPORTANCE","notNull":true,"default":"HIGH"}]}`,
	"schema/array-types/COLUMNS.json": `{"name":"/TRDEF/COLUMNS","texts":{"en":{"label":"Columns"}},"rowType":{"structure":"/TRDEF/COLUMN"}}`,
	"schema/structures/GROUP.json": `{"name":"/TRDEF/GROUP","texts":{"en":{"label":"Group"}},"components":[
		{"name":"ID","domain":"/TRBASE/NAME","notNull":true},{"name":"FIELDS","arrayType":"/TRDEF/COLUMNS"}]}`,
	"schema/array-types/GROUPS.json": `{"name":"/TRDEF/GROUPS","texts":{"en":{"label":"Groups"}},"rowType":{"structure":"/TRDEF/GROUP"}}`,
	"schema/tables/NOTES.json": `{"name":"/TRDEF/NOTES","tenantSpecific":false,"dataClass":"configuration","fields":[
		{"name":"NOTE_ID","domain":"/TRBASE/NAME"},{"name":"TITLE","domain":"/TRBASE/NAME"},{"name":"BODY","domain":"/TRBASE/TEXT"}],
		"primaryKey":["NOTE_ID"]}`,
	"schema/business-objects/NOTE.json": `{"name":"/TRDEF/NOTE","description":{"en":"Note"},"dataClass":"configuration",
		"root":"/TRDEF/NOTES","entities":{"/TRDEF/NOTES":{"operations":["create","update","delete"]}}}`,
}

// TRUSE ships views of the notes of TRDEF and of the companies of TRFIN,
// with the field that TROTTO adds.
func truseFiles() map[string]string {
	return map[string]string{
		"manifest.json": `{"id":"TRUSE","version":"1.0.0","requires":[{"id":"TRBASE"},{"id":"TRDEF"},{"id":"TRFIN"},{"id":"TROTTO"}]}`,
		"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES",
			"TEXTS":[{"LANGUAGE":"en","TITLE":"Notes"},{"LANGUAGE":"de","TITLE":"Notizen"}],
			"COLUMNS":[{"FIELD":"TITLE"},{"FIELD":"BODY","WIDTH":20,"IMPORTANCE":"LOW"}],
			"NEXT":"/TRUSE/COMPANIES"}}`,
		"objects/TRDEF/VIEW/COMPANIES.json": `{"name":"/TRUSE/COMPANIES","content":{"ENTITY":"/TRFIN/COMPANIES",
			"TEXTS":[{"LANGUAGE":"en","TITLE":"Companies"}],
			"GROUPS":[{"ID":"MAIN","FIELDS":[{"FIELD":"NAME"},{"FIELD":"/TROTTO/OTTO_ID"}]}]}}`,
	}
}

func withFiles(files map[string]string, changes map[string]string) map[string]string {
	out := map[string]string{}
	for p, d := range files {
		out[p] = d
	}
	for p, d := range changes {
		if d == "" {
			delete(out, p)
			continue
		}
		out[p] = d
	}
	return out
}

func installObjects(t *testing.T, k *kernel.Kernel, extra ...*model.Package) (*plan.Plan, error) {
	t.Helper()
	initialized(t, k)
	pkgs := []*model.Package{testutil.Example(t, "TRFIN"), testutil.Example(t, "TROTTO"), writePackage(t, trdefFiles)}
	return k.Install(context.Background(), append(pkgs, extra...), kernel.Options{})
}

func TestObjectsInstall(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		p, err := installObjects(t, k, writePackage(t, truseFiles()))
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, c := range p.Changes {
			if strings.HasPrefix(string(c.Kind), "create-object") {
				kinds = append(kinds, string(c.Kind)+" "+c.Name)
			}
		}
		want := []string{"create-object-type /TRDEF/VIEW", "create-object /TRUSE/COMPANIES", "create-object /TRUSE/NOTES"}
		if !slices.Equal(kinds, want) {
			t.Errorf("changes %v, want %v", kinds, want)
		}
		s, err := k.Schema(ctx)
		if err != nil {
			t.Fatal(err)
		}
		o := s.Objects["/TRUSE/NOTES"]
		if o == nil || o.Owner != "TRUSE" || o.Type != "/TRDEF/VIEW" || len(o.Hash) != 64 {
			t.Fatalf("object: %+v", o)
		}
		// The canonical content has every component; defaults fill notNull
		// components, absent optional ones are null, fixed values are names.
		var content map[string]any
		if err := json.Unmarshal(o.Content, &content); err != nil {
			t.Fatal(err)
		}
		cols := content["COLUMNS"].([]any)
		first := cols[0].(map[string]any)
		if first["IMPORTANCE"] != "HIGH" || first["WIDTH"] != nil || content["GROUPS"] != nil {
			t.Errorf("canonical content: %s", o.Content)
		}
		if cols[1].(map[string]any)["IMPORTANCE"] != "LOW" {
			t.Errorf("fixed value by name: %s", o.Content)
		}
		refs := map[string]bool{}
		for _, r := range s.Objects["/TRUSE/COMPANIES"].References {
			refs[string(r.Kind)+" "+r.Target+" "+r.Part+" "+r.Pointer] = true
		}
		for _, w := range []string{"entity /TRFIN/COMPANIES  /ENTITY", "field /TRFIN/COMPANIES NAME /GROUPS/0/FIELDS/0/FIELD",
			"field /TRFIN/COMPANIES /TROTTO/OTTO_ID /GROUPS/0/FIELDS/1/FIELD"} {
			if !refs[w] {
				t.Errorf("reference %q missing: %v", w, refs)
			}
		}
		next := s.Objects["/TRUSE/NOTES"].References
		if r := next[len(next)-1]; r.Kind != model.RefObject || r.Target != "/TRUSE/COMPANIES" || r.ObjectType != "/TRDEF/VIEW" {
			t.Errorf("object reference: %+v", r)
		}
		if s.DataElements["/TRDEF/VIEW_NAME"].ObjectType.Or("") != "/TRDEF/VIEW" {
			t.Error("object type of the data element not stored")
		}
	})
}

// objectErrors returns the messages of all validation errors of err.
func objectErrors(err error) string {
	var out []string
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		out = append(out, v.Location.String()+": "+v.Message)
	}
	for _, v := range rterr.All[*rterr.NamespaceViolation](err) {
		out = append(out, v.Location.String()+": namespace "+v.Name)
	}
	return strings.Join(out, "\n")
}

func TestObjectsRejected(t *testing.T) {
	cases := []struct {
		name    string
		changes map[string]string
		want    string
	}{
		{"reference into a package not required", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRORDER/ORDERS"}}`,
		}, "namespace /TRORDER/ORDERS"},
		{"unknown type", map[string]string{
			"objects/TRDEF/NOPE/X.json": `{"name":"/TRUSE/X","content":{}}`,
		}, "object type /TRDEF/NOPE does not exist"},
		{"type of a package not required", map[string]string{
			"objects/TRORDER/VIEW/X.json": `{"name":"/TRUSE/X","content":{}}`,
		}, "namespace /TRORDER/VIEW"},
		{"field that does not exist", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES","COLUMNS":[{"FIELD":"NOPE"}]}}`,
		}, "NOPE is no field of /TRDEF/NOTES"},
		{"field without entity", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"COLUMNS":[{"FIELD":"TITLE"}]}}`,
		}, "/TRDEF/VIEW needs ENTITY"},
		{"no English text", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES","TEXTS":[{"LANGUAGE":"de","TITLE":"Notizen"}]}}`,
		}, "texts in English (en) are mandatory"},
		{"unknown component", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES","COLOR":"red"}}`,
		}, "/TRDEF/VIEW has no component COLOR"},
		{"duplicate language", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES",
				"TEXTS":[{"LANGUAGE":"en","TITLE":"A"},{"LANGUAGE":"en","TITLE":"B"}]}}`,
		}, "same key"},
		{"object of the wrong type", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES","NEXT":"/TRUSE/NOPE"}}`,
		}, "object /TRUSE/NOPE does not exist"},
		{"wrong file name", map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": "",
			"objects/TRDEF/VIEW/OTHER.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES"}}`,
		}, "must be named NOTES.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
				files := withFiles(truseFiles(), c.changes)
				dir := t.TempDir()
				for p, data := range files {
					testutil.WriteFile(t, dir, p, []byte(data))
				}
				pkg, err := kernel.LoadDir(dir)
				if err == nil {
					_, err = installObjects(t, k, pkg)
				}
				if err == nil || !strings.Contains(objectErrors(err)+err.Error(), c.want) {
					t.Fatalf("want %q, got %v\n%s", c.want, err, objectErrors(err))
				}
			})
		})
	}
}

func TestObjectTextsInLanguageNotSetUp(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		files := withFiles(truseFiles(), map[string]string{
			"objects/TRDEF/VIEW/NOTES.json": `{"name":"/TRUSE/NOTES","content":{"ENTITY":"/TRDEF/NOTES",
				"TEXTS":[{"LANGUAGE":"en","TITLE":"Notes"},{"LANGUAGE":"fr","TITLE":"Notes"}]}}`,
		})
		_, err := installObjects(t, k, writePackage(t, files))
		var missing *rterr.MissingLanguages
		if !errors.As(err, &missing) || !slices.Equal(missing.Languages, []string{"fr"}) {
			t.Fatalf("want missing language fr, got %v", err)
		}
	})
}

func TestObjectTypeRejected(t *testing.T) {
	cases := []struct {
		name    string
		changes map[string]string
		want    string
	}{
		{"field without entity in scope", map[string]string{
			"schema/structures/VIEW.json": `{"name":"/TRDEF/VIEW","texts":{"en":{"label":"View"}},"components":[
				{"name":"COLUMNS","arrayType":"/TRDEF/COLUMNS"}]}`,
		}, "no component of the structure or of an enclosing structure"},
		{"texts without unique key", map[string]string{
			"schema/array-types/VIEW_TEXTS.json": `{"name":"/TRDEF/VIEW_TEXTS","texts":{"en":{"label":"Texts"}},"rowType":{"structure":"/TRDEF/VIEW_TEXT"}}`,
		}, "needs the unique key LANGUAGE"},
		{"content of another package", map[string]string{
			"schema/object-types/VIEW.json": `{"name":"/TRDEF/VIEW","description":{"en":"View"},"content":"/TRBASE/ADDRESS"}`,
		}, "must belong to TRDEF"},
		{"object type of a data element without reference domain", map[string]string{
			"schema/data-elements/VIEW_NAME.json": `{"name":"/TRDEF/VIEW_NAME","domain":"/TRBASE/NAME","objectType":"/TRDEF/VIEW",
				"texts":{"en":{"short":"View","medium":"View","long":"View","heading":"View"}}}`,
		}, "only data elements with the domain /TRBASE/REF_OBJECT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
				initialized(t, k)
				_, err := k.Install(context.Background(), []*model.Package{writePackage(t, withFiles(trdefFiles, c.changes))}, kernel.Options{})
				if err == nil || !strings.Contains(objectErrors(err), c.want) {
					t.Fatalf("want %q, got %v\n%s", c.want, err, objectErrors(err))
				}
			})
		})
	}
}

func TestObjectUpgradeProtection(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := installObjects(t, k, writePackage(t, truseFiles())); err != nil {
			t.Fatal(err)
		}
		// The content structure gets a component that the installed
		// objects lack.
		up := withFiles(trdefFiles, map[string]string{
			"manifest.json": `{"id":"TRDEF","version":"1.1.0","requires":[{"id":"TRBASE"}]}`,
			"schema/structures/COLUMN.json": `{"name":"/TRDEF/COLUMN","texts":{"en":{"label":"Column"}},"components":[
				{"name":"FIELD","dataElement":"/TRBASE/REF_FIELD","notNull":true},
				{"name":"LABEL","domain":"/TRBASE/NAME","notNull":true}]}`,
		})
		_, err := k.Upgrade(ctx, writePackage(t, up), kernel.Options{})
		if err == nil || !strings.Contains(err.Error(), "object /TRUSE/NOTES of package TRUSE") || !strings.Contains(objectErrors(err), "needs LABEL") {
			t.Fatalf("structure change: %v\n%s", err, objectErrors(err))
		}
		// The business object loses a field that an object refers to.
		up = withFiles(trdefFiles, map[string]string{
			"manifest.json": `{"id":"TRDEF","version":"1.1.0","requires":[{"id":"TRBASE"}]}`,
			"schema/tables/NOTES.json": `{"name":"/TRDEF/NOTES","tenantSpecific":false,"dataClass":"configuration","fields":[
				{"name":"NOTE_ID","domain":"/TRBASE/NAME"},{"name":"TITLE","domain":"/TRBASE/NAME"}],"primaryKey":["NOTE_ID"]}`,
		})
		_, err = k.Upgrade(ctx, writePackage(t, up), kernel.Options{AllowDestructive: true})
		if err == nil || !strings.Contains(objectErrors(err), "BODY is no field of /TRDEF/NOTES") {
			t.Fatalf("field removal: %v\n%s", err, objectErrors(err))
		}
		// A compatible change is accepted and rewrites the canonical content.
		up = withFiles(trdefFiles, map[string]string{
			"manifest.json": `{"id":"TRDEF","version":"1.1.0","requires":[{"id":"TRBASE"}]}`,
			"schema/structures/COLUMN.json": `{"name":"/TRDEF/COLUMN","texts":{"en":{"label":"Column"}},"components":[
				{"name":"FIELD","dataElement":"/TRBASE/REF_FIELD","notNull":true},
				{"name":"WIDTH","domain":"/TRBASE/COUNTER"},
				{"name":"IMPORTANCE","domain":"/TRDEF/IMPORTANCE","notNull":true,"default":"HIGH"},
				{"name":"LABEL","domain":"/TRBASE/NAME"}]}`,
		})
		p, err := k.Upgrade(ctx, writePackage(t, up), kernel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		altered := false
		for _, c := range p.Changes {
			altered = altered || c.Kind == plan.AlterObject && c.Name == "/TRUSE/NOTES"
		}
		if !altered {
			t.Errorf("no alter-object: %+v", p.Changes)
		}
		s, _ := k.Schema(ctx)
		if !strings.Contains(string(s.Objects["/TRUSE/NOTES"].Content), `"LABEL":null`) {
			t.Errorf("content not rewritten: %s", s.Objects["/TRUSE/NOTES"].Content)
		}
	})
}

func TestObjectsUninstallAndLanguage(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		if _, err := installObjects(t, k, writePackage(t, truseFiles())); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Uninstall(ctx, "TRDEF", kernel.Options{Confirm: true}); err == nil {
			t.Fatal("TRDEF removed while TRUSE requires it")
		}
		if _, err := k.Languages().Remove(ctx, "de", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		s, _ := k.Schema(ctx)
		if c := string(s.Objects["/TRUSE/NOTES"].Content); strings.Contains(c, `"de"`) || !strings.Contains(c, `"en"`) {
			t.Errorf("texts after removing de: %s", c)
		}
		if _, err := k.Uninstall(ctx, "TRUSE", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		s, _ = k.Schema(ctx)
		if len(s.Objects) != 0 || s.ObjectTypes["/TRDEF/VIEW"] == nil {
			t.Errorf("after uninstall: %d objects", len(s.Objects))
		}
	})
}
