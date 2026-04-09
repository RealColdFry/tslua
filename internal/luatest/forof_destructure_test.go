package luatest

import "testing"

func TestForOfDestructure_Array(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"for-of with array destructuring",
			`const pairs: [string, number][] = [["a", 1], ["b", 2]];
			let result = "";
			for (const [k, v] of pairs) {
				result += k + v;
			}
			return result;`,
			`"a1b2"`,
		},
		{
			"for-of with nested array destructuring",
			`const data: [number, [number, number]][] = [[1, [2, 3]], [4, [5, 6]]];
			let sum = 0;
			for (const [a, [b, c]] of data) {
				sum += a + b + c;
			}
			return sum;`,
			`21`,
		},
		{
			"for-of with omitted elements",
			`const pairs: [string, number, boolean][] = [["a", 1, true], ["b", 2, false]];
			let result = "";
			for (const [k, , flag] of pairs) {
				result += k + flag;
			}
			return result;`,
			`"atruebfalse"`,
		},
		{
			"for-of with default value",
			`const pairs: [string, number?][] = [["a", 1], ["b"]];
			let sum = 0;
			for (const [, v = 99] of pairs) {
				sum += v;
			}
			return sum;`,
			`100`,
		},
		{
			"for-of with rest element",
			`const data: number[][] = [[1, 2, 3], [4, 5, 6]];
			let sum = 0;
			for (const [first, ...rest] of data) {
				sum += first + rest.length;
			}
			return sum;`,
			`9`,
		},
		{
			"for-of with object destructuring",
			`const items = [{ x: 1, y: 2 }, { x: 3, y: 4 }];
			let sum = 0;
			for (const { x, y } of items) {
				sum += x + y;
			}
			return sum;`,
			`10`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}
