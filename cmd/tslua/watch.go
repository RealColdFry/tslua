package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/microsoft/typescript-go/shim/incremental"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

func runWatch(cfg *buildConfig, host compiler.CompilerHost) error {
	cachedResults := map[string]transpiler.TranspileResult{}
	var program *compiler.Program
	var incrProg *incremental.Program

	// --- Initial full build ---
	program = compiler.NewProgram(compiler.ProgramOptions{
		Config:         cfg.configParseResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()
	incrProg = incremental.NewProgram(program, incrProg, nil, false)

	noEmit := noEmitFlag || program.Options().NoEmit.IsTrue()
	noEmitOnError := noEmitOnErrorFlag || program.Options().NoEmitOnError.IsTrue()
	var diagWg sync.WaitGroup

	t0 := time.Now()
	fmt.Fprintf(os.Stderr, "build starting at %s\n", t0.Format("03:04:05 PM"))

	semanticDiags := compiler.SortAndDeduplicateDiagnostics(
		incremental.Program_GetSemanticDiagnostics(incrProg, context.Background(), nil),
	)
	results, transpileDiags := cfg.transpile(program, nil)
	for _, r := range results {
		cachedResults[r.FileName] = r
	}
	hasErrors := reportDiagnostics(cfg, semanticDiags, transpileDiags)
	if !noEmit && (!noEmitOnError || !hasErrors) {
		emitResults(cfg, results)
	}

	fmt.Fprintf(os.Stderr, "build finished in %.2fms\n", msf(time.Since(t0)))

	// --- Set up fsnotify watcher ---
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	// Watch directories containing source files (fsnotify watches dirs, not individual files).
	watchedDirs := map[string]bool{}
	for _, sf := range program.SourceFiles() {
		dir := filepath.Dir(sf.FileName())
		if !watchedDirs[dir] {
			watchedDirs[dir] = true
			_ = watcher.Add(dir)
		}
	}

	// Build a set of source file paths for filtering events.
	sourceFiles := map[string]bool{}
	for _, sf := range program.SourceFiles() {
		sourceFiles[sf.FileName()] = true
	}

	// --- Watch loop: wait for events, debounce, incremental rebuild ---
	for {
		// Block until at least one event arrives.
		changedFiles := map[string]bool{}
		select {
		case ev := <-watcher.Events:
			if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 && sourceFiles[ev.Name] {
				changedFiles[ev.Name] = true
			}
		case err := <-watcher.Errors:
			fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
			continue
		}

		// Brief debounce: drain any events that arrive within 5ms (editors do multi-write).
		debounceTimer := time.NewTimer(5 * time.Millisecond)
	drain:
		for {
			select {
			case ev := <-watcher.Events:
				if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 && sourceFiles[ev.Name] {
					changedFiles[ev.Name] = true
				}
				debounceTimer.Reset(5 * time.Millisecond)
			case <-debounceTimer.C:
				break drain
			}
		}

		if len(changedFiles) == 0 {
			continue
		}

		// Wait for any async diagnostics from the previous cycle to finish
		// before starting a new build (avoids interleaved output).
		diagWg.Wait()

		t0 := time.Now()
		fmt.Fprintf(os.Stderr, "build starting at %s\n", t0.Format("03:04:05 PM"))

		// Use UpdateProgram for single-file changes (fast path: re-parses only the changed file).
		// Fall back to full NewProgram for multi-file changes or structural changes.
		fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
		newHost := compiler.NewCompilerHost(cfg.configDir, fs, bundled.LibPath(), nil, nil)

		reused := false
		if len(changedFiles) == 1 {
			var changedPath string
			for p := range changedFiles {
				changedPath = p
			}
			updatedProgram, ok := compiler.Program_UpdateProgram(
				program,
				tspath.Path(tspath.ToPath(changedPath, "", false)),
				newHost,
			)
			if ok {
				updatedProgram.BindSourceFiles()
				program = updatedProgram
				reused = true
			} else {
				// Structural change (imports changed, etc.) — full rebuild.
				program = compiler.NewProgram(compiler.ProgramOptions{
					Config:         cfg.configParseResult,
					SingleThreaded: core.TSTrue,
					Host:           newHost,
				})
				program.BindSourceFiles()
				changedFiles = nil // transpile all files
			}
		} else {
			// Multiple files changed — full rebuild.
			program = compiler.NewProgram(compiler.ProgramOptions{
				Config:         cfg.configParseResult,
				SingleThreaded: core.TSTrue,
				Host:           newHost,
			})
			program.BindSourceFiles()
			changedFiles = nil // transpile all files
		}

		tProgram := time.Now()

		if noEmit || noEmitOnError {
			// Synchronous path: check first, skip emit if noEmit or errors.
			incrProg = incremental.NewProgram(program, incrProg, nil, false)

			tIncr := time.Now()

			var semanticDiags []*ast.Diagnostic
			if changedFiles != nil {
				for _, sf := range program.SourceFiles() {
					if changedFiles[sf.FileName()] {
						semanticDiags = append(semanticDiags,
							incremental.Program_GetSemanticDiagnostics(incrProg, context.Background(), sf)...)
					}
				}
				semanticDiags = compiler.SortAndDeduplicateDiagnostics(semanticDiags)
			} else {
				semanticDiags = compiler.SortAndDeduplicateDiagnostics(
					incremental.Program_GetSemanticDiagnostics(incrProg, context.Background(), nil),
				)
			}

			tCheck := time.Now()

			freshResults, transpileDiags := cfg.transpile(program, changedFiles)
			for _, r := range freshResults {
				cachedResults[r.FileName] = r
			}

			tTranspile := time.Now()

			hasErrors := reportDiagnostics(cfg, semanticDiags, transpileDiags)
			if !noEmit && !hasErrors {
				if changedFiles != nil {
					emitResults(cfg, freshResults)
				} else {
					var allResults []transpiler.TranspileResult
					for _, sf := range program.SourceFiles() {
						if r, ok := cachedResults[sf.FileName()]; ok {
							allResults = append(allResults, r)
						}
					}
					emitResults(cfg, allResults)
				}
			}

			tWrite := time.Now()

			if timingFlag {
				mode := "full"
				if reused {
					mode = "update"
				}
				fmt.Fprintf(os.Stderr, "  program(%s): %7.2fms\n", mode, msf(tProgram.Sub(t0)))
				fmt.Fprintf(os.Stderr, "  incr:        %7.2fms\n", msf(tIncr.Sub(tProgram)))
				fmt.Fprintf(os.Stderr, "  check:       %7.2fms\n", msf(tCheck.Sub(tIncr)))
				var totalTransform, totalPrint time.Duration
				for _, r := range freshResults {
					totalTransform += r.TransformDur
					totalPrint += r.PrintDur
				}
				fmt.Fprintf(os.Stderr, "  transform:   %7.2fms\n", msf(totalTransform))
				fmt.Fprintf(os.Stderr, "  print:       %7.2fms\n", msf(totalPrint))
				fmt.Fprintf(os.Stderr, "  write:       %7.2fms\n", msf(tWrite.Sub(tTranspile)))
			}
			fmt.Fprintf(os.Stderr, "build finished in %.2fms\n", msf(time.Since(t0)))
		} else {
			// Async path: transpile+write immediately, incr+check in background.
			freshResults, transpileDiags := cfg.transpile(program, changedFiles)
			for _, r := range freshResults {
				cachedResults[r.FileName] = r
			}

			tTranspile := time.Now()

			if changedFiles != nil {
				emitResults(cfg, freshResults)
			} else {
				var allResults []transpiler.TranspileResult
				for _, sf := range program.SourceFiles() {
					if r, ok := cachedResults[sf.FileName()]; ok {
						allResults = append(allResults, r)
					}
				}
				emitResults(cfg, allResults)
			}

			tWrite := time.Now()

			if timingFlag {
				mode := "full"
				if reused {
					mode = "update"
				}
				fmt.Fprintf(os.Stderr, "  program(%s): %7.2fms\n", mode, msf(tProgram.Sub(t0)))
				var totalTransform, totalPrint time.Duration
				for _, r := range freshResults {
					totalTransform += r.TransformDur
					totalPrint += r.PrintDur
				}
				fmt.Fprintf(os.Stderr, "  transform:   %7.2fms\n", msf(totalTransform))
				fmt.Fprintf(os.Stderr, "  print:       %7.2fms\n", msf(totalPrint))
				fmt.Fprintf(os.Stderr, "  write:       %7.2fms\n", msf(tWrite.Sub(tTranspile)))
			}
			fmt.Fprintf(os.Stderr, "build finished in %.2fms\n", msf(time.Since(t0)))

			// Report transpile diagnostics immediately (they're already computed).
			if len(transpileDiags) > 0 {
				dw.FormatTSTLDiagnostics(os.Stderr, transpileDiags, cfg.cwd, cfg.diagFormat, cfg.stderrIsTerminal)
			}

			// Build incremental snapshot + check diagnostics in background.
			diagWg.Add(1)
			go func(oldIncrProg *incremental.Program, prog *compiler.Program, cf map[string]bool) {
				defer diagWg.Done()
				tIncrStart := time.Now()
				newIncrProg := incremental.NewProgram(prog, oldIncrProg, nil, false)
				// Update shared incrProg for next cycle (safe: next cycle waits on diagWg).
				incrProg = newIncrProg

				var semanticDiags []*ast.Diagnostic
				if cf != nil {
					for _, sf := range prog.SourceFiles() {
						if cf[sf.FileName()] {
							semanticDiags = append(semanticDiags,
								incremental.Program_GetSemanticDiagnostics(newIncrProg, context.Background(), sf)...)
						}
					}
					semanticDiags = compiler.SortAndDeduplicateDiagnostics(semanticDiags)
				} else {
					semanticDiags = compiler.SortAndDeduplicateDiagnostics(
						incremental.Program_GetSemanticDiagnostics(newIncrProg, context.Background(), nil),
					)
				}
				if len(semanticDiags) > 0 {
					dw.FormatTSDiagnostics(os.Stderr, semanticDiags, cfg.cwd, cfg.stderrIsTerminal)
				}
				if timingFlag {
					fmt.Fprintf(os.Stderr, "  diag(async): %7.2fms\n", msf(time.Since(tIncrStart)))
				}
			}(incrProg, program, changedFiles)
		}
	}
}
