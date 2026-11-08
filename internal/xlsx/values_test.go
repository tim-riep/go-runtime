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
	"strings"
	"testing"
	"time"
)

// TestDecimal reads stored numbers exactly and rounds only artefacts of
// the binary representation.
func TestDecimal(t *testing.T) {
	for _, c := range []struct {
		raw        string
		p, s       int
		want, fail string
	}{
		{"0.30000000000000004", 15, 2, "0.30", ""},
		{"12.5", 15, 2, "12.50", ""},
		{"1.005", 15, 2, "", "3 decimal places"},
		{"1.0049999999999999", 15, 2, "", "decimal places"},
		{"-0.1", 5, 1, "-0.1", ""},
		{"1.5E-3", 10, 4, "0.0015", ""},
		{"1.2345678901234568E+18", 19, 0, "1234567890123456800", ""},
		{"123456", 5, 2, "", "digits before the point"},
		{"99.999999999999986", 15, 2, "100.00", ""},
		{"0", 5, 2, "0.00", ""},
		{"-0", 5, 2, "0.00", ""},
		{"abc", 5, 2, "", "no number"},
		{"1e", 5, 2, "", "no number"},
	} {
		got, err := Decimal(c.raw, c.p, c.s)
		switch {
		case c.fail == "" && (err != nil || got != c.want):
			t.Errorf("%s: %q %v, want %q", c.raw, got, err, c.want)
		case c.fail != "" && (err == nil || !strings.Contains(err.Error(), c.fail)):
			t.Errorf("%s: %q %v, want error %q", c.raw, got, err, c.fail)
		}
	}
	if !LongDecimal("1234567890.1234567") || LongDecimal("123.45") {
		t.Error("long decimals")
	}
}

// TestSerial converts dates of both date systems and back.
func TestSerial(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	for _, c := range []struct {
		serial string
		d1904  bool
		want   time.Time
	}{
		{"1", false, day("1900-01-01")}, {"59", false, day("1900-02-28")}, {"61", false, day("1900-03-01")},
		{"45969", false, day("2025-11-08")}, {"44507", true, day("2025-11-08")}, {"0", true, day("1904-01-01")},
		{"45969.5", false, day("2025-11-08").Add(12 * time.Hour)}, {"2958465", false, day("9999-12-31")}, {"2957003", true, day("9999-12-31")},
	} {
		got, err := FromSerial(c.serial, c.d1904)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("%s (%v): %v %v, want %v", c.serial, c.d1904, got, err, c.want)
		}
		if back := Serial(c.want, c.d1904); back != c.serial {
			t.Errorf("serial of %v (%v) = %s, want %s", c.want, c.d1904, back, c.serial)
		}
	}
	if _, err := FromSerial("60", false); err == nil {
		t.Error("the 29th of February 1900")
	}
	ts := time.Date(2026, 11, 8, 10, 11, 12, 123000000, time.UTC)
	if got, err := FromSerial(Serial(ts, false), false); err != nil || !got.Equal(ts) {
		t.Errorf("microseconds: %v %v", got, err)
	}
	if !IsDateFormat(14, "") || !IsDateFormat(164, "dd.mm.yyyy") || IsDateFormat(164, `0.00 "days"`) || IsDateFormat(2, "0.00") || !IsDateFormat(164, "[$-407]d. mmmm yyyy") {
		t.Error("date formats")
	}
}

// TestCellValues reads null, "", numbers, text, truth values and
// formulas without stored values.
func TestCellValues(t *testing.T) {
	empty := Cell{}
	if _, ok, err := empty.TextValue(); ok || err != nil || empty.Kind() != Empty {
		t.Error("empty cell")
	}
	blank := Cell{Type: "s", HasValue: true}
	if s, ok, err := blank.TextValue(); !ok || err != nil || s != "" || blank.Kind() != Text {
		t.Error("empty text")
	}
	if _, _, err := (Cell{Type: "s", Raw: "abc", HasValue: true}).DecimalValue(15, 2); err == nil {
		t.Error("text as decimal")
	}
	if v, ok, err := (Cell{Type: "s", Raw: "12.5", HasValue: true}).DecimalValue(15, 2); !ok || err != nil || v != "12.50" {
		t.Error("decimal text")
	}
	if _, _, err := (Cell{Formula: "A1*2"}).IntegerValue(); err == nil {
		t.Error("formula without value")
	}
	if n, ok, err := (Cell{Formula: "A1*2", Raw: "4", HasValue: true}).IntegerValue(); !ok || err != nil || n != 4 {
		t.Error("stored value of a formula")
	}
	if _, _, err := (Cell{Type: "s", Raw: "TRUE", HasValue: true}).BoolValue(false); err == nil {
		t.Error("text as truth value")
	}
	if b, ok, _ := (Cell{Type: "s", Raw: "wahr", HasValue: true}).BoolValue(true); !ok || !b {
		t.Error("text as truth value when allowed")
	}
	if _, _, err := (Cell{Type: "e", Raw: "#DIV/0!", HasValue: true}).TextValue(); err == nil {
		t.Error("error cell")
	}
	if d, ok, err := (Cell{Raw: "45969", HasValue: true, DateFormat: true}).DateValue(false); !ok || err != nil || d.Format("2006-01-02") != "2025-11-08" {
		t.Errorf("date: %v %v", d, err)
	}
	if _, _, err := (Cell{Raw: "45969.25", HasValue: true}).DateValue(false); err == nil {
		t.Error("date with time")
	}
	if ts, ok, err := (Cell{Type: "s", Raw: "2026-11-08T10:00:00+01:00", HasValue: true}).TimestampValue(false); !ok || err != nil || ts.Hour() != 9 {
		t.Errorf("timestamp text: %v %v", ts, err)
	}
}
