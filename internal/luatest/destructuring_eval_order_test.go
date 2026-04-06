package luatest

import "testing"

func TestDestructuring_EvalOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"pre-increment in array destructuring RHS",
			`const arr = [1, 2];
			let i = 0;
			let [v1, v2] = [arr[i], arr[++i]];
			return v1 + "," + v2;`,
			`"1,2"`,
		},
		{
			"pre-decrement in array destructuring RHS",
			`const arr = [1, 2];
			let i = 1;
			let [v1, v2] = [arr[i], arr[--i]];
			return v1 + "," + v2;`,
			`"2,1"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}
