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
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/internal/xlsx"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

const errReadFailed = "/TRBASE/XLSX_READ_FAILED"

// Parameters of the transformation /TRBASE/XLSX.
const (
	paramSheet        = "SHEET"
	paramHeader       = "HEADER"
	paramFirstRow     = "FIRST_ROW"
	paramDateSystem   = "DATE_SYSTEM"
	paramLongDecimals = "LONG_DECIMALS"
	paramBoolText     = "BOOL_TEXT"
	paramMaxErrors    = "MAX_ERRORS"
	defaultSheet      = "Sheet1"
	defaultMaxErrors  = 100
)

func init() {
	native.Default.RegisterTransform(model.TransformXLSX, native.Transform{
		Params: []native.TransformParam{
			{Name: paramSheet},
			{Name: paramHeader, Values: []string{"LABEL", "NAME"}},
			{Name: paramFirstRow},
			{Name: paramDateSystem, Values: []string{"1900", "1904"}},
			{Name: paramLongDecimals, Values: []string{"TEXT", "ERROR"}},
			{Name: paramBoolText, Values: []string{"NO", "YES"}},
			{Name: paramMaxErrors},
		},
		Shapes: []native.TransformShape{{Source: native.ShapeRows, Into: native.ShapeBytes}, {Source: native.ShapeBytes, Into: native.ShapeArrayType}},
		Raises: []string{errXLSX},
		Func:   transformXLSX,
	})
}

// xlsxViolation is a row of /TRBASE/XLSX_VIOLATION_ARRAY.
type xlsxViolation struct {
	RowNo     int32                  `rt:"ROW_NO"`
	Column    string                 `rt:"COLUMN_NAME"`
	Header    string                 `rt:"HEADER"`
	Component string                 `rt:"COMPONENT"`
	Raw       model.Optional[string] `rt:"RAW"`
	Message   string                 `rt:"MESSAGE"`
}

func transformXLSX(c *native.Context, in native.TransformInput) (native.TransformOutput, error) {
	if in.Rows != nil {
		data, err := writeXLSX(c, in)
		return native.TransformOutput{Bytes: data}, err
	}
	rows, err := readXLSX(c, in)
	return native.TransformOutput{Rows: rows}, err
}

// components returns the components of a structure for the fields in
// order; fields without a component are nil.
func components(c *native.Context, t *native.Table) ([]*native.Component, error) {
	comps, err := c.Host.Components(t.Struct)
	if err != nil {
		return nil, err
	}
	out := make([]*native.Component, len(t.Fields))
	for i, f := range t.Fields {
		for j := range comps {
			if comps[j].Name == f {
				out[i] = &comps[j]
			}
		}
	}
	return out, nil
}

func intParam(in native.TransformInput, name string, def int) (int, error) {
	s, ok := in.Params[name]
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 0, native.Raise(errXLSXInvalid, fmt.Sprintf("the parameter %s of %s must be a whole number from 1, not %q", name, in.Name, s))
	}
	return n, nil
}

// heading returns the heading of a component in a language, else in
// English, else its name.
func heading(comp *native.Component, lang string) string {
	for _, l := range []string{lang, model.DefaultLanguage} {
		if h := strings.TrimSpace(comp.Labels[l]); h != "" {
			return h
		}
	}
	return comp.Name
}

func numberFormat(d *model.Domain) string {
	switch d.Type {
	case model.Dec:
		if d.Decimals == 0 {
			return "0"
		}
		return "0." + strings.Repeat("0", d.Decimals)
	case model.Int4, model.Int8:
		return "0"
	case model.Date:
		return xlsx.DateFormat
	case model.Timestamp:
		return xlsx.TimestampFormat
	}
	return ""
}

// writeXLSX writes the rows as a sheet with one column per component.
func writeXLSX(c *native.Context, in native.TransformInput) ([]byte, error) {
	comps, err := components(c, in.Rows)
	if err != nil {
		return nil, err
	}
	d1904 := in.Params[paramDateSystem] == "1904"
	longAsText := in.Params[paramLongDecimals] != "ERROR"
	byName := in.Params[paramHeader] == "NAME"
	sheet := defaultSheet
	if s, ok := in.Params[paramSheet]; ok {
		sheet = s
	}
	var idx []int // the fields written, in order
	for i, comp := range comps {
		if comp != nil {
			idx = append(idx, i)
		}
	}
	cols := make([]xlsx.Column, len(idx))
	count := map[string]int{}
	for j, i := range idx {
		cols[j].Header = comps[i].Name
		if !byName {
			cols[j].Header = heading(comps[i], c.Language())
		}
		count[strings.ToLower(cols[j].Header)]++
		cols[j].Format = numberFormat(comps[i].Domain)
	}
	// Headings that two components share would not be read back.
	for j, i := range idx {
		if count[strings.ToLower(cols[j].Header)] > 1 {
			cols[j].Header = comps[i].Name
		}
	}
	// First pass: the values and the widths of the columns.
	widths := make([]int, len(idx))
	for j := range cols {
		widths[j] = utf8.RuneCountInString(cols[j].Header) + 2
	}
	rows := make([][]any, len(in.Rows.Rows))
	for r, src := range in.Rows.Rows {
		if r%1000 == 0 {
			if err := c.Err(); err != nil {
				return nil, err
			}
		}
		row := make([]any, len(idx))
		for j, i := range idx {
			v, w, err := cellValue(comps[i], src[i], longAsText)
			if err != nil {
				return nil, native.Raise(errXLSXInvalid, fmt.Sprintf("row %d, %s: %v", r+1, comps[i].Name, err))
			}
			if s, isText := v.(string); isText && comps[i].Domain.Type == model.Dec {
				col, _ := xlsx.ColumnName(j + 1)
				slog.Info("a decimal with more than 15 significant digits is written as text", "transformation", in.Name, "cell", col+strconv.Itoa(r+2), "value", s)
			}
			row[j] = v
			widths[j] = max(widths[j], w)
		}
		rows[r] = row
	}
	for j := range cols {
		cols[j].Width = float64(min(max(widths[j], 8), 60))
	}
	w, err := xlsx.NewWriter(sheet, d1904, cols, workbookLimits(c))
	if err != nil {
		return nil, xlsxErr(err)
	}
	for r, row := range rows {
		if r%1000 == 0 {
			if err := c.Err(); err != nil {
				return nil, err
			}
		}
		if err := w.Row(row); err != nil {
			return nil, xlsxErr(err)
		}
		rows[r] = nil
	}
	data, err := w.Close()
	if err != nil {
		return nil, xlsxErr(err)
	}
	return data, nil
}

// cellValue returns the value that the writer writes for a value of a
// component, and its width in characters.
func cellValue(comp *native.Component, v any, longAsText bool) (any, int, error) {
	if v == nil {
		return nil, 0, nil
	}
	d := comp.Domain
	switch x := v.(type) {
	case string:
		switch d.Type {
		case model.Dec:
			if xlsx.LongDecimal(x) {
				if !longAsText {
					return nil, 0, fmt.Errorf("the decimal %s has more than %d significant digits, more than a number holds", x, xlsx.SignificantDigits)
				}
				return x, len(x), nil
			}
			return xlsx.Numeric(x), len(x), nil
		case model.Int4, model.Int8:
			// fixed values of whole numbers come as text
			return xlsx.Numeric(x), len(x), nil
		}
		return x, utf8.RuneCountInString(x), nil
	case int64:
		s := strconv.FormatInt(x, 10)
		if x > 1<<53 || x < -(1<<53) {
			return s, len(s), nil
		}
		return x, len(s), nil
	case bool:
		return x, 5, nil
	case time.Time:
		if d.Type == model.Date {
			return x, 10, nil
		}
		return x, 19, nil
	case []byte:
		s := base64.StdEncoding.EncodeToString(x)
		return s, len(s), nil
	}
	return nil, 0, fmt.Errorf("the value %T cannot be written", v)
}

// reading is the state of reading a sheet into rows.
type reading struct {
	in         native.TransformInput
	comps      []*native.Component
	date1904   bool
	boolText   bool
	max        int
	violations []xlsxViolation
	full       bool
	cellOf     []int // column - 1 by field, -1 for none; reused by rows
	order      []int
}

func (r *reading) violate(row, col int, header, comp string, raw model.Optional[string], format string, args ...any) {
	if len(r.violations) >= r.max {
		r.full = true
		return
	}
	name := ""
	if col > 0 {
		name, _ = xlsx.ColumnName(col)
	}
	r.violations = append(r.violations, xlsxViolation{RowNo: int32(row), Column: name, Header: header, Component: comp, Raw: raw, Message: fmt.Sprintf(format, args...)})
}

func (r *reading) failed() error {
	if len(r.violations) == 0 {
		return nil
	}
	text := fmt.Sprintf("%d values of the sheet do not fit the components of %s", len(r.violations), r.in.Into.Struct)
	if len(r.violations) == 1 {
		v := r.violations[0]
		text = fmt.Sprintf("row %d, column %s (%s): %s", v.RowNo, v.Column, v.Component, v.Message)
	}
	if r.full {
		text += fmt.Sprintf("; reading stopped after %d violations", r.max)
	}
	return native.Raise(errReadFailed, text, r.violations)
}

// stop ends reading early when the violations reached the maximum.
type stop struct{}

func (stop) Error() string { return "stop" }

// readXLSX reads the rows of a sheet into rows of the target structure,
// matching the columns by the heading row.
func readXLSX(c *native.Context, in native.TransformInput) ([][]any, error) {
	comps, err := components(c, in.Into)
	if err != nil {
		return nil, err
	}
	first, err := intParam(in, paramFirstRow, 1)
	if err != nil {
		return nil, err
	}
	maxErr, err := intParam(in, paramMaxErrors, defaultMaxErrors)
	if err != nil {
		return nil, err
	}
	if err := c.CheckSize("the source of "+in.Name, len(in.Bytes)); err != nil {
		return nil, err
	}
	rd, err := xlsx.NewReader(c, in.Bytes, workbookLimits(c))
	if err != nil {
		return nil, xlsxErr(err)
	}
	r := &reading{in: in, comps: comps, date1904: rd.Date1904(), boolText: in.Params[paramBoolText] == "YES", max: maxErr}
	keys := headerKeys(comps)
	var columns []int // field by column - 1, -1 for none
	headers := map[int]string{}
	var out [][]any
	seenHeader := false
	err = rd.Rows(in.Params[paramSheet], func(n int, cells []xlsx.Cell) error {
		if n%1000 == 0 {
			if err := c.Err(); err != nil {
				return err
			}
		}
		switch {
		case n < first:
			return nil
		case n == first:
			seenHeader = true
			columns = r.header(n, cells, keys, headers)
			if len(r.violations) > 0 {
				return stop{}
			}
			return nil
		case !seenHeader:
			r.violate(first, 0, "", "", model.None[string](), "the sheet has no heading row %d", first)
			return stop{}
		}
		row, ok := r.row(n, cells, columns, headers)
		if ok {
			out = append(out, row)
		}
		if r.full {
			return stop{}
		}
		return nil
	})
	if _, isStop := err.(stop); err != nil && !isStop {
		return nil, xlsxErr(err)
	}
	if !seenHeader && len(r.violations) == 0 {
		r.violate(first, 0, "", "", model.None[string](), "the sheet has no heading row %d", first)
	}
	if err := r.failed(); err != nil {
		return nil, err
	}
	return out, nil
}

// headerKeys maps headings to fields: the name of a component, and its
// headings in all languages unless another component has the same one.
func headerKeys(comps []*native.Component) map[string]int {
	keys := map[string]int{}
	labels := map[string][]int{}
	for i, comp := range comps {
		if comp == nil {
			continue
		}
		for _, l := range comp.Labels {
			k := strings.ToLower(strings.TrimSpace(l))
			if k != "" && !slices.Contains(labels[k], i) {
				labels[k] = append(labels[k], i)
			}
		}
	}
	for k, fields := range labels {
		if len(fields) == 1 {
			keys[k] = fields[0]
		}
	}
	for i, comp := range comps {
		if comp != nil {
			keys[strings.ToLower(comp.Name)] = i
		}
	}
	return keys
}

// header maps the columns of the heading row to fields and reports
// columns that name a component twice and missing required components.
func (r *reading) header(n int, cells []xlsx.Cell, keys map[string]int, headers map[int]string) []int {
	columns := make([]int, len(cells))
	byField := map[int]int{}
	for j, cell := range cells {
		columns[j] = -1
		text, ok, _ := cell.TextValue()
		text = strings.TrimSpace(text)
		if !ok || text == "" {
			continue
		}
		headers[j] = text
		i, known := keys[strings.ToLower(text)]
		if !known {
			continue
		}
		if prev, dup := byField[i]; dup {
			col, _ := xlsx.ColumnName(prev + 1)
			r.violate(n, j+1, text, r.comps[i].Name, model.Some(text), "the column %s is the second one for %s, after column %s (%s)", mustColumn(j+1), r.comps[i].Name, col, headers[prev])
			continue
		}
		byField[i] = j
		columns[j] = i
	}
	for i, comp := range r.comps {
		if comp == nil {
			continue
		}
		if _, ok := byField[i]; !ok && comp.NotNull && !comp.Default.Valid() {
			r.violate(n, 0, heading(comp, model.DefaultLanguage), comp.Name, model.None[string](), "the sheet has no column for %s, which needs a value", comp.Name)
		}
	}
	return columns
}

func mustColumn(n int) string {
	s, _ := xlsx.ColumnName(n)
	return s
}

// row reads a data row; rows without values in the mapped columns are
// left out.
func (r *reading) row(n int, cells []xlsx.Cell, columns []int, headers map[int]string) ([]any, bool) {
	empty := true
	if r.cellOf == nil {
		r.cellOf, r.order = make([]int, len(r.comps)), make([]int, len(r.comps))
	}
	cellOf := r.cellOf
	for i := range cellOf {
		cellOf[i] = -1
	}
	for j, i := range columns {
		if i >= 0 && j < len(cells) {
			cellOf[i] = j
			if cells[j].HasValue || cells[j].Formula != "" {
				empty = false
			}
		}
	}
	if empty {
		return nil, false
	}
	out := make([]any, len(r.comps))
	before := len(r.violations)
	// Violations come in the order of the columns.
	order := r.order
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cellOf[a] - cellOf[b] })
	for _, i := range order {
		comp := r.comps[i]
		if comp == nil {
			continue
		}
		j := cellOf[i]
		var cell xlsx.Cell
		if j >= 0 {
			cell = cells[j]
		}
		if !cell.HasValue && cell.Formula == "" {
			switch {
			case comp.Default.Valid() && (comp.NotNull || j < 0):
				out[i] = comp.Default.Or("")
			case comp.NotNull:
				r.violate(n, j+1, headers[j], comp.Name, model.None[string](), "%s needs a value", comp.Name)
			}
			continue
		}
		v, err := r.value(comp, cell)
		if err != nil {
			raw := model.None[string]()
			if cell.HasValue {
				raw = model.Some(cell.Raw)
			}
			r.violate(n, j+1, headers[j], comp.Name, raw, "%v", err)
			continue
		}
		out[i] = v
	}
	return out, len(r.violations) == before
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// value reads a cell for a component and checks it against the domain.
func (r *reading) value(comp *native.Component, cell xlsx.Cell) (any, error) {
	d := comp.Domain
	var (
		v    any
		text string
		ok   bool
		err  error
	)
	switch d.Type {
	case model.Char, model.String, model.UUID, model.Blob:
		var s string
		s, ok, err = cell.TextValue()
		v, text = s, s
	case model.Int4, model.Int8:
		var n int64
		n, ok, err = cell.IntegerValue()
		if err == nil && d.Type == model.Int4 && (n > 1<<31-1 || n < -(1<<31)) {
			return nil, fmt.Errorf("%d is out of the range of INT4", n)
		}
		v, text = n, strconv.FormatInt(n, 10)
	case model.Dec:
		var s string
		s, ok, err = cell.DecimalValue(d.Length, d.Decimals)
		v, text = s, s
	case model.Date:
		var t time.Time
		t, ok, err = cell.DateValue(r.date1904)
		v, text = t, t.Format("2006-01-02")
	case model.Timestamp:
		var t time.Time
		t, ok, err = cell.TimestampValue(r.date1904)
		v, text = t, t.Format(time.RFC3339Nano)
	case model.Bool:
		var b bool
		b, ok, err = cell.BoolValue(r.boolText)
		v, text = b, strconv.FormatBool(b)
	default:
		return nil, fmt.Errorf("values of type %s cannot be read from a sheet", d.Type)
	}
	if m, isM := err.(*xlsx.Mismatch); isM {
		return nil, fmt.Errorf("it is no %s: %s", m.Expected, m.Reason)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s needs a value", comp.Name)
	}
	switch d.Type {
	case model.Char:
		if n := utf8.RuneCountInString(text); d.Length > 0 && n > d.Length {
			return nil, fmt.Errorf("it has %d characters, more than %d", n, d.Length)
		}
	case model.UUID:
		if !uuidPattern.MatchString(strings.TrimSpace(text)) {
			return nil, fmt.Errorf("it is no UUID")
		}
		v = strings.ToLower(strings.TrimSpace(text))
	case model.Blob:
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
		if err != nil {
			return nil, fmt.Errorf("it is no base64 text")
		}
		v = b
	}
	if d.Type.IsString() && text == "" && !d.AllowsEmpty() {
		return nil, fmt.Errorf("%s does not take the empty text", d.Name)
	}
	if len(d.Values) > 0 {
		found := false
		for _, fv := range d.Values {
			found = found || fv.Value == text
		}
		if !found {
			var vals []string
			for _, fv := range d.Values {
				vals = append(vals, fv.Value)
			}
			return nil, fmt.Errorf("it is no fixed value of %s (%s)", d.Name, strings.Join(vals, ", "))
		}
	}
	return v, nil
}
