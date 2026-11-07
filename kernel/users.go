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
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/auth"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// User is a user of http services.
type User = repo.User

// APIToken is an API token of a user, without its secret.
type APIToken = repo.APIToken

// Users manages the users of a kernel.
type Users struct{ k *Kernel }

// Users returns the user operations.
func (k *Kernel) Users() *Users { return &Users{k: k} }

var userName = regexp.MustCompile(`^[a-z0-9][a-z0-9._@-]{0,63}$`)

// UserOptions are the defaults of a new user.
type UserOptions struct {
	// Tenant is the default tenant; the user is assigned to it.
	Tenant   model.Optional[string]
	Language model.Optional[string]
}

// Create adds an active user with a password.
func (u *Users) Create(ctx context.Context, name, password string, opts UserOptions) (*User, error) {
	if !userName.MatchString(name) {
		return nil, &rterr.InvalidArgument{Argument: "user", Message: "a user name has 1 to 64 lower-case letters, digits, '.', '_', '@' or '-' and starts with a letter or digit"}
	}
	if password == "" {
		return nil, &rterr.InvalidArgument{Argument: "password", Message: "the password must not be empty"}
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	k := u.k
	user := &User{Name: name, Status: "active", Language: opts.Language, DefaultTenant: opts.Tenant, PasswordHash: model.Some(hash), CreatedAt: k.timestamp()}
	err = k.userTx(ctx, func(q dialect.Querier) error {
		if existing, err := k.repo.Users(ctx, q, name); err != nil || len(existing) > 0 {
			if err == nil {
				err = &rterr.InvalidArgument{Argument: "user", Message: "user " + name + " exists already"}
			}
			return err
		}
		if err := k.repo.InsertUser(ctx, q, user); err != nil {
			return err
		}
		if t, ok := opts.Tenant.Get(); ok {
			user.Tenants = []string{t}
			return k.repo.AddUserTenant(ctx, q, name, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (k *Kernel) userTx(ctx context.Context, fn func(q dialect.Querier) error) error {
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error { return fn(tx) })
}

// List returns all users ordered by name.
func (u *Users) List(ctx context.Context) ([]*User, error) {
	if err := u.k.requireInit(ctx, u.k.db); err != nil {
		return nil, err
	}
	return u.k.repo.Users(ctx, u.k.db, "")
}

// Get returns a user.
func (u *Users) Get(ctx context.Context, name string) (*User, error) {
	if err := u.k.requireInit(ctx, u.k.db); err != nil {
		return nil, err
	}
	return u.k.user(ctx, u.k.db, name)
}

func (k *Kernel) user(ctx context.Context, q dialect.Querier, name string) (*User, error) {
	list, err := k.repo.Users(ctx, q, name)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, &rterr.NotFound{Kind: "user", Name: name}
	}
	return list[0], nil
}

// change updates a user in a transaction.
func (u *Users) change(ctx context.Context, name string, fn func(user *User) error) error {
	k := u.k
	return k.userTx(ctx, func(q dialect.Querier) error {
		user, err := k.user(ctx, q, name)
		if err != nil {
			return err
		}
		if err := fn(user); err != nil {
			return err
		}
		return k.repo.UpdateUser(ctx, q, user)
	})
}

// Lock locks a user: it cannot sign in any more.
func (u *Users) Lock(ctx context.Context, name string) error {
	return u.change(ctx, name, func(user *User) error {
		user.Status, user.LockedAt = "locked", model.Some(u.k.timestamp())
		return nil
	})
}

// Unlock unlocks a user and resets its failed logins.
func (u *Users) Unlock(ctx context.Context, name string) error {
	return u.change(ctx, name, func(user *User) error {
		user.Status, user.LockedAt, user.FailedLogins = "active", model.None[time.Time](), 0
		return nil
	})
}

// SetPassword replaces the password of a user.
func (u *Users) SetPassword(ctx context.Context, name, password string) error {
	if password == "" {
		return &rterr.InvalidArgument{Argument: "password", Message: "the password must not be empty"}
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return u.change(ctx, name, func(user *User) error {
		user.PasswordHash = model.Some(hash)
		return nil
	})
}

// AddTenant lets a user work in a tenant.
func (u *Users) AddTenant(ctx context.Context, name, tenant string) error {
	k := u.k
	return k.userTx(ctx, func(q dialect.Querier) error {
		user, err := k.user(ctx, q, name)
		if err != nil || slices.Contains(user.Tenants, tenant) {
			return err
		}
		return k.repo.AddUserTenant(ctx, q, name, tenant)
	})
}

// RemoveTenant takes a tenant from a user; if it was the default tenant,
// the user has none afterwards.
func (u *Users) RemoveTenant(ctx context.Context, name, tenant string) error {
	k := u.k
	return k.userTx(ctx, func(q dialect.Querier) error {
		user, err := k.user(ctx, q, name)
		if err != nil {
			return err
		}
		if !slices.Contains(user.Tenants, tenant) {
			return &rterr.NotFound{Kind: "tenant of user " + name, Name: tenant}
		}
		if user.DefaultTenant.Or("") == tenant {
			user.DefaultTenant = model.None[string]()
			if err := k.repo.UpdateUser(ctx, q, user); err != nil {
				return err
			}
		}
		return k.repo.RemoveUserTenant(ctx, q, name, tenant)
	})
}

// Tokens manages the API tokens of users.
type Tokens struct{ k *Kernel }

// Tokens returns the token operations.
func (k *Kernel) Tokens() *Tokens { return &Tokens{k: k} }

// Create creates an API token for a user. The token is returned only
// here; the kernel keeps a hash of it.
func (t *Tokens) Create(ctx context.Context, user, label string, expires model.Optional[time.Time]) (string, *APIToken, error) {
	if label == "" || len(label) > 80 {
		return "", nil, &rterr.InvalidArgument{Argument: "label", Message: "a token needs a label of 1 to 80 characters"}
	}
	k := t.k
	id := repo.NewID()
	token, hash, err := auth.NewToken(id)
	if err != nil {
		return "", nil, err
	}
	info := &APIToken{ID: id, User: user, Label: label, ExpiresAt: expires, Hash: hash, CreatedAt: k.timestamp()}
	err = k.userTx(ctx, func(q dialect.Querier) error {
		if _, err := k.user(ctx, q, user); err != nil {
			return err
		}
		return k.repo.InsertToken(ctx, q, info)
	})
	if err != nil {
		return "", nil, err
	}
	return token, info, nil
}

// List returns the tokens of a user, or of all users for "".
func (t *Tokens) List(ctx context.Context, user string) ([]*APIToken, error) {
	if err := t.k.requireInit(ctx, t.k.db); err != nil {
		return nil, err
	}
	return t.k.repo.Tokens(ctx, t.k.db, user)
}

// Revoke deletes a token.
func (t *Tokens) Revoke(ctx context.Context, id string) error {
	k := t.k
	return k.userTx(ctx, func(q dialect.Querier) error {
		tok, err := k.repo.Token(ctx, q, id)
		if err != nil {
			return err
		}
		if tok == nil {
			return &rterr.NotFound{Kind: "token", Name: id}
		}
		return k.repo.DeleteToken(ctx, q, id)
	})
}

// authStore gives the authentication methods access to users and tokens.
type authStore struct{ k *Kernel }

func (s authStore) Now() time.Time { return s.k.now().UTC() }

func (s authStore) Account(ctx context.Context, name string) (*auth.Account, error) {
	list, err := s.k.repo.Users(ctx, s.k.db, name)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	u := list[0]
	return &auth.Account{User: auth.User{Name: u.Name, Language: u.Language, DefaultTenant: u.DefaultTenant, Tenants: u.Tenants},
		Locked: u.Status == "locked", PasswordHash: u.PasswordHash}, nil
}

func (s authStore) Token(ctx context.Context, id string) (*auth.Token, error) {
	t, err := s.k.repo.Token(ctx, s.k.db, id)
	if err != nil || t == nil {
		return nil, err
	}
	return &auth.Token{ID: t.ID, User: t.User, Hash: t.Hash, ExpiresAt: t.ExpiresAt}, nil
}

func (s authStore) Failed(ctx context.Context, name string) error {
	k := s.k
	return k.userTx(ctx, func(q dialect.Querier) error {
		u, err := k.user(ctx, q, name)
		if err != nil {
			return err
		}
		u.FailedLogins++
		slog.Warn("failed login", "user", name, "failed", u.FailedLogins)
		if u.FailedLogins >= k.maxFailedLogins {
			u.Status, u.LockedAt = "locked", model.Some(k.timestamp())
			slog.Warn("user locked after failed logins", "user", name, "failed", u.FailedLogins)
		}
		return k.repo.UpdateUser(ctx, q, u)
	})
}

func (s authStore) Succeeded(ctx context.Context, name string) error {
	k := s.k
	return k.userTx(ctx, func(q dialect.Querier) error {
		u, err := k.user(ctx, q, name)
		if err != nil || u.FailedLogins == 0 {
			return err
		}
		u.FailedLogins = 0
		return k.repo.UpdateUser(ctx, q, u)
	})
}

// Principal is a user signed in to a request.
type Principal = auth.User

// Authenticate signs in a request with the first of the allowed methods
// whose credentials it carries. Without credentials it returns nil;
// invalid credentials return auth.ErrInvalid.
func (k *Kernel) Authenticate(ctx context.Context, r *http.Request, allowed []string) (*Principal, error) {
	return auth.Authenticate(ctx, r, allowed, authStore{k: k})
}
