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
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

func TestODataActions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		c := itemsKernel(t, k)
		eva, nina := basicAuth("eva", "secret"), basicAuth("nina", "secret")

		// A bound action with a parameter returns its instance.
		etag := c.do("GET", itemsRoot+"Items('I1')", eva, "").header.Get("ETag")
		if a := c.do("POST", itemsRoot+"Items('I1')/TRODATA.CLOSE", with(eva, "If-Match", `W/"MTI"`), `{"TEXT":"done"}`); a.status != 412 {
			t.Errorf("action with a wrong If-Match: %d %s", a.status, a.body)
		}
		a := c.do("POST", itemsRoot+"Items('I1')/TRODATA.CLOSE", with(eva, "If-Match", etag), `{"TEXT":"done"}`)
		if a.status != 200 || !strings.Contains(a.body, `"STATUS":"CLOSED"`) || !strings.Contains(a.body, `"NOTE":"done"`) ||
			!strings.Contains(a.body, `$metadata#Items/$entity"`) || a.header.Get("ETag") == etag {
			t.Fatalf("CLOSE: %d %s", a.status, a.body)
		}
		// Without If-Match the action runs too.
		if a := c.do("POST", itemsRoot+"Items('I2')/TRODATA.CLOSE", with(eva), `{}`); a.status != 200 || !strings.Contains(a.body, `"NOTE":null`) {
			t.Errorf("CLOSE without If-Match and text: %d %s", a.status, a.body)
		}
		if a := c.do("POST", itemsRoot+"Items('I1')/TRODATA.CLOSE", with(eva), `{"WHY":"x"}`); a.status != 400 || errorDoc(t, a).Error.Target != "WHY" {
			t.Errorf("unknown parameter: %d %s", a.status, a.body)
		}
		if a := c.do("POST", itemsRoot+"Items('I3')/TRODATA.CLOSE", with(nina), `{}`); a.status != 403 {
			t.Errorf("action without authorization: %d %s", a.status, a.body)
		}
		if a := c.do("POST", itemsRoot+"Items('I9')/TRODATA.CLOSE", with(eva), `{}`); a.status != 404 {
			t.Errorf("action on an entity of another tenant: %d %s", a.status, a.body)
		}
		if a := c.do("GET", itemsRoot+"Items('I1')/TRODATA.CLOSE", eva, ""); a.status != 405 {
			t.Errorf("GET on an action: %d", a.status)
		}

		// A static action is an action import with a complex result.
		a = c.do("POST", itemsRoot+"STATS", with(eva), "")
		if a.status != 200 || !strings.Contains(a.body, `"ITEMS":3,"TOTAL":30.50`) || !strings.Contains(a.body, `$metadata#TRODATA.TRODATA_STATS"`) {
			t.Errorf("STATS: %d %s", a.status, a.body)
		}
		if a := c.do("POST", itemsRoot+"STATS", with(nina), ""); a.status != 403 {
			t.Errorf("STATS without authorization: %d %s", a.status, a.body)
		}

		// Errors in the language of the request.
		if a := c.do("GET", itemsRoot+"Items('NOPE')", with(eva, "Accept-Language", "de"), ""); !strings.Contains(a.body, "gibt es nicht") ||
			a.header.Get("Content-Language") != "de" {
			t.Errorf("German error: %s", a.body)
		}
	})
}

// TestODataExtensionOfTRFIN runs the action and the dependent entity that
// TROTTO adds to the companies of TRFIN through the service of TRFIN.
func TestODataExtensionOfTRFIN(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		c := odataKernel(t, k)
		if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: "anna", Role: "/TROTTO/SYNC_OPERATOR", Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
			t.Fatal(err)
		}
		if err := k.ActivateService(ctx, companyService, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Run(ctx, kernel.ExecOptions{Tenant: "ACME", User: model.Some("anna")}, "/TRFIN/COMPANY_SERVICE", "ADD_COUNTRY",
			[]json.RawMessage{json.RawMessage(`"FR"`), json.RawMessage(`"France"`)}); err != nil {
			t.Fatal(err)
		}
		anna := basicAuth("anna", "secret")
		const root = "/rt/odata/trfin/company_service/"
		a := c.do("POST", root+"Companies", with(anna), `{"COMPANY_ID":"C1","NAME":"Acme","COUNTRY":"FR","Contacts":[{"NAME":"Ann","EMAIL":"ann@example.com"}]}`)
		if a.status != 201 || !strings.Contains(a.body, `"TROTTO_OTTO_ID":null`) {
			t.Fatalf("POST company: %d %s", a.status, a.body)
		}
		if a := c.do("POST", root+"Companies('C1')/TROTTO_COMPANY_CHANNELS", with(anna), `{"CHANNEL":"web"}`); a.status != 201 ||
			!strings.Contains(a.header.Get("Location"), "Companies('C1')/TROTTO_COMPANY_CHANNELS(") {
			t.Errorf("POST along the contained navigation: %d %v %s", a.status, a.header, a.body)
		}
		if a := c.do("GET", root+"Companies('C1')?$expand=TROTTO_COMPANY_CHANNELS,Contacts", anna, ""); !strings.Contains(a.body, `"CHANNEL":"web"`) ||
			!strings.Contains(a.body, `"EMAIL":"ann@example.com"`) {
			t.Errorf("expanded: %s", a.body)
		}
		// The action of the extension: FR has no marketplace, so the
		// authorization of /TROTTO/MARKETPLACE fails.
		if a := c.do("POST", root+"Companies('C1')/TRFIN.TROTTO_SYNC", with(anna), ""); a.status != 403 {
			t.Errorf("TROTTO_SYNC in France: %d %s", a.status, a.body)
		}
	})
}
