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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/lang/syntax"
)

func format(t *testing.T, path, src string) string {
	t.Helper()
	out, errs := syntax.Format(path, src)
	if len(errs) > 0 {
		t.Fatalf("%s: %v", path, errs)
	}
	return out
}

// TestFormatGolden formats inputs with comments at every possible place
// and with irregular layout.
func TestFormatGolden(t *testing.T) {
	ins, _ := filepath.Glob(filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "fmt", "*.in"))
	if len(ins) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range ins {
		b, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(in), ".in")
		testutil.Golden(t, "fmt/"+name+".out", format(t, in, string(b)))
	}
}

// sources returns every source file of the repository and every
// formatter input.
func sources(t *testing.T) map[string]string {
	out := map[string]string{}
	root := testutil.ModuleRoot(t)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		switch filepath.Ext(p) {
		case ".class", ".interface", ".include", ".in":
			b, err := os.ReadFile(p)
			if err == nil {
				out[p] = string(b)
			}
		}
		return nil
	})
	return out
}

// TestIdempotent requires fmt(fmt(x)) = fmt(x) for all source files and
// that canonical files stay unchanged.
func TestIdempotent(t *testing.T) {
	for p, src := range sources(t) {
		once := format(t, p, src)
		if twice := format(t, p, once); twice != once {
			t.Errorf("%s: formatting is not idempotent:\n%s", p, twice)
		}
		if filepath.Ext(p) != ".in" && once != src {
			t.Errorf("%s is not in canonical form:\n%s", p, once)
		}
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"public class /X/A\n  public method M()\n    var = 1\n  end\nend\n", "3:9: expected a name"},
		{"public class /X/A\n  public method M()\n    x = (1 +\n  end\nend\n", "expected an expression"},
		{"public class /X/A\n  public method M()\n    var s = \"abc\n  end\nend\n", "3:13: string is not terminated"},
		{"public class /X/A\n  public method M()\n    if a = b = c\n    end\n  end\nend\n", "comparisons cannot be chained"},
		{"public class /X/A\n\tpublic constant A: INT4 = 1\nend\n", "2:1: tabs"},
		{"class /X/A\nend\n", "1:1: a file starts with public"},
		{"public class /X/A\n  public method M()\n    try\n      x = 1\n    end\n  end\nend\n", "try needs catch or finally"},
		{"public class /X/A\nend\nend\n", "after the end of the declaration"},
		{"public class /X/A\n  public method M()\n    var s = \"{}\"\n  end\nend\n", "empty placeholder"},
		{"public class /X/A\n  public method M()\n    log loud \"x\"\n  end\nend\n", "expected a log level"},
		{"public class /X/A\n  public method M()\n    var x = /X\n  end\nend\n", "a global name has the form"},
	} {
		_, errs := syntax.Parse("f", c.src)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), c.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: expected %q, got %v", c.src, c.want, errs)
		}
	}
}

// TestDivisionAndNamespace checks that "/" after an operand divides and
// starts a global name elsewhere.
func TestDivisionAndNamespace(t *testing.T) {
	x, errs := syntax.ParseExpr("a / /X/C.LIMIT / B")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	b := x.(*syntax.BinaryExpr)
	if b.Op != "/" || b.Y.(*syntax.Ident).Name != "B" {
		t.Fatalf("%#v", x)
	}
	inner := b.X.(*syntax.BinaryExpr)
	if inner.Y.(*syntax.SelectExpr).X.(*syntax.GlobalExpr).Name.Name != "/X/C" {
		t.Fatalf("%#v", inner)
	}
	if got := syntax.PrintExpr(x); got != "a / /X/C.LIMIT / B" {
		t.Fatal(got)
	}
	if x, _ := syntax.ParseExpr("a/2"); syntax.PrintExpr(x) != "a / 2" {
		t.Fatal(syntax.PrintExpr(x))
	}
}

func TestPrecedence(t *testing.T) {
	for src, want := range map[string]string{
		"a or b and not c = d":    "or(a, and(b, not(=(c, d))))",
		"-a * b + c mod d":        "+(*(-(a), b), mod(c, d))",
		"x default y + 1":         "default(x, +(y, 1))",
		"-a as INT8":              "-(as(a, INT8))",
		"o.A is not null and o.B": "and(is not null(.A(o)), .B(o))",
	} {
		x, errs := syntax.ParseExpr(src)
		if len(errs) > 0 {
			t.Fatal(src, errs)
		}
		if got := tree(x); got != want {
			t.Errorf("%s: got %s, want %s", src, got, want)
		}
	}
}

func tree(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.BinaryExpr:
		return x.Op + "(" + tree(x.X) + ", " + tree(x.Y) + ")"
	case *syntax.UnaryExpr:
		return x.Op + "(" + tree(x.X) + ")"
	case *syntax.IsExpr:
		not := ""
		if x.Not {
			not = "not "
		}
		return "is " + not + x.What + "(" + tree(x.X) + ")"
	case *syntax.AsExpr:
		return "as(" + tree(x.X) + ", " + syntax.PrintType(x.Type) + ")"
	case *syntax.SelectExpr:
		return "." + x.Name.Name + "(" + tree(x.X) + ")"
	}
	return syntax.PrintExpr(x)
}
