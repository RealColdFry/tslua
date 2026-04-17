package luatest

import (
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestOptimizedConcatTostring_Eval verifies runtime parity between tstl and
// optimized emit modes for string concatenation with numeric operands.
// Covers the wrapInToStringForConcat optimization in binary_ops.go.
func TestOptimizedConcatTostring_Eval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"string + number var",
			`const n: number = 42; return "x=" + n;`,
			`"x=42"`,
		},
		{
			"number var + string",
			`const n: number = 42; return n + "x";`,
			`"42x"`,
		},
		{
			"string + numeric expression",
			`const a: number = 2, b: number = 3; return "s=" + (a + b);`,
			`"s=5"`,
		},
		{
			"string + float",
			`const n: number = 3.5; return "n=" + n;`,
			`"n=3.5"`,
		},
		{
			"string + negative number",
			`const n: number = -1; return "n=" + n;`,
			`"n=-1"`,
		},
		{
			"chained numeric concat",
			`const a: number = 1, b: number = 2; return "a=" + a + ",b=" + b;`,
			`"a=1,b=2"`,
		},
		{
			"non-numeric operand still wraps",
			`const v: string | number = 7 as string | number; return "x=" + v;`,
			`"x=7"`,
		},
	}

	modes := []struct {
		name string
		mode transpiler.EmitMode
	}{
		{"tstl", transpiler.EmitModeTSTL},
		{"optimized", transpiler.EmitModeOptimized},
	}

	for _, tc := range cases {
		for _, m := range modes {
			t.Run(tc.name+"/"+m.name, func(t *testing.T) {
				t.Parallel()
				ExpectFunction(t, tc.body, tc.want, Opts{EmitMode: m.mode})
			})
		}
	}
}

// TestOptimizedConcatTostring_Codegen verifies the optimized emit mode actually
// omits the tostring() wrap for numeric-typed operands, while tstl mode still
// emits it. Without this assertion, a regression that silently falls back to
// the wrapped form would still pass the eval tests.
func TestOptimizedConcatTostring_Codegen(t *testing.T) {
	t.Parallel()

	t.Run("numeric operand", func(t *testing.T) {
		t.Parallel()
		body := `const n: number = 42; return "x=" + n;`

		optLua := transpileMainLua(t, body, transpiler.EmitModeOptimized)
		if strings.Contains(optLua, "tostring(n)") {
			t.Errorf("optimized mode: expected no tostring(n) wrap for numeric operand, got:\n%s", optLua)
		}
		if !strings.Contains(optLua, `"x=" .. n`) {
			t.Errorf("optimized mode: expected `\"x=\" .. n`, got:\n%s", optLua)
		}

		tstlLua := transpileMainLua(t, body, transpiler.EmitModeTSTL)
		if !strings.Contains(tstlLua, "tostring(n)") {
			t.Errorf("tstl mode: expected tostring(n) wrap, got:\n%s", tstlLua)
		}
	})

	t.Run("numeric expression", func(t *testing.T) {
		t.Parallel()
		body := `const a: number = 2, b: number = 3; return "s=" + (a + b);`

		optLua := transpileMainLua(t, body, transpiler.EmitModeOptimized)
		if strings.Contains(optLua, "tostring(") {
			t.Errorf("optimized mode: expected no tostring() wrap for numeric expression, got:\n%s", optLua)
		}

		tstlLua := transpileMainLua(t, body, transpiler.EmitModeTSTL)
		if !strings.Contains(tstlLua, "tostring(") {
			t.Errorf("tstl mode: expected tostring() wrap for numeric expression, got:\n%s", tstlLua)
		}
	})

	t.Run("non-numeric operand wraps in both modes", func(t *testing.T) {
		t.Parallel()
		body := `const v: string | number = 7 as string | number; return "x=" + v;`

		for _, mode := range []transpiler.EmitMode{transpiler.EmitModeTSTL, transpiler.EmitModeOptimized} {
			lua := transpileMainLua(t, body, mode)
			if !strings.Contains(lua, "tostring(v)") {
				t.Errorf("mode=%s: expected tostring(v) wrap for non-numeric operand, got:\n%s", mode, lua)
			}
		}
	})

	t.Run("numeric literal never wraps", func(t *testing.T) {
		t.Parallel()
		body := `return "x=" + 42;`

		for _, mode := range []transpiler.EmitMode{transpiler.EmitModeTSTL, transpiler.EmitModeOptimized} {
			lua := transpileMainLua(t, body, mode)
			if strings.Contains(lua, "tostring(42)") {
				t.Errorf("mode=%s: expected no tostring(42) wrap for literal, got:\n%s", mode, lua)
			}
		}
	})
}

func transpileMainLua(t *testing.T, body string, mode transpiler.EmitMode) string {
	t.Helper()
	tsCode := "export function __main() {" + body + "}"
	results := TranspileTS(t, tsCode, Opts{EmitMode: mode})
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			return r.Lua
		}
	}
	t.Fatal("no main.ts result")
	return ""
}
