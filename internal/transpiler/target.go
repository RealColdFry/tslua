package transpiler

import "github.com/realcoldfry/tslua/internal/lua"

// EmitMode controls whether the transpiler matches TSTL output exactly or applies optimizations.
type EmitMode string

const (
	// EmitModeTSTL matches TSTL's Lua output for codegen parity (default).
	EmitModeTSTL EmitMode = "tstl"
	// EmitModeOptimized applies known-safe improvements (e.g., skip unnecessary tostring() wraps).
	EmitModeOptimized EmitMode = "optimized"
)

// LuaLibImportKind controls how lualib polyfill functions are included in output.
type LuaLibImportKind string

const (
	// LuaLibImportRequire emits require("lualib_bundle") and expects the bundle on the Lua path.
	LuaLibImportRequire LuaLibImportKind = "require"
	// LuaLibImportInline embeds the lualib bundle directly in the output.
	LuaLibImportInline LuaLibImportKind = "inline"
	// LuaLibImportNone suppresses all lualib imports — assumes features are provided externally.
	LuaLibImportNone LuaLibImportKind = "none"
)

// ValidLuaLibImport returns true if s is a recognized luaLibImport value.
func ValidLuaLibImport(s string) bool {
	switch LuaLibImportKind(s) {
	case LuaLibImportRequire, LuaLibImportInline, LuaLibImportNone:
		return true
	}
	return false
}

// LuaTarget represents the Lua version to target.
type LuaTarget string

const (
	LuaTargetUniversal LuaTarget = "universal"
	LuaTargetLua50     LuaTarget = "5.0"
	LuaTargetLua51     LuaTarget = "5.1"
	LuaTargetLua52     LuaTarget = "5.2"
	LuaTargetLua53     LuaTarget = "5.3"
	LuaTargetLua54     LuaTarget = "5.4"
	LuaTargetLua55     LuaTarget = "5.5"
	LuaTargetLuaJIT    LuaTarget = "JIT"
)

// DisplayName returns the TSTL-compatible display name for the target.
// JIT → "LuaJIT", others → "Lua 5.0", "Lua universal", etc.
func (t LuaTarget) DisplayName() string {
	if t == LuaTargetLuaJIT {
		return "LuaJIT"
	}
	return "Lua " + string(t)
}

// SupportsGoto returns whether the target supports goto statements and labels.
// LuaJIT and Lua 5.2+ support goto; vanilla Lua 5.0, 5.1 and Universal do not.
func (t LuaTarget) SupportsGoto() bool {
	switch t {
	case LuaTargetLua52, LuaTargetLua53, LuaTargetLua54, LuaTargetLua55, LuaTargetLuaJIT:
		return true
	default:
		return false
	}
}

// BitLibrary returns the name of the built-in bit manipulation library, or "" if none.
// LuaJIT has "bit", Lua 5.2 has "bit32", others have none.
func (t LuaTarget) BitLibrary() string {
	switch t {
	case LuaTargetLuaJIT:
		return "bit"
	case LuaTargetLua52:
		return "bit32"
	default:
		return ""
	}
}

// HasNativeBitwise returns whether the target supports native bitwise operators (&, |, ~, <<, >>).
// Lua 5.3+ has these.
func (t LuaTarget) HasNativeBitwise() bool {
	switch t {
	case LuaTargetLua53, LuaTargetLua54, LuaTargetLua55:
		return true
	default:
		return false
	}
}

// UsesTableUnpack returns whether the target uses table.unpack() instead of global unpack().
// Lua 5.2+ moved unpack into the table library. Universal uses a lualib shim.
func (t LuaTarget) UsesTableUnpack() bool {
	switch t {
	case LuaTargetLua52, LuaTargetLua53, LuaTargetLua54, LuaTargetLua55:
		return true
	default:
		return false
	}
}

// UsesLua50Unpack returns whether the target uses Lua 5.0's unpack (no bounds args).
func (t LuaTarget) UsesLua50Unpack() bool {
	return t == LuaTargetLua50
}

// SupportsFloorDiv returns whether the target supports the // floor division operator.
// Lua 5.3+ has this.
func (t LuaTarget) SupportsFloorDiv() bool {
	switch t {
	case LuaTargetLua53, LuaTargetLua54, LuaTargetLua55:
		return true
	default:
		return false
	}
}

// AllowsUnicodeIds returns whether the target supports unicode characters in identifiers.
func (t LuaTarget) AllowsUnicodeIds() bool {
	return t == LuaTargetLuaJIT
}

// HasLengthOperator returns whether the target supports the # length operator.
// Lua 5.0 does not have #; use table.getn() / string.len() instead.
func (t LuaTarget) HasLengthOperator() bool {
	return t != LuaTargetLua50
}

// HasVarargDots returns whether the target supports ... vararg syntax.
// Lua 5.0 uses the implicit `arg` table instead.
func (t LuaTarget) HasVarargDots() bool {
	return t != LuaTargetLua50
}

// HasMathHuge returns whether the target has math.huge.
// Lua 5.0 does not; use 1/0 instead.
func (t LuaTarget) HasMathHuge() bool {
	return t != LuaTargetLua50
}

// HasModOperator returns whether the target supports the % modulo operator.
// Lua 5.0 does not; use math.mod() instead.
func (t LuaTarget) HasModOperator() bool {
	return t != LuaTargetLua50
}

// MathAtan2Name returns the short function name for two-argument arctangent.
// Lua 5.3+ merged atan2 into math.atan(y, x), so returns "atan"; others return "atan2".
func (t LuaTarget) MathAtan2Name() string {
	switch t {
	case LuaTargetLua53, LuaTargetLua54, LuaTargetLua55:
		return "atan"
	default:
		return "atan2"
	}
}

// LenExpr returns the Lua expression for getting the length of expr.
// Lua 5.0: table.getn(expr); Lua 5.1+: #expr.
func (t LuaTarget) LenExpr(expr lua.Expression) lua.Expression {
	if t == LuaTargetLua50 {
		return lua.Call(lua.Index(lua.Ident("table"), lua.Str("getn")), expr)
	}
	return lua.Unary(lua.OpLen, expr)
}

// StrLenExpr returns the Lua expression for getting the length of a string expr.
// Lua 5.0: string.len(expr); Lua 5.1+: #expr.
func (t LuaTarget) StrLenExpr(expr lua.Expression) lua.Expression {
	if t == LuaTargetLua50 {
		return lua.Call(lua.Index(lua.Ident("string"), lua.Str("len")), expr)
	}
	return lua.Unary(lua.OpLen, expr)
}

// Runtime returns the executable name for running Lua code with this target.
func (t LuaTarget) Runtime() string {
	switch t {
	case LuaTargetLuaJIT:
		return "luajit"
	case LuaTargetLua50:
		return "lua5.0"
	case LuaTargetLua51, LuaTargetUniversal:
		return "lua5.1"
	case LuaTargetLua52:
		return "lua5.2"
	case LuaTargetLua53:
		return "lua5.3"
	case LuaTargetLua54:
		return "lua5.4"
	case LuaTargetLua55:
		return "lua5.5"
	default:
		return "lua5.1"
	}
}

// AllTargets returns all supported LuaTarget values.
func AllTargets() []LuaTarget {
	return []LuaTarget{
		LuaTargetUniversal,
		LuaTargetLua50,
		LuaTargetLua51,
		LuaTargetLua52,
		LuaTargetLua53,
		LuaTargetLua54,
		LuaTargetLua55,
		LuaTargetLuaJIT,
	}
}

// ValidTarget returns whether the given string is a valid LuaTarget.
func ValidTarget(s string) bool {
	switch LuaTarget(s) {
	case LuaTargetUniversal, LuaTargetLua50, LuaTargetLua51, LuaTargetLua52, LuaTargetLua53, LuaTargetLua54, LuaTargetLua55, LuaTargetLuaJIT:
		return true
	default:
		return false
	}
}
