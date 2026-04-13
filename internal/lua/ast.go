// Package lua defines the Lua AST node types used as an intermediate representation
// between TypeScript AST transformation and Lua source code emission.
package lua

// SyntaxKind identifies the type of a Lua AST node.
type SyntaxKind int

const (
	KindFile SyntaxKind = iota
	KindBlock
	// Statements
	KindDoStatement
	KindVariableDeclarationStatement
	KindAssignmentStatement
	KindIfStatement
	KindWhileStatement
	KindRepeatStatement
	KindForStatement
	KindForInStatement
	KindGotoStatement
	KindLabelStatement
	KindReturnStatement
	KindBreakStatement
	KindContinueStatement // Luau only
	KindExpressionStatement
	// Expressions
	KindStringLiteral
	KindNumericLiteral
	KindNilKeyword
	KindDotsKeyword
	KindTrueKeyword
	KindFalseKeyword
	KindFunctionExpression
	KindTableFieldExpression
	KindTableExpression
	KindUnaryExpression
	KindBinaryExpression
	KindConditionalExpression // Luau: if cond then expr else expr
	KindCallExpression
	KindMethodCallExpression
	KindIdentifier
	KindTableIndexExpression
	KindParenthesizedExpression
	// Comments as AST nodes
	KindCommentExpression
	KindCommentStatement
	// Escape hatch for incremental migration (deprecated — use Comment nodes)
	KindRawExpression
	KindRawStatement
)

// Operator represents a Lua operator (binary or unary).
type Operator int

const (
	// Arithmetic
	OpAdd Operator = iota
	OpSub
	OpMul
	OpDiv
	OpFloorDiv
	OpMod
	OpPow
	OpNeg // unary -
	// String
	OpConcat
	// Length
	OpLen // unary #
	// Relational
	OpEq
	OpNeq
	OpLt
	OpLe
	OpGt
	OpGe
	// Logical
	OpAnd
	OpOr
	OpNot // unary not
	// Bitwise
	OpBitAnd
	OpBitOr
	OpBitXor
	OpBitShr
	OpBitShl
	OpBitNot // unary ~
)

// NodeFlags modifies how a node is printed.
type NodeFlags int

const (
	FlagNone        NodeFlags = 0
	FlagInline      NodeFlags = 1 << iota // Keep function body on same line
	FlagDeclaration                       // On FunctionExpression: printer may use `function foo()` syntax for assignments.
	// For local declarations, the printer always uses `local function` regardless of this flag.
	FlagTableUnpackCall // Marks a table.unpack call
)

// SourcePos records the original TypeScript source position for source map generation.
// Zero value (HasPos == false) means "no position".
type SourcePos struct {
	Line       int    // 0-based line in TS source
	Column     int    // 0-based UTF-16 column offset
	HasPos     bool   // true if this position was set
	SourceName string // original TS name when the identifier was renamed (e.g. "type" → "____type")
}

// Positioned is implemented by all expression and statement nodes.
type Positioned interface {
	SetSourcePos(line, col int)
}

// Node is the base interface for all Lua AST nodes.
type Node interface {
	Kind() SyntaxKind
	SourcePosition() SourcePos
}

// Expression is a Lua expression node.
type Expression interface {
	Node
	exprNode()
}

// Statement is a Lua statement node.
type Statement interface {
	Node
	stmtNode()
}

// baseExpr is embedded in all expression types.
type baseExpr struct{ Pos SourcePos }

func (baseExpr) exprNode()                    {}
func (b *baseExpr) SourcePosition() SourcePos { return b.Pos }
func (b *baseExpr) SetSourcePos(line, col int) {
	b.Pos = SourcePos{Line: line, Column: col, HasPos: true}
}

// baseStmt is embedded in all statement types.
type baseStmt struct{ Pos SourcePos }

func (baseStmt) stmtNode()                    {}
func (b *baseStmt) SourcePosition() SourcePos { return b.Pos }
func (b *baseStmt) SetSourcePos(line, col int) {
	b.Pos = SourcePos{Line: line, Column: col, HasPos: true}
}

// Comments holds leading/trailing comments for a statement.
type Comments struct {
	LeadingComments  []string
	TrailingComments []string
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

type StringLiteral struct {
	baseExpr
	Value string
}

func (n *StringLiteral) Kind() SyntaxKind { return KindStringLiteral }

type NumericLiteral struct {
	baseExpr
	Value string // stored as text to preserve formatting (e.g. "0xFF", "1e3")
}

func (n *NumericLiteral) Kind() SyntaxKind { return KindNumericLiteral }

type NilLiteral struct{ baseExpr }

func (n *NilLiteral) Kind() SyntaxKind { return KindNilKeyword }

func IsNilLiteral(e Expression) bool { _, ok := e.(*NilLiteral); return ok }

type DotsLiteral struct{ baseExpr }

func (n *DotsLiteral) Kind() SyntaxKind { return KindDotsKeyword }

type BooleanLiteral struct {
	baseExpr
	Value bool
}

func (n *BooleanLiteral) Kind() SyntaxKind {
	if n.Value {
		return KindTrueKeyword
	}
	return KindFalseKeyword
}

type Identifier struct {
	baseExpr
	Text string
}

func (n *Identifier) Kind() SyntaxKind { return KindIdentifier }

type BinaryExpression struct {
	baseExpr
	Left     Expression
	Operator Operator
	Right    Expression
}

func (n *BinaryExpression) Kind() SyntaxKind { return KindBinaryExpression }

// ConditionalExpression is Luau's ternary: if cond then expr else expr
type ConditionalExpression struct {
	baseExpr
	Condition Expression
	WhenTrue  Expression
	WhenFalse Expression
}

func (n *ConditionalExpression) Kind() SyntaxKind { return KindConditionalExpression }

type UnaryExpression struct {
	baseExpr
	Operator Operator
	Operand  Expression
}

func (n *UnaryExpression) Kind() SyntaxKind { return KindUnaryExpression }

type CallExpression struct {
	baseExpr
	Expression Expression
	Params     []Expression
}

func (n *CallExpression) Kind() SyntaxKind { return KindCallExpression }

type MethodCallExpression struct {
	baseExpr
	Prefix Expression
	Name   string
	Params []Expression
}

func (n *MethodCallExpression) Kind() SyntaxKind { return KindMethodCallExpression }

type TableFieldExpression struct {
	baseExpr
	Value    Expression
	Key      Expression // nil for array-style fields (positional)
	Computed bool       // true → always emit [key] = val bracket notation
}

func (n *TableFieldExpression) Kind() SyntaxKind { return KindTableFieldExpression }

type TableExpression struct {
	baseExpr
	Fields []*TableFieldExpression
}

func (n *TableExpression) Kind() SyntaxKind { return KindTableExpression }

type TableIndexExpression struct {
	baseExpr
	Table Expression
	Index Expression
}

func (n *TableIndexExpression) Kind() SyntaxKind { return KindTableIndexExpression }

type FunctionExpression struct {
	baseExpr
	Params []*Identifier
	Dots   bool
	Body   *Block
	Flags  NodeFlags
}

func (n *FunctionExpression) Kind() SyntaxKind { return KindFunctionExpression }

// ParenthesizedExpression forces parentheses around the inner expression.
// Use ONLY for semantic parens (multi-return truncation, structural disambiguation) —
// never for operator precedence grouping, which the printer handles automatically.
type ParenthesizedExpression struct {
	baseExpr
	Inner Expression
}

func (n *ParenthesizedExpression) Kind() SyntaxKind { return KindParenthesizedExpression }

// CommentExpression emits `nil --[[ text ]]` — a nil placeholder with a comment.
// Used for unimplemented features (TODOs) where an expression is required.
type CommentExpression struct {
	baseExpr
	Text string
}

func (n *CommentExpression) Kind() SyntaxKind { return KindCommentExpression }

// RawExpression holds pre-formatted Lua code during incremental migration.
type RawExpression struct {
	baseExpr
	Code string
}

func (n *RawExpression) Kind() SyntaxKind { return KindRawExpression }

// IsAssignmentTarget reports whether expr is a valid Lua assignment target
// (identifier or table index expression).
func IsAssignmentTarget(expr Expression) bool {
	switch expr.(type) {
	case *Identifier, *TableIndexExpression:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

type Block struct {
	Pos        SourcePos
	Statements []Statement
}

func (n *Block) Kind() SyntaxKind          { return KindBlock }
func (n *Block) SourcePosition() SourcePos { return n.Pos }

type DoStatement struct {
	baseStmt
	Comments
	Body *Block
}

func (n *DoStatement) Kind() SyntaxKind { return KindDoStatement }

type VariableDeclarationStatement struct {
	baseStmt
	Comments
	Left  []*Identifier
	Right []Expression // nil means no initializer
}

func (n *VariableDeclarationStatement) Kind() SyntaxKind { return KindVariableDeclarationStatement }

type AssignmentStatement struct {
	baseStmt
	Comments
	Left  []Expression // Identifier or TableIndexExpression
	Right []Expression
}

func (n *AssignmentStatement) Kind() SyntaxKind { return KindAssignmentStatement }

type IfStatement struct {
	baseStmt
	Comments
	Condition Expression
	IfBlock   *Block
	ElseBlock interface{} // *Block or *IfStatement, nil if no else
}

func (n *IfStatement) Kind() SyntaxKind { return KindIfStatement }

type WhileStatement struct {
	baseStmt
	Comments
	Condition Expression
	Body      *Block
}

func (n *WhileStatement) Kind() SyntaxKind { return KindWhileStatement }

type RepeatStatement struct {
	baseStmt
	Comments
	Condition Expression
	Body      *Block
}

func (n *RepeatStatement) Kind() SyntaxKind { return KindRepeatStatement }

type ForStatement struct {
	baseStmt
	Comments
	ControlVariable            *Identifier
	ControlVariableInitializer Expression
	LimitExpression            Expression
	StepExpression             Expression // nil means step of 1
	Body                       *Block
}

func (n *ForStatement) Kind() SyntaxKind { return KindForStatement }

type ForInStatement struct {
	baseStmt
	Comments
	Names       []*Identifier
	Expressions []Expression
	Body        *Block
}

func (n *ForInStatement) Kind() SyntaxKind { return KindForInStatement }

type GotoStatement struct {
	baseStmt
	Comments
	Label string
}

func (n *GotoStatement) Kind() SyntaxKind { return KindGotoStatement }

type LabelStatement struct {
	baseStmt
	Comments
	Name string
}

func (n *LabelStatement) Kind() SyntaxKind { return KindLabelStatement }

type ReturnStatement struct {
	baseStmt
	Comments
	Expressions []Expression
}

func (n *ReturnStatement) Kind() SyntaxKind { return KindReturnStatement }

type BreakStatement struct {
	baseStmt
	Comments
}

func (n *BreakStatement) Kind() SyntaxKind { return KindBreakStatement }

// ContinueStatement represents the Luau-only `continue` keyword.
type ContinueStatement struct {
	baseStmt
	Comments
}

func (n *ContinueStatement) Kind() SyntaxKind { return KindContinueStatement }

type ExpressionStatement struct {
	baseStmt
	Comments
	Expression Expression
}

func (n *ExpressionStatement) Kind() SyntaxKind { return KindExpressionStatement }

// RawStatement holds pre-formatted Lua code during incremental migration.
// CommentStatement emits `-- text` as a standalone comment line.
type CommentStatement struct {
	baseStmt
	Comments
	Text string
}

func (n *CommentStatement) Kind() SyntaxKind { return KindCommentStatement }

type RawStatement struct {
	baseStmt
	Comments
	Code string
}

func (n *RawStatement) Kind() SyntaxKind { return KindRawStatement }

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

func Ident(text string) *Identifier                          { return &Identifier{Text: text} }
func Str(value string) *StringLiteral                        { return &StringLiteral{Value: value} }
func Num(value string) *NumericLiteral                       { return &NumericLiteral{Value: value} }
func Bool(value bool) *BooleanLiteral                        { return &BooleanLiteral{Value: value} }
func Nil() *NilLiteral                                       { return &NilLiteral{} }
func Dots() *DotsLiteral                                     { return &DotsLiteral{} }
func Comment(text string) *CommentExpression                 { return &CommentExpression{Text: text} }
func CommentStmt(text string) *CommentStatement              { return &CommentStatement{Text: text} }
func Raw(code string) *RawExpression                         { return &RawExpression{Code: code} }
func RawStmt(code string) *RawStatement                      { return &RawStatement{Code: code} }
func Table(fields ...*TableFieldExpression) *TableExpression { return &TableExpression{Fields: fields} }

func Field(value Expression) *TableFieldExpression {
	return &TableFieldExpression{Value: value}
}

func KeyField(key, value Expression) *TableFieldExpression {
	return &TableFieldExpression{Key: key, Value: value}
}

func ComputedKeyField(key, value Expression) *TableFieldExpression {
	return &TableFieldExpression{Key: key, Value: value, Computed: true}
}

func Call(fn Expression, params ...Expression) *CallExpression {
	return &CallExpression{Expression: fn, Params: params}
}

func MethodCall(prefix Expression, name string, params ...Expression) *MethodCallExpression {
	return &MethodCallExpression{Prefix: prefix, Name: name, Params: params}
}

func Binary(left Expression, op Operator, right Expression) *BinaryExpression {
	return &BinaryExpression{Left: left, Operator: op, Right: right}
}

// Conditional creates a Luau conditional expression: if cond then whenTrue else whenFalse
func Conditional(cond, whenTrue, whenFalse Expression) *ConditionalExpression {
	return &ConditionalExpression{Condition: cond, WhenTrue: whenTrue, WhenFalse: whenFalse}
}

func Unary(op Operator, operand Expression) *UnaryExpression {
	return &UnaryExpression{Operator: op, Operand: operand}
}

func Index(table, index Expression) *TableIndexExpression {
	return &TableIndexExpression{Table: table, Index: index}
}

// Paren forces parentheses around the inner expression in the printed output.
// Only for semantic parens: (f()) to truncate multi-return, or structural disambiguation.
// Never use for operator precedence — the printer handles that via printExprInParensIfNeeded.
func Paren(inner Expression) *ParenthesizedExpression {
	return &ParenthesizedExpression{Inner: inner}
}

func Assign(left []Expression, right []Expression) *AssignmentStatement {
	return &AssignmentStatement{Left: left, Right: right}
}

func LocalDecl(names []*Identifier, values []Expression) *VariableDeclarationStatement {
	return &VariableDeclarationStatement{Left: names, Right: values}
}

func Return(exprs ...Expression) *ReturnStatement {
	return &ReturnStatement{Expressions: exprs}
}

func ExprStmt(expr Expression) *ExpressionStatement {
	return &ExpressionStatement{Expression: expr}
}

func If(cond Expression, ifBlock *Block, elseBlock interface{}) *IfStatement {
	return &IfStatement{Condition: cond, IfBlock: ifBlock, ElseBlock: elseBlock}
}

func While(cond Expression, body *Block) *WhileStatement {
	return &WhileStatement{Condition: cond, Body: body}
}

func Repeat(cond Expression, body *Block) *RepeatStatement {
	return &RepeatStatement{Condition: cond, Body: body}
}

func Do(stmts ...Statement) *DoStatement {
	return &DoStatement{Body: &Block{Statements: stmts}}
}

func ForIn(names []*Identifier, exprs []Expression, body *Block) *ForInStatement {
	return &ForInStatement{Names: names, Expressions: exprs, Body: body}
}

func Goto(label string) *GotoStatement {
	return &GotoStatement{Label: label}
}

func GotoLabel(name string) *LabelStatement {
	return &LabelStatement{Name: name}
}

func Break() *BreakStatement {
	return &BreakStatement{}
}

func Continue() *ContinueStatement {
	return &ContinueStatement{}
}

// GetComments returns a pointer to the embedded Comments struct.
// Since all statement types embed Comments, this allows generic access.
func (c *Comments) GetComments() *Comments { return c }
