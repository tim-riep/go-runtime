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

package diff

import (
	"fmt"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/plan"
)

// services lists the changes of services. Services hold no data, so no
// change is destructive; clients of a changed service see the new
// metadata.
func services(cur, want *model.Schema) []plan.Change {
	var out []plan.Change
	for _, name := range want.ServiceNames() {
		w := want.Services[name]
		c, ok := cur.Services[name]
		switch {
		case !ok:
			out = append(out, plan.Change{Kind: plan.CreateService, Name: name, Detail: fmt.Sprintf("%d entity sets at %s", len(w.EntitySets), w.Path())})
		case !sameJSON(c, w):
			out = append(out, plan.Change{Kind: plan.AlterService, Name: name, Detail: fmt.Sprintf("%d entity sets", len(w.EntitySets))})
		}
	}
	for _, name := range cur.ServiceNames() {
		if _, ok := want.Services[name]; !ok {
			out = append(out, plan.Change{Kind: plan.DropService, Name: name, Detail: "its path answers 404"})
		}
	}
	return out
}
