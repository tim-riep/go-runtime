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

package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/rterr"
)

func pkg(id, version string, reqs ...string) *model.Package {
	p := &model.Package{Manifest: model.Manifest{ID: model.PackageID(id), Version: version}}
	for _, r := range reqs {
		rid, c, _ := strings.Cut(r, " ")
		p.Manifest.Requires = append(p.Manifest.Requires, model.Dependency{ID: model.PackageID(rid), Constraint: c})
	}
	return p
}

func ids(ps []*model.Package) string {
	var s []string
	for _, p := range ps {
		s = append(s, string(p.Manifest.ID))
	}
	return strings.Join(s, ",")
}

func TestOrder(t *testing.T) {
	order, err := Resolve(nil, []*model.Package{
		pkg("TROTTO", "1.0.0", "TRFIN >=1.0.0"),
		pkg("TRFIN", "1.0.0", "TRBASE"),
		pkg("TRBASE", "1.0.0"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(order); got != "TRBASE,TRFIN,TROTTO" {
		t.Fatalf("order = %s", got)
	}
}

func TestInstalledDependency(t *testing.T) {
	installed := map[model.PackageID]model.Manifest{"TRBASE": pkg("TRBASE", "1.2.0").Manifest}
	order, err := Resolve(installed, []*model.Package{pkg("TRFIN", "1.0.0", "TRBASE ^1.0.0")})
	if err != nil || ids(order) != "TRFIN" {
		t.Fatalf("got %v, %v", ids(order), err)
	}
}

func dependencyKind(t *testing.T, err error, kind rterr.DependencyKind) *rterr.DependencyError {
	t.Helper()
	for _, d := range rterr.All[*rterr.DependencyError](err) {
		if d.Kind == kind {
			return d
		}
	}
	t.Fatalf("no %s error in %v", kind, err)
	return nil
}

func TestMissing(t *testing.T) {
	_, err := Resolve(nil, []*model.Package{pkg("TRFIN", "1.0.0", "TRBASE")})
	d := dependencyKind(t, err, rterr.DependencyMissing)
	if d.Package != "TRFIN" || d.Required != "TRBASE" {
		t.Fatalf("%+v", d)
	}
}

func TestVersionMismatch(t *testing.T) {
	_, err := Resolve(nil, []*model.Package{pkg("TRBASE", "0.9.0"), pkg("TRFIN", "1.0.0", "TRBASE >=1.0.0")})
	d := dependencyKind(t, err, rterr.DependencyVersion)
	if d.Found != "0.9.0" {
		t.Fatalf("%+v", d)
	}
}

func TestUpgradeBreaksDependent(t *testing.T) {
	installed := map[model.PackageID]model.Manifest{
		"TRBASE": pkg("TRBASE", "1.0.0").Manifest,
		"TRFIN":  pkg("TRFIN", "1.0.0", "TRBASE ^1.0.0").Manifest,
	}
	_, err := Resolve(installed, []*model.Package{pkg("TRBASE", "2.0.0")})
	d := dependencyKind(t, err, rterr.DependencyVersion)
	if d.Package != "TRFIN" {
		t.Fatalf("%+v", d)
	}
}

func TestCycle(t *testing.T) {
	_, err := Resolve(nil, []*model.Package{pkg("A", "1.0.0", "B"), pkg("B", "1.0.0", "C"), pkg("C", "1.0.0", "A")})
	d := dependencyKind(t, err, rterr.DependencyCycle)
	if got := strings.Join(d.Cycle, ">"); got != "A>B>C>A" {
		t.Fatalf("cycle = %s", got)
	}
}

func TestDuplicate(t *testing.T) {
	_, err := Resolve(nil, []*model.Package{pkg("A", "1.0.0"), pkg("A", "1.1.0")})
	var d *rterr.DependencyError
	if !errors.As(err, &d) || d.Kind != rterr.DependencyConflict {
		t.Fatalf("got %v", err)
	}
}
