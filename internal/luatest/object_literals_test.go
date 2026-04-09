package luatest

import (
	"strings"
	"testing"
)

func TestObjectLiteral_ComputedProperty(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const key = "hello";
		const obj: Record<string, number> = { [key]: 42 };
		return obj.hello;
	`, `42`, Opts{})
}

func TestObjectLiteral_MethodDeclaration(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const obj = {
			greet(name: string) { return "hi " + name; }
		};
		return obj.greet("world");
	`, `"hi world"`, Opts{})
}

func TestObjectLiteral_ShorthandProperty(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const x = 10;
		const y = 20;
		const obj = { x, y };
		return obj.x + obj.y;
	`, `30`, Opts{})
}

func TestObjectLiteral_SpreadAssignment(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const base = { a: 1, b: 2 };
		const extended = { ...base, c: 3 };
		return extended.a + extended.b + extended.c;
	`, `6`, Opts{})
}

func TestObjectLiteral_SpreadWithOverride(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const base = { a: 1, b: 2 };
		const result = { ...base, b: 99 };
		return result.b;
	`, `99`, Opts{})
}

func TestObjectLiteral_NumericKey(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const obj: Record<number, string> = { 0: "zero", 1: "one" };
		return obj[0];
	`, `"zero"`, Opts{})
}

func TestObjectLiteral_StringKey(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const obj = { "my-key": 42 };
		return obj["my-key"];
	`, `42`, Opts{})
}

func TestObjectLiteral_SpreadWithComputed(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const base = { a: 1 };
		const key = "b";
		const obj: Record<string, number> = { ...base, [key]: 2 };
		return obj.a + obj.b;
	`, `3`, Opts{})
}

func TestObjectLiteral_SpreadWithShorthand(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const base = { a: 1 };
		const b = 2;
		const obj = { ...base, b };
		return obj.a + obj.b;
	`, `3`, Opts{})
}

func TestObjectLiteral_SpreadWithMethod(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		const base = { x: 10 };
		const obj = { ...base, double() { return this.x * 2; } };
		return obj.double();
	`, `20`, Opts{})
}

func TestObjectLiteral_ComputedPropertyCodegen(t *testing.T) {
	t.Parallel()
	code := `export function __main() {
		const key = "x";
		const obj: Record<string, number> = { [key]: 1 };
		return obj.x;
	}`
	results := TranspileTS(t, code, Opts{})
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			if !strings.Contains(r.Lua, "[key]") {
				t.Errorf("expected computed key bracket notation, got:\n%s", r.Lua)
			}
		}
	}
}
