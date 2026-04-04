// Tests for transpiler correctness on patterns found in TSTL's lualib source.
// These catch regressions that break the tslua-built lualib bundle.
//
// The lualib bundle is built with ExportAsGlobal mode, which strips the module
// wrapper and emits exports as globals. These tests verify correctness in that mode.
package luatest

import (
	"strings"
	"testing"
)

// TestLualib_ExportAsGlobalLocal verifies that exported functions in ExportAsGlobal
// mode emit "local function" rather than bare "function" (which leaks to _G).
// TSTL's bundle has all functions as "local function" since they're referenced
// by the exports table at the bottom.
func TestLualib_ExportAsGlobalLocal(t *testing.T) {
	t.Parallel()

	tsCode := `export function helperA(x: number) { return x + 1; }
export function helperB(x: number) { return x * 2; }
`
	results := TranspileTS(t, tsCode, Opts{ExportAsGlobal: true})
	lua := mainLua(t, results)

	for i, line := range strings.Split(lua, "\n") {
		trimmed := strings.TrimSpace(line)
		// A bare "function name(" at top level without "local" is the bug
		if strings.HasPrefix(trimmed, "function ") &&
			!strings.HasPrefix(trimmed, "function(") &&
			line == trimmed {
			t.Errorf("line %d: exported function should be 'local function' in ExportAsGlobal mode: %s", i+1, line)
		}
	}
}

// TestLualib_ConstantFolding verifies that index expressions like [x - 1 + 1]
// are simplified to [x]. While not a correctness bug (the arithmetic is a no-op),
// TSTL folds these and the lualib relies on clean output.
func TestLualib_ConstantFolding(t *testing.T) {
	t.Parallel()

	// Mirrors TSTL's lualib pattern: $range loop with 0-based array access
	tsCode := `export function copyItems(result: any[], items: any[]) {
    let len = 0;
    for (const i of $range(1, items.length)) {
        len++;
        result[len - 1] = items[i - 1];
    }
    return len;
}
`
	results := TranspileTS(t, tsCode, OptsWithLanguageExtensions())
	lua := mainLua(t, results)

	if strings.Contains(lua, "- 1 + 1") {
		t.Errorf("unfold constant arithmetic (- 1 + 1) should simplify to identity:\n%s", lua)
	}
}

// TestLualib_DoEndScoping verifies that ExportAsGlobal mode wraps block-scoped
// variables in do..end blocks, matching TSTL's bundle output.
// TSTL's lualib uses patterns like:
//
//	local __TS__Symbol, Symbol
//	do
//	  local symbolMetatable = { ... }
//	  function __TS__Symbol(...) ... end
//	  Symbol = { ... }
//	end
func TestLualib_DoEndScoping(t *testing.T) {
	t.Parallel()

	// Block-scoped locals that should not leak to module scope
	tsCode := `
let outerVar: string;
{
    const scoped = "hello";
    outerVar = scoped;
}
export const result = outerVar;
`
	results := TranspileTS(t, tsCode, Opts{ExportAsGlobal: true})
	lua := mainLua(t, results)

	// "scoped" should be inside a do..end block, not at module scope
	if !strings.Contains(lua, "do\n") {
		t.Errorf("expected do..end block for block-scoped variable:\n%s", lua)
	}
}

func mainLua(t *testing.T, results []TranspileResult) string {
	t.Helper()
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			return r.Lua
		}
	}
	t.Fatal("no main.ts in results")
	return ""
}
