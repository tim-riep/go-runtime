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

import "testing"

// TestAuthorizationOf resolves the authorization of entities: their own,
// that of the nearest parent entity with one, and the configuration
// object for business objects of configuration.
func TestAuthorizationOf(t *testing.T) {
	root := &EntityAuthorization{Object: "/T/ROOT"}
	middle := &EntityAuthorization{Object: "/T/MIDDLE"}
	b := &BusinessObject{Name: "/T/BO", DataClass: Application, Root: "/T/A", Entities: []Entity{
		{Table: "/T/A", Authorization: root},
		{Table: "/T/B", Parent: Some("/T/A"), Authorization: middle},
		{Table: "/T/C", Parent: Some("/T/B")},
		{Table: "/T/D", Parent: Some("/T/A")},
	}}
	for table, want := range map[string]struct {
		object string
		source AuthorizationSource
		holder string
	}{
		"/T/A": {"/T/ROOT", AuthorizationOwn, "/T/A"},
		"/T/B": {"/T/MIDDLE", AuthorizationOwn, "/T/B"},
		"/T/C": {"/T/MIDDLE", AuthorizationInherited, "/T/B"},
		"/T/D": {"/T/ROOT", AuthorizationInherited, "/T/A"},
	} {
		ra, ok := b.AuthorizationOf(b.Entity(table))
		if !ok || ra.Authorization.Object != want.object || ra.Source != want.source || ra.Holder.Table != want.holder {
			t.Errorf("%s: %+v %v", table, ra, ok)
		}
	}

	b.Entities[0].Authorization, b.Entities[1].Authorization = nil, nil
	if _, ok := b.AuthorizationOf(b.Entity("/T/C")); ok {
		t.Error("application data without authorization")
	}
	b.DataClass = RuntimeConfiguration
	b.Entities[2].Actions = []Action{{Name: "RESET", Instance: true}}
	ra, ok := b.AuthorizationOf(b.Entity("/T/C"))
	if !ok || ra.Source != AuthorizationConfiguration || ra.Authorization.Object != ConfigurationObject || ra.Holder.Table != "/T/C" ||
		ra.Constants[ConfigurationBusinessObject] != "/T/BO" || ra.Constants[ConfigurationDataClass] != "runtimeConfiguration" ||
		ra.Authorization.Activities["create"] != "CREATE" || ra.Authorization.Activities["RESET"] != "UPDATE" {
		t.Errorf("configuration: %+v %v", ra, ok)
	}
}
