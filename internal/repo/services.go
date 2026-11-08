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
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of services.
const (
	TServices          = model.KernelNamespace + "SERVICES"
	TServiceTexts      = model.KernelNamespace + "SERVICE_TEXTS"
	TServiceExtensions = model.KernelNamespace + "SERVICE_EXTENSION_PACKAGES"
	TServiceSets       = model.KernelNamespace + "SERVICE_ENTITY_SETS"
	TServiceMembers    = model.KernelNamespace + "SERVICE_SET_MEMBERS"
	TServiceStates     = model.KernelNamespace + "SERVICE_STATES"
)

// Kinds of members of entity sets in the repository.
const (
	memberOperation  = "operation"
	memberAction     = "action"
	memberHidden     = "hidden-field"
	memberNavigation = "navigation"
)

// HasServices reports whether the kernel tables have services.
func (r *Repo) HasServices() bool { return r.has(TServices, "") }

func (r *Repo) loadServices(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	if !r.HasServices() {
		return nil
	}
	rows, err := r.selectRows(ctx, q, TServices, []string{"SERVICE_NAME", "PACKAGE_ID", "FILE_PATH", "EXTENSIONS"}, "SERVICE_NAME")
	if err != nil {
		return err
	}
	for _, x := range rows {
		name := str(x["SERVICE_NAME"])
		s.Services[name] = &model.Service{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]), Description: map[string]string{},
			Extensions: model.ExtensionRule(str(x["EXTENSIONS"]))}
	}
	texts, err := r.selectRows(ctx, q, TServiceTexts, []string{"SERVICE_NAME", "LANGUAGE", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if svc := s.Services[str(t["SERVICE_NAME"])]; svc != nil {
			svc.Description[str(t["LANGUAGE"])] = str(t["DESCRIPTION"])
		}
	}
	exts, err := r.selectRows(ctx, q, TServiceExtensions, []string{"SERVICE_NAME", "PACKAGE_ID"}, "SERVICE_NAME", "PACKAGE_ID")
	if err != nil {
		return err
	}
	for _, x := range exts {
		if svc := s.Services[str(x["SERVICE_NAME"])]; svc != nil {
			svc.ExtensionPackages = append(svc.ExtensionPackages, model.PackageID(str(x["PACKAGE_ID"])))
		}
	}
	sets, err := r.selectRows(ctx, q, TServiceSets, []string{"SERVICE_NAME", "SET_NAME", "BO_NAME", "TABLE_NAME", "READ_ONLY", "OPERATIONS_LISTED",
		"ACTIONS_LISTED"}, "SERVICE_NAME", "SET_NAME")
	if err != nil {
		return err
	}
	for _, x := range sets {
		svc := s.Services[str(x["SERVICE_NAME"])]
		if svc == nil {
			continue
		}
		set := model.EntitySet{Name: str(x["SET_NAME"]), ReadOnly: flag(x["READ_ONLY"])}
		if bo, ok := opt(x["BO_NAME"]).Get(); ok {
			set.BusinessObject, set.Entity = model.Some(bo), model.Some(str(x["TABLE_NAME"]))
		} else {
			set.Table = model.Some(str(x["TABLE_NAME"]))
		}
		if flag(x["OPERATIONS_LISTED"]) {
			set.Operations = model.Some([]model.EntityOperation{})
		}
		if flag(x["ACTIONS_LISTED"]) {
			set.Actions = model.Some([]string{})
		}
		svc.EntitySets = append(svc.EntitySets, set)
	}
	members, err := r.selectRows(ctx, q, TServiceMembers, []string{"SERVICE_NAME", "SET_NAME", "MEMBER_KIND", "MEMBER_NAME", "PUBLIC_NAME"},
		"SERVICE_NAME", "SET_NAME", "MEMBER_KIND", "MEMBER_NAME")
	if err != nil {
		return err
	}
	for _, x := range members {
		svc := s.Services[str(x["SERVICE_NAME"])]
		if svc == nil {
			continue
		}
		set := svc.EntitySet(str(x["SET_NAME"]))
		if set == nil {
			continue
		}
		name := str(x["MEMBER_NAME"])
		switch str(x["MEMBER_KIND"]) {
		case memberOperation:
			set.Operations = model.Some(append(set.Operations.Or(nil), model.EntityOperation(name)))
		case memberAction:
			set.Actions = model.Some(append(set.Actions.Or(nil), name))
		case memberHidden:
			set.HiddenFields = append(set.HiddenFields, name)
		case memberNavigation:
			if set.Navigations == nil {
				set.Navigations = map[string]string{}
			}
			set.Navigations[name] = str(x["PUBLIC_NAME"])
		}
	}
	for _, svc := range s.Services {
		for i := range svc.EntitySets {
			if ops, ok := svc.EntitySets[i].Operations.Get(); ok {
				sortOps(ops)
			}
		}
	}
	return nil
}

func (r *Repo) writeServices(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	if !r.HasServices() {
		return nil
	}
	for _, name := range s.ServiceNames() {
		svc := s.Services[name]
		if err := r.insert(ctx, q, TServices, row{"SERVICE_NAME": name, "PACKAGE_ID": string(svc.Owner), "FILE_PATH": svc.File,
			"EXTENSIONS": string(svc.Extensions)}); err != nil {
			return err
		}
		for _, lang := range svc.Languages() {
			if err := r.insert(ctx, q, TServiceTexts, row{"SERVICE_NAME": name, "LANGUAGE": lang, "DESCRIPTION": svc.Description[lang]}); err != nil {
				return err
			}
		}
		for _, p := range svc.ExtensionPackages {
			if err := r.insert(ctx, q, TServiceExtensions, row{"SERVICE_NAME": name, "PACKAGE_ID": string(p)}); err != nil {
				return err
			}
		}
		for _, set := range svc.EntitySets {
			if err := r.insert(ctx, q, TServiceSets, row{"SERVICE_NAME": name, "SET_NAME": set.Name, "BO_NAME": val(set.BusinessObject),
				"TABLE_NAME": set.Source(), "READ_ONLY": set.ReadOnly, "OPERATIONS_LISTED": set.Operations.Valid(),
				"ACTIONS_LISTED": set.Actions.Valid()}); err != nil {
				return err
			}
			member := func(kind, member string, public any) error {
				return r.insert(ctx, q, TServiceMembers, row{"SERVICE_NAME": name, "SET_NAME": set.Name, "MEMBER_KIND": kind,
					"MEMBER_NAME": member, "PUBLIC_NAME": public})
			}
			for _, op := range set.Operations.Or(nil) {
				if err := member(memberOperation, string(op), nil); err != nil {
					return err
				}
			}
			for _, a := range set.Actions.Or(nil) {
				if err := member(memberAction, a, nil); err != nil {
					return err
				}
			}
			for _, f := range set.HiddenFields {
				if err := member(memberHidden, f, nil); err != nil {
					return err
				}
			}
			for _, fk := range slices.Sorted(maps.Keys(set.Navigations)) {
				if err := member(memberNavigation, fk, set.Navigations[fk]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ServiceState is the activation of a service.
type ServiceState struct {
	Name      string
	Package   model.PackageID
	Active    bool
	ChangedAt time.Time
	ChangedBy model.Optional[string]
}

// ServiceStates reads the activation of all services.
func (r *Repo) ServiceStates(ctx context.Context, q dialect.Querier) (map[string]ServiceState, error) {
	out := map[string]ServiceState{}
	if !r.has(TServiceStates, "") {
		return out, nil
	}
	rows, err := r.selectRows(ctx, q, TServiceStates, []string{"SERVICE_NAME", "PACKAGE_ID", "ACTIVE", "CHANGED_AT", "CHANGED_BY"}, "SERVICE_NAME")
	if err != nil {
		return nil, err
	}
	for _, x := range rows {
		st := ServiceState{Name: str(x["SERVICE_NAME"]), Package: owner(x["PACKAGE_ID"]), Active: flag(x["ACTIVE"]), ChangedAt: tm(x["CHANGED_AT"]),
			ChangedBy: opt(x["CHANGED_BY"])}
		out[st.Name] = st
	}
	return out, nil
}

// ReplaceServiceStates replaces the activation of all services.
func (r *Repo) ReplaceServiceStates(ctx context.Context, q dialect.Querier, states map[string]ServiceState) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(TServiceStates)); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(states)) {
		st := states[name]
		if err := r.insert(ctx, q, TServiceStates, row{"SERVICE_NAME": name, "PACKAGE_ID": string(st.Package), "ACTIVE": st.Active,
			"CHANGED_AT": st.ChangedAt, "CHANGED_BY": val(st.ChangedBy)}); err != nil {
			return err
		}
	}
	return nil
}

// TKernelNodeSettings holds the settings of the service nodes of the
// kernel.
const TKernelNodeSettings = model.KernelNamespace + "KERNEL_NODE_SETTINGS"

// KernelNodeSettings are the settings of a service node of the kernel
// that operators changed; absent values take the defaults.
type KernelNodeSettings struct {
	Path     string
	Settings model.HTTPSettings
	// MaxPageSize and MetadataTimeoutSeconds apply to the node of OData.
	MaxPageSize            model.Optional[int64]
	MetadataTimeoutSeconds model.Optional[int64]
	ChangedAt              time.Time
	ChangedBy              model.Optional[string]
}

// KernelNodeSettings reads the settings of the service nodes of the
// kernel by path.
func (r *Repo) KernelNodeSettings(ctx context.Context, q dialect.Querier) (map[string]KernelNodeSettings, error) {
	out := map[string]KernelNodeSettings{}
	if !r.has(TKernelNodeSettings, "") {
		return out, nil
	}
	rows, err := r.selectRows(ctx, q, TKernelNodeSettings, []string{"SERVICE_PATH", "AUTHENTICATION", "TENANT_SOURCES", "LANGUAGE_SOURCES", "MAX_BODY_SIZE",
		"MAX_RESPONSE_SIZE", "TIMEOUT_SECONDS", "BUDGET", "MAX_PAGE_SIZE", "METADATA_TIMEOUT_SECONDS", "CHANGED_AT", "CHANGED_BY"}, "SERVICE_PATH")
	if err != nil {
		return nil, err
	}
	for _, x := range rows {
		k := KernelNodeSettings{Path: str(x["SERVICE_PATH"]), Settings: model.HTTPSettings{Authentication: list(x["AUTHENTICATION"]),
			TenantSources: list(x["TENANT_SOURCES"]), LanguageSources: list(x["LANGUAGE_SOURCES"]), MaxBodySize: optInt64(x["MAX_BODY_SIZE"]),
			MaxResponseSize: optInt64(x["MAX_RESPONSE_SIZE"]), TimeoutSeconds: optInt64(x["TIMEOUT_SECONDS"]), Budget: optInt64(x["BUDGET"])},
			MaxPageSize: optInt64(x["MAX_PAGE_SIZE"]), MetadataTimeoutSeconds: optInt64(x["METADATA_TIMEOUT_SECONDS"]), ChangedAt: tm(x["CHANGED_AT"]),
			ChangedBy: opt(x["CHANGED_BY"])}
		out[k.Path] = k
	}
	return out, nil
}

// WriteKernelNodeSettings replaces the settings of a service node of the
// kernel.
func (r *Repo) WriteKernelNodeSettings(ctx context.Context, q dialect.Querier, k KernelNodeSettings) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(TKernelNodeSettings)+" WHERE "+r.d.QuoteIdent("SERVICE_PATH")+" = "+r.d.Placeholder(1), k.Path); err != nil {
		return err
	}
	set := k.Settings
	return r.insert(ctx, q, TKernelNodeSettings, row{"SERVICE_PATH": k.Path, "AUTHENTICATION": listValue(set.Authentication),
		"TENANT_SOURCES": listValue(set.TenantSources), "LANGUAGE_SOURCES": listValue(set.LanguageSources), "MAX_BODY_SIZE": optValue(set.MaxBodySize),
		"MAX_RESPONSE_SIZE": optValue(set.MaxResponseSize), "TIMEOUT_SECONDS": optValue(set.TimeoutSeconds), "BUDGET": optValue(set.Budget),
		"MAX_PAGE_SIZE": optValue(k.MaxPageSize), "METADATA_TIMEOUT_SECONDS": optValue(k.MetadataTimeoutSeconds), "CHANGED_AT": k.ChangedAt,
		"CHANGED_BY": val(k.ChangedBy)})
}
