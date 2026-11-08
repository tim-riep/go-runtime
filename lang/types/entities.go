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
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// EntityResultStruct is the structure of the result of entity statements.
const EntityResultStruct = "/TRBASE/ENTITY_RESULT"

// EntityStatement describes modify business object, read business object
// or commit and rollback business objects for the compiler.
type EntityStatement struct {
	BusinessObject *model.BusinessObject
	Local          bool
	Steps          []*EntityStep // modify
	Read           *EntityStep   // read
	Save, Discard  bool          // commit and rollback business objects
	// Result is the declared variable of the result, nil without.
	Result *Local
}

// EntityStep is a step of modify business object or the line of read.
type EntityStep struct {
	Op       string // create, update, delete, execute or read
	Entity   *model.Entity
	Table    *Struct // the row type of the table
	ByParent bool
	// Fields are the fields that the step sets or reads, in source order.
	Fields []string
	All    bool
	// Rows is the type of the array of From or Keys.
	Rows Type
	// Action and Param describe execute; Into is the declared variable of
	// the result of the action and IntoType its type.
	Action   *model.Action
	Param    Type
	Into     *Local
	IntoType Type
	// RowType is the type of the rows that read returns.
	RowType *Struct
}

// isBehaviorClass reports whether a class is the behavior class of a
// business object, and returns it.
func (c *checker) behaviorOf(k *Class) *model.BusinessObject {
	if k == nil || c.cfg.Schema == nil {
		return nil
	}
	for _, name := range c.cfg.Schema.BusinessObjectNames() {
		if b := c.cfg.Schema.BusinessObjects[name]; b.BehaviorClass == k.Name {
			return b
		}
	}
	return nil
}

// isValidation reports whether a method of a behavior class is a
// validation, which must not change the buffer.
func isValidation(b *model.BusinessObject, m *Method) bool {
	if b == nil || m == nil {
		return false
	}
	for _, e := range b.Entities {
		for _, v := range e.Validations {
			if v.Name == m.Name {
				return true
			}
		}
	}
	return false
}

// businessObjectOf returns the business object of an entity statement
// that the code may use, or nil.
func (c *checker) businessObjectOf(f *fnCtx, n syntax.Name, local bool) *model.BusinessObject {
	c.checkGlobalName(n)
	var b *model.BusinessObject
	if c.cfg.Schema != nil {
		b = c.cfg.Schema.BusinessObjects[n.Name]
	}
	if b == nil {
		c.errorf(n.Pos, CodeBusinessObject, "business object %s does not exist", n.Name)
		return nil
	}
	if !c.visibleNamespace(n) {
		return nil
	}
	if local && (f.class == nil || f.class.Name != b.BehaviorClass) {
		c.errorf(n.Pos, CodeBusinessObject, "in local mode is only allowed in the behavior class %s of %s", b.BehaviorClass, b.Name)
	}
	c.use(n.Pos, "business-object", b.Name, "")
	return b
}

// entityOf returns the entity of a step.
func (c *checker) entityOf(b *model.BusinessObject, n syntax.Name) (*model.Entity, *Struct) {
	c.checkGlobalName(n)
	e := b.Entity(n.Name)
	if e == nil {
		var names []string
		for _, x := range b.Entities {
			names = append(names, x.Table)
		}
		c.errorf(n.Pos, CodeBusinessObject, "%s is no entity of %s; its entities are %s", n.Name, b.Name, strings.Join(names, ", "))
		return nil, nil
	}
	c.use(n.Pos, "entity", e.Table, "")
	return e, c.schema.structType(e.Table, n.Pos)
}

// resultLocal declares the variable of the result of entity statements.
func (c *checker) resultLocal(f *fnCtx, sc *scope, id *syntax.Ident) *Local {
	if id == nil {
		return nil
	}
	t := c.schema.lookup(EntityResultStruct, id.Pos)
	if t == nil || !c.visiblePackage(EntityResultStruct) {
		c.errorf(id.Pos, CodeBusinessObject, "the result of entity statements is %s; the package needs TRBASE", EntityResultStruct)
		t = TypInvalid
	}
	return c.declare(f, sc, *id, t)
}

// rowsOf checks that an expression is an array of a struct and returns
// the struct.
func (c *checker) rowsOf(f *fnCtx, sc *scope, x syntax.Expr, e env, what string) (Type, *Struct) {
	t := c.expr(f, sc, x, e, nil)
	if t == TypInvalid {
		return t, nil
	}
	c.notNull(x, t)
	a, ok := NonOpt(t).(*Array)
	if !ok {
		c.errorf(x.Start(), CodeType, "%s is %s; it needs an array of rows", what, t)
		return t, nil
	}
	st, ok := NonOpt(a.Elem).(*Struct)
	if !ok {
		c.errorf(x.Start(), CodeType, "%s is %s; its elements need to be structures", what, t)
		return t, nil
	}
	return t, st
}

// rowHas checks that the rows give a field with a type assignable to the
// field of the entity.
func (c *checker) rowHas(rows *Struct, table *Struct, field string, pos syntax.Pos, why string) bool {
	if rows == nil || table == nil {
		return true
	}
	want := table.Field(field)
	got := rows.Field(field)
	switch {
	case want == nil:
		return true
	case got == nil:
		c.errorf(pos, CodeType, "the rows of %s have no component %s, which %s", rows.Name, field, why)
		return false
	case !assignable(NonOpt(got.Type), NonOpt(want.Type)):
		c.errorf(pos, CodeType, "the component %s of %s is %s, but %s needs %s", field, rows.Name, got.Type, table.Name, want.Type)
		return false
	}
	return true
}

// modifyStmt checks modify business object.
func (c *checker) modifyStmt(f *fnCtx, sc *scope, s *syntax.ModifyStmt, e env) {
	info := &EntityStatement{Local: s.Local}
	c.info.Entities[s] = info
	b := c.businessObjectOf(f, s.BusinessObject, s.Local)
	info.BusinessObject = b
	if bo := c.behaviorOf(f.class); bo != nil && isValidation(bo, f.method) {
		c.errorf(s.Pos, CodeBusinessObject, "%s is a validation, which must not change the transaction buffer", f.method.Name)
	}
	for _, st := range s.Steps {
		step := &EntityStep{Op: st.Op, ByParent: st.ByParent}
		info.Steps = append(info.Steps, step)
		var table *Struct
		if b != nil {
			step.Entity, table = c.entityOf(b, st.Entity)
			step.Table = table
		}
		switch st.Op {
		case "create", "update":
			step.Rows = TypInvalid
			var rows *Struct
			step.Rows, rows = c.rowsOf(f, sc, st.From, e, "from")
			c.entityFields(st, step, table, rows, s.Local)
		case "delete":
			var rows *Struct
			step.Rows, rows = c.rowsOf(f, sc, st.Keys, e, "keys")
			if step.Entity != nil && !step.Entity.Allows(model.OperationDelete) {
				c.errorf(st.Entity.Pos, CodeBusinessObject, "%s does not allow delete", st.Entity.Name)
			}
			c.keyRows(step, table, rows, st.Keys.Start())
		case "execute":
			c.executeStep(f, sc, st, step, table, e)
		}
	}
	info.Result = c.resultLocal(f, sc, s.Result)
	e.call()
}

// entityFields checks the field list of create and update: fields of the
// entity, each once, writable by the code, and given by the rows.
func (c *checker) entityFields(st *syntax.ModifyStep, step *EntityStep, table *Struct, rows *Struct, local bool) {
	en := step.Entity
	op := model.EntityOperation(st.Op)
	if en != nil && !en.Allows(op) {
		c.errorf(st.Entity.Pos, CodeBusinessObject, "%s does not allow %s", st.Entity.Name, op)
	}
	if en != nil && op == model.OperationCreate {
		switch {
		case st.ByParent && !en.Parent.Valid():
			c.errorf(st.Entity.Pos, CodeBusinessObject, "%s is the root; create it without by parent", st.Entity.Name)
		case !st.ByParent && en.Parent.Valid():
			c.errorf(st.Entity.Pos, CodeBusinessObject, "%s is a dependent entity; create it by parent", st.Entity.Name)
		}
	}
	var fromParent []string
	if en != nil && st.ByParent {
		if t := c.cfg.Schema.Tables[en.Table]; t != nil {
			if fk := t.ForeignKey(en.ForeignKey.Or("")); fk != nil {
				fromParent = fk.FieldNames()
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range st.Fields {
		name := id.Name
		step.Fields = append(step.Fields, name)
		if table == nil || en == nil {
			continue
		}
		if !c.fieldVisible(name, id.Pos) {
			continue
		}
		fd := table.Field(name)
		isKey := slices.Contains(table.Key, name)
		props := en.Property(name)
		switch {
		case fd == nil:
			c.errorf(id.Pos, CodeUnknown, "%s has no field %s", en.Table, name)
			continue
		case seen[name]:
			c.errorf(id.Pos, CodeDuplicate, "the field %s is given twice", name)
		case slices.Contains(fromParent, name):
			c.errorf(id.Pos, CodeBusinessObject, "%s comes from the parent instance", name)
		case op == model.OperationCreate && isKey && en.Numbering == model.NumberingUUID:
			c.errorf(id.Pos, CodeBusinessObject, "the kernel gives the key %s of %s, which has numbering uuid", name, en.Table)
		case en.Administered(name):
			c.errorf(id.Pos, CodeBusinessObject, "the kernel sets %s when it saves", name)
		case !local && props.ReadOnly:
			c.errorf(id.Pos, CodeBusinessObject, "%s is read-only; only the behavior class of the business object sets it in local mode", name)
		case op == model.OperationUpdate && isKey:
			c.errorf(id.Pos, CodeBusinessObject, "%s is a key field and cannot change", name)
		case op == model.OperationUpdate && !local && props.ReadOnlyOnUpdate:
			c.errorf(id.Pos, CodeBusinessObject, "%s is set only when an instance is created", name)
		}
		seen[name] = true
		c.useField(id.Pos, table, name)
		c.use(id.Pos, "entity", en.Table, name)
		c.rowHas(rows, table, name, id.Pos, "the field list names")
	}
	if table == nil || en == nil {
		return
	}
	pos := st.Entity.Pos
	switch {
	case op == model.OperationCreate && en.Numbering == model.NumberingExternal:
		for _, k := range table.Key {
			if !seen[k] && !slices.Contains(fromParent, k) {
				c.errorf(pos, CodeBusinessObject, "the key field %s of %s is missing in the field list", k, en.Table)
			}
		}
		if st.ByParent && rows != nil && rows.Field(model.ParentLinkField) == nil {
			for _, k := range fromParent {
				c.rowHas(rows, table, k, pos, "names the parent")
			}
		}
	case op == model.OperationCreate && st.ByParent && rows != nil && rows.Field(model.ParentLinkField) == nil:
		for _, k := range fromParent {
			c.rowHas(rows, table, k, pos, "names the parent")
		}
	case op == model.OperationUpdate:
		c.keyRows(step, table, rows, pos)
	}
}

// keyRows checks that rows give the key fields of an entity.
func (c *checker) keyRows(step *EntityStep, table *Struct, rows *Struct, pos syntax.Pos) {
	if table == nil {
		return
	}
	for _, k := range table.Key {
		c.rowHas(rows, table, k, pos, "is part of the key")
	}
}

// executeStep checks execute ENTITY.ACTION [keys ...] [parameter ...]
// [into x].
func (c *checker) executeStep(f *fnCtx, sc *scope, st *syntax.ModifyStep, step *EntityStep, table *Struct, e env) {
	var a *model.Action
	if step.Entity != nil {
		a = step.Entity.Action(st.Action.Name)
		if a == nil {
			var names []string
			for _, x := range step.Entity.Actions {
				names = append(names, x.Name)
			}
			c.errorf(st.Action.Pos, CodeBusinessObject, "%s has no action %s; its actions are %s", step.Entity.Table, st.Action.Name, strings.Join(names, ", "))
		} else {
			c.use(st.Action.Pos, "action", step.Entity.Table, a.Name)
		}
	}
	step.Action = a
	if bo := c.behaviorOf(f.class); a != nil && a.Modifies && bo != nil && isValidation(bo, f.method) {
		c.errorf(st.Action.Pos, CodeBusinessObject, "%s is a validation, which must not run %s, which changes instances", f.method.Name, a.Name)
	}
	switch {
	case st.Keys != nil:
		var rows *Struct
		step.Rows, rows = c.rowsOf(f, sc, st.Keys, e, "keys")
		if a != nil && !a.Instance {
			c.errorf(st.Keys.Start(), CodeBusinessObject, "%s is a static action and takes no keys", a.Name)
		}
		c.keyRows(step, table, rows, st.Keys.Start())
	case a != nil && a.Instance:
		c.errorf(st.Action.Pos, CodeBusinessObject, "%s is an instance action and needs keys", a.Name)
	}
	var param Type
	if a != nil {
		if p, ok := a.Parameter.Get(); ok {
			param = c.structOrInvalid(p)
		}
	}
	switch {
	case st.Param != nil:
		t := c.expr(f, sc, st.Param, e, param)
		if a != nil && param == nil {
			c.errorf(st.Param.Start(), CodeBusinessObject, "%s takes no parameter", a.Name)
		} else if param != nil {
			c.notNull(st.Param, t)
			c.assign(st.Param, t, param, "the parameter")
		}
	case param != nil:
		c.errorf(st.Action.Pos, CodeBusinessObject, "%s needs a parameter of type %s", a.Name, param)
	}
	step.Param = param
	if st.Into == nil {
		return
	}
	var into Type = TypInvalid
	if a != nil {
		r, ok := a.Result.Get()
		switch {
		case !ok:
			c.errorf(st.Into.Pos, CodeBusinessObject, "%s has no result", a.Name)
		case r == model.ResultSelf:
			into = &Array{Elem: table}
		case a.Instance:
			into = &Array{Elem: c.structOrInvalid(r)}
		default:
			into = c.structOrInvalid(r)
		}
	}
	step.IntoType = into
	step.Into = c.declare(f, sc, *st.Into, into)
}

// readExpr checks read business object and returns the type of its rows.
func (c *checker) readExpr(f *fnCtx, sc *scope, x *syntax.ReadExpr, e env) Type {
	info := &EntityStatement{Local: x.Local}
	c.info.Entities[x] = info
	b := c.businessObjectOf(f, x.BusinessObject, x.Local)
	info.BusinessObject = b
	step := &EntityStep{Op: "read", ByParent: x.ByParent, All: x.All}
	info.Read = step
	var table *Struct
	if b != nil {
		step.Entity, table = c.entityOf(b, x.Entity)
		step.Table = table
	}
	var rows *Struct
	step.Rows, rows = c.rowsOf(f, sc, x.Keys, e, "keys")
	en := step.Entity
	if en != nil && table != nil {
		switch {
		case x.ByParent && !en.Parent.Valid():
			c.errorf(x.Entity.Pos, CodeBusinessObject, "%s is the root and has no parent", en.Table)
		case x.ByParent:
			parent := c.schema.structType(en.Parent.Or(""), x.Entity.Pos)
			c.keyRows(step, parent, rows, x.Keys.Start())
		default:
			c.keyRows(step, table, rows, x.Keys.Start())
		}
	}
	var out *Struct
	switch {
	case table == nil:
	case x.All:
		out = table
		for _, fd := range table.Fields {
			step.Fields = append(step.Fields, fd.Name)
		}
	default:
		fields := slices.Clone(table.Key)
		seen := map[string]bool{}
		for _, id := range x.Fields {
			switch {
			case !c.fieldVisible(id.Name, id.Pos):
			case table.Field(id.Name) == nil:
				c.errorf(id.Pos, CodeUnknown, "%s has no field %s", table.Name, id.Name)
			case seen[id.Name]:
				c.errorf(id.Pos, CodeDuplicate, "the field %s is given twice", id.Name)
			default:
				c.useField(id.Pos, table, id.Name)
				c.use(id.Pos, "entity", table.Name, id.Name)
				if !slices.Contains(fields, id.Name) {
					fields = append(fields, id.Name)
				}
			}
			seen[id.Name] = true
		}
		out = c.schema.projection(table, fields)
		step.Fields = fields
	}
	step.RowType = out
	info.Result = c.resultLocal(f, sc, x.Result)
	e.call()
	if out == nil {
		return TypInvalid
	}
	return &Array{Elem: out}
}

// saveStmt checks commit and rollback business objects, which behavior
// classes do not run.
func (c *checker) saveStmt(f *fnCtx, sc *scope, s *syntax.SaveStmt, e env) {
	info := &EntityStatement{Save: !s.Rollback, Discard: s.Rollback}
	c.info.Entities[s] = info
	if b := c.behaviorOf(f.class); b != nil {
		c.errorf(s.Pos, CodeBusinessObject, "the behavior class of %s does not commit or roll back business objects; the caller does", b.Name)
	}
	info.Result = c.resultLocal(f, sc, s.Result)
	e.call()
}
