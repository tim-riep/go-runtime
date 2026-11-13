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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

const itemsRoot = "/rt/odata/trodata/items/"

// itemsKernel installs TRODATA with items in ACME and BETA. eva edits
// items in both tenants, nina reads the items of the region NORTH in ACME.
func itemsKernel(t *testing.T, k *kernel.Kernel) client {
	t.Helper()
	ctx := context.Background()
	initialized(t, k)
	if _, err := k.Install(ctx, []*model.Package{odataPackage(t)}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ACME", "BETA"} {
		if _, err := k.Tenants().Create(ctx, id, model.None[string]()); err != nil {
			t.Fatal(err)
		}
	}
	users := map[string]string{"eva": "/TRODATA/EDITOR", "nina": "/TRODATA/NORTH_READER"}
	for user, role := range users {
		if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.Users().AddTenant(ctx, "eva", "BETA"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "eva", Role: "/TRODATA/EDITOR", Tenant: "BETA"}, kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := k.ActivateService(ctx, itemsService, "test"); err != nil {
		t.Fatal(err)
	}
	run := func(tenant, method string, args ...string) {
		t.Helper()
		var raw []json.RawMessage
		for _, a := range args {
			raw = append(raw, json.RawMessage(a))
		}
		if _, err := k.Run(ctx, kernel.ExecOptions{Tenant: tenant, User: model.Some("eva")}, "/TRODATA/SEED", method, raw); err != nil {
			t.Fatal(err)
		}
	}
	run("ACME", "REGIONS", `[{"REGION_ID":"NORTH","NAME":"North"},{"REGION_ID":"SOUTH","NAME":"South"}]`)
	run("ACME", "ITEMS", `[
		{"LINK":"i1","ITEM_ID":"I1","STATUS":"NEW","NAME":"Alpha","PRIORITY":"LOW","REGION":"NORTH","AMOUNT":"10.50","QUANTITY":9007199254740993,"NOTE":"it's",
		 "ACTIVE":true,"DUE_ON":"2026-01-10","DONE_AT":"2026-01-10T08:00:00.000000Z","REFERENCE":"1b4e28ba-2fa1-11d2-883f-0016d3cca427","DATA":"AQID"},
		{"LINK":"i2","ITEM_ID":"I2","STATUS":"NEW","NAME":"beta","PRIORITY":"HIGH","REGION":"SOUTH","AMOUNT":"20","NOTE":"","ACTIVE":false},
		{"LINK":"i3","ITEM_ID":"I3","STATUS":"NEW","NAME":"Gamma","REGION":"NORTH"}]`,
		`[{"PART_ID":"00000000-0000-0000-0000-000000000001","PARENT_LINK":"i1","ITEM_ID":"I1","NAME":"Wheel","COUNT":4},{"PART_ID":"00000000-0000-0000-0000-000000000001","PARENT_LINK":"i1","ITEM_ID":"I1","NAME":"Axle","COUNT":2},{"PART_ID":"00000000-0000-0000-0000-000000000003","PARENT_LINK":"i3","ITEM_ID":"I3","NAME":"Lid","COUNT":1}]`)
	run("BETA", "ITEMS", `[{"LINK":"i9","ITEM_ID":"I9","STATUS":"NEW","NAME":"Other","REGION":"NORTH"}]`, `[]`)
	srv := httptest.NewServer(k.HTTPHandler())
	t.Cleanup(srv.Close)
	return client{t, srv.URL}
}

// collection is the body of a response with entities.
type collection struct {
	Context  string           `json:"@odata.context"`
	Count    *int64           `json:"@odata.count"`
	NextLink string           `json:"@odata.nextLink"`
	Value    []map[string]any `json:"value"`
}

// get reads a collection and returns it with the values of a property.
func get(t *testing.T, c client, user func(*http.Request), path string) (collection, []string) {
	t.Helper()
	a := c.do("GET", path, user, "")
	var col collection
	if a.status != 200 {
		t.Fatalf("%s: %d %s", path, a.status, a.body)
	}
	d := json.NewDecoder(strings.NewReader(a.body))
	d.UseNumber()
	if err := d.Decode(&col); err != nil {
		t.Fatalf("%s: %v %s", path, err, a.body)
	}
	var ids []string
	for _, v := range col.Value {
		if id, ok := v["ITEM_ID"].(string); ok {
			ids = append(ids, id)
		} else if name, ok := v["NAME"].(string); ok {
			ids = append(ids, name)
		}
	}
	return col, ids
}

func filtered(filter string) string { return itemsRoot + "Items?$filter=" + url.QueryEscape(filter) }

func TestODataRead(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := itemsKernel(t, k)
		eva, nina := basicAuth("eva", "secret"), basicAuth("nina", "secret")

		a := c.do("GET", itemsRoot+"Items", eva, "")
		if a.status != 200 || !strings.Contains(a.body, `"QUANTITY":9007199254740993`) || !strings.Contains(a.body, `"AMOUNT":10.50`) ||
			!strings.Contains(a.body, `"DUE_ON":"2026-01-10","DONE_AT":"2026-01-10T08:00:00Z"`) || !strings.Contains(a.body, `"DATA":"AQID"`) ||
			!strings.Contains(a.body, `"@odata.context":"`+c.url+itemsRoot+`$metadata#Items"`) || !strings.Contains(a.body, `"@odata.etag":"W/`) ||
			!strings.Contains(a.body, `"REFERENCE":"1b4e28ba-2fa1-11d2-883f-0016d3cca427"`) || strings.Contains(a.body, "TENANT") {
			t.Fatalf("items: %d %s", a.status, a.body)
		}
		ieee := c.do("GET", itemsRoot+"Items?$format="+url.QueryEscape("application/json;IEEE754Compatible=true"), eva, "")
		if !strings.Contains(ieee.body, `"QUANTITY":"9007199254740993"`) || !strings.Contains(ieee.body, `"AMOUNT":"10.50"`) ||
			!strings.Contains(ieee.header.Get("Content-Type"), "IEEE754Compatible=true") {
			t.Errorf("IEEE754Compatible: %s", ieee.body)
		}

		for _, f := range []struct{ filter, want string }{
			{"NAME eq 'Alpha'", "I1"},
			{"NOTE eq null", "I3"},
			{"NOTE eq ''", "I2"},
			{"NOTE ne 'it''s'", "I2,I3"},
			{"NOTE ne null", "I1,I2"},
			{"not (PRIORITY eq 1)", "I2,I3"},
			{"PRIORITY in (1, 2)", "I1,I2"},
			{"PRIORITY in (2, null)", "I2,I3"},
			{"contains(NAME, 'amm')", "I3"},
			{"startswith(tolower(NAME), 'b')", "I2"},
			{"endswith(NAME, 'a')", "I1,I2,I3"},
			{"toupper(NAME) eq 'BETA'", "I2"},
			{"AMOUNT gt 15", "I2"},
			{"AMOUNT le 10.5", "I1"},
			{"QUANTITY eq 9007199254740993", "I1"},
			{"DUE_ON ge 2026-01-01", "I1"},
			{"DONE_AT lt 2027-01-01T00:00:00Z", "I1"},
			{"REFERENCE eq 1b4e28ba-2fa1-11d2-883f-0016d3cca427", "I1"},
			{"ACTIVE eq true", "I1"},
			{"ACTIVE ne true", "I2,I3"},
			{"(PRIORITY eq 1 or PRIORITY eq 2) and REGION eq 'NORTH'", "I1"},
			{"contains(NAME, '')", "I1,I2,I3"},
		} {
			if _, ids := get(t, c, eva, filtered(f.filter)); strings.Join(ids, ",") != f.want {
				t.Errorf("$filter=%s: %v, want %s", f.filter, ids, f.want)
			}
		}
		for _, bad := range []struct {
			query  string
			status int
		}{
			{"$filter=" + url.QueryEscape("PRIORITY add 1 eq 2"), 501},
			{"$filter=" + url.QueryEscape("length(NAME) gt 1"), 501},
			{"$filter=" + url.QueryEscape("Parts/any(p: p/COUNT gt 1)"), 501},
			{"$filter=" + url.QueryEscape("NAME eq"), 400},
			{"$filter=" + url.QueryEscape("NOPE eq 1"), 400},
			{"$filter=" + url.QueryEscape("PRIORITY eq 'x'"), 400},
			{"$filter=" + url.QueryEscape("NAME gt null"), 400},
			{"$search=x", 501},
			{"$apply=" + url.QueryEscape("groupby((REGION))"), 501},
			{"$expand=" + url.QueryEscape("Parts($filter=COUNT gt 1)"), 501},
			{"$expand=" + url.QueryEscape("Parts/Items"), 501},
			{"$top=-1", 400},
			{"$wat=1", 400},
		} {
			if a := c.do("GET", itemsRoot+"Items?"+bad.query, eva, ""); a.status != bad.status {
				t.Errorf("%s: %d %s", bad.query, a.status, a.body)
			} else {
				odataError(t, a)
			}
		}

		if _, ids := get(t, c, eva, itemsRoot+"Items?$orderby="+url.QueryEscape("REGION desc,NAME")); strings.Join(ids, ",") != "I2,I1,I3" {
			t.Errorf("$orderby: %v", ids)
		}
		if _, ids := get(t, c, eva, itemsRoot+"Items?$orderby=PRIORITY"); strings.Join(ids, ",") != "I1,I2,I3" {
			t.Errorf("$orderby with null: %v", ids)
		}
		if _, ids := get(t, c, eva, itemsRoot+"Items?$orderby="+url.QueryEscape("PRIORITY desc")); strings.Join(ids, ",") != "I3,I2,I1" {
			t.Errorf("$orderby desc with null: %v", ids)
		}

		// Pages with nextLink.
		page := func(r *http.Request) { eva(r); r.Header.Set("Prefer", "odata.maxpagesize=2") }
		first, ids := get(t, c, page, itemsRoot+"Items?$count=true")
		if strings.Join(ids, ",") != "I1,I2" || first.NextLink == "" || *first.Count != 3 {
			t.Fatalf("first page: %+v", first)
		}
		next, _ := url.Parse(first.NextLink)
		second, ids := get(t, c, page, next.RequestURI())
		if strings.Join(ids, ",") != "I3" || second.NextLink != "" {
			t.Errorf("second page: %+v", second)
		}
		if col, ids := get(t, c, eva, itemsRoot+"Items?$top=2"); strings.Join(ids, ",") != "I1,I2" || col.NextLink != "" {
			t.Errorf("$top: %+v", col)
		}
		if _, ids := get(t, c, eva, itemsRoot+"Items?$skip=1&$top=1"); strings.Join(ids, ",") != "I2" {
			t.Errorf("$skip: %v", ids)
		}

		// The authorization filters rows in SQL, so counts and pages match.
		col, ids := get(t, c, nina, itemsRoot+"Items?$count=true")
		if strings.Join(ids, ",") != "I1,I3" || *col.Count != 2 {
			t.Errorf("north reader: %v %v", ids, *col.Count)
		}
		if a := c.do("GET", itemsRoot+"Items/$count", nina, ""); a.body != "2" {
			t.Errorf("$count of nina: %s", a.body)
		}
		if a := c.do("GET", itemsRoot+"Items('I2')", nina, ""); a.status != 404 {
			t.Errorf("entity without read authorization: %d", a.status)
		}
		// Parts take the authorization of their item.
		if _, names := get(t, c, nina, itemsRoot+"Parts?$orderby=NAME&$select=NAME"); strings.Join(names, ",") != "Axle,Lid,Wheel" {
			t.Errorf("parts of nina: %v", names)
		}

		// $expand with $select.
		col, _ = get(t, c, eva, itemsRoot+"Items?$filter="+url.QueryEscape("ITEM_ID eq 'I1'")+"&$select=NAME&$expand="+url.QueryEscape("Parts($select=NAME),Regions"))
		if len(col.Value) != 1 || len(col.Value[0]["Parts"].([]any)) != 2 || col.Value[0]["Regions"].(map[string]any)["NAME"] != "North" ||
			col.Value[0]["PRIORITY"] != nil || !strings.Contains(col.Context, "#Items(NAME,Parts(NAME),Regions())") {
			t.Errorf("$expand: %+v", col)
		}
		if p := col.Value[0]["Parts"].([]any)[0].(map[string]any); len(p) != 1 || p["NAME"] == nil {
			t.Errorf("$select inside $expand: %v", p)
		}

		// One entity with ETag and 304; navigations.
		a = c.do("GET", itemsRoot+"Items('I1')", eva, "")
		etag := a.header.Get("ETag")
		if a.status != 200 || etag == "" || !strings.Contains(a.body, `$metadata#Items/$entity"`) {
			t.Fatalf("entity: %d %v %s", a.status, a.header, a.body)
		}
		if a := c.do("GET", itemsRoot+"Items('I1')", func(r *http.Request) { eva(r); r.Header.Set("If-None-Match", etag) }, ""); a.status != 304 {
			t.Errorf("If-None-Match: %d", a.status)
		}
		if a := c.do("GET", itemsRoot+"Items('NOPE')", eva, ""); a.status != 404 {
			t.Errorf("unknown entity: %d", a.status)
		}
		if _, names := get(t, c, eva, itemsRoot+"Items('I1')/Parts?$orderby=NAME&$select=NAME"); strings.Join(names, ",") != "Axle,Wheel" {
			t.Errorf("navigation: %v", names)
		}
		if a := c.do("GET", itemsRoot+"Items('I1')/Parts/$count", eva, ""); a.body != "2" {
			t.Errorf("navigation $count: %s", a.body)
		}
		if a := c.do("GET", itemsRoot+"Items('I1')/Regions", eva, ""); a.status != 200 || !strings.Contains(a.body, `"NAME":"North"`) {
			t.Errorf("navigation to one: %d %s", a.status, a.body)
		}

		// Tenants are apart.
		if _, ids := get(t, c, func(r *http.Request) { eva(r); r.Header.Set("X-Tenant", "BETA") }, itemsRoot+"Items"); strings.Join(ids, ",") != "I9" {
			t.Errorf("tenant BETA: %v", ids)
		}
		// A table of configuration needs /TRBASE/CONFIGURATION.
		if _, ids := get(t, c, eva, itemsRoot+"Regions"); strings.Join(ids, ",") != "North,South" {
			t.Errorf("regions of eva: %v", ids)
		}
		if _, ids := get(t, c, nina, itemsRoot+"Regions"); len(ids) != 0 {
			t.Errorf("regions of nina: %v", ids)
		}
	})
}

// TestODataCount counts entities in code with the authorizations of the
// user.
func TestODataCount(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		itemsKernel(t, k)
		count := func(user, set, filter string) (string, error) {
			t.Helper()
			opts := kernel.ExecOptions{Tenant: "ACME"}
			if user != "" {
				opts.User = model.Some(user)
			}
			out, err := k.Run(ctx, opts, "/TRODATA/COUNTER", "COUNT", []json.RawMessage{json.RawMessage(`"` + set + `"`), json.RawMessage(filter)})
			return string(out), err
		}
		for _, c := range []struct{ user, set, filter, want string }{
			{"eva", "Items", `null`, "3"},
			{"nina", "Items", `null`, "2"},
			{"nina", "Items", `"NAME eq 'Gamma'"`, "1"},
			{"eva", "Items", `"STATUS eq 'NEW' and ACTIVE eq false"`, "1"},
		} {
			if got, err := count(c.user, c.set, c.filter); err != nil || got != c.want {
				t.Errorf("%s %s %s: %s %v", c.user, c.set, c.filter, got, err)
			}
		}
		for _, c := range []struct{ user, set, filter, want string }{
			{"eva", "Nope", `null`, "has no entity set Nope"},
			{"eva", "Items", `"NAME eq"`, "/TRBASE/ODATA_ERROR"},
			{"", "Items", `null`, "/RT/NOT_AUTHORIZED"},
		} {
			if _, err := count(c.user, c.set, c.filter); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s %s %s: want %s, got %v", c.user, c.set, c.filter, c.want, err)
			}
		}
		if err := k.DeactivateService(ctx, itemsService, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := count("eva", "Items", `null`); err == nil || !strings.Contains(err.Error(), "no active service") {
			t.Errorf("inactive: %v", err)
		}
	})
}
