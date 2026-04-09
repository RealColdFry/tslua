package luatest

import (
	"strings"
	"testing"
)

// TestArrayPush_SideEffectCaching verifies that array.push() caches the
// receiver expression when it has side effects, avoiding double evaluation.
func TestArrayPush_SideEffectCaching(t *testing.T) {
	t.Parallel()

	t.Run("function call receiver evaluated once", func(t *testing.T) {
		t.Parallel()
		ExpectFunction(t, `
			let count = 0;
			function getArray(): number[] { count++; return [1,2,3]; }
			getArray().push(42);
			return count;
		`, "1", Opts{})
	})

	t.Run("simple identifier not cached", func(t *testing.T) {
		t.Parallel()
		lua := ExpectFunctionLua(t, `
			const arr: number[] = [1,2,3];
			arr.push(42);
			return arr.length;
		`, "4", Opts{})
		// Simple const identifier should inline, not cache to temp.
		if strings.Contains(lua, "local ____arr") {
			t.Errorf("simple identifier should not be cached to temp, got:\n%s", lua)
		}
	})

	t.Run("property chain receiver cached", func(t *testing.T) {
		t.Parallel()
		ExpectFunction(t, `
			let count = 0;
			const obj = { getArr(): number[] { count++; return [1,2,3]; } };
			obj.getArr().push(42);
			return count;
		`, "1", Opts{})
	})
}
