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

package kernel

import (
	"context"
	"path/filepath"
	"testing"
)

// TestAfterCommit checks the functions that run after the next commit of
// a session and are discarded by rollback.
func TestAfterCommit(t *testing.T) {
	ctx := context.Background()
	k, err := Open(ctx, "sqlite:"+filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	x := &execution{k: k, d: k.d, opts: ExecOptions{}}
	x.opts.defaults()
	s := &session{x: x, n: 1}
	x.sessions = []*session{s}
	defer s.close()

	var ran []string
	if err := s.begin(ctx); err != nil {
		t.Fatal(err)
	}
	s.AfterCommit(func() { ran = append(ran, "dropped") })
	if err := s.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.begin(ctx); err != nil {
		t.Fatal(err)
	}
	s.AfterCommit(func() { ran = append(ran, "first") })
	s.AfterCommit(func() { ran = append(ran, "second") })
	if len(ran) != 0 {
		t.Fatalf("ran before commit: %v", ran)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Errorf("ran = %v", ran)
	}
}
