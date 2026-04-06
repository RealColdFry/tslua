package luatest

import "testing"

// TestLogicalAssignmentElementAccess_Eval verifies that logical assignment
// operators (||=, &&=, ??=) on array element access correctly adjust
// 0-based TS indices to 1-based Lua indices.
func TestLogicalAssignmentElementAccess_Eval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
		want string
	}{
		{
			"||= on array element",
			`const arr: (boolean | number)[] = [false, false, false];
			arr[1] ||= 42;
			export const __result = arr[1];`,
			"42",
		},
		{
			"&&= on array element",
			`const arr = [1, 2, 3];
			arr[1] &&= 99;
			export const __result = arr[1];`,
			"99",
		},
		{
			"??= on array element",
			`const arr: (number | null)[] = [1, null, 3];
			arr[1] ??= 55;
			export const __result = arr[1];`,
			"55",
		},
		{
			"||= does not modify truthy element",
			`const arr = [10, 20, 30];
			arr[0] ||= 999;
			export const __result = arr[0];`,
			"10",
		},
		{
			"&&= does not modify falsy element",
			`const arr: (boolean | number)[] = [false, 1, 2];
			arr[0] &&= 999;
			export const __result = arr[0];`,
			"false",
		},
		{
			"??= does not modify non-null element",
			`const arr = [10, 20, 30];
			arr[0] ??= 999;
			export const __result = arr[0];`,
			"10",
		},
		{
			"||= with variable index",
			`const arr: (boolean | number)[] = [false, false, false];
			const i = 2;
			arr[i] ||= 77;
			export const __result = arr[2];`,
			"77",
		},
		{
			"logical assignment expression value",
			`const arr: (boolean | number)[] = [false, false, false];
			export const __result = (arr[1] ||= 42);`,
			"42",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, Opts{})
			got := RunLua(t, results, `mod["__result"]`, Opts{})
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
