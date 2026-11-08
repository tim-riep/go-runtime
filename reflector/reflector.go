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

// Package reflector describes objects of the schema at runtime, resolved
// into types that Go code and the VM read without the repository. This
// version describes authorization objects, the activities of the kernel
// and extension points with their implementations; later versions add
// further kinds of objects.
//
// The types are stable: fields are only added, never renamed or removed,
// within one FormatVersion. null and "" are different values throughout:
// texts that do not exist are model.Optional and written as null.
package reflector

import (
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// FormatVersion is the version of the structure of the types.
const FormatVersion = 1

// Reflector describes the objects of a schema with the texts of a set of
// languages.
type Reflector struct {
	s     *model.Schema
	langs []string
}

// New returns a reflector of a schema. languages are the languages set up
// in the database; texts are given only in these. English is always one.
func New(s *model.Schema, languages []string) *Reflector {
	langs := slices.Clone(languages)
	if !slices.Contains(langs, model.DefaultLanguage) {
		langs = append(langs, model.DefaultLanguage)
	}
	slices.Sort(langs)
	return &Reflector{s: s, langs: langs}
}

// Text is the label and an optional description of an object in a
// language.
type Text struct {
	Label       string                 `json:"label"`
	Description model.Optional[string] `json:"description"`
}

// AuthorizationObject is the resolved definition of an authorization
// object.
type AuthorizationObject struct {
	FormatVersion int             `json:"formatVersion"`
	Name          string          `json:"name"`
	Package       model.PackageID `json:"package"`
	// Texts holds the label and description by language.
	Texts map[string]Text `json:"texts"`
	// Fields are the fields in their order.
	Fields []AuthorizationField `json:"fields"`
	// Activities are the activities that can be granted, in order; empty
	// for objects without the field ACTIVITY.
	Activities []Activity `json:"activities"`
	// Languages are the languages of the texts.
	Languages []string `json:"languages"`
}

// AuthorizationField is a field of an authorization object with its type
// resolved.
type AuthorizationField struct {
	Name string `json:"name"`
	// Activity marks the field ACTIVITY, whose type the kernel gives.
	Activity bool `json:"activity"`
	// DataElement is the data element of the field, model.ActivityDomain
	// for ACTIVITY, and null for fields with a domain.
	DataElement model.Optional[string] `json:"dataElement"`
	// Labels are the field labels of the data element by language, empty
	// for fields with a domain.
	Labels map[string]model.DataElementText `json:"labels"`
	Domain string                           `json:"domain"`
	Type   model.BaseType                   `json:"type"`
	// Length is the length of CHAR, null for other types.
	Length model.Optional[int] `json:"length"`
	// Values are the fixed values of the domain; for ACTIVITY the
	// activities the object allows, with their texts.
	Values []FixedValue `json:"values"`
}

// FixedValue is a fixed value of the domain of a field.
type FixedValue struct {
	Value string `json:"value"`
	// Name is the name of the value in code, if it has one.
	Name        model.Optional[string] `json:"name"`
	Description model.Optional[string] `json:"description"`
	// Texts are the label and explanation by language of an activity;
	// other fixed values have none.
	Texts map[string]model.ActivityText `json:"texts,omitempty"`
}

// Activity is an activity with its label and explanation by language.
type Activity struct {
	Code  string                        `json:"code"`
	Texts map[string]model.ActivityText `json:"texts"`
}

// AuthorizationObject returns the definition of an authorization object,
// or rterr.NotFound.
func (r *Reflector) AuthorizationObject(name string) (*AuthorizationObject, error) {
	o := r.s.AuthorizationObjects[name]
	if o == nil {
		return nil, &rterr.NotFound{Kind: "authorization object", Name: name}
	}
	out := &AuthorizationObject{FormatVersion: FormatVersion, Name: o.Name, Package: o.Owner, Texts: map[string]Text{},
		Fields: []AuthorizationField{}, Activities: []Activity{}, Languages: r.langs}
	for _, l := range r.langs {
		if t, ok := o.Texts[l]; ok {
			out.Texts[l] = Text{Label: t.Label, Description: t.Description}
		}
	}
	for _, code := range o.Activities {
		out.Activities = append(out.Activities, r.activity(code))
	}
	for _, f := range o.Fields {
		out.Fields = append(out.Fields, r.field(o, f))
	}
	return out, nil
}

func (r *Reflector) field(o *model.AuthorizationObject, f model.AuthorizationField) AuthorizationField {
	out := AuthorizationField{Name: f.Name, DataElement: f.DataElement, Labels: map[string]model.DataElementText{}, Values: []FixedValue{},
		Domain: r.s.AuthorizationFieldDomain(f)}
	if f.IsActivity() && !f.DataElement.Valid() && !f.Domain.Valid() {
		out.Activity, out.DataElement = true, model.Some(model.ActivityDomain)
	}
	if de, ok := out.DataElement.Get(); ok {
		if d := r.s.DataElements[de]; d != nil {
			for _, l := range r.langs {
				if t, ok := d.Texts[l]; ok {
					out.Labels[l] = t
				}
			}
		}
	}
	d := r.s.Domains[out.Domain]
	if d == nil {
		return out
	}
	out.Type = d.Type
	if d.Type == model.Char {
		out.Length = model.Some(d.Length)
	}
	if out.Activity {
		for _, code := range o.Activities {
			a := r.activity(code)
			fv := FixedValue{Value: code, Name: model.Some(code), Texts: a.Texts}
			if t, ok := a.Texts[model.DefaultLanguage]; ok {
				fv.Description = model.Some(t.Label)
			}
			out.Values = append(out.Values, fv)
		}
		return out
	}
	for _, v := range d.Values {
		fv := FixedValue{Value: v.Value, Description: v.Description}
		if n, ok := v.CodeName(); ok {
			fv.Name = model.Some(n)
		}
		out.Values = append(out.Values, fv)
	}
	return out
}

// AuthorizationObjects returns the authorization objects of a package, or
// of all packages for "", ordered by name.
func (r *Reflector) AuthorizationObjects(pkg model.PackageID) []*AuthorizationObject {
	out := []*AuthorizationObject{}
	for _, name := range r.s.AuthorizationObjectNames() {
		if pkg != "" && r.s.AuthorizationObjects[name].Owner != pkg {
			continue
		}
		o, _ := r.AuthorizationObject(name)
		out = append(out, o)
	}
	return out
}

// Activities returns the activities of the kernel in their order.
func (r *Reflector) Activities() []Activity {
	out := []Activity{}
	for _, a := range model.Activities {
		if r.activityCode(a.Code) {
			out = append(out, r.activity(a.Code))
		}
	}
	return out
}

// activityCode reports a code of the domain of the field ACTIVITY.
func (r *Reflector) activityCode(code string) bool {
	d := r.s.Domains[model.ActivityDomain]
	return d != nil && slices.ContainsFunc(d.Values, func(v model.FixedValue) bool { return v.Value == code })
}

func (r *Reflector) activity(code string) Activity {
	out := Activity{Code: code, Texts: map[string]model.ActivityText{}}
	if a, ok := model.ActivityByCode(code); ok {
		for _, l := range r.langs {
			if t, ok := a.Texts[l]; ok {
				out.Texts[l] = t
			}
		}
	}
	return out
}

// In returns the text in a language, else in English, else null.
func In[T any](texts map[string]T, lang string) model.Optional[T] {
	if t, ok := texts[lang]; ok {
		return model.Some(t)
	}
	if t, ok := texts[model.DefaultLanguage]; ok {
		return model.Some(t)
	}
	return model.None[T]()
}
