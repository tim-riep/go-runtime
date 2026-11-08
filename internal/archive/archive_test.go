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

package archive

import "testing"

// TestName normalizes names and rejects those that could escape a folder.
func TestName(t *testing.T) {
	for in, want := range map[string]string{"a.txt": "a.txt", "./a//b.txt": "a/b.txt", "dir/": "dir/", "a/./b/": "a/b/", "Grüße.txt": "Grüße.txt"} {
		if got, err := Name(in, false); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if got, _ := Name("docs", true); got != "docs/" {
		t.Errorf("folder: %q", got)
	}
	for _, in := range []string{"", "/", "../x", "a/../../x", "/etc/passwd", `C:\x`, "C:x", `a\b`, "a\x00b", "a\nb", "a\x7fb", "\xff"} {
		if got, err := Name(in, false); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
	if got := DecodeName("Gr\x81\xe1e", false); got != "Grüße" {
		t.Errorf("CP437: %q", got)
	}
	if got := DecodeName("Grüße", false); got != "Grüße" {
		t.Errorf("UTF-8 without flag: %q", got)
	}
}
