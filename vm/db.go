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

package vm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	. "github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/dec"
)

// Database gives an execution access to the database. The kernel
// implements it; the VM only knows statements and values.
//
// Values are exchanged in their Go form: string for STRING, DEC, UUID and
// enum constants of STRING enums, int64 for integers and enum constants of
// integer enums, bool, time.Time for DATE and TIMESTAMP, []byte, nil for
// null and []any for the array of in.
type Database interface {
	// Default returns the default session of the execution.
	Default() Session
	// Open opens another session.
	Open(ctx context.Context) (Session, error)
}

// Session is a connection with its own transaction, which begins with its
// first statement.
type Session interface {
	// Number is 1 for the default session and counts up for the others.
	Number() int
	// Query runs select, select single and select count. Rows have the
	// values of the fields of the row type in their order. stream is set
	// for the cursor of for ... in select, while whose rows the code runs
	// other statements on the same session.
	Query(ctx context.Context, p *Program, q *Query, args []any, limit int64, stream bool) (Rows, error)
	// Exec runs the other statements and returns the number of rows they
	// changed. Row statements get the values of all fields of the row.
	Exec(ctx context.Context, p *Program, q *Query, args []any) (int64, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Rows are the rows of a select.
type Rows interface {
	// Next returns the next row, or false at the end.
	Next() ([]any, bool, error)
	Close() error
}

// DatabaseError is an error of a statement that code can catch: Class is
// a subclass of /RT/DATABASE_ERROR.
type DatabaseError struct {
	Class      string
	Text       string
	Table      string
	Constraint string
}

func (e *DatabaseError) Error() string { return e.Class + ": " + e.Text }

// session is the value of a SESSION.
type session struct {
	s       Session
	cursors int // open cursors
}

// cursor streams the rows of a select in for ... in.
type cursor struct {
	rows   Rows
	sess   *session
	p      *Program
	row    int // TypeDesc
	closed bool
	pc     int // where it was opened
}

func (c *cursor) close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	c.sess.cursors--
	return c.rows.Close()
}

// dbError turns an error of the database into an exception or a fatal
// error of the execution.
func (m *Machine) dbError(err error) (*thrown, error) {
	var de *DatabaseError
	if !errors.As(err, &de) {
		return nil, err
	}
	c := m.image.rt[de.Class]
	if c == nil {
		c = m.image.rt["/RT/DATABASE_ERROR"]
	}
	r := m.newObject(c)
	m.setField(r, m.image.textField, Str(de.Text))
	for name, v := range map[string]string{"TABLE_NAME": de.Table, "CONSTRAINT_NAME": de.Constraint} {
		if i := c.Layout.Field(name); i >= 0 {
			m.setField(r, c.Layout.Fields[i], Str(v))
		}
	}
	return &thrown{obj: r, pc: -1}, nil
}

// sessionOf returns the session of a value, the default session for null.
func (m *Machine) sessionOf(v Value) (*session, error) {
	if m.opts.Database == nil {
		return nil, errors.New("vm: the execution has no database")
	}
	if v.IsNull() {
		if m.defaultSession == nil {
			m.defaultSession = &session{s: m.opts.Database.Default()}
		}
		return m.defaultSession, nil
	}
	return v.R.(*session), nil
}

// rowArgs returns the values of all fields of a row.
func (m *Machine) rowArgs(p *Program, row int, v Value) []any {
	r := v.record()
	l := &p.Layouts[p.Types[row].Layout]
	out := make([]any, len(l.Fields))
	for i, f := range l.Fields {
		out[i] = m.toGo(p, f.Type, m.getField(r, f))
	}
	return out
}

// newRow builds a row from the values of the database.
func (m *Machine) newRow(p *Program, row int, vals []any) (Value, error) {
	l := &p.Layouts[p.Types[row].Layout]
	if len(vals) != len(l.Fields) {
		return Value{}, fmt.Errorf("vm: %d values for %d fields of %s", len(vals), len(l.Fields), l.Name)
	}
	r := &Record{Layout: l, P: p, Data: make([]byte, l.Size), Refs: make([]any, l.Refs)}
	m.alloc(int64(l.Size + 16*l.Refs + 48))
	for i, f := range l.Fields {
		v, err := m.fromGo(p, f.Type, vals[i])
		if err != nil {
			return Value{}, fmt.Errorf("vm: %s.%s: %w", l.Name, f.Name, err)
		}
		m.setField(r, f, v)
	}
	return Value{R: r}, nil
}

// toGo converts a value into its Go form.
func (m *Machine) toGo(p *Program, t int, v Value) any {
	d := &p.Types[t]
	if v.IsNull() {
		return nil
	}
	switch d.Kind {
	case TString:
		return v.str()
	case TInt4, TInt8:
		return v.int()
	case TDec:
		return dec.String(v.dec(), d.Scale)
	case TBool:
		return v.bool()
	case TDate:
		return time.Unix(v.int()*86400, 0).UTC()
	case TTimestamp:
		return time.UnixMicro(v.int()).UTC()
	case TUUID:
		return formatUUID(v)
	case TBytes:
		b, _ := v.R.([]byte)
		return b
	case TEnum:
		e := &p.Enums[d.Enum]
		s := e.Values[v.int()]
		if e.Base != TString {
			n, _ := strconv.ParseInt(s, 10, 64)
			return n
		}
		return s
	case TArray:
		a := v.array()
		out := make([]any, a.Len())
		for i := range out {
			out[i] = m.toGo(p, d.Elem, a.Get(i))
		}
		return out
	}
	return nil
}

// fromGo converts a Go value of the database into a value of type t.
// Strings are read in canonical text form for every type, so defaults of
// the schema use it as well.
func (m *Machine) fromGo(p *Program, t int, x any) (Value, error) {
	d := &p.Types[t]
	switch x := x.(type) {
	case nil:
		if !d.Optional {
			return Value{}, errors.New("null for a value that is not optional")
		}
		return Null, nil
	case string:
		return m.fromCanonical(p, t, x)
	case int64:
		switch d.Kind {
		case TInt4, TInt8:
			return Int(x), nil
		case TEnum, TDec:
			return m.fromCanonical(p, t, strconv.FormatInt(x, 10))
		}
	case bool:
		if d.Kind == TBool {
			return Bool(x), nil
		}
	case time.Time:
		switch d.Kind {
		case TDate:
			return Int(x.UTC().Unix() / 86400), nil
		case TTimestamp:
			return Int(x.UTC().UnixMicro()), nil
		}
	case []byte:
		if d.Kind == TBytes {
			return Value{R: append([]byte{}, x...)}, nil
		}
		if d.Kind == TString {
			return Str(string(x)), nil
		}
	}
	return Value{}, fmt.Errorf("unexpected value %T", x)
}

// fromCanonical reads a value in canonical text form.
func (m *Machine) fromCanonical(p *Program, t int, s string) (Value, error) {
	d := &p.Types[t]
	switch d.Kind {
	case TString:
		return Str(s), nil
	case TInt4, TInt8:
		n, err := strconv.ParseInt(s, 10, 64)
		return Int(n), err
	case TDec:
		x, err := dec.Parse(s, d.Scale)
		return DecValue(x), err
	case TBool:
		return Bool(s == "true"), nil
	case TBytes:
		return Value{R: []byte(s)}, nil
	case TEnum:
		e := &p.Enums[d.Enum]
		for i, v := range e.Values {
			if v == s {
				return Int(int64(i)), nil
			}
		}
		return Value{}, fmt.Errorf("%s has no fixed value %q", e.Name, s)
	case TDate, TTimestamp, TUUID:
		v, th := m.convert(p, TString, d.Kind, 0, 0, 0, Str(s))
		if th != nil {
			return Value{}, errors.New(m.getField(th.obj, m.image.textField).str())
		}
		return v, nil
	}
	return Value{}, fmt.Errorf("no text form for this type")
}

// dbQuery runs DB_QUERY and DB_CURSOR. It pops the session, the limit and
// the parameters or the row, and returns the result to push, if any.
func (m *Machine) dbQuery(p *Program, q *Query, stack []Value, asCursor bool) (rest []Value, result Value, push bool, c *cursor, exc *thrown, err error) {
	sv := stack[len(stack)-1]
	stack = stack[:len(stack)-1]
	sess, err := m.sessionOf(sv)
	if err != nil {
		return nil, Value{}, false, nil, nil, err
	}
	fail := func(e error) ([]Value, Value, bool, *cursor, *thrown, error) {
		t, ferr := m.dbError(e)
		return stack, Value{}, false, nil, t, ferr
	}
	switch q.Kind {
	case QInsert, QUpdateRow, QDeleteRow:
		row := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n, e := sess.s.Exec(m.ctx, p, q, m.rowArgs(p, q.Row, row))
		if e != nil {
			return fail(e)
		}
		if n == 0 && q.Kind != QInsert {
			return stack, Value{}, false, nil, m.fail("/RT/ROW_NOT_FOUND", "%s found no row of %s with the primary key of the row", q.Kind, q.Table), nil
		}
		return stack, Value{}, false, nil, nil, nil
	}
	var limit int64
	if q.Limit {
		limit = stack[len(stack)-1].int()
		stack = stack[:len(stack)-1]
		if limit < 0 {
			return stack, Value{}, false, nil, m.fail("/RT/VALUE_ERROR", "the limit of a select must not be negative"), nil
		}
	}
	np := len(q.Params)
	params := stack[len(stack)-np:]
	stack = stack[:len(stack)-np]
	args := make([]any, np)
	for i, v := range params {
		args[i] = m.toGo(p, q.Params[i], v)
	}
	if q.Kind == QUpdateWhere || q.Kind == QDeleteWhere {
		n, e := sess.s.Exec(m.ctx, p, q, args)
		if e != nil {
			return fail(e)
		}
		return stack, Int(n), true, nil, nil, nil
	}
	rows, e := sess.s.Query(m.ctx, p, q, args, limit, asCursor)
	if e != nil {
		return fail(e)
	}
	if asCursor {
		sess.cursors++
		c := &cursor{rows: rows, sess: sess, p: p, row: q.Row}
		return stack, Value{R: c}, true, c, nil, nil
	}
	defer rows.Close()
	switch q.Kind {
	case QCount:
		vals, _, e := rows.Next()
		if e != nil {
			return fail(e)
		}
		n, _ := vals[0].(int64)
		return stack, Int(n), true, nil, nil, nil
	case QSingle:
		vals, ok, e := rows.Next()
		if e != nil {
			return fail(e)
		}
		if !ok {
			return stack, Null, true, nil, nil, nil
		}
		v, e := m.newRow(p, q.Row, vals)
		return stack, v, true, nil, nil, e
	}
	a := m.newArrayOf(p, q.Row)
	for {
		vals, ok, e := rows.Next()
		if e != nil {
			return fail(e)
		}
		if !ok {
			break
		}
		if a.Len() >= m.opts.MaxRows {
			return stack, Value{}, false, nil, m.fail("/RT/TOO_MANY_ROWS", "select from %s found more than %d rows; use for ... in select to read them one by one, or a limit", q.Table, m.opts.MaxRows), nil
		}
		v, e := m.newRow(p, q.Row, vals)
		if e != nil {
			return stack, Value{}, false, nil, nil, e
		}
		if t := m.appendElem(a, v); t != nil {
			return stack, Value{}, false, nil, t, nil
		}
	}
	return stack, Value{R: a}, true, nil, nil, nil
}

// next reads the next row of a cursor; at the end it closes the cursor.
func (m *Machine) next(c *cursor) (Value, bool, *thrown, error) {
	if c.closed {
		return Value{}, false, nil, nil
	}
	vals, ok, err := c.rows.Next()
	if err != nil {
		c.close()
		t, ferr := m.dbError(err)
		return Value{}, false, t, ferr
	}
	if !ok {
		return Value{}, false, nil, c.close()
	}
	v, err := m.newRow(c.p, c.row, vals)
	return v, true, nil, err
}

// endSession runs commit and rollback.
func (m *Machine) endSession(v Value, rollback bool) (*thrown, error) {
	sess, err := m.sessionOf(v)
	if err != nil {
		return nil, err
	}
	what := "commit"
	if rollback {
		what = "rollback"
	}
	if sess.cursors > 0 {
		return m.fail("/RT/SESSION_ERROR", "%s of session %d would end its open cursor of a for ... in select", what, sess.s.Number()), nil
	}
	if !rollback && sess.s.Number() == 1 && m.opts.Entities != nil && m.opts.Entities.Pending() {
		return m.fail("/RT/SESSION_ERROR", "commit of the default session while the transaction buffer holds changes of business objects; use commit business objects"), nil
	}
	if m.opts.ReadOnly {
		return m.fail("/RT/SESSION_ERROR", "%s is not allowed: the execution only reads", what), nil
	}
	if n := len(m.noCommit); n > 0 && sess.s.Number() == 1 {
		return m.fail("/RT/SESSION_ERROR", "%s of the default session is not allowed in an implementation of extension point %s; open a session of its own", what, m.noCommit[n-1]), nil
	}
	if rollback {
		err = sess.s.Rollback(m.ctx)
	} else {
		err = sess.s.Commit(m.ctx)
	}
	if err != nil {
		return m.dbError(err)
	}
	return nil, nil
}

// openSession runs open session.
func (m *Machine) openSession() (Value, *thrown, error) {
	if m.opts.Database == nil {
		return Value{}, nil, errors.New("vm: the execution has no database")
	}
	s, err := m.opts.Database.Open(m.ctx)
	if err != nil {
		t, ferr := m.dbError(err)
		return Value{}, t, ferr
	}
	return Value{R: &session{s: s}}, nil, nil
}
