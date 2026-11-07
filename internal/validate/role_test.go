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

package validate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// entries reads granted values as written in role files.
func entries(t *testing.T, js string) []model.ValueEntry {
	t.Helper()
	var out []model.ValueEntry
	if err := json.Unmarshal([]byte(js), &out); err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return out
}

// TestCheckAuthorization checks the rules for granted values against the
// fields of /TRFIN/COMPANY (COMPANY_ID CHAR(10), COUNTRY CHAR(2),
// ACTIVITY), /TRFIN/STATUS_SCOPE (STATUS with fixed values) and an object
// with INT4, BOOL and UUID fields.
func TestCheckAuthorization(t *testing.T) {
	s := withObject(t, func(o *model.AuthorizationObject) {
		o.Fields = []model.AuthorizationField{authDomain("COUNT", "/TRBASE/COUNTER"), authDomain("FLAG", "/TRBASE/FLAG"), authDomain("ID", "/TRBASE/GUID"),
			authDomain("CODE", "/TRBASE/COUNTRY_CODE"), activityField}
	})
	company := func(fields map[string]string) model.Authorization {
		a := model.Authorization{Object: "/TRFIN/COMPANY", Fields: map[string][]model.ValueEntry{
			"COMPANY_ID": entries(t, `[{"pattern":"*"}]`), "COUNTRY": entries(t, `[{"value":"DE"}]`), "ACTIVITY": entries(t, `[{"value":"READ"}]`)}}
		for f, js := range fields {
			if js == "" {
				delete(a.Fields, f)
				continue
			}
			a.Fields[f] = entries(t, js)
		}
		return a
	}
	if p := model.CheckAuthorization(s, company(nil)); len(p) != 0 {
		t.Fatalf("valid authorization: %v", p)
	}
	valid := []model.Authorization{
		company(map[string]string{"COUNTRY": `[{"from":"AT","to":"DE"},{"pattern":"D*"},{"value":""}]`, "ACTIVITY": `[{"pattern":"*"}]`}),
		{Object: "/TRS/COMPANY", Fields: map[string][]model.ValueEntry{"COUNT": entries(t, `[{"value":7},{"from":-3,"to":12},{"pattern":"12*"}]`),
			"FLAG": entries(t, `[{"value":true}]`), "ID": entries(t, `[{"value":"0F0E0D0C-0B0A-4908-8706-050403020100"},{"pattern":"*"}]`),
			"CODE": entries(t, `[{"pattern":"*"}]`), "ACTIVITY": entries(t, `[{"value":"UPDATE"}]`)}},
	}
	for _, a := range valid {
		if p := model.CheckAuthorization(s, a); len(p) != 0 {
			t.Errorf("valid authorization on %s: %v", a.Object, p)
		}
	}
	for _, c := range []struct {
		name string
		auth model.Authorization
		want string
	}{
		{"missing field", company(map[string]string{"COUNTRY": ""}), "field COUNTRY: is missing; write * to grant all values"},
		{"unknown field", company(map[string]string{"REGION": `[{"pattern":"*"}]`}), "field REGION: is no field of /TRFIN/COMPANY"},
		{"number for CHAR", company(map[string]string{"COUNTRY": `[{"value":49}]`}), "expected a string for CHAR"},
		{"too long", company(map[string]string{"COUNTRY": `[{"value":"DEU"}]`}), "value has 3 characters, CHAR(2) allows 2"},
		{"prefix too long", company(map[string]string{"COUNTRY": `[{"pattern":"DEU*"}]`}), `pattern "DEU*": value has 3 characters`},
		{"empty range", company(map[string]string{"COUNTRY": `[{"from":"SK","to":"PL"}]`}), "is empty; from must not be greater than to"},
		{"pattern inside", company(map[string]string{"COUNTRY": `[{"pattern":"D*E"}]`}), `pattern "D*E" is no pattern`},
		{"pattern in front", company(map[string]string{"COUNTRY": `[{"pattern":"*E"}]`}), `pattern "*E" is no pattern`},
		{"activity not allowed", company(map[string]string{"ACTIVITY": `[{"value":"APPROVE"}]`}), "APPROVE is not allowed for /TRFIN/COMPANY"},
		{"no activity", company(map[string]string{"ACTIVITY": `[{"value":"DANCE"}]`}), `"DANCE" is no fixed value of /RT/ACTIVITY`},
		{"range on ACTIVITY", company(map[string]string{"ACTIVITY": `[{"from":"CREATE","to":"READ"}]`}), "ACTIVITY takes single activities or *"},
		{"prefix on ACTIVITY", company(map[string]string{"ACTIVITY": `[{"pattern":"RE*"}]`}), "ACTIVITY takes single activities or *"},
		{"not a fixed value", model.Authorization{Object: "/TRFIN/STATUS_SCOPE", Fields: map[string][]model.ValueEntry{
			"STATUS": entries(t, `[{"value":"GONE"}]`), "COUNTRY": entries(t, `[{"pattern":"*"}]`)}}, `"GONE" is no fixed value of /TRFIN/COMPANY_STATUS`},
		{"text for INT4", model.Authorization{Object: "/TRS/COMPANY", Fields: map[string][]model.ValueEntry{"COUNT": entries(t, `[{"value":"7"}]`),
			"FLAG": entries(t, `[{"pattern":"*"}]`), "ID": entries(t, `[{"pattern":"*"}]`), "CODE": entries(t, `[{"pattern":"*"}]`), "ACTIVITY": entries(t, `[{"pattern":"*"}]`)}},
			"expected an integer for INT8"},
		{"range on BOOL", model.Authorization{Object: "/TRS/COMPANY", Fields: map[string][]model.ValueEntry{"COUNT": entries(t, `[{"pattern":"*"}]`),
			"FLAG": entries(t, `[{"from":false,"to":true}]`), "ID": entries(t, `[{"pattern":"*"}]`), "CODE": entries(t, `[{"pattern":"*"}]`), "ACTIVITY": entries(t, `[{"pattern":"*"}]`)}},
			"ranges need CHAR, INT4 or INT8, not BOOL"},
		{"prefix on UUID", model.Authorization{Object: "/TRS/COMPANY", Fields: map[string][]model.ValueEntry{"COUNT": entries(t, `[{"pattern":"*"}]`),
			"FLAG": entries(t, `[{"pattern":"*"}]`), "ID": entries(t, `[{"pattern":"0F*"}]`), "CODE": entries(t, `[{"pattern":"*"}]`), "ACTIVITY": entries(t, `[{"pattern":"*"}]`)}},
			"patterns other than * need CHAR, INT4 or INT8, not UUID"},
		{"unknown object", model.Authorization{Object: "/TRFIN/NOPE"}, "authorization object /TRFIN/NOPE does not exist"},
	} {
		var got []string
		for _, p := range model.CheckAuthorization(s, c.auth) {
			got = append(got, p.String())
		}
		if !strings.Contains(strings.Join(got, "; "), c.want) {
			t.Errorf("%s: %v, want %q", c.name, got, c.want)
		}
	}
}

// TestValueEntryJSON rejects null and unknown forms of entries.
func TestValueEntryJSON(t *testing.T) {
	for js, want := range map[string]string{
		`{"value":null}`:         "null is never granted",
		`{"from":"A","to":null}`: "null is never granted",
		`{"value":"A","to":"B"}`: `an entry is {"value": v}`,
		`{"pattern":7}`:          "a pattern is a string",
		`{"value":{"x":1}}`:      "must be a string, a number or a boolean",
	} {
		var e model.ValueEntry
		if err := json.Unmarshal([]byte(js), &e); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", js, err, want)
		}
	}
}

func role(name string, auths ...model.Authorization) model.Role {
	return model.Role{Name: name, Texts: en, Authorizations: auths}
}

func composite(name string, members ...string) model.Role {
	return model.Role{Name: name, Texts: en, Composite: true, Roles: members}
}

// TestRoleRules checks names and references of roles and composite roles.
func TestRoleRules(t *testing.T) {
	company := model.Authorization{Object: "/TRFIN/COMPANY", Fields: map[string][]model.ValueEntry{}}
	withRoles := func(roles ...model.Role) *model.Schema {
		return withTRS(t, trs(model.Package{Roles: roles}))
	}
	if err := Schema(withRoles(role("/TRS/CLERK", company), composite("/TRS/ALL", "/TRS/CLERK", "/TRFIN/COMPANY_CLERK")), Options{}); err != nil {
		t.Fatalf("valid roles: %v", err)
	}
	for _, c := range []struct {
		name  string
		roles []model.Role
		code  rterr.Code
		want  string
	}{
		{"namespace", []model.Role{role("/TRFIN/MINE")}, rterr.CodeName, "must start with the namespace /TRS/"},
		{"unknown object", []model.Role{role("/TRS/X", model.Authorization{Object: "/TRS/NOPE"})}, rterr.CodeRole, "authorization object /TRS/NOPE does not exist"},
		{"object not required", []model.Role{role("/TRS/X", model.Authorization{Object: "/TROTTO/SYNC"})}, rterr.CodeNotVisible, "/TROTTO/SYNC belongs to TROTTO, which TRS does not require"},
		{"composite in composite", []model.Role{role("/TRS/A"), composite("/TRS/B", "/TRS/A"), composite("/TRS/C", "/TRS/B")}, rterr.CodeRole, "/TRS/B is a composite role"},
		{"all authorizations", []model.Role{composite("/TRS/B", model.AllAuthorizationsRole)}, rterr.CodeReservedObject, "is no member of composite roles"},
		{"unknown member", []model.Role{composite("/TRS/B", "/TRS/NOPE")}, rterr.CodeRole, "role /TRS/NOPE does not exist"},
		{"member twice", []model.Role{role("/TRS/A"), composite("/TRS/B", "/TRS/A", "/TRS/A")}, rterr.CodeDuplicate, "listed twice"},
		{"member not required", []model.Role{composite("/TRS/B", "/TROTTO/SYNC_OPERATOR")}, rterr.CodeNotVisible, "role /TROTTO/SYNC_OPERATOR belongs to TROTTO"},
		{"empty composite", []model.Role{composite("/TRS/B")}, rterr.CodeRole, "at least one role"},
		{"no English", []model.Role{{Name: "/TRS/X", Texts: map[string]model.Label{"de": {Label: "X"}}}}, rterr.CodeText, "texts in English"},
	} {
		t.Run(c.name, func(t *testing.T) { expect(t, withRoles(c.roles...), c.code, c.want) })
	}
	// A local role in a composite role of a package is not visible; local
	// composite roles take roles of any package.
	s := withRoles(role("/TRS/A"))
	s.Roles["/LOCAL/MINE"] = &model.Role{Name: "/LOCAL/MINE", Owner: model.Local, Texts: en}
	s.Roles["/TRS/B"] = &model.Role{Name: "/TRS/B", Owner: "TRS", Texts: en, Composite: true, Roles: []string{"/LOCAL/MINE"}}
	expect(t, s, rterr.CodeNotVisible, "role /LOCAL/MINE belongs to LOCAL, which TRS does not require")
	delete(s.Roles, "/TRS/B")
	s.Roles["/LOCAL/OPS"] = &model.Role{Name: "/LOCAL/OPS", Owner: model.Local, Texts: en, Composite: true, Roles: []string{"/TROTTO/SYNC_OPERATOR", "/TRS/A"}}
	if err := Schema(s, Options{}); err != nil {
		t.Errorf("local composite role: %v", err)
	}
}
