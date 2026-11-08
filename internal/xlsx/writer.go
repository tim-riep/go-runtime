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
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/native"
)

// Column is a column that the writer writes: its heading, the number
// format of its values and its width in characters.
type Column struct {
	Header string
	Format string // a number format code; "" is the general format
	Width  float64
}

// Numeric is the decimal text of a number that the writer writes as it
// is; it holds at most 15 significant digits.
type Numeric string

// MaxText is the largest number of UTF-16 code units of a cell text.
const MaxText = 32767

// Writer writes one sheet of a new workbook row by row: a bold heading
// that stays visible when scrolling, an autofilter over the data and the
// number format of every column. Text is kept exactly: characters that
// XML cannot hold are written in the escape form of the file format, and
// text that does not fit a cell is an error, never cut.
type Writer struct {
	l      native.WorkbookLimits
	name   string
	cols   []Column
	styles []int // style of the values by column
	codes  []string
	out    bytes.Buffer
	zw     *zip.Writer
	sheet  *bufio.Writer
	row    int
	cells  int64
	d1904  bool
}

// NewWriter starts a workbook with one sheet and writes the heading.
func NewWriter(sheet string, date1904 bool, cols []Column, l native.WorkbookLimits) (*Writer, error) {
	if err := sheetName(sheet); err != nil {
		return nil, err
	}
	if len(cols) > l.Columns {
		return nil, fail(LimitExceeded, "the sheet has %d columns, more than the limit of %d", len(cols), l.Columns)
	}
	w := &Writer{l: l, name: sheet, cols: cols, d1904: date1904}
	for _, c := range cols {
		s := 0
		if c.Format != "" {
			i := 0
			for i < len(w.codes) && w.codes[i] != c.Format {
				i++
			}
			if i == len(w.codes) {
				w.codes = append(w.codes, c.Format)
			}
			s = 2 + i
		}
		w.styles = append(w.styles, s)
	}
	w.zw = zip.NewWriter(&w.out)
	f, err := w.zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	w.sheet = bufio.NewWriterSize(f, 64<<10)
	w.sheet.WriteString(xml10 + `<worksheet xmlns="` + nsMain + `" xmlns:r="` + nsRel + `">`)
	w.sheet.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>` +
		`<selection pane="bottomLeft"/></sheetView></sheetViews><sheetFormatPr defaultRowHeight="15"/>`)
	if len(cols) > 0 {
		w.sheet.WriteString(`<cols>`)
		for i, c := range cols {
			fmt.Fprintf(w.sheet, `<col min="%d" max="%d" width="%s" customWidth="1"/>`, i+1, i+1, strconv.FormatFloat(c.Width, 'f', 2, 64))
		}
		w.sheet.WriteString(`</cols>`)
	}
	w.sheet.WriteString(`<sheetData>`)
	head := make([]any, len(cols))
	for i, c := range cols {
		head[i] = c.Header
	}
	if err := w.write(head, true); err != nil {
		return nil, err
	}
	return w, nil
}

// sheetName checks the name of a sheet.
func sheetName(s string) error {
	switch {
	case s == "":
		return fail(Invalid, "the name of a sheet is empty")
	case len(utf16.Encode([]rune(s))) > 31:
		return fail(Invalid, "the name of the sheet %q is longer than 31 characters", s)
	case strings.ContainsAny(s, `[]:*?/\`):
		return fail(Invalid, "the name of the sheet %q contains one of the characters [ ] : * ? / \\", s)
	case strings.HasPrefix(s, "'") || strings.HasSuffix(s, "'"):
		return fail(Invalid, "the name of the sheet %q starts or ends with an apostrophe", s)
	}
	for _, r := range s {
		if r < 0x20 {
			return fail(Invalid, "the name of the sheet %q contains a control character", s)
		}
	}
	return nil
}

// Row writes a row of values by column: nil writes no cell, string a
// text, Numeric a number, int64 a whole number, bool a truth value and
// time.Time a serial number of the date system.
func (w *Writer) Row(vals []any) error { return w.write(vals, false) }

func (w *Writer) write(vals []any, head bool) error {
	if len(vals) > len(w.cols) {
		return fmt.Errorf("xlsx: the row has %d values for %d columns", len(vals), len(w.cols))
	}
	if w.row++; w.row > w.l.Rows {
		return fail(LimitExceeded, "the sheet has more than the limit of %d rows", w.l.Rows)
	}
	b := w.sheet
	b.WriteString(`<row r="`)
	b.WriteString(strconv.Itoa(w.row))
	b.WriteString(`">`)
	for i, v := range vals {
		if v == nil {
			continue
		}
		if w.cells++; w.cells > w.l.Cells {
			return fail(LimitExceeded, "the sheet has more than the limit of %d cells", w.l.Cells)
		}
		col, _ := ColumnName(i + 1)
		b.WriteString(`<c r="`)
		b.WriteString(col)
		b.WriteString(strconv.Itoa(w.row))
		b.WriteByte('"')
		style := w.styles[i]
		if head {
			style = 1
		}
		if style != 0 {
			b.WriteString(` s="`)
			b.WriteString(strconv.Itoa(style))
			b.WriteByte('"')
		}
		switch v := v.(type) {
		case string:
			if !utf8.ValidString(v) {
				return fail(Invalid, "the text in %s%d is not valid UTF-8", col, w.row)
			}
			if n := len(utf16.Encode([]rune(v))); n > MaxText {
				e := fail(Invalid, "the text in %s%d has %d characters, more than a cell holds (%d)", col, w.row, n, MaxText)
				e.Cell = col + strconv.Itoa(w.row)
				return e
			}
			b.WriteString(` t="inlineStr"><is><t`)
			if v != strings.TrimSpace(v) {
				b.WriteString(` xml:space="preserve"`)
			}
			b.WriteByte('>')
			escapeText(b, v)
			b.WriteString(`</t></is></c>`)
		case Numeric:
			b.WriteString(`><v>`)
			b.WriteString(string(v))
			b.WriteString(`</v></c>`)
		case int64:
			if v > maxExact || v < -maxExact {
				return fmt.Errorf("xlsx: the whole number %d does not fit a number exactly", v)
			}
			b.WriteString(`><v>`)
			b.WriteString(strconv.FormatInt(v, 10))
			b.WriteString(`</v></c>`)
		case bool:
			b.WriteString(` t="b"><v>`)
			if v {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
			b.WriteString(`</v></c>`)
		case time.Time:
			serial := Serial(v, w.d1904)
			if _, err := FromSerial(serial, w.d1904); err != nil {
				return fail(Invalid, "%s%d: %s cannot be written as a date: %v", col, w.row, v.Format(time.RFC3339Nano), err)
			}
			b.WriteString(`><v>`)
			b.WriteString(serial)
			b.WriteString(`</v></c>`)
		default:
			return fmt.Errorf("xlsx: the writer cannot write %T", v)
		}
	}
	b.WriteString(`</row>`)
	return nil
}

// escapeText writes text as XML in the escape form of the file format:
// control characters as _xHHHH_ and the underscore of a literal escape
// form as _x005F_.
func escapeText(b *bufio.Writer, s string) {
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '\r':
			b.WriteString("&#13;")
		case r == '_' && escaped.MatchString(s[i:min(i+7, len(s))]):
			b.WriteString("_x005F_")
		case r < 0x20 && r != '\t' && r != '\n', r == 0xFFFE, r == 0xFFFF:
			fmt.Fprintf(b, "_x%04X_", r)
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
}

const (
	xml10   = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsMain  = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkg   = "http://schemas.openxmlformats.org/package/2006/relationships"
	relType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	ctMain  = "application/vnd.openxmlformats-officedocument.spreadsheetml."
)

// Close ends the sheet, writes the other parts and returns the workbook.
func (w *Writer) Close() ([]byte, error) {
	last, _ := ColumnName(max(len(w.cols), 1))
	area := fmt.Sprintf("A1:%s%d", last, w.row)
	b := w.sheet
	b.WriteString(`</sheetData>`)
	if len(w.cols) > 0 {
		b.WriteString(`<autoFilter ref="` + area + `"/>`)
	}
	b.WriteString(`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/></worksheet>`)
	if err := b.Flush(); err != nil {
		return nil, err
	}
	quoted := "'" + strings.ReplaceAll(w.name, "'", "''") + "'"
	abs := fmt.Sprintf("$A$1:$%s$%d", last, w.row)
	var name strings.Builder
	bw := bufio.NewWriter(&name)
	escapeText(bw, w.name)
	bw.Flush()
	var filter string
	if len(w.cols) > 0 {
		var q strings.Builder
		qw := bufio.NewWriter(&q)
		escapeText(qw, quoted)
		qw.Flush()
		filter = `<definedNames><definedName name="_xlnm._FilterDatabase" localSheetId="0" hidden="1">` + q.String() + `!` + abs + `</definedName></definedNames>`
	}
	d1904 := ""
	if w.d1904 {
		d1904 = ` date1904="1"`
	}
	var styles strings.Builder
	styles.WriteString(xml10 + `<styleSheet xmlns="` + nsMain + `">`)
	if len(w.codes) > 0 {
		fmt.Fprintf(&styles, `<numFmts count="%d">`, len(w.codes))
		for i, c := range w.codes {
			var e strings.Builder
			ew := bufio.NewWriter(&e)
			escapeText(ew, c)
			ew.Flush()
			fmt.Fprintf(&styles, `<numFmt numFmtId="%d" formatCode="%s"/>`, 164+i, strings.ReplaceAll(e.String(), `"`, "&quot;"))
		}
		styles.WriteString(`</numFmts>`)
	}
	styles.WriteString(`<fonts count="2"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font>` +
		`<font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
		`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
		`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`)
	fmt.Fprintf(&styles, `<cellXfs count="%d"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`+
		`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>`, 2+len(w.codes))
	for i := range w.codes {
		fmt.Fprintf(&styles, `<xf numFmtId="%d" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`, 164+i)
	}
	styles.WriteString(`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", xml10 + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="` + ctMain + `sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="` + ctMain + `worksheet+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="` + ctMain + `styles+xml"/></Types>`},
		{"_rels/.rels", xml10 + `<Relationships xmlns="` + nsPkg + `"><Relationship Id="rId1" Type="` + relType + `officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", xml10 + `<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `"><workbookPr` + d1904 + `/>` +
			`<bookViews><workbookView/></bookViews><sheets><sheet name="` + strings.ReplaceAll(name.String(), `"`, "&quot;") + `" sheetId="1" r:id="rId1"/></sheets>` +
			filter + `</workbook>`},
		{"xl/_rels/workbook.xml.rels", xml10 + `<Relationships xmlns="` + nsPkg + `">` +
			`<Relationship Id="rId1" Type="` + relType + `worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="` + relType + `styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", styles.String()},
	}
	for _, p := range parts {
		f, err := w.zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := w.zw.Close(); err != nil {
		return nil, err
	}
	return w.out.Bytes(), nil
}
