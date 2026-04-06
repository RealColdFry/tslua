// Package lualibinfo defines types shared between the lualib and transpiler packages.
package lualibinfo

import "strings"

// FeatureInfo describes a single lualib feature (one source file).
type FeatureInfo struct {
	Exports      []string `json:"exports"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// ModuleInfo maps feature name (e.g. "ArrayConcat") to its info.
type ModuleInfo map[string]FeatureInfo

// FeatureData holds all per-feature data needed for selective inlining.
type FeatureData struct {
	ModuleInfo  ModuleInfo        // feature → {exports, deps}
	FeatureCode map[string]string // feature name → Lua code
}

// ResolveInlineCode takes a list of used export names (from the transpiler's
// lualibs list), resolves them to features via the module info, computes
// transitive dependencies in topological order, and returns the concatenated
// Lua code for only the needed features.
func (d *FeatureData) ResolveInlineCode(usedExports []string) string {
	// Build export→feature reverse map.
	exportToFeature := make(map[string]string)
	for feature, info := range d.ModuleInfo {
		for _, exp := range info.Exports {
			exportToFeature[exp] = feature
		}
	}

	// Collect the set of directly-used features.
	needed := make(map[string]bool)
	for _, exp := range usedExports {
		if feature, ok := exportToFeature[exp]; ok {
			needed[feature] = true
		}
	}

	// Resolve transitive dependencies (DFS, deps before dependents).
	var resolved []string
	visited := make(map[string]bool)
	var visit func(feature string)
	visit = func(feature string) {
		if visited[feature] {
			return
		}
		visited[feature] = true
		if info, ok := d.ModuleInfo[feature]; ok {
			for _, dep := range info.Dependencies {
				visit(dep)
			}
		}
		resolved = append(resolved, feature)
	}
	for feature := range needed {
		visit(feature)
	}

	// Concatenate Lua code in dependency order.
	var sb strings.Builder
	for _, feature := range resolved {
		code := d.FeatureCode[feature]
		if code != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(code)
		}
	}
	return sb.String()
}

// ExportFeatureMap builds a reverse map from export name to feature name.
// Used by the transpiler to derive lualibFeatureExports from module info.
func (d *FeatureData) ExportFeatureMap() map[string]string {
	m := make(map[string]string)
	for feature, info := range d.ModuleInfo {
		for _, exp := range info.Exports {
			m[exp] = feature
		}
	}
	return m
}
