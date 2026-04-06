package lua

import (
	"fmt"
	"regexp"
	"strings"
)

var luaIdentRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var luaFuncDeclNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

func isValidLuaDotKey(s string, allowUnicode bool) bool {
	if allowUnicode {
		return isValidLuaDotKeyUnicode(s)
	}
	if !luaIdentRegex.MatchString(s) {
		return false
	}
	return !isLuaKeyword(s)
}

func isValidLuaDotKeyUnicode(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		if c >= 0x80 {
			// LuaJIT accepts any byte 0x80-0xFD, and valid UTF-8 only produces
			// bytes in that range for non-ASCII characters.
			continue
		}
		return false
	}
	return !isLuaKeyword(s)
}

func isLuaKeyword(s string) bool {
	switch s {
	case "and", "bit", "bit32", "break", "do", "else", "elseif", "end",
		"false", "for", "function", "goto", "if", "in",
		"local", "nil", "not", "or", "repeat", "return",
		"then", "true", "until", "while":
		return true
	}
	return false
}

// OperatorString returns the Lua text for an operator.
func OperatorString(op Operator) string {
	switch op {
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	case OpDiv:
		return "/"
	case OpFloorDiv:
		return "//"
	case OpMod:
		return "%"
	case OpPow:
		return "^"
	case OpNeg:
		return "-"
	case OpConcat:
		return ".."
	case OpLen:
		return "#"
	case OpEq:
		return "=="
	case OpNeq:
		return "~="
	case OpLt:
		return "<"
	case OpLe:
		return "<="
	case OpGt:
		return ">"
	case OpGe:
		return ">="
	case OpAnd:
		return "and"
	case OpOr:
		return "or"
	case OpNot:
		return "not"
	case OpBitAnd:
		return "&"
	case OpBitOr:
		return "|"
	case OpBitXor:
		return "~"
	case OpBitShr:
		return ">>"
	case OpBitShl:
		return "<<"
	case OpBitNot:
		return "~"
	}
	return "?"
}

// Operator precedence (higher = binds tighter).
var operatorPrecedence = map[Operator]int{
	OpOr:       1,
	OpAnd:      2,
	OpEq:       3,
	OpNeq:      3,
	OpLt:       3,
	OpLe:       3,
	OpGt:       3,
	OpGe:       3,
	OpBitOr:    4,
	OpBitXor:   5,
	OpBitAnd:   6,
	OpBitShl:   7,
	OpBitShr:   7,
	OpConcat:   8,
	OpAdd:      9,
	OpSub:      9,
	OpMul:      10,
	OpDiv:      10,
	OpFloorDiv: 10,
	OpMod:      10,
	OpNot:      11,
	OpLen:      11,
	OpNeg:      11,
	OpBitNot:   11,
	OpPow:      12,
}

func isRightAssociative(op Operator) bool {
	return op == OpConcat || op == OpPow
}

// Mapping records a single source map mapping entry.
type Mapping struct {
	GenLine int    // 0-based generated line
	GenCol  int    // 0-based generated column (bytes)
	SrcLine int    // 0-based source line
	SrcCol  int    // 0-based source column (UTF-16)
	Name    string // original TS name (empty if no rename)
}

// Printer converts Lua AST nodes to formatted source code.
type Printer struct {
	buf          strings.Builder
	indent       int
	allowUnicode bool

	// Source map tracking (only active when sourceMap == true)
	sourceMap bool
	genLine   int
	genCol    int
	mappings  []Mapping
}

// NewPrinter creates a Printer.
func NewPrinter() *Printer {
	return &Printer{}
}

// NewPrinterWithUnicode creates a Printer that accepts unicode identifiers (for LuaJIT).
func NewPrinterWithUnicode() *Printer {
	return &Printer{allowUnicode: true}
}

func (p *Printer) pushIndent() { p.indent++ }
func (p *Printer) popIndent() {
	if p.indent > 0 {
		p.indent--
	}
}
func (p *Printer) writeIndent() { p.write(strings.Repeat("    ", p.indent)) }
func (p *Printer) write(s string) {
	if p.sourceMap {
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				p.genLine++
				p.genCol = 0
			} else {
				p.genCol++
			}
		}
	}
	p.buf.WriteString(s)
}
func (p *Printer) writeln(s string) { p.writeIndent(); p.write(s); p.write("\n") }
func (p *Printer) newline()         { p.write("\n") }
func (p *Printer) appendSemicolon() {
	// Replace trailing newline with ";\n" to put semicolon at end of previous line
	s := p.buf.String()
	if strings.HasSuffix(s, "\n") {
		p.buf.Reset()
		p.buf.WriteString(s[:len(s)-1])
		// Position tracking: we removed a \n and will re-add it after ';'.
		// The genLine/genCol were already advanced past the \n, so they still
		// point to (nextLine, 0). The semicolon goes on the previous line,
		// but since we immediately write \n again, the net effect is the same.
	}
	p.buf.WriteByte(';')
	p.buf.WriteByte('\n')
	// No need to update genLine/genCol — the \n we removed was already counted,
	// and we're putting it back.
}

// PrintStatements prints a slice of statements and returns the source code.
// If allowUnicode is true, unicode identifiers are allowed in dot/colon syntax (for LuaJIT).
func PrintStatements(stmts []Statement, allowUnicode bool) string {
	p := NewPrinter()
	p.allowUnicode = allowUnicode
	p.printBlock(stmts)
	return p.buf.String()
}

// PrintResult contains Lua source and collected source map mappings.
type PrintResult struct {
	Code     string
	Mappings []Mapping
}

// PrintStatementsWithSourceMap prints statements and collects source map mappings.
func PrintStatementsWithSourceMap(stmts []Statement, allowUnicode bool) PrintResult {
	p := &Printer{allowUnicode: allowUnicode, sourceMap: true}
	p.printBlock(stmts)
	return PrintResult{
		Code:     p.buf.String(),
		Mappings: p.mappings,
	}
}

// PrintExpression prints a single expression and returns the source code.
func PrintExpression(expr Expression) string {
	p := NewPrinter()
	p.printExpression(expr)
	return p.buf.String()
}

// PrintExpressionIndented prints an expression with a given base indent level.
func PrintExpressionIndented(expr Expression, indent int) string {
	p := NewPrinter()
	p.indent = indent
	p.printExpression(expr)
	return p.buf.String()
}

func (p *Printer) emitMapping(node Node) {
	if !p.sourceMap {
		return
	}
	pos := node.SourcePosition()
	if !pos.HasPos {
		return
	}
	p.mappings = append(p.mappings, Mapping{
		GenLine: p.genLine,
		GenCol:  p.genCol,
		SrcLine: pos.Line,
		SrcCol:  pos.Column,
		Name:    pos.SourceName,
	})
}

// ---------------------------------------------------------------------------
// Statement printing
// ---------------------------------------------------------------------------

func (p *Printer) printBlock(stmts []Statement) {
	var prevStmt Statement
	for _, stmt := range stmts {
		if prevStmt != nil && p.needsSemicolon(prevStmt, stmt) {
			// Append semicolon to end of previous line (replace trailing \n with ;\n)
			p.appendSemicolon()
		}
		p.printLeadingComments(stmt)
		p.printStatement(stmt)
		prevStmt = stmt
		// Lua 5.1/LuaJIT require break/return to be the last statement in a block
		if isTerminalStatement(stmt) {
			break
		}
	}
}

func isTerminalStatement(stmt Statement) bool {
	switch stmt.(type) {
	case *BreakStatement, *ContinueStatement, *ReturnStatement, *GotoStatement:
		return true
	}
	return false
}

func (p *Printer) printStatement(stmt Statement) {
	p.emitMapping(stmt)
	switch s := stmt.(type) {
	case *DoStatement:
		p.printDoStatement(s)
	case *VariableDeclarationStatement:
		p.printVariableDeclaration(s)
	case *AssignmentStatement:
		p.printAssignment(s)
	case *IfStatement:
		p.printIfStatement(s, false)
	case *WhileStatement:
		p.printWhileStatement(s)
	case *RepeatStatement:
		p.printRepeatStatement(s)
	case *ForStatement:
		p.printForStatement(s)
	case *ForInStatement:
		p.printForInStatement(s)
	case *GotoStatement:
		p.writeln("goto " + s.Label)
	case *LabelStatement:
		p.writeln("::" + s.Name + "::")
	case *ReturnStatement:
		p.printReturnStatement(s)
	case *BreakStatement:
		p.writeln("break")
	case *ContinueStatement:
		p.writeln("continue")
	case *ExpressionStatement:
		p.printExpressionStatement(s)
	case *CommentStatement:
		p.writeln("-- " + s.Text)
	case *RawStatement:
		// Raw pre-formatted code: emit each line with current indent
		lines := strings.Split(s.Code, "\n")
		for _, line := range lines {
			if line != "" {
				p.writeln(line)
			}
		}
	}
}

func (p *Printer) printDoStatement(s *DoStatement) {
	p.writeln("do")
	p.pushIndent()
	p.printBlock(s.Body.Statements)
	p.popIndent()
	p.writeln("end")
}

func (p *Printer) printVariableDeclaration(s *VariableDeclarationStatement) {
	// Check for function definition syntax: local function foo(...)
	if p.isFunctionDefinition(s.Left, s.Right) {
		p.printFunctionDefinition("local function", s.Left[0].Text, s.Right[0].(*FunctionExpression))
		return
	}

	p.writeIndent()
	p.write("local ")
	for i, id := range s.Left {
		if i > 0 {
			p.write(", ")
		}
		p.emitMapping(id)
		p.write(id.Text)
	}
	if len(s.Right) > 0 {
		p.write(" = ")
		// Declarations use comma-separated (no line breaking) — matching TSTL which
		// only uses multi-line formatting for return/table/function-arg contexts.
		p.printExprCommaSep(s.Right)
	}
	p.newline()
}

func (p *Printer) printAssignment(s *AssignmentStatement) {
	// Check for function definition syntax: function foo.bar(...)
	if len(s.Left) == 1 && len(s.Right) == 1 {
		if fe, ok := s.Right[0].(*FunctionExpression); ok && fe.Flags&FlagDeclaration != 0 {
			name := PrintExpression(s.Left[0])
			if isValidFunctionDeclName(name) {
				p.printFunctionDefinition("function", name, fe)
				return
			}
		}
	}

	p.writeIndent()
	for i, expr := range s.Left {
		if i > 0 {
			p.write(", ")
		}
		p.printExpression(expr)
	}
	p.write(" = ")
	// Assignments use comma-separated (no line breaking) — matching TSTL.
	p.printExprCommaSep(s.Right)
	p.newline()
}

func (p *Printer) printIfStatement(s *IfStatement, isElseIf bool) {
	p.writeIndent()
	if isElseIf {
		p.write("elseif ")
	} else {
		p.write("if ")
	}
	p.printExpression(s.Condition)
	p.write(" then")
	p.newline()
	p.pushIndent()
	p.printBlock(s.IfBlock.Statements)
	p.popIndent()

	switch eb := s.ElseBlock.(type) {
	case *IfStatement:
		p.printIfStatement(eb, true)
		return
	case *Block:
		p.writeln("else")
		p.pushIndent()
		p.printBlock(eb.Statements)
		p.popIndent()
	}
	p.writeln("end")
}

func (p *Printer) printWhileStatement(s *WhileStatement) {
	p.writeIndent()
	p.write("while ")
	p.printExpression(s.Condition)
	p.write(" do")
	p.newline()
	p.pushIndent()
	p.printBlock(s.Body.Statements)
	p.popIndent()
	p.writeln("end")
}

func (p *Printer) printRepeatStatement(s *RepeatStatement) {
	p.writeln("repeat")
	p.pushIndent()
	p.printBlock(s.Body.Statements)
	p.popIndent()
	p.writeIndent()
	p.write("until ")
	p.printExpression(s.Condition)
	p.newline()
}

func (p *Printer) printForStatement(s *ForStatement) {
	p.writeIndent()
	p.write("for ")
	p.write(s.ControlVariable.Text)
	p.write(" = ")
	p.printExpression(s.ControlVariableInitializer)
	p.write(", ")
	p.printExpression(s.LimitExpression)
	if s.StepExpression != nil {
		p.write(", ")
		p.printExpression(s.StepExpression)
	}
	p.write(" do")
	p.newline()
	p.pushIndent()
	p.printBlock(s.Body.Statements)
	p.popIndent()
	p.writeln("end")
}

func (p *Printer) printForInStatement(s *ForInStatement) {
	p.writeIndent()
	p.write("for ")
	for i, name := range s.Names {
		if i > 0 {
			p.write(", ")
		}
		p.emitMapping(name)
		p.write(name.Text)
	}
	p.write(" in ")
	p.printExpressionList(s.Expressions)
	p.write(" do")
	p.newline()
	p.pushIndent()
	p.printBlock(s.Body.Statements)
	p.popIndent()
	p.writeln("end")
}

func (p *Printer) printReturnStatement(s *ReturnStatement) {
	if len(s.Expressions) == 0 {
		p.writeln("return")
		return
	}
	p.writeIndent()
	p.write("return ")
	p.printExprCommaSep(s.Expressions)
	p.newline()
}

func (p *Printer) printExpressionStatement(s *ExpressionStatement) {
	p.writeIndent()
	p.printExpression(s.Expression)
	p.newline()
}

// ---------------------------------------------------------------------------
// Expression printing
// ---------------------------------------------------------------------------

func (p *Printer) printExpression(expr Expression) {
	p.emitMapping(expr)
	switch e := expr.(type) {
	case *StringLiteral:
		p.write(EscapeString(e.Value))
	case *NumericLiteral:
		p.write(e.Value)
	case *NilLiteral:
		p.write("nil")
	case *DotsLiteral:
		p.write("...")
	case *BooleanLiteral:
		if e.Value {
			p.write("true")
		} else {
			p.write("false")
		}
	case *Identifier:
		p.write(e.Text)
	case *BinaryExpression:
		p.printBinaryExpression(e)
	case *UnaryExpression:
		p.printUnaryExpression(e)
	case *CallExpression:
		p.printCallExpression(e)
	case *MethodCallExpression:
		p.printMethodCallExpression(e)
	case *TableFieldExpression:
		p.printTableFieldExpression(e)
	case *TableExpression:
		p.printTableExpression(e)
	case *TableIndexExpression:
		p.printTableIndexExpression(e)
	case *FunctionExpression:
		p.printFunctionExpression(e)
	case *ParenthesizedExpression:
		p.write("(")
		p.printExpression(e.Inner)
		p.write(")")
	case *CommentExpression:
		p.write("nil --[[ ")
		p.write(e.Text)
		p.write(" ]]")
	case *RawExpression:
		p.write(e.Code)
	}
}

func (p *Printer) printBinaryExpression(e *BinaryExpression) {
	prec := operatorPrecedence[e.Operator]
	ra := isRightAssociative(e.Operator)

	leftMinPrec := prec
	rightMinPrec := prec + 1
	if ra {
		leftMinPrec = prec + 1
		rightMinPrec = prec
	}

	p.printExprInParensIfNeeded(e.Left, leftMinPrec)
	p.write(" " + OperatorString(e.Operator) + " ")
	p.printExprInParensIfNeeded(e.Right, rightMinPrec)
}

func (p *Printer) printUnaryExpression(e *UnaryExpression) {
	opStr := OperatorString(e.Operator)
	if e.Operator == OpNot {
		p.write(opStr + " ")
	} else {
		p.write(opStr)
	}
	prec := operatorPrecedence[e.Operator]
	p.printExprInParensIfNeeded(e.Operand, prec)
}

func (p *Printer) printCallExpression(e *CallExpression) {
	needsParens := p.prefixNeedsParens(e.Expression)
	if needsParens {
		p.write("(")
	}
	p.printExpression(e.Expression)
	if needsParens {
		p.write(")")
	}
	p.write("(")
	p.printExpressionList(e.Params)
	p.write(")")
}

func (p *Printer) printMethodCallExpression(e *MethodCallExpression) {
	if !isValidLuaDotKey(e.Name, p.allowUnicode) {
		// The transpiler must handle invalid identifiers before creating MethodCallExpression.
		// It should emit CallExpression with explicit self arg instead.
		panic("MethodCallExpression with invalid Lua identifier " + e.Name + ": transpiler should have used CallExpression with explicit self")
	}
	needsParens := p.prefixNeedsParens(e.Prefix)
	if needsParens {
		p.write("(")
	}
	p.printExpression(e.Prefix)
	if needsParens {
		p.write(")")
	}
	p.write(":")
	p.write(e.Name)
	p.write("(")
	p.printExpressionList(e.Params)
	p.write(")")
}

func (p *Printer) printTableFieldExpression(e *TableFieldExpression) {
	if e.Key != nil {
		if e.Computed {
			// Computed field: always use [key] = value bracket notation
			p.write("[")
			p.printExpression(e.Key)
			p.write("]")
		} else if id, ok := e.Key.(*Identifier); ok && isValidLuaDotKey(id.Text, p.allowUnicode) {
			p.write(id.Text)
		} else if sl, ok := e.Key.(*StringLiteral); ok && isValidLuaDotKey(sl.Value, p.allowUnicode) {
			p.write(sl.Value)
		} else {
			p.write("[")
			p.printExpression(e.Key)
			p.write("]")
		}
		p.write(" = ")
	}
	p.printExpression(e.Value)
}

func (p *Printer) printTableExpression(e *TableExpression) {
	if len(e.Fields) == 0 {
		p.write("{}")
		return
	}

	// Cast to []Expression for the simple check
	exprs := make([]Expression, len(e.Fields))
	for i, f := range e.Fields {
		exprs[i] = f
	}

	if isSimpleExpressionList(exprs) {
		p.write("{")
		for i, f := range e.Fields {
			if i > 0 {
				p.write(", ")
			}
			p.printTableFieldExpression(f)
		}
		p.write("}")
	} else {
		p.write("{")
		p.newline()
		p.pushIndent()
		for i, f := range e.Fields {
			p.writeIndent()
			p.printTableFieldExpression(f)
			if i < len(e.Fields)-1 {
				p.write(",")
			}
			p.newline()
		}
		p.popIndent()
		p.writeIndent()
		p.write("}")
	}
}

func (p *Printer) printTableIndexExpression(e *TableIndexExpression) {
	needsParens := p.prefixNeedsParens(e.Table)
	if needsParens {
		p.write("(")
	}
	p.printExpression(e.Table)
	if needsParens {
		p.write(")")
	}

	// Use dot syntax for string literal keys that are valid identifiers
	if sl, ok := e.Index.(*StringLiteral); ok && isValidLuaDotKey(sl.Value, p.allowUnicode) {
		p.write(".")
		p.write(sl.Value)
	} else {
		p.write("[")
		p.printExpression(e.Index)
		p.write("]")
	}
}

func (p *Printer) printFunctionExpression(e *FunctionExpression) {
	p.write("function(")
	for i, param := range e.Params {
		if i > 0 {
			p.write(", ")
		}
		p.emitMapping(param)
		p.write(param.Text)
	}
	if e.Dots {
		if len(e.Params) > 0 {
			p.write(", ")
		}
		p.write("...")
	}
	p.write(")")

	if e.Body == nil {
		p.write(" end")
		return
	}

	if len(e.Body.Statements) == 0 {
		p.newline()
		p.writeIndent()
		p.write("end")
		return
	}

	// Inline functions: single return statement on same line
	if e.Flags&FlagInline != 0 && len(e.Body.Statements) == 1 {
		if ret, ok := e.Body.Statements[0].(*ReturnStatement); ok {
			p.write(" return ")
			p.printExprCommaSep(ret.Expressions)
			p.write(" end")
			return
		}
	}

	p.newline()
	p.pushIndent()
	p.printBlock(e.Body.Statements)
	p.popIndent()
	p.writeIndent()
	p.write("end")
}

func (p *Printer) printFunctionDefinition(prefix, name string, fe *FunctionExpression) {
	p.writeIndent()
	p.write(prefix + " " + name + "(")
	for i, param := range fe.Params {
		if i > 0 {
			p.write(", ")
		}
		p.emitMapping(param)
		p.write(param.Text)
	}
	if fe.Dots {
		if len(fe.Params) > 0 {
			p.write(", ")
		}
		p.write("...")
	}
	p.write(")")
	p.newline()
	if fe.Body != nil {
		p.pushIndent()
		p.printBlock(fe.Body.Statements)
		p.popIndent()
	}
	p.writeln("end")
}

// ---------------------------------------------------------------------------
// Helper methods
// ---------------------------------------------------------------------------

// printExpressionList prints expressions, deciding single-line vs multiline.
// Used for function args, table constructors, return values, etc.
func (p *Printer) printExpressionList(exprs []Expression) {
	if isSimpleExpressionList(exprs) {
		p.printExprCommaSep(exprs)
	} else {
		p.newline()
		p.pushIndent()
		for i, expr := range exprs {
			p.writeIndent()
			p.printExpression(expr)
			if i < len(exprs)-1 {
				p.write(",")
			}
			p.newline()
		}
		p.popIndent()
		p.writeIndent()
	}
}

func (p *Printer) printExprCommaSep(exprs []Expression) {
	for i, expr := range exprs {
		if i > 0 {
			p.write(", ")
		}
		p.printExpression(expr)
	}
}

// printExprInParensIfNeeded wraps the expression in parens if its precedence
// is lower than the required minimum.
func (p *Printer) printExprInParensIfNeeded(expr Expression, minPrec int) {
	if p.needsParens(expr, minPrec) {
		p.write("(")
		p.printExpression(expr)
		p.write(")")
	} else {
		p.printExpression(expr)
	}
}

func (p *Printer) needsParens(expr Expression, minPrec int) bool {
	switch e := expr.(type) {
	case *BinaryExpression:
		prec, ok := operatorPrecedence[e.Operator]
		return ok && prec < minPrec
	case *UnaryExpression:
		prec, ok := operatorPrecedence[e.Operator]
		return ok && prec < minPrec
	case *FunctionExpression, *TableExpression:
		return true
	}
	return false
}

func (p *Printer) prefixNeedsParens(expr Expression) bool {
	switch expr.(type) {
	case *StringLiteral, *FunctionExpression, *TableExpression:
		return true
	case *BinaryExpression, *UnaryExpression:
		// Binary/unary expressions are not valid Lua prefixes.
		// Without parens, `x + 1.foo` parses as `x + (1.foo)`.
		return true
	case *CallExpression, *MethodCallExpression, *TableIndexExpression:
		// Call results and index results are valid prefixes in Lua: f().x, f()(), t[k].x
		return false
	}
	return !IsSimpleExpression(expr)
}

// IsSimpleExpression returns true if the expression contains no calls or function expressions.
func IsSimpleExpression(expr Expression) bool {
	switch e := expr.(type) {
	case *CallExpression, *MethodCallExpression, *FunctionExpression:
		return false
	case *TableExpression:
		for _, f := range e.Fields {
			if !IsSimpleExpression(f) {
				return false
			}
		}
		return true
	case *TableFieldExpression:
		if e.Key != nil && !IsSimpleExpression(e.Key) {
			return false
		}
		return IsSimpleExpression(e.Value)
	case *TableIndexExpression:
		return IsSimpleExpression(e.Table) && IsSimpleExpression(e.Index)
	case *UnaryExpression:
		return IsSimpleExpression(e.Operand)
	case *BinaryExpression:
		return IsSimpleExpression(e.Left) && IsSimpleExpression(e.Right)
	case *RawExpression:
		// Conservative: treat raw code as not simple since we can't inspect it
		return false
	}
	return true
}

func isSimpleExpressionList(exprs []Expression) bool {
	if len(exprs) <= 1 {
		return true
	}
	if len(exprs) > 4 {
		return false
	}
	for _, e := range exprs {
		if !IsSimpleExpression(e) {
			return false
		}
	}
	return true
}

// needsSemicolon determines if a semicolon is needed between two statements
// to prevent ambiguous Lua parsing (e.g. `foo\n(bar)` could be `foo(bar)`).
func (p *Printer) needsSemicolon(prev, curr Statement) bool {
	if !p.mayRequireSemicolon(prev) {
		return false
	}
	return p.startsWithParen(curr)
}

func (p *Printer) mayRequireSemicolon(stmt Statement) bool {
	switch stmt.(type) {
	case *VariableDeclarationStatement, *AssignmentStatement, *ExpressionStatement, *RawStatement:
		return true
	}
	return false
}

func (p *Printer) startsWithParen(stmt Statement) bool {
	switch s := stmt.(type) {
	case *ExpressionStatement:
		return p.exprStartsWithParen(s.Expression)
	case *AssignmentStatement:
		if len(s.Left) > 0 {
			return p.exprStartsWithParen(s.Left[0])
		}
		return false
	case *RawStatement:
		return len(s.Code) > 0 && s.Code[0] == '('
	}
	return false
}

func (p *Printer) exprStartsWithParen(expr Expression) bool {
	switch e := expr.(type) {
	case *ParenthesizedExpression:
		return true
	case *CallExpression:
		if p.prefixNeedsParens(e.Expression) {
			return true
		}
		return p.exprStartsWithParen(e.Expression)
	case *MethodCallExpression:
		if p.prefixNeedsParens(e.Prefix) {
			return true
		}
		return p.exprStartsWithParen(e.Prefix)
	case *BinaryExpression:
		return p.exprStartsWithParen(e.Left)
	case *TableIndexExpression:
		if p.prefixNeedsParens(e.Table) {
			return true
		}
		return p.exprStartsWithParen(e.Table)
	}
	return false
}

type hasComments interface {
	GetComments() *Comments
}

func (p *Printer) printLeadingComments(stmt Statement) {
	if hc, ok := stmt.(hasComments); ok {
		for _, c := range hc.GetComments().LeadingComments {
			p.writeln(c)
		}
	}
}

// isFunctionDefinition checks if a variable declaration is a single function assignment.
// Used by printVariableDeclaration to emit `local function foo()` syntax (allows recursion).
func (p *Printer) isFunctionDefinition(left []*Identifier, right []Expression) bool {
	if len(left) != 1 || len(right) != 1 {
		return false
	}
	_, ok := right[0].(*FunctionExpression)
	return ok
}

func isValidFunctionDeclName(name string) bool {
	// Valid for `function name()`: allows dots for nested (e.g. "foo.bar")
	return luaFuncDeclNameRegex.MatchString(name)
}

// EscapeString produces a Lua 5.1/LuaJIT compatible quoted string.
// Lua 5.1 only supports \DDD decimal escapes (not \xNN hex or \uXXXX unicode).
func EscapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		case 0:
			b.WriteString(`\0`)
		default:
			if c < 32 || c == 127 {
				fmt.Fprintf(&b, "\\%d", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
