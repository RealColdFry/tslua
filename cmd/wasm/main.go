//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"syscall/js"
	"time"

	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs"
	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// memFS implements vfs.FS backed by an in-memory file map.
type memFS struct {
	files map[string]string
	dirs  map[string]bool
}

func newMemFS(files map[string]string) *memFS {
	dirs := map[string]bool{"/": true}
	for path := range files {
		// Register all parent directories.
		d := path
		for {
			d = parentDir(d)
			if d == "" || dirs[d] {
				break
			}
			dirs[d] = true
		}
	}
	return &memFS{files: files, dirs: dirs}
}

func parentDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		if i == 0 {
			return "/"
		}
		return ""
	}
	return p[:i]
}

func (m *memFS) UseCaseSensitiveFileNames() bool { return true }

func (m *memFS) FileExists(path string) bool {
	_, ok := m.files[path]
	return ok
}

func (m *memFS) ReadFile(path string) (string, bool) {
	c, ok := m.files[path]
	return c, ok
}

func (m *memFS) WriteFile(string, string) error             { return fmt.Errorf("read-only") }
func (m *memFS) Remove(string) error                        { return fmt.Errorf("read-only") }
func (m *memFS) Chtimes(string, time.Time, time.Time) error { return fmt.Errorf("read-only") }

func (m *memFS) DirectoryExists(path string) bool {
	return m.dirs[path]
}

func (m *memFS) GetAccessibleEntries(path string) vfs.Entries {
	prefix := path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var files, directories []string
	seen := map[string]bool{}
	for p := range m.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := p[len(prefix):]
		if i := strings.Index(rest, "/"); i >= 0 {
			dir := rest[:i]
			if !seen[dir] {
				seen[dir] = true
				directories = append(directories, dir)
			}
		} else {
			files = append(files, rest)
		}
	}
	return vfs.Entries{Files: files, Directories: directories}
}

func (m *memFS) Stat(path string) fs.FileInfo { return nil }

func (m *memFS) WalkDir(root string, walkFn fs.WalkDirFunc) error {
	return fmt.Errorf("WalkDir not implemented")
}

func (m *memFS) Realpath(path string) string { return path }

// transpileResult is the JSON structure returned to JS.
type transpileResult struct {
	Lua    string   `json:"lua"`
	Errors []string `json:"errors"`
}

func transpile(tsCode string, luaTarget string) transpileResult {
	const root = "/src"
	const configPath = root + "/tsconfig.json"
	const mainPath = root + "/main.ts"

	tsconfig := map[string]any{
		"compilerOptions": map[string]any{
			"strict":           true,
			"target":           "ESNext",
			"moduleResolution": "node",
			"skipLibCheck":     true,
		},
		"include": []string{"*.ts"},
	}
	tsconfigBytes, _ := json.Marshal(tsconfig)

	mem := newMemFS(map[string]string{
		configPath: string(tsconfigBytes),
		mainPath:   tsCode,
	})

	wrapped := bundled.WrapFS(mem)
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, wrapped, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		var errs []string
		for _, d := range diags {
			errs = append(errs, fmt.Sprintf("%v", d))
		}
		return transpileResult{Errors: errs}
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	lt := transpiler.LuaTarget(luaTarget)

	opts := transpiler.TranspileOptions{
		LuaLibImport: transpiler.LuaLibImportInline,
	}
	if fd, err := lualib.FeatureDataForTarget(string(lt)); err == nil {
		opts.LualibFeatureData = fd
	} else {
		opts.LualibInlineContent = string(lualib.BundleForTarget(string(lt)))
	}
	results, _ := transpiler.TranspileProgramWithOptions(program, root, lt, nil, opts)

	var luaOut strings.Builder
	for _, r := range results {
		luaOut.WriteString(r.Lua)
	}

	return transpileResult{Lua: luaOut.String()}
}

func main() {
	js.Global().Set("tslua_transpile", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return map[string]any{"lua": "", "errors": []any{"missing tsCode argument"}}
		}
		tsCode := args[0].String()
		luaTarget := "JIT"
		if len(args) >= 2 {
			luaTarget = args[1].String()
		}

		result := transpile(tsCode, luaTarget)

		errArr := js.Global().Get("Array").New()
		for _, e := range result.Errors {
			errArr.Call("push", e)
		}

		obj := js.Global().Get("Object").New()
		obj.Set("lua", result.Lua)
		obj.Set("errors", errArr)
		return obj
	}))

	// Keep the Go program alive.
	select {}
}
