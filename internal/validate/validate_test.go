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

package validate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

type examples struct{ base, fin, otto *model.Package }

func load(t *testing.T) examples {
	return examples{testutil.Example(t, "TRBASE"), testutil.Example(t, "TRDEMOFIN"), testutil.Example(t, "TRDEMOOTTO")}
}

func (e examples) schema(t *testing.T) *model.Schema {
	return testutil.AddPackages(t, testutil.KernelSchema(t), e.base, e.fin, e.otto)
}

func expect(t *testing.T, s *model.Schema, code rterr.Code, contains string) {
	t.Helper()
	err := Schema(s, Options{})
	for _, v := range rterr.All[*rterr.ValidationError](err) {
		if v.Code == code && strings.Contains(v.Error(), contains) {
			return
		}
	}
	t.Fatalf("expected %s error containing %q, got:\n%v", code, contains, err)
}

func TestKernelSchemaIsValid(t *testing.T) {
	if err := Schema(testutil.KernelSchema(t), Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestExamplesAreValid(t *testing.T) {
	s := load(t).schema(t)
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	if def := s.Tables["/TRDEMOFIN/COMPANIES"].Field("STATUS").Default; def != model.ValueDefault("ACTIVE") {
		t.Fatalf("default not normalized: %v", def)
	}
	up := testutil.AddPackages(t, testutil.KernelSchema(t), load(t).base, load(t).fin, testutil.Example(t, "upgrade/TRDEMOOTTO"))
	if err := Schema(up, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestPhysicalTenantLayout(t *testing.T) {
	s := testutil.AddPackages(t, testutil.KernelSchema(t), load(t).base, load(t).fin, testutil.Example(t, "upgrade/TRDEMOOTTO"))
	if err := Schema(s, Options{}); err != nil {
		t.Fatal(err)
	}
	p := s.Physical()
	c := p.Tables["/TRDEMOFIN/COMPANIES"]
	if c.Fields[0].Name != model.TenantField || c.PrimaryKey[0] != model.TenantField || c.PrimaryKey[1] != "COMPANY_ID" {
		t.Fatalf("tenant field missing: %+v", c.PrimaryKey)
	}
	if c.Field("/TRDEMOOTTO/OTTO_ID") == nil {
		t.Fatal("extension field missing")
	}
	country := c.ForeignKey("COUNTRY")
	if len(country.Columns) != 1 {
		t.Errorf("foreign key to a shared table must not contain the tenant field: %+v", country.Columns)
	}
	shop := c.ForeignKey("/TRDEMOOTTO/MAIN_SHOP")
	if len(shop.Columns) != 2 || shop.Columns[0].Field != model.TenantField {
		t.Errorf("extension key between tenant tables must contain the tenant field: %+v", shop.Columns)
	}
	if tk := c.ForeignKey(model.TenantForeignKey); tk == nil || tk.Target != model.TenantTable {
		t.Errorf("tenant foreign key missing")
	}
	if p.Tables["/TRDEMOFIN/COUNTRIES"].Field(model.TenantField) != nil {
		t.Error("shared table has a tenant field")
	}
}

func TestSystemClassReserved(t *testing.T) {
	e := load(t)
	tableOf(e.fin, "/TRDEMOFIN/COUNTRIES").DataClass = model.System
	expect(t, e.schema(t), rterr.CodeDataClass, "reserved for the kernel")
}

func TestKernelTablesAreSystem(t *testing.T) {
	s := testutil.KernelSchema(t)
	for _, tb := range s.Tables {
		// The activation of service nodes, services and implementations, the
		// settings of nodes of the kernel, the role assignments and the
		// intervals of number ranges are maintained during operation.
		runtime := tb.Name == "/RT/HTTP_SERVICE_STATES" || tb.Name == "/RT/SERVICE_STATES" || tb.Name == "/RT/KERNEL_NODE_SETTINGS" || tb.Name == "/RT/ROLE_ASSIGNMENTS" ||
			tb.Name == "/RT/IMPLEMENTATION_STATES" || tb.Name == "/RT/IMPLEMENTATION_TENANT_STATES" || tb.Name == "/RT/NUMBER_RANGE_INTERVALS"
		if tb.DataClass != model.System && !(runtime && tb.DataClass == model.RuntimeConfiguration) {
			t.Errorf("%s has class %q", tb.Name, tb.DataClass)
		}
	}
	s.Tables[model.TenantTable].DataClass = model.Configuration
	expect(t, s, rterr.CodeDataClass, "kernel tables")
}

func TestUnknownDomain(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].Fields[2].Domain = "NOPE" // /TRDEMOFIN/BANK_ACCOUNTS.IBAN
	expect(t, e.schema(t), rterr.CodeUnknownDomain, "NOPE")
}

func TestDomainNotVisible(t *testing.T) {
	e := load(t)
	tableOf(e.fin, "/TRDEMOFIN/COUNTRIES").Fields[1].DataElement = model.None[string]() // /TRDEMOFIN/COUNTRIES.NAME
	tableOf(e.fin, "/TRDEMOFIN/COUNTRIES").Fields[1].Domain = "/TRDEMOOTTO/OTTO_ID"     // TRDEMOFIN does not require TRDEMOOTTO
	expect(t, e.schema(t), rterr.CodeNotVisible, "OTTO_ID")
}

func TestErrorLocation(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].Fields[1].Domain = "NOPE" // /TRDEMOFIN/BANK_ACCOUNTS.ACCOUNT_NO
	err := Schema(e.schema(t), Options{})
	var v *rterr.ValidationError
	if !errors.As(err, &v) || v.Location.Package != "TRDEMOFIN" || v.Location.File != "schema/tables/BANK_ACCOUNTS.json" || v.Location.Pointer != "/fields/1/domain" {
		t.Fatalf("location: %+v", v)
	}
}

func TestSharedTableCannotReferenceTenantTable(t *testing.T) {
	e := load(t)
	countries := tableOf(e.fin, "/TRDEMOFIN/COUNTRIES")
	countries.Fields = append(countries.Fields, model.Field{Name: "OWNER", LocalName: "OWNER", Domain: "/TRDEMOFIN/COMPANY_ID", Origin: model.OriginTable})
	countries.ForeignKeys = append(countries.ForeignKeys, model.ForeignKey{Name: "OWNER", Target: "/TRDEMOFIN/COMPANIES", Origin: model.OriginTable,
		Columns: []model.ColumnPair{{Field: "OWNER", References: "COMPANY_ID"}}})
	expect(t, e.schema(t), rterr.CodeTenant, "/TRDEMOFIN/COMPANIES")
}

func TestForeignKeyMustCoverKey(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].ForeignKeys[0].Columns = nil // /TRDEMOFIN/BANK_ACCOUNTS.COMPANY
	expect(t, e.schema(t), rterr.CodeForeignKey, "COMPANY_ID")
}

func TestForeignKeyTypeMismatch(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].Fields[0].DataElement = model.None[string]()
	e.fin.Tables[0].Fields[0].Domain = "/TRDEMOFIN/IBAN" // CHAR(34) referencing CHAR(10)
	expect(t, e.schema(t), rterr.CodeTypeMismatch, "CHAR(34)")
}

func TestExtensionFieldNeedsDefault(t *testing.T) {
	e := load(t)
	e.otto.Extensions[0].Fields[0].NotNull = true
	expect(t, e.schema(t), rterr.CodeExtension, "nullable")
}

func TestExtensionOfRepositoryTable(t *testing.T) {
	e := load(t)
	e.otto.Extensions[0].Table = "/RT/TABLES"
	expect(t, e.schema(t), rterr.CodeReservedObject, "cannot be extended")
}

func TestExtensionOfTenantTableAllowed(t *testing.T) {
	e := load(t)
	e.otto.Extensions[0].Table = model.TenantTable
	// The field moves away from the companies, which TRDEMOOTTO extends.
	delete(e.otto.BusinessObjectExtensions[0].Entities[0].Fields, "/TRDEMOOTTO/OTTO_ID")
	if err := Schema(e.schema(t), Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionNotRequired(t *testing.T) {
	e := load(t)
	e.otto.Manifest.Requires = []model.Dependency{{ID: "TRBASE"}}
	e.otto.Domains[0].Name = "/TRDEMOOTTO/OTTO_ID"
	expect(t, e.schema(t), rterr.CodeNotVisible, "does not require")
}

func TestReferenceRepositoryTable(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].ForeignKeys[0].Target = "/RT/PACKAGES"
	expect(t, e.schema(t), rterr.CodeReservedObject, "cannot be referenced")
}

func TestNameTooLong(t *testing.T) {
	e := load(t)
	long := strings.Repeat("X", 60)
	e.otto.Extensions[0].Fields[0].Name = model.Qualify("TRDEMOOTTO", long)
	expect(t, e.schema(t), rterr.CodeNameTooLong, long)
}

func TestDefaultNotFixedValue(t *testing.T) {
	e := load(t)
	e.fin.Tables[1].Fields[3].DefaultJSON = json.RawMessage(`"GONE"`) // /TRDEMOFIN/COMPANIES.STATUS
	expect(t, e.schema(t), rterr.CodeDefault, "GONE")
}

func TestInvalidDefault(t *testing.T) {
	e := load(t)
	e.fin.Tables[0].Fields[3].DefaultJSON = json.RawMessage(`"1.234"`) // /TRDEMOFIN/BANK_ACCOUNTS.BALANCE DEC(15,2)
	expect(t, e.schema(t), rterr.CodeDefault, "decimal places")
}

func TestDecimalPrecisionLimit(t *testing.T) {
	e := load(t)
	e.base.Domains[0].Length = 19 // AMOUNT
	expect(t, e.schema(t), rterr.CodeTypeParams, "between 1 and 18")
}

func TestFixedValuesOnlyForSomeTypes(t *testing.T) {
	e := load(t)
	for i := range e.base.Domains {
		if e.base.Domains[i].Name == "/TRBASE/FLAG" {
			e.base.Domains[i].Values = []model.FixedValue{{Value: "true"}}
		}
	}
	expect(t, e.schema(t), rterr.CodeFixedValue, "only allowed")
}

func TestPrimaryKeyFieldMissing(t *testing.T) {
	e := load(t)
	tableOf(e.fin, "/TRDEMOFIN/COUNTRIES").PrimaryKey = []string{"NOPE"}
	expect(t, e.schema(t), rterr.CodePrimaryKey, "NOPE")
}

func TestNameConflict(t *testing.T) {
	e := load(t)
	s := e.schema(t)
	other := &model.Package{Manifest: model.Manifest{ID: "TRX", Version: "1.0.0"}, Tables: []model.Table{{Name: "/TRDEMOFIN/COMPANIES"}}}
	var conflict *rterr.NameConflict
	if err := s.AddPackage(other, testutil.Epoch, testutil.Epoch); !errors.As(err, &conflict) || conflict.Owner != "TRDEMOFIN" {
		t.Fatalf("got %v", err)
	}
}
