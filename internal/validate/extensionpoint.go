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
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// extensionPoints checks the extension points and the implementations.
// Whether the classes and the interface fit is checked by the compiler,
// whether implementations of a single extension point overlap by the
// kernel, which knows their activation.
func (v *validator) extensionPoints() {
	for _, name := range v.s.ExtensionPointNames() {
		v.extensionPoint(v.s.ExtensionPoints[name])
	}
	for _, name := range v.s.ImplementationNames() {
		v.implementation(v.s.Implementations[name])
	}
}

func (v *validator) extensionPoint(p *model.ExtensionPoint) {
	obj := "extension point " + p.Name
	loc := p.Src
	v.objectName(p.Name, p.Owner, loc.At("/name"), obj)
	v.descriptions(obj, loc, p.Description, p.Languages())
	v.ownSource(p.Interface, model.SourceInterface, "interface", p.Owner, loc.At("/interface"), obj)
	if fb, ok := p.Fallback.Get(); ok {
		v.ownSource(fb, model.SourceClass, "class", p.Owner, loc.At("/fallback"), obj)
	}
	if p.Activation != "" && p.Activation != model.ActivationTenant && p.Activation != model.ActivationGlobal {
		v.fail(rterr.CodeExtensionPoint, loc.At("/activation"), obj, "activation is tenant or global, not %q", p.Activation)
	}
	if p.Use != model.UseSingle && p.Use != model.UseMultiple {
		v.fail(rterr.CodeExtensionPoint, loc.At("/use"), obj, "use is single or multiple, not %q", p.Use)
	}
	if n := len(p.Filters); n > model.MaxExtensionFilters {
		v.fail(rterr.CodeExtensionPoint, loc.At("/filters"), obj, "an extension point has at most %d filter fields, not %d", model.MaxExtensionFilters, n)
	}
	seen := map[string]bool{}
	for i, f := range p.Filters {
		floc := loc.At(fmt.Sprintf("/filters/%d", i))
		fobj := obj + ", filter field " + f.Name
		switch {
		case !model.ValidLocalName(f.Name) || len(f.Name) > model.MaxAuthorizationFieldName:
			v.fail(rterr.CodeName, floc.At("/name"), fobj, "field names consist of up to %d upper-case letters, digits and underscores and start with a letter", model.MaxAuthorizationFieldName)
		case seen[f.Name]:
			v.fail(rterr.CodeDuplicate, floc.At("/name"), fobj, "the field %s is declared twice", f.Name)
		}
		seen[f.Name] = true
		d := v.filterField(f, p.Owner, floc, fobj)
		if def, ok := f.Default.Get(); ok {
			v.filterValues(d, def, floc.At("/default"), fobj+", default")
		}
	}
}

// filterField checks the type of a filter field: a visible data element or
// domain of a type that single values, ranges and patterns support, as
// for fields of authorization objects. It returns the domain or nil.
func (v *validator) filterField(f model.ExtensionFilter, owner model.PackageID, loc rterr.Location, obj string) *model.Domain {
	de, hasDE := f.DataElement.Get()
	dom, hasDom := f.Domain.Get()
	switch {
	case hasDE && hasDom:
		v.fail(rterr.CodeDataElement, loc, obj, "a filter field references either a data element or a domain, not both")
		return nil
	case !hasDE && !hasDom:
		v.fail(rterr.CodeDataElement, loc, obj, "a filter field needs a data element or a domain")
		return nil
	}
	d := v.valueField(de, dom, hasDE, owner, loc, obj)
	if d != nil && !slices.Contains(authorizationTypes, d.Type) {
		v.fail(rterr.CodeExtensionPoint, loc, obj, "type %s is not allowed; filter fields are CHAR(n), INT4, INT8, BOOL or UUID", d.TypeString())
		return nil
	}
	return d
}

// filterValues checks the entries of a filter field with the rules of
// role files.
func (v *validator) filterValues(d *model.Domain, entries []model.ValueEntry, loc rterr.Location, obj string) {
	if len(entries) == 0 {
		v.fail(rterr.CodeExtensionPoint, loc, obj, "the field has no values; write * for all values")
	}
	if d == nil {
		return
	}
	for i, e := range entries {
		if msg := model.CheckValueEntry(d, e); msg != "" {
			v.fail(rterr.CodeExtensionPoint, loc.At(fmt.Sprintf("/%d", i)), obj, "%s", msg)
		}
	}
}

func (v *validator) implementation(impl *model.Implementation) {
	obj := "implementation " + impl.Name
	loc := impl.Src
	v.objectName(impl.Name, impl.Owner, loc.At("/name"), obj)
	v.descriptions(obj, loc, impl.Description, impl.Languages())
	v.ownSource(impl.Class, model.SourceClass, "class", impl.Owner, loc.At("/class"), obj)
	p := v.s.ExtensionPoints[impl.ExtensionPoint]
	switch {
	case p == nil:
		v.fail(rterr.CodeExtensionPoint, loc.At("/extensionPoint"), obj, "extension point %s does not exist", impl.ExtensionPoint)
		return
	case !v.s.Visible(impl.Owner, p.Owner):
		v.fail(rterr.CodeNotVisible, loc.At("/extensionPoint"), obj, "extension point %s belongs to %s, which %s does not require", p.Name, p.Owner, impl.Owner)
	}
	for _, f := range p.Filters {
		values, ok := impl.Filters[f.Name]
		if !ok {
			if !f.Default.Valid() {
				v.fail(rterr.CodeExtensionPoint, loc.At("/filters"), obj, "the filter field %s of %s is missing; write [{\"pattern\": \"*\"}] for all values", f.Name, p.Name)
			}
			continue
		}
		v.filterValues(v.s.Domains[v.s.FilterDomain(f)], values, loc.At("/filters/"+f.Name), obj+", filter field "+f.Name)
	}
	for _, name := range impl.FilterNames() {
		if _, ok := p.Filter(name); !ok {
			var names []string
			for _, f := range p.Filters {
				names = append(names, f.Name)
			}
			fields := "it has no filter fields"
			if len(names) > 0 {
				fields = "its filter fields are " + strings.Join(names, ", ")
			}
			v.fail(rterr.CodeExtensionPoint, loc.At("/filters/"+name), obj, "%s has no filter field %s; %s", p.Name, name, fields)
		}
	}
}

// descriptions checks descriptions by language: English is mandatory,
// each has 1 to 60 characters.
func (v *validator) descriptions(obj string, loc rterr.Location, texts map[string]string, langs []string) {
	if !slices.Contains(langs, model.DefaultLanguage) {
		v.fail(rterr.CodeText, loc.At("/description"), obj, "a description in English (%s) is mandatory", model.DefaultLanguage)
	}
	for _, lang := range langs {
		if n := utf8.RuneCountInString(texts[lang]); n == 0 || n > model.MaxLabel {
			v.fail(rterr.CodeText, loc.At("/description/"+lang), obj, "the description in %s must have 1 to %d characters", lang, model.MaxLabel)
		}
	}
	v.knownLanguages(obj, langs)
}

// ownSource checks that a class or interface is a source file of the
// package owner.
func (v *validator) ownSource(name string, kind model.SourceKind, what string, owner model.PackageID, loc rterr.Location, obj string) {
	pkg, _, ok := model.SplitName(name)
	if !ok || pkg != owner {
		v.fail(rterr.CodeExtensionPoint, loc, obj, "the %s %s must belong to %s", what, name, owner)
		return
	}
	for _, s := range v.s.Sources[owner] {
		if s.Name == name {
			if s.Kind != kind {
				v.fail(rterr.CodeExtensionPoint, loc, obj, "%s is no %s", name, what)
			}
			return
		}
	}
	v.fail(rterr.CodeExtensionPoint, loc, obj, "the %s %s does not exist in %s", what, name, owner)
}
