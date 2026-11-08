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
	"math"
	"math/big"
	"slices"
	"strings"
)

// EntriesMatch reports whether a value in canonical text form matches one
// of the entries of a field with domain d: a single value, a range with
// both bounds included or a pattern.
func EntriesMatch(d *Domain, entries []ValueEntry, v string) bool {
	for _, e := range entries {
		e = canonicalEntry(d, e)
		switch e.Kind {
		case ValuePattern:
			if e.Low == AllValues || strings.HasPrefix(v, strings.TrimSuffix(e.Low, "*")) {
				return true
			}
		case ValueRange:
			if CompareValues(d.Type, e.Low, v) <= 0 && CompareValues(d.Type, v, e.High) <= 0 {
				return true
			}
		default:
			if e.Low == v {
				return true
			}
		}
	}
	return false
}

// EntriesOverlap reports whether two lists of entries of a field with
// domain d match a common value, and describes the common values. With
// fixed values it compares the fixed values both match; otherwise it
// meets every entry of one list with every entry of the other: CHAR by
// code points, integers by number and prefixes of integers by their
// decimal text.
func EntriesOverlap(d *Domain, a, b []ValueEntry) (string, bool) {
	var common []string
	if len(d.Values) > 0 {
		for _, fv := range d.Values {
			if EntriesMatch(d, a, fv.Value) && EntriesMatch(d, b, fv.Value) {
				common = append(common, showValue(fv.Value))
			}
		}
	} else {
		for _, x := range a {
			for _, y := range b {
				if s, ok := entryOverlap(d, canonicalEntry(d, x), canonicalEntry(d, y)); ok && !slices.Contains(common, s) {
					common = append(common, s)
				}
			}
		}
	}
	return strings.Join(common, ", "), len(common) > 0
}

// canonicalEntry brings the values of an entry into the canonical text
// form of the domain; values that do not fit stay as they are.
func canonicalEntry(d *Domain, e ValueEntry) ValueEntry {
	if e.Kind == ValuePattern {
		return e
	}
	if c, err := d.Canonical(e.Low); err == nil {
		e.Low = c
	}
	if e.Kind == ValueRange {
		if c, err := d.Canonical(e.High); err == nil {
			e.High = c
		}
	}
	return e
}

func entryOverlap(d *Domain, x, y ValueEntry) (string, bool) {
	all := func(e ValueEntry) bool { return e.Kind == ValuePattern && e.Low == AllValues }
	switch {
	case all(x):
		return showEntry(y), true
	case all(y):
		return showEntry(x), true
	}
	rank := map[ValueKind]int{ValueSingle: 0, ValueRange: 1, ValuePattern: 2}
	if rank[x.Kind] > rank[y.Kind] {
		x, y = y, x
	}
	integer := d.Type == Int4 || d.Type == Int8
	switch {
	case x.Kind == ValueSingle && y.Kind == ValueSingle:
		return showValue(x.Low), x.Low == y.Low
	case x.Kind == ValueSingle:
		return showValue(x.Low), EntriesMatch(d, []ValueEntry{y}, x.Low)
	case x.Kind == ValueRange && y.Kind == ValueRange:
		lo, hi := x.Low, x.High
		if CompareValues(d.Type, y.Low, lo) > 0 {
			lo = y.Low
		}
		if CompareValues(d.Type, y.High, hi) < 0 {
			hi = y.High
		}
		if CompareValues(d.Type, lo, hi) > 0 {
			return "", false
		}
		if lo == hi {
			return showValue(lo), true
		}
		return showValue(lo) + " to " + showValue(hi), true
	case x.Kind == ValueRange:
		p := strings.TrimSuffix(y.Low, "*")
		meets := false
		if integer {
			meets = intPrefixMeetsRange(p, x.Low, x.High)
		} else {
			meets = x.Low <= p && p <= x.High || strings.HasPrefix(x.Low, p)
		}
		return showEntry(y) + " from " + showValue(x.Low) + " to " + showValue(x.High), meets
	}
	p, q := strings.TrimSuffix(x.Low, "*"), strings.TrimSuffix(y.Low, "*")
	switch {
	case strings.HasPrefix(p, q):
		return x.Low, true
	case strings.HasPrefix(q, p):
		return y.Low, true
	}
	return "", false
}

// intPrefixMeetsRange reports whether an integer whose decimal text starts
// with prefix lies between lo and hi.
func intPrefixMeetsRange(prefix, lo, hi string) bool {
	l, ok1 := new(big.Int).SetString(lo, 10)
	h, ok2 := new(big.Int).SetString(hi, 10)
	if !ok1 || !ok2 {
		return false
	}
	for _, iv := range intPrefixIntervals(prefix) {
		if iv[0].Cmp(h) <= 0 && iv[1].Cmp(l) >= 0 {
			return true
		}
	}
	return false
}

// intPrefixIntervals returns the 64-bit integers whose decimal text
// starts with prefix as intervals with both bounds included: the prefix
// itself followed by no, one, two and more digits.
func intPrefixIntervals(prefix string) [][2]*big.Int {
	minInt, maxInt := big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)
	neg := strings.HasPrefix(prefix, "-")
	digits := strings.TrimPrefix(prefix, "-")
	if neg && digits == "" {
		return [][2]*big.Int{{minInt, big.NewInt(-1)}}
	}
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok || n.Sign() < 0 || strings.HasPrefix(digits, "0") && (digits != "0" || neg) {
		return nil
	}
	if digits == "0" {
		return [][2]*big.Int{{big.NewInt(0), big.NewInt(0)}}
	}
	limit := new(big.Int).Neg(minInt) // 2^63, the largest magnitude
	if !neg {
		limit = maxInt
	}
	var out [][2]*big.Int
	scale := big.NewInt(1)
	ten := big.NewInt(10)
	for {
		low := new(big.Int).Mul(n, scale)
		if low.Cmp(limit) > 0 {
			break
		}
		high := new(big.Int).Sub(new(big.Int).Add(low, scale), big.NewInt(1))
		if high.Cmp(limit) > 0 {
			high.Set(limit)
		}
		if neg {
			out = append(out, [2]*big.Int{new(big.Int).Neg(high), new(big.Int).Neg(low)})
		} else {
			out = append(out, [2]*big.Int{low, high})
		}
		scale.Mul(scale, ten)
	}
	return out
}

func showEntry(e ValueEntry) string {
	switch e.Kind {
	case ValuePattern:
		return e.Low
	case ValueRange:
		return showValue(e.Low) + " to " + showValue(e.High)
	}
	return showValue(e.Low)
}

// showValue writes a value for messages; the empty string is written as
// "" so that it is visible.
func showValue(v string) string {
	if v == "" {
		return `""`
	}
	return v
}
