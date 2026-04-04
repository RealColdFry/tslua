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

func transpileWithTarget(t *testing.T, tsCode string, target string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tsconfig := `{"compilerOptions":{"strict":true,"target":"` + target + `","lib":["esnext"]}}`
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

	results, _ := TranspileProgram(program, tmpDir, LuaTargetLua55, nil)
	for _, r := range results {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		if rel == "main.ts" {
			return r.Lua
		}
	}
	t.Fatal("no output for main.ts")
	return ""
}

const classFieldOrderTS = `export function __main() {
    class Test {
        public foo = this.bar;
        constructor(public bar: string) {}
    }
    return (new Test("baz")).foo;
}`

// ES2022+ defaults useDefineForClassFields=true: field inits before param properties
// ([[Define]] semantics). self.foo = self.bar runs first (bar not yet set), then self.bar = bar.
func TestClassFieldOrder_ES2022(t *testing.T) {
	lua := transpileWithTarget(t, classFieldOrderTS, "ES2022")
	fooIdx := strings.Index(lua, "self.foo = self.bar")
	barIdx := strings.Index(lua, "self.bar = bar")
	if fooIdx < 0 || barIdx < 0 {
		t.Fatalf("expected both assignments in output:\n%s", lua)
	}
	if fooIdx > barIdx {
		t.Errorf("ES2022: field init (self.foo) should come BEFORE param property (self.bar):\n%s", lua)
	}
}

// ES2017 defaults useDefineForClassFields=false: param properties before field inits (legacy).
// self.bar = bar runs first, then self.foo = self.bar reads the assigned value.
func TestClassFieldOrder_ES2017(t *testing.T) {
	lua := transpileWithTarget(t, classFieldOrderTS, "ES2017")
	fooIdx := strings.Index(lua, "self.foo = self.bar")
	barIdx := strings.Index(lua, "self.bar = bar")
	if fooIdx < 0 || barIdx < 0 {
		t.Fatalf("expected both assignments in output:\n%s", lua)
	}
	if barIdx > fooIdx {
		t.Errorf("ES2017: param property (self.bar) should come BEFORE field init (self.foo):\n%s", lua)
	}
}
