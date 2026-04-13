package luatest

import (
	"testing"
)

// TestDependencies_InternalModule verifies that importing a project-local .ts
// file records a dependency with IsExternal=false and IsLuaSource=false.
func TestDependencies_InternalModule(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { foo } from "./helper";
		export function __main() { return foo; }
	`, Opts{
		ExtraFiles: map[string]string{
			"helper.ts": `export const foo = 42;`,
		},
	})

	// Find the main file's result
	var mainDeps []struct {
		req, resolved string
		ext, lua      bool
	}
	for _, r := range results {
		if r.FileName == "main.ts" {
			for _, d := range r.Dependencies {
				mainDeps = append(mainDeps, struct {
					req, resolved string
					ext, lua      bool
				}{
					d.RequirePath, d.ResolvedPath, d.IsExternal, d.IsLuaSource,
				})
			}
		}
	}

	if len(mainDeps) == 0 {
		t.Fatal("expected at least one dependency for main.ts, got none")
	}

	found := false
	for _, d := range mainDeps {
		if d.req == "helper" {
			found = true
			if d.ext {
				t.Error("helper should not be marked as external")
			}
			if d.lua {
				t.Error("helper.ts should not be marked as IsLuaSource")
			}
			if d.resolved == "" {
				t.Error("helper should have a resolved path")
			}
		}
	}
	if !found {
		t.Errorf("did not find dependency with RequirePath='helper', got: %v", mainDeps)
	}
}

// TestDependencies_NoResolvePaths verifies that modules in noResolvePaths
// produce a dependency with empty ResolvedPath.
func TestDependencies_NoResolvePaths(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { Timer } from "extmod";
		export function __main() { return Timer; }
	`, Opts{
		NoResolvePaths: []string{"extmod"},
		ExtraFiles: map[string]string{
			"node_modules/extmod/index.d.ts": `export declare class Timer { stop(): void; }`,
		},
	})

	var deps = results[0].Dependencies
	if len(deps) == 0 {
		t.Fatal("expected at least one dependency, got none")
	}

	found := false
	for _, d := range deps {
		if d.RequirePath == "extmod" {
			found = true
			if d.ResolvedPath != "" {
				t.Errorf("noResolvePaths module should have empty ResolvedPath, got %q", d.ResolvedPath)
			}
		}
	}
	if !found {
		t.Error("did not find dependency with RequirePath='extmod'")
	}
}

// TestDependencies_ExternalDts verifies that importing a module resolved to
// a .d.ts file in node_modules records IsExternal=true and IsLuaSource=false.
func TestDependencies_ExternalDts(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { Timer } from "extmod";
		export function __main() { return Timer; }
	`, Opts{
		ExtraFiles: map[string]string{
			"node_modules/extmod/index.d.ts": `export declare class Timer { stop(): void; }`,
		},
	})

	var deps = results[0].Dependencies
	found := false
	for _, d := range deps {
		if d.RequirePath != "" && d.ResolvedPath != "" {
			found = true
			if !d.IsExternal {
				t.Error("node_modules dependency should be marked as external")
			}
			if d.IsLuaSource {
				t.Error(".d.ts dependency should not be marked as IsLuaSource")
			}
		}
	}
	if !found {
		t.Error("did not find resolved dependency for extmod")
	}
}

// TestDependencies_MultipleImports verifies that multiple imports from the
// same file produce separate dependency entries.
func TestDependencies_MultipleImports(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { a } from "./mod_a";
		import { b } from "./mod_b";
		export function __main() { return a + b; }
	`, Opts{
		ExtraFiles: map[string]string{
			"mod_a.ts": `export const a = 1;`,
			"mod_b.ts": `export const b = 2;`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if r.FileName == "main.ts" {
			mainResult = r
			break
		}
	}

	if len(mainResult.Dependencies) < 2 {
		t.Fatalf("expected at least 2 dependencies, got %d", len(mainResult.Dependencies))
	}

	requirePaths := make(map[string]bool)
	for _, d := range mainResult.Dependencies {
		requirePaths[d.RequirePath] = true
	}
	if !requirePaths["mod_a"] {
		t.Error("missing dependency for mod_a")
	}
	if !requirePaths["mod_b"] {
		t.Error("missing dependency for mod_b")
	}
}

// TestDependencies_ReExport verifies that re-exports record dependencies.
func TestDependencies_ReExport(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		export { foo } from "./helper";
	`, Opts{
		ExtraFiles: map[string]string{
			"helper.ts": `export const foo = 42;`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if r.FileName == "main.ts" {
			mainResult = r
			break
		}
	}

	found := false
	for _, d := range mainResult.Dependencies {
		if d.RequirePath == "helper" {
			found = true
		}
	}
	if !found {
		t.Error("re-export should produce a dependency for helper")
	}
}
