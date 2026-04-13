// Test helper for migrated TSTL tests. Transpiles TS → Lua in-process, executes with luajit.
package tstltest

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/realcoldfry/tslua/internal/luatest"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// ============================================================================
// Test options — functional options for configuring test behavior
// ============================================================================

type testOpts struct {
	extraFiles       map[string]string
	compilerOptions  map[string]any
	returnExport     []string
	luaTarget        transpiler.LuaTarget
	luaHeader        string
	emitMode         transpiler.EmitMode
	luaFactory       func(code string) string // wraps generated Lua before execution
	mainFileName     string                   // override main file name (e.g. "main.tsx" for JSX)
	allowDiagnostics bool                     // if true, don't fail on TS semantic diagnostics
}

// sourceFileCache caches parsed lib source files across tests.
var sourceFileCache sync.Map

// cachingHost wraps a CompilerHost to cache GetSourceFile results for lib files.
type cachingHost struct {
	compiler.CompilerHost
}

func (h *cachingHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	// Only cache lib .d.ts files (immutable). User test files must not be cached.
	if !strings.Contains(opts.FileName, "/lib.") || !strings.HasSuffix(opts.FileName, ".d.ts") {
		return h.CompilerHost.GetSourceFile(opts)
	}
	if v, ok := sourceFileCache.Load(opts.FileName); ok {
		return v.(*ast.SourceFile)
	}
	sf := h.CompilerHost.GetSourceFile(opts)
	if sf != nil {
		sourceFileCache.Store(opts.FileName, sf)
	}
	return sf
}

type TestOpt func(*testOpts)

func WithExtraFile(name, code string) TestOpt {
	return func(o *testOpts) {
		if o.extraFiles == nil {
			o.extraFiles = make(map[string]string)
		}
		o.extraFiles[name] = code
	}
}

func WithOptions(opts map[string]any) TestOpt {
	return func(o *testOpts) {
		if o.compilerOptions == nil {
			o.compilerOptions = make(map[string]any)
		}
		for k, v := range opts {
			o.compilerOptions[k] = v
		}
	}
}

func WithReturnExport(names ...string) TestOpt {
	return func(o *testOpts) {
		o.returnExport = names
	}
}

func WithLuaTarget(target transpiler.LuaTarget) TestOpt {
	return func(o *testOpts) {
		o.luaTarget = target
	}
}

func WithLuaHeader(header string) TestOpt {
	return func(o *testOpts) {
		o.luaHeader = header
	}
}

// WithLuaFactory wraps the generated Lua code before execution.
// Used for tests like $vararg that need the code wrapped in a function receiving varargs.
func WithLuaFactory(factory func(code string) string) TestOpt {
	return func(o *testOpts) {
		o.luaFactory = factory
	}
}

func WithMainFileName(name string) TestOpt {
	return func(o *testOpts) {
		o.mainFileName = name
	}
}

// WithAllowDiagnostics suppresses the default behavior of failing on TS semantic diagnostics.
func WithAllowDiagnostics() TestOpt {
	return func(o *testOpts) {
		o.allowDiagnostics = true
	}
}

// languageExtensionsPath returns the absolute path to the TSTL language-extensions
// type declarations directory (extern/tstl/language-extensions).
func languageExtensionsPath() string {
	// runtime.Caller gives us this source file's path; navigate to repo root from there.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
}

// WithLanguageExtensions adds the TSTL language extension type declarations
// to the compiler's types, enabling LuaTable, $multi, $range, etc.
func WithLanguageExtensions() TestOpt {
	return WithOptions(map[string]any{
		"types": []string{languageExtensionsPath()},
	})
}

var globalEmitMode = flag.String("emit-mode", "", "override emit mode for all tests (tstl, optimized)")

func buildOpts(opts []TestOpt) testOpts {
	var o testOpts
	for _, opt := range opts {
		opt(&o)
	}
	if *globalEmitMode != "" && o.emitMode == "" {
		o.emitMode = transpiler.EmitMode(*globalEmitMode)
	}
	return o
}

// Test result counters for summary output
var testsPassed atomic.Int64
var testsFailed atomic.Int64
var testsSkipped atomic.Int64

// trackResult registers a cleanup function that increments the appropriate counter
// when the test finishes. Call at the start of leaf test functions.
func trackResult(t *testing.T) {
	t.Cleanup(func() {
		if t.Skipped() {
			testsSkipped.Add(1)
		} else if t.Failed() {
			testsFailed.Add(1)
		} else {
			testsPassed.Add(1)
		}
	})
}

// getLualibBundle returns the appropriate lualib bundle for the target.
// Uses tslua-built bundle if TSLUA_LUALIB=tslua, otherwise the embedded TSTL bundle.
func getLualibBundle(target transpiler.LuaTarget) []byte {
	if tsluaLualibBundle != nil {
		if target == transpiler.LuaTargetLua50 && tsluaLualibBundle50 != nil {
			return tsluaLualibBundle50
		}
		return tsluaLualibBundle
	}
	return lualib.BundleForTarget(string(target))
}

// bundleForResults returns the lualib bundle bytes appropriate to the test
// options: a slim bundle when luaLibImport is require-minimal, otherwise the
// full bundle. usedExports is aggregated across all results' LualibDeps.
func bundleForResults(o testOpts, target transpiler.LuaTarget, results []transpileResult) []byte {
	if mode, _ := o.compilerOptions["luaLibImport"].(string); mode == string(transpiler.LuaLibImportRequireMinimal) {
		seen := make(map[string]bool)
		var usedExports []string
		for _, r := range results {
			for _, exp := range r.lualibDeps {
				if !seen[exp] {
					seen[exp] = true
					usedExports = append(usedExports, exp)
				}
			}
		}
		sort.Strings(usedExports)
		content, err := lualib.MinimalBundleForTarget(string(target), usedExports)
		if err == nil {
			return content
		}
	}
	return getLualibBundle(target)
}

// tslua-built lualib bundles when TSLUA_LUALIB=tslua is set.
var tsluaLualibBundle []byte   // universal (Lua 5.1+)
var tsluaLualibBundle50 []byte // Lua 5.0

func TestMain(m *testing.M) {
	flag.Parse()
	if *globalEmitMode != "" {
		fmt.Fprintf(os.Stderr, "=== EMIT MODE: %s ===\n", *globalEmitMode)
	}
	if os.Getenv("TSLUA_LUALIB") == "tslua" {
		fmt.Fprintln(os.Stderr, "=== LUALIB: tslua-built ===")
		repoRoot := luatest.FindRepoFile("go.mod")
		if repoRoot != "" {
			repoRoot = filepath.Dir(repoRoot)
			srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
			langExtPath := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
			luaTypesPath := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")
			bundle, err := lualib.BuildBundleFromSource(srcDir, langExtPath, luaTypesPath, transpiler.LuaTargetUniversal, "universal")
			if err != nil {
				fmt.Fprintf(os.Stderr, "FATAL: build tslua lualib: %v\n", err)
				os.Exit(1)
			}
			tsluaLualibBundle = []byte(bundle)
			fmt.Fprintf(os.Stderr, "  bundle size: %d bytes\n", len(tsluaLualibBundle))
			bundle50, err := lualib.BuildBundleFromSource(srcDir, langExtPath, luaTypesPath, transpiler.LuaTargetLua50, "5.0")
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: build tslua lualib 5.0: %v\n", err)
			} else {
				tsluaLualibBundle50 = []byte(bundle50)
				fmt.Fprintf(os.Stderr, "  bundle 5.0 size: %d bytes\n", len(tsluaLualibBundle50))
			}
		}
	}
	luatest.Setup()

	code := m.Run()

	luatest.Teardown()

	p := testsPassed.Load()
	f := testsFailed.Load()
	s := testsSkipped.Load()
	total := p + f + s
	fmt.Fprintf(os.Stderr, "\n=== SUMMARY: %d passed, %d failed, %d skipped, %d total ===\n", p, f, s, total)
	os.Exit(code)
}

// ============================================================================
// TS → Lua transpilation
// ============================================================================

// TestSerializer verifies that the Lua serialize function produces consistent output
// across all available Lua runtimes. Catches format differences like \0 vs \000.
func TestSerializer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		expr string // Lua expression to serialize
		want string
	}{
		{"nil", "nil", "nil"},
		{"true", "true", "true"},
		{"false", "false", "false"},
		{"integer", "42", "42"},
		{"negative", "-1", "-1"},
		{"float", "3.14", "3.14"},
		{"nan", "0/0", "NaN"},
		{"inf", "1/0", "Infinity"},
		{"neg_inf", "-1/0", "-Infinity"},
		{"empty_string", `""`, `""`},
		{"simple_string", `"hello"`, `"hello"`},
		{"string_with_quotes", `'say "hi"'`, `"say \"hi\""`},
		{"string_with_newline", `"a\nb"`, `"a\nb"`},
		{"string_with_null", `"a\0c"`, `"a\000c"`},
		{"string_with_tab", `"a\tb"`, `"a\tb"`},
		{"empty_table", "{}", "{}"},
		{"array", "{1, 2, 3}", "{1, 2, 3}"},
		{"nested_array", "{{1, 2}, {3, 4}}", "{{1, 2}, {3, 4}}"},
		{"string_array", `{"a", "b", "c"}`, `{"a", "b", "c"}`},
		{"object", `{x = 1, y = 2}`, `{x = 1, y = 2}`},
		{"mixed_object", `{a = "hello", b = true, c = 42}`, `{a = "hello", b = true, c = 42}`},
	}

	for _, runtime := range []string{"luajit", "lua5.0", "lua5.1", "lua5.2", "lua5.3", "lua5.4", "lua5.5"} {
		e, ok := luatest.Evaluators[runtime]
		if !ok {
			continue
		}
		serFn := luatest.SerializeFn
		if runtime == "lua5.0" {
			serFn = luatest.SerializeFn50
		}
		t.Run(runtime, func(t *testing.T) {
			t.Parallel()
			serFn := serFn // capture for parallel
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					code := serFn + "\nio.write(serialize(" + tc.expr + "))"
					got, err := e.Eval(code)
					if err != nil {
						t.Fatalf("eval error: %v", err)
					}
					if got != tc.want {
						t.Errorf("serialize(%s) = %q, want %q", tc.expr, got, tc.want)
					}
				})
			}
		})
	}
}

// buildTsconfig builds a tsconfig.json string with optional compiler option overrides.
func buildTsconfig(opts testOpts) string {
	compilerOptions := map[string]any{
		"target":           "ES2017",
		"lib":              []string{"esnext"},
		"moduleResolution": "bundler",
		"strict":           false,
		"skipLibCheck":     true,
	}
	for k, v := range opts.compilerOptions {
		// Skip tslua-specific options that aren't real TypeScript compiler options.
		if k == "luaBundle" || k == "luaBundleEntry" || k == "luaLibImport" || k == "noImplicitSelf" || k == "noImplicitGlobalVariables" {
			continue
		}
		// Convert numeric enum values to strings (migrate script emits TS enum ints).
		if k == "jsx" {
			if n, ok := v.(int); ok {
				switch n {
				case 1:
					v = "preserve"
				case 2:
					v = "react"
				case 3:
					v = "react-native"
				case 4:
					v = "react-jsx"
				case 5:
					v = "react-jsxdev"
				}
			}
		}
		if k == "module" {
			if n, ok := v.(int); ok {
				switch n {
				case 1:
					v = "commonjs"
				case 2:
					v = "amd"
				case 3:
					v = "umd"
				case 4:
					v = "system"
				case 5:
					v = "es2015"
				case 6:
					v = "es2020"
				case 7:
					v = "es2022"
				case 99:
					v = "esnext"
				case 100:
					v = "node16"
				}
			}
		}
		compilerOptions[k] = v
	}
	include := []string{"**/*.ts"}
	if _, hasJsx := compilerOptions["jsx"]; hasJsx {
		include = append(include, "**/*.tsx")
	}
	if _, hasJSON := compilerOptions["resolveJsonModule"]; hasJSON {
		include = append(include, "**/*.json")
	}
	tsconfig := map[string]any{
		"compilerOptions": compilerOptions,
		"include":         include,
	}
	b, _ := json.MarshalIndent(tsconfig, "", "\t")
	return string(b)
}

// semanticErrors filters semantic diagnostics to only errors (not warnings/suggestions).
func semanticErrors(program *compiler.Program) []*ast.Diagnostic {
	diags := compiler.SortAndDeduplicateDiagnostics(
		compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil),
	)
	var errors []*ast.Diagnostic
	for _, d := range diags {
		if ast.Diagnostic_Category(d) == 1 { // diagnostics.CategoryError
			errors = append(errors, d)
		}
	}
	return errors
}

// checkSemanticDiagnostics collects TS semantic errors and fails the test if any are found.
func checkSemanticDiagnostics(t *testing.T, program *compiler.Program) {
	t.Helper()
	errors := semanticErrors(program)
	if len(errors) == 0 {
		return
	}
	var msgs []string
	for _, d := range errors {
		msgs = append(msgs, ast.Diagnostic_Localize(d, ast.DefaultLocale()))
	}
	t.Fatalf("TypeScript semantic errors (%d):\n%s", len(errors), strings.Join(msgs, "\n"))
}

// collectSemanticErrorsByFile returns error diagnostics grouped by source file path (relative to baseDir).
func collectSemanticErrorsByFile(program *compiler.Program, baseDir string) map[string][]string {
	errors := semanticErrors(program)
	if len(errors) == 0 {
		return nil
	}
	result := make(map[string][]string)
	for _, d := range errors {
		key := ""
		if d.File() != nil {
			rel, err := filepath.Rel(baseDir, d.File().FileName())
			if err == nil {
				key = rel
			} else {
				key = d.File().FileName()
			}
		}
		result[key] = append(result[key], ast.Diagnostic_Localize(d, ast.DefaultLocale()))
	}
	return result
}

// transpileOpts builds TranspileOptions from test options.
func transpileOpts(o testOpts) transpiler.TranspileOptions {
	opts := transpiler.TranspileOptions{}
	if o.emitMode != "" {
		opts.EmitMode = o.emitMode
	}
	if v, ok := o.compilerOptions["noImplicitSelf"].(bool); ok && v {
		opts.NoImplicitSelf = true
	}
	if v, ok := o.compilerOptions["noImplicitGlobalVariables"].(bool); ok && v {
		opts.NoImplicitGlobalVariables = true
	}
	if v, ok := o.compilerOptions["luaLibImport"].(string); ok && v != "" {
		opts.LuaLibImport = transpiler.LuaLibImportKind(v)
		if opts.LuaLibImport == transpiler.LuaLibImportInline {
			target := o.luaTarget
			if target == "" {
				target = transpiler.LuaTargetLua55
			}
			if fd, err := lualib.FeatureDataForTarget(string(target)); err == nil {
				opts.LualibFeatureData = fd
			}
		}
	}
	return opts
}

// transpileResult holds per-file Lua output.
type transpileResult struct {
	fileName   string
	lua        string
	usesLualib bool
	lualibDeps []string
}

// transpileTS compiles TypeScript source to Lua using the full compiler pipeline.
func transpileTS(t *testing.T, tsCode string, opts ...TestOpt) []transpileResult {
	t.Helper()

	o := buildOpts(opts)
	tmpDir := t.TempDir()

	// Determine the main source file name.
	mainPath := "main.ts"
	if o.mainFileName != "" {
		mainPath = o.mainFileName
	}
	if entry, ok := o.compilerOptions["luaBundleEntry"].(string); ok && entry != "" {
		mainPath = entry
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(buildTsconfig(o)), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(tmpDir, mainPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(tsCode), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, code := range o.extraFiles {
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
	host := &cachingHost{compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)}

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

	if !o.allowDiagnostics {
		checkSemanticDiagnostics(t, program)
	}

	luaTarget := o.luaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}

	// Compute sourceRoot: use rootDir if set, otherwise tmpDir.
	sourceRoot := tmpDir
	if rootDir, ok := o.compilerOptions["rootDir"].(string); ok && rootDir != "" {
		sourceRoot = filepath.Join(tmpDir, rootDir)
	}

	rawResults, _ := transpiler.TranspileProgramWithOptions(program, sourceRoot, luaTarget, nil, transpileOpts(o))

	// Bundle mode: wrap all results into a single bundled Lua file.
	if luaBundle, ok := o.compilerOptions["luaBundle"].(string); ok && luaBundle != "" {
		entryPath := mainPath
		entryModule := transpiler.ModuleNameFromPath(filepath.Join(tmpDir, entryPath), sourceRoot)

		var lualibContent []byte
		usesLualib := false
		for _, r := range rawResults {
			if r.UsesLualib {
				usesLualib = true
				break
			}
		}
		if usesLualib {
			// Build a transpileResult slice so bundleForResults can read LualibDeps.
			var resultSlice []transpileResult
			for _, r := range rawResults {
				resultSlice = append(resultSlice, transpileResult{
					usesLualib: r.UsesLualib,
					lualibDeps: r.LualibDeps,
				})
			}
			lualibContent = bundleForResults(o, luaTarget, resultSlice)
		}

		bundled, err := transpiler.BundleProgram(rawResults, sourceRoot, lualibContent, transpiler.BundleOptions{
			EntryModule: entryModule,
			LuaTarget:   luaTarget,
		})
		if err != nil {
			t.Fatal(err)
		}
		return []transpileResult{{fileName: "main.ts", lua: bundled, usesLualib: false}}
	}

	var results []transpileResult
	for _, r := range rawResults {
		rel, _ := filepath.Rel(sourceRoot, r.FileName)
		results = append(results, transpileResult{
			fileName:   rel,
			lua:        r.Lua,
			usesLualib: r.UsesLualib,
			lualibDeps: r.LualibDeps,
		})
	}
	return results
}

// ============================================================================
// Lua execution
// ============================================================================

// runLua writes all transpiled Lua files to a temp dir and executes with the appropriate Lua runtime.
func runLua(t *testing.T, results []transpileResult, accessor string, opts ...TestOpt) string {
	t.Helper()

	o := buildOpts(opts)
	tmpDir := t.TempDir()
	usesLualib := false
	mainLua := ""
	for _, r := range results {
		luaCode := r.lua
		isMain := strings.HasSuffix(r.fileName, "main.ts") || strings.HasSuffix(r.fileName, "main.tsx")
		// Apply luaFactory to the main file (wraps code, e.g., in a vararg-receiving function)
		if isMain && o.luaFactory != nil {
			luaCode = o.luaFactory(luaCode)
		}
		luaName := strings.TrimSuffix(strings.TrimSuffix(r.fileName, ".tsx"), ".ts") + ".lua"
		outPath := filepath.Join(tmpDir, luaName)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outPath, []byte(luaCode), 0o644); err != nil {
			t.Fatal(err)
		}
		if isMain {
			mainLua = luaCode
		}
		if r.usesLualib {
			usesLualib = true
		}
	}
	luaTarget := o.luaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}
	if usesLualib {
		if err := os.WriteFile(filepath.Join(tmpDir, "lualib_bundle.lua"), bundleForResults(o, luaTarget, results), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	luaRuntime := luaTarget.Runtime()

	runner := luatest.BuildRunner(luaTarget, tmpDir, "main", accessor)
	if o.luaHeader != "" {
		runner = o.luaHeader + "\n" + runner
	}
	// Use eval server if available, fall back to process spawn
	if e, ok := luatest.Evaluators[luaRuntime]; ok {
		result, err := e.Eval(runner)
		if err != nil {
			t.Fatalf("%s error: %v\nlua code:\n%s", luaRuntime, err, luatest.FormatLuaCode(mainLua, err.Error()))
		}
		return result
	}

	cmd := exec.Command(luaRuntime, "-e", runner)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error: %v\noutput: %s\nlua code:\n%s", luaRuntime, err, out, luatest.FormatLuaCode(mainLua, err.Error()))
	}
	return string(out)
}

// ============================================================================
// Lua error formatting (delegated to luatest)
// ============================================================================

// ============================================================================
// Accessor helpers
// ============================================================================

func moduleAccessor(returnExport []string) string {
	if len(returnExport) == 0 {
		return "mod"
	}
	acc := "mod"
	for _, name := range returnExport {
		acc += fmt.Sprintf("[%q]", name)
	}
	return acc
}

// ============================================================================
// Assertion helpers — baked expected values (for expectToEqual cases where
// TSTL intentionally diverges from JS semantics)
// ============================================================================

// batchTestCase holds one test case for batched compilation.
type batchTestCase struct {
	name             string
	tsCode           string // full TS source for this test's main.ts
	accessor         string // Lua expression to extract the result from the module
	want             string
	refLua           string            // TSTL reference Lua output (for error messages)
	allowErrors      bool              // if true, Lua runtime errors are caught and compared as ExecutionError
	extraFiles       map[string]string // optional extra files (e.g. "module.ts" → code)
	tstlBug          string            // if set, TSTL's expected value is wrong; skip codegen comparison
	entryPoint       string            // custom entry point path (e.g. "main/main.ts"); defaults to "main.ts"
	allowDiagnostics bool              // if true, skip TS semantic diagnostic check for this case
}

// exprTestCase is one expression test: compile "export const __result = expr" and check the result.
type exprTestCase struct {
	name             string
	expr             string
	want             string
	refLua           string
	allowErrors      bool
	allowDiagnostics bool
}

// batchRunTests compiles all test cases in a single Program (one .ts file per case),
// avoiding repeated compiler/lib setup. Each test file is independent, so a runtime
// error in one doesn't affect others.
func batchRunTests(t *testing.T, cases []batchTestCase, opts ...TestOpt) {
	t.Helper()

	o := buildOpts(opts)
	tmpDir := t.TempDir()

	for i, c := range cases {
		dir := filepath.Join(tmpDir, fmt.Sprintf("test_%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mainFile := "main.ts"
		if c.entryPoint != "" {
			mainFile = c.entryPoint
		}
		mainPath := filepath.Join(dir, mainFile)
		if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mainPath, []byte(c.tsCode), 0o644); err != nil {
			t.Fatal(err)
		}
		for name, code := range c.extraFiles {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(buildTsconfig(o)), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := &cachingHost{compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)}

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

	var diagsByFile map[string][]string
	if !o.allowDiagnostics {
		diagsByFile = collectSemanticErrorsByFile(program, tmpDir)
	}

	luaTarget := o.luaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}
	rawResults, _ := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, transpileOpts(o))

	// Build expected entry file path per test case.
	entryByTest := make(map[int]string, len(cases))
	for i, c := range cases {
		entry := "main.ts"
		if c.entryPoint != "" {
			entry = c.entryPoint
		}
		entryByTest[i] = fmt.Sprintf("test_%d/%s", i, entry)
	}

	luaByTest := make(map[int]transpileResult)
	usesLualib := false
	for _, r := range rawResults {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		for i, entry := range entryByTest {
			if rel == entry {
				luaByTest[i] = transpileResult{fileName: rel, lua: r.Lua, usesLualib: r.UsesLualib}
				break
			}
		}
		if r.UsesLualib || strings.Contains(r.Lua, `require("lualib_bundle")`) {
			usesLualib = true
		}
	}

	// Write all Lua output preserving subdir structure
	luaDir := t.TempDir()
	for _, r := range rawResults {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		luaName := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(rel, ".tsx"), ".ts"), ".json") + ".lua"
		outPath := filepath.Join(luaDir, luaName)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outPath, []byte(r.Lua), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if usesLualib {
		// Write lualib into each test subdir so require("lualib_bundle") resolves
		for i := range cases {
			dir := filepath.Join(luaDir, fmt.Sprintf("test_%d", i))
			_ = os.MkdirAll(dir, 0o755)
			if err := os.WriteFile(filepath.Join(dir, "lualib_bundle.lua"), getLualibBundle(o.luaTarget), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	luaRuntime := luaTarget.Runtime()

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			trackResult(t)

			// Check for TS semantic diagnostics on this test's source file.
			if diagsByFile != nil && !c.allowDiagnostics {
				if msgs, ok := diagsByFile[entryByTest[i]]; ok {
					t.Fatalf("TypeScript semantic errors (%d):\n%s", len(msgs), strings.Join(msgs, "\n"))
				}
			}

			r, ok := luaByTest[i]
			if !ok {
				t.Fatal("no transpile result")
			}

			testDir := filepath.Join(luaDir, fmt.Sprintf("test_%d", i))
			// Derive Lua module name from entry point: "main/main.ts" → "main.main"
			modName := "main"
			if c.entryPoint != "" {
				modName = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(c.entryPoint, ".ts"), ".tsx"), ".json")
				modName = strings.ReplaceAll(modName, "/", ".")
			}
			runner := luatest.BuildRunnerWithPaths(luaTarget, []string{testDir, luaDir}, modName, c.accessor)

			makeDiff := func(errMsg string) string {
				if c.refLua == "" {
					if errMsg != "" {
						return "\nlua code:\n" + luatest.FormatLuaCode(r.lua, errMsg)
					}
					return ""
				}
				ref := strings.TrimSpace(strings.TrimPrefix(c.refLua, tstlHeader))
				ours := strings.TrimSpace(r.lua)
				if ref == ours {
					if errMsg != "" {
						return "\nlua code:\n" + luatest.FormatLuaCode(r.lua, errMsg)
					}
					return ""
				}
				return "\nlua diff:\n" + unifiedDiff(ref, ours, "tstl", "tslua", errMsg)
			}

			expectsError := c.allowErrors || strings.Contains(c.want, `name = "ExecutionError"`)
			var got string
			if e, ok := luatest.Evaluators[luaRuntime]; ok {
				var err error
				got, err = e.Eval(runner)
				if err != nil {
					if expectsError {
						got = formatExecutionError(err)
					} else {
						t.Fatalf("lua error: %v%s", err, makeDiff(err.Error()))
					}
				}
			} else {
				cmd := exec.Command(luaRuntime, "-e", runner)
				out, err := cmd.CombinedOutput()
				if err != nil {
					if expectsError {
						got = formatExecutionError(fmt.Errorf("%s", out))
					} else {
						t.Fatalf("lua error: %v\noutput: %s%s", err, out, makeDiff(err.Error()))
					}
				} else {
					got = string(out)
				}
			}
			if !valuesMatch(got, c.want) {
				t.Errorf("got %s, want %s\nts: %s%s", got, c.want, c.tsCode, makeDiff(""))
			}
		})
	}
}

// batchExpectExpressions batches expression tests: each case compiles "export const __result = expr;".
func batchExpectExpressions(t *testing.T, cases []exprTestCase, opts ...TestOpt) {
	t.Helper()
	batch := make([]batchTestCase, len(cases))
	for i, c := range cases {
		batch[i] = batchTestCase{
			name:             c.name,
			tsCode:           "export const __result = " + c.expr + ";\n",
			accessor:         `mod["__result"]`,
			want:             c.want,
			refLua:           c.refLua,
			allowErrors:      c.allowErrors,
			allowDiagnostics: c.allowDiagnostics,
		}
	}
	batchRunTests(t, batch, opts...)
}

type funcTestCase struct {
	name             string
	body             string
	want             string
	refLua           string
	allowErrors      bool
	allowDiagnostics bool
}

// batchExpectFunctions batches function tests: each case compiles "export function __main() { body }".
func batchExpectFunctions(t *testing.T, cases []funcTestCase, opts ...TestOpt) {
	t.Helper()
	batch := make([]batchTestCase, len(cases))
	for i, c := range cases {
		batch[i] = batchTestCase{
			name:             c.name,
			tsCode:           "export function __main() {" + c.body + "}\n",
			accessor:         "mod.__main()",
			want:             c.want,
			refLua:           c.refLua,
			allowErrors:      c.allowErrors,
			allowDiagnostics: c.allowDiagnostics,
		}
	}
	batchRunTests(t, batch, opts...)
}

type moduleTestCase struct {
	name             string
	code             string
	want             string
	refLua           string
	allowErrors      bool
	allowDiagnostics bool
}

// batchExpectModules batches module tests: each case compiles the full TS source as a module.
func batchExpectModules(t *testing.T, cases []moduleTestCase, opts ...TestOpt) {
	t.Helper()
	o := buildOpts(opts)
	acc := moduleAccessor(o.returnExport)
	batch := make([]batchTestCase, len(cases))
	for i, c := range cases {
		batch[i] = batchTestCase{
			name:             c.name,
			tsCode:           c.code,
			accessor:         acc,
			want:             c.want,
			refLua:           c.refLua,
			allowErrors:      c.allowErrors,
			allowDiagnostics: c.allowDiagnostics,
		}
	}
	batchRunTests(t, batch, opts...)
}

func expectExpression(t *testing.T, tsExpr string, want string, opts ...TestOpt) {
	t.Helper()
	trackResult(t)
	tsCode := "export const __result = " + tsExpr + ";"
	results := transpileTS(t, tsCode, opts...)
	got := runLua(t, results, `mod["__result"]`, opts...)
	if !valuesMatch(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

// formatExecutionError converts a Lua runtime error into the serialized form
// {message = "...", name = "ExecutionError"} for comparison with baked expected values.
func formatExecutionError(err error) string {
	msg := err.Error()
	// Strip wrapper prefixes: "lua error: " from Go evaluator
	msg = strings.TrimPrefix(msg, "lua error: ")
	// Lua errors can be multi-line (e.g. module loading errors with nested causes).
	// The actual error message is after the last "\n\t" or after the "file:line: " prefix.
	if idx := strings.LastIndex(msg, "\n\t"); idx >= 0 {
		msg = msg[idx+2:]
	}
	// Strip Lua's "file:line: " prefix (e.g. "...main.lua:5: actual message")
	if idx := strings.Index(msg, ".lua:"); idx >= 0 {
		rest := msg[idx+4:] // skip ".lua"
		if colonIdx := strings.Index(rest, ": "); colonIdx >= 0 {
			msg = rest[colonIdx+2:]
		}
	}
	return `{message = ` + fmt.Sprintf("%q", msg) + `, name = "ExecutionError"}`
}

// valuesMatch compares got against want, handling ~contains: markers for substring matching.
// The want string may contain "~contains:X" tokens where X is matched as a substring in got.
// For example, want = `{stack = ~contains:stack traceback}` matches got = `{stack = "stack traceback:\n\t..."}`.
func valuesMatch(got, want string) bool {
	// Normalize platform-dependent NaN representation: Linux uses "-nan", macOS uses "nan".
	got = strings.ReplaceAll(got, "-nan", "nan")
	if got == want {
		return true
	}
	if !strings.Contains(want, "~contains:") {
		return approxEqualStrings(got, want)
	}
	// Split want into literal segments and contains-match segments.
	// Pattern: everything before "~contains:" is literal, then the contains value
	// runs until the next "}" or "," (end of the serialized field).
	remainder := got
	pos := 0
	for pos < len(want) {
		idx := strings.Index(want[pos:], "~contains:")
		if idx < 0 {
			// Rest is literal — must match exactly
			return remainder == want[pos:]
		}
		// Literal part before the marker must match
		literal := want[pos : pos+idx]
		if !strings.HasPrefix(remainder, literal) {
			return false
		}
		remainder = remainder[len(literal):]
		// Extract the contains value (runs until next }, or comma at same nesting)
		markerStart := pos + idx + len("~contains:")
		end := markerStart
		for end < len(want) && want[end] != '}' && want[end] != ',' {
			end++
		}
		substr := want[markerStart:end]
		// Find the corresponding field value in got — it's a quoted string or unquoted value
		// ending at the same delimiter
		gotEnd := 0
		for gotEnd < len(remainder) && remainder[gotEnd] != '}' && remainder[gotEnd] != ',' {
			gotEnd++
		}
		gotField := remainder[:gotEnd]
		if !strings.Contains(gotField, substr) {
			return false
		}
		remainder = remainder[gotEnd:]
		pos = end
	}
	return remainder == ""
}

// approxEqualStrings compares two strings as floats with epsilon tolerance.
// Returns false if either is not a valid number.
func approxEqualStrings(a, b string) bool {
	af, aerr := strconv.ParseFloat(a, 64)
	bf, berr := strconv.ParseFloat(b, 64)
	if aerr != nil || berr != nil {
		return false
	}
	if af == bf {
		return true
	}
	diff := math.Abs(af - bf)
	scale := math.Max(math.Abs(af), math.Abs(bf))
	if scale == 0 {
		return diff < 1e-15
	}
	return diff/scale < 1e-10
}

func expectExpressionApprox(t *testing.T, tsExpr string, want float64, opts ...TestOpt) {
	t.Helper()
	trackResult(t)
	tsCode := "export const __result = " + tsExpr + ";"
	results := transpileTS(t, tsCode, opts...)
	got := runLua(t, results, `mod["__result"]`, opts...)
	gotF, err := strconv.ParseFloat(got, 64)
	if err != nil {
		t.Errorf("got non-numeric result %q, want %g", got, want)
		return
	}
	if math.Abs(gotF-want) > 1e-10 {
		t.Errorf("got %s, want %g", got, want)
	}
}

func expectFunction(t *testing.T, tsBody string, want string, opts ...TestOpt) {
	t.Helper()
	trackResult(t)
	var tsCode string
	// If tsBody already contains the exported __main function (pre-wrapped with header),
	// use it as-is. Otherwise wrap the body in the function.
	if strings.Contains(tsBody, "export function __main()") {
		tsCode = tsBody
	} else {
		tsCode = "export function __main() {" + tsBody + "}"
	}
	results := transpileTS(t, tsCode, opts...)
	got := runLua(t, results, "mod.__main()", opts...)
	if !valuesMatch(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func expectModule(t *testing.T, tsCode string, want string, opts ...TestOpt) {
	t.Helper()
	trackResult(t)
	o := buildOpts(opts)
	results := transpileTS(t, tsCode, opts...)
	got := runLua(t, results, moduleAccessor(o.returnExport), opts...)
	if !valuesMatch(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

type codegenTestCase struct {
	name        string
	mode        string // "expression", "function", "module"
	tsCode      string
	contains    []string // Lua output must contain these strings
	notContains []string // Lua output must NOT contain these strings
	matches     []string // Lua output must match these regex patterns
	notMatches  []string // Lua output must NOT match these regex patterns
}

type diagTestCase struct {
	name        string
	mode        string // "expression", "function", "module"
	tsCode      string
	wantCodes   []int32
	wantCodegen []string // if set, Lua output must contain these strings
}

// batchExpectDiagnostics compiles each diagnostic test case independently.
// Each test gets its own Program to match TSTL's per-test compilation model,
// avoiding global scope collisions between script-mode files.
func batchExpectDiagnostics(t *testing.T, cases []diagTestCase, opts ...TestOpt) {
	t.Helper()

	o := buildOpts(opts)
	tmpDir := t.TempDir()
	tsconfigJSON := buildTsconfig(o)
	luaTarget := o.luaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}

	type testResult struct {
		diags []*ast.Diagnostic
		lua   string
	}
	results := make([]testResult, len(cases))

	// Split cases: those needing codegen checks compile individually (they can't
	// have `export {}` appended without changing output shape), the rest batch
	// into a single Program with `export {}` for module-scope isolation.
	var batchIndices []int
	var soloIndices []int
	for i, c := range cases {
		if len(c.wantCodegen) > 0 {
			soloIndices = append(soloIndices, i)
		} else {
			batchIndices = append(batchIndices, i)
		}
	}

	// Write batch files under shared tmpDir.
	for _, i := range batchIndices {
		c := cases[i]
		var tsCode string
		switch c.mode {
		case "expression":
			tsCode = "export const __result = " + c.tsCode + ";\n"
		case "function":
			tsCode = "export function __main() {" + c.tsCode + "}\n"
		default:
			tsCode = c.tsCode + "\nexport {};\n"
		}
		dir := filepath.Join(tmpDir, fmt.Sprintf("test_%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(tsCode), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))

	// Batch compilation: single Program for all non-codegen cases.
	if len(batchIndices) > 0 {
		if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfigJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
		configDir := tspath.GetDirectoryPath(configPath)
		host := &cachingHost{compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)}

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

		transpileResults, tstlDiags := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, transpileOpts(o))

		luaByFile := make(map[string]string)
		for _, tr := range transpileResults {
			rel, _ := filepath.Rel(tmpDir, tr.FileName)
			luaByFile[rel] = tr.Lua
		}
		diagsByFile := make(map[string][]*ast.Diagnostic)
		for _, d := range tstlDiags {
			if d.File() != nil {
				rel, _ := filepath.Rel(tmpDir, d.File().FileName())
				diagsByFile[rel] = append(diagsByFile[rel], d)
			}
		}
		for _, i := range batchIndices {
			entry := fmt.Sprintf("test_%d/main.ts", i)
			results[i] = testResult{
				diags: diagsByFile[entry],
				lua:   luaByFile[entry],
			}
		}
	}

	// Solo compilation: individual Program per codegen case.
	// Use a separate root dir so batch tsconfig's `**/*.ts` doesn't pick these up.
	soloDir := t.TempDir()
	for _, i := range soloIndices {
		c := cases[i]
		var tsCode string
		switch c.mode {
		case "expression":
			tsCode = "export const __result = " + c.tsCode + ";\n"
		case "function":
			tsCode = "export function __main() {" + c.tsCode + "}\n"
		default:
			tsCode = c.tsCode
		}
		dir := filepath.Join(soloDir, fmt.Sprintf("solo_%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(tsCode), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfigJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		configPath := tspath.ResolvePath(dir, "tsconfig.json")
		configDir := tspath.GetDirectoryPath(configPath)
		host := &cachingHost{compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)}

		configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
		if len(diags) > 0 {
			t.Fatalf("solo_%d tsconfig parse errors: %d diagnostic(s)", i, len(diags))
		}

		program := compiler.NewProgram(compiler.ProgramOptions{
			Config:         configResult,
			SingleThreaded: core.TSTrue,
			Host:           host,
		})
		program.BindSourceFiles()

		transpileResults, tstlDiags := transpiler.TranspileProgramWithOptions(program, dir, luaTarget, nil, transpileOpts(o))

		var r testResult
		r.diags = tstlDiags
		mainPath := filepath.Join(dir, "main.ts")
		for _, tr := range transpileResults {
			if tr.FileName == mainPath {
				r.lua = tr.Lua
			}
		}
		results[i] = r
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			trackResult(t)

			r := results[i]

			gotCodes := make(map[int32]bool)
			for _, d := range r.diags {
				gotCodes[d.Code()] = true
			}

			for _, want := range c.wantCodes {
				if !gotCodes[want] {
					var gotList []int32
					for _, d := range r.diags {
						gotList = append(gotList, d.Code())
					}
					t.Errorf("expected diagnostic code %d, got codes %v", want, gotList)
					return
				}
			}

			// Check codegen if expected
			if len(c.wantCodegen) > 0 {
				for _, want := range c.wantCodegen {
					if !strings.Contains(r.lua, want) {
						t.Errorf("expected Lua output to contain %q\ngot:\n%s", want, r.lua)
						return
					}
				}
			}
		})
	}
}

// batchExpectCodegen batches codegen assertion tests in a single Program.
// It compiles TS to Lua and checks the output contains/doesn't contain expected strings.
func batchExpectCodegen(t *testing.T, cases []codegenTestCase, opts ...TestOpt) {
	t.Helper()

	o := buildOpts(opts)
	tmpDir := t.TempDir()

	for i, c := range cases {
		var tsCode string
		switch c.mode {
		case "expression":
			tsCode = "export const __result = " + c.tsCode + ";\n"
		case "function":
			tsCode = "export function __main() {" + c.tsCode + "}\n"
		default:
			tsCode = c.tsCode
		}
		dir := filepath.Join(tmpDir, fmt.Sprintf("test_%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(tsCode), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(buildTsconfig(o)), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := &cachingHost{compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)}

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

	luaTarget := o.luaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}
	results, _ := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, transpileOpts(o))

	luaByTest := make(map[int]string)
	for _, r := range results {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		var idx int
		if _, err := fmt.Sscanf(rel, "test_%d/main.ts", &idx); err == nil {
			luaByTest[idx] = r.Lua
		}
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			trackResult(t)

			lua, ok := luaByTest[i]
			if !ok {
				t.Fatal("no transpile result")
			}

			for _, s := range c.contains {
				if !strings.Contains(lua, s) {
					t.Errorf("expected Lua output to contain %q\ngot:\n%s", s, lua)
				}
			}
			for _, s := range c.notContains {
				if strings.Contains(lua, s) {
					t.Errorf("expected Lua output NOT to contain %q\ngot:\n%s", s, lua)
				}
			}
			for _, pattern := range c.matches {
				re := regexp.MustCompile(pattern)
				if !re.MatchString(lua) {
					t.Errorf("expected Lua output to match /%s/\ngot:\n%s", pattern, lua)
				}
			}
			for _, pattern := range c.notMatches {
				re := regexp.MustCompile(pattern)
				if re.MatchString(lua) {
					t.Errorf("expected Lua output NOT to match /%s/\ngot:\n%s", pattern, lua)
				}
			}
		})
	}
}

// tstlHeader is the comment TSTL prepends to all output.
const tstlHeader = "--[[ Generated with https://github.com/TypeScriptToLua/TypeScriptToLua ]]\n"

func unifiedDiff(a, b, labelA, labelB, errMsg string) string {
	return luatest.UnifiedDiff(a, b, labelA, labelB, errMsg)
}

// batchCompareCodegen compiles test cases with tslua and compares the Lua output
// byte-for-byte against the embedded TSTL reference Lua (refLua). Skips cases
// with empty refLua. No Lua execution needed.
func batchCompareCodegen(t *testing.T, cases []batchTestCase, opts ...TestOpt) {
	t.Helper()

	// Filter to cases with refLua
	var filtered []batchTestCase
	for _, c := range cases {
		if c.refLua != "" {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return
	}

	o := buildOpts(opts)
	tmpDir := t.TempDir()

	for i, c := range filtered {
		dir := filepath.Join(tmpDir, fmt.Sprintf("test_%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(c.tsCode), 0o644); err != nil {
			t.Fatal(err)
		}
		for name, code := range c.extraFiles {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(buildTsconfig(o)), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := &cachingHost{compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)}

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

	luaTarget := o.luaTarget
	if luaTarget == "" {
		luaTarget = transpiler.LuaTargetLua55
	}
	rawResults, _ := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, transpileOpts(o))

	luaByTest := make(map[int]string)
	for _, r := range rawResults {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		var idx int
		if _, err := fmt.Sscanf(rel, "test_%d/main.ts", &idx); err == nil {
			luaByTest[idx] = r.Lua
		}
	}

	for i, c := range filtered {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			trackResult(t)

			if c.tstlBug != "" {
				t.Skipf("TSTL bug: %s", c.tstlBug)
			}

			got := strings.TrimSpace(luaByTest[i])
			want := strings.TrimSpace(strings.TrimPrefix(c.refLua, tstlHeader))

			if got != want {
				t.Errorf("codegen mismatch:\n%s", unifiedDiff(want, got, "tstl", "tslua", ""))
			}
		})
	}
}
