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
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/internal/testutil"
	"github.com/tim-riep/go-runtime/kernel"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/vm"
)

func xlsxFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.ModuleRoot(t), "testdata", "xlsx", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWorkbooks reads a workbook of a spreadsheet program row by row and
// cell by cell, reports type mismatches with cell and stored value, and
// rejects encrypted workbooks and bombs.
func TestWorkbooks(t *testing.T) {
	forEachDB(t, func(t *testing.T, k *kernel.Kernel) {
		ctx := context.Background()
		initialized(t, k)
		if _, err := k.Install(ctx, []*model.Package{testutil.LoadDir(t, filepath.Join(testutil.ModuleRoot(t), "testdata", "lang", "xlsx", "TRXLSX"))}, kernel.Options{}); err != nil {
			t.Fatal(err)
		}
		const W = "/TRXLSX/WORKBOOKS"
		prices := xlsxFile(t, "numbers-prices.xlsx")
		var rows []string
		if err := json.Unmarshal([]byte(mustCall(t, k, "", W, "PRICES", b64(prices), `"Prices"`)), &rows); err != nil {
			t.Fatal(err)
		}
		// The program stored the truth value of C7 as the number 1.
		if len(rows) != 4 || rows[0] != "3 Apple: 0.10" || rows[2] != "6 Plum: 12.50" || rows[3] != "7 Text 42: 1.00" {
			t.Errorf("rows: %q", rows)
		}
		for _, c := range []struct{ sheet, cell, want string }{
			{"Prices", "E3", "0.30"}, {"Prices", "D4", "null"}, {"Prices", "A3", "mismatch Prices A3 Apple decimal"},
			{"Prices", "E8", "mismatch Prices E8 <null> decimal"}, {"None", "A1", "sheet None"}, {"Prices", "1A", "cell 1A"},
		} {
			if got := mustCall(t, k, "", W, "DECIMAL", b64(prices), js(c.sheet), js(c.cell)); got != js(c.want) {
				t.Errorf("DECIMAL(%s, %s) = %s, want %q", c.sheet, c.cell, got, c.want)
			}
		}
		if got := mustCall(t, k, "", W, "KIND", b64(prices), `"Prices"`, `"D3"`); got != `"DATE"` {
			t.Errorf("kind: %s", got)
		}
		if got := mustCall(t, k, "", W, "DECIMAL", b64(xlsxFile(t, "numbers-protected.xlsx")), `"Prices"`, `"A1"`); got != `"protected"` {
			t.Errorf("protected: %s", got)
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("xl/worksheets/sheet1.xml")
		f.Write(make([]byte, 5<<20))
		zw.Close()
		if got := mustCall(t, k, "", W, "DECIMAL", b64(buf.Bytes()), `"Sheet1"`, `"A1"`); got != `"limit"` {
			t.Errorf("bomb: %s", got)
		}
		var report string
		if err := json.Unmarshal([]byte(mustCall(t, k, "", W, "REPORT", `"1234.50"`)), &report); err != nil {
			t.Fatal(err)
		}
		data, _ := base64.StdEncoding.DecodeString(report)
		if got := mustCall(t, k, "", W, "DECIMAL", b64(data), `"Summary"`, `"B1"`); got != `"1234.50"` {
			t.Errorf("report: %s", got)
		}
		// The time limit of the execution stops a workbook being read.
		_, err := call(k, kernel.ExecOptions{Timeout: time.Nanosecond}, W, "DECIMAL", b64(prices), `"Prices"`, `"C3"`)
		var le *vm.LimitError
		if !errors.As(err, &le) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("time limit: %v", err)
		}
		results, err := k.Test(ctx, "TRXLSX", nil, kernel.ExecOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if !r.Passed {
				t.Errorf("%s.%s: %s (line %d)", r.Class, r.Method, r.Message, r.Line)
			}
		}
	})
}
