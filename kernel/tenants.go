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
	"fmt"
	"slices"

	"github.com/timriep/go-runtime/dialect"
	"github.com/timriep/go-runtime/internal/repo"
	"github.com/timriep/go-runtime/model"
	"github.com/timriep/go-runtime/plan"
	"github.com/timriep/go-runtime/rterr"
)

// Tenant is a tenant of the database.
type Tenant = repo.Tenant

// Tenants manages the tenants of a kernel.
type Tenants struct{ k *Kernel }

// Tenants returns the tenant operations.
func (k *Kernel) Tenants() *Tenants { return &Tenants{k: k} }

func checkTenantID(id string) error {
	if !model.ValidTenantID(id) {
		return &rterr.InvalidArgument{Argument: "tenant", Message: fmt.Sprintf("%q must consist of up to %d upper-case letters, digits and underscores and start with a letter or digit", id, model.TenantIDLength)}
	}
	return nil
}

// Create adds a tenant.
func (t *Tenants) Create(ctx context.Context, id, description string) (*Tenant, error) {
	if err := checkTenantID(id); err != nil {
		return nil, err
	}
	k := t.k
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	ten := Tenant{ID: id, Description: description, CreatedAt: k.timestamp()}
	if err := k.repo.InsertTenant(ctx, k.db, ten); err != nil {
		return nil, err
	}
	return &ten, nil
}

// List returns all tenants.
func (t *Tenants) List(ctx context.Context) ([]Tenant, error) {
	if err := t.k.requireInit(ctx, t.k.db); err != nil {
		return nil, err
	}
	return t.k.repo.Tenants(ctx, t.k.db)
}

// Delete removes a tenant together with its rows in all tenant-specific
// tables, which are emptied in dependency order. It needs Options.Confirm
// unless it is a dry run. Rows of tables that are not tenant-specific but
// reference the tenant are never deleted; they make the deletion fail.
func (t *Tenants) Delete(ctx context.Context, id string, opts Options) (*plan.Plan, error) {
	if err := checkTenantID(id); err != nil {
		return nil, err
	}
	k := t.k
	var result *plan.Plan
	started := k.timestamp()
	run := func(q dialect.Querier, execute bool) error {
		if err := k.requireInit(ctx, q); err != nil {
			return err
		}
		s, err := k.repo.Load(ctx, q)
		if err != nil {
			return err
		}
		p, err := k.tenantDeletePlan(ctx, q, s.Physical(), id)
		if err != nil {
			return err
		}
		result = p
		if !execute {
			return nil
		}
		if !opts.Confirm {
			return &rterr.ConfirmationRequired{Operation: "deletion", Object: "tenant " + id}
		}
		if err := k.execute(ctx, q, p); err != nil {
			return err
		}
		return k.writeLog(ctx, q, p, started, repo.StatusSuccess, "")
	}
	if opts.DryRun {
		if err := run(k.db, false); err != nil {
			return nil, err
		}
		return result, nil
	}
	if err := k.d.Transact(ctx, k.db, dialect.TxOptions{DeferForeignKeys: true}, func(tx *sql.Tx) error { return run(tx, true) }); err != nil {
		return nil, err
	}
	result.Executed = true
	return result, nil
}

func (k *Kernel) tenantDeletePlan(ctx context.Context, q dialect.Querier, phys *model.Physical, id string) (*plan.Plan, error) {
	tenantDom := phys.Domains[model.TenantDomain]
	lit, err := k.d.Literal(tenantDom, id)
	if err != nil {
		return nil, err
	}
	keyCol := k.d.QuoteIdent(model.TenantTableKey)
	countWhere := func(table, cond string) (int, error) {
		var n int
		err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+k.d.QuoteIdent(table)+" WHERE "+cond).Scan(&n)
		return n, err
	}
	n, err := countWhere(model.TenantTable, keyCol+" = "+lit)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, &rterr.NotFound{Kind: "tenant", Name: id}
	}

	// Tables that are not tenant-specific may reference the tenant table,
	// e.g. through an extension field. Their rows are not tenant data.
	referenced := &rterr.TenantReferenced{Tenant: id}
	var tenantTables []string
	for _, name := range phys.TableNames() {
		tab := phys.Tables[name]
		if tab.TenantSpecific {
			tenantTables = append(tenantTables, name)
			continue
		}
		for _, fk := range tab.ForeignKeys {
			if fk.Target != model.TenantTable {
				continue
			}
			n, err := countWhere(name, k.d.QuoteIdent(fk.Columns[0].Field)+" = "+lit)
			if err != nil {
				return nil, err
			}
			if n > 0 && !slices.Contains(referenced.Tables, name) {
				referenced.Tables = append(referenced.Tables, name)
			}
		}
	}
	if len(referenced.Tables) > 0 {
		return nil, referenced
	}

	p := &plan.Plan{Operation: plan.TenantDelete, Dialect: k.d.Name(), Target: id}
	// Referencing tables first: the reverse of the creation order.
	order, _ := dialect.CreationOrder(phys, tenantTables)
	slices.Reverse(order)
	tenantCol := k.d.QuoteIdent(model.TenantField)
	for _, name := range order {
		n, err := countWhere(name, tenantCol+" = "+lit)
		if err != nil {
			return nil, err
		}
		ch := plan.Change{Kind: plan.DeleteRows, Table: name, Destructive: true, Detail: fmt.Sprintf("%d rows", n)}
		p.Changes = append(p.Changes, ch)
		p.Steps = append(p.Steps, plan.Step{Description: ch.String(), Destructive: true,
			Statements: []string{"DELETE FROM " + k.d.QuoteIdent(name) + " WHERE " + tenantCol + " = " + lit}})
	}
	ch := plan.Change{Kind: plan.DeleteRows, Table: model.TenantTable, Destructive: true, Detail: "tenant " + id}
	p.Changes = append(p.Changes, ch)
	p.Steps = append(p.Steps, plan.Step{Description: ch.String(), Destructive: true,
		Statements: []string{"DELETE FROM " + k.d.QuoteIdent(model.TenantTable) + " WHERE " + keyCol + " = " + lit}})
	return p, nil
}
