package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
	"github.com/realcoldfry/tslua/internal/emitpath"
	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/resolve"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

type serverRequest struct {
	// File-based mode: point to a tsconfig on disk
	Project string `json:"project,omitempty"`
	Outdir  string `json:"outdir,omitempty"`
	// Inline mode: send source directly, get Lua back in response
	Source          string            `json:"source,omitempty"`
	MainFileName    string            `json:"mainFileName,omitempty"` // override main file name (e.g. "main.tsx" for JSX)
	ExtraFiles      map[string]string `json:"extraFiles,omitempty"`
	LuaTarget       string            `json:"luaTarget"`
	Types           []string          `json:"types,omitempty"`
	CompilerOptions map[string]any    `json:"compilerOptions,omitempty"`
	LuaBundle       string            `json:"luaBundle,omitempty"`
	LuaBundleEntry  string            `json:"luaBundleEntry,omitempty"`
}

type serverResponse struct {
	OK          bool               `json:"ok"`
	Error       string             `json:"error,omitempty"`
	Files       map[string]string  `json:"files,omitempty"`
	SourceMaps  map[string]string  `json:"sourceMaps,omitempty"`
	Diagnostics []serverDiagnostic `json:"diagnostics,omitempty"`
}

type serverDiagnostic struct {
	Code      int32  `json:"code"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Character int    `json:"character,omitempty"`
}

func convertDiagnostics(diags []*ast.Diagnostic, baseDir string) []serverDiagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]serverDiagnostic, len(diags))
	for i, d := range diags {
		code, msg := dw.DiagnosticInfo(d)
		sd := serverDiagnostic{Code: code, Category: dw.DiagnosticCategory(d), Message: msg}
		if file, line, character, ok := dw.DiagnosticLocation(d); ok {
			if rel, err := filepath.Rel(baseDir, file); err == nil {
				file = rel
			}
			sd.File = file
			sd.Line = line
			sd.Character = character
		}
		out[i] = sd
	}
	return out
}

// requestCh serializes requests through a single worker goroutine.
// If the worker hangs, subsequent requests time out but the server stays alive.
type serverJob struct {
	req    serverRequest
	respCh chan serverResponse
}

var requestCh chan serverJob

func initRequestWorker() {
	requestCh = make(chan serverJob, 100)
	go func() {
		for job := range requestCh {
			resp := func() (r serverResponse) {
				defer func() {
					if p := recover(); p != nil {
						fmt.Fprintf(os.Stderr, "tslua server panic: %v\n", p)
						// Clear cached program but keep projectDir — the directory still exists
						inlineOldProgram = nil
						inlineCachedConfig = ""
						r = serverResponse{Error: fmt.Sprintf("server panic: %v", p)}
					}
				}()
				return handleServerRequest(job.req)
			}()
			job.respCh <- resp
		}
	}()
}

func runServer() error {
	serverDebugTiming = os.Getenv("TSLUA_TIMING") != ""
	initRequestWorker()

	if socketFlag != "" {
		return runSocketServer(socketFlag)
	}

	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	_, _ = fmt.Fprintln(os.Stdout, `{"ready":true}`)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req serverRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(serverResponse{Error: fmt.Sprintf("invalid request: %v", err)})
			continue
		}

		job := serverJob{req: req, respCh: make(chan serverResponse, 1)}
		requestCh <- job
		resp := <-job.respCh
		_ = enc.Encode(resp)
	}
	return scanner.Err()
}

func runSocketServer(socketPath string) error {
	// Clean up stale socket
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()            //nolint:errcheck
	defer os.Remove(socketPath) //nolint:errcheck

	// Clean up on signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		_ = ln.Close()
		_ = os.Remove(socketPath)
		os.Exit(0)
	}()

	// Do a cold-start transpile to warm the cache before signaling ready.
	// Don't cache the warmup program — the first real request will set the right compilerOptions.
	warmupReq := serverRequest{Source: "const __warmup = 0;", LuaTarget: "5.4"}
	handleServerRequest(warmupReq)
	inlineOldProgram = nil

	// Signal ready by writing to stdout
	_, _ = fmt.Fprintln(os.Stdout, `{"ready":true}`)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil // listener closed
		}
		go handleSocketConn(conn)
	}
}

func handleSocketConn(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	// Set a read/write deadline so a stuck request doesn't block forever
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	body, err := io.ReadAll(conn)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(serverResponse{Error: fmt.Sprintf("read: %v", err)})
		return
	}

	var req serverRequest
	if err := json.Unmarshal(body, &req); err != nil {
		_ = json.NewEncoder(conn).Encode(serverResponse{Error: fmt.Sprintf("invalid request: %v", err)})
		return
	}

	job := serverJob{req: req, respCh: make(chan serverResponse, 1)}
	requestCh <- job

	select {
	case resp := <-job.respCh:
		_ = json.NewEncoder(conn).Encode(resp)
	case <-time.After(9 * time.Second):
		_ = json.NewEncoder(conn).Encode(serverResponse{Error: "request timeout (9s)"})
	}
}

func handleServerRequest(req serverRequest) serverResponse {
	luaTarget := transpiler.LuaTarget(req.LuaTarget)
	if !transpiler.ValidTarget(req.LuaTarget) {
		return serverResponse{Error: fmt.Sprintf("unsupported luaTarget: %s", req.LuaTarget)}
	}

	if req.Project != "" {
		return handleProjectRequest(req, luaTarget)
	}
	return handleInlineRequest(req, luaTarget)
}

// Cached state for inline requests — reuses Program across requests via UpdateProgram.
var (
	inlineProjectDir   string
	inlineOldProgram   *compiler.Program
	inlineSourceRoot   string
	inlineMainPath     tspath.Path // tspath.Path for main.ts
	inlineCachedConfig string      // serialized compilerOptions+types used to create cached program
)

func getInlineProjectDir() (string, error) {
	if inlineProjectDir != "" {
		return inlineProjectDir, nil
	}
	dir, err := os.MkdirTemp("", "tslua-srv-")
	if err != nil {
		return "", err
	}
	inlineProjectDir = dir
	return dir, nil
}

func handleInlineRequest(req serverRequest, luaTarget transpiler.LuaTarget) serverResponse {
	t0 := time.Now()

	projectDir, err := getInlineProjectDir()
	if err != nil {
		return serverResponse{Error: fmt.Sprintf("project dir: %v", err)}
	}

	// Only supports single-file mode for UpdateProgram optimization.
	// Multi-file requests (extraFiles) fall back to full program creation.
	hasExtraFiles := len(req.ExtraFiles) > 0

	mainFile := "main.ts"
	if req.MainFileName != "" {
		mainFile = req.MainFileName
		// Strip leading slash from absolute paths so they become relative
		// within the temp project directory (e.g. "/proj/src/foo.ts" → "proj/src/foo.ts").
		if filepath.IsAbs(mainFile) {
			mainFile = strings.TrimPrefix(filepath.Clean(mainFile), string(filepath.Separator))
		}
	}
	mainPath := filepath.Join(projectDir, mainFile)
	if !isInsideDir(mainPath, projectDir) {
		return serverResponse{Error: fmt.Sprintf("mainFileName escapes project directory: %s", req.MainFileName)}
	}
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		return serverResponse{Error: fmt.Sprintf("mkdir source: %v", err)}
	}
	if err := os.WriteFile(mainPath, []byte(req.Source), 0o644); err != nil {
		return serverResponse{Error: fmt.Sprintf("write source: %v", err)}
	}

	for name, content := range req.ExtraFiles {
		// Extra-file paths are resolved relative to the project directory,
		// matching TSTL's virtual project semantics where addExtraFile paths
		// correspond to tsconfig "files" entries.
		// Absolute paths are mapped into the project directory by stripping
		// the leading slash (same as mainFileName handling).
		var fpath string
		if filepath.IsAbs(name) {
			fpath = filepath.Join(projectDir, strings.TrimPrefix(filepath.Clean(name), string(filepath.Separator)))
		} else {
			fpath = filepath.Clean(filepath.Join(projectDir, name))
		}
		if !isInsideDir(fpath, projectDir) {
			return serverResponse{Error: fmt.Sprintf("extraFiles path escapes project directory: %s", name)}
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
			return serverResponse{Error: fmt.Sprintf("mkdir: %v", err)}
		}
		if err := os.WriteFile(fpath, []byte(content), 0o644); err != nil {
			return serverResponse{Error: fmt.Sprintf("write extra: %v", err)}
		}
	}

	var program *compiler.Program
	sourceRoot := projectDir

	configKey, _ := json.Marshal([]any{req.CompilerOptions, req.Types, mainFile})
	configChanged := string(configKey) != inlineCachedConfig
	if inlineOldProgram != nil && !hasExtraFiles && !configChanged {
		// Fast path: reuse old program, only re-parse main.ts
		t1 := time.Now()

		fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
		configDir := tspath.GetDirectoryPath(tspath.ResolvePath("", filepath.Join(projectDir, "tsconfig.json")))
		newHost := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

		updatedProgram, reused := compiler.Program_UpdateProgram(inlineOldProgram, inlineMainPath, newHost)

		t2 := time.Now()

		updatedProgram.BindSourceFiles()

		t3 := time.Now()

		if serverDebugTiming {
			fmt.Fprintf(os.Stderr, "  [update] reused=%v update=%.1fms bind=%.1fms total=%.1fms\n",
				reused, ms(t2.Sub(t1)), ms(t3.Sub(t2)), ms(t3.Sub(t0)))
		}

		program = updatedProgram
		sourceRoot = inlineSourceRoot
	} else {
		// Cold path: create full program (first request or multi-file)
		files := []string{mainFile}
		for name := range req.ExtraFiles {
			files = append(files, name)
		}
		compilerOpts := map[string]any{}
		for k, v := range req.CompilerOptions {
			compilerOpts[k] = v
		}
		// Save original outDir/rootDir before rewriting for tsconfig.
		// inlineLuaOutputKey needs the originals to compute correct output paths.
		origOutDir, _ := compilerOpts["outDir"].(string)
		origRootDir, _ := compilerOpts["rootDir"].(string)
		origExtension, _ := compilerOpts["extension"].(string)
		// Strip leading slash from absolute paths so they become relative
		// within the temp project directory.
		for _, key := range []string{"outDir", "rootDir"} {
			if s, ok := compilerOpts[key].(string); ok && filepath.IsAbs(s) {
				compilerOpts[key] = strings.TrimPrefix(filepath.Clean(s), string(filepath.Separator))
			}
		}
		if len(req.Types) > 0 {
			compilerOpts["types"] = req.Types
		}
		// When configFilePath is provided (e.g. "/virtual/tsconfig.json"),
		// place the tsconfig at the equivalent mapped location so that
		// relative paths in "paths" resolve correctly.
		tsconfigDir := projectDir
		if cfp, ok := compilerOpts["configFilePath"].(string); ok && cfp != "" {
			delete(compilerOpts, "configFilePath")
			if filepath.IsAbs(cfp) {
				cfp = strings.TrimPrefix(filepath.Clean(cfp), string(filepath.Separator))
			}
			tsconfigDir = filepath.Dir(filepath.Join(projectDir, cfp))
		}
		// Make file paths relative to tsconfig directory
		relFiles := make([]string, len(files))
		for i, f := range files {
			abs := filepath.Join(projectDir, f)
			rel, err := filepath.Rel(tsconfigDir, abs)
			if err != nil {
				rel = f
			}
			relFiles[i] = filepath.ToSlash(rel)
		}
		// Also make rootDir/outDir relative to tsconfig directory.
		// Note: these values have already been slash-stripped above, so they
		// are always relative here. The IsAbs guard is defensive.
		for _, key := range []string{"outDir", "rootDir"} {
			if s, ok := compilerOpts[key].(string); ok && s != "" {
				abs := s
				if !filepath.IsAbs(s) {
					abs = filepath.Join(projectDir, s)
				}
				rel, err := filepath.Rel(tsconfigDir, abs)
				if err == nil {
					compilerOpts[key] = filepath.ToSlash(rel)
				}
			}
		}
		tsconfigObj := map[string]any{
			"compilerOptions": compilerOpts,
			"files":           relFiles,
		}
		tsconfigBytes, _ := json.Marshal(tsconfigObj)
		tsconfig := string(tsconfigBytes)
		tsconfigPath := filepath.Join(tsconfigDir, "tsconfig.json")
		if err := os.MkdirAll(tsconfigDir, 0o755); err != nil {
			return serverResponse{Error: fmt.Sprintf("mkdir tsconfig: %v", err)}
		}
		if err := os.WriteFile(tsconfigPath, []byte(tsconfig), 0o644); err != nil {
			return serverResponse{Error: fmt.Sprintf("write tsconfig: %v", err)}
		}

		coldCfg := buildConfigFromRequest(req, "", luaTarget)
		coldProgram, results, coldDiags, err := transpileProjectReturnProgram(tsconfigPath, luaTarget, coldCfg.transpileOpts())
		if err != nil {
			return serverResponse{Error: err.Error()}
		}

		// Cache for subsequent UpdateProgram calls (only for single-file requests)
		if !hasExtraFiles {
			inlineOldProgram = coldProgram
			inlineSourceRoot = projectDir
			inlineCachedConfig = string(configKey)
			// Compute the tspath.Path for main.ts
			mainAbsPath := filepath.Join(projectDir, mainFile)
			inlineMainPath = tspath.Path(tspath.ToPath(mainAbsPath, "", false))
		}

		luaFiles := make(map[string]string, len(results))
		sourceMaps := make(map[string]string, len(results))
		for _, r := range results {
			key := inlineLuaOutputKey(r.FileName, projectDir, origOutDir, origRootDir, origExtension)
			luaFiles[key] = r.Lua
			if r.SourceMap != "" {
				sourceMaps[key] = r.SourceMap
			}
		}
		addMinimalLualib(luaFiles, results, coldCfg.transpileOpts(), luaTarget)
		if req.LuaBundle != "" {
			bundledFiles, bundleDiags := bundleInlineResults(req, luaFiles, results, projectDir, origOutDir, luaTarget, coldCfg.transpileOpts())
			coldDiags = append(coldDiags, bundleDiags...)
			if bundledFiles != nil {
				luaFiles = bundledFiles
				sourceMaps = nil
			}
		}
		return serverResponse{OK: true, Files: luaFiles, SourceMaps: sourceMaps, Diagnostics: convertDiagnostics(coldDiags, projectDir)}
	}

	// Pre-emit diagnostics (syntactic + semantic), matching ts.getPreEmitDiagnostics
	syntacticDiags := compiler.Program_GetSyntacticDiagnostics(program, context.Background(), nil)
	semanticDiags := compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil)
	preEmitDiags := compiler.SortAndDeduplicateDiagnostics(append(syntacticDiags, semanticDiags...))

	// Transpile using the updated program
	cfg := buildConfigFromRequest(req, sourceRoot, luaTarget)
	results, diags := cfg.transpile(program, nil)

	// Cache program for next request
	inlineOldProgram = program
	inlineSourceRoot = sourceRoot

	allDiags := compiler.SortAndDeduplicateDiagnostics(append(preEmitDiags, diags...))
	luaFiles := make(map[string]string, len(results))
	sourceMaps := make(map[string]string, len(results))
	for _, r := range results {
		reqOutDir, _ := req.CompilerOptions["outDir"].(string)
		reqRootDir, _ := req.CompilerOptions["rootDir"].(string)
		reqExtension, _ := req.CompilerOptions["extension"].(string)
		key := inlineLuaOutputKey(r.FileName, projectDir, reqOutDir, reqRootDir, reqExtension)
		luaFiles[key] = r.Lua
		if r.SourceMap != "" {
			sourceMaps[key] = r.SourceMap
		}
	}
	addMinimalLualib(luaFiles, results, cfg.transpileOpts(), luaTarget)
	if req.LuaBundle != "" {
		hotOutDir, _ := req.CompilerOptions["outDir"].(string)
		bundledFiles, bundleDiags := bundleInlineResults(req, luaFiles, results, projectDir, hotOutDir, luaTarget, cfg.transpileOpts())
		allDiags = append(allDiags, bundleDiags...)
		if bundledFiles != nil {
			luaFiles = bundledFiles
			sourceMaps = nil
		}
	}
	return serverResponse{OK: true, Files: luaFiles, SourceMaps: sourceMaps, Diagnostics: convertDiagnostics(allDiags, projectDir)}
}

func handleProjectRequest(req serverRequest, luaTarget transpiler.LuaTarget) serverResponse {
	configPath := tspath.ResolvePath("", req.Project)
	configDir := string(tspath.GetDirectoryPath(configPath))

	cfg := buildConfigFromRequest(req, configDir, luaTarget)
	if req.Outdir != "" {
		cfg.outdir = req.Outdir
	}

	// Create program from tsconfig.
	program, results, projectDiags, err := transpileProjectReturnProgram(string(configPath), luaTarget, cfg.transpileOpts())
	if err != nil {
		return serverResponse{Error: err.Error()}
	}
	_ = program // not cached for project mode

	// Resolve external dependencies and write output.
	resolved := resolve.ResolveDependencies(results, resolve.Options{
		SourceRoot: cfg.sourceRoot,
		BuildMode:  resolve.BuildMode(cfg.buildMode),
	})

	needsLualib := false
	for _, r := range results {
		if r.UsesLualib {
			needsLualib = true
			break
		}
	}
	for _, f := range resolved.Files {
		outPath := emitpath.OutputPath(f.FileName, cfg.sourceRoot, cfg.outdir, "")
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return serverResponse{Error: fmt.Sprintf("mkdir: %v", err)}
		}
		if err := os.WriteFile(outPath, []byte(f.Lua), 0o644); err != nil {
			return serverResponse{Error: fmt.Sprintf("write: %v", err)}
		}
	}
	if needsLualib {
		bundlePath := filepath.Join(cfg.outdir, "lualib_bundle.lua")
		if err := os.WriteFile(bundlePath, lualib.BundleForTarget(string(luaTarget)), 0o644); err != nil {
			return serverResponse{Error: fmt.Sprintf("write lualib: %v", err)}
		}
	}

	return serverResponse{OK: true, Diagnostics: convertDiagnostics(projectDiags, configDir)}
}

var serverDebugTiming bool

// cachedLibFS is reused across inline requests so TypeScript lib .d.ts files
// don't get re-read from disk on every request. For project-based requests
// we create a fresh FS since the project files may be in different locations.
var cachedLibFS vfs.FS

func getCachedFS() vfs.FS {
	if cachedLibFS == nil {
		cachedLibFS = bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	}
	return cachedLibFS
}

func transpileProjectReturnProgram(tsconfigPath string, luaTarget transpiler.LuaTarget, opts ...transpiler.TranspileOptions) (*compiler.Program, []transpiler.TranspileResult, []*ast.Diagnostic, error) {
	program, results, diags, err := transpileProjectInner(tsconfigPath, luaTarget, false, opts...)
	return program, results, diags, err
}

func transpileProjectInner(tsconfigPath string, luaTarget transpiler.LuaTarget, useCache bool, extraOpts ...transpiler.TranspileOptions) (*compiler.Program, []transpiler.TranspileResult, []*ast.Diagnostic, error) {
	t0 := time.Now()

	var fs vfs.FS
	if useCache {
		fs = getCachedFS()
	} else {
		fs = bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	}

	t1 := time.Now()

	configPath := tspath.ResolvePath("", tsconfigPath)
	if !fs.FileExists(configPath) {
		return nil, nil, nil, fmt.Errorf("tsconfig not found: %s", configPath)
	}

	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	t2 := time.Now()

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		return nil, nil, nil, fmt.Errorf("tsconfig parse error: %d diagnostic(s)", len(diags))
	}

	t3 := time.Now()

	sourceRoot := string(configDir)
	if configResult.CompilerOptions().RootDir != "" {
		sourceRoot = tspath.ResolvePath(string(configDir), string(configResult.CompilerOptions().RootDir))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})

	t4 := time.Now()

	program.BindSourceFiles()

	t5 := time.Now()

	syntacticDiags := compiler.Program_GetSyntacticDiagnostics(program, context.Background(), nil)
	semanticDiags := compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil)
	preEmitDiags := compiler.SortAndDeduplicateDiagnostics(append(syntacticDiags, semanticDiags...))

	t5b := time.Now()

	var opts transpiler.TranspileOptions
	if len(extraOpts) > 0 {
		opts = extraOpts[0]
	}
	results, tsDiags := transpiler.TranspileProgramWithOptions(program, sourceRoot, luaTarget, nil, opts)

	t6 := time.Now()

	if serverDebugTiming {
		fmt.Fprintf(os.Stderr, "  fs=%.1fms host=%.1fms config=%.1fms program=%.1fms bind=%.1fms check=%.1fms transpile=%.1fms total=%.1fms\n",
			ms(t1.Sub(t0)), ms(t2.Sub(t1)), ms(t3.Sub(t2)), ms(t4.Sub(t3)), ms(t5.Sub(t4)), ms(t5b.Sub(t5)), ms(t6.Sub(t5b)), ms(t6.Sub(t0)))
	}

	allDiags := append(preEmitDiags, tsDiags...)
	return program, results, allDiags, nil
}

// bundleInlineResults bundles per-file transpile results into a single file when
// luaBundle/luaBundleEntry are set. Returns the modified files map and any diagnostics.
// If bundling is not requested, returns the files map unchanged.
func bundleInlineResults(req serverRequest, luaFiles map[string]string, results []transpiler.TranspileResult, projectDir string, origOutDir string, luaTarget transpiler.LuaTarget, opts transpiler.TranspileOptions) (map[string]string, []*ast.Diagnostic) {
	if req.LuaBundle == "" {
		return luaFiles, nil
	}

	var diags []*ast.Diagnostic

	// Validation: luaBundleEntry is required
	if req.LuaBundleEntry == "" {
		diags = append(diags, dw.NewConfigError(dw.LuaBundleEntryIsRequired,
			"'luaBundleEntry' is required when 'luaBundle' is enabled."))
		return nil, diags
	}

	// Validation: cannot bundle library mode
	if bm, ok := req.CompilerOptions["buildMode"].(string); ok && bm == "library" {
		diags = append(diags, dw.NewConfigError(dw.CannotBundleLibrary,
			`Cannot bundle projects with "buildmode": "library". Projects including the library can still bundle (which will include external library files).`))
		return nil, diags
	}

	// Validation: inline lualib warning
	if opts.LuaLibImport == transpiler.LuaLibImportInline {
		diags = append(diags, dw.NewConfigWarning(dw.UsingLuaBundleWithInlineMightDuplicate,
			`Using 'luaBundle' with 'luaLibImport: "inline"' might generate duplicate code. It is recommended to use 'luaLibImport: "require"'.`))
	}

	// Compute entry module name
	entryFile := req.LuaBundleEntry
	if !filepath.IsAbs(entryFile) {
		entryFile = filepath.Join(projectDir, entryFile)
	}

	// Check if entry point exists in results
	entryModule := transpiler.ModuleNameFromPath(entryFile, projectDir)
	found := false
	for _, r := range results {
		if transpiler.ModuleNameFromPath(r.FileName, projectDir) == entryModule {
			found = true
			break
		}
	}
	if !found {
		diags = append(diags, dw.NewConfigError(dw.CouldNotFindBundleEntryPoint,
			fmt.Sprintf("Could not find bundle entry point '%s'. It should be a file in the project.", req.LuaBundleEntry)))
		return nil, diags
	}

	// Build lualib content for bundling
	var lualibContent []byte
	switch opts.LuaLibImport {
	case transpiler.LuaLibImportRequire:
		for _, r := range results {
			if r.UsesLualib {
				lualibContent = lualib.BundleForTarget(string(luaTarget))
				break
			}
		}
	case transpiler.LuaLibImportRequireMinimal:
		usedExports := aggregateLualibExports(results)
		if len(usedExports) > 0 {
			content, err := lualib.MinimalBundleForTarget(string(luaTarget), usedExports)
			if err == nil {
				lualibContent = content
			}
		}
	}

	bundled, err := transpiler.BundleProgram(results, projectDir, lualibContent, transpiler.BundleOptions{
		EntryModule: entryModule,
		LuaTarget:   luaTarget,
	})
	if err != nil {
		diags = append(diags, dw.NewConfigError(dw.CouldNotFindBundleEntryPoint, err.Error()))
		return nil, diags
	}

	// Compute bundle output key: relative to outDir if set, otherwise projectDir.
	// Matches TSTL's behavior where the bundle path resolves relative to the
	// output directory or project root.
	bundleKey := req.LuaBundle
	if origOutDir != "" {
		if filepath.IsAbs(origOutDir) {
			bundleKey = filepath.Join(origOutDir, req.LuaBundle)
		} else {
			bundleKey = filepath.Join(projectDir, origOutDir, req.LuaBundle)
		}
	}
	bundleFiles := map[string]string{bundleKey: bundled}
	return bundleFiles, diags
}

// buildConfigFromRequest constructs a buildConfig from a server request.
// This is the server-side equivalent of buildConfigFromCLI.
func buildConfigFromRequest(req serverRequest, sourceRoot string, luaTarget transpiler.LuaTarget) *buildConfig {
	cfg := &buildConfig{
		sourceRoot: sourceRoot,
		luaTarget:  luaTarget,
		buildMode:  "default",
	}
	if v, ok := req.CompilerOptions["noImplicitSelf"].(bool); ok {
		cfg.noImplicitSelf = v
	}
	if v, ok := req.CompilerOptions["noImplicitGlobalVariables"].(bool); ok {
		cfg.noImplicitGlobalVariables = v
	}
	if v, ok := req.CompilerOptions["sourceMap"].(bool); ok {
		cfg.sourceMap = v
	}
	if v, ok := req.CompilerOptions["sourceMapTraceback"].(bool); ok && v {
		cfg.sourceMapTraceback = true
		cfg.sourceMap = true
	}
	if v, ok := req.CompilerOptions["inlineSourceMap"].(bool); ok && v {
		cfg.inlineSourceMap = true
		cfg.sourceMap = true
	}
	if v, ok := req.CompilerOptions["emitMode"].(string); ok && v != "" {
		cfg.emitMode = transpiler.EmitMode(v)
	}
	if v, ok := req.CompilerOptions["classStyle"].(string); ok && v != "" {
		cfg.classStyle = transpiler.ClassStyle(v)
	}
	if v, ok := req.CompilerOptions["luaLibImport"].(string); ok && v != "" {
		cfg.luaLibImport = transpiler.LuaLibImportKind(v)
	}
	if v, ok := req.CompilerOptions["exportAsGlobal"].(bool); ok {
		cfg.exportAsGlobal = v
	}
	if v, ok := req.CompilerOptions["buildMode"].(string); ok && v != "" {
		cfg.buildMode = v
	}
	if v, ok := req.CompilerOptions["outDir"].(string); ok && v != "" {
		cfg.outdir = v
	}
	return cfg
}

// addMinimalLualib adds a tree-shaken lualib_bundle.lua to the file map when
// LuaLibImport is RequireMinimal. Mirrors the logic in main.go's emit step.
func addMinimalLualib(luaFiles map[string]string, results []transpiler.TranspileResult, opts transpiler.TranspileOptions, luaTarget transpiler.LuaTarget) {
	if opts.LuaLibImport != transpiler.LuaLibImportRequireMinimal {
		return
	}
	usedExports := aggregateLualibExports(results)
	if len(usedExports) == 0 {
		return
	}
	content, err := lualib.MinimalBundleForTarget(string(luaTarget), usedExports)
	if err != nil {
		return
	}
	luaFiles["lualib_bundle.lua"] = string(content)
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// isInsideDir checks that path is inside dir after cleaning.
func isInsideDir(path, dir string) bool {
	cleaned := filepath.Clean(path)
	return strings.HasPrefix(cleaned, filepath.Clean(dir)+string(filepath.Separator))
}

// inlineLuaOutputKey computes the response file key for inline mode.
// Resolves rootDir/outDir from compilerOpts relative to projectDir,
// then delegates to emitpath.OutputPath.
// inlineLuaOutputKey computes the response file key for inline mode.
// outDir, rootDir, and extension should be the original values from the request
// (before any rewriting for tsconfig).
func inlineLuaOutputKey(fileName, projectDir, outDir, rootDir, extension string) string {
	// Resolve sourceRoot: rootDir > projectDir
	sourceRoot := projectDir
	if rootDir != "" {
		if filepath.IsAbs(rootDir) {
			sourceRoot = rootDir
		} else {
			sourceRoot = filepath.Join(projectDir, rootDir)
		}
	}

	// Resolve outDir to absolute if relative
	if outDir != "" && !filepath.IsAbs(outDir) {
		outDir = filepath.Join(projectDir, outDir)
	}

	return emitpath.OutputPath(fileName, sourceRoot, outDir, extension)
}
