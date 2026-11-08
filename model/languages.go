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

package model

import "maps"

// WithLanguages returns a copy of the package that keeps only texts and
// asset variants in English and in the languages for which keep reports
// true. The kernel installs its kernel packages this way, since they
// cannot wait for languages to be set up.
func (p *Package) WithLanguages(keep func(lang string) bool) *Package {
	ok := func(l string) bool { return l == DefaultLanguage || keep(l) }
	filter := func(m map[string]string) map[string]string {
		if m == nil {
			return nil
		}
		out := map[string]string{}
		for l, v := range m {
			if ok(l) {
				out[l] = v
			}
		}
		return out
	}
	c := *p
	c.DataElements = make([]DataElement, len(p.DataElements))
	for i, d := range p.DataElements {
		d.Texts = maps.Clone(d.Texts)
		maps.DeleteFunc(d.Texts, func(l string, _ DataElementText) bool { return !ok(l) })
		c.DataElements[i] = d
	}
	c.Structures = make([]Structure, len(p.Structures))
	for i, s := range p.Structures {
		s.Texts = maps.Clone(s.Texts)
		maps.DeleteFunc(s.Texts, func(l string, _ Label) bool { return !ok(l) })
		c.Structures[i] = s
	}
	c.ArrayTypes = make([]ArrayType, len(p.ArrayTypes))
	for i, a := range p.ArrayTypes {
		a.Texts = maps.Clone(a.Texts)
		maps.DeleteFunc(a.Texts, func(l string, _ Label) bool { return !ok(l) })
		c.ArrayTypes[i] = a
	}
	c.Transformations = make([]Transformation, len(p.Transformations))
	for i, t := range p.Transformations {
		t.Texts = maps.Clone(t.Texts)
		maps.DeleteFunc(t.Texts, func(l string, _ Label) bool { return !ok(l) })
		c.Transformations[i] = t
	}
	c.HTTPServices = make([]HTTPService, len(p.HTTPServices))
	for i, h := range p.HTTPServices {
		h.Texts = maps.Clone(h.Texts)
		maps.DeleteFunc(h.Texts, func(l string, _ Label) bool { return !ok(l) })
		c.HTTPServices[i] = h
	}
	c.AuthorizationObjects = make([]AuthorizationObject, len(p.AuthorizationObjects))
	for i, o := range p.AuthorizationObjects {
		o.Texts = maps.Clone(o.Texts)
		maps.DeleteFunc(o.Texts, func(l string, _ Label) bool { return !ok(l) })
		c.AuthorizationObjects[i] = o
	}
	c.Roles = make([]Role, len(p.Roles))
	for i, r := range p.Roles {
		r.Texts = maps.Clone(r.Texts)
		maps.DeleteFunc(r.Texts, func(l string, _ Label) bool { return !ok(l) })
		c.Roles[i] = r
	}
	c.ExtensionPoints = make([]ExtensionPoint, len(p.ExtensionPoints))
	for i, e := range p.ExtensionPoints {
		e.Description = filter(e.Description)
		c.ExtensionPoints[i] = e
	}
	c.BusinessObjects = make([]BusinessObject, len(p.BusinessObjects))
	for i, b := range p.BusinessObjects {
		b.Description = filter(b.Description)
		c.BusinessObjects[i] = b
	}
	c.Implementations = make([]Implementation, len(p.Implementations))
	for i, impl := range p.Implementations {
		impl.Description = filter(impl.Description)
		c.Implementations[i] = impl
	}
	c.AssetRepositories = make([]AssetRepository, len(p.AssetRepositories))
	for i, r := range p.AssetRepositories {
		r.Description = filter(r.Description)
		entries := make(map[string]*AssetEntry, len(r.Entries))
		for path, e := range r.Entries {
			ce := *e
			ce.Description = filter(e.Description)
			if e.Variants != nil {
				ce.Variants = map[string]*AssetContent{}
				for l, v := range e.Variants {
					if ok(l) {
						ce.Variants[l] = v
					}
				}
			}
			entries[path] = &ce
		}
		r.Entries = entries
		c.AssetRepositories[i] = r
	}
	return &c
}
