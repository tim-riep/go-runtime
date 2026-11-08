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

// Package rterr defines the typed errors returned by the kernel.
//
// Every error the kernel reports for a domain condition is one of the types
// in this package, so callers can inspect it with errors.As instead of
// parsing messages. Several problems found in one pass are returned as a
// List, which unwraps to its members.
package rterr

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// Code classifies a validation problem.
type Code string

// Validation codes.
const (
	CodeSchema          Code = "schema"           // document does not match its JSON Schema
	CodeSyntax          Code = "syntax"           // file is not valid JSON, or a source file has a syntax error
	CodeFile            Code = "file"             // unexpected, missing or misnamed file
	CodeName            Code = "name"             // invalid or reserved name
	CodeNameTooLong     Code = "name-too-long"    // identifier exceeds the byte limit
	CodeDuplicate       Code = "duplicate"        // name declared twice
	CodeUnknownDomain   Code = "unknown-domain"   // domain does not exist
	CodeUnknownTable    Code = "unknown-table"    // table does not exist
	CodeUnknownField    Code = "unknown-field"    // field does not exist
	CodeNotVisible      Code = "not-visible"      // object belongs to a package that is not required
	CodeTypeParams      Code = "type-params"      // invalid length or decimals for a base type
	CodeFixedValue      Code = "fixed-value"      // invalid fixed value
	CodeDefault         Code = "default"          // invalid default value
	CodePrimaryKey      Code = "primary-key"      // invalid primary key
	CodeForeignKey      Code = "foreign-key"      // invalid foreign key
	CodeTypeMismatch    Code = "type-mismatch"    // foreign key field types are incompatible
	CodeTenant          Code = "tenant"           // violates a tenant rule
	CodeExtension       Code = "extension"        // violates an extension rule
	CodeReservedObject  Code = "reserved-object"  // kernel object may not be used this way
	CodeManifestMissing Code = "manifest-missing" // package has no manifest
	CodeDataClass       Code = "data-class"       // invalid or reserved data class
	CodeText            Code = "text"             // missing or invalid translatable text
	CodeDataElement     Code = "data-element"     // invalid data element or field reference
	CodeSetting         Code = "setting"          // invalid setting or setting extension
	CodeAsset           Code = "asset"            // invalid asset repository, folder or asset
	CodeStructure       Code = "structure"        // invalid structure, array type, include or structure extension
	CodeSource          Code = "source"           // invalid source file or language version
	CodeAuthorization   Code = "authorization"    // invalid authorization object
	CodeRole            Code = "role"             // invalid role or authorization of a role
	CodeExtensionPoint  Code = "extension-point"  // invalid extension point or implementation
	CodeBusinessObject  Code = "business-object"  // invalid business object
)

// Location points at the place in a package that caused an error.
type Location struct {
	Package string `json:"package,omitempty"`
	File    string `json:"file,omitempty"`
	Pointer string `json:"pointer,omitempty"` // JSON Pointer inside File
	// Line and Column point into source files; Column counts characters.
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
}

// IsZero reports whether no location information is present.
func (l Location) IsZero() bool { return l == Location{} }

func (l Location) String() string {
	var parts []string
	if l.Package != "" {
		parts = append(parts, "package "+l.Package)
	}
	if l.File != "" {
		parts = append(parts, "file "+l.File)
	}
	if l.Pointer != "" {
		parts = append(parts, "at "+l.Pointer)
	}
	if l.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d, column %d", l.Line, l.Column))
	}
	return strings.Join(parts, ", ")
}

// At returns a copy of l with pointer appended to its JSON Pointer.
func (l Location) At(pointer string) Location {
	l.Pointer += pointer
	return l
}

// ValidationError reports a problem in a package or in the resulting schema.
type ValidationError struct {
	Code     Code
	Location Location
	Object   string // affected object, e.g. "table COMPANIES"
	Message  string
}

func (e *ValidationError) Error() string {
	var sb strings.Builder
	if !e.Location.IsZero() {
		sb.WriteString(e.Location.String())
		sb.WriteString(": ")
	}
	if e.Object != "" {
		sb.WriteString(e.Object)
		sb.WriteString(": ")
	}
	sb.WriteString(e.Message)
	return sb.String()
}

// List collects several errors. It unwraps to its members, so errors.As
// finds the first member of the requested type.
type List []error

func (l List) Error() string {
	msgs := make([]string, len(l))
	for i, err := range l {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "\n")
}

// Unwrap returns the members of the list.
func (l List) Unwrap() []error { return l }

// Err returns nil for an empty list, the only member for a list of one, and
// the list itself otherwise.
func (l List) Err() error {
	switch len(l) {
	case 0:
		return nil
	case 1:
		return l[0]
	}
	return l
}

// All returns every error of type T contained in err, including members of
// nested lists.
func All[T error](err error) []T {
	var out []T
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if t, ok := e.(T); ok {
			out = append(out, t)
			return
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, m := range u.Unwrap() {
				walk(m)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	return out
}

// DependencyKind classifies a DependencyError.
type DependencyKind string

// Dependency error kinds.
const (
	DependencyMissing  DependencyKind = "missing"
	DependencyVersion  DependencyKind = "version"
	DependencyCycle    DependencyKind = "cycle"
	DependencyConflict DependencyKind = "conflict" // same package given twice
)

// DependencyError reports an unresolvable package dependency.
type DependencyError struct {
	Kind       DependencyKind
	Package    string   // package whose requirement fails
	Required   string   // required package
	Constraint string   // version constraint of the requirement
	Found      string   // version that was found, if any
	Cycle      []string // packages forming a cycle
}

func (e *DependencyError) Error() string {
	switch e.Kind {
	case DependencyMissing:
		return fmt.Sprintf("package %s requires %s, which is neither installed nor part of this operation", e.Package, e.Required)
	case DependencyVersion:
		return fmt.Sprintf("package %s requires %s %s, but version %s is available", e.Package, e.Required, e.Constraint, e.Found)
	case DependencyCycle:
		return "dependency cycle: " + strings.Join(e.Cycle, " -> ")
	case DependencyConflict:
		return fmt.Sprintf("package %s is given more than once", e.Package)
	}
	return "dependency error"
}

// NameConflict reports that an object name is already taken by another owner.
type NameConflict struct {
	Kind     string // "table" or "domain"
	Name     string
	Owner    string // current owner, empty for the kernel
	Package  string // package that tried to declare the name
	Location Location
}

func (e *NameConflict) Error() string {
	owner := e.Owner
	if owner == "" {
		owner = "the kernel"
	}
	msg := fmt.Sprintf("%s %s of package %s is already owned by %s", e.Kind, e.Name, e.Package, owner)
	if !e.Location.IsZero() {
		msg = e.Location.String() + ": " + msg
	}
	return msg
}

// Change describes one planned change in a DestructiveChange error.
type Change struct {
	Object      string
	Description string
}

// DestructiveChange reports that a plan contains destructive or restricting
// changes and the caller did not allow them.
type DestructiveChange struct {
	Package string
	Changes []Change
}

func (e *DestructiveChange) Error() string {
	lines := []string{fmt.Sprintf("operation on %s contains %d destructive change(s); allow them explicitly to proceed:", e.Package, len(e.Changes))}
	for _, c := range e.Changes {
		lines = append(lines, "  "+c.Object+": "+c.Description)
	}
	return strings.Join(lines, "\n")
}

// InUse reports that a package cannot be removed because others require it.
type InUse struct {
	Package    string
	RequiredBy []string
}

func (e *InUse) Error() string {
	return fmt.Sprintf("package %s is required by %s", e.Package, strings.Join(e.RequiredBy, ", "))
}

// NotInstalled reports that a package is not installed.
type NotInstalled struct{ Package string }

func (e *NotInstalled) Error() string { return "package " + e.Package + " is not installed" }

// AlreadyInstalled reports that a package is already installed.
type AlreadyInstalled struct {
	Package string
	Version string
}

func (e *AlreadyInstalled) Error() string {
	return fmt.Sprintf("package %s is already installed in version %s", e.Package, e.Version)
}

// ConfirmationRequired reports that an operation loses data and needs
// explicit confirmation.
type ConfirmationRequired struct {
	Operation string
	Object    string
}

func (e *ConfirmationRequired) Error() string {
	return fmt.Sprintf("%s of %s deletes data and must be confirmed explicitly", e.Operation, e.Object)
}

// NotFound reports that a requested object does not exist.
type NotFound struct {
	Kind string
	Name string
}

func (e *NotFound) Error() string { return e.Kind + " " + e.Name + " not found" }

// Ambiguous reports that a name matches several kinds of objects.
type Ambiguous struct {
	Name  string
	Kinds []string
}

func (e *Ambiguous) Error() string {
	kinds := make([]string, len(e.Kinds))
	for i, k := range e.Kinds {
		kinds[i] = article(k) + " " + k
	}
	return fmt.Sprintf("%s is ambiguous, it names %s; qualify it as %s", e.Name, strings.Join(kinds, " and "), strings.Join(e.Kinds, ":"+e.Name+" or ")+":"+e.Name)
}

func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// InvalidArgument reports an invalid argument passed to the API.
type InvalidArgument struct {
	Argument string
	Message  string
}

func (e *InvalidArgument) Error() string { return "invalid " + e.Argument + ": " + e.Message }

// ExecError reports a failed statement while executing a plan. The whole
// plan has been rolled back when this error is returned.
type ExecError struct {
	Step      string
	Statement string
	Err       error
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("executing %q failed (rolled back): %v\nstatement: %s", e.Step, e.Err, e.Statement)
}

func (e *ExecError) Unwrap() error { return e.Err }

// ViolationKind classifies a ConstraintViolation.
type ViolationKind string

// Constraint violation kinds.
const (
	ViolationForeignKey ViolationKind = "foreign-key"
	ViolationCheck      ViolationKind = "check"
	ViolationNotNull    ViolationKind = "not-null"
	ViolationUnique     ViolationKind = "unique"
)

// ConstraintViolation reports a database constraint violation.
type ConstraintViolation struct {
	Kind       ViolationKind
	Table      string
	Constraint string
	Err        error
}

func (e *ConstraintViolation) Error() string {
	msg := string(e.Kind) + " constraint violated"
	if e.Table != "" {
		msg += " on " + e.Table
	}
	if e.Constraint != "" {
		msg += " (" + e.Constraint + ")"
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *ConstraintViolation) Unwrap() error { return e.Err }

// LockTimeout reports a statement that waited too long for a lock.
type LockTimeout struct{ Err error }

func (e *LockTimeout) Error() string { return "lock timeout: " + e.Err.Error() }

func (e *LockTimeout) Unwrap() error { return e.Err }

// TenantReferenced reports that a tenant cannot be deleted because tables
// that are not tenant-specific still reference it.
type TenantReferenced struct {
	Tenant string
	Tables []string
}

func (e *TenantReferenced) Error() string {
	return fmt.Sprintf("tenant %s is still referenced by %s", e.Tenant, strings.Join(e.Tables, ", "))
}

// NamespaceViolation reports a name that does not carry an allowed
// namespace. Definitions must carry the namespace of their package;
// references that of the package, of a package it requires directly, or of
// the kernel.
type NamespaceViolation struct {
	Location  Location
	Name      string   // name found
	Expected  []string // allowed namespaces
	Reference bool     // the name is a reference, not a definition
}

func (e *NamespaceViolation) Error() string {
	what := "definition"
	if e.Reference {
		what = "reference"
	}
	msg := fmt.Sprintf("%s %s: expected namespace %s", what, e.Name, strings.Join(e.Expected, ", "))
	if !e.Location.IsZero() {
		msg = e.Location.String() + ": " + msg
	}
	return msg
}

// AssetNotFound reports that an asset repository, a folder or an asset
// does not exist, or has no version for the requested language. It matches
// fs.ErrNotExist.
type AssetNotFound struct {
	Package    string
	Repository string
	Path       string // empty if the repository does not exist
	Language   string
}

func (e *AssetNotFound) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("package %s has no asset repository %s", e.Package, e.Repository)
	}
	msg := fmt.Sprintf("asset %s not found in repository %s of package %s", e.Path, e.Repository, e.Package)
	if e.Language != "" {
		msg += " for language " + e.Language
	}
	return msg
}

// Is reports whether target is fs.ErrNotExist.
func (e *AssetNotFound) Is(target error) bool { return target == fs.ErrNotExist }

// MissingLanguages reports texts in languages that are not set up in the
// database.
type MissingLanguages struct {
	Languages []string // missing language codes
	Objects   []string // objects with texts in these languages
}

func (e *MissingLanguages) Error() string {
	return fmt.Sprintf("texts in languages that are not set up: %s (used by %s); add the languages first",
		strings.Join(e.Languages, ", "), strings.Join(e.Objects, ", "))
}

// UnsupportedDatabase reports an unknown connection string.
type UnsupportedDatabase struct{ DSN string }

func (e *UnsupportedDatabase) Error() string {
	return "unsupported database connection string; use sqlite:<path> or postgres://..."
}

// ErrNotInitialized is returned when the kernel tables are missing or older
// than this kernel version. Run Init first.
var ErrNotInitialized = errors.New("database is not initialized for this kernel version; run init")

// ErrNewerSchema is returned when the database was initialized by a newer
// kernel version.
var ErrNewerSchema = errors.New("database was initialized by a newer kernel version")

// CompileDiagnostic is an error in the source files of a package.
type CompileDiagnostic struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// IncludedFrom lists the inclusions through which the code was
	// inserted, innermost first, e.g. "/TRX/CHECKS included from src/A.class:4".
	IncludedFrom []string `json:"includedFrom,omitempty"`
}

func (d CompileDiagnostic) String() string {
	s := fmt.Sprintf("%s:%d:%d: %s: %s", d.File, d.Line, d.Column, d.Code, d.Message)
	for _, i := range d.IncludedFrom {
		s += "\n    " + i
	}
	return s
}

// KernelPackage reports an attempt to install, upgrade or remove a kernel
// package, or to install a package marked as one. Kernel packages come
// only from the kernel binary.
type KernelPackage struct {
	Package   string
	Operation string // install, upgrade or uninstall
}

func (e *KernelPackage) Error() string {
	return fmt.Sprintf("%s is a kernel package; the kernel installs and upgrades it from its binary, so it cannot be given to %s", e.Package, e.Operation)
}

// KernelPackageUpgrade reports that the kernel could not install or
// upgrade its kernel packages when opening a database; the kernel does
// not start.
type KernelPackageUpgrade struct {
	Packages []string
	Err      error
}

func (e *KernelPackageUpgrade) Error() string {
	return fmt.Sprintf("the kernel packages %s cannot be brought to the version of this kernel: %v", strings.Join(e.Packages, ", "), e.Err)
}

func (e *KernelPackageUpgrade) Unwrap() error { return e.Err }

// CompileError reports that the code of a package does not compile. The
// whole operation is rejected.
type CompileError struct {
	Package     string
	Reason      string // why the package was compiled, e.g. "installed" or "requires TRFIN"
	Diagnostics []CompileDiagnostic
}

func (e *CompileError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s does not compile", e.Package)
	if e.Reason != "" && e.Reason != "installed" {
		fmt.Fprintf(&b, " (compiled because it %s)", e.Reason)
	}
	for _, d := range e.Diagnostics {
		b.WriteString("\n" + d.String())
	}
	return b.String()
}

// NotAuthorized reports that the actor of a library call lacks an
// authorization. It names the object and the activity, not the
// authorizations of the actor.
type NotAuthorized struct {
	User     string
	Object   string
	Activity string
}

func (e *NotAuthorized) Error() string {
	return fmt.Sprintf("user %s is not authorized for %s with activity %s", e.User, e.Object, e.Activity)
}

// OverlapValues are the values in which two implementations overlap in one
// filter field.
type OverlapValues struct {
	Field  string `json:"field"`
	Values string `json:"values"`
}

// ImplementationOverlap reports two active implementations of an extension
// point used single that match the same filter values. Tenant is empty for
// the global activation.
type ImplementationOverlap struct {
	ExtensionPoint string `json:"extensionPoint"`
	// Implementations are the two implementations.
	Implementations [2]string       `json:"implementations"`
	Tenant          string          `json:"tenant,omitempty"`
	Values          []OverlapValues `json:"values"`
}

func (e *ImplementationOverlap) Error() string {
	where := "when active everywhere"
	if e.Tenant != "" {
		where = "in tenant " + e.Tenant
	}
	msg := fmt.Sprintf("extension point %s runs a single implementation, but %s and %s both match %s", e.ExtensionPoint, e.Implementations[0], e.Implementations[1], where)
	if len(e.Values) == 0 {
		return msg + " (it has no filter fields)"
	}
	var parts []string
	for _, v := range e.Values {
		parts = append(parts, v.Field+" = "+v.Values)
	}
	return msg + ": " + strings.Join(parts, "; ")
}

// UnsafeArchive reports a package archive that is rejected: an entry
// outside the package, a link, a size beyond the limits or no package in
// it.
type UnsafeArchive struct {
	Archive string
	Entry   string // "" for the archive as a whole
	Reason  string
}

func (e *UnsafeArchive) Error() string {
	if e.Entry == "" {
		return fmt.Sprintf("archive %s is rejected: %s", e.Archive, e.Reason)
	}
	return fmt.Sprintf("archive %s is rejected: entry %q %s", e.Archive, e.Entry, e.Reason)
}

// RateLimited reports that a package source refuses requests until Reset.
type RateLimited struct {
	Host  string
	Reset time.Time
}

func (e *RateLimited) Error() string {
	return fmt.Sprintf("the rate limit of %s is reached until %s; set a token (RT_GITHUB_TOKEN) for a higher limit", e.Host, e.Reset.UTC().Format(time.RFC3339))
}

// SourceError reports a failed request to a package source. It names the
// method, the path and the status, never credentials.
type SourceError struct {
	Method string
	URL    string // without query
	Status int    // 0 for network errors
	Detail string
}

func (e *SourceError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("%s %s failed: %s", e.Method, e.URL, e.Detail)
	}
	s := fmt.Sprintf("%s %s answered %d", e.Method, e.URL, e.Status)
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

// IntegrityError reports a package archive that is not what was expected:
// another hash than requested or recorded, or a tag that moved.
type IntegrityError struct {
	Address string
	Detail  string
}

func (e *IntegrityError) Error() string { return fmt.Sprintf("%s: %s", e.Address, e.Detail) }
