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

// Package xlsx reads and writes workbooks in xlsx format. It hides the
// library behind its own types: a workbook for access cell by cell, a
// reader that goes through a sheet row by row and a writer for whole
// sheets, all under limits against archives that unpack to more than
// they claim. Values map exactly: decimals never go through float64, an
// empty cell is null and a cell with empty text is "".
package xlsx

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind is the kind of a cell.
type Kind string

// Kinds of cells.
const (
	Empty   Kind = "EMPTY"
	Text    Kind = "TEXT"
	Number  Kind = "NUMBER"
	Bool    Kind = "BOOL"
	Date    Kind = "DATE"
	Formula Kind = "FORMULA"
	Error   Kind = "ERROR"
)

// Cell is a cell as stored: the type attribute, the raw value, the
// formula and whether its number format is one of dates.
type Cell struct {
	Type       string // t: s, inlineStr, str, b, e, d, n or ""
	Raw        string // the stored value, the text of shared strings resolved
	HasValue   bool   // there is a value, also an empty text
	Formula    string
	DateFormat bool
}

// Kind returns the kind of the cell.
func (c Cell) Kind() Kind {
	switch {
	case c.Formula != "":
		return Formula
	case !c.HasValue:
		return Empty
	}
	switch c.Type {
	case "s", "inlineStr", "str":
		return Text
	case "b":
		return Bool
	case "e":
		return Error
	case "d":
		return Date
	}
	if c.DateFormat {
		return Date
	}
	return Number
}

// Mismatch reports a value that does not fit the requested type.
type Mismatch struct {
	Expected string
	Raw      string
	Reason   string
}

func (m *Mismatch) Error() string {
	return fmt.Sprintf("%q is no %s: %s", m.Raw, m.Expected, m.Reason)
}

func mismatch(c Cell, expected, format string, args ...any) error {
	return &Mismatch{Expected: expected, Raw: c.Raw, Reason: fmt.Sprintf(format, args...)}
}

// value returns whether a typed read has a value: false for empty cells,
// an error for errors and formulas without a stored value.
func (c Cell) value(expected string) (bool, error) {
	switch {
	case c.Formula != "" && !c.HasValue:
		return false, mismatch(c, expected, "the formula has no stored value")
	case !c.HasValue:
		return false, nil
	case c.Type == "e":
		return false, mismatch(c, expected, "the cell holds an error")
	}
	return true, nil
}

func (c Cell) text() bool { return c.Type == "s" || c.Type == "inlineStr" || c.Type == "str" }

// TextValue reads a cell as text: text as it is, numbers in their stored
// form, truth values as TRUE or FALSE.
func (c Cell) TextValue() (string, bool, error) {
	ok, err := c.value("text")
	if !ok || err != nil {
		return "", false, err
	}
	if c.Type == "b" {
		if c.Raw == "1" {
			return "TRUE", true, nil
		}
		return "FALSE", true, nil
	}
	return c.Raw, true, nil
}

// DecimalValue reads a number, or text that is a decimal number, with at
// most scale decimal places and precision digits; see Decimal.
func (c Cell) DecimalValue(precision, scale int) (string, bool, error) {
	ok, err := c.value("decimal")
	if !ok || err != nil {
		return "", false, err
	}
	if c.Type == "b" || c.Type == "d" {
		return "", false, mismatch(c, "decimal", "the cell holds no number")
	}
	s, err := Decimal(c.Raw, precision, scale)
	if err != nil {
		return "", false, mismatch(c, "decimal", "%v", err)
	}
	return s, true, nil
}

// IntegerValue reads a whole number from a number or a text.
func (c Cell) IntegerValue() (int64, bool, error) {
	s, ok, err := c.DecimalValue(19, 0)
	if !ok || err != nil {
		if m, isM := err.(*Mismatch); isM {
			m.Expected = "integer"
		}
		return 0, false, err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false, mismatch(c, "integer", "it is out of range")
	}
	return n, true, nil
}

// BoolValue reads a truth value from a truth value cell, or from text
// TRUE, FALSE, WAHR or FALSCH when text is allowed.
func (c Cell) BoolValue(text bool) (bool, bool, error) {
	ok, err := c.value("truth value")
	if !ok || err != nil {
		return false, false, err
	}
	if c.Type == "b" {
		return c.Raw == "1", true, nil
	}
	if text && c.text() {
		switch strings.ToUpper(strings.TrimSpace(c.Raw)) {
		case "TRUE", "WAHR":
			return true, true, nil
		case "FALSE", "FALSCH":
			return false, true, nil
		}
	}
	return false, false, mismatch(c, "truth value", "the cell holds no truth value")
}

// DateValue reads a date from a serial number of the date system or ISO
// text YYYY-MM-DD.
func (c Cell) DateValue(date1904 bool) (time.Time, bool, error) {
	ok, err := c.value("date")
	if !ok || err != nil {
		return time.Time{}, false, err
	}
	if c.text() || c.Type == "d" {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(c.Raw)); err == nil {
			return t, true, nil
		}
		if t, err := parseTimestamp(c.Raw); err == nil && t.Equal(t.Truncate(24*time.Hour)) {
			return t, true, nil
		}
		return time.Time{}, false, mismatch(c, "date", "the text is no date YYYY-MM-DD")
	}
	if c.Type == "b" {
		return time.Time{}, false, mismatch(c, "date", "the cell holds a truth value")
	}
	t, err := FromSerial(c.Raw, date1904)
	if err != nil {
		return time.Time{}, false, mismatch(c, "date", "%v", err)
	}
	if !t.Equal(t.Truncate(24 * time.Hour)) {
		return time.Time{}, false, mismatch(c, "date", "it has a time of day")
	}
	return t, true, nil
}

// TimestampValue reads a point in time in UTC from a serial number or
// ISO text.
func (c Cell) TimestampValue(date1904 bool) (time.Time, bool, error) {
	ok, err := c.value("timestamp")
	if !ok || err != nil {
		return time.Time{}, false, err
	}
	if c.text() || c.Type == "d" {
		t, err := parseTimestamp(c.Raw)
		if err != nil {
			return time.Time{}, false, mismatch(c, "timestamp", "the text is no ISO 8601 time")
		}
		return t, true, nil
	}
	if c.Type == "b" {
		return time.Time{}, false, mismatch(c, "timestamp", "the cell holds a truth value")
	}
	t, err := FromSerial(c.Raw, date1904)
	if err != nil {
		return time.Time{}, false, mismatch(c, "timestamp", "%v", err)
	}
	return t, true, nil
}

func parseTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Round(time.Microsecond), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is no ISO 8601 time", s)
}

var numberPattern = regexp.MustCompile(`^([+-]?)([0-9]*)(?:\.([0-9]*))?(?:[eE]([+-]?[0-9]+))?$`)

// digits parses a decimal number, also with exponent, exactly into
// digits D and exponent E with value D × 10^E and no trailing zeros in D.
func digits(raw string) (neg bool, d *big.Int, e int, err error) {
	m := numberPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil || m[2] == "" && m[3] == "" {
		return false, nil, 0, fmt.Errorf("%q is no number", raw)
	}
	d, _ = new(big.Int).SetString(m[2]+m[3]+"0", 10)
	d.Quo(d, big.NewInt(10))
	e = -len(m[3])
	if m[4] != "" {
		x, err := strconv.Atoi(m[4])
		if err != nil || x > 400 || x < -400 {
			return false, nil, 0, fmt.Errorf("%q is out of range", raw)
		}
		e += x
	}
	ten := big.NewInt(10)
	r := new(big.Int)
	for d.Sign() != 0 {
		q, rem := new(big.Int).QuoRem(d, ten, r)
		if rem.Sign() != 0 {
			break
		}
		d, e = q, e+1
	}
	if d.Sign() == 0 {
		e = 0
	}
	return m[1] == "-", d, e, nil
}

// SignificantDigits is the precision with which the programs store
// numbers: digits beyond it are artefacts of the binary representation.
const SignificantDigits = 15

// Decimal returns a stored number as canonical decimal text with scale
// decimal places. The rule: the exact value counts if it has at most
// scale decimal places; otherwise the value rounded to 15 significant
// digits (half away from zero) counts if that has at most scale places,
// which removes artefacts such as 0.30000000000000004; otherwise the
// number has more decimal places than allowed. More than precision -
// scale digits before the point are an error as well.
func Decimal(raw string, precision, scale int) (string, error) {
	neg, d, e, err := digits(raw)
	if err != nil {
		return "", err
	}
	if -e > scale {
		n := len(d.String())
		if n > SignificantDigits {
			cut := n - SignificantDigits
			p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(cut)), nil)
			q, r := new(big.Int).QuoRem(d, p, new(big.Int))
			if new(big.Int).Mul(r, big.NewInt(2)).Cmp(p) >= 0 {
				q.Add(q, big.NewInt(1))
			}
			d, e = q, e+cut
			_, d, e, _ = digits(d.String() + "e" + strconv.Itoa(e))
		}
		if -e > scale {
			return "", fmt.Errorf("it has %d decimal places, more than %d", -e, scale)
		}
	}
	// Scale the digits to scale decimal places.
	s := d.String()
	if e >= 0 {
		s += strings.Repeat("0", e+scale)
	} else {
		s += strings.Repeat("0", scale+e)
	}
	if len(s) <= scale {
		s = strings.Repeat("0", scale-len(s)+1) + s
	}
	intPart, frac := s[:len(s)-scale], s[len(s)-scale:]
	intPart = strings.TrimLeft(intPart, "0")
	if len(intPart) > precision-scale {
		return "", fmt.Errorf("it has %d digits before the point, more than %d", len(intPart), precision-scale)
	}
	if intPart == "" {
		intPart = "0"
	}
	out := intPart
	if scale > 0 {
		out += "." + frac
	}
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out, nil
}

// LongDecimal reports a decimal with more significant digits than the
// programs keep exactly.
func LongDecimal(text string) bool {
	_, d, _, err := digits(text)
	return err == nil && len(d.String()) > SignificantDigits
}

var (
	epoch1900 = time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	epoch1904 = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
)

const (
	microsPerDay = 86_400_000_000
	millisPerDay = 86_400_000
)

// FromSerial turns a serial number of the 1900 or 1904 date system into
// a time in UTC, rounded to milliseconds: serial numbers keep 15
// significant digits, which are about ten microseconds for current
// dates. The 1900 system counts the
// 29th of February 1900 that never was; serial 60 is rejected.
func FromSerial(raw string, date1904 bool) (time.Time, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	if !ok {
		return time.Time{}, fmt.Errorf("%q is no serial number", raw)
	}
	micros := new(big.Rat).Mul(r, big.NewRat(millisPerDay, 1))
	n := new(big.Int).Quo(new(big.Int).Add(new(big.Int).Mul(micros.Num(), big.NewInt(2)), micros.Denom()), new(big.Int).Mul(micros.Denom(), big.NewInt(2)))
	if !n.IsInt64() || n.Sign() < 0 || n.Int64() > 2_958_466*millisPerDay {
		return time.Time{}, fmt.Errorf("%s is out of the range of dates", raw)
	}
	us := n.Int64() * 1000
	if date1904 {
		return epoch1904.Add(time.Duration(us) * time.Microsecond), nil
	}
	day := us / microsPerDay
	switch {
	case day == 60:
		return time.Time{}, fmt.Errorf("the serial number 60 names the 29th of February 1900, which does not exist")
	case day > 60:
		us -= microsPerDay
	case day == 0:
		return time.Time{}, fmt.Errorf("the serial number 0 names no date of the 1900 system")
	}
	return epoch1900.Add(time.Duration(us) * time.Microsecond), nil
}

// Serial turns a time into a serial number of the date system, as
// decimal text with milliseconds.
func Serial(t time.Time, date1904 bool) string {
	t = t.UTC().Round(time.Millisecond)
	var us int64
	if date1904 {
		us = t.Sub(epoch1904).Microseconds()
	} else {
		us = t.Sub(epoch1900).Microseconds()
		if us >= 60*microsPerDay {
			us += microsPerDay
		}
	}
	r := new(big.Rat).SetFrac(big.NewInt(us), big.NewInt(microsPerDay))
	s := r.FloatString(12)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

// dateFormats are the built-in number formats of dates and times.
var dateFormats = map[int]bool{14: true, 15: true, 16: true, 17: true, 18: true, 19: true, 20: true, 21: true, 22: true,
	27: true, 28: true, 29: true, 30: true, 31: true, 32: true, 33: true, 34: true, 35: true, 36: true,
	45: true, 46: true, 47: true, 50: true, 51: true, 52: true, 53: true, 54: true, 55: true, 56: true, 57: true, 58: true}

var quoted = regexp.MustCompile(`"[^"]*"|\\.|\[[^\]]*\]`)

// IsDateFormat reports whether a number format shows dates or times.
func IsDateFormat(id int, code string) bool {
	if dateFormats[id] {
		return true
	}
	if code == "" {
		return false
	}
	c := strings.ToLower(quoted.ReplaceAllString(code, ""))
	if i := strings.Index(c, ";"); i >= 0 {
		c = c[:i]
	}
	return strings.ContainsAny(c, "dmyhs") && !strings.Contains(c, "general")
}
