package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// tsluaConfig holds tslua-specific options parsed from the "tstl" or "tslua" section of tsconfig.json.
type tsluaConfig struct {
	ExportAsGlobal      any    `json:"exportAsGlobal"` // bool or string (regex pattern for file matching)
	exportAsGlobalBool  bool   // resolved: apply to all files
	exportAsGlobalMatch string // resolved: regex pattern for selective application
}

// parseTsluaConfig reads the "tstl" or "tslua" section from a tsconfig.json file.
// Both section names are accepted, but specifying both is an error.
// Returns nil config (no error) if neither section is present.
func parseTsluaConfig(tsconfigPath string) (*tsluaConfig, error) {
	data, err := os.ReadFile(tsconfigPath)
	if err != nil {
		return nil, nil // missing file is not an error here; tsgo handles that
	}

	var raw struct {
		Tstl  *tsluaConfig `json:"tstl"`
		Tslua *tsluaConfig `json:"tslua"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil // let tsgo report JSON errors
	}

	if raw.Tstl != nil && raw.Tslua != nil {
		return nil, fmt.Errorf("tsconfig.json contains both \"tstl\" and \"tslua\" sections; use one or the other")
	}

	cfg := raw.Tstl
	if cfg == nil {
		cfg = raw.Tslua
	}
	if cfg == nil {
		return nil, nil
	}

	switch v := cfg.ExportAsGlobal.(type) {
	case bool:
		cfg.exportAsGlobalBool = v
	case string:
		cfg.exportAsGlobalMatch = v
	case nil:
		// not set
	default:
		return nil, fmt.Errorf("exportAsGlobal must be a boolean or regex string, got %T", v)
	}

	return cfg, nil
}
