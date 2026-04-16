package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

func TestResolveBundleOutputPath(t *testing.T) {
	t.Parallel()

	projectDir := filepath.Join(string(filepath.Separator), "project")

	tests := []struct {
		name      string
		outDir    string
		luaBundle string
		want      string
	}{
		{
			name:      "relative bundle without outDir is rooted at project",
			luaBundle: filepath.Join("out", "bundle.lua"),
			want:      filepath.Join(projectDir, "out", "bundle.lua"),
		},
		{
			name:      "relative bundle with relative outDir is rooted at outDir",
			outDir:    "dist",
			luaBundle: "bundle.lua",
			want:      filepath.Join(projectDir, "dist", "bundle.lua"),
		},
		{
			name:      "relative bundle with absolute outDir is rooted at absolute outDir",
			outDir:    filepath.Join(string(filepath.Separator), "tmp", "build"),
			luaBundle: "bundle.lua",
			want:      filepath.Join(string(filepath.Separator), "tmp", "build", "bundle.lua"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveBundleOutputPath(projectDir, tt.outDir, tt.luaBundle); got != tt.want {
				t.Fatalf("resolveBundleOutputPath(...) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddMinimalLualib_ScansLuaFiles(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	luaPath := filepath.Join(sourceRoot, "vendor.lua")
	luaCode := "local ____lualib = require(\"lualib_bundle\")\n" +
		"local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf\n"
	if err := os.WriteFile(luaPath, []byte(luaCode), 0o644); err != nil {
		t.Fatal(err)
	}

	luaFiles := map[string]string{}
	addMinimalLualib(luaFiles, nil, sourceRoot, transpiler.TranspileOptions{
		LuaLibImport: transpiler.LuaLibImportRequireMinimal,
	}, transpiler.LuaTargetLua55)

	bundle, ok := luaFiles["lualib_bundle.lua"]
	if !ok {
		t.Fatal("expected lualib_bundle.lua to be emitted")
	}
	if !strings.Contains(bundle, "local function __TS__ArrayIndexOf") {
		t.Fatalf("expected minimal bundle to include __TS__ArrayIndexOf, got:\n%s", bundle)
	}
}
