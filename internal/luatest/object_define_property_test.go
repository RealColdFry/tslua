package luatest

import (
	"testing"
)

// Tests for Object.defineProperty behavior.
// See: https://github.com/TypeScriptToLua/TypeScriptToLua/issues/1625

func TestObjectDefineProperty_InstanceIsolation(t *testing.T) {
	t.Parallel()

	// Each instance should get its own value from Object.defineProperty in the constructor.
	// TSTL bug: descriptors are stored on the shared metatable, so all instances share the same value.
	ExpectFunction(t, `
		class Test {
			declare obj: object;
			constructor() {
				Object.defineProperty(this, "obj", { value: {}, writable: true, configurable: true });
			}
		}
		const t1 = new Test();
		const t2 = new Test();
		return t1.obj === t2.obj;
	`, "false", Opts{})
}
