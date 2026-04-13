// Package emitpath computes output file paths for transpiled Lua files.
// Ported from: src/transpilation/transpiler.ts (getEmitPath, getEmitPathRelativeToOutDir)
package emitpath

import (
	"path/filepath"
	"strings"
)

// OutputPath computes the output file path for a transpiled source file.
// sourceFile is the absolute path to the .ts/.tsx source.
// sourceRoot is the rootDir (or project/config directory if rootDir is unset).
// outDir is the output directory (absolute or empty for same-as-source).
// extension is the output extension without leading dot (default "lua").
func OutputPath(sourceFile, sourceRoot, outDir, extension string) string {
	rel := RelativeToOutDir(sourceFile, sourceRoot, extension)
	if outDir != "" {
		return filepath.Join(outDir, rel)
	}
	return rel
}

// RelativeToOutDir computes the output path relative to outDir.
// This is the path component that gets joined with outDir to form the full output path.
func RelativeToOutDir(sourceFile, sourceRoot, extension string) string {
	rel, err := filepath.Rel(sourceRoot, sourceFile)
	if err != nil {
		rel = filepath.Base(sourceFile)
	}

	// If source is in a parent directory of sourceRoot, filter out ".." segments
	// to avoid escaping the output directory. Matches TSTL behavior.
	parts := strings.Split(filepath.ToSlash(rel), "/")
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != ".." {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		filtered = []string{filepath.Base(sourceFile)}
	}

	// Replace extension
	if extension == "" {
		extension = "lua"
	}
	extension = strings.TrimPrefix(extension, ".")
	last := filtered[len(filtered)-1]
	last = trimTSExtension(last) + "." + extension
	filtered[len(filtered)-1] = last

	return filepath.Join(filtered...)
}

// trimTSExtension removes .ts, .tsx, .mts, .cts extensions.
func trimTSExtension(name string) string {
	for _, ext := range []string{".tsx", ".mts", ".cts", ".ts"} {
		if trimmed, ok := strings.CutSuffix(name, ext); ok {
			return trimmed
		}
	}
	return name
}
