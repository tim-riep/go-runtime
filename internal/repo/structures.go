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
	"fmt"
	"strings"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Kernel tables of structures, array types and includes.
const (
	TStructures          = model.KernelNamespace + "STRUCTURES"
	TStructureTexts      = model.KernelNamespace + "STRUCTURE_TEXTS"
	TStructureElements   = model.KernelNamespace + "STRUCTURE_ELEMENTS"
	TStructureExtensions = model.KernelNamespace + "STRUCTURE_EXTENSIONS"
	TArrayTypes          = model.KernelNamespace + "ARRAY_TYPES"
	TArrayTypeTexts      = model.KernelNamespace + "ARRAY_TYPE_TEXTS"
	TArrayTypeKeys       = model.KernelNamespace + "ARRAY_TYPE_KEYS"
	TTableIncludes       = model.KernelNamespace + "TABLE_INCLUDES"
)

// Values of ELEMENT_KIND.
const (
	elementComponent = "COMPONENT"
	elementInclude   = "INCLUDE"
)

var typeKinds = map[model.TypeKind]string{
	model.TypeDataElement: "DATA_ELEMENT",
	model.TypeDomain:      "DOMAIN",
	model.TypeStructure:   "STRUCTURE",
	model.TypeArray:       "ARRAY_TYPE",
}

func typeKindValue(k model.TypeKind) string { return typeKinds[k] }

func readTypeKind(v any) model.TypeKind {
	for k, s := range typeKinds {
		if s == str(v) {
			return k
		}
	}
	return model.TypeKind(str(v))
}

func (r *Repo) loadStructures(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	sts, err := r.selectRows(ctx, q, TStructures, []string{"STRUCTURE_NAME", "PACKAGE_ID"})
	if err != nil {
		return err
	}
	for _, st := range sts {
		name := str(st["STRUCTURE_NAME"])
		s.Structures[name] = &model.Structure{Name: name, Owner: owner(st["PACKAGE_ID"]), Texts: map[string]model.Label{}}
	}
	texts, err := r.selectRows(ctx, q, TStructureTexts, []string{"STRUCTURE_NAME", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range texts {
		if st := s.Structures[str(t["STRUCTURE_NAME"])]; st != nil {
			st.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	exts, err := r.selectRows(ctx, q, TStructureExtensions, []string{"PACKAGE_ID", "STRUCTURE_NAME"})
	if err != nil {
		return err
	}
	for _, e := range exts {
		ext := &model.StructureExtension{Package: owner(e["PACKAGE_ID"]), Structure: str(e["STRUCTURE_NAME"])}
		s.StructureExtensions[ext.Key()] = ext
	}
	elems, err := r.selectRows(ctx, q, TStructureElements, []string{"STRUCTURE_NAME", "ELEMENT_KIND", "COMPONENT_NAME", "PACKAGE_ID",
		"ORIGIN", "TYPE_KIND", "TYPE_NAME", "INCLUDE_NAME", "NOT_NULL", "DEFAULT_KIND", "DEFAULT_VALUE", "DESCRIPTION"}, "STRUCTURE_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, e := range elems {
		name := str(e["STRUCTURE_NAME"])
		st := s.Structures[name]
		if st == nil {
			return fmt.Errorf("repository: element of unknown structure %s", name)
		}
		if str(e["ELEMENT_KIND"]) == elementInclude {
			st.Elements = append(st.Elements, model.Element{Included: opt(e["INCLUDE_NAME"])})
			continue
		}
		c := model.Component{Name: str(e["COMPONENT_NAME"]), Owner: owner(e["PACKAGE_ID"]), Origin: model.ComponentOrigin(str(e["ORIGIN"])),
			Description: opt(e["DESCRIPTION"]), Type: model.TypeRef{Kind: readTypeKind(e["TYPE_KIND"]), Name: str(e["TYPE_NAME"])},
			NotNull: flag(e["NOT_NULL"]), Default: readDefault(e["DEFAULT_KIND"], e["DEFAULT_VALUE"])}
		if c.Origin == model.ComponentExtension {
			ext := s.StructureExtensions[model.StructureExtensionKey{Package: c.Owner, Structure: name}]
			if ext == nil {
				return fmt.Errorf("repository: extension component %s of %s without extension", c.Name, name)
			}
			ext.Components = append(ext.Components, c)
			continue
		}
		st.Elements = append(st.Elements, model.Element{Component: &c})
	}

	arrs, err := r.selectRows(ctx, q, TArrayTypes, []string{"ARRAY_TYPE", "PACKAGE_ID", "ROW_KIND", "ROW_TYPE", "KEY_UNIQUE"})
	if err != nil {
		return err
	}
	for _, a := range arrs {
		name := str(a["ARRAY_TYPE"])
		at := &model.ArrayType{Name: name, Owner: owner(a["PACKAGE_ID"]), Texts: map[string]model.Label{},
			Row: model.TypeRef{Kind: readTypeKind(a["ROW_KIND"]), Name: str(a["ROW_TYPE"])}}
		if u, ok := optBool(a["KEY_UNIQUE"]).Get(); ok {
			at.Key = &model.ArrayKey{Unique: u}
		}
		s.ArrayTypes[name] = at
	}
	arrTexts, err := r.selectRows(ctx, q, TArrayTypeTexts, []string{"ARRAY_TYPE", "LANGUAGE", "LABEL", "DESCRIPTION"})
	if err != nil {
		return err
	}
	for _, t := range arrTexts {
		if a := s.ArrayTypes[str(t["ARRAY_TYPE"])]; a != nil {
			a.Texts[str(t["LANGUAGE"])] = model.Label{Label: str(t["LABEL"]), Description: opt(t["DESCRIPTION"])}
		}
	}
	keys, err := r.selectRows(ctx, q, TArrayTypeKeys, []string{"ARRAY_TYPE", "FIELD_NAME"}, "ARRAY_TYPE", "POSITION")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if a := s.ArrayTypes[str(k["ARRAY_TYPE"])]; a != nil && a.Key != nil {
			a.Key.Fields = append(a.Key.Fields, str(k["FIELD_NAME"]))
		}
	}

	incs, err := r.selectRows(ctx, q, TTableIncludes, []string{"TABLE_NAME", "INCLUDE_NAME", "AFTER_FIELDS"}, "TABLE_NAME", "POSITION")
	if err != nil {
		return err
	}
	for _, i := range incs {
		t := s.Tables[str(i["TABLE_NAME"])]
		if t == nil {
			return fmt.Errorf("repository: include %s of unknown table %s", str(i["INCLUDE_NAME"]), str(i["TABLE_NAME"]))
		}
		t.Includes = append(t.Includes, model.TableInclude{Name: str(i["INCLUDE_NAME"]), After: num(i["AFTER_FIELDS"])})
	}
	return nil
}

func (r *Repo) writeStructures(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	for _, name := range s.StructureNames() {
		st := s.Structures[name]
		if err := r.insert(ctx, q, TStructures, row{"STRUCTURE_NAME": name, "PACKAGE_ID": string(st.Owner)}); err != nil {
			return err
		}
		for _, lang := range st.Languages() {
			t := st.Texts[lang]
			if err := r.insert(ctx, q, TStructureTexts, row{"STRUCTURE_NAME": name, "LANGUAGE": lang, "LABEL": t.Label, "DESCRIPTION": val(t.Description)}); err != nil {
				return err
			}
		}
	}
	for _, e := range s.SortedStructureExtensions() {
		if err := r.insert(ctx, q, TStructureExtensions, row{"PACKAGE_ID": string(e.Package), "STRUCTURE_NAME": e.Structure}); err != nil {
			return err
		}
	}
	for _, name := range s.StructureNames() {
		st := s.Structures[name]
		pos := 0
		component := func(c *model.Component) error {
			pos++
			kind, value := writeDefault(c.Default)
			return r.insert(ctx, q, TStructureElements, row{"STRUCTURE_NAME": name, "POSITION": int64(pos), "ELEMENT_KIND": elementComponent,
				"COMPONENT_NAME": c.Name, "PACKAGE_ID": string(c.Owner), "ORIGIN": string(c.Origin), "TYPE_KIND": typeKindValue(c.Type.Kind),
				"TYPE_NAME": c.Type.Name, "NOT_NULL": c.NotNull, "DEFAULT_KIND": kind, "DEFAULT_VALUE": value, "DESCRIPTION": val(c.Description)})
		}
		for _, e := range st.Elements {
			if inc, ok := e.Included.Get(); ok {
				pos++
				kind, _ := writeDefault(model.NoDefault())
				if err := r.insert(ctx, q, TStructureElements, row{"STRUCTURE_NAME": name, "POSITION": int64(pos), "ELEMENT_KIND": elementInclude,
					"PACKAGE_ID": string(st.Owner), "INCLUDE_NAME": inc, "NOT_NULL": false, "DEFAULT_KIND": kind}); err != nil {
					return err
				}
				continue
			}
			if err := component(e.Component); err != nil {
				return err
			}
		}
		for _, ext := range s.ExtensionsOfStructure(name) {
			for i := range ext.Components {
				if err := component(&ext.Components[i]); err != nil {
					return err
				}
			}
		}
	}

	for _, name := range s.ArrayTypeNames() {
		a := s.ArrayTypes[name]
		var unique any
		if a.Key != nil {
			unique = a.Key.Unique
		}
		if err := r.insert(ctx, q, TArrayTypes, row{"ARRAY_TYPE": name, "PACKAGE_ID": string(a.Owner), "ROW_KIND": typeKindValue(a.Row.Kind),
			"ROW_TYPE": a.Row.Name, "KEY_UNIQUE": unique}); err != nil {
			return err
		}
		for _, lang := range a.Languages() {
			t := a.Texts[lang]
			if err := r.insert(ctx, q, TArrayTypeTexts, row{"ARRAY_TYPE": name, "LANGUAGE": lang, "LABEL": t.Label, "DESCRIPTION": val(t.Description)}); err != nil {
				return err
			}
		}
		if a.Key != nil {
			for i, f := range a.Key.Fields {
				if err := r.insert(ctx, q, TArrayTypeKeys, row{"ARRAY_TYPE": name, "POSITION": int64(i + 1), "FIELD_NAME": f}); err != nil {
					return err
				}
			}
		}
	}

	for _, name := range s.TableNames() {
		for i, inc := range s.Tables[name].Includes {
			if err := r.insert(ctx, q, TTableIncludes, row{"TABLE_NAME": name, "POSITION": int64(i + 1), "INCLUDE_NAME": inc.Name,
				"AFTER_FIELDS": int64(inc.After)}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ColumnProblem is a stored column that differs from the column computed
// from the tables, their includes and extensions.
type ColumnProblem struct {
	Object  string
	Problem string
}

// CheckColumns compares the columns stored in the repository with those
// that phys computes, including the origin of included columns.
func (r *Repo) CheckColumns(ctx context.Context, q dialect.Querier, phys *model.Physical) ([]ColumnProblem, error) {
	if !r.has(TFields, "INCLUDE_NAME") {
		return nil, nil
	}
	rows, err := r.selectRows(ctx, q, TFields, []string{"TABLE_NAME", "FIELD_NAME", "ORIGIN", "INCLUDE_NAME", "SOURCE_NAME"}, "TABLE_NAME", "POSITION")
	if err != nil {
		return nil, err
	}
	stored := map[string][]row{}
	for _, f := range rows {
		stored[str(f["TABLE_NAME"])] = append(stored[str(f["TABLE_NAME"])], f)
	}
	var out []ColumnProblem
	describe := func(origin string, inc, src model.Optional[string]) string {
		if i, ok := inc.Get(); ok {
			return fmt.Sprintf("%s from include %s, declared by %s", origin, i, src.Or("?"))
		}
		return origin
	}
	for _, name := range phys.TableNames() {
		t := phys.Tables[name]
		byName := map[string]row{}
		var storedNames []string
		for _, f := range stored[name] {
			byName[str(f["FIELD_NAME"])] = f
			storedNames = append(storedNames, str(f["FIELD_NAME"]))
		}
		before := len(out)
		var computed []string
		for _, f := range t.Fields {
			computed = append(computed, f.Name)
			sf, ok := byName[f.Name]
			if !ok {
				out = append(out, ColumnProblem{name + "." + f.Name, "column " + describe(string(f.Origin), f.Included, f.Source) + " is not stored in the repository"})
				continue
			}
			want := describe(string(f.Origin), f.Included, f.Source)
			if got := describe(str(sf["ORIGIN"]), opt(sf["INCLUDE_NAME"]), opt(sf["SOURCE_NAME"])); got != want {
				out = append(out, ColumnProblem{name + "." + f.Name, "stored as " + got + ", resolved as " + want})
			}
		}
		for _, n := range storedNames {
			if t.Field(n) == nil {
				out = append(out, ColumnProblem{name + "." + n, "stored column is not part of the resolved table"})
			}
		}
		if len(out) == before && strings.Join(storedNames, ",") != strings.Join(computed, ",") {
			out = append(out, ColumnProblem{name, "stored column order " + strings.Join(storedNames, ", ") + " differs from " + strings.Join(computed, ", ")})
		}
	}
	return out, nil
}
