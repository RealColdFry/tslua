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

func TestDecodeVLQ(t *testing.T) {
	tests := []struct {
		seg    string
		values []int
	}{
		{"A", []int{0}},
		{"C", []int{1}},
		{"D", []int{-1}},
		{"K", []int{5}},
		{"L", []int{-5}},
		{"gB", []int{16}},
		{"hB", []int{-16}},
		{"AACA", []int{0, 0, 1, 0}},
	}
	for _, tt := range tests {
		got, err := decodeVLQSegment(tt.seg)
		if err != nil {
			t.Errorf("decodeVLQ(%q) error: %v", tt.seg, err)
			continue
		}
		if len(got) != len(tt.values) {
			t.Errorf("decodeVLQ(%q) = %v, want %v", tt.seg, got, tt.values)
			continue
		}
		for i := range got {
			if got[i] != tt.values[i] {
				t.Errorf("decodeVLQ(%q)[%d] = %d, want %d", tt.seg, i, got[i], tt.values[i])
			}
		}
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	g := NewGenerator("out.lua", "")
	src := g.AddSource("input.ts")
	g.SetSourceContent(src, "const x = 1;\nconst y = 2;")

	g.AddMapping(0, 0, src, 0, 0)
	g.AddMapping(0, 6, src, 0, 6)
	g.AddMapping(1, 0, src, 1, 0)
	g.AddMapping(1, 6, src, 1, 6)

	raw := g.RawSourceMap()
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if len(decoded) != 4 {
		t.Fatalf("got %d mappings, want 4", len(decoded))
	}

	want := []DecodedMapping{
		{GenLine: 0, GenCol: 0, SrcIdx: 0, SrcLine: 0, SrcCol: 0, NameIdx: 0, HasSource: true},
		{GenLine: 0, GenCol: 6, SrcIdx: 0, SrcLine: 0, SrcCol: 6, NameIdx: 0, HasSource: true},
		{GenLine: 1, GenCol: 0, SrcIdx: 0, SrcLine: 1, SrcCol: 0, NameIdx: 0, HasSource: true},
		{GenLine: 1, GenCol: 6, SrcIdx: 0, SrcLine: 1, SrcCol: 6, NameIdx: 0, HasSource: true},
	}
	for i, m := range decoded {
		if m != want[i] {
			t.Errorf("mapping[%d] = %+v, want %+v", i, m, want[i])
		}
	}
}

func TestDecodeWithNames(t *testing.T) {
	g := NewGenerator("out.lua", "")
	src := g.AddSource("input.ts")
	g.AddNamedMapping(0, 0, src, 0, 0, "foo")
	g.AddMapping(0, 10, src, 0, 10)
	g.AddNamedMapping(1, 0, src, 1, 0, "bar")

	raw := g.RawSourceMap()
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if len(decoded) != 3 {
		t.Fatalf("got %d mappings, want 3", len(decoded))
	}
	if !decoded[0].HasName || raw.Names[decoded[0].NameIdx] != "foo" {
		t.Errorf("mapping[0] name: got idx=%d hasName=%v, want foo", decoded[0].NameIdx, decoded[0].HasName)
	}
	if decoded[1].HasName {
		t.Errorf("mapping[1] should not have name")
	}
	if !decoded[2].HasName || raw.Names[decoded[2].NameIdx] != "bar" {
		t.Errorf("mapping[2] name: got idx=%d hasName=%v, want bar", decoded[2].NameIdx, decoded[2].HasName)
	}
}

func TestVLQRoundTrip(t *testing.T) {
	// Encode then decode a range of values including large ones.
	values := []int{0, 1, -1, 15, -15, 16, -16, 100, -100, 1000, -1000, 65536, -65536}
	for _, v := range values {
		g := &Generator{}
		g.appendVLQ(v)
		seg := g.mappings.String()
		decoded, err := decodeVLQSegment(seg)
		if err != nil {
			t.Errorf("VLQ round-trip(%d): decode error: %v", v, err)
			continue
		}
		if len(decoded) != 1 || decoded[0] != v {
			t.Errorf("VLQ round-trip(%d): encoded %q, decoded %v", v, seg, decoded)
		}
	}
}

func TestDecodeEmptyMappings(t *testing.T) {
	raw := &RawSourceMap{
		Version:  3,
		Sources:  []string{"a.ts"},
		Names:    nil,
		Mappings: "",
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("expected 0 mappings, got %d", len(decoded))
	}
}

func TestDecodeConsecutiveSemicolons(t *testing.T) {
	// ";;AACA" = empty line 0, empty line 1, one mapping on line 2
	g := NewGenerator("out.lua", "")
	src := g.AddSource("input.ts")
	g.AddMapping(2, 0, src, 1, 0)

	raw := g.RawSourceMap()
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("got %d mappings, want 1", len(decoded))
	}
	if decoded[0].GenLine != 2 {
		t.Errorf("genLine = %d, want 2", decoded[0].GenLine)
	}
	if decoded[0].SrcLine != 1 {
		t.Errorf("srcLine = %d, want 1", decoded[0].SrcLine)
	}
}

func TestDecodeMultipleSourcesRoundTrip(t *testing.T) {
	g := NewGenerator("bundle.lua", "")
	src0 := g.AddSource("a.ts")
	src1 := g.AddSource("b.ts")

	g.AddMapping(0, 0, src0, 0, 0)
	g.AddMapping(0, 10, src0, 0, 10)
	g.AddMapping(1, 0, src1, 5, 3)
	g.AddMapping(1, 8, src1, 5, 11)
	g.AddMapping(2, 0, src0, 2, 0)

	raw := g.RawSourceMap()
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(decoded) != 5 {
		t.Fatalf("got %d mappings, want 5", len(decoded))
	}

	// Verify source indices
	if decoded[0].SrcIdx != 0 || decoded[1].SrcIdx != 0 {
		t.Errorf("line 0 mappings should reference src0")
	}
	if decoded[2].SrcIdx != 1 || decoded[3].SrcIdx != 1 {
		t.Errorf("line 1 mappings should reference src1")
	}
	if decoded[4].SrcIdx != 0 {
		t.Errorf("line 2 mapping should reference src0")
	}

	// Verify positions
	if decoded[2].SrcLine != 5 || decoded[2].SrcCol != 3 {
		t.Errorf("decoded[2] src = %d:%d, want 5:3", decoded[2].SrcLine, decoded[2].SrcCol)
	}
	if decoded[3].SrcLine != 5 || decoded[3].SrcCol != 11 {
		t.Errorf("decoded[3] src = %d:%d, want 5:11", decoded[3].SrcLine, decoded[3].SrcCol)
	}
}

func TestDecodeVLQErrors(t *testing.T) {
	// Invalid base64 character
	_, err := decodeVLQSegment("!")
	if err == nil {
		t.Error("expected error for invalid base64 char '!'")
	}

	// Truncated continuation (continuation bit set, no following char)
	_, err = decodeVLQSegment("g")
	if err == nil {
		t.Error("expected error for truncated VLQ 'g' (continuation bit set)")
	}
}

func TestDecodeJSONInvalid(t *testing.T) {
	_, _, err := DecodeJSON("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMappingForName(t *testing.T) {
	g := NewGenerator("out.lua", "")
	src := g.AddSource("input.ts")
	g.AddMapping(0, 0, src, 0, 0)
	g.AddNamedMapping(0, 10, src, 0, 10, "alpha")
	g.AddNamedMapping(1, 0, src, 1, 0, "beta")
	g.AddMapping(1, 5, src, 1, 5)

	raw := g.RawSourceMap()
	decoded, _ := Decode(raw)

	m := MappingForName(raw, decoded, "alpha")
	if !m.HasName || m.GenCol != 10 {
		t.Errorf("MappingForName(alpha): got genCol=%d hasName=%v, want genCol=10", m.GenCol, m.HasName)
	}

	m = MappingForName(raw, decoded, "beta")
	if !m.HasName || m.GenLine != 1 || m.GenCol != 0 {
		t.Errorf("MappingForName(beta): got gen=%d:%d hasName=%v", m.GenLine, m.GenCol, m.HasName)
	}

	m = MappingForName(raw, decoded, "missing")
	if m.HasName {
		t.Error("MappingForName(missing) should return HasName=false")
	}
}

func TestOriginalPositionFor(t *testing.T) {
	g := NewGenerator("out.lua", "")
	src := g.AddSource("input.ts")
	g.AddMapping(0, 0, src, 0, 0)
	g.AddMapping(0, 10, src, 0, 20)
	g.AddMapping(2, 4, src, 5, 8)

	raw := g.RawSourceMap()
	decoded, _ := Decode(raw)

	// Exact match
	m := OriginalPositionFor(decoded, 0, 0)
	if m.SrcLine != 0 || m.SrcCol != 0 {
		t.Errorf("exact match at 0,0: got %d,%d want 0,0", m.SrcLine, m.SrcCol)
	}

	// Column after a mapping (should find preceding)
	m = OriginalPositionFor(decoded, 0, 5)
	if m.SrcLine != 0 || m.SrcCol != 0 {
		t.Errorf("between 0,0 and 0,10: got src %d,%d want 0,0", m.SrcLine, m.SrcCol)
	}
	m = OriginalPositionFor(decoded, 0, 15)
	if m.SrcLine != 0 || m.SrcCol != 20 {
		t.Errorf("after 0,10: got src %d,%d want 0,20", m.SrcLine, m.SrcCol)
	}

	// No mapping on line 1
	m = OriginalPositionFor(decoded, 1, 0)
	if m.HasSource {
		t.Errorf("line 1 should have no mapping")
	}

	// Line 2
	m = OriginalPositionFor(decoded, 2, 4)
	if m.SrcLine != 5 || m.SrcCol != 8 {
		t.Errorf("line 2,4: got src %d,%d want 5,8", m.SrcLine, m.SrcCol)
	}
}
