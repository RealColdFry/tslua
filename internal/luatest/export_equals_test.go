package luatest

import (
	"strings"
	"testing"
)

func TestExportEquals_SkipsEmptyTableInit(t *testing.T) {
	t.Parallel()
	// `export = expr` should produce `local ____exports = expr`, not
	// `local ____exports = {} ... ____exports = expr`.
	results := TranspileTS(t, `declare const globalVariable: number; export = globalVariable;`, Opts{})
	if len(results) != 1 {
		t.Fatalf("expected 1 file, got %d", len(results))
	}
	lua := results[0].Lua
	if !strings.Contains(lua, "local ____exports = globalVariable") {
		t.Errorf("expected direct init, got:\n%s", lua)
	}
	if strings.Contains(lua, "local ____exports = {}") {
		t.Errorf("should not have empty table init, got:\n%s", lua)
	}
}
