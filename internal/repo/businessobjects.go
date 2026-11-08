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
	TBusinessObjects          = model.KernelNamespace + "BUSINESS_OBJECTS"
	TBusinessObjectTexts      = model.KernelNamespace + "BUSINESS_OBJECT_TEXTS"
	TBusinessObjectEntity     = model.KernelNamespace + "BUSINESS_OBJECT_ENTITIES"
	TBusinessObjectFields     = model.KernelNamespace + "BUSINESS_OBJECT_FIELDS"
	TBusinessObjectMethod     = model.KernelNamespace + "BUSINESS_OBJECT_METHODS"
	TBusinessObjectTrig       = model.KernelNamespace + "BUSINESS_OBJECT_TRIGGER_FIELDS"
	TBusinessObjectAuth       = model.KernelNamespace + "BUSINESS_OBJECT_AUTH"
	TBusinessObjectExts       = model.KernelNamespace + "BUSINESS_OBJECT_EXTENSIONS"
	TBusinessObjectActAuth    = model.KernelNamespace + "BUSINESS_OBJECT_ACTION_AUTH"
	TBusinessObjectExtensible = model.KernelNamespace + "BUSINESS_OBJECT_EXTENSIBLE"
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

// boRows holds the definitions of a business object and its extensions
// while they are read: the entities by the package that defines them.
type boRows struct {
	s *model.Schema
}

// entityOf returns the entity of a row: of the business object when the
// row has no package or that of its owner, else of the extension of the
// package.
func (x boRows) entityOf(row row) *model.Entity {
	b := x.s.BusinessObjects[str(row["BO_NAME"])]
	if b == nil {
		return nil
	}
	if pkg, ok := opt(row["PACKAGE_ID"]).Get(); ok && model.PackageID(pkg) != b.Owner {
		if ext := x.s.BusinessObjectExtensions[model.BusinessObjectExtensionKey{Package: model.PackageID(pkg), BusinessObject: b.Name}]; ext != nil {
			return ext.Entity(str(row["TABLE_NAME"]))
		}
		return nil
	}
	return b.Entity(str(row["TABLE_NAME"]))
}

func sortOps(ops []model.EntityOperation) {
	slices.SortFunc(ops, func(a, b model.EntityOperation) int {
		return slices.Index(model.EntityOperations, a) - slices.Index(model.EntityOperations, b)
	})
}

func (r *Repo) loadBusinessObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	bos, err := r.selectRows(ctx, q, TBusinessObjects, []string{"BO_NAME", "PACKAGE_ID", "FILE_PATH", "ROOT_TABLE", "BEHAVIOR_CLASS", "DATA_CLASS",
		"EXTENSIBLE_LISTED"}, "BO_NAME")
	if err != nil {
		return err
	}
	for _, x := range bos {
		name := str(x["BO_NAME"])
		b := &model.BusinessObject{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]), Description: map[string]string{},
			Root: str(x["ROOT_TABLE"]), BehaviorClass: opt(x["BEHAVIOR_CLASS"]), DataClass: model.DataClass(str(x["DATA_CLASS"]))}
		if b.DataClass == "" {
			b.DataClass = model.Application
		}
		if flag(x["EXTENSIBLE_LISTED"]) {
			b.Extensible = model.Some([]model.ExtensionKind{})
		}
		s.BusinessObjects[name] = b
	}
	exts, err := r.selectRows(ctx, q, TBusinessObjectExts, []string{"BO_NAME", "PACKAGE_ID", "FILE_PATH", "BEHAVIOR_CLASS"}, "BO_NAME", "PACKAGE_ID")
	if err != nil {
		return err
	}
	for _, x := range exts {
		ext := &model.BusinessObjectExtension{BusinessObject: str(x["BO_NAME"]), Package: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]),
			BehaviorClass: opt(x["BEHAVIOR_CLASS"])}
		s.BusinessObjectExtensions[ext.Key()] = ext
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
	rows := boRows{s: s}
	entities, err := r.selectRows(ctx, q, TBusinessObjectEntity, []string{"BO_NAME", "TABLE_NAME", "PACKAGE_ID", "POSITION", "PARENT_TABLE", "FK_NAME", "NUMBERING",
		"CAN_CREATE", "CAN_UPDATE", "CAN_DELETE", "CREATED_BY", "CREATED_AT", "CHANGED_BY", "CHANGED_AT", "ETAG_FIELD", "AUTH_OBJECT", "EXTENSIBLE_LISTED"},
		"BO_NAME", "PACKAGE_ID", "POSITION")
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
		sortOps(e.Operations)
		if o, ok := opt(x["AUTH_OBJECT"]).Get(); ok {
			e.Authorization = &model.EntityAuthorization{Object: o, Fields: map[string]string{}, Activities: map[string]string{}}
		}
		if flag(x["EXTENSIBLE_LISTED"]) {
			e.Extensible = model.Some([]model.ExtensionKind{})
		}
		if pkg, ok := opt(x["PACKAGE_ID"]).Get(); ok && model.PackageID(pkg) != b.Owner {
			if ext := s.BusinessObjectExtensions[model.BusinessObjectExtensionKey{Package: model.PackageID(pkg), BusinessObject: b.Name}]; ext != nil {
				ext.Entities = append(ext.Entities, e)
			}
			continue
		}
		b.Entities = append(b.Entities, e)
	}
	fields, err := r.selectRows(ctx, q, TBusinessObjectFields, []string{"BO_NAME", "TABLE_NAME", "PACKAGE_ID", "FIELD_NAME", "READ_ONLY", "READ_ONLY_ON_UPDATE", "MANDATORY"})
	if err != nil {
		return err
	}
	for _, x := range fields {
		if e := rows.entityOf(x); e != nil {
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
	actionAuth := map[[2]string]map[string]string{}
	aa, err := r.selectRows(ctx, q, TBusinessObjectActAuth, []string{"BO_NAME", "METHOD_NAME", "FIELD_NAME", "ENTITY_FIELD"})
	if err != nil {
		return err
	}
	for _, x := range aa {
		k := [2]string{str(x["BO_NAME"]), str(x["METHOD_NAME"])}
		if actionAuth[k] == nil {
			actionAuth[k] = map[string]string{}
		}
		actionAuth[k][str(x["FIELD_NAME"])] = str(x["ENTITY_FIELD"])
	}
	methods, err := r.selectRows(ctx, q, TBusinessObjectMethod, []string{"BO_NAME", "METHOD_NAME", "TABLE_NAME", "PACKAGE_ID", "METHOD_KIND", "POSITION", "RUNS_WHEN",
		"ON_CREATE", "ON_UPDATE", "ON_DELETE", "IS_INSTANCE", "PARAMETER_TYPE", "RESULT_TYPE", "MODIFIES", "ACTIVITY", "AUTH_OBJECT"},
		"BO_NAME", "TABLE_NAME", "METHOD_KIND", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range methods {
		e := rows.entityOf(x)
		if e == nil {
			continue
		}
		name := str(x["METHOD_NAME"])
		k := [2]string{str(x["BO_NAME"]), name}
		t := model.Trigger{Fields: triggers[k]}
		for op, col := range map[model.EntityOperation]string{model.OperationCreate: "ON_CREATE", model.OperationUpdate: "ON_UPDATE", model.OperationDelete: "ON_DELETE"} {
			if flag(x[col]) {
				t.On = append(t.On, op)
			}
		}
		sortOps(t.On)
		switch str(x["METHOD_KIND"]) {
		case methodDetermination:
			e.Determinations = append(e.Determinations, model.Determination{Name: name, When: model.When(str(x["RUNS_WHEN"])), Trigger: t})
		case methodValidation:
			e.Validations = append(e.Validations, model.Validation{Name: name, Trigger: t})
		case methodAction:
			a := model.Action{Name: name, Instance: flag(x["IS_INSTANCE"]), Parameter: opt(x["PARAMETER_TYPE"]),
				Result: opt(x["RESULT_TYPE"]), Modifies: flag(x["MODIFIES"]), Activity: opt(x["ACTIVITY"])}
			if o, ok := opt(x["AUTH_OBJECT"]).Get(); ok {
				a.Authorization = &model.ActionAuthorization{Object: o, Fields: actionAuth[k]}
				if a.Authorization.Fields == nil {
					a.Authorization.Fields = map[string]string{}
				}
			}
			e.Actions = append(e.Actions, a)
		}
	}
	auth, err := r.selectRows(ctx, q, TBusinessObjectAuth, []string{"BO_NAME", "TABLE_NAME", "PACKAGE_ID", "MAPPING_KIND", "MAPPING_NAME", "MAPPING_VALUE"})
	if err != nil {
		return err
	}
	for _, x := range auth {
		e := rows.entityOf(x)
		if e == nil || e.Authorization == nil {
			continue
		}
		if str(x["MAPPING_KIND"]) == "field" {
			e.Authorization.Fields[str(x["MAPPING_NAME"])] = str(x["MAPPING_VALUE"])
		} else {
			e.Authorization.Activities[str(x["MAPPING_NAME"])] = str(x["MAPPING_VALUE"])
		}
	}
	kinds, err := r.selectRows(ctx, q, TBusinessObjectExtensible, []string{"BO_NAME", "POSITION", "TABLE_NAME", "EXTENSION_KIND"}, "BO_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range kinds {
		b := s.BusinessObjects[str(x["BO_NAME"])]
		if b == nil {
			continue
		}
		kind := model.ExtensionKind(str(x["EXTENSION_KIND"]))
		target := &b.Extensible
		if table, ok := opt(x["TABLE_NAME"]).Get(); ok {
			if e := b.Entity(table); e != nil {
				target = &e.Extensible
			}
		}
		*target = model.Some(append(target.Or(nil), kind))
	}
	for _, ext := range s.BusinessObjectExtensions {
		var extended, own []model.Entity
		for _, e := range ext.Entities {
			if e.Parent.Valid() {
				own = append(own, e)
			} else {
				extended = append(extended, e)
			}
		}
		ext.Entities = append(extended, own...)
	}
	return nil
}

func (r *Repo) writeBusinessObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.BusinessObjectNames() {
		b := s.BusinessObjects[name]
		if err := r.insert(ctx, q, TBusinessObjects, row{"BO_NAME": name, "PACKAGE_ID": ownerValue(b.Owner), "FILE_PATH": b.File, "ROOT_TABLE": b.Root,
			"BEHAVIOR_CLASS": val(b.BehaviorClass), "DATA_CLASS": string(b.DataClass), "EXTENSIBLE_LISTED": b.Extensible.Valid()}); err != nil {
			return err
		}
		for _, lang := range b.Languages() {
			if err := r.insert(ctx, q, TBusinessObjectTexts, row{"BO_NAME": name, "LANGUAGE": lang, "DESCRIPTION": b.Description[lang]}); err != nil {
				return err
			}
		}
		if err := r.writeEntities(ctx, q, name, b.Owner, b.Entities); err != nil {
			return err
		}
		pos := 0
		kinds := func(table any, l model.Optional[[]model.ExtensionKind]) error {
			for _, k := range l.Or(nil) {
				pos++
				if err := r.insert(ctx, q, TBusinessObjectExtensible, row{"BO_NAME": name, "POSITION": int64(pos), "TABLE_NAME": table, "EXTENSION_KIND": string(k)}); err != nil {
					return err
				}
			}
			return nil
		}
		if err := kinds(nil, b.Extensible); err != nil {
			return err
		}
		for _, e := range b.Entities {
			if err := kinds(e.Table, e.Extensible); err != nil {
				return err
			}
		}
	}
	for _, ext := range s.SortedBusinessObjectExtensions() {
		if err := r.insert(ctx, q, TBusinessObjectExts, row{"BO_NAME": ext.BusinessObject, "PACKAGE_ID": string(ext.Package), "FILE_PATH": ext.File,
			"BEHAVIOR_CLASS": val(ext.BehaviorClass)}); err != nil {
			return err
		}
		if err := r.writeEntities(ctx, q, ext.BusinessObject, ext.Package, ext.Entities); err != nil {
			return err
		}
	}
	return nil
}

// writeEntities writes the entities a package defines for a business
// object: its own as owner, or those an extension adds or extends.
func (r *Repo) writeEntities(ctx context.Context, q dialect.Querier, name string, pkg model.PackageID, entities []model.Entity) error {
	for i, e := range entities {
		x := row{"BO_NAME": name, "TABLE_NAME": e.Table, "PACKAGE_ID": ownerValue(pkg), "POSITION": int64(i + 1), "PARENT_TABLE": val(e.Parent),
			"FK_NAME": val(e.ForeignKey), "NUMBERING": nil, "CAN_CREATE": e.Allows(model.OperationCreate), "CAN_UPDATE": e.Allows(model.OperationUpdate),
			"CAN_DELETE": e.Allows(model.OperationDelete), "CREATED_BY": val(e.Administrative.CreatedBy), "CREATED_AT": val(e.Administrative.CreatedAt),
			"CHANGED_BY": val(e.Administrative.ChangedBy), "CHANGED_AT": val(e.Administrative.ChangedAt), "ETAG_FIELD": val(e.ETag), "AUTH_OBJECT": nil,
			"EXTENSIBLE_LISTED": e.Extensible.Valid()}
		if e.Numbering != "" {
			x["NUMBERING"] = string(e.Numbering)
		}
		if e.Authorization != nil {
			x["AUTH_OBJECT"] = e.Authorization.Object
		}
		if err := r.insert(ctx, q, TBusinessObjectEntity, x); err != nil {
			return err
		}
		for _, f := range slices.Sorted(maps.Keys(e.Fields)) {
			p := e.Fields[f]
			if err := r.insert(ctx, q, TBusinessObjectFields, row{"BO_NAME": name, "TABLE_NAME": e.Table, "PACKAGE_ID": ownerValue(pkg), "FIELD_NAME": f,
				"READ_ONLY": p.ReadOnly, "READ_ONLY_ON_UPDATE": p.ReadOnlyOnUpdate, "MANDATORY": p.Mandatory}); err != nil {
				return err
			}
		}
		method := func(name2, kind string, pos int, t model.Trigger, extra row) error {
			x := row{"BO_NAME": name, "METHOD_NAME": name2, "TABLE_NAME": e.Table, "PACKAGE_ID": ownerValue(pkg), "METHOD_KIND": kind, "POSITION": int64(pos + 1),
				"RUNS_WHEN": nil, "ON_CREATE": slices.Contains(t.On, model.OperationCreate), "ON_UPDATE": slices.Contains(t.On, model.OperationUpdate),
				"ON_DELETE": slices.Contains(t.On, model.OperationDelete), "IS_INSTANCE": false, "PARAMETER_TYPE": nil, "RESULT_TYPE": nil, "MODIFIES": false,
				"ACTIVITY": nil, "AUTH_OBJECT": nil}
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
			extra := row{"IS_INSTANCE": a.Instance, "PARAMETER_TYPE": val(a.Parameter), "RESULT_TYPE": val(a.Result), "MODIFIES": a.Modifies,
				"ACTIVITY": val(a.Activity)}
			if a.Authorization != nil {
				extra["AUTH_OBJECT"] = a.Authorization.Object
			}
			if err := method(a.Name, methodAction, j, model.Trigger{}, extra); err != nil {
				return err
			}
			if a.Authorization != nil {
				for _, f := range slices.Sorted(maps.Keys(a.Authorization.Fields)) {
					if err := r.insert(ctx, q, TBusinessObjectActAuth, row{"BO_NAME": name, "METHOD_NAME": a.Name, "FIELD_NAME": f,
						"ENTITY_FIELD": a.Authorization.Fields[f]}); err != nil {
						return err
					}
				}
			}
		}
		if au := e.Authorization; au != nil {
			for kind, m := range map[string]map[string]string{"field": au.Fields, "activity": au.Activities} {
				for _, k := range slices.Sorted(maps.Keys(m)) {
					if err := r.insert(ctx, q, TBusinessObjectAuth, row{"BO_NAME": name, "TABLE_NAME": e.Table, "PACKAGE_ID": ownerValue(pkg),
						"MAPPING_KIND": kind, "MAPPING_NAME": k, "MAPPING_VALUE": m[k]}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
