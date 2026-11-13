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

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

const (
	objectsClass         = "/TRBASE/OBJECTS"
	errObjectTypeVisible = "/TRBASE/OBJECT_TYPE_NOT_VISIBLE"
)

func objectInfos(objs []*model.Object) []ObjectInfo {
	out := make([]ObjectInfo, len(objs))
	for i, o := range objs {
		out[i] = objectInfo(o)
	}
	return out
}

func objectInfo(o *model.Object) ObjectInfo {
	return ObjectInfo{Name: o.Name, Type: o.Type, PackageID: string(o.Owner), Hash: o.Hash, Content: string(o.Content)}
}

// objectError turns an object type the code may not use into
// /TRBASE/OBJECT_TYPE_NOT_VISIBLE.
func objectError(err error) error {
	var nv *native.ObjectTypeNotVisible
	if errors.As(err, &nv) {
		return native.Raise(errObjectTypeVisible, nv.Error(), nv.Type)
	}
	return err
}

func init() {
	raises := " raises " + errObjectTypeVisible
	static(objectsClass, "OF_TYPE", "(type: STRING, pkg: STRING?) returns /TRBASE/OBJECT_INFO_ARRAY"+raises,
		func(c *native.Context, typ string, pkg model.Optional[string]) ([]ObjectInfo, error) {
			objs, err := c.Host.Objects(c.Reader(), typ, model.PackageID(pkg.Or("")), nil)
			if err != nil {
				return nil, objectError(err)
			}
			return objectInfos(objs), nil
		})
	static(objectsClass, "BY_REFERENCE", "(type: STRING, kind: /TRBASE/REFERENCE_KIND, target: STRING, part: STRING?) returns /TRBASE/OBJECT_INFO_ARRAY"+raises,
		func(c *native.Context, typ string, kind ReferenceKind, target string, part model.Optional[string]) ([]ObjectInfo, error) {
			ref := &model.ObjectReference{Kind: model.ReferenceKind(kind), Target: target, Part: part.Or("")}
			objs, err := c.Host.Objects(c.Reader(), typ, "", ref)
			if err != nil {
				return nil, objectError(err)
			}
			return objectInfos(objs), nil
		})
	static(objectsClass, "GET", "(name: STRING) returns /TRBASE/OBJECT_INFO?"+raises,
		func(c *native.Context, name string) (model.Optional[ObjectInfo], error) {
			o, err := c.Host.Object(c.Reader(), name)
			if err != nil {
				return model.None[ObjectInfo](), objectError(err)
			}
			if o == nil {
				return model.None[ObjectInfo](), nil
			}
			return model.Some(objectInfo(o)), nil
		})
}
