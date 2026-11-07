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
	"cmp"
	"slices"

	"github.com/tim-riep/go-runtime/rterr"
)

// MaxSettingLabel is the maximum length of a setting label in characters.
const MaxSettingLabel = 60

// Operations are the operations allowed on the rows of a table in a
// setting.
type Operations struct {
	Create bool `json:"create"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}

// SettingText holds the label of a setting in one language.
type SettingText struct {
	Label       string           `json:"label"`
	Description Optional[string] `json:"description"`
}

// SettingTable is a table of a setting. Every table but the root names its
// parent table in the setting and its foreign key to that parent.
type SettingTable struct {
	Table      string           `json:"table"`
	Parent     Optional[string] `json:"parent"`     // None for the root
	ForeignKey Optional[string] `json:"foreignKey"` // None for the root
	Operations Operations       `json:"operations"`
	Src        rterr.Location   `json:"-"`
}

// Setting groups tables that together form one configuration, as a tree
// below a root table connected through foreign keys.
type Setting struct {
	ID     string                 `json:"id"`
	Owner  PackageID              `json:"owner"`
	Texts  map[string]SettingText `json:"texts"` // by language code
	Root   SettingTable           `json:"root"`
	Tables []SettingTable         `json:"tables,omitempty"` // dependent tables
	Src    rterr.Location         `json:"-"`
}

// Languages returns the languages of the texts in sorted order.
func (s *Setting) Languages() []string { return sortedKeys(s.Texts) }

// SettingExtension adds dependent tables to a setting of another package.
type SettingExtension struct {
	Package PackageID      `json:"package"`
	Setting string         `json:"setting"`
	Tables  []SettingTable `json:"tables"`
	Src     rterr.Location `json:"-"`
}

// Key returns the identity of the setting extension.
func (e *SettingExtension) Key() SettingExtensionKey {
	return SettingExtensionKey{Package: e.Package, Setting: e.Setting}
}

// SettingExtensionKey identifies a setting extension: one package extends
// one setting at most once.
type SettingExtensionKey struct {
	Package PackageID
	Setting string
}

// SettingNode is a table of a setting with the package that added it.
type SettingNode struct {
	SettingTable
	Origin PackageID // owner of the setting or the extending package
	Root   bool
}

func cloneSetting(s *Setting) *Setting {
	c := *s
	c.Texts = make(map[string]SettingText, len(s.Texts))
	for l, t := range s.Texts {
		c.Texts[l] = t
	}
	c.Tables = slices.Clone(s.Tables)
	return &c
}

func cloneSettingExtension(e *SettingExtension) *SettingExtension {
	c := *e
	c.Tables = slices.Clone(e.Tables)
	return &c
}

// SettingIDs returns all setting IDs in sorted order.
func (s *Schema) SettingIDs() []string { return sortedKeys(s.Settings) }

// ExtensionsOfSetting returns the extensions of a setting ordered by
// package ID.
func (s *Schema) ExtensionsOfSetting(id string) []*SettingExtension {
	var out []*SettingExtension
	for k, e := range s.SettingExtensions {
		if k.Setting == id {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b *SettingExtension) int { return cmp.Compare(a.Package, b.Package) })
	return out
}

// SortedSettingExtensions returns all setting extensions ordered by
// setting and package.
func (s *Schema) SortedSettingExtensions() []*SettingExtension {
	out := make([]*SettingExtension, 0, len(s.SettingExtensions))
	for _, e := range s.SettingExtensions {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b *SettingExtension) int {
		return cmp.Or(cmp.Compare(a.Setting, b.Setting), cmp.Compare(a.Package, b.Package))
	})
	return out
}

// SettingNodes merges a setting with all its extensions: the root, the
// dependent tables of the setting in declaration order, then those of each
// extension ordered by package ID.
func (s *Schema) SettingNodes(id string) []SettingNode {
	st, ok := s.Settings[id]
	if !ok {
		return nil
	}
	nodes := []SettingNode{{SettingTable: st.Root, Origin: st.Owner, Root: true}}
	for _, t := range st.Tables {
		nodes = append(nodes, SettingNode{SettingTable: t, Origin: st.Owner})
	}
	for _, e := range s.ExtensionsOfSetting(id) {
		for _, t := range e.Tables {
			nodes = append(nodes, SettingNode{SettingTable: t, Origin: e.Package})
		}
	}
	return nodes
}

// SettingOf returns the setting a table belongs to, or "".
func (s *Schema) SettingOf(table string) string {
	for _, id := range s.SettingIDs() {
		for _, n := range s.SettingNodes(id) {
			if n.Table == table {
				return id
			}
		}
	}
	return ""
}
