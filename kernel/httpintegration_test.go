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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
)

// TestHTTPTokensAndUpgrade signs in with API tokens and lets a request run
// while an upgrade commits new code: it ends with the code it started
// with, the next request gets the new code, and the activation of the
// nodes survives the upgrade.
func TestHTTPTokensAndUpgrade(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		h := &haltingLog{reached: make(chan struct{}), release: make(chan struct{})}
		k.SetHTTPLog(h)
		c := httpKernel(t, k)
		if err := k.ActivateHTTPService(ctx, "/trhttp"); err != nil {
			t.Fatal(err)
		}
		token, info, err := k.Tokens().Create(ctx, "anna", "test", model.None[time.Time]())
		if err != nil {
			t.Fatal(err)
		}
		bearer := func(tok string) func(r *http.Request) {
			return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
		}
		if a := c.do("GET", "/trhttp/notes", bearer(token), ""); a.status != 200 || a.body != "0" {
			t.Fatalf("token: %d %s", a.status, a.body)
		}
		if a := c.do("GET", "/trhttp/notes", bearer(token+"x"), ""); a.status != 401 {
			t.Errorf("wrong token: %d", a.status)
		}

		done := make(chan answer)
		go func() { done <- c.do("GET", "/trhttp/version", bearer(token), "") }()
		<-h.reached
		k.SetHTTPLog(nil)
		dir := testutil.CopyDir(t, httpPackage(t))
		testutil.WriteFile(t, dir, "manifest.json", []byte(`{ "id": "TRHTTP", "version": "1.1.0", "languageVersion": 1, "requires": [{ "id": "TRBASE" }] }`))
		src := strings.Replace(string(mustRead(t, filepath.Join(dir, "src", "VERSION.class"))), `"v1"`, `"v2"`, 1)
		testutil.WriteFile(t, dir, "src/VERSION.class", []byte(src))
		if _, err := k.Upgrade(ctx, testutil.LoadDir(t, dir), kernel.Options{}); err != nil {
			close(h.release)
			t.Fatal(err)
		}
		if a := c.do("GET", "/trhttp/version", bearer(token), ""); a.status != 200 || a.body != "v2" {
			t.Errorf("after the upgrade: %d %s", a.status, a.body)
		}
		close(h.release)
		if a := <-done; a.status != 200 || a.body != "v1" {
			t.Errorf("the running request answered %d %s; want the old code's v1", a.status, a.body)
		}

		if err := k.Tokens().Revoke(ctx, info.ID); err != nil {
			t.Fatal(err)
		}
		if a := c.do("GET", "/trhttp/notes", bearer(token), ""); a.status != 401 {
			t.Errorf("revoked token: %d", a.status)
		}
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
