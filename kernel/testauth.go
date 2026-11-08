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

package kernel

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// testGrantsRole is the role that holds the authorizations a test method
// grants itself with /TRBASE/TEST_AUTHORIZATIONS.GRANT.
const testGrantsRole = model.KernelNamespace + "TEST_GRANTS"

// testGrants are the authorizations of a test method.
type testGrants struct {
	all   bool
	roles []string
	auths []model.Authorization
}

// GrantForTest changes the authorizations of a test method; the checks of
// the execution use them instead of the roles of its user.
func (x *execution) GrantForTest(g native.TestGrant) error {
	if !x.test {
		return errors.New("/TRBASE/TEST_AUTHORIZATIONS works only in test methods that rt test runs")
	}
	if x.grants == nil {
		x.grants = &testGrants{}
	}
	switch {
	case g.Clear:
		x.grants = &testGrants{}
	case g.All:
		x.grants.all = true
	case g.Role != "":
		if x.schema.Roles[g.Role] == nil {
			return fmt.Errorf("role %s does not exist", g.Role)
		}
		x.grants.roles = append(x.grants.roles, g.Role)
	default:
		o := x.schema.AuthorizationObjects[g.Object]
		if o == nil {
			return fmt.Errorf("authorization object %s does not exist", g.Object)
		}
		a := model.Authorization{Object: g.Object, Fields: map[string][]model.ValueEntry{}}
		for field, entries := range g.Fields {
			if _, ok := o.Field(field); !ok {
				return fmt.Errorf("%s has no field %s", g.Object, field)
			}
			for _, e := range entries {
				a.Fields[field] = append(a.Fields[field], testEntry(e))
			}
		}
		for _, f := range o.Fields {
			if _, ok := a.Fields[f.Name]; !ok {
				return fmt.Errorf("the authorization on %s needs the field %s", g.Object, f.Name)
			}
		}
		x.grants.auths = append(x.grants.auths, a)
	}
	x.auth, x.authLoaded = x.grants.effective(x), true
	return nil
}

// testEntry reads an entry in the text form of role files.
func testEntry(s string) model.ValueEntry {
	switch {
	case strings.HasSuffix(s, "*"):
		return model.ValueEntry{Kind: model.ValuePattern, Low: s}
	case strings.Contains(s, ".."):
		low, high, _ := strings.Cut(s, "..")
		return model.ValueEntry{Kind: model.ValueRange, Low: low, High: high}
	}
	return model.ValueEntry{Kind: model.ValueSingle, Low: s}
}

// effective resolves the granted authorizations like assigned roles.
func (g *testGrants) effective(x *execution) *authorization.Effective {
	s := *x.schema
	s.Roles = maps.Clone(x.schema.Roles)
	s.Roles[testGrantsRole] = &model.Role{Name: testGrantsRole, Owner: model.Kernel, Authorizations: g.auths}
	var assignments []authorization.Assignment
	for i, r := range g.roles {
		assignments = append(assignments, authorization.Assignment{ID: fmt.Sprintf("test-%d", i+1), Role: r})
	}
	assignments = append(assignments, authorization.Assignment{ID: "test-grants", Role: testGrantsRole})
	if g.all {
		assignments = append(assignments, authorization.Assignment{ID: "test-all", Role: model.AllAuthorizationsRole})
	}
	return authorization.Resolve(&s, x.opts.User.Or("test"), x.tenant, x.k.now(), false, assignments)
}
