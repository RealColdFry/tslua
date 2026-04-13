package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// tsluaConfig holds tslua-specific options parsed from the "tstl" or "tslua" section of tsconfig.json.
type tsluaConfig struct {
	ExportAsGlobal      any    `json:"exportAsGlobal"` // bool or string (regex pattern for file matching)
	exportAsGlobalBool  bool   // resolved: apply to all files
	exportAsGlobalMatch string // resolved: regex pattern for selective application
	ClassStyle          string `json:"classStyle"`

	// Options also available as CLI flags (CLI wins when explicitly set).
	LuaTarget                 string   `json:"luaTarget"`
	NoImplicitSelf            *bool    `json:"noImplicitSelf"`
	NoImplicitGlobalVariables *bool    `json:"noImplicitGlobalVariables"`
	EmitMode                  string   `json:"emitMode"`
	LuaLibImport              string   `json:"luaLibImport"`
	NoHeader                  *bool    `json:"noHeader"`
	LuaBundle                 string   `json:"luaBundle"`
	LuaBundleEntry            string   `json:"luaBundleEntry"`
	SourceMapTraceback        *bool    `json:"sourceMapTraceback"`
	BuildMode                 string   `json:"buildMode"`
	NoResolvePaths            []string `json:"noResolvePaths"`
}

// parseTsluaConfig reads the "tstl" or "tslua" section from a tsconfig.json file.
// Both section names are accepted, but specifying both is an error.
// Returns nil config (no error) if neither section is present.
func parseTsluaConfig(tsconfigPath string) (*tsluaConfig, error) {
	data, err := os.ReadFile(tsconfigPath)
	if err != nil {
		return nil, nil // missing file is not an error here; tsgo handles that
	}

	data = stripJSONC(data)

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

// stripJSONC converts JSONC (as used by tsconfig.json) to valid JSON by
// removing single-line comments (//) and trailing commas before } or ].
// It respects quoted strings (won't strip "//" inside "https://...").
var jsoncTrailingComma = regexp.MustCompile(`,\s*([}\]])`)

func stripJSONC(data []byte) []byte {
	// Remove // line comments that aren't inside strings.
	var out []byte
	inString := false
	i := 0
	for i < len(data) {
		ch := data[i]
		if inString {
			out = append(out, ch)
			if ch == '\\' && i+1 < len(data) {
				i++
				out = append(out, data[i])
			} else if ch == '"' {
				inString = false
			}
		} else {
			if ch == '"' {
				inString = true
				out = append(out, ch)
			} else if ch == '/' && i+1 < len(data) && data[i+1] == '/' {
				// Skip to end of line.
				for i < len(data) && data[i] != '\n' {
					i++
				}
				continue
			} else {
				out = append(out, ch)
			}
		}
		i++
	}
	out = jsoncTrailingComma.ReplaceAll(out, []byte("$1"))
	return out
}
