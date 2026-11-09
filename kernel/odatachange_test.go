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
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
)

// odataErrorDoc reads an error of OData with its details.
type odataErrorDoc struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Target  string `json:"target"`
		Details []struct {
			Code, Message, Target string
		} `json:"details"`
	} `json:"error"`
}

func errorDoc(t *testing.T, a answer) odataErrorDoc {
	t.Helper()
	var d odataErrorDoc
	if err := json.Unmarshal([]byte(a.body), &d); err != nil || d.Error.Code == "" {
		t.Fatalf("no error of OData: %d %s", a.status, a.body)
	}
	return d
}

// with adds headers to a user.
func with(user func(*http.Request), headers ...string) func(*http.Request) {
	return func(r *http.Request) {
		user(r)
		r.Header.Set("Content-Type", "application/json")
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
	}
}

func TestODataChange(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := itemsKernel(t, k)
		eva, nina := basicAuth("eva", "secret"), basicAuth("nina", "secret")
		items := itemsRoot + "Items"

		// POST with a deep insert of parts with keys from UUIDs; the
		// determination warns about the missing priority.
		a := c.do("POST", items, with(eva), `{"ITEM_ID":"I5","NAME":"Delta","REGION":"NORTH","AMOUNT":"1.5","Parts":[{"NAME":"Bolt","COUNT":3},{"NAME":"Nut"}]}`)
		if a.status != 201 || !strings.HasSuffix(a.header.Get("Location"), itemsRoot+"Items('I5')") || a.header.Get("ETag") == "" ||
			!strings.Contains(a.body, `"STATUS":"NEW"`) || !strings.Contains(a.body, `"@Core.Messages":[{"code":"WARNING","message":"the item I5 has no priority","severity":"warning"`) ||
			!strings.Contains(a.body, `"CHANGED_BY":"eva"`) {
			t.Fatalf("POST: %d %v %s", a.status, a.header, a.body)
		}
		if _, names := get(t, c, eva, itemsRoot+"Items('I5')/Parts?$orderby=NAME&$select=NAME"); strings.Join(names, ",") != "Bolt,Nut" {
			t.Errorf("deep insert: %v", names)
		}
		if a := c.do("POST", itemsRoot+"Items('I5')/Parts", with(eva), `{"NAME":"Hinge"}`); a.status != 201 || !strings.Contains(a.body, `"ITEM_ID":"I5"`) {
			t.Errorf("POST along a navigation: %d %s", a.status, a.body)
		}
		if a := c.do("POST", items, with(eva, "Prefer", "return=minimal"), `{"ITEM_ID":"I6","NAME":"Epsilon","REGION":"SOUTH","PRIORITY":2}`); a.status != 204 ||
			a.header.Get("Location") == "" || a.header.Get("Preference-Applied") != "return=minimal" {
			t.Errorf("return=minimal: %d %v", a.status, a.header)
		}

		// Rejected bodies name the property.
		for _, bad := range []struct {
			body, code, target string
			status             int
		}{
			{`{"ITEM_ID":"I7","NAME":"x","REGION":"NORTH","STATUS":"CLOSED"}`, "READ_ONLY_PROPERTY", "STATUS", 400},
			{`{"ITEM_ID":"I7","NAME":"x","REGION":"NORTH","COLOUR":"red"}`, "UNKNOWN_PROPERTY", "COLOUR", 400},
			{`{"ITEM_ID":"I7","NAME":"x","REGION":"NORTH","Parts":[{"PART_ID":"00000000-0000-0000-0000-000000000009"}]}`, "READ_ONLY_PROPERTY", "Parts(1)/PART_ID", 400},
			{`{"ITEM_ID":"I7","NAME":"x","REGION":"NORTH","PRIORITY":1.5}`, "INVALID_VALUE", "PRIORITY", 400},
			{`{"ITEM_ID":"I7","NAME":"x","REGION":"NORTH","AMOUNT":1.123}`, "INVALID_VALUE", "AMOUNT", 400},
			{`{"ITEM_ID":"I7","NAME":"x","REGION":null}`, "INVALID_VALUE", "REGION", 400},
			{`{"ITEM_ID":"I1","NAME":"x","REGION":"NORTH"}`, "ALREADY_EXISTS", "", 409},
			{`{"ITEM_ID":"I7","NAME":"BAD","REGION":"NORTH"}`, "VALIDATION", "NAME", 400},
			{`{"ITEM_ID":"I7","REGION":"NORTH"}`, "VALIDATION", "NAME", 400},
		} {
			a := c.do("POST", items, with(eva), bad.body)
			if d := errorDoc(t, a); a.status != bad.status || d.Error.Code != bad.code || d.Error.Target != bad.target {
				t.Errorf("%s: %d %s", bad.body, a.status, a.body)
			}
		}
		if a := c.do("POST", items, with(eva), `{"ITEM_ID":"I7","NAME":"BAD","REGION":"NORTH"}`); !strings.Contains(a.body, "the name BAD is not allowed") {
			t.Errorf("message of the validation: %s", a.body)
		}
		if a := c.do("GET", itemsRoot+"Items('I7')", eva, ""); a.status != 404 {
			t.Errorf("a failed POST wrote: %d", a.status)
		}

		// PATCH needs the ETag.
		etag := c.do("GET", itemsRoot+"Items('I1')", eva, "").header.Get("ETag")
		if a := c.do("PATCH", itemsRoot+"Items('I1')", with(eva), `{"NOTE":null}`); a.status != 428 || errorDoc(t, a).Error.Code != "ETAG_REQUIRED" {
			t.Errorf("without If-Match: %d %s", a.status, a.body)
		}
		if a := c.do("PATCH", itemsRoot+"Items('I1')", with(eva, "If-Match", `W/"MTI"`), `{"NOTE":null}`); a.status != 412 || errorDoc(t, a).Error.Code != "ETAG_MISMATCH" {
			t.Errorf("wrong If-Match: %d %s", a.status, a.body)
		}
		a = c.do("PATCH", itemsRoot+"Items('I1')", with(eva, "If-Match", etag), `{"NOTE":null,"AMOUNT":"12345678901.25","QUANTITY":"9223372036854775807"}`)
		if a.status != 204 || a.header.Get("ETag") == etag || a.header.Get("ETag") == "" {
			t.Fatalf("PATCH: %d %v %s", a.status, a.header, a.body)
		}
		etag = a.header.Get("ETag")
		got := c.do("GET", itemsRoot+"Items('I1')", eva, "").body
		if !strings.Contains(got, `"NOTE":null`) || !strings.Contains(got, `"NAME":"Alpha"`) || !strings.Contains(got, `"AMOUNT":12345678901.25`) ||
			!strings.Contains(got, `"QUANTITY":9223372036854775807`) {
			t.Errorf("after PATCH with null: %s", got)
		}
		a = c.do("PATCH", itemsRoot+"Items('I1')", with(eva, "If-Match", etag, "Prefer", "return=representation"), `{"NOTE":"","AMOUNT":99.99}`)
		if a.status != 200 || !strings.Contains(a.body, `"NOTE":""`) || !strings.Contains(a.body, `"AMOUNT":99.99`) || !strings.Contains(a.body, `"NAME":"Alpha"`) {
			t.Errorf("PATCH with the empty string: %d %s", a.status, a.body)
		}
		etag = a.header.Get("ETag")
		for _, bad := range []string{`{"STATUS":"CLOSED"}`, `{"REGION":"SOUTH"}`, `{"ITEM_ID":"I8"}`, `{"Parts":[]}`} {
			if a := c.do("PATCH", itemsRoot+"Items('I1')", with(eva, "If-Match", etag), bad); a.status != 400 || errorDoc(t, a).Error.Code != "READ_ONLY_PROPERTY" {
				t.Errorf("PATCH %s: %d %s", bad, a.status, a.body)
			}
		}

		// PUT replaces every field the client may change.
		etag2 := c.do("GET", itemsRoot+"Items('I2')", eva, "").header.Get("ETag")
		if a := c.do("PUT", itemsRoot+"Items('I2')", with(eva, "If-Match", etag2), `{"NAME":"beta2"}`); a.status != 204 {
			t.Fatalf("PUT: %d %s", a.status, a.body)
		}
		got = c.do("GET", itemsRoot+"Items('I2')", eva, "").body
		if !strings.Contains(got, `"NAME":"beta2"`) || !strings.Contains(got, `"PRIORITY":null`) || !strings.Contains(got, `"NOTE":null`) || !strings.Contains(got, `"REGION":"SOUTH"`) {
			t.Errorf("after PUT: %s", got)
		}

		// Authorization: nina reads I3 but may not change it, and does not
		// see I2.
		etag3 := c.do("GET", itemsRoot+"Items('I3')", nina, "").header.Get("ETag")
		if a := c.do("PATCH", itemsRoot+"Items('I3')", with(nina, "If-Match", etag3), `{"NOTE":"x"}`); a.status != 403 || errorDoc(t, a).Error.Code != "NOT_AUTHORIZED" {
			t.Errorf("PATCH without authorization: %d %s", a.status, a.body)
		}
		if a := c.do("DELETE", itemsRoot+"Items('I2')", with(nina, "If-Match", "*"), ""); a.status != 404 {
			t.Errorf("DELETE without read authorization: %d %s", a.status, a.body)
		}
		if a := c.do("POST", items, with(nina), `{"ITEM_ID":"I8","NAME":"n","REGION":"NORTH"}`); a.status != 403 {
			t.Errorf("POST without authorization: %d %s", a.status, a.body)
		}

		// DELETE removes the dependent instances.
		if a := c.do("DELETE", itemsRoot+"Items('I1')", with(eva), ""); a.status != 428 {
			t.Errorf("DELETE without If-Match: %d", a.status)
		}
		if a := c.do("DELETE", itemsRoot+"Items('I1')", with(eva, "If-Match", etag), ""); a.status != 204 {
			t.Fatalf("DELETE: %d %s", a.status, a.body)
		}
		if a := c.do("GET", itemsRoot+"Items('I1')", eva, ""); a.status != 404 {
			t.Errorf("deleted: %d", a.status)
		}
		if _, names := get(t, c, eva, itemsRoot+"Parts?$select=NAME&$filter="+url.QueryEscape("ITEM_ID eq 'I1'")); len(names) != 0 {
			t.Errorf("parts of the deleted item: %v", names)
		}
		if a := c.do("DELETE", itemsRoot+"Regions('NORTH')", with(eva, "If-Match", "*"), ""); a.status != 405 {
			t.Errorf("DELETE on a table: %d", a.status)
		}
	})
}
