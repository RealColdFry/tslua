// Generates per-feature .lua files and module info JSON for the lualib.
// Used by update-lualib.sh to populate internal/lualib/features/.
//
// Usage: go run ./scripts/gen-lualib-features [--target universal|5.0]
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

func main() {
	target := "universal"
	if len(os.Args) > 2 && os.Args[1] == "--target" {
		target = os.Args[2]
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		fatal(err)
	}

	srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
	langExtPath := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
	luaTypesPath := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")

	var luaTarget transpiler.LuaTarget
	var overrideDir string
	switch target {
	case "5.0":
		luaTarget = transpiler.LuaTargetLua50
		overrideDir = "5.0"
	default:
		luaTarget = transpiler.LuaTargetUniversal
		overrideDir = "universal"
	}

	data, err := lualib.BuildFeatureDataFromSource(srcDir, langExtPath, luaTypesPath, luaTarget, overrideDir, nil)
	if err != nil {
		fatal(err)
	}

	// Determine output directory
	var outDir string
	if target == "5.0" {
		outDir = filepath.Join(repoRoot, "internal", "lualib", "features_50")
	} else {
		outDir = filepath.Join(repoRoot, "internal", "lualib", "features")
	}

	// Clean and recreate
	_ = os.RemoveAll(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	// Write per-feature .lua files
	for feature, code := range data.FeatureCode {
		path := filepath.Join(outDir, feature+".lua")
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			fatal(fmt.Errorf("write %s: %w", path, err))
		}
	}

	// Write module info JSON
	infoJSON, err := marshalModuleInfo(data.ModuleInfo)
	if err != nil {
		fatal(err)
	}
	var infoFile string
	if target == "5.0" {
		infoFile = filepath.Join(repoRoot, "internal", "lualib", "lualib_module_info_50.json")
	} else {
		infoFile = filepath.Join(repoRoot, "internal", "lualib", "lualib_module_info.json")
	}
	if err := os.WriteFile(infoFile, infoJSON, 0o644); err != nil {
		fatal(err)
	}

	fmt.Fprintf(os.Stderr, "wrote %d features to %s\n", len(data.FeatureCode), outDir)
}

func marshalModuleInfo(info lualib.ModuleInfo) ([]byte, error) {
	keys := make([]string, 0, len(info))
	for k := range info {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := []byte("{\n")
	for i, k := range keys {
		entryJSON, err := json.Marshal(info[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, fmt.Sprintf("  %q: %s", k, entryJSON)...)
		if i < len(keys)-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, '\n')
	}
	buf = append(buf, "}\n"...)
	return buf, nil
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("cannot find repo root (no go.mod found)")
		}
		dir = parent
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
