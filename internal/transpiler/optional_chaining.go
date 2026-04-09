package transpiler

import (
	"github.com/microsoft/typescript-go/shim/ast"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// flattenChain walks backward from the outermost expression through the chain,
// collecting links until we reach a node with questionDotToken (the chain root).
// Returns the expression before the first ?. and the array of links to apply.
func flattenChain(node *ast.Node) (expression *ast.Node, links []*ast.Node) {
	current := skipNonNullChains(node)
	links = []*ast.Node{current}

	for !hasQuestionDotToken(current) && current.Kind != ast.KindTaggedTemplateExpression {
		nextLink := getChainExpression(current)
		if nextLink == nil || !ast.IsOptionalChain(nextLink) {
			break
		}
		current = skipNonNullChains(nextLink)
		links = append([]*ast.Node{current}, links...)
	}

	expression = getChainExpression(current)
	return
}

func getChainExpression(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		return node.AsPropertyAccessExpression().Expression
	case ast.KindElementAccessExpression:
		return node.AsElementAccessExpression().Expression
	case ast.KindCallExpression:
		return node.AsCallExpression().Expression
	}
	return nil
}

func skipNonNullChains(node *ast.Node) *ast.Node {
	for node.Kind == ast.KindNonNullExpression {
		node = node.AsNonNullExpression().Expression
	}
	return node
}

func hasQuestionDotToken(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		return node.AsPropertyAccessExpression().QuestionDotToken != nil
	case ast.KindElementAccessExpression:
		return node.AsElementAccessExpression().QuestionDotToken != nil
	case ast.KindCallExpression:
		return node.AsCallExpression().QuestionDotToken != nil
	}
	return false
}

func expressionResultIsUsed(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return true
	}
	for parent.Kind == ast.KindNonNullExpression || parent.Kind == ast.KindParenthesizedExpression {
		parent = parent.Parent
		if parent == nil {
			return true
		}
	}
	// void expr discards the result — treat as unused
	if parent.Kind == ast.KindVoidExpression {
		return false
	}
	return parent.Kind != ast.KindExpressionStatement
}

func isAccessExpression(node *ast.Node) bool {
	return node.Kind == ast.KindPropertyAccessExpression || node.Kind == ast.KindElementAccessExpression
}

// transformLeftWithThisCapture transforms a non-optional property/element access,
// splitting it to capture the object as a this-value for contextual calls.
// Returns (accessExpr, precedingStatements, thisValue).
func (t *Transpiler) transformLeftWithThisCapture(tsLeftExpression *ast.Node) (lua.Expression, []lua.Statement, lua.Expression) {
	t.pushPrecedingStatements()
	objNode := getChainExpression(tsLeftExpression)
	objExpr := t.transformExpression(objNode)

	if _, isIdent := objExpr.(*lua.Identifier); !isIdent {
		thisTemp := t.nextTemp("this")
		t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{lua.Ident(thisTemp)}, []lua.Expression{objExpr}))
		objExpr = lua.Ident(thisTemp)
	}

	var leftExpr lua.Expression
	switch tsLeftExpression.Kind {
	case ast.KindPropertyAccessExpression:
		pa := tsLeftExpression.AsPropertyAccessExpression()
		leftExpr = lua.Index(objExpr, lua.Str(pa.Name().AsIdentifier().Text))
	case ast.KindElementAccessExpression:
		ea := tsLeftExpression.AsElementAccessExpression()
		leftExpr = lua.Index(objExpr, t.transformElementIndex(ea))
	}
	prec := t.popPrecedingStatements()
	return leftExpr, prec, objExpr
}

// transformLeftChainWithThisCapture transforms a left expression that is itself
// an optional chain, requesting the inner chain to capture a this-value.
// Returns (chainExpr, precedingStatements, thisValue).
func (t *Transpiler) transformLeftChainWithThisCapture(tsLeftExpression *ast.Node) (lua.Expression, []lua.Statement, lua.Expression) {
	outerThisTemp := t.nextTemp("this")
	outerThisIdent := lua.Ident(outerThisTemp)
	t.pushPrecedingStatements()
	t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{outerThisIdent}, nil))
	leftExpr := t.transformOptionalChainInner(tsLeftExpression, outerThisIdent)
	prec := t.popPrecedingStatements()
	return leftExpr, prec, outerThisIdent
}

// transformOptionalChain implements TSTL's chain-flattening algorithm.
func (t *Transpiler) transformOptionalChain(node *ast.Node) lua.Expression {
	return t.transformOptionalChainInner(node, nil)
}

// transformOptionalChainInner handles the optional chain, optionally capturing
// the this-value (the object before the last access) into thisCapture.
func (t *Transpiler) transformOptionalChainInner(node *ast.Node, thisCapture *lua.Identifier) lua.Expression {
	tsLeftExpression, chain := flattenChain(node)

	// Allocate opt temp FIRST to match TSTL's temp allocation order.
	// TSTL: allocate opt → build right side → allocate this → build left side.
	tempName := t.nextTemp("opt")
	leftIdent := lua.Ident(tempName)

	// Build right side FIRST (matches TSTL order — right side before left side).
	// This ensures recursive chain transforms in the left side get later temp numbers.
	// Save diagnostic count — this call may be redone below with a captured this-value,
	// and we don't want duplicate diagnostics from the speculative first call.
	diagCount := len(t.diagnostics)
	rightExpr, rightPrec := t.buildChainRight(leftIdent, chain, nil, thisCapture)

	// Determine if the first chain link is a call that needs this-context.
	// This only applies when chain[0] is a CallExpression (direct call like obj.method?.(args)),
	// not when the call follows a property access (like obj?.method(args)).
	var capturedThisValue lua.Expression
	needsThisCapture := false
	if len(chain) > 0 && chain[0].Kind == ast.KindCallExpression {
		if t.calleeNeedsSelf(chain[0]) {
			needsThisCapture = true
		}
	}

	// Allocate this temp (matches TSTL's allocation order — between right and left)
	_ = t.nextTemp("this")

	// Transform the left expression, capturing this value if needed
	var leftExpr lua.Expression
	var leftPrec []lua.Statement

	if needsThisCapture && isAccessExpression(tsLeftExpression) && !ast.IsOptionalChain(tsLeftExpression) {
		leftExpr, leftPrec, capturedThisValue = t.transformLeftWithThisCapture(tsLeftExpression)
	} else if needsThisCapture && ast.IsOptionalChain(tsLeftExpression) {
		leftExpr, leftPrec, capturedThisValue = t.transformLeftChainWithThisCapture(tsLeftExpression)
	} else {
		leftExpr, leftPrec = t.transformExprInScope(tsLeftExpression)
	}

	// If we captured a this-value, rebuild the right side with it.
	// Discard diagnostics from the speculative first buildChainRight call.
	if capturedThisValue != nil {
		t.diagnostics = t.diagnostics[:diagCount]
		rightExpr, rightPrec = t.buildChainRight(leftIdent, chain, capturedThisValue, thisCapture)
	}

	t.addPrecedingStatements(leftPrec...)

	canReuseLeft := false
	if ident, ok := leftExpr.(*lua.Identifier); ok &&
		(len(rightPrec) == 0 || !t.shouldMoveToTempWithNode(leftExpr, tsLeftExpression)) {
		// Reuse left identifier when it's safe: either no right-side preceding statements,
		// or the left is a simple identifier that doesn't need caching (const, temp, etc.)
		// Patch all references to the temp in the already-built right expression.
		leftIdent = ident
		canReuseLeft = true
		patchIdentifier(rightExpr, tempName, ident.Text)
		for _, stmt := range rightPrec {
			patchStatementIdentifier(stmt, tempName, ident.Text)
		}
	}

	if !canReuseLeft {
		t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{leftIdent}, []lua.Expression{leftExpr}))
	}

	// Choose output pattern
	if !expressionResultIsUsed(node) {
		var innerStmts []lua.Statement
		innerStmts = append(innerStmts, rightPrec...)
		if stmt := wrapExprInStatement(rightExpr); stmt != nil {
			innerStmts = append(innerStmts, stmt)
		}
		if len(innerStmts) > 0 {
			t.addPrecedingStatements(lua.If(
				lua.Binary(leftIdent, lua.OpNeq, lua.Nil()),
				&lua.Block{Statements: innerStmts},
				nil,
			))
		}
		return lua.Nil()
	}

	if len(rightPrec) == 0 && !t.canBeFalsyWhenNotNull(tsLeftExpression) {
		return lua.Binary(leftIdent, lua.OpAnd, rightExpr)
	}

	// Complex case: if left ~= nil then result = right end
	var resultIdent *lua.Identifier
	if !canReuseLeft {
		// Reuse the temp for the result
		resultIdent = leftIdent
	} else {
		resultName := t.nextTemp("opt_result")
		resultIdent = lua.Ident(resultName)
		t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{resultIdent}, nil))
	}

	ifBody := make([]lua.Statement, 0, len(rightPrec)+1)
	ifBody = append(ifBody, rightPrec...)
	ifBody = append(ifBody, lua.Assign([]lua.Expression{resultIdent}, []lua.Expression{rightExpr}))

	t.addPrecedingStatements(lua.If(
		lua.Binary(leftIdent, lua.OpNeq, lua.Nil()),
		&lua.Block{Statements: ifBody},
		nil,
	))
	return resultIdent
}

// unwrapCallee strips NonNull, type assertions, parens, and satisfies from an expression.
func unwrapCallee(node *ast.Node) *ast.Node {
	for {
		switch node.Kind {
		case ast.KindNonNullExpression:
			node = node.AsNonNullExpression().Expression
		case ast.KindAsExpression:
			node = node.AsAsExpression().Expression
		case ast.KindTypeAssertionExpression:
			node = node.AsTypeAssertion().Expression
		case ast.KindParenthesizedExpression:
			node = node.AsParenthesizedExpression().Expression
		case ast.KindSatisfiesExpression:
			node = node.AsSatisfiesExpression().Expression
		default:
			return node
		}
	}
}

// buildChainRight constructs the right expression by applying chain links to a base.
// PropertyAccess and ElementAccess links are built as Lua AST directly.
// Call links are handled by transformCallInChain for proper builtin/self/arg handling.
func (t *Transpiler) buildChainRight(base *lua.Identifier, chain []*ast.Node, initialThisValue lua.Expression, thisCapture *lua.Identifier) (lua.Expression, []lua.Statement) {
	t.pushPrecedingStatements()

	var result lua.Expression = base
	var lastObjBeforeAccess lua.Expression

	for i, link := range chain {
		isLast := i == len(chain)-1

		switch link.Kind {
		case ast.KindPropertyAccessExpression:
			lastObjBeforeAccess = result
			pa := link.AsPropertyAccessExpression()
			prop := pa.Name().AsIdentifier().Text

			// @compileMembersOnly enums cannot be used in optional chains
			if t.hasTypeAnnotation(pa.Expression, AnnotCompileMembersOnly) {
				t.addError(link, dw.UnsupportedOptionalCompileMembersOnly,
					"Optional calls are not supported on enums marked with @compileMembersOnly.")
			}

			// Check .length on arrays and strings
			if prop == "length" && (t.isArrayType(pa.Expression) || t.isStringExpression(pa.Expression)) {
				result = t.luaTarget.LenExpr(result)
			} else {
				result = lua.Index(result, lua.Str(prop))
			}

			// Capture this-value for outer chain if this is the last PA/EA
			if isLast && thisCapture != nil {
				t.addPrecedingStatements(lua.Assign([]lua.Expression{thisCapture}, []lua.Expression{lastObjBeforeAccess}))
			}

		case ast.KindElementAccessExpression:
			lastObjBeforeAccess = result
			ea := link.AsElementAccessExpression()
			index := t.transformElementIndex(ea)
			result = lua.Index(result, index)

			// Capture this-value for outer chain if this is the last PA/EA
			if isLast && thisCapture != nil {
				t.addPrecedingStatements(lua.Assign([]lua.Expression{thisCapture}, []lua.Expression{lastObjBeforeAccess}))
			}

		case ast.KindCallExpression:
			// Determine this-value for the call
			var thisValue lua.Expression
			if i == 0 && initialThisValue != nil {
				// Direct call (chain starts with call): use captured this from left expression
				thisValue = initialThisValue
			} else if lastObjBeforeAccess != nil {
				// Method call (call follows property/element access): use the object
				thisValue = lastObjBeforeAccess
			}
			result = t.transformCallInChain(link, result, thisValue)
			lastObjBeforeAccess = nil
		}
	}

	prec := t.popPrecedingStatements()
	return result, prec
}

// transformCallInChain handles a call expression within an optional chain.
// It uses the original TS call node for type info/builtins/self-detection,
// but with a pre-built Lua callee expression from the chain.
func (t *Transpiler) transformCallInChain(callNode *ast.Node, calleeExpr lua.Expression, thisValue lua.Expression) lua.Expression {
	ce := callNode.AsCallExpression()

	// Language extension calls (uses original node)
	if result := t.tryTransformLanguageExtensionCallExpression(callNode); result != nil {
		// Only diagnose if the call itself is optional (has ?.), not just part of an optional chain
		if hasQuestionDotToken(callNode) {
			t.addError(callNode, dw.UnsupportedBuiltinOptionalCall,
				"Optional calls are not supported for builtin or language extension functions.")
		}
		return result
	}

	// Function.prototype.call/apply/bind: detect from original callee
	calleeOriginal := unwrapCallee(ce.Expression)
	if calleeOriginal.Kind == ast.KindPropertyAccessExpression {
		pa := calleeOriginal.AsPropertyAccessExpression()
		method := pa.Name().AsIdentifier().Text
		if method == "call" || method == "apply" || method == "bind" {
			// For call/apply/bind in a chain, the function is the chain-built callee's table part.
			// e.g., fn?.call(thisArg, args) → calleeExpr = ____opt_0.call → fn = ____opt_0
			if idx, ok := calleeExpr.(*lua.TableIndexExpression); ok {
				return t.tryTransformFunctionCallApplyWithCallee(ce, idx.Table, method)
			}
		}
	}

	// Transform arguments
	argExprs, argPrec := t.transformArgsInScope(ce.Arguments)

	// Builtin calls: use original node for type detection, chain-built receiver
	if calleeOriginal.Kind == ast.KindPropertyAccessExpression {
		if idx, ok := calleeExpr.(*lua.TableIndexExpression); ok {
			obj := idx.Table
			if len(argPrec) > 0 && t.shouldMoveToTemp(obj) {
				obj = t.moveToPrecedingTemp(obj)
			}
			if result := t.tryTransformBuiltinCallWithArgs(ce, argExprs, obj); result != nil {
				// Only diagnose if the call itself is optional (has ?.), not just part of an optional chain
				if hasQuestionDotToken(callNode) {
					t.addError(callNode, dw.UnsupportedBuiltinOptionalCall,
						"Optional calls are not supported for builtin or language extension functions.")
				}
				t.addPrecedingStatements(argPrec...)
				return result
			}
		} else if hasQuestionDotToken(callNode) && t.isBuiltinCall(ce) {
			// When ?. is on the call itself, flattenChain pulls the property access into the
			// left expression, so calleeExpr is a bare identifier (not TableIndexExpression).
			// We can't do the full builtin transform without the obj, but we still diagnose.
			t.addError(callNode, dw.UnsupportedBuiltinOptionalCall,
				"Optional calls are not supported for builtin or language extension functions.")
		}
	} else if hasQuestionDotToken(callNode) && t.isBuiltinCall(ce) {
		// Bare global function calls in optional chains: Number?.(), String?.(), etc.
		t.addError(callNode, dw.UnsupportedBuiltinOptionalCall,
			"Optional calls are not supported for builtin or language extension functions.")
	}

	// Cache callee before arg side effects if needed
	if len(argPrec) > 0 {
		calleeExpr = t.moveToPrecedingTemp(calleeExpr)
		if thisValue != nil && t.shouldMoveToTemp(thisValue) {
			thisValue = t.moveToPrecedingTemp(thisValue)
		}
		t.addPrecedingStatements(argPrec...)
	}

	// Self-context handling
	needsSelf := t.calleeNeedsSelf(callNode)
	if needsSelf {
		// Use colon syntax when callee is a property access with a valid Lua method name
		if idx, ok := calleeExpr.(*lua.TableIndexExpression); ok {
			if str, ok2 := idx.Index.(*lua.StringLiteral); ok2 && isValidLuaIdentifier(str.Value, t.luaTarget.AllowsUnicodeIds()) {
				return lua.MethodCall(idx.Table, str.Value, argExprs...)
			}
			// Invalid identifier: use explicit self
			if thisValue != nil {
				params := append([]lua.Expression{thisValue}, argExprs...)
				return lua.Call(calleeExpr, params...)
			}
		}
		if thisValue != nil {
			params := append([]lua.Expression{thisValue}, argExprs...)
			return lua.Call(calleeExpr, params...)
		}
		params := append([]lua.Expression{t.defaultSelfContext()}, argExprs...)
		return lua.Call(calleeExpr, params...)
	}

	return lua.Call(calleeExpr, argExprs...)
}

// patchStatementIdentifier patches identifiers in a Lua statement tree.
func patchStatementIdentifier(stmt lua.Statement, oldName, newName string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *lua.VariableDeclarationStatement:
		for _, id := range s.Left {
			if id.Text == oldName {
				id.Text = newName
			}
		}
		for _, e := range s.Right {
			patchIdentifier(e, oldName, newName)
		}
	case *lua.AssignmentStatement:
		for _, e := range s.Left {
			patchIdentifier(e, oldName, newName)
		}
		for _, e := range s.Right {
			patchIdentifier(e, oldName, newName)
		}
	case *lua.ExpressionStatement:
		patchIdentifier(s.Expression, oldName, newName)
	case *lua.IfStatement:
		patchIdentifier(s.Condition, oldName, newName)
		if s.IfBlock != nil {
			for _, is := range s.IfBlock.Statements {
				patchStatementIdentifier(is, oldName, newName)
			}
		}
	}
}

// patchIdentifier recursively replaces all identifiers named oldName with newName
// in a Lua expression tree. Used to avoid double-transforming optional chain right sides.
func patchIdentifier(expr lua.Expression, oldName, newName string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *lua.Identifier:
		if e.Text == oldName {
			e.Text = newName
		}
	case *lua.TableIndexExpression:
		patchIdentifier(e.Table, oldName, newName)
		patchIdentifier(e.Index, oldName, newName)
	case *lua.CallExpression:
		patchIdentifier(e.Expression, oldName, newName)
		for _, a := range e.Params {
			patchIdentifier(a, oldName, newName)
		}
	case *lua.MethodCallExpression:
		patchIdentifier(e.Prefix, oldName, newName)
		for _, a := range e.Params {
			patchIdentifier(a, oldName, newName)
		}
	case *lua.BinaryExpression:
		patchIdentifier(e.Left, oldName, newName)
		patchIdentifier(e.Right, oldName, newName)
	case *lua.UnaryExpression:
		patchIdentifier(e.Operand, oldName, newName)
	}
}

func wrapExprInStatement(expr lua.Expression) lua.Statement {
	switch expr.(type) {
	case *lua.CallExpression, *lua.MethodCallExpression:
		return lua.ExprStmt(expr)
	case *lua.Identifier, *lua.NilLiteral:
		return nil
	}
	return lua.LocalDecl([]*lua.Identifier{lua.Ident("____")}, []lua.Expression{expr})
}
