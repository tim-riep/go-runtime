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

package syntax_test

import (
	"testing"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

func seed(f *testing.F) {
	for _, src := range sources(&testing.T{}) {
		f.Add(src)
	}
	f.Add("public class /X/A\nend\n")
	f.Add(`"{a {"b"}}"`)
}

// FuzzLex requires that the lexer never panics and always ends with EOF.
func FuzzLex(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := syntax.Lex(src)
		if len(toks) == 0 || toks[len(toks)-1].Kind != syntax.EOF {
			t.Fatal("no EOF token")
		}
	})
}

// FuzzParse requires that every input that parses without errors
// survives formatting and parsing again unchanged.
func FuzzParse(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, src string) {
		file, errs := syntax.Parse("fuzz", src)
		if len(errs) > 0 {
			return
		}
		out := syntax.Print(file)
		again, errs := syntax.Parse("fuzz", out)
		if len(errs) > 0 {
			t.Fatalf("formatted output does not parse: %v\ninput:\n%q\noutput:\n%s", errs, src, out)
		}
		if twice := syntax.Print(again); twice != out {
			t.Fatalf("formatting changed again:\n%q\n%q", out, twice)
		}
	})
}
