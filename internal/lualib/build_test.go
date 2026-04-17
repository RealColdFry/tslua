package lualib

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestCommittedBundleUpToDate asserts that the committed
// internal/lualib/lualib_bundle.lua byte-matches what BuildBundleFromSource
// would produce right now. Catches hand-edits to the committed file and
// drift after transpiler changes. On failure, run `just update-lualib`.
func TestCommittedBundleUpToDate(t *testing.T) {
	repoRoot, err := findRepoRootFromTest()
	if err != nil {
		t.Skipf("cannot locate repo root: %v", err)
	}
	srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
	langExt := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
	luaTypes := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")
	if _, err := os.Stat(filepath.Join(luaTypes, "5.4.d.ts")); err != nil {
		t.Skip("extern/tstl not set up (run `just tstl-setup`)")
	}

	built, err := BuildBundleFromSource(srcDir, langExt, luaTypes, transpiler.LuaTargetUniversal, "universal")
	if err != nil {
		t.Fatalf("BuildBundleFromSource: %v", err)
	}

	if built != string(Bundle) {
		diffContext := firstDifferingLine(built, string(Bundle))
		t.Fatalf("committed internal/lualib/lualib_bundle.lua is stale. Run `just update-lualib`.\nFirst diff: %s", diffContext)
	}
}

func firstDifferingLine(a, b string) string {
	la := strings.Split(a, "\n")
	lb := strings.Split(b, "\n")
	n := len(la)
	if len(lb) < n {
		n = len(lb)
	}
	for i := 0; i < n; i++ {
		if la[i] != lb[i] {
			return fmt.Sprintf("line %d:\n  built:     %q\n  committed: %q", i+1, la[i], lb[i])
		}
	}
	if len(la) != len(lb) {
		return fmt.Sprintf("line counts differ: built=%d committed=%d", len(la), len(lb))
	}
	return "(identical?)"
}

// TestSelfBuiltBundleExportsStable sanity-checks the export extraction helper
// used by other tooling (lualib-diff, etc.). The extractBundleExports helper
// reads the trailing `return { ... }` table and returns the keys.
var returnTableEntryRe = regexp.MustCompile(`(?m)^\s+(\w+)\s*=\s*\w+,?$`)

func extractBundleExports(bundle string) []string {
	idx := strings.LastIndex(bundle, "\nreturn {")
	if idx < 0 {
		return nil
	}
	tail := bundle[idx:]
	var names []string
	seen := map[string]bool{}
	for _, m := range returnTableEntryRe.FindAllStringSubmatch(tail, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

func TestSelfBuiltBundleExportsStable(t *testing.T) {
	repoRoot, err := findRepoRootFromTest()
	if err != nil {
		t.Skipf("cannot locate repo root: %v", err)
	}
	srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
	langExt := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
	luaTypes := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")
	if _, err := os.Stat(filepath.Join(luaTypes, "5.4.d.ts")); err != nil {
		t.Skip("extern/tstl not set up (run `just tstl-setup`)")
	}

	built, err := BuildBundleFromSource(srcDir, langExt, luaTypes, transpiler.LuaTargetUniversal, "universal")
	if err != nil {
		t.Fatalf("BuildBundleFromSource: %v", err)
	}

	gotExports := extractBundleExports(built)
	wantExports := extractBundleExports(string(Bundle))

	if len(gotExports) == 0 || len(wantExports) == 0 {
		t.Fatalf("empty export list: got=%d want=%d", len(gotExports), len(wantExports))
	}

	wantSet := map[string]bool{}
	for _, n := range wantExports {
		wantSet[n] = true
	}
	gotSet := map[string]bool{}
	for _, n := range gotExports {
		gotSet[n] = true
	}
	var missing, extra []string
	for n := range wantSet {
		if !gotSet[n] {
			missing = append(missing, n)
		}
	}
	for n := range gotSet {
		if !wantSet[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("export set mismatch\nmissing from self-built (%d): %v\nextra in self-built (%d): %v",
			len(missing), missing, len(extra), extra)
	}
}

func TestHasExportLeak(t *testing.T) {
	cases := []struct {
		name string
		body string
		exp  string
		want bool
	}{
		{"bare assign", "Map = __TS__Class()", "Map", true},
		{"local assign", "local Map = __TS__Class()", "Map", false},
		{"local function", "local function Foo(x)\nend", "Foo", false},
		{"global function decl", "function Foo(x)\nend", "Foo", true},
		{"property set only", "Map.prototype.x = 1", "Map", false},
		{"method def only", "function Map:m() end", "Map", false},
		{"equality not assign", "if Map == nil then end", "Map", false},
		{"name as substring", "SomeMap = {}", "Map", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasExportLeak(tc.body, tc.exp); got != tc.want {
				t.Errorf("hasExportLeak(%q, %q) = %v, want %v", tc.body, tc.exp, got, tc.want)
			}
		})
	}
}

func TestWrapFileBody(t *testing.T) {
	// No leak: body returned as-is.
	body := "local function __TS__ArrayAt(self, i)\nreturn self[i]\nend"
	got := wrapFileBody(body, []string{"__TS__ArrayAt"})
	if got != body {
		t.Errorf("expected no wrap for non-leaking body; got:\n%s", got)
	}

	// Leak: forward-decl + do/end wrap, body indented, exports sorted.
	body2 := "Map = __TS__Class()\nSet = __TS__Class()"
	got2 := wrapFileBody(body2, []string{"Set", "Map"})
	want2 := "local Map, Set\ndo\n    Map = __TS__Class()\n    Set = __TS__Class()\nend"
	if got2 != want2 {
		t.Errorf("wrap mismatch:\ngot:\n%s\n\nwant:\n%s", got2, want2)
	}
}

// TestSelfBuiltBundleLeaksNoGlobals mirrors TSTL's "Lualib bundle does not
// assign globals" check: snapshot _G before loading the bundle, load it, then
// assert no new keys appeared. Protects against a regression where
// `export class X` emits `X = ...` at bundle scope (an implicit global).
func TestSelfBuiltBundleLeaksNoGlobals(t *testing.T) {
	repoRoot, err := findRepoRootFromTest()
	if err != nil {
		t.Skipf("cannot locate repo root: %v", err)
	}
	luaBin, err := exec.LookPath("lua5.4")
	if err != nil {
		t.Skip("lua5.4 not installed")
	}
	srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
	langExt := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
	luaTypes := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")
	if _, err := os.Stat(filepath.Join(luaTypes, "5.4.d.ts")); err != nil {
		t.Skip("extern/tstl not set up (run `just tstl-setup`)")
	}

	built, err := BuildBundleFromSource(srcDir, langExt, luaTypes, transpiler.LuaTargetUniversal, "universal")
	if err != nil {
		t.Fatalf("BuildBundleFromSource: %v", err)
	}

	tmp := t.TempDir()
	bundlePath := filepath.Join(tmp, "lualib_bundle.lua")
	if err := os.WriteFile(bundlePath, []byte(built), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	// Pure-Lua probe: require the bundle, diff _G before and after.
	probe := `
		local pre = {}
		for k in pairs(_G) do pre[k] = true end
		require("lualib_bundle")
		local leaks = {}
		for k in pairs(_G) do if not pre[k] then table.insert(leaks, k) end end
		table.sort(leaks)
		io.write(table.concat(leaks, ","))
	`
	cmd := exec.Command(luaBin, "-e", "package.path='"+tmp+"/?.lua;'..package.path", "-e", probe)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lua probe: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "" {
		t.Errorf("lualib bundle leaks globals: %s", got)
	}
}

func findRepoRootFromTest() (string, error) {
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
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
