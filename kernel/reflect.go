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

func (k *Kernel) languageCodes(ctx context.Context) ([]string, error) {
	langs, err := k.repo.Languages(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(langs))
	for i, l := range langs {
		out[i] = l.Code
	}
	return out, nil
}

// Reflector returns a reflector of the installed schema with the texts of
// the languages set up.
func (k *Kernel) Reflector(ctx context.Context) (*reflector.Reflector, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	langs, err := k.languageCodes(ctx)
	if err != nil {
		return nil, err
	}
	return reflector.New(s, langs), nil
}

// AuthorizationObject returns the resolved definition of an authorization
// object, or rterr.NotFound.
func (k *Kernel) AuthorizationObject(ctx context.Context, name string) (*reflector.AuthorizationObject, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	return r.AuthorizationObject(name)
}

// AuthorizationObjects returns the authorization objects of a package, or
// of all packages for "", ordered by name.
func (k *Kernel) AuthorizationObjects(ctx context.Context, pkg model.PackageID) ([]*reflector.AuthorizationObject, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	if pkg != "" {
		if s, _ := k.Schema(ctx); s != nil && s.Packages[pkg] == nil {
			return nil, &rterr.NotInstalled{Package: string(pkg)}
		}
	}
	return r.AuthorizationObjects(pkg), nil
}

// Activities returns the activities of the kernel with their texts.
func (k *Kernel) Activities(ctx context.Context) ([]reflector.Activity, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	return r.Activities(), nil
}
