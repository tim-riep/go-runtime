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
	"fmt"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// EntityResponseClass is the class of the response that methods of
// behavior classes get.
const EntityResponseClass = "/TRBASE/ENTITY_RESPONSE"

func init() {
	native.Default.RegisterClass(pkg, EntityResponseClass, (*native.EntityResponse)(nil))
	add := func(r *native.EntityResponse, position int32, severity MessageSeverity, text string, field model.Optional[string]) error {
		if position < 1 || int(position) > r.Count {
			return fmt.Errorf("position %d is not one of the %d keys", position, r.Count)
		}
		if severity == MessageSeverityError {
			r.Failed[int(position)] = true
		}
		if text != "" || severity != MessageSeverityError {
			r.Messages = append(r.Messages, native.EntityMessage{Position: int(position), Severity: string(severity), Text: text, Field: field})
		}
		return nil
	}
	instance(EntityResponseClass, "FAIL", "(position: INT4)", func(c *native.Context, r *native.EntityResponse, position int32) error {
		return add(r, position, MessageSeverityError, "", model.None[string]())
	})
	instance(EntityResponseClass, "ERROR", "(position: INT4, text: STRING)", func(c *native.Context, r *native.EntityResponse, position int32, text string) error {
		return add(r, position, MessageSeverityError, text, model.None[string]())
	})
	instance(EntityResponseClass, "FIELD_ERROR", "(position: INT4, field: STRING, text: STRING)",
		func(c *native.Context, r *native.EntityResponse, position int32, field, text string) error {
			return add(r, position, MessageSeverityError, text, model.Some(field))
		})
	instance(EntityResponseClass, "MESSAGE", "(position: INT4, severity: /TRBASE/MESSAGE_SEVERITY, text: STRING)",
		func(c *native.Context, r *native.EntityResponse, position int32, severity MessageSeverity, text string) error {
			return add(r, position, severity, text, model.None[string]())
		})
	instance(EntityResponseClass, "GLOBAL_MESSAGE", "(severity: /TRBASE/MESSAGE_SEVERITY, text: STRING)",
		func(c *native.Context, r *native.EntityResponse, severity MessageSeverity, text string) error {
			r.Messages = append(r.Messages, native.EntityMessage{Severity: string(severity), Text: text})
			return nil
		})
}
