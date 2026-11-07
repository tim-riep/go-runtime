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

package dialect

import (
	"fmt"
	"strconv"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Canonicalize converts a Go value into the canonical text form of the
// domain. Accepted inputs are strings in any form the domain accepts,
// integers for INT4, INT8 and DEC, bools for BOOL and time.Time for DATE and
// TIMESTAMP. BLOB has no text form.
func Canonicalize(d *model.Domain, v any) (string, error) {
	switch x := v.(type) {
	case string:
		return d.Canonical(x)
	case int:
		return d.Canonical(strconv.FormatInt(int64(x), 10))
	case int32:
		return d.Canonical(strconv.FormatInt(int64(x), 10))
	case int64:
		return d.Canonical(strconv.FormatInt(x, 10))
	case bool:
		if d.Type == model.Bool {
			return strconv.FormatBool(x), nil
		}
	case time.Time:
		switch d.Type {
		case model.Date:
			return x.Format(model.DateLayout), nil
		case model.Timestamp:
			return model.FormatTimestamp(x)
		}
	}
	return "", fmt.Errorf("cannot store %T in a %s field", v, d.TypeString())
}

// EncodeBlob accepts the Go values allowed for BLOB fields.
func EncodeBlob(v any) (any, error) {
	switch x := v.(type) {
	case []byte:
		return x, nil
	case string:
		return []byte(x), nil
	}
	return nil, fmt.Errorf("cannot store %T in a BLOB field", v)
}
