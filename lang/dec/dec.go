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

// Package dec implements the exact decimal numbers of the language: a
// signed integer of at most 38 digits, kept in 128 bits, with a scale that
// is known from the type and therefore not stored.
package dec

import (
	"errors"
	"math/big"
	"math/bits"
	"strings"
)

// MaxDigits is the largest number of digits.
const MaxDigits = 38

// Dec is the unscaled value of a decimal as a two's complement 128-bit
// integer.
type Dec struct {
	Lo uint64
	Hi uint64
}

// ErrOverflow reports a result with more than MaxDigits digits.
var ErrOverflow = errors.New("the number has more than 38 digits")

// ErrDivisionByZero reports a division by zero.
var ErrDivisionByZero = errors.New("division by zero")

var (
	limit    = new(big.Int).Exp(big.NewInt(10), big.NewInt(MaxDigits), nil) // 10^38
	pow10big [MaxDigits*2 + 1]*big.Int
)

func init() {
	for i := range pow10big {
		pow10big[i] = new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(i)), nil)
	}
}

// FromInt64 returns n with scale 0.
func FromInt64(n int64) Dec {
	return Dec{Lo: uint64(n), Hi: uint64(n >> 63)}
}

// Neg reports whether d is negative.
func (d Dec) Neg() bool { return int64(d.Hi) < 0 }

// IsZero reports d = 0.
func (d Dec) IsZero() bool { return d.Lo == 0 && d.Hi == 0 }

// Big returns d as big integer.
func (d Dec) Big() *big.Int {
	b := new(big.Int).SetUint64(d.Hi)
	b.Lsh(b, 64)
	b.Or(b, new(big.Int).SetUint64(d.Lo))
	if d.Neg() {
		b.Sub(b, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	return b
}

// FromBig converts a big integer, failing when it has too many digits.
func FromBig(b *big.Int) (Dec, error) {
	if new(big.Int).Abs(b).Cmp(limit) >= 0 {
		return Dec{}, ErrOverflow
	}
	v := new(big.Int).Set(b)
	if v.Sign() < 0 {
		v.Add(v, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	words := v.Bits()
	var lo, hi uint64
	switch bits.UintSize {
	case 64:
		if len(words) > 0 {
			lo = uint64(words[0])
		}
		if len(words) > 1 {
			hi = uint64(words[1])
		}
	default:
		mask := new(big.Int).SetUint64(^uint64(0))
		lo = new(big.Int).And(v, mask).Uint64()
		hi = new(big.Int).Rsh(v, 64).Uint64()
	}
	return Dec{Lo: lo, Hi: hi}, nil
}

// Int64 returns d if it fits into int64.
func (d Dec) Int64() (int64, bool) {
	n := int64(d.Lo)
	return n, d.Hi == uint64(n>>63)
}

// Add returns a + b of the same scale.
func Add(a, b Dec) (Dec, error) {
	if x, ok := a.Int64(); ok {
		if y, ok := b.Int64(); ok {
			if s := x + y; (s > x) == (y > 0) {
				return FromInt64(s), nil
			}
		}
	}
	return FromBig(new(big.Int).Add(a.Big(), b.Big()))
}

// Sub returns a - b of the same scale.
func Sub(a, b Dec) (Dec, error) {
	if x, ok := a.Int64(); ok {
		if y, ok := b.Int64(); ok {
			if s := x - y; (s < x) == (y > 0) {
				return FromInt64(s), nil
			}
		}
	}
	return FromBig(new(big.Int).Sub(a.Big(), b.Big()))
}

// Negate returns -a.
func Negate(a Dec) Dec {
	lo, borrow := bits.Sub64(0, a.Lo, 0)
	hi, _ := bits.Sub64(0, a.Hi, borrow)
	return Dec{Lo: lo, Hi: hi}
}

// Mul returns a * b; the scale of the result is the sum of the scales.
func Mul(a, b Dec) (Dec, error) {
	return FromBig(new(big.Int).Mul(a.Big(), b.Big()))
}

// Rescale changes the scale of a from from to to, rounding half away from
// zero when the scale gets smaller.
func Rescale(a Dec, from, to int) (Dec, error) {
	if from == to {
		return a, nil
	}
	if to > from {
		return FromBig(new(big.Int).Mul(a.Big(), pow10big[to-from]))
	}
	return FromBig(roundDiv(a.Big(), pow10big[from-to]))
}

// roundDiv returns n / d rounded half away from zero.
func roundDiv(n, d *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	r.Abs(r).Lsh(r, 1)
	if r.Cmp(new(big.Int).Abs(d)) >= 0 {
		if (n.Sign() < 0) != (d.Sign() < 0) {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

// Div returns a / b for operands of scales sa and sb as a number of scale
// sr, rounded half away from zero.
func Div(a Dec, sa int, b Dec, sb int, sr int) (Dec, error) {
	if b.IsZero() {
		return Dec{}, ErrDivisionByZero
	}
	// a/10^sa / (b/10^sb) * 10^sr = a * 10^(sr-sa+sb) / b
	n := a.Big()
	exp := sr - sa + sb
	d := b.Big()
	if exp >= 0 {
		n.Mul(n, pow10big[exp])
	} else {
		d = new(big.Int).Mul(d, pow10big[-exp])
	}
	return FromBig(roundDiv(n, d))
}

// Mod returns the remainder of a / b with the sign of a; both have the same
// scale.
func Mod(a, b Dec) (Dec, error) {
	if b.IsZero() {
		return Dec{}, ErrDivisionByZero
	}
	return FromBig(new(big.Int).Rem(a.Big(), b.Big()))
}

// Cmp compares a and b of the same scale.
func Cmp(a, b Dec) int {
	if x, ok := a.Int64(); ok {
		if y, ok := b.Int64(); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	}
	return a.Big().Cmp(b.Big())
}

// Digits returns the number of digits of a, at least 1.
func Digits(a Dec) int {
	s := new(big.Int).Abs(a.Big()).String()
	return len(s)
}

// Fits reports whether a of scale s fits into DEC(p,s): at most p-s digits
// before the point.
func Fits(a Dec, p, s int) bool {
	if a.IsZero() {
		return true
	}
	return new(big.Int).Abs(a.Big()).Cmp(pow10big[p]) < 0
}

// String formats a with scale s, e.g. "-12.50".
func String(a Dec, s int) string {
	b := a.Big()
	neg := b.Sign() < 0
	digits := new(big.Int).Abs(b).String()
	if s > 0 {
		if len(digits) <= s {
			digits = strings.Repeat("0", s-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-s] + "." + digits[len(digits)-s:]
	}
	if neg {
		return "-" + digits
	}
	return digits
}

// Parse reads a decimal number such as "-12.5" into scale s. More decimals
// than s are rounded half away from zero.
func Parse(text string, s int) (Dec, error) {
	t := strings.TrimSpace(strings.ReplaceAll(text, "_", ""))
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimPrefix(strings.TrimPrefix(t, "-"), "+")
	ip, fp, _ := strings.Cut(t, ".")
	if ip == "" && fp == "" || strings.ContainsFunc(ip+fp, func(r rune) bool { return r < '0' || r > '9' }) {
		return Dec{}, errors.New("not a decimal number: " + text)
	}
	n, ok := new(big.Int).SetString(ip+fp+"0", 10)
	if !ok {
		return Dec{}, errors.New("not a decimal number: " + text)
	}
	n.Quo(n, big.NewInt(10))
	if neg {
		n.Neg(n)
	}
	d, err := FromBig(clamp(n, len(fp), s))
	if err != nil {
		return Dec{}, err
	}
	return d, nil
}

func clamp(n *big.Int, from, to int) *big.Int {
	switch {
	case to > from:
		return new(big.Int).Mul(n, pow10big[min(to-from, len(pow10big)-1)])
	case to < from:
		if from-to >= len(pow10big) {
			return new(big.Int)
		}
		return roundDiv(n, pow10big[from-to])
	}
	return n
}
