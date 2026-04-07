package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
)

// runEval transpiles inline TypeScript source and prints the Lua output.
// Source can be provided via -e flag or stdin.
func runEval(source string) error {
	if source == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		source = string(b)
	}

	luaTarget := transpiler.LuaTarget(luaTargetFlag)
	if !transpiler.ValidTarget(luaTargetFlag) {
		return fmt.Errorf("unsupported luaTarget: %s", luaTargetFlag)
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

	tmpDir, err := os.MkdirTemp("", "tslua-eval-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	mainFile := "main.ts"
	if err := os.WriteFile(filepath.Join(tmpDir, mainFile), []byte(source), 0o644); err != nil {
		return fmt.Errorf("write source: %w", err)
	}

	files := []string{mainFile}
	filesJSON, _ := json.Marshal(files)
	tsconfig := fmt.Sprintf(`{"compilerOptions":{"target":"ESNext","lib":["ESNext"],"strict":true,"skipLibCheck":true,"moduleResolution":"node"},"files":%s}`, filesJSON)
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		return fmt.Errorf("write tsconfig: %w", err)
	}

	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath("", filepath.Join(tmpDir, "tsconfig.json"))
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		return fmt.Errorf("tsconfig parse error: %d diagnostic(s)", len(diags))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	stderrIsTerminal := false
	if fi, err := os.Stderr.Stat(); err == nil {
		stderrIsTerminal = fi.Mode()&os.ModeCharDevice != 0
	}

	luaLibImport := transpiler.LuaLibImportKind(luaLibImportFlag)
	if !transpiler.ValidLuaLibImport(luaLibImportFlag) {
		return fmt.Errorf("unsupported luaLibImport: %s (supported: require, inline, none)", luaLibImportFlag)
	}
	opts := transpiler.TranspileOptions{
		EmitMode:                  transpiler.EmitMode(emitModeFlag),
		NoImplicitSelf:            noImplicitSelfFlag,
		NoImplicitGlobalVariables: noImplicitGlobalVariablesFlag,
		ClassStyle:                transpiler.ClassStyle(classStyleFlag),
		LuaLibImport:              luaLibImport,
		Trace:                     traceFlag,
	}
	if luaLibImport == transpiler.LuaLibImportInline {
		if fd, err := lualib.FeatureDataForTarget(string(luaTarget)); err == nil {
			opts.LualibFeatureData = fd
		} else {
			opts.LualibInlineContent = lualibInlineContent(luaTarget)
		}
	}
	semanticDiags := compiler.SortAndDeduplicateDiagnostics(
		compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil),
	)

	results, transpileDiags := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, opts)

	for _, r := range results {
		rel, _ := filepath.Rel(tmpDir, r.FileName)
		if strings.HasSuffix(rel, "main.ts") {
			fmt.Print(r.Lua)
			if len(semanticDiags) > 0 || len(transpileDiags) > 0 {
				if len(semanticDiags) > 0 {
					dw.FormatTSDiagnostics(os.Stderr, semanticDiags, tmpDir, stderrIsTerminal)
				}
				if len(transpileDiags) > 0 {
					if len(semanticDiags) > 0 {
						fmt.Fprintln(os.Stderr)
					}
					dw.FormatTSTLDiagnostics(os.Stderr, transpileDiags, tmpDir, diagFormat, stderrIsTerminal)
				}
			}
			return nil
		}
	}

	return fmt.Errorf("no output generated")
}
