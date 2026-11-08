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

package natives

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tim-riep/go-runtime/native"
)

const testAuthorizationsClass = "/TRBASE/TEST_AUTHORIZATIONS"

func grantForTest(c *native.Context, g native.TestGrant) error {
	if c.Host == nil {
		return errors.New("natives: test authorizations need the kernel")
	}
	return c.Host.GrantForTest(g)
}

func init() {
	static(testAuthorizationsClass, "GRANT_ALL", "()", func(c *native.Context) error {
		return grantForTest(c, native.TestGrant{All: true})
	})
	static(testAuthorizationsClass, "GRANT_ROLE", "(role: STRING)", func(c *native.Context, role string) error {
		return grantForTest(c, native.TestGrant{Role: role})
	})
	static(testAuthorizationsClass, "GRANT", "(object: STRING, entries: array of STRING)", func(c *native.Context, object string, entries []string) error {
		g := native.TestGrant{Object: object, Fields: map[string][]string{}}
		for _, e := range entries {
			field, value, ok := strings.Cut(e, "=")
			if !ok {
				return fmt.Errorf("%q is no entry FIELD=VALUE", e)
			}
			g.Fields[field] = append(g.Fields[field], value)
		}
		return grantForTest(c, g)
	})
	static(testAuthorizationsClass, "CLEAR", "()", func(c *native.Context) error {
		return grantForTest(c, native.TestGrant{Clear: true})
	})
}
