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

package validate

import (
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// settings checks all settings and setting extensions. Problems are
// reported for the setting they make invalid, so a change of another
// package that breaks a setting names it.
func (v *validator) settings() {
	phys := v.s.Physical()
	owners := map[string]string{} // table -> setting it belongs to
	for _, e := range v.s.SortedSettingExtensions() {
		v.settingExtension(e)
	}
	for _, id := range v.s.SettingIDs() {
		v.setting(v.s.Settings[id], phys, owners)
	}
}

func (v *validator) settingExtension(e *model.SettingExtension) {
	obj := "setting " + e.Setting
	st, ok := v.s.Settings[e.Setting]
	switch {
	case !ok:
		v.fail(rterr.CodeSetting, e.Src.At("/setting"), obj, "extension by %s: the setting does not exist", e.Package)
	case st.Owner == e.Package:
		v.fail(rterr.CodeSetting, e.Src.At("/setting"), obj, "a package cannot extend its own setting")
	case !v.s.Requires(e.Package)[st.Owner]:
		v.fail(rterr.CodeNotVisible, e.Src.At("/setting"), obj, "extension by %s: the setting belongs to package %s, which %s does not require", e.Package, st.Owner, e.Package)
	}
}

func (v *validator) setting(st *model.Setting, phys *model.Physical, owners map[string]string) {
	obj := "setting " + st.ID
	v.objectName(st.ID, st.Owner, st.Src.At("/id"), obj)
	for _, lang := range st.Languages() {
		if n := utf8.RuneCountInString(st.Texts[lang].Label); n == 0 || n > model.MaxSettingLabel {
			v.fail(rterr.CodeText, st.Src.At("/texts/"+lang+"/label"), obj, "label in %s must have 1 to %d characters", lang, model.MaxSettingLabel)
		}
	}
	v.languages(obj, st.Src, st.Languages())

	nodes := v.s.SettingNodes(st.ID)
	root, ok := v.s.Tables[st.Root.Table]
	if !ok {
		v.fail(rterr.CodeSetting, st.Root.Src.At("/table"), obj, "root table %s does not exist", st.Root.Table)
		return
	}
	inTree := map[string]model.SettingNode{}
	for _, n := range nodes {
		loc := n.Src
		t, ok := v.s.Tables[n.Table]
		if !ok {
			v.fail(rterr.CodeSetting, loc.At("/table"), obj, "table %s does not exist", n.Table)
			continue
		}
		if _, dup := inTree[n.Table]; dup {
			v.fail(rterr.CodeSetting, loc.At("/table"), obj, "table %s is part of the setting more than once", n.Table)
			continue
		}
		inTree[n.Table] = n
		if other, taken := owners[n.Table]; taken {
			v.fail(rterr.CodeSetting, loc.At("/table"), obj, "table %s already belongs to setting %s; a table belongs to at most one setting", n.Table, other)
		}
		owners[n.Table] = st.ID
		switch {
		case t.Owner == model.Kernel:
			v.fail(rterr.CodeReservedObject, loc.At("/table"), obj, "kernel table %s cannot be part of a setting", t.Name)
		case !v.s.Visible(n.Origin, t.Owner):
			v.fail(rterr.CodeNotVisible, loc.At("/table"), obj, "table %s belongs to package %s, which %s does not require", t.Name, t.Owner, n.Origin)
		}
		switch {
		case !t.DataClass.IsConfiguration():
			v.fail(rterr.CodeSetting, loc.At("/table"), obj, "table %s has the data class %s; settings contain only configuration and runtimeConfiguration tables", t.Name, t.DataClass)
		case t.DataClass != root.DataClass:
			v.fail(rterr.CodeSetting, loc.At("/table"), obj, "table %s has the data class %s, the root table %s has %s; all tables of a setting have the same class", t.Name, t.DataClass, root.Name, root.DataClass)
		}
		if t.TenantSpecific != root.TenantSpecific {
			v.fail(rterr.CodeTenant, loc.At("/table"), obj, "table %s is tenant-specific %v, the root table %s %v; all tables of a setting have the same tenant flag", t.Name, t.TenantSpecific, root.Name, root.TenantSpecific)
		}
	}

	// Every dependent table hangs below a table of the setting through one
	// of its foreign keys, and the whole tree is reachable from the root.
	children := map[string][]string{}
	for _, n := range nodes {
		if n.Root {
			continue
		}
		loc := n.Src
		parentName := n.Parent.Or("")
		parent, ok := inTree[parentName]
		switch {
		case !n.Parent.Valid():
			v.fail(rterr.CodeSetting, loc.At("/parent"), obj, "table %s has no parent", n.Table)
			continue
		case !ok:
			v.fail(rterr.CodeSetting, loc.At("/parent"), obj, "parent %s of table %s is not part of the setting", parentName, n.Table)
			continue
		case parentName == n.Table:
			v.fail(rterr.CodeSetting, loc.At("/parent"), obj, "table %s cannot be its own parent", n.Table)
			continue
		case n.Origin != st.Owner && !v.s.Visible(n.Origin, parent.Origin):
			v.fail(rterr.CodeNotVisible, loc.At("/parent"), obj, "table %s was added by package %s, which %s does not require", n.Parent, parent.Origin, n.Origin)
		}
		children[parentName] = append(children[parentName], n.Table)
		pt := phys.Tables[n.Table]
		if pt == nil {
			continue
		}
		fkName, hasFK := n.ForeignKey.Get()
		fk := pt.ForeignKey(fkName)
		switch {
		case !hasFK:
			v.fail(rterr.CodeSetting, loc.At("/foreignKey"), obj, "table %s names no foreign key to its parent", n.Table)
		case fk == nil:
			v.fail(rterr.CodeSetting, loc.At("/foreignKey"), obj, "table %s has no foreign key %s", n.Table, fkName)
		case fk.Target != parentName:
			v.fail(rterr.CodeSetting, loc.At("/foreignKey"), obj, "foreign key %s.%s references %s, not the parent %s", n.Table, fkName, fk.Target, parentName)
		}
	}
	reached := map[string]bool{}
	var walk func(string)
	walk = func(t string) {
		if reached[t] {
			return
		}
		reached[t] = true
		for _, c := range children[t] {
			walk(c)
		}
	}
	walk(st.Root.Table)
	for _, n := range nodes {
		if _, ok := inTree[n.Table]; ok && !reached[n.Table] {
			v.fail(rterr.CodeSetting, n.Src.At("/parent"), obj, "table %s is not connected to the root table %s", n.Table, st.Root.Table)
		}
	}
}
