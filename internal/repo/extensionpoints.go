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

package repo

import (
	"context"
	"maps"
	"slices"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of extension points and implementations.
const (
	TExtensionPoints        = model.KernelNamespace + "EXTENSION_POINTS"
	TExtensionPointTexts    = model.KernelNamespace + "EXTENSION_POINT_TEXTS"
	TExtensionPointFilters  = model.KernelNamespace + "EXTENSION_POINT_FILTERS"
	TExtensionPointDefaults = model.KernelNamespace + "EXTENSION_POINT_DEFAULTS"
	TImplementations        = model.KernelNamespace + "EXTENSION_IMPLEMENTATIONS"
	TImplementationTexts    = model.KernelNamespace + "EXTENSION_IMPLEMENTATION_TEXTS"
	TImplementationValues   = model.KernelNamespace + "EXTENSION_IMPLEMENTATION_VALUES"
)

// HasExtensionPoints reports whether the kernel tables have extension
// points.
func (r *Repo) HasExtensionPoints() bool { return r.has(TExtensionPoints, "") }

func (r *Repo) loadExtensionPoints(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	points, err := r.selectRows(ctx, q, TExtensionPoints, []string{"POINT_NAME", "PACKAGE_ID", "FILE_PATH", "INTERFACE_NAME", "EXTENSION_USE",
		"FALLBACK_CLASS", "COMMIT_ALLOWED", "ACTIVATION"}, "POINT_NAME")
	if err != nil {
		return err
	}
	for _, x := range points {
		name := str(x["POINT_NAME"])
		s.ExtensionPoints[name] = &model.ExtensionPoint{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]), Description: map[string]string{},
			Interface: str(x["INTERFACE_NAME"]), Use: model.ExtensionUse(str(x["EXTENSION_USE"])), Fallback: opt(x["FALLBACK_CLASS"]),
			CommitAllowed: flag(x["COMMIT_ALLOWED"]), Activation: model.ExtensionActivation(str(x["ACTIVATION"]))}
		if s.ExtensionPoints[name].Activation == "" {
			s.ExtensionPoints[name].Activation = model.ActivationTenant
		}
	}
	texts, err := r.selectRows(ctx, q, TExtensionPointTexts, []string{"POINT_NAME", "LANGUAGE", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if p := s.ExtensionPoints[str(t["POINT_NAME"])]; p != nil {
			p.Description[str(t["LANGUAGE"])] = str(t["DESCRIPTION"])
		}
	}
	filters, err := r.selectRows(ctx, q, TExtensionPointFilters, []string{"POINT_NAME", "POSITION", "FIELD_NAME", "DATA_ELEMENT", "DOMAIN_NAME", "HAS_DEFAULT"},
		"POINT_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, f := range filters {
		if p := s.ExtensionPoints[str(f["POINT_NAME"])]; p != nil {
			filter := model.ExtensionFilter{Name: str(f["FIELD_NAME"]), DataElement: opt(f["DATA_ELEMENT"]), Domain: opt(f["DOMAIN_NAME"])}
			if flag(f["HAS_DEFAULT"]) {
				filter.Default = model.Some([]model.ValueEntry{})
			}
			p.Filters = append(p.Filters, filter)
		}
	}
	defaults, err := r.selectRows(ctx, q, TExtensionPointDefaults, []string{"POINT_NAME", "POSITION", "VALUE_NO", "VALUE_KIND", "LOW", "HIGH"},
		"POINT_NAME", "POSITION", "VALUE_NO")
	if err != nil {
		return err
	}
	for _, x := range defaults {
		p := s.ExtensionPoints[str(x["POINT_NAME"])]
		pos := num(x["POSITION"])
		if p == nil || pos < 1 || pos > len(p.Filters) {
			continue
		}
		f := &p.Filters[pos-1]
		f.Default = model.Some(append(f.Default.Or(nil), storedEntry(x)))
	}
	impls, err := r.selectRows(ctx, q, TImplementations, []string{"IMPLEMENTATION_NAME", "PACKAGE_ID", "FILE_PATH", "POINT_NAME", "CLASS_NAME", "SORT_ORDER"},
		"IMPLEMENTATION_NAME")
	if err != nil {
		return err
	}
	for _, x := range impls {
		name := str(x["IMPLEMENTATION_NAME"])
		s.Implementations[name] = &model.Implementation{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]), Description: map[string]string{},
			ExtensionPoint: str(x["POINT_NAME"]), Class: str(x["CLASS_NAME"]), Filters: map[string][]model.ValueEntry{}, Order: int32(num(x["SORT_ORDER"]))}
	}
	texts, err = r.selectRows(ctx, q, TImplementationTexts, []string{"IMPLEMENTATION_NAME", "LANGUAGE", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if i := s.Implementations[str(t["IMPLEMENTATION_NAME"])]; i != nil {
			i.Description[str(t["LANGUAGE"])] = str(t["DESCRIPTION"])
		}
	}
	values, err := r.selectRows(ctx, q, TImplementationValues, []string{"IMPLEMENTATION_NAME", "FIELD_NAME", "VALUE_NO", "VALUE_KIND", "LOW", "HIGH"},
		"IMPLEMENTATION_NAME", "FIELD_NAME", "VALUE_NO")
	if err != nil {
		return err
	}
	for _, x := range values {
		if i := s.Implementations[str(x["IMPLEMENTATION_NAME"])]; i != nil {
			field := str(x["FIELD_NAME"])
			i.Filters[field] = append(i.Filters[field], storedEntry(x))
		}
	}
	return nil
}

func storedEntry(x row) model.ValueEntry {
	return model.ValueEntry{Kind: model.ValueKind(str(x["VALUE_KIND"])), Low: str(x["LOW"]), High: opt(x["HIGH"]).Or("")}
}

func (r *Repo) writeExtensionPoints(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.ExtensionPointNames() {
		p := s.ExtensionPoints[name]
		if err := r.insert(ctx, q, TExtensionPoints, row{"POINT_NAME": name, "PACKAGE_ID": ownerValue(p.Owner), "FILE_PATH": p.File,
			"INTERFACE_NAME": p.Interface, "EXTENSION_USE": string(p.Use), "FALLBACK_CLASS": val(p.Fallback), "COMMIT_ALLOWED": p.CommitAllowed, "ACTIVATION": string(p.Activation)}); err != nil {
			return err
		}
		for _, lang := range p.Languages() {
			if err := r.insert(ctx, q, TExtensionPointTexts, row{"POINT_NAME": name, "LANGUAGE": lang, "DESCRIPTION": p.Description[lang]}); err != nil {
				return err
			}
		}
		for i, f := range p.Filters {
			pos := int64(i + 1)
			def, hasDefault := f.Default.Get()
			if err := r.insert(ctx, q, TExtensionPointFilters, row{"POINT_NAME": name, "POSITION": pos, "FIELD_NAME": f.Name,
				"DATA_ELEMENT": val(f.DataElement), "DOMAIN_NAME": val(f.Domain), "HAS_DEFAULT": hasDefault}); err != nil {
				return err
			}
			for j, e := range def {
				if err := r.insert(ctx, q, TExtensionPointDefaults, entryRow(e, row{"POINT_NAME": name, "POSITION": pos, "VALUE_NO": int64(j + 1)})); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range s.ImplementationNames() {
		impl := s.Implementations[name]
		if err := r.insert(ctx, q, TImplementations, row{"IMPLEMENTATION_NAME": name, "PACKAGE_ID": ownerValue(impl.Owner), "FILE_PATH": impl.File,
			"POINT_NAME": impl.ExtensionPoint, "CLASS_NAME": impl.Class, "SORT_ORDER": int64(impl.Order)}); err != nil {
			return err
		}
		for _, lang := range impl.Languages() {
			if err := r.insert(ctx, q, TImplementationTexts, row{"IMPLEMENTATION_NAME": name, "LANGUAGE": lang, "DESCRIPTION": impl.Description[lang]}); err != nil {
				return err
			}
		}
		for _, field := range slices.Sorted(maps.Keys(impl.Filters)) {
			for j, e := range impl.Filters[field] {
				if err := r.insert(ctx, q, TImplementationValues, entryRow(e, row{"IMPLEMENTATION_NAME": name, "FIELD_NAME": field, "VALUE_NO": int64(j + 1)})); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// entryRow adds the columns of a value entry to a row.
func entryRow(e model.ValueEntry, x row) row {
	x["VALUE_KIND"], x["LOW"], x["HIGH"] = string(e.Kind), e.Low, nil
	if e.Kind == model.ValueRange {
		x["HIGH"] = e.High
	}
	return x
}
