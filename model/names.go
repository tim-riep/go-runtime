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

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

const (
	// KernelPackageID is reserved; no package may use it. Kernel objects
	// live in its namespace.
	KernelPackageID = "RT"

	// KernelNamespace prefixes the names of all kernel objects.
	KernelNamespace = "/" + KernelPackageID + "/"

	// TenantTable is the kernel table holding the tenants.
	TenantTable = KernelNamespace + "TENANTS"

	// TenantTableKey is the key field of TenantTable.
	TenantTableKey = "TENANT"

	// TenantField is the field the kernel adds as first key field to every
	// tenant-specific table.
	TenantField = KernelNamespace + "TENANT"

	// TenantDomain is the domain of the tenant key.
	TenantDomain = KernelNamespace + "TENANT_ID"

	// TenantForeignKey is the name of the foreign key from TenantField to
	// TenantTable.
	TenantForeignKey = KernelNamespace + "TENANT"

	// DefaultLanguage is the language that always exists and in which every
	// translatable object must have texts.
	DefaultLanguage = "en"

	// MaxIdentifierBytes is the maximum length of any generated SQL
	// identifier. It matches the PostgreSQL limit and applies to all dialects
	// so packages stay portable.
	MaxIdentifierBytes = 63

	// MaxPackageIDLength limits package IDs so namespaced names stay short.
	MaxPackageIDLength = 16

	// MaxDecimalPrecision is the maximum precision of DEC. It is bounded by
	// the scaled 64-bit integer representation used for SQLite and applies
	// to all dialects.
	MaxDecimalPrecision = 18

	// MaxCharLength is the maximum length of CHAR.
	MaxCharLength = 10485760

	constraintSep = "#"
)

var (
	packageIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)
	localNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	tenantIDPattern  = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_]*$`)
)

// ValidPackageID reports whether id is a syntactically valid package ID.
func ValidPackageID(id string) bool {
	return packageIDPattern.MatchString(id) && len(id) <= MaxPackageIDLength
}

// ValidLocalName reports whether name is a valid name of a table, domain,
// field or foreign key as written in a package.
func ValidLocalName(name string) bool { return localNamePattern.MatchString(name) }

// ValidTenantID reports whether id is a valid tenant key.
func ValidTenantID(id string) bool {
	return tenantIDPattern.MatchString(id) && len(id) <= TenantIDLength
}

// TenantIDLength is the length of the tenant key domain.
const TenantIDLength = 16

// Namespace returns the namespace prefix of a package, e.g. "/TROTTO/".
func Namespace(id PackageID) string { return "/" + string(id) + "/" }

// SplitName splits a name with namespace into package ID and the part after
// the namespace. It reports false for names without namespace.
func SplitName(name string) (PackageID, string, bool) {
	if !strings.HasPrefix(name, "/") {
		return "", "", false
	}
	pkg, local, ok := strings.Cut(name[1:], "/")
	if !ok {
		return "", "", false
	}
	return PackageID(pkg), local, true
}

// OwnerNamespace returns the namespace of an owner; kernel objects live in
// KernelNamespace.
func OwnerNamespace(owner PackageID) string {
	if owner == Kernel {
		return KernelNamespace
	}
	return Namespace(owner)
}

// Qualify prefixes a local name with the namespace of id.
func Qualify(id PackageID, local string) string { return Namespace(id) + local }

// IsKernelName reports whether name lies in the kernel namespace.
func IsKernelName(name string) bool { return strings.HasPrefix(name, KernelNamespace) }

// GeneratedName returns the SQL name the kernel generates for a logical
// name such as "/TRFIN/COMPANIES#FK#COUNTRY". A logical name that fits into
// MaxIdentifierBytes is used as it is. A longer one keeps its first 52
// bytes, followed by "~" and the first 10 hex digits of its SHA-256, which
// makes exactly MaxIdentifierBytes. The result is deterministic; "#" and "~"
// never occur in object names, so generated names cannot collide with
// them.
func GeneratedName(logical string) string {
	if len(logical) <= MaxIdentifierBytes {
		return logical
	}
	sum := sha256.Sum256([]byte(logical))
	const hashLen = 10
	return logical[:MaxIdentifierBytes-hashLen-1] + "~" + hex.EncodeToString(sum[:])[:hashLen]
}

// PrimaryKeyName returns the name of the primary key constraint of a table,
// which is also the name of its index.
func PrimaryKeyName(table string) string { return GeneratedName(table + constraintSep + "PK") }

// ForeignKeyName returns the constraint name of a foreign key.
func ForeignKeyName(table, fk string) string {
	return GeneratedName(table + constraintSep + "FK" + constraintSep + fk)
}

// CheckName returns the name of the check constraint of a column.
func CheckName(table, field string) string {
	return GeneratedName(table + constraintSep + "CK" + constraintSep + field)
}

// RebuildName returns the name of the temporary table used while a table is
// rebuilt.
func RebuildName(table string) string { return GeneratedName(table + constraintSep + "NEW") }
