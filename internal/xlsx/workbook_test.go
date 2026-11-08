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

package xlsx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/native"
)

func testFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "xlsx", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var limits = native.DefaultLimits.Workbook

func open(t *testing.T, data []byte) *Workbook {
	t.Helper()
	w, err := Open(context.Background(), data, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func cell(t *testing.T, w *Workbook, sheet, ref string) Cell {
	t.Helper()
	c, err := w.Cell(sheet, ref)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestNumbersFile reads a workbook that a spreadsheet program wrote:
// texts, numbers, dates with times, formulas with and without stored
// values and empty cells between values.
func TestNumbersFile(t *testing.T) {
	w := open(t, testFile(t, "numbers-prices.xlsx"))
	if s := w.Sheets(); len(s) != 1 || s[0] != "Prices" || w.Date1904() {
		t.Fatalf("sheets: %v", s)
	}
	if v, ok, _ := cell(t, w, "Prices", "A3").TextValue(); !ok || v != "Apple" {
		t.Errorf("A3: %q", v)
	}
	if v, _, err := cell(t, w, "Prices", "C3").DecimalValue(15, 2); v != "0.10" || err != nil {
		t.Errorf("C3: %q %v", v, err)
	}
	if c := cell(t, w, "Prices", "D3"); c.Kind() != Date {
		t.Errorf("D3 kind %s", c.Kind())
	} else if d, _, err := c.DateValue(false); err != nil || d.Format("2006-01-02") != "2026-11-08" {
		t.Errorf("D3: %v %v", d, err)
	}
	if ts, _, err := cell(t, w, "Prices", "D6").TimestampValue(false); err != nil || ts.Format("2006-01-02 15:04") != "2026-11-09 14:30" {
		t.Errorf("D6: %v %v", ts, err)
	}
	if c := cell(t, w, "Prices", "E3"); c.Kind() != Formula || c.Formula != "B3*C3" {
		t.Errorf("E3: %+v", c)
	} else if v, _, err := c.DecimalValue(15, 2); v != "0.30" || err != nil {
		t.Errorf("E3 value: %q %v", v, err)
	}
	if c := cell(t, w, "Prices", "D4"); c.Kind() != Empty {
		t.Errorf("D4: %+v", c)
	}
	if _, _, err := cell(t, w, "Prices", "E8").IntegerValue(); err == nil || !strings.Contains(err.Error(), "no stored value") {
		t.Errorf("E8: %v", err)
	}
	if _, _, err := cell(t, w, "Prices", "A3").DecimalValue(15, 2); err == nil {
		t.Error("text as decimal")
	}
	d, err := w.Dimension("Prices")
	if err != nil || d.Ref != "A1:E8" {
		t.Errorf("dimension: %+v %v", d, err)
	}
	rows, err := w.Rows("Prices")
	if err != nil || len(rows) != 7 || rows[1].Number != 2 || rows[2].Number != 3 {
		t.Errorf("rows: %d %v", len(rows), err)
	}
}

// TestRoundTrip writes every type, saves, opens again and keeps null and
// "" apart.
func TestRoundTrip(t *testing.T) {
	w := Create(limits)
	if err := w.RenameSheet("Sheet1", "Data"); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 11, 8, 10, 11, 12, 345678000, time.UTC)
	steps := []error{
		w.SetText("Data", "A1", "text"), w.SetText("Data", "A2", ""), w.SetInteger("Data", "B1", 42), w.SetInteger("Data", "B2", 1<<60),
		w.SetDate("Data", "C1", day), w.SetTimestamp("Data", "C2", at), w.SetBool("Data", "D1", true), w.SetFormula("Data", "D2", "=B1*2"),
		w.SetText("Data", "E1", "gone"), w.Clear("Data", "E1"), w.AddSheet("Second"), w.SetBold("Data", "A1:B1"), w.FreezeRows("Data", 1),
		w.AutoFilter("Data", "A1:D2"), w.SetColumnWidth("Data", "A", 20), w.SetColumnFormat("Data", "B", "0"), w.SetNumberFormat("Data", "B1", "0.00"),
	}
	long, err := w.SetDecimal("Data", "F1", "12345678901234567.8")
	steps = append(steps, err)
	if _, err := w.SetDecimal("Data", "F2", "0.30"); err != nil {
		steps = append(steps, err)
	}
	for i, err := range steps {
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if !long {
		t.Error("long decimal not as text")
	}
	data, err := w.Save()
	if err != nil {
		t.Fatal(err)
	}
	r := open(t, data)
	if s := r.Sheets(); len(s) != 2 || s[1] != "Second" {
		t.Errorf("sheets: %v", s)
	}
	if c := cell(t, r, "Data", "A2"); c.Kind() != Text {
		t.Errorf(`"" is %s`, c.Kind())
	}
	if c := cell(t, r, "Data", "E1"); c.Kind() != Empty {
		t.Errorf("cleared cell is %s", c.Kind())
	}
	if c := cell(t, r, "Data", "Z9"); c.Kind() != Empty {
		t.Errorf("missing cell is %s", c.Kind())
	}
	if n, _, err := cell(t, r, "Data", "B2").IntegerValue(); err != nil || n != 1<<60 {
		t.Errorf("large integer: %d %v", n, err)
	}
	if d, _, err := cell(t, r, "Data", "C1").DateValue(false); err != nil || !d.Equal(day) || cell(t, r, "Data", "C1").Kind() != Date {
		t.Errorf("date: %v %v", d, err)
	}
	if ts, _, err := cell(t, r, "Data", "C2").TimestampValue(false); err != nil || !ts.Equal(at.Round(time.Millisecond)) {
		t.Errorf("timestamp: %v %v", ts, err)
	}
	if b, _, err := cell(t, r, "Data", "D1").BoolValue(false); err != nil || !b {
		t.Errorf("bool: %v %v", b, err)
	}
	if c := cell(t, r, "Data", "D2"); c.Kind() != Formula || c.Formula != "B1*2" {
		t.Errorf("formula: %+v", c)
	}
	if v, _, err := cell(t, r, "Data", "F1").TextValue(); err != nil || v != "12345678901234567.8" {
		t.Errorf("long decimal: %q %v", v, err)
	}
	if v, _, err := cell(t, r, "Data", "F2").DecimalValue(15, 2); err != nil || v != "0.30" {
		t.Errorf("decimal: %q %v", v, err)
	}
	expect := func(err error, kind ErrorKind) {
		t.Helper()
		var e *Err
		if !errors.As(err, &e) || e.Kind != kind {
			t.Errorf("expected kind %d, got %v", kind, err)
		}
	}
	_, err = r.Cell("None", "A1")
	expect(err, SheetNotFound)
	_, err = r.Cell("Data", "1A")
	expect(err, CellInvalid)
	expect(r.AddSheet("Data"), Invalid)
	expect(r.DeleteSheet("None"), SheetNotFound)
	if err := r.DeleteSheet("Second"); err != nil {
		t.Error(err)
	}
	expect(r.DeleteSheet("Data"), Invalid)
	if n, _ := ColumnNumber("c"); n != 3 {
		t.Error("column C")
	}
	if s, _ := ColumnName(28); s != "AB" {
		t.Error("column 28")
	}
}

// TestRejected rejects encrypted workbooks and bombs, reads the 1904
// date system and removes macros when saving.
func TestRejected(t *testing.T) {
	ctx := context.Background()
	_, err := Open(ctx, testFile(t, "numbers-protected.xlsx"), limits)
	var e *Err
	if !errors.As(err, &e) || e.Kind != Protected {
		t.Errorf("protected: %v", err)
	}
	if _, err := Open(ctx, []byte("no workbook"), limits); !errors.As(err, &e) || e.Kind != Invalid {
		t.Errorf("invalid: %v", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("xl/worksheets/sheet1.xml")
	f.Write(make([]byte, 5<<20))
	zw.Close()
	if _, err := Open(ctx, buf.Bytes(), limits); !errors.As(err, &e) || e.Kind != LimitExceeded {
		t.Errorf("bomb: %v", err)
	}
	w := open(t, testFile(t, "date1904.xlsx"))
	if !w.Date1904() {
		t.Error("1904 system")
	}
	if d, _, err := cell(t, w, "Dates", "A2").DateValue(true); err != nil || d.Format("2006-01-02") != "2026-11-08" {
		t.Errorf("1904 date: %v %v", d, err)
	}
	m := open(t, testFile(t, "macro.xlsm"))
	if v, _, _ := cell(t, m, "Data", "A2").TextValue(); v != "a" {
		t.Errorf("xlsm: %q", v)
	}
	saved, err := m.Save()
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(saved), int64(len(saved)))
	for _, f := range zr.File {
		if strings.Contains(f.Name, "vbaProject") {
			t.Errorf("macro kept: %s", f.Name)
		}
		if f.Name == "[Content_Types].xml" {
			r, _ := f.Open()
			b := new(bytes.Buffer)
			b.ReadFrom(r)
			if strings.Contains(b.String(), "macroEnabled") || strings.Contains(b.String(), "vbaProject") {
				t.Errorf("content types: %s", b)
			}
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Open(cctx, testFile(t, "numbers-prices.xlsx"), limits); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}
