package transpiler

import "regexp"

// Ported from: src/LuaLib.ts findUsedLualibFeatures

// lualibRefRe matches lines like:
//
//	local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf
//
// where the local name equals the export name.
var lualibRefRe = regexp.MustCompile(`(?m)^local (\w+) = ____lualib\.(\w+)$`)

// ScanLuaForLualibDeps scans Lua source code for ____lualib references and
// returns the export names found (e.g. "__TS__ArrayIndexOf").
func ScanLuaForLualibDeps(lua string) []string {
	matches := lualibRefRe.FindAllStringSubmatch(lua, -1)
	var deps []string
	for _, m := range matches {
		localName, exportName := m[1], m[2]
		if localName != exportName {
			continue
		}
		deps = append(deps, exportName)
	}
	return deps
}
