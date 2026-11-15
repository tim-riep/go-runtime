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

package pkgload

import (
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/schemas"
)

// NumberRangesDir holds the number range objects of a package, one per
// file named after the object.
const NumberRangesDir = SchemaDir + "/number-ranges"

type numberRangeDoc struct {
	Name      string                 `json:"name"`
	Texts     map[string]model.Label `json:"texts"`
	Length    int                    `json:"length"`
	Subobject *struct {
		Name        string                 `json:"name"`
		DataElement model.Optional[string] `json:"dataElement"`
		Domain      model.Optional[string] `json:"domain"`
	} `json:"subobject"`
	PerFiscalYear bool `json:"perFiscalYear"`
	Buffering     *struct {
		Mode      model.NumberRangeBuffering `json:"mode"`
		BlockSize int                        `json:"blockSize"`
	} `json:"buffering"`
	WarningPercent *int `json:"warningPercent"`
}

func init() {
	register(ObjectType{
		Kind: "number range", Document: jsonIn(NumberRangesDir), Schema: schemas.NumberRange,
		NewDoc:      func() any { return &numberRangeDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*numberRangeDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*numberRangeDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*numberRangeDoc)
			if d.Subobject == nil {
				return nil
			}
			out := ref(d.Subobject.DataElement.Or(""), "/subobject/dataElement")
			return append(out, ref(d.Subobject.Domain.Or(""), "/subobject/domain")...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*numberRangeDoc)
			n := model.NumberRange{Name: d.Name, Owner: l.owner(), File: file, Texts: d.Texts, Length: d.Length, PerFiscalYear: d.PerFiscalYear,
				Buffering: model.BufferingNone, WarningPercent: model.DefaultWarningPercent, Src: l.loc(file)}
			if s := d.Subobject; s != nil {
				n.Subobject = model.Some(model.NumberRangeSubobject{Name: s.Name, DataElement: s.DataElement, Domain: s.Domain})
			}
			if b := d.Buffering; b != nil {
				n.Buffering, n.BlockSize = b.Mode, b.BlockSize
			}
			if d.WarningPercent != nil {
				n.WarningPercent = *d.WarningPercent
			}
			pkg.NumberRanges = append(pkg.NumberRanges, n)
		},
	})
}
