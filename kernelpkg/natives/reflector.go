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

package natives

import (
	"errors"
	"slices"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
)

const reflectorClass = "/TRBASE/REFLECTOR"

func init() {
	static(reflectorClass, "AUTHORIZATION_OBJECT", "(name: STRING) returns /TRBASE/AUTHORIZATION_OBJECT raises /TRBASE/AUTHORIZATION_OBJECT_NOT_FOUND",
		func(c *native.Context, name string) (AuthorizationObject, error) {
			r, err := reflectorOf(c)
			if err != nil {
				return AuthorizationObject{}, err
			}
			o, err := r.AuthorizationObject(name)
			var nf *rterr.NotFound
			if errors.As(err, &nf) {
				return AuthorizationObject{}, native.Raise("/TRBASE/AUTHORIZATION_OBJECT_NOT_FOUND", name)
			}
			if err != nil {
				return AuthorizationObject{}, err
			}
			return authorizationObject(o, c.Language()), nil
		})
	static(reflectorClass, "AUTHORIZATION_OBJECT_EXISTS", "(name: STRING) returns BOOL", func(c *native.Context, name string) (bool, error) {
		r, err := reflectorOf(c)
		if err != nil {
			return false, err
		}
		_, err = r.AuthorizationObject(name)
		return err == nil, nil
	})
	static(reflectorClass, "AUTHORIZATION_OBJECTS", "(pkg: STRING?) returns /TRBASE/AUTHORIZATION_OBJECT_ARRAY",
		func(c *native.Context, pkg model.Optional[string]) ([]AuthorizationObject, error) {
			r, err := reflectorOf(c)
			if err != nil {
				return nil, err
			}
			out := []AuthorizationObject{}
			for _, o := range r.AuthorizationObjects(model.PackageID(pkg.Or(""))) {
				out = append(out, authorizationObject(o, c.Language()))
			}
			return out, nil
		})
	static(reflectorClass, "ACTIVITIES", "() returns /TRBASE/ACTIVITY_ARRAY", func(c *native.Context) ([]ActivityInfo, error) {
		r, err := reflectorOf(c)
		if err != nil {
			return nil, err
		}
		out := []ActivityInfo{}
		for _, a := range r.Activities() {
			out = append(out, activityInfo(a, c.Language()))
		}
		return out, nil
	})
	static(reflectorClass, "ALLOWS", "(object: /TRBASE/AUTHORIZATION_OBJECT, activity: /RT/ACTIVITY) returns BOOL",
		func(c *native.Context, o AuthorizationObject, a Activity) (bool, error) {
			return slices.Contains(o.ActivityCodes, a), nil
		})
}

func reflectorOf(c *native.Context) (*reflector.Reflector, error) {
	if c.Host == nil {
		return nil, errors.New("natives: the reflector needs the kernel")
	}
	return c.Host.Reflector()
}

// authorizationObject turns a definition into the structure of TRBASE
// with the texts of a language, else English, else null.
func authorizationObject(o *reflector.AuthorizationObject, lang string) AuthorizationObject {
	out := AuthorizationObject{Name: o.Name, PackageID: string(o.Package), Fields: []AuthorizationField{}, Activities: []ActivityInfo{}, ActivityCodes: []Activity{}}
	if t, ok := reflector.In(o.Texts, lang).Get(); ok {
		out.Label, out.Description = model.Some(t.Label), t.Description
	}
	for _, f := range o.Fields {
		af := AuthorizationField{Name: f.Name, IsActivity: f.Activity, DataElement: f.DataElement, Domain: f.Domain, Type: string(f.Type), Values: []FixedValue{}}
		if n, ok := f.Length.Get(); ok {
			af.Length = model.Some(int32(n))
		}
		if l, ok := reflector.In(f.Labels, lang).Get(); ok {
			af.ShortLabel, af.Label, af.LongLabel, af.Heading = model.Some(l.Short), model.Some(l.Medium), model.Some(l.Long), model.Some(l.Heading)
		}
		for _, v := range f.Values {
			fv := FixedValue{Value: v.Value, Name: v.Name, Description: v.Description}
			if t, ok := reflector.In(v.Texts, lang).Get(); ok {
				fv.Description = model.Some(t.Label)
			}
			af.Values = append(af.Values, fv)
		}
		out.Fields = append(out.Fields, af)
	}
	for _, a := range o.Activities {
		out.Activities = append(out.Activities, activityInfo(a, lang))
		out.ActivityCodes = append(out.ActivityCodes, Activity(a.Code))
	}
	return out
}

func activityInfo(a reflector.Activity, lang string) ActivityInfo {
	out := ActivityInfo{Code: Activity(a.Code)}
	if t, ok := reflector.In(a.Texts, lang).Get(); ok {
		out.Label, out.Description = model.Some(t.Label), model.Some(t.Description)
	}
	return out
}
