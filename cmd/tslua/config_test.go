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

func TestParseTsluaConfig_MissingFile(t *testing.T) {
	cfg, err := parseTsluaConfig("/nonexistent/tsconfig.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Error("expected nil config for missing file")
	}
}
