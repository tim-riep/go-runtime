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
	"maps"
	"slices"
	"strings"

	"github.com/tim-riep/go-runtime/lang/syntax"
	"github.com/tim-riep/go-runtime/model"
)

// The exceptions of transform, from the kernel package TRBASE.
const (
	XSLTFailed    = "/TRBASE/XSLT_FAILED"
	ResultInvalid = "/TRBASE/TRANSFORM_RESULT_INVALID"
)

// Transform describes a transform expression.
type Transform struct {
	Name string // the transformation
	// Params are the names of the parameters in the order of their values
	// on the stack, ParamTypes their types.
	Params     []string
	ParamTypes []Type
	// Source is the type of the source; SourceXML tells that it is XML
	// text, STRING or BYTES, rather than a value in the canonical format.
	Source    Type
	SourceXML bool
	Into      Type // STRING without into
	// Format is xml for stylesheets, or the format of a native
	// transformation such as json.
	Format string
}

// transform checks transform SOURCE using /PKG/T parameters ... into T.
func (c *checker) transform(f *fnCtx, sc *scope, x *syntax.TransformExpr, e env) Type {
	info := &Transform{Name: x.Using.Name, Into: TypString, Format: "xml"}
	src := c.expr(f, sc, x.Source, e, nil)
	c.notNull(x.Source, src)
	info.Source = NonOpt(src)
	if IsKind(info.Source, String) || IsKind(info.Source, Bytes) {
		info.SourceXML = true
	} else if src != TypInvalid && !c.serializable(info.Source) {
		c.errorf(x.Source.Start(), CodeType, "transform needs XML as STRING or BYTES or a value of a structure, table, array or simple type, not %s", src)
	}
	if x.Into != nil {
		info.Into = c.resolveType(x.Into)
		if info.Into != TypInvalid && !c.serializable(info.Into) {
			c.errorf(x.Into.Pos, CodeType, "transform reads its result into a structure, table, array or simple type, not %s", info.Into)
		}
	}

	c.checkGlobalName(x.Using)
	t := c.cfg.Schema.Transformations[x.Using.Name]
	switch {
	case t == nil:
		c.errorf(x.Using.Pos, CodeUnknown, "%s is not a transformation", x.Using.Name)
	case !c.visiblePackage(x.Using.Name):
		c.visibleNamespace(x.Using)
	default:
		c.use(x.Using.Pos, "transformation", x.Using.Name, "")
		info.Format = t.Native.Or("xml")
	}
	if info.Format != "xml" && info.Format != model.TransformJSON {
		return c.nativeTransform(f, sc, x, e, info)
	}
	seen := map[string]bool{}
	for _, pv := range x.Params {
		name := pv.Name.Name
		if seen[name] {
			c.errorf(pv.Name.Pos, CodeDuplicate, "the parameter %s is given twice", name)
		}
		seen[name] = true
		if t != nil {
			if _, ok := t.Param(name); !ok {
				var names []string
				for _, p := range t.Params {
					names = append(names, p.Name)
				}
				list := "it has none"
				if len(names) > 0 {
					list = "its parameters are " + strings.Join(names, ", ")
				}
				c.errorf(pv.Name.Pos, CodeUnknown, "%s has no top-level xsl:param %s; %s", t.Name, name, list)
			}
		}
		vt := c.expr(f, sc, pv.Value, e, nil)
		if vt != TypInvalid && !isSimple(vt) {
			c.errorf(pv.Value.Start(), CodeType, "the value of the parameter %s must be of a simple type, not %s", name, vt)
		}
		info.Params = append(info.Params, name)
		info.ParamTypes = append(info.ParamTypes, vt)
	}
	if t != nil {
		for _, p := range t.Params {
			if p.Required && !seen[p.Name] {
				c.errorf(x.Pos, CodeType, "%s needs the parameter %s", t.Name, p.Name)
			}
		}
	}
	// transform raises /TRBASE/XSLT_FAILED, a checked exception of TRBASE.
	if k := c.classes[XSLTFailed]; k != nil && c.visiblePackage(XSLTFailed) {
		c.raise(f, k, x.Pos)
	} else {
		c.errorf(x.Pos, CodeVisibility, "transform raises %s; the package needs TRBASE in requires", XSLTFailed)
	}
	c.info.Transforms[x] = info
	e.call()
	return info.Into
}

// serializable reports values that transform reads and writes in the
// canonical XML format: built-in types, enums, structures, tables, arrays
// and maps of them.
func (c *checker) serializable(t Type) bool { return Serializable(t) }

// isSimple reports built-in types, named types and enums, also optional.
func isSimple(t Type) bool {
	switch x := NonOpt(t).(type) {
	case *Basic:
		return x.Kind != Void && x.Kind != Invalid && x.Kind != Session && x.Kind != Null
	case *Named, *Enum:
		return true
	}
	return t == TypNull
}

// nativeTransform checks a transformation that a kernel package
// implements natively, such as xlsx: its parameters, the shapes of source
// and result and the exceptions, as the registry of the kernel states
// them.
func (c *checker) nativeTransform(f *fnCtx, sc *scope, x *syntax.TransformExpr, e env, info *Transform) Type {
	var params map[string][]string
	var shapes [][2]string
	var raises []string
	ok := false
	if c.cfg.Natives != nil {
		params, shapes, raises, ok = c.cfg.Natives.NativeTransform(info.Format)
	}
	if !ok {
		c.errorf(x.Using.Pos, CodeNative, "the kernel has no native transformation of format %s", info.Format)
	}
	seen := map[string]bool{}
	for _, pv := range x.Params {
		name := pv.Name.Name
		if seen[name] {
			c.errorf(pv.Name.Pos, CodeDuplicate, "the parameter %s is given twice", name)
		}
		seen[name] = true
		vt := c.expr(f, sc, pv.Value, e, nil)
		if vt != TypInvalid && !isSimple(vt) {
			c.errorf(pv.Value.Start(), CodeType, "the value of the parameter %s must be of a simple type, not %s", name, vt)
		}
		info.Params = append(info.Params, name)
		info.ParamTypes = append(info.ParamTypes, vt)
		if !ok {
			continue
		}
		values, known := params[name]
		if !known {
			names := slices.Sorted(maps.Keys(params))
			c.errorf(pv.Name.Pos, CodeUnknown, "%s has no parameter %s; its parameters are %s", x.Using.Name, name, strings.Join(names, ", "))
			continue
		}
		if lit, isLit := literal(pv.Value); isLit && len(values) > 0 && !slices.Contains(values, lit) {
			c.errorf(pv.Value.Start(), CodeType, "the parameter %s of %s takes %s, not %q", name, x.Using.Name, strings.Join(values, ", "), lit)
		}
	}
	if ok {
		src, into := shapeOf(info.Source, false), shapeOf(info.Into, true)
		match := false
		var allowed []string
		for _, s := range shapes {
			match = match || s[0] == src && s[1] == into
			allowed = append(allowed, s[0]+" into "+s[1])
		}
		if !match && info.Source != TypInvalid && info.Into != TypInvalid {
			c.errorf(x.Pos, CodeType, "%s transforms %s; %s into %s is not possible (an array type of the schema with simple components, or an array of a structure with simple components)",
				x.Using.Name, strings.Join(allowed, " or "), describeShape(info.Source), describeShape(info.Into))
		}
		for _, r := range raises {
			if k := c.classes[r]; k != nil && c.visiblePackage(r) {
				c.raise(f, k, x.Pos)
			} else {
				c.errorf(x.Pos, CodeVisibility, "transform with %s raises %s; the package needs its package in requires", x.Using.Name, r)
			}
		}
	}
	c.info.Transforms[x] = info
	e.call()
	return info.Into
}

// shapeOf classifies a type for native transformations: bytes, an array
// of a flat structure, or an array type of the schema with a flat row.
func shapeOf(t Type, into bool) string {
	if IsKind(t, Bytes) {
		return "bytes"
	}
	a, ok := NonOpt(t).(*Array)
	if !ok {
		return ""
	}
	st, ok := NonOpt(a.Elem).(*Struct)
	if !ok || !flat(st) {
		return ""
	}
	if into {
		if a.Name == "" {
			return ""
		}
		return "array type"
	}
	return "rows"
}

func flat(st *Struct) bool {
	for _, f := range st.Fields {
		if !isSimple(f.Type) {
			return false
		}
	}
	return true
}

func describeShape(t Type) string {
	if a, ok := NonOpt(t).(*Array); ok {
		if st, ok := NonOpt(a.Elem).(*Struct); ok && !flat(st) {
			return t.String() + " (with components that are not simple)"
		}
		if a.Name == "" {
			return t.String() + " (no array type of the schema)"
		}
	}
	return t.String()
}
