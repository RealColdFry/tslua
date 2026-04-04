package luatest

import (
	"strings"
	"testing"
)

func TestLuaTable_Eval(t *testing.T) {
	t.Parallel()

	opts := OptsWithLanguageExtensions()

	cases := []struct {
		name string
		code string
		want string
	}{
		{"get & set stand-alone function", `
declare const getTable: LuaTableGet<{}, string, number>;
declare const setTable: LuaTableSet<{}, string, number>;
const tbl = {};
setTable(tbl, "foo", 3);
export const result = getTable(tbl, "foo");
`, "3"},

		{"get & set method", `
interface Table {
    get: LuaTableGetMethod<string, number>;
    set: LuaTableSetMethod<string, number>;
}
const tbl = {} as Table;
tbl.set("foo", 3);
export const result = tbl.get("foo");
`, "3"},

		{"get & set namespace function", `
declare namespace Table {
    export const get: LuaTableGet<{}, string, number>;
    export const set: LuaTableSet<{}, string, number>;
}
const tbl = {};
Table.set(tbl, "foo", 3);
export const result = Table.get(tbl, "foo");
`, "3"},

		{"has - true", `
declare const hasTable: LuaTableHas<{}, string>;
const tbl: any = { foo: 1 };
export const result = hasTable(tbl, "foo");
`, "true"},

		{"has - false", `
declare const hasTable: LuaTableHas<{}, string>;
const tbl: any = {};
export const result = hasTable(tbl, "foo");
`, "false"},

		{"delete", `
declare const deleteTable: LuaTableDelete<{}, string>;
const tbl: any = { foo: 1 };
deleteTable(tbl, "foo");
export const result = (tbl as any).foo === undefined;
`, "true"},

		{"isEmpty - true", `
declare const isEmpty: LuaTableIsEmpty<{}>;
const tbl = {};
export const result = isEmpty(tbl);
`, "true"},

		{"isEmpty - false", `
declare const isEmpty: LuaTableIsEmpty<{}>;
const tbl: any = { foo: 1 };
export const result = isEmpty(tbl);
`, "false"},

		{"new LuaTable", `
const tbl = new LuaTable<string, number>();
tbl.set("foo", 42);
export const result = tbl.get("foo");
`, "42"},

		{"addKey", `
interface MySet {
    addKey: LuaTableAddKeyMethod<string>;
    has: LuaTableHasMethod<string>;
}
const s = {} as MySet;
s.addKey("hello");
export const result = s.has("hello");
`, "true"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, opts)
			got := RunLua(t, results, `mod["result"]`, opts)
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRange_Eval(t *testing.T) {
	t.Parallel()

	opts := OptsWithLanguageExtensions()

	cases := []struct {
		name string
		body string
		want string
	}{
		{"basic", `
			const result: number[] = [];
			for (const i of $range(1, 5)) {
				result.push(i);
			}
			return result;
		`, "{1, 2, 3, 4, 5}"},

		{"with step", `
			const result: number[] = [];
			for (const i of $range(1, 10, 2)) {
				result.push(i);
			}
			return result;
		`, "{1, 3, 5, 7, 9}"},

		{"reverse", `
			const result: number[] = [];
			for (const i of $range(5, 1, -1)) {
				result.push(i);
			}
			return result;
		`, "{5, 4, 3, 2, 1}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, opts)
		})
	}
}

func TestMulti_Eval(t *testing.T) {
	t.Parallel()

	opts := OptsWithLanguageExtensions()

	// $multi in return: return $multi(a, b) → return a, b
	t.Run("return $multi", func(t *testing.T) {
		t.Parallel()
		code := `
function multi(): LuaMultiReturn<[number, string]> {
	return $multi(1, "hello");
}
export function __main() {
	const [a, b] = multi();
	return a + " " + b;
}
`
		results := TranspileTS(t, code, opts)
		got := RunLua(t, results, "mod.__main()", opts)
		if got != `"1 hello"` {
			t.Errorf("got %s, want \"1 hello\"", got)
		}
	})

	// Simpler multi-return destructuring (top-level module)
	t.Run("destructure multi-return", func(t *testing.T) {
		t.Parallel()
		code := `
declare function multi(): LuaMultiReturn<[number, string]>;
const [a, b] = multi();
export const result = a;
`
		results := TranspileTS(t, code, opts)
		for _, r := range results {
			if strings.HasSuffix(r.FileName, "main.ts") {
				if strings.Contains(r.Lua, "unpack") {
					t.Errorf("multi-return call should not use unpack:\n%s", r.Lua)
				}
			}
		}
	})

	// $multi codegen: verify the return emits bare values, not wrapped
	t.Run("$multi codegen", func(t *testing.T) {
		t.Parallel()
		code := `
export function foo(): LuaMultiReturn<[number, number]> {
	return $multi(1, 2);
}
`
		results := TranspileTS(t, code, opts)
		for _, r := range results {
			if strings.HasSuffix(r.FileName, "main.ts") {
				if !strings.Contains(r.Lua, "return 1, 2") {
					t.Errorf("expected 'return 1, 2' in output, got:\n%s", r.Lua)
				}
			}
		}
	})
}
