package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

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
		skip string // non-empty to skip; describes the known gap
	}{
		{
			name: "basic closure capture",
			body: `const fns: (() => number)[] = [];
			for (let i = 0; i < 3; i++) {
				fns.push(() => i);
			}
			return fns.map(f => f()).join(",");`,
			want: `"0,1,2"`,
		},
		{
			name: "closure captures mutated let",
			body: `const fns: (() => number)[] = [];
			for (let i = 0; i < 3; i++) {
				const x = i * 10;
				fns.push(() => x + i);
			}
			return fns.map(f => f()).join(",");`,
			want: `"0,11,22"`,
		},
		{
			name: "var does not get per-iteration binding",
			body: `const fns: (() => number)[] = [];
			for (var i = 0; i < 3; i++) {
				fns.push(() => i);
			}
			return fns.map(f => f()).join(",");`,
			want: `"3,3,3"`,
		},
		{
			name: "body reassigns captured loop var - results",
			body: `const results: number[] = [];
			for (let i = 0; i < 10; i++) {
				results.push(i);
				if (i === 2) i = 8;
			}
			return results.join(",");`,
			want: `"0,1,2,9"`,
		},
		{
			name: "body reassigns captured loop var - closures",
			body: `const fns: (() => number)[] = [];
			for (let i = 0; i < 10; i++) {
				fns.push(() => i);
				if (i === 2) i = 8;
			}
			return fns.map(f => f()).join(",");`,
			want: `"0,1,8,9"`,
		},
		{
			name: "body reassigns captured loop var with continue",
			body: `const fns: (() => number)[] = [];
			for (let i = 0; i < 10; i++) {
				fns.push(() => i);
				if (i === 2) { i = 8; continue; }
			}
			return fns.map(f => f()).join(",");`,
			want: `"0,1,8,9"`,
		},
		{
			name: "captured loop var reassigned via destructuring - results",
			body: `const results: number[] = [];
			const fns: (() => number)[] = [];
			for (let i = 0; i < 10; i++) {
				fns.push(() => i);
				results.push(i);
				if (i === 2) [i] = [8];
			}
			return results.join(",");`,
			want: `"0,1,2,9"`,
		},
		{
			name: "captured loop var reassigned via iife - results",
			body: `const results: number[] = [];
			const fns: (() => number)[] = [];
			for (let i = 0; i < 10; i++) {
				fns.push(() => i);
				results.push(i);
				if (i === 2) (() => { i = 8; })();
			}
			return results.join(",");`,
			want: `"0,1,2,9"`,
		},
		{
			name: "captured loop var reassigned via named inner fn",
			body: `const results: number[] = [];
			const fns: (() => number)[] = [];
			for (let i = 0; i < 10; i++) {
				fns.push(() => i);
				results.push(i);
				const bump = () => { i = 8; };
				if (i === 2) bump();
			}
			return results.join(",");`,
			want: `"0,1,2,9"`,
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
				if tc.skip != "" {
					t.Skip(tc.skip)
				}
				ExpectFunction(t, tc.body, tc.want, Opts{EmitMode: m.mode})
			})
		}
	}
}
