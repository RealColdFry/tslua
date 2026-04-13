package emitpath

import (
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	tests := []struct {
		name       string
		sourceFile string
		sourceRoot string
		outDir     string
		extension  string
		want       string
	}{
		{
			name:       "same directory, no outDir",
			sourceFile: "/project/main.ts",
			sourceRoot: "/project",
			want:       "main.lua",
		},
		{
			name:       "subdirectory, no outDir",
			sourceFile: "/project/src/util.ts",
			sourceRoot: "/project",
			want:       filepath.Join("src", "util.lua"),
		},
		{
			name:       "with outDir relative",
			sourceFile: "/project/main.ts",
			sourceRoot: "/project",
			outDir:     "/project/out",
			want:       filepath.Join("/project", "out", "main.lua"),
		},
		{
			name:       "with outDir absolute",
			sourceFile: "/tmp/srv/main.ts",
			sourceRoot: "/tmp/srv",
			outDir:     "/home/user/out",
			want:       filepath.Join("/home", "user", "out", "main.lua"),
		},
		{
			name:       "with rootDir strips prefix",
			sourceFile: "/project/src/main.ts",
			sourceRoot: "/project/src",
			outDir:     "/project/out",
			want:       filepath.Join("/project", "out", "main.lua"),
		},
		{
			name:       "source outside sourceRoot filters ..",
			sourceFile: "/other/lib.ts",
			sourceRoot: "/project",
			outDir:     "/project/out",
			want:       filepath.Join("/project", "out", "other", "lib.lua"),
		},
		{
			name:       "custom extension with dot",
			sourceFile: "/project/main.ts",
			sourceRoot: "/project",
			extension:  ".scar",
			want:       "main.scar",
		},
		{
			name:       "custom extension without dot",
			sourceFile: "/project/main.ts",
			sourceRoot: "/project",
			extension:  "scar",
			want:       "main.scar",
		},
		{
			name:       "tsx file",
			sourceFile: "/project/app.tsx",
			sourceRoot: "/project",
			want:       "app.lua",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OutputPath(tt.sourceFile, tt.sourceRoot, tt.outDir, tt.extension)
			if got != tt.want {
				t.Errorf("OutputPath(%q, %q, %q, %q) = %q, want %q",
					tt.sourceFile, tt.sourceRoot, tt.outDir, tt.extension, got, tt.want)
			}
		})
	}
}
