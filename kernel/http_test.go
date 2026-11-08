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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// servicePackage writes a package TRSVC with service nodes.
func servicePackage(t *testing.T, version string, nodes map[string]string) string {
	dir := filepath.Join(t.TempDir(), "TRSVC")
	testutil.WriteFile(t, dir, "manifest.json", []byte(`{"id":"TRSVC","version":"`+version+`"}`))
	for file, doc := range nodes {
		testutil.WriteFile(t, dir, "schema/http-services/"+file, []byte(doc))
	}
	return dir
}

func stateOf(t *testing.T, k *kernel.Kernel, path string) (string, bool) {
	t.Helper()
	n, err := k.HTTPService(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return n.State, n.Reachable
}

// TestHTTPServiceLifecycle installs service nodes, activates them, keeps
// the activation across an upgrade and removes it with the package.
func TestHTTPServiceLifecycle(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		v1 := map[string]string{
			"root.json":  `{"path":"","texts":{"en":{"label":"Service"}},"settings":{"timeoutSeconds":5}}`,
			"items.json": `{"path":"items/v1","texts":{"en":{"label":"Items"},"de":{"label":"Posten"}},"settings":{"anonymous":true,"anonymousTenant":"ACME"}}`,
		}
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, servicePackage(t, "1.0.0", v1))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		list, err := k.HTTPServices(ctx, "TRSVC")
		if err != nil || len(list) != 3 {
			t.Fatalf("nodes: %+v, %v", list, err)
		}
		n, _ := k.HTTPService(ctx, "/trsvc/items/v1")
		if n.Settings.Timeout != 5*time.Second || !n.Settings.Anonymous || n.Settings.AnonymousTenant.Or("") != "ACME" || n.Texts["de"].Label != "Posten" ||
			n.State != "new" || n.Reachable {
			t.Errorf("node: %+v", n)
		}
		if mid, _ := k.HTTPService(ctx, "trsvc/items"); mid.File.Valid() || mid.Settings.Timeout != 5*time.Second {
			t.Errorf("implicit node: %+v", mid)
		}

		if err := k.DeactivateHTTPService(ctx, "/trsvc/items"); err != nil {
			t.Fatal(err)
		}
		if err := k.ActivateHTTPService(ctx, "/trsvc"); err != nil {
			t.Fatal(err)
		}
		if st, reach := stateOf(t, k, "/trsvc/items/v1"); st != "active" || reach {
			t.Errorf("below a deactivated node: %s %v", st, reach)
		}
		if err := k.ActivateHTTPService(ctx, "/trsvc/items"); err != nil {
			t.Fatal(err)
		}
		if _, reach := stateOf(t, k, "/trsvc/items/v1"); !reach {
			t.Error("not reachable after activation")
		}
		var nf *rterr.NotFound
		if err := k.ActivateHTTPService(ctx, "/trsvc/nope"); !errors.As(err, &nf) {
			t.Errorf("unknown node: %v", err)
		}

		// An upgrade keeps the states and adds new nodes as new.
		v2 := map[string]string{
			"root.json":  v1["root.json"],
			"items.json": v1["items.json"],
			"more.json":  `{"path":"items/v2","texts":{"en":{"label":"Items 2"}}}`,
		}
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, servicePackage(t, "1.1.0", v2)), kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, reach := stateOf(t, k, "/trsvc/items/v1"); !reach {
			t.Error("the upgrade lost the activation")
		}
		if st, reach := stateOf(t, k, "/trsvc/items/v2"); st != "new" || reach {
			t.Errorf("new node of the upgrade: %s %v", st, reach)
		}
		consistent(t, k)

		if _, err := k.Uninstall(ctx, "TRSVC", kernel.Options{Confirm: true}); err != nil {
			t.Fatal(err)
		}
		var left int
		if err := k.DB().QueryRowContext(ctx, `SELECT count(*) FROM "/RT/HTTP_SERVICE_STATES" WHERE "PACKAGE_ID" = 'TRSVC'`).Scan(&left); err != nil || left != 0 {
			t.Errorf("states left: %d %v", left, err)
		}
		consistent(t, k)
	})
}

// TestNodeCheckDefinitions rejects additional checks of service nodes
// whose object, fields or values do not fit, and needs the object's
// package in requires.
func TestNodeCheckDefinitions(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		node := func(auth string) string {
			dir := servicePackage(t, "1.0.0", map[string]string{"node.json": `{"path":"node","texts":{"en":{"label":"Node"}},"settings":{"authorization":` + auth + `}}`})
			testutil.WriteFile(t, dir, "manifest.json", []byte(`{"id":"TRSVC","version":"1.0.0","requires":[{"id":"TRBASE"}]}`))
			return dir
		}
		for _, c := range []struct{ auth, want string }{
			{`{"object":"/RT/NONE","fields":{}}`, "authorization object /RT/NONE does not exist"},
			{`{"object":"/TRBASE/USER_ADMIN","fields":{"USER_NAME":{"any":true}}}`, "needs the field ACTIVITY"},
			{`{"object":"/TRBASE/USER_ADMIN","fields":{"USER_NAME":{"any":true},"ACTIVITY":"EXECUTE"}}`, `does not allow the activity "EXECUTE"`},
			{`{"object":"/TRBASE/USER_ADMIN","fields":{"USER_NAME":{"any":true},"ACTIVITY":"READ","COLOR":"RED"}}`, "has no field COLOR"},
			{`{"object":"/TRBASE/HTTP_SERVICE","fields":{"PACKAGE_ID":"` + strings.Repeat("X", 40) + `","SERVICE_PATH":{"any":true},"ACTIVITY":"EXECUTE"}}`, "is no value of PACKAGE_ID"},
		} {
			_, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, node(c.auth))}, kernel.Options{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v", c.auth, err)
			}
		}
		if _, err := kernel.LoadDir(node(`{"object":"/TRBASE/USER_ADMIN","fields":{"USER_NAME":{"any":false},"ACTIVITY":"READ"}}`)); err == nil ||
			!strings.Contains(err.Error(), "at /settings/authorization") {
			t.Errorf("any false: %v", err)
		}
		// Objects of packages that are not required are not visible.
		_, err := kernel.LoadDir(node(`{"object":"/TRFIN/COMPANY","fields":{}}`))
		var nv *rterr.NamespaceViolation
		if !errors.As(err, &nv) {
			t.Errorf("object of another package: %v", err)
		}
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t,
			node(`{"object":"/TRBASE/USER_ADMIN","fields":{"USER_NAME":"anna","ACTIVITY":"READ"}}`))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		n, err := k.HTTPService(ctx, "/trsvc/node")
		if err != nil || n.Settings.Authorization == nil || n.Settings.Authorization.Fields["USER_NAME"].Value != "anna" {
			t.Errorf("node: %+v %v", n, err)
		}
	})
}
