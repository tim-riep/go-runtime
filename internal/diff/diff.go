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

// Package diff compares two schemas and lists the changes between them,
// each classified as safe or destructive.
//
// Safe changes keep all existing data valid: new domains, tables, fields
// that are nullable or have a default, longer CHAR fields, new foreign keys
// and new fixed values, as well as relaxing NOT NULL and changing defaults.
// Everything that removes or restricts is destructive.
package diff

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// Diff returns the changes that turn cur into want.
func Diff(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	out = append(out, domains(cur, want)...)
	out = append(out, dataElements(cur, want)...)
	out = append(out, structures(cur, want)...)
	out = append(out, assets(cur, want)...)
	out = append(out, authorizationObjects(cur, want)...)
	out = append(out, roles(cur, want)...)
	out = append(out, extensionPoints(cur, want)...)
	out = append(out, businessObjects(cur, want)...)
	out = append(out, businessObjectExtensions(cur, want)...)
	out = append(out, services(cur, want)...)
	out = append(out, objects(cur, want)...)
	out = append(out, tables(cur.Physical(), want.Physical())...)
	return out
}

func domains(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.DomainNames() {
		w := want.Domains[name]
		c, ok := cur.Domains[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateDomain, Name: name, Detail: w.TypeString()})
			continue
		}
		if detail, destructive, changed := compareDomains(c, w); changed {
			out = append(out, plan.Change{Kind: plan.AlterDomain, Name: name, Detail: detail, Destructive: destructive})
		}
	}
	for _, name := range cur.DomainNames() {
		if _, ok := want.Domains[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropDomain, Name: name})
		}
	}
	return out
}

func dataElements(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.DataElementNames() {
		w := want.DataElements[name]
		c, ok := cur.DataElements[name]
		if !ok {
			out = append(out, plan.Change{Kind: plan.CreateDataElement, Name: name, Detail: "domain " + w.Domain})
			continue
		}
		var parts []string
		if c.Domain != w.Domain {
			parts = append(parts, "domain "+c.Domain+" -> "+w.Domain)
		}
		if !reflect.DeepEqual(c.Texts, w.Texts) {
			parts = append(parts, "texts")
		}
		if len(parts) > 0 {
			out = append(out, plan.Change{Kind: plan.AlterDataElement, Name: name, Detail: strings.Join(parts, "; ")})
		}
	}
	for _, name := range cur.DataElementNames() {
		if _, ok := want.DataElements[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropDataElement, Name: name})
		}
	}
	return out
}

// assets lists the changes of asset repositories, one per repository,
// folder, asset and variant. They never touch the database layout and are
// never destructive.
func assets(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	ch := func(kind plan.ChangeKind, k model.AssetRepositoryKey, p, lang, detail string) {
		out = append(out, plan.Change{Kind: kind, Package: string(k.Package), Repository: k.ID, Path: p, Language: lang, Detail: detail})
	}
	for _, w := range want.SortedAssetRepositories() {
		k := w.Key()
		c := cur.AssetRepositories[k]
		if c == nil {
			ch(plan.AddAssetRepository, k, "", "", fmt.Sprintf("%d entries", len(w.Entries)))
			c = &model.AssetRepository{Entries: map[string]*model.AssetEntry{}}
		} else if !maps.Equal(c.Description, w.Description) {
			ch(plan.AlterAssetRepository, k, "", "", "label")
		}
		for _, p := range w.Paths() {
			we, ce := w.Entries[p], c.Entries[p]
			if ce != nil && ce.Folder != we.Folder {
				ch(dropKind(ce), k, p, "", "")
				ce = nil
			}
			if ce == nil {
				if we.Folder {
					ch(plan.AddAssetFolder, k, p, "", "")
				} else {
					ch(plan.AddAsset, k, p, "", describeAsset(we))
				}
				continue
			}
			if we.Folder {
				if !maps.Equal(ce.Description, we.Description) {
					ch(plan.AlterAssetFolder, k, p, "", "description")
				}
				continue
			}
			switch {
			case we.Content != nil && ce.Content == nil:
				ch(plan.AddAsset, k, p, "", "language-independent version, "+describeContent(we.Content))
			case we.Content == nil && ce.Content != nil:
				ch(plan.DropAsset, k, p, "", "language-independent version")
			case we.Content != nil && we.Content.SHA256 != ce.Content.SHA256:
				ch(plan.ReplaceAsset, k, p, "", describeContent(ce.Content)+" -> "+describeContent(we.Content))
			}
			var parts []string
			if ce.MimeType != we.MimeType {
				parts = append(parts, "media type "+ce.MimeType.Or("none")+" -> "+we.MimeType.Or("none"))
			}
			if !maps.Equal(ce.Description, we.Description) {
				parts = append(parts, "description")
			}
			if len(parts) > 0 {
				ch(plan.AlterAsset, k, p, "", strings.Join(parts, "; "))
			}
			for _, l := range we.Languages() {
				wv, cv := we.Variants[l], ce.Variants[l]
				switch {
				case cv == nil:
					ch(plan.AddAssetVariant, k, p, l, describeContent(wv))
				case cv.SHA256 != wv.SHA256:
					ch(plan.ReplaceAssetVariant, k, p, l, describeContent(cv)+" -> "+describeContent(wv))
				}
			}
			for _, l := range ce.Languages() {
				if we.Variants[l] == nil {
					ch(plan.DropAssetVariant, k, p, l, "")
				}
			}
		}
		for _, p := range c.Paths() {
			if w.Entries[p] == nil {
				ch(dropKind(c.Entries[p]), k, p, "", "")
			}
		}
	}
	for _, c := range cur.SortedAssetRepositories() {
		if want.AssetRepositories[c.Key()] == nil {
			ch(plan.DropAssetRepository, c.Key(), "", "", fmt.Sprintf("%d entries", len(c.Entries)))
		}
	}
	return out
}

func dropKind(e *model.AssetEntry) plan.ChangeKind {
	if e.Folder {
		return plan.DropAssetFolder
	}
	return plan.DropAsset
}

func describeContent(c *model.AssetContent) string {
	return fmt.Sprintf("%d bytes, sha256 %s", c.Size, c.SHA256[:min(12, len(c.SHA256))])
}

func describeAsset(e *model.AssetEntry) string {
	parts := []string{e.MimeType.Or("no media type")}
	if e.Content != nil {
		parts = append(parts, describeContent(e.Content))
	}
	if len(e.Variants) > 0 {
		parts = append(parts, "languages "+strings.Join(e.Languages(), ", "))
	}
	return strings.Join(parts, ", ")
}

func describeReference(f *model.Field) string {
	if de, ok := f.DataElement.Get(); ok {
		return "data element " + de
	}
	return "domain " + f.Domain
}

// compareDomains describes the type and value differences of two versions
// of a domain.
func compareDomains(c, w *model.Domain) (detail string, destructive, changed bool) {
	var parts []string
	if c.AllowsEmpty() != w.AllowsEmpty() {
		changed = true
		parts = append(parts, fmt.Sprintf("allowEmpty %v -> %v", c.AllowsEmpty(), w.AllowsEmpty()))
		destructive = destructive || !w.AllowsEmpty()
	}
	if c.Default != w.Default {
		changed = true
		parts = append(parts, describeDefault(c.Default)+" -> "+describeDefault(w.Default))
	}
	if c.TypeString() != w.TypeString() {
		changed = true
		parts = append(parts, c.TypeString()+" -> "+w.TypeString())
		destructive = !widens(c, w)
	}
	added, removed := valueChanges(c, w)
	if len(added) > 0 || len(removed) > 0 {
		changed = true
		if len(added) > 0 {
			parts = append(parts, "new values "+strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			parts = append(parts, "removed values "+strings.Join(removed, ", "))
		}
		if restricts(c, w) {
			destructive = true
		}
	}
	if valueNames(c) != valueNames(w) {
		changed = true
		parts = append(parts, "value names")
	}
	return strings.Join(parts, "; "), destructive, changed
}

// widens reports whether w accepts every value of c with the same storage,
// which holds only for a longer CHAR.
func widens(c, w *model.Domain) bool {
	return c.Type == model.Char && w.Type == model.Char && w.Length >= c.Length
}

func valueChanges(c, w *model.Domain) (added, removed []string) {
	has := func(d *model.Domain, v string) bool {
		return slices.ContainsFunc(d.Values, func(fv model.FixedValue) bool { return fv.Value == v })
	}
	for _, v := range w.Values {
		if !has(c, v.Value) {
			added = append(added, v.Value)
		}
	}
	for _, v := range c.Values {
		if !has(w, v.Value) {
			removed = append(removed, v.Value)
		}
	}
	return added, removed
}

// valueNames returns the code names of the fixed values for comparison.
func valueNames(d *model.Domain) string {
	var parts []string
	for _, v := range d.Values {
		parts = append(parts, v.Value+"="+v.Name.Or(""))
	}
	return strings.Join(parts, "\x00")
}

// restricts reports whether the fixed values of w reject a value that c
// accepts: a value was removed, or an unrestricted domain got fixed values.
func restricts(c, w *model.Domain) bool {
	added, removed := valueChanges(c, w)
	return len(removed) > 0 || (len(c.Values) == 0 && len(added) > 0)
}

func tables(cur, want *model.Physical) []plan.Change {
	var out []plan.Change
	for _, name := range want.TableNames() {
		w := want.Tables[name]
		c, ok := cur.Tables[name]
		switch {
		case !ok:
			out = append(out, plan.Change{Kind: plan.CreateTable, Table: name})
		case c.TenantSpecific != w.TenantSpecific:
			out = append(out, plan.Change{Kind: plan.RecreateTable, Table: name, Destructive: true,
				Detail: fmt.Sprintf("tenantSpecific %v -> %v, all rows are lost", c.TenantSpecific, w.TenantSpecific)})
		default:
			if c.DataClass != w.DataClass {
				out = append(out, plan.Change{Kind: plan.AlterDataClass, Table: name, Notice: true,
					Detail: describeClass(c.DataClass) + " -> " + describeClass(w.DataClass)})
			}
			out = append(out, columns(cur, want, c, w)...)
		}
	}
	for _, name := range cur.TableNames() {
		if _, ok := want.Tables[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropTable, Table: name, Destructive: true, Detail: "all rows are lost"})
		}
	}
	// Foreign keys are compared for every table that exists in both
	// versions, including recreated ones, so keys of other tables that
	// point at a recreated table are dropped and added around it.
	for _, name := range want.TableNames() {
		if c, ok := cur.Tables[name]; ok {
			out = append(out, foreignKeys(c, want.Tables[name])...)
		}
	}
	return out
}

func columns(cur, want *model.Physical, c, w *model.Table) []plan.Change {
	var out []plan.Change
	t := w.Name
	for i := range w.Fields {
		wf := &w.Fields[i]
		cf := c.Field(wf.Name)
		wd := want.Domain(wf)
		if cf == nil {
			ch := plan.Change{Kind: plan.AddColumn, Table: t, Column: wf.Name, Detail: wd.TypeString()}
			if wf.NotNull && wf.ResolvedDefault.Kind != model.DefaultValue {
				ch.Destructive = true
				ch.Detail += ", NOT NULL without default fails for existing rows"
			}
			out = append(out, ch)
			continue
		}
		if cf.DataElement != wf.DataElement {
			out = append(out, plan.Change{Kind: plan.AlterFieldReference, Table: t, Column: wf.Name,
				Detail: describeReference(cf) + " -> " + describeReference(wf)})
		}
		cd := cur.Domain(cf)
		if cd.TypeString() != wd.TypeString() {
			out = append(out, plan.Change{Kind: plan.AlterType, Table: t, Column: wf.Name,
				Detail: cd.TypeString() + " -> " + wd.TypeString(), Destructive: !widens(cd, wd)})
		}
		if added, removed := valueChanges(cd, wd); len(added) > 0 || len(removed) > 0 {
			detail := ""
			if len(added) > 0 {
				detail = "new values " + strings.Join(added, ", ")
			}
			if len(removed) > 0 {
				detail = strings.TrimPrefix(detail+"; removed values "+strings.Join(removed, ", "), "; ")
			}
			out = append(out, plan.Change{Kind: plan.AlterValues, Table: t, Column: wf.Name, Detail: detail, Destructive: restricts(cd, wd)})
		}
		if cd.AllowsEmpty() != wd.AllowsEmpty() && wd.Type.IsString() {
			out = append(out, plan.Change{Kind: plan.AlterValues, Table: t, Column: wf.Name,
				Detail: fmt.Sprintf("allowEmpty %v -> %v", cd.AllowsEmpty(), wd.AllowsEmpty()), Destructive: !wd.AllowsEmpty()})
		}
		switch {
		case wf.NotNull && !cf.NotNull:
			out = append(out, plan.Change{Kind: plan.SetNotNull, Table: t, Column: wf.Name, Destructive: true})
		case !wf.NotNull && cf.NotNull:
			out = append(out, plan.Change{Kind: plan.DropNotNull, Table: t, Column: wf.Name})
		}
		if cf.ResolvedDefault != wf.ResolvedDefault {
			out = append(out, plan.Change{Kind: plan.SetDefault, Table: t, Column: wf.Name, Detail: describeDefault(wf.ResolvedDefault)})
		}
	}
	for _, cf := range c.Fields {
		if w.Field(cf.Name) == nil {
			out = append(out, plan.Change{Kind: plan.DropColumn, Table: t, Column: cf.Name, Destructive: true, Detail: "column data is lost"})
		}
	}
	if !slices.Equal(c.PrimaryKey, w.PrimaryKey) {
		out = append(out, plan.Change{Kind: plan.AlterPrimaryKey, Table: t, Destructive: true,
			Detail: strings.Join(c.PrimaryKey, ", ") + " -> " + strings.Join(w.PrimaryKey, ", ")})
	}
	// Changes of included columns name the structure they come from.
	for i := range out {
		if f := w.Field(out[i].Column); f != nil && f.Source.Valid() {
			out[i].Via = f.Source.Or("")
		} else if f := c.Field(out[i].Column); f != nil {
			out[i].Via = f.Source.Or("")
		}
	}
	return out
}

func foreignKeys(c, w *model.Table) []plan.Change {
	var out []plan.Change
	recreated := c.TenantSpecific != w.TenantSpecific
	for _, cfk := range c.ForeignKeys {
		wfk := w.ForeignKey(cfk.Name)
		if recreated {
			continue // dropped together with the table
		}
		if wfk == nil || !sameKey(&cfk, wfk) {
			out = append(out, plan.Change{Kind: plan.DropForeignKey, Table: c.Name, Name: cfk.Name, Destructive: true, Detail: "references " + cfk.Target})
		}
	}
	for _, wfk := range w.ForeignKeys {
		cfk := c.ForeignKey(wfk.Name)
		if recreated {
			continue // created together with the table
		}
		if cfk == nil || !sameKey(cfk, &wfk) {
			out = append(out, plan.Change{Kind: plan.AddForeignKey, Table: w.Name, Name: wfk.Name, Detail: "references " + wfk.Target})
		}
	}
	return out
}

func describeClass(c model.DataClass) string {
	if c == "" {
		return "none"
	}
	return string(c)
}

func sameKey(a, b *model.ForeignKey) bool {
	return a.Target == b.Target && slices.Equal(a.Columns, b.Columns)
}

func describeDefault(d model.Default) string {
	switch d.Kind {
	case model.DefaultNull:
		return "default null"
	case model.DefaultValue:
		return "default " + strconv.Quote(d.Value)
	}
	return "no default"
}
