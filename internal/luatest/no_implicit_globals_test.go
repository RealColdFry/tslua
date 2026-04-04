// Tests for noImplicitGlobalVariables option.
package luatest

import (
	"strings"
	"testing"
)

func TestNoImplicitGlobalVariables(t *testing.T) {
	t.Parallel()

	tsCode := `function foo() {}
const bar = 123;`

	// Without the option: script-mode declarations are global
	t.Run("default creates globals", func(t *testing.T) {
		t.Parallel()
		results := TranspileTS(t, tsCode, Opts{})
		lua := mainLua(t, results)
		if strings.Contains(lua, "local") {
			t.Errorf("expected no local declarations in script mode, got:\n%s", lua)
		}
	})

	// With the option: all declarations forced to local
	t.Run("noImplicitGlobalVariables forces local", func(t *testing.T) {
		t.Parallel()
		results := TranspileTS(t, tsCode, Opts{NoImplicitGlobalVariables: true})
		lua := mainLua(t, results)
		if !strings.Contains(lua, "local function foo(") {
			t.Errorf("expected 'local function foo(', got:\n%s", lua)
		}
		if !strings.Contains(lua, "local bar =") {
			t.Errorf("expected 'local bar =', got:\n%s", lua)
		}
	})
}
