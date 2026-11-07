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

// Package mimetype derives the media type of an asset from its file
// extension. The table is fixed, so the result does not depend on the
// configuration of the system the kernel runs on.
package mimetype

import (
	"path"
	"strings"
)

// Default is the type of files with an unknown extension.
const Default = "application/octet-stream"

var byExtension = map[string]string{
	"avif":  "image/avif",
	"bmp":   "image/bmp",
	"css":   "text/css",
	"csv":   "text/csv",
	"eot":   "application/vnd.ms-fontobject",
	"gif":   "image/gif",
	"htm":   "text/html",
	"html":  "text/html",
	"ico":   "image/vnd.microsoft.icon",
	"jpeg":  "image/jpeg",
	"jpg":   "image/jpeg",
	"js":    "text/javascript",
	"json":  "application/json",
	"map":   "application/json",
	"md":    "text/markdown",
	"mjs":   "text/javascript",
	"otf":   "font/otf",
	"pdf":   "application/pdf",
	"png":   "image/png",
	"svg":   "image/svg+xml",
	"tif":   "image/tiff",
	"tiff":  "image/tiff",
	"ttf":   "font/ttf",
	"txt":   "text/plain",
	"wasm":  "application/wasm",
	"webp":  "image/webp",
	"woff":  "font/woff",
	"woff2": "font/woff2",
	"xml":   "application/xml",
	"xsd":   "application/xml",
	"xsl":   "application/xslt+xml",
	"xslt":  "application/xslt+xml",
	"zip":   "application/zip",
}

// ForPath returns the media type for the extension of p, ignoring case.
func ForPath(p string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
	if t, ok := byExtension[ext]; ok {
		return t
	}
	return Default
}
