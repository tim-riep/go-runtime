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

// Comments are the comments of a line-based node: own lines before it and
// the comment at the end of its first line.
type Comments struct {
	// Blank tells whether a blank line separates the node from the one
	// before it.
	Blank    bool
	Leading  []Leading
	Trailing *Comment
}

// Leading is a comment on its own line, possibly after a blank line.
type Leading struct {
	Blank   bool
	Comment Comment
}

// Break is an optional line break inside an expression or list, after the
// token it belongs to.
type Break struct {
	Line     bool     // the author broke the line here
	Trailing *Comment // comment at the end of the broken line
	Lines    []Line   // comment and blank lines before the continuation
}

// List holds the line breaks of a parenthesized or bracketed list: after
// the opening token, after each comma, and before the closing token.
type List struct {
	Open   Break
	Commas []Break
	Close  Break // set when the closing token starts its own line
}

// FileKind is the kind of object a file defines.
type FileKind int

// File kinds.
const (
	ClassFile FileKind = iota
	InterfaceFile
	IncludeFile
)

// File is a parsed source file.
type File struct {
	Path   string
	Kind   FileKind
	Header []Leading // comments before the declaration
	// HeaderBlank tells whether a blank line separates the header comments
	// from the declaration.
	HeaderBlank bool
	Class       *ClassDecl
	Interface   *InterfaceDecl
	Include     *IncludeDecl
	// Footer holds the comment and blank lines after the final end.
	Footer []Line
}

// Name returns the global name the file defines.
func (f *File) Name() Name {
	switch {
	case f.Class != nil:
		return f.Class.Name
	case f.Interface != nil:
		return f.Interface.Name
	case f.Include != nil:
		return f.Include.Name
	}
	return Name{}
}

// Name is a global name with its position.
type Name struct {
	Pos  Pos
	Name string
}

// Visibility of classes and members.
type Visibility string

// Visibilities.
const (
	Public  Visibility = "public"
	Package Visibility = "package"
	Private Visibility = "private"
)

// ClassDecl is a class, exception class or test class.
type ClassDecl struct {
	Pos        Pos
	Doc        Comments // trailing comment of the class line
	Visibility Visibility
	Test       bool
	Open       bool
	Name       Name
	Extends    *Name
	Implements []Name
	// Native classes, marked with executed by kernel, have objects that
	// carry a value of the kernel.
	Native  bool
	Members []Member
	End     EndLine
}

// EndLine is the closing "end" of a block with the comments before it and
// at its end.
type EndLine struct {
	Pos      Pos
	Leading  []Leading
	Trailing *Comment
}

// InterfaceDecl is an interface.
type InterfaceDecl struct {
	Pos        Pos
	Doc        Comments
	Visibility Visibility
	Name       Name
	Extends    []Name
	Methods    []*MethodSig
	End        EndLine
}

// IncludeKind is what an include contains.
type IncludeKind string

// Include kinds.
const (
	MembersInclude    IncludeKind = "members"
	StatementsInclude IncludeKind = "statements"
)

// IncludeDecl is an include file.
type IncludeDecl struct {
	Pos     Pos
	Doc     Comments
	Kind    IncludeKind
	Name    Name
	Members []Member // members include
	Body    *Block   // statements include
}

// Member is a member of a class or members include.
type Member interface {
	Comments() *Comments
	Start() Pos
}

// MemberBase holds what every member has.
type MemberBase struct {
	Pos Pos
	Doc Comments
}

func (m *MemberBase) Comments() *Comments { return &m.Doc }
func (m *MemberBase) Start() Pos          { return m.Pos }

// ConstantDecl is a constant.
type ConstantDecl struct {
	MemberBase
	Visibility Visibility
	Name       Ident
	Type       *Type
	Value      Expr
	Break      Break // after "="
}

// AttributeDecl is an attribute.
type AttributeDecl struct {
	MemberBase
	Visibility Visibility
	Static     bool
	Name       Ident
	Type       *Type
	Value      Expr // nil without initial value
	Break      Break
}

// MethodDecl is a method or the constructor (Constructor true, no name).
type MethodDecl struct {
	MemberBase
	Visibility  Visibility
	Static      bool
	Open        bool
	Override    bool
	Test        bool
	Constructor bool
	Sig         *MethodSig
	Native      bool // executed by kernel: no body
	Body        *Block
}

// MethodSig is the signature of a method.
type MethodSig struct {
	Pos     Pos
	Doc     Comments // for interface methods
	Name    Ident
	Params  []*Param
	List    List
	Returns *Type
	Raises  []Name
}

// Param is a parameter.
type Param struct {
	Name Ident
	Type *Type
}

// IncludeMember includes a members include.
type IncludeMember struct {
	MemberBase
	Name    Name
	IfFound bool
}

// TypeKind is the kind of a type expression.
type TypeKind int

// Type kinds.
const (
	BuiltinType TypeKind = iota
	NamedType            // a global name
	ArrayType
	MapType
	FuncType
	ExtensionPointType // extension point GLOBAL: the value of an extension point
)

// Type is a type expression.
type Type struct {
	Pos       Pos
	Kind      TypeKind
	Name      string // built-in or global name
	Precision int    // DEC
	Scale     int
	Elem      *Type // array element, map value
	Key       *Type // map key
	Params    []*Type
	Returns   *Type
	Raises    []Name
	Optional  bool
}

// Block is a sequence of statements. End is the line that closes it:
// end, or the else, elseif, catch or finally line that follows, with the
// comments before that line and at its end.
type Block struct {
	Stmts []Stmt
	End   EndLine
}

// Stmt is a statement.
type Stmt interface {
	Comments() *Comments
	Start() Pos
}

// StmtBase holds what every statement has.
type StmtBase struct {
	Pos Pos
	Doc Comments
}

func (s *StmtBase) Comments() *Comments { return &s.Doc }
func (s *StmtBase) Start() Pos          { return s.Pos }

// Statements.
type (
	VarStmt struct {
		StmtBase
		Name  Ident
		Type  *Type // nil when derived
		Value Expr
		Break Break
	}
	AssignStmt struct {
		StmtBase
		Target Expr
		Value  Expr
		Break  Break
	}
	ExprStmt struct {
		StmtBase
		X Expr
	}
	IfStmt struct {
		StmtBase
		Clauses []*IfClause // if and elseif
		Else    *Block      // nil without else
	}
	ForStmt struct {
		StmtBase
		Key  *Ident // for k, v in m: Key is k
		Var  Ident
		In   Expr // for ... in
		From Expr // for ... from ... to
		To   Expr
		Step Expr
		Body *Block
	}
	WhileStmt struct {
		StmtBase
		Cond Expr
		Body *Block
	}
	BreakStmt    struct{ StmtBase }
	ContinueStmt struct{ StmtBase }
	ReturnStmt   struct {
		StmtBase
		Value Expr
	}
	RaiseStmt struct {
		StmtBase
		Class Name
		Args  []Expr
		Comma []Break
	}
	TryStmt struct {
		StmtBase
		Body    *Block
		Catches []*CatchClause
		Finally *Block
	}
	AssertStmt struct {
		StmtBase
		Cond    Expr
		Message Expr
	}
	LogStmt struct {
		StmtBase
		Level string
		Value Expr
	}
	IncludeStmt struct {
		StmtBase
		Name    Name
		IfFound bool
	}
	// DatabaseStmt is commit or rollback of the default session or of
	// Session.
	DatabaseStmt struct {
		StmtBase
		Keyword string
		Session Expr // nil for the default session
	}
	// AuthCheckStmt is check authorization: it raises /RT/NOT_AUTHORIZED
	// when the user lacks the authorization. Doc.Trailing is the comment
	// after the object, End the end line.
	AuthCheckStmt struct {
		StmtBase
		AuthCheck
		End EndLine
	}
	// RowStmt is insert, update or delete of one row, given as value of the
	// row type of a table.
	RowStmt struct {
		StmtBase
		Op    string // insert, update or delete
		Row   Expr
		Using Expr
	}
)

// IfClause is the condition and block of if or elseif.
type IfClause struct {
	Pos  Pos
	Cond Expr
	Body *Block
}

// CatchClause is a catch block.
type CatchClause struct {
	Pos   Pos
	Var   Ident
	Class Name
	Body  *Block
}

// Expr is an expression.
type Expr interface{ Start() Pos }

// Ident is a name without namespace: a local or member name.
type Ident struct {
	Pos  Pos
	Name string
}

func (e *Ident) Start() Pos { return e.Pos }

// Expressions.
type (
	GlobalExpr struct{ Name Name }
	// LiteralExpr is an integer, decimal, true, false or null.
	LiteralExpr struct {
		Pos  Pos
		Kind Kind // INT, DEC or KEYWORD
		Raw  string
	}
	StringExpr struct {
		Pos   Pos
		Parts []StringPart
	}
	MeExpr    struct{ Pos Pos }
	SuperExpr struct{ Pos Pos }
	ParenExpr struct {
		Pos  Pos
		X    Expr
		List List
	}
	UnaryExpr struct {
		Pos Pos
		Op  string // "-" or "not"
		X   Expr
	}
	BinaryExpr struct {
		X     Expr
		OpPos Pos
		Op    string // + - * / mod = <> < > <= >= and or default
		Y     Expr
		Break Break // after the operator
	}
	// IsExpr is "x is [not] null" or "x is [not] empty".
	IsExpr struct {
		X     Expr
		OpPos Pos
		Not   bool
		What  string // "null" or "empty"
	}
	AsExpr struct {
		X    Expr
		Type *Type
	}
	CallExpr struct {
		Fun  Expr
		Args []Arg
		List List
	}
	SelectExpr struct {
		X    Expr
		Name Ident
	}
	IndexExpr struct {
		X     Expr
		Index Expr
		List  List
	}
	// NewExpr creates an object, a struct or an empty collection.
	NewExpr struct {
		Pos  Pos
		Type *Type
		Args []Arg
		List List
		Call bool // with parentheses
	}
	ArrayExpr struct {
		Pos   Pos
		Elems []Expr
		List  List
	}
	MapExpr struct {
		Pos    Pos
		Keys   []Expr
		Values []Expr
		List   List
	}
	FuncExpr struct {
		Pos     Pos
		Params  []*Param
		List    List
		Returns *Type
		Raises  []Name
		Short   Expr     // function (...) => expr
		Body    *Block   // function (...) ... end
		Head    *Comment // comment at the end of the first line of a block function
	}
)

// Database expressions.
type (
	// QueryExpr is select: it reads the rows of a table, all (Mode ""),
	// one ("single") or their number ("count").
	QueryExpr struct {
		Pos   Pos
		Mode  string
		Table Name
		Where Expr // nil without where
		Order []OrderItem
		Limit Expr
		Using Expr
	}
	// UpdateExpr changes the rows of a table that match Where and yields
	// their number.
	UpdateExpr struct {
		Pos   Pos
		Table Name
		Sets  []SetItem
		Where Expr
		Using Expr
	}
	// DeleteExpr deletes the rows of a table that match Where and yields
	// their number.
	DeleteExpr struct {
		Pos   Pos
		Table Name
		Where Expr
		Using Expr
	}
	// OpenSessionExpr opens another database session.
	OpenSessionExpr struct{ Pos Pos }
	// TransformExpr runs a transformation on a value or an XML document.
	TransformExpr struct {
		Pos    Pos
		Source Expr
		Using  Name
		Params []ParamValue
		Into   *Type // nil: the result is a STRING
	}
	// ExtensionPointExpr selects the implementations of an extension point
	// by the values of its filter fields.
	ExtensionPointExpr struct {
		Pos     Pos
		Point   Name
		Filters []ParamValue
	}
	// AuthorizedExpr tells whether the user has an authorization. Head is
	// the comment after the object, End the comments before end.
	AuthorizedExpr struct {
		Pos Pos
		AuthCheck
		Head *Comment
		End  EndLine
	}
)

// AuthCheck is the object and the fields of an authorization check.
type AuthCheck struct {
	Object Name
	Fields []*AuthField
}

// AuthField is FIELD = value or FIELD = any of an authorization check, on
// a line of its own.
type AuthField struct {
	Doc   Comments
	Name  Ident
	Any   bool
	Value Expr // nil for any
}

// ParamValue is NAME = value of transform ... parameters and of
// extension point ... filters.
type ParamValue struct {
	Name  Ident
	Value Expr
}

// OrderItem is a field to sort by.
type OrderItem struct {
	Field      Ident
	Descending bool
	Explicit   bool // ascending or descending was written
}

// SetItem is FIELD = value of an update.
type SetItem struct {
	Field Ident
	Value Expr
}

func (e *QueryExpr) Start() Pos          { return e.Pos }
func (e *UpdateExpr) Start() Pos         { return e.Pos }
func (e *DeleteExpr) Start() Pos         { return e.Pos }
func (e *OpenSessionExpr) Start() Pos    { return e.Pos }
func (e *TransformExpr) Start() Pos      { return e.Pos }
func (e *ExtensionPointExpr) Start() Pos { return e.Pos }
func (e *AuthorizedExpr) Start() Pos     { return e.Pos }

// Arg is an argument, possibly named (NAME: value).
type Arg struct {
	Name  *Ident
	Value Expr
}

// StringPart is literal text or a placeholder of a string.
type StringPart struct {
	Text string // decoded literal text
	Expr Expr   // placeholder, nil for text
}

func (e *GlobalExpr) Start() Pos  { return e.Name.Pos }
func (e *LiteralExpr) Start() Pos { return e.Pos }
func (e *StringExpr) Start() Pos  { return e.Pos }
func (e *MeExpr) Start() Pos      { return e.Pos }
func (e *SuperExpr) Start() Pos   { return e.Pos }
func (e *ParenExpr) Start() Pos   { return e.Pos }
func (e *UnaryExpr) Start() Pos   { return e.Pos }
func (e *BinaryExpr) Start() Pos  { return e.X.Start() }
func (e *IsExpr) Start() Pos      { return e.X.Start() }
func (e *AsExpr) Start() Pos      { return e.X.Start() }
func (e *CallExpr) Start() Pos    { return e.Fun.Start() }
func (e *SelectExpr) Start() Pos  { return e.X.Start() }
func (e *IndexExpr) Start() Pos   { return e.X.Start() }
func (e *NewExpr) Start() Pos     { return e.Pos }
func (e *ArrayExpr) Start() Pos   { return e.Pos }
func (e *MapExpr) Start() Pos     { return e.Pos }
func (e *FuncExpr) Start() Pos    { return e.Pos }
