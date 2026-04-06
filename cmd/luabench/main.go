package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/realcoldfry/tslua/internal/luabench"
	"github.com/realcoldfry/tslua/internal/transpiler"
	"github.com/spf13/cobra"
)

func main() {
	var (
		runtime string
		showLua bool
	)

	cmd := &cobra.Command{
		Use:   "luabench [names...]",
		Short: "Lua runtime benchmarks for tslua (tstl vs optimized emit)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath(runtime); err != nil {
				return fmt.Errorf("%s not found in PATH", runtime)
			}

			allBenchmarks := []luabench.Bench{
				{Name: "array_iterate", Iterations: 100},
				{Name: "array_push", Iterations: 100},
				{Name: "array_entries", Iterations: 100},
				{Name: "map_iterate", Iterations: 100},
				{Name: "set_iterate", Iterations: 100},
				{Name: "numeric_for", Iterations: 1000},
				{Name: "string_iterate", Iterations: 100},
				{Name: "string_concat", Iterations: 100},
			}

			// Filter by name if args provided
			benchmarks := allBenchmarks
			if len(args) > 0 {
				filter := make(map[string]bool, len(args))
				for _, a := range args {
					filter[a] = true
				}
				benchmarks = nil
				for _, b := range allBenchmarks {
					if filter[b.Name] {
						benchmarks = append(benchmarks, b)
					}
				}
				if len(benchmarks) == 0 {
					return fmt.Errorf("no benchmarks matched: %v", args)
				}
			}

			modes := []transpiler.EmitMode{
				transpiler.EmitModeTSTL,
				transpiler.EmitModeOptimized,
			}

			var results []luabench.Result
			for _, b := range benchmarks {
				for _, mode := range modes {
					fmt.Fprintf(os.Stderr, "running %s [%s]...\n", b.Name, mode)
					r, err := luabench.Run(b, mode, runtime)
					if err != nil {
						return err
					}
					results = append(results, r)
				}
			}

			luabench.PrintTable(results)
			if showLua {
				luabench.PrintLua(results)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&runtime, "runtime", "luajit", "Lua runtime binary (luajit, lua5.1, lua5.4, etc.)")
	cmd.Flags().BoolVar(&showLua, "show-lua", false, "print transpiled Lua for each benchmark")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
