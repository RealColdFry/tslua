package transpiler

import (
	"fmt"
	"strconv"

	"github.com/microsoft/typescript-go/shim/ast"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// containsContinue checks if a statement (typically a loop body) contains
// any ContinueStatement at any nesting depth. It stops at nested loops
// since those would have their own continue labels.
func containsContinue(node *ast.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == ast.KindContinueStatement {
		return true
	}
	// Don't descend into nested loops — their continues are for themselves
	if node.Kind == ast.KindForStatement || node.Kind == ast.KindWhileStatement ||
		node.Kind == ast.KindDoStatement || node.Kind == ast.KindForOfStatement ||
		node.Kind == ast.KindForInStatement {
		return false
	}
	found := false
	node.ForEachChild(func(child *ast.Node) bool {
		if containsContinue(child) {
			found = true
			return true // stop iteration
		}
		return false
	})
	return found
}

// containsLabeledBreak checks if any descendant has `break LABEL` targeting the given TS label.
// Descends into nested loops (unlike containsContinue) because labeled break can cross loop boundaries.
func containsLabeledBreak(node *ast.Node, label string) bool {
	if node == nil {
		return false
	}
	if node.Kind == ast.KindBreakStatement {
		bs := node.AsBreakStatement()
		if bs.Label != nil && bs.Label.Text() == label {
			return true
		}
	}
	found := false
	node.ForEachChild(func(child *ast.Node) bool {
		if containsLabeledBreak(child, label) {
			found = true
			return true
		}
		return false
	})
	return found
}

// containsLabeledContinue checks if any descendant has `continue LABEL` targeting the given TS label.
// Descends into nested loops because labeled continue targets an outer loop.
func containsLabeledContinue(node *ast.Node, label string) bool {
	if node == nil {
		return false
	}
	if node.Kind == ast.KindContinueStatement {
		cs := node.AsContinueStatement()
		if cs.Label != nil && cs.Label.Text() == label {
			return true
		}
	}
	found := false
	node.ForEachChild(func(child *ast.Node) bool {
		if containsLabeledContinue(child, label) {
			found = true
			return true
		}
		return false
	})
	return found
}

// isIterationStatement checks if a node is a loop statement.
func isIterationStatement(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindForStatement, ast.KindWhileStatement, ast.KindDoStatement,
		ast.KindForOfStatement, ast.KindForInStatement:
		return true
	}
	return false
}

// transformLabeledStatement handles `label: statement`.
// For goto-capable targets, emits goto labels for break/continue.
// For non-goto targets, emits a diagnostic if labeled break/continue is used.
func (t *Transpiler) transformLabeledStatement(node *ast.Node) []lua.Statement {
	ls := node.AsLabeledStatement()
	tsLabel := ls.Label.Text()
	innerStmt := ls.Statement

	// Unwrap nested labels: `a: b: for (...)` → get the innermost statement
	for innerStmt.Kind == ast.KindLabeledStatement {
		innerStmt = innerStmt.AsLabeledStatement().Statement
	}

	hasBreak := containsLabeledBreak(innerStmt, tsLabel)
	hasContinue := isIterationStatement(innerStmt) && containsLabeledContinue(innerStmt, tsLabel)

	// If no labeled break/continue targets this label, just emit the inner statement
	if !hasBreak && !hasContinue {
		return t.transformStatementWithComments(ls.Statement)
	}

	if !t.luaTarget.SupportsGoto() {
		t.addError(node, dw.UnsupportedForTarget, "Labeled break/continue requires goto support (Lua 5.2+ or LuaJIT).")
		// Still emit the inner statement without label support
		return t.transformStatementWithComments(ls.Statement)
	}

	// Register label names
	breakLabel := fmt.Sprintf("__break_%s", tsLabel)
	continueLabel := fmt.Sprintf("__continue_%s", tsLabel)

	if t.breakLabels == nil {
		t.breakLabels = make(map[string]string)
	}
	if t.continueLabelMap == nil {
		t.continueLabelMap = make(map[string]string)
	}
	if t.labelScopeDepths == nil {
		t.labelScopeDepths = make(map[string]int)
	}

	if hasBreak {
		t.breakLabels[tsLabel] = breakLabel
	}
	if hasContinue {
		t.continueLabelMap[tsLabel] = continueLabel
		t.activeLabeledContinue = continueLabel
	}
	// Record the scope-stack length at registration. The inner iteration/block
	// pushes its own scope at this index, so a break/continue inside that scope
	// targeting this label whose scope-stack index is below this length has
	// crossed any Try/Catch scope in between.
	t.labelScopeDepths[tsLabel] = len(t.scopeStack)

	// Transform the inner statement (it will use the registered labels)
	stmts := t.transformStatementWithComments(ls.Statement)

	// Clean up labels
	delete(t.breakLabels, tsLabel)
	delete(t.continueLabelMap, tsLabel)
	delete(t.labelScopeDepths, tsLabel)
	t.activeLabeledContinue = ""

	if hasBreak {
		stmts = append(stmts, lua.GotoLabel(breakLabel))
	}

	return stmts
}

// transformLoopBody returns the loop body statements, wrapping with continue support if needed.
// LuaJIT/5.2+: do { body } end ::__continueN::  (goto-based)
// Lua 5.1:     do local __continueN; repeat { body; __continueN = true } until true; if not __continueN then break end end
func (t *Transpiler) transformLoopBody(body *ast.Node) []lua.Statement {
	// Always push a Loop scope to keep scope IDs aligned with TSTL.
	// TSTL uses scope.id for continue label numbering.
	// TSTL's transformBlockOrStatement does not push a scope — the Loop scope
	// is the only scope for the loop body. Use transformBlockStatementsNoScope
	// to avoid the extra Block scope that transformBlockOrStatement would push.
	scope := t.pushScope(ScopeLoop, body)

	hasContinue := containsContinue(body)
	var label string
	if hasContinue {
		label = fmt.Sprintf("__continue%d", scope.ID)
		t.continueLabels = append(t.continueLabels, label)
	}

	// Consume any labeled continue target set by transformLabeledStatement
	labeledContinue := t.activeLabeledContinue
	t.activeLabeledContinue = ""

	// Push nil incrementor for non-for loops so continue doesn't duplicate anything
	if t.luaTarget.HasNativeContinue() && hasContinue {
		t.forLoopIncrementors = append(t.forLoopIncrementors, nil)
	}
	bodyStmts := t.transformBlockStatementsNoScope(body)
	if t.luaTarget.HasNativeContinue() && hasContinue {
		t.forLoopIncrementors = t.forLoopIncrementors[:len(t.forLoopIncrementors)-1]
	}
	t.popScope()

	if hasContinue {
		t.continueLabels = t.continueLabels[:len(t.continueLabels)-1]
	}

	if !hasContinue && labeledContinue == "" {
		return bodyStmts
	}

	// Luau: native continue keyword needs no wrapping or labels
	if t.luaTarget.HasNativeContinue() {
		return bodyStmts
	}

	if t.luaTarget.SupportsGoto() {
		stmts := []lua.Statement{lua.Do(bodyStmts...)}
		if hasContinue {
			stmts = append(stmts, lua.GotoLabel(label))
		}
		if labeledContinue != "" {
			stmts = append(stmts, lua.GotoLabel(labeledContinue))
		}
		return stmts
	}

	return t.wrapRepeatBreakContinue(bodyStmts, label)
}

// wrapRepeatBreakContinue wraps already-transformed body statements with the
// Lua 5.0/5.1 continue workaround using repeat/until true.
func (t *Transpiler) wrapRepeatBreakContinue(bodyStmts []lua.Statement, label string) []lua.Statement {
	bodyStmts = append(bodyStmts, lua.Assign(
		[]lua.Expression{lua.Ident(label)},
		[]lua.Expression{lua.Bool(true)},
	))

	return []lua.Statement{
		lua.Do(
			lua.LocalDecl([]*lua.Identifier{lua.Ident(label)}, nil),
			lua.Repeat(lua.Bool(true), &lua.Block{Statements: bodyStmts}),
			lua.If(
				lua.Unary(lua.OpNot, lua.Ident(label)),
				&lua.Block{Statements: []lua.Statement{lua.Break()}},
				nil,
			),
		),
	}
}

func (t *Transpiler) transformWhileStatement(node *ast.Node) []lua.Statement {
	ws := node.AsWhileStatement()
	t.checkOnlyTruthyCondition(ws.Expression)
	cond, precCond := t.transformExprInScope(ws.Expression)
	bodyStmts := t.transformLoopBody(ws.Statement)

	// If condition has preceding statements (e.g. ++i in `while(++i < 10)`),
	// they must re-execute every iteration:
	//   while true do
	//     <precCond>
	//     if not <cond> then break end
	//     <body>
	//   end
	if len(precCond) > 0 {
		var inner []lua.Statement
		inner = append(inner, precCond...)
		inner = append(inner, lua.If(
			lua.Unary(lua.OpNot, cond),
			&lua.Block{Statements: []lua.Statement{lua.Break()}},
			nil,
		))
		inner = append(inner, bodyStmts...)
		return []lua.Statement{lua.While(lua.Bool(true), &lua.Block{Statements: inner})}
	}

	return []lua.Statement{lua.While(cond, &lua.Block{Statements: bodyStmts})}
}

// invertCondition negates a Lua expression, simplifying double negation.
func invertCondition(expr lua.Expression) lua.Expression {
	if unary, ok := expr.(*lua.UnaryExpression); ok && unary.Operator == lua.OpNot {
		// not X → X (strip redundant parens too)
		inner := unary.Operand
		if paren, ok := inner.(*lua.ParenthesizedExpression); ok {
			return paren.Inner
		}
		return inner
	}
	// Let the printer decide whether parens are needed based on precedence.
	// No explicit Paren wrapping — the printer's printExprInParensIfNeeded handles it.
	return lua.Unary(lua.OpNot, expr)
}

func (t *Transpiler) transformDoWhileStatement(node *ast.Node) []lua.Statement {
	ds := node.AsDoStatement()
	t.checkOnlyTruthyCondition(ds.Expression)
	bodyStmts := t.transformLoopBody(ds.Statement)
	cond, precCond := t.transformExprInScope(ds.Expression)

	// Wrap body in do...end to isolate block-scoped variables from the until condition
	// (Lua's repeat/until shares scope between body and condition)
	innerBody := []lua.Statement{lua.Do(bodyStmts...)}
	innerBody = append(innerBody, precCond...)

	return []lua.Statement{
		lua.Repeat(
			invertCondition(cond),
			&lua.Block{Statements: innerBody},
		),
	}
}

// tryNumericFor attempts to emit a Lua numeric for loop for simple C-style for statements.
// Only enabled when emitMode == optimized. Returns nil if the pattern doesn't match.
//
// Matches: for (let i = start; i < limit; i++)  where limit is a numeric literal
// and the loop variable is not reassigned in the body.
func (t *Transpiler) tryNumericFor(node *ast.Node) []lua.Statement {
	if t.emitMode != EmitModeOptimized {
		return nil
	}
	fs := node.AsForStatement()

	// 1. Initializer: must be a single let/const variable declaration with an initializer.
	// Reject `var`: JS `var` is function-scoped (shared across iterations and visible after
	// the loop), while Lua numeric for rebinds its counter per iteration and scopes it to
	// the body, so the two semantics disagree when the counter is captured or read later.
	if fs.Initializer == nil || fs.Initializer.Kind != ast.KindVariableDeclarationList {
		return nil
	}
	if fs.Initializer.Flags&(ast.NodeFlagsLet|ast.NodeFlagsConst) == 0 {
		return nil
	}
	declList := fs.Initializer.AsVariableDeclarationList()
	if len(declList.Declarations.Nodes) != 1 {
		return nil
	}
	decl := declList.Declarations.Nodes[0].AsVariableDeclaration()
	if decl.Name().Kind != ast.KindIdentifier || decl.Initializer == nil {
		return nil
	}
	loopVarName := decl.Name().AsIdentifier().Text
	loopVarSym := t.checker.GetSymbolAtLocation(decl.Name())

	// 2. Condition: must be loopVar < literal, loopVar <= literal, etc.
	if fs.Condition == nil || fs.Condition.Kind != ast.KindBinaryExpression {
		return nil
	}
	cond := fs.Condition.AsBinaryExpression()
	op := cond.OperatorToken.Kind

	// Determine which side is the loop var and which is the limit
	var limitNode *ast.Node
	ascending := true // true for <, <=; false for >, >=
	strict := false   // true for < or > (need limit adjustment)

	switch op {
	case ast.KindLessThanToken: // i < limit
		if !t.isLoopVar(cond.Left, loopVarSym) {
			return nil
		}
		limitNode = cond.Right
		ascending = true
		strict = true
	case ast.KindLessThanEqualsToken: // i <= limit
		if !t.isLoopVar(cond.Left, loopVarSym) {
			return nil
		}
		limitNode = cond.Right
		ascending = true
		strict = false
	case ast.KindGreaterThanToken: // i > limit  OR  limit > i (mirrored)
		if t.isLoopVar(cond.Left, loopVarSym) {
			// i > limit → descending
			limitNode = cond.Right
			ascending = false
			strict = true
		} else if t.isLoopVar(cond.Right, loopVarSym) {
			// limit > i → ascending (same as i < limit)
			limitNode = cond.Left
			ascending = true
			strict = true
		} else {
			return nil
		}
	case ast.KindGreaterThanEqualsToken: // i >= limit  OR  limit >= i
		if t.isLoopVar(cond.Left, loopVarSym) {
			limitNode = cond.Right
			ascending = false
			strict = false
		} else if t.isLoopVar(cond.Right, loopVarSym) {
			limitNode = cond.Left
			ascending = true
			strict = false
		} else {
			return nil
		}
	default:
		return nil
	}

	// 3. Limit must be a numeric literal
	if limitNode.Kind != ast.KindNumericLiteral {
		return nil
	}

	// 4. Incrementor: i++, ++i, i--, --i, i += literal, i -= literal
	if fs.Incrementor == nil {
		return nil
	}
	stepVal := 0.0
	switch fs.Incrementor.Kind {
	case ast.KindPostfixUnaryExpression:
		pu := fs.Incrementor.AsPostfixUnaryExpression()
		if !t.isLoopVar(pu.Operand, loopVarSym) {
			return nil
		}
		switch pu.Operator {
		case ast.KindPlusPlusToken:
			stepVal = 1
		case ast.KindMinusMinusToken:
			stepVal = -1
		default:
			return nil
		}
	case ast.KindPrefixUnaryExpression:
		pu := fs.Incrementor.AsPrefixUnaryExpression()
		if !t.isLoopVar(pu.Operand, loopVarSym) {
			return nil
		}
		switch pu.Operator {
		case ast.KindPlusPlusToken:
			stepVal = 1
		case ast.KindMinusMinusToken:
			stepVal = -1
		default:
			return nil
		}
	case ast.KindBinaryExpression:
		be := fs.Incrementor.AsBinaryExpression()
		if !t.isLoopVar(be.Left, loopVarSym) {
			return nil
		}
		if be.Right.Kind != ast.KindNumericLiteral {
			return nil
		}
		rv, err := strconv.ParseFloat(be.Right.AsNumericLiteral().Text, 64)
		if err != nil || rv == 0 {
			return nil
		}
		switch be.OperatorToken.Kind {
		case ast.KindPlusEqualsToken:
			stepVal = rv
		case ast.KindMinusEqualsToken:
			stepVal = -rv
		default:
			return nil
		}
	default:
		return nil
	}

	// Sanity: step direction must match condition direction
	if ascending && stepVal <= 0 {
		return nil
	}
	if !ascending && stepVal >= 0 {
		return nil
	}

	// 5. Loop variable must not be assigned anywhere in the body, including inside
	// closures. Lua 5.4+ makes the numeric-for control variable `<const>`, so a
	// closure that writes to it fails at runtime. On earlier Lua versions the write
	// would succeed and still match JS `let` semantics (per-iteration rebind), but
	// one helper is cheaper than branching on target here.
	if loopVarSym != nil && containsAssignmentToSymbolDeep(fs.Statement, loopVarSym, t) {
		return nil
	}

	// All checks passed — emit numeric for loop

	// Transform start expression
	startExpr, startPrec := t.transformExprInScope(decl.Initializer)

	// Transform limit expression, adjusting for strict comparisons.
	// Since we know the limit is a numeric literal, fold the arithmetic at compile time.
	limitText := limitNode.AsNumericLiteral().Text
	if strict {
		limitVal, err := strconv.ParseFloat(limitText, 64)
		if err != nil {
			return nil
		}
		if ascending {
			limitVal-- // i < N → limit = N - 1
		} else {
			limitVal++ // i > N → limit = N + 1
		}
		limitText = formatNumericLiteral(limitVal)
	}
	limitExpr := lua.Num(limitText)

	// Step expression: omit when step is 1 (Lua default)
	var stepExpr lua.Expression
	if stepVal != 1 {
		stepExpr = lua.Num(formatNumericLiteral(stepVal))
	}

	// Transform body with continue support
	bodyStmts := t.transformLoopBody(fs.Statement)

	var result []lua.Statement
	result = append(result, startPrec...)
	result = append(result, &lua.ForStatement{
		ControlVariable:            lua.Ident(loopVarName),
		ControlVariableInitializer: startExpr,
		LimitExpression:            limitExpr,
		StepExpression:             stepExpr,
		Body:                       &lua.Block{Statements: bodyStmts},
	})
	return result
}

// formatNumericLiteral formats a float as an integer string if whole, otherwise as a float.
func formatNumericLiteral(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// closureCapturesSymbol returns true if any function (arrow or expression) nested
// inside node references the given symbol. This is used to detect when a let variable
// in a for-loop initializer is captured by a closure, requiring per-iteration binding.
func (t *Transpiler) closureCapturesSymbol(node *ast.Node, sym *ast.Symbol) bool {
	if node == nil || sym == nil {
		return false
	}
	found := false
	var walk func(n *ast.Node, insideClosure bool)
	walk = func(n *ast.Node, insideClosure bool) {
		if found {
			return
		}
		if insideClosure && n.Kind == ast.KindIdentifier {
			if s := t.checker.GetSymbolAtLocation(n); s == sym {
				found = true
				return
			}
		}
		isClosure := n.Kind == ast.KindFunctionExpression || n.Kind == ast.KindArrowFunction
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child, insideClosure || isClosure)
			return found
		})
	}
	walk(node, false)
	return found
}

// capturedLetVar describes a let-declared loop variable that is captured by a closure
// somewhere in the for-statement. reassigned=true means the variable is synchronously
// mutated (in body or incrementor), which requires renaming the outer counter and
// syncing per-iteration values back. capturedInBody / capturedInInc track where the
// closure captures occur, so the emitter can decide whether to create a per-iteration
// scope around the body, the incrementor, or both. ECMA-262 §14.7.4 specifies that
// the body and the update expression run in separate per-iteration environments, so
// closures built in the body and in the incrementor need distinct per-iter bindings.
type capturedLetVar struct {
	name           string      // original TS identifier, also the inner per-iteration Lua name
	sym            *ast.Symbol // TS symbol of the let declaration
	reassigned     bool        // body or incrementor synchronously reassigns the variable
	capturedInBody bool        // a closure in the body captures this variable
	capturedInInc  bool        // a closure in the incrementor captures this variable
	outerName      string      // renamed outer-counter name when reassigned
}

// forCapturedLetVars returns every let-declared variable in a for-statement initializer
// that is captured by a closure in the body or the incrementor, classifying each as
// reassigned or not and recording where the captures occur.
func (t *Transpiler) forCapturedLetVars(fs *ast.ForStatement) []capturedLetVar {
	if fs.Initializer == nil || fs.Initializer.Kind != ast.KindVariableDeclarationList {
		return nil
	}
	if fs.Initializer.Flags&ast.NodeFlagsLet == 0 {
		return nil
	}
	declList := fs.Initializer.AsVariableDeclarationList()
	var out []capturedLetVar
	for _, decl := range declList.Declarations.Nodes {
		d := decl.AsVariableDeclaration()
		if d.Name().Kind != ast.KindIdentifier {
			continue
		}
		sym := t.checker.GetSymbolAtLocation(d.Name())
		if sym == nil {
			continue
		}
		capturedInBody := t.closureCapturesSymbol(fs.Statement, sym)
		capturedInInc := fs.Incrementor != nil && t.closureCapturesSymbol(fs.Incrementor, sym)
		if !capturedInBody && !capturedInInc {
			continue
		}
		v := capturedLetVar{
			name:           d.Name().Text(),
			sym:            sym,
			capturedInBody: capturedInBody,
			capturedInInc:  capturedInInc,
		}
		reassigned := containsAssignmentToSymbolDeep(fs.Statement, sym, t)
		if !reassigned && fs.Incrementor != nil {
			reassigned = containsAssignmentToSymbolDeep(fs.Incrementor, sym, t)
		}
		if reassigned {
			v.reassigned = true
			v.outerName = t.nextTemp(v.name)
		}
		out = append(out, v)
	}
	return out
}

// isLoopVar checks if a TS node is an identifier referring to the given symbol.
func (t *Transpiler) isLoopVar(node *ast.Node, sym *ast.Symbol) bool {
	if node.Kind != ast.KindIdentifier || sym == nil {
		return false
	}
	nodeSym := t.checker.GetSymbolAtLocation(node)
	return nodeSym == sym
}

// containsAssignmentToSymbolDeep checks if a TS subtree contains any assignment to the given
// symbol, descending into nested functions. Used by forCapturedLetVars to classify captured
// let variables as reassigned (a synchronously-called IIFE writing the counter behaves like
// a direct write, and we can't statically tell sync from async) and by tryNumericFor to
// reject patterns where a closure writes to the loop counter (Lua 5.4+ makes the numeric-for
// control variable `<const>`, so such writes fail at runtime). Over-triggering is safe: the
// fallback while-loop emit is always semantically correct, just slightly more verbose.
func containsAssignmentToSymbolDeep(node *ast.Node, sym *ast.Symbol, t *Transpiler) bool {
	if node == nil {
		return false
	}

	switch node.Kind {
	case ast.KindBinaryExpression:
		be := node.AsBinaryExpression()
		op := be.OperatorToken.Kind
		if op == ast.KindEqualsToken || isCompoundAssignment(op) {
			if destructuringLHSAssignsToSymbol(be.Left, sym, t) {
				return true
			}
		}
	case ast.KindPrefixUnaryExpression:
		pu := node.AsPrefixUnaryExpression()
		if pu.Operator == ast.KindPlusPlusToken || pu.Operator == ast.KindMinusMinusToken {
			if pu.Operand.Kind == ast.KindIdentifier {
				if s := t.checker.GetSymbolAtLocation(pu.Operand); s == sym {
					return true
				}
			}
		}
	case ast.KindPostfixUnaryExpression:
		pu := node.AsPostfixUnaryExpression()
		if pu.Operator == ast.KindPlusPlusToken || pu.Operator == ast.KindMinusMinusToken {
			if pu.Operand.Kind == ast.KindIdentifier {
				if s := t.checker.GetSymbolAtLocation(pu.Operand); s == sym {
					return true
				}
			}
		}
	}

	found := false
	node.ForEachChild(func(child *ast.Node) bool {
		if containsAssignmentToSymbolDeep(child, sym, t) {
			found = true
			return true
		}
		return false
	})
	return found
}

// destructuringLHSAssignsToSymbol returns true if lhs, treated as the LHS of an
// assignment expression, writes to sym. Handles plain identifiers as well as
// array/object destructuring patterns (including spread and defaults).
func destructuringLHSAssignsToSymbol(lhs *ast.Node, sym *ast.Symbol, t *Transpiler) bool {
	if lhs == nil {
		return false
	}
	switch lhs.Kind {
	case ast.KindIdentifier:
		return t.checker.GetSymbolAtLocation(lhs) == sym
	case ast.KindParenthesizedExpression:
		return destructuringLHSAssignsToSymbol(lhs.AsParenthesizedExpression().Expression, sym, t)
	case ast.KindArrayLiteralExpression:
		for _, elem := range lhs.AsArrayLiteralExpression().Elements.Nodes {
			if elem == nil || elem.Kind == ast.KindOmittedExpression {
				continue
			}
			if elem.Kind == ast.KindSpreadElement {
				if destructuringLHSAssignsToSymbol(elem.AsSpreadElement().Expression, sym, t) {
					return true
				}
				continue
			}
			if destructuringLHSAssignsToSymbol(elem, sym, t) {
				return true
			}
		}
	case ast.KindObjectLiteralExpression:
		for _, prop := range lhs.AsObjectLiteralExpression().Properties.Nodes {
			switch prop.Kind {
			case ast.KindShorthandPropertyAssignment:
				if t.checker.GetSymbolAtLocation(prop.AsShorthandPropertyAssignment().Name()) == sym {
					return true
				}
			case ast.KindPropertyAssignment:
				if destructuringLHSAssignsToSymbol(prop.AsPropertyAssignment().Initializer, sym, t) {
					return true
				}
			case ast.KindSpreadAssignment:
				if destructuringLHSAssignsToSymbol(prop.AsSpreadAssignment().Expression, sym, t) {
					return true
				}
			}
		}
	case ast.KindBinaryExpression:
		// Default-value pattern inside destructuring: `[a = 5]` parses as BinaryExpression(a, =, 5).
		// Only the LHS of that inner expression is an assignment target.
		be := lhs.AsBinaryExpression()
		if be.OperatorToken.Kind == ast.KindEqualsToken {
			return destructuringLHSAssignsToSymbol(be.Left, sym, t)
		}
	}
	return false
}

// setRenameForCtx configures t.loopVarRenames for a specific emit context.
// Reassigned captured vars listed by perIterCtx use per-iteration bindings (so the
// rename is REMOVED, causing identifier emission to use the original name `i`).
// Reassigned vars NOT in that set use the renamed outer counter (`____i`).
// Non-reassigned vars are never in the rename map.
func (t *Transpiler) setRenameForCtx(capturedLetVars []capturedLetVar, perIterCtx func(capturedLetVar) bool) {
	for _, v := range capturedLetVars {
		if !v.reassigned {
			continue
		}
		if perIterCtx(v) {
			delete(t.loopVarRenames, v.sym)
		} else {
			t.loopVarRenames[v.sym] = v.outerName
		}
	}
}

// transformIncrementor emits the for-loop's update expression. When any captured
// let variable is captured inside the incrementor, the update expression runs in
// its own per-iteration scope (a `do local i = ____i; <inc>; ____i = i end` block)
// so closures built in the update clause (e.g. `fns.push(() => i)`) bind to a
// fresh per-iteration `i`, matching ECMA-262's separate per-iteration environment
// for the update step. Otherwise the incrementor emits in the outer scope using
// the renamed counter.
func (t *Transpiler) transformIncrementor(fs *ast.ForStatement, capturedLetVars []capturedLetVar) []lua.Statement {
	if fs.Incrementor == nil {
		return nil
	}
	hasIncCapture := false
	for _, v := range capturedLetVars {
		if v.capturedInInc {
			hasIncCapture = true
			break
		}
	}

	// Rename is disabled for vars captured in the incrementor so `i++` emits as
	// `i = i + 1` against the per-iteration local. Other reassigned vars stay
	// renamed so their writes go to the outer counter.
	t.setRenameForCtx(capturedLetVars, func(v capturedLetVar) bool { return v.capturedInInc })
	incrStmts := t.transformAsStatement(fs.Incrementor)
	for _, s := range incrStmts {
		if p, ok := s.(lua.Positioned); ok {
			if n, ok := s.(lua.Node); ok && !n.SourcePosition().HasPos {
				_ = p
				t.setNodePos(p, fs.Incrementor)
			}
		}
	}

	if !hasIncCapture {
		return incrStmts
	}

	// Wrap in a per-iteration `do local i = ____i; <inc>; ____i = i end` block.
	// Non-reassigned vars captured here get a shadow `local i = i`; reassigned
	// vars get a fresh `local i = ____i` and a `____i = i` sync-back.
	var inner []lua.Statement
	for _, v := range capturedLetVars {
		if !v.capturedInInc {
			continue
		}
		if v.reassigned {
			inner = append(inner, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(v.name)},
				[]lua.Expression{lua.Ident(v.outerName)},
			))
		} else {
			inner = append(inner, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(v.name)},
				[]lua.Expression{lua.Ident(v.name)},
			))
		}
	}
	inner = append(inner, incrStmts...)
	for _, v := range capturedLetVars {
		if v.capturedInInc && v.reassigned {
			inner = append(inner, lua.Assign(
				[]lua.Expression{lua.Ident(v.outerName)},
				[]lua.Expression{lua.Ident(v.name)},
			))
		}
	}
	return []lua.Statement{lua.Do(inner...)}
}

// prependPerIterationCopies adds `local i = i` declarations at the start of a
// statement list for each captured let variable, creating per-iteration bindings.
func prependPerIterationCopies(vars []string, body []lua.Statement) []lua.Statement {
	copies := make([]lua.Statement, 0, len(vars)+len(body))
	for _, name := range vars {
		copies = append(copies, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(name)},
			[]lua.Expression{lua.Ident(name)},
		))
	}
	return append(copies, body...)
}

// C-style for → do { initializer; while (cond) { body; incrementor } } end
func (t *Transpiler) transformForStatement(node *ast.Node) []lua.Statement {
	// Try numeric for optimization (optimized emit mode only)
	if stmts := t.tryNumericFor(node); stmts != nil {
		return stmts
	}

	fs := node.AsForStatement()

	// Push a Loop scope for the for-statement's initializer/condition, matching TSTL.
	// transformLoopBody will push a second Loop scope for the body.
	t.pushScope(ScopeLoop, node)

	// ES6 per-iteration binding: classify let-declared loop variables captured
	// by closures. For those also reassigned in the body, rename the outer
	// counter (init/condition/incrementor see `____i`) so the body's fresh
	// `local i = ____i` can be synced back before the incrementor. Without
	// rename, a body `i = 8` would write to the per-iteration shadow and the
	// loop counter would never observe the skip.
	capturedLetVars := t.forCapturedLetVars(fs)
	if t.loopVarRenames == nil && len(capturedLetVars) > 0 {
		t.loopVarRenames = make(map[*ast.Symbol]string)
	}
	// Init/condition context: every reassigned captured var uses the renamed
	// outer counter (`____i`), so init/cond emit against the outer binding.
	t.setRenameForCtx(capturedLetVars, func(capturedLetVar) bool { return false })

	var outerStmts []lua.Statement

	// Initializer (typically `let j = 0`) — rename is active, so reassigned+captured
	// vars emit as `local ____j = 0` here.
	if fs.Initializer != nil {
		if fs.Initializer.Kind == ast.KindVariableDeclarationList {
			t.checkVariableDeclarationList(fs.Initializer)
			declList := fs.Initializer.AsVariableDeclarationList()
			for _, decl := range declList.Declarations.Nodes {
				d := decl.AsVariableDeclaration()
				nameNode := d.Name()
				if nameNode.Kind == ast.KindArrayBindingPattern || nameNode.Kind == ast.KindObjectBindingPattern {
					outerStmts = append(outerStmts, t.transformVariableDestructuring(nameNode, d.Initializer, true, false)...)
					continue
				}
				nameExpr, namePrec := t.transformExprInScope(nameNode)
				name := identName(nameExpr)
				outerStmts = append(outerStmts, namePrec...)
				if d.Initializer != nil {
					init, prec := t.transformExprInScope(d.Initializer)
					outerStmts = append(outerStmts, prec...)
					initDecl := lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(name)},
						[]lua.Expression{init},
					)
					t.setNodePos(initDecl, decl)
					outerStmts = append(outerStmts, initDecl)
				} else {
					initDecl := lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(name)},
						nil,
					)
					t.setNodePos(initDecl, decl)
					outerStmts = append(outerStmts, initDecl)
				}
			}
		} else {
			outerStmts = append(outerStmts, t.transformAsStatement(fs.Initializer)...)
		}
	}

	// Condition — rename still active, emits `____j < 10`.
	var cond lua.Expression
	var condPrecStmts []lua.Statement
	if fs.Condition != nil {
		cond, condPrecStmts = t.transformExprInScope(fs.Condition)
	} else {
		cond = lua.Bool(true)
	}

	// Transform the incrementor up front so its statements can be pushed onto
	// the forLoopIncrementors stack before body transformation (native-continue
	// targets reference it when a `continue` is encountered inside the body).
	// transformIncrementor sets the rename map appropriately and, when the
	// update expression captures a loop var, wraps its emit in a per-iteration
	// do-block so closures built there bind to a fresh `i`.
	incrStmts := t.transformIncrementor(fs, capturedLetVars)

	// Body context: disable rename for body-captured reassigned vars so body
	// references emit as `i` (resolving to the per-iteration local). All other
	// reassigned vars stay renamed to `____i`.
	t.setRenameForCtx(capturedLetVars, func(v capturedLetVar) bool { return v.capturedInBody })

	// For-statements handle continue directly (not via transformLoopBody) because
	// the incrementor must execute after body but before the next iteration.
	bodyScope := t.pushScope(ScopeLoop, fs.Statement)
	hasContinue := containsContinue(fs.Statement)

	// Consume any labeled continue target set by transformLabeledStatement
	labeledContinue := t.activeLabeledContinue
	t.activeLabeledContinue = ""

	// Sync-back statements run after the body (and before every continue on
	// targets that skip trailing statements): `____i = i` for each reassigned
	// body-captured var. Vars captured only in the incrementor sync back inside
	// the incrementor's own do-block, not here.
	var syncBack []lua.Statement
	for _, v := range capturedLetVars {
		if v.reassigned && v.capturedInBody {
			syncBack = append(syncBack, lua.Assign(
				[]lua.Expression{lua.Ident(v.outerName)},
				[]lua.Expression{lua.Ident(v.name)},
			))
		}
	}

	// Per-iteration binding declarations at the top of the while body. Only
	// emitted for body-captured reassigned vars; non-reassigned shadows are
	// handled separately via prependPerIterationCopies.
	var perIterDecls []lua.Statement
	for _, v := range capturedLetVars {
		if v.reassigned && v.capturedInBody {
			perIterDecls = append(perIterDecls, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(v.name)},
				[]lua.Expression{lua.Ident(v.outerName)},
			))
		}
	}

	var bodyStmts []lua.Statement
	if hasContinue || labeledContinue != "" {
		label := fmt.Sprintf("__continue%d", bodyScope.ID)
		if hasContinue {
			t.continueLabels = append(t.continueLabels, label)
		}

		if t.luaTarget.HasNativeContinue() {
			t.forLoopIncrementors = append(t.forLoopIncrementors, incrStmts)
			t.forLoopPreContinue = append(t.forLoopPreContinue, syncBack)
		}
		innerBody := t.transformBlockStatementsNoScope(fs.Statement)
		if t.luaTarget.HasNativeContinue() {
			t.forLoopIncrementors = t.forLoopIncrementors[:len(t.forLoopIncrementors)-1]
			t.forLoopPreContinue = t.forLoopPreContinue[:len(t.forLoopPreContinue)-1]
		}
		// Prepend per-iteration copies for body-captured non-reassigned vars inside
		// the continue wrapper (cheap shadow; no sync-back needed).
		var shadowOnly []string
		for _, v := range capturedLetVars {
			if !v.reassigned && v.capturedInBody {
				shadowOnly = append(shadowOnly, v.name)
			}
		}
		if len(shadowOnly) > 0 {
			innerBody = prependPerIterationCopies(shadowOnly, innerBody)
		}

		// Reassigned-var declarations go OUTSIDE the continue wrapper so they
		// remain in scope for the sync-back after the continue label.
		bodyStmts = append(bodyStmts, perIterDecls...)

		if t.luaTarget.HasNativeContinue() {
			// Luau: native continue; sync-back + incrementor duplicated before each
			// continue via forLoopPreContinue/forLoopIncrementors, and also run on
			// fallthrough.
			bodyStmts = append(bodyStmts, innerBody...)
			bodyStmts = append(bodyStmts, syncBack...)
			bodyStmts = append(bodyStmts, incrStmts...)
		} else if t.luaTarget.SupportsGoto() {
			bodyStmts = append(bodyStmts, lua.Do(innerBody...))
			if hasContinue {
				bodyStmts = append(bodyStmts, lua.GotoLabel(label))
			}
			if labeledContinue != "" {
				bodyStmts = append(bodyStmts, lua.GotoLabel(labeledContinue))
			}
			// Sync-back then incrementor run after the continue label so
			// fallthrough and continue both hit them.
			bodyStmts = append(bodyStmts, syncBack...)
			bodyStmts = append(bodyStmts, incrStmts...)
		} else {
			// Lua 5.0/5.1: repeat-break wrapper; sync-back + incrementor after the
			// wrapper run on fallthrough and continue (flag=true) but are skipped
			// on break.
			bodyStmts = append(bodyStmts, t.wrapRepeatBreakContinue(innerBody, label)...)
			bodyStmts = append(bodyStmts, syncBack...)
			bodyStmts = append(bodyStmts, incrStmts...)
		}

		if hasContinue {
			t.continueLabels = t.continueLabels[:len(t.continueLabels)-1]
		}
	} else {
		innerBody := t.transformBlockStatementsNoScope(fs.Statement)
		var shadowOnly []string
		for _, v := range capturedLetVars {
			if !v.reassigned && v.capturedInBody {
				shadowOnly = append(shadowOnly, v.name)
			}
		}
		if len(shadowOnly) > 0 && len(perIterDecls) == 0 {
			// Pure shadow-only case — keep legacy do-wrapper so the shadow doesn't
			// leak past the body (matches prior emit exactly).
			innerBody = prependPerIterationCopies(shadowOnly, innerBody)
			bodyStmts = append(bodyStmts, lua.Do(innerBody...))
			bodyStmts = append(bodyStmts, incrStmts...)
		} else {
			bodyStmts = append(bodyStmts, perIterDecls...)
			if len(shadowOnly) > 0 {
				innerBody = prependPerIterationCopies(shadowOnly, innerBody)
			}
			bodyStmts = append(bodyStmts, innerBody...)
			bodyStmts = append(bodyStmts, syncBack...)
			bodyStmts = append(bodyStmts, incrStmts...)
		}
	}
	t.popScope() // body scope

	// Clean up renames on the way out.
	for _, v := range capturedLetVars {
		if v.reassigned {
			delete(t.loopVarRenames, v.sym)
		}
	}

	// If condition has preceding statements, they must re-execute every iteration
	if len(condPrecStmts) > 0 {
		var inner []lua.Statement
		inner = append(inner, condPrecStmts...)
		inner = append(inner, lua.If(
			lua.Unary(lua.OpNot, cond),
			&lua.Block{Statements: []lua.Statement{lua.Break()}},
			nil,
		))
		inner = append(inner, bodyStmts...)
		outerStmts = append(outerStmts, lua.While(lua.Bool(true), &lua.Block{Statements: inner}))
	} else {
		outerStmts = append(outerStmts, lua.While(cond, &lua.Block{Statements: bodyStmts}))
	}

	t.popScope() // outer for-statement scope

	// Wrap in do/end to scope the loop variable
	return []lua.Statement{lua.Do(outerStmts...)}
}

func (t *Transpiler) transformForOfStatement(node *ast.Node) []lua.Statement {
	fs := node.AsForInOrOfStatement()

	// $range: for (const i of $range(start, limit, step)) → for i = start, limit, step do
	if stmts := t.tryTransformRangeForOf(node, fs); stmts != nil {
		return stmts
	}

	// LuaIterable/LuaPairsIterable: for...of on extension iterables → for...in directly
	if stmts := t.tryTransformIterableForOf(node, fs); stmts != nil {
		return stmts
	}

	// Optimized emit: zero-garbage iteration
	if t.emitMode == EmitModeOptimized {
		if stmts := t.tryOptimizedMapSetForOf(fs); stmts != nil {
			return stmts
		}
		if stmts := t.tryOptimizedArrayEntriesForOf(fs); stmts != nil {
			return stmts
		}
	}

	iterExpr, precIter := t.transformExprInScope(fs.Expression)

	varName, varPrec, bodyPreamble := t.extractForOfInitializer(fs.Initializer)

	var iterCall lua.Expression
	if t.isArrayType(fs.Expression) {
		iterCall = lua.Call(lua.Ident("ipairs"), iterExpr)
	} else {
		iter := t.requireLualib("__TS__Iterator")
		iterCall = lua.Call(lua.Ident(iter), iterExpr)
	}

	loopBodyStmts := t.transformLoopBody(fs.Statement)

	var bodyStmts []lua.Statement
	bodyStmts = append(bodyStmts, bodyPreamble...)
	bodyStmts = append(bodyStmts, loopBodyStmts...)

	result := make([]lua.Statement, 0, len(varPrec)+len(precIter)+1)
	result = append(result, varPrec...)
	result = append(result, precIter...)
	forInStmt := lua.ForIn(
		[]*lua.Identifier{lua.Ident("____"), lua.Ident(varName)},
		[]lua.Expression{iterCall},
		&lua.Block{Statements: bodyStmts},
	)
	t.setNodePos(forInStmt, node)
	// Position the iteration variable identifier at the initializer declaration
	if len(forInStmt.Names) > 1 {
		t.setNodePos(forInStmt.Names[1], t.forOfInitializerNameNode(fs.Initializer))
	}
	// Position the iterator expression at the iterable expression
	t.setNodePos(iterCall.(*lua.CallExpression), fs.Expression)
	return append(result, forInStmt)
}

// tryOptimizedMapSetForOf attempts to emit zero-garbage for-of loops over Map/Set.
// Returns nil if the pattern doesn't match (falls through to __TS__Iterator).
func (t *Transpiler) tryOptimizedMapSetForOf(fs *ast.ForInOrOfStatement) []lua.Statement {
	expr := fs.Expression
	init := fs.Initializer

	// Detect .keys()/.values()/.entries() calls on Map/Set
	var methodName string
	var receiver *ast.Node
	if expr.Kind == ast.KindCallExpression {
		call := expr.AsCallExpression()
		if call.Expression.Kind == ast.KindPropertyAccessExpression {
			prop := call.Expression.AsPropertyAccessExpression()
			name := prop.Name().Text()
			if name == "keys" || name == "values" || name == "entries" {
				methodName = name
				receiver = prop.Expression
			}
		}
	}

	// Determine which collection we're iterating
	var iterTarget *ast.Node // the Map/Set expression
	var isMap, isSet bool
	if receiver != nil && (t.isMapType(receiver) || t.isSetType(receiver)) {
		iterTarget = receiver
		isMap = t.isMapType(receiver)
		isSet = t.isSetType(receiver)
	} else if methodName == "" {
		// Direct iteration: for (... of map) or for (... of set)
		if t.isMapType(expr) {
			iterTarget = expr
			isMap = true
		} else if t.isSetType(expr) {
			iterTarget = expr
			isSet = true
		}
	}

	if iterTarget == nil {
		return nil
	}

	iterExpr, precIter := t.transformExprInScope(iterTarget)

	// Choose lualib function and loop variables based on collection type + method
	var libName string
	var loopVars []*lua.Identifier
	var bodyPreamble []lua.Statement

	switch {
	case isMap && (methodName == "" || methodName == "entries"):
		// for (const [k, v] of map) or for (const [k, v] of map.entries())
		libName = "__TS__MapForOf"
		vars, preamble := t.mapDestructureVars(init)
		if vars == nil {
			return nil // can't optimize this destructuring pattern
		}
		loopVars = vars
		bodyPreamble = preamble

	case isMap && methodName == "keys":
		// for (const k of map.keys())
		libName = "__TS__MapKeysForOf"
		varName, varPrec, preamble := t.extractForOfInitializer(init)
		loopVars = []*lua.Identifier{lua.Ident(varName)}
		precIter = append(precIter, varPrec...)
		bodyPreamble = preamble

	case isMap && methodName == "values":
		// for (const v of map.values())
		libName = "__TS__MapValuesForOf"
		varName, varPrec, preamble := t.extractForOfInitializer(init)
		// __TS__MapValuesForOf returns (key, value) — key is control var, user gets value
		loopVars = []*lua.Identifier{lua.Ident("____"), lua.Ident(varName)}
		precIter = append(precIter, varPrec...)
		bodyPreamble = preamble

	case isSet && (methodName == "" || methodName == "values" || methodName == "keys"):
		// for (const v of set)
		libName = "__TS__SetForOf"
		varName, varPrec, preamble := t.extractForOfInitializer(init)
		loopVars = []*lua.Identifier{lua.Ident(varName)}
		precIter = append(precIter, varPrec...)
		bodyPreamble = preamble

	case isSet && methodName == "entries":
		// for (const [v, v] of set.entries()) — uncommon, skip optimization
		return nil

	default:
		return nil
	}

	fn := t.requireLualib(libName)
	iterCall := lua.Call(lua.Ident(fn), iterExpr)

	loopBodyStmts := t.transformLoopBody(fs.Statement)

	var bodyStmts []lua.Statement
	bodyStmts = append(bodyStmts, bodyPreamble...)
	bodyStmts = append(bodyStmts, loopBodyStmts...)

	result := make([]lua.Statement, 0, len(precIter)+1)
	result = append(result, precIter...)
	result = append(result, lua.ForIn(loopVars, []lua.Expression{iterCall}, &lua.Block{Statements: bodyStmts}))
	return result
}

// tryOptimizedArrayEntriesForOf optimizes for (const [i, v] of arr.entries())
// to use ipairs directly with 0-based index adjustment.
func (t *Transpiler) tryOptimizedArrayEntriesForOf(fs *ast.ForInOrOfStatement) []lua.Statement {
	expr := fs.Expression

	// Must be a .entries() call on an array type
	if expr.Kind != ast.KindCallExpression {
		return nil
	}
	call := expr.AsCallExpression()
	if call.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil
	}
	prop := call.Expression.AsPropertyAccessExpression()
	if prop.Name().Text() != "entries" {
		return nil
	}
	if !t.isArrayType(prop.Expression) {
		return nil
	}

	arrExpr, precIter := t.transformExprInScope(prop.Expression)
	iterCall := lua.Call(lua.Ident("ipairs"), arrExpr)

	// Extract destructuring variables [i, v]
	vars, _ := t.mapDestructureVars(fs.Initializer)
	if vars == nil {
		return nil
	}

	// ipairs gives 1-based index; JS entries() gives 0-based.
	// Use a temp for the index and adjust in the body preamble.
	var loopVars []*lua.Identifier
	var bodyPreamble []lua.Statement

	idxVar := vars[0]
	idxIsUsed := idxVar.Text != "____"

	if idxIsUsed {
		// for ____i, v in ipairs(arr) do local i = ____i - 1
		tempIdx := lua.Ident("____i")
		loopVars = append(loopVars, tempIdx)
		bodyPreamble = append(bodyPreamble, lua.LocalDecl(
			[]*lua.Identifier{idxVar},
			[]lua.Expression{lua.Binary(tempIdx, lua.OpSub, lua.Num("1"))},
		))
	} else {
		loopVars = append(loopVars, lua.Ident("____"))
	}

	if len(vars) > 1 {
		loopVars = append(loopVars, vars[1])
	}

	loopBodyStmts := t.transformLoopBody(fs.Statement)

	var bodyStmts []lua.Statement
	bodyStmts = append(bodyStmts, bodyPreamble...)
	bodyStmts = append(bodyStmts, loopBodyStmts...)

	result := make([]lua.Statement, 0, len(precIter)+1)
	result = append(result, precIter...)
	result = append(result, lua.ForIn(loopVars, []lua.Expression{iterCall}, &lua.Block{Statements: bodyStmts}))
	return result
}

// mapDestructureVars extracts loop variable names from a for-of initializer
// that destructures [key, value] from a Map. Returns the loop variable identifiers
// and any body preamble, or nil if the pattern can't be optimized.
func (t *Transpiler) mapDestructureVars(init *ast.Node) ([]*lua.Identifier, []lua.Statement) {
	if init.Kind != ast.KindVariableDeclarationList {
		return nil, nil
	}
	declList := init.AsVariableDeclarationList()
	if len(declList.Declarations.Nodes) == 0 {
		return nil, nil
	}
	d := declList.Declarations.Nodes[0].AsVariableDeclaration()
	name := d.Name()
	if name.Kind != ast.KindArrayBindingPattern {
		return nil, nil
	}

	nameNode := (*ast.Node)(name)
	bp := nameNode.AsBindingPattern()
	elements := bp.Elements.Nodes
	if len(elements) < 1 || len(elements) > 2 {
		return nil, nil
	}

	// Extract key variable (first element)
	keyIdent := t.bindingElementIdent(elements[0])
	if keyIdent == nil {
		return nil, nil
	}

	// Extract value variable (second element, if present)
	if len(elements) == 1 {
		// for (const [k] of map) — only key
		return []*lua.Identifier{keyIdent}, nil
	}

	valIdent := t.bindingElementIdent(elements[1])
	if valIdent == nil {
		return nil, nil
	}

	return []*lua.Identifier{keyIdent, valIdent}, nil
}

// bindingElementIdent extracts a simple identifier from a binding pattern element,
// returning ____ for omitted elements and nil for complex patterns.
func (t *Transpiler) bindingElementIdent(elem *ast.Node) *lua.Identifier {
	if elem.Kind == ast.KindOmittedExpression {
		return lua.Ident("____")
	}
	be := elem.AsBindingElement()
	name := be.Name()
	if name == nil {
		// Omitted binding element (e.g., [, value] — first element has no name)
		return lua.Ident("____")
	}
	if name.Kind != ast.KindIdentifier {
		return nil // nested destructuring, bail
	}
	return lua.Ident(name.Text())
}

// isDestructuringPattern checks if a node is an array or object destructuring pattern.
func isDestructuringPattern(node *ast.Node) bool {
	return node.Kind == ast.KindArrayLiteralExpression || node.Kind == ast.KindObjectLiteralExpression ||
		node.Kind == ast.KindArrayBindingPattern || node.Kind == ast.KindObjectBindingPattern
}

// transformExistingVarDestructuring unpacks a temp loop variable into existing
// destructuring targets (e.g. `for ([a,b] of arr)` → `a = temp[1]; b = temp[2]`).
func (t *Transpiler) transformExistingVarDestructuring(init *ast.Node, tempName string) []lua.Statement {
	switch init.Kind {
	case ast.KindArrayLiteralExpression:
		return t.destructureArrayForOf(init.AsArrayLiteralExpression(), tempName)
	case ast.KindObjectLiteralExpression:
		return t.destructureObjectForOf(init.AsObjectLiteralExpression(), tempName)
	}
	return nil
}

func (t *Transpiler) destructureArrayForOf(al *ast.ArrayLiteralExpression, tempName string) []lua.Statement {
	var stmts []lua.Statement
	for i, elem := range al.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			continue
		}
		indexExpr := lua.Index(lua.Ident(tempName), lua.Num(fmt.Sprintf("%d", i+1)))

		switch elem.Kind {
		case ast.KindArrayLiteralExpression:
			nestedTemp := t.nextTemp("temp")
			stmts = append(stmts, lua.Assign([]lua.Expression{lua.Ident(nestedTemp)}, []lua.Expression{indexExpr}))
			stmts = append(stmts, t.destructureArrayForOf(elem.AsArrayLiteralExpression(), nestedTemp)...)
		case ast.KindObjectLiteralExpression:
			nestedTemp := t.nextTemp("temp")
			stmts = append(stmts, lua.Assign([]lua.Expression{lua.Ident(nestedTemp)}, []lua.Expression{indexExpr}))
			stmts = append(stmts, t.destructureObjectForOf(elem.AsObjectLiteralExpression(), nestedTemp)...)
		case ast.KindBinaryExpression:
			// Default value: [foo = 'bar'] → if value == nil then foo = default else foo = value end
			bin := elem.AsBinaryExpression()
			stmts = append(stmts, t.assignWithDefault(bin.Left, indexExpr, bin.Right)...)
		case ast.KindSpreadElement:
			se := elem.AsSpreadElement()
			target, prec := t.transformExprInScope(se.Expression)
			stmts = append(stmts, prec...)
			fn := t.requireLualib("__TS__ArraySlice")
			stmts = append(stmts, lua.Assign(
				[]lua.Expression{target},
				[]lua.Expression{lualibCall(fn, lua.Ident(tempName), lua.Num(fmt.Sprintf("%d", i)))},
			))
			stmts = append(stmts, t.emitExportSync(se.Expression)...)
		default:
			target, targetPrec := t.transformExprInScope(elem)
			stmts = append(stmts, targetPrec...)
			stmts = append(stmts, lua.Assign([]lua.Expression{target}, []lua.Expression{indexExpr}))
			stmts = append(stmts, t.emitExportSync(elem)...)
		}
	}
	return stmts
}

func (t *Transpiler) destructureObjectForOf(ol *ast.ObjectLiteralExpression, tempName string) []lua.Statement {
	var stmts []lua.Statement
	for _, prop := range ol.Properties.Nodes {
		switch prop.Kind {
		case ast.KindShorthandPropertyAssignment:
			// { foo } → foo = temp.foo
			spa := prop.AsShorthandPropertyAssignment()
			name := spa.Name().AsIdentifier().Text
			extractExpr := lua.Index(lua.Ident(tempName), lua.Str(name))
			stmts = append(stmts, lua.Assign([]lua.Expression{lua.Ident(name)}, []lua.Expression{extractExpr}))
			stmts = append(stmts, t.emitExportSync(spa.Name())...)
			// Handle default: { foo = 'bar' }
			if spa.ObjectAssignmentInitializer != nil {
				defaultVal, prec := t.transformExprInScope(spa.ObjectAssignmentInitializer)
				stmts = append(stmts, prec...)
				stmts = append(stmts, lua.If(
					lua.Binary(lua.Ident(name), lua.OpEq, lua.Nil()),
					&lua.Block{Statements: append(
						[]lua.Statement{lua.Assign([]lua.Expression{lua.Ident(name)}, []lua.Expression{defaultVal})},
						t.emitExportSync(spa.Name())...,
					)},
					nil,
				))
			}
		case ast.KindPropertyAssignment:
			// { x: foo } → foo = temp.x
			pa := prop.AsPropertyAssignment()
			propName := t.propertyKey(pa.Name())
			extractExpr := lua.Index(lua.Ident(tempName), propName)
			init := pa.Initializer
			if isDestructuringPattern(init) {
				nestedTemp := t.nextTemp("temp")
				stmts = append(stmts, lua.Assign([]lua.Expression{lua.Ident(nestedTemp)}, []lua.Expression{extractExpr}))
				stmts = append(stmts, t.transformExistingVarDestructuring(init, nestedTemp)...)
			} else if init.Kind == ast.KindBinaryExpression {
				// { x: foo = 'default' }
				bin := init.AsBinaryExpression()
				stmts = append(stmts, t.assignWithDefault(bin.Left, extractExpr, bin.Right)...)
			} else {
				target, prec := t.transformExprInScope(init)
				stmts = append(stmts, prec...)
				stmts = append(stmts, lua.Assign([]lua.Expression{target}, []lua.Expression{extractExpr}))
				stmts = append(stmts, t.emitExportSync(init)...)
			}
		}
	}
	return stmts
}

// assignWithDefault emits: local temp = value; if temp == nil then target = default else target = temp end
// with export sync after each assignment.
func (t *Transpiler) assignWithDefault(targetNode *ast.Node, valueExpr lua.Expression, defaultNode *ast.Node) []lua.Statement {
	target, targetPrec := t.transformExprInScope(targetNode)
	defaultVal, defaultPrec := t.transformExprInScope(defaultNode)
	elemTemp := t.nextTemp("value")

	var stmts []lua.Statement
	stmts = append(stmts, lua.LocalDecl([]*lua.Identifier{lua.Ident(elemTemp)}, []lua.Expression{valueExpr}))

	ifStmts := append(targetPrec, defaultPrec...)
	ifStmts = append(ifStmts, lua.Assign([]lua.Expression{target}, []lua.Expression{defaultVal}))
	ifStmts = append(ifStmts, t.emitExportSync(targetNode)...)

	elseStmts := []lua.Statement{lua.Assign([]lua.Expression{target}, []lua.Expression{lua.Ident(elemTemp)})}
	elseStmts = append(elseStmts, t.emitExportSync(targetNode)...)

	stmts = append(stmts, lua.If(
		lua.Binary(lua.Ident(elemTemp), lua.OpEq, lua.Nil()),
		&lua.Block{Statements: ifStmts},
		&lua.Block{Statements: elseStmts},
	))
	return stmts
}

// emitExportSync returns statements to sync a local variable to ____exports if it's exported.
//
//	src/transformation/visitors/binary-expression/assignments.ts (dependent sync)
func (t *Transpiler) emitExportSync(node *ast.Node) []lua.Statement {
	if node.Kind != ast.KindIdentifier || !t.isModule || t.exportAsGlobal {
		return nil
	}
	name := node.AsIdentifier().Text
	if t.isLocalShadow(node) {
		return nil
	}
	luaName := name
	if t.hasUnsafeIdentifierName(node) {
		luaName = luaSafeName(name)
	}
	var stmts []lua.Statement
	// Sync for `export { x }` or `export { x as a }` (named export specifiers).
	// Note: `export let x` does NOT need sync — those identifiers are already
	// rewritten to ____exports.x by transformExpression.
	for _, exportName := range t.namedExports[name] {
		stmts = append(stmts, lua.Assign(
			[]lua.Expression{lua.Index(lua.Ident("____exports"), lua.Str(exportName))},
			[]lua.Expression{lua.Ident(luaName)},
		))
	}
	return stmts
}

func (t *Transpiler) transformForInStatement(node *ast.Node) []lua.Statement {
	fs := node.AsForInOrOfStatement()

	// Iterating an array with for...in is always broken (yields enumerable string keys
	// including inherited properties). TSTL's forbiddenForIn is an error; match that.
	if t.isArrayType(fs.Expression) {
		t.addError(node, dw.ForbiddenForIn, "Iterating over arrays with 'for ... in' is not allowed.")
	}

	iterExpr, precIter := t.transformExprInScope(fs.Expression)
	varIdent, varPreamble := t.extractForInInitializer(fs.Initializer)

	bodyStmts := t.transformLoopBody(fs.Statement)

	// Preamble statements go inside the loop body (e.g., ____exports.key = ____value)
	if len(varPreamble) > 0 {
		bodyStmts = append(varPreamble, bodyStmts...)
	}

	result := make([]lua.Statement, 0, len(precIter)+1)
	result = append(result, precIter...)
	result = append(result, lua.ForIn(
		[]*lua.Identifier{varIdent},
		[]lua.Expression{lua.Call(lua.Ident("pairs"), iterExpr)},
		&lua.Block{Statements: bodyStmts},
	))
	return result
}

// extractForOfInitializer gets the loop variable name and any body preamble statements
// for a for-of initializer. Matches TSTL's transformForInitializer behavior:
// - `const x of ...` → varName=x, no preamble (direct)
// - `const [a,b] of ...` → varName=____value, preamble=destructuring
// - `x of ...` (existing var) → varName=____value, preamble=x=____value
// - `[a,b] of ...` (existing destructuring) → varName=____value, preamble=unpack
// forOfInitializerNameNode returns the TS AST node for the iteration variable name.
// Used for source map positioning.
func (t *Transpiler) forOfInitializerNameNode(init *ast.Node) *ast.Node {
	if init.Kind == ast.KindVariableDeclarationList {
		declList := init.AsVariableDeclarationList()
		if len(declList.Declarations.Nodes) > 0 {
			return declList.Declarations.Nodes[0].AsVariableDeclaration().Name()
		}
	}
	return init
}

func (t *Transpiler) extractForOfInitializer(init *ast.Node) (string, []lua.Statement, []lua.Statement) {
	if init.Kind == ast.KindVariableDeclarationList {
		t.checkVariableDeclarationList(init)
		declList := init.AsVariableDeclarationList()
		if len(declList.Declarations.Nodes) > 0 {
			d := declList.Declarations.Nodes[0].AsVariableDeclaration()
			name := d.Name()
			if name.Kind == ast.KindArrayBindingPattern || name.Kind == ast.KindObjectBindingPattern {
				// Destructuring declaration: use temp var + unpack
				tempName := "____value"
				preamble := t.transformBindingPattern(name, lua.Ident(tempName), true, false)
				return tempName, nil, preamble
			}
			// Simple variable declaration: use the name directly
			expr, stmts := t.transformExprInScope(name)
			return identName(expr), stmts, nil
		}
	}

	// Existing variable(s) — always use ____value temp + assignment in body
	tempName := "____value"
	if isDestructuringPattern(init) {
		preamble := t.transformExistingVarDestructuring(init, tempName)
		return tempName, nil, preamble
	}
	// Simple existing variable: x = ____value
	target, targetPrec := t.transformExprInScope(init)
	var preamble []lua.Statement
	preamble = append(preamble, targetPrec...)
	preamble = append(preamble, lua.Assign([]lua.Expression{target}, []lua.Expression{lua.Ident(tempName)}))
	// Sync export if the variable is exported
	preamble = append(preamble, t.emitExportSync(init)...)
	return tempName, nil, preamble
}

// extractForInInitializer gets the loop variable and any body preamble for a for-in initializer.
// Returns the loop variable identifier and statements to prepend to the loop body.
// When the initializer is an existing non-identifier expression (e.g., ____exports.key),
// uses a temp variable and assigns it in the body preamble.
func (t *Transpiler) extractForInInitializer(init *ast.Node) (*lua.Identifier, []lua.Statement) {
	if init.Kind == ast.KindVariableDeclarationList {
		t.checkVariableDeclarationList(init)
		declList := init.AsVariableDeclarationList()
		if len(declList.Declarations.Nodes) > 0 {
			d := declList.Declarations.Nodes[0].AsVariableDeclaration()
			expr, stmts := t.transformExprInScope(d.Name())
			if ident, ok := expr.(*lua.Identifier); ok {
				return ident, stmts
			}
			// Binding pattern or complex name — use temp
			temp := lua.Ident("____value")
			preamble := append(stmts, lua.Assign([]lua.Expression{expr}, []lua.Expression{temp}))
			return temp, preamble
		}
	}
	// Assignment to existing variable — must use a temp and assign in the body.
	// Lua's for-in creates a new local for the loop variable, so directly using the
	// outer variable name would shadow it. Use ____value and assign inside the body.
	expr, stmts := t.transformExprInScope(init)
	temp := lua.Ident("____value")
	preamble := append(stmts, lua.Assign([]lua.Expression{expr}, []lua.Expression{temp}))
	preamble = append(preamble, t.emitExportSync(init)...)
	return temp, preamble
}
