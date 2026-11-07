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
	"path/filepath"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// haltingLog stops an execution at its first log statement until the test
// releases it.
type haltingLog struct {
	reached, release chan struct{}
}

func (h *haltingLog) Log(level, text string) {
	close(h.reached)
	<-h.release
}

// TestGenerations checks that an execution keeps the code it started with
// while an upgrade commits new code, and that the next execution uses
// the new code.
func TestGenerations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		gen := func(v string) *model.Package {
			return testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "gen", v, "TRGEN"))
		}
		if _, err := k.Install(ctx, []*model.Package{gen("v1")}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.Tenants().Create(ctx, "ACME", model.None[string]()); err != nil {
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", "/TRGEN/VERSION", "RUN", `"v1"`)

		h := &haltingLog{reached: make(chan struct{}), release: make(chan struct{})}
		type result struct {
			out string
			err error
		}
		done := make(chan result)
		go func() {
			out, err := call(k, kernel.ExecOptions{Tenant: "ACME", VM: vm.Options{Log: h}}, "/TRGEN/VERSION", "RUN")
			done <- result{out, err}
		}()
		<-h.reached
		if _, err := k.Upgrade(ctx, gen("v2"), kernel.Options{}); err != nil {
			close(h.release)
			t.Fatal(err)
		}
		expectCall(t, k, "ACME", "/TRGEN/VERSION", "RUN", `"v2"`)
		close(h.release)
		r := <-done
		if r.err != nil || r.out != `"v1"` {
			t.Errorf("the running execution returned %s, %v; want the old code's v1", r.out, r.err)
		}
		expectCall(t, k, "ACME", "/TRGEN/VERSION", "RUN", `"v2"`)
	})
}
