package luatest

import (
	"path/filepath"
	"testing"

	"github.com/realcoldfry/tslua/internal/emitpath"
)

// Tests that mirror TSTL's test/transpile/paths.spec.ts "getEmitPath" suite,
// exercising emitpath.OutputPath against real transpiler output filenames.
func TestEmitPath_FilesNextToInput(t *testing.T) {
	t.Parallel()
	results := TranspileTS(t, "", Opts{
		ExtraFiles: map[string]string{"dir/extra.ts": ""},
	})
	sourceRoot := filepath.Dir(results[0].FileName)
	for _, r := range results {
		got := emitpath.OutputPath(r.FileName, sourceRoot, "", "")
		base := filepath.Base(got)
		if base != "main.lua" && base != "extra.lua" {
			t.Errorf("unexpected output: %s", got)
		}
	}
}

func TestEmitPath_OutDir(t *testing.T) {
	t.Parallel()
	results := TranspileTS(t, "", Opts{
		ExtraFiles: map[string]string{"dir/extra.ts": ""},
	})
	sourceRoot := filepath.Dir(results[0].FileName)
	outDir := "/out/build"

	paths := map[string]bool{}
	for _, r := range results {
		got := emitpath.OutputPath(r.FileName, sourceRoot, outDir, "")
		paths[got] = true
	}
	want := []string{
		filepath.Join(outDir, "main.lua"),
		filepath.Join(outDir, "dir", "extra.lua"),
	}
	for _, w := range want {
		if !paths[w] {
			t.Errorf("missing expected path %q, got %v", w, paths)
		}
	}
}

func TestEmitPath_RootDirInOutDir(t *testing.T) {
	t.Parallel()
	// Put main and extra under src/ to simulate rootDir: "src".
	// Extra file has content to ensure it appears in transpile results.
	results := TranspileTS(t, "", Opts{
		MainFileName: "src/main.ts",
		ExtraFiles:   map[string]string{"src/extra.ts": "export const x = 1;"},
		CompilerOptions: map[string]any{
			"rootDir": "src",
		},
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(results), results)
	}
	// With rootDir: "src", sourceRoot is the src/ directory
	sourceRoot := filepath.Dir(results[0].FileName)
	outDir := "/out/build"

	paths := map[string]bool{}
	for _, r := range results {
		got := emitpath.OutputPath(r.FileName, sourceRoot, outDir, "")
		paths[got] = true
	}
	want := []string{
		filepath.Join(outDir, "main.lua"),
		filepath.Join(outDir, "extra.lua"),
	}
	for _, w := range want {
		if !paths[w] {
			t.Errorf("missing expected path %q, got %v", w, paths)
		}
	}
}

func TestEmitPath_RootDirInOutDir_EmptyExtraFile(t *testing.T) {
	t.Parallel()
	// Mirrors jest "puts files from rootDir in outdir" exactly: empty extra file.
	results := TranspileTS(t, "", Opts{
		MainFileName: "src/main.ts",
		ExtraFiles:   map[string]string{"src/extra.ts": ""},
		CompilerOptions: map[string]any{
			"rootDir": "src",
		},
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 files (main + empty extra), got %d", len(results))
	}
}

func TestEmitPath_CustomExtension(t *testing.T) {
	t.Parallel()
	results := TranspileTS(t, "", Opts{
		ExtraFiles: map[string]string{"dir/extra.ts": ""},
	})
	sourceRoot := filepath.Dir(results[0].FileName)

	for _, ext := range []string{".scar", "scar"} {
		paths := map[string]bool{}
		for _, r := range results {
			got := emitpath.OutputPath(r.FileName, sourceRoot, "", ext)
			paths[got] = true
		}
		if !paths["main.scar"] {
			t.Errorf("ext=%q: missing main.scar, got %v", ext, paths)
		}
		if !paths[filepath.Join("dir", "extra.scar")] {
			t.Errorf("ext=%q: missing dir/extra.scar, got %v", ext, paths)
		}
	}
}
