package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTsluaConfig_TsluaSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"compilerOptions":{},"tslua":{"exportAsGlobal":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || !cfg.exportAsGlobalBool {
		t.Error("expected exportAsGlobalBool=true from tslua section")
	}
}

func TestParseTsluaConfig_TstlSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"compilerOptions":{},"tstl":{"exportAsGlobal":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || !cfg.exportAsGlobalBool {
		t.Error("expected exportAsGlobalBool=true from tstl section")
	}
}

func TestParseTsluaConfig_BothSectionsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"tstl":{"exportAsGlobal":true},"tslua":{"exportAsGlobal":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := parseTsluaConfig(path)
	if err == nil {
		t.Fatal("expected error when both sections present")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("error should mention both sections: %s", err)
	}
}

func TestParseTsluaConfig_ExportAsGlobalRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"tstl":{"exportAsGlobal":"\\.script\\.ts$"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected config")
		return
	}
	if cfg.exportAsGlobalBool {
		t.Error("expected exportAsGlobalBool=false")
	}
	if cfg.exportAsGlobalMatch != `\.script\.ts$` {
		t.Errorf("expected regex pattern, got %q", cfg.exportAsGlobalMatch)
	}
}

func TestParseTsluaConfig_NoSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"compilerOptions":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Error("expected nil config when no section present")
	}
}

func TestParseTsluaConfig_ClassStyle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"tstl":{"classStyle":"luabind"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected config")
		return
	}
	if cfg.ClassStyle != "luabind" {
		t.Errorf("expected classStyle=luabind, got %q", cfg.ClassStyle)
	}
}

func TestParseTsluaConfig_LuaTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"tstl":{"luaTarget":"5.3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.LuaTarget != "5.3" {
		t.Errorf("expected luaTarget=5.3, got %q", cfg.LuaTarget)
	}
}

func TestParseTsluaConfig_NoImplicitSelf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"tstl":{"noImplicitSelf":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.NoImplicitSelf == nil || !*cfg.NoImplicitSelf {
		t.Error("expected noImplicitSelf=true")
	}
}

func TestParseTsluaConfig_NoImplicitSelfFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"tstl":{"noImplicitSelf":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.NoImplicitSelf == nil || *cfg.NoImplicitSelf {
		t.Error("expected noImplicitSelf=false (explicit)")
	}
}

func TestParseTsluaConfig_JSONC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	// tsconfig.json with trailing commas and // comments (standard JSONC)
	content := `{
  "compilerOptions": {
    "outDir": "build/", // trailing comma
  },
  "tstl": {
    "luaTarget": "universal",
    "noImplicitSelf": true, // trailing comma
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
		return
	}
	if cfg.LuaTarget != "universal" {
		t.Errorf("expected luaTarget=universal, got %q", cfg.LuaTarget)
	}
	if cfg.NoImplicitSelf == nil || !*cfg.NoImplicitSelf {
		t.Error("expected noImplicitSelf=true")
	}
}

func TestParseTsluaConfig_JSONC_SchemaURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	// $schema contains "https://" which must not be treated as a // comment
	content := `{
  "$schema": "https://raw.githubusercontent.com/TypeScriptToLua/TypeScriptToLua/master/tsconfig-schema.json",
  "tstl": {
    "luaTarget": "JIT",
    "luaBundle": "control.lua",
    "luaBundleEntry": "src/control.ts"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTsluaConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
		return
	}
	if cfg.LuaTarget != "JIT" {
		t.Errorf("expected luaTarget=JIT, got %q", cfg.LuaTarget)
	}
	if cfg.LuaBundle != "control.lua" {
		t.Errorf("expected luaBundle=control.lua, got %q", cfg.LuaBundle)
	}
}

func TestParseTsluaConfig_MissingFile(t *testing.T) {
	cfg, err := parseTsluaConfig("/nonexistent/tsconfig.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Error("expected nil config for missing file")
	}
}
