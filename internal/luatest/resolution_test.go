package luatest

import (
	"strings"
	"testing"
)

// TestResolution_RootDir verifies that rootDir affects require path computation.
// Files under rootDir/src/ should produce require paths relative to rootDir.
func TestResolution_RootDir(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { helper } from "./helper";
		export function __main() { return helper(); }
	`, Opts{
		CompilerOptions: map[string]any{
			"rootDir": "src",
		},
		MainFileName: "src/main.ts",
		ExtraFiles: map[string]string{
			"src/helper.ts": `export function helper() { return 42; }`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainResult = r
			break
		}
	}

	// With rootDir=src, the require should be relative to src/
	if !strings.Contains(mainResult.Lua, `require("helper")`) {
		t.Errorf("expected require(\"helper\") with rootDir=src, got:\n%s", mainResult.Lua)
	}
}

// TestResolution_RootDirNested verifies require paths for nested files under rootDir.
func TestResolution_RootDirNested(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { util } from "./lib/util";
		export function __main() { return util(); }
	`, Opts{
		CompilerOptions: map[string]any{
			"rootDir": "src",
		},
		MainFileName: "src/main.ts",
		ExtraFiles: map[string]string{
			"src/lib/util.ts": `export function util() { return 1; }`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainResult = r
			break
		}
	}

	// Nested import should produce dot-separated path
	if !strings.Contains(mainResult.Lua, `require("lib.util")`) {
		t.Errorf("expected require(\"lib.util\"), got:\n%s", mainResult.Lua)
	}
}

// TestResolution_Paths verifies that tsconfig paths mappings are resolved.
func TestResolution_Paths(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { foo } from "@app/utils";
		export function __main() { return foo; }
	`, Opts{
		CompilerOptions: map[string]any{
			"rootDir": "src",
			"paths": map[string]any{
				"@app/*": []string{"src/*"},
			},
		},
		MainFileName: "src/main.ts",
		ExtraFiles: map[string]string{
			"src/utils.ts": `export const foo = 42;`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainResult = r
			break
		}
	}

	// paths mapping @app/* -> src/* should resolve to the actual file,
	// and the require path should be relative to rootDir
	if !strings.Contains(mainResult.Lua, `require("utils")`) {
		t.Errorf("expected require(\"utils\") via paths mapping, got:\n%s", mainResult.Lua)
	}
}

// TestResolution_PathsNested verifies paths mappings with nested targets.
func TestResolution_PathsNested(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { bar } from "@lib/bar";
		export function __main() { return bar; }
	`, Opts{
		CompilerOptions: map[string]any{
			"rootDir": "src",
			"paths": map[string]any{
				"@lib/*": []string{"src/lib/*"},
			},
		},
		MainFileName: "src/main.ts",
		ExtraFiles: map[string]string{
			"src/lib/bar.ts": `export const bar = "hello";`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainResult = r
			break
		}
	}

	if !strings.Contains(mainResult.Lua, `require("lib.bar")`) {
		t.Errorf("expected require(\"lib.bar\") via paths mapping, got:\n%s", mainResult.Lua)
	}
}

// TestResolution_CrossFileRequire verifies that two files importing each other
// produce correct require paths regardless of directory depth.
func TestResolution_CrossFileRequire(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { b } from "./sub/b";
		export const a = 1;
		export function __main() { return b; }
	`, Opts{
		ExtraFiles: map[string]string{
			"sub/b.ts": `import { a } from "../main"; export const b = a + 1;`,
		},
	})

	var subResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "b.ts") {
			subResult = r
			break
		}
	}

	// sub/b.ts importing ../main should resolve to require("main")
	if !strings.Contains(subResult.Lua, `require("main")`) {
		t.Errorf("expected require(\"main\") from sub/b.ts, got:\n%s", subResult.Lua)
	}
}

// TestResolution_DependencyMetadata_RootDir verifies that dependency metadata
// records the correct resolved paths when rootDir is set.
func TestResolution_DependencyMetadata_RootDir(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { x } from "./helper";
		export function __main() { return x; }
	`, Opts{
		CompilerOptions: map[string]any{
			"rootDir": "src",
		},
		MainFileName: "src/main.ts",
		ExtraFiles: map[string]string{
			"src/helper.ts": `export const x = 1;`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainResult = r
			break
		}
	}

	if len(mainResult.Dependencies) == 0 {
		t.Fatal("expected at least one dependency")
	}

	found := false
	for _, d := range mainResult.Dependencies {
		if d.RequirePath == "helper" {
			found = true
			if d.IsExternal {
				t.Error("project file should not be external")
			}
			if d.ResolvedPath == "" {
				t.Error("should have a resolved path")
			}
		}
	}
	if !found {
		t.Errorf("did not find dependency with RequirePath='helper', got: %v", mainResult.Dependencies)
	}
}

// TestResolution_DependencyMetadata_Paths verifies that paths-resolved imports
// produce correct dependency metadata.
func TestResolution_DependencyMetadata_Paths(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `
		import { val } from "@pkg/mod";
		export function __main() { return val; }
	`, Opts{
		CompilerOptions: map[string]any{
			"rootDir": "src",
			"paths": map[string]any{
				"@pkg/*": []string{"src/packages/*"},
			},
		},
		MainFileName: "src/main.ts",
		ExtraFiles: map[string]string{
			"src/packages/mod.ts": `export const val = 99;`,
		},
	})

	var mainResult TranspileResult
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainResult = r
			break
		}
	}

	found := false
	for _, d := range mainResult.Dependencies {
		if d.RequirePath == "packages.mod" {
			found = true
			if d.IsExternal {
				t.Error("paths-resolved project file should not be external")
			}
		}
	}
	if !found {
		t.Errorf("did not find dependency with RequirePath='packages.mod', got deps: %v", mainResult.Dependencies)
	}
}
