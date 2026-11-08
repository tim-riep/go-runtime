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

package validate

import (
	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// services checks the services against the business objects and tables
// as they are, so that a change of a business object, an extension or a
// table that breaks an installed service fails with the name of the
// service.
func (v *validator) services() {
	for _, name := range v.s.ServiceNames() {
		v.service(v.s.Services[name])
	}
}

func (v *validator) service(svc *model.Service) {
	obj := "service " + svc.Name
	fail := func(at rterr.Location, format string, args ...any) {
		v.fail(rterr.CodeService, at, obj, format, args...)
	}
	v.objectName(svc.Name, svc.Owner, svc.Src.At("/name"), obj)
	v.descriptions(obj, svc.Src, svc.Description, svc.Languages())
	for _, p := range svc.ExtensionPackages {
		if p == svc.Owner {
			fail(svc.Src.At("/extensions"), "%s is the package of the service, whose extensions the service always offers", p)
		}
	}
	ok := true
	for _, set := range svc.EntitySets {
		var source string
		var at rterr.Location
		if bo, isBO := set.BusinessObject.Get(); isBO {
			source, at = bo, set.Src.At("/businessObject")
		} else {
			source, at = set.Table.Or(""), set.Src.At("/table")
		}
		if pkg, _, split := model.SplitName(source); split && !v.s.Visible(svc.Owner, pkg) {
			v.fail(rterr.CodeNotVisible, at, obj, "%s belongs to %s, which %s does not require", source, pkg, svc.Owner)
			ok = false
		}
	}
	if !ok {
		return
	}
	_, problems := odata.Build(v.s, v.physical(), svc)
	for _, p := range problems {
		fail(p.At, "%s", p.Message)
	}
}
