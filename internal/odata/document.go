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

package odata

// serviceEntry is an entry of the service document.
type serviceEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// ServiceDocument returns the service document of a service whose URL is
// base: its entity sets. Action imports are not listed, as OData lists
// only function imports.
func ServiceDocument(m *Model, base string) map[string]any {
	value := []serviceEntry{}
	for _, s := range m.Sets {
		value = append(value, serviceEntry{Name: s.Name, Kind: "EntitySet", URL: s.Name})
	}
	return map[string]any{"@odata.context": base + "$metadata", "value": value}
}
