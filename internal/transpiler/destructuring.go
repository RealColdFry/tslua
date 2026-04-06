package transpiler

import (
	"fmt"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/realcoldfry/tslua/internal/lua"
)

// Emits preceding statements for array destructuring reassignment: [a, b] = expr
// Returns the temp variable holding the RHS so the expression value is preserved.
func (t *Transpiler) transformArrayDestructuringAssignment(be *ast.BinaryExpression) lua.Expression {
	al := be.Left.AsArrayLiteralExpression()
	initExpr := t.transformExpression(be.Right)

	// Multi-return call: wrap in table to capture all return values
	if t.isMultiReturnCall(be.Right) {
		initExpr = lua.Table(lua.Field(initExpr))
	}

	// Use moveToPrecedingTempWithNode: skip temp for const vars, this, literals
	root := t.moveToPrecedingTempWithNode(initExpr, be.Right)

	// Empty binding: just evaluate RHS
	if len(al.Elements.Nodes) == 0 {
		return root
	}

	// Always element-by-element extraction matching TSTL's transformArrayLiteralAssignmentPattern.
	// No unpack path for expression-level destructuring.
	rightHasPrecedingStatements := false // TODO: track this properly
	t.destructureArrayLiteralAssignment(al, root, rightHasPrecedingStatements)

	return root
}

// destructureArrayLiteralAssignment implements TSTL's transformArrayLiteralAssignmentPattern.
// It always extracts element-by-element, passing index expressions directly to nested patterns.
func (t *Transpiler) destructureArrayLiteralAssignment(al *ast.ArrayLiteralExpression, root lua.Expression, rightHasPrecedingStatements bool) {
	for i, elem := range al.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			continue
		}
		indexedRoot := lua.Index(root, lua.Num(fmt.Sprintf("%d", i+1)))

		switch elem.Kind {
		case ast.KindObjectLiteralExpression:
			// Nested object pattern: pass indexed root directly (no temp)
			t.destructureObjectLiteralAssignment(elem.AsObjectLiteralExpression(), indexedRoot, rightHasPrecedingStatements)

		case ast.KindArrayLiteralExpression:
			// Nested array pattern: pass indexed root directly (no temp)
			t.destructureArrayLiteralAssignment(elem.AsArrayLiteralExpression(), indexedRoot, rightHasPrecedingStatements)

		case ast.KindBinaryExpression:
			// Default value: [x = defaultVal] = arr
			bin := elem.AsBinaryExpression()

			assignedVar := t.nextTempForLuaExpression(indexedRoot)
			t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{lua.Ident(assignedVar)}, []lua.Expression{indexedRoot}))

			nilCond := lua.Binary(lua.Ident(assignedVar), lua.OpEq, lua.Nil())

			// Default branch: transform default and assign to target
			defaultVal, defaultPrec := t.transformExprInScope(bin.Right)
			defaultStmts := t.transformAssignmentDestructuring(bin.Left, defaultVal, rightHasPrecedingStatements)
			ifBody := make([]lua.Statement, 0, len(defaultPrec)+len(defaultStmts))
			ifBody = append(ifBody, defaultPrec...)
			ifBody = append(ifBody, defaultStmts...)

			// Else branch: assign temp to target
			elseStmts := t.transformAssignmentDestructuring(bin.Left, lua.Ident(assignedVar), false)

			t.addPrecedingStatements(lua.If(
				nilCond,
				&lua.Block{Statements: ifBody},
				&lua.Block{Statements: elseStmts},
			))

		case ast.KindSpreadElement:
			se := elem.AsSpreadElement()
			fn := t.requireLualib("__TS__ArraySlice")
			restExpr := lualibCall(fn, root, lua.Num(fmt.Sprintf("%d", i)))
			stmts := t.transformAssignmentDestructuring(se.Expression, restExpr, rightHasPrecedingStatements)
			t.addPrecedingStatements(stmts...)

		default:
			// Simple assignment: identifier, property access, element access
			stmts, prec := t.transformAssignmentDestructuringInScope(elem, indexedRoot, rightHasPrecedingStatements)
			t.addPrecedingStatements(prec...)
			t.addPrecedingStatements(stmts...)
		}
	}
}

// destructureObjectLiteralAssignment implements TSTL's transformObjectLiteralAssignmentPattern.
func (t *Transpiler) destructureObjectLiteralAssignment(ol *ast.ObjectLiteralExpression, root lua.Expression, rightHasPrecedingStatements bool) {
	for _, prop := range ol.Properties.Nodes {
		switch prop.Kind {
		case ast.KindShorthandPropertyAssignment:
			t.destructureShorthandPropertyAssignment(prop.AsShorthandPropertyAssignment(), root)

		case ast.KindPropertyAssignment:
			t.destructurePropertyAssignment(prop.AsPropertyAssignment(), root, rightHasPrecedingStatements)

		case ast.KindSpreadAssignment:
			sa := prop.AsSpreadAssignment()
			target := t.transformExpression(sa.Expression)
			fn := t.requireLualib("__TS__ObjectRest")
			var excludedFields []*lua.TableFieldExpression
			for _, other := range ol.Properties.Nodes {
				if other.Kind == ast.KindSpreadAssignment {
					continue
				}
				var name string
				switch other.Kind {
				case ast.KindShorthandPropertyAssignment:
					name = other.AsShorthandPropertyAssignment().Name().AsIdentifier().Text
				case ast.KindPropertyAssignment:
					opa := other.AsPropertyAssignment()
					if opa.Name().Kind == ast.KindIdentifier {
						name = opa.Name().AsIdentifier().Text
					}
				}
				if name != "" {
					excludedFields = append(excludedFields, lua.KeyField(lua.Str(name), lua.Bool(true)))
				}
			}
			t.addPrecedingStatements(lua.Assign(
				[]lua.Expression{target},
				[]lua.Expression{lualibCall(fn, root, lua.Table(excludedFields...))},
			))
		}
	}
}

// destructureShorthandPropertyAssignment handles { x } and { x = default } in assignments.
func (t *Transpiler) destructureShorthandPropertyAssignment(spa *ast.ShorthandPropertyAssignment, root lua.Expression) {
	name := spa.Name().AsIdentifier().Text
	extractionIndex := lua.Str(name)
	extractExpr := lua.Index(root, extractionIndex)

	// Assign extracted value to target: x = root.x
	stmts := t.transformAssignmentDestructuring(spa.Name(), extractExpr, false)
	t.addPrecedingStatements(stmts...)

	// Default value: if x == nil then x = default end
	if spa.ObjectAssignmentInitializer != nil {
		target := t.transformExpression(spa.Name())
		defaultVal := t.transformExpression(spa.ObjectAssignmentInitializer)
		defaultStmts := t.transformAssignmentDestructuring(spa.Name(), defaultVal, false)
		t.addPrecedingStatements(lua.If(
			lua.Binary(target, lua.OpEq, lua.Nil()),
			&lua.Block{Statements: defaultStmts},
			nil,
		))
	}
}

// destructurePropertyAssignment handles { key: target } and { key: target = default } in assignments.
func (t *Transpiler) destructurePropertyAssignment(pa *ast.PropertyAssignment, root lua.Expression, rightHasPrecedingStatements bool) {
	initializer := pa.Initializer

	// Nested patterns: { x: [a, b] } or { x: { a, b } }
	if initializer.Kind == ast.KindArrayLiteralExpression || initializer.Kind == ast.KindObjectLiteralExpression {
		propKey := t.propertyKey(pa.Name())
		newRoot := lua.Index(root, propKey)
		if initializer.Kind == ast.KindObjectLiteralExpression {
			t.destructureObjectLiteralAssignment(initializer.AsObjectLiteralExpression(), newRoot, rightHasPrecedingStatements)
		} else {
			t.destructureArrayLiteralAssignment(initializer.AsArrayLiteralExpression(), newRoot, rightHasPrecedingStatements)
		}
		return
	}

	// Non-pattern: isolate preceding statements for this property
	t.pushPrecedingStatements()

	propKey := t.propertyKey(pa.Name())
	propKey = t.moveToPrecedingTemp(propKey)
	extractingExpr := lua.Index(root, propKey)

	var destructureStmts []lua.Statement

	if initializer.Kind == ast.KindBinaryExpression {
		bin := initializer.AsBinaryExpression()
		if bin.OperatorToken.Kind == ast.KindEqualsToken {
			if bin.Left.Kind == ast.KindPropertyAccessExpression || bin.Left.Kind == ast.KindElementAccessExpression {
				// Access expression LHS needs table+index cached
				left := t.transformExpression(bin.Left)
				defaultExpr, defaultPrec := t.transformExprInScope(bin.Right)

				if tableIdx, ok := left.(*lua.TableIndexExpression); ok {
					tableTemp := t.moveToPrecedingTemp(tableIdx.Table)
					indexTemp := t.moveToPrecedingTemp(tableIdx.Index)

					elemTemp := t.nextTempForLuaExpression(extractingExpr)
					t.addPrecedingStatements(lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(elemTemp)},
						[]lua.Expression{extractingExpr},
					))
					ifBody := make([]lua.Statement, 0, len(defaultPrec)+1)
					ifBody = append(ifBody, defaultPrec...)
					ifBody = append(ifBody, lua.Assign(
						[]lua.Expression{lua.Ident(elemTemp)},
						[]lua.Expression{defaultExpr},
					))
					t.addPrecedingStatements(lua.If(
						lua.Binary(lua.Ident(elemTemp), lua.OpEq, lua.Nil()),
						&lua.Block{Statements: ifBody},
						nil,
					))
					t.addPrecedingStatements(lua.Assign(
						[]lua.Expression{lua.Index(tableTemp, indexTemp)},
						[]lua.Expression{lua.Ident(elemTemp)},
					))
				} else {
					elemTemp := t.nextTempForLuaExpression(extractingExpr)
					t.addPrecedingStatements(lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(elemTemp)},
						[]lua.Expression{extractingExpr},
					))
					ifBody := make([]lua.Statement, 0, len(defaultPrec)+1)
					ifBody = append(ifBody, defaultPrec...)
					ifBody = append(ifBody, lua.Assign(
						[]lua.Expression{lua.Ident(elemTemp)},
						[]lua.Expression{defaultExpr},
					))
					t.addPrecedingStatements(lua.If(
						lua.Binary(lua.Ident(elemTemp), lua.OpEq, lua.Nil()),
						&lua.Block{Statements: ifBody},
						nil,
					))
					t.addPrecedingStatements(lua.Assign(
						[]lua.Expression{left},
						[]lua.Expression{lua.Ident(elemTemp)},
					))
				}
			} else {
				// Simple LHS (identifier etc.): use TSTL's pattern
				target := t.transformExpression(bin.Left)
				nilCond := lua.Binary(target, lua.OpEq, lua.Nil())
				// if block: full assignment statement (target = default)
				assignStmts := t.transformAssignmentDestructuring(initializer, nil, false)
				destructureStmts = []lua.Statement{lua.If(nilCond, &lua.Block{Statements: assignStmts}, nil)}
			}
		} else {
			target := t.transformExpression(initializer)
			t.emitAssignToTarget(initializer, target, extractingExpr)
		}
	} else {
		// Direct assignment, no default
		stmts := t.transformAssignmentDestructuring(initializer, extractingExpr, rightHasPrecedingStatements)
		destructureStmts = stmts
	}

	innerPrec := t.popPrecedingStatements()
	t.addPrecedingStatements(innerPrec...)
	t.addPrecedingStatements(destructureStmts...)
}

// Emits preceding statements for object destructuring reassignment: ({ x, y } = expr)
func (t *Transpiler) transformObjectDestructuringAssignment(be *ast.BinaryExpression) lua.Expression {
	ol := be.Left.AsObjectLiteralExpression()
	initExpr := t.transformExpression(be.Right)

	// Use moveToPrecedingTempWithNode: skip temp for const vars, this, literals
	root := t.moveToPrecedingTempWithNode(initExpr, be.Right)

	t.destructureObjectLiteralAssignment(ol, root, false)

	return root
}

// transformAssignmentDestructuring is a simplified port of TSTL's transformAssignment.
// It assigns `right` to `lhs` (a TS node), handling exports and array length.
func (t *Transpiler) transformAssignmentDestructuring(lhs *ast.Node, right lua.Expression, rightHasPrecedingStatements bool) []lua.Statement {
	if t.isArrayLengthTarget(lhs) {
		target := t.transformExpression(lhs)
		if unary, ok := target.(*lua.UnaryExpression); ok && unary.Operator == lua.OpLen {
			fn := t.requireLualib("__TS__ArraySetLength")
			call := lualibCall(fn, unary.Operand, right)
			return []lua.Statement{lua.ExprStmt(call)}
		}
	}

	target := t.transformAssignmentLHSExpression(lhs, rightHasPrecedingStatements)
	stmts := []lua.Statement{lua.Assign([]lua.Expression{target}, []lua.Expression{right})}
	stmts = append(stmts, t.emitExportSync(lhs)...)
	return stmts
}

// transformAssignmentDestructuringInScope transforms an assignment in its own preceding statement scope.
func (t *Transpiler) transformAssignmentDestructuringInScope(lhs *ast.Node, right lua.Expression, rightHasPrecedingStatements bool) ([]lua.Statement, []lua.Statement) {
	t.pushPrecedingStatements()
	stmts := t.transformAssignmentDestructuring(lhs, right, rightHasPrecedingStatements)
	prec := t.popPrecedingStatements()
	return stmts, prec
}

// transformAssignmentLHSExpression transforms the LHS of an assignment, caching
// table/index components when rightHasPrecedingStatements is true.
func (t *Transpiler) transformAssignmentLHSExpression(node *ast.Node, rightHasPrecedingStatements bool) lua.Expression {
	if rightHasPrecedingStatements && (node.Kind == ast.KindElementAccessExpression || node.Kind == ast.KindPropertyAccessExpression) {
		left := t.transformExpression(node)
		if tableIdx, ok := left.(*lua.TableIndexExpression); ok {
			tableIdx.Table = t.moveToPrecedingTempWithNode(tableIdx.Table, node)
			tableIdx.Index = t.moveToPrecedingTemp(tableIdx.Index)
			return tableIdx
		}
		return left
	}
	left := t.transformExpression(node)
	// Handle exports
	if ident, ok := left.(*lua.Identifier); ok {
		if t.checker != nil {
			sym := t.checker.GetSymbolAtLocation(node)
			if sym != nil && t.getIdentifierExportScope(node) != nil {
				return lua.Index(lua.Ident("____exports"), lua.Str(ident.Text))
			}
		}
	}
	return left
}

// nextTempForLuaExpression creates a temp name derived from a Lua expression,
// matching TSTL's createTempNameForLuaExpression naming convention.
func (t *Transpiler) nextTempForLuaExpression(expr lua.Expression) string {
	prefix := tempNameForLuaExpression(expr)
	if prefix == "" {
		prefix = "temp"
	}
	return t.nextTemp(prefix)
}

// isArrayLengthTarget returns true if the node is a property access of .length on an array type.
// In Lua, #arr is a read-only expression (not an lvalue), so assignments to array.length
// need special handling via __TS__ArraySetLength.
func (t *Transpiler) isArrayLengthTarget(node *ast.Node) bool {
	if node.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	pa := node.AsPropertyAccessExpression()
	return pa.Name().AsIdentifier().Text == "length" && t.isArrayType(pa.Expression)
}

// canUseSimpleArrayAssignment checks whether an array destructuring assignment
// can use Lua's multi-assignment (a, b = unpack(rhs)) instead of element-by-element.
// Only simple identifiers and omitted elements qualify — property access, spreads,
// defaults, nesting, and side effects all require element-by-element handling.
func (t *Transpiler) canUseSimpleArrayAssignment(al *ast.ArrayLiteralExpression) bool {
	for _, elem := range al.Elements.Nodes {
		if elem.Kind == ast.KindOmittedExpression {
			continue
		}
		if elem.Kind != ast.KindIdentifier {
			return false
		}
		if t.isArrayLengthTarget(elem) {
			return false
		}
	}
	return true
}

// emitAssignToTarget assigns a value to a target expression, using __TS__ArraySetLength
// when the target is array.length (since #arr is not an lvalue in Lua).
func (t *Transpiler) emitAssignToTarget(targetNode *ast.Node, targetExpr, valueExpr lua.Expression) {
	if t.isArrayLengthTarget(targetNode) {
		// targetExpr is #arr — extract the array from the UnaryExpression
		if unary, ok := targetExpr.(*lua.UnaryExpression); ok && unary.Operator == lua.OpLen {
			fn := t.requireLualib("__TS__ArraySetLength")
			call := lualibCall(fn, unary.Operand, valueExpr)
			t.addPrecedingStatements(lua.ExprStmt(call))
			return
		}
	}
	t.addPrecedingStatements(lua.Assign([]lua.Expression{targetExpr}, []lua.Expression{valueExpr}))
	t.addPrecedingStatements(t.emitExportSync(targetNode)...)
}

// propertyKey transforms a TS property name node into a Lua key expression.
// Identifiers and string literals become lua.Str, computed names are evaluated.
func (t *Transpiler) propertyKey(name *ast.Node) lua.Expression {
	switch name.Kind {
	case ast.KindIdentifier:
		return lua.Str(name.AsIdentifier().Text)
	case ast.KindStringLiteral:
		return lua.Str(name.AsStringLiteral().Text)
	case ast.KindNumericLiteral:
		return lua.Num(name.AsNumericLiteral().Text)
	case ast.KindComputedPropertyName:
		return t.transformExpression(name.AsComputedPropertyName().Expression)
	default:
		return t.transformExpression(name)
	}
}
