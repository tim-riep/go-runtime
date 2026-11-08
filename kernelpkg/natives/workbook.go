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
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/internal/xlsx"
	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// The exceptions of workbooks.
const (
	errXLSX         = "/TRBASE/XLSX_ERROR"
	errXLSXInvalid  = "/TRBASE/XLSX_INVALID"
	errSheet        = "/TRBASE/XLSX_SHEET_NOT_FOUND"
	errCell         = "/TRBASE/XLSX_CELL_INVALID"
	errMismatch     = "/TRBASE/XLSX_TYPE_MISMATCH"
	errXLSXLimit    = "/TRBASE/XLSX_LIMIT_EXCEEDED"
	errProtected    = "/TRBASE/XLSX_PROTECTED"
	workbookClass   = "/TRBASE/XLSX_WORKBOOK"
	rowClass        = "/TRBASE/XLSX_ROW"
	decimalScale    = 15
	decimalDigits   = 38
	workbookRaises  = " raises " + errXLSX
	cellSignature   = "(sheet: STRING, cell: STRING)"
	columnSignature = "(column: INT4)"
)

// xlsxErr turns errors of workbooks into exceptions.
func xlsxErr(err error) error {
	var e *xlsx.Err
	switch {
	case err == nil:
		return nil
	case errors.As(err, &e):
		switch e.Kind {
		case xlsx.SheetNotFound:
			return native.Raise(errSheet, e.Sheet)
		case xlsx.CellInvalid:
			return native.Raise(errCell, e.Cell, e.Message)
		case xlsx.LimitExceeded:
			return native.Raise(errXLSXLimit, e.Message)
		case xlsx.Protected:
			return native.Raise(errProtected, e.Message)
		}
		return native.Raise(errXLSXInvalid, e.Message)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return native.Raise(errXLSXInvalid, err.Error())
}

func workbookLimits(c *native.Context) native.WorkbookLimits { return c.Limits.OrDefault().Workbook }

// workbook is the Go value of /TRBASE/XLSX_WORKBOOK.
type workbook struct{ w *xlsx.Workbook }

// Close implements io.Closer for the release of the object.
func (w *workbook) Close() error { return w.w.Close() }

// row is the Go value of /TRBASE/XLSX_ROW.
type row struct {
	sheet    string
	r        xlsx.Row
	date1904 bool
}

func (r *row) cell(column int32) (xlsx.Cell, string, error) {
	name, err := xlsx.ColumnName(int(column))
	if err != nil {
		return xlsx.Cell{}, "", xlsxErr(err)
	}
	ref := name + strconv.Itoa(r.r.Number)
	if int(column) > len(r.r.Cells) {
		return xlsx.Cell{}, ref, nil
	}
	return r.r.Cells[column-1], ref, nil
}

// reader reads a typed value of a cell; mismatches become
// /TRBASE/XLSX_TYPE_MISMATCH with sheet, cell and stored value.
type reader[T any] func(c xlsx.Cell, date1904 bool) (T, bool, error)

func typed[T any](sheet, ref string, c xlsx.Cell, date1904 bool, read reader[T]) (model.Optional[T], error) {
	v, ok, err := read(c, date1904)
	var m *xlsx.Mismatch
	switch {
	case errors.As(err, &m):
		raw := model.None[string]()
		if c.HasValue {
			raw = model.Some(c.Raw)
		}
		return model.None[T](), native.Raise(errMismatch, sheet, ref, raw, m.Expected, m.Reason)
	case err != nil:
		return model.None[T](), err
	case !ok:
		return model.None[T](), nil
	}
	return model.Some(v), nil
}

var (
	readText = func(c xlsx.Cell, _ bool) (string, bool, error) { return c.TextValue() }
	readInt  = func(c xlsx.Cell, _ bool) (int64, bool, error) { return c.IntegerValue() }
	readDec  = func(c xlsx.Cell, _ bool) (dec.Dec, bool, error) {
		s, ok, err := c.DecimalValue(decimalDigits, decimalScale)
		if !ok || err != nil {
			return dec.Dec{}, ok, err
		}
		d, err := dec.Parse(s, decimalScale)
		return d, err == nil, err
	}
	readDate = func(c xlsx.Cell, d1904 bool) (native.Date, bool, error) {
		t, ok, err := c.DateValue(d1904)
		return native.DateOf(t), ok, err
	}
	readTimestamp = func(c xlsx.Cell, d1904 bool) (time.Time, bool, error) { return c.TimestampValue(d1904) }
	readBool      = func(c xlsx.Cell, _ bool) (bool, bool, error) { return c.BoolValue(false) }
)

func raw(c xlsx.Cell) model.Optional[string] {
	if !c.HasValue {
		return model.None[string]()
	}
	return model.Some(c.Raw)
}

func formula(c xlsx.Cell) model.Optional[string] {
	if c.Formula == "" {
		return model.None[string]()
	}
	return model.Some("=" + c.Formula)
}

func init() {
	native.Default.RegisterClass(pkg, workbookClass, (*workbook)(nil))
	native.Default.RegisterClass(pkg, rowClass, (*row)(nil))
	static(workbookClass, "OPEN", "(data: BYTES) returns /TRBASE/XLSX_WORKBOOK"+workbookRaises, func(c *native.Context, data []byte) (*workbook, error) {
		if err := c.CheckSize("the workbook", len(data)); err != nil {
			return nil, err
		}
		w, err := xlsx.Open(c, data, workbookLimits(c))
		if err != nil {
			return nil, xlsxErr(err)
		}
		return &workbook{w: w}, nil
	})
	static(workbookClass, "CREATE", "() returns /TRBASE/XLSX_WORKBOOK", func(c *native.Context) (*workbook, error) {
		return &workbook{w: xlsx.Create(workbookLimits(c))}, nil
	})
	static(workbookClass, "COLUMN", "(name: STRING) returns INT4"+workbookRaises, func(c *native.Context, name string) (int32, error) {
		n, err := xlsx.ColumnNumber(name)
		return int32(n), xlsxErr(err)
	})
	static(workbookClass, "COLUMN_NAME", "(number: INT4) returns STRING"+workbookRaises, func(c *native.Context, n int32) (string, error) {
		s, err := xlsx.ColumnName(int(n))
		return s, xlsxErr(err)
	})
	instance(workbookClass, "SAVE", "() returns BYTES"+workbookRaises, func(c *native.Context, w *workbook) ([]byte, error) {
		b, err := w.w.Save()
		return b, xlsxErr(err)
	})
	native.Default.Register(native.Method{Package: pkg, Class: workbookClass, Method: "CLOSE", Signature: "()", Releases: true,
		Func: func(c *native.Context, w *workbook) error { return nil }})
	instance(workbookClass, "SHEETS", "() returns array of STRING", func(c *native.Context, w *workbook) ([]string, error) { return w.w.Sheets(), nil })
	instance(workbookClass, "HAS_SHEET", "(sheet: STRING) returns BOOL", func(c *native.Context, w *workbook, s string) (bool, error) { return w.w.HasSheet(s), nil })
	instance(workbookClass, "ADD_SHEET", "(sheet: STRING)"+workbookRaises, func(c *native.Context, w *workbook, s string) error { return xlsxErr(w.w.AddSheet(s)) })
	instance(workbookClass, "RENAME_SHEET", "(sheet: STRING, name: STRING)"+workbookRaises, func(c *native.Context, w *workbook, s, to string) error {
		return xlsxErr(w.w.RenameSheet(s, to))
	})
	instance(workbookClass, "DELETE_SHEET", "(sheet: STRING)"+workbookRaises, func(c *native.Context, w *workbook, s string) error { return xlsxErr(w.w.DeleteSheet(s)) })
	instance(workbookClass, "DIMENSION", "(sheet: STRING) returns /TRBASE/XLSX_DIMENSION"+workbookRaises, func(c *native.Context, w *workbook, s string) (XlsxDimension, error) {
		d, err := w.w.Dimension(s)
		return XlsxDimension{FirstRow: int32(d.FirstRow), FirstColumn: int32(d.FirstColumn), LastRow: int32(d.LastRow), LastColumn: int32(d.LastColumn), Ref: d.Ref}, xlsxErr(err)
	})
	instance(workbookClass, "ROWS", "(sheet: STRING) returns array of /TRBASE/XLSX_ROW"+workbookRaises, func(c *native.Context, w *workbook, s string) ([]*row, error) {
		rows, err := w.w.Rows(s)
		if err != nil {
			return nil, xlsxErr(err)
		}
		out := make([]*row, len(rows))
		for i, r := range rows {
			out[i] = &row{sheet: s, r: r, date1904: w.w.Date1904()}
		}
		return out, nil
	})

	// Typed reads of a cell of the workbook and of a row.
	cellOf := func(w *workbook, sheet, ref string) (xlsx.Cell, error) {
		cell, err := w.w.Cell(sheet, ref)
		return cell, xlsxErr(err)
	}
	instance(workbookClass, "TEXT", cellSignature+" returns STRING?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[string], error) {
		cell, err := cellOf(w, s, ref)
		if err != nil {
			return model.None[string](), err
		}
		return typed(s, ref, cell, w.w.Date1904(), readText)
	})
	instance(workbookClass, "INTEGER", cellSignature+" returns INT8?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[int64], error) {
		cell, err := cellOf(w, s, ref)
		if err != nil {
			return model.None[int64](), err
		}
		return typed(s, ref, cell, w.w.Date1904(), readInt)
	})
	instance(workbookClass, "DECIMAL", cellSignature+" returns DEC(38,15)?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[dec.Dec], error) {
		cell, err := cellOf(w, s, ref)
		if err != nil {
			return model.None[dec.Dec](), err
		}
		return typed(s, ref, cell, w.w.Date1904(), readDec)
	})
	instance(workbookClass, "DATE", cellSignature+" returns DATE?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[native.Date], error) {
		cell, err := cellOf(w, s, ref)
		if err != nil {
			return model.None[native.Date](), err
		}
		return typed(s, ref, cell, w.w.Date1904(), readDate)
	})
	instance(workbookClass, "TIMESTAMP", cellSignature+" returns TIMESTAMP?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[time.Time], error) {
		cell, err := cellOf(w, s, ref)
		if err != nil {
			return model.None[time.Time](), err
		}
		return typed(s, ref, cell, w.w.Date1904(), readTimestamp)
	})
	instance(workbookClass, "BOOL", cellSignature+" returns BOOL?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[bool], error) {
		cell, err := cellOf(w, s, ref)
		if err != nil {
			return model.None[bool](), err
		}
		return typed(s, ref, cell, w.w.Date1904(), readBool)
	})
	instance(workbookClass, "KIND", cellSignature+" returns /TRBASE/XLSX_CELL_KIND"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (XlsxCellKind, error) {
		cell, err := cellOf(w, s, ref)
		return XlsxCellKind(cell.Kind()), err
	})
	instance(workbookClass, "RAW", cellSignature+" returns STRING?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[string], error) {
		cell, err := cellOf(w, s, ref)
		return raw(cell), err
	})
	instance(workbookClass, "FORMULA", cellSignature+" returns STRING?"+workbookRaises, func(c *native.Context, w *workbook, s, ref string) (model.Optional[string], error) {
		cell, err := cellOf(w, s, ref)
		return formula(cell), err
	})

	instance(rowClass, "NUMBER", "() returns INT4", func(c *native.Context, r *row) (int32, error) { return int32(r.r.Number), nil })
	instance(rowClass, "LAST_COLUMN", "() returns INT4", func(c *native.Context, r *row) (int32, error) { return int32(len(r.r.Cells)), nil })
	instance(rowClass, "TEXT", columnSignature+" returns STRING?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[string], error) {
		cell, ref, err := r.cell(col)
		if err != nil {
			return model.None[string](), err
		}
		return typed(r.sheet, ref, cell, r.date1904, readText)
	})
	instance(rowClass, "INTEGER", columnSignature+" returns INT8?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[int64], error) {
		cell, ref, err := r.cell(col)
		if err != nil {
			return model.None[int64](), err
		}
		return typed(r.sheet, ref, cell, r.date1904, readInt)
	})
	instance(rowClass, "DECIMAL", columnSignature+" returns DEC(38,15)?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[dec.Dec], error) {
		cell, ref, err := r.cell(col)
		if err != nil {
			return model.None[dec.Dec](), err
		}
		return typed(r.sheet, ref, cell, r.date1904, readDec)
	})
	instance(rowClass, "DATE", columnSignature+" returns DATE?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[native.Date], error) {
		cell, ref, err := r.cell(col)
		if err != nil {
			return model.None[native.Date](), err
		}
		return typed(r.sheet, ref, cell, r.date1904, readDate)
	})
	instance(rowClass, "TIMESTAMP", columnSignature+" returns TIMESTAMP?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[time.Time], error) {
		cell, ref, err := r.cell(col)
		if err != nil {
			return model.None[time.Time](), err
		}
		return typed(r.sheet, ref, cell, r.date1904, readTimestamp)
	})
	instance(rowClass, "BOOL", columnSignature+" returns BOOL?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[bool], error) {
		cell, ref, err := r.cell(col)
		if err != nil {
			return model.None[bool](), err
		}
		return typed(r.sheet, ref, cell, r.date1904, readBool)
	})
	instance(rowClass, "KIND", columnSignature+" returns /TRBASE/XLSX_CELL_KIND"+workbookRaises, func(c *native.Context, r *row, col int32) (XlsxCellKind, error) {
		cell, _, err := r.cell(col)
		return XlsxCellKind(cell.Kind()), err
	})
	instance(rowClass, "RAW", columnSignature+" returns STRING?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[string], error) {
		cell, _, err := r.cell(col)
		return raw(cell), err
	})
	instance(rowClass, "FORMULA", columnSignature+" returns STRING?"+workbookRaises, func(c *native.Context, r *row, col int32) (model.Optional[string], error) {
		cell, _, err := r.cell(col)
		return formula(cell), err
	})

	// Writes.
	set := func(method, typ string, fn any) {
		instance(workbookClass, method, "(sheet: STRING, cell: STRING, "+typ+")"+workbookRaises, fn)
	}
	set("SET_TEXT", "value: STRING", func(c *native.Context, w *workbook, s, ref, v string) error { return xlsxErr(w.w.SetText(s, ref, v)) })
	set("SET_INTEGER", "value: INT8", func(c *native.Context, w *workbook, s, ref string, v int64) error {
		return xlsxErr(w.w.SetInteger(s, ref, v))
	})
	set("SET_DECIMAL", "value: DEC(38,15)", func(c *native.Context, w *workbook, s, ref string, v dec.Dec) error {
		text := dec.String(v, decimalScale)
		if strings.Contains(text, ".") {
			text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
		}
		asText, err := w.w.SetDecimal(s, ref, text)
		if asText {
			slog.Info("a decimal with more than 15 significant digits is written as text", "sheet", s, "cell", ref, "value", text)
		}
		return xlsxErr(err)
	})
	set("SET_DATE", "value: DATE", func(c *native.Context, w *workbook, s, ref string, v native.Date) error {
		return xlsxErr(w.w.SetDate(s, ref, v.Time()))
	})
	set("SET_TIMESTAMP", "value: TIMESTAMP", func(c *native.Context, w *workbook, s, ref string, v time.Time) error {
		return xlsxErr(w.w.SetTimestamp(s, ref, v))
	})
	set("SET_BOOL", "value: BOOL", func(c *native.Context, w *workbook, s, ref string, v bool) error {
		return xlsxErr(w.w.SetBool(s, ref, v))
	})
	set("SET_FORMULA", "formula: STRING", func(c *native.Context, w *workbook, s, ref, f string) error {
		return xlsxErr(w.w.SetFormula(s, ref, f))
	})
	set("SET_NUMBER_FORMAT", "format: STRING", func(c *native.Context, w *workbook, s, ref, f string) error {
		return xlsxErr(w.w.SetNumberFormat(s, ref, f))
	})
	instance(workbookClass, "CLEAR", cellSignature+workbookRaises, func(c *native.Context, w *workbook, s, ref string) error { return xlsxErr(w.w.Clear(s, ref)) })
	instance(workbookClass, "SET_COLUMN_FORMAT", "(sheet: STRING, column: STRING, format: STRING)"+workbookRaises, func(c *native.Context, w *workbook, s, col, f string) error {
		return xlsxErr(w.w.SetColumnFormat(s, col, f))
	})
	instance(workbookClass, "SET_COLUMN_WIDTH", "(sheet: STRING, column: STRING, width: DEC(5,2))"+workbookRaises, func(c *native.Context, w *workbook, s, col string, width dec.Dec) error {
		x, _ := strconv.ParseFloat(dec.String(width, 2), 64)
		return xlsxErr(w.w.SetColumnWidth(s, col, x))
	})
	instance(workbookClass, "SET_BOLD", "(sheet: STRING, range: STRING)"+workbookRaises, func(c *native.Context, w *workbook, s, rng string) error { return xlsxErr(w.w.SetBold(s, rng)) })
	instance(workbookClass, "FREEZE_ROWS", "(sheet: STRING, rows: INT4)"+workbookRaises, func(c *native.Context, w *workbook, s string, n int32) error {
		return xlsxErr(w.w.FreezeRows(s, int(n)))
	})
	instance(workbookClass, "SET_AUTOFILTER", "(sheet: STRING, range: STRING)"+workbookRaises, func(c *native.Context, w *workbook, s, rng string) error {
		return xlsxErr(w.w.AutoFilter(s, rng))
	})
}
