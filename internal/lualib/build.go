// Lualib bundle builder: transpiles TSTL's lualib TypeScript source into a
// self-contained lualib_bundle.lua matching TSTL's bundle format.
package lualib

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// patchExportRe matches top-level `local function __TS__Foo(` declarations
// in patches.lua. These get added to the bundle's export table.
var patchExportRe = regexp.MustCompile(`(?m)^local function (__TS__\w+)\(`)

// TODO: replace this string-based leak detection and wrap with an AST-level
// pass (see TSTL's lualibFileVisitor in extern/tstl/src/lualib-build/plugin.ts,
// which prepends a VariableDeclaration and wraps the body in a DoStatement).

// hasExportLeak reports whether body assigns to name at file scope without
// using `local`, which would make name an implicit global once the body is
// concatenated into the bundle's top-level chunk. Covers:
//
//   - bare `name = ...` assignment (e.g. `Map = __TS__Class()`)
//   - global function declaration `function name(...)`
//
// Property access and method definitions on an already-bound identifier
// (`Map.prototype.X = ...`, `function Map:method(...)`) are NOT leaks on
// their own; they rely on name being in scope, which the forward
// declaration below guarantees.
func hasExportLeak(body, name string) bool {
	q := regexp.QuoteMeta(name)
	// `^name<space>*=<not =>`: bare top-level assignment.
	assignRe := regexp.MustCompile(`(?m)^` + q + `\s*=[^=]`)
	if assignRe.MatchString(body) {
		return true
	}
	// `^function <name>(`: global function declaration.
	fnRe := regexp.MustCompile(`(?m)^function\s+` + q + `\s*\(`)
	return fnRe.MatchString(body)
}

// wrapFileBody wraps body in `local <leaks>\ndo\n<body>\nend` when any of
// its exports would otherwise leak as globals. Mirrors TSTL's per-feature
// bundling structure: forward-declared locals outside a `do...end` keep the
// helper locals inside the block scoped while still exposing the exports
// to the rest of the bundle. When no export leaks, body is returned as-is
// (matching TSTL's treatment of simple single-function feature files like
// ArrayAt.ts).
func wrapFileBody(body string, exports []string) string {
	if body == "" {
		return body
	}
	var leaks []string
	for _, name := range exports {
		if hasExportLeak(body, name) {
			leaks = append(leaks, name)
		}
	}
	if len(leaks) == 0 {
		return body
	}
	sort.Strings(leaks)
	var sb strings.Builder
	fmt.Fprintf(&sb, "local %s\n", strings.Join(leaks, ", "))
	sb.WriteString("do\n")
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			sb.WriteByte('\n')
			continue
		}
		sb.WriteString("    ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteString("end")
	return sb.String()
}

// transpileLualibSource transpiles TSTL's lualib TypeScript source files and
// returns the per-file results plus an export→file-index map.
func transpileLualibSource(lualibSrcDir, langExtPath, luaTypesPath string, luaTarget transpiler.LuaTarget, overrideDir string) ([]transpiler.TranspileResult, map[string]int, error) {
	// Collect .ts files from the base directory
	baseFiles, err := collectTSFiles(lualibSrcDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read lualib dir: %w", err)
	}

	// Apply overrides: if an override file exists, it replaces the base file
	files := make(map[string]string) // basename → full path
	for _, f := range baseFiles {
		files[filepath.Base(f)] = f
	}
	if overrideDir != "" {
		overridePath := filepath.Join(lualibSrcDir, overrideDir)
		overrides, err := collectTSFiles(overridePath)
		if err == nil {
			for _, f := range overrides {
				files[filepath.Base(f)] = f
			}
		}
	}

	// Write files to a temp directory for transpilation
	tmpDir, err := os.MkdirTemp("", "tslua-lualib-*")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	for basename, srcPath := range files {
		src, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", srcPath, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, basename), src, 0o644); err != nil {
			return nil, nil, err
		}
	}

	// Copy .d.ts files from the source directory. These aren't transpiled but
	// provide type info for the checker (e.g. SparseArray.d.ts defines the
	// intersection type that lets isArrayType recognize sparse arrays).
	if entries, err := os.ReadDir(lualibSrcDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".d.ts") {
				data, _ := os.ReadFile(filepath.Join(lualibSrcDir, e.Name()))
				_ = os.WriteFile(filepath.Join(tmpDir, e.Name()), data, 0o644)
			}
		}
	}
	// Copy declaration files from subdirectory (declarations/tstl.d.ts, etc.)
	declsDir := filepath.Join(lualibSrcDir, "declarations")
	if err := copyDeclFiles(declsDir, tmpDir); err != nil {
		_ = err
	}

	// Determine lua-types version file based on target
	luaTypesVersion := "5.4" // default
	switch luaTarget {
	case transpiler.LuaTargetLua50:
		luaTypesVersion = "5.0"
	case transpiler.LuaTargetLua51, transpiler.LuaTargetLuaJIT:
		luaTypesVersion = "5.1"
	case transpiler.LuaTargetLua52:
		luaTypesVersion = "5.2"
	case transpiler.LuaTargetLua53:
		luaTypesVersion = "5.3"
	case transpiler.LuaTargetLua54:
		luaTypesVersion = "5.4"
	case transpiler.LuaTargetLua55:
		// lua-types may not have 5.5 yet; fall back to 5.4
		luaTypesVersion = "5.4"
	}
	luaTypesFile := filepath.Join(luaTypesPath, luaTypesVersion)

	// Write tsconfig (include declarations/**/*.ts for type info: LuaClass, unpack, etc.)
	tsconfig := fmt.Sprintf(`{
	"compilerOptions": {
		"target": "ESNext",
		"lib": ["ESNext"],
		"strict": true,
		"skipLibCheck": true,
		"types": [%q, %q]
	},
	"include": ["*.ts", "declarations/**/*.ts"]
}`, langExtPath, luaTypesFile)
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		return nil, nil, err
	}

	// Transpile with exportAsGlobal (bare globals, no module wrapper) and
	// noLualibImport (suppress require("lualib_bundle") and cross-file imports).
	fs := bundled.WrapFS(cachedvfs.From(osvfs.FS()))
	configPath := tspath.ResolvePath(tmpDir, "tsconfig.json")
	configDir := tspath.GetDirectoryPath(configPath)
	host := compiler.NewCompilerHost(configDir, fs, bundled.LibPath(), nil, nil)

	configResult, diags := tsoptions.GetParsedCommandLineOfConfigFile(configPath, &core.CompilerOptions{}, nil, host, nil)
	if len(diags) > 0 {
		return nil, nil, fmt.Errorf("tsconfig parse errors: %d diagnostic(s)", len(diags))
	}

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         configResult,
		SingleThreaded: core.TSTrue,
		Host:           host,
	})
	program.BindSourceFiles()

	results, tsDiags := transpiler.TranspileProgramWithOptions(program, tmpDir, luaTarget, nil, transpiler.TranspileOptions{
		ExportAsGlobal: true,
		LuaLibImport:   transpiler.LuaLibImportNone,
	})
	if len(tsDiags) > 0 {
		var msgs []string
		for _, d := range tsDiags {
			msgs = append(msgs, fmt.Sprintf("[%d] %s", d.Code(), ast.Diagnostic_MessageKey(d)))
		}
		return nil, nil, fmt.Errorf("transpile diagnostics: %s", strings.Join(msgs, "; "))
	}

	// Build a map from exported name → which file provides it.
	exportToFile := map[string]int{}
	for i, r := range results {
		for _, e := range r.ExportedNames {
			exportToFile[e] = i
		}
	}

	// Report unresolved deps.
	var unresolvedDeps []string
	for _, r := range results {
		for _, dep := range r.LualibDeps {
			if _, ok := exportToFile[dep]; !ok {
				unresolvedDeps = append(unresolvedDeps, dep+" (from "+filepath.Base(r.FileName)+")")
			}
		}
	}
	if len(unresolvedDeps) > 0 {
		fmt.Fprintf(os.Stderr, "lualib bundle: %d unresolved deps: %v\n", len(unresolvedDeps), unresolvedDeps)
	}

	return results, exportToFile, nil
}

// BuildBundleFromSource transpiles the TSTL lualib TypeScript source files
// and assembles them into a lualib_bundle.lua.
func BuildBundleFromSource(lualibSrcDir, langExtPath, luaTypesPath string, luaTarget transpiler.LuaTarget, overrideDir string) (string, error) {
	results, exportToFile, err := transpileLualibSource(lualibSrcDir, langExtPath, luaTypesPath, luaTarget, overrideDir)
	if err != nil {
		return "", err
	}

	// Topological sort: files that provide deps must come before files that use them.
	ordered := topoSortResults(results, exportToFile)

	// Verify ordering: every dep should appear before its dependent
	orderedPos := map[string]int{} // export name → position in ordered
	for i, r := range ordered {
		for _, e := range r.ExportedNames {
			orderedPos[e] = i
		}
	}
	for i, r := range ordered {
		for _, dep := range r.LualibDeps {
			if pos, ok := orderedPos[dep]; ok && pos > i {
				fmt.Fprintf(os.Stderr, "lualib ordering violation: %s (pos %d) needs %s (pos %d)\n",
					filepath.Base(r.FileName), i, dep, pos)
			}
		}
	}

	// Assemble bundle
	var sb strings.Builder
	allExports := map[string]bool{}

	for _, r := range ordered {
		body := stripLuaComments(r.Lua)
		if body != "" {
			sb.WriteString(wrapFileBody(body, r.ExportedNames))
			sb.WriteString("\n")
		}
		for _, e := range r.ExportedNames {
			allExports[e] = true
		}
	}

	// Inject tslua-specific pure-Lua patches (Map/Set for-of fast paths) and
	// register their exports. Mirrors scripts/update-lualib.sh apply_patches.
	patches := stripLuaComments(string(patchesLua))
	if patches != "" {
		sb.WriteString(patches)
		sb.WriteString("\n")
		for _, m := range patchExportRe.FindAllStringSubmatch(patches, -1) {
			allExports[m[1]] = true
		}
	}

	// Append return table exporting all public names
	var exportNames []string
	for n := range allExports {
		exportNames = append(exportNames, n)
	}
	sort.Strings(exportNames)

	sb.WriteString("return {\n")
	for _, n := range exportNames {
		fmt.Fprintf(&sb, "  %s = %s,\n", n, n)
	}
	sb.WriteString("}\n")

	return sb.String(), nil
}

// BuildFeatureDataFromSource transpiles the TSTL lualib TypeScript source files
// and returns per-feature metadata for selective inlining.
func BuildFeatureDataFromSource(lualibSrcDir, langExtPath, luaTypesPath string, luaTarget transpiler.LuaTarget, overrideDir string) (*FeatureData, error) {
	results, exportToFile, err := transpileLualibSource(lualibSrcDir, langExtPath, luaTypesPath, luaTarget, overrideDir)
	if err != nil {
		return nil, err
	}

	// Build export→feature reverse map (export name → feature name derived from filename).
	exportToFeature := map[string]string{}
	for _, r := range results {
		feature := featureNameFromFile(r.FileName)
		for _, e := range r.ExportedNames {
			exportToFeature[e] = feature
		}
	}

	moduleInfo := make(ModuleInfo, len(results))
	featureCode := make(map[string]string, len(results))

	for _, r := range results {
		feature := featureNameFromFile(r.FileName)

		// Convert LualibDeps (export names) to feature-level dependencies.
		depSet := map[string]bool{}
		for _, dep := range r.LualibDeps {
			if depFeature, ok := exportToFeature[dep]; ok && depFeature != feature {
				depSet[depFeature] = true
			}
		}
		var deps []string
		for d := range depSet {
			deps = append(deps, d)
		}
		sort.Strings(deps)

		info := FeatureInfo{Exports: r.ExportedNames}
		if len(deps) > 0 {
			info.Dependencies = deps
		}
		moduleInfo[feature] = info

		body := stripLuaComments(r.Lua)
		featureCode[feature] = wrapFileBody(body, r.ExportedNames)
	}

	// Inject tslua-specific patches as a synthetic feature. Mirrors
	// scripts/update-lualib.sh apply_patches_to_features: one feature named
	// "TsluaIterators" carrying the Map/Set for-of helpers, depending on the
	// Map and Set features whose internal layout they read.
	patches := stripLuaComments(string(patchesLua))
	if patches != "" {
		var patchExports []string
		for _, m := range patchExportRe.FindAllStringSubmatch(patches, -1) {
			patchExports = append(patchExports, m[1])
		}
		if len(patchExports) > 0 {
			const patchFeature = "TsluaIterators"
			moduleInfo[patchFeature] = FeatureInfo{
				Exports:      patchExports,
				Dependencies: []string{"Map", "Set"},
			}
			featureCode[patchFeature] = patches
		}
	}

	// Validate: every dependency should map to a known feature.
	for feature, info := range moduleInfo {
		for _, dep := range info.Dependencies {
			if _, ok := moduleInfo[dep]; !ok {
				if _, ok := exportToFile[dep]; !ok {
					fmt.Fprintf(os.Stderr, "lualib feature %s: unresolved dependency %q\n", feature, dep)
				}
			}
		}
	}

	return &FeatureData{ModuleInfo: moduleInfo, FeatureCode: featureCode}, nil
}

// featureNameFromFile derives the feature name from a source file path
// (e.g. "/tmp/tslua-lualib-xxx/ArrayConcat.ts" → "ArrayConcat").
func featureNameFromFile(path string) string {
	base := filepath.Base(path)
	// Strip .lua or .ts extension
	for _, ext := range []string{".lua", ".ts"} {
		if strings.HasSuffix(base, ext) {
			return strings.TrimSuffix(base, ext)
		}
	}
	return base
}

// copyDeclFiles recursively copies .d.ts files from src to dst, preserving directory structure.
func copyDeclFiles(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, "declarations", rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if strings.HasSuffix(info.Name(), ".d.ts") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		}
		return nil
	})
}

func collectTSFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") || strings.HasSuffix(e.Name(), ".d.ts") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	return files, nil
}

// topoSortResults orders transpile results so that files providing lualib
// dependencies come before files that use them.
func topoSortResults(results []transpiler.TranspileResult, exportToFile map[string]int) []transpiler.TranspileResult {
	n := len(results)
	visited := make([]bool, n)
	visiting := make([]bool, n)
	var order []transpiler.TranspileResult

	var visit func(i int)
	visit = func(i int) {
		if visited[i] || visiting[i] {
			return
		}
		visiting[i] = true
		for _, dep := range results[i].LualibDeps {
			if j, ok := exportToFile[dep]; ok && j != i {
				visit(j)
			}
		}
		visited[i] = true
		visiting[i] = false
		order = append(order, results[i])
	}

	for i := range n {
		visit(i)
	}
	return order
}

// stripLuaComments removes all Lua comments (line and block) and collapses
// resulting blank lines. This keeps the lualib output small.
func stripLuaComments(lua string) string {
	var out strings.Builder
	prevBlank := false
	for _, line := range strings.Split(lua, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip pure comment lines (-- and --[[ block ]])
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		blank := trimmed == ""
		if blank && prevBlank {
			continue
		}
		prevBlank = blank
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}
