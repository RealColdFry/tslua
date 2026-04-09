package luatest

import (
	"strings"
	"testing"
)

// TestAsyncOptionalChain_MapCallbackReturn verifies that return statements
// inside callbacks (e.g. Array.map) within an optional chain + nullish
// coalescing expression in an async function do not leak the outer
// awaiter_resolve into the callback body.
//
// Bug: `return s?.split(",").map(x => { ...; return v; }) ?? []` in an async
// function was emitting `return ____awaiter_resolve(nil, v)` inside the map
// callback instead of `return v`. This caused the promise to resolve with the
// first map element instead of the full array.
func TestAsyncOptionalChain_MapCallbackReturn(t *testing.T) {
	t.Parallel()

	t.Run("block body callback", func(t *testing.T) {
		t.Parallel()
		lua := ExpectFunctionLua(t, `
			async function foo() {
				const s: string | undefined = "a,b,c";
				return s?.split(",").map(x => { const v = x + "!"; return v; }) ?? [];
			}
			let result: any;
			foo().then(r => { result = r; });
			return result;
		`, `{"a!", "b!", "c!"}`, Opts{})
		// The map callback must NOT contain awaiter_resolve.
		// Look for the pattern: awaiter_resolve inside the map's callback body,
		// i.e. "return ____awaiter_resolve" after "function(____, x)".
		if strings.Contains(lua, "return ____awaiter_resolve(nil, v)") {
			t.Errorf("map callback should not wrap return in awaiter_resolve:\n%s", lua)
		}
	})
}
