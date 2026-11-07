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

import (
	"fmt"
	"strings"
)

// Op is an instruction. Its operands follow it in the code.
type Op int32

// Instructions. The comments name the operands and the effect on the
// operand stack.
const (
	NOP Op = iota

	// Constants and locals.
	CONST_I     // v: push integer v
	CONST_I64   // hi lo: push integer
	CONST_DEC   // k: push DEC constant
	CONST_STR   // k: push string constant
	CONST_BYTES // k: push BYTES constant
	CONST_UUID  // k: push UUID constant (stored with the DEC constants)
	TRUE
	FALSE
	NULL
	ZERO       // t: push the zero value of type t
	LOAD       // slot
	STORE      // slot
	LOAD_CELL  // slot: push the value in the cell of a captured local
	STORE_CELL // slot
	NEW_CELL   // slot: move the value of the local into a new cell
	PUSH_CELL  // slot: push the cell itself, to capture it
	LOAD_UP    // i: push the value of captured cell i of the running function literal
	STORE_UP   // i
	PUSH_UP    // i: push captured cell i itself
	LOAD_ME    // push the object of the running method
	DUP
	POP
	SWAP
	COPY // t: replace a value of type t by a deep copy

	// Arithmetic. Integer operations check the range of their type, DEC
	// operations take the scales of their operands and result.
	ADD_I4
	SUB_I4
	MUL_I4
	DIV_I4
	MOD_I4
	NEG_I4
	ADD_I8
	SUB_I8
	MUL_I8
	DIV_I8
	MOD_I8
	NEG_I8
	ADD_DEC     // sa sb sr
	SUB_DEC     // sa sb sr
	MUL_DEC     // the result has the scale sa+sb
	DIV_DEC     // sa sb sr
	MOD_DEC     // sa sb sr
	NEG_DEC     //
	DEC_RESCALE // from to: rounding half away from zero when the scale gets smaller
	DEC_FIT     // p: raise /RT/OVERFLOW unless the value has at most p digits
	I_TO_DEC    // s: integer to DEC of scale s
	CONCAT      // two strings
	CONCATN     // n: n strings
	FMT         // kind extra: format a value of the type kind as text; extra is the scale or enum

	// Comparison and logic.
	CMP_I     // push -1, 0 or 1
	CMP_STR   //
	CMP_DEC   // sa sb
	CMP_BOOL  //
	CMP_UUID  //
	CMP_BYTES //
	EQ_VALUE  // t: deep equality of values of type t
	EQ_REF    // identity of objects and functions
	TEST      // cond: turn -1/0/1 into BOOL; cond 0 =, 1 <>, 2 <, 3 <=, 4 >, 5 >=
	NOT
	IS_NULL
	IS_EMPTY // kind
	CONV     // from to p s extra: explicit conversion with as
	CHECK    // t: check the rules of a named type: length, empty string, digits

	// Jumps to absolute positions.
	JUMP
	JUMP_FALSE
	JUMP_TRUE
	JUMP_NULL    // pc: jump if the top is null, without removing it
	JUMP_NOTNULL // pc

	// Fields of objects and structs; the object is below the value. pres
	// is the byte of the present bit of optional fields, or -1.
	GETBOOL  // off bit
	SETBOOL  // off bit
	GETOBOOL // off bit: present bit, value bit next
	SETOBOOL // off bit
	GETI4    // off pres presbit
	SETI4    // off pres presbit
	GETI8    // off pres presbit
	SETI8    // off pres presbit
	GETDEC   // off pres presbit
	SETDEC   // off pres presbit
	GETUUID  // off pres presbit
	SETUUID  // off pres presbit
	GETDATE  // off pres presbit
	SETDATE  // off pres presbit
	GETTS    // off pres presbit
	SETTS    // off pres presbit
	GETENUM  // off pres presbit
	SETENUM  // off pres presbit
	GETREF   // slot pres presbit
	SETREF   // slot pres presbit
	STATICS  // class: push the object holding the static attributes

	// Objects and collections.
	NEW_OBJ    // class: a new object with zero values
	NEW_STRUCT // layout
	NEW_ARRAY  // t
	NEW_MAP    // t
	ARRAY_LIT  // t n
	MAP_LIT    // t n: n key-value pairs
	INDEX_GET  // array index
	INDEX_SET  // array index value
	MINDEX_GET // map key
	MINDEX_SET // map key value

	// Calls.
	CALL         // fn argc: argc includes the object of instance methods
	CALL_VIRT    // slot argc
	CALL_IFACE   // iface method argc
	CALL_FN      // argc: the function value is below the arguments
	CALL_BUILTIN // id argc t r: t describes the receiver, r the result
	MAKE_FN      // fn ncells: the cells are on the stack, the object of me below them for instance methods
	RETURN
	RETURN_VALUE
	RAISE    // the exception object
	ASSERT   // hasmsg: raise /RT/ASSERTION_FAILED if false
	LOG      // level: 0 debug, 1 info, 2 warning, 3 error
	FOR_TEST // i to step exit: leave the counting loop when i is past to
	FOR_STEP // i to step exit int4: i += step, leaving the loop on overflow

	// Database. The session is on top of the stack, null for the default
	// session; below it the limit, if any, and below that the parameters
	// of the query, or the row of row statements.
	DB_QUERY  // q: run query q and push its result, nothing for row statements
	DB_CURSOR // q: run select q and push a cursor over its rows
	DB_NEXT   // exit: pop a cursor; push its next row or close it and jump to exit
	DB_CLOSE  // pop a cursor and close it
	DB_END    // rollback: pop a session and commit it, or roll it back if rollback is 1
	DB_OPEN   // push a new session

	// Transformations.
	XFORM // t: pop the source and the parameters of transform t and push its result

	opCount
)

var opInfo = [opCount]struct {
	name     string
	operands int
}{
	NOP: {"NOP", 0}, CONST_I: {"CONST_I", 1}, CONST_I64: {"CONST_I64", 2}, CONST_DEC: {"CONST_DEC", 1}, CONST_STR: {"CONST_STR", 1},
	CONST_BYTES: {"CONST_BYTES", 1}, CONST_UUID: {"CONST_UUID", 1}, TRUE: {"TRUE", 0}, FALSE: {"FALSE", 0}, NULL: {"NULL", 0},
	ZERO: {"ZERO", 1}, LOAD: {"LOAD", 1}, STORE: {"STORE", 1}, LOAD_CELL: {"LOAD_CELL", 1}, STORE_CELL: {"STORE_CELL", 1},
	NEW_CELL: {"NEW_CELL", 1}, PUSH_CELL: {"PUSH_CELL", 1}, LOAD_UP: {"LOAD_UP", 1}, STORE_UP: {"STORE_UP", 1}, PUSH_UP: {"PUSH_UP", 1},
	LOAD_ME: {"LOAD_ME", 0}, DUP: {"DUP", 0}, POP: {"POP", 0}, SWAP: {"SWAP", 0}, COPY: {"COPY", 1},
	ADD_I4: {"ADD_I4", 0}, SUB_I4: {"SUB_I4", 0}, MUL_I4: {"MUL_I4", 0}, DIV_I4: {"DIV_I4", 0}, MOD_I4: {"MOD_I4", 0}, NEG_I4: {"NEG_I4", 0},
	ADD_I8: {"ADD_I8", 0}, SUB_I8: {"SUB_I8", 0}, MUL_I8: {"MUL_I8", 0}, DIV_I8: {"DIV_I8", 0}, MOD_I8: {"MOD_I8", 0}, NEG_I8: {"NEG_I8", 0},
	ADD_DEC: {"ADD_DEC", 3}, SUB_DEC: {"SUB_DEC", 3}, MUL_DEC: {"MUL_DEC", 0}, DIV_DEC: {"DIV_DEC", 3}, MOD_DEC: {"MOD_DEC", 3}, NEG_DEC: {"NEG_DEC", 0},
	DEC_RESCALE: {"DEC_RESCALE", 2}, DEC_FIT: {"DEC_FIT", 1}, I_TO_DEC: {"I_TO_DEC", 1}, CONCAT: {"CONCAT", 0}, CONCATN: {"CONCATN", 1}, FMT: {"FMT", 2},
	CMP_I: {"CMP_I", 0}, CMP_STR: {"CMP_STR", 0}, CMP_DEC: {"CMP_DEC", 2}, CMP_BOOL: {"CMP_BOOL", 0}, CMP_UUID: {"CMP_UUID", 0}, CMP_BYTES: {"CMP_BYTES", 0},
	EQ_VALUE: {"EQ_VALUE", 1}, EQ_REF: {"EQ_REF", 0}, TEST: {"TEST", 1}, NOT: {"NOT", 0}, IS_NULL: {"IS_NULL", 0}, IS_EMPTY: {"IS_EMPTY", 1},
	CONV: {"CONV", 5}, CHECK: {"CHECK", 1},
	JUMP: {"JUMP", 1}, JUMP_FALSE: {"JUMP_FALSE", 1}, JUMP_TRUE: {"JUMP_TRUE", 1}, JUMP_NULL: {"JUMP_NULL", 1}, JUMP_NOTNULL: {"JUMP_NOTNULL", 1},
	GETBOOL: {"GETBOOL", 2}, SETBOOL: {"SETBOOL", 2}, GETOBOOL: {"GETOBOOL", 2}, SETOBOOL: {"SETOBOOL", 2},
	GETI4: {"GETI4", 3}, SETI4: {"SETI4", 3}, GETI8: {"GETI8", 3}, SETI8: {"SETI8", 3}, GETDEC: {"GETDEC", 3}, SETDEC: {"SETDEC", 3},
	GETUUID: {"GETUUID", 3}, SETUUID: {"SETUUID", 3}, GETDATE: {"GETDATE", 3}, SETDATE: {"SETDATE", 3}, GETTS: {"GETTS", 3}, SETTS: {"SETTS", 3},
	GETENUM: {"GETENUM", 3}, SETENUM: {"SETENUM", 3}, GETREF: {"GETREF", 3}, SETREF: {"SETREF", 3}, STATICS: {"STATICS", 1},
	NEW_OBJ: {"NEW_OBJ", 1}, NEW_STRUCT: {"NEW_STRUCT", 1}, NEW_ARRAY: {"NEW_ARRAY", 1}, NEW_MAP: {"NEW_MAP", 1}, ARRAY_LIT: {"ARRAY_LIT", 2},
	MAP_LIT: {"MAP_LIT", 2}, INDEX_GET: {"INDEX_GET", 0}, INDEX_SET: {"INDEX_SET", 0}, MINDEX_GET: {"MINDEX_GET", 0}, MINDEX_SET: {"MINDEX_SET", 0},
	CALL: {"CALL", 2}, CALL_VIRT: {"CALL_VIRT", 2}, CALL_IFACE: {"CALL_IFACE", 3}, CALL_FN: {"CALL_FN", 1}, CALL_BUILTIN: {"CALL_BUILTIN", 4},
	MAKE_FN: {"MAKE_FN", 2}, RETURN: {"RETURN", 0}, RETURN_VALUE: {"RETURN_VALUE", 0}, RAISE: {"RAISE", 0}, ASSERT: {"ASSERT", 1},
	LOG: {"LOG", 1}, FOR_TEST: {"FOR_TEST", 4}, FOR_STEP: {"FOR_STEP", 5},
	DB_QUERY: {"DB_QUERY", 1}, DB_CURSOR: {"DB_CURSOR", 1}, DB_NEXT: {"DB_NEXT", 1}, DB_CLOSE: {"DB_CLOSE", 0}, DB_END: {"DB_END", 1}, DB_OPEN: {"DB_OPEN", 0},
	XFORM: {"XFORM", 1},
}

func (o Op) String() string {
	if o >= 0 && o < opCount && opInfo[o].name != "" {
		return opInfo[o].name
	}
	return fmt.Sprintf("OP(%d)", int32(o))
}

// Operands returns the number of operands of an instruction.
func (o Op) Operands() int {
	if o < 0 || o >= opCount {
		return 0
	}
	return opInfo[o].operands
}

// Valid reports a known instruction.
func (o Op) Valid() bool { return o >= 0 && o < opCount && opInfo[o].name != "" }

// Disassemble lists the instructions of a function, one per line.
func Disassemble(f *Func) string {
	var b strings.Builder
	for pc := 0; pc < len(f.Code); {
		op := Op(f.Code[pc])
		fmt.Fprintf(&b, "%4d %s", pc, op)
		n := op.Operands()
		for i := 1; i <= n && pc+i < len(f.Code); i++ {
			fmt.Fprintf(&b, " %d", f.Code[pc+i])
		}
		b.WriteByte('\n')
		pc += 1 + n
	}
	return b.String()
}
