package lualib

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed lualib_bundle.lua
var Bundle []byte

//go:embed lualib_bundle_50.lua
var Bundle50 []byte

//go:embed features
var featuresFS embed.FS

//go:embed features_50
var features50FS embed.FS

//go:embed lualib_module_info.json
var moduleInfoJSON []byte

//go:embed lualib_module_info_50.json
var moduleInfo50JSON []byte

//go:embed patches.lua
var patchesLua []byte

//go:embed middleclass/middleclass.lua
var middleclassSource []byte

// Middleclass returns the embedded kikito/middleclass library source. Used by
// the ClassStyleMiddleclass emit path; the transpiler injects
// `require("middleclass")`, and tooling (playground, future bundling) can
// resolve that require against this source. MIT-licensed; see
// internal/lualib/middleclass/MIT-LICENSE.txt.
func Middleclass() []byte { return middleclassSource }

// Patches returns the tslua-specific pure-Lua helpers that have no TS source
// (Map/Set for-of fast paths). BuildBundleFromSource and
// BuildFeatureDataFromSource fold these in when producing bundles, so they
// are present in both the committed embedded bundle and any on-demand
// rebuild.
func Patches() []byte { return patchesLua }

// BundleForTarget returns the appropriate lualib bundle for the given target.
// The embedded Bundle / Bundle50 are produced by BuildBundleFromSource at
// development time (via `just update-lualib`). TestCommittedBundleUpToDate
// enforces that they stay byte-equivalent to a fresh rebuild from TS sources.
func BundleForTarget(target string) []byte {
	if target == "5.0" {
		return Bundle50
	}
	return Bundle
}

// MinimalBundleForTarget returns a slim lualib_bundle.lua containing only the
// features needed by usedExports (plus their transitive deps), matching TSTL's
// luaLibImport=require-minimal output.
func MinimalBundleForTarget(target string, usedExports []string) ([]byte, error) {
	data, err := FeatureDataForTarget(target)
	if err != nil {
		return nil, err
	}
	return []byte(data.ResolveMinimalBundle(usedExports)), nil
}

var (
	featureDataOnce   sync.Once
	featureData       *FeatureData
	featureData50Once sync.Once
	featureData50     *FeatureData
)

// FeatureDataForTarget returns per-feature lualib data for selective inlining.
func FeatureDataForTarget(target string) (*FeatureData, error) {
	if target == "5.0" {
		return loadFeatureData50()
	}
	return loadFeatureData()
}

func loadFeatureData() (*FeatureData, error) {
	var err error
	featureDataOnce.Do(func() {
		featureData, err = parseFeatureData(moduleInfoJSON, featuresFS, "features")
	})
	return featureData, err
}

func loadFeatureData50() (*FeatureData, error) {
	var err error
	featureData50Once.Do(func() {
		featureData50, err = parseFeatureData(moduleInfo50JSON, features50FS, "features_50")
	})
	return featureData50, err
}

func parseFeatureData(infoJSON []byte, fsys embed.FS, dir string) (*FeatureData, error) {
	var moduleInfo ModuleInfo
	if err := json.Unmarshal(infoJSON, &moduleInfo); err != nil {
		return nil, err
	}

	featureCode := make(map[string]string)
	err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".lua") {
			return nil
		}
		data, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.Base(path), ".lua")
		featureCode[name] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &FeatureData{ModuleInfo: moduleInfo, FeatureCode: featureCode}, nil
}
