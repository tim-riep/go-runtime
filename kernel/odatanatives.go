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

	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// odataCount counts the entities of an entity set that the user of the
// execution may read, as $count of a request would.
func odataCount(c *native.Context, pkg, service, entitySet string, filter model.Optional[string]) (int64, error) {
	const errClass = "/TRBASE/ODATA_ERROR"
	x, err := executionOf(c)
	if err != nil {
		return 0, err
	}
	user, ok := x.opts.User.Get()
	if !ok || x.tenant == "" {
		return 0, &native.NotAuthorized{Text: "counting entities of services needs a signed-in user and a tenant"}
	}
	k := x.k
	g, err := k.generation(c)
	if err != nil {
		return 0, err
	}
	st, err := k.activation(c)
	if err != nil {
		return 0, err
	}
	svc := g.schema.Services[service]
	if svc == nil || string(svc.Owner) != pkg || !st.services[svc.Name] {
		return 0, native.Raise(errClass, fmt.Sprintf("package %s has no active service %s", pkg, service))
	}
	kr := &kernelRequest{g: g, st: st, user: &auth.User{Name: user}, tenant: model.Some(x.tenant), lang: x.Language(), entry: &accessEntry{}}
	allowed, err := k.checkService(c, kr, svc, true)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, native.Raise(errClass, fmt.Sprintf("user %s may not start the service %s", user, service))
	}
	sr := &serviceRequest{kernelRequest: kr, k: k, svc: svc, m: g.odataModel(svc)}
	set := sr.m.Set(entitySet)
	if set == nil {
		return 0, native.Raise(errClass, fmt.Sprintf("the service %s has no entity set %s", service, entitySet))
	}
	var expr odata.Expr
	if f, ok := filter.Get(); ok && f != "" {
		var e *odata.Error
		if expr, e = odata.ParseFilter(f, set.Type); e != nil {
			return 0, native.Raise(errClass, e.Error())
		}
	}
	if set.Type.Object != nil {
		rows, err := objectRows(g, set.Type, odata.Format{}, nil, expr)
		if err != nil {
			return 0, odataError(err)
		}
		return int64(len(rows)), nil
	}
	if err := sr.effective(c); err != nil {
		return 0, err
	}
	q, err := sr.newQuery(set.Type)
	if err != nil {
		return 0, odataError(err)
	}
	if expr != nil {
		if err := q.filter(expr); err != nil {
			return 0, odataError(err)
		}
	}
	return sr.count(c, q)
}

// odataError turns an error of OData into /TRBASE/ODATA_ERROR.
func odataError(err error) error {
	var e *odata.Error
	if errors.As(err, &e) {
		return native.Raise("/TRBASE/ODATA_ERROR", e.Error())
	}
	return err
}

// The natives of TRBASE for services over OData.
func init() {
	static := func(class, method, sig string, fn any) {
		native.Default.Register(native.Method{Package: "TRBASE", Class: class, Method: method, Static: true, Signature: sig, Func: fn})
	}
	static("/TRBASE/ODATA", "COUNT", "(pkg: STRING, service: STRING, set: STRING, filter: STRING?) returns INT8 raises /TRBASE/ODATA_ERROR", odataCount)
	static("/TRBASE/ODATA_METADATA_CACHE", "INVALIDATE", "(pkg: STRING?, service: STRING?)",
		func(c *native.Context, pkg, service model.Optional[string]) error {
			x, err := executionOf(c)
			if err != nil {
				return err
			}
			if x.bound == nil {
				return fmt.Errorf("%s works only in CHANGED of the behavior of an object type", c.Method)
			}
			var names []string
			switch {
			case service.Valid():
				names = []string{service.Or("")}
			case pkg.Valid():
				for _, name := range x.schema.ServiceNames() {
					if string(x.schema.Services[name].Owner) == pkg.Or("") {
						names = append(names, name)
					}
				}
			default:
				names = []string{repo.AllServices}
			}
			// The kernel writes them after the execution, whose end rolls
			// back what it did in the transaction.
			x.invalidated = append(x.invalidated, names...)
			return nil
		})
}
