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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// odataNodeRole lets a user call /rt/odata without starting any service.
const odataNodeRole = `{ "name": "/LOCAL/ODATA_NODE", "texts": { "en": { "label": "OData node" } }, "authorizations": [
	{ "object": "/TRBASE/HTTP_SERVICE", "fields": { "PACKAGE_ID": [{ "value": "RT" }], "SERVICE_PATH": [{ "value": "/rt/odata" }],
		"ACTIVITY": [{ "value": "EXECUTE" }] } } ] }`

// odataKernel installs TRFIN and TROTTO with three users in ACME: anna
// may use the service of companies, bob may call the node of OData but no
// service, carl nothing.
func odataKernel(t *testing.T, k *kernel.Kernel) client {
	t.Helper()
	ctx := context.Background()
	installCompanies(t, k)
	for _, id := range []string{"ACME", "BETA"} {
		if _, err := k.Tenants().Create(ctx, id, model.None[string]()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.Roles().Import(ctx, []byte(odataNodeRole), "ODATA_NODE.json", kernel.RoleOptions{By: "test"}); err != nil {
		t.Fatal(err)
	}
	roles := map[string][]string{
		"anna": {"/TRFIN/ODATA_USER", "/TRFIN/COMPANY_ADMIN"},
		"bob":  {"/LOCAL/ODATA_NODE", "/TRFIN/COMPANY_ADMIN"},
		"carl": {"/TRFIN/COMPANY_ADMIN"},
	}
	for _, user := range []string{"anna", "bob", "carl"} {
		if _, err := k.Users().Create(ctx, user, "secret", kernel.UserOptions{Tenant: model.Some("ACME")}); err != nil {
			t.Fatal(err)
		}
		for _, role := range roles[user] {
			if _, _, err := k.Roles().Assign(ctx, kernel.Assignment{User: user, Role: role, Tenant: "ACME"}, kernel.RoleOptions{By: "test"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	srv := httptest.NewServer(k.HTTPHandler())
	t.Cleanup(srv.Close)
	return client{t, srv.URL}
}

// odataError returns the code of an error in the format of OData.
func odataError(t *testing.T, a answer) string {
	t.Helper()
	var doc struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(a.body), &doc); err != nil || doc.Error.Code == "" || doc.Error.Message == "" {
		t.Fatalf("no error of OData: %d %s", a.status, a.body)
	}
	return doc.Error.Code
}

func TestODataDeployment(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		c := odataKernel(t, k)
		anna, bob, carl := basicAuth("anna", "secret"), basicAuth("bob", "secret"), basicAuth("carl", "secret")
		const root = "/rt/odata/trfin/company_service/"

		// The node of the kernel exists and is active after rt init.
		node, err := k.HTTPService(ctx, "/rt/odata")
		if err != nil || node.State != "active" || !node.Reachable || node.Package != model.Kernel || node.Settings.Anonymous ||
			node.OData == nil || node.OData.MaxPageSize != kernel.DefaultMaxPageSize {
			t.Fatalf("node: %+v, %v", node, err)
		}

		// A service is inactive after installation.
		if a := c.do("GET", root, anna, ""); a.status != 404 || odataError(t, a) != "NOT_FOUND" {
			t.Fatalf("inactive service: %d %s", a.status, a.body)
		}
		if err := k.ActivateService(ctx, companyService, "test"); err != nil {
			t.Fatal(err)
		}
		a := c.do("GET", root, anna, "")
		var doc struct {
			Context string `json:"@odata.context"`
			Value   []struct{ Name, Kind, URL string }
		}
		if err := json.Unmarshal([]byte(a.body), &doc); a.status != 200 || err != nil || len(doc.Value) != 4 || doc.Value[0].Name != "Companies" ||
			doc.Context != c.url+root+"$metadata" || a.header.Get("OData-Version") != "4.0" {
			t.Fatalf("service document: %d %s", a.status, a.body)
		}
		// Package and service ignore case.
		if a := c.do("GET", "/rt/odata/TRFIN/Company_Service/", anna, ""); a.status != 200 {
			t.Errorf("path in other case: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/rt/odata/trfin/nothing/", anna, ""); a.status != 404 {
			t.Errorf("unknown service: %d", a.status)
		}

		// Start authorization of the service and of the node.
		if a := c.do("GET", root, bob, ""); a.status != 403 || odataError(t, a) != "NOT_AUTHORIZED" {
			t.Errorf("without start authorization: %d %s", a.status, a.body)
		}
		if a := c.do("GET", root, carl, ""); a.status != 403 {
			t.Errorf("without authorization of the node: %d %s", a.status, a.body)
		}
		if a := c.do("GET", root, nil, ""); a.status != 401 {
			t.Errorf("anonymous: %d", a.status)
		}
		if a := c.do("GET", root, func(r *http.Request) { anna(r); r.Header.Set("OData-MaxVersion", "3.0") }, ""); a.status != 400 ||
			odataError(t, a) != "UNSUPPORTED_VERSION" {
			t.Errorf("OData 3: %d %s", a.status, a.body)
		}

		// The list of services shows the active services a user may
		// start.
		list := func(user func(*http.Request)) []map[string]string {
			a := c.do("GET", "/rt/odata/", user, "")
			var doc struct{ Value []map[string]string }
			if err := json.Unmarshal([]byte(a.body), &doc); a.status != 200 || err != nil {
				t.Fatalf("list: %d %s", a.status, a.body)
			}
			return doc.Value
		}
		if l := list(anna); len(l) != 1 || l[0]["name"] != companyService || l[0]["package"] != "TRFIN" || l[0]["url"] != root ||
			l[0]["description"] != "Companies" {
			t.Errorf("list of anna: %v", l)
		}
		if l := list(func(r *http.Request) { anna(r); r.Header.Set("Accept-Language", "de") }); len(l) != 1 || l[0]["description"] != "Firmen" {
			t.Errorf("list in German: %v", l)
		}
		if l := list(bob); len(l) != 0 {
			t.Errorf("list of bob: %v", l)
		}
		if err := k.DeactivateService(ctx, companyService, "test"); err != nil {
			t.Fatal(err)
		}
		if l := list(anna); len(l) != 0 {
			t.Errorf("list with inactive service: %v", l)
		}

		// The settings of the node are maintained; anonymous access cannot
		// be switched on, and nodes of packages take theirs from the
		// package.
		var inv *rterr.InvalidArgument
		if err := k.ConfigureHTTPService(ctx, "/rt/odata", kernel.KernelNodeChange{MaxPageSize: model.Some[int64](50), TimeoutSeconds: model.Some[int64](10)}, "test"); err != nil {
			t.Fatal(err)
		}
		if n, _ := k.HTTPService(ctx, "/rt/odata"); n.OData.MaxPageSize != 50 || n.Settings.Timeout.Seconds() != 10 {
			t.Errorf("configured: %+v", n)
		}
		if err := k.ConfigureHTTPService(ctx, "/rt/odata", kernel.KernelNodeChange{Authentication: model.Some([]string{})}, "test"); !errors.As(err, &inv) {
			t.Errorf("without authentication: %v", err)
		}
		if err := k.ConfigureHTTPService(ctx, "/trfin/companies", kernel.KernelNodeChange{MaxPageSize: model.Some[int64](5)}, "test"); !errors.As(err, &inv) {
			t.Errorf("node of a package: %v", err)
		}
		if err := k.ConfigureHTTPService(ctx, "/rt/odata", kernel.KernelNodeChange{Reset: true}, "test"); err != nil {
			t.Fatal(err)
		}
		if n, _ := k.HTTPService(ctx, "/rt/odata"); n.OData.MaxPageSize != kernel.DefaultMaxPageSize {
			t.Errorf("reset: %+v", n.OData)
		}

		// The node belongs to no package: it stays when all packages go.
		for _, id := range []model.PackageID{"TROTTO", "TRFIN"} {
			if _, err := k.Uninstall(ctx, id, kernel.Options{AllowDestructive: true, Confirm: true}); err != nil {
				t.Fatal(err)
			}
		}
		if n, err := k.HTTPService(ctx, "/rt/odata"); err != nil || !n.Reachable {
			t.Errorf("node after uninstalling: %+v, %v", n, err)
		}
		if _, err := k.Uninstall(ctx, model.Kernel, kernel.Options{}); err == nil {
			t.Errorf("uninstalling the kernel: %v", err)
		}
	})
}

// TestODataNodeAfterMigration checks that a migration of an older
// database adds the node of OData, active.
func TestODataNodeAfterMigration(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		k.MigrateKeepingKernelPackages(19)
		initialized(t, k)
		k.MigrateKeepingKernelPackages(kernelschema.Latest())
		if _, err := k.Init(ctx, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if n, err := k.HTTPService(ctx, "/rt/odata"); err != nil || n.State != "active" || !n.Reachable {
			t.Errorf("node after migration: %+v, %v", n, err)
		}
	})
}
