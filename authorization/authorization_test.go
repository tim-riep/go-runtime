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

package authorization_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/model"
)

func schema(t *testing.T) *model.Schema {
	t.Helper()
	return testutil.AddPackages(t, testutil.KernelSchema(t), testutil.Example(t, "TRBASE"), testutil.Example(t, "TRFIN"), testutil.Example(t, "TROTTO"))
}

func day(s string) time.Time {
	d, err := time.Parse(model.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func open(id, role string) authorization.Assignment { return authorization.Assignment{ID: id, Role: role} }

func entries(t *testing.T, js string) []model.ValueEntry {
	t.Helper()
	var out []model.ValueEntry
	if err := json.Unmarshal([]byte(js), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type check struct {
	values map[string]string
	want   bool
}

func expect(t *testing.T, e *authorization.Effective, object string, checks ...check) {
	t.Helper()
	for _, c := range checks {
		if got := e.Allows(object, c.values); got != c.want {
			t.Errorf("%s %v = %v, want %v", object, c.values, got, c.want)
		}
	}
}

func company(id, country, activity string) map[string]string {
	return map[string]string{"COMPANY_ID": id, "COUNTRY": country, "ACTIVITY": activity}
}

// TestClerk evaluates /TRFIN/COMPANY_CLERK: and over fields, or over the
// entries of a field, ranges, * with "" and activities.
func TestClerk(t *testing.T) {
	e := authorization.Resolve(schema(t), "anna", "ACME", day("2026-11-08"), false, []authorization.Assignment{open("a1", "/TRFIN/COMPANY_CLERK")})
	expect(t, e, "/TRFIN/COMPANY",
		check{company("C1", "DE", "READ"), true},
		check{company("", "AT", "UPDATE"), true}, // * includes ""
		check{company("C1", "PL", "READ"), true},  // lower bound
		check{company("C1", "SK", "READ"), true},  // upper bound
		check{company("C1", "PT", "READ"), true},  // inside
		check{company("C1", "SL", "READ"), false}, // after the range
		check{company("C1", "FR", "READ"), false},
		check{company("C1", "DE", "DELETE"), false},
		check{map[string]string{"COMPANY_ID": "C1", "COUNTRY": "DE"}, false}, // a field without value
	)
	expect(t, e, "/TROTTO/MARKETPLACE", check{map[string]string{"COUNTRY": "DE", "ACTIVITY": "EXECUTE"}, false})
	if len(e.Objects) != 1 || e.Objects["/TRFIN/COMPANY"].Grants[0].Origin.Role != "/TRFIN/COMPANY_CLERK" || e.All {
		t.Errorf("grants: %+v", e.Objects)
	}
}

// TestPrefixAndComposite evaluates a prefix, which never matches "", and a
// composite role, whose members combine with or.
func TestPrefixAndComposite(t *testing.T) {
	s := schema(t)
	s.Roles["/LOCAL/OPS"] = &model.Role{Name: "/LOCAL/OPS", Owner: model.Local, Composite: true, Roles: []string{"/TRFIN/COMPANY_CLERK", "/TROTTO/SYNC_OPERATOR"}}
	e := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, []authorization.Assignment{open("a1", "/LOCAL/OPS")})
	sync := func(country, activity string) map[string]string { return map[string]string{"COUNTRY": country, "ACTIVITY": activity} }
	expect(t, e, "/TROTTO/MARKETPLACE", check{sync("DK", "IMPORT"), true}, check{sync("D", "EXECUTE"), true}, check{sync("", "EXECUTE"), false},
		check{sync("CH", "EXECUTE"), true}, check{sync("FR", "EXECUTE"), false}, check{sync("DE", "SEND"), false})
	expect(t, e, "/TRFIN/COMPANY", check{company("C1", "DE", "UPDATE"), true})
	if o := e.Objects["/TROTTO/MARKETPLACE"].Grants[0].Origin; o.Role != "/TROTTO/SYNC_OPERATOR" || o.Composite != model.Some("/LOCAL/OPS") || o.Assignment != "a1" {
		t.Errorf("origin: %+v", o)
	}
}

// TestIntegers evaluates ranges and prefixes of integer fields, and or
// over two roles on the same object.
func TestIntegers(t *testing.T) {
	s := schema(t)
	s.AuthorizationObjects["/LOCAL/LIMITS"] = &model.AuthorizationObject{Name: "/LOCAL/LIMITS", Owner: model.Local,
		Fields: []model.AuthorizationField{{Name: "AMOUNT", Domain: model.Some("/TRBASE/COUNTER")}}}
	auth := func(js string) []model.Authorization {
		return []model.Authorization{{Object: "/LOCAL/LIMITS", Fields: map[string][]model.ValueEntry{"AMOUNT": entries(t, js)}}}
	}
	s.Roles["/LOCAL/LOW"] = &model.Role{Name: "/LOCAL/LOW", Owner: model.Local, Authorizations: auth(`[{"from":-5,"to":20}]`)}
	s.Roles["/LOCAL/PREFIX"] = &model.Role{Name: "/LOCAL/PREFIX", Owner: model.Local, Authorizations: auth(`[{"pattern":"12*"},{"value":100}]`)}
	e := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, []authorization.Assignment{open("a1", "/LOCAL/LOW"), open("a2", "/LOCAL/PREFIX")})
	amount := func(v string) map[string]string { return map[string]string{"AMOUNT": v} }
	expect(t, e, "/LOCAL/LIMITS", check{amount("-5"), true}, check{amount("9"), true}, check{amount("20"), true}, check{amount("21"), false},
		check{amount("120"), true}, check{amount("1234"), true}, check{amount("100"), true}, check{amount("13"), true}, check{amount("99"), false})
}

// TestActivityStar includes activities that an object allows later, and an
// authorization that misses a new field grants nothing.
func TestActivityStar(t *testing.T) {
	s := schema(t)
	admin := []authorization.Assignment{open("a1", "/TRFIN/COMPANY_ADMIN")}
	e := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, admin)
	expect(t, e, "/TRFIN/COMPANY", check{company("C1", "XX", "EXPORT"), true}, check{company("C1", "XX", "APPROVE"), false})
	o := s.AuthorizationObjects["/TRFIN/COMPANY"]
	o.Activities = append(o.Activities, "APPROVE")
	e = authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, admin)
	expect(t, e, "/TRFIN/COMPANY", check{company("C1", "XX", "APPROVE"), true})
	o.Fields = append(o.Fields, model.AuthorizationField{Name: "REGION", DataElement: model.Some("/TRBASE/NAME")})
	e = authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, admin)
	expect(t, e, "/TRFIN/COMPANY", check{map[string]string{"COMPANY_ID": "C1", "COUNTRY": "DE", "ACTIVITY": "READ", "REGION": "N"}, false})
	if len(e.NotEffective) != 1 || e.NotEffective[0].Object != "/TRFIN/COMPANY" || e.NotEffective[0].Problems[0].Field != "REGION" {
		t.Errorf("not effective: %+v", e.NotEffective)
	}
	expect(t, e, "/TRFIN/STATUS_SCOPE", check{map[string]string{"STATUS": "ACTIVE", "COUNTRY": "DE"}, true})
}

// TestValidityLockAndAll checks the period of assignments at its bounds,
// locked users and the role with all authorizations.
func TestValidityLockAndAll(t *testing.T) {
	s := schema(t)
	a := authorization.Assignment{ID: "a1", Role: "/TRFIN/COMPANY_CLERK", ValidFrom: model.Some(day("2026-01-01")), ValidTo: model.Some(day("2026-06-30"))}
	values := company("C1", "DE", "READ")
	for d, want := range map[string]bool{"2025-12-31": false, "2026-01-01": true, "2026-06-30": true, "2026-07-01": false} {
		e := authorization.Resolve(s, "anna", "ACME", day(d), false, []authorization.Assignment{a})
		if e.Allows("/TRFIN/COMPANY", values) != want {
			t.Errorf("on %s: want %v", d, want)
		}
	}
	a.ValidFrom = model.None[time.Time]()
	if e := authorization.Resolve(s, "anna", "ACME", day("1999-01-01"), false, []authorization.Assignment{a}); !e.Allows("/TRFIN/COMPANY", values) {
		t.Error("open start")
	}
	if e := authorization.Resolve(s, "anna", "ACME", day("2026-03-01"), true, []authorization.Assignment{a}); e.Allows("/TRFIN/COMPANY", values) || !e.Locked {
		t.Error("locked user")
	}
	all := authorization.Resolve(s, "anna", "ACME", day("2026-03-01"), false, []authorization.Assignment{open("a9", model.AllAuthorizationsRole)})
	if !all.All || !all.Allows("/TRFIN/COMPANY", values) || !all.Allows("/LATER/OBJECT", nil) || all.AllOrigins[0].Assignment != "a9" {
		t.Errorf("all authorizations: %+v", all)
	}
	if b, err := json.Marshal(authorization.Resolve(s, "anna", "ACME", day("2026-03-01"), false, []authorization.Assignment{a})); err != nil ||
		!json.Valid(b) {
		t.Errorf("JSON: %v", err)
	}
}

// TestCheck evaluates checks with any fields: an any field is not
// checked, the others match as in Allows, and nil, locked and all
// authorizations decide alone.
func TestCheck(t *testing.T) {
	s := schema(t)
	e := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, []authorization.Assignment{open("a1", "/TRFIN/COMPANY_CLERK")})
	read := map[string]string{"COUNTRY": "DE", "ACTIVITY": "READ"}
	for _, c := range []struct {
		name  string
		check authorization.Check
		want  bool
	}{
		{"all values", authorization.Check{Object: "/TRFIN/COMPANY", Values: company("C1", "DE", "READ")}, true},
		{"any company", authorization.Check{Object: "/TRFIN/COMPANY", Values: read, Any: map[string]bool{"COMPANY_ID": true}}, true},
		{"any country", authorization.Check{Object: "/TRFIN/COMPANY", Values: map[string]string{"COMPANY_ID": "C1", "ACTIVITY": "READ"},
			Any: map[string]bool{"COUNTRY": true}}, true},
		{"any activity", authorization.Check{Object: "/TRFIN/COMPANY", Values: map[string]string{"COMPANY_ID": "C1", "COUNTRY": "FR"},
			Any: map[string]bool{"ACTIVITY": true}}, false},
		{"empty company", authorization.Check{Object: "/TRFIN/COMPANY", Values: company("", "DE", "READ")}, true},
		{"missing field", authorization.Check{Object: "/TRFIN/COMPANY", Values: read}, false},
		{"other activity", authorization.Check{Object: "/TRFIN/COMPANY", Values: company("C1", "DE", "DELETE")}, false},
		{"other object", authorization.Check{Object: "/TROTTO/MARKETPLACE", Values: read}, false},
	} {
		if got := e.Check(c.check); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	all := authorization.Check{Object: "/TRFIN/COMPANY", Values: company("C1", "DE", "READ")}
	var none *authorization.Effective
	if none.Check(all) {
		t.Error("without user")
	}
	locked := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), true, []authorization.Assignment{open("a1", "/TRFIN/COMPANY_CLERK")})
	if locked.Check(all) {
		t.Error("locked user")
	}
	full := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, []authorization.Assignment{open("a1", model.AllAuthorizationsRole)})
	if !full.Check(authorization.Check{Object: "/LATER/OBJECT"}) {
		t.Error("all authorizations")
	}
	if g := e.Grants("/TRFIN/COMPANY"); len(g) != 1 || g[0].Origin.Role != "/TRFIN/COMPANY_CLERK" || e.Grants("/TROTTO/MARKETPLACE") != nil {
		t.Errorf("grants: %+v", g)
	}
}

// BenchmarkCheck measures a check in memory against a role with values,
// a range and a star.
func BenchmarkCheck(b *testing.B) {
	s := testutil.AddPackages(b, testutil.KernelSchema(b), testutil.Example(b, "TRBASE"), testutil.Example(b, "TRFIN"), testutil.Example(b, "TROTTO"))
	e := authorization.Resolve(s, "anna", "ACME", day("2026-11-08"), false, []authorization.Assignment{open("a1", "/TRFIN/COMPANY_CLERK")})
	c := authorization.Check{Object: "/TRFIN/COMPANY", Values: company("C1", "PT", "READ")}
	for b.Loop() {
		if !e.Check(c) {
			b.Fatal("not authorized")
		}
	}
}
