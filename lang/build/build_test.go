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

package build_test

import (
	"path/filepath"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/lang/build"
)

// TestAssignmentExample builds the example of the language assignment.
func TestAssignmentExample(t *testing.T) {
	r, err := build.Dir(filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "TRFIN"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Diagnostics {
		t.Log(d)
	}
	if !r.OK() {
		t.Fatal("build failed")
	}
}
