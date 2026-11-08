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

// Tables of business objects.
const (
	TBusinessObjects      = model.KernelNamespace + "BUSINESS_OBJECTS"
	TBusinessObjectTexts  = model.KernelNamespace + "BUSINESS_OBJECT_TEXTS"
	TBusinessObjectEntity = model.KernelNamespace + "BUSINESS_OBJECT_ENTITIES"
	TBusinessObjectFields = model.KernelNamespace + "BUSINESS_OBJECT_FIELDS"
	TBusinessObjectMethod = model.KernelNamespace + "BUSINESS_OBJECT_METHODS"
	TBusinessObjectTrig   = model.KernelNamespace + "BUSINESS_OBJECT_TRIGGER_FIELDS"
	TBusinessObjectAuth   = model.KernelNamespace + "BUSINESS_OBJECT_AUTH"
)

// Kinds of methods of behavior classes in the repository.
const (
	methodDetermination = "determination"
	methodValidation    = "validation"
	methodAction        = "action"
)

// HasBusinessObjects reports whether the kernel tables have business
// objects.
func (r *Repo) HasBusinessObjects() bool { return r.has(TBusinessObjects, "") }

func (r *Repo) loadBusinessObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	bos, err := r.selectRows(ctx, q, TBusinessObjects, []string{"BO_NAME", "PACKAGE_ID", "FILE_PATH", "ROOT_TABLE", "BEHAVIOR_CLASS", "DATA_CLASS"}, "BO_NAME")
	if err != nil {
		return err
	}
	for _, x := range bos {
		name := str(x["BO_NAME"])
		s.BusinessObjects[name] = &model.BusinessObject{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]), Description: map[string]string{},
			Root: str(x["ROOT_TABLE"]), BehaviorClass: opt(x["BEHAVIOR_CLASS"]), DataClass: model.DataClass(str(x["DATA_CLASS"]))}
		if s.BusinessObjects[name].DataClass == "" {
			s.BusinessObjects[name].DataClass = model.Application
		}
	}
	texts, err := r.selectRows(ctx, q, TBusinessObjectTexts, []string{"BO_NAME", "LANGUAGE", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if b := s.BusinessObjects[str(t["BO_NAME"])]; b != nil {
			b.Description[str(t["LANGUAGE"])] = str(t["DESCRIPTION"])
		}
	}
	entities, err := r.selectRows(ctx, q, TBusinessObjectEntity, []string{"BO_NAME", "TABLE_NAME", "POSITION", "PARENT_TABLE", "FK_NAME", "NUMBERING",
		"CAN_CREATE", "CAN_UPDATE", "CAN_DELETE", "CREATED_BY", "CREATED_AT", "CHANGED_BY", "CHANGED_AT", "ETAG_FIELD", "AUTH_OBJECT"}, "BO_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range entities {
		b := s.BusinessObjects[str(x["BO_NAME"])]
		if b == nil {
			continue
		}
		e := model.Entity{Table: str(x["TABLE_NAME"]), Parent: opt(x["PARENT_TABLE"]), ForeignKey: opt(x["FK_NAME"]), Numbering: model.Numbering(str(x["NUMBERING"])),
			Fields: map[string]model.FieldProperties{}, ETag: opt(x["ETAG_FIELD"]),
			Administrative: model.AdministrativeFields{CreatedBy: opt(x["CREATED_BY"]), CreatedAt: opt(x["CREATED_AT"]), ChangedBy: opt(x["CHANGED_BY"]), ChangedAt: opt(x["CHANGED_AT"])}}
		for op, col := range map[model.EntityOperation]string{model.OperationCreate: "CAN_CREATE", model.OperationUpdate: "CAN_UPDATE", model.OperationDelete: "CAN_DELETE"} {
			if flag(x[col]) {
				e.Operations = append(e.Operations, op)
			}
		}
		slices.SortFunc(e.Operations, func(a, b model.EntityOperation) int {
			return slices.Index(model.EntityOperations, a) - slices.Index(model.EntityOperations, b)
		})
		if o, ok := opt(x["AUTH_OBJECT"]).Get(); ok {
			e.Authorization = &model.EntityAuthorization{Object: o, Fields: map[string]string{}, Activities: map[string]string{}}
		}
		b.Entities = append(b.Entities, e)
	}
	fields, err := r.selectRows(ctx, q, TBusinessObjectFields, []string{"BO_NAME", "TABLE_NAME", "FIELD_NAME", "READ_ONLY", "READ_ONLY_ON_UPDATE", "MANDATORY"})
	if err != nil {
		return err
	}
	for _, x := range fields {
		if e := r.entityOf(s, x); e != nil {
			e.Fields[str(x["FIELD_NAME"])] = model.FieldProperties{ReadOnly: flag(x["READ_ONLY"]), ReadOnlyOnUpdate: flag(x["READ_ONLY_ON_UPDATE"]), Mandatory: flag(x["MANDATORY"])}
		}
	}
	triggers := map[[2]string][]string{}
	tf, err := r.selectRows(ctx, q, TBusinessObjectTrig, []string{"BO_NAME", "METHOD_NAME", "POSITION", "FIELD_NAME"}, "BO_NAME", "METHOD_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range tf {
		k := [2]string{str(x["BO_NAME"]), str(x["METHOD_NAME"])}
		triggers[k] = append(triggers[k], str(x["FIELD_NAME"]))
	}
	methods, err := r.selectRows(ctx, q, TBusinessObjectMethod, []string{"BO_NAME", "METHOD_NAME", "TABLE_NAME", "METHOD_KIND", "POSITION", "RUNS_WHEN",
		"ON_CREATE", "ON_UPDATE", "ON_DELETE", "IS_INSTANCE", "PARAMETER_TYPE", "RESULT_TYPE", "MODIFIES"}, "BO_NAME", "TABLE_NAME", "METHOD_KIND", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range methods {
		e := r.entityOf(s, x)
		if e == nil {
			continue
		}
		name := str(x["METHOD_NAME"])
		t := model.Trigger{Fields: triggers[[2]string{str(x["BO_NAME"]), name}]}
		for op, col := range map[model.EntityOperation]string{model.OperationCreate: "ON_CREATE", model.OperationUpdate: "ON_UPDATE", model.OperationDelete: "ON_DELETE"} {
			if flag(x[col]) {
				t.On = append(t.On, op)
			}
		}
		slices.SortFunc(t.On, func(a, b model.EntityOperation) int {
			return slices.Index(model.EntityOperations, a) - slices.Index(model.EntityOperations, b)
		})
		switch str(x["METHOD_KIND"]) {
		case methodDetermination:
			e.Determinations = append(e.Determinations, model.Determination{Name: name, When: model.When(str(x["RUNS_WHEN"])), Trigger: t})
		case methodValidation:
			e.Validations = append(e.Validations, model.Validation{Name: name, Trigger: t})
		case methodAction:
			e.Actions = append(e.Actions, model.Action{Name: name, Instance: flag(x["IS_INSTANCE"]), Parameter: opt(x["PARAMETER_TYPE"]),
				Result: opt(x["RESULT_TYPE"]), Modifies: flag(x["MODIFIES"])})
		}
	}
	auth, err := r.selectRows(ctx, q, TBusinessObjectAuth, []string{"BO_NAME", "TABLE_NAME", "MAPPING_KIND", "MAPPING_NAME", "MAPPING_VALUE"})
	if err != nil {
		return err
	}
	for _, x := range auth {
		e := r.entityOf(s, x)
		if e == nil || e.Authorization == nil {
			continue
		}
		if str(x["MAPPING_KIND"]) == "field" {
			e.Authorization.Fields[str(x["MAPPING_NAME"])] = str(x["MAPPING_VALUE"])
		} else {
			e.Authorization.Activities[str(x["MAPPING_NAME"])] = str(x["MAPPING_VALUE"])
		}
	}
	return nil
}

func (r *Repo) entityOf(s *model.Schema, x row) *model.Entity {
	if b := s.BusinessObjects[str(x["BO_NAME"])]; b != nil {
		return b.Entity(str(x["TABLE_NAME"]))
	}
	return nil
}

func (r *Repo) writeBusinessObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.BusinessObjectNames() {
		b := s.BusinessObjects[name]
		if err := r.insert(ctx, q, TBusinessObjects, row{"BO_NAME": name, "PACKAGE_ID": ownerValue(b.Owner), "FILE_PATH": b.File, "ROOT_TABLE": b.Root,
			"BEHAVIOR_CLASS": val(b.BehaviorClass), "DATA_CLASS": string(b.DataClass)}); err != nil {
			return err
		}
		for _, lang := range b.Languages() {
			if err := r.insert(ctx, q, TBusinessObjectTexts, row{"BO_NAME": name, "LANGUAGE": lang, "DESCRIPTION": b.Description[lang]}); err != nil {
				return err
			}
		}
		for i, e := range b.Entities {
			x := row{"BO_NAME": name, "TABLE_NAME": e.Table, "POSITION": int64(i + 1), "PARENT_TABLE": val(e.Parent), "FK_NAME": val(e.ForeignKey),
				"NUMBERING": string(e.Numbering), "CAN_CREATE": e.Allows(model.OperationCreate), "CAN_UPDATE": e.Allows(model.OperationUpdate),
				"CAN_DELETE": e.Allows(model.OperationDelete), "CREATED_BY": val(e.Administrative.CreatedBy), "CREATED_AT": val(e.Administrative.CreatedAt),
				"CHANGED_BY": val(e.Administrative.ChangedBy), "CHANGED_AT": val(e.Administrative.ChangedAt), "ETAG_FIELD": val(e.ETag), "AUTH_OBJECT": nil}
			if e.Authorization != nil {
				x["AUTH_OBJECT"] = e.Authorization.Object
			}
			if err := r.insert(ctx, q, TBusinessObjectEntity, x); err != nil {
				return err
			}
			for _, f := range slices.Sorted(maps.Keys(e.Fields)) {
				p := e.Fields[f]
				if err := r.insert(ctx, q, TBusinessObjectFields, row{"BO_NAME": name, "TABLE_NAME": e.Table, "FIELD_NAME": f, "READ_ONLY": p.ReadOnly,
					"READ_ONLY_ON_UPDATE": p.ReadOnlyOnUpdate, "MANDATORY": p.Mandatory}); err != nil {
					return err
				}
			}
			method := func(name2, kind string, pos int, t model.Trigger, extra row) error {
				x := row{"BO_NAME": name, "METHOD_NAME": name2, "TABLE_NAME": e.Table, "METHOD_KIND": kind, "POSITION": int64(pos + 1), "RUNS_WHEN": nil,
					"ON_CREATE": slices.Contains(t.On, model.OperationCreate), "ON_UPDATE": slices.Contains(t.On, model.OperationUpdate),
					"ON_DELETE": slices.Contains(t.On, model.OperationDelete), "IS_INSTANCE": false, "PARAMETER_TYPE": nil, "RESULT_TYPE": nil, "MODIFIES": false}
				maps.Copy(x, extra)
				if err := r.insert(ctx, q, TBusinessObjectMethod, x); err != nil {
					return err
				}
				for j, f := range t.Fields {
					if err := r.insert(ctx, q, TBusinessObjectTrig, row{"BO_NAME": name, "METHOD_NAME": name2, "POSITION": int64(j + 1), "FIELD_NAME": f}); err != nil {
						return err
					}
				}
				return nil
			}
			for j, d := range e.Determinations {
				if err := method(d.Name, methodDetermination, j, d.Trigger, row{"RUNS_WHEN": string(d.When)}); err != nil {
					return err
				}
			}
			for j, v := range e.Validations {
				if err := method(v.Name, methodValidation, j, v.Trigger, nil); err != nil {
					return err
				}
			}
			for j, a := range e.Actions {
				if err := method(a.Name, methodAction, j, model.Trigger{}, row{"IS_INSTANCE": a.Instance, "PARAMETER_TYPE": val(a.Parameter),
					"RESULT_TYPE": val(a.Result), "MODIFIES": a.Modifies}); err != nil {
					return err
				}
			}
			if au := e.Authorization; au != nil {
				for kind, m := range map[string]map[string]string{"field": au.Fields, "activity": au.Activities} {
					for _, k := range slices.Sorted(maps.Keys(m)) {
						if err := r.insert(ctx, q, TBusinessObjectAuth, row{"BO_NAME": name, "TABLE_NAME": e.Table, "MAPPING_KIND": kind, "MAPPING_NAME": k,
							"MAPPING_VALUE": m[k]}); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
