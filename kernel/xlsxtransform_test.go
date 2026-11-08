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

package kernel_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/internal/xlsx"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

const sheets = "/TRXLSX/SHEETS"

// items are values of all types: nulls and empty texts, a whole number
// beyond 2^53, a decimal with more digits than numbers hold, both kinds
// of truth values and times with milliseconds.
const items = `[
 {"ITEM_ID":"A1","AMOUNT":"-0.01","STATUS":"CLOSED","BIG":9007199254740993,"DAY":"2000-02-29","AT":"2026-11-08T10:30:15.123Z","FLAG":true,
  "NOTE":"","COMMENT":"x\u0001y _x0041_","RATE":"12345678.0123456789","COUNT":-2147483648,"REF":"0190a3b4-5c6d-7e8f-9a0b-1c2d3e4f5a6b","CODE":""},
 {"ITEM_ID":"B2","AMOUNT":"25000.50","STATUS":"ACTIVE","BIG":-42,"DAY":"9999-12-31","AT":"1904-01-01T00:00:00Z","FLAG":false,
  "NOTE":"  spaces  ","COMMENT":null,"RATE":"0.0000000001","COUNT":0,"REF":null,"CODE":"ABC"},
 {"ITEM_ID":"C3","AMOUNT":null,"STATUS":"ACTIVE","BIG":null,"DAY":null,"AT":null,"FLAG":null,"NOTE":null,"COMMENT":null,"RATE":null,"COUNT":null,"REF":null,"CODE":null}
]`

func installXLSX(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	initialized(t, k)
	if _, err := k.Install(context.Background(), []*model.Package{testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "xlsx", "TRXLSX"))}, kernel.Options{}); err != nil {
		t.Fatal(err)
	}
}

// normalized parses JSON so that equal values compare equal.
func normalized(t *testing.T, s string) any {
	t.Helper()
	var v any
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	return v
}

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	var text string
	if err := json.Unmarshal([]byte(s), &text); err != nil {
		t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// headings reads the cells of the first row of a sheet.
func headings(t *testing.T, data []byte) []string {
	t.Helper()
	r, err := xlsx.NewReader(context.Background(), data, native.DefaultLimits.Workbook)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	r.Rows("", func(n int, cells []xlsx.Cell) error {
		if n == 1 {
			for _, c := range cells {
				out = append(out, c.Raw)
			}
		}
		return nil
	})
	return out
}

// TestXLSXTransform writes arrays as sheets and reads them back in both
// date systems with headings by label in two languages and by name.
func TestXLSXTransform(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		installXLSX(t, k)
		want := normalized(t, items)
		for _, system := range []string{"1900", "1904"} {
			for _, header := range []string{"LABEL", "NAME"} {
				for _, lang := range []string{"en", "de"} {
					out, err := call(k, kernel.ExecOptions{Language: lang}, sheets, "WRITE", items, js(header), js(system))
					if err != nil {
						t.Fatalf("%s %s %s: %v", system, header, lang, err)
					}
					data := unb64(t, out)
					heads := strings.Join(headings(t, data), "|")
					wantHeads := map[string]string{
						"LABEL en": "Item|Amount|Status|Big number|Day|Time|Flag|NOTE|COMMENT|Rate|Count|Reference|CODE",
						"LABEL de": "Artikel|Betrag|Status|Große Zahl|Tag|Zeitpunkt|Kennzeichen|Notiz|Kommentar|Satz|Anzahl|Referenz|CODE",
					}[header+" "+lang]
					if wantHeads == "" {
						wantHeads = "ITEM_ID|AMOUNT|STATUS|BIG|DAY|AT|FLAG|NOTE|COMMENT|RATE|COUNT|REF|CODE"
					}
					if heads != wantHeads {
						t.Errorf("%s %s: headings %s", header, lang, heads)
					}
					back := mustCall(t, k, "", sheets, "READ", js(base64.StdEncoding.EncodeToString(data)), "null", "1", `"NO"`)
					if got := normalized(t, back); !reflect.DeepEqual(got, want) {
						t.Errorf("%s %s %s: read back\n%s\nwant\n%s", system, header, lang, back, items)
					}
				}
			}
		}
		if got := mustCall(t, k, "", sheets, "WRITE_STRICT", items); !strings.Contains(got, "more than 15 significant digits") || !strings.Contains(got, "RATE") {
			t.Errorf("long decimals as error: %s", got)
		}
		if got := mustCall(t, k, "", sheets, "WRITE_STRICT", "[]"); got != `"ok"` {
			t.Errorf("no rows: %s", got)
		}
	})
}

// sheetOf builds a workbook with a sheet Data from rows of cells: strings
// are text, float64 numbers, bool truth values, nil no cell.
func sheetOf(t *testing.T, rows ...[]any) string {
	t.Helper()
	w := xlsx.Create(native.DefaultLimits.Workbook)
	defer w.Close()
	if err := w.RenameSheet("Sheet1", "Data"); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		for j, v := range row {
			col, _ := xlsx.ColumnName(j + 1)
			ref := col + itoa(i+1)
			var err error
			switch v := v.(type) {
			case string:
				err = w.SetText("Data", ref, v)
			case float64:
				_, err = w.SetDecimal("Data", ref, strings.TrimRight(strings.TrimRight(jsNum(v), "0"), "."))
			case bool:
				err = w.SetBool("Data", ref, v)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := w.Save()
	if err != nil {
		t.Fatal(err)
	}
	return js(base64.StdEncoding.EncodeToString(data))
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func jsNum(f float64) string { b, _ := json.Marshal(f); return string(b) + ".0" }

// TestXLSXHeadings reads sheets of other programs: headings in any case
// and language below a title, columns in another order, unknown and
// ambiguous columns, defaults for missing columns and empty rows.
func TestXLSXHeadings(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		installXLSX(t, k)
		data := sheetOf(t,
			[]any{"Price list"},
			nil,
			[]any{"  artikel ", "Extra", "amount", "flag", "Remark", "Satz", "Tag"},
			[]any{"X1", "junk", 12.5, "TRUE", "ambiguous", 1.5, "2026-11-08"},
			nil,
			[]any{nil, "only junk"},
			[]any{"X2", nil, "7", true, nil, nil, 46334.0},
		)
		got := mustCall(t, k, "", sheets, "READ", data, `"Data"`, "3", `"YES"`)
		want := `[{"ITEM_ID":"X1","AMOUNT":"12.50","STATUS":"ACTIVE","BIG":null,"DAY":"2026-11-08","AT":null,"FLAG":true,"NOTE":null,"COMMENT":null,"RATE":"1.5000000000","COUNT":null,"REF":null,"CODE":null},
			{"ITEM_ID":"X2","AMOUNT":"7.00","STATUS":"ACTIVE","BIG":null,"DAY":"2026-11-08","AT":null,"FLAG":true,"NOTE":null,"COMMENT":null,"RATE":null,"COUNT":null,"REF":null,"CODE":null}]`
		if !reflect.DeepEqual(normalized(t, got), normalized(t, want)) {
			t.Errorf("read %s", got)
		}
		// Truth values as text only with BOOL_TEXT.
		if _, err := call(k, kernel.ExecOptions{}, sheets, "READ", data, `"Data"`, "3", `"NO"`); exceptionClass(err) != "/TRBASE/XLSX_READ_FAILED" {
			t.Errorf("bool text: %v", err)
		}
		if _, err := call(k, kernel.ExecOptions{}, sheets, "READ", data, `"Other"`, "1", `"NO"`); exceptionClass(err) != "/TRBASE/XLSX_SHEET_NOT_FOUND" {
			t.Errorf("sheet: %v", err)
		}
		if _, err := call(k, kernel.ExecOptions{}, sheets, "READ", b64(xlsxFile(t, "numbers-protected.xlsx")), "null", "1", `"NO"`); exceptionClass(err) != "/TRBASE/XLSX_PROTECTED" {
			t.Errorf("protected: %v", err)
		}
		if _, err := call(k, kernel.ExecOptions{}, sheets, "READ", data, "null", "0", `"NO"`); exceptionClass(err) != "/TRBASE/XLSX_INVALID" {
			t.Errorf("first row 0: %v", err)
		}
	})
}

// TestXLSXViolations reports all values that do not fit together, with
// row, column, heading, component and the stored value.
func TestXLSXViolations(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		installXLSX(t, k)
		violations := func(data string, max int) []string {
			t.Helper()
			var out []string
			if err := json.Unmarshal([]byte(mustCall(t, k, "", sheets, "VIOLATIONS", data, itoa(max))), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		expect := func(name string, got []string, want ...string) {
			t.Helper()
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s:\n%s\nwant\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		}
		expect("missing column", violations(sheetOf(t, []any{"Amount"}, []any{1.0}), 100),
			"row 1, column  (ITEM_ID): the sheet has no column for ITEM_ID, which needs a value",
			"1  Item ITEM_ID <null>: the sheet has no column for ITEM_ID, which needs a value")
		expect("two columns", violations(sheetOf(t, []any{"Item", "Artikel", "ITEM_ID"}), 100),
			"2 values of the sheet do not fit the components of /TRXLSX/ITEM",
			"1 B Artikel ITEM_ID Artikel: the column B is the second one for ITEM_ID, after column A (Item)",
			"1 C ITEM_ID ITEM_ID ITEM_ID: the column C is the second one for ITEM_ID, after column A (Item)")
		expect("empty", violations(sheetOf(t), 100),
			"row 1, column  (): the sheet has no heading row 1",
			"1    <null>: the sheet has no heading row 1")
		values := sheetOf(t,
			[]any{"Item", "Amount", "Status", "Count", "Day", "Reference", "CODE", "Flag", "Big number", "Time"},
			[]any{"", "abc", "OPEN", 3000000000.0, "2026-13-01", "nope", "TOOLONG", "yes", 1.5, true},
			[]any{nil, 1.005, "ACTIVE"},
			[]any{"OK", 2.0},
		)
		all := violations(values, 100)
		expect("values", all,
			"12 values of the sheet do not fit the components of /TRXLSX/ITEM",
			`2 A Item ITEM_ID : /TRXLSX/ITEM_ID does not take the empty text`,
			`2 B Amount AMOUNT abc: it is no decimal: "abc" is no number`,
			`2 C Status STATUS OPEN: it is no fixed value of /TRXLSX/STATUS (ACTIVE, CLOSED)`,
			`2 D Count COUNT 3000000000: 3000000000 is out of the range of INT4`,
			`2 E Day DAY 2026-13-01: it is no date: the text is no date YYYY-MM-DD`,
			`2 F Reference REF nope: it is no UUID`,
			`2 G CODE CODE TOOLONG: it has 7 characters, more than 3`,
			`2 H Flag FLAG yes: it is no truth value: the cell holds no truth value`,
			`2 I Big number BIG 1.5: it is no integer: it has 1 decimal places, more than 0`,
			`2 J Time AT 1: it is no timestamp: the cell holds a truth value`,
			`3 A Item ITEM_ID <null>: ITEM_ID needs a value`,
			`3 B Amount AMOUNT 1.005: it is no decimal: it has 3 decimal places, more than 2`,
		)
		few := violations(values, 3)
		if len(few) != 4 || !strings.HasSuffix(few[0], "; reading stopped after 3 violations") || few[3] != all[3] {
			t.Errorf("max 3: %q", few)
		}
	})
}
