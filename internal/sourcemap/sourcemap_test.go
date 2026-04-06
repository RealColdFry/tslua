package sourcemap

import (
	"encoding/json"
	"testing"
)

func TestVLQEncoding(t *testing.T) {
	tests := []struct {
		value    int
		expected string
	}{
		{0, "A"},
		{1, "C"},
		{-1, "D"},
		{5, "K"},
		{-5, "L"},
		{15, "e"},
		{16, "gB"},
		{-16, "hB"},
	}
	for _, tt := range tests {
		g := &Generator{}
		g.appendVLQ(tt.value)
		got := g.mappings.String()
		if got != tt.expected {
			t.Errorf("VLQ(%d) = %q, want %q", tt.value, got, tt.expected)
		}
	}
}

func TestBasicSourceMap(t *testing.T) {
	g := NewGenerator("output.lua", "")
	srcIdx := g.AddSource("input.ts")
	g.SetSourceContent(srcIdx, "const x = 1;")

	// Map generated line 0, col 0 → source line 0, col 0
	g.AddMapping(0, 0, srcIdx, 0, 0)
	// Map generated line 0, col 6 → source line 0, col 6
	g.AddMapping(0, 6, srcIdx, 0, 6)
	// Map generated line 1, col 0 → source line 1, col 0
	g.AddMapping(1, 0, srcIdx, 1, 0)

	raw := g.RawSourceMap()
	if raw.Version != 3 {
		t.Errorf("version = %d, want 3", raw.Version)
	}
	if raw.File != "output.lua" {
		t.Errorf("file = %q, want %q", raw.File, "output.lua")
	}
	if len(raw.Sources) != 1 || raw.Sources[0] != "input.ts" {
		t.Errorf("sources = %v, want [input.ts]", raw.Sources)
	}
	if raw.SourcesContent == nil || *raw.SourcesContent[0] != "const x = 1;" {
		t.Errorf("unexpected sourcesContent")
	}

	// Verify the JSON is valid
	jsonStr := g.String()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["version"].(float64) != 3 {
		t.Error("JSON version != 3")
	}
}

func TestMultipleSources(t *testing.T) {
	g := NewGenerator("bundle.lua", "")
	src0 := g.AddSource("a.ts")
	src1 := g.AddSource("b.ts")

	g.AddMapping(0, 0, src0, 0, 0)
	g.AddMapping(5, 0, src1, 0, 0)

	raw := g.RawSourceMap()
	if len(raw.Sources) != 2 {
		t.Errorf("sources count = %d, want 2", len(raw.Sources))
	}
	// Verify dedup
	dup := g.AddSource("a.ts")
	if dup != src0 {
		t.Errorf("duplicate source got index %d, want %d", dup, src0)
	}
}
