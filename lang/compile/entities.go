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

package compile

import (
	"github.com/tim-riep/go-runtime/lang/bytecode"
	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/lang/types"
)

// entityStmt adds an entity statement to the table of the program.
func (f *fn) entityStmt(kind bytecode.EntityKind, info *types.EntityStatement) *bytecode.EntityStmt {
	e := bytecode.EntityStmt{Kind: kind, Local: info.Local, Result: -1}
	if info.BusinessObject != nil {
		e.BusinessObject = info.BusinessObject.Name
	}
	if info.Result != nil {
		e.Result = f.c.typeDesc(info.Result.Type)
	}
	f.c.out.Entities = append(f.c.out.Entities, e)
	return &f.c.out.Entities[len(f.c.out.Entities)-1]
}

func (f *fn) entityStep(st *types.EntityStep) bytecode.EntityStep {
	out := bytecode.EntityStep{Op: st.Op, ByParent: st.ByParent, Fields: st.Fields, Rows: -1, Param: -1, Into: -1}
	if st.Entity != nil {
		out.Entity = st.Entity.Table
	}
	if st.Action != nil {
		out.Action = st.Action.Name
	}
	return out
}

// modifyStmt pushes the values of the steps, runs the statement and
// stores the results of actions and the result.
func (f *fn) modifyStmt(s *syntax.ModifyStmt) {
	info := f.c.info.Entities[s]
	var steps []bytecode.EntityStep
	var intos []*types.Local
	for i, st := range info.Steps {
		bs := f.entityStep(st)
		src := s.Steps[i]
		for _, x := range []syntax.Expr{src.From, src.Keys} {
			if x != nil {
				f.expr(x)
				bs.Rows = f.c.typeDesc(f.c.info.Types[x])
			}
		}
		if src.Param != nil {
			f.valueOwn(src.Param)
			bs.Param = f.c.typeDesc(st.Param)
		}
		if st.Into != nil {
			bs.Into = f.c.typeDesc(st.IntoType)
			intos = append(intos, st.Into)
		}
		steps = append(steps, bs)
	}
	f.line(s.Pos)
	e := f.entityStmt(bytecode.EntityModify, info)
	e.Steps = steps
	f.emit(bytecode.ENTITY, len(f.c.out.Entities)-1)
	if info.Result != nil {
		f.storeLocal(info.Result, true)
	}
	for i := len(intos) - 1; i >= 0; i-- {
		f.storeLocal(intos[i], true)
	}
}

// readExpr pushes the keys, runs the statement, stores the result and
// leaves the rows.
func (f *fn) readExpr(x *syntax.ReadExpr) {
	info := f.c.info.Entities[x]
	bs := f.entityStep(info.Read)
	f.expr(x.Keys)
	bs.Rows = f.c.typeDesc(f.c.info.Types[x.Keys])
	bs.Into = f.c.typeDesc(f.c.info.Types[x])
	f.line(x.Pos)
	e := f.entityStmt(bytecode.EntityRead, info)
	e.Steps = []bytecode.EntityStep{bs}
	f.emit(bytecode.ENTITY, len(f.c.out.Entities)-1)
	if info.Result != nil {
		f.storeLocal(info.Result, true)
	}
}

// saveStmt runs commit or rollback business objects.
func (f *fn) saveStmt(s *syntax.SaveStmt) {
	info := f.c.info.Entities[s]
	kind := bytecode.EntitySave
	if s.Rollback {
		kind = bytecode.EntityDiscard
	}
	f.entityStmt(kind, info)
	f.emit(bytecode.ENTITY, len(f.c.out.Entities)-1)
	if info.Result != nil {
		f.storeLocal(info.Result, true)
	}
}
