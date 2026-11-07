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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/lang/build"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/testrun"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

// TestLanguageReference compiles every example of docs/language.md with
// the example package TRORDER and runs the fragments as tests.
func TestLanguageReference(t *testing.T) {
	root := testutil.ModuleRoot(t)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "language.md"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := pkgload.Load(os.DirFS(filepath.Join(root, "examples", "TRORDER")), "TRORDER")
	if err != nil {
		t.Fatal(err)
	}
	files, fragments := 0, 0
	lines := strings.Split(string(doc), "\n")
	for i := 0; i < len(lines); i++ {
		kind := strings.TrimPrefix(lines[i], "```")
		if kind != "rt" && kind != "rt-fragment" || !strings.HasPrefix(lines[i], "```") {
			continue
		}
		start := i + 1
		for i++; i < len(lines) && lines[i] != "```"; i++ {
		}
		text := strings.Join(lines[start:i], "\n") + "\n"
		where := fmt.Sprintf("docs/language.md:%d", start)
		if kind == "rt-fragment" {
			fragments++
			var body strings.Builder
			for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
				body.WriteString("    " + l + "\n")
			}
			name := fmt.Sprintf("/TRORDER/DOC_FRAGMENT_%d", fragments)
			text = "public test class " + name + "\n\n  public test method RUN()\n" + body.String() + "  end\n\nend\n"
			out, errs := syntax.Format(where, text)
			if len(errs) > 0 {
				t.Fatalf("%s: %v", where, errs)
			}
			if out != text {
				t.Errorf("%s: the fragment is not in canonical form", where)
			}
			pkg.Sources = append(pkg.Sources, model.Source{Name: name, Kind: model.SourceClass, File: where, Text: text})
			continue
		}
		files++
		f, errs := syntax.Parse(where, text)
		if len(errs) > 0 {
			t.Fatalf("%s: %v", where, errs)
		}
		if syntax.Print(f) != text {
			t.Errorf("%s: the example is not in canonical form", where)
		}
		kinds := map[syntax.FileKind]model.SourceKind{syntax.ClassFile: model.SourceClass, syntax.InterfaceFile: model.SourceInterface, syntax.IncludeFile: model.SourceInclude}
		pkg.Sources = append(pkg.Sources, model.Source{Name: f.Name().Name, Kind: kinds[f.Kind], File: where, Text: text})
	}
	if files < 5 || fragments < 4 {
		t.Fatalf("found %d files and %d fragments", files, fragments)
	}
	r := build.Package(pkg, nil)
	for _, d := range r.Errors() {
		t.Error(d)
	}
	if !r.OK() {
		t.FailNow()
	}
	p, err := r.Compile(true)
	if err != nil {
		t.Fatal(err)
	}
	results, err := testrun.Run(context.Background(), p, vm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if !res.Passed {
			t.Errorf("%s.%s: %s at %s:%d", res.Class, res.Method, res.Message, res.File, res.Line)
		}
	}
}
