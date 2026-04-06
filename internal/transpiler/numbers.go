// Handles Number constructor methods, instance methods, and property access.
package transpiler

import (
	"github.com/realcoldfry/tslua/internal/lua"
)

// transformNumberCall handles Number.isFinite, Number.isNaN, etc.
func (t *Transpiler) transformNumberCall(method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "isFinite":
		fn := t.requireLualib("__TS__NumberIsFinite")
		return lualibCall(fn, argExprs...)
	case "isNaN":
		fn := t.requireLualib("__TS__NumberIsNaN")
		return lualibCall(fn, argExprs...)
	case "isInteger":
		fn := t.requireLualib("__TS__NumberIsInteger")
		return lualibCall(fn, argExprs...)
	case "parseInt":
		fn := t.requireLualib("__TS__ParseInt")
		return lualibCall(fn, argExprs...)
	case "parseFloat":
		fn := t.requireLualib("__TS__ParseFloat")
		return lualibCall(fn, argExprs...)
	}
	return nil
}

// transformNumberInstanceCall handles number.toFixed(), number.toString(radix).
func (t *Transpiler) transformNumberInstanceCall(objExpr lua.Expression, method string, argExprs []lua.Expression) lua.Expression {
	switch method {
	case "toFixed":
		fn := t.requireLualib("__TS__NumberToFixed")
		return lualibCall(fn, append([]lua.Expression{objExpr}, argExprs...)...)
	case "toString":
		if len(argExprs) == 0 {
			return lua.Call(lua.Ident("tostring"), objExpr)
		}
		fn := t.requireLualib("__TS__NumberToString")
		return lualibCall(fn, append([]lua.Expression{objExpr}, argExprs...)...)
	}
	return nil
}

// transformNumberProperty handles Number.MAX_SAFE_INTEGER, Number.NaN, etc.
func (t *Transpiler) transformNumberProperty(prop string) lua.Expression {
	switch prop {
	case "MAX_SAFE_INTEGER":
		// 2^53 - 1 = 9007199254740991
		// Note: TSTL emits 2 ^ 1024 which is Infinity — that's a TSTL bug.
		return lua.Num("9007199254740991")
	case "MIN_SAFE_INTEGER":
		// -(2^53 - 1) = -9007199254740991
		// Note: TSTL emits -2 ^ 1074 which is -5e-324 — that's a TSTL bug.
		return lua.Num("-9007199254740991")
	case "POSITIVE_INFINITY":
		if t.luaTarget.HasMathHuge() {
			return memberAccess(lua.Ident("math"), "huge")
		}
		return lua.Binary(lua.Num("1"), lua.OpDiv, lua.Num("0"))
	case "NEGATIVE_INFINITY":
		if t.luaTarget.HasMathHuge() {
			return lua.Unary(lua.OpNeg, memberAccess(lua.Ident("math"), "huge"))
		}
		return lua.Unary(lua.OpNeg, lua.Binary(lua.Num("1"), lua.OpDiv, lua.Num("0")))
	case "NaN":
		return lua.Binary(lua.Num("0"), lua.OpDiv, lua.Num("0"))
	case "EPSILON":
		// 2 ^ -52
		return lua.Binary(lua.Num("2"), lua.OpPow, lua.Num("-52"))
	case "MIN_VALUE":
		// 2 ^ -1074 = 5e-324
		// Note: TSTL emits -2 ^ 1074 which is wrong (negative, wrong exponent sign).
		return lua.Binary(lua.Num("2"), lua.OpPow, lua.Num("-1074"))
	case "MAX_VALUE":
		// (2 - 2^-52) * 2^1023 ≈ 1.7976931348623157e+308
		// Note: TSTL emits 2 ^ 1024 which is Infinity — that's a TSTL bug.
		return lua.Binary(
			lua.Binary(lua.Num("2"), lua.OpSub, lua.Binary(lua.Num("2"), lua.OpPow, lua.Num("-52"))),
			lua.OpMul,
			lua.Binary(lua.Num("2"), lua.OpPow, lua.Num("1023")),
		)
	}
	return nil
}
