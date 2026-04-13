package luatest

import (
	"strings"
	"testing"
)

func TestNamespaceExport_EmptyCreatesTable(t *testing.T) {
	t.Parallel()
	// An empty exported namespace should still emit ____exports.TestSpace = {}
	results := TranspileTS(t, `export namespace TestSpace {}`, Opts{})
	lua := results[0].Lua
	if !strings.Contains(lua, "____exports.TestSpace = {}") {
		t.Errorf("expected exported table creation, got:\n%s", lua)
	}
	if strings.Contains(lua, "local TestSpace") {
		t.Errorf("should not have local alias for empty namespace, got:\n%s", lua)
	}
}

func TestNamespaceExport_NoAliasWithoutExportedMembers(t *testing.T) {
	t.Parallel()
	// Namespace with non-exported members: table created, no local alias
	results := TranspileTS(t, `export namespace TestSpace { function innerFunc() {} }`, Opts{})
	lua := results[0].Lua
	if !strings.Contains(lua, "____exports.TestSpace = {}") {
		t.Errorf("expected exported table creation, got:\n%s", lua)
	}
	if strings.Contains(lua, "local TestSpace = ____exports.TestSpace") {
		t.Errorf("should not have local alias without exported members, got:\n%s", lua)
	}
}

func TestNamespaceExport_AliasWithExportedMembers(t *testing.T) {
	t.Parallel()
	// Namespace with exported members: table + local alias
	results := TranspileTS(t, `export namespace TestSpace { export function innerFunc() {} }`, Opts{})
	lua := results[0].Lua
	if !strings.Contains(lua, "____exports.TestSpace = {}") {
		t.Errorf("expected exported table creation, got:\n%s", lua)
	}
	if !strings.Contains(lua, "local TestSpace = ____exports.TestSpace") {
		t.Errorf("expected local alias for namespace with exports, got:\n%s", lua)
	}
}
