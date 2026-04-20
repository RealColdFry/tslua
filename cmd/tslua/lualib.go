package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// runLualib builds the lualib bundle from TSTL's TypeScript source using tslua's
// own transpiler and prints it to stdout.
func runLualib() error {
	luaTarget := transpiler.LuaTarget(luaTargetFlag)
	if !transpiler.ValidTarget(luaTargetFlag) {
		return fmt.Errorf("unsupported luaTarget: %s", luaTargetFlag)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}

	srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
	langExtPath := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
	luaTypesPath := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")

	overrideDir := "universal"
	if luaTarget == transpiler.LuaTargetLua50 {
		overrideDir = "5.0"
	}

	bundle, err := lualib.BuildBundleFromSource(srcDir, langExtPath, luaTypesPath, luaTarget, overrideDir, nil)
	if err != nil {
		return err
	}

	fmt.Print(bundle)
	return nil
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	repoRoot := cwd
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			return repoRoot, nil
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			return "", fmt.Errorf("cannot find repo root (no go.mod found)")
		}
		repoRoot = parent
	}
}
