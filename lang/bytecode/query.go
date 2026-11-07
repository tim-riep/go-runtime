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

package bytecode

// QueryKind is the kind of a database statement.
type QueryKind uint8

// Database statements.
const (
	QSelect      QueryKind = iota + 1 // select: an array of rows or a cursor
	QSingle                           // select single: a row or null
	QCount                            // select count
	QInsert                           // insert row
	QUpdateRow                        // update row, by the primary key
	QDeleteRow                        // delete row, by the primary key
	QUpdateWhere                      // update T set ... where ...
	QDeleteWhere                      // delete from T where ...
)

var queryKinds = [...]string{"", "select", "select single", "select count", "insert", "update", "delete", "update set", "delete from"}

func (k QueryKind) String() string {
	if int(k) < len(queryKinds) {
		return queryKinds[k]
	}
	return "query"
}

// Query is a database statement independent of the database system; the
// kernel turns it into SQL. Rows are values of the struct type Row whose
// fields are the columns of the table except the tenant.
type Query struct {
	Kind  QueryKind
	Table string
	Row   int // TypeDesc
	// Key are the primary key fields that update and delete of a row use.
	Key []string
	// Sets are the fields that update ... set assigns, in the order of
	// their values on the stack.
	Sets []string
	// Where is the condition: an index into Conds, -1 without condition.
	Where int
	Conds []Cond
	Order []OrderKey
	Limit bool // the limit is on the stack after the parameters
	// Params are the types of the values of the code that the statement
	// uses, in the order the code pushes them: the values of Sets, then
	// the values of the condition.
	Params []int
}

// CondOp is the operator of a condition.
type CondOp uint8

// Conditions. Comparisons relate Field to the parameter Param or, with
// Param -1, to the field Other.
const (
	CAnd CondOp = iota + 1 // Kids
	COr                    // Kids
	CNot                   // Kids[0]
	CEq
	CNe
	CLt
	CLe
	CGt
	CGe
	CIn      // Field in the array Param
	CIsNull  // Field is null
	CIsEmpty // Field = ""
)

// Cond is a node of a condition.
type Cond struct {
	Op    CondOp
	Kids  []int
	Field string
	Other string
	Param int
}

// OrderKey is a field of order by.
type OrderKey struct {
	Field      string
	Descending bool
}
