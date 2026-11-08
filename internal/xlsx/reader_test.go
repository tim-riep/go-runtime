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
	"strings"
	"testing"
	"time"
)

func reader(t *testing.T, data []byte) *Reader {
	t.Helper()
	r, err := NewReader(context.Background(), data, limits)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type readRow struct {
	n     int
	cells []Cell
}

func readAll(t *testing.T, r *Reader, sheet string) []readRow {
	t.Helper()
	var out []readRow
	if err := r.Rows(sheet, func(n int, cells []Cell) error {
		out = append(out, readRow{n, append([]Cell(nil), cells...)})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestReaderMatchesWorkbook reads the files of other programs as a stream
// and cell by cell; both give every cell the same kind, value and date
// format.
func TestReaderMatchesWorkbook(t *testing.T) {
	for _, name := range []string{"numbers-prices.xlsx", "date1904.xlsx", "macro.xlsm"} {
		data := testFile(t, name)
		w, r := open(t, data), reader(t, data)
		if r.Date1904() != w.Date1904() || strings.Join(r.Sheets(), ",") != strings.Join(w.Sheets(), ",") {
			t.Fatalf("%s: %v %v, %v %v", name, r.Date1904(), r.Sheets(), w.Date1904(), w.Sheets())
		}
		for _, sheet := range w.Sheets() {
			rows, err := w.Rows(sheet)
			if err != nil {
				t.Fatal(err)
			}
			got := readAll(t, r, sheet)
			if len(got) != len(rows) {
				t.Fatalf("%s %s: %d rows, want %d", name, sheet, len(got), len(rows))
			}
			for i, row := range rows {
				if got[i].n != row.Number {
					t.Errorf("%s: row %d is %d", name, row.Number, got[i].n)
				}
				for j, want := range row.Cells {
					var c Cell
					if j < len(got[i].cells) {
						c = got[i].cells[j]
					}
					wt, _, _ := want.TextValue()
					ct, _, _ := c.TextValue()
					if c.Kind() != want.Kind() || ct != wt || c.HasValue != want.HasValue {
						t.Errorf("%s %s row %d column %d: %+v, want %+v", name, sheet, row.Number, j+1, c, want)
					}
				}
			}
		}
	}
}

// TestWriterRoundTrip writes every kind of value and reads it back with
// the reader and with the workbook.
func TestWriterRoundTrip(t *testing.T) {
	ts := time.Date(2026, 11, 8, 10, 30, 15, 250e6, time.UTC)
	texts := []string{"", " padded ", "a\x01b\x1fc", "_x0041_ stays", "line\nbreak\r\n", "<&>\"'", "Grüße 漢字 🙂", "_x005F_"}
	for _, d1904 := range []bool{false, true} {
		first := time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)
		if d1904 {
			first = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		cols := []Column{{Header: "Text", Width: 20}, {Header: "Number", Format: "0.00", Width: 10}, {Header: "Integer", Format: "0"},
			{Header: "Bool"}, {Header: "Date", Format: DateFormat}, {Header: "Time", Format: TimestampFormat}}
		w, err := NewWriter("Data's", d1904, cols, limits)
		if err != nil {
			t.Fatal(err)
		}
		for i, s := range texts {
			var date any
			if i == 0 {
				date = first
			}
			if err := w.Row([]any{s, Numeric("-12345.67"), int64(9007199254740992), i%2 == 0, date, ts}); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Row([]any{nil, nil, nil, nil, nil, nil}); err != nil {
			t.Fatal(err)
		}
		data, err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
		r := reader(t, data)
		if r.Date1904() != d1904 || r.Sheets()[0] != "Data's" {
			t.Fatalf("%v %v", r.Date1904(), r.Sheets())
		}
		rows := readAll(t, r, "")
		if len(rows) != 1+len(texts) || rows[0].cells[1].Raw != "Number" {
			t.Fatalf("%d rows: %+v", len(rows), rows[0])
		}
		for i, s := range texts {
			c := rows[i+1].cells
			if v, ok, err := c[0].TextValue(); err != nil || !ok || v != s {
				t.Errorf("text %q: %q %v %v", s, v, ok, err)
			}
			if v, _, err := c[1].DecimalValue(9, 2); err != nil || v != "-12345.67" {
				t.Errorf("number: %q %v", v, err)
			}
			if v, _, err := c[2].IntegerValue(); err != nil || v != 9007199254740992 {
				t.Errorf("integer: %d %v", v, err)
			}
			if v, _, err := c[3].BoolValue(false); err != nil || v != (i%2 == 0) {
				t.Errorf("bool: %v %v", v, err)
			}
			if v, _, err := c[5].TimestampValue(d1904); err != nil || !v.Equal(ts) || c[5].Kind() != Date {
				t.Errorf("timestamp: %v %v %v", v, err, c[5].Kind())
			}
		}
		if v, ok, err := rows[1].cells[4].DateValue(d1904); err != nil || !ok || !v.Equal(first) {
			t.Errorf("date: %v %v %v", v, ok, err)
		}
		if rows[2].cells[4].HasValue {
			t.Errorf("a missing date has a value: %+v", rows[2].cells[4])
		}
		// The library of the workbook reads the same file.
		wb := open(t, data)
		if c := cell(t, wb, "Data's", "A3"); c.Raw != " padded " {
			t.Errorf("workbook: %+v", c)
		}
		if c := cell(t, wb, "Data's", "A4"); c.Raw != "a\x01b\x1fc" {
			t.Errorf("workbook control characters: %q", c.Raw)
		}
		if c := cell(t, wb, "Data's", "A2"); c.Kind() != Text || c.Raw != "" {
			t.Errorf("workbook empty text: %+v", c)
		}
		if c := cell(t, wb, "Data's", "E3"); c.Kind() != Empty {
			t.Errorf("workbook null: %+v", c)
		}
		if c := cell(t, wb, "Data's", "F2"); c.Kind() != Date {
			t.Errorf("workbook timestamp: %+v", c)
		}
	}
}

func TestWriterErrors(t *testing.T) {
	for _, name := range []string{"", "a:b", "'x", strings.Repeat("x", 32)} {
		if _, err := NewWriter(name, false, nil, limits); kind(err) != Invalid {
			t.Errorf("sheet %q: %v", name, err)
		}
	}
	w, err := NewWriter("S", false, []Column{{Header: "A"}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Row([]any{strings.Repeat("x", MaxText+1)}); kind(err) != Invalid {
		t.Errorf("long text: %v", err)
	}
	w, _ = NewWriter("S", true, []Column{{Header: "A"}}, limits)
	if err := w.Row([]any{time.Date(1903, 12, 31, 0, 0, 0, 0, time.UTC)}); kind(err) != Invalid {
		t.Errorf("date before 1904: %v", err)
	}
	if err := w.Row([]any{time.Date(9999, 12, 31, 23, 59, 59, 999e6, time.UTC)}); err != nil {
		t.Errorf("last date: %v", err)
	}
	l := limits
	l.Rows = 2
	w, _ = NewWriter("S", false, []Column{{Header: "A"}}, l)
	if err := w.Row([]any{"1"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Row([]any{"2"}); kind(err) != LimitExceeded {
		t.Errorf("rows: %v", err)
	}
}

func kind(err error) ErrorKind {
	var e *Err
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

// zipOf builds a workbook from parts.
func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range parts {
		f, _ := zw.Create(name)
		f.Write([]byte(body))
	}
	zw.Close()
	return b.Bytes()
}

// minimal is a workbook as other programs write it: shared strings with
// rich text and phonetic hints, cells and rows without references, a
// shared formula and an inline string.
func minimal(sheet string) map[string]string {
	return map[string]string{
		"_rels/.rels": `<Relationships xmlns="` + nsPkg + `"><Relationship Id="r" Type="` + relType + `officeDocument" Target="/xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `"><workbookPr date1904="true"/><sheets>` +
			`<sheet name="One" sheetId="1" r:id="rS"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="` + nsPkg + `"><Relationship Id="rS" Type="` + relType + `worksheet" Target="sheets/s.xml"/>` +
			`<Relationship Id="rT" Type="` + relType + `sharedStrings" Target="strings.xml"/>` +
			`<Relationship Id="rX" Type="` + relType + `hyperlink" Target="http://example.com/" TargetMode="External"/></Relationships>`,
		"xl/strings.xml": `<sst xmlns="` + nsMain + `"><si><t>plain</t></si><si><r><t>ri</t></r><r><rPr><b/></rPr><t>ch</t></r><rPh><t>hint</t></rPh></si>` +
			`<si><t>a_x000D_b</t></si></sst>`,
		"xl/sheets/s.xml": `<worksheet xmlns="` + nsMain + `"><sheetData>` + sheet + `</sheetData></worksheet>`,
	}
}

func TestReaderForms(t *testing.T) {
	data := zipOf(t, minimal(`<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>`+
		`<row r="4"><c r="B4"><f t="shared" ref="B4:B5" si="0">A1*2</f><v>2</v></c><c t="inlineStr"><is><r><t>in</t></r><r><t>line</t></r></is></c></row>`+
		`<row><c r="B5"><f t="shared" si="0"/><v>4</v></c><c r="D5" t="s"><v>2</v></c><c r="E5" t="e"><v>#DIV/0!</v></c></row>`))
	r := reader(t, data)
	if !r.Date1904() {
		t.Error("date1904=true")
	}
	rows := readAll(t, r, "One")
	if len(rows) != 3 || rows[0].n != 1 || rows[1].n != 4 || rows[2].n != 5 {
		t.Fatalf("%+v", rows)
	}
	if rows[0].cells[0].Raw != "plain" || rows[0].cells[1].Raw != "rich" {
		t.Errorf("shared strings: %+v", rows[0].cells)
	}
	if c := rows[1].cells; c[0].HasValue || c[1].Formula != "A1*2" || c[1].Raw != "2" || c[2].Raw != "inline" {
		t.Errorf("row 4: %+v", c)
	}
	if c := rows[2].cells; c[1].Kind() != Formula || c[1].Raw != "4" || c[3].Raw != "a\rb" || c[4].Kind() != Error || c[2].HasValue {
		t.Errorf("row 5: %+v", c)
	}
	if err := r.Rows("Two", func(int, []Cell) error { return nil }); kind(err) != SheetNotFound {
		t.Errorf("missing sheet: %v", err)
	}
	for name, sheet := range map[string]string{
		"rows out of order": `<row r="3"/><row r="2"/>`,
		"cell in other row": `<row r="1"><c r="A2"/></row>`,
		"missing string":    `<row><c t="s"><v>7</v></c></row>`,
	} {
		if err := reader(t, zipOf(t, minimal(sheet))).Rows("", func(int, []Cell) error { return nil }); kind(err) != Invalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	l := limits
	l.Columns = 2
	r, _ = NewReader(context.Background(), data, l)
	if err := r.Rows("", func(int, []Cell) error { return nil }); kind(err) != LimitExceeded {
		t.Errorf("columns: %v", err)
	}
	if _, err := NewReader(context.Background(), testFile(t, "numbers-protected.xlsx"), limits); kind(err) != Protected {
		t.Errorf("protected: %v", err)
	}
	if _, err := NewReader(context.Background(), []byte("no zip"), limits); kind(err) != Invalid {
		t.Errorf("no zip: %v", err)
	}
}
