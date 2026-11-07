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

package syntax

import (
	"fmt"
	"strings"
	"unicode"
)

// Format parses src and returns it in canonical form. It fails on syntax
// errors, so that nothing is lost.
func Format(path, src string) (string, []Error) {
	f, errs := Parse(path, src)
	if len(errs) > 0 {
		return "", errs
	}
	return Print(f), nil
}

// Print returns the canonical form of a file: two spaces per block level,
// four more for continuation lines, single spaces around operators, one
// blank line at most, the author's line breaks inside expressions and all
// comments.
func Print(f *File) string {
	p := &printer{}
	p.file(f)
	return p.b.String()
}

// PrintExpr returns the canonical form of an expression on one line.
func PrintExpr(x Expr) string {
	p := &printer{}
	p.expr(x)
	return p.b.String()
}

// PrintType returns the canonical form of a type.
func PrintType(t *Type) string {
	p := &printer{}
	p.typ(t)
	return p.b.String()
}

type printer struct {
	b       strings.Builder
	stmtInd int // indentation of the current statement
	lineInd int // indentation of the current output line
}

func (p *printer) w(s string) { p.b.WriteString(s) }

// newline ends the current line, with a comment if given, and starts the
// next one at indentation ind.
func (p *printer) startLine(ind int) {
	p.w(strings.Repeat(" ", ind))
	p.lineInd = ind
}

func (p *printer) endLine(c *Comment) {
	if c != nil {
		p.w(" ")
		p.w(commentText(c))
	}
	p.w("\n")
}

// commentText returns the canonical form of a comment.
func commentText(c *Comment) string {
	if c.Text == "" || c.Text[0] == ' ' {
		return "#" + c.Text
	}
	return "# " + c.Text
}

func (p *printer) blank() { p.w("\n") }

// leading prints comments on their own lines. allowBlank controls whether
// a blank line before the first one is kept.
func (p *printer) leading(ls []Leading, ind int, allowBlank bool) {
	for i, l := range ls {
		if l.Blank && (i > 0 || allowBlank) {
			p.blank()
		}
		p.startLine(ind)
		p.w(commentText(&l.Comment))
		p.w("\n")
	}
}

// doc prints the blank line and comments before a node. It reports
// whether anything was printed.
func (p *printer) doc(c *Comments, ind int, allowBlank bool) {
	if c.Blank && allowBlank && len(c.Leading) == 0 {
		p.blank()
	}
	if len(c.Leading) > 0 {
		p.leading(c.Leading, ind, allowBlank && (c.Leading[0].Blank || c.Blank))
		if c.Blank {
			p.blank()
		}
	}
}

func (p *printer) file(f *File) {
	if len(f.Header) > 0 {
		p.leading(f.Header, 0, false)
		if f.HeaderBlank {
			p.blank()
		}
	}
	switch {
	case f.Class != nil:
		p.class(f.Class)
	case f.Interface != nil:
		p.iface(f.Interface)
	case f.Include != nil:
		p.include(f.Include)
	}
	var footer []Leading
	blank := false
	for _, l := range f.Footer {
		if l.Comment == nil {
			blank = true
			continue
		}
		footer = append(footer, Leading{Blank: blank, Comment: *l.Comment})
		blank = false
	}
	p.leading(footer, 0, true)
}

func (p *printer) names(ns []Name) {
	for i, n := range ns {
		if i > 0 {
			p.w(", ")
		}
		p.w(n.Name)
	}
}

func (p *printer) class(c *ClassDecl) {
	p.startLine(0)
	p.w(string(c.Visibility))
	if c.Test {
		p.w(" test")
	}
	if c.Open {
		p.w(" open")
	}
	p.w(" class ")
	p.w(c.Name.Name)
	if c.Extends != nil {
		p.w(" extends " + c.Extends.Name)
	}
	if len(c.Implements) > 0 {
		p.w(" implements ")
		p.names(c.Implements)
	}
	if c.Native {
		p.w(" executed by kernel")
	}
	p.endLine(c.Doc.Trailing)
	p.members(c.Members, 2)
	p.declEnd(c.End, len(c.Members) > 0)
}

// declEnd prints the end of a class, interface or include with one blank
// line before it when the body is not empty.
func (p *printer) declEnd(e EndLine, body bool) {
	if body || len(e.Leading) > 0 {
		p.blank()
	}
	p.leading(e.Leading, 2, false)
	p.startLine(0)
	p.w("end")
	p.endLine(e.Trailing)
}

func (p *printer) iface(d *InterfaceDecl) {
	p.startLine(0)
	p.w(string(d.Visibility) + " interface " + d.Name.Name)
	if len(d.Extends) > 0 {
		p.w(" extends ")
		p.names(d.Extends)
	}
	p.endLine(d.Doc.Trailing)
	for i, m := range d.Methods {
		if i == 0 {
			p.blank()
		}
		p.doc(&m.Doc, 2, i > 0)
		p.startLine(2)
		p.stmtInd = 2
		p.sig(m)
		p.endLine(m.Doc.Trailing)
	}
	p.declEnd(d.End, len(d.Methods) > 0)
}

func (p *printer) include(d *IncludeDecl) {
	p.startLine(0)
	p.w(string(d.Kind) + " include " + d.Name.Name)
	p.endLine(d.Doc.Trailing)
	if d.Kind == MembersInclude {
		p.members(d.Members, 2)
		p.declEnd(d.Body.End, len(d.Members) > 0)
		return
	}
	p.stmts(d.Body.Stmts, 2)
	p.blockEnd(d.Body.End, 0, "end")
}

// isBlockMember reports members that span several lines.
func isBlockMember(m Member) bool {
	md, ok := m.(*MethodDecl)
	return ok && !md.Native
}

// members prints class members. A blank line follows the class line and
// surrounds every method with a body; between other members the author's
// blank lines are kept.
func (p *printer) members(ms []Member, ind int) {
	for i, m := range ms {
		doc := m.Comments()
		forced := i == 0 || isBlockMember(m) || isBlockMember(ms[i-1])
		if forced {
			p.blank()
			p.leading(doc.Leading, ind, false)
			if len(doc.Leading) > 0 && doc.Blank {
				p.blank()
			}
		} else {
			p.doc(doc, ind, true)
		}
		p.member(m, ind)
	}
}

func (p *printer) member(m Member, ind int) {
	p.startLine(ind)
	p.stmtInd = ind
	switch m := m.(type) {
	case *IncludeMember:
		p.w("include " + m.Name.Name)
		if m.IfFound {
			p.w(" if found")
		}
		p.endLine(m.Doc.Trailing)
	case *ConstantDecl:
		p.w(string(m.Visibility) + " constant " + m.Name.Name + ": ")
		p.typ(m.Type)
		p.w(" =")
		p.brkOrSpace(m.Break)
		p.expr(m.Value)
		p.endLine(m.Doc.Trailing)
	case *AttributeDecl:
		p.w(string(m.Visibility))
		if m.Static {
			p.w(" static")
		}
		p.w(" attribute " + m.Name.Name + ": ")
		p.typ(m.Type)
		if m.Value != nil {
			p.w(" =")
			p.brkOrSpace(m.Break)
			p.expr(m.Value)
		}
		p.endLine(m.Doc.Trailing)
	case *MethodDecl:
		p.w(string(m.Visibility))
		if m.Static {
			p.w(" static")
		}
		if m.Open {
			p.w(" open")
		}
		if m.Override {
			p.w(" override")
		}
		if m.Test {
			p.w(" test")
		}
		p.w(" ")
		if m.Constructor {
			p.w("constructor")
			p.params(m.Sig.Params, m.Sig.List)
			if len(m.Sig.Raises) > 0 {
				p.w(" raises ")
				p.names(m.Sig.Raises)
			}
		} else {
			p.sig(m.Sig)
		}
		if m.Native {
			p.w(" executed by kernel")
			p.endLine(m.Doc.Trailing)
			return
		}
		p.endLine(m.Doc.Trailing)
		p.stmts(m.Body.Stmts, ind+2)
		p.blockEnd(m.Body.End, ind, "end")
	}
}

func (p *printer) sig(s *MethodSig) {
	p.w("method " + s.Name.Name)
	p.params(s.Params, s.List)
	if s.Returns != nil {
		p.w(" returns ")
		p.typ(s.Returns)
	}
	if len(s.Raises) > 0 {
		p.w(" raises ")
		p.names(s.Raises)
	}
}

func (p *printer) params(ps []*Param, l List) {
	p.w("(")
	p.listOpen(l)
	for i, prm := range ps {
		if i > 0 {
			p.w(",")
			p.brkOrSpace(at(l.Commas, i-1))
		}
		p.w(prm.Name.Name + ": ")
		p.typ(prm.Type)
	}
	p.listClose(l)
	p.w(")")
}

func at(bs []Break, i int) Break {
	if i < len(bs) {
		return bs[i]
	}
	return Break{}
}

// brk continues an expression on the next line if the author broke it.
func (p *printer) brk(b Break) bool {
	if !b.Line {
		return false
	}
	p.endLine(b.Trailing)
	for _, l := range b.Lines {
		if l.Comment != nil {
			p.startLine(p.stmtInd + 4)
			p.w(commentText(l.Comment))
			p.w("\n")
		}
	}
	p.startLine(p.stmtInd + 4)
	return true
}

func (p *printer) brkOrSpace(b Break) {
	if !p.brk(b) {
		p.w(" ")
	}
}

func (p *printer) listOpen(l List) { p.brk(l.Open) }

// listClose puts the closing token on its own line at the indentation of
// the statement if the author did.
func (p *printer) listClose(l List) {
	if !l.Close.Line {
		return
	}
	p.endLine(l.Close.Trailing)
	for _, ln := range l.Close.Lines {
		if ln.Comment != nil {
			p.startLine(p.stmtInd + 4)
			p.w(commentText(ln.Comment))
			p.w("\n")
		}
	}
	p.startLine(p.stmtInd)
}

// stmts prints statements of a block; no blank line follows the opening
// line.
func (p *printer) stmts(ss []Stmt, ind int) {
	for i, s := range ss {
		p.doc(s.Comments(), ind, i > 0)
		p.stmt(s, ind)
	}
}

// blockEnd prints the comments before the closing line of a block, which
// are indented like the block, and the closing keyword.
func (p *printer) blockEnd(e EndLine, ind int, keyword string) {
	p.leading(e.Leading, ind+2, true)
	p.startLine(ind)
	p.w(keyword)
	if keyword == "end" {
		p.endLine(e.Trailing)
	}
}

func (p *printer) stmt(s Stmt, ind int) {
	p.startLine(ind)
	p.stmtInd = ind
	trailing := s.Comments().Trailing
	switch s := s.(type) {
	case *VarStmt:
		p.w("var " + s.Name.Name)
		if s.Type != nil {
			p.w(": ")
			p.typ(s.Type)
		}
		if s.Value == nil {
			break
		}
		p.w(" =")
		p.brkOrSpace(s.Break)
		p.expr(s.Value)
	case *AssignStmt:
		p.expr(s.Target)
		p.w(" =")
		p.brkOrSpace(s.Break)
		p.expr(s.Value)
	case *ExprStmt:
		p.expr(s.X)
	case *IfStmt:
		for i, c := range s.Clauses {
			if i == 0 {
				p.w("if ")
				p.expr(c.Cond)
				p.endLine(trailing)
			}
			p.stmts(c.Body.Stmts, ind+2)
			if i+1 < len(s.Clauses) {
				p.blockEnd(c.Body.End, ind, "elseif ")
				p.stmtInd = ind
				p.expr(s.Clauses[i+1].Cond)
				p.endLine(c.Body.End.Trailing)
			} else if s.Else != nil {
				p.blockEnd(c.Body.End, ind, "else")
				p.endLine(c.Body.End.Trailing)
				p.stmts(s.Else.Stmts, ind+2)
				p.blockEnd(s.Else.End, ind, "end")
			} else {
				p.blockEnd(c.Body.End, ind, "end")
			}
		}
		return
	case *ForStmt:
		p.w("for ")
		if s.Key != nil {
			p.w(s.Key.Name + ", ")
		}
		p.w(s.Var.Name)
		if s.In != nil {
			p.w(" in ")
			p.expr(s.In)
		} else {
			p.w(" from ")
			p.expr(s.From)
			p.w(" to ")
			p.expr(s.To)
			if s.Step != nil {
				p.w(" step ")
				p.expr(s.Step)
			}
		}
		p.body(trailing, s.Body, ind)
		return
	case *WhileStmt:
		p.w("while ")
		p.expr(s.Cond)
		p.body(trailing, s.Body, ind)
		return
	case *BreakStmt:
		p.w("break")
	case *ContinueStmt:
		p.w("continue")
	case *ReturnStmt:
		p.w("return")
		if s.Value != nil {
			p.w(" ")
			p.expr(s.Value)
		}
	case *RaiseStmt:
		p.w("raise " + s.Class.Name)
		for i, a := range s.Args {
			if i == 0 {
				p.w(" with ")
			} else {
				p.w(",")
				p.brkOrSpace(at(s.Comma, i-1))
			}
			p.expr(a)
		}
	case *TryStmt:
		p.w("try")
		p.endLine(trailing)
		p.stmts(s.Body.Stmts, ind+2)
		last := s.Body
		for _, c := range s.Catches {
			p.blockEnd(last.End, ind, "catch "+c.Var.Name+": "+c.Class.Name)
			p.endLine(last.End.Trailing)
			p.stmts(c.Body.Stmts, ind+2)
			last = c.Body
		}
		if s.Finally != nil {
			p.blockEnd(last.End, ind, "finally")
			p.endLine(last.End.Trailing)
			p.stmts(s.Finally.Stmts, ind+2)
			last = s.Finally
		}
		p.blockEnd(last.End, ind, "end")
		return
	case *AssertStmt:
		p.w("assert ")
		p.expr(s.Cond)
		if s.Message != nil {
			p.w(", ")
			p.expr(s.Message)
		}
	case *LogStmt:
		p.w("log " + s.Level + " ")
		p.expr(s.Value)
	case *IncludeStmt:
		p.w("include " + s.Name.Name)
		if s.IfFound {
			p.w(" if found")
		}
	case *DatabaseStmt:
		p.w(s.Keyword)
		if s.Session != nil {
			p.w(" ")
			p.expr(s.Session)
		}
	case *RowStmt:
		p.w(s.Op + " ")
		p.expr(s.Row)
		p.using(s.Using)
	}
	p.endLine(trailing)
}

func (p *printer) body(trailing *Comment, b *Block, ind int) {
	p.endLine(trailing)
	p.stmts(b.Stmts, ind+2)
	p.blockEnd(b.End, ind, "end")
}

func (p *printer) typ(t *Type) {
	switch t.Kind {
	case BuiltinType:
		p.w(t.Name)
		if t.Name == "DEC" {
			p.w(fmt.Sprintf("(%d,%d)", t.Precision, t.Scale))
		}
	case NamedType:
		p.w(t.Name)
	case ArrayType:
		p.w("array of ")
		p.typ(t.Elem)
		return
	case MapType:
		p.w("map of ")
		p.typ(t.Key)
		p.w(" to ")
		p.typ(t.Elem)
		return
	case FuncType:
		p.w("function (")
		for i, prm := range t.Params {
			if i > 0 {
				p.w(", ")
			}
			p.typ(prm)
		}
		p.w(")")
		if t.Returns != nil {
			p.w(" returns ")
			p.typ(t.Returns)
		}
		if len(t.Raises) > 0 {
			p.w(" raises ")
			p.names(t.Raises)
		}
		return
	}
	if t.Optional {
		p.w("?")
	}
}

func (p *printer) args(as []Arg, l List) {
	p.w("(")
	p.listOpen(l)
	for i, a := range as {
		if i > 0 {
			p.w(",")
			p.brkOrSpace(at(l.Commas, i-1))
		}
		if a.Name != nil {
			p.w(a.Name.Name + ": ")
		}
		p.expr(a.Value)
	}
	p.listClose(l)
	p.w(")")
}

func (p *printer) exprs(xs []Expr, l List) {
	for i, x := range xs {
		if i > 0 {
			p.w(",")
			p.brkOrSpace(at(l.Commas, i-1))
		}
		p.expr(x)
	}
}

func (p *printer) expr(x Expr) {
	switch x := x.(type) {
	case *Ident:
		p.w(x.Name)
	case *GlobalExpr:
		p.w(x.Name.Name)
	case *LiteralExpr:
		p.w(x.Raw)
	case *StringExpr:
		p.w(`"`)
		for _, part := range x.Parts {
			if part.Expr != nil {
				p.w("{")
				p.expr(part.Expr)
				p.w("}")
			} else {
				p.w(escape(part.Text))
			}
		}
		p.w(`"`)
	case *MeExpr:
		p.w("me")
	case *SuperExpr:
		p.w("super")
	case *ParenExpr:
		p.w("(")
		p.listOpen(x.List)
		p.expr(x.X)
		p.listClose(x.List)
		p.w(")")
	case *UnaryExpr:
		if x.Op == "not" {
			p.w("not ")
		} else {
			p.w("-")
		}
		p.expr(x.X)
	case *BinaryExpr:
		p.expr(x.X)
		p.w(" " + x.Op)
		p.brkOrSpace(x.Break)
		p.expr(x.Y)
	case *IsExpr:
		p.expr(x.X)
		p.w(" is ")
		if x.Not {
			p.w("not ")
		}
		p.w(x.What)
	case *AsExpr:
		p.expr(x.X)
		p.w(" as ")
		p.typ(x.Type)
	case *CallExpr:
		p.expr(x.Fun)
		p.args(x.Args, x.List)
	case *SelectExpr:
		p.expr(x.X)
		p.w("." + x.Name.Name)
	case *IndexExpr:
		p.expr(x.X)
		p.w("[")
		p.listOpen(x.List)
		p.expr(x.Index)
		p.listClose(x.List)
		p.w("]")
	case *NewExpr:
		p.w("new ")
		if x.Type != nil {
			p.typ(x.Type)
		}
		if x.Call {
			p.args(x.Args, x.List)
		}
	case *ArrayExpr:
		p.w("[")
		p.listOpen(x.List)
		p.exprs(x.Elems, x.List)
		p.listClose(x.List)
		p.w("]")
	case *MapExpr:
		p.w("[")
		p.listOpen(x.List)
		if len(x.Keys) == 0 {
			p.w(":")
		}
		for i := range x.Keys {
			if i > 0 {
				p.w(",")
				p.brkOrSpace(at(x.List.Commas, i-1))
			}
			p.expr(x.Keys[i])
			p.w(": ")
			p.expr(x.Values[i])
		}
		p.listClose(x.List)
		p.w("]")
	case *QueryExpr:
		p.w("select ")
		if x.Mode != "" {
			p.w(x.Mode + " ")
		}
		p.w("from " + x.Table.Name)
		p.where(x.Where)
		for i, o := range x.Order {
			if i == 0 {
				p.w(" order by ")
			} else {
				p.w(", ")
			}
			p.w(o.Field.Name)
			if o.Explicit {
				if o.Descending {
					p.w(" descending")
				} else {
					p.w(" ascending")
				}
			}
		}
		if x.Limit != nil {
			p.w(" limit ")
			p.expr(x.Limit)
		}
		p.using(x.Using)
	case *TransformExpr:
		p.w("transform ")
		p.expr(x.Source)
		p.w(" using " + x.Using.Name)
		for i, pv := range x.Params {
			if i == 0 {
				p.w(" parameters ")
			} else {
				p.w(", ")
			}
			p.w(pv.Name.Name + " = ")
			p.expr(pv.Value)
		}
		if x.Into != nil {
			p.w(" into ")
			p.typ(x.Into)
		}
	case *UpdateExpr:
		p.w("update " + x.Table.Name + " set ")
		for i, s := range x.Sets {
			if i > 0 {
				p.w(", ")
			}
			p.w(s.Field.Name + " = ")
			p.expr(s.Value)
		}
		p.where(x.Where)
		p.using(x.Using)
	case *DeleteExpr:
		p.w("delete from " + x.Table.Name)
		p.where(x.Where)
		p.using(x.Using)
	case *OpenSessionExpr:
		p.w("open session")
	case *FuncExpr:
		p.w("function ")
		p.params(x.Params, x.List)
		if x.Returns != nil {
			p.w(" returns ")
			p.typ(x.Returns)
		}
		if len(x.Raises) > 0 {
			p.w(" raises ")
			p.names(x.Raises)
		}
		if x.Body == nil {
			p.w(" => ")
			if x.Short != nil {
				p.expr(x.Short)
			}
			return
		}
		ind, saved := p.lineInd, p.stmtInd
		p.endLine(x.Head)
		p.stmts(x.Body.Stmts, ind+2)
		p.leading(x.Body.End.Leading, ind+2, true)
		p.startLine(ind)
		p.w("end")
		p.stmtInd = saved
	}
}

// escape returns literal string text with the canonical escapes.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '\\', '{', '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 {
				fmt.Fprintf(&b, `\u{%X}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func (p *printer) where(x Expr) {
	if x != nil {
		p.w(" where ")
		p.expr(x)
	}
}

func (p *printer) using(x Expr) {
	if x != nil {
		p.w(" using ")
		p.expr(x)
	}
}
