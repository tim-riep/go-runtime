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
	"database/sql"
	"log/slog"
	"maps"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// audit logs an event of the role with all authorizations at level warn,
// whatever level the log is set to: it hands the record to the handler of
// the default logger without asking whether the level is enabled. The
// attribute audit=true marks such records.
func audit(ctx context.Context, msg string, args ...any) {
	r := slog.NewRecord(time.Now(), slog.LevelWarn, msg, 0)
	r.Add(args...)
	r.Add("audit", true)
	_ = slog.Default().Handler().Handle(ctx, r)
}

// definedAllRole returns the role with all authorizations as the kernel
// defines it, with the texts of the languages set up.
func (k *Kernel) definedAllRole(ctx context.Context, q dialect.Querier) (*model.Role, error) {
	pkg, err := kernelschema.Package(k.target)
	if err != nil {
		return nil, err
	}
	langs, err := k.repo.LanguageCodes(ctx, q)
	if err != nil {
		return nil, err
	}
	pkg = pkg.WithLanguages(func(l string) bool { return langs[l] })
	for i := range pkg.Roles {
		if pkg.Roles[i].Name == model.AllAuthorizationsRole {
			r := model.CloneRole(&pkg.Roles[i])
			r.Owner, r.Src = model.Kernel, rterr.Location{}
			return r, nil
		}
	}
	return nil, nil
}

// ensureAllAuthorizationsRole checks that the role with all authorizations
// is stored as the kernel defines it. A missing or changed role, e.g. by
// direct access to the database, is restored; its assignments stay. The
// restoration is recorded in the history and the audit log.
func (k *Kernel) ensureAllAuthorizationsRole(ctx context.Context) error {
	if !k.repo.HasRoles() {
		return nil
	}
	if v, err := k.repo.KernelVersion(ctx, k.db); err != nil || v != k.target {
		return err
	}
	var restored bool
	err := k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		want, err := k.definedAllRole(ctx, tx)
		if err != nil || want == nil {
			return err
		}
		got, err := k.repo.Role(ctx, tx, want.Name)
		if err != nil {
			return err
		}
		if got != nil && got.Owner == want.Owner && !got.Composite && got.File == want.File && len(got.Authorizations) == 0 && len(got.Roles) == 0 &&
			maps.Equal(got.Texts, want.Texts) {
			return nil
		}
		if err := k.repo.ReplaceRole(ctx, tx, want); err != nil {
			return err
		}
		at := k.timestamp()
		c := RoleChange{ChangedAt: at, ChangedBy: operationBy, Kind: ChangeAllRestored, Role: model.Some(want.Name), NewState: roleState(nil, want)}
		if got != nil {
			c.OldState = roleState(nil, got)
		}
		if err := k.repo.InsertRoleChange(ctx, tx, c); err != nil {
			return err
		}
		restored = true
		return k.repo.NextAuthGeneration(ctx, tx, at)
	})
	if err == nil && restored {
		audit(ctx, "role with all authorizations restored to its defined form", "role", model.AllAuthorizationsRole)
	}
	return err
}

// AllAuthorizationsHolder is an assignment of the role with all
// authorizations.
type AllAuthorizationsHolder struct {
	User       string                    `json:"user"`
	Status     string                    `json:"status"`
	Tenant     string                    `json:"tenant"`
	Assignment string                    `json:"assignment"`
	ValidFrom  model.Optional[time.Time] `json:"validFrom"`
	ValidTo    model.Optional[time.Time] `json:"validTo"`
	// Effective tells that the user is active and the assignment valid
	// today.
	Effective bool `json:"effective"`
}

// AllAuthorizationsHolders lists the assignments of the role with all
// authorizations in all tenants.
func (k *Kernel) AllAuthorizationsHolders(ctx context.Context) ([]AllAuthorizationsHolder, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.allHolders(ctx, k.db)
}

func (k *Kernel) allHolders(ctx context.Context, q dialect.Querier) ([]AllAuthorizationsHolder, error) {
	list, err := k.repo.RoleAssignments(ctx, q, repo.AssignmentFilter{Role: model.AllAuthorizationsRole})
	if err != nil {
		return nil, err
	}
	today := k.now().UTC()
	out := []AllAuthorizationsHolder{}
	for _, a := range list {
		u, err := k.user(ctx, q, a.User)
		if err != nil {
			return nil, err
		}
		out = append(out, AllAuthorizationsHolder{User: a.User, Status: u.Status, Tenant: a.Tenant, Assignment: a.ID, ValidFrom: a.ValidFrom, ValidTo: a.ValidTo,
			Effective: u.Status != "locked" && validOn(a, time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC))})
	}
	return out, nil
}

// keepAllHolder fails without confirmation when a change takes away the
// last effective assignment of the role with all authorizations, after
// which nobody can administer everything. lost reports the holders the
// change takes away.
func (k *Kernel) keepAllHolder(ctx context.Context, q dialect.Querier, opts RoleOptions, what string, lost func(h AllAuthorizationsHolder) bool) error {
	if opts.Confirm || !k.repo.HasRoles() {
		return nil
	}
	holders, err := k.allHolders(ctx, q)
	if err != nil {
		return err
	}
	before, after := 0, 0
	for _, h := range holders {
		if h.Effective {
			before++
			if !lost(h) {
				after++
			}
		}
	}
	if before > 0 && after == 0 {
		return &rterr.ConfirmationRequired{Operation: what, Object: "the last user with " + model.AllAuthorizationsRole + "; nobody can administer everything afterwards"}
	}
	return nil
}
