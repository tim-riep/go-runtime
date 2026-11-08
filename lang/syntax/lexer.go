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
	"unicode/utf8"
)

// Error is a syntax error.
type Error struct {
	Pos Pos
	Msg string
}

func (e Error) Error() string { return e.Pos.String() + ": " + e.Msg }

type lexer struct {
	src     string
	off     int
	line    int
	col     int
	toks    []Token
	errs    []Error
	operand bool // the previous token ends an operand, so "/" divides
}

// Lex splits src into tokens. A line break yields one NEWLINE token that
// carries the comment at the end of the line and the following blank and
// comment-only lines. Comment and blank lines at the start of the file
// yield a NEWLINE token first. The last token is EOF.
func Lex(src string) ([]Token, []Error) {
	l := &lexer{src: src, line: 1, col: 1}
	l.run()
	return l.toks, l.errs
}

func (l *lexer) pos() Pos { return Pos{Line: l.line, Col: l.col, Off: l.off} }

func (l *lexer) errorf(p Pos, format string, args ...any) {
	l.errs = append(l.errs, Error{Pos: p, Msg: fmt.Sprintf(format, args...)})
}

func (l *lexer) peek(n int) byte {
	if l.off+n < len(l.src) {
		return l.src[l.off+n]
	}
	return 0
}

// advance moves over n bytes on the current line.
func (l *lexer) advance(n int) {
	for i := 0; i < n && l.off < len(l.src); {
		_, size := utf8.DecodeRuneInString(l.src[l.off:])
		l.off += size
		i += size
		l.col++
	}
}

func (l *lexer) emit(k Kind, text string, p Pos) {
	l.toks = append(l.toks, Token{Kind: k, Text: text, Pos: p})
	switch k {
	case IDENT:
		// After the contextual words of update ... set and order by a
		// global name may follow, such as an extension field, and after
		// extension point the name of the extension point.
		// The same holds after business object and at the start of the
		// steps create and execute of modify business object.
		l.operand = text != "set" && text != "by" && !(text == "point" && l.after("extension")) &&
			!(text == "object" && l.after("business")) && !((text == "create" || text == "execute") && l.lineStart())
	case GLOBAL, INT, DEC, STRING:
		l.operand = true
	case KEYWORD:
		l.operand = text == "null" || text == "true" || text == "false" || text == "me" || text == "end"
	case OP:
		l.operand = text == ")" || text == "]" || text == "?"
	default:
		l.operand = false
	}
}

// after reports whether the token before the one just emitted is the
// name word.
func (l *lexer) after(word string) bool {
	n := len(l.toks)
	return n >= 2 && l.toks[n-2].Kind == IDENT && l.toks[n-2].Text == word && l.toks[n-2].Pos.Line == l.toks[n-1].Pos.Line
}

// lineStart reports whether the token just emitted starts its line.
func (l *lexer) lineStart() bool {
	n := len(l.toks)
	return n == 1 || n >= 2 && (l.toks[n-2].Kind == NEWLINE || l.toks[n-2].Pos.Line != l.toks[n-1].Pos.Line)
}

func (l *lexer) run() {
	// Comment and blank lines before the first code line.
	if lines := l.emptyLines(); len(lines) > 0 {
		l.toks = append(l.toks, Token{Kind: NEWLINE, Pos: Pos{Line: 1, Col: 1}, Lines: lines})
	}
	var trailing *Comment
	for l.off < len(l.src) {
		c := l.src[l.off]
		p := l.pos()
		switch {
		case c == ' ':
			l.advance(1)
		case c == '\t' || c == '\r':
			l.errorf(p, "tabs and carriage returns are not allowed")
			l.advance(1)
		case c == '\n':
			l.off++
			l.line++
			l.col = 1
			tok := Token{Kind: NEWLINE, Pos: p, Trailing: trailing}
			trailing = nil
			tok.Lines = l.emptyLines()
			l.toks = append(l.toks, tok)
			l.operand = false
		case c == '#':
			trailing = &Comment{Pos: p, Text: l.restOfLine()}
		case isDigit(c):
			l.number(p)
		case isLetter(c):
			start := l.off
			for l.off < len(l.src) && (isLetter(l.src[l.off]) || isDigit(l.src[l.off]) || l.src[l.off] == '_') {
				l.advance(1)
			}
			word := l.src[start:l.off]
			if Keywords[word] {
				l.emit(KEYWORD, word, p)
			} else {
				l.emit(IDENT, word, p)
			}
		case c == '"':
			l.str(p)
		case c == '/' && !l.operand && isUpper(l.peek(1)):
			l.global(p)
		default:
			if op := l.operator(); op != "" {
				l.advance(len(op))
				l.emit(OP, op, p)
				continue
			}
			r, size := utf8.DecodeRuneInString(l.src[l.off:])
			l.errorf(p, "unexpected character %q", r)
			l.advance(size)
		}
	}
	if trailing != nil || len(l.toks) == 0 || l.toks[len(l.toks)-1].Kind != NEWLINE {
		l.toks = append(l.toks, Token{Kind: NEWLINE, Pos: l.pos(), Trailing: trailing})
	}
	l.toks = append(l.toks, Token{Kind: EOF, Pos: l.pos()})
}

// emptyLines reads the blank and comment-only lines at the current
// position, which is the start of a line.
func (l *lexer) emptyLines() []Line {
	var out []Line
	for l.off < len(l.src) {
		i := l.off
		for i < len(l.src) && l.src[i] == ' ' {
			i++
		}
		switch {
		case i == len(l.src):
			l.col += i - l.off
			l.off = i
			if i > 0 && l.src[i-1] != '\n' {
				out = append(out, Line{})
			}
			return out
		case l.src[i] == '\n':
			out = append(out, Line{})
			l.off = i + 1
			l.line++
			l.col = 1
		case l.src[i] == '#':
			l.col += i - l.off
			l.off = i
			p := l.pos()
			out = append(out, Line{Comment: &Comment{Pos: p, Text: l.restOfLine()}})
			if l.off < len(l.src) {
				l.off++
				l.line++
				l.col = 1
			}
		default:
			l.col += i - l.off
			l.off = i
			return out
		}
	}
	return out
}

// restOfLine reads a comment up to the end of the line, without "#" and
// without trailing spaces. The line break is not consumed.
func (l *lexer) restOfLine() string {
	start := l.off + 1
	end := strings.IndexByte(l.src[start:], '\n')
	if end < 0 {
		end = len(l.src)
	} else {
		end += start
	}
	text := l.src[start:end]
	if strings.ContainsAny(text, "\t\r") {
		l.errorf(l.pos(), "tabs and carriage returns are not allowed")
	}
	l.advance(end - l.off)
	return strings.TrimRight(text, " \t\r")
}

var operators = []string{"<>", "<=", ">=", "=>", "+", "-", "*", "/", "=", "<", ">", "(", ")", "[", "]", ",", ":", ".", "?"}

func (l *lexer) operator() string {
	for _, op := range operators {
		if strings.HasPrefix(l.src[l.off:], op) {
			return op
		}
	}
	return ""
}

func (l *lexer) number(p Pos) {
	start := l.off
	digits := func() {
		for l.off < len(l.src) && (isDigit(l.src[l.off]) || l.src[l.off] == '_') {
			l.advance(1)
		}
	}
	digits()
	kind := INT
	if l.peek(0) == '.' && isDigit(l.peek(1)) {
		kind = DEC
		l.advance(1)
		digits()
	}
	text := l.src[start:l.off]
	if strings.HasSuffix(text, "_") || strings.Contains(text, "__") || strings.Contains(text, "_.") || strings.Contains(text, "._") {
		l.errorf(p, "misplaced _ in number %s", text)
	}
	if l.off < len(l.src) && (isLetter(l.src[l.off]) || l.src[l.off] == '_') {
		l.errorf(l.pos(), "a number must not be followed by a letter")
	}
	l.emit(kind, text, p)
}

// global reads /NS/NAME.
func (l *lexer) global(p Pos) {
	start := l.off
	l.advance(1)
	for l.off < len(l.src) && (isUpper(l.src[l.off]) || isDigit(l.src[l.off])) {
		l.advance(1)
	}
	if l.peek(0) != '/' || !isLetter(l.peek(1)) {
		l.errorf(p, "a global name has the form /NAMESPACE/NAME")
		l.emit(ILLEGAL, l.src[start:l.off], p)
		return
	}
	l.advance(1)
	for l.off < len(l.src) && (isLetter(l.src[l.off]) || isDigit(l.src[l.off]) || l.src[l.off] == '_') {
		l.advance(1)
	}
	l.emit(GLOBAL, l.src[start:l.off], p)
}

// str reads a string literal including placeholders, which may contain
// strings themselves. The token text is the literal as written.
func (l *lexer) str(p Pos) {
	start := l.off
	if !l.skipString(p) {
		l.emit(ILLEGAL, l.src[start:l.off], p)
		return
	}
	l.emit(STRING, l.src[start:l.off], p)
}

func (l *lexer) skipString(p Pos) bool {
	l.advance(1) // opening quote
	for l.off < len(l.src) {
		switch c := l.src[l.off]; c {
		case '"':
			l.advance(1)
			return true
		case '\n':
			l.errorf(p, "string is not terminated before the end of the line")
			return false
		case '\\':
			l.advance(1)
			if l.off < len(l.src) && l.src[l.off] != '\n' {
				l.advance(1)
			}
		case '{':
			l.advance(1)
			depth := 1
			for depth > 0 {
				if l.off >= len(l.src) || l.src[l.off] == '\n' {
					l.errorf(p, "placeholder is not closed before the end of the line")
					return false
				}
				switch l.src[l.off] {
				case '{':
					depth++
					l.advance(1)
				case '}':
					depth--
					l.advance(1)
				case '"':
					if !l.skipString(l.pos()) {
						return false
					}
				default:
					l.advance(1)
				}
			}
		default:
			l.advance(1)
		}
	}
	l.errorf(p, "string is not terminated")
	return false
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isUpper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isLetter(c byte) bool { return isUpper(c) || c >= 'a' && c <= 'z' }

// NameCase tells how a name without namespace is written.
type NameCase int

// Name cases.
const (
	MixedCase NameCase = iota
	LowerCase          // local names: [a-z][a-z0-9_]*
	UpperCase          // member names: [A-Z][A-Z0-9_]*
)

// CaseOf returns how a name is written.
func CaseOf(name string) NameCase {
	if name == "" {
		return MixedCase
	}
	lower, upper := name[0] >= 'a' && name[0] <= 'z', isUpper(name[0])
	for i := 1; i < len(name); i++ {
		c := name[i]
		switch {
		case isDigit(c) || c == '_':
		case c >= 'a' && c <= 'z':
			upper = false
		case isUpper(c):
			lower = false
		default:
			return MixedCase
		}
	}
	switch {
	case lower:
		return LowerCase
	case upper:
		return UpperCase
	}
	return MixedCase
}
