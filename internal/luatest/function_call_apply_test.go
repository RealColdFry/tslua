package luatest

import (
	"strings"
	"testing"
)

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

// TestMethodNamedCall verifies that a user-defined method named "call", "apply",
// or "bind" is NOT intercepted as Function.prototype.call/apply/bind.
func TestMethodNamedCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
		want string // substring expected in Lua output
	}{
		{
			"method named call is not intercepted",
			`interface DBus { session(): { call(service: string): string } }
			declare const dbus: DBus;
			export function __main() { return dbus.session().call("org.test"); }`,
			`:call(`,
		},
		{
			"method named apply is not intercepted",
			`interface Obj { apply(x: string): string }
			declare const obj: Obj;
			export function __main() { return obj.apply("test"); }`,
			`:apply(`,
		},
		{
			"method named bind is not intercepted",
			`interface Obj { bind(x: string): string }
			declare const obj: Obj;
			export function __main() { return obj.bind("test"); }`,
			`:bind(`,
		},
		{
			"chained method named call",
			`interface Session { call(service: string, path: string): void }
			interface DBus { system(): Session; session(): Session }
			declare const dbus: DBus;
			export function __main() { dbus.system().call("org.test", "/path"); }`,
			`:call(`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, Opts{})
			luaCode := results[0].Lua
			if !strings.Contains(luaCode, tc.want) {
				t.Errorf("expected Lua to contain %q\ngot:\n%s", tc.want, luaCode)
			}
		})
	}
}
