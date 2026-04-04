package luatest

import (
	"testing"
)

// Caveat tests document known divergences between JS and Lua behavior that
// TSTL explicitly does not fix, and tslua follows.
// See: https://typescripttolua.github.io/docs/caveats
//
// These are "anti-tests": they assert the Lua output (which differs from JS).
// If any of these start matching JS behavior, it means the lualib changed
// and downstream optimizations (e.g., ipairs-based iteration) may need updating.

func TestCaveats_NilKeyDeletion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		luaWant string // what Lua produces (differs from JS)
		jsWould string // what JS would produce (for documentation)
	}{
		{
			"undefined deletes object key",
			`const foo: Record<string, any> = {};
			foo.someProp1 = 123;
			foo.someProp2 = undefined;
			foo.someProp3 = null;
			return Object.keys(foo).length;`,
			"1", // Lua: undefined/null → nil → key deleted
			"3", // JS: all 3 keys exist
		},
		{
			"null deletes object key",
			`const foo: Record<string, any> = { a: 1, b: null, c: 3 };
			return Object.keys(foo).length;`,
			"2", // Lua: b = nil → key doesn't exist
			"3", // JS: b exists with value null
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.luaWant, Opts{})
		})
	}
}

func TestCaveats_ArrayLength(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		luaWant string
		jsWould string
	}{
		{
			"undefined hole shortens array",
			`const arr: any[] = [1, 2, 3];
			arr[1] = undefined;
			return arr.length;`,
			"1", // Lua: #arr stops at first nil
			"3", // JS: length unchanged
		},
		{
			"sparse assignment doesn't extend length",
			`const arr: any[] = [1, 2, 3];
			arr[4] = 5;
			return arr.length;`,
			"3", // Lua: #arr doesn't see gap
			"5", // JS: length = highest index + 1
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.luaWant, Opts{})
		})
	}
}

func TestCaveats_SparseArrayIteration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		luaWant string
		jsWould string
	}{
		{
			"for-of stops at nil hole",
			`const arr: any[] = [1, undefined, 3];
			let count = 0;
			for (const v of arr) { count++; }
			return count;`,
			"1", // Lua: ipairs stops at nil
			"3", // JS: iterates all 3 including undefined
		},
		{
			"entries() stops at nil hole",
			`const arr: any[] = [1, undefined, 3];
			const keys: number[] = [];
			for (const [i] of arr.entries()) { keys.push(i); }
			return keys.length;`,
			"1", // Lua: __TS__ArrayEntries checks arr[key+1] == nil
			"3", // JS: entries() yields all indices
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.luaWant, Opts{})
		})
	}
}

// TestCaveats_BooleanCoercion documents that Lua treats NaN, "", and 0 as truthy
// while JS treats them as falsy. TSTL adheres to Lua's evaluation rules.
func TestCaveats_BooleanCoercion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		luaWant string
		jsWould string
	}{
		{
			"NaN is truthy in Lua",
			`const x: any = NaN;
			return x ? "truthy" : "falsy";`,
			`"truthy"`, // Lua: NaN is not nil/false → truthy
			`"falsy"`,  // JS: NaN is falsy
		},
		{
			"empty string is truthy in Lua",
			`const x: any = "";
			return x ? "truthy" : "falsy";`,
			`"truthy"`, // Lua: "" is not nil/false → truthy
			`"falsy"`,  // JS: "" is falsy
		},
		{
			"zero is truthy in Lua",
			`const x: any = 0;
			return x ? "truthy" : "falsy";`,
			`"truthy"`, // Lua: 0 is not nil/false → truthy
			`"falsy"`,  // JS: 0 is falsy
		},
		{
			"false is falsy in both",
			`const x: any = false;
			return x ? "truthy" : "falsy";`,
			`"falsy"`,
			`"falsy"`,
		},
		{
			"undefined is falsy in both",
			`const x: any = undefined;
			return x ? "truthy" : "falsy";`,
			`"falsy"`,
			`"falsy"`,
		},
		{
			"null is falsy in both",
			`const x: any = null;
			return x ? "truthy" : "falsy";`,
			`"falsy"`,
			`"falsy"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.luaWant, Opts{})
		})
	}
}

// TestCaveats_LooseEquality documents that TSTL treats == and === identically,
// compiling both to Lua's == (which is always strict).
func TestCaveats_LooseEquality(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		luaWant string
		jsWould string
	}{
		{
			"== does not coerce number/string",
			`const x: any = 1;
			const y: any = "1";
			return x == y;`,
			"false", // Lua: 1 == "1" is false (strict)
			"true",  // JS: 1 == "1" is true (loose coercion)
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.luaWant, Opts{})
		})
	}
}

// TestCaveats_NullUndefined documents that both null and undefined transpile to nil.
func TestCaveats_NullUndefined(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		luaWant string
		jsWould string
	}{
		{
			"null and undefined are identical",
			`const a: any = null;
			const b: any = undefined;
			return a === b;`,
			"true",  // Lua: both are nil
			"false", // JS: null !== undefined (strict equality)
		},
		{
			"typeof null returns undefined not object",
			`const x: any = null;
			return typeof x;`,
			`"undefined"`, // Lua: null → nil, __TS__TypeOf(nil) → "undefined"
			`"object"`,    // JS: typeof null == "object"
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.luaWant, Opts{})
		})
	}
}
