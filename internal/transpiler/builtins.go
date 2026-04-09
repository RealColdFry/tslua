// Handles translation of JS built-in methods and objects to Lua equivalents.
package transpiler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// tryTransformBuiltinCall checks if a call expression is a known JS built-in
// and returns the Lua equivalent. Returns nil if not a built-in.
// tryTransformBuiltinCallWithArgs checks if a property access call is a known builtin,
// using pre-transformed arguments and receiver. Returns nil if not a builtin.
func (t *Transpiler) tryTransformBuiltinCallWithArgs(ce *ast.CallExpression, argExprs []lua.Expression, objExpr lua.Expression) lua.Expression {
	if ce.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil
	}
	pa := ce.Expression.AsPropertyAccessExpression()
	if pa.Name().Kind != ast.KindIdentifier {
		return nil
	}
	method := pa.Name().AsIdentifier().Text

	// Global object methods: Math.*, Object.*, Array.*
	if pa.Expression.Kind == ast.KindIdentifier {
		obj := pa.Expression.AsIdentifier().Text
		if result := t.tryTransformGlobalCall(obj, method, argExprs, ce.Arguments); result != nil {
			return result
		}
	}

	// Instance method calls
	if result := t.tryTransformMethodCall(pa, objExpr, method, argExprs, ce.Arguments, ce.AsNode()); result != nil {
		return result
	}

	return nil
}

// isBuiltinCall returns true if the call expression would be handled as a builtin transform.
// This is a lightweight check that doesn't perform the actual transform or cause side effects.
func (t *Transpiler) isBuiltinCall(ce *ast.CallExpression) bool {
	// Bare global function calls: Number(), String(), parseInt(), etc.
	if ce.Expression.Kind == ast.KindIdentifier {
		switch ce.Expression.AsIdentifier().Text {
		case "parseInt", "parseFloat", "isNaN", "isFinite", "Number", "Symbol":
			return true
		}
		return t.isStringConstructorCall(ce)
	}

	if ce.Expression.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	pa := ce.Expression.AsPropertyAccessExpression()
	if pa.Name().Kind != ast.KindIdentifier {
		return false
	}
	method := pa.Name().AsIdentifier().Text

	// Global object methods: Math.*, Object.*, Array.*, console.*, etc.
	if pa.Expression.Kind == ast.KindIdentifier {
		switch pa.Expression.AsIdentifier().Text {
		case "Math", "Object", "Array", "console", "Symbol", "Number", "String", "Promise":
			return true
		case "Map":
			return method == "groupBy"
		}
	}

	// Instance method calls: type-directed
	if t.isArrayType(pa.Expression) || t.isStringExpression(pa.Expression) ||
		t.isNumericExpression(pa.Expression) || t.isMapOrSetType(pa.Expression) {
		return true
	}

	// Object prototype methods
	if method == "toString" || method == "hasOwnProperty" {
		return true
	}

	return false
}

// tryTransformGlobalCall handles Math.*, Object.*, Array.*, console.*, etc.
func (t *Transpiler) tryTransformGlobalCall(obj, method string, argExprs []lua.Expression, argNodes *ast.NodeList) lua.Expression {
	switch obj {
	case "Math":
		return t.transformMathCall(method, argExprs)
	case "Object":
		return t.transformObjectCall(method, argExprs)
	case "Array":
		return t.transformArrayStaticCall(method, argExprs)
	case "console":
		return t.transformConsoleCall(method, argExprs, argNodes)
	case "Symbol":
		return t.transformSymbolCall(method, argExprs)
	case "Map":
		if method == "groupBy" {
			fn := t.requireLualib("__TS__MapGroupBy")
			return lualibCall(fn, argExprs...)
		}
	case "Number":
		return t.transformNumberCall(method, argExprs)
	case "String":
		return t.transformStringStaticCall(method, argExprs)
	case "Promise":
		return t.transformPromiseStaticCall(method, argExprs)
	}
	return nil
}

func isStringFormatTemplate(argNodes *ast.NodeList, index int) bool {
	if argNodes == nil || index >= len(argNodes.Nodes) {
		return false
	}
	node := argNodes.Nodes[index]
	return node.Kind == ast.KindStringLiteral && strings.Contains(node.AsStringLiteral().Text, "%")
}

func (t *Transpiler) transformConsoleCall(method string, argExprs []lua.Expression, argNodes *ast.NodeList) lua.Expression {
	stringFormatCall := func(args []lua.Expression) lua.Expression {
		return lua.Call(memberAccess(lua.Ident("string"), "format"), args...)
	}

	switch method {
	case "log", "warn", "info", "error":
		if isStringFormatTemplate(argNodes, 0) {
			return lua.Call(lua.Ident("print"), stringFormatCall(argExprs))
		}
		return lua.Call(lua.Ident("print"), argExprs...)
	case "assert":
		if isStringFormatTemplate(argNodes, 1) {
			return lua.Call(lua.Ident("assert"), argExprs[0], stringFormatCall(argExprs[1:]))
		}
		return lua.Call(lua.Ident("assert"), argExprs...)
	case "trace":
		traceArgs := argExprs
		if isStringFormatTemplate(argNodes, 0) {
			traceArgs = []lua.Expression{stringFormatCall(argExprs)}
		}
		return lua.Call(lua.Ident("print"), lua.Call(memberAccess(lua.Ident("debug"), "traceback"), traceArgs...))
	}
	return nil
}

func (t *Transpiler) transformSymbolCall(method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "for":
		fn := t.requireLualib("__TS__SymbolRegistryFor")
		return lualibCall(fn, argExprs...)
	case "keyFor":
		fn := t.requireLualib("__TS__SymbolRegistryKeyFor")
		return lualibCall(fn, argExprs...)
	}
	return nil
}

func (t *Transpiler) transformPromiseStaticCall(method string, argExprs []lua.Expression) lua.Expression {
	promiseName := t.requireLualib("__TS__Promise")
	switch method {
	case "resolve", "reject":
		return lua.Call(lua.Index(lua.Ident(promiseName), lua.Str(method)), argExprs...)
	case "all":
		fn := t.requireLualib("__TS__PromiseAll")
		return lualibCall(fn, argExprs...)
	case "allSettled":
		fn := t.requireLualib("__TS__PromiseAllSettled")
		return lualibCall(fn, argExprs...)
	case "any":
		fn := t.requireLualib("__TS__PromiseAny")
		return lualibCall(fn, argExprs...)
	case "race":
		fn := t.requireLualib("__TS__PromiseRace")
		return lualibCall(fn, argExprs...)
	}
	return nil
}

func (t *Transpiler) transformStringStaticCall(method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "fromCharCode":
		return lua.Call(memberAccess(lua.Ident("string"), "char"), argExprs...)
	}
	return nil
}

// mathDotCall returns lua.Call(math.method, argExprs...)
func mathDotCall(method string, argExprs []lua.Expression) *lua.CallExpression {
	return lua.Call(lua.Index(lua.Ident("math"), lua.Str(method)), argExprs...)
}

// lualibCall returns lua.Call(lua.Ident(fnName), args...)
func lualibCall(fnName string, args ...lua.Expression) *lua.CallExpression {
	return lua.Call(lua.Ident(fnName), args...)
}

// coerceNilArgsToZero replaces nil literals with 0 in argument lists.
// In JS, null coerces to 0 in numeric contexts (e.g. string.slice(3, null) → slice(3, 0)).
func coerceNilArgsToZero(args []lua.Expression) []lua.Expression {
	result := make([]lua.Expression, len(args))
	for i, a := range args {
		if _, ok := a.(*lua.NilLiteral); ok {
			result[i] = lua.Num("0")
		} else {
			result[i] = a
		}
	}
	return result
}

func (t *Transpiler) transformMathCall(method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "abs", "ceil", "floor", "log", "max", "min", "random", "sin", "cos",
		"tan", "asin", "acos", "atan", "sqrt", "exp":
		return mathDotCall(method, argExprs)
	case "round":
		// math.floor(x + 0.5) — matches TSTL's inline approach
		if len(argExprs) == 1 {
			return lua.Call(lua.Index(lua.Ident("math"), lua.Str("floor")), lua.Binary(argExprs[0], lua.OpAdd, lua.Num("0.5")))
		}
		return mathDotCall("floor", argExprs)
	case "pow":
		// Math.pow(x, y) → x ^ y (Lua's ^ operator works on all versions)
		if len(argExprs) >= 2 {
			return lua.Binary(argExprs[0], lua.OpPow, argExprs[1])
		}
		return mathDotCall("pow", argExprs)
	case "sign":
		fn := t.requireLualib("__TS__MathSign")
		return lualibCall(fn, argExprs...)
	case "trunc":
		fn := t.requireLualib("__TS__MathTrunc")
		return lualibCall(fn, argExprs...)
	case "clamp":
		// math.max(math.min(args...))
		return lua.Call(memberAccess(lua.Ident("math"), "max"), lua.Call(memberAccess(lua.Ident("math"), "min"), argExprs...))
	case "PI":
		return memberAccess(lua.Ident("math"), "pi")
	case "atan2":
		// Universal: lualib polyfill; others: target-appropriate math.atan or math.atan2.
		// TSTL bug: math.atan2 handling is wrong on Lua 5.4+
		if t.luaTarget == LuaTargetUniversal {
			fn := t.requireLualib("__TS__MathAtan2")
			return lualibCall(fn, argExprs...)
		}
		return mathDotCall(t.luaTarget.MathAtan2Name(), argExprs)
	case "log10":
		// math.log(x) / math.log(10)
		if len(argExprs) >= 1 {
			return lua.Binary(mathDotCall("log", argExprs[:1]), lua.OpDiv, lua.Num("2.302585092994046"))
		}
	case "log2":
		// math.log(x) / math.log(2)
		if len(argExprs) >= 1 {
			return lua.Binary(mathDotCall("log", argExprs[:1]), lua.OpDiv, lua.Num("0.6931471805599453"))
		}
	case "log1p":
		// math.log(1 + x)
		if len(argExprs) >= 1 {
			return lua.Call(memberAccess(lua.Ident("math"), "log"), lua.Binary(lua.Num("1"), lua.OpAdd, argExprs[0]))
		}
	}
	return mathDotCall(method, argExprs)
}

func (t *Transpiler) transformObjectCall(method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "keys":
		fn := t.requireLualib("__TS__ObjectKeys")
		return lualibCall(fn, argExprs...)
	case "values":
		fn := t.requireLualib("__TS__ObjectValues")
		return lualibCall(fn, argExprs...)
	case "entries":
		fn := t.requireLualib("__TS__ObjectEntries")
		return lualibCall(fn, argExprs...)
	case "assign":
		fn := t.requireLualib("__TS__ObjectAssign")
		return lualibCall(fn, argExprs...)
	case "fromEntries":
		fn := t.requireLualib("__TS__ObjectFromEntries")
		return lualibCall(fn, argExprs...)
	case "freeze":
		if len(argExprs) == 1 {
			return argExprs[0] // no-op in Lua (no freeze support)
		}
		return argExprs[0]
	case "groupBy":
		fn := t.requireLualib("__TS__ObjectGroupBy")
		return lualibCall(fn, argExprs...)
	case "defineProperty":
		fn := t.requireLualib("__TS__ObjectDefineProperty")
		return lualibCall(fn, argExprs...)
	case "getOwnPropertyDescriptor":
		fn := t.requireLualib("__TS__ObjectGetOwnPropertyDescriptor")
		return lualibCall(fn, argExprs...)
	case "getOwnPropertyDescriptors":
		fn := t.requireLualib("__TS__ObjectGetOwnPropertyDescriptors")
		return lualibCall(fn, argExprs...)
	}
	return nil
}

func (t *Transpiler) transformArrayStaticCall(method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "isArray":
		fn := t.requireLualib("__TS__ArrayIsArray")
		return lualibCall(fn, argExprs...)
	case "from":
		fn := t.requireLualib("__TS__ArrayFrom")
		return lualibCall(fn, argExprs...)
	case "of":
		// Array.of(1,2,3) → {1,2,3}
		var fields []*lua.TableFieldExpression
		for _, a := range argExprs {
			fields = append(fields, lua.Field(a))
		}
		return lua.Table(fields...)
	}
	return nil
}

// tryTransformMethodCall handles instance method calls on arrays, strings, Maps, Sets, etc.
func (t *Transpiler) tryTransformMethodCall(pa *ast.PropertyAccessExpression, objExpr lua.Expression, method string, argExprs []lua.Expression, argNodes *ast.NodeList, callNode *ast.Node) lua.Expression {
	// Type-directed: check if the object is an array
	if t.isArrayType(pa.Expression) {
		return t.transformArrayMethodCall(objExpr, method, argExprs, argNodes, callNode, pa.Expression)
	}

	// Check if it's a string type
	if t.isStringExpression(pa.Expression) {
		return t.transformStringMethodCall(objExpr, method, argExprs)
	}

	// Number instance methods: .toFixed(), .toString(radix)
	if t.isNumericExpression(pa.Expression) {
		return t.transformNumberInstanceCall(objExpr, method, argExprs)
	}

	// Map/Set: type-directed check
	if t.isMapOrSetType(pa.Expression) {
		return t.transformMapSetMethodCall(objExpr, method, argExprs)
	}

	// Object prototype methods — fallback for any type
	switch method {
	case "toString":
		return lua.Call(lua.Ident("tostring"), objExpr)
	case "hasOwnProperty":
		if len(argExprs) > 0 {
			return lua.Binary(lua.Call(lua.Ident("rawget"), objExpr, argExprs[0]), lua.OpNeq, lua.Nil())
		}
	}

	return nil
}

func (t *Transpiler) transformArrayMethodCall(objExpr lua.Expression, method string, argExprs []lua.Expression, argNodes *ast.NodeList, callNode *ast.Node, arrayTSNode *ast.Node) lua.Expression {
	allArgs := append([]lua.Expression{objExpr}, argExprs...)

	switch method {
	case "push":
		if argNodes != nil && len(argNodes.Nodes) == 1 {
			arg := argNodes.Nodes[0]
			// Single spread arg: arr.push(...items) → __TS__ArrayPushArray(arr, items)
			if arg.Kind == ast.KindSpreadElement {
				innerExpr := t.transformExpression(arg.AsSpreadElement().Expression)
				if t.isOptimizedVarArgSpread(arg.AsSpreadElement().Expression) {
					// Optimized vararg: use __TS__ArrayPush with ... directly
					fn := t.requireLualib("__TS__ArrayPush")
					if t.luaTarget.HasVarargDots() {
						return lualibCall(fn, objExpr, lua.Dots())
					}
					return lualibCall(fn, objExpr, lua.Call(t.unpackIdent(), lua.Ident("arg")))
				}
				fn := t.requireLualib("__TS__ArrayPushArray")
				return lualibCall(fn, objExpr, innerExpr)
			}
		}
		// Check if any arg is a spread — use __TS__ArrayPush which handles varargs
		if argNodes != nil {
			hasSpread := false
			for _, arg := range argNodes.Nodes {
				if arg.Kind == ast.KindSpreadElement {
					hasSpread = true
					break
				}
			}
			if hasSpread {
				fn := t.requireLualib("__TS__ArrayPush")
				return lualibCall(fn, allArgs...)
			}
		}
		// Single-arg push: arr[#arr + 1] = val
		// When return value is used, cache #arr+1 to temp and return it
		if len(argExprs) == 1 {
			val := argExprs[0]
			lenExpr := lua.Binary(t.luaTarget.LenExpr(objExpr), lua.OpAdd, lua.Num("1"))
			// Check if the push result is used (parent is not ExpressionStatement)
			resultUsed := callNode != nil && callNode.Parent != nil &&
				callNode.Parent.Kind != ast.KindExpressionStatement
			if resultUsed {
				temp := t.moveToPrecedingTemp(lenExpr)
				t.addPrecedingStatements(lua.Assign(
					[]lua.Expression{lua.Index(objExpr, temp)},
					[]lua.Expression{val},
				))
				return temp
			}
			t.addPrecedingStatements(lua.Assign(
				[]lua.Expression{lua.Index(objExpr, lenExpr)},
				[]lua.Expression{val},
			))
			return lua.Nil()
		}
		fn := t.requireLualib("__TS__ArrayPush")
		return lualibCall(fn, allArgs...)
	case "pop":
		return lua.Call(lua.Index(lua.Ident("table"), lua.Str("remove")), objExpr)
	case "shift":
		return lua.Call(lua.Index(lua.Ident("table"), lua.Str("remove")), objExpr, lua.Num("1"))
	case "unshift":
		fn := t.requireLualib("__TS__ArrayUnshift")
		return lualibCall(fn, allArgs...)
	case "splice":
		fn := t.requireLualib("__TS__ArraySplice")
		return lualibCall(fn, allArgs...)
	case "slice":
		fn := t.requireLualib("__TS__ArraySlice")
		return lualibCall(fn, allArgs...)
	case "concat":
		fn := t.requireLualib("__TS__ArrayConcat")
		return lualibCall(fn, allArgs...)
	case "join":
		// Optimization: when element type is string|number, emit table.concat() directly.
		// Ported from: TSTL builtins/array.ts join (lines 161-186)
		if t.isElementTypeStringOrNumber(arrayTSNode) {
			defaultSep := lua.Str(",")
			var sep lua.Expression
			if len(argExprs) == 0 {
				sep = defaultSep
			} else if _, ok := argExprs[0].(*lua.StringLiteral); ok {
				sep = argExprs[0]
			} else {
				sep = lua.Binary(argExprs[0], lua.OpOr, defaultSep)
			}
			return lua.Call(lua.Index(lua.Ident("table"), lua.Str("concat")), objExpr, sep)
		}
		fn := t.requireLualib("__TS__ArrayJoin")
		if len(argExprs) == 0 {
			return lualibCall(fn, objExpr)
		}
		return lualibCall(fn, objExpr, argExprs[0])
	case "reverse":
		fn := t.requireLualib("__TS__ArrayReverse")
		return lualibCall(fn, objExpr)
	case "sort":
		fn := t.requireLualib("__TS__ArraySort")
		return lualibCall(fn, allArgs...)
	case "indexOf":
		fn := t.requireLualib("__TS__ArrayIndexOf")
		return lualibCall(fn, allArgs...)
	case "includes":
		fn := t.requireLualib("__TS__ArrayIncludes")
		return lualibCall(fn, allArgs...)
	case "find":
		fn := t.requireLualib("__TS__ArrayFind")
		return lualibCall(fn, allArgs...)
	case "findIndex":
		fn := t.requireLualib("__TS__ArrayFindIndex")
		return lualibCall(fn, allArgs...)
	case "map":
		fn := t.requireLualib("__TS__ArrayMap")
		return lualibCall(fn, allArgs...)
	case "filter":
		fn := t.requireLualib("__TS__ArrayFilter")
		return lualibCall(fn, allArgs...)
	case "reduce":
		fn := t.requireLualib("__TS__ArrayReduce")
		return lualibCall(fn, allArgs...)
	case "forEach":
		fn := t.requireLualib("__TS__ArrayForEach")
		return lualibCall(fn, allArgs...)
	case "some":
		fn := t.requireLualib("__TS__ArraySome")
		return lualibCall(fn, allArgs...)
	case "every":
		fn := t.requireLualib("__TS__ArrayEvery")
		return lualibCall(fn, allArgs...)
	case "flat":
		fn := t.requireLualib("__TS__ArrayFlat")
		return lualibCall(fn, allArgs...)
	case "flatMap":
		fn := t.requireLualib("__TS__ArrayFlatMap")
		return lualibCall(fn, allArgs...)
	case "fill":
		fn := t.requireLualib("__TS__ArrayFill")
		return lualibCall(fn, allArgs...)
	case "at":
		fn := t.requireLualib("__TS__ArrayAt")
		return lualibCall(fn, allArgs...)
	case "entries":
		fn := t.requireLualib("__TS__ArrayEntries")
		return lualibCall(fn, objExpr)
	case "reduceRight":
		fn := t.requireLualib("__TS__ArrayReduceRight")
		return lualibCall(fn, allArgs...)
	case "toReversed":
		fn := t.requireLualib("__TS__ArrayToReversed")
		return lualibCall(fn, objExpr)
	case "toSorted":
		fn := t.requireLualib("__TS__ArrayToSorted")
		return lualibCall(fn, allArgs...)
	case "toSpliced":
		fn := t.requireLualib("__TS__ArrayToSpliced")
		return lualibCall(fn, allArgs...)
	case "with":
		fn := t.requireLualib("__TS__ArrayWith")
		return lualibCall(fn, allArgs...)
	case "length":
		return t.luaTarget.LenExpr(objExpr)
	}
	return nil
}

func (t *Transpiler) transformStringMethodCall(objExpr lua.Expression, method string, argExprs []lua.Expression) lua.Expression {
	allArgs := append([]lua.Expression{objExpr}, argExprs...)

	switch method {
	case "toLowerCase":
		return lua.Call(lua.Index(lua.Ident("string"), lua.Str("lower")), objExpr)
	case "toUpperCase":
		return lua.Call(lua.Index(lua.Ident("string"), lua.Str("upper")), objExpr)
	case "substring":
		fn := t.requireLualib("__TS__StringSubstring")
		return lualibCall(fn, coerceNilArgsToZero(allArgs)...)
	case "slice":
		// Inline string.sub when both args are non-negative numeric literals
		if len(argExprs) > 0 {
			if _, val1, ok := getNonNegativeNumericLiteral(argExprs[0]); ok {
				plusOne1 := lua.Num(strconv.FormatFloat(val1+1, 'f', -1, 64))
				subArgs := []lua.Expression{objExpr, plusOne1}
				canInline := true
				if len(argExprs) > 1 {
					if _, val2, ok := getNonNegativeNumericLiteral(argExprs[1]); ok {
						subArgs = append(subArgs, lua.Num(strconv.FormatFloat(val2, 'f', -1, 64)))
					} else {
						canInline = false
					}
				}
				if canInline {
					return lua.Call(memberAccess(lua.Ident("string"), "sub"), subArgs...)
				}
			}
		}
		fn := t.requireLualib("__TS__StringSlice")
		return lualibCall(fn, coerceNilArgsToZero(allArgs)...)
	case "substr":
		fn := t.requireLualib("__TS__StringSubstr")
		return lualibCall(fn, coerceNilArgsToZero(allArgs)...)
	case "concat":
		// table.concat({str, arg1, arg2, ...})
		tableArgs := append([]lua.Expression{objExpr}, argExprs...)
		var fields []*lua.TableFieldExpression
		for _, a := range tableArgs {
			fields = append(fields, lua.Field(a))
		}
		return lua.Call(memberAccess(lua.Ident("table"), "concat"), lua.Table(fields...))
	case "at":
		fn := t.requireLualib("__TS__StringAt")
		return lualibCall(fn, allArgs...)
	case "toString":
		return lua.Call(lua.Ident("tostring"), objExpr)
	case "indexOf":
		// string.find(str, searchVal, offset, true) or 0) - 1
		// The `true` flag disables pattern matching (plain search)
		var searchVal lua.Expression
		if len(argExprs) > 0 {
			searchVal = argExprs[0]
		} else {
			searchVal = lua.Str("undefined")
		}
		var offset lua.Expression
		if len(argExprs) > 1 {
			// string.find uses 1-based index; negative offsets mean "same as 0" for indexOf
			// Fold literal addition: addToNumericExpression(params[1], 1)
			offsetPlusOne := addToNumericExpression(argExprs[1], 1)
			offset = lua.Call(memberAccess(lua.Ident("math"), "max"), offsetPlusOne, lua.Num("1"))
		} else {
			offset = lua.Nil()
		}
		findCall := lua.Call(memberAccess(lua.Ident("string"), "find"), objExpr, searchVal, offset, lua.Bool(true))
		return lua.Binary(lua.Binary(findCall, lua.OpOr, lua.Num("0")), lua.OpSub, lua.Num("1"))
	case "includes":
		fn := t.requireLualib("__TS__StringIncludes")
		return lualibCall(fn, allArgs...)
	case "startsWith":
		fn := t.requireLualib("__TS__StringStartsWith")
		return lualibCall(fn, allArgs...)
	case "endsWith":
		fn := t.requireLualib("__TS__StringEndsWith")
		return lualibCall(fn, allArgs...)
	case "replace":
		fn := t.requireLualib("__TS__StringReplace")
		return lualibCall(fn, allArgs...)
	case "replaceAll":
		fn := t.requireLualib("__TS__StringReplaceAll")
		return lualibCall(fn, allArgs...)
	case "split":
		fn := t.requireLualib("__TS__StringSplit")
		return lualibCall(fn, allArgs...)
	case "trim":
		fn := t.requireLualib("__TS__StringTrim")
		return lualibCall(fn, objExpr)
	case "trimStart", "trimLeft":
		fn := t.requireLualib("__TS__StringTrimStart")
		return lualibCall(fn, objExpr)
	case "trimEnd", "trimRight":
		fn := t.requireLualib("__TS__StringTrimEnd")
		return lualibCall(fn, objExpr)
	case "padStart":
		fn := t.requireLualib("__TS__StringPadStart")
		return lualibCall(fn, allArgs...)
	case "padEnd":
		fn := t.requireLualib("__TS__StringPadEnd")
		return lualibCall(fn, allArgs...)
	case "repeat":
		// JS .repeat() floors the count; Lua string.rep requires integer
		var count lua.Expression
		if len(argExprs) > 0 {
			count = argExprs[0]
		} else {
			count = lua.Num("0")
		}
		flooredCount := lua.Call(memberAccess(lua.Ident("math"), "floor"), count)
		return lua.Call(lua.Index(lua.Ident("string"), lua.Str("rep")), objExpr, flooredCount)
	case "charAt":
		// Inline string.sub when index is a non-negative numeric literal
		if len(argExprs) > 0 {
			if _, val, ok := getNonNegativeNumericLiteral(argExprs[0]); ok {
				plusOne := lua.Num(strconv.FormatFloat(val+1, 'f', -1, 64))
				return lua.Call(memberAccess(lua.Ident("string"), "sub"), objExpr, plusOne, plusOne)
			}
		}
		fn := t.requireLualib("__TS__StringCharAt")
		return lualibCall(fn, allArgs...)
	case "charCodeAt":
		// Inline string.byte when index is a non-negative numeric literal
		if len(argExprs) > 0 {
			if _, val, ok := getNonNegativeNumericLiteral(argExprs[0]); ok {
				plusOne := lua.Num(strconv.FormatFloat(val+1, 'f', -1, 64))
				byteCall := lua.Call(memberAccess(lua.Ident("string"), "byte"), objExpr, plusOne)
				nan := lua.Binary(lua.Num("0"), lua.OpDiv, lua.Num("0"))
				return lua.Binary(byteCall, lua.OpOr, nan)
			}
		}
		fn := t.requireLualib("__TS__StringCharCodeAt")
		return lualibCall(fn, allArgs...)
	case "match":
		fn := t.requireLualib("__TS__StringMatch")
		return lualibCall(fn, allArgs...)
	}
	return nil
}

// Map and Set method calls
func (t *Transpiler) transformMapSetMethodCall(objExpr lua.Expression, method string, argExprs []lua.Expression) lua.Expression {
	mc := func(m string, args []lua.Expression) lua.Expression {
		return t.wrapOptionalChainCall(objExpr, func(inner lua.Expression) lua.Expression {
			return lua.MethodCall(inner, m, args...)
		})
	}
	switch method {
	case "get":
		return mc("get", argExprs)
	case "set":
		return mc("set", argExprs)
	case "has":
		return mc("has", argExprs)
	case "delete":
		return mc("delete", argExprs)
	case "clear":
		return mc("clear", nil)
	case "size":
		return lua.Index(objExpr, lua.Str("size"))
	case "add":
		return mc("add", argExprs)
	case "forEach":
		return mc("forEach", argExprs)
	case "keys":
		return mc("keys", nil)
	case "values":
		return mc("values", nil)
	case "entries":
		return mc("entries", nil)
	}
	return nil
}

// transformPropertyLength handles .length on arrays and strings
func (t *Transpiler) tryTransformPropertyAccess(pa *ast.PropertyAccessExpression) lua.Expression {
	// Optional chains must go through the chain-flattening algorithm for nil guards.
	if ast.IsOptionalChain(pa.AsNode()) {
		return nil
	}

	prop := pa.Name().AsIdentifier().Text

	// Math constants
	if pa.Expression.Kind == ast.KindIdentifier {
		obj := pa.Expression.AsIdentifier().Text
		if obj == "Math" {
			switch prop {
			case "PI":
				return memberAccess(lua.Ident("math"), "pi")
			case "E":
				return lua.Num(fmt.Sprintf("%g", 2.718281828459045))
			case "LN2":
				return lua.Num(fmt.Sprintf("%g", 0.6931471805599453))
			case "LN10":
				return lua.Num(fmt.Sprintf("%g", 2.302585092994046))
			case "LOG2E":
				return lua.Num(fmt.Sprintf("%g", 1.4426950408889634))
			case "LOG10E":
				return lua.Num(fmt.Sprintf("%g", 0.4342944819032518))
			case "SQRT2":
				return lua.Num(fmt.Sprintf("%g", 1.4142135623730951))
			case "SQRT1_2":
				return lua.Num(fmt.Sprintf("%g", 0.7071067811865476))
			default:
				t.addError(pa.Name().AsNode(), dw.UnsupportedProperty, fmt.Sprintf("Math.%s is unsupported.", prop))
			}
		}
		if obj == "Number" {
			if expr := t.transformNumberProperty(prop); expr != nil {
				return expr
			}
			t.addError(pa.Name().AsNode(), dw.UnsupportedProperty, fmt.Sprintf("Number.%s is unsupported.", prop))
		}
	}

	// .length on strings → #obj (arrays handled elsewhere)
	if prop == "length" && t.isStringExpression(pa.Expression) {
		return t.luaTarget.StrLenExpr(t.transformExpression(pa.Expression))
	}

	// .size on Map/Set — handled by transformMapSetMethodCall for calls,
	// but property access needs handling too
	if prop == "size" {
		return lua.Index(t.transformExpression(pa.Expression), lua.Str("size"))
	}

	// function.length → debug.getinfo(fn).nparams - 1 (minus 1 for self)
	if prop == "length" && t.isFunctionType(pa.Expression) {
		if t.luaTarget == LuaTargetLua50 || t.luaTarget == LuaTargetLua51 || t.luaTarget == LuaTargetUniversal {
			t.addError(pa.AsNode(), dw.UnsupportedForTarget, fmt.Sprintf("function.length is/are not supported for target %s.", t.luaTarget.DisplayName()))
		}
		obj := t.transformExpression(pa.Expression)
		getInfo := lua.Call(lua.Index(lua.Ident("debug"), lua.Str("getinfo")), obj)
		nparams := lua.Index(getInfo, lua.Str("nparams"))
		// Check if function has `this: void` — if so, don't subtract 1
		if t.functionHasThisVoid(pa.Expression) {
			return nparams
		}
		// Subtract 1 for self parameter (non-void context)
		return lua.Binary(nparams, lua.OpSub, lua.Num("1"))
	}

	return nil
}

// functionHasThisVoid checks if the function expression's declaration has `this: void`.
func (t *Transpiler) functionHasThisVoid(node *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(node)
	if sym == nil {
		return false
	}
	for _, decl := range sym.Declarations {
		if k := getThisParamKind(decl); k == thisParamVoid {
			return true
		}
		// For variable declarations, check the initializer
		if decl.Kind == ast.KindVariableDeclaration {
			vd := decl.AsVariableDeclaration()
			if vd.Initializer != nil {
				if k := getThisParamKind(vd.Initializer); k == thisParamVoid {
					return true
				}
			}
		}
	}
	return false
}

// String constructor calls: String(x) → tostring(x)
func (t *Transpiler) isStringConstructorCall(ce *ast.CallExpression) bool {
	if ce.Expression.Kind != ast.KindIdentifier {
		return false
	}
	return ce.Expression.AsIdentifier().Text == "String"
}

// performance.now() → os.clock() * 1000
func (t *Transpiler) tryTransformPerformanceCall(ce *ast.CallExpression) lua.Expression {
	if ce.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil
	}
	pa := ce.Expression.AsPropertyAccessExpression()
	if pa.Expression.Kind != ast.KindIdentifier {
		return nil
	}
	if pa.Expression.AsIdentifier().Text == "performance" && pa.Name().AsIdentifier().Text == "now" {
		return lua.Binary(
			lua.Call(lua.Index(lua.Ident("os"), lua.Str("clock"))),
			lua.OpMul,
			lua.Num("1000"),
		)
	}
	return nil
}

// tryTransformGlobalFunctionCall handles direct calls to JS global functions:
// parseInt, parseFloat, isNaN, isFinite, Number()
func (t *Transpiler) tryTransformGlobalFunctionCall(ce *ast.CallExpression) lua.Expression {
	if ce.Expression.Kind != ast.KindIdentifier {
		return nil
	}
	name := ce.Expression.AsIdentifier().Text

	// Only transform args if name matches a global function (avoid double transformation)
	switch name {
	case "parseInt", "parseFloat", "isNaN", "isFinite", "Number", "Symbol":
		// falls through to switch below
	default:
		return nil
	}

	argExprs := t.transformArgExprs(ce.Arguments)
	switch name {
	case "parseInt":
		fn := t.requireLualib("__TS__ParseInt")
		return lualibCall(fn, argExprs...)
	case "parseFloat":
		fn := t.requireLualib("__TS__ParseFloat")
		return lualibCall(fn, argExprs...)
	case "isNaN":
		// Global isNaN coerces to number first: __TS__NumberIsNaN(__TS__Number(x))
		numFn := t.requireLualib("__TS__Number")
		nanFn := t.requireLualib("__TS__NumberIsNaN")
		coerced := lualibCall(numFn, argExprs...)
		return lualibCall(nanFn, coerced)
	case "isFinite":
		numFn := t.requireLualib("__TS__Number")
		finiteFn := t.requireLualib("__TS__NumberIsFinite")
		coerced := lualibCall(numFn, argExprs...)
		return lualibCall(finiteFn, coerced)
	case "Number":
		fn := t.requireLualib("__TS__Number")
		return lualibCall(fn, argExprs...)
	case "Symbol":
		fn := t.requireLualib("__TS__Symbol")
		return lualibCall(fn, argExprs...)
	}
	return nil
}

// tryTransformFunctionCallApply handles Function.prototype.call and Function.prototype.apply.
// fn.call(thisArg, arg1, arg2) → fn(thisArg, arg1, arg2)
// fn.apply(thisArg, argsArray) → fn(thisArg, unpack(argsArray))
func (t *Transpiler) tryTransformFunctionCallApply(ce *ast.CallExpression) lua.Expression {
	if ce.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil
	}
	pa := ce.Expression.AsPropertyAccessExpression()
	if pa.Name().Kind != ast.KindIdentifier {
		return nil
	}
	method := pa.Name().AsIdentifier().Text
	if method != "call" && method != "apply" && method != "bind" {
		return nil
	}

	// Only intercept when the method actually belongs to Function/CallableFunction/NewableFunction.
	// A user-defined method named "call" on a non-function type must not be intercepted.
	// Ported from: TSTL builtins/index.ts tryTransformBuiltinPrototypeMethodCall
	callSym := t.checker.GetSymbolAtLocation(ce.Expression)
	if callSym == nil || callSym.Parent == nil {
		return nil
	}
	name := callSym.Parent.Name
	if name != "Function" && name != "CallableFunction" && name != "NewableFunction" {
		return nil
	}

	// Transform fn FIRST, then args in scope, to preserve evaluation order
	fnExpr := t.transformExpression(pa.Expression)
	argExprs, argPrec := t.transformArgsInScope(ce.Arguments)
	if len(argPrec) > 0 {
		fnExpr = t.moveToPrecedingTemp(fnExpr)
		t.addPrecedingStatements(argPrec...)
	}

	if method == "bind" {
		// fn.bind(thisArg, ...args) → __TS__FunctionBind(fn, thisArg, ...args)
		fn := t.requireLualib("__TS__FunctionBind")
		allArgs := append([]lua.Expression{fnExpr}, argExprs...)
		return lua.Call(lua.Ident(fn), allArgs...)
	}

	if method == "call" {
		// fn.call(thisArg, ...args) → fn(thisArg, ...args)
		return lua.Call(fnExpr, argExprs...)
	}

	// fn.apply(thisArg, argsArray) → fn(thisArg, unpack(argsArray))
	if len(argExprs) == 0 {
		return lua.Call(fnExpr)
	}
	if len(argExprs) == 1 {
		// fn.apply(thisArg) → fn(thisArg)
		return lua.Call(fnExpr, argExprs[0])
	}
	// fn.apply(thisArg, argsArray) → fn(thisArg, unpack(argsArray))
	thisArg := argExprs[0]
	argsArray := argExprs[1]
	return lua.Call(fnExpr, thisArg, lua.Call(t.unpackIdent(), argsArray))
}

// tryTransformFunctionCallApplyWithCallee handles Function.prototype.call/apply/bind
// when the callee is already a pre-built Lua expression (used in optional chains).
func (t *Transpiler) tryTransformFunctionCallApplyWithCallee(ce *ast.CallExpression, fnExpr lua.Expression, method string) lua.Expression {
	argExprs, argPrec := t.transformArgsInScope(ce.Arguments)
	if len(argPrec) > 0 {
		fnExpr = t.moveToPrecedingTemp(fnExpr)
		t.addPrecedingStatements(argPrec...)
	}

	if method == "bind" {
		fn := t.requireLualib("__TS__FunctionBind")
		allArgs := append([]lua.Expression{fnExpr}, argExprs...)
		return lua.Call(lua.Ident(fn), allArgs...)
	}

	if method == "call" {
		return lua.Call(fnExpr, argExprs...)
	}

	// apply
	if len(argExprs) == 0 {
		return lua.Call(fnExpr)
	}
	if len(argExprs) == 1 {
		return lua.Call(fnExpr, argExprs[0])
	}
	thisArg := argExprs[0]
	argsArray := argExprs[1]
	return lua.Call(fnExpr, thisArg, lua.Call(t.unpackIdent(), argsArray))
}

// addToNumericExpression folds a constant addition into a numeric literal if possible.
// If expr is a NumericLiteral, returns a new literal with value + n. Otherwise returns expr + n.
func addToNumericExpression(expr lua.Expression, n float64) lua.Expression {
	if n == 0 {
		return expr
	}
	if lit, ok := expr.(*lua.NumericLiteral); ok {
		val, err := strconv.ParseFloat(lit.Value, 64)
		if err == nil {
			return lua.Num(strconv.FormatFloat(val+n, 'f', -1, 64))
		}
	}
	// Fold binary expressions: (x - N) + N → x, (x + N) + (-N) → x
	if bin, ok := expr.(*lua.BinaryExpression); ok {
		if rlit, ok := bin.Right.(*lua.NumericLiteral); ok {
			rval, err := strconv.ParseFloat(rlit.Value, 64)
			if err == nil {
				if (bin.Operator == lua.OpSub && rval == n) ||
					(bin.Operator == lua.OpAdd && rval == -n) {
					return bin.Left
				}
			}
		}
	}
	if n > 0 {
		return lua.Binary(expr, lua.OpAdd, lua.Num(strconv.FormatFloat(n, 'f', -1, 64)))
	}
	return lua.Binary(expr, lua.OpSub, lua.Num(strconv.FormatFloat(-n, 'f', -1, 64)))
}

// getNonNegativeNumericLiteral checks if a Lua expression is a non-negative numeric literal.
// Returns the literal, its float64 value, and true if so.
func getNonNegativeNumericLiteral(expr lua.Expression) (*lua.NumericLiteral, float64, bool) {
	lit, ok := expr.(*lua.NumericLiteral)
	if !ok {
		return nil, 0, false
	}
	val, err := strconv.ParseFloat(lit.Value, 64)
	if err != nil || val < 0 {
		return nil, 0, false
	}
	return lit, val, true
}
