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

package vm

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/lang/types"
)

func errUnknownBuiltin(id int) error { return fmt.Errorf("vm: unknown built-in method %d", id) }

func parseInt(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n, err == nil
}

const dateLayout = "2006-01-02"

// timestampLayout writes UTC with up to microseconds.
const timestampLayout = "2006-01-02T15:04:05.999999Z07:00"

func formatDate(days int64) string { return time.Unix(days*86400, 0).UTC().Format(dateLayout) }
func formatTimestamp(us int64) string {
	return time.UnixMicro(us).UTC().Format(timestampLayout)
}

// format returns the text of a value for placeholders, log and conversion
// to STRING. extra is the scale of DEC and the enum of enum constants.
func (m *Machine) format(k TypeKind, extra int, v Value) string {
	if v.IsNull() {
		return "null"
	}
	switch k {
	case TString:
		return v.str()
	case TInt4, TInt8:
		return strconv.FormatInt(v.int(), 10)
	case TDec:
		return dec.String(v.dec(), extra)
	case TDate:
		return formatDate(v.int())
	case TTimestamp:
		return formatTimestamp(v.int())
	case TBool:
		return strconv.FormatBool(v.bool())
	case TUUID:
		return formatUUID(v)
	case TBytes:
		b, _ := v.R.([]byte)
		return base64.StdEncoding.EncodeToString(b)
	case TEnum:
		return m.p.Enums[extra].Names[v.int()]
	}
	return fmt.Sprint(v.R)
}

// convert converts a value explicitly (as). p and s describe a DEC target,
// extra the scale of a DEC source or the enum of an enum source.
func (m *Machine) convert(from, to TypeKind, p, s, extra int, v Value) (Value, *thrown) {
	bad := func(format string, args ...any) (Value, *thrown) {
		return Value{}, m.fail("/RT/CONVERSION_ERROR", format, args...)
	}
	overflow := func() (Value, *thrown) {
		return Value{}, m.fail("/RT/OVERFLOW", "%s does not fit into the target type", m.format(from, extra, v))
	}
	if from == TEnum {
		e := &m.p.Enums[extra]
		text := e.Values[v.int()]
		if to == TString {
			return Str(text), nil
		}
		n, _ := parseInt(text)
		v, from = Int(n), TInt8
	}
	isInt := func(k TypeKind) bool { return k == TInt4 || k == TInt8 }
	switch {
	case from == to && from != TDec:
		return v, nil
	case to == TString:
		return Str(m.format(from, extra, v)), nil
	case isInt(from) && isInt(to):
		if to == TInt4 && !fitsInt4(v.int()) {
			return overflow()
		}
		return v, nil
	case isInt(from) && to == TDec:
		d, err := dec.Rescale(dec.FromInt64(v.int()), 0, s)
		if err != nil || !dec.Fits(d, p, s) {
			return overflow()
		}
		return DecValue(d), nil
	case from == TDec && isInt(to):
		d, err := dec.Rescale(v.dec(), extra, 0)
		n, ok := d.Int64()
		if err != nil || !ok || to == TInt4 && !fitsInt4(n) {
			return overflow()
		}
		return Int(n), nil
	case from == TDec && to == TDec:
		d, err := dec.Rescale(v.dec(), extra, s)
		if err != nil || !dec.Fits(d, p, s) {
			return overflow()
		}
		return DecValue(d), nil
	case from == TDate && to == TTimestamp:
		return Int(v.int() * 86_400_000_000), nil
	case from == TTimestamp && to == TDate:
		r, _, _ := m.builtin(int(types.TsDate), -1, -1, []Value{v})
		return r, nil
	case from == TString:
		text := v.str()
		switch to {
		case TInt4, TInt8:
			n, ok := parseInt(text)
			if !ok {
				return bad("%q is not a whole number", text)
			}
			if to == TInt4 && !fitsInt4(n) {
				return overflow()
			}
			return Int(n), nil
		case TDec:
			d, err := dec.Parse(text, s)
			if err != nil {
				return bad("%q is not a decimal number", text)
			}
			if !dec.Fits(d, p, s) {
				return overflow()
			}
			return DecValue(d), nil
		case TDate:
			t, err := time.Parse(dateLayout, text)
			if err != nil {
				return bad("%q is not a date of the form YYYY-MM-DD", text)
			}
			return Int(t.Unix() / 86400), nil
		case TTimestamp:
			t, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return bad("%q is not a timestamp of the form YYYY-MM-DDThh:mm:ssZ", text)
			}
			return Int(t.UnixMicro()), nil
		case TBool:
			switch text {
			case "true":
				return Bool(true), nil
			case "false":
				return Bool(false), nil
			}
			return bad("%q is neither true nor false", text)
		case TUUID:
			u, ok := parseUUID(text)
			if !ok {
				return bad("%q is not a UUID", text)
			}
			return u, nil
		}
	}
	return bad("cannot convert this value")
}

// check enforces the rules of a named type when a value is stored.
func (m *Machine) check(t int, v Value) *thrown {
	if v.IsNull() {
		return nil
	}
	d := &m.p.Types[t]
	switch d.Kind {
	case TString:
		s := v.str()
		if d.NoEmpty && s == "" {
			return m.fail("/RT/VALUE_ERROR", "%s does not allow the empty string", d.Name)
		}
		if d.MaxLength > 0 {
			if n := utf8.RuneCountInString(s); n > d.MaxLength {
				return m.fail("/RT/VALUE_ERROR", "%s allows %d characters, the value has %d", d.Name, d.MaxLength, n)
			}
		}
	case TDec:
		if !dec.Fits(v.dec(), d.Precision, d.Scale) {
			return m.fail("/RT/OVERFLOW", "the value has more digits than %s allows", d.Name)
		}
	}
	return nil
}
