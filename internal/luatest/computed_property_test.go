package luatest

import (
	"strings"
	"testing"
)

func TestComputedPropertyKeys(t *testing.T) {
	t.Parallel()

	t.Run("post-increment computed key", func(t *testing.T) {
		t.Parallel()
		lua := ExpectFunctionLua(t, `
			let c = 0;
			const obj = { [c++]: "a", [c++]: "b", [c++]: "c" };
			return [c, obj[0], obj[1], obj[2]];`,
			`{3, "a", "b", "c"}`, Opts{})
		// The temp variables must use bracket notation [____c_X] not identifier notation ____c_X
		if strings.Contains(lua, "____c_0 =") && !strings.Contains(lua, "[____c_0]") {
			t.Errorf("computed key emitted as identifier instead of bracket notation:\n%s", lua)
		}
	})

	t.Run("pre-increment computed key", func(t *testing.T) {
		t.Parallel()
		lua := ExpectFunctionLua(t, `
			let c = 0;
			const obj = { [++c]: "a", [++c]: "b" };
			return [c, obj[1], obj[2]];`,
			`{2, "a", "b"}`, Opts{})
		if strings.Contains(lua, "____c_") && !strings.Contains(lua, "[") {
			t.Errorf("computed key emitted as identifier instead of bracket notation:\n%s", lua)
		}
	})

	t.Run("variable computed key with side effects on other properties", func(t *testing.T) {
		t.Parallel()
		ExpectFunction(t, `
			let x = 0;
			const key = "k";
			const obj = { [key]: "val", other: x++ };
			return [obj["k"], x];`,
			`{"val", 1}`, Opts{})
	})

	t.Run("numeric literal computed key", func(t *testing.T) {
		t.Parallel()
		ExpectFunction(t, `
			const obj = { [0]: "a", [1]: "b", [2]: "c" };
			return [obj[0], obj[1], obj[2]];`,
			`{"a", "b", "c"}`, Opts{})
	})
}
