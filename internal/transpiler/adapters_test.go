package transpiler

import (
	"strings"
	"testing"
)

const adapterTsconfig = `{"compilerOptions":{"strict":true,"target":"ESNext","lib":["esnext"]}}`

// Default (no @luaArrayRuntime): arr.length emits #arr.
func TestArrayLength_DefaultEmitsHashOperator(t *testing.T) {
	code := `
declare function print(...args: unknown[]): void;
const a = [1, 2, 3];
print(a.length);
`
	lua := transpileWithConfig(t, code, adapterTsconfig)
	if !strings.Contains(lua, "#a") {
		t.Errorf("expected emitted Lua to contain `#a` (default length emit); got:\n%s", lua)
	}
	if strings.Contains(lua, "Len(") {
		t.Errorf("expected no `Len(` call in default emit; got:\n%s", lua)
	}
}

// User @luaArrayRuntime: arr.length emits Len(a) instead of #a.
func TestArrayLength_UserAdapterEmitsFunctionCall(t *testing.T) {
	code := `
declare function print(...args: unknown[]): void;
declare function Len(arr: readonly unknown[]): number;

/** @luaArrayRuntime */
declare const HostArrays: {
    length: typeof Len;
};

const a = [1, 2, 3];
print(a.length);
`
	lua := transpileWithConfig(t, code, adapterTsconfig)
	if !strings.Contains(lua, "Len(a)") {
		t.Errorf("expected emitted Lua to contain `Len(a)`; got:\n%s", lua)
	}
	if strings.Contains(lua, "#a") {
		t.Errorf("expected no `#a` in adapter emit; got:\n%s", lua)
	}
}

// An adapter declaration with a malformed property (non-typeof) leaves the
// default emit in place. Kernel-scope behavior: silently fall back to default.
// (Signature validation with explicit diagnostics lands in the next kernel step.)
func TestArrayLength_MalformedAdapterFallsBackToDefault(t *testing.T) {
	code := `
declare function print(...args: unknown[]): void;

/** @luaArrayRuntime */
declare const Broken: {
    length: number;
};

const a = [1, 2, 3];
print(a.length);
`
	lua := transpileWithConfig(t, code, adapterTsconfig)
	if !strings.Contains(lua, "#a") {
		t.Errorf("expected fallback to `#a` on malformed adapter; got:\n%s", lua)
	}
}

// A @luaArrayRuntime declaration with no recognized primitives leaves all
// defaults in place (no user-adapter flag set).
func TestArrayLength_EmptyAdapterLeavesDefaults(t *testing.T) {
	code := `
declare function print(...args: unknown[]): void;

/** @luaArrayRuntime */
declare const Empty: {};

const a = [1, 2, 3];
print(a.length);
`
	lua := transpileWithConfig(t, code, adapterTsconfig)
	if !strings.Contains(lua, "#a") {
		t.Errorf("expected default `#a` for empty adapter; got:\n%s", lua)
	}
}
