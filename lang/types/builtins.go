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

package types

import (
	"slices"
	"sort"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// Builtin is a built-in method of a type.
type Builtin struct {
	ID   BuiltinID
	Name string
	// Mutates tells that the method changes its receiver, which must then
	// be a place.
	Mutates bool
}

// BuiltinID identifies a built-in method.
type BuiltinID int

// Built-in methods, grouped by receiver.
const (
	ArrLength BuiltinID = iota
	ArrAppend
	ArrInsert
	ArrRemove
	ArrClear
	ArrContains
	ArrIndexOf
	ArrFind
	ArrFilter
	ArrMap
	ArrAny
	ArrAll
	ArrSort
	ArrSortBy
	ArrReverse
	ArrSlice
	ArrJoin
	ArrLookup
	MapLength
	MapGet
	MapSet
	MapContains
	MapRemove
	MapKeys
	MapValues
	MapClear
	StrLength
	StrUpper
	StrLower
	StrTrim
	StrContains
	StrStartsWith
	StrEndsWith
	StrIndexOf
	StrSubstring
	StrReplace
	StrSplit
	BytesLength
	DateAddDays
	DateDaysUntil
	DateYear
	DateMonth
	DateDay
	TsDate
	EnumParse
	EnumValueOf
	EnumName
	UUIDRandom
)

type builtinSig struct {
	id      BuiltinID
	mutates bool
	// sig returns the parameter types and the result for a receiver; nil
	// params with a non-nil result are not used.
	sig func(recv Type) ([]Type, Type)
}

func fixed(params []Type, result Type) func(Type) ([]Type, Type) {
	return func(Type) ([]Type, Type) { return params, result }
}

func elemOf(t Type) Type { return t.(*Array).Elem }

var arrayBuiltins = map[string]builtinSig{
	"LENGTH":   {ArrLength, false, fixed(nil, TypInt8)},
	"APPEND":   {ArrAppend, true, func(r Type) ([]Type, Type) { return []Type{elemOf(r)}, TypVoid }},
	"INSERT":   {ArrInsert, true, func(r Type) ([]Type, Type) { return []Type{TypInt8, elemOf(r)}, TypVoid }},
	"REMOVE":   {ArrRemove, true, fixed([]Type{TypInt8}, TypVoid)},
	"CLEAR":    {ArrClear, true, fixed(nil, TypVoid)},
	"CONTAINS": {ArrContains, false, func(r Type) ([]Type, Type) { return []Type{elemOf(r)}, TypBool }},
	"INDEX_OF": {ArrIndexOf, false, func(r Type) ([]Type, Type) { return []Type{elemOf(r)}, Opt(TypInt8) }},
	"FIND":     {ArrFind, false, func(r Type) ([]Type, Type) { return []Type{predicate(r)}, Opt(elemOf(r)) }},
	"FILTER":   {ArrFilter, false, func(r Type) ([]Type, Type) { return []Type{predicate(r)}, r }},
	"ANY":      {ArrAny, false, func(r Type) ([]Type, Type) { return []Type{predicate(r)}, TypBool }},
	"ALL":      {ArrAll, false, func(r Type) ([]Type, Type) { return []Type{predicate(r)}, TypBool }},
	"SORT":     {ArrSort, false, func(r Type) ([]Type, Type) { return nil, r }},
	"REVERSE":  {ArrReverse, false, func(r Type) ([]Type, Type) { return nil, r }},
	"SLICE":    {ArrSlice, false, func(r Type) ([]Type, Type) { return []Type{TypInt8, TypInt8}, r }},
	"JOIN":     {ArrJoin, false, fixed([]Type{TypString}, TypString)},
	// MAP, SORT_BY and LOOKUP are typed in builtinCall.
	"MAP":     {ArrMap, false, nil},
	"SORT_BY": {ArrSortBy, false, nil},
	"LOOKUP":  {ArrLookup, false, nil},
}

func predicate(r Type) Type { return &Func{Params: []Type{elemOf(r)}, Result: TypBool} }

var mapBuiltins = map[string]builtinSig{
	"LENGTH":   {MapLength, false, fixed(nil, TypInt8)},
	"GET":      {MapGet, false, func(r Type) ([]Type, Type) { m := r.(*Map); return []Type{m.Key}, Opt(m.Elem) }},
	"SET":      {MapSet, true, func(r Type) ([]Type, Type) { m := r.(*Map); return []Type{m.Key, m.Elem}, TypVoid }},
	"CONTAINS": {MapContains, false, func(r Type) ([]Type, Type) { return []Type{r.(*Map).Key}, TypBool }},
	"REMOVE":   {MapRemove, true, func(r Type) ([]Type, Type) { return []Type{r.(*Map).Key}, TypVoid }},
	"KEYS":     {MapKeys, false, func(r Type) ([]Type, Type) { return nil, &Array{Elem: r.(*Map).Key} }},
	"VALUES":   {MapValues, false, func(r Type) ([]Type, Type) { return nil, &Array{Elem: r.(*Map).Elem} }},
	"CLEAR":    {MapClear, true, fixed(nil, TypVoid)},
}

var stringBuiltins = map[string]builtinSig{
	"LENGTH":      {StrLength, false, fixed(nil, TypInt8)},
	"UPPER":       {StrUpper, false, fixed(nil, TypString)},
	"LOWER":       {StrLower, false, fixed(nil, TypString)},
	"TRIM":        {StrTrim, false, fixed(nil, TypString)},
	"CONTAINS":    {StrContains, false, fixed([]Type{TypString}, TypBool)},
	"STARTS_WITH": {StrStartsWith, false, fixed([]Type{TypString}, TypBool)},
	"ENDS_WITH":   {StrEndsWith, false, fixed([]Type{TypString}, TypBool)},
	"INDEX_OF":    {StrIndexOf, false, fixed([]Type{TypString}, Opt(TypInt8))},
	"SUBSTRING":   {StrSubstring, false, fixed([]Type{TypInt8, TypInt8}, TypString)},
	"REPLACE":     {StrReplace, false, fixed([]Type{TypString, TypString}, TypString)},
	"SPLIT":       {StrSplit, false, fixed([]Type{TypString}, &Array{Elem: TypString})},
}

var dateBuiltins = map[string]builtinSig{
	"ADD_DAYS":   {DateAddDays, false, fixed([]Type{TypInt8}, TypDate)},
	"DAYS_UNTIL": {DateDaysUntil, false, fixed([]Type{TypDate}, TypInt8)},
	"YEAR":       {DateYear, false, fixed(nil, TypInt4)},
	"MONTH":      {DateMonth, false, fixed(nil, TypInt4)},
	"DAY":        {DateDay, false, fixed(nil, TypInt4)},
}

// BuiltinNames returns the names of the built-in methods of a type, for
// messages and documentation.
func BuiltinNames(t Type) []string {
	var m map[string]builtinSig
	switch {
	case isArray(t):
		m = arrayBuiltins
	case isMap(t):
		m = mapBuiltins
	case IsKind(t, String):
		m = stringBuiltins
	case IsKind(t, Date):
		m = dateBuiltins
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isArray(t Type) bool { _, ok := t.(*Array); return ok }
func isMap(t Type) bool   { _, ok := t.(*Map); return ok }

// ordered reports types that SORT and SORT_BY can order.
func ordered(t Type) bool {
	if _, ok := t.(*Enum); ok {
		return false
	}
	b := BasicOf(t)
	return b != nil && (b.Kind == String || b.Kind == Int4 || b.Kind == Int8 || b.Kind == Dec || b.Kind == Date || b.Kind == Timestamp)
}

// comparable reports types whose values can be compared with =.
func comparable(t Type) bool {
	switch x := NonOpt(t).(type) {
	case *Func:
		return false
	case *Struct:
		for _, f := range x.Fields {
			if !comparable(f.Type) {
				return false
			}
		}
	case *Array:
		return comparable(x.Elem)
	case *Map:
		return comparable(x.Elem)
	}
	return true
}

// builtinCall checks a call of a built-in method of recvType. recv is nil
// for methods of an enum type such as PARSE.
func (c *checker) builtinCall(f *fnCtx, sc *scope, x *syntax.CallExpr, recv syntax.Expr, recvType Type, e env, want Type) Type {
	fun := x.Fun.(*syntax.SelectExpr)
	name := fun.Name.Name
	fail := func() Type {
		c.errorf(fun.Name.Pos, CodeUnknown, "%s has no method %s", recvType, name)
		c.args(f, sc, x, &Func{}, e, nil)
		return TypInvalid
	}
	record := func(b builtinSig, result Type) Type {
		bi := &Builtin{ID: b.id, Name: name, Mutates: b.mutates}
		c.info.Calls[x] = &Call{Kind: CallBuiltin, Builtin: bi, Receiver: recv, Args: argValues(x)}
		c.info.Selects[fun] = &Selection{Kind: SelectBuiltin}
		if b.mutates && recv != nil {
			if _, ok := syntacticPath(recv); !ok {
				c.errorf(fun.Name.Pos, CodeType, "%s changes the value; call it on a variable, an attribute or a component", name)
			} else if _, isConst := c.info.Uses[identOf(recv)].(*Constant); isConst {
				c.errorf(fun.Name.Pos, CodeType, "%s cannot change a constant", name)
			}
		}
		for _, a := range x.Args {
			if _, ok := a.Value.(*syntax.FuncExpr); ok {
				e.call()
			}
		}
		return result
	}
	if en, ok := recvType.(*Enum); ok {
		switch {
		case recv == nil && name == "PARSE":
			c.args(f, sc, x, &Func{Params: []Type{en.Base}}, e, nil)
			return record(builtinSig{id: EnumParse}, Opt(en))
		case recv != nil && name == "VALUE":
			c.args(f, sc, x, &Func{}, e, nil)
			return record(builtinSig{id: EnumValueOf}, en.Base)
		case recv != nil && name == "NAME":
			c.args(f, sc, x, &Func{}, e, nil)
			return record(builtinSig{id: EnumName}, TypString)
		}
		return fail()
	}
	var table map[string]builtinSig
	switch r := recvType.(type) {
	case *Array:
		table = arrayBuiltins
		b, ok := table[name]
		if !ok {
			return fail()
		}
		switch name {
		case "MAP":
			if len(x.Args) != 1 {
				c.args(f, sc, x, &Func{Params: []Type{&Func{Params: []Type{r.Elem}}}}, e, nil)
				return TypInvalid
			}
			ft := c.expr(f, sc, x.Args[0].Value, e, &Func{Params: []Type{r.Elem}})
			fn, ok := ft.(*Func)
			if !ok || len(fn.Params) != 1 || !Identical(fn.Params[0], r.Elem) || fn.Result == TypVoid || len(fn.Raises) > 0 {
				c.errorf(x.Args[0].Value.Start(), CodeType, "MAP needs a function (x: %s) returns T without raises", r.Elem)
				return TypInvalid
			}
			return record(b, &Array{Elem: fn.Result})
		case "SORT_BY":
			if len(x.Args) != 1 {
				c.args(f, sc, x, &Func{Params: []Type{&Func{Params: []Type{r.Elem}}}}, e, nil)
				return TypInvalid
			}
			ft := c.expr(f, sc, x.Args[0].Value, e, &Func{Params: []Type{r.Elem}})
			fn, ok := ft.(*Func)
			if !ok || len(fn.Params) != 1 || !Identical(fn.Params[0], r.Elem) || !ordered(fn.Result) || len(fn.Raises) > 0 {
				c.errorf(x.Args[0].Value.Start(), CodeType, "SORT_BY needs a function (x: %s) returns a string, number, date or timestamp", r.Elem)
				return TypInvalid
			}
			return record(b, r)
		case "LOOKUP":
			st, isStruct := r.Elem.(*Struct)
			if r.Name == "" || !r.Unique || !isStruct {
				c.errorf(fun.Name.Pos, CodeType, "LOOKUP needs an array type of the schema with a unique key")
				c.args(f, sc, x, &Func{}, e, nil)
				return TypInvalid
			}
			named := map[string]Type{}
			for _, k := range r.Key {
				if fld := st.Field(k); fld != nil {
					named[k] = NonOpt(fld.Type)
				}
			}
			c.args(f, sc, x, nil, e, named)
			for _, a := range x.Args {
				if a.Name != nil {
					c.useField(a.Name.Pos, st, a.Name.Name)
				}
			}
			var given []string
			for _, a := range x.Args {
				if a.Name != nil {
					given = append(given, a.Name.Name)
				}
			}
			if !sameSet(given, r.Key) {
				c.errorf(fun.Name.Pos, CodeType, "LOOKUP needs exactly the key %s", strings.Join(r.Key, ", "))
			}
			res := record(b, Opt(r.Elem))
			call := c.info.Calls[x]
			call.Args, call.Named = nil, nil
			// Arguments in key order.
			for _, k := range r.Key {
				for _, a := range x.Args {
					if a.Name != nil && a.Name.Name == k {
						call.Args = append(call.Args, a.Value)
						call.Named = append(call.Named, k)
					}
				}
			}
			return res
		case "SORT":
			if !ordered(NonOpt(r.Elem)) {
				c.errorf(fun.Name.Pos, CodeType, "SORT orders strings, numbers, dates and timestamps; use SORT_BY for %s", r.Elem)
			}
		case "JOIN":
			if !IsKind(r.Elem, String) || IsOptional(r.Elem) {
				c.errorf(fun.Name.Pos, CodeType, "JOIN needs an array of STRING")
			}
		case "CONTAINS", "INDEX_OF":
			if !comparable(r.Elem) {
				c.errorf(fun.Name.Pos, CodeType, "%s compares elements, which %s cannot", name, r.Elem)
			}
		}
		params, result := b.sig(r)
		c.args(f, sc, x, &Func{Params: params}, e, nil)
		return record(b, result)
	case *Map:
		table = mapBuiltins
	default:
		switch {
		case IsKind(recvType, String):
			table = stringBuiltins
		case IsKind(recvType, Bytes):
			table = map[string]builtinSig{"LENGTH": {BytesLength, false, fixed(nil, TypInt8)}}
		case IsKind(recvType, Date):
			table = dateBuiltins
		case IsKind(recvType, Timestamp):
			table = map[string]builtinSig{"DATE": {TsDate, false, fixed(nil, TypDate)}}
		}
	}
	b, ok := table[name]
	if !ok {
		return fail()
	}
	params, result := b.sig(recvType)
	c.args(f, sc, x, &Func{Params: params}, e, nil)
	return record(b, result)
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
