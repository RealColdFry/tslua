// Test that tslua can transpile TSTL's lualib TypeScript source and produce
// a working lualib bundle. Builds the bundle, then runs smoke tests.
package luatest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/lualib"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func TestTranspile_LualibSmoke(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot()
	if repoRoot == "" {
		t.Skip("cannot find repo root")
	}

	srcDir := filepath.Join(repoRoot, "extern", "tstl", "src", "lualib")
	langExtPath := filepath.Join(repoRoot, "extern", "tstl", "language-extensions")
	luaTypesPath := filepath.Join(repoRoot, "extern", "tstl", "node_modules", "lua-types")

	bundle, err := lualib.BuildBundleFromSource(srcDir, langExtPath, luaTypesPath, transpiler.LuaTargetLua54, "universal")
	if err != nil {
		t.Fatalf("build bundle: %v", err)
	}
	if bundle == "" {
		t.Fatal("empty bundle")
	}
	// Check __TS__Class appears before first usage
	classDefLine := -1
	firstUseLine := -1
	bundleLines := strings.Split(bundle, "\n")
	for i, line := range bundleLines {
		if strings.HasPrefix(strings.TrimSpace(line), "local function __TS__Class(") || strings.HasPrefix(strings.TrimSpace(line), "function __TS__Class(") {
			classDefLine = i + 1
		}
		if classDefLine < 0 && strings.Contains(line, "__TS__Class()") {
			firstUseLine = i + 1
		}
	}
	if firstUseLine > 0 && (classDefLine < 0 || firstUseLine < classDefLine) {
		t.Errorf("__TS__Class used at line %d but defined at line %d — ordering bug", firstUseLine, classDefLine)
	}

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "lualib_bundle.lua"), []byte(bundle), 0o644); err != nil {
		t.Fatal(err)
	}

	luaRuntime := "lua5.4"

	cases := []struct {
		name string
		lua  string
		want string
	}{
		{"ArrayPush", `
local lb = require("lualib_bundle")
local arr = {1, 2, 3}
lb.__TS__ArrayPush(arr, 4, 5)
io.write(#arr .. ":" .. arr[4] .. "," .. arr[5])
`, "5:4,5"},
		{"ArrayIncludes", `
local lb = require("lualib_bundle")
local arr = {10, 20, 30}
local yes = lb.__TS__ArrayIncludes(arr, 20)
local no = lb.__TS__ArrayIncludes(arr, 99)
io.write(tostring(yes) .. "," .. tostring(no))
`, "true,false"},
		{"StringStartsWith", `
local lb = require("lualib_bundle")
local yes = lb.__TS__StringStartsWith("hello world", "hello")
local no = lb.__TS__StringStartsWith("hello world", "world")
io.write(tostring(yes) .. "," .. tostring(no))
`, "true,false"},
		{"ArrayMap", `
local lb = require("lualib_bundle")
local arr = {1, 2, 3}
local doubled = lb.__TS__ArrayMap(arr, function(_, x) return x * 2 end)
io.write(doubled[1] .. "," .. doubled[2] .. "," .. doubled[3])
`, "2,4,6"},
		{"TypeOf", `
local lb = require("lualib_bundle")
io.write(lb.__TS__TypeOf("hello") .. "," .. lb.__TS__TypeOf(42) .. "," .. lb.__TS__TypeOf(true))
`, "string,number,boolean"},
		{"MapExists", `
local lb = require("lualib_bundle")
io.write(type(lb) .. ":" .. type(lb.Map))
`, "table:table"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			luaCode := fmt.Sprintf(`package.path = %q .. "/?.lua;" .. package.path
%s`, tmpDir, tc.lua)

			if e, ok := Evaluators[luaRuntime]; ok {
				got, err := e.Eval(luaCode)
				if err != nil {
					t.Fatalf("lua error: %v", err)
				}
				if got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			} else {
				t.Skipf("%s not available", luaRuntime)
			}
		})
	}
}
