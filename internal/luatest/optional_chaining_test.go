package luatest

import (
	"strings"
	"testing"
)

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

// TestOptionalChaining_LengthNilGuard verifies that optional chaining on .length
// preserves the nil guard. s?.length must not crash when s is nil.
// Found via awesome-config wild testing: output?.length > 0 dropped the nil check.
func TestOptionalChaining_LengthNilGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"optional length on nil returns nil",
			`function test(s: string | undefined): number | undefined { return s?.length; }
			return test(undefined) ?? -1;`,
			`-1`,
		},
		{
			"optional length on string returns length",
			`function test(s: string | undefined): number | undefined { return s?.length; }
			return test("hello") ?? -1;`,
			`5`,
		},
		{
			"optional length in comparison",
			`function test(s: string | undefined): boolean { return (s?.length ?? 0) > 0; }
			return test(undefined);`,
			`false`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

// TestOptionalChaining_LengthCodegen verifies that s?.length emits a nil guard
// in the Lua output (not just bare #s which crashes on nil).
func TestOptionalChaining_LengthCodegen(t *testing.T) {
	t.Parallel()

	code := `function test(s: string | undefined): number | undefined { return s?.length; }
	export function __main() { return test(undefined); }`
	results := TranspileTS(t, code, Opts{})
	lua := results[0].Lua
	// Should NOT contain bare "#s" without a nil guard
	if strings.Contains(lua, "return #s") && !strings.Contains(lua, "s and") {
		t.Errorf("optional chain on .length should include nil guard, got:\n%s", lua)
	}
}
