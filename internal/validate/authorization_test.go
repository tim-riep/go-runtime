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
	"fmt"
	"testing"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func authDE(name, de string) model.AuthorizationField {
	return model.AuthorizationField{Name: name, DataElement: model.Some(de)}
}

func authDomain(name, d string) model.AuthorizationField {
	return model.AuthorizationField{Name: name, Domain: model.Some(d)}
}

var activityField = model.AuthorizationField{Name: model.ActivityField}

// authObject returns a valid object of TRS with the field ACTIVITY.
func authObject() model.AuthorizationObject {
	return model.AuthorizationObject{Name: "/TRS/COMPANY", Texts: en,
		Fields:     []model.AuthorizationField{authDE("COMPANY_ID", "/TRFIN/COMPANY"), authDE("COUNTRY", "/TRBASE/COUNTRY"), activityField},
		Activities: []string{"READ", "UPDATE"}}
}

func withObject(t *testing.T, change func(o *model.AuthorizationObject)) *model.Schema {
	o := authObject()
	change(&o)
	return withTRS(t, trs(model.Package{AuthorizationObjects: []model.AuthorizationObject{o}}))
}

func TestAuthorizationObjectValid(t *testing.T) {
	for _, s := range []*model.Schema{
		withObject(t, func(o *model.AuthorizationObject) {}),
		// Without ACTIVITY and without activities; with a domain of fixed values, INT4, BOOL and UUID.
		withObject(t, func(o *model.AuthorizationObject) {
			o.Fields = []model.AuthorizationField{authDomain("STATUS", "/TRFIN/COMPANY_STATUS"), authDomain("COUNT", "/TRBASE/COUNTER"),
				authDomain("FLAG", "/TRBASE/FLAG"), authDomain("ID", "/TRBASE/GUID")}
			o.Activities = nil
		}),
	} {
		if err := Schema(s, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAuthorizationObjectRules(t *testing.T) {
	many := []model.AuthorizationField{}
	for i := range 11 {
		many = append(many, authDE(fmt.Sprintf("F%d", i), "/TRBASE/NAME"))
	}
	for _, c := range []struct {
		name     string
		change   func(o *model.AuthorizationObject)
		code     rterr.Code
		contains string
	}{
		{"no field", func(o *model.AuthorizationObject) { o.Fields, o.Activities = nil, nil }, rterr.CodeAuthorization, "at least one field"},
		{"eleven fields", func(o *model.AuthorizationObject) { o.Fields, o.Activities = many, nil }, rterr.CodeAuthorization, "at most 10 fields, not 11"},
		{"field twice", func(o *model.AuthorizationObject) { o.Fields[1].Name = "COMPANY_ID" }, rterr.CodeDuplicate, "the field COMPANY_ID is declared twice"},
		{"bad field name", func(o *model.AuthorizationObject) { o.Fields[1].Name = "country" }, rterr.CodeName, "field names consist of"},
		{"data element and domain", func(o *model.AuthorizationObject) { o.Fields[1].Domain = model.Some("/TRBASE/COUNTRY_CODE") }, rterr.CodeDataElement, "either a data element or a domain"},
		{"field without type", func(o *model.AuthorizationObject) { o.Fields[1].DataElement = model.None[string]() }, rterr.CodeDataElement, "only the field ACTIVITY has none"},
		{"ACTIVITY with a type", func(o *model.AuthorizationObject) { o.Fields[2].Domain = model.Some("/TRBASE/NAME") }, rterr.CodeReservedObject, "the kernel gives the field ACTIVITY its type"},
		{"DEC", func(o *model.AuthorizationObject) { o.Fields[1] = authDomain("AMOUNT", "/TRBASE/AMOUNT") }, rterr.CodeAuthorization, "type DEC(15,2) is not allowed"},
		{"STRING", func(o *model.AuthorizationObject) { o.Fields[1] = authDomain("NOTE", "/TRBASE/TEXT") }, rterr.CodeAuthorization, "type STRING is not allowed"},
		{"DATE", func(o *model.AuthorizationObject) { o.Fields[1] = authDomain("DAY", "/TRBASE/DATE") }, rterr.CodeAuthorization, "type DATE is not allowed"},
		{"unknown data element", func(o *model.AuthorizationObject) { o.Fields[1] = authDE("X", "/TRS/NOPE") }, rterr.CodeStructure, "dataElement /TRS/NOPE does not exist"},
		{"activities without ACTIVITY", func(o *model.AuthorizationObject) { o.Fields = o.Fields[:2] }, rterr.CodeAuthorization, "activities need the field ACTIVITY"},
		{"ACTIVITY without activities", func(o *model.AuthorizationObject) { o.Activities = nil }, rterr.CodeAuthorization, "lists the activities that can be granted"},
		{"unknown activity", func(o *model.AuthorizationObject) { o.Activities = []string{"READ", "DANCE"} }, rterr.CodeAuthorization, "DANCE is no activity"},
		{"activity twice", func(o *model.AuthorizationObject) { o.Activities = []string{"READ", "READ"} }, rterr.CodeDuplicate, "the activity READ is listed twice"},
		{"namespace", func(o *model.AuthorizationObject) { o.Name = "/TRFIN/MINE" }, rterr.CodeName, "must start with the namespace /TRS/"},
		{"no English", func(o *model.AuthorizationObject) { o.Texts = map[string]model.Label{"de": {Label: "Firma"}} }, rterr.CodeText, "texts in English"},
	} {
		t.Run(c.name, func(t *testing.T) { expect(t, withObject(t, c.change), c.code, c.contains) })
	}
}

// TestAuthorizationObjectNotVisible rejects a data element of a package
// that the owner does not require.
func TestAuthorizationObjectNotVisible(t *testing.T) {
	o := authObject()
	o.Name = "/TRT/COMPANY"
	p := &model.Package{Manifest: model.Manifest{ID: "TRT", Version: "1.0.0", Requires: []model.Dependency{{ID: "TRBASE"}}},
		AuthorizationObjects: []model.AuthorizationObject{o}}
	expect(t, withTRS(t, p), rterr.CodeNotVisible, "/TRFIN/COMPANY belongs to package TRFIN, which TRT does not require")
}

// TestAuthorizationTypeChangeElsewhere rejects a domain that changes to a
// type fields of authorization objects cannot have, naming the object.
func TestAuthorizationTypeChangeElsewhere(t *testing.T) {
	s := withObject(t, func(o *model.AuthorizationObject) {})
	d := s.Domains["/TRBASE/COUNTRY_CODE"]
	d.Type, d.Length, d.Decimals = model.Dec, 10, 2
	expect(t, s, rterr.CodeAuthorization, "authorization object /TRS/COMPANY, field COUNTRY: type DEC(10,2) is not allowed")
}
