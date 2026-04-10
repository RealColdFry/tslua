package luatest

import (
	"strings"
	"testing"
)

// TestNoResolvePaths verifies that module specifiers listed in noResolvePaths
// are emitted as-is in require() calls, without resolving through the TS
// program's module resolution. This is needed for external Lua modules
// that have type declarations installed via npm but should be required by
// their original Lua module name at runtime.
func TestNoResolvePaths(t *testing.T) {
	t.Parallel()

	t.Run("import preserved as-is", func(t *testing.T) {
		t.Parallel()
		// "extmod" is in noResolvePaths and has a type declaration available.
		// The require path should be "extmod", not the resolved filesystem path.
		results := TranspileTS(t, `
			import { Timer } from "extmod";
			export function __main() { return Timer; }
		`, Opts{
			NoResolvePaths: []string{"extmod"},
			ExtraFiles: map[string]string{
				"node_modules/extmod/index.d.ts": `export declare class Timer { stop(): void; }`,
			},
		})
		lua := results[0].Lua
		if !strings.Contains(lua, `require("extmod")`) {
			t.Errorf("expected require(\"extmod\") but got:\n%s", lua)
		}
		if strings.Contains(lua, "node_modules") {
			t.Errorf("should not resolve to node_modules path:\n%s", lua)
		}
	})

	t.Run("re-export preserved as-is", func(t *testing.T) {
		t.Parallel()
		results := TranspileTS(t, `
			export { Timer } from "extmod";
		`, Opts{
			NoResolvePaths: []string{"extmod"},
			ExtraFiles: map[string]string{
				"node_modules/extmod/index.d.ts": `export declare class Timer { stop(): void; }`,
			},
		})
		lua := results[0].Lua
		if !strings.Contains(lua, `require("extmod")`) {
			t.Errorf("expected require(\"extmod\") but got:\n%s", lua)
		}
	})

	t.Run("non-listed module still resolved", func(t *testing.T) {
		t.Parallel()
		results := TranspileTS(t, `
			import { foo } from "./helper";
			export function __main() { return foo; }
		`, Opts{
			NoResolvePaths: []string{"extmod"},
			ExtraFiles: map[string]string{
				"helper.ts": `export const foo = 42;`,
			},
		})
		var lua string
		for _, r := range results {
			if strings.HasSuffix(r.FileName, "main.ts") {
				lua = r.Lua
				break
			}
		}
		// "./helper" should be resolved normally (not contain "./" prefix)
		if !strings.Contains(lua, `require("helper")`) {
			t.Errorf("expected require(\"helper\") but got:\n%s", lua)
		}
	})

	t.Run("prefix match for dotted paths", func(t *testing.T) {
		t.Parallel()
		// "vendor.job" is in noResolvePaths. An import of "vendor.job"
		// should be preserved even if types are installed.
		results := TranspileTS(t, `
			declare module "vendor.job" { export function run(): void; }
			import { run } from "vendor.job";
			export function __main() { return run; }
		`, Opts{
			NoResolvePaths: []string{"vendor.job"},
		})
		lua := results[0].Lua
		if !strings.Contains(lua, `require("vendor.job")`) {
			t.Errorf("expected require(\"vendor.job\") but got:\n%s", lua)
		}
	})
}
