package transpiler

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BundleOptions configures Lua bundle generation.
type BundleOptions struct {
	EntryModule string    // module name for the entry point (e.g., "main")
	LuaTarget   LuaTarget // needed for Lua 5.0 vs 5.1+ require shim variant
}

// BundleProgram takes transpiled per-file results and produces a single bundled Lua string.
// sourceRoot is used to convert TranspileResult.FileName to dot-separated module names.
// If lualibContent is non-empty, it is included as a "lualib_bundle" module entry.
func BundleProgram(results []TranspileResult, sourceRoot string, lualibContent []byte, opts BundleOptions) (string, error) {
	if opts.EntryModule == "" {
		return "", fmt.Errorf("BundleOptions.EntryModule is required")
	}

	var b strings.Builder

	// Require shim
	b.WriteString(requireShim(opts.LuaTarget))
	b.WriteString("\n")

	// Module table
	b.WriteString("____modules = {\n")

	// Add lualib as a module if the caller provided it
	if len(lualibContent) > 0 {
		b.WriteString("[\"lualib_bundle\"] = function(...) \n")
		b.Write(lualibContent)
		b.WriteString("\n end,\n")
	}

	for _, r := range results {
		modName := ModuleNameFromPath(r.FileName, sourceRoot)
		b.WriteString("[\"")
		b.WriteString(modName)
		b.WriteString("\"] = function(...) \n")
		b.WriteString(r.Lua)
		b.WriteString(" end,\n")
	}

	b.WriteString("}\n")

	// Entry point call
	// Uses a local to avoid tail-call optimization removing the bundle's stack frame.
	if opts.LuaTarget == LuaTargetLua50 {
		fmt.Fprintf(&b, "local ____entry = require(\"%s\", unpack(arg == nil and {} or arg))\n", opts.EntryModule)
	} else {
		fmt.Fprintf(&b, "return require(\"%s\", ...)\n", opts.EntryModule)
	}
	if opts.LuaTarget == LuaTargetLua50 {
		b.WriteString("return ____entry\n")
	}

	return b.String(), nil
}

// ModuleNameFromPath converts an absolute .ts file path to a dot-separated module name
// relative to sourceRoot. This matches the logic in resolveModulePath (modules.go).
func ModuleNameFromPath(filePath, sourceRoot string) string {
	rel, err := filepath.Rel(sourceRoot, filePath)
	if err != nil {
		rel = filepath.Base(filePath)
	}
	rel = strings.TrimSuffix(rel, ".d.ts")
	rel = strings.TrimSuffix(rel, ".ts")
	rel = strings.TrimSuffix(rel, ".tsx")
	rel = strings.TrimSuffix(rel, ".json")
	rel = filepath.ToSlash(rel)
	return strings.ReplaceAll(rel, "/", ".")
}

// requireShim returns the Lua code for the bundle's custom require() function.
func requireShim(target LuaTarget) string {
	var varargCheck, varargPass string
	if target == LuaTargetLua50 {
		varargCheck = "table.getn(arg) > 0"
		varargPass = "module(unpack(arg))"
	} else {
		varargCheck = "select(\"#\", ...) > 0"
		varargPass = "module(...)"
	}

	return fmt.Sprintf(`local ____modules = {}
local ____moduleCache = {}
local ____originalRequire = require
local function require(file, ...)
    if ____moduleCache[file] then
        return ____moduleCache[file].value
    end
    if ____modules[file] then
        local module = ____modules[file]
        local value = nil
        if (%s) then value = %s else value = module(file) end
        ____moduleCache[file] = { value = value }
        return value
    else
        if ____originalRequire then
            return ____originalRequire(file)
        else
            error("module '" .. file .. "' not found")
        end
    end
end
`, varargCheck, varargPass)
}
