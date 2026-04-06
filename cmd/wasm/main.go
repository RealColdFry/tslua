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
	"github.com/microsoft/typescript-go/shim/scanner"
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

// wasmDiagnostic is a structured diagnostic for Monaco markers.
type wasmDiagnostic struct {
	StartLine int    `json:"startLine"` // 1-based
	StartCol  int    `json:"startCol"`  // 1-based, UTF-16
	EndLine   int    `json:"endLine"`
	EndCol    int    `json:"endCol"`
	Message   string `json:"message"`
	Severity  int    `json:"severity"` // Monaco: 1=hint, 2=info, 4=warning, 8=error
	Code      int32  `json:"code"`     // diagnostic code (e.g. 100037)
}

// transpileResult is the JSON structure returned to JS.
type transpileResult struct {
	Lua         string           `json:"lua"`
	Errors      []string         `json:"errors"`
	Diagnostics []wasmDiagnostic `json:"diagnostics"`
}

// wasmOptions holds the JSON structure passed from the JS side.
type wasmOptions struct {
	CompilerOptions map[string]any `json:"compilerOptions,omitempty"`
	TSTL            struct {
		LuaTarget                 string `json:"luaTarget,omitempty"`
		EmitMode                  string `json:"emitMode,omitempty"`
		ClassStyle                string `json:"classStyle,omitempty"`
		NoImplicitSelf            bool   `json:"noImplicitSelf,omitempty"`
		NoImplicitGlobalVariables bool   `json:"noImplicitGlobalVariables,omitempty"`
		Trace                     bool   `json:"trace,omitempty"`
	} `json:"tstl,omitempty"`
}

func transpile(tsCode string, wopts wasmOptions) transpileResult {
	const root = "/src"
	const configPath = root + "/tsconfig.json"
	const mainPath = root + "/main.ts"

	compilerOpts := map[string]any{
		"strict":           true,
		"target":           "ESNext",
		"moduleResolution": "node",
		"skipLibCheck":     true,
	}
	// Apply user compiler options (override defaults)
	for k, v := range wopts.CompilerOptions {
		compilerOpts[k] = v
	}

	tsconfig := map[string]any{
		"compilerOptions": compilerOpts,
		"include":         []string{"*.ts"},
	}
	tsconfigBytes, _ := json.Marshal(tsconfig)

	mem := newMemFS(map[string]string{
		configPath: string(tsconfigBytes),
		mainPath:   tsCode,
	})

	wrapped := bundled.WrapFS(mem)
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, wrapped, bundled.LibPath(), nil, nil)

	configResult, cfgDiags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(cfgDiags) > 0 {
		var errs []string
		for _, d := range cfgDiags {
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

	luaTarget := wopts.TSTL.LuaTarget
	if luaTarget == "" {
		luaTarget = "JIT"
	}
	lt := transpiler.LuaTarget(luaTarget)

	opts := transpiler.TranspileOptions{
		LuaLibImport:              transpiler.LuaLibImportInline,
		EmitMode:                  transpiler.EmitMode(wopts.TSTL.EmitMode),
		ClassStyle:                transpiler.ClassStyle(wopts.TSTL.ClassStyle),
		NoImplicitSelf:            wopts.TSTL.NoImplicitSelf,
		NoImplicitGlobalVariables: wopts.TSTL.NoImplicitGlobalVariables,
		Trace:                     wopts.TSTL.Trace,
	}
	if fd, err := lualib.FeatureDataForTarget(string(lt)); err == nil {
		opts.LualibFeatureData = fd
	} else {
		opts.LualibInlineContent = string(lualib.BundleForTarget(string(lt)))
	}
	results, tsDiags := transpiler.TranspileProgramWithOptions(program, root, lt, nil, opts)

	var luaOut strings.Builder
	for _, r := range results {
		luaOut.WriteString(r.Lua)
	}

	var diags []wasmDiagnostic
	for _, d := range tsDiags {
		if d.File() == nil {
			continue
		}
		// diagnostics.Category: 0=Warning, 1=Error, 2=Suggestion, 3=Message
		// Monaco MarkerSeverity: 1=Hint, 2=Info, 4=Warning, 8=Error
		var severity int
		switch int(d.Category()) {
		case 1: // Error
			severity = 8
		case 0: // Warning
			severity = 4
		case 2: // Suggestion
			severity = 1
		default: // Message
			severity = 2
		}
		startLine, startChar := scanner.GetECMALineAndUTF16CharacterOfPosition(d.File(), d.Pos())
		endLine, endChar := scanner.GetECMALineAndUTF16CharacterOfPosition(d.File(), d.End())
		diags = append(diags, wasmDiagnostic{
			StartLine: startLine + 1, // ECMA is 0-based, Monaco is 1-based
			StartCol:  int(startChar) + 1,
			EndLine:   endLine + 1,
			EndCol:    int(endChar) + 1,
			Message:   d.String(),
			Severity:  severity,
			Code:      d.Code(),
		})
	}

	return transpileResult{Lua: luaOut.String(), Diagnostics: diags}
}

func main() {
	js.Global().Set("tslua_transpile", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return map[string]any{"lua": "", "errors": []any{"missing tsCode argument"}}
		}
		tsCode := args[0].String()
		var wopts wasmOptions
		if len(args) >= 2 && args[1].Type() == js.TypeString {
			_ = json.Unmarshal([]byte(args[1].String()), &wopts)
		}

		result := transpile(tsCode, wopts)

		errArr := js.Global().Get("Array").New()
		for _, e := range result.Errors {
			errArr.Call("push", e)
		}

		diagArr := js.Global().Get("Array").New()
		for _, d := range result.Diagnostics {
			dobj := js.Global().Get("Object").New()
			dobj.Set("startLine", d.StartLine)
			dobj.Set("startCol", d.StartCol)
			dobj.Set("endLine", d.EndLine)
			dobj.Set("endCol", d.EndCol)
			dobj.Set("message", d.Message)
			dobj.Set("severity", d.Severity)
			dobj.Set("code", d.Code)
			diagArr.Call("push", dobj)
		}

		obj := js.Global().Get("Object").New()
		obj.Set("lua", result.Lua)
		obj.Set("errors", errArr)
		obj.Set("diagnostics", diagArr)
		return obj
	}))

	// Keep the Go program alive.
	select {}
}
