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
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxDepth limits the nesting of blocks and expressions.
const maxDepth = 200

type parser struct {
	toks    []Token
	i       int
	tok     Token
	errs    []Error
	pending []Line // comment and blank lines before the next line-based node
	depth   int
}

// Parse parses a source file. It returns the file even when there are
// errors, as far as it could be read.
func Parse(path, src string) (*File, []Error) {
	toks, errs := Lex(src)
	p := &parser{toks: toks, errs: errs}
	p.tok = toks[0]
	f := p.file(path)
	return f, p.errs
}

// ParseExpr parses a single expression, for tests and tools.
func ParseExpr(src string) (Expr, []Error) {
	toks, errs := Lex(src)
	p := &parser{toks: toks, errs: errs}
	p.tok = toks[0]
	x := p.expr(0)
	if p.tok.Kind == NEWLINE {
		p.next()
	}
	if p.tok.Kind != EOF {
		p.errorf(p.tok.Pos, "unexpected %s after the expression", p.tok)
	}
	return x, p.errs
}

func (p *parser) next() {
	if p.i < len(p.toks)-1 {
		p.i++
	}
	p.tok = p.toks[p.i]
}

func (p *parser) errorf(pos Pos, format string, args ...any) {
	// One error per position is enough.
	if n := len(p.errs); n > 0 && p.errs[n-1].Pos == pos {
		return
	}
	p.errs = append(p.errs, Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

func (p *parser) is(k Kind, text string) bool { return p.tok.Kind == k && p.tok.Text == text }
func (p *parser) isKw(text string) bool       { return p.is(KEYWORD, text) }
func (p *parser) isOp(text string) bool       { return p.is(OP, text) }

// isWord reports a contextual word such as "test" or "found".
func (p *parser) isWord(text string) bool { return p.is(IDENT, text) }

func (p *parser) expect(k Kind, text string) Pos {
	pos := p.tok.Pos
	if !p.is(k, text) {
		p.errorf(pos, "expected %q, found %s", text, p.tok)
		return pos
	}
	p.next()
	return pos
}

func (p *parser) expectKw(text string) Pos { return p.expect(KEYWORD, text) }
func (p *parser) expectOp(text string) Pos { return p.expect(OP, text) }

// lineEnd consumes the NEWLINE that ends a line and returns its trailing
// comment. The following comment and blank lines become pending.
func (p *parser) lineEnd() *Comment {
	if p.tok.Kind != NEWLINE {
		if p.tok.Kind != EOF {
			p.errorf(p.tok.Pos, "unexpected %s, expected the end of the line", p.tok)
		}
		p.skipLine()
		if p.tok.Kind != NEWLINE {
			return nil
		}
	}
	t := p.tok
	p.next()
	p.pending = append(p.pending, t.Lines...)
	return t.Trailing
}

// skipLine moves to the next NEWLINE after an error.
func (p *parser) skipLine() {
	for p.tok.Kind != NEWLINE && p.tok.Kind != EOF {
		p.next()
	}
}

// doc turns the pending lines into the leading comments of a node.
func (p *parser) doc() Comments {
	var c Comments
	c.Leading, c.Blank = takeLines(p.pending)
	p.pending = nil
	return c
}

func takeLines(lines []Line) ([]Leading, bool) {
	var out []Leading
	blank := false
	for _, l := range lines {
		if l.Comment == nil {
			blank = true
			continue
		}
		out = append(out, Leading{Blank: blank, Comment: *l.Comment})
		blank = false
	}
	return out, blank
}

// brk consumes an optional line break inside an expression.
func (p *parser) brk() Break {
	if p.tok.Kind != NEWLINE {
		return Break{}
	}
	t := p.tok
	p.next()
	if p.tok.Kind == EOF {
		p.errorf(t.Pos, "unexpected end of file")
	}
	return Break{Line: true, Trailing: t.Trailing, Lines: t.Lines}
}

func (p *parser) enter(pos Pos) bool {
	p.depth++
	if p.depth > maxDepth {
		p.errorf(pos, "nesting is too deep")
		return false
	}
	return true
}

func (p *parser) leave() { p.depth-- }

func (p *parser) ident() Ident {
	id := Ident{Pos: p.tok.Pos, Name: p.tok.Text}
	if p.tok.Kind != IDENT {
		p.errorf(p.tok.Pos, "expected a name, found %s", p.tok)
		return id
	}
	p.next()
	return id
}

// memberName reads a member name, which is a global name for fields
// added by extensions of other packages, e.g. /TROTTO/OTTO_ID.
func (p *parser) memberName() Ident {
	if p.tok.Kind == GLOBAL {
		id := Ident{Pos: p.tok.Pos, Name: p.tok.Text}
		p.next()
		return id
	}
	return p.ident()
}

func (p *parser) global() Name {
	n := Name{Pos: p.tok.Pos, Name: p.tok.Text}
	if p.tok.Kind != GLOBAL {
		p.errorf(p.tok.Pos, "expected a name with namespace such as /PKG/NAME, found %s", p.tok)
		return n
	}
	p.next()
	return n
}

func (p *parser) globals() []Name {
	out := []Name{p.global()}
	for p.isOp(",") {
		p.next()
		out = append(out, p.global())
	}
	return out
}

// ---- declarations ----

func (p *parser) file(path string) *File {
	f := &File{Path: path}
	if p.tok.Kind == NEWLINE {
		p.pending = append(p.pending, p.tok.Lines...)
		p.next()
	}
	f.Header, f.HeaderBlank = takeLines(p.pending)
	p.pending = nil
	switch {
	case p.isWord("members") || p.isWord("statements"):
		f.Kind = IncludeFile
		f.Include = p.includeDecl()
	default:
		vis, pos := p.visibility()
		if !vis.valid() {
			p.errorf(pos, "a file starts with public, package or private, or with members include or statements include")
			p.skipLine()
		}
		test := false
		if p.isWord("test") {
			test = true
			p.next()
		}
		open := false
		if p.isKw("open") {
			open = true
			p.next()
		}
		switch {
		case p.isKw("class"):
			f.Kind = ClassFile
			f.Class = p.classDecl(pos, vis, test, open)
		case p.isKw("interface") && !test && !open:
			f.Kind = InterfaceFile
			f.Interface = p.interfaceDecl(pos, vis)
		default:
			p.errorf(p.tok.Pos, "expected class or interface, found %s", p.tok)
			return f
		}
	}
	f.Footer = p.pending
	p.pending = nil
	if p.tok.Kind != EOF {
		p.errorf(p.tok.Pos, "unexpected %s after the end of the declaration", p.tok)
	}
	return f
}

func (v Visibility) valid() bool { return v == Public || v == Package || v == Private }

func (p *parser) visibility() (Visibility, Pos) {
	pos := p.tok.Pos
	if p.tok.Kind == KEYWORD && (p.tok.Text == "public" || p.tok.Text == "package" || p.tok.Text == "private") {
		v := Visibility(p.tok.Text)
		p.next()
		return v, pos
	}
	return "", pos
}

func (p *parser) classDecl(pos Pos, vis Visibility, test, open bool) *ClassDecl {
	c := &ClassDecl{Pos: pos, Visibility: vis, Test: test, Open: open}
	p.expectKw("class")
	c.Name = p.global()
	if p.isKw("extends") {
		p.next()
		n := p.global()
		c.Extends = &n
	}
	if p.isKw("implements") {
		p.next()
		c.Implements = p.globals()
	}
	if p.isWord("executed") {
		p.executedByKernel()
		c.Native = true
	}
	c.Doc.Trailing = p.lineEnd()
	c.Members = p.members()
	c.End = p.endLine()
	return c
}

// executedByKernel reads "executed by kernel".
func (p *parser) executedByKernel() {
	p.next()
	if !p.isWord("by") {
		p.errorf(p.tok.Pos, `expected "by kernel"`)
		return
	}
	p.next()
	if !p.isWord("kernel") {
		p.errorf(p.tok.Pos, `expected "kernel"`)
		return
	}
	p.next()
}

func (p *parser) interfaceDecl(pos Pos, vis Visibility) *InterfaceDecl {
	d := &InterfaceDecl{Pos: pos, Visibility: vis}
	p.expectKw("interface")
	d.Name = p.global()
	if p.isKw("extends") {
		p.next()
		d.Extends = p.globals()
	}
	d.Doc.Trailing = p.lineEnd()
	for !p.isKw("end") && p.tok.Kind != EOF {
		start := p.i
		doc := p.doc()
		if !p.isKw("method") {
			p.errorf(p.tok.Pos, "an interface contains only method signatures, found %s", p.tok)
			p.skipLine()
			p.lineEnd()
		} else {
			sig := p.methodSig()
			sig.Doc = doc
			sig.Doc.Trailing = p.lineEnd()
			d.Methods = append(d.Methods, sig)
		}
		if p.i == start {
			p.next()
		}
	}
	d.End = p.endLine()
	return d
}

func (p *parser) includeDecl() *IncludeDecl {
	d := &IncludeDecl{Pos: p.tok.Pos, Kind: IncludeKind(p.tok.Text)}
	p.next()
	p.expectKw("include")
	d.Name = p.global()
	d.Doc.Trailing = p.lineEnd()
	if d.Kind == MembersInclude {
		d.Members = p.members()
		d.Body = &Block{End: p.endLine()}
	} else {
		d.Body = p.block()
		d.Body.End = p.endLine()
	}
	return d
}

// endLine reads the "end" line of a declaration or block.
func (p *parser) endLine() EndLine {
	e := EndLine{Pos: p.tok.Pos}
	e.Leading, _ = takeLines(p.pending)
	p.pending = nil
	p.expectKw("end")
	e.Trailing = p.lineEnd()
	return e
}

func (p *parser) members() []Member {
	var out []Member
	for !p.isKw("end") && p.tok.Kind != EOF {
		start := p.i
		doc := p.doc()
		if m := p.member(); m != nil {
			*m.Comments() = Comments{Blank: doc.Blank, Leading: doc.Leading, Trailing: m.Comments().Trailing}
			out = append(out, m)
		}
		if p.i == start {
			p.next()
		}
	}
	return out
}

func (p *parser) member() Member {
	pos := p.tok.Pos
	if p.isKw("include") {
		p.next()
		m := &IncludeMember{MemberBase: MemberBase{Pos: pos}, Name: p.global()}
		m.IfFound = p.ifFound()
		m.Doc.Trailing = p.lineEnd()
		return m
	}
	vis, _ := p.visibility()
	if !vis.valid() {
		p.errorf(pos, "a member starts with public, package or private, found %s", p.tok)
		p.skipLine()
		p.lineEnd()
		return nil
	}
	static := p.isKw("static")
	if static {
		p.next()
	}
	open, override := p.isKw("open"), p.isKw("override")
	if open || override {
		p.next()
	}
	test := p.isWord("test")
	if test {
		p.next()
	}
	switch {
	case p.isKw("constant"):
		p.next()
		c := &ConstantDecl{MemberBase: MemberBase{Pos: pos}, Visibility: vis, Name: p.ident()}
		p.expectOp(":")
		c.Type = p.typ()
		p.expectOp("=")
		c.Break = p.brk()
		c.Value = p.expr(0)
		c.Doc.Trailing = p.lineEnd()
		if static || open || override || test {
			p.errorf(pos, "a constant takes no static, open, override or test")
		}
		return c
	case p.isKw("attribute"):
		p.next()
		a := &AttributeDecl{MemberBase: MemberBase{Pos: pos}, Visibility: vis, Static: static, Name: p.ident()}
		p.expectOp(":")
		a.Type = p.typ()
		if p.isOp("=") {
			p.next()
			a.Break = p.brk()
			a.Value = p.expr(0)
		}
		a.Doc.Trailing = p.lineEnd()
		if open || override || test {
			p.errorf(pos, "an attribute takes no open, override or test")
		}
		return a
	case p.isKw("method") || p.isKw("constructor"):
		m := &MethodDecl{MemberBase: MemberBase{Pos: pos}, Visibility: vis, Static: static, Open: open, Override: override, Test: test}
		if p.isKw("constructor") {
			m.Constructor = true
			sigPos := p.tok.Pos
			p.next()
			m.Sig = &MethodSig{Pos: sigPos}
			m.Sig.Params, m.Sig.List = p.params()
			if p.isKw("raises") {
				p.next()
				m.Sig.Raises = p.globals()
			}
		} else {
			m.Sig = p.methodSig()
		}
		if p.isWord("executed") {
			p.executedByKernel()
			m.Native = true
			m.Doc.Trailing = p.lineEnd()
			return m
		}
		m.Doc.Trailing = p.lineEnd()
		m.Body = p.block()
		m.Body.End = p.endLine()
		return m
	}
	p.errorf(p.tok.Pos, "expected constant, attribute, method or constructor, found %s", p.tok)
	p.skipLine()
	p.lineEnd()
	return nil
}

func (p *parser) ifFound() bool {
	if !p.isKw("if") {
		return false
	}
	p.next()
	if !p.isWord("found") {
		p.errorf(p.tok.Pos, `expected "found" after "if"`)
		return false
	}
	p.next()
	return true
}

func (p *parser) methodSig() *MethodSig {
	s := &MethodSig{Pos: p.tok.Pos}
	p.expectKw("method")
	s.Name = p.ident()
	s.Params, s.List = p.params()
	if p.isKw("returns") {
		p.next()
		s.Returns = p.typ()
	}
	if p.isKw("raises") {
		p.next()
		s.Raises = p.globals()
	}
	return s
}

func (p *parser) params() ([]*Param, List) {
	var out []*Param
	var l List
	p.expectOp("(")
	l.Open = p.brk()
	for !p.isOp(")") && p.tok.Kind != EOF {
		if p.tok.Kind == NEWLINE {
			l.Close = p.brk()
			break
		}
		prm := &Param{Name: p.ident()}
		p.expectOp(":")
		prm.Type = p.typ()
		out = append(out, prm)
		if !p.isOp(",") {
			if p.tok.Kind == NEWLINE {
				l.Close = p.brk()
			}
			break
		}
		p.next()
		l.Commas = append(l.Commas, p.brk())
	}
	p.expectOp(")")
	return out, l
}

// typ parses a type expression.
func (p *parser) typ() *Type {
	t := &Type{Pos: p.tok.Pos}
	if !p.enter(t.Pos) {
		p.leave()
		return t
	}
	defer p.leave()
	switch {
	case p.tok.Kind == IDENT && BuiltinTypes[p.tok.Text]:
		t.Kind, t.Name = BuiltinType, p.tok.Text
		p.next()
		if t.Name == "DEC" {
			p.expectOp("(")
			t.Precision = p.intLit()
			p.expectOp(",")
			t.Scale = p.intLit()
			p.expectOp(")")
		}
	case p.tok.Kind == GLOBAL:
		t.Kind, t.Name = NamedType, p.tok.Text
		p.next()
	case p.isKw("array"):
		p.next()
		p.expectKw("of")
		t.Kind, t.Elem = ArrayType, p.typ()
		return t
	case p.isKw("map"):
		p.next()
		p.expectKw("of")
		t.Kind, t.Key = MapType, p.typ()
		p.expectKw("to")
		t.Elem = p.typ()
		return t
	case p.isKw("function"):
		p.next()
		t.Kind = FuncType
		p.expectOp("(")
		for !p.isOp(")") && p.tok.Kind != EOF && p.tok.Kind != NEWLINE {
			t.Params = append(t.Params, p.typ())
			if !p.isOp(",") {
				break
			}
			p.next()
		}
		p.expectOp(")")
		if p.isKw("returns") {
			p.next()
			t.Returns = p.typ()
		}
		if p.isKw("raises") {
			p.next()
			t.Raises = p.globals()
		}
		return t
	default:
		p.errorf(t.Pos, "expected a type, found %s", p.tok)
		return t
	}
	if p.isOp("?") {
		t.Optional = true
		p.next()
	}
	return t
}

func (p *parser) intLit() int {
	if p.tok.Kind != INT {
		p.errorf(p.tok.Pos, "expected a number, found %s", p.tok)
		return 0
	}
	n, err := strconv.Atoi(strings.ReplaceAll(p.tok.Text, "_", ""))
	if err != nil || n > 1000 {
		p.errorf(p.tok.Pos, "number %s is too large here", p.tok.Text)
	}
	p.next()
	return n
}

// ---- statements ----

// block reads statements up to end, else, elseif, catch or finally. The
// caller reads the closing line into End.
func (p *parser) block() *Block {
	b := &Block{}
	if !p.enter(p.tok.Pos) {
		p.leave()
		for p.tok.Kind != EOF {
			p.next()
		}
		return b
	}
	defer p.leave()
	for p.tok.Kind != EOF && !p.isKw("end") && !p.isKw("else") && !p.isKw("elseif") && !p.isKw("catch") && !p.isKw("finally") {
		start := p.i
		doc := p.doc()
		if s := p.stmt(); s != nil {
			*s.Comments() = Comments{Blank: doc.Blank, Leading: doc.Leading, Trailing: s.Comments().Trailing}
			b.Stmts = append(b.Stmts, s)
		}
		if p.i == start {
			p.next()
		}
	}
	return b
}

// closeLine reads the line that closes a block: comments before it, and
// for the keyword given its trailing comment after the rest of the line
// has been parsed by the caller.
func (p *parser) closeLeading() EndLine {
	e := EndLine{Pos: p.tok.Pos}
	e.Leading, _ = takeLines(p.pending)
	p.pending = nil
	return e
}

func (p *parser) stmt() Stmt {
	pos := p.tok.Pos
	base := StmtBase{Pos: pos}
	if p.tok.Kind == KEYWORD {
		switch p.tok.Text {
		case "var":
			p.next()
			s := &VarStmt{StmtBase: base, Name: p.ident()}
			if p.isOp(":") {
				p.next()
				s.Type = p.typ()
				// var x: T alone starts with the initial values of T.
				if p.tok.Kind == NEWLINE || p.tok.Kind == EOF {
					s.Doc.Trailing = p.lineEnd()
					return s
				}
			}
			p.expectOp("=")
			s.Break = p.brk()
			s.Value = p.expr(0)
			s.Doc.Trailing = p.lineEnd()
			return s
		case "if":
			return p.ifStmt(base)
		case "for":
			return p.forStmt(base)
		case "while":
			p.next()
			s := &WhileStmt{StmtBase: base, Cond: p.expr(0)}
			s.Doc.Trailing = p.lineEnd()
			s.Body = p.block()
			s.Body.End = p.endLine()
			return s
		case "break":
			p.next()
			s := &BreakStmt{StmtBase: base}
			s.Doc.Trailing = p.lineEnd()
			return s
		case "continue":
			p.next()
			s := &ContinueStmt{StmtBase: base}
			s.Doc.Trailing = p.lineEnd()
			return s
		case "return":
			p.next()
			s := &ReturnStmt{StmtBase: base}
			if p.tok.Kind != NEWLINE && p.tok.Kind != EOF {
				s.Value = p.expr(0)
			}
			s.Doc.Trailing = p.lineEnd()
			return s
		case "raise":
			p.next()
			s := &RaiseStmt{StmtBase: base, Class: p.global()}
			if p.isKw("with") {
				p.next()
				s.Args = append(s.Args, p.expr(0))
				for p.isOp(",") {
					p.next()
					s.Comma = append(s.Comma, p.brk())
					s.Args = append(s.Args, p.expr(0))
				}
			}
			s.Doc.Trailing = p.lineEnd()
			return s
		case "try":
			return p.tryStmt(base)
		case "assert":
			p.next()
			s := &AssertStmt{StmtBase: base, Cond: p.expr(0)}
			if p.isOp(",") {
				p.next()
				s.Message = p.expr(0)
			}
			s.Doc.Trailing = p.lineEnd()
			return s
		case "log":
			p.next()
			s := &LogStmt{StmtBase: base, Level: p.tok.Text}
			if p.tok.Kind != IDENT || !slices.Contains(LogLevels, p.tok.Text) {
				p.errorf(p.tok.Pos, "expected a log level (%s), found %s", strings.Join(LogLevels, ", "), p.tok)
			} else {
				p.next()
			}
			s.Value = p.expr(0)
			s.Doc.Trailing = p.lineEnd()
			return s
		case "include":
			p.next()
			s := &IncludeStmt{StmtBase: base, Name: p.global()}
			s.IfFound = p.ifFound()
			s.Doc.Trailing = p.lineEnd()
			return s
		case "commit", "rollback":
			s := &DatabaseStmt{StmtBase: base, Keyword: p.tok.Text}
			p.next()
			if p.tok.Kind != NEWLINE && p.tok.Kind != EOF {
				s.Session = p.expr(0)
			}
			s.Doc.Trailing = p.lineEnd()
			return s
		case "insert", "update", "delete":
			// update /PKG/T set ... and delete from ... are expressions.
			setForm := p.tok.Text == "update" && p.i+2 < len(p.toks) && p.toks[p.i+1].Kind == GLOBAL && p.toks[p.i+2].Kind == IDENT && p.toks[p.i+2].Text == "set"
			fromForm := p.tok.Text == "delete" && p.i+1 < len(p.toks) && p.toks[p.i+1].Kind == KEYWORD && p.toks[p.i+1].Text == "from"
			if !setForm && !fromForm {
				s := &RowStmt{StmtBase: base, Op: p.tok.Text}
				p.next()
				s.Row = p.expr(0)
				if p.isKw("using") {
					p.next()
					s.Using = p.expr(0)
				}
				s.Doc.Trailing = p.lineEnd()
				return s
			}
		}
		if !p.startsOperand() {
			p.errorf(pos, "unexpected %s at the start of a statement", p.tok)
			p.skipLine()
			p.lineEnd()
			return nil
		}
	}
	x := p.postfix(p.primary())
	if p.isOp("=") {
		p.next()
		s := &AssignStmt{StmtBase: base, Target: x}
		s.Break = p.brk()
		s.Value = p.expr(0)
		s.Doc.Trailing = p.lineEnd()
		return s
	}
	s := &ExprStmt{StmtBase: base, X: x}
	s.Doc.Trailing = p.lineEnd()
	return s
}

func (p *parser) startsOperand() bool {
	switch p.tok.Text {
	case "me", "super", "new", "null", "true", "false", "function", "select", "update", "delete", "open":
		return true
	}
	return false
}

func (p *parser) ifStmt(base StmtBase) *IfStmt {
	s := &IfStmt{StmtBase: base}
	p.next()
	c := &IfClause{Pos: base.Pos, Cond: p.expr(0)}
	s.Doc.Trailing = p.lineEnd()
	c.Body = p.block()
	s.Clauses = append(s.Clauses, c)
	for {
		end := p.closeLeading()
		switch {
		case p.isKw("elseif"):
			p.next()
			n := &IfClause{Pos: end.Pos, Cond: p.expr(0)}
			end.Trailing = p.lineEnd()
			c.Body.End = end
			n.Body = p.block()
			s.Clauses = append(s.Clauses, n)
			c = n
			continue
		case p.isKw("else"):
			p.next()
			end.Trailing = p.lineEnd()
			c.Body.End = end
			s.Else = p.block()
			s.Else.End = p.endLine()
			return s
		}
		p.pending = append(linesOf(end.Leading), p.pending...)
		c.Body.End = p.endLine()
		return s
	}
}

// linesOf turns leading comments back into lines.
func linesOf(ls []Leading) []Line {
	var out []Line
	for _, l := range ls {
		if l.Blank {
			out = append(out, Line{})
		}
		c := l.Comment
		out = append(out, Line{Comment: &c})
	}
	return out
}

func (p *parser) forStmt(base StmtBase) *ForStmt {
	s := &ForStmt{StmtBase: base}
	p.next()
	s.Var = p.ident()
	if p.isOp(",") {
		p.next()
		k := s.Var
		s.Key = &k
		s.Var = p.ident()
	}
	switch {
	case p.isKw("in"):
		p.next()
		s.In = p.expr(0)
	case p.isKw("from") && s.Key == nil:
		p.next()
		s.From = p.expr(0)
		p.expectKw("to")
		s.To = p.expr(0)
		if p.isWord("step") {
			p.next()
			s.Step = p.expr(0)
		}
	default:
		p.errorf(p.tok.Pos, `expected "in" or "from", found %s`, p.tok)
	}
	s.Doc.Trailing = p.lineEnd()
	s.Body = p.block()
	s.Body.End = p.endLine()
	return s
}

func (p *parser) tryStmt(base StmtBase) *TryStmt {
	s := &TryStmt{StmtBase: base}
	p.next()
	s.Doc.Trailing = p.lineEnd()
	s.Body = p.block()
	last := s.Body
	for p.isKw("catch") {
		end := p.closeLeading()
		p.next()
		c := &CatchClause{Pos: end.Pos, Var: p.ident()}
		p.expectOp(":")
		c.Class = p.global()
		end.Trailing = p.lineEnd()
		last.End = end
		c.Body = p.block()
		s.Catches = append(s.Catches, c)
		last = c.Body
	}
	if p.isKw("finally") {
		end := p.closeLeading()
		p.next()
		end.Trailing = p.lineEnd()
		last.End = end
		s.Finally = p.block()
		last = s.Finally
	}
	if len(s.Catches) == 0 && s.Finally == nil {
		p.errorf(base.Pos, "try needs catch or finally")
	}
	last.End = p.endLine()
	return s
}

// ---- expressions ----

// Binding powers of binary operators.
var precedence = map[string]int{
	"default": 1, "or": 2, "and": 3,
	"=": 5, "<>": 5, "<": 5, ">": 5, "<=": 5, ">=": 5, "is": 5, "in": 5,
	"+": 6, "-": 6, "*": 7, "/": 7, "mod": 7, "as": 9,
}

func (p *parser) binaryOp() (string, int) {
	if p.tok.Kind != OP && p.tok.Kind != KEYWORD {
		return "", 0
	}
	if prec, ok := precedence[p.tok.Text]; ok {
		return p.tok.Text, prec
	}
	return "", 0
}

func (p *parser) bad(pos Pos) Expr { return &LiteralExpr{Pos: pos, Kind: ILLEGAL} }

// expr parses an expression whose operators bind at least as strong as
// min.
func (p *parser) expr(min int) Expr {
	pos := p.tok.Pos
	if !p.enter(pos) {
		p.leave()
		p.skipLine()
		return p.bad(pos)
	}
	defer p.leave()
	var x Expr
	switch {
	case p.isOp("-"):
		p.next()
		x = &UnaryExpr{Pos: pos, Op: "-", X: p.expr(8)}
	case p.isKw("not"):
		p.next()
		x = &UnaryExpr{Pos: pos, Op: "not", X: p.expr(4)}
	default:
		x = p.postfix(p.primary())
	}
	for {
		op, prec := p.binaryOp()
		if op == "" || prec < min {
			return x
		}
		opPos := p.tok.Pos
		p.next()
		switch op {
		case "as":
			x = &AsExpr{X: x, Type: p.typ()}
			continue
		case "is":
			e := &IsExpr{X: x, OpPos: opPos}
			if p.isKw("not") {
				e.Not = true
				p.next()
			}
			if p.isKw("null") || p.isKw("empty") {
				e.What = p.tok.Text
				p.next()
			} else {
				p.errorf(p.tok.Pos, `expected "null" or "empty" after "is", found %s`, p.tok)
			}
			x = e
		default:
			b := &BinaryExpr{X: x, OpPos: opPos, Op: op}
			b.Break = p.brk()
			b.Y = p.expr(prec + 1)
			x = b
		}
		if prec == 5 {
			if _, next := p.binaryOp(); next == 5 {
				p.errorf(p.tok.Pos, "comparisons cannot be chained; use and")
			}
		}
	}
}

func (p *parser) primary() Expr {
	t := p.tok
	switch t.Kind {
	case INT, DEC:
		p.next()
		return &LiteralExpr{Pos: t.Pos, Kind: t.Kind, Raw: t.Text}
	case STRING:
		p.next()
		return p.stringLit(t)
	case IDENT:
		p.next()
		return &Ident{Pos: t.Pos, Name: t.Text}
	case GLOBAL:
		p.next()
		return &GlobalExpr{Name: Name{Pos: t.Pos, Name: t.Text}}
	case KEYWORD:
		switch t.Text {
		case "true", "false", "null":
			p.next()
			return &LiteralExpr{Pos: t.Pos, Kind: KEYWORD, Raw: t.Text}
		case "me":
			p.next()
			return &MeExpr{Pos: t.Pos}
		case "super":
			p.next()
			return &SuperExpr{Pos: t.Pos}
		case "new":
			return p.newExpr()
		case "function":
			return p.funcExpr()
		case "select":
			return p.selectExpr()
		case "update":
			return p.updateExpr()
		case "delete":
			return p.deleteExpr()
		case "open":
			p.next()
			if !p.isKw("session") {
				p.errorf(p.tok.Pos, `expected "session" after "open", found %s`, p.tok)
				return p.bad(t.Pos)
			}
			p.next()
			return &OpenSessionExpr{Pos: t.Pos}
		}
	case OP:
		switch t.Text {
		case "(":
			p.next()
			e := &ParenExpr{Pos: t.Pos}
			e.List.Open = p.brk()
			e.X = p.expr(0)
			e.List.Close = p.brk()
			p.expectOp(")")
			return e
		case "[":
			return p.collection()
		}
	}
	p.errorf(t.Pos, "expected an expression, found %s", t)
	if t.Kind != NEWLINE && t.Kind != EOF {
		p.next()
	}
	return p.bad(t.Pos)
}

func (p *parser) postfix(x Expr) Expr {
	for {
		switch {
		case p.isOp("."):
			p.next()
			x = &SelectExpr{X: x, Name: p.memberName()}
		case p.isOp("("):
			c := &CallExpr{Fun: x}
			c.Args, c.List = p.args()
			x = c
		case p.isOp("["):
			p.next()
			e := &IndexExpr{X: x}
			e.List.Open = p.brk()
			e.Index = p.expr(0)
			e.List.Close = p.brk()
			p.expectOp("]")
			x = e
		default:
			return x
		}
	}
}

// args parses a parenthesized argument list.
func (p *parser) args() ([]Arg, List) {
	var out []Arg
	var l List
	p.expectOp("(")
	l.Open = p.brk()
	for !p.isOp(")") && p.tok.Kind != EOF && p.tok.Kind != NEWLINE {
		var a Arg
		if (p.tok.Kind == IDENT || p.tok.Kind == GLOBAL) && p.i+1 < len(p.toks) && p.toks[p.i+1].Kind == OP && p.toks[p.i+1].Text == ":" {
			id := p.memberName()
			a.Name = &id
			p.next()
		}
		a.Value = p.expr(0)
		out = append(out, a)
		if !p.isOp(",") {
			break
		}
		p.next()
		l.Commas = append(l.Commas, p.brk())
	}
	l.Close = p.brk()
	p.expectOp(")")
	return out, l
}

func (p *parser) newExpr() Expr {
	e := &NewExpr{Pos: p.tok.Pos}
	p.next()
	switch {
	case p.tok.Kind == GLOBAL:
		e.Type = &Type{Pos: p.tok.Pos, Kind: NamedType, Name: p.tok.Text}
		p.next()
		e.Call = true
		e.Args, e.List = p.args()
	case p.isKw("array") || p.isKw("map"):
		e.Type = p.typ()
	default:
		p.errorf(p.tok.Pos, "expected a class, structure, array or map after new, found %s", p.tok)
	}
	return e
}

func (p *parser) collection() Expr {
	pos := p.tok.Pos
	p.next()
	var l List
	l.Open = p.brk()
	if p.isOp(":") {
		p.next()
		l.Close = p.brk()
		p.expectOp("]")
		return &MapExpr{Pos: pos, List: l}
	}
	if p.isOp("]") {
		p.next()
		return &ArrayExpr{Pos: pos, List: l}
	}
	first := p.expr(0)
	if p.isOp(":") {
		m := &MapExpr{Pos: pos}
		p.next()
		m.Keys, m.Values = []Expr{first}, []Expr{p.expr(0)}
		for p.isOp(",") {
			p.next()
			l.Commas = append(l.Commas, p.brk())
			m.Keys = append(m.Keys, p.expr(0))
			p.expectOp(":")
			m.Values = append(m.Values, p.expr(0))
		}
		l.Close = p.brk()
		p.expectOp("]")
		m.List = l
		return m
	}
	a := &ArrayExpr{Pos: pos, Elems: []Expr{first}}
	for p.isOp(",") {
		p.next()
		l.Commas = append(l.Commas, p.brk())
		a.Elems = append(a.Elems, p.expr(0))
	}
	l.Close = p.brk()
	p.expectOp("]")
	a.List = l
	return a
}

func (p *parser) funcExpr() Expr {
	e := &FuncExpr{Pos: p.tok.Pos}
	p.next()
	e.Params, e.List = p.params()
	if p.isKw("returns") {
		p.next()
		e.Returns = p.typ()
	}
	if p.isKw("raises") {
		p.next()
		e.Raises = p.globals()
	}
	if p.isOp("=>") {
		p.next()
		e.Short = p.expr(0)
		return e
	}
	if p.tok.Kind != NEWLINE {
		p.errorf(p.tok.Pos, `expected "=>" or the end of the line after the function head, found %s`, p.tok)
		return e
	}
	saved := p.pending
	p.pending = nil
	e.Head = p.lineEnd()
	e.Body = p.block()
	e.Body.End = p.closeLeading()
	p.expectKw("end")
	p.pending = saved
	return e
}

// stringLit splits a string literal into text and placeholders.
func (p *parser) stringLit(t Token) Expr {
	e := &StringExpr{Pos: t.Pos}
	raw := t.Text[1 : len(t.Text)-1]
	var text strings.Builder
	col := t.Pos.Col + 1
	off := t.Pos.Off + 1
	flush := func() {
		if text.Len() > 0 {
			e.Parts = append(e.Parts, StringPart{Text: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRuneInString(raw[i:])
		here := Pos{Line: t.Pos.Line, Col: col, Off: off + i}
		switch r {
		case '\\':
			if i+1 >= len(raw) {
				p.errorf(here, "incomplete escape sequence")
				i++
				continue
			}
			switch raw[i+1] {
			case '"', '\\', '{', '}':
				text.WriteByte(raw[i+1])
			case 'n':
				text.WriteByte('\n')
			case 't':
				text.WriteByte('\t')
			case 'u':
				end := strings.IndexByte(raw[i:], '}')
				if !strings.HasPrefix(raw[i:], `\u{`) || end < 0 {
					p.errorf(here, `expected \u{hex}`)
					i += 2
					col += 2
					continue
				}
				n, err := strconv.ParseUint(raw[i+3:i+end], 16, 32)
				if err != nil || n > utf8.MaxRune || !utf8.ValidRune(rune(n)) || end == 3 {
					p.errorf(here, "invalid character code in %s", raw[i:i+end+1])
				} else {
					text.WriteRune(rune(n))
				}
				col += utf8.RuneCountInString(raw[i : i+end+1])
				i += end + 1
				continue
			default:
				p.errorf(here, `unknown escape sequence \%c`, raw[i+1])
			}
			i += 2
			col += 2
		case '{':
			flush()
			end := matchBrace(raw, i)
			inner := raw[i+1 : end]
			x := p.subExpr(inner, Pos{Line: t.Pos.Line, Col: col + 1, Off: off + i + 1})
			e.Parts = append(e.Parts, StringPart{Expr: x})
			col += utf8.RuneCountInString(raw[i : end+1])
			i = end + 1
		default:
			text.WriteRune(r)
			i += size
			col++
		}
	}
	flush()
	return e
}

// matchBrace returns the index of the "}" closing the "{" at i; the lexer
// guarantees that it exists.
func matchBrace(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j
			}
		case '"':
			for j++; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' {
					j++
				} else if s[j] == '{' {
					j = matchBrace(s, j)
				}
			}
		}
	}
	return len(s) - 1
}

// subExpr parses the expression of a placeholder.
func (p *parser) subExpr(src string, base Pos) Expr {
	toks, errs := Lex(src)
	shift := func(q Pos) Pos {
		return Pos{Line: base.Line, Col: q.Col + base.Col - 1, Off: q.Off + base.Off}
	}
	for i := range toks {
		toks[i].Pos = shift(toks[i].Pos)
		if toks[i].Kind == NEWLINE && (toks[i].Trailing != nil || len(toks[i].Lines) > 0) {
			p.errorf(toks[i].Pos, "a placeholder cannot contain a comment")
		}
	}
	for _, e := range errs {
		p.errorf(shift(e.Pos), "%s", e.Msg)
	}
	sub := &parser{toks: toks, depth: p.depth}
	sub.tok = toks[0]
	if sub.tok.Kind == NEWLINE || sub.tok.Kind == EOF {
		p.errorf(base, "empty placeholder")
		return p.bad(base)
	}
	x := sub.expr(0)
	if sub.tok.Kind == NEWLINE {
		sub.next()
	}
	if sub.tok.Kind != EOF {
		sub.errorf(sub.tok.Pos, "unexpected %s in the placeholder", sub.tok)
	}
	p.errs = append(p.errs, sub.errs...)
	return x
}

// using reads an optional "using session".
func (p *parser) using() Expr {
	if !p.isKw("using") {
		return nil
	}
	p.next()
	return p.expr(0)
}

// where reads an optional where condition.
func (p *parser) where() Expr {
	if !p.isKw("where") {
		return nil
	}
	p.next()
	return p.expr(0)
}

func (p *parser) selectExpr() Expr {
	e := &QueryExpr{Pos: p.tok.Pos}
	p.next()
	if p.isWord("single") || p.isWord("count") {
		e.Mode = p.tok.Text
		p.next()
	}
	p.expectKw("from")
	e.Table = p.global()
	e.Where = p.where()
	if p.isWord("order") {
		p.next()
		if !p.isWord("by") {
			p.errorf(p.tok.Pos, `expected "by" after "order"`)
		} else {
			p.next()
		}
		for {
			item := OrderItem{Field: p.memberName()}
			if p.isWord("ascending") || p.isWord("descending") {
				item.Explicit, item.Descending = true, p.tok.Text == "descending"
				p.next()
			}
			e.Order = append(e.Order, item)
			if !p.isOp(",") {
				break
			}
			p.next()
		}
	}
	if p.isWord("limit") {
		p.next()
		e.Limit = p.expr(0)
	}
	e.Using = p.using()
	return e
}

func (p *parser) updateExpr() Expr {
	e := &UpdateExpr{Pos: p.tok.Pos}
	p.next()
	e.Table = p.global()
	if !p.isWord("set") {
		p.errorf(p.tok.Pos, `expected "set" after the table`)
		return e
	}
	p.next()
	for {
		item := SetItem{Field: p.memberName()}
		p.expectOp("=")
		item.Value = p.expr(0)
		e.Sets = append(e.Sets, item)
		if !p.isOp(",") {
			break
		}
		p.next()
	}
	e.Where = p.where()
	e.Using = p.using()
	return e
}

func (p *parser) deleteExpr() Expr {
	e := &DeleteExpr{Pos: p.tok.Pos}
	p.next()
	p.expectKw("from")
	e.Table = p.global()
	e.Where = p.where()
	e.Using = p.using()
	return e
}
