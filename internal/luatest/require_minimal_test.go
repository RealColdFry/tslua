package luatest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// buildMinimalBundleForTS transpiles tsCode in require-minimal mode, aggregates
// LualibDeps across results, and returns the slim bundle bytes. Mirrors the
// aggregation done by cmd/tslua/main.go:aggregateLualibExports.
func buildMinimalBundleForTS(t *testing.T, tsCode string) (bundle string, exports []string) {
	return buildMinimalBundleWithExtras(t, tsCode, nil)
}

// buildMinimalBundleWithExtras is like buildMinimalBundleForTS but also scans
// extraLuaContents for ____lualib references, merging them into the deps.
func buildMinimalBundleWithExtras(t *testing.T, tsCode string, extraLuaContents []string) (bundle string, exports []string) {
	t.Helper()
	results := TranspileTS(t, tsCode, Opts{
		LuaLibImport: transpiler.LuaLibImportRequireMinimal,
	})
	seen := make(map[string]bool)
	for _, r := range results {
		for _, exp := range r.LualibDeps {
			if !seen[exp] {
				seen[exp] = true
				exports = append(exports, exp)
			}
		}
	}
	for _, lua := range extraLuaContents {
		for _, dep := range transpiler.ScanLuaForLualibDeps(lua) {
			if !seen[dep] {
				seen[dep] = true
				exports = append(exports, dep)
			}
		}
	}
	slices.Sort(exports)
	if len(exports) == 0 {
		return "", nil
	}
	b, err := lualib.MinimalBundleForTarget(string(transpiler.LuaTargetLua55), exports)
	if err != nil {
		t.Fatalf("MinimalBundleForTarget: %v", err)
	}
	return string(b), exports
}

func TestRequireMinimal_SlimBundleExcludesUnusedFeatures(t *testing.T) {
	t.Parallel()
	bundle, exports := buildMinimalBundleForTS(t, `export const r = [0, 1, 2].indexOf(1);`)

	if !slices.Contains(exports, "__TS__ArrayIndexOf") {
		t.Fatalf("expected __TS__ArrayIndexOf in exports, got %v", exports)
	}
	if !strings.Contains(bundle, "local function __TS__ArrayIndexOf") {
		t.Errorf("bundle missing __TS__ArrayIndexOf definition, got:\n%s", bundle)
	}
	// Features we know are NOT used — slim bundle must omit them.
	for _, excluded := range []string{
		"__TS__ArrayConcat",
		"__TS__StringStartsWith",
		"RangeError",
	} {
		if strings.Contains(bundle, excluded) {
			t.Errorf("slim bundle should not contain %q", excluded)
		}
	}
	// Footer should export only the directly-used feature.
	footerIdx := strings.LastIndex(bundle, "return {")
	if footerIdx < 0 {
		t.Fatalf("bundle has no return footer, got:\n%s", bundle)
	}
	footer := bundle[footerIdx:]
	if !strings.Contains(footer, "__TS__ArrayIndexOf = __TS__ArrayIndexOf") {
		t.Errorf("footer missing __TS__ArrayIndexOf export, got footer:\n%s", footer)
	}
}

// TestRequireMinimal_TransitiveDepsBodyNotFooter verifies that transitive lualib
// dependencies appear as file-local definitions in the slim bundle body (so
// directly-used features can reference them at load time) but are NOT listed in
// the return-table footer (which is the public API surface the bundle exposes).
//
// ArrayFlat depends on ArrayIsArray in the current lualib_module_info.json.
// If that manifest changes, this test needs to be updated.
func TestRequireMinimal_TransitiveDepsBodyNotFooter(t *testing.T) {
	t.Parallel()
	bundle, exports := buildMinimalBundleForTS(t, `export const r = [[1], [2]].flat();`)

	if !slices.Contains(exports, "__TS__ArrayFlat") {
		t.Fatalf("expected __TS__ArrayFlat in exports, got %v", exports)
	}
	// Transitive dep should appear in body as a local function.
	if !strings.Contains(bundle, "__TS__ArrayIsArray") {
		t.Errorf("expected transitive dep __TS__ArrayIsArray in bundle body, got:\n%s", bundle)
	}
	// But NOT in the return-table footer.
	footerIdx := strings.LastIndex(bundle, "return {")
	if footerIdx < 0 {
		t.Fatalf("bundle has no return footer")
	}
	footer := bundle[footerIdx:]
	if !strings.Contains(footer, "__TS__ArrayFlat") {
		t.Errorf("footer missing __TS__ArrayFlat, got footer:\n%s", footer)
	}
	if strings.Contains(footer, "__TS__ArrayIsArray") {
		t.Errorf("footer must not re-export transitive dep __TS__ArrayIsArray, got footer:\n%s", footer)
	}
}

// TestRequireMinimal_BundleIsExecutable writes the slim bundle to disk and runs
// the transpiled TS through a real Lua interpreter, verifying the bundle is
// loadable and that require("lualib_bundle") returns a working table.
func TestRequireMinimal_BundleIsExecutable(t *testing.T) {
	t.Parallel()
	const tsCode = `export const r = [0, 1, 2].indexOf(1);`

	results := TranspileTS(t, tsCode, Opts{
		LuaLibImport: transpiler.LuaLibImportRequireMinimal,
	})

	tmpDir := t.TempDir()
	// Write transpiled main.lua.
	var mainLua string
	for _, r := range results {
		luaName := strings.TrimSuffix(strings.TrimSuffix(r.FileName, ".tsx"), ".ts") + ".lua"
		outPath := filepath.Join(tmpDir, luaName)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outPath, []byte(r.Lua), 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(r.FileName, "main.ts") {
			mainLua = r.Lua
		}
	}

	// Build + write the slim lualib bundle.
	bundle, exports := buildMinimalBundleForTS(t, tsCode)
	if len(exports) == 0 {
		t.Fatal("expected at least one used export; got none")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "lualib_bundle.lua"), []byte(bundle), 0o644); err != nil {
		t.Fatal(err)
	}

	luaTarget := transpiler.LuaTargetLua55
	luaRuntime := luaTarget.Runtime()
	runner := BuildRunner(luaTarget, tmpDir, "main", "mod.r")

	e, ok := Evaluators[luaRuntime]
	if !ok {
		t.Skipf("%s not available", luaRuntime)
	}
	got, err := e.Eval(runner)
	if err != nil {
		t.Fatalf("%s error: %v\nbundle:\n%s\nmain:\n%s", luaRuntime, err, bundle, mainLua)
	}
	// [0,1,2].indexOf(1) === 1 in JS. Lua serialization: "1".
	if got != "1" {
		t.Errorf("got %q, want %q", got, "1")
	}
}

// TestRequireMinimal_ExtraLuaFileLualibDeps verifies that lualib references
// in external .lua files are included in the minimal bundle.
func TestRequireMinimal_ExtraLuaFileLualibDeps(t *testing.T) {
	t.Parallel()
	extraLua := "local ____lualib = require(\"lualib_bundle\")\n" +
		"local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf\n" +
		"__TS__ArrayIndexOf({}, 1)\n"

	// The TS main doesn't use any lualib features itself
	bundle, exports := buildMinimalBundleWithExtras(t, `export {}`, []string{extraLua})

	if !slices.Contains(exports, "__TS__ArrayIndexOf") {
		t.Fatalf("expected __TS__ArrayIndexOf in exports from extra .lua, got %v", exports)
	}
	if !strings.Contains(bundle, "local function __TS__ArrayIndexOf") {
		t.Errorf("bundle missing __TS__ArrayIndexOf definition")
	}
}

// TestRequireMinimal_ExtraLuaNoLualibDeps verifies that extra .lua files
// without lualib references do not add spurious deps.
func TestRequireMinimal_ExtraLuaNoLualibDeps(t *testing.T) {
	t.Parallel()
	extraLua := "local x = 42\nprint(x)\n"

	_, exports := buildMinimalBundleWithExtras(t, `export {}`, []string{extraLua})

	if len(exports) != 0 {
		t.Errorf("expected no exports from plain .lua, got %v", exports)
	}
}

// TestRequireMinimal_ExtraLuaDeduplicated verifies that lualib deps from
// extra .lua files are deduplicated with those from TS transpilation.
func TestRequireMinimal_ExtraLuaDeduplicated(t *testing.T) {
	t.Parallel()
	extraLua := "local ____lualib = require(\"lualib_bundle\")\n" +
		"local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf\n"

	// TS code also uses ArrayIndexOf
	bundle, exports := buildMinimalBundleWithExtras(t, `export const r = [0, 1, 2].indexOf(1);`, []string{extraLua})

	count := 0
	for _, e := range exports {
		if e == "__TS__ArrayIndexOf" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected __TS__ArrayIndexOf exactly once in exports, got %d (exports: %v)", count, exports)
	}
	if !strings.Contains(bundle, "local function __TS__ArrayIndexOf") {
		t.Errorf("bundle missing __TS__ArrayIndexOf definition")
	}
}
