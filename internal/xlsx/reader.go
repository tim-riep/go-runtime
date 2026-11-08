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
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/internal/archive"
	"github.com/tim-riep/go-runtime/native"
)

// Reader goes through the rows of a sheet as a stream. Unlike the
// workbook it keeps only the shared strings and the styles in memory, and
// it reads the type of every cell, so that an empty cell, an empty text,
// a truth value and a number stay apart.
type Reader struct {
	ctx      context.Context
	l        native.WorkbookLimits
	meter    *archive.Meter
	parts    map[string]*zip.File
	sheets   []sheetRef
	date1904 bool
	shared   []string
	dates    []bool // by style index: the number format is one of dates
}

type sheetRef struct{ name, part string }

// NewReader opens a workbook for reading row by row.
func NewReader(ctx context.Context, data []byte, l native.WorkbookLimits) (*Reader, error) {
	if bytes.HasPrefix(data, cfb) {
		return nil, fail(Protected, "the workbook is encrypted with a password")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fail(Invalid, "the data is no workbook: %v", err)
	}
	if len(zr.File) > l.Archive.Entries {
		return nil, fail(LimitExceeded, "the workbook has %d parts, more than the limit of %d", len(zr.File), l.Archive.Entries)
	}
	r := &Reader{ctx: ctx, l: l, meter: archive.NewMeter(ctx, l.Archive), parts: map[string]*zip.File{}}
	for _, f := range zr.File {
		if f.Flags&1 != 0 {
			return nil, fail(Protected, "the part %s is encrypted", f.Name)
		}
		r.parts[strings.TrimPrefix(f.Name, "/")] = f
	}
	main := "xl/workbook.xml"
	for _, rel := range r.rels("_rels/.rels") {
		if strings.HasSuffix(rel.Type, "/officeDocument") {
			main = target("", rel.Target)
		}
	}
	if r.parts[main] == nil {
		return nil, fail(Invalid, "the data is no workbook: it has no part %s", main)
	}
	if err := r.workbook(main); err != nil {
		return nil, err
	}
	if len(r.sheets) > l.Sheets {
		return nil, fail(LimitExceeded, "the workbook has %d sheets, more than the limit of %d", len(r.sheets), l.Sheets)
	}
	return r, nil
}

// Sheets returns the names of the sheets in order.
func (r *Reader) Sheets() []string {
	out := make([]string, len(r.sheets))
	for i, s := range r.sheets {
		out[i] = s.name
	}
	return out
}

// Date1904 tells whether the workbook counts dates from 1904.
func (r *Reader) Date1904() bool { return r.date1904 }

type relationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Mode   string `xml:"TargetMode,attr"`
}

// open opens a part under the limits.
func (r *Reader) open(name string) (io.Reader, error) {
	f := r.parts[name]
	if f == nil {
		return nil, fail(Invalid, "the workbook has no part %s", name)
	}
	if err := r.meter.Entry(); err != nil {
		return nil, limitErr(err)
	}
	rc, err := r.meter.OpenEntry(f)
	if err != nil {
		return nil, fail(Invalid, "the part %s cannot be read: %v", name, err)
	}
	return rc, nil
}

// decode reads a whole small part into v.
func (r *Reader) decode(name string, v any) error {
	rc, err := r.open(name)
	if err != nil {
		return err
	}
	if err := xml.NewDecoder(rc).Decode(v); err != nil {
		return partErr(name, err)
	}
	return nil
}

func partErr(name string, err error) error {
	var le *archive.LimitError
	if errors.As(err, &le) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, archive.ErrCorrupt) {
		return limitErr(err)
	}
	return fail(Invalid, "the part %s is damaged: %v", name, err)
}

// rels reads the relationships of a part; missing ones are none.
func (r *Reader) rels(name string) []relationship {
	if r.parts[name] == nil {
		return nil
	}
	var doc struct {
		Rels []relationship `xml:"Relationship"`
	}
	if r.decode(name, &doc) != nil {
		return nil
	}
	return doc.Rels
}

// target resolves the target of a relationship of a part in dir.
func target(dir, t string) string {
	if strings.HasPrefix(t, "/") {
		return strings.TrimPrefix(path.Clean(t), "/")
	}
	return strings.TrimPrefix(path.Clean(path.Join(dir, t)), "/")
}

// workbook reads the sheets, the date system, the shared strings and the
// number formats of the styles.
func (r *Reader) workbook(main string) error {
	var doc struct {
		Pr struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name string     `xml:"name,attr"`
			Attr []xml.Attr `xml:",any,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := r.decode(main, &doc); err != nil {
		return err
	}
	r.date1904 = doc.Pr.Date1904 == "1" || doc.Pr.Date1904 == "true"
	dir := path.Dir(main)
	relsName := path.Join(dir, "_rels", path.Base(main)+".rels")
	byID := map[string]string{}
	var sharedPart, stylesPart string
	for _, rel := range r.rels(relsName) {
		if rel.Mode == "External" {
			continue
		}
		t := target(dir, rel.Target)
		byID[rel.ID] = t
		switch {
		case strings.HasSuffix(rel.Type, "/sharedStrings"):
			sharedPart = t
		case strings.HasSuffix(rel.Type, "/styles"):
			stylesPart = t
		}
	}
	for _, s := range doc.Sheets {
		ref := sheetRef{name: s.Name}
		for _, a := range s.Attr {
			if a.Name.Local == "id" {
				ref.part = byID[a.Value]
			}
		}
		r.sheets = append(r.sheets, ref)
	}
	if sharedPart != "" && r.parts[sharedPart] != nil {
		if err := r.sharedStrings(sharedPart); err != nil {
			return err
		}
	}
	if stylesPart != "" && r.parts[stylesPart] != nil {
		if err := r.styles(stylesPart); err != nil {
			return err
		}
	}
	return nil
}

// sharedStrings reads the table of shared strings; rich text is joined,
// phonetic hints are left out.
func (r *Reader) sharedStrings(name string) error {
	rc, err := r.open(name)
	if err != nil {
		return err
	}
	d := xml.NewDecoder(rc)
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return partErr(name, err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "si" {
			s, err := richText(d, "si")
			if err != nil {
				return partErr(name, err)
			}
			r.shared = append(r.shared, s)
		}
	}
}

// richText reads the text of an si or is element up to its end.
func richText(d *xml.Decoder, end string) (string, error) {
	var b strings.Builder
	depth, skip := 0, 0
	inT := false
	for {
		tok, err := d.RawToken()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case t.Name.Local == "rPh":
				skip = depth
			case t.Name.Local == "t" && skip == 0:
				inT = true
			}
		case xml.EndElement:
			if depth == 0 && t.Name.Local == end {
				return unescape(b.String()), nil
			}
			if skip == depth {
				skip = 0
			}
			if t.Name.Local == "t" {
				inT = false
			}
			depth--
		case xml.CharData:
			if inT {
				b.Write(t)
			}
		}
	}
}

// escaped is a character in the escape form of the file format.
var escaped = regexp.MustCompile(`_x[0-9A-Fa-f]{4}_`)

// unescape turns _xHHHH_ into its character; _x005F_ stands for the
// underscore of a literal escape form.
func unescape(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	return escaped.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.ParseUint(m[2:6], 16, 16)
		return string(rune(n))
	})
}

// styles reads which cell styles have a number format of dates.
func (r *Reader) styles(name string) error {
	var doc struct {
		NumFmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			NumFmt int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := r.decode(name, &doc); err != nil {
		return err
	}
	codes := map[int]string{}
	for _, n := range doc.NumFmts {
		codes[n.ID] = n.Code
	}
	for _, x := range doc.Xfs {
		r.dates = append(r.dates, IsDateFormat(x.NumFmt, codes[x.NumFmt]))
	}
	return nil
}

// Rows calls fn for every row of a sheet that has values or formulas,
// with the number of the row and its cells by column up to the last one
// with a value, the first at index 0; columns without a value are Cell{}. The slice is reused between calls. The empty
// name is the first sheet.
func (r *Reader) Rows(sheet string, fn func(row int, cells []Cell) error) error {
	var part string
	for _, s := range r.sheets {
		if s.name == sheet || sheet == "" {
			part = s.part
			sheet = s.name
			break
		}
	}
	if sheet == "" || part == "" {
		if sheet == "" {
			return &Err{Kind: SheetNotFound, Message: "the workbook has no sheet"}
		}
		return &Err{Kind: SheetNotFound, Sheet: sheet, Message: "the workbook has no sheet " + strconv.Quote(sheet)}
	}
	rc, err := r.open(part)
	if err != nil {
		return err
	}
	d := xml.NewDecoder(rc)
	var (
		cells      []Cell
		row, col   int
		inRow      bool
		count      int64
		c          Cell
		cellCol    int
		inV, inF   bool
		v, f       strings.Builder
		hasF, hasV bool
	)
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return partErr(part, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				n := row + 1
				if a := attr(t, "r"); a != "" {
					if n, err = strconv.Atoi(a); err != nil || n <= row {
						return fail(Invalid, "the sheet %s has a row with the invalid number %q", sheet, a)
					}
				}
				if n > r.l.Rows {
					return fail(LimitExceeded, "the sheet %s has more than the limit of %d rows", sheet, r.l.Rows)
				}
				row, col, inRow = n, 0, true
				cells = cells[:0]
			case "c":
				if !inRow {
					continue
				}
				cellCol = col + 1
				if a := attr(t, "r"); a != "" {
					cc, rr, err := ref(a)
					if err != nil || rr != row || cc <= col {
						return fail(Invalid, "the sheet %s has a cell with the invalid reference %q in row %d", sheet, a, row)
					}
					cellCol = cc
				}
				if cellCol > r.l.Columns {
					return fail(LimitExceeded, "the sheet %s has more than the limit of %d columns", sheet, r.l.Columns)
				}
				if count++; count > r.l.Cells {
					return fail(LimitExceeded, "the sheet %s has more than the limit of %d cells", sheet, r.l.Cells)
				}
				c = Cell{Type: attr(t, "t")}
				if s := attr(t, "s"); s != "" {
					if i, err := strconv.Atoi(s); err == nil && i >= 0 && i < len(r.dates) {
						c.DateFormat = r.dates[i]
					}
				}
				v.Reset()
				f.Reset()
				hasF, hasV = false, false
			case "v":
				inV, hasV = true, true
			case "f":
				inF, hasF = true, true
			case "is":
				s, err := richText(d, "is")
				if err != nil {
					return partErr(part, err)
				}
				v.WriteString(s)
				hasV = true
			}
		case xml.CharData:
			switch {
			case inV:
				v.Write(t)
			case inF:
				f.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "f":
				inF = false
			case "c":
				if !inRow {
					continue
				}
				c.Raw, c.HasValue = v.String(), hasV
				if hasF {
					// A cell in a group of a shared formula has no text of
					// its own.
					c.Formula = f.String()
					if c.Formula == "" {
						c.Formula = "="
					}
				}
				switch c.Type {
				case "s":
					i, err := strconv.Atoi(c.Raw)
					if err != nil || i < 0 || i >= len(r.shared) {
						return fail(Invalid, "the sheet %s refers to the missing shared string %q", sheet, c.Raw)
					}
					c.Raw = r.shared[i]
				case "str":
					c.Raw = unescape(c.Raw)
				}
				col = cellCol
				if !c.HasValue && !hasF {
					continue
				}
				for len(cells) < cellCol-1 {
					cells = append(cells, Cell{})
				}
				cells = append(cells, c)
			case "row":
				inRow = false
				if len(cells) > 0 {
					if err := fn(row, cells); err != nil {
						return err
					}
				}
			}
		}
	}
}

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}
