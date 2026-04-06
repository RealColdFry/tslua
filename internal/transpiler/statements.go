package transpiler

import (
	"fmt"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// checkVariableDeclarationList emits TL1009 if the declaration list uses `var` instead of
// `let`, `const`, `using`, or `await using`.
func (t *Transpiler) checkVariableDeclarationList(node *ast.Node) {
	flags := node.Flags
	if flags&(ast.NodeFlagsLet|ast.NodeFlagsConst|ast.NodeFlagsUsing|ast.NodeFlagsAwaitUsing) == 0 {
		t.addError(node, dw.UnsupportedVarDeclaration, "`var` declarations are not supported. Use `let` or `const` instead.")
	}
}

// transformVariableStatement handles `let x = 1`, `export const y = 2`, destructuring, etc.
func (t *Transpiler) transformVariableStatement(node *ast.Node) []lua.Statement {
	vs := node.AsVariableStatement()
	declList := vs.DeclarationList.AsVariableDeclarationList()
	t.checkVariableDeclarationList(vs.DeclarationList)
	isExported := hasExportModifier(node)

	comments := t.getLeadingComments(node)

	var result []lua.Statement

	// Check for @customName annotation
	customName := t.getCustomName(node)

	for _, decl := range declList.Declarations.Nodes {
		d := decl.AsVariableDeclaration()
		nameNode := d.Name()

		// Validate function context compatibility when both initializer and type annotation exist.
		// Runs for all declarations (including destructuring) before the name-specific handling.
		if d.Initializer != nil && d.Type != nil && t.checker != nil {
			initType := t.checker.GetTypeAtLocation(d.Initializer)
			varType := checker.Checker_getTypeFromTypeNode(t.checker, d.Type)
			if initType != nil && varType != nil {
				t.validateAssignment(d.Initializer, initType, varType, "")
			}
		}

		switch nameNode.Kind {
		case ast.KindArrayBindingPattern, ast.KindObjectBindingPattern:
			result = append(result, t.transformVariableDestructuring(nameNode, d.Initializer, !isExported && t.shouldUseLocalDeclaration(), isExported)...)
		default:
			name := nameNode.AsIdentifier().Text
			safeName := name
			if customName != "" {
				name = customName
				safeName = customName
			}
			if t.hasUnsafeIdentifierName(nameNode) {
				safeName = luaSafeName(name)
			}

			// Register the LHS symbol BEFORE transforming the initializer.
			// This is the first "reference" for hasMultipleReferences counting;
			// any reference inside the initializer will be the second.
			var symID SymbolID
			if t.inScope() && t.checker != nil {
				if sym := t.checker.GetSymbolAtLocation(nameNode); sym != nil {
					symID = t.getOrCreateSymbolID(sym)
					t.markSymbolDeclared(symID)
				}
			}

			var initExpr lua.Expression
			var prec []lua.Statement
			if d.Initializer != nil {
				initExpr, prec = t.transformExprInScope(d.Initializer)
				result = append(result, prec...)

				// Wrap functions being assigned to a type that contains additional properties
				// This catches 'const foo = function() {}; foo.bar = "FOOBAR";'
				if initExpr != nil && t.shouldWrapInCallableTable(d.Initializer, nameNode) {
					initExpr = createCallableTable(initExpr)
				}
			}

			if isExported {
				// Exported: target.name = value
				// Inside a namespace, target is the namespace table; otherwise ____exports
				// With exportAsGlobal: emit as bare global assignment
				if t.isExportAsGlobalTopLevel() {
					rhs := initExpr
					if rhs == nil {
						rhs = lua.Nil()
					}
					result = append(result, lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(safeName)},
						[]lua.Expression{rhs},
					))
				} else {
					exportTarget := "____exports"
					if t.currentNamespace != "" {
						exportTarget = t.currentNamespace
					}
					rhs := initExpr
					if rhs == nil {
						rhs = lua.Nil()
					}
					result = append(result, lua.Assign(
						[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str(name))},
						[]lua.Expression{rhs},
					))
				}
			} else if t.shouldUseLocalDeclaration() {
				// Centralized declaration splitting via hasMultipleReferences.
				// If the symbol was referenced during its initializer transform
				// (count > 1: declaration + usage), split into `local x; x = val`.
				// This handles both self-referencing closures and callable table wrapping.
				scope := t.peekScope()
				if t.inScope() && initExpr != nil && t.hasMultipleReferences(scope, symID) {
					// Split: local x; x = val
					precedingDecl := lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(safeName)},
						nil,
					)
					// Insert before any preceding statements from the initializer
					insertIdx := len(result) - len(prec)
					if insertIdx < 0 {
						insertIdx = 0
					}
					result = append(result[:insertIdx], append([]lua.Statement{precedingDecl}, result[insertIdx:]...)...)
					result = append(result, lua.Assign(
						[]lua.Expression{lua.Ident(safeName)},
						[]lua.Expression{initExpr},
					))
					t.addScopeVariableDeclaration(precedingDecl, symID)
				} else {
					var vals []lua.Expression
					if initExpr != nil {
						vals = []lua.Expression{initExpr}
					}
					decl := lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(safeName)},
						vals,
					)
					if t.inScope() {
						t.addScopeVariableDeclaration(decl, symID)
					}
					result = append(result, decl)
				}
			} else {
				// Script top-level: global
				rhs := initExpr
				if rhs == nil {
					rhs = lua.Nil()
				}
				result = append(result, lua.Assign(
					[]lua.Expression{lua.Ident(safeName)},
					[]lua.Expression{rhs},
				))
			}
		}
	}

	if len(comments) > 0 && len(result) > 0 {
		setLeadingComments(result[0], comments)
	}

	return result
}

// shouldWrapInCallableTable checks whether a variable initializer should be wrapped
// in a callable table (setmetatable). This applies when the initializer is a function/arrow
// expression and the variable's type has properties beyond call signatures.
func (t *Transpiler) shouldWrapInCallableTable(initializer *ast.Node, nameNode *ast.Node) bool {
	if t.checker == nil {
		return false
	}
	// Skip outer expressions (parentheses, type assertions, non-null assertions) in any order
	inner := initializer
	for {
		switch inner.Kind {
		case ast.KindParenthesizedExpression:
			inner = inner.AsParenthesizedExpression().Expression
			continue
		case ast.KindAsExpression:
			inner = inner.AsAsExpression().Expression
			continue
		case ast.KindNonNullExpression:
			inner = inner.AsNonNullExpression().Expression
			continue
		case ast.KindTypeAssertionExpression:
			inner = inner.AsTypeAssertion().Expression
			continue
		}
		break
	}
	// Must be a function expression or arrow function
	if inner.Kind != ast.KindFunctionExpression && inner.Kind != ast.KindArrowFunction {
		return false
	}
	// Skip named function expressions — they get wrapped in transformFunctionExpression
	if inner.Kind == ast.KindFunctionExpression && inner.AsFunctionExpression().Name() != nil {
		return false
	}
	typ := t.checker.GetTypeAtLocation(nameNode)
	return t.isFunctionTypeWithProperties(typ)
}

// transformVariableDestructuring handles both array and object destructuring in variable declarations.
// It wraps the initializer in a temp variable and uses transformBindingPattern for recursive extraction.
func (t *Transpiler) transformVariableDestructuring(pattern *ast.Node, init *ast.Node, useLocal bool, isExported bool) []lua.Statement {
	bp := pattern.AsBindingPattern()

	// Fast path: simple array destructuring (flat, no rest, no nesting, no defaults) → use unpack
	if pattern.Kind == ast.KindArrayBindingPattern && !hasComplexBindingElement(bp) {
		return t.transformSimpleArrayDestructuring(bp, init, useLocal, isExported)
	}

	// Complex path: temp var + recursive extraction
	var result []lua.Statement
	var tableExpr lua.Expression

	if init != nil {
		initExpr, prec := t.transformExprInScope(init)
		result = append(result, prec...)
		// Multi-return call: wrap in table to capture all return values
		if t.isMultiReturnCall(init) {
			initExpr = lua.Table(lua.Field(initExpr))
		}
		// Cache init in a temp to avoid re-evaluation — but skip for const
		// identifiers and already-cached temps that can't change.
		if t.shouldMoveToTempWithNode(initExpr, init) {
			temp := t.nextTemp(tempNameForLuaExpression(initExpr))
			result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{initExpr}))
			tableExpr = lua.Ident(temp)
		} else {
			tableExpr = initExpr
		}
	} else {
		tableExpr = lua.Nil()
	}

	result = append(result, t.transformBindingPattern(pattern, tableExpr, useLocal, isExported)...)
	return result
}

// hasComplexBindingElement returns true if any element has nesting, rest, or defaults.
func hasComplexBindingElement(bp *ast.BindingPattern) bool {
	for _, elem := range bp.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			continue
		}
		be := elem.AsBindingElement()
		if be.DotDotDotToken != nil {
			return true
		}
		// Note: defaults (be.Initializer != nil) are NOT complex — TSTL handles them
		// in the simple unpack path's post-declaration loop.
		if be.Name() != nil && (be.Name().Kind == ast.KindArrayBindingPattern || be.Name().Kind == ast.KindObjectBindingPattern) {
			return true
		}
	}
	return false
}

// transformSimpleArrayDestructuring handles flat array destructuring with unpack.
func (t *Transpiler) transformSimpleArrayDestructuring(bp *ast.BindingPattern, init *ast.Node, useLocal bool, isExported bool) []lua.Statement {
	var names []string
	for _, elem := range bp.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			names = append(names, "____")
			continue
		}
		be := elem.AsBindingElement()
		// Simple path: names are always plain identifiers (no nesting/rest/defaults)
		name := t.bindingElementName(be)
		names = append(names, name)
	}

	// Empty binding pattern: anonymous identifier ____ is always local (never exported).
	// TSTL uses nil for empty array literal init, evaluates others for side effects.
	emitLocal := useLocal || t.isModule // ____ is always local in modules regardless of export
	if len(names) == 0 {
		var result []lua.Statement
		if init != nil && init.Kind == ast.KindArrayLiteralExpression && len(init.AsArrayLiteralExpression().Elements.Nodes) == 0 {
			// Empty array literal: just emit nil (matching TSTL)
			if emitLocal {
				result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident("____")}, []lua.Expression{lua.Nil()}))
			}
		} else if init != nil {
			initExpr, prec := t.transformExprInScope(init)
			result = append(result, prec...)
			if emitLocal {
				result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident("____")}, []lua.Expression{initExpr}))
			}
		} else if emitLocal {
			result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident("____")}, []lua.Expression{lua.Nil()}))
		}
		return result
	}

	var result []lua.Statement

	// Array literal initializer: don't use unpack, assign elements directly.
	// Preserve left-to-right evaluation order when elements have side effects
	// (e.g. [arr[i], arr[++i]]) by caching earlier expressions to temps.
	if init != nil && init.Kind == ast.KindArrayLiteralExpression {
		ale := init.AsArrayLiteralExpression()
		nodes := ale.Elements.Nodes
		n := len(nodes)
		rhs := make([]lua.Expression, n)
		precs := make([][]lua.Statement, n)
		lastPrecIdx := -1
		for i, elem := range nodes {
			rhs[i], precs[i] = t.transformExprInScope(elem)
			if len(precs[i]) > 0 {
				lastPrecIdx = i
			}
		}
		if lastPrecIdx >= 0 {
			for i := 0; i < lastPrecIdx; i++ {
				result = append(result, precs[i]...)
				if t.shouldMoveToTemp(rhs[i]) {
					temp := t.nextTemp(tempNameForLuaExpression(rhs[i]))
					result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{rhs[i]}))
					rhs[i] = lua.Ident(temp)
				}
			}
			result = append(result, precs[lastPrecIdx]...)
		}
		if len(rhs) == 0 {
			rhs = []lua.Expression{lua.Nil()}
		}
		if useLocal {
			result = append(result, lua.LocalDecl(identList(names), rhs))
		} else {
			leftExprs := t.makeDestructLHS(names, isExported)
			result = append(result, lua.Assign(leftExprs, rhs))
		}
		// Default values: if x == nil then x = default end
		result = append(result, t.emitArrayBindingDefaults(bp, isExported)...)
		return result
	}

	// Register LHS symbols before transforming the initializer, so
	// hasMultipleReferences can detect self-referencing closures.
	var symIDs []SymbolID
	if useLocal && t.inScope() && t.checker != nil {
		for _, elem := range bp.Elements.Nodes {
			if elem.Kind == ast.KindOmittedExpression {
				continue
			}
			be := elem.AsBindingElement()
			if be.Name() != nil && be.Name().Kind == ast.KindIdentifier {
				if sym := t.checker.GetSymbolAtLocation(be.Name()); sym != nil {
					id := t.getOrCreateSymbolID(sym)
					t.markSymbolDeclared(id)
					symIDs = append(symIDs, id)
				}
			}
		}
	}

	initExpr, prec := t.transformExprInScope(init)
	result = append(result, prec...)

	// Multi-return calls: skip unpack, assign directly
	var unpackExpr lua.Expression
	if t.isMultiReturnCall(init) {
		unpackExpr = initExpr
	} else {
		unpackExpr = t.unpackCall(initExpr, len(names))
	}

	// Check if any LHS symbol was referenced during the initializer transform.
	needsSplit := false
	if useLocal && init != nil && t.inScope() {
		scope := t.peekScope()
		needsSplit = t.hasMultipleReferences(scope, symIDs...)
	}

	if needsSplit {
		result = append([]lua.Statement{lua.LocalDecl(identList(names), nil)}, result...)
		leftExprs := t.makeDestructLHS(names, isExported)
		result = append(result, lua.Assign(leftExprs, []lua.Expression{unpackExpr}))
	} else if useLocal {
		result = append(result, lua.LocalDecl(identList(names), []lua.Expression{unpackExpr}))
	} else {
		leftExprs := t.makeDestructLHS(names, isExported)
		result = append(result, lua.Assign(leftExprs, []lua.Expression{unpackExpr}))
	}
	// Default values: if x == nil then x = default end
	result = append(result, t.emitArrayBindingDefaults(bp, isExported)...)
	return result
}

// emitArrayBindingDefaults emits `if x == nil then x = default end` for each
// element with a default value in a simple array binding pattern.
func (t *Transpiler) emitArrayBindingDefaults(bp *ast.BindingPattern, isExported bool) []lua.Statement {
	var result []lua.Statement
	for _, elem := range bp.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			continue
		}
		be := elem.AsBindingElement()
		if be.Initializer != nil {
			name := t.bindingElementName(be)
			lhsExpr := t.bindingLHS(name, isExported)
			defaultVal := t.transformExpression(be.Initializer)
			result = append(result, lua.If(
				lua.Binary(lhsExpr, lua.OpEq, lua.Nil()),
				&lua.Block{Statements: []lua.Statement{
					lua.Assign([]lua.Expression{lhsExpr}, []lua.Expression{defaultVal}),
				}},
				nil,
			))
		}
	}
	return result
}

func (t *Transpiler) makeDestructLHS(names []string, isExported bool) []lua.Expression {
	leftExprs := make([]lua.Expression, len(names))
	for i, n := range names {
		if isExported && n != "_" && n != "____" {
			leftExprs[i] = lua.Index(lua.Ident("____exports"), lua.Str(n))
		} else {
			leftExprs[i] = lua.Ident(n)
		}
	}
	return leftExprs
}

// transformBindingPattern recursively extracts variables from a binding pattern,
// indexing into tableExpr. Uses a propertyAccessStack to chain nested accesses directly
// (e.g., obj.a.b) instead of creating intermediate temps at each nesting level.
func (t *Transpiler) transformBindingPattern(pattern *ast.Node, tableExpr lua.Expression, useLocal bool, isExported bool) []lua.Statement {
	return t.transformBindingPatternInner(pattern, tableExpr, useLocal, isExported, nil)
}

func (t *Transpiler) transformBindingPatternInner(pattern *ast.Node, tableExpr lua.Expression, useLocal bool, isExported bool, propertyAccessStack []lua.Expression) []lua.Statement {
	bp := pattern.AsBindingPattern()
	isObject := pattern.Kind == ast.KindObjectBindingPattern
	var hoisted []lua.Statement // split local declarations (hoisted above assignments)
	var result []lua.Statement

	for i, elem := range bp.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			continue
		}
		be := elem.AsBindingElement()

		// Nested binding pattern: push property onto stack and recurse
		if be.Name() != nil && (be.Name().Kind == ast.KindArrayBindingPattern || be.Name().Kind == ast.KindObjectBindingPattern) {
			var propKey lua.Expression
			if isObject {
				var propPrec []lua.Statement
				propKey, propPrec = t.bindingElementPropertyKey(be)
				result = append(result, propPrec...)
			} else {
				propKey = lua.Num(fmt.Sprintf("%d", i+1))
			}

			newStack := append(propertyAccessStack, propKey)
			result = append(result, t.transformBindingPatternInner(be.Name(), tableExpr, useLocal, isExported, newStack)...)
			continue
		}

		if be.Name() == nil {
			continue
		}

		// Build the full path to the table by applying the property access stack
		fullTableExpr := tableExpr
		for _, prop := range propertyAccessStack {
			fullTableExpr = lua.Index(fullTableExpr, prop)
		}

		// Leaf: extract a variable
		name := t.bindingElementName(be)

		var expression lua.Expression

		if be.DotDotDotToken != nil {
			// Rest element
			if isObject {
				fn := t.requireLualib("__TS__ObjectRest")
				var excludedFields []*lua.TableFieldExpression
				for _, other := range bp.Elements.Nodes {
					if other.Kind == ast.KindOmittedExpression {
						continue
					}
					otherBe := other.AsBindingElement()
					if otherBe.DotDotDotToken != nil {
						continue
					}
					excludedKey, _ := t.bindingElementPropertyKey(otherBe)
					excludedFields = append(excludedFields, lua.KeyField(excludedKey, lua.Bool(true)))
				}
				expression = lualibCall(fn, fullTableExpr, lua.Table(excludedFields...))
			} else {
				fn := t.requireLualib("__TS__ArraySlice")
				expression = lualibCall(fn, fullTableExpr, lua.Num(fmt.Sprintf("%d", i)))
			}
		} else if isObject {
			propKey, propPrec := t.bindingElementPropertyKey(be)
			result = append(result, propPrec...)
			expression = lua.Index(fullTableExpr, propKey)
		} else {
			expression = lua.Index(fullTableExpr, lua.Num(fmt.Sprintf("%d", i+1)))
		}

		// Check if we need to split the declaration (hasMultipleReferences).
		// TSTL's createLocalOrExportedOrGlobalDeclaration prepends the local declaration
		// above all other statements, then returns only the assignment.
		needsSplit := false
		if useLocal && be.Name() != nil && t.checker != nil && t.inScope() {
			sym := t.checker.GetSymbolAtLocation(be.Name())
			if sym != nil {
				id := t.getOrCreateSymbolID(sym)
				t.markSymbolDeclared(id)
				scope := t.peekScope()
				if t.hasMultipleReferences(scope, id) {
					needsSplit = true
				}
			}
		}

		if needsSplit {
			// Prepend to hoisted (matching TSTL's prependPrecedingStatements which inserts at start)
			hoisted = append([]lua.Statement{lua.LocalDecl([]*lua.Identifier{lua.Ident(name)}, nil)}, hoisted...)
			result = append(result, lua.Assign([]lua.Expression{lua.Ident(name)}, []lua.Expression{expression}))
		} else {
			result = append(result, t.emitBindingDeclaration(name, expression, useLocal, isExported)...)
		}

		// Default value: if var == nil then var = default end
		if be.Initializer != nil {
			lhsExpr := t.bindingLHS(name, isExported)
			defaultVal, defPrec := t.transformExprInScope(be.Initializer)
			ifBody := make([]lua.Statement, 0, len(defPrec)+1)
			ifBody = append(ifBody, defPrec...)
			ifBody = append(ifBody, lua.Assign([]lua.Expression{lhsExpr}, []lua.Expression{defaultVal}))
			result = append(result, lua.If(
				lua.Binary(lhsExpr, lua.OpEq, lua.Nil()),
				&lua.Block{Statements: ifBody},
				nil,
			))
		}
	}
	// Prepend hoisted local declarations above all assignment statements
	if len(hoisted) > 0 {
		result = append(hoisted, result...)
	}
	return result
}

// bindingElementName extracts the string name from a binding element.
func (t *Transpiler) bindingElementName(be *ast.BindingElement) string {
	if be.Name() == nil {
		return "____"
	}
	if be.Name().Kind == ast.KindIdentifier {
		text := be.Name().AsIdentifier().Text
		if t.hasUnsafeIdentifierName(be.Name()) {
			return luaSafeName(text)
		}
		return text
	}
	return "____"
}

// emitBindingDeclaration emits either a local declaration or assignment for a binding.
func (t *Transpiler) emitBindingDeclaration(name string, expression lua.Expression, useLocal, isExported bool) []lua.Statement {
	if useLocal {
		return []lua.Statement{lua.LocalDecl([]*lua.Identifier{lua.Ident(name)}, []lua.Expression{expression})}
	}
	lhs := t.bindingLHS(name, isExported)
	return []lua.Statement{lua.Assign([]lua.Expression{lhs}, []lua.Expression{expression})}
}

// bindingLHS returns the LHS expression for a binding: ____exports.name when exported, bare name otherwise.
func (t *Transpiler) bindingLHS(name string, isExported bool) lua.Expression {
	if isExported && name != "_" && name != "____" {
		return lua.Index(lua.Ident("____exports"), lua.Str(name))
	}
	return lua.Ident(name)
}

// bindingElementPropertyKey returns the property key expression and any preceding statements
// for an object binding element. Computed property names may generate preceding statements
// (e.g., caching function refs for call expressions like [e(s += "D")]).
func (t *Transpiler) bindingElementPropertyKey(be *ast.BindingElement) (lua.Expression, []lua.Statement) {
	if be.PropertyName != nil {
		if be.PropertyName.Kind == ast.KindIdentifier {
			return lua.Str(be.PropertyName.AsIdentifier().Text), nil
		}
		if be.PropertyName.Kind == ast.KindStringLiteral {
			return lua.Str(be.PropertyName.AsStringLiteral().Text), nil
		}
		if be.PropertyName.Kind == ast.KindNumericLiteral {
			return lua.Num(be.PropertyName.AsNumericLiteral().Text), nil
		}
		return t.transformExprInScope(be.PropertyName)
	}
	if be.Name() != nil && be.Name().Kind == ast.KindIdentifier {
		return lua.Str(be.Name().AsIdentifier().Text), nil
	}
	return lua.Str("_"), nil
}

func (t *Transpiler) transformExpressionStatement(node *ast.Node) []lua.Statement {
	es := node.AsExpressionStatement()

	// `delete obj.prop` as statement: emit __TS__Delete(obj, "prop") directly
	if es.Expression.Kind == ast.KindDeleteExpression {
		luaExpr, prec := t.transformExprInScope(es.Expression)
		result := make([]lua.Statement, 0, len(prec)+1)
		result = append(result, prec...)
		result = append(result, lua.ExprStmt(luaExpr))
		return result
	}

	// `void expr` as statement: evaluate operand for side effects only, discard nil result.
	if es.Expression.Kind == ast.KindVoidExpression {
		ve := es.Expression.AsVoidExpression()
		operand := ve.Expression
		for operand.Kind == ast.KindParenthesizedExpression {
			operand = operand.AsParenthesizedExpression().Expression
		}
		if operand.Kind == ast.KindCallExpression {
			inner, prec := t.transformExprInScope(operand)
			result := make([]lua.Statement, 0, len(prec)+1)
			result = append(result, prec...)
			// Skip emitting the result if it's nil (e.g. optional chain that already
			// emitted its side effects as preceding statements).
			if _, isNil := inner.(*lua.NilLiteral); !isNil {
				result = append(result, lua.ExprStmt(inner))
			}
			return result
		}
		return nil
	}

	// Postfix/prefix i++/i--/++i/--i as statement: emit simple x = x + 1
	if operandNode, op, ok := asIncrementDecrement(es.Expression); ok {
		return t.transformIncrementDecrementStmt(operandNode, op)
	}

	// Assignments as statement: emit directly, avoiding the preceding-statement path
	if es.Expression.Kind == ast.KindBinaryExpression {
		be := es.Expression.AsBinaryExpression()
		op := be.OperatorToken.Kind
		if op == ast.KindEqualsToken || isCompoundAssignment(op) {
			if op == ast.KindAmpersandAmpersandEqualsToken || op == ast.KindBarBarEqualsToken ||
				op == ast.KindQuestionQuestionEqualsToken {
				// Logical assignments emit via addPrecedingStatements; discard the result
				_, prec := t.transformExprInScope(es.Expression)
				return prec
			}
			return t.transformAsStatement(es.Expression)
		}
	}

	luaExpr, prec := t.transformExprInScope(es.Expression)

	// Optional chaining used as statement: emit as `if guard then body end`
	if bin, ok := luaExpr.(*lua.BinaryExpression); ok && bin.Operator == lua.OpAnd {
		result := make([]lua.Statement, 0, len(prec)+1)
		result = append(result, prec...)
		result = append(result, lua.If(
			bin.Left,
			&lua.Block{Statements: []lua.Statement{lua.ExprStmt(bin.Right)}},
			nil,
		))
		return result
	}

	result := make([]lua.Statement, 0, len(prec)+1)
	result = append(result, prec...)

	// Only function calls are valid Lua expression statements.
	// Synthetic expressions (literals, binary ops from extensions) are dropped.
	// Other non-call expressions are wrapped in `local ____ = expr`.
	if isLuaCallExpression(luaExpr) {
		result = append(result, lua.ExprStmt(luaExpr))
		return result
	}
	if isLuaLiteral(luaExpr) {
		return result
	}
	// Non-call, non-literal: wrap to make legal Lua
	result = append(result, lua.LocalDecl(
		[]*lua.Identifier{lua.Ident("____")},
		[]lua.Expression{luaExpr},
	))
	return result
}

// isLuaLiteral returns true if the Lua expression is a literal value (nil, bool, string, number).
func isLuaLiteral(expr lua.Expression) bool {
	switch expr.(type) {
	case *lua.NilLiteral, *lua.BooleanLiteral, *lua.StringLiteral, *lua.NumericLiteral:
		return true
	}
	return false
}

// isLuaCallExpression returns true if the Lua expression is a function/method call.
func isLuaCallExpression(expr lua.Expression) bool {
	switch expr.(type) {
	case *lua.CallExpression, *lua.MethodCallExpression:
		return true
	}
	return false
}

func (t *Transpiler) transformReturnStatement(node *ast.Node) []lua.Statement {
	rs := node.AsReturnStatement()

	// Inside async functions, return becomes: return ____awaiter_resolve(nil, value)
	if t.asyncDepth > 0 {
		if rs.Expression != nil {
			expr, prec := t.transformExprInScope(rs.Expression)
			result := make([]lua.Statement, 0, len(prec)+1)
			result = append(result, prec...)
			result = append(result, lua.Return(lua.Call(lua.Ident("____awaiter_resolve"), lua.Nil(), expr)))
			return result
		}
		return []lua.Statement{lua.Return(lua.Call(lua.Ident("____awaiter_resolve"), lua.Nil()))}
	}

	// Inside try blocks, return becomes: return true, value
	// This signals to pcall caller that the try block returned a value
	// Multi-return values (both $multi and LuaMultiReturn calls) are wrapped in a table
	// to preserve all values through the pcall mechanism.
	if t.tryDepth > 0 {
		if rs.Expression != nil {
			t.pushPrecedingStatements()
			exprs := t.transformExpressionsInReturn(rs.Expression)
			prec := t.popPrecedingStatements()
			result := make([]lua.Statement, 0, len(prec)+1)
			result = append(result, prec...)
			if len(exprs) > 1 {
				// $multi return in try: wrap in table
				var fields []*lua.TableFieldExpression
				for _, e := range exprs {
					fields = append(fields, lua.Field(e))
				}
				result = append(result, lua.Return(lua.Bool(true), lua.Table(fields...)))
			} else {
				retExpr := exprs[0]
				inner := skipOuterExpressionsDown(rs.Expression)
				// Wrap LuaMultiReturn calls in a table to preserve multi-values through pcall.
				// In the try block's pcall function, shouldMultiReturnCallBeWrapped handles this.
				// But in the catch block, the TS-level parent function is the enclosing function
				// (not ____catch), so shouldMultiReturnCallBeWrapped may skip wrapping.
				if inner.Kind == ast.KindCallExpression && t.returnsMultiType(inner) &&
					!t.shouldMultiReturnCallBeWrapped(inner) {
					retExpr = lua.Table(lua.Field(retExpr))
				}
				result = append(result, lua.Return(lua.Bool(true), retExpr))
			}
			return result
		}
		return []lua.Statement{lua.Return(lua.Bool(true))}
	}

	// Validate return value function context compatibility
	if rs.Expression != nil && t.checker != nil {
		exprType := t.checker.GetTypeAtLocation(rs.Expression)
		returnType := checker.Checker_getContextualType(t.checker, rs.Expression, 0)
		if exprType != nil && returnType != nil {
			t.validateAssignment(node, exprType, returnType, "")
		}
	}

	if rs.Expression != nil {
		// $multi support: return $multi(a, b) → return a, b
		t.pushPrecedingStatements()
		exprs := t.transformExpressionsInReturn(rs.Expression)
		prec := t.popPrecedingStatements()
		result := make([]lua.Statement, 0, len(prec)+1)
		result = append(result, prec...)
		result = append(result, lua.Return(exprs...))
		return result
	}
	return []lua.Statement{lua.Return()}
}

func (t *Transpiler) transformIfStatement(node *ast.Node) []lua.Statement {
	prec, ifStmt := t.buildIfStatement(node)
	result := make([]lua.Statement, 0, len(prec)+1)
	result = append(result, prec...)
	result = append(result, ifStmt)
	return result
}

func (t *Transpiler) buildIfStatement(node *ast.Node) ([]lua.Statement, *lua.IfStatement) {
	is := node.AsIfStatement()
	t.checkOnlyTruthyCondition(is.Expression)
	cond, precCond := t.transformExprInScope(is.Expression)

	thenStmts := t.transformBlockOrStatement(is.ThenStatement)

	var elseBlock interface{}
	if is.ElseStatement != nil {
		if is.ElseStatement.Kind == ast.KindIfStatement {
			elsePrec, elseIfStmt := t.buildIfStatement(is.ElseStatement)
			if len(elsePrec) > 0 {
				// Can't use elseif — convert to else block with preceding stmts + if
				elseStmts := make([]lua.Statement, 0, len(elsePrec)+1)
				elseStmts = append(elseStmts, elsePrec...)
				elseStmts = append(elseStmts, elseIfStmt)
				elseBlock = &lua.Block{Statements: elseStmts}
			} else {
				elseBlock = elseIfStmt
			}
		} else {
			elseStmts := t.transformBlockOrStatement(is.ElseStatement)
			elseBlock = &lua.Block{Statements: elseStmts}
		}
	}

	return precCond, &lua.IfStatement{
		Condition: cond,
		IfBlock:   &lua.Block{Statements: thenStmts},
		ElseBlock: elseBlock,
	}
}

// transformBlockOrStatement returns the inner statements of a block (without do...end wrapping)
// or dispatches a non-block statement.
func (t *Transpiler) transformBlockOrStatement(node *ast.Node) []lua.Statement {
	if node.Kind == ast.KindBlock {
		return t.transformBlock(node)
	}
	return t.transformStatement(node)
}

// transformBlockStatementsNoScope extracts and transforms block statements without pushing a scope.
// Used when the caller has already pushed a scope (e.g., Loop scope in transformLoopBody).
// Matches TSTL's transformBlockOrStatement which does not push its own scope.
func (t *Transpiler) transformBlockStatementsNoScope(node *ast.Node) []lua.Statement {
	if node.Kind == ast.KindBlock {
		block := node.AsBlock()
		if block.Statements == nil {
			return nil
		}
		var stmts []lua.Statement
		for _, stmt := range block.Statements.Nodes {
			stmts = append(stmts, t.transformStatement(stmt)...)
		}
		return stmts
	}
	return t.transformStatement(node)
}

func (t *Transpiler) transformBlock(node *ast.Node) []lua.Statement {
	return t.transformScopeBlock(node, ScopeBlock)
}

// transformBlockStatementsOnly transforms a block's statements without pushing a new scope.
// Used by function handlers where the function scope already covers the body.
func (t *Transpiler) transformBlockStatementsOnly(node *ast.Node) []lua.Statement {
	block := node.AsBlock()
	if block.Statements == nil {
		return nil
	}

	// Check for `using` declarations and use the using-aware transform.
	// Function bodies should return from using calls.
	if blockHasUsingDeclaration(block.Statements) {
		_, stmts := t.transformStatementsWithUsing(block.Statements.Nodes, true)
		return stmts
	}

	var stmts []lua.Statement
	for _, stmt := range block.Statements.Nodes {
		stmts = append(stmts, t.transformStatement(stmt)...)
	}
	return stmts
}

// transformScopeBlock transforms a block with a specific scope type.
func (t *Transpiler) transformScopeBlock(node *ast.Node, scopeType ScopeType) []lua.Statement {
	block := node.AsBlock()
	scope := t.pushScope(scopeType, node)
	var stmts []lua.Statement

	// Check if any statement is a `using` declaration and use the using-aware transform.
	// Standalone blocks (do...end) should not return from using calls.
	if blockHasUsingDeclaration(block.Statements) {
		_, stmts = t.transformStatementsWithUsing(block.Statements.Nodes, false)
	} else {
		for _, stmt := range block.Statements.Nodes {
			stmts = append(stmts, t.transformStatement(stmt)...)
		}
	}

	stmts = t.performHoisting(scope, stmts)
	t.popScope()
	return stmts
}

// blockHasUsingDeclaration checks if a statement list contains any `using` declarations.
func blockHasUsingDeclaration(stmts *ast.NodeList) bool {
	if stmts == nil {
		return false
	}
	for _, stmt := range stmts.Nodes {
		if isUsingDeclaration(stmt) {
			return true
		}
	}
	return false
}

func containsBreakOrReturn(nodes []*ast.Node) bool {
	for _, n := range nodes {
		if n.Kind == ast.KindBreakStatement || n.Kind == ast.KindReturnStatement {
			return true
		}
		if n.Kind == ast.KindBlock {
			block := n.AsBlock()
			if block.Statements != nil && containsBreakOrReturn(block.Statements.Nodes) {
				return true
			}
		}
	}
	return false
}

// containsReturnInBlock recursively checks if a block contains a return statement.
func containsReturnInBlock(block *ast.Node) bool {
	b := block.AsBlock()
	if b.Statements == nil {
		return false
	}
	return containsReturnInNodes(b.Statements.Nodes)
}

func containsReturnInNodes(nodes []*ast.Node) bool {
	for _, n := range nodes {
		if n.Kind == ast.KindReturnStatement {
			return true
		}
		if n.Kind == ast.KindBlock {
			if containsReturnInBlock(n) {
				return true
			}
		}
		if n.Kind == ast.KindIfStatement {
			is := n.AsIfStatement()
			if is.ThenStatement != nil && containsReturnInNodes([]*ast.Node{is.ThenStatement}) {
				return true
			}
			if is.ElseStatement != nil && containsReturnInNodes([]*ast.Node{is.ElseStatement}) {
				return true
			}
		}
		if n.Kind == ast.KindTryStatement {
			ts := n.AsTryStatement()
			if containsReturnInBlock(ts.TryBlock) {
				return true
			}
			if ts.CatchClause != nil {
				if containsReturnInBlock(ts.CatchClause.AsCatchClause().Block) {
					return true
				}
			}
		}
	}
	return false
}

func (t *Transpiler) transformSwitchStatement(node *ast.Node) []lua.Statement {
	ss := node.AsSwitchStatement()

	// Push switch scope for hoisting
	scope := t.pushScope(ScopeSwitch, node)
	switchTemp := fmt.Sprintf("____switch%d", scope.ID)
	condTemp := fmt.Sprintf("____cond%d", scope.ID)

	cb := ss.CaseBlock.AsCaseBlock()
	if cb.Clauses == nil {
		t.popScope()
		return nil
	}
	clauses := cb.Clauses.Nodes

	// Collect all hoisted identifiers and statements from case clauses
	var allHoistedIdents []*lua.Identifier
	var allHoistedStmts []lua.Statement

	// transformClauseStmts transforms a case clause's statements and extracts hoisted items
	transformClauseStmts := func(cc *ast.CaseOrDefaultClause) []lua.Statement {
		var clauseStmts []lua.Statement
		if cc.Statements != nil {
			for _, stmt := range cc.Statements.Nodes {
				clauseStmts = append(clauseStmts, t.transformStatement(stmt)...)
			}
		}
		// Extract hoisted items from this clause's statements
		hoisted, idents, remaining := t.separateHoistedStatements(scope, clauseStmts)
		allHoistedStmts = append(allHoistedStmts, hoisted...)
		allHoistedIdents = append(allHoistedIdents, idents...)
		return remaining
	}

	// If the switch only has a default clause, wrap in do block
	if len(clauses) == 1 && clauses[0].Kind == ast.KindDefaultClause {
		cc := clauses[0].AsCaseOrDefaultClause()
		defaultStmts := transformClauseStmts(cc)
		t.popScope()

		switchExpr, precSwitch := t.transformExprInScope(ss.Expression)
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(switchTemp)},
			[]lua.Expression{switchExpr},
		))
		if len(allHoistedIdents) > 0 {
			bodyStmts = append(bodyStmts, lua.LocalDecl(allHoistedIdents, nil))
		}
		bodyStmts = append(bodyStmts, allHoistedStmts...)
		bodyStmts = append(bodyStmts, lua.Do(defaultStmts...))
		result := make([]lua.Statement, 0, len(precSwitch)+1)
		result = append(result, precSwitch...)
		result = append(result, lua.Repeat(lua.Bool(true), &lua.Block{Statements: bodyStmts}))
		return result
	}

	var statements []lua.Statement
	isInitialCondition := true
	defaultTransformed := false
	var condition lua.Expression

	for i, clause := range clauses {
		cc := clause.AsCaseOrDefaultClause()

		// Skip default clauses — they'll be handled in the fallback section below
		if clause.Kind == ast.KindDefaultClause {
			// If default is at position 0, skip
			if i == 0 {
				continue
			}
			// If previous clause has break/return, skip default (it's unreachable via fallthrough)
			prevCC := clauses[i-1].AsCaseOrDefaultClause()
			if prevCC.Statements != nil && containsBreakOrReturn(prevCC.Statements.Nodes) {
				continue
			}
			// Allow fallthrough to final default clause at the end
			if i == len(clauses)-1 {
				// Emit condition initialization with accumulated condition
				if isInitialCondition && condition != nil {
					statements = append(statements, lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(condTemp)},
						[]lua.Expression{condition},
					))
					isInitialCondition = false
				} else if !isInitialCondition && condition != nil {
					statements = append(statements, lua.Assign(
						[]lua.Expression{lua.Ident(condTemp)},
						[]lua.Expression{lua.Binary(lua.Ident(condTemp), lua.OpOr, condition)},
					))
				}
				condition = nil
				continue
			}
			// Default in the middle that can be fallen-through-to: need to handle condition init
			if isInitialCondition {
				statements = append(statements, lua.LocalDecl(
					[]*lua.Identifier{lua.Ident(condTemp)},
					[]lua.Expression{lua.Bool(false)},
				))
				isInitialCondition = false
			}
			// Default in the middle with fallthrough — transform its body inline
			goto transformClauseBody
		}

		// Build condition
		{
			caseExpr, precCase := t.transformExprInScope(cc.Expression)

			newCond := lua.Binary(lua.Ident(switchTemp), lua.OpEq, caseExpr)
			if len(precCase) > 0 && !isInitialCondition {
				// Case expression has side effects and a prior case condition has been emitted.
				// Guard the side effects so they only run if no prior case matched.
				// createShortCircuitBinaryExpressionPrecedingStatements)
				guardTemp := t.nextTemp("cond")
				lhsExpr := lua.Expression(lua.Ident(condTemp))
				if condition != nil {
					lhsExpr = lua.Binary(lua.Ident(condTemp), lua.OpOr, condition)
				}
				statements = append(statements, lua.LocalDecl(
					[]*lua.Identifier{lua.Ident(guardTemp)},
					[]lua.Expression{lhsExpr},
				))
				ifBody := append(precCase, lua.Assign(
					[]lua.Expression{lua.Ident(guardTemp)},
					[]lua.Expression{newCond},
				))
				statements = append(statements, lua.If(
					lua.Unary(lua.OpNot, lua.Ident(guardTemp)),
					&lua.Block{Statements: ifBody},
					nil,
				))
				condition = lua.Ident(guardTemp)
			} else if condition != nil {
				condition = lua.Binary(condition, lua.OpOr, newCond)
			} else {
				statements = append(statements, precCase...)
				condition = newCond
			}

			// Skip empty clauses (fallthrough to next) unless it's the last
			if i != len(clauses)-1 && (cc.Statements == nil || len(cc.Statements.Nodes) == 0) {
				continue
			}

			// Emit condition
			if isInitialCondition {
				statements = append(statements, lua.LocalDecl(
					[]*lua.Identifier{lua.Ident(condTemp)},
					[]lua.Expression{condition},
				))
			} else {
				statements = append(statements, lua.Assign(
					[]lua.Expression{lua.Ident(condTemp)},
					[]lua.Expression{lua.Binary(lua.Ident(condTemp), lua.OpOr, condition)},
				))
			}
			isInitialCondition = false
		}

	transformClauseBody:
		// Transform clause body with hoisting extraction
		clauseStmts := transformClauseStmts(cc)
		if i == len(clauses)-1 && (cc.Statements == nil || !containsBreakOrReturn(cc.Statements.Nodes)) {
			clauseStmts = append(clauseStmts, lua.Break())
		}

		// Remember that we transformed default clause so we don't duplicate hoisted statements later
		if clause.Kind == ast.KindDefaultClause {
			defaultTransformed = true
		}

		statements = append(statements, lua.If(
			lua.Ident(condTemp),
			&lua.Block{Statements: clauseStmts},
			nil,
		))
		condition = nil
	}

	// Default clause fallback: find the default and emit its body + fallthrough
	defaultIdx := -1
	for i, clause := range clauses {
		if clause.Kind == ast.KindDefaultClause {
			defaultIdx = i
			break
		}
	}
	if defaultIdx >= 0 {
		// Find end of fallthrough chain from default
		endIdx := -1
		for i := defaultIdx; i < len(clauses); i++ {
			cc := clauses[i].AsCaseOrDefaultClause()
			if cc.Statements != nil && containsBreakOrReturn(cc.Statements.Nodes) {
				endIdx = i + 1
				break
			}
		}

		// Transform default clause statements
		var defaultStmts []lua.Statement
		defaultCC := clauses[defaultIdx].AsCaseOrDefaultClause()
		if defaultCC.Statements != nil {
			for _, stmt := range defaultCC.Statements.Nodes {
				defaultStmts = append(defaultStmts, t.transformStatement(stmt)...)
			}
		}
		// Only hoist from default if it wasn't already transformed in the main loop
		hoisted, idents, remaining := t.separateHoistedStatements(scope, defaultStmts)
		if !defaultTransformed {
			allHoistedStmts = append(allHoistedStmts, hoisted...)
			allHoistedIdents = append(allHoistedIdents, idents...)
		}
		defaultStmts = remaining

		// Combine fallthrough statements after default, discarding hoisted items
		// (they were already collected when clauses were initially transformed above)
		end := endIdx
		if end < 0 {
			end = len(clauses)
		}
		for i := defaultIdx + 1; i < end; i++ {
			cc := clauses[i].AsCaseOrDefaultClause()
			if cc.Statements != nil {
				var ftStmts []lua.Statement
				for _, stmt := range cc.Statements.Nodes {
					ftStmts = append(ftStmts, t.transformStatement(stmt)...)
				}
				// Drop hoisted statements — they were already added during initial clause transformation
				_, _, ftRemaining := t.separateHoistedStatements(scope, ftStmts)
				defaultStmts = append(defaultStmts, ftRemaining...)
			}
		}

		if len(defaultStmts) > 0 {
			statements = append(statements, lua.Do(defaultStmts...))
		}
	}

	t.popScope()

	// Build final output
	switchExpr, precSwitch := t.transformExprInScope(ss.Expression)
	var bodyStmts []lua.Statement
	bodyStmts = append(bodyStmts, lua.LocalDecl(
		[]*lua.Identifier{lua.Ident(switchTemp)},
		[]lua.Expression{switchExpr},
	))
	// Insert hoisted declarations at switch top
	if len(allHoistedIdents) > 0 {
		bodyStmts = append(bodyStmts, lua.LocalDecl(allHoistedIdents, nil))
	}
	bodyStmts = append(bodyStmts, allHoistedStmts...)
	bodyStmts = append(bodyStmts, statements...)

	result := make([]lua.Statement, 0, len(precSwitch)+1)
	result = append(result, precSwitch...)
	result = append(result, lua.Repeat(lua.Bool(true), &lua.Block{Statements: bodyStmts}))
	return result
}

func (t *Transpiler) transformTryStatement(node *ast.Node) []lua.Statement {
	ts := node.AsTryStatement()

	// Inside async functions, use promise-based try/catch instead of pcall
	if t.asyncDepth > 0 {
		return t.transformAsyncTry(ts)
	}

	// coroutine.yield cannot be called inside pcall on Lua 5.0/5.1/universal
	if t.generatorDepth > 0 && (t.luaTarget == LuaTargetLua50 || t.luaTarget == LuaTargetLua51 || t.luaTarget == LuaTargetUniversal) {
		t.addError(node, dw.UnsupportedForTarget, fmt.Sprintf("try/catch inside generator functions is/are not supported for target %s.", t.luaTarget.DisplayName()))
		return t.transformBlock(ts.TryBlock)
	}

	// Track that we're inside a try block (return → return true, value)
	t.tryDepth++
	tryBlockStmts := t.transformBlock(ts.TryBlock)
	tryHasReturn := containsReturnInBlock(ts.TryBlock)
	t.tryDepth--

	tryFn := &lua.FunctionExpression{
		Body: &lua.Block{Statements: tryBlockStmts},
	}

	// Structure matches TSTL: branches set up pcall + returnCondition,
	// then shared post-chain code handles finally, re-throw, and return.
	var result []lua.Statement
	var returnCondition lua.Expression

	catchHasStatements := ts.CatchClause != nil && ts.CatchClause.AsCatchClause().Block.AsBlock().Statements != nil && len(ts.CatchClause.AsCatchClause().Block.AsBlock().Statements.Nodes) > 0
	if catchHasStatements {
		// try with catch (non-empty catch block)
		cc := ts.CatchClause.AsCatchClause()

		hasCatchVar := cc.VariableDeclaration != nil
		catchVar := ""
		if hasCatchVar {
			catchVar = "____err"
			vd := cc.VariableDeclaration.AsVariableDeclaration()
			if vd.Name().Kind == ast.KindIdentifier {
				catchVar = vd.Name().AsIdentifier().Text
				if t.hasUnsafeIdentifierName(vd.Name()) {
					catchVar = luaSafeName(catchVar)
				}
			}
		}

		t.tryDepth++
		catchBlockStmts := t.transformBlock(cc.Block)
		catchHasReturn := containsReturnInBlock(cc.Block)
		t.tryDepth--

		var catchParams []*lua.Identifier
		if hasCatchVar {
			catchParams = []*lua.Identifier{lua.Ident(catchVar)}
		}
		catchFn := &lua.FunctionExpression{
			Params: catchParams,
			Body:   &lua.Block{Statements: catchBlockStmts},
			Flags:  lua.FlagDeclaration,
		}

		hasReturn := tryHasReturn || catchHasReturn

		result = append(result, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident("____catch")},
			[]lua.Expression{catchFn},
		))

		// pcall returns: (ok, errorOrReturnFlag, ...).
		// Only capture ____hasReturned when catch has a variable binding or either block returns.
		needsErrorCapture := hasCatchVar || hasReturn
		declVars := []*lua.Identifier{lua.Ident("____try")}
		if needsErrorCapture {
			declVars = append(declVars, lua.Ident("____hasReturned"))
		}
		if hasReturn {
			declVars = append(declVars, lua.Ident("____returnValue"))
			returnCondition = lua.Ident("____hasReturned")
		}
		result = append(result, lua.LocalDecl(declVars, []lua.Expression{lua.Call(lua.Ident("pcall"), tryFn)}))

		var catchCallStmt lua.Statement
		if hasReturn {
			var catchArg []lua.Expression
			if hasCatchVar {
				catchArg = []lua.Expression{lua.Ident("____hasReturned")}
			}
			catchCallStmt = lua.Assign(
				[]lua.Expression{lua.Ident("____hasReturned"), lua.Ident("____returnValue")},
				[]lua.Expression{lua.Call(lua.Ident("____catch"), catchArg...)},
			)
		} else if hasCatchVar {
			catchCallStmt = lua.ExprStmt(lua.Call(lua.Ident("____catch"), lua.Ident("____hasReturned")))
		} else {
			catchCallStmt = lua.ExprStmt(lua.Call(lua.Ident("____catch")))
		}
		result = append(result, lua.If(
			lua.Unary(lua.OpNot, lua.Ident("____try")),
			&lua.Block{Statements: []lua.Statement{catchCallStmt}},
			nil,
		))
	} else if tryHasReturn {
		// try with return, but no catch
		result = append(result, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident("____try"), lua.Ident("____hasReturned"), lua.Ident("____returnValue")},
			[]lua.Expression{lua.Call(lua.Ident("pcall"), tryFn)},
		))
		returnCondition = lua.Binary(lua.Ident("____try"), lua.OpAnd, lua.Ident("____hasReturned"))
	} else if ts.FinallyBlock != nil {
		// try without catch, but with finally — capture error for re-throw
		result = append(result, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident("____try"), lua.Ident("____error")},
			[]lua.Expression{lua.Call(lua.Ident("pcall"), tryFn)},
		))
	} else {
		// try without return or catch
		result = append(result, lua.ExprStmt(lua.Call(lua.Ident("pcall"), tryFn)))
	}

	// Finally block
	if ts.FinallyBlock != nil {
		result = append(result, t.transformBlock(ts.FinallyBlock)...)
	}

	// Re-throw error if try had no catch but had a finally
	if ts.CatchClause == nil && ts.FinallyBlock != nil {
		// When tryHasReturn, pcall returns (false, error) — error is in ____hasReturned.
		// Otherwise, pcall returns (false, error) — error is in ____error.
		errorVar := "____error"
		if tryHasReturn {
			errorVar = "____hasReturned"
		}
		result = append(result, lua.If(
			lua.Unary(lua.OpNot, lua.Ident("____try")),
			&lua.Block{Statements: []lua.Statement{
				lua.ExprStmt(lua.Call(lua.Ident("error"), lua.Ident(errorVar), lua.Num("0"))),
			}},
			nil,
		))
	}

	// Return propagation
	if returnCondition != nil {
		var returnValueExpr lua.Expression = lua.Ident("____returnValue")
		if t.isInMultiReturnFunction(node) {
			returnValueExpr = lua.Call(t.unpackIdent(), returnValueExpr)
		}
		var returnStmt lua.Statement
		if t.tryDepth > 0 {
			returnStmt = lua.Return(lua.Bool(true), returnValueExpr)
		} else {
			returnStmt = lua.Return(returnValueExpr)
		}
		result = append(result, lua.If(
			returnCondition,
			&lua.Block{Statements: []lua.Statement{returnStmt}},
			nil,
		))
	}

	return []lua.Statement{lua.Do(result...)}
}

// transformAsyncTry handles try/catch/finally inside async functions using promise-based error handling.
// Instead of pcall, wraps the try block in __TS__AsyncAwaiter and uses .catch()/.finally() on the promise.
func (t *Transpiler) transformAsyncTry(ts *ast.TryStatement) []lua.Statement {
	// try/catch inside async functions is not supported for Lua 5.0/5.1/universal
	if t.luaTarget == LuaTargetLua50 || t.luaTarget == LuaTargetLua51 || t.luaTarget == LuaTargetUniversal {
		t.addError(ts.AsNode(), dw.UnsupportedForTargetButOverrideAvailable, fmt.Sprintf("try/catch inside async functions is/are not supported for target %s.", t.luaTarget.DisplayName()))
		return t.transformBlock(ts.TryBlock)
	}

	// Transform try block body (no tryDepth increment — not using pcall)
	tryBlockStmts := t.transformBlock(ts.TryBlock)

	// local ____try = __TS__AsyncAwaiter(function() <try body> end)
	asyncAwaiterFn := t.requireLualib("__TS__AsyncAwaiter")
	tryFn := &lua.FunctionExpression{
		Body: &lua.Block{Statements: tryBlockStmts},
	}
	awaiterCall := lua.Call(lua.Ident(asyncAwaiterFn), tryFn)
	tryIdent := lua.Ident("____try")

	var result []lua.Statement
	result = append(result, lua.LocalDecl([]*lua.Identifier{tryIdent}, []lua.Expression{awaiterCall}))

	// Chain catch before finally (order matters for Promise semantics).
	// Each handler that contains await must be wrapped in __TS__AsyncAwaiter
	// so that coroutine.yield works inside it.
	// Fix for: https://github.com/TypeScriptToLua/TypeScriptToLua/issues/1659

	if ts.CatchClause != nil {
		cc := ts.CatchClause.AsCatchClause()

		catchVar := ""
		if cc.VariableDeclaration != nil {
			vd := cc.VariableDeclaration.AsVariableDeclaration()
			if vd.Name().Kind == ast.KindIdentifier {
				catchVar = vd.Name().AsIdentifier().Text
				if t.hasUnsafeIdentifierName(vd.Name()) {
					catchVar = luaSafeName(catchVar)
				}
			}
		}

		catchBlockStmts := t.transformBlock(cc.Block)

		// Wrap catch body in __TS__AsyncAwaiter so await works inside it
		var catchBody []lua.Statement
		catchBody = append(catchBody, lua.Return(lua.Call(lua.Ident(asyncAwaiterFn), &lua.FunctionExpression{
			Body: &lua.Block{Statements: catchBlockStmts},
		})))

		var catchParams []*lua.Identifier
		catchParams = append(catchParams, lua.Ident("____")) // self placeholder
		if catchVar != "" {
			catchParams = append(catchParams, lua.Ident(catchVar))
		}
		catchFn := &lua.FunctionExpression{
			Params: catchParams,
			Body:   &lua.Block{Statements: catchBody},
		}

		// ____try = ____try.catch(____try, function(____, err) return __TS__AsyncAwaiter(function() ... end) end)
		catchMethod := lua.Index(lua.Ident("____try"), lua.Str("catch"))
		catchCall := lua.Call(catchMethod, lua.Ident("____try"), catchFn)
		result = append(result, lua.Assign(
			[]lua.Expression{lua.Ident("____try")},
			[]lua.Expression{catchCall},
		))
	}

	if ts.FinallyBlock != nil {
		finallyStmts := t.transformBlock(ts.FinallyBlock)

		// Wrap finally body in __TS__AsyncAwaiter so await works inside it
		var finallyBody []lua.Statement
		finallyBody = append(finallyBody, lua.Return(lua.Call(lua.Ident(asyncAwaiterFn), &lua.FunctionExpression{
			Body: &lua.Block{Statements: finallyStmts},
		})))

		finallyFn := &lua.FunctionExpression{
			Body: &lua.Block{Statements: finallyBody},
		}

		// ____try = ____try.finally(____try, function() return __TS__AsyncAwaiter(function() ... end) end)
		finallyMethod := lua.Index(lua.Ident("____try"), lua.Str("finally"))
		finallyCall := lua.Call(finallyMethod, lua.Ident("____try"), finallyFn)
		result = append(result, lua.Assign(
			[]lua.Expression{lua.Ident("____try")},
			[]lua.Expression{finallyCall},
		))
	}

	// __TS__Await(____try)
	awaitFn := t.requireLualib("__TS__Await")
	awaitCall := lua.Call(lua.Ident(awaitFn), lua.Ident("____try"))
	result = append(result, lua.ExprStmt(awaitCall))

	return result
}

// throw expr → error(expr, 0)
func (t *Transpiler) transformThrowStatement(node *ast.Node) []lua.Statement {
	ts := node.AsThrowStatement()
	expr, prec := t.transformExprInScope(ts.Expression)
	result := make([]lua.Statement, 0, len(prec)+1)
	result = append(result, prec...)
	result = append(result, lua.ExprStmt(lua.Call(lua.Ident("error"), expr, lua.Num("0"))))
	return result
}

// delete obj.prop → __TS__Delete(obj, "prop")
// delete obj[expr] → __TS__Delete(obj, expr)
func (t *Transpiler) transformDeleteExpression(node *ast.Node) lua.Expression {
	de := node.AsDeleteExpression()
	expr := de.Expression

	// Optional chain: delete table?.bar → if table ~= nil then __TS__Delete(table, "bar") end; true
	if ast.IsOptionalChain(expr) {
		var ownerNode *ast.Node
		var propExpr lua.Expression
		switch expr.Kind {
		case ast.KindPropertyAccessExpression:
			pa := expr.AsPropertyAccessExpression()
			ownerNode = pa.Expression
			propExpr = lua.Str(pa.Name().AsIdentifier().Text)
		case ast.KindElementAccessExpression:
			ea := expr.AsElementAccessExpression()
			ownerNode = ea.Expression
			propExpr = t.transformElementAccessIndex(ea)
		}
		if ownerNode != nil && propExpr != nil {
			ownerExpr := t.transformExpression(ownerNode)
			ownerTemp := t.moveToPrecedingTemp(ownerExpr)
			fn := t.requireLualib("__TS__Delete")
			t.addPrecedingStatements(lua.If(
				lua.Binary(ownerTemp, lua.OpNeq, lua.Nil()),
				&lua.Block{Statements: []lua.Statement{
					lua.ExprStmt(lualibCall(fn, ownerTemp, propExpr)),
				}},
				nil,
			))
			return lua.Bool(true)
		}
	}

	var ownerExpr lua.Expression
	var propExpr lua.Expression

	switch expr.Kind {
	case ast.KindPropertyAccessExpression:
		pa := expr.AsPropertyAccessExpression()
		ownerExpr = t.transformExpression(pa.Expression)
		propExpr = lua.Str(pa.Name().AsIdentifier().Text)
	case ast.KindElementAccessExpression:
		ea := expr.AsElementAccessExpression()
		ownerExpr = t.transformExpression(ea.Expression)
		propExpr = t.transformElementAccessIndex(ea)
	}

	if ownerExpr == nil || propExpr == nil {
		// Unsupported delete target — fall back to setting nil
		target := t.transformExpression(expr)
		t.addPrecedingStatements(lua.Assign([]lua.Expression{target}, []lua.Expression{lua.Nil()}))
		return lua.Nil()
	}

	fn := t.requireLualib("__TS__Delete")
	return lualibCall(fn, ownerExpr, propExpr)
}

// asIncrementDecrement checks if a node is prefix/postfix ++/-- and returns the operand and operator.
func asIncrementDecrement(node *ast.Node) (operand *ast.Node, op ast.Kind, ok bool) {
	if node.Kind == ast.KindPostfixUnaryExpression {
		pu := node.AsPostfixUnaryExpression()
		if pu.Operator == ast.KindPlusPlusToken || pu.Operator == ast.KindMinusMinusToken {
			return pu.Operand, pu.Operator, true
		}
	}
	if node.Kind == ast.KindPrefixUnaryExpression {
		pu := node.AsPrefixUnaryExpression()
		if pu.Operator == ast.KindPlusPlusToken || pu.Operator == ast.KindMinusMinusToken {
			return pu.Operand, pu.Operator, true
		}
	}
	return nil, 0, false
}

// transformAsStatement transforms an expression node as a statement, handling postfix/prefix ops
// and compound assignments cleanly.
// transformIncrementDecrementStmt emits `x = x +/- 1` for postfix/prefix ++/--.
// transformIncrementDecrementStmt emits `x = x +/- 1` for postfix/prefix ++/--.
// For property/element access, caches obj/index to avoid double evaluation.
func (t *Transpiler) transformIncrementDecrementStmt(operandNode *ast.Node, op ast.Kind) []lua.Statement {
	operand, prec := t.transformExprInScope(operandNode)
	var binOp lua.Operator
	if op == ast.KindPlusPlusToken {
		binOp = lua.OpAdd
	} else {
		binOp = lua.OpSub
	}

	result := make([]lua.Statement, 0, len(prec)+4)
	result = append(result, prec...)

	// Cache obj/index for property access to avoid re-evaluating side effects.
	// Only cache when table or index have actual side effects (calls, etc.), not for simple identifiers.
	if idx, ok := operand.(*lua.TableIndexExpression); ok {
		if luaExprHasSideEffect(idx.Table) || luaExprHasSideEffect(idx.Index) {
			objTemp := t.nextTempForLuaExpression(idx.Table)
			idxTemp := t.nextTempForLuaExpression(idx.Index)
			result = append(result, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(objTemp), lua.Ident(idxTemp)},
				[]lua.Expression{idx.Table, idx.Index},
			))
			cached := lua.Index(lua.Ident(objTemp), lua.Ident(idxTemp))
			result = append(result, lua.Assign([]lua.Expression{cached}, []lua.Expression{lua.Binary(cached, binOp, lua.Num("1"))}))
			result = append(result, t.emitExportSync(operandNode)...)
			return result
		}
	}

	newValue := lua.Binary(operand, binOp, lua.Num("1"))
	result = append(result, lua.Assign([]lua.Expression{operand}, []lua.Expression{newValue}))
	result = append(result, t.emitExportSync(operandNode)...)
	return result
}

func (t *Transpiler) transformAsStatement(node *ast.Node) []lua.Statement {
	if operandNode, op, ok := asIncrementDecrement(node); ok {
		return t.transformIncrementDecrementStmt(operandNode, op)
	}
	if node.Kind == ast.KindBinaryExpression {
		be := node.AsBinaryExpression()
		op := be.OperatorToken.Kind
		// Comma expression: evaluate both sides for side effects, discard result
		if op == ast.KindCommaToken {
			var result []lua.Statement
			result = append(result, t.transformAsStatement(be.Left)...)
			result = append(result, t.transformAsStatement(be.Right)...)
			return result
		}
		if op == ast.KindEqualsToken {
			if ast.IsOptionalChain(be.Left) {
				t.addError(be.Left, dw.NotAllowedOptionalAssignment,
					"The left-hand side of an assignment expression may not be an optional property access.")
				return nil
			}
			if be.Left.Kind == ast.KindArrayLiteralExpression {
				al := be.Left.AsArrayLiteralExpression()
				if t.canUseSimpleArrayAssignment(al) {
					var leftExprs []lua.Expression
					var leftPrec []lua.Statement
					for _, elem := range al.Elements.Nodes {
						if elem.Kind == ast.KindOmittedExpression {
							leftExprs = append(leftExprs, lua.Ident("_"))
						} else {
							expr, prec := t.transformExprInScope(elem)
							leftExprs = append(leftExprs, expr)
							leftPrec = append(leftPrec, prec...)
						}
					}
					right, rightPrec := t.transformExprInScope(be.Right)
					var arrResult []lua.Statement
					arrResult = append(arrResult, leftPrec...)
					arrResult = append(arrResult, rightPrec...)
					// Multi-return calls: assign directly without unpack (call returns multiple values)
					// Non-multi-return: wrap in unpack to extract elements from table/array
					var rhs lua.Expression
					if t.isMultiReturnCall(be.Right) {
						rhs = right
					} else {
						rhs = t.unpackCall(right, len(leftExprs))
					}
					arrResult = append(arrResult, lua.Assign(
						leftExprs,
						[]lua.Expression{rhs},
					))
					for _, elem := range al.Elements.Nodes {
						arrResult = append(arrResult, t.emitExportSync(elem)...)
					}
					return arrResult
				}
				// Complex case: delegate to expression path for element-by-element handling
				_, prec := t.transformExprInScope(node)
				return prec
			}
			// array.length = x → __TS__ArraySetLength(array, x)
			if be.Left.Kind == ast.KindPropertyAccessExpression {
				pa := be.Left.AsPropertyAccessExpression()
				if pa.Name().Kind == ast.KindIdentifier && pa.Name().AsIdentifier().Text == "length" && t.isArrayType(pa.Expression) {
					arrExpr, arrPrec := t.transformExprInScope(pa.Expression)
					right, rightPrec := t.transformExprInScope(be.Right)
					var result []lua.Statement
					result = append(result, arrPrec...)
					result = append(result, rightPrec...)
					fn := t.requireLualib("__TS__ArraySetLength")
					call := lualibCall(fn, arrExpr, right)
					result = append(result, lua.ExprStmt(call))
					return result
				}
			}
			// super.prop = value where prop is a set accessor
			if be.Left.Kind == ast.KindPropertyAccessExpression {
				pa := be.Left.AsPropertyAccessExpression()
				if pa.Expression.Kind == ast.KindSuperKeyword && t.isSuperSetAccessor(be.Left) {
					right, rightPrec := t.transformExprInScope(be.Right)
					prop := pa.Name().AsIdentifier().Text
					fn := t.requireLualib("__TS__DescriptorSet")
					call := lua.Call(lua.Ident(fn), lua.Ident("self"), t.superBaseExpression(), lua.Str(prop), right)
					var result []lua.Statement
					result = append(result, rightPrec...)
					result = append(result, lua.ExprStmt(call))
					return result
				}
			}
			// Validate function context compatibility
			t.validateBinaryAssignment(be)
			right, rightPrec := t.transformExprInScope(be.Right)
			t.pushPrecedingStatements()
			left := t.transformAssignmentLHS(be.Left, len(rightPrec) > 0)
			leftPrec := t.popPrecedingStatements()
			var result []lua.Statement
			result = append(result, leftPrec...)
			result = append(result, rightPrec...)
			result = append(result, lua.Assign([]lua.Expression{left}, []lua.Expression{right}))
			result = append(result, t.emitExportSync(be.Left)...)
			return result
		}
		if isCompoundAssignment(op) {
			be := node.AsBinaryExpression()
			return t.transformCompoundAssignmentStmt(be)
		}
	}
	expr, prec := t.transformExprInScope(node)
	result := make([]lua.Statement, 0, len(prec)+1)
	result = append(result, prec...)
	// Only emit as statement if it's a call expression (has side effects).
	// Skip bare literals/identifiers that are invalid as Lua statements.
	if _, isCall := expr.(*lua.CallExpression); isCall {
		result = append(result, lua.ExprStmt(expr))
	}
	return result
}

func isCompoundAssignment(op ast.Kind) bool {
	switch op {
	case ast.KindPlusEqualsToken, ast.KindMinusEqualsToken,
		ast.KindAsteriskEqualsToken, ast.KindSlashEqualsToken,
		ast.KindPercentEqualsToken, ast.KindAsteriskAsteriskEqualsToken,
		ast.KindAmpersandEqualsToken, ast.KindBarEqualsToken,
		ast.KindCaretEqualsToken, ast.KindLessThanLessThanEqualsToken,
		ast.KindGreaterThanGreaterThanEqualsToken, ast.KindGreaterThanGreaterThanGreaterThanEqualsToken,
		ast.KindAmpersandAmpersandEqualsToken, ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken:
		return true
	}
	return false
}
