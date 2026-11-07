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

package native

import (
	"encoding/hex"
	"time"
)

// Go types of the values of the language that have no Go type of their
// own. STRING is string, INT4 int32, INT8 int64, DEC dec.Dec, TIMESTAMP
// time.Time in UTC, BOOL bool, BYTES []byte, T? model.Optional[T] and
// array of T []T.

// Date is a DATE: the days since 1970-01-01.
type Date int64

// DateOf returns the date of a point in time in UTC.
func DateOf(t time.Time) Date { return Date(t.UTC().Unix() / 86400) }

// Time returns the start of the day in UTC.
func (d Date) Time() time.Time { return time.Unix(int64(d)*86400, 0).UTC() }

func (d Date) String() string { return d.Time().Format(time.DateOnly) }

// UUID is a UUID.
type UUID [16]byte

func (u UUID) String() string {
	b := hex.EncodeToString(u[:])
	return b[0:8] + "-" + b[8:12] + "-" + b[12:16] + "-" + b[16:20] + "-" + b[20:]
}
