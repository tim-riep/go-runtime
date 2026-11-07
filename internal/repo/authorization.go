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

// Tables of authorization objects.
const (
	TAuthObjects          = model.KernelNamespace + "AUTH_OBJECTS"
	TAuthObjectTexts      = model.KernelNamespace + "AUTH_OBJECT_TEXTS"
	TAuthObjectFields     = model.KernelNamespace + "AUTH_OBJECT_FIELDS"
	TAuthObjectActivities = model.KernelNamespace + "AUTH_OBJECT_ACTIVITIES"
)

func (r *Repo) loadAuthorizationObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	objs, err := r.selectRows(ctx, q, TAuthObjects, []string{"OBJECT_NAME", "PACKAGE_ID", "FILE_PATH"}, "OBJECT_NAME")
	if err != nil {
		return err
	}
	for _, o := range objs {
		name := str(o["OBJECT_NAME"])
		s.AuthorizationObjects[name] = &model.AuthorizationObject{Name: name, Owner: owner(o["PACKAGE_ID"]), File: str(o["FILE_PATH"]),
			Texts: map[string]model.Label{}}
	}
	texts, err := r.selectRows(ctx, q, TAuthObjectTexts, []string{"OBJECT_NAME", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if o := s.AuthorizationObjects[str(t["OBJECT_NAME"])]; o != nil {
			o.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	fields, err := r.selectRows(ctx, q, TAuthObjectFields, []string{"OBJECT_NAME", "POSITION", "FIELD_NAME", "DATA_ELEMENT", "DOMAIN_NAME"}, "OBJECT_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, f := range fields {
		if o := s.AuthorizationObjects[str(f["OBJECT_NAME"])]; o != nil {
			o.Fields = append(o.Fields, model.AuthorizationField{Name: str(f["FIELD_NAME"]), DataElement: opt(f["DATA_ELEMENT"]), Domain: opt(f["DOMAIN_NAME"])})
		}
	}
	acts, err := r.selectRows(ctx, q, TAuthObjectActivities, []string{"OBJECT_NAME", "ACTIVITY", "POSITION"}, "OBJECT_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, a := range acts {
		if o := s.AuthorizationObjects[str(a["OBJECT_NAME"])]; o != nil {
			o.Activities = append(o.Activities, str(a["ACTIVITY"]))
		}
	}
	return nil
}

func (r *Repo) writeAuthorizationObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.AuthorizationObjectNames() {
		o := s.AuthorizationObjects[name]
		if err := r.insert(ctx, q, TAuthObjects, row{"OBJECT_NAME": name, "PACKAGE_ID": ownerValue(o.Owner), "FILE_PATH": o.File}); err != nil {
			return err
		}
		for _, lang := range slices.Sorted(maps.Keys(o.Texts)) {
			l := o.Texts[lang]
			if err := r.insert(ctx, q, TAuthObjectTexts, row{"OBJECT_NAME": name, "LANGUAGE": lang, "LABEL": l.Label, "DESCRIPTION": val(l.Description)}); err != nil {
				return err
			}
		}
		for i, f := range o.Fields {
			if err := r.insert(ctx, q, TAuthObjectFields, row{"OBJECT_NAME": name, "POSITION": int64(i + 1), "FIELD_NAME": f.Name,
				"DATA_ELEMENT": val(f.DataElement), "DOMAIN_NAME": val(f.Domain)}); err != nil {
				return err
			}
		}
		for i, a := range o.Activities {
			if err := r.insert(ctx, q, TAuthObjectActivities, row{"OBJECT_NAME": name, "ACTIVITY": a, "POSITION": int64(i + 1)}); err != nil {
				return err
			}
		}
	}
	return nil
}
