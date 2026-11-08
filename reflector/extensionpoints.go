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

package reflector

import (
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// ExtensionPoint is the resolved definition of an extension point with
// its implementations in the order they run.
type ExtensionPoint struct {
	FormatVersion int             `json:"formatVersion"`
	Name          string          `json:"name"`
	Package       model.PackageID `json:"package"`
	// Description holds the description by language.
	Description map[string]string  `json:"description"`
	Interface   string             `json:"interface"`
	Use         model.ExtensionUse `json:"use"`
	// Filters are the filter fields in their order.
	Filters       []ExtensionFilter         `json:"filters"`
	Fallback      model.Optional[string]    `json:"fallback"`
	CommitAllowed bool                      `json:"commitAllowed"`
	Activation    model.ExtensionActivation `json:"activation"`
	// Implementations are those of all packages, in the order they run.
	Implementations []Implementation `json:"implementations"`
	// Languages are the languages of the texts.
	Languages []string `json:"languages"`
}

// ExtensionFilter is a filter field of an extension point with its type
// resolved, as a field of an authorization object, and the values of
// implementations that do not name it.
type ExtensionFilter struct {
	AuthorizationField
	Default model.Optional[[]model.ValueEntry] `json:"default"`
}

// Implementation is an implementation of an extension point.
type Implementation struct {
	Name        string            `json:"name"`
	Package     model.PackageID   `json:"package"`
	Description map[string]string `json:"description"`
	Class       string            `json:"class"`
	// Filters are the values per filter field, defaults included.
	Filters map[string][]model.ValueEntry `json:"filters"`
	Order   int32                         `json:"order"`
	// Activation is the activation, which the kernel adds; the reflector
	// of a schema alone leaves it out.
	Activation *Activation `json:"activation,omitempty"`
}

// Activation is the activation of an implementation: the global setting,
// null when none was made, the settings of tenants, and the result
// without tenant.
type Activation struct {
	Active  bool                 `json:"active"`
	Global  model.Optional[bool] `json:"global"`
	Tenants map[string]bool      `json:"tenants"`
}

// ExtensionPoint returns the definition of an extension point, or
// rterr.NotFound.
func (r *Reflector) ExtensionPoint(name string) (*ExtensionPoint, error) {
	p := r.s.ExtensionPoints[name]
	if p == nil {
		return nil, &rterr.NotFound{Kind: "extension point", Name: name}
	}
	out := &ExtensionPoint{FormatVersion: FormatVersion, Name: p.Name, Package: p.Owner, Description: r.texts(p.Description), Interface: p.Interface,
		Use: p.Use, Filters: []ExtensionFilter{}, Fallback: p.Fallback, CommitAllowed: p.CommitAllowed, Activation: p.Activation, Implementations: []Implementation{}, Languages: r.langs}
	for _, f := range p.Filters {
		af := model.AuthorizationField{Name: f.Name, DataElement: f.DataElement, Domain: f.Domain}
		out.Filters = append(out.Filters, ExtensionFilter{AuthorizationField: r.field(&model.AuthorizationObject{}, af), Default: f.Default})
	}
	for _, impl := range r.s.ImplementationsOf(name) {
		out.Implementations = append(out.Implementations, Implementation{Name: impl.Name, Package: impl.Owner, Description: r.texts(impl.Description),
			Class: impl.Class, Filters: r.s.ImplementationValues(impl), Order: impl.Order})
	}
	return out, nil
}

// ExtensionPoints returns the extension points of a package, or of all
// packages for "", ordered by name.
func (r *Reflector) ExtensionPoints(pkg model.PackageID) []*ExtensionPoint {
	out := []*ExtensionPoint{}
	for _, name := range r.s.ExtensionPointNames() {
		if pkg != "" && r.s.ExtensionPoints[name].Owner != pkg {
			continue
		}
		p, _ := r.ExtensionPoint(name)
		out = append(out, p)
	}
	return out
}

// texts keeps the texts in the languages of the reflector.
func (r *Reflector) texts(in map[string]string) map[string]string {
	out := map[string]string{}
	for _, l := range r.langs {
		if t, ok := in[l]; ok {
			out[l] = t
		}
	}
	return out
}
