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

import _ "embed"

// The vocabulary RT.Fields of the kernel in the XML and the JSON format
// of CSDL; /rt/odata/$vocabularies/ serves them.
var (
	//go:embed vocabulary/RT.Fields.xml
	VocabularyXML []byte
	//go:embed vocabulary/RT.Fields.json
	VocabularyJSON []byte
)
