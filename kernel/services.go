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

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
)

// ServiceDetail describes a service with its activation.
type ServiceDetail struct {
	*reflector.Service
	// Active tells whether the kernel serves the service; services are
	// inactive after installation.
	Active    bool                   `json:"active"`
	ChangedBy model.Optional[string] `json:"changedBy"`
}

// Service returns the description of a service with its activation, or
// rterr.NotFound.
func (k *Kernel) Service(ctx context.Context, name string) (*ServiceDetail, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	svc, err := r.Service(name)
	if err != nil {
		return nil, err
	}
	states, err := k.repo.ServiceStates(ctx, k.db)
	if err != nil {
		return nil, err
	}
	st := states[name]
	return &ServiceDetail{Service: svc, Active: st.Active, ChangedBy: st.ChangedBy}, nil
}

// Services returns the services of a package, or of all for "", with
// their activation, ordered by name.
func (k *Kernel) Services(ctx context.Context, pkg model.PackageID) ([]*ServiceDetail, error) {
	r, err := k.Reflector(ctx)
	if err != nil {
		return nil, err
	}
	if pkg != "" {
		if s, _ := k.Schema(ctx); s != nil && s.Packages[pkg] == nil {
			return nil, &rterr.NotInstalled{Package: string(pkg)}
		}
	}
	states, err := k.repo.ServiceStates(ctx, k.db)
	if err != nil {
		return nil, err
	}
	out := []*ServiceDetail{}
	for _, svc := range r.Services(pkg) {
		st := states[svc.Name]
		out = append(out, &ServiceDetail{Service: svc, Active: st.Active, ChangedBy: st.ChangedBy})
	}
	return out, nil
}

// ActivateService lets the kernel serve a service. by names who changes
// it.
func (k *Kernel) ActivateService(ctx context.Context, name, by string) error {
	return k.changeServiceState(ctx, name, true, by)
}

// DeactivateService stops serving a service; its path answers 404.
func (k *Kernel) DeactivateService(ctx context.Context, name, by string) error {
	return k.changeServiceState(ctx, name, false, by)
}

func (k *Kernel) changeServiceState(ctx context.Context, name string, active bool, by string) error {
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	return k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		svc := s.Services[name]
		if svc == nil {
			return &rterr.NotFound{Kind: "service", Name: name}
		}
		states, err := k.repo.ServiceStates(ctx, tx)
		if err != nil {
			return err
		}
		if old, ok := states[name]; ok && old.Active == active {
			return nil
		}
		now := k.timestamp()
		states[name] = repo.ServiceState{Name: name, Package: svc.Owner, Active: active, ChangedAt: now, ChangedBy: optionalUser(by)}
		if err := k.repo.ReplaceServiceStates(ctx, tx, states); err != nil {
			return err
		}
		return k.repo.NextHTTPGeneration(ctx, tx, now)
	})
}

// syncServiceStates removes the activation of services that are gone
// after an operation; services that stay keep theirs across upgrades.
func (k *Kernel) syncServiceStates(ctx context.Context, q dialect.Querier, want *model.Schema) error {
	if !k.repo.HasServices() {
		return nil
	}
	states, err := k.repo.ServiceStates(ctx, q)
	if err != nil {
		return err
	}
	changed := false
	for name := range states {
		if want.Services[name] == nil {
			delete(states, name)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := k.repo.ReplaceServiceStates(ctx, q, states); err != nil {
		return err
	}
	return k.repo.NextHTTPGeneration(ctx, q, k.timestamp())
}

func optionalUser(by string) model.Optional[string] {
	if by == "" {
		return model.None[string]()
	}
	return model.Some(by)
}
