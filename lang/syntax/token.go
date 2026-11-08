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

// Package syntax reads and prints source files of the go-runtime language:
// tokens, the abstract syntax tree with comments, the parser and the
// printer that produces the canonical form.
package syntax

import (
	"fmt"
	"slices"
)

// LanguageVersion is the newest language version. Grammar and canonical
// form are defined per version.
const LanguageVersion = 1

// Pos is a position in a source file. Line and Col start at 1; Col counts
// characters.
type Pos struct {
	Line int `json:"line"`
	Col  int `json:"column"`
	Off  int `json:"-"` // byte offset
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Kind is the kind of a token.
type Kind int

// Token kinds.
const (
	EOF     Kind = iota
	NEWLINE      // end of a line with the comments and blank lines after it
	IDENT        // a name without namespace: local (lower case) or member (upper case)
	GLOBAL       // a name with namespace: /NS/NAME
	INT          // 123
	DEC          // 12.50
	STRING       // "..." with placeholders
	KEYWORD      // a reserved word
	OP           // operator or punctuation
	ILLEGAL
)

var kindNames = [...]string{"end of file", "end of line", "name", "global name", "integer", "decimal", "string", "keyword", "operator", "illegal"}

func (k Kind) String() string { return kindNames[k] }

// Comment is a line comment. Text is everything after "#".
type Comment struct {
	Pos  Pos
	Text string
}

// Line is a line without code after a NEWLINE: blank or a comment.
type Line struct {
	Comment *Comment // nil for a blank line
}

// Token is a token of a source file.
type Token struct {
	Kind Kind
	Text string
	Pos  Pos
	// For NEWLINE: the comment at the end of the line, and the blank and
	// comment-only lines that follow it.
	Trailing *Comment
	Lines    []Line
}

func (t Token) String() string {
	switch t.Kind {
	case EOF, NEWLINE:
		return t.Kind.String()
	case KEYWORD, OP:
		return fmt.Sprintf("%q", t.Text)
	}
	return fmt.Sprintf("%s %s", t.Kind, t.Text)
}

// Keywords are the reserved words of the language. Some words, such as
// the log levels, "members", "statements", "test", "executed", "by",
// "kernel", "found" and "step", only have a meaning at certain places and
// remain usable as names.
var Keywords = map[string]bool{}

// DatabaseKeywords are reserved for database access in a later language
// part.
var DatabaseKeywords = []string{"commit", "rollback", "session", "using", "select", "insert", "update", "delete", "from", "where"}

// TransformKeywords belong to the expression transform.
var TransformKeywords = []string{"transform", "parameters", "into"}

// AuthorizationKeywords belong to authorization checks; "check" and "any"
// only have a meaning there and remain usable as names.
var AuthorizationKeywords = []string{"authorization", "authorized"}

func init() {
	for _, k := range []string{"class", "interface", "include", "public", "package", "private", "static", "open", "override",
		"extends", "implements", "constant", "attribute", "constructor", "method", "returns", "raises", "var", "if", "elseif",
		"else", "end", "for", "in", "to", "while", "break", "continue", "return", "raise", "with", "try", "catch", "finally",
		"assert", "log", "and", "or", "not", "is", "null", "empty", "default", "true", "false", "new", "me", "super",
		"function", "as", "of", "array", "map", "mod"} {
		Keywords[k] = true
	}
	for _, k := range slices.Concat(DatabaseKeywords, TransformKeywords, AuthorizationKeywords) {
		Keywords[k] = true
	}
}

// BuiltinTypes are the names of the built-in types.
var BuiltinTypes = map[string]bool{"STRING": true, "INT4": true, "INT8": true, "DEC": true, "DATE": true, "TIMESTAMP": true,
	"BOOL": true, "UUID": true, "BYTES": true, "SESSION": true}

// LogLevels are the levels of the log statement.
var LogLevels = []string{"debug", "info", "warning", "error"}
