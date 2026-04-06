package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
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
	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
	"github.com/spf13/cobra"
)

// version and gitCommit are set at build time via ldflags.
var (
	version   = "0.0.0-dev"
	gitCommit = ""
)

var (
	projectFlag        string
	outdirFlag         string
	luaTargetFlag      string
	emitModeFlag       string
	diagFormatFlag     string
	cpuprofileFlag     string
	luaBundleFlag      string
	luaBundleEntryFlag string
	exportAsGlobalFlag bool
	verboseFlag        bool
	timingFlag         bool
	watchFlag          bool
	luaLibImportFlag   string
	socketFlag         string
	evalSourceFlag     string
	sourceMapFlag      bool
	noImplicitSelfFlag bool
	traceFlag          bool
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

	// Persistent flags: shared across root and subcommands.
	rootCmd.PersistentFlags().StringVar(&luaTargetFlag, "luaTarget", "JIT", "Lua target version (JIT, 5.0, 5.1, 5.2, 5.3, 5.4, 5.5, Luau, universal)")
	rootCmd.PersistentFlags().StringVar(&diagFormatFlag, "diagnosticFormat", "tstl", "diagnostic code format (tstl, native)")
	rootCmd.PersistentFlags().StringVar(&cpuprofileFlag, "cpuprofile", "", "write CPU profile to file")
	rootCmd.PersistentFlags().BoolVar(&timingFlag, "timing", false, "print phase timings to stderr")
	rootCmd.PersistentFlags().StringVar(&luaLibImportFlag, "luaLibImport", "require", "how lualib features are included (require, inline, none)")

	// Root-only flags: project build mode.
	rootCmd.Flags().StringVarP(&projectFlag, "project", "p", "", "path to tsconfig.json")
	rootCmd.Flags().StringVar(&outdirFlag, "outdir", "", "output directory for Lua files (default: stdout)")
	rootCmd.Flags().StringVar(&emitModeFlag, "emitMode", "tstl", "emit mode: tstl (match TSTL output) or optimized")
	rootCmd.Flags().StringVar(&luaBundleFlag, "luaBundle", "", "output all modules as a single bundled Lua file")
	rootCmd.Flags().StringVar(&luaBundleEntryFlag, "luaBundleEntry", "", "entry point source file for bundle mode")
	rootCmd.Flags().BoolVar(&exportAsGlobalFlag, "exportAsGlobal", false, "strip module wrapper, emit exports as globals")
	rootCmd.Flags().BoolVar(&sourceMapFlag, "sourceMap", false, "generate .lua.map source map files")
	rootCmd.Flags().BoolVar(&noImplicitSelfFlag, "noImplicitSelf", false, "default functions to no-self unless annotated")
	rootCmd.Flags().BoolVar(&verboseFlag, "verbose", false, "print each output file path")
	rootCmd.Flags().BoolVarP(&watchFlag, "watch", "w", false, "watch for file changes and rebuild")

	// project is required for root command
	rootCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if projectFlag == "" {
			return fmt.Errorf("required flag \"project\" not set")
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
	cwd                 string
	configDir           string
	configParseResult   *tsoptions.ParsedCommandLine
	sourceRoot          string
	outdir              string
	diagFormat          dw.DiagnosticFormat
	luaTarget           transpiler.LuaTarget
	emitMode            transpiler.EmitMode
	luaLibImport        transpiler.LuaLibImportKind
	luaBundle           string
	luaBundleEntry      string
	exportAsGlobal      bool
	exportAsGlobalMatch string
	noImplicitSelf      bool
	sourceMap           bool
	stderrIsTerminal    bool
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

	// Parse tslua-specific config from tsconfig.json.
	tsluaCfg, err := parseTsluaConfig(string(resolvedConfigPath))
	if err != nil {
		return err
	}

	sourceRoot := string(configDir)
	if configParseResult.CompilerOptions().RootDir != "" {
		sourceRoot = tspath.ResolvePath(string(configDir), string(configParseResult.CompilerOptions().RootDir))
	}

	outdir := outdirFlag
	if outdir == "" && configParseResult.CompilerOptions().OutDir != "" {
		outdir = tspath.ResolvePath(string(configDir), configParseResult.CompilerOptions().OutDir)
	}

	var diagFormat dw.DiagnosticFormat
	switch diagFormatFlag {
	case "tstl":
		diagFormat = dw.DiagFormatTSTL
	case "native":
		diagFormat = dw.DiagFormatNative
	default:
		return fmt.Errorf("unsupported diagnosticFormat: %s (supported: tstl, native)", diagFormatFlag)
	}

	luaTarget := transpiler.LuaTarget(luaTargetFlag)
	if !transpiler.ValidTarget(luaTargetFlag) {
		return fmt.Errorf("unsupported luaTarget: %s (supported: JIT, 5.0, 5.1, 5.2, 5.3, 5.4, 5.5, Luau, universal)", luaTarget)
	}

	emitMode := transpiler.EmitMode(emitModeFlag)
	if emitMode != transpiler.EmitModeTSTL && emitMode != transpiler.EmitModeOptimized {
		return fmt.Errorf("unsupported emitMode: %s (supported: tstl, optimized)", emitMode)
	}

	luaLibImport := transpiler.LuaLibImportKind(luaLibImportFlag)
	if !transpiler.ValidLuaLibImport(luaLibImportFlag) {
		return fmt.Errorf("unsupported luaLibImport: %s (supported: require, inline, none)", luaLibImportFlag)
	}

	// Validate bundle options: both must be set or neither.
	if (luaBundleFlag != "") != (luaBundleEntryFlag != "") {
		return fmt.Errorf("--luaBundle and --luaBundleEntry must both be specified")
	}

	// Merge tslua tsconfig options with CLI flags (CLI wins).
	exportAsGlobal := exportAsGlobalFlag
	var exportAsGlobalMatch string
	if tsluaCfg != nil {
		if !exportAsGlobal {
			exportAsGlobal = tsluaCfg.exportAsGlobalBool
		}
		if exportAsGlobalMatch == "" {
			exportAsGlobalMatch = tsluaCfg.exportAsGlobalMatch
		}
	}

	cfg := &buildConfig{
		cwd:                 cwd,
		configDir:           string(configDir),
		configParseResult:   configParseResult,
		sourceRoot:          sourceRoot,
		outdir:              outdir,
		diagFormat:          diagFormat,
		luaTarget:           luaTarget,
		emitMode:            emitMode,
		luaLibImport:        luaLibImport,
		luaBundle:           luaBundleFlag,
		luaBundleEntry:      luaBundleEntryFlag,
		exportAsGlobal:      exportAsGlobal,
		exportAsGlobalMatch: exportAsGlobalMatch,
		noImplicitSelf:      noImplicitSelfFlag,
		sourceMap:           sourceMapFlag || configParseResult.CompilerOptions().SourceMap.IsTrue(),
		stderrIsTerminal:    stderrIsTerminal,
	}

	if watchFlag {
		if outdir == "" {
			return fmt.Errorf("--watch requires --outdir")
		}
		return runWatch(cfg, host)
	}

	return runOnce(cfg, host)
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

	semanticDiags := compiler.SortAndDeduplicateDiagnostics(
		compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil),
	)

	tCheck := time.Now()

	opts := transpiler.TranspileOptions{
		EmitMode:            cfg.emitMode,
		ExportAsGlobal:      cfg.exportAsGlobal,
		ExportAsGlobalMatch: cfg.exportAsGlobalMatch,
		NoImplicitSelf:      cfg.noImplicitSelf,
		LuaLibImport:        cfg.luaLibImport,
		SourceMap:           cfg.sourceMap,
	}
	if cfg.luaLibImport == transpiler.LuaLibImportInline {
		if fd, err := lualib.FeatureDataForTarget(string(cfg.luaTarget)); err == nil {
			opts.LualibFeatureData = fd
		} else {
			opts.LualibInlineContent = lualibInlineContent(cfg.luaTarget)
		}
	}
	results, transpileDiags := transpiler.TranspileProgramWithOptions(program, cfg.sourceRoot, cfg.luaTarget, nil, opts)

	hasErrors := reportDiagnostics(cfg, semanticDiags, transpileDiags)

	noEmitOnError := program.Options().NoEmitOnError.IsTrue()
	tWrite := time.Now()
	if !noEmitOnError || !hasErrors {
		if cfg.luaBundle != "" {
			if err := writeBundle(cfg, results); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		} else if cfg.outdir != "" {
			writeResults(cfg, results)
		} else {
			for _, r := range results {
				fmt.Printf("-- %s\n", r.FileName)
				fmt.Print(r.Lua)
				fmt.Println()
			}
		}
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

func reportDiagnostics(cfg *buildConfig, semanticDiags []*ast.Diagnostic, transpileDiags []*ast.Diagnostic) bool {
	hasErrors := false
	if len(semanticDiags) > 0 {
		dw.FormatTSDiagnostics(os.Stderr, semanticDiags, cfg.cwd, cfg.stderrIsTerminal)
		hasErrors = true
	}
	if len(transpileDiags) > 0 {
		if hasErrors {
			fmt.Fprintln(os.Stderr)
		}
		dw.FormatTSTLDiagnostics(os.Stderr, transpileDiags, cfg.cwd, cfg.diagFormat, cfg.stderrIsTerminal)
		hasErrors = true
	}
	return hasErrors
}

func writeBundle(cfg *buildConfig, results []transpiler.TranspileResult) error {
	// Compute entry module name from luaBundleEntry relative to sourceRoot.
	entryPath := tspath.ResolvePath(cfg.configDir, cfg.luaBundleEntry)
	entryModule := transpiler.ModuleNameFromPath(string(entryPath), cfg.sourceRoot)

	var lualibContent []byte
	for _, r := range results {
		if r.UsesLualib {
			lualibContent = lualib.BundleForTarget(string(cfg.luaTarget))
			break
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
		outPath := luaOutputPath(r.FileName, cfg.outdir, cfg.sourceRoot)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating directory: %v\n", err)
			continue
		}
		luaCode := r.Lua
		if r.SourceMap != "" {
			// Append sourceMappingURL comment
			mapFileName := filepath.Base(outPath) + ".map"
			luaCode += "--# sourceMappingURL=" + mapFileName + "\n"
		}
		if err := os.WriteFile(outPath, []byte(luaCode), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", outPath, err)
			continue
		}
		if r.SourceMap != "" {
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
	if needsLualib && cfg.luaLibImport == transpiler.LuaLibImportRequire {
		bundlePath := filepath.Join(cfg.outdir, "lualib_bundle.lua")
		if err := os.WriteFile(bundlePath, lualib.BundleForTarget(string(cfg.luaTarget)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", bundlePath, err)
		}
		if verboseFlag {
			fmt.Printf("  %s\n", bundlePath)
		}
	}
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

// luaOutputPath converts a .ts source path to a .lua output path,
// preserving directory structure relative to sourceRoot.
func luaOutputPath(tsPath string, outdir string, sourceRoot string) string {
	rel, err := filepath.Rel(sourceRoot, tsPath)
	if err != nil {
		// Fallback: just use basename
		rel = filepath.Base(tsPath)
	}
	rel = strings.TrimSuffix(rel, ".ts")
	rel = strings.TrimSuffix(rel, ".tsx")
	return filepath.Join(outdir, rel+".lua")
}
