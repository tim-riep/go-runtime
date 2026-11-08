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

// Package kernel is the entry point for embedding the kernel: it opens a
// database, initializes the kernel tables and installs, upgrades and
// removes packages.
//
// Every operation follows the same path. The current schema is read from
// the repository, the operation computes the schema it should produce, that
// schema is validated as a whole, the difference becomes a plan of
// statements, and the plan runs in one transaction together with the
// repository update. With Options.DryRun only the plan is returned.
package kernel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/dialect/postgres"
	"github.com/tim-riep/go-runtime/dialect/sqlite"
	"github.com/tim-riep/go-runtime/internal/kernelschema"
	"github.com/tim-riep/go-runtime/internal/pkgload"
	"github.com/tim-riep/go-runtime/internal/planner"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/internal/validate"
	"github.com/tim-riep/go-runtime/kernelpkg"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/plan"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// Options control an operation.
type Options struct {
	// DryRun returns the plan without changing the database.
	DryRun bool
	// AllowDestructive permits changes that remove or restrict structures.
	AllowDestructive bool
	// Confirm acknowledges that the operation deletes data. Uninstalling a
	// package and deleting a tenant require it.
	Confirm bool
}

// Kernel manages packages in one database.
type Kernel struct {
	db     *sql.DB
	d      dialect.Dialect
	repo   *repo.Repo
	target int // kernel schema version this kernel works with
	ownsDB bool
	// maxAssetSize limits the size of one asset in bytes.
	maxAssetSize int64
	now          func() time.Time
	// code is the linked code of the newest generation that an execution
	// loaded.
	code atomic.Pointer[generation]
	// natives are the native methods of the kernel binary.
	natives *native.Registry
	// kernelPkgs are the kernel packages in installation order.
	kernelPkgs []*model.Package
	// maxFailedLogins locks a user after this many failed logins.
	maxFailedLogins int
	// httpStates caches the activation of the service nodes.
	httpStates atomic.Pointer[httpState]
	// maxAssetRead limits the size of an asset that code loads into the
	// memory of an execution.
	maxAssetRead int64
	// contents caches small asset contents by their hash; contentReads
	// counts the contents read from the database.
	contents     *contentCache
	contentReads atomic.Int64
	// effective caches effective authorizations; effectiveComputed counts
	// the computations.
	effective         *effectiveCache
	effectiveComputed atomic.Int64
	// httpLog receives the log statements of requests; nil writes them
	// to slog.
	httpLog vm.LogSink
}

// Open connects to a database. The connection string selects the dialect:
// "sqlite:<path>" for SQLite and "postgres://..." or "postgresql://..." for
// PostgreSQL. Call Init before other operations on a new database.
func Open(ctx context.Context, dsn string) (*Kernel, error) {
	return OpenConfig(ctx, dsn, Config{})
}

// OpenConfig is Open with a configuration. When the database is
// initialized, it installs or upgrades the kernel packages; when that
// fails, the kernel does not open.
func OpenConfig(ctx context.Context, dsn string, cfg Config) (*Kernel, error) {
	var d dialect.Dialect
	target := dsn
	switch {
	case strings.HasPrefix(dsn, "sqlite:"):
		d = sqlite.New()
		target = strings.TrimPrefix(strings.TrimPrefix(dsn, "sqlite:"), "//")
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		d = postgres.New()
	default:
		return nil, &rterr.UnsupportedDatabase{DSN: dsn}
	}
	if target == "" {
		return nil, &rterr.InvalidArgument{Argument: "connection string", Message: "missing database path"}
	}
	db, err := d.Open(target)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	k, err := NewConfig(db, d, cfg)
	if err != nil {
		db.Close()
		return nil, err
	}
	k.ownsDB = true
	if err := k.Start(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return k, nil
}

// Start brings an initialized database to this kernel: it compiles code
// of an older bytecode format again and installs or upgrades the kernel
// packages. Open calls it; kernels created with New call it themselves.
func (k *Kernel) Start(ctx context.Context) error {
	if err := k.recompileOutdated(ctx); err != nil {
		return fmt.Errorf("compiling the code of an older bytecode format: %w", err)
	}
	v, err := k.repo.KernelVersion(ctx, k.db)
	if err != nil || v != k.target {
		return err
	}
	if _, err := k.syncKernelPackages(ctx, false); err != nil {
		return err
	}
	if err := k.ensureAllAuthorizationsRole(ctx); err != nil {
		return err
	}
	k.warnUndeclaredNatives(ctx)
	return nil
}

// New creates a kernel on an existing connection pool. For SQLite the pool
// must come from the dialect's Open so every connection enforces foreign
// keys.
func New(db *sql.DB, d dialect.Dialect) *Kernel {
	k, err := NewConfig(db, d, Config{})
	if err != nil {
		panic(err) // the embedded kernel packages are checked by the tests
	}
	return k
}

// NewConfig is New with a configuration.
func NewConfig(db *sql.DB, d dialect.Dialect, cfg Config) (*Kernel, error) {
	k := &Kernel{db: db, d: d, repo: repo.New(d), target: kernelschema.Latest(), now: time.Now, maxAssetSize: model.DefaultMaxAssetSize,
		natives: cfg.Natives, kernelPkgs: cfg.KernelPackages, maxFailedLogins: cfg.MaxFailedLogins,
		maxAssetRead: cfg.MaxAssetRead, contents: newContentCache(contentCacheSize, contentCacheEntry),
		effective: newEffectiveCache()}
	if k.maxAssetRead <= 0 {
		k.maxAssetRead = DefaultMaxAssetRead
	}
	if k.maxFailedLogins <= 0 {
		k.maxFailedLogins = 5
	}
	if k.natives == nil {
		k.natives = native.Default
	}
	if k.kernelPkgs == nil {
		pkgs, err := kernelpkg.Packages()
		if err != nil {
			return nil, err
		}
		k.kernelPkgs = pkgs
	}
	return k, nil
}

// Close closes the connection pool if Open created it.
func (k *Kernel) Close() error {
	if k.ownsDB {
		return k.db.Close()
	}
	return nil
}

// DB returns the connection pool.
func (k *Kernel) DB() *sql.DB { return k.db }

// Dialect returns the dialect of the database.
func (k *Kernel) Dialect() dialect.Dialect { return k.d }

// SetMaxAssetSize sets the maximum size of one asset in bytes for later
// installations and upgrades. The default is model.DefaultMaxAssetSize.
func (k *Kernel) SetMaxAssetSize(n int64) { k.maxAssetSize = n }

// DefaultMaxAssetRead is the default of Config.MaxAssetRead.
const DefaultMaxAssetRead = 16 << 20

// SetMaxAssetRead sets the largest asset in bytes that code loads into
// the memory of an execution with /TRBASE/ASSET_REPOSITORY; see
// Config.MaxAssetRead.
func (k *Kernel) SetMaxAssetRead(n int64) {
	if n > 0 {
		k.maxAssetRead = n
	}
}

// timestamp returns the current time in the precision the kernel stores.
func (k *Kernel) timestamp() time.Time { return k.now().UTC().Truncate(time.Microsecond) }

// LoadDir loads a package from a directory.
func LoadDir(path string) (*model.Package, error) {
	return pkgload.Load(os.DirFS(path), path)
}

// LoadFS loads a package from a file system, e.g. an opened ZIP archive.
func LoadFS(fsys fs.FS, origin string) (*model.Package, error) {
	return pkgload.Load(fsys, origin)
}

// Init creates the kernel tables in a new database or migrates them to the
// version of this kernel. It returns the executed plan, or a plan without
// changes if the database is up to date.
func (k *Kernel) Init(ctx context.Context, opts Options) (*plan.Plan, error) {
	var result *plan.Plan
	executed := false
	run := func(q dialect.Querier, execute bool) error {
		version, err := k.repo.KernelVersion(ctx, q)
		if err != nil {
			return err
		}
		latest := k.target
		if version > latest {
			return rterr.ErrNewerSchema
		}
		old := k.repo.At(max(version, 1))
		cur := model.NewSchema()
		if version > 0 {
			if cur, err = old.Load(ctx, q); err != nil {
				return err
			}
		}
		langs := map[string]bool{}
		if version >= languagesVersion {
			if langs, err = old.LanguageCodes(ctx, q); err != nil {
				return err
			}
		}
		want, err := kernelschema.Replace(cur, latest, func(l string) bool { return langs[l] })
		if err != nil {
			return err
		}
		// Languages are created by the migration itself.
		if err := validate.Schema(want, validate.Options{}); err != nil {
			return err
		}
		p, err := planner.Build(k.d, plan.Migrate, cur, want, nil)
		if err != nil {
			return err
		}
		if version > 0 && !old.HasDataClasses() {
			for _, name := range cur.TableNames() {
				if t := cur.Tables[name]; t.Owner != model.Kernel {
					p.Changes = append(p.Changes, plan.Change{Kind: plan.AlterDataClass, Table: name, Notice: true,
						Detail: "none -> " + string(t.DataClass) + ", provisional until package " + string(t.Owner) + " is applied again"})
				}
			}
		}
		result = p
		if !execute || version == latest {
			return nil
		}
		started := k.timestamp()
		// The repository is rewritten from want after the statements ran.
		// Emptying it first lets new NOT NULL columns be added to its tables.
		if version > 0 {
			if err := old.Clear(ctx, q); err != nil {
				return err
			}
		}
		if err := k.execute(ctx, q, p); err != nil {
			return err
		}
		// English exists before the repository gets the texts of the
		// kernel.
		if version < languagesVersion && latest >= languagesVersion {
			if err := migrationHooks[languagesVersion](ctx, k, q); err != nil {
				return err
			}
		}
		if err := k.persist(ctx, q, want); err != nil {
			return err
		}
		for v := version + 1; v <= latest; v++ {
			if hook := migrationHooks[v]; hook != nil {
				if err := hook(ctx, k, q); err != nil {
					return fmt.Errorf("migration to kernel version %d: %w", v, err)
				}
			}
			if err := k.repo.AddMigration(ctx, q, v, started); err != nil {
				return err
			}
		}
		executed = true
		return k.writeLog(ctx, q, p, started, repo.StatusSuccess, model.None[string]())
	}
	if opts.DryRun {
		if err := run(k.db, false); err != nil {
			return nil, err
		}
		return result, nil
	}
	k.dropIdleConnections()
	err := k.d.Transact(ctx, k.db, dialect.TxOptions{DDL: true}, func(tx *sql.Tx) error { return run(tx, true) })
	if err != nil {
		return nil, err
	}
	result.Executed = executed
	if executed {
		k.dropIdleConnections()
	}
	// The kernel packages follow in their own operation.
	pkgs, err := k.syncKernelPackages(ctx, false)
	if err != nil {
		return nil, err
	}
	if pkgs != nil {
		merge(result, pkgs)
		result.Executed = true
	}
	if err := k.ensureAllAuthorizationsRole(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// migrationHooks run after the statements of a kernel migration, keyed by
// the version they migrate to.
var migrationHooks = map[int]func(ctx context.Context, k *Kernel, q dialect.Querier) error{
	languagesVersion: func(ctx context.Context, k *Kernel, q dialect.Querier) error {
		langs, err := k.repo.LanguageCodes(ctx, q)
		if err != nil || langs[model.DefaultLanguage] {
			return err
		}
		return k.repo.InsertLanguage(ctx, q, repo.Language{Code: model.DefaultLanguage, Name: "English"})
	},
	byteOrderVersion: alignTextColumns,
}

// byteOrderVersion is the kernel version from which text columns compare
// in byte order.
const byteOrderVersion = 14

// alignTextColumns gives text columns the type the dialect expects, with
// their collation, where only that differs: under PostgreSQL the columns
// of all tables compare in byte order from kernel version 14.
func alignTextColumns(ctx context.Context, k *Kernel, q dialect.Querier) error {
	s, err := k.repo.Load(ctx, q)
	if err != nil {
		return err
	}
	phys := s.Physical()
	want := k.d.Expected(phys)
	have, err := k.d.Introspect(ctx, q)
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(want.Tables)) {
		ht := have.Tables[name]
		if ht == nil {
			continue
		}
		var alters []string
		for _, f := range phys.Tables[name].Fields {
			d := phys.Domain(&f)
			if d == nil || d.Type != model.Char && d.Type != model.String {
				continue
			}
			w, h := want.Tables[name].Columns[f.Name], ht.Columns[f.Name]
			if h.Type != "" && h.Type != w.Type {
				alters = append(alters, "ALTER COLUMN "+k.d.QuoteIdent(f.Name)+" TYPE "+w.Type)
			}
		}
		if len(alters) > 0 {
			if _, err := q.ExecContext(ctx, "ALTER TABLE "+k.d.QuoteIdent(name)+" "+strings.Join(alters, ", ")); err != nil {
				return err
			}
		}
	}
	return nil
}

// validate checks the target schema of an operation against the languages
// set up in the database. Kernel versions without languages skip that
// check.
func (k *Kernel) validate(ctx context.Context, q dialect.Querier, s *model.Schema) error {
	opts := validate.Options{MaxAssetSize: k.maxAssetSize}
	if k.repo.HasLanguages() {
		langs, err := k.repo.LanguageCodes(ctx, q)
		if err != nil {
			return err
		}
		opts.Languages = langs
	}
	return validate.Schema(s, opts)
}

// requireInit fails unless the kernel tables have the version of this
// kernel.
func (k *Kernel) requireInit(ctx context.Context, q dialect.Querier) error {
	v, err := k.repo.KernelVersion(ctx, q)
	switch {
	case err != nil:
		return err
	case v > k.target:
		return rterr.ErrNewerSchema
	case v < k.target:
		return rterr.ErrNotInitialized
	}
	return nil
}

// Schema returns the current schema from the repository.
func (k *Kernel) Schema(ctx context.Context) (*model.Schema, error) {
	if err := k.requireInit(ctx, k.db); err != nil {
		return nil, err
	}
	return k.repo.Load(ctx, k.db)
}

// operation describes a schema change. target computes the desired schema
// from the current one; check may reject the resulting plan.
type operation struct {
	op       plan.Operation
	packages func(cur *model.Schema) []plan.PackageChange
	target   func(cur *model.Schema, now time.Time) (*model.Schema, error)
	check    func(p *plan.Plan) error
}

// apply runs an operation. Without DryRun everything happens in one
// transaction: reading the repository, planning, executing the statements
// and writing repository and log. A failed execution is logged afterwards
// in a separate transaction.
func (k *Kernel) apply(ctx context.Context, o operation, opts Options) (*plan.Plan, error) {
	var result *plan.Plan
	var started time.Time
	run := func(q dialect.Querier, execute bool) error {
		if err := k.requireInit(ctx, q); err != nil {
			return err
		}
		cur, err := k.repo.Load(ctx, q)
		if err != nil {
			return err
		}
		started = k.timestamp()
		want, err := o.target(cur, started)
		if err != nil {
			return err
		}
		stampAssets(cur, want, started)
		if err := k.validate(ctx, q, want); err != nil {
			return err
		}
		code, compileChanges, err := k.compile(ctx, q, cur, want, started)
		if err != nil {
			return err
		}
		roleNotes, err := k.checkRoles(ctx, q, o.op, cur, want)
		if err != nil {
			return err
		}
		p, err := planner.Build(k.d, o.op, cur, want, o.packages(cur))
		if err != nil {
			return err
		}
		p.Changes = append(p.Changes, compileChanges...)
		p.Changes = append(p.Changes, roleNotes...)
		result = p
		if o.check != nil {
			if err := o.check(p); err != nil {
				return err
			}
		}
		if !execute {
			return nil
		}
		if err := k.execute(ctx, q, p); err != nil {
			return err
		}
		if err := k.persist(ctx, q, want); err != nil {
			return err
		}
		if err := k.saveCode(ctx, q, code, started); err != nil {
			return err
		}
		if err := k.syncHTTPStates(ctx, q, want); err != nil {
			return err
		}
		if err := k.afterRoles(ctx, q, cur, want, started); err != nil {
			return err
		}
		return k.writeLog(ctx, q, p, started, repo.StatusSuccess, model.None[string]())
	}
	if opts.DryRun {
		if err := run(k.db, false); err != nil {
			return nil, err
		}
		return result, nil
	}
	err := k.d.Transact(ctx, k.db, dialect.TxOptions{DDL: true}, func(tx *sql.Tx) error { return run(tx, true) })
	if err != nil {
		var ee *rterr.ExecError
		if errors.As(err, &ee) && result != nil {
			k.logFailure(ctx, result, started, err)
		}
		return nil, err
	}
	result.Executed = true
	if err := k.ensureAllAuthorizationsRole(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// persist stores the contents of new assets, writes the repository and
// removes contents that are no longer referenced.
func (k *Kernel) persist(ctx context.Context, q dialect.Querier, want *model.Schema) error {
	if k.repo.HasAssets() {
		for _, r := range want.SortedAssetRepositories() {
			for _, p := range r.Paths() {
				for _, c := range r.Entries[p].Contents() {
					if err := k.repo.StoreContent(ctx, q, c); err != nil {
						return fmt.Errorf("storing asset %s in repository %s of package %s: %w", p, r.ID, r.Package, err)
					}
				}
			}
		}
	}
	if err := k.repo.Write(ctx, q, want); err != nil {
		return err
	}
	return k.repo.DeleteUnusedContents(ctx, q)
}

// stampAssets sets the time of the last change of every asset content:
// contents that are unchanged keep their time, all others get now.
func stampAssets(cur, want *model.Schema, now time.Time) {
	for key, w := range want.AssetRepositories {
		c := cur.AssetRepositories[key]
		for p, we := range w.Entries {
			var ce *model.AssetEntry
			if c != nil {
				ce = c.Entries[p]
			}
			stamp := func(wc, cc *model.AssetContent) {
				if wc == nil {
					return
				}
				if cc != nil && cc.SHA256 == wc.SHA256 && ce.MimeType == we.MimeType {
					wc.ModifiedAt = cc.ModifiedAt
				} else {
					wc.ModifiedAt = now
				}
			}
			if ce == nil {
				ce = &model.AssetEntry{}
			}
			stamp(we.Content, ce.Content)
			for l, v := range we.Variants {
				stamp(v, ce.Variants[l])
			}
		}
	}
}

// execute runs the statements of a plan.
func (k *Kernel) execute(ctx context.Context, q dialect.Querier, p *plan.Plan) error {
	for _, step := range p.Steps {
		for _, stmt := range step.Statements {
			if _, err := q.ExecContext(ctx, stmt); err != nil {
				return &rterr.ExecError{Step: step.Description, Statement: stmt, Err: k.d.ClassifyError(err)}
			}
		}
	}
	return nil
}

func (k *Kernel) logEntry(p *plan.Plan, started time.Time, status string, message model.Optional[string]) *repo.LogEntry {
	e := &repo.LogEntry{Operation: logOperation(p.Operation), Target: p.Target, StartedAt: started, FinishedAt: k.timestamp(), Status: status, Message: message}
	for _, pc := range p.Packages {
		e.Packages = append(e.Packages, repo.LogPackage{ID: pc.ID, From: pc.From, To: pc.To})
	}
	for _, s := range p.Steps {
		for _, st := range s.Statements {
			e.Statements = append(e.Statements, repo.LogStatement{SQL: st, Destructive: s.Destructive})
		}
	}
	return e
}

func (k *Kernel) writeLog(ctx context.Context, q dialect.Querier, p *plan.Plan, started time.Time, status string, message model.Optional[string]) error {
	e := k.logEntry(p, started, status, message)
	if err := k.repo.WriteLog(ctx, q, e); err != nil {
		return err
	}
	p.LogID = e.ID
	return nil
}

// logFailure records a failed operation. Errors are ignored: the original
// error is what the caller needs.
func (k *Kernel) logFailure(ctx context.Context, p *plan.Plan, started time.Time, cause error) {
	_ = k.d.Transact(ctx, k.db, dialect.TxOptions{}, func(tx *sql.Tx) error {
		return k.writeLog(ctx, tx, p, started, repo.StatusFailed, model.Some(cause.Error()))
	})
}

func logOperation(op plan.Operation) string {
	switch op {
	case plan.Install:
		return "INSTALL"
	case plan.Upgrade:
		return "UPGRADE"
	case plan.Uninstall:
		return "UNINSTALL"
	case plan.TenantDelete:
		return "TENANT_DELETE"
	case plan.LanguageRemove:
		return "LANGUAGE_REMOVE"
	}
	return "MIGRATE"
}

// dropIdleConnections closes the pooled connections of PostgreSQL. They
// cache prepared statements that describe the kernel tables, which
// PostgreSQL rejects once a migration changed the type of a column.
func (k *Kernel) dropIdleConnections() {
	if k.d.Name() == "postgres" {
		k.db.SetMaxIdleConns(0)
		k.db.SetMaxIdleConns(2)
	}
}
