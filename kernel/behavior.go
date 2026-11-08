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
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/vm"
)

// determine runs the determinations of a time for instances.
func (b *entityBuffer) determine(ctx context.Context, behavior vm.Behavior, when model.When, touched []*instance, r *result) error {
	return nil
}

// validate runs the validations in the save sequence.
func (b *entityBuffer) validate(ctx context.Context, behavior vm.Behavior, r *result) error {
	return nil
}

// execute runs an action.
func (b *entityBuffer) execute(ctx context.Context, bo *model.BusinessObject, e *model.Entity, st vm.ModifyStep, req vm.ModifyRequest, r *result) (any, []*instance, error) {
	return nil, nil, &native.RuntimeError{Class: "/RT/SESSION_ERROR", Text: "actions are not available yet"}
}

// authorize checks the authorization of an operation on new values.
func (b *entityBuffer) authorize(ctx context.Context, local bool, bo *model.BusinessObject, e *model.Entity, op model.EntityOperation, values map[string]any, parent *instance, at native.CheckLocation) (bool, error) {
	return true, nil
}

// authorizeChange checks the authorization of an operation, read or an
// action on an instance; after are the values after an update.
func (b *entityBuffer) authorizeChange(ctx context.Context, req vm.ModifyRequest, bo *model.BusinessObject, e *model.Entity, op any, in *instance, after map[string]any) (bool, error) {
	return true, nil
}
