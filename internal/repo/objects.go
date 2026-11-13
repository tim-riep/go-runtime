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
	"encoding/json"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Tables of package-defined object types and their objects.
const (
	TObjectTypes      = model.KernelNamespace + "OBJECT_TYPES"
	TObjectTypeTexts  = model.KernelNamespace + "OBJECT_TYPE_TEXTS"
	TObjects          = model.KernelNamespace + "OBJECTS"
	TObjectReferences = model.KernelNamespace + "OBJECT_REFERENCES"
)

// HasObjects reports whether the kernel tables have package-defined object
// types.
func (r *Repo) HasObjects() bool { return r.has(TObjectTypes, "") }

func (r *Repo) loadObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	if !r.HasObjects() {
		return nil
	}
	types, err := r.selectRows(ctx, q, TObjectTypes, []string{"TYPE_NAME", "PACKAGE_ID", "FILE_PATH", "CONTENT_STRUCTURE", "BEHAVIOR_CLASS"}, "TYPE_NAME")
	if err != nil {
		return err
	}
	for _, x := range types {
		name := str(x["TYPE_NAME"])
		s.ObjectTypes[name] = &model.ObjectType{Name: name, Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]),
			Description: map[string]string{}, Content: str(x["CONTENT_STRUCTURE"]), Behavior: opt(x["BEHAVIOR_CLASS"])}
	}
	texts, err := r.selectRows(ctx, q, TObjectTypeTexts, []string{"TYPE_NAME", "LANGUAGE", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if ot := s.ObjectTypes[str(t["TYPE_NAME"])]; ot != nil {
			ot.Description[str(t["LANGUAGE"])] = str(t["DESCRIPTION"])
		}
	}
	objs, err := r.selectRows(ctx, q, TObjects, []string{"OBJECT_NAME", "TYPE_NAME", "PACKAGE_ID", "FILE_PATH", "CONTENT", "SHA256"}, "OBJECT_NAME")
	if err != nil {
		return err
	}
	for _, x := range objs {
		name := str(x["OBJECT_NAME"])
		s.Objects[name] = &model.Object{Name: name, Type: str(x["TYPE_NAME"]), Owner: owner(x["PACKAGE_ID"]), File: str(x["FILE_PATH"]),
			Content: json.RawMessage(str(x["CONTENT"])), Hash: str(x["SHA256"]), References: []model.ObjectReference{}}
		s.Objects[name].Src = rterr.Location{Package: string(s.Objects[name].Owner), File: s.Objects[name].File}
	}
	refs, err := r.selectRows(ctx, q, TObjectReferences, []string{"OBJECT_NAME", "POSITION", "KIND", "TARGET", "PART", "TARGET_TYPE", "POINTER"},
		"OBJECT_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, x := range refs {
		if o := s.Objects[str(x["OBJECT_NAME"])]; o != nil {
			o.References = append(o.References, model.ObjectReference{Kind: model.ReferenceKind(str(x["KIND"])), Target: str(x["TARGET"]),
				Part: opt(x["PART"]).Or(""), ObjectType: opt(x["TARGET_TYPE"]).Or(""), Pointer: str(x["POINTER"])})
		}
	}
	return nil
}

func (r *Repo) writeObjects(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	if !r.HasObjects() {
		return nil
	}
	for _, name := range s.ObjectTypeNames() {
		t := s.ObjectTypes[name]
		if err := r.insert(ctx, q, TObjectTypes, row{"TYPE_NAME": name, "PACKAGE_ID": string(t.Owner), "FILE_PATH": t.File,
			"CONTENT_STRUCTURE": t.Content, "BEHAVIOR_CLASS": val(t.Behavior)}); err != nil {
			return err
		}
		for _, lang := range t.Languages() {
			if err := r.insert(ctx, q, TObjectTypeTexts, row{"TYPE_NAME": name, "LANGUAGE": lang, "DESCRIPTION": t.Description[lang]}); err != nil {
				return err
			}
		}
	}
	for _, name := range s.ObjectNames() {
		o := s.Objects[name]
		if err := r.insert(ctx, q, TObjects, row{"OBJECT_NAME": name, "TYPE_NAME": o.Type, "PACKAGE_ID": string(o.Owner), "FILE_PATH": o.File,
			"CONTENT": string(o.Content), "SHA256": o.Hash}); err != nil {
			return err
		}
		for i, ref := range o.References {
			if err := r.insert(ctx, q, TObjectReferences, row{"OBJECT_NAME": name, "POSITION": i + 1, "KIND": string(ref.Kind), "TARGET": ref.Target,
				"PART": nonEmpty(ref.Part), "TARGET_TYPE": nonEmpty(ref.ObjectType), "POINTER": ref.Pointer}); err != nil {
				return err
			}
		}
	}
	return nil
}

func nonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
