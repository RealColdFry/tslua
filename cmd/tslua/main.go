package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
	"github.com/realcoldfry/tslua/internal/emitpath"
	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/resolve"
	"github.com/realcoldfry/tslua/internal/transpiler"
	"github.com/spf13/cobra"
)

// version and gitCommit are set at build time via ldflags.
var (
	version   = "0.1.0-dev"
	gitCommit = ""
)

var (
	projectFlag                   string
	outdirFlag                    string
	luaTargetFlag                 string
	emitModeFlag                  string
	diagFormatFlag                string
	cpuprofileFlag                string
	luaBundleFlag                 string
	luaBundleEntryFlag            string
	exportAsGlobalFlag            bool
	verboseFlag                   bool
	timingFlag                    bool
	watchFlag                     bool
	luaLibImportFlag              string
	socketFlag                    string
	evalSourceFlag                string
	sourceMapFlag                 bool
	sourceMapTracebackFlag        bool
	inlineSourceMapFlag           bool
	noImplicitSelfFlag            bool
	noImplicitGlobalVariablesFlag bool
	classStyleFlag                string
	buildModeFlag                 string
	traceFlag                     bool
	noEmitFlag                    bool
	noEmitOnErrorFlag             bool
	evalLanguageExtensionsFlag    bool
	evalTypesFlag                 []string
)

func main() {
	rootCmd := &cobra.Command{
		Use:           "tslua",
		Short:         "TypeScript-to-Lua transpiler",
		Version:       versionString(),
		RunE:          run,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	// Persistent flags: shared across root and subcommands (affect transpilation behavior).
	rootCmd.PersistentFlags().StringVar(&luaTargetFlag, "luaTarget", "JIT", "Lua target version (JIT, 5.0, 5.1, 5.2, 5.3, 5.4, 5.5, Luau, universal)")
	rootCmd.PersistentFlags().StringVar(&diagFormatFlag, "diagnosticFormat", "tstl", "diagnostic code format (tstl, native)")
	rootCmd.PersistentFlags().StringVar(&cpuprofileFlag, "cpuprofile", "", "write CPU profile to file")
	rootCmd.PersistentFlags().BoolVar(&timingFlag, "timing", false, "print phase timings to stderr")
	rootCmd.PersistentFlags().StringVar(&luaLibImportFlag, "luaLibImport", "require", "how lualib features are included (require, inline, none)")
	rootCmd.PersistentFlags().StringVar(&emitModeFlag, "emitMode", "tstl", "emit mode: tstl (match TSTL output) or optimized")
	rootCmd.PersistentFlags().BoolVar(&noImplicitSelfFlag, "noImplicitSelf", false, "default functions to no-self unless annotated")
	rootCmd.PersistentFlags().BoolVar(&noImplicitGlobalVariablesFlag, "noImplicitGlobalVariables", false, "force local declarations in script-mode top-level scope")
	rootCmd.PersistentFlags().StringVar(&classStyleFlag, "classStyle", "", "class emit style (tstl, luabind, middleclass, inline)")
	rootCmd.PersistentFlags().StringVar(&buildModeFlag, "buildMode", "", "build mode: default or library")

	// Root-only flags: project build mode.
	rootCmd.Flags().StringVarP(&projectFlag, "project", "p", "", "path to tsconfig.json")
	rootCmd.Flags().StringVar(&outdirFlag, "outdir", "", "output directory for Lua files (default: stdout)")
	rootCmd.Flags().StringVar(&luaBundleFlag, "luaBundle", "", "output all modules as a single bundled Lua file")
	rootCmd.Flags().StringVar(&luaBundleEntryFlag, "luaBundleEntry", "", "entry point source file for bundle mode")
	rootCmd.Flags().BoolVar(&exportAsGlobalFlag, "exportAsGlobal", false, "strip module wrapper, emit exports as globals")
	rootCmd.Flags().BoolVar(&sourceMapFlag, "sourceMap", false, "generate .lua.map source map files")
	rootCmd.Flags().BoolVar(&sourceMapTracebackFlag, "sourceMapTraceback", false, "register source maps at runtime for debug.traceback rewriting")
	rootCmd.Flags().BoolVar(&inlineSourceMapFlag, "inlineSourceMap", false, "embed source map as base64 data URL in Lua output")
	rootCmd.Flags().BoolVar(&verboseFlag, "verbose", false, "print each output file path")
	rootCmd.Flags().BoolVarP(&watchFlag, "watch", "w", false, "watch for file changes and rebuild")
	rootCmd.Flags().BoolVar(&noEmitFlag, "noEmit", false, "type-check without emitting Lua files")
	rootCmd.Flags().BoolVar(&noEmitOnErrorFlag, "noEmitOnError", false, "skip emit when diagnostics contain errors")

	// default to current directory if --project not specified (matches TSTL behavior)
	rootCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if projectFlag == "" {
			projectFlag = "."
		}
		return nil
	}

	// eval subcommand
	evalCmd := &cobra.Command{
		Use:   "eval [source]",
		Short: "Transpile inline TypeScript to Lua",
		Long:  "Transpile TypeScript source to Lua and print the result. Source via -e flag, argument, or stdin.",
		RunE: func(cmd *cobra.Command, args []string) error {
			source := evalSourceFlag
			if source == "" && len(args) > 0 {
				source = args[0]
			}
			return runEval(source)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	evalCmd.Flags().StringVarP(&evalSourceFlag, "expr", "e", "", "TypeScript source to transpile")
	evalCmd.Flags().BoolVar(&traceFlag, "trace", false, "emit --[[trace: ...]] comments showing which TS node produced each Lua statement")
	evalCmd.Flags().BoolVar(&evalLanguageExtensionsFlag, "languageExtensions", false, "include TSTL language extension types ($multi, LuaMultiReturn, etc.)")
	evalCmd.Flags().StringArrayVar(&evalTypesFlag, "types", nil, "additional type roots to include (can be specified multiple times)")
	rootCmd.AddCommand(evalCmd)

	// ast subcommand
	astCmd := &cobra.Command{
		Use:   "ast [source]",
		Short: "Print TypeScript AST tree",
		Long:  "Parse TypeScript source and print the AST. Source via -e flag, argument, or stdin.",
		RunE: func(cmd *cobra.Command, args []string) error {
			source := evalSourceFlag
			if source == "" && len(args) > 0 {
				source = args[0]
			}
			return runAST(source)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	astCmd.Flags().StringVarP(&evalSourceFlag, "expr", "e", "", "TypeScript source to parse")
	astCmd.Flags().BoolVar(&showPos, "pos", false, "show [pos,end) ranges for each node")
	rootCmd.AddCommand(astCmd)

	// server subcommand
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Run as JSON-over-stdin/stdout server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	serverCmd.Flags().StringVar(&socketFlag, "socket", "", "Unix socket path for server mode")
	rootCmd.AddCommand(serverCmd)

	// lualib subcommand
	lualibCmd := &cobra.Command{
		Use:   "lualib",
		Short: "Build lualib bundle from TSTL source using tslua's transpiler",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Default to universal — the bundle must work on all Lua targets.
			if !cmd.Flags().Changed("luaTarget") {
				luaTargetFlag = "universal"
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLualib()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(lualibCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// buildConfig holds resolved settings shared across builds.
type buildConfig struct {
	cwd                       string
	configDir                 string
	configParseResult         *tsoptions.ParsedCommandLine
	sourceRoot                string
	outdir                    string
	diagFormat                dw.DiagnosticFormat
	luaTarget                 transpiler.LuaTarget
	emitMode                  transpiler.EmitMode
	luaLibImport              transpiler.LuaLibImportKind
	luaBundle                 string
	luaBundleEntry            string
	exportAsGlobal            bool
	exportAsGlobalMatch       string
	noImplicitSelf            bool
	noImplicitGlobalVariables bool
	sourceMap                 bool
	emitSourceMapFiles        bool
	sourceMapTraceback        bool
	inlineSourceMap           bool
	classStyle                transpiler.ClassStyle
	buildMode                 string
	trace                     bool
	noResolvePaths            []string
	stderrIsTerminal          bool
}

// transpileOpts returns the TranspileOptions derived from this build config.
// This is the single chokepoint for CLI entrypoints — all TranspileOptions
// construction for project builds goes through here.
func (cfg *buildConfig) transpileOpts() transpiler.TranspileOptions {
	opts := transpiler.TranspileOptions{
		EmitMode:                  cfg.emitMode,
		ExportAsGlobal:            cfg.exportAsGlobal,
		ExportAsGlobalMatch:       cfg.exportAsGlobalMatch,
		NoImplicitSelf:            cfg.noImplicitSelf,
		NoImplicitGlobalVariables: cfg.noImplicitGlobalVariables,
		LuaLibImport:              cfg.luaLibImport,
		SourceMap:                 cfg.sourceMap,
		SourceMapTraceback:        cfg.sourceMapTraceback,
		InlineSourceMap:           cfg.inlineSourceMap,
		ClassStyle:                cfg.classStyle,
		Trace:                     cfg.trace,
		NoResolvePaths:            cfg.noResolvePaths,
	}
	if cfg.luaLibImport == transpiler.LuaLibImportInline {
		if fd, err := lualib.FeatureDataForTarget(string(cfg.luaTarget)); err == nil {
			opts.LualibFeatureData = fd
		} else {
			opts.LualibInlineContent = lualibInlineContent(cfg.luaTarget)
		}
	}
	return opts
}

// transpile is the single transpilation chokepoint. Every entry point that
// produces Lua output calls this method. It wraps TranspileProgramWithOptions
// and centralizes validation (e.g. bundle+library mode conflict).
func (cfg *buildConfig) transpile(program *compiler.Program, onlyFiles map[string]bool) ([]transpiler.TranspileResult, []*ast.Diagnostic) {
	results, diags := transpiler.TranspileProgramWithOptions(program, cfg.sourceRoot, cfg.luaTarget, onlyFiles, cfg.transpileOpts())
	if cfg.luaBundle != "" && cfg.buildMode == "library" {
		diags = append(diags, dw.NewConfigError(dw.CannotBundleLibrary,
			`Cannot bundle projects with "buildMode": "library". Projects including the library can still bundle (which will include external library files).`))
	}
	return results, diags
}

func run(cmd *cobra.Command, args []string) error {
	if cpuprofileFlag != "" {
		f, err := os.Create(cpuprofileFlag)
		if err != nil {
			return fmt.Errorf("error creating profile: %w", err)
		}
		_ = pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	stderrIsTerminal := false
	if fi, err := os.Stderr.Stat(); err == nil {
		stderrIsTerminal = fi.Mode()&os.ModeCharDevice != 0
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting cwd: %w", err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))

	resolvedConfigPath := tspath.ResolvePath(cwd, projectFlag)
	if !fs.FileExists(resolvedConfigPath) {
		return fmt.Errorf("tsconfig not found: %s", resolvedConfigPath)
	}

	configDir := tspath.GetDirectoryPath(resolvedConfigPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configParseResult, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile(resolvedConfigPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diagnostics) > 0 {
		for _, d := range diagnostics {
			fmt.Fprintf(os.Stderr, "tsconfig error: %s\n", ast.Diagnostic_Localize(d, ast.DefaultLocale()))
		}
		os.Exit(1)
	}

	cfg, err := buildConfigFromCLI(cmd, configParseResult, string(resolvedConfigPath), string(configDir), cwd, stderrIsTerminal)
	if err != nil {
		return err
	}

	if watchFlag {
		return runWatch(cfg, host)
	}

	return runOnce(cfg, host)
}

// buildConfigFromCLI constructs a buildConfig by merging CLI flags with
// tsconfig values. CLI flags take priority when explicitly set.
func buildConfigFromCLI(cmd *cobra.Command, configParseResult *tsoptions.ParsedCommandLine, configPath, configDir, cwd string, stderrIsTerminal bool) (*buildConfig, error) {
	tsluaCfg, err := parseTsluaConfig(configPath)
	if err != nil {
		return nil, err
	}

	sourceRoot := configDir
	if configParseResult.CompilerOptions().RootDir != "" {
		sourceRoot = tspath.ResolvePath(configDir, string(configParseResult.CompilerOptions().RootDir))
	}

	outdir := outdirFlag
	if outdir == "" && configParseResult.CompilerOptions().OutDir != "" {
		outdir = tspath.ResolvePath(configDir, configParseResult.CompilerOptions().OutDir)
	}
	// Default to writing .lua files next to sources (matches TSTL behavior).
	if outdir == "" {
		outdir = sourceRoot
	}

	var diagFormat dw.DiagnosticFormat
	switch diagFormatFlag {
	case "tstl":
		diagFormat = dw.DiagFormatTSTL
	case "native":
		diagFormat = dw.DiagFormatNative
	default:
		return nil, fmt.Errorf("unsupported diagnosticFormat: %s (supported: tstl, native)", diagFormatFlag)
	}

	// Resolve options: CLI flag wins when explicitly set, otherwise fall back to tsconfig.
	luaTargetStr := luaTargetFlag
	if !cmd.Flags().Changed("luaTarget") && tsluaCfg != nil && tsluaCfg.LuaTarget != "" {
		luaTargetStr = tsluaCfg.LuaTarget
	}
	luaTarget := transpiler.LuaTarget(luaTargetStr)
	if !transpiler.ValidTarget(luaTargetStr) {
		if cmd.Flags().Changed("luaTarget") {
			return nil, fmt.Errorf("unsupported luaTarget: %s (supported: JIT, 5.0, 5.1, 5.2, 5.3, 5.4, 5.5, Luau, universal)", luaTarget)
		}
		fmt.Fprintf(os.Stderr, "warning: unknown luaTarget %q from tsconfig, falling back to universal\n", luaTargetStr)
		luaTarget = transpiler.LuaTargetUniversal
	}

	emitModeStr := emitModeFlag
	if !cmd.Flags().Changed("emitMode") && tsluaCfg != nil && tsluaCfg.EmitMode != "" {
		emitModeStr = tsluaCfg.EmitMode
	}
	emitMode := transpiler.EmitMode(emitModeStr)
	if emitMode != transpiler.EmitModeTSTL && emitMode != transpiler.EmitModeOptimized {
		return nil, fmt.Errorf("unsupported emitMode: %s (supported: tstl, optimized)", emitMode)
	}

	luaLibImportStr := luaLibImportFlag
	if !cmd.Flags().Changed("luaLibImport") && tsluaCfg != nil && tsluaCfg.LuaLibImport != "" {
		luaLibImportStr = tsluaCfg.LuaLibImport
	}
	luaLibImport := transpiler.LuaLibImportKind(luaLibImportStr)
	if !transpiler.ValidLuaLibImport(luaLibImportStr) {
		return nil, fmt.Errorf("unsupported luaLibImport: %s (supported: require, require-minimal, inline, none)", luaLibImportStr)
	}

	// Resolve bundle options from CLI or tsconfig.
	luaBundle := luaBundleFlag
	luaBundleEntry := luaBundleEntryFlag
	if luaBundle == "" && tsluaCfg != nil && tsluaCfg.LuaBundle != "" {
		luaBundle = tsluaCfg.LuaBundle
	}
	if luaBundleEntry == "" && tsluaCfg != nil && tsluaCfg.LuaBundleEntry != "" {
		luaBundleEntry = tsluaCfg.LuaBundleEntry
	}
	if (luaBundle != "") != (luaBundleEntry != "") {
		return nil, fmt.Errorf("luaBundle and luaBundleEntry must both be specified")
	}

	// Merge tslua tsconfig options with CLI flags (CLI wins).
	exportAsGlobal := exportAsGlobalFlag
	var exportAsGlobalMatch string
	classStyle := classStyleFlag
	noImplicitSelf := noImplicitSelfFlag
	noImplicitGlobalVariables := noImplicitGlobalVariablesFlag
	var noResolvePaths []string
	if tsluaCfg != nil {
		if !exportAsGlobal {
			exportAsGlobal = tsluaCfg.exportAsGlobalBool
		}
		if exportAsGlobalMatch == "" {
			exportAsGlobalMatch = tsluaCfg.exportAsGlobalMatch
		}
		if classStyle == "" {
			classStyle = tsluaCfg.ClassStyle
		}
		if !cmd.Flags().Changed("noImplicitSelf") && tsluaCfg.NoImplicitSelf != nil {
			noImplicitSelf = *tsluaCfg.NoImplicitSelf
		}
		if !cmd.Flags().Changed("noImplicitGlobalVariables") && tsluaCfg.NoImplicitGlobalVariables != nil {
			noImplicitGlobalVariables = *tsluaCfg.NoImplicitGlobalVariables
		}
		if !cmd.Flags().Changed("sourceMapTraceback") && tsluaCfg.SourceMapTraceback != nil {
			sourceMapTracebackFlag = *tsluaCfg.SourceMapTraceback
		}
		noResolvePaths = tsluaCfg.NoResolvePaths
	}

	// Resolve buildMode: CLI flag wins, then tsconfig, then default.
	buildMode := buildModeFlag
	if buildMode == "" && tsluaCfg != nil && tsluaCfg.BuildMode != "" {
		buildMode = tsluaCfg.BuildMode
	}
	if buildMode == "" {
		buildMode = "default"
	}
	if buildMode != "default" && buildMode != "library" {
		return nil, fmt.Errorf("unsupported buildMode: %s (supported: default, library)", buildMode)
	}

	// sourceMap controls internal source map generation (needed by traceback too).
	// emitSourceMapFiles controls writing .map files and sourceMappingURL comments.
	sourceMap := sourceMapFlag || sourceMapTracebackFlag || inlineSourceMapFlag || configParseResult.CompilerOptions().SourceMap.IsTrue()
	emitSourceMapFiles := sourceMapFlag || configParseResult.CompilerOptions().SourceMap.IsTrue()

	return &buildConfig{
		cwd:                       cwd,
		configDir:                 configDir,
		configParseResult:         configParseResult,
		sourceRoot:                sourceRoot,
		outdir:                    outdir,
		diagFormat:                diagFormat,
		luaTarget:                 luaTarget,
		emitMode:                  emitMode,
		luaLibImport:              luaLibImport,
		luaBundle:                 luaBundle,
		luaBundleEntry:            luaBundleEntry,
		exportAsGlobal:            exportAsGlobal,
		exportAsGlobalMatch:       exportAsGlobalMatch,
		noImplicitSelf:            noImplicitSelf,
		noImplicitGlobalVariables: noImplicitGlobalVariables,
		sourceMap:                 sourceMap,
		emitSourceMapFiles:        emitSourceMapFiles,
		sourceMapTraceback:        sourceMapTracebackFlag,
		inlineSourceMap:           inlineSourceMapFlag,
		classStyle:                transpiler.ClassStyle(classStyle),
		buildMode:                 buildMode,
		noResolvePaths:            noResolvePaths,
		stderrIsTerminal:          stderrIsTerminal,
	}, nil
}

func runOnce(cfg *buildConfig, host compiler.CompilerHost) error {
	t0 := time.Now()

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         cfg.configParseResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	tBind := time.Now()

	semanticDiags := compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil)

	tCheck := time.Now()

	results, transpileDiags := cfg.transpile(program, nil)

	allDiags := mergeAndSortDiagnostics(semanticDiags, transpileDiags)
	hasErrors := reportDiagnostics(cfg, allDiags)

	noEmit := noEmitFlag || program.Options().NoEmit.IsTrue()
	noEmitOnError := noEmitOnErrorFlag || program.Options().NoEmitOnError.IsTrue()
	tWrite := time.Now()
	if !noEmit && (!noEmitOnError || !hasErrors) {
		emitResults(cfg, results)
	}

	if timingFlag {
		tEnd := time.Now()
		fmt.Fprintf(os.Stderr, "\n=== Timing ===\n")
		fmt.Fprintf(os.Stderr, "  parse+bind:%7.2fms\n", msf(tBind.Sub(t0)))
		fmt.Fprintf(os.Stderr, "  check:     %7.2fms\n", msf(tCheck.Sub(tBind)))
		var totalTransform, totalPrint time.Duration
		for _, r := range results {
			totalTransform += r.TransformDur
			totalPrint += r.PrintDur
		}
		fmt.Fprintf(os.Stderr, "  transform: %7.2fms\n", msf(totalTransform))
		fmt.Fprintf(os.Stderr, "  print:     %7.2fms\n", msf(totalPrint))
		fmt.Fprintf(os.Stderr, "  write:     %7.2fms\n", msf(tEnd.Sub(tWrite)))
		fmt.Fprintf(os.Stderr, "  total:     %7.2fms\n", msf(tEnd.Sub(t0)))
	}

	if hasErrors {
		os.Exit(1)
	}
	return nil
}

func versionString() string {
	s := "tslua " + version
	if gitCommit != "" {
		s += " (" + gitCommit + ")"
	}
	s += " (TypeScript " + core.Version() + ")"
	return s
}

func msf(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// mergeAndSortDiagnostics combines pre-emit (TS) and transpiler (TSTL) diagnostics
// into a single sorted, deduplicated slice. This is the centralized chokepoint for
// diagnostic ordering across all command modes (CLI, eval, watch, server, wasm).
func mergeAndSortDiagnostics(preEmitDiags, transpileDiags []*ast.Diagnostic) []*ast.Diagnostic {
	return compiler.SortAndDeduplicateDiagnostics(append(preEmitDiags, transpileDiags...))
}

func reportDiagnostics(cfg *buildConfig, allDiags []*ast.Diagnostic) bool {
	if len(allDiags) == 0 {
		return false
	}
	dw.FormatAllDiagnostics(os.Stderr, allDiags, cfg.cwd, cfg.diagFormat, cfg.stderrIsTerminal)
	return true
}

func writeBundle(cfg *buildConfig, results []transpiler.TranspileResult) error {
	// Compute entry module name from luaBundleEntry relative to sourceRoot.
	entryPath := tspath.ResolvePath(cfg.configDir, cfg.luaBundleEntry)
	entryModule := transpiler.ModuleNameFromPath(string(entryPath), cfg.sourceRoot)

	var lualibContent []byte
	switch cfg.luaLibImport {
	case transpiler.LuaLibImportRequire:
		for _, r := range results {
			if r.UsesLualib {
				lualibContent = lualib.BundleForTarget(string(cfg.luaTarget))
				break
			}
		}
	case transpiler.LuaLibImportRequireMinimal:
		usedExports := aggregateLualibExportsWithLuaFiles(results, cfg.sourceRoot)
		if len(usedExports) > 0 {
			content, err := lualib.MinimalBundleForTarget(string(cfg.luaTarget), usedExports)
			if err != nil {
				return fmt.Errorf("error building minimal lualib bundle: %w", err)
			}
			lualibContent = content
		}
	}

	bundled, err := transpiler.BundleProgram(results, cfg.sourceRoot, lualibContent, transpiler.BundleOptions{
		EntryModule: entryModule,
		LuaTarget:   cfg.luaTarget,
	})
	if err != nil {
		return err
	}

	outPath := cfg.luaBundle
	if cfg.outdir != "" {
		outPath = filepath.Join(cfg.outdir, cfg.luaBundle)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("error creating directory: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(bundled), 0o644); err != nil {
		return fmt.Errorf("error writing %s: %w", outPath, err)
	}
	if verboseFlag {
		fmt.Printf("  %s\n", outPath)
	}
	return nil
}

func writeResults(cfg *buildConfig, results []transpiler.TranspileResult) {
	needsLualib := false
	for _, r := range results {
		outPath := emitpath.OutputPath(r.FileName, cfg.sourceRoot, cfg.outdir, "")
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating directory: %v\n", err)
			continue
		}
		luaCode := r.Lua
		if r.SourceMap != "" && cfg.emitSourceMapFiles {
			// Append sourceMappingURL comment and write .map file only when
			// sourceMap is explicitly enabled (not just implied by sourceMapTraceback).
			mapFileName := filepath.Base(outPath) + ".map"
			luaCode += "--# sourceMappingURL=" + mapFileName + "\n"
		}
		if err := os.WriteFile(outPath, []byte(luaCode), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", outPath, err)
			continue
		}
		if r.SourceMap != "" && cfg.emitSourceMapFiles {
			mapPath := outPath + ".map"
			if err := os.WriteFile(mapPath, []byte(r.SourceMap), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "error writing %s: %v\n", mapPath, err)
			}
			if verboseFlag {
				fmt.Printf("  %s\n", mapPath)
			}
		}
		if verboseFlag {
			fmt.Printf("  %s\n", outPath)
		}
		if r.UsesLualib {
			needsLualib = true
		}
	}
	if needsLualib {
		var bundleContent []byte
		switch cfg.luaLibImport {
		case transpiler.LuaLibImportRequire:
			bundleContent = lualib.BundleForTarget(string(cfg.luaTarget))
		case transpiler.LuaLibImportRequireMinimal:
			usedExports := aggregateLualibExportsWithLuaFiles(results, cfg.sourceRoot)
			if len(usedExports) > 0 {
				content, err := lualib.MinimalBundleForTarget(string(cfg.luaTarget), usedExports)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error building minimal lualib bundle: %v\n", err)
				} else {
					bundleContent = content
				}
			}
		}
		if bundleContent != nil {
			bundlePath := filepath.Join(cfg.outdir, "lualib_bundle.lua")
			if err := os.WriteFile(bundlePath, bundleContent, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "error writing %s: %v\n", bundlePath, err)
			}
			if verboseFlag {
				fmt.Printf("  %s\n", bundlePath)
			}
		}
	}
}

// emitResults is the shared post-transpilation pipeline: resolve external
// dependencies, then write (or bundle) all output files. Every code path
// that produces project output (runOnce, watch, handleProjectRequest) must
// go through this function.
func emitResults(cfg *buildConfig, results []transpiler.TranspileResult) {
	resolved := resolve.ResolveDependencies(results, resolve.Options{
		SourceRoot: cfg.sourceRoot,
		BuildMode:  resolve.BuildMode(cfg.buildMode),
	})
	for _, e := range resolved.Errors {
		fmt.Fprintln(os.Stderr, "resolve:", e)
	}

	if cfg.luaBundle != "" {
		if err := writeBundle(cfg, results); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	} else {
		writeResults(cfg, results)
		writeExternalFiles(cfg, resolved)
	}
}

// writeExternalFiles writes discovered external .lua dependencies to the output directory.
func writeExternalFiles(cfg *buildConfig, resolved resolve.Result) {
	for _, f := range resolved.Files {
		if f.IsTranspiled {
			continue // already written by writeResults
		}
		outPath := emitpath.OutputPath(f.FileName, cfg.sourceRoot, cfg.outdir, "")
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating directory: %v\n", err)
			continue
		}
		if err := os.WriteFile(outPath, []byte(f.Lua), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", outPath, err)
			continue
		}
		if verboseFlag {
			fmt.Printf("  %s\n", outPath)
		}
	}
}

// aggregateLualibExports collects a deduplicated, sorted list of lualib export
// names used across all transpile results. Requires TranspileResult.LualibDeps
// to be populated (only for LuaLibImportNone and LuaLibImportRequireMinimal).
func aggregateLualibExports(results []transpiler.TranspileResult) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range results {
		for _, exp := range r.LualibDeps {
			if !seen[exp] {
				seen[exp] = true
				out = append(out, exp)
			}
		}
	}
	sort.Strings(out)
	return out
}

// aggregateLualibExportsWithLuaFiles extends aggregateLualibExports by also
// scanning .lua source files in sourceRoot for ____lualib references.
func aggregateLualibExportsWithLuaFiles(results []transpiler.TranspileResult, sourceRoot string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range results {
		for _, exp := range r.LualibDeps {
			if !seen[exp] {
				seen[exp] = true
				out = append(out, exp)
			}
		}
	}

	// Walk sourceRoot for .lua files and scan for lualib deps
	if sourceRoot != "" {
		_ = filepath.Walk(sourceRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".lua") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			for _, dep := range transpiler.ScanLuaForLualibDeps(string(data)) {
				if !seen[dep] {
					seen[dep] = true
					out = append(out, dep)
				}
			}
			return nil
		})
	}

	sort.Strings(out)
	return out
}

// lualibInlineContent returns the lualib bundle with the trailing return table
// stripped, so the functions remain as locals in scope for inline mode.
func lualibInlineContent(target transpiler.LuaTarget) string {
	content := string(lualib.BundleForTarget(string(target)))
	if idx := strings.Index(content, "\nreturn {"); idx >= 0 {
		content = content[:idx+1] // keep up to (and including) the newline before "return {"
	}
	return content
}
