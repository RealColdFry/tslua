package luatest

import "testing"

// TestCompoundAssignmentSideEffectIndex verifies that compound assignment on
// element access with a side-effect index expression (e.g. arr[i++] += 10)
// doesn't panic and produces correct results.
func TestCompoundAssignmentSideEffectIndex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
		want string
	}{
		{
			"arr[i++] += 10",
			`const arr: number[] = [1, 2, 3];
			let i: number = 0;
			arr[i++] += 10;
			export const __result = arr[0];`,
			"11",
		},
		{
			"arr[++i] += 10",
			`const arr: number[] = [1, 2, 3];
			let i: number = 0;
			arr[++i] += 10;
			export const __result = arr[1];`,
			"12",
		},
		{
			"arr[fn()] += value with side-effect key",
			`const arr: number[] = [10, 20, 30];
			let calls: number = 0;
			function idx(): number { calls++; return 1; }
			arr[idx()] += 5;
			export const __result = arr[1];`,
			"25",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, Opts{})
			got := RunLua(t, results, `mod["__result"]`, Opts{})
			if got != tc.want {
				t.Errorf("got %s, want %s\nLua:\n%s", got, tc.want, results[0].Lua)
			}
		})
	}
}
