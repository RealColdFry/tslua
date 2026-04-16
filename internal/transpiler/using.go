package transpiler

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/realcoldfry/tslua/internal/lua"
)

// isUsingDeclaration checks if a variable statement uses the `using` or `await using` flag.
func isUsingDeclaration(node *ast.Node) bool {
	if node.Kind != ast.KindVariableStatement {
		return false
	}
	vs := node.AsVariableStatement()
	return vs.DeclarationList.AsVariableDeclarationList().Flags&ast.NodeFlagsUsing != 0
}

// isAwaitUsingDeclaration checks if a variable statement is `await using`.
func isAwaitUsingDeclaration(node *ast.Node) bool {
	if node.Kind != ast.KindVariableStatement {
		return false
	}
	vs := node.AsVariableStatement()
	flags := vs.DeclarationList.AsVariableDeclarationList().Flags
	return flags&ast.NodeFlagsAwaitUsing == ast.NodeFlagsAwaitUsing
}

// transformStatementsWithUsing processes a list of statements, detecting `using` declarations
// and wrapping remaining statements in __TS__Using/__TS__UsingAsync callbacks.
// shouldReturn controls whether the using call is wrapped in a return statement (function bodies)
// or emitted as an expression statement (source files and standalone blocks).
// Returns (hasUsings, transformedStatements).
func (t *Transpiler) transformStatementsWithUsing(stmts []*ast.Node, shouldReturn bool) (bool, []lua.Statement) {
	var result []lua.Statement

	for i, stmt := range stmts {
		if isUsingDeclaration(stmt) {
			isAwait := isAwaitUsingDeclaration(stmt)

			var usingFn string
			if isAwait {
				usingFn = t.requireLualib("__TS__UsingAsync")
			} else {
				usingFn = t.requireLualib("__TS__Using")
			}

			vs := stmt.AsVariableStatement()
			declList := vs.DeclarationList.AsVariableDeclarationList()

			// Collect variable names for callback parameters
			var paramNames []*lua.Identifier
			var initExprs []lua.Expression
			for _, decl := range declList.Declarations.Nodes {
				d := decl.AsVariableDeclaration()
				name := d.Name().AsIdentifier().Text
				paramNames = append(paramNames, lua.Ident(name))
				if d.Initializer != nil {
					initExprs = append(initExprs, t.transformExpression(d.Initializer))
				} else {
					initExprs = append(initExprs, lua.Ident("nil"))
				}
			}

			// For await using inside async functions, the callback body is itself async.
			// Bump asyncDepth so return statements inside get ____awaiter_resolve wrapping.
			inAsync := isAwait && t.asyncDepth > 0
			if inAsync {
				t.asyncDepth++
			}

			// Transform the remaining statements into the callback body.
			// Push a function scope so hoisting and symbol tracking work
			// inside the callback, matching TSTL's pre-transformer approach
			// where the callback is a real function expression.
			remainingStmts := stmts[i+1:]
			var callbackBody []lua.Statement
			if len(remainingStmts) > 0 {
				scope := t.pushScope(ScopeFunction, nil)
				hasNestedUsing, nestedStmts := t.transformStatementsWithUsing(remainingStmts, shouldReturn)
				if hasNestedUsing {
					callbackBody = nestedStmts
				} else {
					for _, s := range remainingStmts {
						callbackBody = append(callbackBody, t.transformStatementWithComments(s)...)
					}
				}
				callbackBody = t.performHoisting(scope, callbackBody)
				t.popScope()
			}

			if inAsync {
				// Wrap callback body in __TS__AsyncAwaiter
				callbackBody = t.wrapInAsyncAwaiter(callbackBody)
				t.asyncDepth--
			}

			callback := &lua.FunctionExpression{
				Params: paramNames,
				Body:   &lua.Block{Statements: callbackBody},
			}

			// Build the call: __TS__Using(nil, callback, init1, init2, ...)
			args := []lua.Expression{lua.Nil(), callback}
			args = append(args, initExprs...)
			callExpr := lua.Call(lua.Ident(usingFn), args...)

			// If await using, wrap in __TS__Await
			if isAwait {
				awaitFn := t.requireLualib("__TS__Await")
				callExpr = lua.Call(lua.Ident(awaitFn), callExpr)
			}

			if shouldReturn {
				// In async context, return via ____awaiter_resolve
				if t.asyncDepth > 0 {
					result = append(result, lua.Return(
						lua.Call(lua.Ident("____awaiter_resolve"), lua.Nil(), callExpr),
					))
				} else {
					result = append(result, lua.Return(callExpr))
				}
			} else {
				result = append(result, lua.ExprStmt(callExpr))
			}

			return true, result
		}

		// Not a using declaration — transform normally and add to result
		result = append(result, t.transformStatementWithComments(stmt)...)
	}

	return false, result
}
