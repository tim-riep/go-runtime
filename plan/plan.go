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

// Package plan describes what an operation will do: the logical changes and
// the SQL statements that carry them out.
package plan

import (
	"fmt"
	"strings"

	"github.com/tim-riep/go-runtime/model"
)

// Operation names the kind of operation a plan belongs to.
type Operation string

// Operations.
const (
	Install        Operation = "install"
	Upgrade        Operation = "upgrade"
	Uninstall      Operation = "uninstall"
	Migrate        Operation = "migrate"
	TenantDelete   Operation = "tenant-delete"
	LanguageRemove Operation = "language-remove"
)

// ChangeKind classifies a logical change.
type ChangeKind string

// Change kinds.
const (
	CreateDomain    ChangeKind = "create-domain"
	DropDomain      ChangeKind = "drop-domain"
	AlterDomain     ChangeKind = "alter-domain"
	CreateTable     ChangeKind = "create-table"
	DropTable       ChangeKind = "drop-table"
	RecreateTable   ChangeKind = "recreate-table" // tenant flag changed
	AddColumn       ChangeKind = "add-column"
	DropColumn      ChangeKind = "drop-column"
	AlterType       ChangeKind = "alter-type"
	AlterValues     ChangeKind = "alter-values" // fixed values of the column's domain changed
	SetNotNull      ChangeKind = "set-not-null"
	DropNotNull     ChangeKind = "drop-not-null"
	SetDefault      ChangeKind = "set-default"
	AlterPrimaryKey ChangeKind = "alter-primary-key"
	AddForeignKey   ChangeKind = "add-foreign-key"
	DropForeignKey  ChangeKind = "drop-foreign-key"
	DeleteRows      ChangeKind = "delete-rows"

	// Changes of the repository only; they produce no statements.
	AlterDataClass        ChangeKind = "alter-data-class"
	CreateDataElement     ChangeKind = "create-data-element"
	DropDataElement       ChangeKind = "drop-data-element"
	AlterDataElement      ChangeKind = "alter-data-element"
	AlterFieldReference   ChangeKind = "alter-field-reference" // field switches between domain and data element
	CreateSetting         ChangeKind = "create-setting"
	DropSetting           ChangeKind = "drop-setting"
	AlterSetting          ChangeKind = "alter-setting"
	AddSettingExtension   ChangeKind = "add-setting-extension"
	DropSettingExtension  ChangeKind = "drop-setting-extension"
	AlterSettingExtension ChangeKind = "alter-setting-extension"

	CreateStructure         ChangeKind = "create-structure"
	DropStructure           ChangeKind = "drop-structure"
	AlterStructure          ChangeKind = "alter-structure"
	AddStructureExtension   ChangeKind = "add-structure-extension"
	DropStructureExtension  ChangeKind = "drop-structure-extension"
	AlterStructureExtension ChangeKind = "alter-structure-extension"
	CompilePackage          ChangeKind = "compile-package" // the code of a package is compiled
	// CompileTransformation: an XSLT stylesheet is new or changed and is
	// compiled to check it.
	CompileTransformation ChangeKind = "compile-transformation"
	CreateArrayType       ChangeKind = "create-array-type"
	DropArrayType         ChangeKind = "drop-array-type"
	AlterArrayType        ChangeKind = "alter-array-type"

	AddAssetRepository   ChangeKind = "add-asset-repository"
	DropAssetRepository  ChangeKind = "drop-asset-repository"
	AlterAssetRepository ChangeKind = "alter-asset-repository"
	AddAssetFolder       ChangeKind = "add-asset-folder"
	DropAssetFolder      ChangeKind = "drop-asset-folder"
	AlterAssetFolder     ChangeKind = "alter-asset-folder"
	AddAsset             ChangeKind = "add-asset"
	ReplaceAsset         ChangeKind = "replace-asset" // content changed
	DropAsset            ChangeKind = "drop-asset"
	AlterAsset           ChangeKind = "alter-asset" // media type or description changed
	AddAssetVariant      ChangeKind = "add-asset-variant"
	ReplaceAssetVariant  ChangeKind = "replace-asset-variant"
	DropAssetVariant     ChangeKind = "drop-asset-variant"
)

// Physical reports whether the change alters the database layout. Other
// changes only update the repository.
func (k ChangeKind) Physical() bool {
	switch k {
	case CreateDomain, DropDomain, AlterDomain, AlterDataClass, CreateDataElement, DropDataElement, AlterDataElement,
		AlterFieldReference, CreateSetting, DropSetting, AlterSetting, AddSettingExtension, DropSettingExtension, AlterSettingExtension,
		CreateStructure, DropStructure, AlterStructure, AddStructureExtension, DropStructureExtension, AlterStructureExtension,
		CreateArrayType, DropArrayType, AlterArrayType, CompilePackage, CompileTransformation,
		AddAssetRepository, DropAssetRepository, AlterAssetRepository, AddAssetFolder, DropAssetFolder, AlterAssetFolder,
		AddAsset, ReplaceAsset, DropAsset, AlterAsset, AddAssetVariant, ReplaceAssetVariant, DropAssetVariant:
		return false
	}
	return true
}

// Change is one logical change.
type Change struct {
	Kind   ChangeKind `json:"kind"`
	Table  string     `json:"table,omitempty"`
	Column string     `json:"column,omitempty"`
	Name   string     `json:"name,omitempty"` // domain or foreign key name
	// Asset changes name the package, the repository and the path
	// separately; Language is set for variants.
	Package    string `json:"package,omitempty"`
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path,omitempty"`
	Language   string `json:"language,omitempty"`
	Detail     string `json:"detail,omitempty"`
	// Via names the structure or table that declares an included column,
	// i.e. where a column change comes from.
	Via         string `json:"via,omitempty"`
	Destructive bool   `json:"destructive"`
	// Notice marks changes that are not destructive but need attention,
	// such as a changed data class.
	Notice bool `json:"notice,omitempty"`
}

// Object names the affected object.
func (c Change) Object() string {
	if c.Repository != "" {
		s := "package " + c.Package + ", repository " + c.Repository
		if c.Path != "" {
			s += ", path " + c.Path
		}
		if c.Language != "" {
			s += ", language " + c.Language
		}
		return s
	}
	switch {
	case c.Column != "":
		return c.Table + "." + c.Column
	case c.Table != "" && c.Name != "":
		return c.Table + "." + c.Name
	case c.Table != "":
		return c.Table
	}
	return c.Name
}

func (c Change) String() string {
	s := string(c.Kind) + " " + c.Object()
	if c.Detail != "" {
		s += " (" + c.Detail + ")"
	}
	if c.Via != "" {
		s += " via " + c.Via
	}
	return s
}

// Step is a group of statements executed for one purpose.
type Step struct {
	Description string   `json:"description"`
	Destructive bool     `json:"destructive"`
	Statements  []string `json:"statements"`
}

// PackageChange records the version change of one package.
type PackageChange struct {
	ID   string                 `json:"id"`
	From model.Optional[string] `json:"from"` // None for installations
	To   model.Optional[string] `json:"to"`   // None for removals
}

// Plan is the complete description of an operation.
type Plan struct {
	Operation Operation              `json:"operation"`
	Dialect   string                 `json:"dialect"`
	Packages  []PackageChange        `json:"packages,omitempty"`
	Target    model.Optional[string] `json:"target"` // e.g. the tenant of a tenant deletion
	Changes   []Change               `json:"changes"`
	Steps     []Step                 `json:"steps"`
	Executed  bool                   `json:"executed"`
	LogID     string                 `json:"logId,omitempty"`
}

// Destructive returns the destructive changes.
func (p *Plan) Destructive() []Change {
	var out []Change
	for _, c := range p.Changes {
		if c.Destructive {
			out = append(out, c)
		}
	}
	return out
}

// Notices returns the changes marked as notice.
func (p *Plan) Notices() []Change {
	var out []Change
	for _, c := range p.Changes {
		if c.Notice {
			out = append(out, c)
		}
	}
	return out
}

// Statements returns all statements in execution order.
func (p *Plan) Statements() []string {
	var out []string
	for _, s := range p.Steps {
		out = append(out, s.Statements...)
	}
	return out
}

// SQL renders the statements as a script with one comment per step.
func (p *Plan) SQL() string {
	var sb strings.Builder
	for _, s := range p.Steps {
		sb.WriteString("-- ")
		sb.WriteString(s.Description)
		if s.Destructive {
			sb.WriteString(" [DESTRUCTIVE]")
		}
		sb.WriteByte('\n')
		for _, st := range s.Statements {
			sb.WriteString(st)
			sb.WriteString(";\n")
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Summary returns a one-line description of the operation.
func (p *Plan) Summary() string {
	var parts []string
	for _, pc := range p.Packages {
		switch {
		case !pc.From.Valid():
			parts = append(parts, pc.ID+" "+pc.To.Or(""))
		case !pc.To.Valid():
			parts = append(parts, pc.ID+" "+pc.From.Or(""))
		default:
			parts = append(parts, fmt.Sprintf("%s %s -> %s", pc.ID, pc.From.Or(""), pc.To.Or("")))
		}
	}
	if t, ok := p.Target.Get(); ok {
		parts = append(parts, t)
	}
	return fmt.Sprintf("%s %s (%s)", p.Operation, strings.Join(parts, ", "), p.Dialect)
}

// Text renders the plan for humans: summary, changes with destructive ones
// marked, and the statements.
func (p *Plan) Text() string {
	var sb strings.Builder
	sb.WriteString("Plan: ")
	sb.WriteString(p.Summary())
	sb.WriteString("\n\nChanges:\n")
	if len(p.Changes) == 0 {
		sb.WriteString("  (none)\n")
	}
	for _, c := range p.Changes {
		mark := "  "
		switch {
		case c.Destructive:
			mark = "! "
		case c.Notice:
			mark = "~ "
		}
		sb.WriteString("  ")
		sb.WriteString(mark)
		sb.WriteString(c.String())
		switch {
		case c.Destructive:
			sb.WriteString("  [DESTRUCTIVE]")
		case c.Kind == AlterDataClass:
			sb.WriteString("  [DATA CLASS]")
		}
		sb.WriteByte('\n')
	}
	if n := len(p.Destructive()); n > 0 {
		fmt.Fprintf(&sb, "\n%d destructive change(s).\n", n)
	}
	classes := 0
	for _, c := range p.Changes {
		if c.Kind == AlterDataClass {
			classes++
		}
	}
	if classes > 0 {
		fmt.Fprintf(&sb, "\n%d data class change(s).\n", classes)
	}
	sb.WriteString("\nStatements:\n")
	if len(p.Steps) == 0 {
		sb.WriteString("  (none)\n")
	}
	for _, line := range strings.Split(strings.TrimRight(p.SQL(), "\n"), "\n") {
		if line == "" {
			sb.WriteByte('\n')
			continue
		}
		sb.WriteString("  ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}
