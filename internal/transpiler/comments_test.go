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

func transpileWithConfig(t *testing.T, tsCode, tsconfig string) string {
	t.Helper()
	tmpDir := t.TempDir()
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

	results, _ := TranspileProgram(program, tmpDir, LuaTargetLuaJIT, nil)
	for _, r := range results {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		if rel == "main.ts" {
			return r.Lua
		}
	}
	t.Fatal("no output for main.ts")
	return ""
}

const defaultConfig = `{"compilerOptions":{"strict":true,"target":"ESNext","lib":["esnext"]}}`
const removeCommentsConfig = `{"compilerOptions":{"strict":true,"target":"ESNext","lib":["esnext"],"removeComments":true}}`

func TestCommentPreservation(t *testing.T) {
	code := `// single line comment
const x = 1;
/* block comment */
const y = 2;
/** JSDoc comment */
function foo() { return 3; }
`

	t.Run("default preserves all comments", func(t *testing.T) {
		lua := transpileWithConfig(t, code, defaultConfig)
		if !strings.Contains(lua, "-- single line comment") {
			t.Errorf("expected single-line comment preserved\ngot:\n%s", lua)
		}
		if !strings.Contains(lua, "-- block comment") {
			t.Errorf("expected block comment preserved\ngot:\n%s", lua)
		}
		if !strings.Contains(lua, "--- JSDoc comment") {
			t.Errorf("expected JSDoc comment preserved\ngot:\n%s", lua)
		}
	})

	t.Run("removeComments strips all comments", func(t *testing.T) {
		lua := transpileWithConfig(t, code, removeCommentsConfig)
		if strings.Contains(lua, "single line comment") {
			t.Errorf("expected single-line comment stripped\ngot:\n%s", lua)
		}
		if strings.Contains(lua, "block comment") {
			t.Errorf("expected block comment stripped\ngot:\n%s", lua)
		}
		if strings.Contains(lua, "JSDoc comment") {
			t.Errorf("expected JSDoc comment stripped\ngot:\n%s", lua)
		}
	})

	t.Run("annotations work with removeComments", func(t *testing.T) {
		lua := transpileWithConfig(t, `/** @noSelf */ function foo(x: number) { return x; }`, removeCommentsConfig)
		if !strings.Contains(lua, "function foo(x)") {
			t.Errorf("expected @noSelf to still work with removeComments\ngot:\n%s", lua)
		}
	})
}
