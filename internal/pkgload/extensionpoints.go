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
	"fmt"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/schemas"
)

// ExtensionPointsDir holds the extension points of a package, one per file
// named after the extension point.
const ExtensionPointsDir = SchemaDir + "/extension-points"

// ImplementationsDir holds the implementations of extension points that a
// package brings, one per file named after the implementation.
const ImplementationsDir = SchemaDir + "/extension-implementations"

type extensionPointDoc struct {
	Name        string            `json:"name"`
	Description map[string]string `json:"description"`
	Interface   string            `json:"interface"`
	Use         string            `json:"use"`
	Filters     []struct {
		Name        string                             `json:"name"`
		DataElement model.Optional[string]             `json:"dataElement"`
		Domain      model.Optional[string]             `json:"domain"`
		Default     model.Optional[[]model.ValueEntry] `json:"default"`
	} `json:"filters"`
	Fallback      model.Optional[string] `json:"fallback"`
	CommitAllowed bool                   `json:"commitAllowed"`
	Activation    string                 `json:"activation"`
}

type implementationDoc struct {
	Name           string                        `json:"name"`
	Description    map[string]string             `json:"description"`
	ExtensionPoint string                        `json:"extensionPoint"`
	Class          string                        `json:"class"`
	Filters        map[string][]model.ValueEntry `json:"filters"`
	Order          int32                         `json:"order"`
}

func init() {
	register(ObjectType{
		Kind: "extension point", Document: jsonIn(ExtensionPointsDir), Schema: schemas.ExtensionPoint,
		NewDoc:      func() any { return &extensionPointDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*extensionPointDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*extensionPointDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*extensionPointDoc)
			out := ref(d.Interface, "/interface")
			out = append(out, ref(d.Fallback.Or(""), "/fallback")...)
			for i, f := range d.Filters {
				out = append(out, ref(f.DataElement.Or(""), fmt.Sprintf("/filters/%d/dataElement", i))...)
				out = append(out, ref(f.Domain.Or(""), fmt.Sprintf("/filters/%d/domain", i))...)
			}
			return out
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*extensionPointDoc)
			p := model.ExtensionPoint{Name: d.Name, Owner: l.owner(), File: file, Description: d.Description, Interface: d.Interface,
				Use: model.ExtensionUse(d.Use), Fallback: d.Fallback, CommitAllowed: d.CommitAllowed, Activation: model.ActivationTenant, Src: l.loc(file)}
			if d.Activation != "" {
				p.Activation = model.ExtensionActivation(d.Activation)
			}
			for _, f := range d.Filters {
				p.Filters = append(p.Filters, model.ExtensionFilter{Name: f.Name, DataElement: f.DataElement, Domain: f.Domain, Default: f.Default})
			}
			pkg.ExtensionPoints = append(pkg.ExtensionPoints, p)
		},
	})
	register(ObjectType{
		Kind: "implementation", Document: jsonIn(ImplementationsDir), Schema: schemas.Implementation,
		NewDoc:      func() any { return &implementationDoc{} },
		FileName:    func(doc any) (string, bool) { return fileNameOf(doc.(*implementationDoc).Name) },
		Definitions: func(doc any) []NameRef { return ref(doc.(*implementationDoc).Name, "/name") },
		References: func(doc any) []NameRef {
			d := doc.(*implementationDoc)
			return append(ref(d.ExtensionPoint, "/extensionPoint"), ref(d.Class, "/class")...)
		},
		Build: func(l *loader, file string, doc any, pkg *model.Package) {
			d := doc.(*implementationDoc)
			filters := d.Filters
			if filters == nil {
				filters = map[string][]model.ValueEntry{}
			}
			pkg.Implementations = append(pkg.Implementations, model.Implementation{Name: d.Name, Owner: l.owner(), File: file, Description: d.Description,
				ExtensionPoint: d.ExtensionPoint, Class: d.Class, Filters: filters, Order: d.Order, Src: l.loc(file)})
		},
	})
}
