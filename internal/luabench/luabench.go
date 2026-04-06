// Package luabench measures runtime and memory performance of transpiled Lua code.
// Benchmarks are TypeScript files in benchmarks/ that export a default function.
// The harness transpiles them, wraps with os.clock/collectgarbage measurement,
// and runs via LuaJIT (or another runtime).
package luabench

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

//go:embed benchmarks/*.ts
var benchmarkFS embed.FS

// Bench defines a benchmark to run.
type Bench struct {
	Name       string
	Iterations int // how many times the function is called for timing
}

// Result holds measurements from a single benchmark run.
type Result struct {
	Name       string
	EmitMode   transpiler.EmitMode
	TimeSec    float64
	Iterations int
	GarbageKB  float64
	TotalMemKB float64
	Lua        string // transpiled Lua source (main.ts only)
}

// LuaTargetForRuntime maps a runtime binary name to a transpiler LuaTarget.
func LuaTargetForRuntime(runtime string) transpiler.LuaTarget {
	switch runtime {
	case "luajit":
		return transpiler.LuaTargetLuaJIT
	case "lua5.1", "lua-5.1":
		return transpiler.LuaTargetLua51
	case "lua5.2", "lua-5.2":
		return transpiler.LuaTargetLua52
	case "lua5.3", "lua-5.3":
		return transpiler.LuaTargetLua53
	case "lua5.4", "lua-5.4":
		return transpiler.LuaTargetLua54
	case "lua5.0", "lua-5.0":
		return transpiler.LuaTargetLua50
	default:
		return transpiler.LuaTargetLuaJIT
	}
}

type transpileResult struct {
	fileName   string
	lua        string
	usesLualib bool
}

// transpileTS compiles TypeScript source to Lua using the full compiler pipeline.
func transpileTS(tsCode string, luaTarget transpiler.LuaTarget, emitMode transpiler.EmitMode) ([]transpileResult, error) {
	tmpDir, err := os.MkdirTemp("", "luabench-transpile-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tsconfig := `{"compilerOptions":{"strict":true,"target":"ESNext","moduleResolution":"bundler","outDir":"./out"},"include":["*.ts"]}`
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.ts"), []byte(tsCode), 0o644); err != nil {
		return nil, err
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		return nil, fmt.Errorf("tsconfig parse errors: %d diagnostic(s)", len(diags))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	rawResults, _ := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, transpiler.TranspileOptions{
		EmitMode: emitMode,
	})

	var results []transpileResult
	for _, r := range rawResults {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		results = append(results, transpileResult{
			fileName:   rel,
			lua:        r.Lua,
			usesLualib: r.UsesLualib,
		})
	}
	return results, nil
}

// Run transpiles a benchmark .ts file and measures its Lua performance.
func Run(b Bench, mode transpiler.EmitMode, runtime string) (Result, error) {
	tsCode, err := benchmarkFS.ReadFile("benchmarks/" + b.Name + ".ts")
	if err != nil {
		return Result{}, fmt.Errorf("read benchmark %s: %w", b.Name, err)
	}

	results, err := transpileTS(string(tsCode), LuaTargetForRuntime(runtime), mode)
	if err != nil {
		return Result{}, fmt.Errorf("transpile %s: %w", b.Name, err)
	}

	// Write transpiled Lua to temp dir
	dir, err := os.MkdirTemp("", "luabench-run-*")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	var mainLua string
	for _, r := range results {
		luaName := strings.TrimSuffix(r.fileName, ".ts") + ".lua"
		outPath := filepath.Join(dir, luaName)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(outPath, []byte(r.lua), 0o644); err != nil {
			return Result{}, err
		}
		if strings.HasSuffix(r.fileName, "main.ts") {
			mainLua = r.lua
		}
		if r.usesLualib {
			if err := os.WriteFile(
				filepath.Join(dir, "lualib_bundle.lua"),
				lualib.BundleForTarget(string(LuaTargetForRuntime(runtime))),
				0o644,
			); err != nil {
				return Result{}, err
			}
		}
	}

	// Write measurement runner
	runner := fmt.Sprintf(`package.path = %q
local mod = require("main")
local fn = mod.default
local N = %d

-- Warmup
fn()

-- Runtime
collectgarbage("collect")
local t0 = os.clock()
for i = 1, N do
    fn()
end
local elapsed = os.clock() - t0

-- Memory (single iteration, GC stopped)
collectgarbage("collect")
collectgarbage("stop")
local pre = collectgarbage("count")
local result = fn()
local post = collectgarbage("count")
collectgarbage("restart")
collectgarbage("collect")
local postGC = collectgarbage("count")

-- prevent result from being collected before measurement
local _ = result

io.write(string.format("TIME:%%.9f\n", elapsed))
io.write(string.format("GARBAGE:%%.4f\n", post - postGC))
io.write(string.format("TOTALMEM:%%.4f\n", post - pre))
io.write(string.format("ITERS:%%d\n", N))
`, filepath.Join(dir, "?.lua"), b.Iterations)

	runnerPath := filepath.Join(dir, "bench_runner.lua")
	if err := os.WriteFile(runnerPath, []byte(runner), 0o644); err != nil {
		return Result{}, err
	}

	cmd := exec.Command(runtime, runnerPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("benchmark %s (%s) failed: %v\n%s", b.Name, mode, err, out)
	}

	res := parseOutput(b.Name, mode, string(out))
	res.Lua = mainLua
	return res, nil
}

func parseOutput(name string, mode transpiler.EmitMode, output string) Result {
	r := Result{Name: name, EmitMode: mode}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		switch parts[0] {
		case "TIME":
			r.TimeSec, _ = strconv.ParseFloat(val, 64)
		case "GARBAGE":
			r.GarbageKB, _ = strconv.ParseFloat(val, 64)
		case "TOTALMEM":
			r.TotalMemKB, _ = strconv.ParseFloat(val, 64)
		case "ITERS":
			r.Iterations, _ = strconv.Atoi(val)
		}
	}
	return r
}

// PrintTable prints benchmark results as an aligned table with deltas.
// Results are expected in pairs: [tstl, optimized] for each benchmark.
func PrintTable(results []Result) {
	fmt.Printf("\n%-20s %-10s %12s %14s %16s\n",
		"Name", "Mode", "Time (ms)", "Garbage (KB)", "Total Mem (KB)")
	fmt.Println(strings.Repeat("─", 76))

	for i, r := range results {
		timeMs := r.TimeSec * 1000 / float64(r.Iterations)
		fmt.Printf("%-20s %-10s %12.3f %14.2f %16.2f\n",
			r.Name, r.EmitMode, timeMs, r.GarbageKB, r.TotalMemKB)

		// After each pair, print delta line
		if i%2 == 1 {
			base := results[i-1]
			baseTimeMs := base.TimeSec * 1000 / float64(base.Iterations)
			fmt.Printf("%-20s %-10s %s %s %s\n",
				"", "Δ",
				padRight(fmtDelta(timeMs, baseTimeMs), 12),
				padRight(fmtDelta(r.GarbageKB, base.GarbageKB), 14),
				padRight(fmtDelta(r.TotalMemKB, base.TotalMemKB), 16),
			)
			if i < len(results)-1 {
				fmt.Println()
			}
		}
	}
	fmt.Println()
}

// padRight pads a string (which may contain ANSI codes) to the given visible width.
func padRight(s string, width int) string {
	// Count visible length by stripping ANSI escape sequences.
	visible := 0
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
		} else if inEsc && r == 'm' {
			inEsc = false
		} else if !inEsc {
			visible++
		}
	}
	if visible < width {
		s = strings.Repeat(" ", width-visible) + s
	}
	return s
}

// fmtDelta formats the relative change from base to val with color.
// Green = val is smaller (better), red = val is larger (worse).
func fmtDelta(val, base float64) string {
	if base == 0 {
		if val == 0 {
			return "="
		}
		return "\033[31m+∞\033[0m"
	}
	pct := (val - base) / base * 100
	if pct > 0 {
		return fmt.Sprintf("\033[31m+%.1f%%\033[0m", pct)
	}
	if pct < 0 {
		return fmt.Sprintf("\033[32m%.1f%%\033[0m", pct)
	}
	return "="
}

// PrintLua prints the transpiled Lua for each benchmark, grouped by name
// with tstl and optimized side by side.
func PrintLua(results []Result) {
	// Group results by benchmark name, preserving order.
	type pair struct {
		name   string
		byMode map[transpiler.EmitMode]string
	}
	var pairs []pair
	seen := map[string]int{}
	for _, r := range results {
		if idx, ok := seen[r.Name]; ok {
			pairs[idx].byMode[r.EmitMode] = r.Lua
		} else {
			seen[r.Name] = len(pairs)
			pairs = append(pairs, pair{
				name:   r.Name,
				byMode: map[transpiler.EmitMode]string{r.EmitMode: r.Lua},
			})
		}
	}

	for _, p := range pairs {
		tstlLua := p.byMode[transpiler.EmitModeTSTL]
		optLua := p.byMode[transpiler.EmitModeOptimized]

		if tstlLua == optLua {
			fmt.Printf("── %s (identical) ──\n", p.name)
			printNumbered(tstlLua)
		} else {
			fmt.Printf("── %s [tstl] ──\n", p.name)
			printNumbered(tstlLua)
			fmt.Printf("── %s [optimized] ──\n", p.name)
			printNumbered(optLua)
		}
		fmt.Println()
	}
}

func printNumbered(lua string) {
	lines := strings.Split(strings.TrimRight(lua, "\n"), "\n")
	width := len(strconv.Itoa(len(lines)))
	for i, line := range lines {
		fmt.Printf("  %*d│ %s\n", width, i+1, line)
	}
}
