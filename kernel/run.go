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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/httpsvc"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// EndMode decides what happens to the default session at the end of an
// execution.
type EndMode int

// End modes.
const (
	// CommitOnSuccess commits the default session unless an exception
	// ended the execution, and rolls it back otherwise.
	CommitOnSuccess EndMode = iota
	// RollbackUnlessCommitted rolls back whatever the code did not commit.
	RollbackUnlessCommitted
)

// ExecOptions control an execution of code.
type ExecOptions struct {
	// Tenant is the tenant whose rows the code reads and writes; it must
	// exist.
	Tenant string
	// Language is the language of texts for the execution.
	Language string
	// User is the signed-in user, for requests of http services.
	User model.Optional[string]
	// Rollback, when set, decides at the end of an execution without
	// exception whether the default session is rolled back instead of
	// committed: http services roll back responses with a status of 400
	// and above.
	Rollback func() bool
	End      EndMode
	// MaxSessions limits the sessions of an execution including the
	// default session; 0 means 4.
	MaxSessions int
	// LockTimeout is how long a statement waits for a lock; 0 means 2
	// seconds.
	LockTimeout time.Duration
	// MaxRows limits the rows a select reads into an array; 0 means 10 000.
	MaxRows int
	// Timeout limits the time of the execution, also in native methods;
	// 0 means 60 seconds.
	Timeout time.Duration
	// MaxNativeInput is the largest input of a native method in bytes; 0
	// means 16 MiB.
	MaxNativeInput int64
	// VM are the limits and the log of the VM; its Database is set by the
	// kernel.
	VM vm.Options
}

func (o *ExecOptions) defaults() {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 4
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = 2 * time.Second
	}
	if o.MaxRows <= 0 {
		o.MaxRows = 10_000
	}
	if o.Timeout <= 0 {
		o.Timeout = 60 * time.Second
	}
	if o.MaxNativeInput <= 0 {
		o.MaxNativeInput = 16 << 20
	}
}

// Run executes a public static method of an installed package with
// arguments in JSON and returns its result in JSON.
func (k *Kernel) Run(ctx context.Context, opts ExecOptions, class, method string, args []json.RawMessage) ([]byte, error) {
	var out []byte
	err := k.Execute(ctx, opts, func(ctx context.Context, m *vm.Machine) error {
		var err error
		out, err = m.CallJSON(ctx, class, method, args)
		return err
	})
	return out, err
}

// Execute prepares a machine with the code of all installed packages and
// the database access for a tenant, and runs fn on it with a context that
// ends with the time limit of the execution. At the end it commits or
// rolls back the default session as opts.End says and rolls back the
// other sessions.
func (k *Kernel) Execute(ctx context.Context, opts ExecOptions, fn func(ctx context.Context, m *vm.Machine) error) (err error) {
	opts.defaults()
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if err := k.requireInit(ctx, k.db); err != nil {
		return err
	}
	tenants, err := k.Tenants().List(ctx)
	if err != nil {
		return err
	}
	// Anonymous requests of http services may run without tenant.
	if opts.Tenant != "" && !slices.ContainsFunc(tenants, func(t Tenant) bool { return t.ID == opts.Tenant }) {
		return &rterr.NotFound{Kind: "tenant", Name: opts.Tenant}
	}
	g, err := k.generation(ctx)
	if err != nil {
		return err
	}
	return k.executeWith(ctx, g, opts, fn)
}

// executeWith runs fn on a machine with the code of generation g.
func (k *Kernel) executeWith(ctx context.Context, g *generation, opts ExecOptions, fn func(ctx context.Context, m *vm.Machine) error) (err error) {
	opts.defaults()
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	image := g.image
	x := &execution{k: k, d: k.d, phys: g.phys, schema: g.schema, sheets: g.sheets, ctx: ctx, tenant: opts.Tenant, opts: opts}
	x.sessions = []*session{{x: x, n: 1}}
	x.extensions = g.extensionRegistry()
	defer func() { err = errors.Join(err, x.finish(err)) }()
	vo := opts.VM
	vo.Database = x
	vo.MaxRows = opts.MaxRows
	vo.MaxNativeInput = opts.MaxNativeInput
	vo.Host = x
	vo.Limits = k.formats.OrDefault()
	vo.Transformer = x
	m, err := vm.New(image, vo)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.ReleaseHandles()) }()
	return fn(ctx, m)
}

// finish ends the sessions of an execution.
func (x *execution) finish(failure error) error {
	ctx := context.Background()
	var errs []error
	for _, s := range x.sessions {
		switch {
		case x.test:
			if err := s.discard(ctx); err != nil {
				errs = append(errs, err)
			}
		case s.n == 1 && failure == nil && x.opts.End == CommitOnSuccess && (x.opts.Rollback == nil || !x.opts.Rollback()):
			if err := s.Commit(ctx); err != nil {
				errs = append(errs, fmt.Errorf("commit at the end of the execution: %w", err))
			}
		case s.open:
			if s.n > 1 && x.opts.VM.Log != nil {
				x.opts.VM.Log.Log("warning", fmt.Sprintf("session %d rolled back at the end of the execution", s.n))
			}
			if err := s.Rollback(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		s.close()
	}
	x.flushDiagnosis()
	x.flushSelections()
	return errors.Join(errs...)
}

// generation is the code of all installed packages at one generation,
// linked, with the schema it was compiled against.
type generation struct {
	n      int64
	image  *vm.Image
	phys   *model.Physical
	schema *model.Schema
	sheets *sheets
	// tree are the service nodes, built when a request needs them.
	tree     *httpsvc.Tree
	treeOnce sync.Once
	// extensions are the extension points with their implementations,
	// built when an execution runs one.
	extensions     *extensionRegistry
	extensionsOnce sync.Once
}

// generation returns the code of the current generation. An execution
// reads the generation number; only when it is newer than the cached code
// are the schema and the code loaded and linked again, in one
// transaction. An execution keeps its generation to its end; older ones
// are dropped once no execution holds them.
func (k *Kernel) generation(ctx context.Context) (*generation, error) {
	n, err := k.repo.Generation(ctx, k.db)
	if err != nil {
		return nil, err
	}
	if g := k.code.Load(); g != nil && g.n >= n {
		return g, nil
	}
	var g *generation
	err = k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		n, err := k.repo.Generation(ctx, tx)
		if err != nil {
			return err
		}
		s, err := k.repo.Load(ctx, tx)
		if err != nil {
			return err
		}
		image, err := k.loadImage(ctx, tx)
		if err != nil {
			return err
		}
		g = &generation{n: n, image: image, phys: s.Physical(), schema: s, sheets: newSheets(s)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for {
		old := k.code.Load()
		if old != nil && old.n >= g.n {
			return old, nil
		}
		if k.code.CompareAndSwap(old, g) {
			return g, nil
		}
	}
}
