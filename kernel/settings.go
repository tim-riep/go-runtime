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

package kernel

import (
	"context"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/setting"
)

// SettingSummary is an installed setting in a list.
type SettingSummary struct {
	ID             string            `json:"id"`
	Owner          model.PackageID   `json:"owner"`
	Label          string            `json:"label"` // English label
	Root           string            `json:"root"`
	Tables         int               `json:"tables"`
	DataClass      model.DataClass   `json:"dataClass"`
	TenantSpecific bool              `json:"tenantSpecific"`
	Extensions     []model.PackageID `json:"extensions,omitempty"`
}

func (k *Kernel) languageCodes(ctx context.Context) ([]string, error) {
	langs, err := k.repo.Languages(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(langs))
	for i, l := range langs {
		out[i] = l.Code
	}
	return out, nil
}

// Setting returns the resolved definition of a setting with all installed
// extensions and texts in all languages set up in the database.
func (k *Kernel) Setting(ctx context.Context, id string) (*setting.Definition, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	langs, err := k.languageCodes(ctx)
	if err != nil {
		return nil, err
	}
	return setting.Resolve(s, langs, id)
}

// Settings lists the installed settings ordered by ID.
func (k *Kernel) Settings(ctx context.Context) ([]SettingSummary, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	out := []SettingSummary{}
	for _, id := range s.SettingIDs() {
		st := s.Settings[id]
		sum := SettingSummary{ID: id, Owner: st.Owner, Label: st.Texts[model.DefaultLanguage].Label, Root: st.Root.Table,
			Tables: len(s.SettingNodes(id))}
		if root := s.Tables[st.Root.Table]; root != nil {
			sum.DataClass, sum.TenantSpecific = root.DataClass, root.TenantSpecific
		}
		for _, e := range s.ExtensionsOfSetting(id) {
			sum.Extensions = append(sum.Extensions, e.Package)
		}
		out = append(out, sum)
	}
	return out, nil
}
