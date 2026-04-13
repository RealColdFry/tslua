package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDependencies_TranspiledOnly(t *testing.T) {
	t.Parallel()

	// When all dependencies are internal (not external), ResolveDependencies
	// should return exactly the transpiled files with no extras.
	results := []transpiler.TranspileResult{
		{
			FileName: "/project/src/main.ts",
			Lua:      `local helper = require("helper")`,
			Dependencies: []transpiler.ModuleDependency{
				{RequirePath: "helper", ResolvedPath: "/project/src/helper.ts", IsExternal: false},
			},
		},
		{
			FileName: "/project/src/helper.ts",
			Lua:      `local ____exports = {}; return ____exports`,
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: "/project/src"})

	if len(res.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(res.Files))
	}
	if len(res.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", res.Errors)
	}
	for _, f := range res.Files {
		if !f.IsTranspiled {
			t.Errorf("expected all files to be transpiled, got external: %s", f.FileName)
		}
	}
}

func TestResolveDependencies_ExternalLua(t *testing.T) {
	t.Parallel()

	// An external .lua dependency should be read from disk and included.
	tmp := t.TempDir()
	luaPath := filepath.Join(tmp, "node_modules", "mylib", "init.lua")
	writeFile(t, luaPath, `return { greet = function() return "hello" end }`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "main.ts"),
			Lua:      `local mylib = require("mylib")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "mylib",
					ResolvedPath: luaPath,
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: filepath.Join(tmp, "src")})

	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Files) != 2 {
		t.Fatalf("expected 2 files (1 transpiled + 1 external), got %d", len(res.Files))
	}

	ext := res.Files[1]
	if ext.IsTranspiled {
		t.Error("expected external file to have IsTranspiled=false")
	}
	if ext.FileName != luaPath {
		t.Errorf("expected FileName=%s, got %s", luaPath, ext.FileName)
	}
	if ext.Lua != `return { greet = function() return "hello" end }` {
		t.Errorf("unexpected Lua content: %s", ext.Lua)
	}
}

func TestResolveDependencies_ExternalDtsOnly(t *testing.T) {
	t.Parallel()

	// An external dependency that is .d.ts only (IsLuaSource=false) should
	// NOT be included in the output.
	results := []transpiler.TranspileResult{
		{
			FileName: "/project/src/main.ts",
			Lua:      `local types = require("types")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "types",
					ResolvedPath: "/project/node_modules/@types/mylib/index.d.ts",
					IsExternal:   true,
					IsLuaSource:  false,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: "/project/src"})

	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file (transpiled only), got %d", len(res.Files))
	}
}

func TestResolveDependencies_LibraryModeExcludesNodeModules(t *testing.T) {
	t.Parallel()

	// In library mode, external .lua files in node_modules should be excluded.
	tmp := t.TempDir()
	luaPath := filepath.Join(tmp, "node_modules", "dep", "init.lua")
	writeFile(t, luaPath, `return {}`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "main.ts"),
			Lua:      `local dep = require("dep")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "dep",
					ResolvedPath: luaPath,
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{
		SourceRoot: filepath.Join(tmp, "src"),
		BuildMode:  BuildModeLibrary,
	})

	if len(res.Files) != 1 {
		t.Fatalf("library mode should exclude node_modules deps, got %d files", len(res.Files))
	}
	if !res.Files[0].IsTranspiled {
		t.Error("only file should be the transpiled source")
	}
}

func TestResolveDependencies_LibraryModeIncludesNonNodeModules(t *testing.T) {
	t.Parallel()

	// In library mode, external .lua files NOT in node_modules should still
	// be included (e.g. local lua sources in the project).
	tmp := t.TempDir()
	luaPath := filepath.Join(tmp, "lua_modules", "helper.lua")
	writeFile(t, luaPath, `return { x = 1 }`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "main.ts"),
			Lua:      `local helper = require("helper")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "helper",
					ResolvedPath: luaPath,
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{
		SourceRoot: filepath.Join(tmp, "src"),
		BuildMode:  BuildModeLibrary,
	})

	if len(res.Files) != 2 {
		t.Fatalf("library mode should include non-node_modules external deps, got %d files", len(res.Files))
	}
}

func TestResolveDependencies_DefaultModeIncludesNodeModules(t *testing.T) {
	t.Parallel()

	// In default mode, external .lua files in node_modules SHOULD be included.
	tmp := t.TempDir()
	luaPath := filepath.Join(tmp, "node_modules", "dep", "init.lua")
	writeFile(t, luaPath, `return {}`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "main.ts"),
			Lua:      `local dep = require("dep")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "dep",
					ResolvedPath: luaPath,
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{
		SourceRoot: filepath.Join(tmp, "src"),
		BuildMode:  BuildModeDefault,
	})

	if len(res.Files) != 2 {
		t.Fatalf("default mode should include node_modules deps, got %d files", len(res.Files))
	}
}

func TestResolveDependencies_RecursiveExternalDeps(t *testing.T) {
	t.Parallel()

	// An external .lua file that itself requires another .lua file should
	// cause recursive discovery via FindLuaRequires.
	tmp := t.TempDir()
	libDir := filepath.Join(tmp, "node_modules", "mylib")
	writeFile(t, filepath.Join(libDir, "init.lua"),
		`local util = require("util")
return { x = util.x }`)
	writeFile(t, filepath.Join(libDir, "util.lua"),
		`return { x = 42 }`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "main.ts"),
			Lua:      `local mylib = require("mylib")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "mylib",
					ResolvedPath: filepath.Join(libDir, "init.lua"),
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: filepath.Join(tmp, "src")})

	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Files) != 3 {
		t.Fatalf("expected 3 files (1 transpiled + 2 external), got %d", len(res.Files))
	}

	// Verify the recursive dep was found
	found := false
	for _, f := range res.Files {
		if f.FileName == filepath.Join(libDir, "util.lua") {
			found = true
			if f.IsTranspiled {
				t.Error("util.lua should not be marked as transpiled")
			}
		}
	}
	if !found {
		t.Error("recursive dependency util.lua was not discovered")
	}
}

func TestResolveDependencies_DeduplicatesDeps(t *testing.T) {
	t.Parallel()

	// If two transpiled files depend on the same external .lua, it should
	// only be included once.
	tmp := t.TempDir()
	luaPath := filepath.Join(tmp, "node_modules", "shared", "init.lua")
	writeFile(t, luaPath, `return {}`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "a.ts"),
			Lua:      `local shared = require("shared")`,
			Dependencies: []transpiler.ModuleDependency{
				{RequirePath: "shared", ResolvedPath: luaPath, IsExternal: true, IsLuaSource: true},
			},
		},
		{
			FileName: filepath.Join(tmp, "src", "b.ts"),
			Lua:      `local shared = require("shared")`,
			Dependencies: []transpiler.ModuleDependency{
				{RequirePath: "shared", ResolvedPath: luaPath, IsExternal: true, IsLuaSource: true},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: filepath.Join(tmp, "src")})

	if len(res.Files) != 3 {
		t.Fatalf("expected 3 files (2 transpiled + 1 external), got %d", len(res.Files))
	}
}

func TestResolveDependencies_MissingExternalFile(t *testing.T) {
	t.Parallel()

	// If an external .lua file doesn't exist on disk, it should produce an error
	// but not crash.
	results := []transpiler.TranspileResult{
		{
			FileName: "/project/src/main.ts",
			Lua:      `local x = require("missing")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "missing",
					ResolvedPath: "/project/node_modules/missing/init.lua",
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: "/project/src"})

	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error for missing file, got %d: %v", len(res.Errors), res.Errors)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected only the transpiled file, got %d", len(res.Files))
	}
}

func TestResolveDependencies_NoResolvePath(t *testing.T) {
	t.Parallel()

	// Dependencies with empty ResolvedPath (from noResolvePaths) should be
	// silently skipped.
	results := []transpiler.TranspileResult{
		{
			FileName: "/project/src/main.ts",
			Lua:      `local ext = require("extmod")`,
			Dependencies: []transpiler.ModuleDependency{
				{RequirePath: "extmod", ResolvedPath: ""},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: "/project/src"})

	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	if len(res.Errors) != 0 {
		t.Fatalf("expected no errors for noResolvePaths dep, got %v", res.Errors)
	}
}

func TestResolveDependencies_CircularExternalDeps(t *testing.T) {
	t.Parallel()

	// Circular requires between external .lua files should not cause infinite
	// recursion. The seen set should break the cycle.
	tmp := t.TempDir()
	libDir := filepath.Join(tmp, "node_modules", "circular")
	writeFile(t, filepath.Join(libDir, "a.lua"),
		`local b = require("b"); return {}`)
	writeFile(t, filepath.Join(libDir, "b.lua"),
		`local a = require("a"); return {}`)

	results := []transpiler.TranspileResult{
		{
			FileName: filepath.Join(tmp, "src", "main.ts"),
			Lua:      `local a = require("a")`,
			Dependencies: []transpiler.ModuleDependency{
				{
					RequirePath:  "a",
					ResolvedPath: filepath.Join(libDir, "a.lua"),
					IsExternal:   true,
					IsLuaSource:  true,
				},
			},
		},
	}

	res := ResolveDependencies(results, Options{SourceRoot: filepath.Join(tmp, "src")})

	// Should not hang. Both files should be included exactly once.
	externalCount := 0
	for _, f := range res.Files {
		if !f.IsTranspiled {
			externalCount++
		}
	}
	// a.lua is included. b.lua requires a.lua which resolves relative to b.lua's
	// directory, so it may or may not find a sibling "a.lua". The key assertion
	// is that we don't infinite loop.
	if externalCount < 1 {
		t.Errorf("expected at least 1 external file, got %d", externalCount)
	}
}

func TestIsNodeModulesPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"/project/node_modules/foo/init.lua", true},
		{"/project/src/helper.lua", false},
		{"/project/lua_modules/bar.lua", false},
		{"/project/node_modules/@scope/pkg/index.lua", true},
		{"node_modules/x.lua", true},
	}

	for _, tt := range tests {
		if got := isNodeModulesPath(tt.path); got != tt.want {
			t.Errorf("isNodeModulesPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
