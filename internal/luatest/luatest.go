// Package luatest provides shared test infrastructure for transpiling TypeScript
// to Lua and evaluating the result. Used by tstltest (migrated TSTL tests) and
// hand-written test suites.
package luatest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// ============================================================================
// Lua evaluator — long-running Lua process to avoid per-test process spawns
// ============================================================================

// Evaluator wraps a long-running Lua process for fast code evaluation.
type Evaluator struct {
	cmd    *exec.Cmd
	stdin  io.Writer
	stdout *bufio.Reader
	mu     sync.Mutex
}

// Evaluators holds per-runtime eval servers, keyed by runtime name (e.g. "luajit", "lua5.5").
var Evaluators map[string]*Evaluator

// Setup starts Lua eval servers for all available runtimes. Call from TestMain.
func Setup() {
	luaServerScript := FindRepoFile("scripts/lua_eval_server.lua")
	luaServerScript50 := FindRepoFile("scripts/lua_eval_server_50.lua")
	luaServerScriptLuau := FindRepoFile("scripts/lua_eval_server_luau.luau")
	if luaServerScript == "" {
		return
	}
	Evaluators = make(map[string]*Evaluator)
	for _, runtime := range []string{"luajit", "lua5.0", "lua5.1", "lua5.2", "lua5.3", "lua5.4", "lua5.5"} {
		if _, err := exec.LookPath(runtime); err == nil {
			script := luaServerScript
			if runtime == "lua5.0" && luaServerScript50 != "" {
				script = luaServerScript50
			}
			if e, err := startEvaluator(runtime, script); err == nil {
				Evaluators[runtime] = e
			}
		}
	}
	// Lune (Luau runtime) uses "lune run <script>" instead of "<runtime> <script>"
	if luaServerScriptLuau != "" {
		if _, err := exec.LookPath("lune"); err == nil {
			if e, err := startEvaluator("lune", "run", luaServerScriptLuau); err == nil {
				Evaluators["lune"] = e
			}
		}
	}
}

// Teardown shuts down all eval servers. Call from TestMain after m.Run().
func Teardown() {
	for _, e := range Evaluators {
		e.close()
	}
}

func startEvaluator(runtime string, args ...string) (*Evaluator, error) {
	cmd := exec.Command(runtime, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != "READY" {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("lua eval server did not send ready signal")
	}

	return &Evaluator{cmd: cmd, stdin: stdin, stdout: reader}, nil
}

func (e *Evaluator) Eval(code string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	header := fmt.Sprintf("%d\n", len(code))
	if _, err := io.WriteString(e.stdin, header+code+"\n"); err != nil {
		return "", fmt.Errorf("write to lua eval server: %w", err)
	}

	respLine, err := e.stdout.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("lua eval server closed unexpectedly: %w", err)
	}
	respLine = strings.TrimSuffix(respLine, "\n")

	var prefix string
	var size int
	if _, err := fmt.Sscanf(respLine, "%s %d", &prefix, &size); err != nil {
		return "", fmt.Errorf("parse lua eval response %q: %w", respLine, err)
	}

	body := make([]byte, size)
	if _, err := io.ReadFull(e.stdout, body); err != nil {
		return "", fmt.Errorf("read lua eval response body: %w", err)
	}
	if _, err := e.stdout.ReadByte(); err != nil {
		return "", fmt.Errorf("read lua eval trailing newline: %w", err)
	}

	if prefix == "ERR" {
		return "", fmt.Errorf("lua error: %s", body)
	}
	return string(body), nil
}

func (e *Evaluator) close() {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.stdin.(io.Closer).Close()
		_ = e.cmd.Wait()
	}
}

// FindRepoFile searches upward from cwd for a file relative to the repo root.
func FindRepoFile(rel string) string {
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ============================================================================
// Test options
// ============================================================================

// Opts configures transpilation and evaluation for a test.
type Opts struct {
	LuaTarget                 transpiler.LuaTarget
	EmitMode                  transpiler.EmitMode
	LuaLibImport              transpiler.LuaLibImportKind // how lualib features are included (default: require)
	ExportAsGlobal            bool
	NoImplicitGlobalVariables bool
	ClassStyle                transpiler.ClassStyle // alternative class emit style
	SourceMap                 bool                  // enable source map generation
	SourceMapTraceback        bool                  // register source maps at runtime for debug.traceback
	InlineSourceMap           bool                  // embed source map as base64 in Lua output
	LuaPreamble               string                // Lua code prepended before main module (e.g. mock runtime)
	MainFileName              string                // override main file name (default "main.ts", use "main.tsx" for JSX)
	CompilerOptions           map[string]any        // extra tsconfig compilerOptions
	ExtraFiles                map[string]string     // additional TS files (e.g. "helper.ts" → code)
	NoResolvePaths            []string              // module specifiers to emit as-is (TSTL noResolvePaths)
}

// ============================================================================
// Transpile + eval
// ============================================================================

// TranspileResult holds per-file Lua output.
type TranspileResult struct {
	FileName     string
	Lua          string
	SourceMap    string // V3 source map JSON (empty if source maps disabled)
	UsesLualib   bool
	LualibDeps   []string                      // lualib exports referenced (populated for None and RequireMinimal modes)
	Dependencies []transpiler.ModuleDependency // module dependencies discovered during transformation
}

// TranspileTS compiles TypeScript source to Lua using the full compiler pipeline.
func TranspileTS(t *testing.T, tsCode string, opts Opts) []TranspileResult {
	t.Helper()

	tmpDir := t.TempDir()

	tsconfig := buildTsconfig(opts)
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	mainFile := opts.MainFileName
	if mainFile == "" {
		mainFile = "main.ts"
	}
	mainPath := filepath.Join(tmpDir, mainFile)
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(tsCode), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, code := range opts.ExtraFiles {
		p := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		t.Fatalf("tsconfig parse errors: %d diagnostic(s)", len(diags))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	luaTarget := opts.LuaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}

	transpileOpts := transpiler.TranspileOptions{
		EmitMode:                  opts.EmitMode,
		LuaLibImport:              opts.LuaLibImport,
		ExportAsGlobal:            opts.ExportAsGlobal,
		NoImplicitGlobalVariables: opts.NoImplicitGlobalVariables,
		ClassStyle:                opts.ClassStyle,
		SourceMap:                 opts.SourceMap || opts.SourceMapTraceback || opts.InlineSourceMap,
		SourceMapTraceback:        opts.SourceMapTraceback,
		InlineSourceMap:           opts.InlineSourceMap,
		NoResolvePaths:            opts.NoResolvePaths,
	}
	if transpileOpts.LuaLibImport == transpiler.LuaLibImportInline {
		if fd, err := lualib.FeatureDataForTarget(string(luaTarget)); err == nil {
			transpileOpts.LualibFeatureData = fd
		}
	}
	sourceRoot := tmpDir
	if configResult.CompilerOptions().RootDir != "" {
		sourceRoot = tspath.ResolvePath(tmpDir, configResult.CompilerOptions().RootDir)
	}
	rawResults, tsDiags := transpiler.TranspileProgramWithOptions(program, sourceRoot, luaTarget, nil, transpileOpts)
	_ = tsDiags

	var results []TranspileResult
	for _, r := range rawResults {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		results = append(results, TranspileResult{
			FileName:     rel,
			Lua:          r.Lua,
			SourceMap:    r.SourceMap,
			UsesLualib:   r.UsesLualib,
			LualibDeps:   r.LualibDeps,
			Dependencies: r.Dependencies,
		})
	}
	return results
}

// TranspileProgramDiags is like TranspileTS but also returns diagnostics.
func TranspileProgramDiags(t *testing.T, tsCode string, opts Opts) ([]TranspileResult, []*ast.Diagnostic) {
	t.Helper()

	tmpDir := t.TempDir()

	tsconfig := buildTsconfig(opts)
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	mainFile := opts.MainFileName
	if mainFile == "" {
		mainFile = "main.ts"
	}
	if err := os.WriteFile(filepath.Join(tmpDir, mainFile), []byte(tsCode), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		t.Fatalf("tsconfig parse errors: %d diagnostic(s)", len(diags))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	luaTarget := opts.LuaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}

	var emitArgs []transpiler.EmitMode
	if opts.EmitMode != "" {
		emitArgs = []transpiler.EmitMode{opts.EmitMode}
	}
	rawResults, tsDiags := transpiler.TranspileProgram(program, tmpDir, luaTarget, nil, emitArgs...)

	var results []TranspileResult
	for _, r := range rawResults {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		results = append(results, TranspileResult{
			FileName:   rel,
			Lua:        r.Lua,
			UsesLualib: r.UsesLualib,
		})
	}
	return results, tsDiags
}

// RunLua writes transpiled Lua files to a temp dir and evaluates with the appropriate runtime.
func RunLua(t *testing.T, results []TranspileResult, accessor string, opts Opts) string {
	t.Helper()

	tmpDir := t.TempDir()
	usesLualib := false
	mainLua := ""
	for _, r := range results {
		luaName := strings.TrimSuffix(strings.TrimSuffix(r.FileName, ".tsx"), ".ts") + ".lua"
		outPath := filepath.Join(tmpDir, luaName)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outPath, []byte(r.Lua), 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(r.FileName, "main.ts") || strings.HasSuffix(r.FileName, "main.tsx") {
			mainLua = r.Lua
		}
		if r.UsesLualib {
			usesLualib = true
		}
	}
	luaTarget := opts.LuaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}
	if usesLualib {
		if err := os.WriteFile(filepath.Join(tmpDir, "lualib_bundle.lua"), lualib.BundleForTarget(string(luaTarget)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Prepend preamble (e.g. mock runtime) to the main Lua file.
	if opts.LuaPreamble != "" && mainLua != "" {
		mainLuaName := strings.TrimSuffix(strings.TrimSuffix(results[0].FileName, ".tsx"), ".ts") + ".lua"
		for _, r := range results {
			if strings.HasSuffix(r.FileName, "main.ts") || strings.HasSuffix(r.FileName, "main.tsx") {
				mainLuaName = strings.TrimSuffix(strings.TrimSuffix(r.FileName, ".tsx"), ".ts") + ".lua"
				break
			}
		}
		combined := opts.LuaPreamble + "\n" + mainLua
		if err := os.WriteFile(filepath.Join(tmpDir, mainLuaName), []byte(combined), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	luaRuntime := luaTarget.Runtime()
	runner := BuildRunner(luaTarget, tmpDir, "main", accessor)

	if e, ok := Evaluators[luaRuntime]; ok {
		result, err := e.Eval(runner)
		if err != nil {
			t.Fatalf("%s error: %v\nlua code:\n%s", luaRuntime, err, FormatLuaCode(mainLua, err.Error()))
		}
		return result
	}

	var cmd *exec.Cmd
	if luaRuntime == "lune" {
		// Lune doesn't support -e; write to a temp file
		tmpFile := filepath.Join(tmpDir, "__runner.luau")
		if err := os.WriteFile(tmpFile, []byte(runner), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command("lune", "run", tmpFile)
	} else {
		cmd = exec.Command(luaRuntime, "-e", runner)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error: %v\noutput: %s\nlua code:\n%s", luaRuntime, err, out, FormatLuaCode(mainLua, err.Error()))
	}
	return string(out)
}

// ExpectFunction transpiles "export function __main() { body }" and checks the eval result.
func ExpectFunction(t *testing.T, body string, want string, opts Opts) {
	t.Helper()
	tsCode := "export function __main() {" + body + "}"
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// ExpectFunctionLua is like ExpectFunction but also returns the generated Lua for inspection.
func ExpectFunctionLua(t *testing.T, body string, want string, opts Opts) string {
	t.Helper()
	tsCode := "export function __main() {" + body + "}"
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			return r.Lua
		}
	}
	return ""
}

// ============================================================================
// Helpers
// ============================================================================

func buildTsconfig(opts Opts) string {
	compilerOptions := map[string]any{
		"target":           "ES2017",
		"lib":              []string{"ESNext"},
		"moduleResolution": "bundler",
		"strict":           true,
		"skipLibCheck":     true,
	}
	for k, v := range opts.CompilerOptions {
		compilerOptions[k] = v
	}
	include := []string{"**/*.ts"}
	if _, hasJsx := compilerOptions["jsx"]; hasJsx {
		include = append(include, "**/*.tsx")
	}
	tsconfig := map[string]any{
		"compilerOptions": compilerOptions,
		"include":         include,
	}
	b, _ := json.MarshalIndent(tsconfig, "", "\t")
	return string(b)
}

// TestdataFile reads a file from the luatest/testdata directory.
func TestdataFile(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(thisFile), "testdata", name)
	data, err := os.ReadFile(p)
	if err != nil {
		panic(fmt.Sprintf("luatest.TestdataFile(%q): %v", name, err))
	}
	return string(data)
}

// LanguageExtensionsPath returns the absolute path to the TSTL language-extensions
// type declarations directory (extern/tstl/language-extensions).
func LanguageExtensionsPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
}

// OptsWithLanguageExtensions returns Opts with TSTL language extension types enabled.
func OptsWithLanguageExtensions() Opts {
	return Opts{
		CompilerOptions: map[string]any{
			"types": []string{LanguageExtensionsPath()},
		},
	}
}

// SerializeFn is the Lua 5.1+ serialize function for deterministic value output.
const SerializeFn = `
local function serialize(v)
	local t = type(v)
	if v == nil then return "nil"
	elseif t == "boolean" then return tostring(v)
	elseif t == "number" then
		if v ~= v then return "NaN"
		elseif v == math.huge then return "Infinity"
		elseif v == -math.huge then return "-Infinity"
		elseif v == math.floor(v) and v >= -2^53 and v <= 2^53 then
			return string.format("%.0f", v)
		else
			for p = 14, 17 do
				local s = string.format("%." .. p .. "g", v)
				if tonumber(s) == v then return s end
			end
			return string.format("%.17g", v)
		end
	elseif t == "string" then
		local s = string.format("%q", v)
		s = s:gsub("\\\n", "\\n")
		s = s:gsub("\t", "\\t")
		s = s:gsub("\\(%d)([^%d])", "\\00%1%2")
		s = s:gsub("\\(%d%d)([^%d])", "\\0%1%2")
		s = s:gsub("\\009", "\\t")
		s = s:gsub("\\012", "\\f")
		s = s:gsub("\\013", "\\r")
		return s
	elseif t == "table" then
		local n = #v
		local isArray = true
		if n == 0 then
			isArray = false
		else
			for i = 1, n do
				if v[i] == nil then isArray = false; break end
			end
		end
		if isArray then
			local parts = {}
			for i = 1, n do parts[i] = serialize(v[i]) end
			return "{" .. table.concat(parts, ", ") .. "}"
		else
			local parts = {}
			for k, val in pairs(v) do
				parts[#parts+1] = tostring(k) .. " = " .. serialize(val)
			end
			table.sort(parts)
			return "{" .. table.concat(parts, ", ") .. "}"
		end
	else return tostring(v) end
end
`

// SerializeFn50 is the Lua 5.0 version (no # operator, no method syntax).
const SerializeFn50 = `
local function serialize(v)
	local t = type(v)
	if v == nil then return "nil"
	elseif t == "boolean" then return tostring(v)
	elseif t == "number" then
		if v ~= v then return "NaN"
		elseif v == 1/0 then return "Infinity"
		elseif v == -1/0 then return "-Infinity"
		elseif v == math.floor(v) and v >= -2^53 and v <= 2^53 then
			return string.format("%.0f", v)
		else
			for p = 14, 17 do
				local s = string.format("%." .. p .. "g", v)
				if tonumber(s) == v then return s end
			end
			return string.format("%.17g", v)
		end
	elseif t == "string" then
		local s = string.format("%q", v)
		s = string.gsub(s, "\\\n", "\\n")
		s = string.gsub(s, "\t", "\\t")
		s = string.gsub(s, "\\(%d)([^%d])", "\\00%1%2")
		s = string.gsub(s, "\\(%d%d)([^%d])", "\\0%1%2")
		s = string.gsub(s, "\\009", "\\t")
		s = string.gsub(s, "\\012", "\\f")
		s = string.gsub(s, "\\013", "\\r")
		return s
	elseif t == "table" then
		local n = table.getn(v)
		local isArray = true
		if n == 0 then
			isArray = false
		else
			for i = 1, n do
				if v[i] == nil then isArray = false; break end
			end
		end
		if isArray then
			local parts = {}
			for i = 1, n do parts[i] = serialize(v[i]) end
			return "{" .. table.concat(parts, ", ") .. "}"
		else
			local parts = {}
			for k, val in pairs(v) do
				parts[table.getn(parts)+1] = tostring(k) .. " = " .. serialize(val)
			end
			table.sort(parts)
			return "{" .. table.concat(parts, ", ") .. "}"
		end
	else return tostring(v) end
end
`

// SerializeFnForTarget returns the appropriate serialize function for the target.
func SerializeFnForTarget(target transpiler.LuaTarget) string {
	if target == transpiler.LuaTargetLua50 {
		return SerializeFn50
	}
	return SerializeFn
}

// BuildRunner builds a Lua runner string for a single search path.
func BuildRunner(target transpiler.LuaTarget, dir string, modName string, accessor string) string {
	return BuildRunnerWithPaths(target, []string{dir}, modName, accessor)
}

// BuildRunnerWithPaths builds a Lua runner string that loads a module and serializes the result.
// For Lua 5.0, uses _LOADED + loadfile instead of package.path + require.
func BuildRunnerWithPaths(target transpiler.LuaTarget, dirs []string, modName string, accessor string) string {
	ser := SerializeFnForTarget(target)

	if target == transpiler.LuaTargetLua50 {
		var preloads string
		for _, dir := range dirs {
			preloads += fmt.Sprintf(`
do local f = loadfile(%q)
if f then _LOADED["lualib_bundle"] = f() end end
`, filepath.Join(dir, "lualib_bundle.lua"))
		}
		for _, dir := range dirs {
			preloads += fmt.Sprintf(`
if not _LOADED[%q] then
  local f = loadfile(%q)
  if f then _LOADED[%q] = f() end
end
`, modName, filepath.Join(dir, modName+".lua"), modName)
		}
		return ser + preloads + fmt.Sprintf(`
local mod = _LOADED[%q] or require(%q)
local result = %s
io.write(serialize(result))
`, modName, modName, accessor)
	}

	var pathParts []string
	for _, dir := range dirs {
		pathParts = append(pathParts, dir+"/?.lua")
	}

	return ser + fmt.Sprintf(`
package.path = %q
local mod = require(%q)
local result = %s
io.write(serialize(result))
`, strings.Join(pathParts, ";"), modName, accessor)
}

// FormatLuaCode returns Lua code with line numbers, highlighting the error line.
func FormatLuaCode(lua string, errMsg string) string {
	luaErrLineRe := regexp.MustCompile(`main\.lua:(\d+)`)
	errLine := -1
	if m := luaErrLineRe.FindStringSubmatch(errMsg); m != nil {
		errLine, _ = strconv.Atoi(m[1])
	}
	lines := strings.Split(lua, "\n")
	width := len(strconv.Itoa(len(lines)))
	var b strings.Builder
	for i, line := range lines {
		lineNo := i + 1
		if lineNo == errLine {
			fmt.Fprintf(&b, "\033[1;31m%*d│ %s\033[0m\n", width, lineNo, line)
		} else {
			fmt.Fprintf(&b, "%*d│ %s\n", width, lineNo, line)
		}
	}
	return b.String()
}

// ============================================================================
// Unified diff
// ============================================================================

// UseColor returns true when diff output should use ANSI colors.
var UseColor = sync.OnceValue(func() bool {
	return os.Getenv("FORCE_COLOR") != ""
})

// UnifiedDiff produces a unified-diff-style string comparing two texts.
// Only differing lines and up to 3 lines of context are shown.
// Colors output git-style (red/green) when FORCE_COLOR is set.
// If errMsg is non-empty, the error line (extracted from main.lua:N) is
// annotated with the error detail on a following line.
func UnifiedDiff(a, b, labelA, labelB, errMsg string) string {
	errLine := -1
	errDetail := ""
	if errMsg != "" {
		re := regexp.MustCompile(`main\.lua:(\d+):\s*(.*)`)
		if m := re.FindStringSubmatch(errMsg); m != nil {
			errLine, _ = strconv.Atoi(m[1])
			errDetail = m[2]
		}
	}
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")

	// Simple LCS-based diff via O(NM) DP
	n, m := len(aLines), len(bLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if aLines[i] == bLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	type diffLine struct {
		op   byte
		text string
		aNum int
		bNum int
	}

	var lines []diffLine
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && aLines[i] == bLines[j] {
			lines = append(lines, diffLine{' ', aLines[i], i + 1, j + 1})
			i++
			j++
		} else if i < n && (j >= m || dp[i+1][j] >= dp[i][j+1]) {
			lines = append(lines, diffLine{'-', aLines[i], i + 1, 0})
			i++
		} else {
			lines = append(lines, diffLine{'+', bLines[j], 0, j + 1})
			j++
		}
	}

	const ctx = 3
	color := UseColor()
	var buf strings.Builder
	if color {
		fmt.Fprintf(&buf, "\033[1;31m--- %s\033[0m\n\033[1;32m+++ %s\033[0m\n", labelA, labelB)
	} else {
		fmt.Fprintf(&buf, "--- %s\n+++ %s\n", labelA, labelB)
	}

	type span struct{ start, end int }
	var spans []span
	for k, l := range lines {
		if l.op != ' ' {
			s := k - ctx
			if s < 0 {
				s = 0
			}
			e := k + ctx + 1
			if e > len(lines) {
				e = len(lines)
			}
			if len(spans) > 0 && s <= spans[len(spans)-1].end {
				spans[len(spans)-1].end = e
			} else {
				spans = append(spans, span{s, e})
			}
		}
	}

	const (
		red   = "\033[31m"
		green = "\033[32m"
		cyan  = "\033[36m"
		reset = "\033[0m"
	)

	numWidth := len(strconv.Itoa(len(bLines)))
	pad := strings.Repeat(" ", numWidth)

	for _, sp := range spans {
		aStart, aCount, bStart, bCount := 0, 0, 0, 0
		for k := sp.start; k < sp.end; k++ {
			switch lines[k].op {
			case ' ':
				aCount++
				bCount++
				if aStart == 0 {
					aStart = lines[k].aNum
				}
				if bStart == 0 {
					bStart = lines[k].bNum
				}
			case '-':
				aCount++
				if aStart == 0 {
					aStart = lines[k].aNum
				}
			case '+':
				bCount++
				if bStart == 0 {
					bStart = lines[k].bNum
				}
			}
		}
		if aStart == 0 {
			aStart = 1
		}
		if bStart == 0 {
			bStart = 1
		}
		hdr := fmt.Sprintf("@@ -%d,%d +%d,%d @@", aStart, aCount, bStart, bCount)
		if color {
			fmt.Fprintf(&buf, "%s%s%s\n", cyan, hdr, reset)
		} else {
			fmt.Fprintln(&buf, hdr)
		}
		for k := sp.start; k < sp.end; k++ {
			l := lines[k]
			if color {
				switch l.op {
				case '-':
					fmt.Fprintf(&buf, "%s %s-%s%s\n", red, pad, l.text, reset)
				case '+':
					fmt.Fprintf(&buf, "%s%*d│+%s%s\n", green, numWidth, l.bNum, l.text, reset)
				default:
					fmt.Fprintf(&buf, "%*d│ %s\n", numWidth, l.bNum, l.text)
				}
			} else {
				switch l.op {
				case '-':
					fmt.Fprintf(&buf, " %s-%s\n", pad, l.text)
				case '+':
					fmt.Fprintf(&buf, "%*d│+%s\n", numWidth, l.bNum, l.text)
				default:
					fmt.Fprintf(&buf, "%*d│ %s\n", numWidth, l.bNum, l.text)
				}
			}
			if l.op != '-' && l.bNum == errLine && errDetail != "" {
				annotation := fmt.Sprintf("%s  ← %s", pad, errDetail)
				if color {
					fmt.Fprintf(&buf, "\033[1;31m%s%s\n", annotation, reset)
				} else {
					fmt.Fprintf(&buf, "%s\n", annotation)
				}
			}
		}
	}

	return buf.String()
}
