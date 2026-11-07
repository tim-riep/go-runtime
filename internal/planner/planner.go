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

// Package planner builds the plan that turns one schema into another.
package planner

import (
	"github.com/timriep/go-runtime/dialect"
	"github.com/timriep/go-runtime/internal/diff"
	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/plan"
)

// Build compares cur and want and lets the dialect turn the changes into
// statements. Both schemas must be valid.
func Build(d dialect.Dialect, op plan.Operation, cur, want *model.Schema, pkgs []plan.PackageChange) (*plan.Plan, error) {
	changes := diff.Diff(cur, want)
	steps, err := d.PlanChanges(cur.Physical(), want.Physical(), changes)
	if err != nil {
		return nil, err
	}
	if changes == nil {
		changes = []plan.Change{}
	}
	return &plan.Plan{Operation: op, Dialect: d.Name(), Packages: pkgs, Changes: changes, Steps: steps}, nil
}
