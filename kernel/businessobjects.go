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
	"context"
	"fmt"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
)

// BusinessObjectDetail describes a business object with the places
// where code works on it: entity statements on the business object, its
// entities and fields, and actions.
type BusinessObjectDetail struct {
	*reflector.BusinessObject
	CodeUses []CodeUse `json:"codeUses"`
}

// BusinessObjectOptions tell how to describe a business object.
type BusinessObjectOptions struct {
	// Base leaves out the extensions of other packages.
	Base bool
}

// BusinessObject returns the description of a business object with its
// installed extensions, or without them, or rterr.NotFound.
func (k *Kernel) BusinessObject(ctx context.Context, name string, opts BusinessObjectOptions) (*BusinessObjectDetail, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	get := r.BusinessObject
	if opts.Base {
		get = r.BaseBusinessObject
	}
	b, err := get(name)
	if err != nil {
		return nil, err
	}
	d := &BusinessObjectDetail{BusinessObject: b, CodeUses: []CodeUse{}}
	objects := []string{name}
	var tables func(e *reflector.Entity)
	tables = func(e *reflector.Entity) {
		objects = append(objects, e.Table)
		for _, c := range e.Children {
			tables(c)
		}
	}
	tables(b.Root)
	for _, o := range objects {
		uses, err := k.CodeUses(ctx, o)
		if err != nil {
			return nil, err
		}
		for _, u := range uses {
			if slices.Contains([]string{"business-object", "business-object-extension", "entity", "action"}, u.Kind) {
				d.CodeUses = append(d.CodeUses, u)
			}
		}
	}
	return d, nil
}

// BusinessObjects returns the business objects of a package and a data
// class, or of all for empty fields, ordered by name.
func (k *Kernel) BusinessObjects(ctx context.Context, f reflector.BusinessObjectFilter) ([]*reflector.BusinessObject, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	if f.Package != "" {
		if s, _ := k.Schema(ctx); s != nil && s.Packages[f.Package] == nil {
			return nil, &rterr.NotInstalled{Package: string(f.Package)}
		}
	}
	if f.DataClass != "" && f.DataClass != model.Application && f.DataClass != model.Configuration && f.DataClass != model.RuntimeConfiguration {
		return nil, &rterr.InvalidArgument{Argument: "data class", Message: fmt.Sprintf("the data class is application, configuration or runtimeConfiguration, not %q", f.DataClass)}
	}
	return r.BusinessObjects(f), nil
}
