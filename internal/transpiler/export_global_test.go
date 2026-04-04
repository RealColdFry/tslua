package transpiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
)

func transpileWithExportAsGlobal(t *testing.T, tsCode string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tsconfig := `{"compilerOptions":{"strict":true,"target":"ESNext","lib":["esnext"]}}`
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.ts"), []byte(tsCode), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)
	configResult, _ := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	results, _ := TranspileProgramWithOptions(program, tmpDir, LuaTargetLua55, nil, TranspileOptions{
		ExportAsGlobal: true,
	})
	for _, r := range results {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		if rel == "main.ts" {
			return r.Lua
		}
	}
	t.Fatal("no output for main.ts")
	return ""
}

func TestExportAsGlobal_Variable(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `export const foo = 10;`)
	if strings.Contains(lua, "____exports") {
		t.Errorf("should not contain ____exports:\n%s", lua)
	}
	if !strings.Contains(lua, "foo = 10") {
		t.Errorf("should contain 'foo = 10':\n%s", lua)
	}
}

func TestExportAsGlobal_Function(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `export function init() { return 1; }`)
	if strings.Contains(lua, "____exports") {
		t.Errorf("should not contain ____exports:\n%s", lua)
	}
	if !strings.Contains(lua, "function init(") {
		t.Errorf("should contain 'function init(':\n%s", lua)
	}
}

func TestExportAsGlobal_Enum(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `export enum Color { Red, Green, Blue }`)
	if strings.Contains(lua, "____exports") {
		t.Errorf("should not contain ____exports:\n%s", lua)
	}
	if !strings.Contains(lua, "Color = Color or") {
		t.Errorf("should contain 'Color = Color or':\n%s", lua)
	}
}

func TestExportAsGlobal_Class(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `export class Foo { x = 1; }`)
	if strings.Contains(lua, "____exports") {
		t.Errorf("should not contain ____exports:\n%s", lua)
	}
	if !strings.Contains(lua, "Foo = __TS__Class()") {
		t.Errorf("should contain 'Foo = __TS__Class()':\n%s", lua)
	}
}

func TestExportAsGlobal_NoReturnExports(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `export const x = 1;`)
	if strings.Contains(lua, "return ____exports") {
		t.Errorf("should not contain 'return ____exports':\n%s", lua)
	}
}

func TestExportAsGlobal_MixedExports(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `
export const foo = 10;
const bar = 20;
export function update() { return bar; }
`)
	if strings.Contains(lua, "____exports") {
		t.Errorf("should not contain ____exports:\n%s", lua)
	}
	// bar should still be local
	if !strings.Contains(lua, "local bar = 20") {
		t.Errorf("non-exported 'bar' should be local:\n%s", lua)
	}
}

func TestExportAsGlobal_ReferenceExportedName(t *testing.T) {
	lua := transpileWithExportAsGlobal(t, `
export const foo = 10;
export const bar = foo + 1;
`)
	if strings.Contains(lua, "____exports") {
		t.Errorf("should not contain ____exports:\n%s", lua)
	}
}

// transpileMultiFileWithMatch creates a two-file project where ExportAsGlobalMatch
// selectively applies export-as-global to files matching the regex.
func transpileMultiFileWithMatch(t *testing.T, matchPattern string, files map[string]string) map[string]string {
	t.Helper()
	tmpDir := t.TempDir()
	tsconfig := `{"compilerOptions":{"strict":true,"target":"ESNext","lib":["esnext"]}}`
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, code := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)
	configResult, _ := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	results, _ := TranspileProgramWithOptions(program, tmpDir, LuaTargetLua55, nil, TranspileOptions{
		ExportAsGlobalMatch: matchPattern,
	})
	out := map[string]string{}
	for _, r := range results {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		out[rel] = r.Lua
	}
	return out
}

func TestExportAsGlobalMatch_SelectiveMatch(t *testing.T) {
	results := transpileMultiFileWithMatch(t, `\.script\.ts$`, map[string]string{
		"game.script.ts": `export function init() { return 1; }`,
		"util.ts":        `export function helper() { return 2; }`,
	})

	// game.script.ts should have export-as-global applied
	gameLua := results["game.script.ts"]
	if strings.Contains(gameLua, "____exports") {
		t.Errorf("game.script.ts should not contain ____exports:\n%s", gameLua)
	}
	if !strings.Contains(gameLua, "function init(") {
		t.Errorf("game.script.ts should contain global 'function init(':\n%s", gameLua)
	}

	// util.ts should keep normal module exports
	utilLua := results["util.ts"]
	if !strings.Contains(utilLua, "____exports") {
		t.Errorf("util.ts should contain ____exports (not matched):\n%s", utilLua)
	}
}

func TestExportAsGlobalMatch_NoMatch(t *testing.T) {
	results := transpileMultiFileWithMatch(t, `\.script\.ts$`, map[string]string{
		"main.ts": `export const x = 1;`,
	})
	mainLua := results["main.ts"]
	if !strings.Contains(mainLua, "____exports") {
		t.Errorf("main.ts should contain ____exports (pattern doesn't match):\n%s", mainLua)
	}
}
