package transpiler

import (
	"fmt"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

func (t *Transpiler) transformCallExpression(node *ast.Node) lua.Expression {
	ce := node.AsCallExpression()

	// super(...) calls
	if isSuperCall(node) {
		return t.transformSuperCall(node)
	}

	// super.method(...) calls
	if isSuperMethodCall(node) {
		return t.transformSuperMethodCall(node)
	}

	// Optional chaining: delegate to chain-flattening algorithm.
	// Skip if the callee is overridden — we're inside a chain build and should
	// use normal dispatch instead of re-entering the chain handler.
	if ast.IsOptionalChain(node) {
		return t.transformOptionalChain(node)
	}

	// Language extension calls: LuaTable methods, operators, etc.
	if result := t.tryTransformLanguageExtensionCallExpression(node); result != nil {
		return result
	}

	// Function.prototype.call/apply: fn.call(thisArg, ...args) → fn(thisArg, ...args)
	//                                 fn.apply(thisArg, argsArray) → fn(thisArg, unpack(argsArray))
	if result := t.tryTransformFunctionCallApply(ce); result != nil {
		return result
	}

	// Raw require("path/with/slashes") → require("path.with.dots")
	// Lua's require uses dots as path separators, not slashes.
	// Import-generated requires go through resolveModulePath which handles this,
	// but raw require() calls in TS source bypass that path.
	if ce.Expression.Kind == ast.KindIdentifier && ce.Expression.AsIdentifier().Text == "require" &&
		ce.Arguments != nil && len(ce.Arguments.Nodes) > 0 &&
		ce.Arguments.Nodes[0].Kind == ast.KindStringLiteral {
		argText := ce.Arguments.Nodes[0].AsStringLiteral().Text
		if strings.Contains(argText, "/") {
			p := argText
			for strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
				if strings.HasPrefix(p, "./") {
					p = p[2:]
				} else {
					p = p[3:]
				}
			}
			normalized := strings.ReplaceAll(p, "/", ".")
			argExprs := []lua.Expression{lua.Str(normalized)}
			return lua.Call(lua.Ident("require"), argExprs...)
		}
	}

	// performance.now() → os.clock() * 1000
	if result := t.tryTransformPerformanceCall(ce); result != nil {
		return result
	}

	// String(x) → tostring(x)
	if t.isStringConstructorCall(ce) {
		return lua.Call(lua.Ident("tostring"), t.transformArgExprs(ce.Arguments)...)
	}

	// Global function calls: parseInt, parseFloat, isNaN, isFinite, Number()
	if result := t.tryTransformGlobalFunctionCall(ce); result != nil {
		return result
	}

	// Validate function context compatibility on call arguments
	t.validateCallArguments(node)

	// import("./module") → __TS__Promise.resolve(require("module"))
	if ce.Expression.Kind == ast.KindImportKeyword {
		return t.transformImportExpression(ce)
	}

	// Unwrap type assertions and non-null operator to find the actual callee kind
	calleeExpr := ce.Expression
	for calleeExpr.Kind == ast.KindNonNullExpression || calleeExpr.Kind == ast.KindAsExpression ||
		calleeExpr.Kind == ast.KindTypeAssertionExpression || calleeExpr.Kind == ast.KindParenthesizedExpression ||
		calleeExpr.Kind == ast.KindSatisfiesExpression {
		switch calleeExpr.Kind {
		case ast.KindNonNullExpression:
			calleeExpr = calleeExpr.AsNonNullExpression().Expression
		case ast.KindAsExpression:
			calleeExpr = calleeExpr.AsAsExpression().Expression
		case ast.KindTypeAssertionExpression:
			calleeExpr = calleeExpr.AsTypeAssertion().Expression
		case ast.KindParenthesizedExpression:
			calleeExpr = calleeExpr.AsParenthesizedExpression().Expression
		case ast.KindSatisfiesExpression:
			calleeExpr = calleeExpr.AsSatisfiesExpression().Expression
		}
	}

	// Property access calls: obj.method(args)
	// Decide between colon syntax (obj:method(args)) and dot syntax (obj.method(args))
	if calleeExpr.Kind == ast.KindPropertyAccessExpression {
		pa := calleeExpr.AsPropertyAccessExpression()
		// Transform receiver FIRST (before args) to preserve JS evaluation order
		obj := t.transformExpression(pa.Expression)
		method := t.resolvePropertyName(pa)

		// Transform arguments in an isolated scope to detect preceding statements.
		argExprs, argPrec := t.transformArgsInScope(ce.Arguments)

		// If args had side effects, cache receiver and callee before emitting arg precs.
		// In JS, the callee (obj.method) is evaluated before arguments, so we must
		// cache both obj and obj.method before any arg side effects execute.
		if len(argPrec) > 0 {
			// Cache receiver before arg side effects — the receiver may depend on
			// state that args modify (e.g., foo(i).substr(++i) must cache foo(i) first).
			// Skip caching for simple identifiers and built-in globals (Array, Math, etc.)
			// that can't be affected by side effects.
			if t.shouldMoveToTemp(obj) {
				obj = t.moveToPrecedingTemp(obj)
			}

			// Check for built-in calls — built-in receivers (Array, Math, etc.) are not
			// real Lua globals, so don't cache the callee for them.
			// Insert argPrec before any statements the builtin adds (e.g. push optimization)
			// so that temps referenced in argExprs are defined before use.
			precBefore := t.precedingStatementsLen()
			if result := t.tryTransformBuiltinCallWithArgs(ce, argExprs, obj); result != nil {
				t.insertPrecedingStatements(precBefore, argPrec...)
				return result
			}

			var fn lua.Expression = lua.Index(obj, lua.Str(method))
			fn = t.moveToPrecedingTemp(fn)
			t.addPrecedingStatements(argPrec...)

			if t.calleeNeedsSelf(node) {
				allArgs := append([]lua.Expression{obj}, argExprs...)
				return lua.Call(fn, allArgs...)
			}
			return lua.Call(fn, argExprs...)
		}

		// Try built-in method/global calls: Math.min, arr.push, obj.keys, etc.
		if result := t.tryTransformBuiltinCallWithArgs(ce, argExprs, obj); result != nil {
			return result
		}

		if t.calleeNeedsSelf(node) {
			if !isValidLuaIdentifier(method, t.luaTarget.AllowsUnicodeIds()) {
				// Invalid Lua identifiers (keywords, $, unicode) can't use colon syntax.
				// Emit as obj["method"](obj, args) and cache receiver to avoid double evaluation.
				if _, isIdent := obj.(*lua.Identifier); !isIdent {
					temp := t.nextTemp("self")
					t.addPrecedingStatements(lua.LocalDecl(
						[]*lua.Identifier{lua.Ident(temp)},
						[]lua.Expression{obj},
					))
					obj = lua.Ident(temp)
				}
				allArgs := append([]lua.Expression{obj}, argExprs...)
				return lua.Call(lua.Index(obj, lua.Str(method)), allArgs...)
			}
			return lua.MethodCall(obj, method, argExprs...)
		}
		// No self needed: dot syntax call
		return lua.Call(lua.Index(obj, lua.Str(method)), argExprs...)
	}

	// Transform arguments in an isolated scope to detect preceding statements.
	argExprs, argPrec := t.transformArgsInScope(ce.Arguments)

	// Element access calls: obj["method"](args) or obj[expr](args)
	// Pass the object as self context, like property access calls.
	if calleeExpr.Kind == ast.KindElementAccessExpression && t.calleeNeedsSelf(node) {
		ea := calleeExpr.AsElementAccessExpression()
		obj := t.transformExpression(ea.Expression)
		// Cache the object to avoid double evaluation, but skip for simple identifiers
		selfExpr := obj
		if _, isIdent := obj.(*lua.Identifier); !isIdent {
			temp := t.nextTemp("self")
			t.addPrecedingStatements(lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(temp)},
				[]lua.Expression{obj},
			))
			selfExpr = lua.Ident(temp)
		}
		index := t.transformElementAccessIndex(ea)
		var fn lua.Expression = lua.Index(selfExpr, index)
		if len(argPrec) > 0 {
			fn = t.moveToPrecedingTemp(fn)
			t.addPrecedingStatements(argPrec...)
		}
		params := append([]lua.Expression{selfExpr}, argExprs...)
		return lua.Call(fn, params...)
	}

	// Transform callee BEFORE emitting arg preceding statements (preserves eval order)
	fn := t.transformExpression(ce.Expression)

	needsSelf := t.calleeNeedsSelf(node)

	// If arguments had side effects, cache callee and context
	if len(argPrec) > 0 {
		fn = t.moveToPrecedingTemp(fn)
		if needsSelf { //nolint:staticcheck // TODO: cache self context when callee has side-effect args
			// _G is a simple ident so moveToPrecedingTemp won't cache it,
			// but we include it in the flow for ordering correctness
		}
		t.addPrecedingStatements(argPrec...)
	}

	if needsSelf {
		params := append([]lua.Expression{t.defaultSelfContext()}, argExprs...)
		return lua.Call(fn, params...)
	}

	return lua.Call(fn, argExprs...)
}

// defaultSelfContext returns the self parameter for non-method calls that need self.
// In strict mode (ES modules), uses nil. In non-strict mode, uses _G.
func (t *Transpiler) defaultSelfContext() lua.Expression {
	if t.isStrict {
		return lua.Nil()
	}
	return lua.Ident("_G")
}

func (t *Transpiler) calleeNeedsSelf(node *ast.Node) bool {
	ce := node.AsCallExpression()

	// Direct function calls by name: check if it's a Lua global or known builtin
	if ce.Expression.Kind == ast.KindIdentifier {
		name := ce.Expression.AsIdentifier().Text
		if isLuaGlobal(name) {
			return false
		}
	}

	return t.getCallContextType(node) != contextVoid
}

func luaSafeName(name string) string {
	return "____" + fixInvalidLuaIdentifier(name)
}

func fixInvalidLuaIdentifier(name string) string {
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			fmt.Fprintf(&b, "_%X", c)
		}
	}
	return b.String()
}

func isLuaKeyword(name string) bool {
	switch name {
	case "and", "bit", "bit32", "break", "do", "else", "elseif", "end",
		"false", "for", "function", "goto", "if", "in",
		"local", "nil", "not", "or", "repeat", "return",
		"then", "true", "until", "while":
		return true
	}
	return false
}

func isLuaBuiltin(name string) bool {
	switch name {
	case "_G", "assert", "coroutine", "debug", "error", "ipairs",
		"math", "pairs", "pcall", "print", "rawget", "rawset", "repeat",
		"require", "self", "string", "table", "tostring", "tonumber", "type", "unpack":
		return true
	}
	return false
}

func isValidLuaIdentifier(name string, allowUnicode bool) bool {
	if isLuaKeyword(name) || len(name) == 0 {
		return false
	}
	for i, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		if allowUnicode && c >= 0x80 {
			// LuaJIT accepts any byte 0x80-0xFD in identifiers, and valid UTF-8
			// encoding of any code point >= U+0080 only produces bytes in that range.
			// TSTL's regex (\u007F-\uFFFD) operates on UTF-16 code units, which
			// effectively accepts all non-ASCII characters including surrogates,
			// combining marks, and connector punctuation.
			continue
		}
		return false
	}
	return true
}

func (t *Transpiler) isLuaUnsafeName(name string) bool {
	return !isValidLuaIdentifier(name, t.luaTarget.AllowsUnicodeIds()) || isLuaBuiltin(name)
}

// shouldRenameValueSymbol checks if a value symbol needs Lua-safe renaming.
// Used for shorthand properties where the property symbol differs from the value symbol.
func (t *Transpiler) shouldRenameValueSymbol(name string, valueSym *ast.Symbol) bool {
	if !t.isLuaUnsafeName(name) {
		return false
	}
	// Ambient declarations should not be renamed
	for _, decl := range valueSym.Declarations {
		if ast.GetCombinedModifierFlags(decl)&ast.ModifierFlagsAmbient != 0 {
			return false
		}
	}
	// Exported symbols are accessed via ____exports.name, no rename
	if t.isModule && t.exportedNames[name] {
		return false
	}
	return true
}

func (t *Transpiler) hasUnsafeIdentifierName(node *ast.Node) bool {
	name := node.AsIdentifier().Text
	allowUnicode := t.luaTarget.AllowsUnicodeIds()

	if !t.isLuaUnsafeName(name) {
		return false
	}

	symbol := t.checker.GetSymbolAtLocation(node)
	if symbol == nil {
		// No symbol — report diagnostic for invalid identifiers
		if !isValidLuaIdentifier(name, allowUnicode) {
			t.addError(node, dw.InvalidAmbientIdentifierName, fmt.Sprintf(
				"Invalid ambient identifier name '%s'. Ambient identifiers must be valid lua identifiers.", name))
		}
		return !isValidLuaIdentifier(name, allowUnicode)
	}

	// Properties, methods, and accessors are never renamed — unless they're parameter properties,
	// where the parameter name is a local variable that can shadow Lua builtins.
	if symbol.Flags&(ast.SymbolFlagsProperty|ast.SymbolFlagsMethod|ast.SymbolFlagsGetAccessor|ast.SymbolFlagsSetAccessor) != 0 &&
		symbol.Flags&(ast.SymbolFlagsVariable|ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsEnum) == 0 {
		isParam := false
		for _, decl := range symbol.Declarations {
			if decl.Kind == ast.KindParameter {
				isParam = true
				break
			}
		}
		if !isParam {
			return false
		}
	}

	// Ambient declarations (declare var, .d.ts) should not be renamed.
	// Report diagnostic for any invalid identifier (keywords AND non-identifier chars).
	// Builtins like `error`, `string`, `math` are intentionally ambient (from lua-types).
	isAmbient := false
	for _, decl := range symbol.Declarations {
		if ast.GetCombinedModifierFlags(decl)&ast.ModifierFlagsAmbient != 0 {
			isAmbient = true
			break
		}
	}
	if isAmbient {
		if !isValidLuaIdentifier(name, allowUnicode) {
			t.addError(node, dw.InvalidAmbientIdentifierName, fmt.Sprintf(
				"Invalid ambient identifier name '%s'. Ambient identifiers must be valid lua identifiers.", name))
			return true
		}
		return false
	}

	// Exported symbols are accessed via ____exports.name, no rename needed
	if t.isModule && t.exportedNames[name] {
		return false
	}

	return true
}

func isLuaGlobal(name string) bool {
	switch name {
	case "print", "tonumber", "tostring", "type", "error", "pcall", "xpcall",
		"require", "rawget", "rawset", "rawequal", "rawlen",
		"setmetatable", "getmetatable", "pairs", "ipairs", "next",
		"select", "unpack", "assert", "collectgarbage", "dofile",
		"loadfile", "loadstring", "load":
		return true
	}
	return false
}

// wrapOptionalChainCall folds a method/function call into the right branch of
// an optional chain's `and` expression. Without this, `(obj?.prop):method()` would
// crash when obj is nil. With this, it becomes `obj and obj.prop:method()`.
func (t *Transpiler) wrapOptionalChainCall(objExpr lua.Expression, makeCall func(inner lua.Expression) lua.Expression) lua.Expression {
	bin, ok := objExpr.(*lua.BinaryExpression)
	if !ok || bin.Operator != lua.OpAnd {
		return makeCall(objExpr)
	}
	return lua.Binary(bin.Left, lua.OpAnd, makeCall(bin.Right))
}

// resolvePropertyName returns the Lua property name for a PropertyAccessExpression,
// applying @customName if the property's symbol has one.
func (t *Transpiler) resolvePropertyName(pa *ast.PropertyAccessExpression) string {
	if pa.Name().Kind != ast.KindIdentifier {
		return pa.Name().AsPrivateIdentifier().Text
	}
	name := pa.Name().AsIdentifier().Text
	if sym := t.checker.GetSymbolAtLocation(pa.Name()); sym != nil {
		if customName := t.getCustomNameFromSymbol(sym); customName != "" {
			return customName
		}
	}
	return name
}

func (t *Transpiler) transformPropertyAccessExpression(node *ast.Node) lua.Expression {
	// Const enum inlining: ConstEnum.Member → literal value
	if val, ok := t.tryGetConstEnumValue(node); ok {
		return val
	}

	pa := node.AsPropertyAccessExpression()

	// Private identifiers (#x) are not supported in Lua
	if pa.Name().Kind == ast.KindPrivateIdentifier {
		t.addError(node, dw.UnsupportedProperty, "Private identifiers are not supported.")
		return lua.Nil()
	}

	// @compileMembersOnly: skip the enum table in the access path
	if t.hasTypeAnnotation(pa.Expression, AnnotCompileMembersOnly) {
		prop := pa.Name().AsIdentifier().Text
		if pa.Expression.Kind == ast.KindPropertyAccessExpression {
			// x.Enum.Member → x.Member
			parent := pa.Expression.AsPropertyAccessExpression()
			parentObj := t.transformExpression(parent.Expression)
			return lua.Index(parentObj, lua.Str(prop))
		}
		// Ambient (declare) enums have no runtime code — members are globals.
		// Only use export scope for non-ambient enums.
		if !t.isAmbientSymbol(pa.Expression) {
			if exportScope := t.getIdentifierExportScope(pa.Expression); exportScope != nil {
				return lua.Index(exportScope, lua.Str(prop))
			}
		}
		return lua.Ident(prop)
	}

	// Built-in property access: Math.PI, Number.MAX_SAFE_INTEGER, etc.
	if result := t.tryTransformPropertyAccess(pa); result != nil {
		return result
	}

	// Optional chaining: delegate to chain-flattening algorithm.
	// Skip if the base expression is overridden — we're inside a chain build.
	if ast.IsOptionalChain(node) {
		return t.transformOptionalChain(node)
	}

	prop := t.resolvePropertyName(pa)

	// super.prop where prop is a get accessor
	if pa.Expression.Kind == ast.KindSuperKeyword && t.isSuperGetAccessor(node) {
		fn := t.requireLualib("__TS__DescriptorGet")
		return lua.Call(lua.Ident(fn), lua.Ident("self"), t.superBaseExpression(), lua.Str(prop))
	}

	// Accessing properties on LuaMultiReturn calls is invalid
	if t.isMultiReturnCall(pa.Expression) {
		t.addError(node, dw.InvalidMultiReturnAccess, "The LuaMultiReturn type can only be accessed via an element access expression of a numeric type.")
	}

	// Check if this is a method call extension (e.g. table.has) used as a value, not called
	if pa.Expression.Kind == ast.KindIdentifier && node.Parent != nil {
		isCallee := node.Parent.Kind == ast.KindCallExpression && node.Parent.AsCallExpression().Expression == node
		if !isCallee {
			if kind, ok := t.getExtensionKindForNode(node); ok && t.isCallExtensionKind(kind) {
				t.addError(node, dw.InvalidCallExtensionUse, "This function must be called directly and cannot be referred to.")
			}
		}
	}

	obj := t.transformExpression(pa.Expression)

	// .length on arrays → #obj (or table.getn for 5.0)
	if prop == "length" && t.isArrayType(pa.Expression) {
		return t.luaTarget.LenExpr(obj)
	}

	return lua.Index(obj, lua.Str(prop))
}

// arr[0] → arr[1] (1-indexed for arrays with numeric index)
func (t *Transpiler) transformElementAccessExpression(node *ast.Node) lua.Expression {
	// Const enum inlining: TestEnum["C"] → literal value
	if val, ok := t.tryGetConstEnumValue(node); ok {
		return val
	}

	// Optional chaining: delegate to chain-flattening algorithm.
	// Skip if the base expression is overridden — we're inside a chain build.
	if ast.IsOptionalChain(node) {
		return t.transformOptionalChain(node)
	}

	ea := node.AsElementAccessExpression()

	// Multi-return element access: foo()[0] → (foo()), foo()[n] → select(n+1, foo())
	if t.isMultiReturnCall(ea.Expression) {
		// Accessing non-numeric properties on LuaMultiReturn is invalid
		if !t.isNumericExpression(ea.ArgumentExpression) {
			t.addError(node, dw.InvalidMultiReturnAccess, "The LuaMultiReturn type can only be accessed via an element access expression of a numeric type.")
		}
		obj := t.transformExpression(ea.Expression)
		// foo()[0] → (foo()) — parentheses extract only the first return value
		if ea.ArgumentExpression.Kind == ast.KindNumericLiteral && ea.ArgumentExpression.AsNumericLiteral().Text == "0" {
			return lua.Paren(obj)
		}
		// foo()[n] → select(n+1, foo()) — only valid for numeric indices
		index := t.transformExpression(ea.ArgumentExpression)
		if t.isNumericExpression(ea.ArgumentExpression) {
			return lua.Call(lua.Ident("select"), addToNumericExpression(index, 1), obj)
		}
		// Non-numeric index on multi-return (already diagnosed above): emit select without offset
		return lua.Call(lua.Ident("select"), index, obj)
	}

	obj := t.transformExpression(ea.Expression)

	index, indexPrec := t.transformExprInScope(ea.ArgumentExpression)
	// Preserve evaluation order: if index has side effects, cache object first.
	// Skip caching for const identifiers (can't be reassigned).
	if len(indexPrec) > 0 {
		obj = t.moveToPrecedingTempWithNode(obj, ea.Expression)
		t.addPrecedingStatements(indexPrec...)
	}

	// For array access with non-literal numeric index, add 1
	if t.isArrayType(ea.Expression) && t.isNumericExpression(ea.ArgumentExpression) {
		return lua.Index(obj, addToNumericExpression(index, 1))
	}

	// String index access: str[0] → __TS__StringAccess(str, 0)
	// StringAccess returns nil for out-of-bounds (matching JS behavior), unlike StringCharAt which returns ""
	if t.isStringExpression(ea.Expression) && t.isNumericExpression(ea.ArgumentExpression) {
		fn := t.requireLualib("__TS__StringAccess")
		return lualibCall(fn, obj, index)
	}

	return lua.Index(obj, index)
}

func (t *Transpiler) transformVoidExpression(node *ast.Node) lua.Expression {
	ve := node.AsVoidExpression()
	expr := ve.Expression
	// If the operand is a literal, it's safe to just return nil (no side effects).
	// Otherwise, evaluate the operand as a preceding statement for side effects.
	if !isLiteralExpression(expr) {
		inner := t.transformExpression(expr)
		// Unwrap unnecessary parens — `void(x())` should emit `x()` not `(x())`,
		// which would cause Lua ambiguity after a previous statement ending with `end`.
		for p, ok := inner.(*lua.ParenthesizedExpression); ok; p, ok = inner.(*lua.ParenthesizedExpression) {
			inner = p.Inner
		}
		// Only emit as a statement if it has side effects. Simple identifiers (including
		// generated temps from postfix expressions) are pure references and don't need
		// to be emitted — their side effects were already captured in preceding statements.
		switch inner.(type) {
		case *lua.Identifier, *lua.NilLiteral:
			// Pure references / nil — side effects already in preceding statements.
		default:
			t.addPrecedingStatements(lua.ExprStmt(inner))
		}
	}
	return lua.Nil()
}

func isLiteralExpression(node *ast.Node) bool {
	// Unwrap parenthesized expressions
	for node.Kind == ast.KindParenthesizedExpression {
		node = node.AsParenthesizedExpression().Expression
	}
	switch node.Kind {
	case ast.KindNumericLiteral, ast.KindStringLiteral,
		ast.KindRegularExpressionLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return true
	}
	return false
}

func (t *Transpiler) transformPrefixUnaryExpression(node *ast.Node) lua.Expression {
	pu := node.AsPrefixUnaryExpression()
	operand := t.transformExpression(pu.Operand)
	switch pu.Operator {
	case ast.KindExclamationToken:
		return lua.Unary(lua.OpNot, operand)
	case ast.KindMinusToken:
		// If operand is not a number type, wrap in __TS__Number for coercion
		if !t.isNumericExpression(pu.Operand) {
			fn := t.requireLualib("__TS__Number")
			return lualibCall(fn, lua.Unary(lua.OpNeg, operand))
		}
		return lua.Unary(lua.OpNeg, operand)
	case ast.KindPlusToken:
		// Unary + casts to number: if already number, just return operand
		if t.isNumericExpression(pu.Operand) {
			return operand
		}
		fn := t.requireLualib("__TS__Number")
		return lualibCall(fn, operand)
	case ast.KindPlusPlusToken, ast.KindMinusMinusToken:
		binOp := lua.OpAdd
		if pu.Operator == ast.KindMinusMinusToken {
			binOp = lua.OpSub
		}
		// Always cache obj/index for property/element access in expression context.
		if idx, ok := operand.(*lua.TableIndexExpression); ok {
			origName := tempNameForLuaExpression(operand)
			objTemp := t.nextTempForLuaExpression(idx.Table)
			idxTemp := t.nextTempForLuaExpression(idx.Index)
			t.addPrecedingStatements(lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(objTemp), lua.Ident(idxTemp)},
				[]lua.Expression{idx.Table, idx.Index},
			))
			cached := lua.Index(lua.Ident(objTemp), lua.Ident(idxTemp))
			resultTemp := t.nextTemp(origName)
			newVal := lua.Binary(cached, binOp, lua.Num("1"))
			t.addPrecedingStatements(lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(resultTemp)},
				[]lua.Expression{newVal},
			))
			t.addPrecedingStatements(lua.Assign([]lua.Expression{cached}, []lua.Expression{lua.Ident(resultTemp)}))
			t.addPrecedingStatements(t.emitExportSync(pu.Operand)...)
			return lua.Ident(resultTemp)
		}
		t.addPrecedingStatements(lua.Assign([]lua.Expression{operand}, []lua.Expression{lua.Binary(operand, binOp, lua.Num("1"))}))
		t.addPrecedingStatements(t.emitExportSync(pu.Operand)...)
		return operand
	case ast.KindTildeToken:
		if t.luaTarget.HasNativeBitwise() {
			return lua.Unary(lua.OpBitNot, operand)
		}
		lib := t.luaTarget.BitLibrary()
		if lib == "" {
			t.addError(node, dw.UnsupportedForTarget, fmt.Sprintf("Bitwise operations is/are not supported for target %s.", t.luaTarget.DisplayName()))
			lib = "bit"
		}
		return lua.Call(memberAccess(lua.Ident(lib), "bnot"), operand)
	default:
		panic(fmt.Sprintf("unhandled prefix unary operator: %d", pu.Operator))
	}
}

// When used as expression: emit temp var to capture pre-increment value.
// When used as statement: simple x = x + 1 (handled by isNonStatementExpression).
func (t *Transpiler) transformPostfixUnaryExpression(node *ast.Node) lua.Expression {
	pu := node.AsPostfixUnaryExpression()
	operand := t.transformExpression(pu.Operand)

	if pu.Operator == ast.KindPlusPlusToken || pu.Operator == ast.KindMinusMinusToken {
		luaOp := lua.OpAdd
		if pu.Operator == ast.KindMinusMinusToken {
			luaOp = lua.OpSub
		}
		// Always cache obj/index for property/element access in expression context.
		if idx, ok := operand.(*lua.TableIndexExpression); ok {
			{
				// Derive temp name from ORIGINAL expression, not cached — matches TSTL
				origName := tempNameForLuaExpression(operand)
				objTemp := t.nextTempForLuaExpression(idx.Table)
				idxTemp := t.nextTempForLuaExpression(idx.Index)
				t.addPrecedingStatements(lua.LocalDecl(
					[]*lua.Identifier{lua.Ident(objTemp), lua.Ident(idxTemp)},
					[]lua.Expression{idx.Table, idx.Index},
				))
				cached := lua.Index(lua.Ident(objTemp), lua.Ident(idxTemp))
				temp := t.nextTemp(origName)
				t.addPrecedingStatements(
					lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{cached}),
					lua.Assign([]lua.Expression{cached}, []lua.Expression{lua.Binary(lua.Ident(temp), luaOp, lua.Num("1"))}),
				)
				t.addPrecedingStatements(t.emitExportSync(pu.Operand)...)
				return lua.Ident(temp)
			}
		}
		prefix := tempNameForLuaExpression(operand)
		if prefix == "" {
			prefix = "postfix"
		}
		temp := t.nextTemp(prefix)
		t.addPrecedingStatements(
			lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{operand}),
			lua.Assign([]lua.Expression{operand}, []lua.Expression{lua.Binary(lua.Ident(temp), luaOp, lua.Num("1"))}),
		)
		t.addPrecedingStatements(t.emitExportSync(pu.Operand)...)
		return lua.Ident(temp)
	}

	panic(fmt.Sprintf("unhandled postfix unary operator: %d", pu.Operator))
}

// cond ? a : b → cond and a or b (simple case)
// cond ? a : b → local temp; if cond then temp = a else temp = b end (when branches have setup or a could be falsy)
func (t *Transpiler) transformConditionalExpression(node *ast.Node) lua.Expression {
	ce := node.AsConditionalExpression()
	t.checkOnlyTruthyCondition(ce.Condition)

	// Transform each part in its own scope, capturing any preceding statements
	condExpr, condPre := t.transformExprInScope(ce.Condition)
	whenTrueExpr, whenTruePre := t.transformExprInScope(ce.WhenTrue)
	whenFalseExpr, whenFalsePre := t.transformExprInScope(ce.WhenFalse)

	// Use if/else + temp var when:
	// 1. Branches generated setup statements (optional chain temp vars, nested ternaries)
	// 2. whenTrue could be falsy (false/nil) — breaks the `and/or` pattern in Lua
	if len(whenTruePre) > 0 || len(whenFalsePre) > 0 || t.couldBeFalsy(ce.WhenTrue) {
		temp := t.nextTempForLuaExpression(condExpr)

		trueStmts := make([]lua.Statement, 0, len(whenTruePre)+1)
		trueStmts = append(trueStmts, whenTruePre...)
		trueStmts = append(trueStmts, lua.Assign(
			[]lua.Expression{lua.Ident(temp)}, []lua.Expression{whenTrueExpr},
		))

		falseStmts := make([]lua.Statement, 0, len(whenFalsePre)+1)
		falseStmts = append(falseStmts, whenFalsePre...)
		falseStmts = append(falseStmts, lua.Assign(
			[]lua.Expression{lua.Ident(temp)}, []lua.Expression{whenFalseExpr},
		))

		var stmts []lua.Statement
		stmts = append(stmts, lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, nil))
		stmts = append(stmts, condPre...)
		stmts = append(stmts, &lua.IfStatement{
			Condition: condExpr,
			IfBlock:   &lua.Block{Statements: trueStmts},
			ElseBlock: &lua.Block{Statements: falseStmts},
		})

		t.addPrecedingStatements(stmts...)
		return lua.Ident(temp)
	}

	// Simple case: cond and whenTrue or whenFalse
	t.addPrecedingStatements(condPre...)
	return lua.Binary(lua.Binary(condExpr, lua.OpAnd, whenTrueExpr), lua.OpOr, whenFalseExpr)
}

// Handles &&, ||, ?? — all short-circuit operators.
// When RHS has preceding statements, wraps in if/temp to preserve short-circuit semantics.
// For ??, also uses if/temp when LHS could be falsy (boolean/unknown/any/generic).
func (t *Transpiler) transformShortCircuitBinaryExpression(be *ast.BinaryExpression, op ast.Kind) lua.Expression {
	leftExpr, leftPre := t.transformExprInScope(be.Left)
	rightExpr, rightPre := t.transformExprInScope(be.Right)

	// Determine if we need the if/temp pattern:
	// - Always when RHS has preceding statements (short-circuit semantics)
	// - For ??: also when LHS could be falsy (can't use simple `or`)
	needsTemp := len(rightPre) > 0
	if op == ast.KindQuestionQuestionToken && !needsTemp {
		needsTemp = t.canBeFalsyWhenNotNull(be.Left)
	}

	if needsTemp {
		temp := t.nextTempForLuaExpression(leftExpr)

		var stmts []lua.Statement
		stmts = append(stmts, leftPre...)
		stmts = append(stmts, lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{leftExpr}))

		thenStmts := make([]lua.Statement, 0, len(rightPre)+1)
		thenStmts = append(thenStmts, rightPre...)
		thenStmts = append(thenStmts, lua.Assign(
			[]lua.Expression{lua.Ident(temp)}, []lua.Expression{rightExpr},
		))

		var cond lua.Expression
		switch op {
		case ast.KindAmpersandAmpersandToken:
			cond = lua.Ident(temp) // if temp then temp = rhs end
		case ast.KindBarBarToken:
			cond = lua.Unary(lua.OpNot, lua.Ident(temp)) // if not temp then temp = rhs end
		case ast.KindQuestionQuestionToken:
			cond = lua.Binary(lua.Ident(temp), lua.OpEq, lua.Nil()) // if temp == nil then temp = rhs end
		}

		stmts = append(stmts, &lua.IfStatement{
			Condition: cond,
			IfBlock:   &lua.Block{Statements: thenStmts},
		})

		t.addPrecedingStatements(stmts...)
		return lua.Ident(temp)
	}

	// Simple case: no preceding statements and safe to use Lua operator
	t.addPrecedingStatements(leftPre...)
	switch op {
	case ast.KindAmpersandAmpersandToken:
		return lua.Binary(leftExpr, lua.OpAnd, rightExpr)
	case ast.KindBarBarToken:
		return lua.Binary(leftExpr, lua.OpOr, rightExpr)
	default: // QuestionQuestionToken
		return lua.Binary(leftExpr, lua.OpOr, rightExpr)
	}
}

// Checks if a type could be falsy for reasons other than being null/undefined.
// Used by ?? to decide whether simple `or` is safe.
func (t *Transpiler) canBeFalsyWhenNotNull(node *ast.Node) bool {

	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return true
	}
	return t.typeCanBeFalsyWhenNotNull(typ)
}

const falsyWhenNotNullFlags = checker.TypeFlagsBooleanLike | checker.TypeFlagsNever |
	checker.TypeFlagsVoid | checker.TypeFlagsUnknown | checker.TypeFlagsAny

func (t *Transpiler) typeCanBeFalsyWhenNotNull(typ *checker.Type) bool {
	flags := checker.Type_flags(typ)

	// Type parameter: check base constraint, or assume true if unconstrained
	if flags&checker.TypeFlagsTypeParameter != 0 {
		base := checker.Checker_getBaseConstraintOfType(t.checker, typ)
		if base == nil {
			return true
		}
		return t.typeCanBeFalsyWhenNotNull(base)
	}

	if flags&falsyWhenNotNullFlags != 0 {
		return true
	}

	// Recurse into union/intersection types
	if flags&checker.TypeFlagsUnionOrIntersection != 0 {
		for _, member := range typ.Types() {
			if t.typeCanBeFalsyWhenNotNull(member) {
				return true
			}
		}
	}

	return false
}

// Checks if an expression's type could be false or nil (undefined/null).
// When true, the ternary `cond and whenTrue or whenFalse` pattern is unsafe because
// Lua treats false/nil as falsy, causing the `or` branch to execute instead.
func (t *Transpiler) couldBeFalsy(node *ast.Node) bool {
	// Literal checks — known falsy values
	if node.Kind == ast.KindFalseKeyword || node.Kind == ast.KindNullKeyword || node.Kind == ast.KindUndefinedKeyword {
		return true
	}
	if node.Kind == ast.KindIdentifier && node.AsIdentifier().Text == "undefined" {
		return true
	}
	// Known non-falsy literals — safe for and/or pattern
	switch node.Kind {
	case ast.KindTrueKeyword, ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindNumericLiteral,
		ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression,
		ast.KindArrowFunction, ast.KindFunctionExpression:
		return false
	}

	// For non-literal expressions, check if the type could be falsy.
	// Note: even with strictNullChecks, Lua doesn't enforce TS types,
	// so uninitialized variables can be nil at runtime.

	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return true
	}

	// String variables could be nil in Lua even if TS says they can't be undefined.
	// Only string LITERALS are guaranteed non-nil. Be conservative for identifiers.
	if node.Kind == ast.KindIdentifier {
		flags := checker.Type_flags(typ)
		// If the type is just `string` (not a literal), it could be nil at runtime
		if flags&checker.TypeFlagsString != 0 {
			return true
		}
	}

	return t.typeCanBeFalsy(typ)
}

const falsyFlags = checker.TypeFlagsBooleanLike | checker.TypeFlagsUndefined | checker.TypeFlagsNull |
	checker.TypeFlagsVoid | checker.TypeFlagsNever | checker.TypeFlagsUnknown | checker.TypeFlagsAny

// typeCanBeFalsy checks type flags, recursing into unions and type parameters.
func (t *Transpiler) typeCanBeFalsy(typ *checker.Type) bool {
	// Without strictNullChecks, the type checker doesn't track nullability —
	// non-literal types could be nil at runtime even if the type doesn't show it.
	strictNullChecks := t.compilerOptions != nil &&
		(t.compilerOptions.Strict.IsTrue() || t.compilerOptions.StrictNullChecks.IsTrue())
	flags := checker.Type_flags(typ)
	if !strictNullChecks && flags&checker.TypeFlagsLiteral == 0 {
		return true
	}

	if flags&checker.TypeFlagsTypeParameter != 0 {
		base := checker.Checker_getBaseConstraintOfType(t.checker, typ)
		if base == nil {
			return true
		}
		return t.typeCanBeFalsy(base)
	}

	if flags&falsyFlags != 0 {
		return true
	}

	if flags&checker.TypeFlagsUnionOrIntersection != 0 {
		for _, member := range typ.Types() {
			if t.typeCanBeFalsy(member) {
				return true
			}
		}
	}

	return false
}

// checkOnlyTruthyCondition warns when a condition expression can only be truthy in Lua.
// In Lua, only false and nil are falsy — 0, "", etc. are truthy unlike in JS.
func (t *Transpiler) checkOnlyTruthyCondition(condition *ast.Node) {
	if t.compilerOptions == nil {
		return
	}
	// TSTL skips only when strictNullChecks is explicitly false.
	// When unset (default), the warning is still emitted.
	if t.compilerOptions.StrictNullChecks.IsFalse() {
		return
	}
	// Element access expressions are excluded (dynamic keys can't be analyzed)
	if condition.Kind == ast.KindElementAccessExpression {
		return
	}
	typ := t.checker.GetTypeAtLocation(condition)
	if typ == nil {
		return
	}
	if !t.typeCanBeFalsy(typ) {
		t.addWarning(condition, dw.TruthyOnlyConditionalValue,
			"Only false and nil evaluate to 'false' in Lua, everything else is considered 'true'. "+
				"Explicitly compare the value with ===.")
	}
}

// typeof x → __TS__TypeOf(x)
func (t *Transpiler) transformTypeOfExpression(node *ast.Node) lua.Expression {
	te := node.AsTypeOfExpression()
	expr := t.transformExpression(te.Expression)
	fn := t.requireLualib("__TS__TypeOf")
	return lua.Call(lua.Ident(fn), expr)
}

// tryTransformTypeOfBinaryExpression optimizes `typeof x === "type"` to `type(x) == "type"`
func (t *Transpiler) tryTransformTypeOfBinaryExpression(be *ast.BinaryExpression, op ast.Kind) lua.Expression {
	if op != ast.KindEqualsEqualsToken && op != ast.KindEqualsEqualsEqualsToken &&
		op != ast.KindExclamationEqualsToken && op != ast.KindExclamationEqualsEqualsToken {
		return nil
	}

	var typeOfExpr *ast.TypeOfExpression
	var literalExpr *ast.Node

	if be.Left.Kind == ast.KindTypeOfExpression {
		typeOfExpr = be.Left.AsTypeOfExpression()
		literalExpr = be.Right
	} else if be.Right.Kind == ast.KindTypeOfExpression {
		typeOfExpr = be.Right.AsTypeOfExpression()
		literalExpr = be.Left
	} else {
		return nil
	}

	// The compared expression must be a string literal
	compared := t.transformExpression(literalExpr)
	strLit, ok := compared.(*lua.StringLiteral)
	if !ok {
		return nil
	}

	// Map JS type names to Lua type names
	switch strLit.Value {
	case "object":
		strLit = &lua.StringLiteral{Value: "table"}
	case "undefined":
		strLit = &lua.StringLiteral{Value: "nil"}
	}

	inner := t.transformExpression(typeOfExpr.Expression)
	typeCall := lua.Call(lua.Ident("type"), inner)

	var luaOp lua.Operator
	if op == ast.KindEqualsEqualsToken || op == ast.KindEqualsEqualsEqualsToken {
		luaOp = lua.OpEq
	} else {
		luaOp = lua.OpNeq
	}

	return lua.Binary(typeCall, luaOp, strLit)
}

func (t *Transpiler) transformSpreadElement(node *ast.Node) lua.Expression {
	se := node.AsSpreadElement()

	// $vararg language extension: ...$vararg → ... (module-level varargs)
	// Only valid at file scope (not inside functions).
	if kind, ok := t.getExtensionKindForNode(se.Expression); ok && kind == ExtVarargConstant {
		if !t.isInsideFunction() {
			if t.luaTarget.HasVarargDots() {
				return lua.Dots()
			}
			return lua.Call(t.unpackIdent(), lua.Ident("arg"))
		}
		// $vararg inside a function is invalid — fall through to normal spread transform.
		// The $vararg identifier will get TL1010 from the identifier extension check.
	}

	// Optimized vararg spread: if the spread expression is a rest parameter that is
	// only used in spread positions, emit ... directly instead of table.unpack(args).
	if t.isOptimizedVarArgSpread(se.Expression) {
		if t.luaTarget.HasVarargDots() {
			return lua.Dots()
		}
		// Lua 5.0: varargs accessed via implicit `arg` table
		return lua.Call(t.unpackIdent(), lua.Ident("arg"))
	}

	expr := t.transformExpression(se.Expression)

	// Multi-return spread: ...multiCall() → multiCall() (pass through directly)
	if t.isMultiReturnCall(se.Expression) {
		return expr
	}

	// Iterable extension spread: ...iterable → __TS__LuaIteratorSpread(iterable)
	if iterKind, ok := t.getIterableExtensionKindForNode(se.Expression); ok {
		switch iterKind {
		case IterableKindIterable:
			fn := t.requireLualib("__TS__LuaIteratorSpread")
			return lua.Call(lua.Ident(fn), expr)
		case IterableKindPairs:
			entriesFn := t.requireLualib("__TS__ObjectEntries")
			return lua.Call(t.unpackIdent(), lua.Call(lua.Ident(entriesFn), expr))
		case IterableKindPairsKey:
			keysFn := t.requireLualib("__TS__ObjectKeys")
			return lua.Call(t.unpackIdent(), lua.Call(lua.Ident(keysFn), expr))
		}
	}

	// If the expression is definitely an array type, use unpack directly.
	// Otherwise, use __TS__Spread which handles strings, iterables, and union types.
	if t.isArrayType(se.Expression) {
		return lua.Call(t.unpackIdent(), expr)
	}
	fn := t.requireLualib("__TS__Spread")
	return lua.Call(lua.Ident(fn), expr)
}

// transformOrderedExpressions transforms a list of expressions while preserving
// JS left-to-right evaluation order. If expression N has preceding statements,
// expressions 0..N-1 are cached in temp variables to prevent reordering.
func (t *Transpiler) transformOrderedExpressions(nodes []*ast.Node) []lua.Expression {
	n := len(nodes)
	if n == 0 {
		return nil
	}
	exprs := make([]lua.Expression, n)
	precs := make([][]lua.Statement, n)
	lastPrecIdx := -1
	for i, a := range nodes {
		exprs[i], precs[i] = t.transformExprInScope(a)
		if len(precs[i]) > 0 {
			lastPrecIdx = i
		}
	}
	return t.emitOrderedPrecedingStatements(exprs, precs, lastPrecIdx, nodes...)
}

// emitOrderedPrecedingStatements emits accumulated preceding statements in order,
// caching earlier expressions to temps when later expressions have side effects.
// When tsNodes is provided, const identifiers are skipped (they can't be reassigned).
func (t *Transpiler) emitOrderedPrecedingStatements(exprs []lua.Expression, precs [][]lua.Statement, lastPrecIdx int, tsNodes ...*ast.Node) []lua.Expression {
	if lastPrecIdx < 0 {
		return exprs
	}
	for i := range exprs {
		t.addPrecedingStatements(precs[i]...)
		if i < lastPrecIdx {
			var tsNode *ast.Node
			if i < len(tsNodes) {
				tsNode = tsNodes[i]
			}
			exprs[i] = t.moveToPrecedingTempWithNode(exprs[i], tsNode)
		}
	}
	return exprs
}

// transformExpressionList transforms a list of expressions while handling mid-list spread
// and preserving execution order. Used for call arguments and array literals.
func (t *Transpiler) transformExpressionList(nodes []*ast.Node) []lua.Expression {
	if len(nodes) == 0 {
		return nil
	}

	n := len(nodes)
	exprs := make([]lua.Expression, n)
	precs := make([][]lua.Statement, n)

	// Transform each expression in its own scope
	lastPrecIdx := -1
	for i, a := range nodes {
		exprs[i], precs[i] = t.transformExprInScope(a)
		if len(precs[i]) > 0 {
			lastPrecIdx = i
		}
	}

	// Check for non-tail spread — requires SparseArray pattern
	hasNonTailSpread := false
	for i, node := range nodes {
		if node.Kind == ast.KindSpreadElement && i < n-1 {
			hasNonTailSpread = true
			break
		}
	}

	// Also use SparseArray when too many temps would be needed for execution order
	maxTemps := 2
	tempsNeeded := 0
	if lastPrecIdx > 0 {
		for i := 0; i < lastPrecIdx; i++ {
			if t.shouldMoveToTempWithNode(exprs[i], nodes[i]) {
				tempsNeeded++
			}
		}
	}

	if hasNonTailSpread || tempsNeeded > maxTemps {
		return t.transformExpressionsUsingSparseArray(nodes, exprs, precs)
	}

	return t.emitOrderedPrecedingStatements(exprs, precs, lastPrecIdx, nodes...)
}

// transformExpressionsUsingSparseArray handles expression lists with mid-list spread
// or excessive preceding statements by collecting into a SparseArray.
func (t *Transpiler) transformExpressionsUsingSparseArray(
	nodes []*ast.Node,
	exprs []lua.Expression,
	precs [][]lua.Statement,
) []lua.Expression {
	sparseNew := t.requireLualib("__TS__SparseArrayNew")
	sparsePush := t.requireLualib("__TS__SparseArrayPush")
	sparseSpread := t.requireLualib("__TS__SparseArraySpread")

	tempName := t.nextTemp("array")
	temp := lua.Ident(tempName)
	var batch []lua.Expression
	first := true

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if first {
			t.addPrecedingStatements(lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(tempName)},
				[]lua.Expression{lua.Call(lua.Ident(sparseNew), batch...)},
			))
			first = false
		} else {
			args := append([]lua.Expression{temp}, batch...)
			t.addPrecedingStatements(lua.ExprStmt(lua.Call(lua.Ident(sparsePush), args...)))
		}
		batch = nil
	}

	for i := range nodes {
		// Expressions with preceding statements should start a new batch
		if len(precs[i]) > 0 && len(batch) > 0 {
			flush()
		}
		t.addPrecedingStatements(precs[i]...)
		batch = append(batch, exprs[i])

		// Spread expressions should end a batch
		if nodes[i].Kind == ast.KindSpreadElement {
			flush()
		}
	}
	flush()

	return []lua.Expression{lua.Call(lua.Ident(sparseSpread), temp)}
}

func (t *Transpiler) transformArgExprs(args *ast.NodeList) []lua.Expression {
	if args == nil || len(args.Nodes) == 0 {
		return nil
	}
	return t.transformExpressionList(args.Nodes)
}

// transformArgsInScope transforms call arguments in an isolated preceding statement scope,
// capturing any setup statements the arguments generate. This is the centralized pattern
// from TSTL's transformCallAndArguments: isolate arg side effects so the callee can be
// cached before they execute.
func (t *Transpiler) transformArgsInScope(args *ast.NodeList) ([]lua.Expression, []lua.Statement) {
	t.pushPrecedingStatements()
	exprs := t.transformArgExprs(args)
	prec := t.popPrecedingStatements()
	return exprs, prec
}

// shouldMoveToTemp returns whether an expression needs to be cached in a temp variable
// to preserve evaluation order when later expressions have side effects.
// luaExprHasSideEffect returns true if evaluating the expression could have side effects.
// Literals and identifiers are safe; everything else (calls, table access, etc.) might not be.
func luaExprHasSideEffect(expr lua.Expression) bool {
	switch expr.(type) {
	case *lua.NumericLiteral, *lua.StringLiteral, *lua.NilLiteral, *lua.BooleanLiteral, *lua.Identifier:
		return false
	}
	return true
}

func (t *Transpiler) shouldMoveToTemp(expr lua.Expression) bool {
	switch e := expr.(type) {
	case *lua.NumericLiteral, *lua.StringLiteral, *lua.NilLiteral, *lua.BooleanLiteral:
		return false
	case *lua.Identifier:
		// Generated temp variables (____xxx) are effectively constants
		return !strings.HasPrefix(e.Text, "____")
	}
	return true
}

// shouldMoveToTempWithNode is like shouldMoveToTemp but also considers the original
// TS node. A const identifier doesn't need caching since it can't be reassigned.
func (t *Transpiler) shouldMoveToTempWithNode(expr lua.Expression, tsOriginal *ast.Node) bool {
	if !t.shouldMoveToTemp(expr) {
		return false
	}
	if tsOriginal != nil {
		if t.isConstIdentifier(tsOriginal) {
			return false
		}
		if tsOriginal.Kind == ast.KindThisKeyword {
			return false
		}
	}
	return true
}

// isConstIdentifier checks if a TS node is an identifier declared with const.
func (t *Transpiler) isConstIdentifier(node *ast.Node) bool {
	if node == nil {
		return false
	}
	ident := node
	if ident.Kind == ast.KindComputedPropertyName {
		ident = ident.AsComputedPropertyName().Expression
	}
	if ident.Kind != ast.KindIdentifier {
		return false
	}
	sym := t.checker.GetSymbolAtLocation(ident)
	if sym == nil || len(sym.Declarations) == 0 {
		return false
	}
	for _, d := range sym.Declarations {
		if d.Parent != nil && d.Parent.Kind == ast.KindVariableDeclarationList {
			if d.Parent.Flags&ast.NodeFlagsConst != 0 {
				return true
			}
		}
	}
	return false
}

// tempNameForLuaExpression extracts a descriptive name from a Lua expression for
// use as a temp variable prefix. Returns "" if no descriptive name can be derived.
func tempNameForLuaExpression(expr lua.Expression) string {
	switch e := expr.(type) {
	case *lua.StringLiteral:
		return e.Value
	case *lua.NumericLiteral:
		return "_" + e.Value
	case *lua.Identifier:
		name := e.Text
		name = strings.TrimPrefix(name, "____")
		return name
	case *lua.CallExpression:
		if name := tempNameForLuaExpression(e.Expression); name != "" {
			return name + "_result"
		}
	case *lua.TableIndexExpression:
		tableName := tempNameForLuaExpression(e.Table)
		indexName := tempNameForLuaExpression(e.Index)
		if tableName != "" || indexName != "" {
			if tableName == "" {
				tableName = "table"
			}
			if indexName == "" {
				indexName = "index"
			}
			return tableName + "_" + indexName
		}
	}
	return ""
}

// moveToPrecedingTemp caches an expression in a temp variable via a preceding statement
// and returns a reference to that temp. Skips simple identifiers/literals that don't need caching.
func (t *Transpiler) moveToPrecedingTemp(expr lua.Expression) lua.Expression {
	return t.moveToPrecedingTempWithNode(expr, nil)
}

// moveToPrecedingTempWithNode caches an expression in a temp variable via a preceding statement.
// When tsOriginal is provided, const identifiers are skipped.
func (t *Transpiler) moveToPrecedingTempWithNode(expr lua.Expression, tsOriginal *ast.Node) lua.Expression {
	if tsOriginal != nil {
		if !t.shouldMoveToTempWithNode(expr, tsOriginal) {
			return expr
		}
	} else if !t.shouldMoveToTemp(expr) {
		return expr
	}
	prefix := tempNameForLuaExpression(expr)
	if prefix == "" {
		prefix = "temp"
	}
	temp := t.nextTemp(prefix)
	t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{expr}))
	return lua.Ident(temp)
}

// superBaseExpression returns the Lua expression for the super class base.
func (t *Transpiler) superBaseExpression() lua.Expression {
	var base lua.Expression
	if t.currentBaseClassName != "" {
		base = lua.Ident(t.currentBaseClassName)
	} else if t.currentClassRef != nil {
		base = lua.Index(t.currentClassRef, lua.Str("____super"))
	} else {
		base = memberAccess(lua.Ident("self"), "____super")
	}
	return lua.Index(base, lua.Str("prototype"))
}

// isSuperGetAccessor checks if a node's symbol refers to a get accessor.
func (t *Transpiler) isSuperGetAccessor(node *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(node)
	return sym != nil && sym.Flags&ast.SymbolFlagsGetAccessor != 0
}

// isSuperSetAccessor checks if a node's symbol refers to a set accessor.
func (t *Transpiler) isSuperSetAccessor(node *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(node)
	return sym != nil && sym.Flags&ast.SymbolFlagsSetAccessor != 0
}
