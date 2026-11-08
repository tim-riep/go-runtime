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

// Package schemas embeds the JSON Schemas of the package file formats.
package schemas

import "embed"

// FS holds the *.schema.json files.
//
//go:embed *.schema.json
var FS embed.FS

// BaseURL is the common prefix of the schema IDs.
const BaseURL = "https://github.com/tim-riep/go-runtime/schemas/"

// Schema file names.
const (
	Common              = "common.schema.json"
	Manifest            = "manifest.schema.json"
	Domain              = "domain.schema.json"
	Table               = "table.schema.json"
	Extension           = "extension.schema.json"
	DataElement         = "data-element.schema.json"
	Setting             = "setting.schema.json"
	SettingExtension    = "setting-extension.schema.json"
	Asset               = "asset.schema.json"
	Structure           = "structure.schema.json"
	ArrayType           = "array-type.schema.json"
	StructureExtension  = "structure-extension.schema.json"
	Transformation      = "transformation.schema.json"
	HTTPService         = "http-service.schema.json"
	AuthorizationObject = "authorization-object.schema.json"
	Role                = "role.schema.json"
	ExtensionPoint      = "extension-point.schema.json"
	Implementation      = "extension-implementation.schema.json"
	BusinessObject      = "business-object.schema.json"
)
