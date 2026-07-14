package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestBitwiseShift_Eval pins down JS-conformant behavior of the shift operators
// `<<`, `>>`, and `>>>` across all Lua targets where the operator is not
// diagnosed. JS shift semantics: operands are coerced to int32 (`<<`, `>>`) or
// uint32 (`>>>`), the shift count is masked to its low 5 bits, `>>` is
// arithmetic, `>>>` is logical (unsigned).
//
// Targets where a shift is diagnosed (e.g. `>>` on 5.3+, all bitwise on
// 5.0/5.1/universal) are skipped: the user opted out of those constructs.
func TestBitwiseShift_Eval(t *testing.T) {
	t.Parallel()

	type shiftCase struct {
		name string
		expr string
		want string
	}

	cases := []shiftCase{
		{"-8 >> 1", "-8 >> 1", "-4"},
		{"-1 >> 16", "-1 >> 16", "-1"},
		// Shift count masked to 5 bits: 33 & 31 == 1, so equivalent to -8 >> 1.
		{"-8 >> 33", "-8 >> 33", "-4"},
		// Result truncated to int32: 1 << 31 wraps to INT32_MIN.
		{"1 << 31", "1 << 31", "-2147483648"},
		// Shift count masked to 5 bits: 32 & 31 == 0, so equivalent to 1 << 0.
		{"1 << 32", "1 << 32", "1"},
		// Shift count masked to 5 bits: 32 & 31 == 0, so equivalent to 1 >>> 0.
		{"1 >>> 32", "1 >>> 32", "1"},
		// `>>>` returns an unsigned value: -1 reinterpreted as uint32.
		{"-1 >>> 0", "-1 >>> 0", "4294967295"},
	}

	// shiftKind classifies a case by its operator so we can skip cases that the
	// transpiler diagnoses on a given target.
	shiftKind := func(name string) string {
		switch {
		case containsOp(name, ">>>"):
			return ">>>"
		case containsOp(name, ">>"):
			return ">>"
		default:
			return "<<"
		}
	}

	type targetSpec struct {
		name   string
		target transpiler.LuaTarget
		// diagnosed lists shift kinds that this target rejects with a
		// diagnostic; those cases are skipped (treated as non-bugs).
		diagnosed map[string]bool
	}
	targets := []targetSpec{
		{"LuaJIT", transpiler.LuaTargetLuaJIT, nil},
		// 5.0/5.1/universal: all bitwise ops are diagnosed; nothing to test.
		{"5.2", transpiler.LuaTargetLua52, nil},
		// 5.3+ diagnose `>>` (steers users to `>>>`); `<<` and `>>>` are emitted.
		{"5.3", transpiler.LuaTargetLua53, map[string]bool{">>": true}},
		{"5.4", transpiler.LuaTargetLua54, map[string]bool{">>": true}},
		{"5.5", transpiler.LuaTargetLua55, map[string]bool{">>": true}},
	}

	for _, tc := range cases {
		for _, tgt := range targets {
			if tgt.diagnosed[shiftKind(tc.name)] {
				continue
			}
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

func containsOp(s, op string) bool {
	for i := 0; i+len(op) <= len(s); i++ {
		if s[i:i+len(op)] == op {
			return true
		}
	}
	return false
}
