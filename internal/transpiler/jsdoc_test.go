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

func transpileToLua(t *testing.T, tsCode string) string {
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

func TestNoSelfAnnotation(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
		deny string
	}{
		{
			"@noSelf on function declaration",
			"/** @noSelf */ function foo(x: number) { return x; }\nfoo(42);",
			"function foo(x)",
			"function foo(self, x)",
		},
		{
			"no annotation — needs self",
			"function foo(x: number) { return x; }",
			"function foo(self, x)",
			"",
		},
		{
			"@noSelfInFile at file level",
			"/** @noSelfInFile */\nfunction foo(x: number) { return x; }\nfoo(42);",
			"function foo(x)",
			"function foo(self, x)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lua := transpileToLua(t, tt.code)
			if !strings.Contains(lua, tt.want) {
				t.Errorf("expected output to contain %q\ngot:\n%s", tt.want, lua)
			}
			if tt.deny != "" && strings.Contains(lua, tt.deny) {
				t.Errorf("expected output NOT to contain %q\ngot:\n%s", tt.deny, lua)
			}
		})
	}
}
