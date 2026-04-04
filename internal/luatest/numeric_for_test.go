package luatest

import (
	"os"
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

func TestMain(m *testing.M) {
	Setup()
	code := m.Run()
	Teardown()
	os.Exit(code)
}

// TestNumericFor_Eval verifies that for-loop eval results are identical between
// tstl (while-loop) and optimized (numeric for) emit modes.
// Each case runs in both modes and compares against the expected value.
func TestNumericFor_Eval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		// === Basic patterns (should be optimizable) ===
		{
			"i++ literal limit",
			`let sum = 0; for (let i = 0; i < 10; i++) { sum += i; } return sum;`,
			"45",
		},
		{
			"i++ literal limit <=",
			`let sum = 0; for (let i = 0; i <= 9; i++) { sum += i; } return sum;`,
			"45",
		},
		{
			"i-- decrementing",
			`let result = ""; for (let i = 5; i > 0; i--) { result += i; } return result;`,
			`"54321"`,
		},
		{
			"i-- decrementing >=",
			`let result = ""; for (let i = 5; i >= 1; i--) { result += i; } return result;`,
			`"54321"`,
		},
		{
			"i += 2 step",
			`let sum = 0; for (let i = 0; i < 10; i += 2) { sum += i; } return sum;`,
			"20",
		},
		{
			"i -= 2 step",
			`let sum = 0; for (let i = 10; i > 0; i -= 2) { sum += i; } return sum;`,
			"30",
		},
		{
			"start from 1",
			`let sum = 0; for (let i = 1; i <= 5; i++) { sum += i; } return sum;`,
			"15",
		},
		{
			"empty loop body",
			`let x = 0; for (let i = 0; i < 5; i++) {} return x;`,
			"0",
		},
		{
			"zero iterations",
			`let sum = 0; for (let i = 10; i < 5; i++) { sum += i; } return sum;`,
			"0",
		},

		// === With break ===
		{
			"break in loop",
			`let sum = 0; for (let i = 0; i < 100; i++) { if (i >= 5) break; sum += i; } return sum;`,
			"10",
		},
		{
			"break with return value",
			`for (let i = 0; i < 100; i++) { if (i * i > 50) return i; } return -1;`,
			"8",
		},

		// === With continue ===
		{
			"continue skip evens",
			`let sum = 0; for (let i = 0; i < 10; i++) { if (i % 2 === 0) continue; sum += i; } return sum;`,
			"25",
		},
		{
			"continue and break",
			`let sum = 0; for (let i = 0; i < 100; i++) { if (i % 3 === 0) continue; if (i > 10) break; sum += i; } return sum;`,
			"37",
		},

		// === Nested loops ===
		{
			"nested for loops",
			`let sum = 0; for (let i = 0; i < 5; i++) { for (let j = 0; j < 5; j++) { sum += 1; } } return sum;`,
			"25",
		},
		{
			"nested with outer var in inner limit",
			`let total = 0;
			for (let i = 1; i <= 3; i++) {
				for (let j = 0; j < i; j++) { total += j; }
			}
			return total;`,
			"4",
		},

		// === Variable limit (non-optimizable, must still produce correct results) ===
		{
			"variable limit",
			`let sum = 0; const n = 5; for (let i = 0; i < n; i++) { sum += i; } return sum;`,
			"10",
		},
		{
			"array.length limit",
			`let arr = [10, 20, 30]; let sum = 0; for (let i = 0; i < arr.length; i++) { sum += arr[i]; } return sum;`,
			"60",
		},

		// === Limit modified in body (must NOT optimize — semantics differ) ===
		{
			"limit modified in body",
			`let n = 10; let count = 0;
			for (let i = 0; i < n; i++) { n = 5; count++; }
			return count;`,
			"5",
		},
		{
			"array shrinks in body",
			`let arr = [1, 2, 3, 4, 5]; let sum = 0;
			for (let i = 0; i < arr.length; i++) { sum += arr[i]; if (i === 2) arr.length = 3; }
			return sum;`,
			"6",
		},

		// === Loop var modified in body (must NOT optimize) ===
		{
			"loop var reassigned",
			`let sum = 0;
			for (let i = 0; i < 10; i++) { sum += i; if (i === 2) i = 7; }
			return sum;`,
			"20",
		},
		{
			"loop var incremented extra",
			`let sum = 0;
			for (let i = 0; i < 10; i++) { sum += i; if (i % 2 === 0) i++; }
			return sum;`,
			"20",
		},

		// === Complex conditions (non-optimizable) ===
		{
			"function call in condition",
			`let calls = 0;
			function limit() { calls++; return 3; }
			for (let i = 0; i < limit(); i++) {}
			return calls;`,
			"4",
		},

		// === Side effects in incrementor ===
		{
			"compound expression in incrementor",
			`let result: number[] = [];
			for (let i = 0; i < 5; i++) { result.push(i); }
			return result;`,
			"{0, 1, 2, 3, 4}",
		},

		// === Using loop var after loop ===
		{
			"loop var scoped to do/end",
			`let outer = 0;
			for (let i = 0; i < 5; i++) { outer = i; }
			return outer;`,
			"4",
		},

		// === Large iteration count (performance-relevant case) ===
		{
			"large loop",
			`let sum = 0; for (let i = 0; i < 1000; i++) { sum += i; } return sum;`,
			"499500",
		},

		// === Mixed types that shouldn't confuse the optimizer ===
		{
			"string concat in body",
			`let s = ""; for (let i = 0; i < 3; i++) { s += String(i); } return s;`,
			`"012"`,
		},

		// === Condition forms ===
		{
			"i < expr (literal arithmetic)",
			`let sum = 0; for (let i = 0; i < 2 + 3; i++) { sum += i; } return sum;`,
			"10",
		},
		{
			"mirrored condition (limit > i)",
			`let sum = 0; for (let i = 0; 10 > i; i++) { sum += i; } return sum;`,
			"45",
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

// TestNumericFor_Codegen verifies that the optimized emit mode actually produces
// Lua numeric for loops for the expected patterns.
func TestNumericFor_Codegen(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		body          string
		shouldContain string // substring expected in optimized Lua output
		shouldNotFor  bool   // if true, expect no "for ... do" in optimized output (i.e., should NOT optimize)
	}{
		{
			"simple i++ < literal emits numeric for",
			`for (let i = 0; i < 10; i++) {}`,
			"for i = 0, 9 do",
			false,
		},
		{
			"variable limit does NOT emit numeric for",
			`const n = 5; for (let i = 0; i < n; i++) {}`,
			"",
			true,
		},
		{
			"i += 2 literal step emits numeric for",
			`for (let i = 0; i < 10; i += 2) {}`,
			"for i = 0, 9, 2 do", // Lua: runs while i <= 9, stepping by 2: 0,2,4,6,8
			false,
		},
		{
			"i -= 2 literal step emits numeric for",
			`for (let i = 10; i > 0; i -= 2) {}`,
			"for i = 10, 1, -2 do", // Lua: runs while i >= 1, stepping by -2: 10,8,6,4,2
			false,
		},
		{
			"i += non-literal step does NOT emit numeric for",
			`const s = 2; for (let i = 0; i < 10; i += s) {}`,
			"",
			true,
		},
		{
			"loop var modified does NOT emit numeric for",
			`for (let i = 0; i < 10; i++) { if (true) i = 5; }`,
			"",
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tsCode := "export function __main() {" + tc.body + "}"
			results := TranspileTS(t, tsCode, Opts{EmitMode: transpiler.EmitModeOptimized})
			var mainLua string
			for _, r := range results {
				if strings.HasSuffix(r.FileName, "main.ts") {
					mainLua = r.Lua
				}
			}

			if tc.shouldNotFor {
				// Should use while loop, not numeric for
				// Count "for " occurrences — the only "for" should NOT be a numeric for
				// (it will be "while" in current impl)
				lines := strings.Split(mainLua, "\n")
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "for ") && strings.Contains(trimmed, " = ") && strings.Contains(trimmed, " do") {
						t.Errorf("expected no numeric for loop, but found: %s\nlua:\n%s", trimmed, mainLua)
					}
				}
			} else if tc.shouldContain != "" {
				if !strings.Contains(mainLua, tc.shouldContain) {
					t.Errorf("expected Lua to contain %q\nlua:\n%s", tc.shouldContain, mainLua)
				}
			}
		})
	}
}
