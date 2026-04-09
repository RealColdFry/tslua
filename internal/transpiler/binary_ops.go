package transpiler

import (
	"fmt"
	"strconv"

	"github.com/microsoft/typescript-go/shim/ast"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// In TSTL compat mode: wraps all non-string-literal, non-numeric-literal, non-concat exprs.
// In optimized mode: also skips wrapping for numeric types (Lua .. handles numbers natively).
func (t *Transpiler) wrapInToStringForConcat(expr lua.Expression, tsNode *ast.Node) lua.Expression {
	if t.isStringExpression(tsNode) {
		return expr
	}
	switch expr.(type) {
	case *lua.StringLiteral, *lua.NumericLiteral:
		return expr
	}
	if be, ok := expr.(*lua.BinaryExpression); ok && be.Operator == lua.OpConcat {
		return expr
	}
	if t.emitMode == EmitModeOptimized && t.isNumericExpression(tsNode) {
		return expr
	}
	return lua.Call(lua.Ident("tostring"), expr)
}

func (t *Transpiler) transformBinaryExpression(node *ast.Node) lua.Expression {
	be := node.AsBinaryExpression()
	op := be.OperatorToken.Kind

	if op == ast.KindEqualsToken {
		if ast.IsOptionalChain(be.Left) {
			t.addError(be.Left, dw.NotAllowedOptionalAssignment,
				"The left-hand side of an assignment expression may not be an optional property access.")
			return lua.Nil()
		}
		// Array destructuring reassignment: [a, b] = expr → temp=expr; a,b = unpack(temp); return temp
		if be.Left.Kind == ast.KindArrayLiteralExpression {
			return t.transformArrayDestructuringAssignment(be)
		}
		// Object destructuring reassignment: ({ x, y } = expr) → temp = expr; x = temp.x; y = temp.y
		if be.Left.Kind == ast.KindObjectLiteralExpression {
			return t.transformObjectDestructuringAssignment(be)
		}
		// array.length = x → __TS__ArraySetLength(array, x)
		if t.isArrayLengthTarget(be.Left) {
			pa := be.Left.AsPropertyAccessExpression()
			exprs := t.transformOrderedExpressions([]*ast.Node{pa.Expression, be.Right})
			fn := t.requireLualib("__TS__ArraySetLength")
			call := lualibCall(fn, exprs[0], exprs[1])
			t.addPrecedingStatements(lua.ExprStmt(call))
			return exprs[1]
		}
		// super.prop = value where prop is a set accessor
		if be.Left.Kind == ast.KindPropertyAccessExpression {
			pa := be.Left.AsPropertyAccessExpression()
			if pa.Expression.Kind == ast.KindSuperKeyword && t.isSuperSetAccessor(be.Left) {
				right := t.transformExpression(be.Right)
				prop := pa.Name().AsIdentifier().Text
				fn := t.requireLualib("__TS__DescriptorSet")
				call := lua.Call(lua.Ident(fn), lua.Ident("self"), t.superBaseExpression(), lua.Str(prop), right)
				t.addPrecedingStatements(lua.ExprStmt(call))
				return right
			}
		}
		// Property/element access LHS needs execution order preservation when RHS has side effects
		if isAccessExpression(be.Left) {
			right, rightPrec := t.transformExprInScope(be.Right)
			left := t.transformAssignmentLHS(be.Left, len(rightPrec) > 0)
			t.addPrecedingStatements(rightPrec...)
			rightTemp := t.moveToPrecedingTemp(right)
			t.addPrecedingStatements(lua.Assign([]lua.Expression{left}, []lua.Expression{rightTemp}))
			return rightTemp
		}
		left := t.transformExpression(be.Left)
		right := t.transformExpression(be.Right)
		// Validate function context compatibility after RHS transform,
		// so type assertion diagnostics (from inner expressions) come first.
		t.validateBinaryAssignment(be)
		stmts := []lua.Statement{lua.Assign([]lua.Expression{left}, []lua.Expression{right})}
		stmts = append(stmts, t.emitExportSync(be.Left)...)
		t.addPrecedingStatements(stmts...)
		return left
	}

	// Short-circuit operators: &&, ||, ??
	if op == ast.KindAmpersandAmpersandToken || op == ast.KindBarBarToken || op == ast.KindQuestionQuestionToken {
		return t.transformShortCircuitBinaryExpression(be, op)
	}

	// typeof x === "type" → type(x) == "type" (with JS→Lua type name mapping)
	if result := t.tryTransformTypeOfBinaryExpression(be, op); result != nil {
		return result
	}

	// Logical assignment operators: RHS is only conditionally evaluated,
	// so it must be transformed in its own scope to capture preceding statements.
	if op == ast.KindAmpersandAmpersandEqualsToken || op == ast.KindBarBarEqualsToken || op == ast.KindQuestionQuestionEqualsToken {
		return t.transformLogicalAssignment(be, op)
	}

	// Compound assignment: left = left op right
	// Emit as preceding statement so it works in expression context (e.g. return x += 1)
	if isCompoundAssignment(op) {
		return t.transformCompoundAssignment(be)
	}

	// in operator: "key" in obj → obj.key ~= nil
	// Matches TSTL: raw transform for both sides (bubble-up)
	if op == ast.KindInKeyword {
		left := t.transformExpression(be.Left)
		right := t.transformExpression(be.Right)
		return lua.Binary(lua.Index(right, left), lua.OpNeq, lua.Nil())
	}

	// instanceof
	// Matches TSTL: raw transform for both sides (bubble-up)
	if op == ast.KindInstanceOfKeyword {
		left := t.transformExpression(be.Left)
		right := t.transformExpression(be.Right)
		// Alternative class style: method-based instanceof
		switch t.classStyle.instanceOfBehavior() {
		case "method":
			return lua.MethodCall(left, "isInstanceOf", right)
		case "none":
			t.addError(be.Left, dw.UnsupportedProperty, "instanceof is not supported with this class style")
			return lua.Bool(false)
		}
		if be.Right.Kind == ast.KindIdentifier && be.Right.AsIdentifier().Text == "Object" {
			fn := t.requireLualib("__TS__InstanceOfObject")
			return lua.Call(lua.Ident(fn), left)
		}
		fn := t.requireLualib("__TS__InstanceOf")
		return lua.Call(lua.Ident(fn), left, right)
	}

	// Comma operator: evaluate left for side effects, return right
	if op == ast.KindCommaToken {
		t.addPrecedingStatements(t.transformAsStatement(be.Left)...)
		return t.transformExpression(be.Right)
	}

	// Default: all remaining binary operators (arithmetic, comparison, bitwise, %)
	// Transform both operands with evaluation order preservation.
	exprs := t.transformOrderedExpressions([]*ast.Node{be.Left, be.Right})
	left, right := exprs[0], exprs[1]

	// Lua 5.0: % → math.mod(left, right)
	if op == ast.KindPercentToken && !t.luaTarget.HasModOperator() {
		return lua.Call(lua.Index(lua.Ident("math"), lua.Str("mod")), left, right)
	}

	// Bitwise operators: native (5.3+), bit32.band (5.2), bit.band (JIT/5.1)
	if _, ok := bitwiseLibFunc(op); ok {
		return t.transformBitwiseBinaryOp(node, op, left, right)
	}

	// Map to Lua AST operator
	if luaOp, ok := t.mapBinaryToLuaOp(op, be); ok {
		if luaOp == lua.OpConcat {
			left = t.wrapInToStringForConcat(left, be.Left)
			right = t.wrapInToStringForConcat(right, be.Right)
		}
		return lua.Binary(left, luaOp, right)
	}

	panic(fmt.Sprintf("unhandled binary operator: %d", op))
}

// transformCompoundAssignmentStmt handles compound assignments (+=, -=, etc.) in statement context.
// Unlike the expression path, this only caches obj/index when they have actual side effects
// or when the RHS has preceding statements. For simple cases like `o.p += 1`, emits directly
// as `o.p = o.p + 1` without temps.
func (t *Transpiler) transformCompoundAssignmentStmt(be *ast.BinaryExpression) []lua.Statement {
	op := be.OperatorToken.Kind

	// array.length op= val → __TS__ArraySetLength(arr, #arr op val)
	if t.isArrayLengthTarget(be.Left) {
		pa := be.Left.AsPropertyAccessExpression()
		rightExpr, rightPrec := t.transformExprInScope(be.Right)
		arrExpr := t.transformExpression(pa.Expression)
		lenExpr := t.luaTarget.LenExpr(arrExpr)
		newLen := t.compoundRHS(op, lenExpr, rightExpr, be)
		fn := t.requireLualib("__TS__ArraySetLength")
		call := lualibCall(fn, arrExpr, newLen)
		var result []lua.Statement
		result = append(result, rightPrec...)
		result = append(result, lua.ExprStmt(call))
		return result
	}

	// For property/element access LHS: only cache when table/index have side effects or RHS has preceding statements.
	if isAccessExpression(be.Left) {
		leftExpr, leftPrec := t.transformExprInScope(be.Left)
		rightExpr, rightPrec := t.transformExprInScope(be.Right)

		idx, isIndex := leftExpr.(*lua.TableIndexExpression)
		needsCache := isIndex && (len(leftPrec) > 0 || len(rightPrec) > 0 || luaExprHasSideEffect(idx.Table) || luaExprHasSideEffect(idx.Index))
		if needsCache {
			objTemp := t.nextTempForLuaExpression(idx.Table)
			idxTemp := t.nextTempForLuaExpression(idx.Index)
			cachedLeft := lua.Index(lua.Ident(objTemp), lua.Ident(idxTemp))
			rhs := t.compoundRHS(op, cachedLeft, rightExpr, be)
			var result []lua.Statement
			result = append(result, leftPrec...)
			result = append(result, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(objTemp), lua.Ident(idxTemp)},
				[]lua.Expression{idx.Table, idx.Index},
			))
			result = append(result, rightPrec...)
			result = append(result, lua.Assign([]lua.Expression{cachedLeft}, []lua.Expression{rhs}))
			return result
		}

		var result []lua.Statement
		result = append(result, leftPrec...)
		result = append(result, rightPrec...)
		rhs := t.compoundRHS(op, leftExpr, rightExpr, be)
		result = append(result, lua.Assign([]lua.Expression{leftExpr}, []lua.Expression{rhs}))
		return result
	}

	// Simple identifier case: no caching needed
	leftExpr := t.transformExpression(be.Left)
	if !lua.IsAssignmentTarget(leftExpr) {
		t.addError(be.Left, dw.CannotAssignToNodeOfKind,
			fmt.Sprintf("Cannot create assignment assigning to a node of type %d.", leftExpr.Kind()))
		return nil
	}
	rightExpr, rightPrec := t.transformExprInScope(be.Right)
	rhs := t.compoundRHS(op, leftExpr, rightExpr, be)
	var result []lua.Statement
	result = append(result, rightPrec...)
	result = append(result, lua.Assign([]lua.Expression{leftExpr}, []lua.Expression{rhs}))
	result = append(result, t.emitExportSync(be.Left)...)
	return result
}

// Handles compound assignments (+=, -=, etc.) in expression context with side-effect caching.
// When the LHS is a property/element access, caches the receiver and index in
// temp vars to avoid evaluating them twice and to preserve evaluation order
// when the RHS has preceding statements.
func (t *Transpiler) transformCompoundAssignment(be *ast.BinaryExpression) lua.Expression {
	op := be.OperatorToken.Kind

	// array.length op= val → __TS__ArraySetLength(arr, #arr op val)
	if t.isArrayLengthTarget(be.Left) {
		pa := be.Left.AsPropertyAccessExpression()
		// Preserve evaluation order: array (left) before value (right)
		exprs := t.transformOrderedExpressions([]*ast.Node{pa.Expression, be.Right})
		arrExpr, rightExpr := exprs[0], exprs[1]
		// Cache array reference if it could have side effects (avoids double evaluation)
		if t.shouldMoveToTemp(arrExpr) {
			arrExpr = t.moveToPrecedingTemp(arrExpr)
		}
		lenExpr := t.luaTarget.LenExpr(arrExpr)
		newLen := t.compoundRHS(op, lenExpr, rightExpr, be)
		fn := t.requireLualib("__TS__ArraySetLength")
		call := lualibCall(fn, arrExpr, newLen)
		t.addPrecedingStatements(lua.ExprStmt(call))
		return t.luaTarget.LenExpr(arrExpr)
	}

	// For property/element access LHS: transform LHS first, then RHS in scope,
	// and cache obj/index if either the LHS has side effects or RHS has preceding statements.
	if isAccessExpression(be.Left) {
		leftExpr := t.transformExpression(be.Left)
		rightExpr, rightPrec := t.transformExprInScope(be.Right)

		// Expression context: always cache table+index and result value.
		idx, isIndex := leftExpr.(*lua.TableIndexExpression)
		if isIndex {
			origName := tempNameForLuaExpression(leftExpr)
			objTemp := t.nextTempForLuaExpression(idx.Table)
			idxTemp := t.nextTempForLuaExpression(idx.Index)
			t.addPrecedingStatements(lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(objTemp), lua.Ident(idxTemp)},
				[]lua.Expression{idx.Table, idx.Index},
			))
			cachedLeft := lua.Index(lua.Ident(objTemp), lua.Ident(idxTemp))
			t.addPrecedingStatements(rightPrec...)
			rhs := t.compoundRHS(op, cachedLeft, rightExpr, be)
			// Cache result in temp — avoids re-reading property (matches TSTL)
			resultTemp := t.nextTemp(origName)
			t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{lua.Ident(resultTemp)}, []lua.Expression{rhs}))
			t.addPrecedingStatements(lua.Assign([]lua.Expression{cachedLeft}, []lua.Expression{lua.Ident(resultTemp)}))
			return lua.Ident(resultTemp)
		}

		t.addPrecedingStatements(rightPrec...)
		rhs := t.compoundRHS(op, leftExpr, rightExpr, be)
		t.addPrecedingStatements(lua.Assign([]lua.Expression{leftExpr}, []lua.Expression{rhs}))
		return leftExpr
	}

	// Simple identifier case: no caching needed
	leftExpr := t.transformExpression(be.Left)
	if !lua.IsAssignmentTarget(leftExpr) {
		t.addError(be.Left, dw.CannotAssignToNodeOfKind,
			fmt.Sprintf("Cannot create assignment assigning to a node of type %d.", leftExpr.Kind()))
		return leftExpr
	}
	rightExpr := t.transformExpression(be.Right)
	rhs := t.compoundRHS(op, leftExpr, rightExpr, be)
	t.addPrecedingStatements(lua.Assign([]lua.Expression{leftExpr}, []lua.Expression{rhs}))
	t.addPrecedingStatements(t.emitExportSync(be.Left)...)
	return leftExpr
}

// compoundRHS builds the right-hand side of a compound assignment: left op right.
// For bitwise compound ops (e.g. &=), returns bit.band(left, right).
// For arithmetic compound ops (e.g. +=), returns left + (right).
// be is the original TS binary expression, needed for type-checking += (string concat vs add).
func (t *Transpiler) compoundRHS(op ast.Kind, left, right lua.Expression, be *ast.BinaryExpression) lua.Expression {
	// Bitwise compound assignments — delegate to target-aware bitwise codegen
	var bitwiseOp ast.Kind
	switch op {
	case ast.KindAmpersandEqualsToken:
		bitwiseOp = ast.KindAmpersandToken
	case ast.KindBarEqualsToken:
		bitwiseOp = ast.KindBarToken
	case ast.KindCaretEqualsToken:
		bitwiseOp = ast.KindCaretToken
	case ast.KindLessThanLessThanEqualsToken:
		bitwiseOp = ast.KindLessThanLessThanToken
	case ast.KindGreaterThanGreaterThanEqualsToken:
		bitwiseOp = ast.KindGreaterThanGreaterThanToken
	case ast.KindGreaterThanGreaterThanGreaterThanEqualsToken:
		bitwiseOp = ast.KindGreaterThanGreaterThanGreaterThanToken
	}
	if bitwiseOp != 0 {
		return t.transformBitwiseBinaryOp(be.OperatorToken, bitwiseOp, left, right)
	}
	var luaOp lua.Operator
	switch op {
	case ast.KindPlusEqualsToken:
		luaOp = lua.OpAdd
		if t.mapPlusOperator(be) == ".." {
			luaOp = lua.OpConcat
		}
	case ast.KindMinusEqualsToken:
		luaOp = lua.OpSub
	case ast.KindAsteriskEqualsToken:
		luaOp = lua.OpMul
	case ast.KindSlashEqualsToken:
		luaOp = lua.OpDiv
	case ast.KindPercentEqualsToken:
		if !t.luaTarget.HasModOperator() {
			return lua.Call(lua.Index(lua.Ident("math"), lua.Str("mod")), left, right)
		}
		luaOp = lua.OpMod
	case ast.KindAsteriskAsteriskEqualsToken:
		luaOp = lua.OpPow
	default:
		return lua.Comment(fmt.Sprintf("TODO: compound op %d", op))
	}
	return lua.Binary(left, luaOp, right)
}

// transformElementAccessIndex transforms the index of an element access, adding +1 for array types.
// Mirrors TSTL's transformElementAccessArgument.
func (t *Transpiler) transformElementAccessIndex(ea *ast.ElementAccessExpression) lua.Expression {
	// For array access with numeric literal index, add 1
	if t.isArrayType(ea.Expression) && ea.ArgumentExpression.Kind == ast.KindNumericLiteral {
		n, err := strconv.Atoi(ea.ArgumentExpression.AsNumericLiteral().Text)
		if err == nil {
			return lua.Num(fmt.Sprintf("%d", n+1))
		}
	}

	index := t.transformExpression(ea.ArgumentExpression)

	// For array access with non-literal numeric index, add 1
	if t.isArrayType(ea.Expression) && t.isNumericExpression(ea.ArgumentExpression) {
		return addToNumericExpression(index, 1)
	}

	return index
}

// transformAssignmentLHS transforms the left-hand side of an assignment.
// If rightHasPrecedingStatements is true and LHS is a property/element access,
// caches the table and index expressions in temps to preserve evaluation order.
func (t *Transpiler) transformAssignmentLHS(node *ast.Node, rightHasPrecedingStatements bool) lua.Expression {
	if rightHasPrecedingStatements {
		if node.Kind == ast.KindPropertyAccessExpression {
			pa := node.AsPropertyAccessExpression()
			table := t.transformExpression(pa.Expression)
			table = t.moveToPrecedingTemp(table)
			return lua.Index(table, lua.Str(pa.Name().AsIdentifier().Text))
		}
		if node.Kind == ast.KindElementAccessExpression {
			ea := node.AsElementAccessExpression()
			table := t.transformExpression(ea.Expression)
			table = t.moveToPrecedingTemp(table)
			index := t.transformElementAccessIndex(ea)
			index = t.moveToPrecedingTemp(index)
			return lua.Index(table, index)
		}
	}
	left := t.transformExpression(node)
	if !lua.IsAssignmentTarget(left) {
		t.addError(node, dw.CannotAssignToNodeOfKind,
			fmt.Sprintf("Cannot create assignment assigning to a node of type %d.", left.Kind()))
		return lua.Ident("_")
	}
	return left
}

// mapBinaryToLuaOp maps a TS binary operator to a lua.Operator, returning false for
// operators that need special handling (assignment, compound, bitwise funcs, etc.)
func (t *Transpiler) mapBinaryToLuaOp(op ast.Kind, be *ast.BinaryExpression) (lua.Operator, bool) {
	switch op {
	case ast.KindPlusToken:
		if t.mapPlusOperator(be) == ".." {
			return lua.OpConcat, true
		}
		return lua.OpAdd, true
	case ast.KindMinusToken:
		return lua.OpSub, true
	case ast.KindAsteriskToken:
		return lua.OpMul, true
	case ast.KindSlashToken:
		return lua.OpDiv, true
	case ast.KindPercentToken:
		return lua.OpMod, true
	case ast.KindAsteriskAsteriskToken:
		return lua.OpPow, true
	case ast.KindEqualsEqualsToken, ast.KindEqualsEqualsEqualsToken:
		return lua.OpEq, true
	case ast.KindExclamationEqualsToken, ast.KindExclamationEqualsEqualsToken:
		return lua.OpNeq, true
	case ast.KindLessThanToken:
		return lua.OpLt, true
	case ast.KindGreaterThanToken:
		return lua.OpGt, true
	case ast.KindLessThanEqualsToken:
		return lua.OpLe, true
	case ast.KindGreaterThanEqualsToken:
		return lua.OpGe, true
	}
	return 0, false
}

func (t *Transpiler) transformLogicalAssignment(be *ast.BinaryExpression, op ast.Kind) lua.Expression {
	// Cache the object/index of property/element access LHS to avoid double evaluation.
	// getObj().prop ||= 4 → local ____temp = getObj(); if not ____temp.prop then ____temp.prop = 4 end
	var leftExpr lua.Expression
	switch be.Left.Kind {
	case ast.KindPropertyAccessExpression:
		pa := be.Left.AsPropertyAccessExpression()
		obj := t.transformExpression(pa.Expression)
		obj = t.moveToPrecedingTemp(obj)
		prop := pa.Name().AsIdentifier().Text
		leftExpr = lua.Index(obj, lua.Str(prop))
	case ast.KindElementAccessExpression:
		ea := be.Left.AsElementAccessExpression()
		obj := t.transformExpression(ea.Expression)
		obj = t.moveToPrecedingTemp(obj)
		idx := t.transformElementAccessIndex(ea)
		idx = t.moveToPrecedingTemp(idx)
		leftExpr = lua.Index(obj, idx)
	default:
		leftExpr = t.transformExpression(be.Left)
	}
	rightExpr, rightPre := t.transformExprInScope(be.Right)

	var cond lua.Expression
	switch op {
	case ast.KindAmpersandAmpersandEqualsToken:
		cond = leftExpr // x &&= y → if x then x = y end
	case ast.KindBarBarEqualsToken:
		cond = lua.Unary(lua.OpNot, leftExpr) // x ||= y → if not x then x = y end
	case ast.KindQuestionQuestionEqualsToken:
		cond = lua.Binary(leftExpr, lua.OpEq, lua.Nil()) // x ??= y → if x == nil then x = y end
	}

	ifBody := make([]lua.Statement, 0, len(rightPre)+1)
	ifBody = append(ifBody, rightPre...)
	ifBody = append(ifBody, lua.Assign([]lua.Expression{leftExpr}, []lua.Expression{rightExpr}))

	t.addPrecedingStatements(&lua.IfStatement{
		Condition: cond,
		IfBlock:   &lua.Block{Statements: ifBody},
	})
	return leftExpr
}

func (t *Transpiler) mapPlusOperator(be *ast.BinaryExpression) string {
	leftType := t.checker.GetTypeAtLocation(be.Left)
	rightType := t.checker.GetTypeAtLocation(be.Right)
	if t.isStringType(leftType) || t.isStringType(rightType) {
		return ".."
	}
	return "+"
}

func bitwiseLibFunc(op ast.Kind) (string, bool) {
	switch op {
	case ast.KindAmpersandToken:
		return "band", true
	case ast.KindBarToken:
		return "bor", true
	case ast.KindCaretToken:
		return "bxor", true
	case ast.KindLessThanLessThanToken:
		return "lshift", true
	case ast.KindGreaterThanGreaterThanToken:
		return "arshift", true
	case ast.KindGreaterThanGreaterThanGreaterThanToken:
		return "rshift", true
	}
	return "", false
}

func bitwiseNativeOp(op ast.Kind) (lua.Operator, bool) {
	switch op {
	case ast.KindAmpersandToken:
		return lua.OpBitAnd, true
	case ast.KindBarToken:
		return lua.OpBitOr, true
	case ast.KindCaretToken:
		return lua.OpBitXor, true
	case ast.KindLessThanLessThanToken:
		return lua.OpBitShl, true
	case ast.KindGreaterThanGreaterThanToken:
		return lua.OpBitShr, true
	}
	return 0, false
}

func (t *Transpiler) transformBitwiseBinaryOp(node *ast.Node, op ast.Kind, left, right lua.Expression) lua.Expression {
	// Native operators for Lua 5.3+
	if t.luaTarget.HasNativeBitwise() {
		// Lua 5.3's >> is arithmetic (sign-extending). TS >> is also arithmetic, so it maps directly.
		// TS >>> is unsigned (zero-fill) — mask left operand to uint32 first: (a & 0xFFFFFFFF) >> b
		if op == ast.KindGreaterThanGreaterThanToken {
			t.addError(node, dw.UnsupportedRightShiftOperator, "Right shift operator is not supported for target Lua 5.3. Use `>>>` instead.")
			return lua.Binary(left, lua.OpBitShr, right)
		}
		if op == ast.KindGreaterThanGreaterThanGreaterThanToken {
			masked := lua.Binary(left, lua.OpBitAnd, lua.Num("0xFFFFFFFF"))
			return lua.Binary(lua.Paren(masked), lua.OpBitShr, right)
		}
		if luaOp, ok := bitwiseNativeOp(op); ok {
			return lua.Binary(left, luaOp, right)
		}
	}
	// Library-based for older targets
	lib := t.luaTarget.BitLibrary()
	if lib == "" {
		t.addError(node, dw.UnsupportedForTarget, fmt.Sprintf("Bitwise operations is/are not supported for target %s.", t.luaTarget.DisplayName()))
		lib = "bit"
	}
	fn, _ := bitwiseLibFunc(op)
	return lua.Call(memberAccess(lua.Ident(lib), fn), left, right)
}
