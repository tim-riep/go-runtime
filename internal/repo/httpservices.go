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
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of http services.
const (
	THTTPServices      = model.KernelNamespace + "HTTP_SERVICES"
	THTTPServiceTexts  = model.KernelNamespace + "HTTP_SERVICE_TEXTS"
	THTTPHandlers      = model.KernelNamespace + "HTTP_HANDLERS"
	THTTPHandlerParams = model.KernelNamespace + "HTTP_HANDLER_PARAMETERS"
	THTTPStates        = model.KernelNamespace + "HTTP_SERVICE_STATES"
	THTTPGeneration    = model.KernelNamespace + "HTTP_GENERATION"
)

func list(v any) model.Optional[[]string] {
	s, ok := v.(string)
	if !ok {
		return model.None[[]string]()
	}
	if s == "" {
		return model.Some([]string{})
	}
	return model.Some(strings.Split(s, ","))
}

func listValue(o model.Optional[[]string]) any {
	v, ok := o.Get()
	if !ok {
		return nil
	}
	return strings.Join(v, ",")
}

func optInt64(v any) model.Optional[int64] {
	n, ok := v.(int64)
	if !ok {
		return model.None[int64]()
	}
	return model.Some(n)
}

func optValue[T any](o model.Optional[T]) any {
	v, ok := o.Get()
	if !ok {
		return nil
	}
	return v
}

func (r *Repo) loadHTTPServices(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	rows, err := r.selectRows(ctx, q, THTTPServices, []string{"SERVICE_PATH", "PACKAGE_ID", "LOCAL_PATH", "FILE_PATH", "AUTHENTICATION", "ANONYMOUS",
		"ANONYMOUS_TENANT", "TENANT_SOURCES", "LANGUAGE_SOURCES", "MAX_BODY_SIZE", "MAX_RESPONSE_SIZE", "TIMEOUT_SECONDS", "BUDGET"}, "SERVICE_PATH")
	if err != nil {
		return err
	}
	for _, h := range rows {
		path := str(h["SERVICE_PATH"])
		local := str(h["LOCAL_PATH"])
		if local == "-" {
			local = "" // the root node of a package
		}
		s.HTTPServices[path] = &model.HTTPService{Path: local, Owner: owner(h["PACKAGE_ID"]), File: str(h["FILE_PATH"]), Texts: map[string]model.Label{},
			Settings: model.HTTPSettings{Authentication: list(h["AUTHENTICATION"]), Anonymous: optBool(h["ANONYMOUS"]), AnonymousTenant: opt(h["ANONYMOUS_TENANT"]),
				TenantSources: list(h["TENANT_SOURCES"]), LanguageSources: list(h["LANGUAGE_SOURCES"]), MaxBodySize: optInt64(h["MAX_BODY_SIZE"]),
				MaxResponseSize: optInt64(h["MAX_RESPONSE_SIZE"]), TimeoutSeconds: optInt64(h["TIMEOUT_SECONDS"]), Budget: optInt64(h["BUDGET"])}}
	}
	texts, err := r.selectRows(ctx, q, THTTPServiceTexts, []string{"SERVICE_PATH", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if h := s.HTTPServices[str(t["SERVICE_PATH"])]; h != nil {
			h.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	handlers, err := r.selectRows(ctx, q, THTTPHandlers, []string{"SERVICE_PATH", "POSITION", "CLASS_NAME"}, "SERVICE_PATH", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range handlers {
		if h := s.HTTPServices[str(x["SERVICE_PATH"])]; h != nil {
			h.Handlers = append(h.Handlers, model.HTTPHandlerRef{Class: str(x["CLASS_NAME"])})
		}
	}
	params, err := r.selectRows(ctx, q, THTTPHandlerParams, []string{"SERVICE_PATH", "POSITION", "PARAMETER_NAME", "VALUE"}, "SERVICE_PATH", "POSITION", "PARAMETER_NAME")
	if err != nil {
		return err
	}
	for _, p := range params {
		h := s.HTTPServices[str(p["SERVICE_PATH"])]
		pos := num(p["POSITION"])
		if h != nil && pos >= 1 && pos <= len(h.Handlers) {
			hr := &h.Handlers[pos-1]
			hr.Parameters = append(hr.Parameters, model.HTTPParam{Name: str(p["PARAMETER_NAME"]), Value: str(p["VALUE"])})
		}
	}
	return nil
}

func (r *Repo) writeHTTPServices(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, path := range slices.Sorted(maps.Keys(s.HTTPServices)) {
		h := s.HTTPServices[path]
		local := h.Path
		if local == "" {
			local = "-"
		}
		set := h.Settings
		if err := r.insert(ctx, q, THTTPServices, row{"SERVICE_PATH": path, "PACKAGE_ID": string(h.Owner), "LOCAL_PATH": local, "FILE_PATH": h.File,
			"AUTHENTICATION": listValue(set.Authentication), "ANONYMOUS": optValue(set.Anonymous), "ANONYMOUS_TENANT": val(set.AnonymousTenant),
			"TENANT_SOURCES": listValue(set.TenantSources), "LANGUAGE_SOURCES": listValue(set.LanguageSources), "MAX_BODY_SIZE": optValue(set.MaxBodySize),
			"MAX_RESPONSE_SIZE": optValue(set.MaxResponseSize), "TIMEOUT_SECONDS": optValue(set.TimeoutSeconds), "BUDGET": optValue(set.Budget)}); err != nil {
			return err
		}
		for _, lang := range slices.Sorted(maps.Keys(h.Texts)) {
			l := h.Texts[lang]
			if err := r.insert(ctx, q, THTTPServiceTexts, row{"SERVICE_PATH": path, "LANGUAGE": lang, "LABEL": l.Label, "DESCRIPTION": val(l.Description)}); err != nil {
				return err
			}
		}
		for i, hr := range h.Handlers {
			if err := r.insert(ctx, q, THTTPHandlers, row{"SERVICE_PATH": path, "POSITION": int64(i + 1), "CLASS_NAME": hr.Class}); err != nil {
				return err
			}
			for _, p := range hr.Parameters {
				if err := r.insert(ctx, q, THTTPHandlerParams, row{"SERVICE_PATH": path, "POSITION": int64(i + 1), "PARAMETER_NAME": p.Name, "VALUE": p.Value}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// HTTPState is the stored activation state of a service node.
type HTTPState struct {
	Path      string
	Package   model.PackageID
	State     string
	ChangedAt time.Time
}

// HasHTTPServices reports a kernel version with http services.
func (r *Repo) HasHTTPServices() bool { return r.has(THTTPStates, "") }

// HTTPStates returns the activation states by path.
func (r *Repo) HTTPStates(ctx context.Context, q dialect.Querier) (map[string]HTTPState, error) {
	rows, err := r.selectRows(ctx, q, THTTPStates, []string{"SERVICE_PATH", "PACKAGE_ID", "STATE", "CHANGED_AT"})
	if err != nil {
		return nil, err
	}
	out := map[string]HTTPState{}
	for _, s := range rows {
		out[str(s["SERVICE_PATH"])] = HTTPState{Path: str(s["SERVICE_PATH"]), Package: owner(s["PACKAGE_ID"]), State: str(s["STATE"]), ChangedAt: tm(s["CHANGED_AT"])}
	}
	return out, nil
}

// ReplaceHTTPStates stores the activation states of all service nodes.
func (r *Repo) ReplaceHTTPStates(ctx context.Context, q dialect.Querier, states map[string]HTTPState) error {
	if !r.HasHTTPServices() {
		return nil
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(THTTPStates)); err != nil {
		return err
	}
	for _, p := range slices.Sorted(maps.Keys(states)) {
		s := states[p]
		if err := r.insert(ctx, q, THTTPStates, row{"SERVICE_PATH": p, "PACKAGE_ID": string(s.Package), "STATE": s.State, "CHANGED_AT": s.ChangedAt}); err != nil {
			return err
		}
	}
	return nil
}

// HTTPGeneration returns the counter of activation changes.
func (r *Repo) HTTPGeneration(ctx context.Context, q dialect.Querier) (int64, error) {
	rows, err := r.selectRows(ctx, q, THTTPGeneration, []string{"GENERATION"})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	n, _ := rows[0]["GENERATION"].(int64)
	return n, nil
}

// NextHTTPGeneration increases the counter of activation changes.
func (r *Repo) NextHTTPGeneration(ctx context.Context, q dialect.Querier, at time.Time) error {
	if !r.HasHTTPServices() {
		return nil
	}
	g, err := r.HTTPGeneration(ctx, q)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(THTTPGeneration)); err != nil {
		return err
	}
	return r.insert(ctx, q, THTTPGeneration, row{"ID": int64(1), "GENERATION": g + 1, "CHANGED_AT": at})
}
