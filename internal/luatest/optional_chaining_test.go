package luatest

import "testing"

func TestOptionalChaining_UnwrapCallee(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"optional call with non-null assertion",
			`const obj: { fn?: () => number } = { fn: () => 42 };
			return obj.fn!();`,
			`42`,
		},
		{
			"optional call with as assertion",
			`const obj: { fn?: () => number } = { fn: () => 42 };
			return (obj.fn as () => number)();`,
			`42`,
		},
		{
			"optional chain on property",
			`const obj: { a?: { b: number } } = { a: { b: 99 } };
			return obj.a?.b ?? 0;`,
			`99`,
		},
		{
			"optional chain returns nil",
			`const obj: { a?: { b: number } } = {};
			return obj.a?.b ?? -1;`,
			`-1`,
		},
		{
			"optional method call",
			`const obj: { greet?: (s: string) => string } = { greet: (s) => "hi " + s };
			return obj.greet?.("world") ?? "none";`,
			`"hi world"`,
		},
		{
			"optional method call on nil",
			`const obj: { greet?: (s: string) => string } = {};
			return obj.greet?.("world") ?? "none";`,
			`"none"`,
		},
		{
			"optional chain with parenthesized expression",
			`const obj: { a?: { b: number } } = { a: { b: 5 } };
			return (obj.a)?.b ?? 0;`,
			`5`,
		},
		{
			"optional chain with satisfies",
			`const obj: { a?: { b: number } } = { a: { b: 7 } };
			return (obj.a satisfies { b: number } | undefined)?.b ?? 0;`,
			`7`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}
