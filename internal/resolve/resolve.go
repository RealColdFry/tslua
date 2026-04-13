// Package resolve handles post-transpilation dependency resolution.
//
// The transpiler emits structured ModuleDependency metadata alongside each
// file's Lua output. This package walks that metadata to discover external
// .lua dependencies (e.g. pre-built TSTL libraries in node_modules),
// reads them from disk, and recursively resolves their dependencies using
// FindLuaRequires (text scanning - the only option for third-party Lua).
//
// For files we transpiled ourselves, no text scanning is needed: the
// transpiler already resolved every import and recorded it as metadata.
package resolve

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// Options configures dependency resolution.
type Options struct {
	// SourceRoot is the project root directory (rootDir from tsconfig).
	SourceRoot string
	// BuildMode controls library vs default mode behavior.
	BuildMode BuildMode
}

// BuildMode controls how external dependencies are handled.
type BuildMode string

const (
	BuildModeDefault BuildMode = "default"
	BuildModeLibrary BuildMode = "library"
)

// ResolvedFile represents a file to be included in the output.
type ResolvedFile struct {
	// FileName is the absolute path of the file.
	FileName string
	// Lua is the Lua source code.
	Lua string
	// IsTranspiled is true for files we transpiled (vs external .lua).
	IsTranspiled bool
}

// Result holds the output of dependency resolution.
type Result struct {
	// Files is the ordered list of files to include in the output.
	// Transpiled files come first, followed by discovered external deps.
	Files []ResolvedFile
	// Errors collects non-fatal resolution issues.
	Errors []string
}

// ResolveDependencies walks the ModuleDependency metadata from transpiled
// results, discovers external .lua files, and returns the complete set of
// files to include in the output.
func ResolveDependencies(results []transpiler.TranspileResult, opts Options) Result {
	var res Result
	seen := make(map[string]bool)

	// Add all transpiled files first
	for _, r := range results {
		seen[r.FileName] = true
		res.Files = append(res.Files, ResolvedFile{
			FileName:     r.FileName,
			Lua:          r.Lua,
			IsTranspiled: true,
		})
	}

	// Walk dependencies from transpiled files
	for _, r := range results {
		for _, dep := range r.Dependencies {
			resolveDep(&res, seen, dep, opts)
		}
	}

	return res
}

func resolveDep(res *Result, seen map[string]bool, dep transpiler.ModuleDependency, opts Options) {
	if dep.ResolvedPath == "" || seen[dep.ResolvedPath] {
		return
	}
	if !dep.IsExternal {
		return // already in transpiled results
	}
	if !dep.IsLuaSource {
		return // .d.ts only, no Lua to include
	}

	// Library mode: don't include node_modules dependencies
	if opts.BuildMode == BuildModeLibrary && isNodeModulesPath(dep.ResolvedPath) {
		return
	}

	seen[dep.ResolvedPath] = true

	content, err := os.ReadFile(dep.ResolvedPath)
	if err != nil {
		res.Errors = append(res.Errors, "could not read dependency: "+dep.ResolvedPath)
		return
	}

	lua := string(content)
	res.Files = append(res.Files, ResolvedFile{
		FileName:     dep.ResolvedPath,
		Lua:          lua,
		IsTranspiled: false,
	})

	// Recursively resolve requires in external Lua files using text scanning.
	// This is the only place FindLuaRequires is needed in production.
	for _, req := range transpiler.FindLuaRequires(lua) {
		childPath := resolveExternalRequire(dep.ResolvedPath, req.RequirePath)
		if childPath == "" {
			continue
		}
		child := transpiler.ModuleDependency{
			RequirePath:  req.RequirePath,
			ResolvedPath: childPath,
			IsExternal:   true,
			IsLuaSource:  true,
		}
		resolveDep(res, seen, child, opts)
	}
}

// resolveExternalRequire resolves a require path found in an external .lua file
// to an absolute filesystem path. Searches relative to the requiring file's
// directory, trying .lua and init.lua conventions.
func resolveExternalRequire(fromFile string, requirePath string) string {
	dir := filepath.Dir(fromFile)

	// Convert dot-separated require path to filesystem path
	relPath := strings.ReplaceAll(requirePath, ".", string(filepath.Separator))

	candidates := []string{
		filepath.Join(dir, relPath+".lua"),
		filepath.Join(dir, relPath, "init.lua"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}

func isNodeModulesPath(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, part := range parts {
		if part == "node_modules" {
			return true
		}
	}
	return false
}
