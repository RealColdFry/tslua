package luatest

import "testing"

func TestFunctionCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"fn.call with this",
			`function greet(this: { name: string }) { return "hi " + this.name; }
			const obj = { name: "world" };
			return greet.call(obj);`,
			`"hi world"`,
		},
		{
			"fn.call with args",
			`function add(this: { n: number }, a: number) { return this.n + a; }
			return add.call({ n: 10 }, 5);`,
			`15`,
		},
		{
			"fn.apply with array args",
			`function sum(this: { n: number }, a: number) { return this.n + a; }
			return sum.apply({ n: 10 }, [20]);`,
			`30`,
		},
		{
			"fn.apply with no args",
			`function getValue(this: { x: number }) { return this.x; }
			return getValue.apply({ x: 42 });`,
			`42`,
		},
		{
			"fn.bind",
			`function greet(this: { name: string }) { return "hello " + this.name; }
			const bound = greet.bind({ name: "world" });
			return bound();`,
			`"hello world"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}
