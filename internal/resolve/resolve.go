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

// Package resolve checks package dependencies and orders packages so that
// every package comes after the packages it requires.
package resolve

import (
	"slices"

	"github.com/tim-riep/go-runtime/internal/semver"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Resolve checks the dependencies of the package set that results from
// adding or replacing changed in installed, and returns changed in
// topological order.
//
// Every package of the resulting set is checked, not only the changed ones,
// so an upgrade that breaks the requirement of an installed package is
// reported as well.
func Resolve(installed map[model.PackageID]model.Manifest, changed []*model.Package) ([]*model.Package, error) {
	var errs rterr.List
	avail := make(map[model.PackageID]model.Manifest, len(installed)+len(changed))
	for id, m := range installed {
		avail[id] = m
	}
	byID := map[model.PackageID]*model.Package{}
	for _, p := range changed {
		id := p.Manifest.ID
		if _, dup := byID[id]; dup {
			errs = append(errs, &rterr.DependencyError{Kind: rterr.DependencyConflict, Package: string(id)})
			continue
		}
		byID[id] = p
		avail[id] = p.Manifest
	}

	ids := make([]model.PackageID, 0, len(avail))
	for id := range avail {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		for _, dep := range avail[id].Requires {
			req, ok := avail[dep.ID]
			if !ok {
				errs = append(errs, &rterr.DependencyError{Kind: rterr.DependencyMissing, Package: string(id), Required: string(dep.ID), Constraint: dep.Constraint})
				continue
			}
			ok, err := semver.Satisfies(req.Version, dep.Constraint)
			if err != nil {
				errs = append(errs, &rterr.ValidationError{Code: rterr.CodeSchema, Location: rterr.Location{Package: string(id), File: "manifest.json"},
					Message: "invalid version constraint for " + string(dep.ID) + ": " + err.Error()})
				continue
			}
			if !ok {
				errs = append(errs, &rterr.DependencyError{Kind: rterr.DependencyVersion, Package: string(id), Required: string(dep.ID), Constraint: dep.Constraint, Found: req.Version})
			}
		}
	}
	if cycle := findCycle(ids, avail); cycle != nil {
		errs = append(errs, &rterr.DependencyError{Kind: rterr.DependencyCycle, Cycle: cycle})
	}
	if err := errs.Err(); err != nil {
		return nil, err
	}

	// Depth-first post-order over the changed packages yields dependencies
	// first; sorted roots make the order deterministic.
	var order []*model.Package
	done := map[model.PackageID]bool{}
	var visit func(model.PackageID)
	visit = func(id model.PackageID) {
		if done[id] {
			return
		}
		done[id] = true
		for _, dep := range avail[id].Requires {
			visit(dep.ID)
		}
		if p, ok := byID[id]; ok {
			order = append(order, p)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	return order, nil
}

func findCycle(ids []model.PackageID, avail map[model.PackageID]model.Manifest) []string {
	const (
		white = iota
		grey
		black
	)
	color := map[model.PackageID]int{}
	var stack []model.PackageID
	var found []string
	var visit func(model.PackageID) bool
	visit = func(id model.PackageID) bool {
		color[id] = grey
		stack = append(stack, id)
		for _, dep := range avail[id].Requires {
			if _, ok := avail[dep.ID]; !ok {
				continue
			}
			switch color[dep.ID] {
			case grey:
				start := slices.Index(stack, dep.ID)
				for _, s := range stack[start:] {
					found = append(found, string(s))
				}
				found = append(found, string(dep.ID))
				return true
			case white:
				if visit(dep.ID) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
		return false
	}
	for _, id := range ids {
		if color[id] == white && visit(id) {
			return found
		}
	}
	return nil
}
