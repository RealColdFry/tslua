package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestNumberConstants_Eval verifies that Number constants produce correct values
// across all Lua targets. This catches TSTL bugs where constants like
// MAX_SAFE_INTEGER, MIN_VALUE, etc. emit wrong Lua expressions.
// See notes/tstl-bugs/number-constants.md for details.
func TestNumberConstants_Eval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		expr string
		want string
	}{
		// Verify the full ordering:
		// NEGATIVE_INFINITY < MIN_SAFE_INTEGER < 0 < MIN_VALUE < EPSILON < MAX_SAFE_INTEGER < MAX_VALUE < POSITIVE_INFINITY
		{"NEGATIVE_INFINITY < MIN_SAFE_INTEGER", "Number.NEGATIVE_INFINITY < Number.MIN_SAFE_INTEGER", "true"},
		{"MIN_SAFE_INTEGER < 0", "Number.MIN_SAFE_INTEGER < 0", "true"},
		{"0 < MIN_VALUE", "0 < Number.MIN_VALUE", "true"},
		{"MIN_VALUE < EPSILON", "Number.MIN_VALUE < Number.EPSILON", "true"},
		{"EPSILON < MAX_SAFE_INTEGER", "Number.EPSILON < Number.MAX_SAFE_INTEGER", "true"},
		{"MAX_SAFE_INTEGER < MAX_VALUE", "Number.MAX_SAFE_INTEGER < Number.MAX_VALUE", "true"},
		{"MAX_VALUE < POSITIVE_INFINITY", "Number.MAX_VALUE < Number.POSITIVE_INFINITY", "true"},

		// Verify specific values
		{"MIN_VALUE > 0", "Number.MIN_VALUE > 0", "true"},
		{"MIN_SAFE_INTEGER === -(2**53 - 1)", "Number.MIN_SAFE_INTEGER === -(2**53 - 1)", "true"},
		{"MAX_SAFE_INTEGER === 2**53 - 1", "Number.MAX_SAFE_INTEGER === 2**53 - 1", "true"},
		{"MAX_SAFE_INTEGER + 1 !== MAX_SAFE_INTEGER", "Number.MAX_SAFE_INTEGER + 1 !== Number.MAX_SAFE_INTEGER", "true"},
		{"MIN_SAFE_INTEGER - 1 !== MIN_SAFE_INTEGER", "Number.MIN_SAFE_INTEGER - 1 !== Number.MIN_SAFE_INTEGER", "true"},
	}

	targets := []struct {
		name   string
		target transpiler.LuaTarget
	}{
		{"LuaJIT", transpiler.LuaTargetLuaJIT},
		{"5.0", transpiler.LuaTargetLua50},
		{"5.1", transpiler.LuaTargetLua51},
		{"5.2", transpiler.LuaTargetLua52},
		{"5.3", transpiler.LuaTargetLua53},
		{"5.4", transpiler.LuaTargetLua54},
		{"5.5", transpiler.LuaTargetLua55},
	}

	for _, tc := range cases {
		for _, tgt := range targets {
			t.Run(tc.name+" ["+tgt.name+"]", func(t *testing.T) {
				t.Parallel()
				tsCode := "export const __result = " + tc.expr + ";"
				results := TranspileTS(t, tsCode, Opts{LuaTarget: tgt.target})
				got := RunLua(t, results, `mod["__result"]`, Opts{LuaTarget: tgt.target})
				if got != tc.want {
					t.Errorf("got %s, want %s", got, tc.want)
				}
			})
		}
	}
}
