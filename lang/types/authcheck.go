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
	"unicode/utf8"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// AuthCheck describes check authorization or authorized.
type AuthCheck struct {
	Object string
	// Fields are in source order; their values are evaluated in this
	// order.
	Fields []AuthCheckField
}

// AuthCheckField is a field of an authorization check.
type AuthCheckField struct {
	Name  string
	Any   bool
	Type  Type        // the type of the field
	Value syntax.Expr // nil for any and for a bare activity
	// Activity is set for a bare activity such as UPDATE: the enum and the
	// index of the constant.
	Activity      *Enum
	ActivityIndex int
	// Column is the field of the table whose values a filter in a where
	// condition checks.
	Column string
}

// authCheck checks the object and the fields of an authorization check.
// In a where condition of the table st, upper-case names are its fields.
func (c *checker) authCheck(f *fnCtx, sc *scope, x *syntax.AuthCheck, st *Struct, e env) {
	info := &AuthCheck{Object: x.Object.Name}
	c.info.AuthChecks[x] = info
	c.checkGlobalName(x.Object)
	obj := c.cfg.Schema.AuthorizationObjects[x.Object.Name]
	switch {
	case obj == nil:
		c.errorf(x.Object.Pos, CodeAuthorization, "authorization object %s does not exist", x.Object.Name)
	case !c.visibleNamespace(x.Object):
		obj = nil
	default:
		c.use(x.Object.Pos, "authorization-object", obj.Name, "")
	}
	seen := map[string]bool{}
	for _, af := range x.Fields {
		name := af.Name.Name
		field := AuthCheckField{Name: name, Any: af.Any, Type: TypInvalid}
		var def model.AuthorizationField
		known := obj == nil
		if obj != nil {
			def, known = obj.Field(name)
			switch {
			case !known:
				var names []string
				for _, fd := range obj.Fields {
					names = append(names, fd.Name)
				}
				c.errorf(af.Name.Pos, CodeUnknown, "%s has no field %s; its fields are %s", obj.Name, name, strings.Join(names, ", "))
			case seen[name]:
				c.errorf(af.Name.Pos, CodeDuplicate, "the field %s is given twice", name)
			default:
				c.use(af.Name.Pos, "authorization-object", obj.Name, name)
				field.Type = c.authFieldType(def, af.Name.Pos)
			}
			seen[name] = true
		}
		switch {
		case af.Any:
		case st != nil && c.authColumn(st, def, af, &field):
		default:
			c.authValue(f, sc, obj, def, af, &field, e)
		}
		info.Fields = append(info.Fields, field)
	}
	if obj != nil {
		for _, fd := range obj.Fields {
			if !seen[fd.Name] {
				c.errorf(x.Object.Pos, CodeAuthorization, "the check of %s needs the field %s, with a value or any", obj.Name, fd.Name)
			}
		}
	}
}

// authColumn checks a field of the table as the value of a filter: its
// values have the base type of the field, STRING or an integer. A bare
// activity that is no field of the table is not a column.
func (c *checker) authColumn(st *Struct, def model.AuthorizationField, af *syntax.AuthField, field *AuthCheckField) bool {
	x := unparen(af.Value)
	if !isFieldExpr(x) {
		return false
	}
	if id, ok := x.(*syntax.Ident); ok && def.IsActivity() && st.Field(id.Name) == nil {
		if en, ok := field.Type.(*Enum); ok && en.Index(id.Name) >= 0 {
			return false
		}
	}
	ct, _ := c.field(st, x)
	if ct == TypInvalid || field.Type == TypInvalid {
		return true
	}
	cb, fb := filterBase(ct), filterBase(field.Type)
	switch {
	case cb == nil:
		c.errorf(x.Start(), CodeType, "authorized in a where condition checks STRING and integer fields, not %s", NonOpt(ct))
	case fb == nil || cb.Kind != fb.Kind && !(isInteger(cb) && isInteger(fb)):
		c.errorf(x.Start(), CodeType, "the field %s is %s, but %s of %s is %s", syntax.PrintExpr(x), NonOpt(ct), af.Name.Name, st.Name, field.Type)
	}
	if fr := c.info.Fields[x]; fr != nil {
		field.Column = fr.Field
	}
	return true
}

// filterBase returns the base type of a field that a filter can check:
// STRING, INT4 or INT8, also of enums.
func filterBase(t Type) *Basic {
	var b *Basic
	switch x := NonOpt(t).(type) {
	case *Enum:
		b = x.Base
	default:
		b = BasicOf(x)
	}
	if b == nil || b.Kind != String && !isInteger(b) {
		return nil
	}
	return b
}

func isInteger(b *Basic) bool { return b.Kind == Int4 || b.Kind == Int8 }

// authFieldType returns the type of a field: the enum of the activities
// for ACTIVITY, else the type of its data element or domain.
func (c *checker) authFieldType(def model.AuthorizationField, pos syntax.Pos) Type {
	name := c.cfg.Schema.AuthorizationFieldDomain(def)
	if de, ok := def.DataElement.Get(); ok {
		name = de
	}
	if t := c.schema.lookup(name, pos); t != nil {
		return t
	}
	return TypInvalid
}

// authValue checks the value of a field: a bare activity for ACTIVITY, or
// an expression of the type of the field that is not null.
func (c *checker) authValue(f *fnCtx, sc *scope, obj *model.AuthorizationObject, def model.AuthorizationField, af *syntax.AuthField, field *AuthCheckField, e env) {
	want := field.Type
	if id, ok := af.Value.(*syntax.Ident); ok && def.IsActivity() {
		if en, ok := want.(*Enum); ok && en.Index(id.Name) >= 0 {
			field.Activity, field.ActivityIndex = en, en.Index(id.Name)
			c.activityAllowed(obj, en.Values[field.ActivityIndex].Value, id.Name, id.Pos)
			return
		}
	}
	field.Value = af.Value
	t := c.expr(f, sc, af.Value, e, want)
	if t == TypNull || IsOptional(t) {
		c.notNull(af.Value, t)
		return
	}
	if want == TypInvalid {
		return
	}
	if !c.assign(af.Value, t, want, "the value of "+af.Name.Name) {
		return
	}
	if def.IsActivity() {
		if sel := c.selectedEnum(af.Value); sel != nil {
			v := sel.Enum.Values[sel.EnumIndex]
			c.activityAllowed(obj, v.Value, v.Name, af.Value.Start())
		}
		return
	}
	// A literal is checked against the length of the field now; other
	// values when the check runs.
	if text, ok := literal(af.Value); ok {
		if n, ok := want.(*Named); ok && n.Domain != nil && n.Domain.Length > 0 && utf8.RuneCountInString(text) > n.Domain.Length {
			c.errorf(af.Value.Start(), CodeType, "%q is longer than the %d characters of %s", text, n.Domain.Length, af.Name.Name)
		}
	}
}

// selectedEnum returns the constant of an enum that an expression such as
// /RT/ACTIVITY.READ selects.
func (c *checker) selectedEnum(x syntax.Expr) *Selection {
	if p, ok := x.(*syntax.ParenExpr); ok {
		return c.selectedEnum(p.X)
	}
	s, ok := x.(*syntax.SelectExpr)
	if !ok {
		return nil
	}
	if sel := c.info.Selects[s]; sel != nil && sel.Kind == SelectEnum {
		return sel
	}
	return nil
}

// activityAllowed reports an activity that the object does not allow and
// records the use of an allowed one.
func (c *checker) activityAllowed(obj *model.AuthorizationObject, code, name string, pos syntax.Pos) {
	if obj == nil {
		return
	}
	if !slices.Contains(obj.Activities, code) {
		c.errorf(pos, CodeAuthorization, "%s does not allow the activity %s; it allows %s", obj.Name, name, strings.Join(obj.Activities, ", "))
		return
	}
	c.use(pos, "authorization-object", obj.Name, model.ActivityField+"="+code)
}
