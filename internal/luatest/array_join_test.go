package luatest

import (
	"strings"
	"testing"
)

// TestArrayJoinTableConcat verifies that .join() on string/number arrays
// emits table.concat() directly instead of __TS__ArrayJoin.
// Ported from: TSTL builtins/array.ts join optimization (lines 161-186)
func TestArrayJoinTableConcat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		code       string
		wantConcat bool // true = expect table.concat, false = expect __TS__ArrayJoin
	}{
		{
			"string array join",
			`const arr: string[] = ["a", "b", "c"];
			export function __main() { return arr.join(","); }`,
			true,
		},
		{
			"number array join",
			`const arr: number[] = [1, 2, 3];
			export function __main() { return arr.join("-"); }`,
			true,
		},
		{
			"string array join no separator",
			`const arr: string[] = ["a", "b"];
			export function __main() { return arr.join(); }`,
			true,
		},
		{
			"any array join uses lualib",
			`const arr: any[] = [1, "a", {}];
			export function __main() { return arr.join(","); }`,
			false,
		},
		{
			"object array join uses lualib",
			`const arr: {x: number}[] = [{x: 1}];
			export function __main() { return arr.join(","); }`,
			false,
		},
		{
			"union string|number join uses table.concat",
			`const arr: (string | number)[] = [1, "a"];
			export function __main() { return arr.join(","); }`,
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, Opts{})
			luaCode := results[0].Lua
			hasConcat := strings.Contains(luaCode, "table.concat(")
			hasArrayJoin := strings.Contains(luaCode, "__TS__ArrayJoin")
			if tc.wantConcat {
				if !hasConcat {
					t.Errorf("expected table.concat() in output\ngot:\n%s", luaCode)
				}
				if hasArrayJoin {
					t.Errorf("expected no __TS__ArrayJoin in output\ngot:\n%s", luaCode)
				}
			} else {
				if hasConcat {
					t.Errorf("expected no table.concat() in output\ngot:\n%s", luaCode)
				}
				if !hasArrayJoin {
					t.Errorf("expected __TS__ArrayJoin in output\ngot:\n%s", luaCode)
				}
			}
		})
	}
}
