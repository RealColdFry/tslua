// Package lualibinfo defines types shared between the lualib and transpiler packages.
package lualibinfo

import (
	"slices"
	"strings"
)

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
	// Sort features for deterministic output order.
	sortedNeeded := make([]string, 0, len(needed))
	for feature := range needed {
		sortedNeeded = append(sortedNeeded, feature)
	}
	slices.Sort(sortedNeeded)
	for _, feature := range sortedNeeded {
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

// ResolveMinimalBundle returns a slim lualib_bundle.lua body containing only the
// features whose exports are in usedExports, their transitive dependencies, and
// a trailing `return { ... }` table listing only the directly-used features'
// exports (matching TSTL's buildMinimalLualibBundle behavior).
//
// Transitive deps live as file-scope locals inside the bundle, so features can
// still reference each other, but they are not re-exported through the table.
func (d *FeatureData) ResolveMinimalBundle(usedExports []string) string {
	body := d.ResolveInlineCode(usedExports)

	exportToFeature := d.ExportFeatureMap()
	directFeatures := make(map[string]bool)
	for _, exp := range usedExports {
		if feature, ok := exportToFeature[exp]; ok {
			directFeatures[feature] = true
		}
	}

	var footerExports []string
	seen := make(map[string]bool)
	sortedFeatures := make([]string, 0, len(directFeatures))
	for feature := range directFeatures {
		sortedFeatures = append(sortedFeatures, feature)
	}
	slices.Sort(sortedFeatures)
	for _, feature := range sortedFeatures {
		for _, exp := range d.ModuleInfo[feature].Exports {
			if !seen[exp] {
				seen[exp] = true
				footerExports = append(footerExports, exp)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(body)
	sb.WriteString("\nreturn {\n")
	for i, exp := range footerExports {
		sb.WriteString("  ")
		sb.WriteString(exp)
		sb.WriteString(" = ")
		sb.WriteString(exp)
		if i < len(footerExports)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("}\n")
	return sb.String()
}
