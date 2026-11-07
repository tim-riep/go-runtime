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

package pkgload

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

func sourceFS() fstest.MapFS {
	fsys := validFS()
	fsys["manifest.json"] = file(`{"id":"TRX","version":"1.0.0","languageVersion":1,"requires":[{"id":"TRBASE","version":">=1.0.0"}]}`)
	fsys["src/RATING.class"] = file("public class /TRX/RATING implements /TRBASE/RATER\n\n  public constant LIMIT: /TRX/QTY = 1\n\nend\n")
	fsys["src/RATER.interface"] = file("public interface /TRX/RATER\nend\n")
	fsys["src/CHECKS.include"] = file("statements include /TRX/CHECKS\n  log info \"{/TRX/RATING.LIMIT}\"\nend\n")
	return fsys
}

func TestLoadSources(t *testing.T) {
	pkg, err := Load(sourceFS(), "mem")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.LanguageVersion != 1 || len(pkg.Sources) != 3 {
		t.Fatalf("sources: %+v", pkg.Sources)
	}
	kinds := map[string]model.SourceKind{}
	for _, s := range pkg.Sources {
		kinds[s.Name] = s.Kind
	}
	if kinds["/TRX/RATING"] != model.SourceClass || kinds["/TRX/RATER"] != model.SourceInterface || kinds["/TRX/CHECKS"] != model.SourceInclude {
		t.Fatalf("kinds: %v", kinds)
	}
}

func TestSourceErrors(t *testing.T) {
	for _, c := range []struct {
		change func(fstest.MapFS)
		want   string
	}{
		{func(f fstest.MapFS) {
			f["manifest.json"] = file(`{"id":"TRX","version":"1.0.0","requires":[{"id":"TRBASE"}]}`)
		}, "languageVersion is required"},
		{func(f fstest.MapFS) {
			f["manifest.json"] = file(`{"id":"TRX","version":"1.0.0","languageVersion":9,"requires":[{"id":"TRBASE"}]}`)
		}, "language version 9 is not supported"},
		{func(f fstest.MapFS) {
			f["src/BROKEN.class"] = file("public class /TRX/BROKEN\n  public method M(\nend\n")
		}, "file src/BROKEN.class, line 3, column 1: expected a name"},
		{func(f fstest.MapFS) { f["src/WRONG.class"] = file("public interface /TRX/WRONG\nend\n") }, "the file must contain a class"},
		{func(f fstest.MapFS) { f["src/OTHER.class"] = file("public class /TRX/NAMED\nend\n") }, "must be named NAMED.class"},
		{func(f fstest.MapFS) { f["src/sub/A.class"] = file("public class /TRX/A\nend\n") }, "does not belong to any known object type"},
		{func(f fstest.MapFS) {
			f["src/USES.class"] = file("public class /TRX/USES\n\n  public constant A: /TROTTO/X = 1\n\nend\n")
		}, "file src/USES.class, line 3, column 22: reference /TROTTO/X"},
		{func(f fstest.MapFS) { f["src/FOREIGN.class"] = file("public class /TRZ/FOREIGN\nend\n") }, "line 1, column 14: definition /TRZ/FOREIGN"},
	} {
		fsys := sourceFS()
		c.change(fsys)
		_, err := Load(fsys, "mem")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("expected %q, got %v", c.want, err)
		}
	}
	// Placeholders in strings are part of the namespace check.
	fsys := sourceFS()
	fsys["src/CHECKS.include"] = file("statements include /TRX/CHECKS\n  log info \"total {/TRZ/C.A}\"\nend\n")
	_, err := Load(fsys, "mem")
	vs := rterr.All[*rterr.NamespaceViolation](err)
	if len(vs) != 1 || vs[0].Location.Line != 2 || vs[0].Location.Column != 20 {
		t.Fatalf("placeholder reference: %v", err)
	}
}
