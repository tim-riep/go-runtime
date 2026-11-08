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
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/internal/archive"
	"github.com/tim-riep/go-runtime/native"
	"github.com/xuri/excelize/v2"
)

// ErrorKind classifies errors of workbooks.
type ErrorKind int

// Kinds of errors.
const (
	Invalid ErrorKind = iota + 1
	SheetNotFound
	CellInvalid
	TypeMismatch
	LimitExceeded
	Protected
)

// Err is an error of a workbook.
type Err struct {
	Kind     ErrorKind
	Sheet    string
	Cell     string
	Raw      string
	Expected string
	Message  string
}

func (e *Err) Error() string { return e.Message }

func fail(kind ErrorKind, format string, args ...any) *Err {
	return &Err{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// cfb is the signature of the container of encrypted workbooks.
var cfb = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

// precheck unpacks every part of a workbook under the limits, measuring
// what is unpacked, before the library trusts the sizes it claims.
func precheck(ctx context.Context, data []byte, l native.WorkbookLimits) error {
	if bytes.HasPrefix(data, cfb) {
		return fail(Protected, "the workbook is encrypted with a password")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fail(Invalid, "the data is no workbook: %v", err)
	}
	m := archive.NewMeter(ctx, l.Archive)
	for _, f := range zr.File {
		if err := m.Entry(); err != nil {
			return limitErr(err)
		}
		if f.Flags&1 != 0 {
			return fail(Protected, "the part %s is encrypted", f.Name)
		}
		r, err := m.OpenEntry(f)
		if err != nil {
			return fail(Invalid, "the part %s cannot be read: %v", f.Name, err)
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			return limitErr(err)
		}
	}
	return nil
}

func limitErr(err error) error {
	var le *archive.LimitError
	switch {
	case errors.As(err, &le):
		return fail(LimitExceeded, "the workbook: %v", le)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return fail(Invalid, "the workbook is damaged: %v", err)
}

// Workbook is a workbook in memory, for access cell by cell.
type Workbook struct {
	f        *excelize.File
	l        native.WorkbookLimits
	date1904 bool
	// formats are the codes of the number formats the file defines, also
	// those with ids of built-in formats.
	formats map[int]string
}

// Open reads a workbook. Macros are kept out of what is saved; external
// links and embedded objects are never followed.
func Open(ctx context.Context, data []byte, l native.WorkbookLimits) (*Workbook, error) {
	if err := precheck(ctx, data, l); err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: l.Archive.Total, UnzipXMLSizeLimit: l.Archive.Entry})
	if err != nil {
		return nil, fail(Invalid, "the data is no workbook: %v", err)
	}
	w := &Workbook{f: f, l: l, formats: numberFormats(data)}
	if props, err := f.GetWorkbookProps(); err == nil && props.Date1904 != nil {
		w.date1904 = *props.Date1904
	}
	if n := len(f.GetSheetList()); n > l.Sheets {
		f.Close()
		return nil, fail(LimitExceeded, "the workbook has %d sheets, more than the limit of %d", n, l.Sheets)
	}
	var cells int64
	for _, s := range f.GetSheetList() {
		d, err := w.Dimension(s)
		if err != nil {
			continue
		}
		if d.LastRow > l.Rows || d.LastColumn > l.Columns {
			f.Close()
			return nil, fail(LimitExceeded, "the sheet %s has %d rows and %d columns, more than the limits of %d and %d", s, d.LastRow, d.LastColumn, l.Rows, l.Columns)
		}
		if cells += int64(d.LastRow) * int64(d.LastColumn); cells > l.Cells {
			f.Close()
			return nil, fail(LimitExceeded, "the workbook has more than the limit of %d cells", l.Cells)
		}
	}
	return w, nil
}

// Create returns a workbook with the sheet Sheet1.
func Create(l native.WorkbookLimits) *Workbook {
	return &Workbook{f: excelize.NewFile(), l: l, formats: map[int]string{}}
}

// Close frees the workbook.
func (w *Workbook) Close() error {
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *Workbook) file() (*excelize.File, error) {
	if w.f == nil {
		return nil, fail(Invalid, "the workbook is closed")
	}
	return w.f, nil
}

var (
	vbaOverride    = regexp.MustCompile(`<Override[^>]*PartName="/xl/vbaProject\.bin"[^>]*(?:/>|></Override>)`)
	vbaDefault     = regexp.MustCompile(`<Default[^>]*Extension="bin"[^>]*vbaProject[^>]*(?:/>|></Default>)`)
	vbaRelation    = regexp.MustCompile(`<Relationship[^>]*Target="[^"]*vbaProject\.bin"[^>]*(?:/>|></Relationship>)`)
	macroWorkbook  = "application/vnd.ms-excel.sheet.macroEnabled.main+xml"
	normalWorkbook = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
)

// Save returns the workbook in xlsx format, without macros.
func (w *Workbook) Save() ([]byte, error) {
	f, err := w.file()
	if err != nil {
		return nil, err
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fail(Invalid, "the workbook cannot be saved: %v", err)
	}
	return stripMacros(buf.Bytes())
}

// stripMacros removes a macro project and turns the workbook into a
// plain one.
func stripMacros(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	macros := false
	for _, f := range zr.File {
		macros = macros || f.Name == "xl/vbaProject.bin"
	}
	if !macros {
		return data, nil
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if f.Name == "xl/vbaProject.bin" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		switch f.Name {
		case "[Content_Types].xml":
			b = vbaOverride.ReplaceAll(vbaDefault.ReplaceAll(b, nil), nil)
			b = bytes.ReplaceAll(b, []byte(macroWorkbook), []byte(normalWorkbook))
		case "xl/_rels/workbook.xml.rels":
			b = vbaRelation.ReplaceAll(b, nil)
		}
		fw, err := zw.Create(f.Name)
		if err != nil {
			return nil, err
		}
		fw.Write(b)
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Sheets returns the names of the sheets in order.
func (w *Workbook) Sheets() []string {
	if w.f == nil {
		return []string{}
	}
	return w.f.GetSheetList()
}

// HasSheet reports whether a sheet exists.
func (w *Workbook) HasSheet(name string) bool {
	if w.f == nil {
		return false
	}
	i, err := w.f.GetSheetIndex(name)
	return err == nil && i >= 0
}

func (w *Workbook) sheet(name string) (*excelize.File, error) {
	f, err := w.file()
	if err != nil {
		return nil, err
	}
	if !w.HasSheet(name) {
		e := fail(SheetNotFound, "the workbook has no sheet %s", name)
		e.Sheet = name
		return nil, e
	}
	return f, nil
}

// AddSheet adds an empty sheet at the end.
func (w *Workbook) AddSheet(name string) error {
	f, err := w.file()
	if err != nil {
		return err
	}
	if w.HasSheet(name) {
		return fail(Invalid, "the workbook has a sheet %s already", name)
	}
	if len(f.GetSheetList()) >= w.l.Sheets {
		return fail(LimitExceeded, "the workbook has the limit of %d sheets", w.l.Sheets)
	}
	if _, err := f.NewSheet(name); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// RenameSheet renames a sheet.
func (w *Workbook) RenameSheet(name, to string) error {
	f, err := w.sheet(name)
	if err != nil {
		return err
	}
	if w.HasSheet(to) {
		return fail(Invalid, "the workbook has a sheet %s already", to)
	}
	if err := f.SetSheetName(name, to); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// DeleteSheet removes a sheet; the last one stays.
func (w *Workbook) DeleteSheet(name string) error {
	f, err := w.sheet(name)
	if err != nil {
		return err
	}
	if len(f.GetSheetList()) == 1 {
		return fail(Invalid, "the sheet %s is the only one of the workbook", name)
	}
	if err := f.DeleteSheet(name); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// Dimension is the used area of a sheet.
type Dimension struct {
	FirstRow, FirstColumn, LastRow, LastColumn int
	Ref                                        string
}

// Dimension returns the used area of a sheet; an empty sheet has rows
// and columns 0.
func (w *Workbook) Dimension(sheet string) (Dimension, error) {
	f, err := w.sheet(sheet)
	if err != nil {
		return Dimension{}, err
	}
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return Dimension{}, fail(Invalid, "%v", err)
	}
	d := Dimension{}
	for i, r := range rows {
		for j := range r {
			if c, _ := w.cellAt(f, sheet, j+1, i+1); c.HasValue || c.Formula != "" {
				if d.FirstRow == 0 || i+1 < d.FirstRow {
					d.FirstRow = i + 1
				}
				if d.FirstColumn == 0 || j+1 < d.FirstColumn {
					d.FirstColumn = j + 1
				}
				d.LastRow, d.LastColumn = max(d.LastRow, i+1), max(d.LastColumn, j+1)
			}
		}
	}
	if d.LastRow > 0 {
		a, _ := excelize.CoordinatesToCellName(d.FirstColumn, d.FirstRow)
		b, _ := excelize.CoordinatesToCellName(d.LastColumn, d.LastRow)
		d.Ref = a + ":" + b
	}
	return d, nil
}

// ref checks a cell reference such as B7.
func ref(cell string) (int, int, error) {
	col, row, err := excelize.CellNameToCoordinates(strings.ToUpper(strings.TrimSpace(cell)))
	if err != nil {
		e := fail(CellInvalid, "%q is no cell reference such as B7", cell)
		e.Cell = cell
		return 0, 0, e
	}
	return col, row, nil
}

// ColumnNumber returns the number of a column such as C, from 1.
func ColumnNumber(name string) (int, error) {
	n, err := excelize.ColumnNameToNumber(strings.ToUpper(strings.TrimSpace(name)))
	if err != nil {
		e := fail(CellInvalid, "%q is no column such as C", name)
		e.Cell = name
		return 0, e
	}
	return n, nil
}

// ColumnName returns the name of a column number from 1.
func ColumnName(n int) (string, error) {
	s, err := excelize.ColumnNumberToName(n)
	if err != nil {
		return "", fail(CellInvalid, "%d is no column number from 1 to 16384", n)
	}
	return s, nil
}

func (w *Workbook) cellAt(f *excelize.File, sheet string, col, row int) (Cell, error) {
	name, _ := excelize.CoordinatesToCellName(col, row)
	typ, err := f.GetCellType(sheet, name)
	if err != nil {
		return Cell{}, fail(Invalid, "%v", err)
	}
	raw, _ := f.GetCellValue(sheet, name, excelize.Options{RawCellValue: true})
	formula, _ := f.GetCellFormula(sheet, name)
	c := Cell{Raw: raw, Formula: formula}
	c.Type = map[excelize.CellType]string{excelize.CellTypeBool: "b", excelize.CellTypeDate: "d", excelize.CellTypeError: "e",
		excelize.CellTypeFormula: "str", excelize.CellTypeInlineString: "inlineStr", excelize.CellTypeSharedString: "s", excelize.CellTypeNumber: "n"}[typ]
	c.HasValue = raw != "" || c.text()
	if c.HasValue && (c.Type == "" || c.Type == "n") {
		if id, err := f.GetCellStyle(sheet, name); err == nil && id > 0 {
			if st, err := f.GetStyle(id); err == nil {
				code := w.formats[st.NumFmt]
				if st.CustomNumFmt != nil {
					code = *st.CustomNumFmt
				}
				c.DateFormat = IsDateFormat(st.NumFmt, code)
			}
		}
	}
	return c, nil
}

// Cell returns a cell of a sheet.
func (w *Workbook) Cell(sheet, cell string) (Cell, error) {
	f, err := w.sheet(sheet)
	if err != nil {
		return Cell{}, err
	}
	col, row, err := ref(cell)
	if err != nil {
		return Cell{}, err
	}
	return w.cellAt(f, sheet, col, row)
}

// Date1904 reports the 1904 date system.
func (w *Workbook) Date1904() bool { return w.date1904 }

// Row is a row of a sheet with its cells by column from 1.
type Row struct {
	Number int
	Cells  []Cell // index column - 1
}

// Rows returns the rows of a sheet that have cells, in order.
func (w *Workbook) Rows(sheet string) ([]Row, error) {
	f, err := w.sheet(sheet)
	if err != nil {
		return nil, err
	}
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fail(Invalid, "%v", err)
	}
	var out []Row
	var cells int64
	for i, r := range rows {
		row := Row{Number: i + 1}
		for j := range r {
			c, err := w.cellAt(f, sheet, j+1, i+1)
			if err != nil {
				return nil, err
			}
			row.Cells = append(row.Cells, c)
		}
		if cells += int64(len(row.Cells)); cells > w.l.Cells {
			return nil, fail(LimitExceeded, "the sheet %s has more than the limit of %d cells", sheet, w.l.Cells)
		}
		for len(row.Cells) > 0 && !row.Cells[len(row.Cells)-1].HasValue && row.Cells[len(row.Cells)-1].Formula == "" {
			row.Cells = row.Cells[:len(row.Cells)-1]
		}
		if len(row.Cells) > 0 {
			out = append(out, row)
		}
	}
	return out, nil
}

// set writes a cell after the checks of sheet and reference.
func (w *Workbook) set(sheet, cell string, fn func(f *excelize.File, name string) error) error {
	f, err := w.sheet(sheet)
	if err != nil {
		return err
	}
	col, row, err := ref(cell)
	if err != nil {
		return err
	}
	if row > w.l.Rows || col > w.l.Columns {
		return fail(LimitExceeded, "the cell %s is beyond the limits of %d rows and %d columns", cell, w.l.Rows, w.l.Columns)
	}
	name, _ := excelize.CoordinatesToCellName(col, row)
	if err := fn(f, name); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// SetText writes text; "" is a text cell without content.
func (w *Workbook) SetText(sheet, cell, v string) error {
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		f.SetCellFormula(sheet, n, "")
		return f.SetCellStr(sheet, n, v)
	})
}

// maxExact is the largest integer that numbers hold exactly.
const maxExact = 1 << 53

// SetInteger writes a whole number; beyond 2^53 as text, which numbers
// cannot hold exactly.
func (w *Workbook) SetInteger(sheet, cell string, v int64) error {
	if v > maxExact || v < -maxExact {
		return w.SetText(sheet, cell, strconv.FormatInt(v, 10))
	}
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		f.SetCellFormula(sheet, n, "")
		return f.SetCellInt(sheet, n, v)
	})
}

// SetDecimal writes decimal text as number; with more than 15
// significant digits as text, and the result says so.
func (w *Workbook) SetDecimal(sheet, cell, v string) (asText bool, err error) {
	if LongDecimal(v) {
		return true, w.SetText(sheet, cell, v)
	}
	x, perr := strconv.ParseFloat(v, 64)
	if perr != nil {
		return false, fail(Invalid, "%q is no decimal", v)
	}
	return false, w.set(sheet, cell, func(f *excelize.File, n string) error {
		f.SetCellFormula(sheet, n, "")
		return f.SetCellFloat(sheet, n, x, -1, 64)
	})
}

// style returns a style with a number format code or bold font.
func (w *Workbook) style(f *excelize.File, s *excelize.Style) (int, error) { return f.NewStyle(s) }

func (w *Workbook) setSerial(sheet, cell string, t time.Time, code string) error {
	serial, _ := strconv.ParseFloat(Serial(t, w.date1904), 64)
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		f.SetCellFormula(sheet, n, "")
		if err := f.SetCellFloat(sheet, n, serial, -1, 64); err != nil {
			return err
		}
		id, err := w.style(f, &excelize.Style{CustomNumFmt: &code})
		if err != nil {
			return err
		}
		return f.SetCellStyle(sheet, n, n, id)
	})
}

// Formats of dates and times that the workbook writes.
const (
	DateFormat      = "yyyy-mm-dd"
	TimestampFormat = "yyyy-mm-dd hh:mm:ss"
)

// SetDate writes a date as serial number with a date format.
func (w *Workbook) SetDate(sheet, cell string, t time.Time) error {
	return w.setSerial(sheet, cell, t, DateFormat)
}

// SetTimestamp writes a point in time in UTC as serial number.
func (w *Workbook) SetTimestamp(sheet, cell string, t time.Time) error {
	return w.setSerial(sheet, cell, t, TimestampFormat)
}

// SetBool writes a truth value.
func (w *Workbook) SetBool(sheet, cell string, v bool) error {
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		f.SetCellFormula(sheet, n, "")
		return f.SetCellBool(sheet, n, v)
	})
}

// SetFormula writes a formula; the kernel never computes it, so it has
// no stored value until a program opens and saves the workbook.
func (w *Workbook) SetFormula(sheet, cell, formula string) error {
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		return f.SetCellFormula(sheet, n, strings.TrimPrefix(formula, "="))
	})
}

// Clear empties a cell.
func (w *Workbook) Clear(sheet, cell string) error {
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		f.SetCellFormula(sheet, n, "")
		return f.SetCellValue(sheet, n, nil)
	})
}

// SetNumberFormat gives a cell a number format code.
func (w *Workbook) SetNumberFormat(sheet, cell, code string) error {
	return w.set(sheet, cell, func(f *excelize.File, n string) error {
		id, err := w.style(f, &excelize.Style{CustomNumFmt: &code})
		if err != nil {
			return err
		}
		return f.SetCellStyle(sheet, n, n, id)
	})
}

func (w *Workbook) column(sheet, col string) (*excelize.File, string, error) {
	f, err := w.sheet(sheet)
	if err != nil {
		return nil, "", err
	}
	n, err := ColumnNumber(col)
	if err != nil {
		return nil, "", err
	}
	name, _ := ColumnName(n)
	return f, name, nil
}

// SetColumnFormat gives a column a number format code.
func (w *Workbook) SetColumnFormat(sheet, col, code string) error {
	f, c, err := w.column(sheet, col)
	if err != nil {
		return err
	}
	id, err := w.style(f, &excelize.Style{CustomNumFmt: &code})
	if err == nil {
		err = f.SetColStyle(sheet, c, id)
	}
	if err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// SetColumnWidth sets the width of a column in characters.
func (w *Workbook) SetColumnWidth(sheet, col string, width float64) error {
	f, c, err := w.column(sheet, col)
	if err != nil {
		return err
	}
	if err := f.SetColWidth(sheet, c, c, width); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

func (w *Workbook) area(sheet, rng string) (*excelize.File, string, string, error) {
	f, err := w.sheet(sheet)
	if err != nil {
		return nil, "", "", err
	}
	a, b, _ := strings.Cut(rng, ":")
	if b == "" {
		b = a
	}
	for _, c := range []string{a, b} {
		if _, _, err := ref(c); err != nil {
			return nil, "", "", err
		}
	}
	return f, strings.ToUpper(a), strings.ToUpper(b), nil
}

// SetBold makes the cells of a range such as A1:D1 bold.
func (w *Workbook) SetBold(sheet, rng string) error {
	f, a, b, err := w.area(sheet, rng)
	if err != nil {
		return err
	}
	id, err := w.style(f, &excelize.Style{Font: &excelize.Font{Bold: true}})
	if err == nil {
		err = f.SetCellStyle(sheet, a, b, id)
	}
	if err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// FreezeRows keeps the first rows of a sheet in view.
func (w *Workbook) FreezeRows(sheet string, rows int) error {
	f, err := w.sheet(sheet)
	if err != nil {
		return err
	}
	top, _ := excelize.CoordinatesToCellName(1, rows+1)
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: rows, TopLeftCell: top, ActivePane: "bottomLeft"}); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// AutoFilter puts a filter over a range such as A1:D20.
func (w *Workbook) AutoFilter(sheet, rng string) error {
	f, a, b, err := w.area(sheet, rng)
	if err != nil {
		return err
	}
	if err := f.AutoFilter(sheet, a+":"+b, nil); err != nil {
		return fail(Invalid, "%v", err)
	}
	return nil
}

// numberFormats reads the codes of the number formats of a file.
func numberFormats(data []byte) map[int]string {
	out := map[int]string{}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return out
	}
	for _, f := range zr.File {
		if f.Name != "xl/styles.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return out
		}
		defer r.Close()
		var doc struct {
			NumFmts []struct {
				ID   int    `xml:"numFmtId,attr"`
				Code string `xml:"formatCode,attr"`
			} `xml:"numFmts>numFmt"`
		}
		if xml.NewDecoder(r).Decode(&doc) == nil {
			for _, n := range doc.NumFmts {
				out[n.ID] = n.Code
			}
		}
	}
	return out
}
