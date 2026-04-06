package luatest

import (
	"strings"
	"testing"
)

func TestPrinter_PrefixParentheses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ts   string
		want string // substring that must appear in Lua output
	}{
		{"binary property access", `declare var x: number; (x + 1 as any).foo;`, "(x + 1).foo"},
		{"binary method call", `declare var x: number; (x + 1 as any).foo();`, "(x + 1):foo()"},
		{"binary index", `declare var x: number; (x + 1 as any)["foo"];`, `(x + 1).foo`},
		{"exponent property access", `declare var x: number; (x ** 2 as any).foo;`, "(x ^ 2).foo"},
		{"double cast property access", `declare var x: number; (x + 1 as unknown as any).foo;`, "(x + 1).foo"},
		{"unary property access", `declare var x: boolean; (!x as any).toString();`, "(not x)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.ts, Opts{})
			lua := results[0].Lua
			if !strings.Contains(lua, tc.want) {
				t.Errorf("expected %q in output:\n%s", tc.want, lua)
			}
		})
	}
}
