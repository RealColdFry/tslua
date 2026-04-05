package luatest

import "testing"

// TestForLoopClosureCapture verifies that `let` variables in C-style for loops
// get per-iteration bindings when captured by closures (ES6 §13.7.4.9).
// Bug: both TSTL and tslua emit a single shared `local i` so all closures
// see the final value instead of their iteration's value.
func TestForLoopClosureCapture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"basic closure capture",
			`const fns: (() => number)[] = [];
			for (let i = 0; i < 3; i++) {
				fns.push(() => i);
			}
			return fns.map(f => f()).join(",");`,
			`"0,1,2"`,
		},
		{
			"closure captures mutated let",
			`const fns: (() => number)[] = [];
			for (let i = 0; i < 3; i++) {
				const x = i * 10;
				fns.push(() => x + i);
			}
			return fns.map(f => f()).join(",");`,
			`"0,11,22"`,
		},
		{
			"var does not get per-iteration binding",
			`const fns: (() => number)[] = [];
			for (var i = 0; i < 3; i++) {
				fns.push(() => i);
			}
			return fns.map(f => f()).join(",");`,
			`"3,3,3"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}
