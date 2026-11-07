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

package dec

import (
	"errors"
	"strings"
	"testing"
)

func TestArithmetic(t *testing.T) {
	p := func(s string, scale int) Dec {
		d, err := Parse(s, scale)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for _, c := range []struct {
		got  func() (Dec, error)
		want string
		s    int
	}{
		{func() (Dec, error) { return Add(p("1.25", 2), p("-3.50", 2)) }, "-2.25", 2},
		{func() (Dec, error) { return Mul(p("1.5", 1), p("2.25", 2)) }, "3.375", 3},
		{func() (Dec, error) { return Div(p("10", 0), 0, p("3", 0), 0, 6) }, "3.333333", 6},
		{func() (Dec, error) { return Div(p("2", 0), 0, p("3", 0), 0, 2) }, "0.67", 2},
		{func() (Dec, error) { return Rescale(p("2.345", 3), 3, 2) }, "2.35", 2},
		{func() (Dec, error) { return Rescale(p("-2.345", 3), 3, 2) }, "-2.35", 2},
		{func() (Dec, error) { return Rescale(p("2.344", 3), 3, 2) }, "2.34", 2},
		{func() (Dec, error) { return Mod(p("-7", 0), p("3", 0)) }, "-1", 0},
		{func() (Dec, error) { return p("0.05", 1), nil }, "0.1", 1},
	} {
		d, err := c.got()
		if err != nil || String(d, c.s) != c.want {
			t.Errorf("got %s %v, want %s", String(d, c.s), err, c.want)
		}
	}
	big := p(strings.Repeat("9", 38), 0)
	if _, err := Add(big, p("1", 0)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow: %v", err)
	}
	if _, err := Div(big, 0, Dec{}, 0, 0); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("division by zero: %v", err)
	}
	if !Fits(p("999.99", 2), 5, 2) || Fits(p("1000.00", 2), 5, 2) {
		t.Fatal("fits")
	}
	if String(Negate(p("1.50", 2)), 2) != "-1.50" || Cmp(p("-1", 0), p("1", 0)) != -1 {
		t.Fatal("negate or compare")
	}
}
