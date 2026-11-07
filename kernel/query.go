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
	"strings"

	"github.com/timriep/go-runtime/internal/repo"
	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/rterr"
)

// LogEntry is one entry of the operation log.
type LogEntry = repo.LogEntry

// Log returns the operation log, newest entry first.
func (k *Kernel) Log(ctx context.Context) ([]*LogEntry, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.repo.ReadLog(ctx, k.db)
}

// Packages returns the installed packages ordered by ID.
func (k *Kernel) Packages(ctx context.Context) ([]*model.PackageInfo, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.PackageInfo, 0, len(s.Packages))
	for _, id := range s.PackageIDs() {
		out = append(out, s.Packages[id])
	}
	return out, nil
}

// PackageDetail describes an installed package and its objects.
type PackageDetail struct {
	Package    *model.PackageInfo `json:"package"`
	RequiredBy []model.PackageID  `json:"requiredBy,omitempty"`
	Domains    []*model.Domain    `json:"domains,omitempty"`
	Tables     []string           `json:"tables,omitempty"`
	Extensions []*model.Extension `json:"extensions,omitempty"`
}

// Package returns an installed package with its objects.
func (k *Kernel) Package(ctx context.Context, id model.PackageID) (*PackageDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	info, ok := s.Packages[id]
	if !ok {
		return nil, &rterr.NotInstalled{Package: string(id)}
	}
	d := &PackageDetail{Package: info, RequiredBy: s.Dependents(id)}
	for _, n := range s.DomainNames() {
		if s.Domains[n].Owner == id {
			d.Domains = append(d.Domains, s.Domains[n])
		}
	}
	for _, n := range s.TableNames() {
		if s.Tables[n].Owner == id {
			d.Tables = append(d.Tables, n)
		}
	}
	for _, e := range s.SortedExtensions() {
		if e.Package == id {
			d.Extensions = append(d.Extensions, e)
		}
	}
	return d, nil
}

// TableDetail describes a table with all its physical fields.
type TableDetail struct {
	Table        *model.Table       `json:"table"` // physical layout incl. tenant and extension fields
	Extensions   []*model.Extension `json:"extensions,omitempty"`
	ReferencedBy []string           `json:"referencedBy,omitempty"` // "TABLE.FOREIGN_KEY"
}

// Table returns the physical layout of a table.
func (k *Kernel) Table(ctx context.Context, name string) (*TableDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	p := s.Physical()
	t, ok := p.Tables[name]
	if !ok {
		return nil, &rterr.NotFound{Kind: "table", Name: name}
	}
	d := &TableDetail{Table: t, Extensions: s.ExtensionsOf(name)}
	for _, other := range p.TableNames() {
		for _, fk := range p.Tables[other].ForeignKeys {
			if fk.Target == name {
				d.ReferencedBy = append(d.ReferencedBy, other+"."+fk.Name)
			}
		}
	}
	return d, nil
}

// DomainDetail describes a domain and where it is used.
type DomainDetail struct {
	Domain *model.Domain `json:"domain"`
	UsedBy []string      `json:"usedBy,omitempty"` // "TABLE.FIELD"
}

// Domain returns a domain and the fields that use it.
func (k *Kernel) Domain(ctx context.Context, name string) (*DomainDetail, error) {
	s, err := k.Schema(ctx)
	if err != nil {
		return nil, err
	}
	dom, ok := s.Domains[name]
	if !ok {
		return nil, &rterr.NotFound{Kind: "domain", Name: name}
	}
	d := &DomainDetail{Domain: dom}
	p := s.Physical()
	for _, tn := range p.TableNames() {
		for _, f := range p.Tables[tn].Fields {
			if f.Domain == name {
				d.UsedBy = append(d.UsedBy, tn+"."+f.Name)
			}
		}
	}
	return d, nil
}

// Object kinds understood by Resolve.
const (
	KindPackage = "package"
	KindTable   = "table"
	KindDomain  = "domain"
)

// Resolve determines which kind of object a name refers to. A prefix
// "package:", "table:" or "domain:" selects the kind explicitly; a bare name
// that matches several kinds is reported as rterr.Ambiguous.
func (k *Kernel) Resolve(ctx context.Context, ref string) (kind, name string, err error) {
	if kd, n, ok := strings.Cut(ref, ":"); ok && (kd == KindPackage || kd == KindTable || kd == KindDomain) {
		return kd, n, nil
	}
	s, err := k.Schema(ctx)
	if err != nil {
		return "", "", err
	}
	var kinds []string
	if _, ok := s.Packages[model.PackageID(ref)]; ok {
		kinds = append(kinds, KindPackage)
	}
	if _, ok := s.Tables[ref]; ok {
		kinds = append(kinds, KindTable)
	}
	if _, ok := s.Domains[ref]; ok {
		kinds = append(kinds, KindDomain)
	}
	switch len(kinds) {
	case 0:
		return "", "", &rterr.NotFound{Kind: "object", Name: ref}
	case 1:
		return kinds[0], ref, nil
	}
	return "", "", &rterr.Ambiguous{Name: ref, Kinds: kinds}
}
