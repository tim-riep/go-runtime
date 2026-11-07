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

package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Text layouts of the canonical value forms.
const (
	DateLayout      = "2006-01-02"
	TimestampLayout = "2006-01-02T15:04:05.000000Z"
)

var (
	decimalPattern = regexp.MustCompile(`^([+-]?)([0-9]+)(?:\.([0-9]+))?$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ParseJSON converts a JSON value written in a package into the canonical
// text form of the domain.
func (d *Domain) ParseJSON(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	switch d.Type {
	case Char, String, Date, Timestamp, UUID:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("expected a string for %s", d.Type)
		}
		return d.Canonical(s)
	case Dec:
		switch x := v.(type) {
		case string:
			return d.Canonical(x)
		case json.Number:
			return d.Canonical(x.String())
		}
		return "", errors.New("expected a decimal string for DEC")
	case Int4, Int8:
		n, ok := v.(json.Number)
		if !ok {
			return "", fmt.Errorf("expected an integer for %s", d.Type)
		}
		return d.Canonical(n.String())
	case Bool:
		b, ok := v.(bool)
		if !ok {
			return "", errors.New("expected true or false for BOOL")
		}
		return strconv.FormatBool(b), nil
	case Blob:
		return "", errors.New("BLOB values cannot be written as literals")
	}
	return "", fmt.Errorf("unknown base type %s", d.Type)
}

// Canonical validates s against the domain's type and returns its canonical
// text form. It does not check fixed values.
func (d *Domain) Canonical(s string) (string, error) {
	switch d.Type {
	case Char:
		if n := utf8.RuneCountInString(s); n > d.Length {
			return "", fmt.Errorf("value has %d characters, CHAR(%d) allows %d", n, d.Length, d.Length)
		}
		return s, nil
	case String:
		return s, nil
	case Int4, Int8:
		bits := 32
		if d.Type == Int8 {
			bits = 64
		}
		n, err := strconv.ParseInt(s, 10, bits)
		if err != nil {
			return "", fmt.Errorf("%q is not a valid %s", s, d.Type)
		}
		return strconv.FormatInt(n, 10), nil
	case Dec:
		return canonicalDecimal(s, d.Length, d.Decimals)
	case Date:
		t, err := time.Parse(DateLayout, s)
		if err != nil {
			return "", fmt.Errorf("%q is not a date in the form YYYY-MM-DD", s)
		}
		return t.Format(DateLayout), nil
	case Timestamp:
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return "", fmt.Errorf("%q is not an RFC 3339 timestamp", s)
		}
		return FormatTimestamp(t)
	case Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return "", fmt.Errorf("%q is not a boolean", s)
		}
		return strconv.FormatBool(b), nil
	case UUID:
		if !uuidPattern.MatchString(s) {
			return "", fmt.Errorf("%q is not a UUID", s)
		}
		return strings.ToLower(s), nil
	case Blob:
		return "", errors.New("BLOB values cannot be written as literals")
	}
	return "", fmt.Errorf("unknown base type %s", d.Type)
}

// FormatTimestamp returns the canonical form of t in UTC with microsecond
// precision. Finer precision is rejected rather than rounded.
func FormatTimestamp(t time.Time) (string, error) {
	if t.Nanosecond()%1000 != 0 {
		return "", errors.New("timestamps support microsecond precision only")
	}
	return t.UTC().Format(TimestampLayout), nil
}

func canonicalDecimal(s string, precision, scale int) (string, error) {
	m := decimalPattern.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("%q is not a decimal number", s)
	}
	sign, intPart, frac := m[1], strings.TrimLeft(m[2], "0"), m[3]
	if len(frac) > scale {
		if strings.Trim(frac[scale:], "0") != "" {
			return "", fmt.Errorf("%q has more than %d decimal places", s, scale)
		}
		frac = frac[:scale]
	}
	frac += strings.Repeat("0", scale-len(frac))
	if len(intPart)+scale > precision {
		return "", fmt.Errorf("%q exceeds DEC(%d,%d)", s, precision, scale)
	}
	if intPart == "" {
		intPart = "0"
	}
	if sign == "+" || strings.Trim(intPart+frac, "0") == "" {
		sign = ""
	}
	out := sign + intPart
	if scale > 0 {
		out += "." + frac
	}
	return out, nil
}

// ScaledDecimal converts a canonical DEC value into an integer in units of
// 10^-scale, e.g. "12.34" with scale 2 becomes 1234.
func ScaledDecimal(canonical string) (int64, error) {
	return strconv.ParseInt(strings.Replace(canonical, ".", "", 1), 10, 64)
}

// UnscaleDecimal converts a scaled integer back into canonical text form.
func UnscaleDecimal(v int64, scale int) string {
	neg := v < 0
	digits := strconv.FormatUint(uint64(abs(v)), 10)
	if scale > 0 {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}

func abs(v int64) uint64 {
	if v < 0 {
		return uint64(-v)
	}
	return uint64(v)
}

// HasValue reports whether v is one of the domain's fixed values. Domains
// without fixed values accept every value.
func (d *Domain) HasValue(v string) bool {
	if len(d.Values) == 0 {
		return true
	}
	for _, fv := range d.Values {
		if fv.Value == v {
			return true
		}
	}
	return false
}

// TypeString returns the type with its parameters, e.g. "DEC(15,2)".
func (d *Domain) TypeString() string {
	switch d.Type {
	case Char:
		return fmt.Sprintf("CHAR(%d)", d.Length)
	case Dec:
		return fmt.Sprintf("DEC(%d,%d)", d.Length, d.Decimals)
	}
	return string(d.Type)
}
