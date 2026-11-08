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

package natives

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// componentHost gives the components of one structure.
type componentHost struct {
	native.Host
	comps []native.Component
}

func (h componentHost) Components(string) ([]native.Component, error) { return h.comps, nil }
func (h componentHost) Language() string                              { return model.DefaultLanguage }

var benchComps = []native.Component{
	{Name: "ID", Domain: &model.Domain{Name: "/T/ID", Type: model.Char, Length: 10}, NotNull: true, Labels: map[string]string{"en": "Id"}},
	{Name: "NAME", Domain: &model.Domain{Name: "/T/NAME", Type: model.String}, Labels: map[string]string{"en": "Name"}},
	{Name: "AMOUNT", Domain: &model.Domain{Name: "/T/AMOUNT", Type: model.Dec, Length: 15, Decimals: 2}, Labels: map[string]string{"en": "Amount"}},
	{Name: "COUNT", Domain: &model.Domain{Name: "/T/COUNT", Type: model.Int8}},
	{Name: "DAY", Domain: &model.Domain{Name: "/T/DAY", Type: model.Date}},
	{Name: "PAID", Domain: &model.Domain{Name: "/T/PAID", Type: model.Bool}},
}

func benchTable(n int) *native.Table {
	t := &native.Table{Struct: "/T/ROW", Fields: []string{"ID", "NAME", "AMOUNT", "COUNT", "DAY", "PAID"}}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		t.Rows = append(t.Rows, []any{"R" + strconv.Itoa(i), "Customer " + strconv.Itoa(i%977), strconv.Itoa(i) + ".25", int64(i), day.AddDate(0, 0, i%365), i%2 == 0})
	}
	return t
}

// BenchmarkXLSX writes and reads 200 000 rows; the allocations of reading
// beyond the result grow with the shared strings only, since the sheet is
// read as a stream.
func BenchmarkXLSX(b *testing.B) {
	const rows = 200_000
	c := &native.Context{Context: context.Background(), Host: componentHost{comps: benchComps}}
	src := benchTable(rows)
	into := &native.Table{Struct: src.Struct, Fields: src.Fields}
	out, err := transformXLSX(c, native.TransformInput{Name: "/TRBASE/XLSX", Params: map[string]string{}, Rows: src})
	if err != nil {
		b.Fatal(err)
	}
	b.Run("write", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := transformXLSX(c, native.TransformInput{Name: "/TRBASE/XLSX", Params: map[string]string{}, Rows: src}); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(out.Bytes)), "bytes/workbook")
	})
	b.Run("read", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			got, err := transformXLSX(c, native.TransformInput{Name: "/TRBASE/XLSX", Params: map[string]string{}, Bytes: out.Bytes, Into: into})
			if err != nil {
				b.Fatal(err)
			}
			if len(got.Rows) != rows {
				b.Fatalf("%d rows", len(got.Rows))
			}
		}
	})
}

// TestXLSXRows reads back what it writes, as the benchmark does.
func TestXLSXRows(t *testing.T) {
	c := &native.Context{Context: context.Background(), Host: componentHost{comps: benchComps}}
	src := benchTable(3)
	out, err := transformXLSX(c, native.TransformInput{Name: "/TRBASE/XLSX", Params: map[string]string{}, Rows: src})
	if err != nil {
		t.Fatal(err)
	}
	got, err := transformXLSX(c, native.TransformInput{Name: "/TRBASE/XLSX", Params: map[string]string{}, Bytes: out.Bytes, Into: &native.Table{Struct: src.Struct, Fields: src.Fields}})
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range got.Rows {
		for j, v := range row {
			want := src.Rows[i][j]
			if tm, ok := want.(time.Time); ok {
				if !tm.Equal(v.(time.Time)) {
					t.Errorf("row %d field %d: %v, want %v", i, j, v, want)
				}
				continue
			}
			if v != want {
				t.Errorf("row %d field %d: %#v, want %#v", i, j, v, want)
			}
		}
	}
}
