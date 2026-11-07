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
	"errors"
	"testing"
	"testing/fstest"

	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/rterr"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func validFS() fstest.MapFS {
	return fstest.MapFS{
		"manifest.json": file(`{"id":"TRX","version":"1.0.0","requires":[{"id":"TRBASE","version":">=1.0.0"}]}`),
		"schema/domains/STATUS.json": file(`{"name":"STATUS","type":"CHAR","length":3,
			"values":[{"value":"NEW"},{"value":"OLD","description":"Old"}]}`),
		"schema/domains/QTY.json": file(`{"name":"QTY","type":"INT4","values":[{"value":1},{"value":2}]}`),
		"schema/tables/ORDERS.json": file(`{"name":"ORDERS","tenantSpecific":true,
			"fields":[{"name":"ORDER_ID","domain":"ID","notNull":true},{"name":"STATUS","domain":"STATUS","default":"NEW"}],
			"primaryKey":["ORDER_ID"],
			"foreignKeys":[{"name":"STATUS","table":"OTHER","fields":[{"field":"STATUS","references":"CODE"}]}]}`),
		"schema/extensions/COMPANIES.json": file(`{"table":"COMPANIES",
			"fields":[{"name":"EXT_ID","domain":"ID"}],
			"foreignKeys":[{"name":"EXT","table":"ORDERS","fields":[{"field":"EXT_ID","references":"ORDER_ID"}]}]}`),
	}
}

func TestLoadValid(t *testing.T) {
	pkg, err := Load(validFS(), "mem")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "TRX" || len(pkg.Manifest.Requires) != 1 || pkg.Manifest.Requires[0].Constraint != ">=1.0.0" {
		t.Fatalf("manifest: %+v", pkg.Manifest)
	}
	if len(pkg.Domains) != 2 || len(pkg.Tables) != 1 || len(pkg.Extensions) != 1 {
		t.Fatalf("objects: %d domains, %d tables, %d extensions", len(pkg.Domains), len(pkg.Tables), len(pkg.Extensions))
	}
	qty := pkg.Domains[0]
	if qty.Name != "QTY" || qty.Values[1].Value != "2" {
		t.Errorf("QTY: %+v", qty)
	}
	ext := pkg.Extensions[0]
	if ext.Fields[0].Name != "/TRX/EXT_ID" || ext.Fields[0].LocalName != "EXT_ID" || ext.Fields[0].Origin != model.OriginExtension {
		t.Errorf("extension field: %+v", ext.Fields[0])
	}
	if fk := ext.ForeignKeys[0]; fk.Name != "/TRX/EXT" || fk.Columns[0].Field != "/TRX/EXT_ID" || fk.Columns[0].References != "ORDER_ID" {
		t.Errorf("extension key: %+v", fk)
	}
	if f := pkg.Tables[0].Fields[1]; string(f.DefaultJSON) != `"NEW"` || f.Src.Pointer != "/fields/1" {
		t.Errorf("field: %+v", f)
	}
}

func expectError(t *testing.T, fsys fstest.MapFS, code rterr.Code, file, pointer string) {
	t.Helper()
	_, err := Load(fsys, "mem")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Code == code && v.Location.File == file && v.Location.Pointer == pointer {
			return
		}
	}
	t.Fatalf("no %s error at %s%s in:\n%v", code, file, pointer, err)
}

func TestLoadErrors(t *testing.T) {
	t.Run("missing tenant flag", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/tables/ORDERS.json"] = file(`{"name":"ORDERS","fields":[{"name":"A","domain":"ID"}],"primaryKey":["A"]}`)
		expectError(t, fsys, rterr.CodeSchema, "schema/tables/ORDERS.json", "")
	})
	t.Run("invalid field name", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/tables/ORDERS.json"] = file(`{"name":"ORDERS","tenantSpecific":false,"fields":[{"name":"a","domain":"ID"}],"primaryKey":["A"]}`)
		expectError(t, fsys, rterr.CodeSchema, "schema/tables/ORDERS.json", "/fields/0/name")
	})
	t.Run("missing manifest", func(t *testing.T) {
		fsys := validFS()
		delete(fsys, "manifest.json")
		expectError(t, fsys, rterr.CodeManifestMissing, "manifest.json", "")
	})
	t.Run("reserved package id", func(t *testing.T) {
		fsys := validFS()
		fsys["manifest.json"] = file(`{"id":"RT","version":"1.0.0"}`)
		expectError(t, fsys, rterr.CodeName, "manifest.json", "/id")
	})
	t.Run("bad version", func(t *testing.T) {
		fsys := validFS()
		fsys["manifest.json"] = file(`{"id":"TRX","version":"1.0"}`)
		expectError(t, fsys, rterr.CodeSchema, "manifest.json", "/version")
	})
	t.Run("syntax", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/domains/QTY.json"] = file(`{"name":`)
		expectError(t, fsys, rterr.CodeSyntax, "schema/domains/QTY.json", "")
	})
	t.Run("file name", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/domains/OTHER.json"] = file(`{"name":"QTY2","type":"BOOL"}`)
		expectError(t, fsys, rterr.CodeFile, "schema/domains/OTHER.json", "/name")
	})
	t.Run("char without length", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/domains/QTY.json"] = file(`{"name":"QTY","type":"CHAR"}`)
		expectError(t, fsys, rterr.CodeSchema, "schema/domains/QTY.json", "")
	})
	t.Run("fixed value too long", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/domains/STATUS.json"] = file(`{"name":"STATUS","type":"CHAR","length":3,"values":[{"value":"LONG"}]}`)
		expectError(t, fsys, rterr.CodeFixedValue, "schema/domains/STATUS.json", "/values/0/value")
	})
	t.Run("unexpected directory", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/table/X.json"] = file(`{}`)
		expectError(t, fsys, rterr.CodeFile, "schema/table", "")
	})
	t.Run("errors carry package", func(t *testing.T) {
		fsys := validFS()
		fsys["schema/domains/QTY.json"] = file(`{"name":"QTY","type":"NUMBER"}`)
		_, err := Load(fsys, "mem")
		var v *rterr.ValidationError
		if !errors.As(err, &v) || v.Location.Package != "TRX" {
			t.Fatalf("got %v", err)
		}
	})
}

func TestLoadKernel(t *testing.T) {
	fsys := fstest.MapFS{
		"manifest.json":             file(`{"id":"RT","version":"1.0.0"}`),
		"schema/domains/NAME.json":  file(`{"name":"NAME","type":"CHAR","length":10}`),
		"schema/tables/THINGS.json": file(`{"name":"THINGS","tenantSpecific":false,"fields":[{"name":"NAME","domain":"NAME"}],"primaryKey":["NAME"],"foreignKeys":[{"name":"SELF","table":"THINGS","fields":[{"field":"NAME","references":"NAME"}]}]}`),
	}
	pkg, err := LoadWith(fsys, "kernel", Options{Kernel: true})
	if err != nil {
		t.Fatal(err)
	}
	tb := pkg.Tables[0]
	if tb.Name != "/RT/THINGS" || tb.Owner != model.Kernel || tb.Fields[0].Domain != "/RT/NAME" || tb.ForeignKeys[0].Target != "/RT/THINGS" {
		t.Fatalf("kernel table: %+v", tb)
	}
	if pkg.Domains[0].Name != "/RT/NAME" {
		t.Fatalf("kernel domain: %+v", pkg.Domains[0])
	}
}
