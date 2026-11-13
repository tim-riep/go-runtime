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

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
)

// ObjectTypes describes the object types of a package, or of all for "".
func (k *Kernel) ObjectTypes(ctx context.Context, pkg model.PackageID) ([]*reflector.ObjectType, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.requireInstalled(ctx, pkg); err != nil {
		return nil, err
	}
	return r.ObjectTypes(pkg), nil
}

// ObjectType describes an object type with its resolved content structure.
func (k *Kernel) ObjectType(ctx context.Context, name string) (*reflector.ObjectType, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	return r.ObjectType(name)
}

// ObjectTypeSchema returns a JSON Schema of the object files of a type.
func (k *Kernel) ObjectTypeSchema(ctx context.Context, name, language string) (map[string]any, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	return r.ObjectTypeSchema(name, language)
}

// Objects describes the objects of a type, optionally of one package.
func (k *Kernel) Objects(ctx context.Context, typeName string, pkg model.PackageID) ([]*reflector.Object, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := r.ObjectType(typeName); err != nil {
		return nil, err
	}
	if err := k.requireInstalled(ctx, pkg); err != nil {
		return nil, err
	}
	return r.Objects(typeName, pkg), nil
}

// Object describes an object with its content and references.
func (k *Kernel) Object(ctx context.Context, name string) (*reflector.Object, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	return r.Object(name)
}

// ObjectUses returns the references of objects to name.
func (k *Kernel) ObjectUses(ctx context.Context, name string) ([]reflector.ObjectUse, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	return r.ObjectUses(name), nil
}

func (k *Kernel) requireInstalled(ctx context.Context, pkg model.PackageID) error {
	if pkg == "" {
		return nil
	}
	s, err := k.Schema(ctx)
	if err != nil {
		return err
	}
	if s.Packages[pkg] == nil {
		return &rterr.NotInstalled{Package: string(pkg)}
	}
	return nil
}
