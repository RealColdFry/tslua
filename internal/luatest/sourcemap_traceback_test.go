package luatest

import (
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestSourceMapTraceback_Ordering verifies that the __TS__SourceMapTraceBack call
// is emitted AFTER the lualib block for both require and inline modes.
func TestSourceMapTraceback_Ordering(t *testing.T) {
	t.Parallel()

	code := `export function foo() { return "bar"; }`

	t.Run("require mode", func(t *testing.T) {
		t.Parallel()
		results := TranspileTS(t, code, Opts{
			SourceMapTraceback: true,
			LuaLibImport:       transpiler.LuaLibImportRequire,
		})
		if len(results) == 0 {
			t.Fatal("no transpile results")
		}
		lua := results[0].Lua

		lualibIdx := strings.Index(lua, `require("lualib_bundle")`)
		tracebackIdx := strings.Index(lua, "__TS__SourceMapTraceBack(debug.getinfo(1)")
		userCodeIdx := strings.Index(lua, "____exports")

		if lualibIdx < 0 {
			t.Fatal("lualib require not found in output")
		}
		if tracebackIdx < 0 {
			t.Fatal("traceback call not found in output")
		}
		if userCodeIdx < 0 {
			t.Fatal("user code not found in output")
		}
		if tracebackIdx < lualibIdx {
			t.Errorf("traceback call appears before lualib require:\n%s", lua)
		}
		if userCodeIdx < tracebackIdx {
			t.Errorf("user code appears before traceback call:\n%s", lua)
		}
	})

	t.Run("inline mode", func(t *testing.T) {
		t.Parallel()
		results := TranspileTS(t, code, Opts{
			SourceMapTraceback: true,
			LuaLibImport:       transpiler.LuaLibImportInline,
		})
		if len(results) == 0 {
			t.Fatal("no transpile results")
		}
		lua := results[0].Lua

		inlineEndIdx := strings.Index(lua, "-- End of Lua Library inline imports")
		tracebackIdx := strings.Index(lua, "__TS__SourceMapTraceBack(debug.getinfo(1)")
		userCodeIdx := strings.Index(lua, "____exports")

		if inlineEndIdx < 0 {
			t.Fatal("inline lualib end marker not found in output")
		}
		if tracebackIdx < 0 {
			t.Fatal("traceback call not found in output")
		}
		if userCodeIdx < 0 {
			t.Fatal("user code not found in output")
		}
		if tracebackIdx < inlineEndIdx {
			t.Errorf("traceback call appears before inline lualib block:\n%s", lua)
		}
		if userCodeIdx < tracebackIdx {
			t.Errorf("user code appears before traceback call:\n%s", lua)
		}
	})
}
