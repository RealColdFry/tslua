package transpiler

import (
	"fmt"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// getDecorators extracts decorator nodes from a node's modifiers list.
// Equivalent to TypeScript's ts.getDecorators().
func getDecorators(node *ast.Node) []*ast.Node {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return nil
	}
	var decorators []*ast.Node
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindDecorator {
			decorators = append(decorators, m)
		}
	}
	return decorators
}

func hasPrivateModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindPrivateKeyword {
			return true
		}
	}
	return false
}

// hasParameterDecorators checks if any parameter of a function has decorators.
func (t *Transpiler) hasParameterDecorators(node *ast.Node) bool {
	var params *ast.NodeList
	switch node.Kind {
	case ast.KindMethodDeclaration:
		params = node.AsMethodDeclaration().Parameters
	case ast.KindConstructor:
		params = node.AsConstructorDeclaration().Parameters
	default:
		return false
	}
	if params == nil {
		return false
	}
	for _, p := range params.Nodes {
		if len(getDecorators(p)) > 0 {
			return true
		}
	}
	return false
}

// transformDecoratorExpression transforms a decorator's expression into a Lua expression.
func (t *Transpiler) transformDecoratorExpression(decorator *ast.Node) lua.Expression {
	expr := decorator.AsDecorator().Expression
	// Check if decorator function has 'this: void' — incompatible with class context
	if t.checker != nil {
		typ := t.checker.GetTypeAtLocation(expr)
		if typ != nil && t.getFunctionContextType(typ) == contextVoid {
			t.addError(decorator, dw.DecoratorInvalidContext,
				"Decorator function cannot have 'this: void'.")
		}
	}
	return t.transformExpression(expr)
}

// createClassDecoratingExpression creates the __TS__Decorate or __TS__DecorateLegacy call for a class.
func (t *Transpiler) createClassDecoratingExpression(classNode *ast.Node, className lua.Expression, origName string) lua.Expression {
	decorators := getDecorators(classNode)
	decoratorExprs := make([]lua.Expression, len(decorators))
	for i, d := range decorators {
		decoratorExprs[i] = t.transformDecoratorExpression(d)
	}

	if t.compilerOptions != nil && t.compilerOptions.ExperimentalDecorators.IsTrue() {
		return t.createLegacyDecoratingExpression(classNode.Kind, decoratorExprs, className, nil)
	}

	// TC39 decorator
	context := lua.Table(
		lua.KeyField(lua.Ident("kind"), lua.Str("class")),
		lua.KeyField(lua.Ident("name"), lua.Str(origName)),
	)
	return t.createDecoratingExpression(className, className, decoratorExprs, context)
}

// createClassMethodDecoratingExpression creates the __TS__Decorate call for a method.
func (t *Transpiler) createClassMethodDecoratingExpression(method *ast.Node, originalMethod lua.Expression, className lua.Expression) lua.Expression {
	methodDecorators := getDecorators(method)
	decoratorExprs := make([]lua.Expression, len(methodDecorators))
	for i, d := range methodDecorators {
		decoratorExprs[i] = t.transformDecoratorExpression(d)
	}

	md := method.AsMethodDeclaration()
	methodName := t.propertyKey(md.Name())

	if t.compilerOptions != nil && t.compilerOptions.ExperimentalDecorators.IsTrue() {
		// Legacy: include parameter decorators
		paramDecorators := t.getParameterDecorators(method)
		allDecorators := append(decoratorExprs, paramDecorators...)
		memberOwner := t.transformMemberExpressionOwnerName(method, className)
		return t.createLegacyDecoratingExpression(method.Kind, allDecorators, memberOwner, methodName)
	}

	// TC39 decorator
	context := lua.Table(
		lua.KeyField(lua.Ident("kind"), lua.Str("method")),
		lua.KeyField(lua.Ident("name"), methodName),
		lua.KeyField(lua.Ident("private"), lua.Bool(hasPrivateModifier(method))),
		lua.KeyField(lua.Ident("static"), lua.Bool(hasStaticModifier(method))),
	)
	return t.createDecoratingExpression(className, originalMethod, decoratorExprs, context)
}

// createClassAccessorDecoratingExpression creates the __TS__Decorate call for a getter/setter.
func (t *Transpiler) createClassAccessorDecoratingExpression(accessor *ast.Node, originalAccessor lua.Expression, className lua.Expression) lua.Expression {
	decorators := getDecorators(accessor)
	decoratorExprs := make([]lua.Expression, len(decorators))
	for i, d := range decorators {
		decoratorExprs[i] = t.transformDecoratorExpression(d)
	}

	var propName lua.Expression
	if accessor.Kind == ast.KindGetAccessor {
		propName = t.propertyKey(accessor.AsGetAccessorDeclaration().Name())
	} else {
		propName = t.propertyKey(accessor.AsSetAccessorDeclaration().Name())
	}

	if t.compilerOptions != nil && t.compilerOptions.ExperimentalDecorators.IsTrue() {
		memberOwner := t.transformMemberExpressionOwnerName(accessor, className)
		return t.createLegacyDecoratingExpression(accessor.Kind, decoratorExprs, memberOwner, propName)
	}

	// TC39 decorator
	kind := "getter"
	if accessor.Kind == ast.KindSetAccessor {
		kind = "setter"
	}
	context := lua.Table(
		lua.KeyField(lua.Ident("kind"), lua.Str(kind)),
		lua.KeyField(lua.Ident("name"), propName),
		lua.KeyField(lua.Ident("private"), lua.Bool(hasPrivateModifier(accessor))),
		lua.KeyField(lua.Ident("static"), lua.Bool(hasStaticModifier(accessor))),
	)
	return t.createDecoratingExpression(className, originalAccessor, decoratorExprs, context)
}

// createClassPropertyDecoratingExpression creates the __TS__Decorate call for a field.
func (t *Transpiler) createClassPropertyDecoratingExpression(property *ast.Node, className lua.Expression) lua.Expression {
	decorators := getDecorators(property)
	decoratorExprs := make([]lua.Expression, len(decorators))
	for i, d := range decorators {
		decoratorExprs[i] = t.transformDecoratorExpression(d)
	}

	// Warn when a field decorator returns a non-void value (TSTL ignores returned initializers)
	if t.checker != nil {
		for _, d := range decorators {
			sig := t.checker.GetResolvedSignature(d)
			if sig != nil {
				retType := checker.Checker_getReturnTypeOfSignature(t.checker, sig)
				if retType != nil && checker.Type_flags(retType)&checker.TypeFlagsVoid == 0 {
					t.addWarning(property, dw.IncompleteFieldDecoratorWarning,
						"You are using a class field decorator, note that tstl ignores returned value initializers!")
					break
				}
			}
		}
	}

	pd := property.AsPropertyDeclaration()
	propName := t.propertyKey(pd.Name())

	if t.compilerOptions != nil && t.compilerOptions.ExperimentalDecorators.IsTrue() {
		memberOwner := t.transformMemberExpressionOwnerName(property, className)
		return t.createLegacyDecoratingExpression(property.Kind, decoratorExprs, memberOwner, propName)
	}

	// TC39 decorator — field decorator
	context := lua.Table(
		lua.KeyField(lua.Ident("kind"), lua.Str("field")),
		lua.KeyField(lua.Ident("name"), propName),
		lua.KeyField(lua.Ident("private"), lua.Bool(hasPrivateModifier(property))),
		lua.KeyField(lua.Ident("static"), lua.Bool(hasStaticModifier(property))),
	)
	return t.createDecoratingExpression(className, lua.Nil(), decoratorExprs, context)
}

// createDecoratingExpression generates: __TS__Decorate(className, originalValue, {decorators}, {context})
func (t *Transpiler) createDecoratingExpression(className lua.Expression, originalValue lua.Expression, decorators []lua.Expression, context lua.Expression) lua.Expression {
	fn := t.requireLualib("__TS__Decorate")
	decoratorFields := make([]*lua.TableFieldExpression, len(decorators))
	for i, d := range decorators {
		decoratorFields[i] = lua.Field(d)
	}
	decoratorTable := lua.Table(decoratorFields...)
	return lua.Call(lua.Ident(fn), className, originalValue, decoratorTable, context)
}

// createLegacyDecoratingExpression generates: __TS__DecorateLegacy({decorators}, target[, key[, true|nil]])
func (t *Transpiler) createLegacyDecoratingExpression(kind ast.Kind, decorators []lua.Expression, targetTableName lua.Expression, targetFieldExpression lua.Expression) lua.Expression {
	fn := t.requireLualib("__TS__DecorateLegacy")
	decoratorFields := make([]*lua.TableFieldExpression, len(decorators))
	for i, d := range decorators {
		decoratorFields[i] = lua.Field(d)
	}
	decoratorTable := lua.Table(decoratorFields...)

	args := []lua.Expression{decoratorTable, targetTableName}
	if targetFieldExpression != nil {
		args = append(args, targetFieldExpression)
		isMethodOrAccessor := kind == ast.KindMethodDeclaration ||
			kind == ast.KindGetAccessor ||
			kind == ast.KindSetAccessor
		if isMethodOrAccessor {
			args = append(args, lua.Bool(true))
		} else {
			args = append(args, lua.Nil())
		}
	}
	return lua.Call(lua.Ident(fn), args...)
}

// getParameterDecorators collects __TS__DecorateParam calls for legacy parameter decorators.
func (t *Transpiler) getParameterDecorators(node *ast.Node) []lua.Expression {
	var params *ast.NodeList
	switch node.Kind {
	case ast.KindMethodDeclaration:
		params = node.AsMethodDeclaration().Parameters
	case ast.KindConstructor:
		params = node.AsConstructorDeclaration().Parameters
	default:
		return nil
	}
	if params == nil {
		return nil
	}

	fn := t.requireLualib("__TS__DecorateParam")
	var result []lua.Expression
	for i, p := range params.Nodes {
		decorators := getDecorators(p)
		for _, d := range decorators {
			result = append(result, lua.Call(
				lua.Ident(fn),
				lua.Num(fmt.Sprintf("%d", i)),
				t.transformDecoratorExpression(d),
			))
		}
	}
	return result
}

// transformMemberExpressionOwnerName returns either className (for static) or className.prototype (for instance).
func (t *Transpiler) transformMemberExpressionOwnerName(node *ast.Node, className lua.Expression) lua.Expression {
	if hasStaticModifier(node) {
		return className
	}
	return memberAccess(className, "prototype")
}

// createConstructorDecoratingExpression creates legacy parameter decorator calls for constructors.
func (t *Transpiler) createConstructorDecoratingExpression(constructor *ast.Node, className lua.Expression) lua.Statement {
	paramDecorators := t.getParameterDecorators(constructor)
	if len(paramDecorators) == 0 {
		return nil
	}
	decorateCall := t.createLegacyDecoratingExpression(constructor.Kind, paramDecorators, className, nil)
	return lua.ExprStmt(decorateCall)
}
