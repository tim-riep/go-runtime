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
	"errors"
	"fmt"
	"log/slog"

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/dialect"
	"github.com/tim-riep/go-runtime/internal/repo"
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
	"github.com/tim-riep/go-runtime/reflector"
	"github.com/tim-riep/go-runtime/rterr"
	"github.com/tim-riep/go-runtime/vm"
)

// execution is the database access of one execution of code: its
// sessions, the tenant and the schema the code was compiled against.
type execution struct {
	k        *Kernel
	d        dialect.Dialect
	phys     *model.Physical
	schema   *model.Schema
	ctx      context.Context // of the execution, for the host
	sheets   *sheets
	tenant   string
	opts     ExecOptions
	sessions []*session
	// test runs the execution for a test method: commit only sets a
	// savepoint that rollback returns to, and everything is rolled back
	// at the end.
	test bool
	// contents are the asset contents that code of the execution loaded,
	// by hash.
	contents map[string][]byte
	// reflector describes the schema of the execution, made on first use.
	reflector *reflector.Reflector
	// auth are the effective authorizations of the user, loaded on first
	// use; nil without user.
	auth       *authorization.Effective
	authLoaded bool
	// failed is the last failed check of the execution, traced the checks
	// a trace records; both are written when the execution ends.
	failed *checkRecord
	traced []checkRecord
	// grants are the authorizations a test method granted itself.
	grants *testGrants
	// extensions are the extension points and implementations of the
	// schema, and activation their activation, read on first use.
	extensions *extensionRegistry
	activation *activation
	// selections are the evaluations of extension points a trace records,
	// written when the execution ends.
	selections []repo.SelectionEntry
}

// session is a database session: its own connection and a transaction
// that begins with the first statement.
type session struct {
	x       *execution
	n       int
	conn    *sql.Conn
	open    bool // a transaction is running
	written bool // the transaction changed rows
	cursors int  // cursors declared, for their names
	saved   bool // a commit of a test set the savepoint rt_committed
	after   []func()
}

func (x *execution) Default() vm.Session { return x.sessions[0] }

func (x *execution) Open(ctx context.Context) (vm.Session, error) {
	if len(x.sessions) >= x.opts.MaxSessions {
		return nil, &vm.DatabaseError{Class: "/RT/SESSION_ERROR", Text: fmt.Sprintf("an execution may use at most %d sessions, including the default session", x.opts.MaxSessions)}
	}
	s := &session{x: x, n: len(x.sessions) + 1}
	x.sessions = append(x.sessions, s)
	return s, nil
}

func (s *session) Number() int { return s.n }

func (s *session) sqlite() bool { return s.x.d.Name() == "sqlite" }

// begin starts the transaction of the session if none is running.
func (s *session) begin(ctx context.Context) error {
	if s.open {
		return nil
	}
	if s.conn == nil {
		conn, err := s.x.k.db.Conn(ctx)
		if err != nil {
			return err
		}
		s.conn = conn
		if s.sqlite() {
			if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", s.x.opts.LockTimeout.Milliseconds())); err != nil {
				return err
			}
		}
	}
	// Transactions begin without a lock; under SQLite several sessions
	// read at the same time and the first write takes the write lock.
	begin := "BEGIN"
	if s.sqlite() {
		begin = "BEGIN DEFERRED"
	}
	if _, err := s.conn.ExecContext(ctx, begin); err != nil {
		return err
	}
	s.open = true
	if !s.sqlite() {
		if _, err := s.conn.ExecContext(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", s.x.opts.LockTimeout.Milliseconds())); err != nil {
			return err
		}
	}
	return nil
}

// guard runs a statement. Under PostgreSQL it runs in a savepoint, so that
// the transaction goes on after an error that code catches.
func (s *session) guard(ctx context.Context, fn func() error) error {
	if s.sqlite() {
		return s.x.classify(fn())
	}
	if _, err := s.conn.ExecContext(ctx, "SAVEPOINT rt_statement"); err != nil {
		return err
	}
	if err := fn(); err != nil {
		if _, rerr := s.conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT rt_statement"); rerr != nil {
			return errors.Join(err, rerr)
		}
		return s.x.classify(err)
	}
	_, err := s.conn.ExecContext(ctx, "RELEASE SAVEPOINT rt_statement")
	return err
}

// classify turns errors of the database that code can handle into
// database errors of the VM.
func (x *execution) classify(err error) error {
	if err == nil {
		return nil
	}
	var de *vm.DatabaseError
	if errors.As(err, &de) {
		return err
	}
	err = x.d.ClassifyError(err)
	var cv *rterr.ConstraintViolation
	if errors.As(err, &cv) {
		class := map[rterr.ViolationKind]string{
			rterr.ViolationUnique:     "/RT/DUPLICATE_ROW",
			rterr.ViolationForeignKey: "/RT/FOREIGN_KEY_VIOLATION",
			rterr.ViolationCheck:      "/RT/CHECK_VIOLATION",
			rterr.ViolationNotNull:    "/RT/NOT_NULL_VIOLATION",
		}[cv.Kind]
		if class == "" {
			class = "/RT/DATABASE_ERROR"
		}
		return &vm.DatabaseError{Class: class, Text: cv.Error(), Table: cv.Table, Constraint: cv.Constraint}
	}
	var lt *rterr.LockTimeout
	if errors.As(err, &lt) {
		text := "a statement waited too long for a lock"
		if len(x.sessions) > 1 {
			text += "; probably another session of the same execution holds it"
		}
		return &vm.DatabaseError{Class: "/RT/LOCK_TIMEOUT", Text: text}
	}
	return err
}

func (s *session) statement(p *bytecode.Program, q *bytecode.Query, args []any, limit int64) (*statement, error) {
	var auth *authorization.Effective
	if len(q.Auths) > 0 {
		var err error
		if auth, err = s.x.authorizations(); err != nil {
			return nil, err
		}
	}
	st, err := buildStatement(s.x.d, s.x.phys, s.x.tenant, auth, p, q, args, limit)
	if err != nil {
		return nil, err
	}
	slog.Debug("statement of code", "session", s.n, "sql", st.text)
	return st, nil
}

// traceFilters records the authorization filters of a statement that a
// trace of the user records, with the rows it read or changed.
func (s *session) traceFilters(st *statement, rows model.Optional[int64]) {
	x := s.x
	user, ok := x.opts.User.Get()
	if !ok || len(st.filters) == 0 || !x.auth.Tracing(x.k.now()) {
		return
	}
	for _, f := range st.filters {
		x.traced = append(x.traced, checkRecord{user: user, tenant: x.tenant, at: x.k.now(), mode: native.CheckFilter, check: f.check,
			columns: f.columns, condition: f.condition, rows: rows})
	}
}

func (s *session) Query(ctx context.Context, p *bytecode.Program, q *bytecode.Query, args []any, limit int64, stream bool) (vm.Rows, error) {
	st, err := s.statement(p, q, args, limit)
	if err != nil {
		return nil, err
	}
	if err := s.begin(ctx); err != nil {
		return nil, s.x.classify(err)
	}
	if stream && !s.sqlite() {
		// A PostgreSQL connection runs one statement at a time, so the rows
		// of a cursor are fetched in portions.
		s.cursors++
		name := fmt.Sprintf("rt_cursor_%d", s.cursors)
		err := s.guard(ctx, func() error {
			_, err := s.conn.ExecContext(ctx, "DECLARE "+name+" NO SCROLL CURSOR FOR "+st.text, st.args...)
			return err
		})
		if err != nil {
			return nil, err
		}
		s.traceFilters(st, model.None[int64]())
		return &fetchRows{s: s, ctx: ctx, name: name, st: st}, nil
	}
	// The rows are read at once: the VM reads all of them anyway, and the
	// savepoint of the statement can only be released afterwards.
	buf := &bufferedRows{}
	err = s.guard(ctx, func() error {
		rows, err := s.conn.QueryContext(ctx, st.text, st.args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		rr := &resultRows{x: s.x, rows: rows, st: st, count: q.Kind == bytecode.QCount}
		for {
			vals, ok, err := rr.Next()
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			buf.rows = append(buf.rows, vals)
			if len(buf.rows) > s.x.opts.MaxRows {
				return nil // the VM reports too many rows
			}
		}
	})
	if err != nil {
		return nil, err
	}
	s.traceFilters(st, model.Some(int64(len(buf.rows))))
	return buf, nil
}

// bufferedRows are rows read before the VM asks for them.
type bufferedRows struct{ rows [][]any }

func (b *bufferedRows) Next() ([]any, bool, error) {
	if len(b.rows) == 0 {
		return nil, false, nil
	}
	row := b.rows[0]
	b.rows = b.rows[1:]
	return row, true, nil
}

func (b *bufferedRows) Close() error { return nil }

func (s *session) Exec(ctx context.Context, p *bytecode.Program, q *bytecode.Query, args []any) (int64, error) {
	st, err := s.statement(p, q, args, 0)
	if err != nil {
		return 0, err
	}
	if s.sqlite() {
		// SQLite has one writing transaction at a time. Waiting for it
		// would wait for this execution itself.
		for _, o := range s.x.sessions {
			if o != s && o.written {
				return 0, &vm.DatabaseError{Class: "/RT/SESSION_ERROR", Text: fmt.Sprintf("SQLite allows only one writing transaction; commit or roll back session %d first", o.n)}
			}
		}
	}
	if err := s.begin(ctx); err != nil {
		return 0, s.x.classify(err)
	}
	var n int64
	err = s.guard(ctx, func() error {
		res, err := s.conn.ExecContext(ctx, st.text, st.args...)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return 0, err
	}
	s.written = true
	s.traceFilters(st, model.Some(n))
	return n, nil
}

// AfterCommit registers a function that runs after the next successful
// commit of the session; rollback discards it.
func (s *session) AfterCommit(fn func()) { s.after = append(s.after, fn) }

func (s *session) Commit(ctx context.Context) error {
	if !s.open {
		s.runAfter()
		return nil
	}
	if s.x.test {
		if _, err := s.conn.ExecContext(ctx, "SAVEPOINT rt_committed"); err != nil {
			return err
		}
		s.saved = true
		s.runAfter()
		return nil
	}
	if _, err := s.conn.ExecContext(ctx, "COMMIT"); err != nil {
		s.end(ctx)
		return s.x.classify(err)
	}
	s.open, s.written = false, false
	s.runAfter()
	return nil
}

func (s *session) runAfter() {
	after := s.after
	s.after = nil
	for _, fn := range after {
		fn()
	}
}

func (s *session) Rollback(ctx context.Context) error {
	s.after = nil
	if !s.open {
		return nil
	}
	if s.x.test && s.saved {
		_, err := s.conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT rt_committed")
		return err
	}
	_, err := s.conn.ExecContext(ctx, "ROLLBACK")
	s.open, s.written = false, false
	return err
}

// discard rolls back everything at the end of a test.
func (s *session) discard(ctx context.Context) error {
	s.after = nil
	if !s.open {
		return nil
	}
	_, err := s.conn.ExecContext(ctx, "ROLLBACK")
	s.open, s.written, s.saved = false, false, false
	return err
}

// end rolls back a running transaction after a failed commit.
func (s *session) end(ctx context.Context) {
	if s.open {
		_, _ = s.conn.ExecContext(ctx, "ROLLBACK")
	}
	s.open, s.written, s.after = false, false, nil
}

// close returns the connection of the session to the pool.
func (s *session) close() {
	if s.conn == nil {
		return
	}
	ctx := context.Background()
	if s.open {
		_, _ = s.conn.ExecContext(ctx, "ROLLBACK")
		s.open = false
	}
	if s.sqlite() {
		_, _ = s.conn.ExecContext(ctx, "PRAGMA busy_timeout = 10000")
	}
	_ = s.conn.Close()
	s.conn = nil
}

// resultRows decodes the rows of a select.
type resultRows struct {
	x     *execution
	rows  *sql.Rows
	st    *statement
	count bool
}

func (r *resultRows) Next() ([]any, bool, error) {
	if !r.rows.Next() {
		return nil, false, r.x.classify(r.rows.Err())
	}
	if r.count {
		var n int64
		if err := r.rows.Scan(&n); err != nil {
			return nil, false, err
		}
		return []any{n}, true, nil
	}
	raw := make([]any, len(r.st.columns))
	ptrs := make([]any, len(raw))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := r.rows.Scan(ptrs...); err != nil {
		return nil, false, err
	}
	for i, v := range raw {
		dv, err := r.x.d.Decode(r.st.domains[i], v)
		if err != nil {
			return nil, false, fmt.Errorf("kernel: column %s: %w", r.st.columns[i], err)
		}
		raw[i] = dv
	}
	return raw, true, nil
}

func (r *resultRows) Close() error { return r.rows.Close() }

// fetchRows reads the rows of a PostgreSQL cursor in portions.
type fetchRows struct {
	s      *session
	ctx    context.Context
	name   string
	st     *statement
	buf    [][]any
	done   bool
	closed bool
}

const fetchSize = 200

func (r *fetchRows) Next() ([]any, bool, error) {
	if len(r.buf) == 0 && !r.done {
		rows, err := r.s.conn.QueryContext(r.ctx, fmt.Sprintf("FETCH %d FROM %s", fetchSize, r.name))
		if err != nil {
			return nil, false, r.s.x.classify(err)
		}
		rr := &resultRows{x: r.s.x, rows: rows, st: r.st}
		for {
			vals, ok, err := rr.Next()
			if err != nil {
				rows.Close()
				return nil, false, err
			}
			if !ok {
				break
			}
			r.buf = append(r.buf, vals)
		}
		rows.Close()
		if len(r.buf) < fetchSize {
			r.done = true
		}
	}
	if len(r.buf) == 0 {
		return nil, false, nil
	}
	row := r.buf[0]
	r.buf = r.buf[1:]
	return row, true, nil
}

func (r *fetchRows) Close() error {
	if r.closed || !r.s.open {
		return nil
	}
	r.closed = true
	_, err := r.s.conn.ExecContext(context.Background(), "CLOSE "+r.name)
	return err
}
