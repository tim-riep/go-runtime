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

package repo

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/model"
)

// Tables of users.
const (
	TUsers       = model.KernelNamespace + "USERS"
	TUserTenants = model.KernelNamespace + "USER_TENANTS"
	TAPITokens   = model.KernelNamespace + "API_TOKENS"
)

// User is a stored user.
type User struct {
	Name          string                    `json:"name"`
	Status        string                    `json:"status"` // active or locked
	Language      model.Optional[string]    `json:"language"`
	DefaultTenant model.Optional[string]    `json:"defaultTenant"`
	PasswordHash  model.Optional[string]    `json:"-"`
	FailedLogins  int                       `json:"failedLogins"`
	LockedAt      model.Optional[time.Time] `json:"lockedAt"`
	CreatedAt     time.Time                 `json:"createdAt"`
	Tenants       []string                  `json:"tenants"`
}

// APIToken is a stored API token.
type APIToken struct {
	ID        string                    `json:"id"`
	User      string                    `json:"user"`
	Label     string                    `json:"label"`
	ExpiresAt model.Optional[time.Time] `json:"expiresAt"`
	Hash      string                    `json:"-"`
	CreatedAt time.Time                 `json:"createdAt"`
}

func optTime(v any) model.Optional[time.Time] {
	if t, ok := v.(time.Time); ok {
		return model.Some(t)
	}
	return model.None[time.Time]()
}

// Users returns the users ordered by name, or the user with the given
// name if name is not empty.
func (r *Repo) Users(ctx context.Context, q dialect.Querier, name string) ([]*User, error) {
	var filter row
	if name != "" {
		filter = row{"USER_NAME": name}
	}
	rows, err := r.selectWhere(ctx, q, TUsers, []string{"USER_NAME", "STATUS", "LANGUAGE", "DEFAULT_TENANT", "PASSWORD_HASH", "FAILED_LOGINS",
		"LOCKED_AT", "CREATED_AT"}, filter, "USER_NAME")
	if err != nil {
		return nil, err
	}
	var out []*User
	byName := map[string]*User{}
	for _, u := range rows {
		x := &User{Name: str(u["USER_NAME"]), Status: str(u["STATUS"]), Language: opt(u["LANGUAGE"]), DefaultTenant: opt(u["DEFAULT_TENANT"]),
			PasswordHash: opt(u["PASSWORD_HASH"]), FailedLogins: num(u["FAILED_LOGINS"]), LockedAt: optTime(u["LOCKED_AT"]), CreatedAt: tm(u["CREATED_AT"]),
			Tenants: []string{}}
		out = append(out, x)
		byName[x.Name] = x
	}
	tenants, err := r.selectWhere(ctx, q, TUserTenants, []string{"USER_NAME", "TENANT"}, filter, "USER_NAME", "TENANT")
	if err != nil {
		return nil, err
	}
	for _, t := range tenants {
		if u := byName[str(t["USER_NAME"])]; u != nil {
			u.Tenants = append(u.Tenants, str(t["TENANT"]))
		}
	}
	return out, nil
}

// InsertUser stores a new user.
func (r *Repo) InsertUser(ctx context.Context, q dialect.Querier, u *User) error {
	return r.insert(ctx, q, TUsers, row{"USER_NAME": u.Name, "STATUS": u.Status, "LANGUAGE": val(u.Language), "DEFAULT_TENANT": val(u.DefaultTenant),
		"PASSWORD_HASH": val(u.PasswordHash), "FAILED_LOGINS": int64(u.FailedLogins), "LOCKED_AT": optValue(u.LockedAt), "CREATED_AT": u.CreatedAt})
}

// UpdateUser stores the status, password, failed logins and defaults of a
// user.
func (r *Repo) UpdateUser(ctx context.Context, q dialect.Querier, u *User) error {
	return r.update(ctx, q, TUsers, row{"USER_NAME": u.Name}, row{"STATUS": u.Status, "LANGUAGE": val(u.Language), "DEFAULT_TENANT": val(u.DefaultTenant),
		"PASSWORD_HASH": val(u.PasswordHash), "FAILED_LOGINS": int64(u.FailedLogins), "LOCKED_AT": optValue(u.LockedAt)})
}

// AddUserTenant allows a user to work in a tenant.
func (r *Repo) AddUserTenant(ctx context.Context, q dialect.Querier, user, tenant string) error {
	return r.insert(ctx, q, TUserTenants, row{"USER_NAME": user, "TENANT": tenant})
}

// RemoveUserTenant takes a tenant from a user.
func (r *Repo) RemoveUserTenant(ctx context.Context, q dialect.Querier, user, tenant string) error {
	return r.deleteWhere(ctx, q, TUserTenants, row{"USER_NAME": user, "TENANT": tenant})
}

// ForgetTenant removes a tenant from all users, before it is deleted.
func (r *Repo) ForgetTenant(ctx context.Context, q dialect.Querier, tenant string) error {
	if !r.has(TUsers, "") {
		return nil
	}
	if err := r.deleteWhere(ctx, q, TUserTenants, row{"TENANT": tenant}); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "UPDATE "+r.d.QuoteIdent(TUsers)+" SET "+r.d.QuoteIdent("DEFAULT_TENANT")+" = NULL WHERE "+
		r.d.QuoteIdent("DEFAULT_TENANT")+" = "+r.d.Placeholder(1), tenant)
	return err
}

// Tokens returns the API tokens of a user, or of all users for "".
func (r *Repo) Tokens(ctx context.Context, q dialect.Querier, user string) ([]*APIToken, error) {
	var filter row
	if user != "" {
		filter = row{"USER_NAME": user}
	}
	rows, err := r.selectWhere(ctx, q, TAPITokens, []string{"TOKEN_ID", "USER_NAME", "LABEL", "EXPIRES_AT", "TOKEN_HASH", "CREATED_AT"}, filter, "USER_NAME", "CREATED_AT")
	if err != nil {
		return nil, err
	}
	var out []*APIToken
	for _, t := range rows {
		out = append(out, &APIToken{ID: str(t["TOKEN_ID"]), User: str(t["USER_NAME"]), Label: str(t["LABEL"]), ExpiresAt: optTime(t["EXPIRES_AT"]),
			Hash: str(t["TOKEN_HASH"]), CreatedAt: tm(t["CREATED_AT"])})
	}
	return out, nil
}

// Token returns an API token by ID, or nil.
func (r *Repo) Token(ctx context.Context, q dialect.Querier, id string) (*APIToken, error) {
	rows, err := r.selectWhere(ctx, q, TAPITokens, []string{"TOKEN_ID", "USER_NAME", "LABEL", "EXPIRES_AT", "TOKEN_HASH", "CREATED_AT"}, row{"TOKEN_ID": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	t := rows[0]
	return &APIToken{ID: str(t["TOKEN_ID"]), User: str(t["USER_NAME"]), Label: str(t["LABEL"]), ExpiresAt: optTime(t["EXPIRES_AT"]),
		Hash: str(t["TOKEN_HASH"]), CreatedAt: tm(t["CREATED_AT"])}, nil
}

// InsertToken stores an API token.
func (r *Repo) InsertToken(ctx context.Context, q dialect.Querier, t *APIToken) error {
	return r.insert(ctx, q, TAPITokens, row{"TOKEN_ID": t.ID, "USER_NAME": t.User, "LABEL": t.Label, "EXPIRES_AT": optValue(t.ExpiresAt),
		"TOKEN_HASH": t.Hash, "CREATED_AT": t.CreatedAt})
}

// DeleteToken removes an API token.
func (r *Repo) DeleteToken(ctx context.Context, q dialect.Querier, id string) error {
	return r.deleteWhere(ctx, q, TAPITokens, row{"TOKEN_ID": id})
}

// encodeConds writes "COL = $n" for the columns of a row, in sorted order,
// starting at placeholder start.
func (r *Repo) encodeConds(table string, values row, start int, sep string) (string, []any, error) {
	cols := slices.Sorted(maps.Keys(values))
	var parts []string
	var args []any
	for _, c := range cols {
		d, err := r.domain(table, c)
		if err != nil {
			return "", nil, err
		}
		v, err := r.d.Encode(d, values[c])
		if err != nil {
			return "", nil, fmt.Errorf("%s.%s: %w", table, c, err)
		}
		args = append(args, v)
		parts = append(parts, r.d.QuoteIdent(c)+" = "+r.d.Placeholder(start+len(args)))
	}
	return strings.Join(parts, sep), args, nil
}

// update sets columns of the rows that match key.
func (r *Repo) update(ctx context.Context, q dialect.Querier, table string, key, values row) error {
	set, args, err := r.encodeConds(table, values, 0, ", ")
	if err != nil {
		return err
	}
	where, wargs, err := r.encodeConds(table, key, len(args), " AND ")
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "UPDATE "+r.d.QuoteIdent(table)+" SET "+set+" WHERE "+where, append(args, wargs...)...); err != nil {
		return fmt.Errorf("writing %s: %w", table, r.d.ClassifyError(err))
	}
	return nil
}

// deleteWhere deletes the rows that match key.
func (r *Repo) deleteWhere(ctx context.Context, q dialect.Querier, table string, key row) error {
	where, args, err := r.encodeConds(table, key, 0, " AND ")
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM "+r.d.QuoteIdent(table)+" WHERE "+where, args...); err != nil {
		return fmt.Errorf("writing %s: %w", table, r.d.ClassifyError(err))
	}
	return nil
}
