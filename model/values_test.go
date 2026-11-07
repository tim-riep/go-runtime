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

import "testing"

func TestCanonical(t *testing.T) {
	cases := []struct {
		d    Domain
		in   string
		want string
		ok   bool
	}{
		{Domain{Type: Char, Length: 3}, "äöü", "äöü", true},
		{Domain{Type: Char, Length: 3}, "abcd", "", false},
		{Domain{Type: Int4}, "2147483648", "", false},
		{Domain{Type: Int8}, "-0042", "-42", true},
		{Domain{Type: Dec, Length: 5, Decimals: 2}, "12.3", "12.30", true},
		{Domain{Type: Dec, Length: 5, Decimals: 2}, "-0.00", "0.00", true},
		{Domain{Type: Dec, Length: 5, Decimals: 2}, "123.4", "123.40", true},
		{Domain{Type: Dec, Length: 5, Decimals: 2}, "1234.5", "", false},
		{Domain{Type: Dec, Length: 5, Decimals: 2}, "1.234", "", false},
		{Domain{Type: Dec, Length: 5, Decimals: 2}, "1.230", "1.23", true},
		{Domain{Type: Dec, Length: 3}, "+7", "7", true},
		{Domain{Type: Date}, "2024-02-29", "2024-02-29", true},
		{Domain{Type: Date}, "2023-02-29", "", false},
		{Domain{Type: Timestamp}, "2024-01-02T03:04:05+02:00", "2024-01-02T01:04:05.000000Z", true},
		{Domain{Type: Timestamp}, "2024-01-02T03:04:05.123456789Z", "", false},
		{Domain{Type: Bool}, "true", "true", true},
		{Domain{Type: UUID}, "6F9619FF-8B86-D011-B42D-00C04FC964FF", "6f9619ff-8b86-d011-b42d-00c04fc964ff", true},
		{Domain{Type: UUID}, "nope", "", false},
		{Domain{Type: Blob}, "x", "", false},
	}
	for _, c := range cases {
		got, err := c.d.Canonical(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("%s %q: got %q, %v", c.d.TypeString(), c.in, got, err)
		}
	}
}

func TestScaledDecimal(t *testing.T) {
	for _, c := range []struct {
		canon string
		scale int
		v     int64
	}{{"12.34", 2, 1234}, {"-0.05", 2, -5}, {"7", 0, 7}, {"0.00", 2, 0}} {
		v, err := ScaledDecimal(c.canon)
		if err != nil || v != c.v {
			t.Errorf("ScaledDecimal(%q) = %d, %v", c.canon, v, err)
		}
		if back := UnscaleDecimal(c.v, c.scale); back != c.canon {
			t.Errorf("UnscaleDecimal(%d, %d) = %q", c.v, c.scale, back)
		}
	}
}

func TestParseJSON(t *testing.T) {
	d := Domain{Type: Int4}
	if v, err := d.ParseJSON([]byte(`12`)); err != nil || v != "12" {
		t.Errorf("int: %q %v", v, err)
	}
	if _, err := d.ParseJSON([]byte(`"12"`)); err == nil {
		t.Error("string for INT4 accepted")
	}
	b := Domain{Type: Bool}
	if v, err := b.ParseJSON([]byte(`false`)); err != nil || v != "false" {
		t.Errorf("bool: %q %v", v, err)
	}
	dec := Domain{Type: Dec, Length: 10, Decimals: 3}
	if v, err := dec.ParseJSON([]byte(`1.5`)); err != nil || v != "1.500" {
		t.Errorf("dec: %q %v", v, err)
	}
}
