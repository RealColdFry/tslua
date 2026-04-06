// Package sourcemap implements V3 source map generation.
// Reference: https://sourcemaps.info/spec.html
package sourcemap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RawSourceMap is the JSON-serializable V3 source map structure.
type RawSourceMap struct {
	Version        int       `json:"version"`
	File           string    `json:"file"`
	SourceRoot     string    `json:"sourceRoot,omitempty"`
	Sources        []string  `json:"sources"`
	Names          []string  `json:"names"`
	Mappings       string    `json:"mappings"`
	SourcesContent []*string `json:"sourcesContent,omitempty"`
}

// Generator builds a V3 source map incrementally.
type Generator struct {
	file           string
	sourceRoot     string
	sources        []string
	sourceIndexMap map[string]int
	sourcesContent []*string
	names          []string
	nameIndexMap   map[string]int
	mappings       strings.Builder

	lastGenLine int
	lastGenCol  int
	lastSrcIdx  int
	lastSrcLine int
	lastSrcCol  int
	lastNameIdx int
	hasLast     bool

	pendingGenLine int
	pendingGenCol  int
	pendingSrcIdx  int
	pendingSrcLine int
	pendingSrcCol  int
	pendingNameIdx int
	hasPending     bool
	hasPendingSrc  bool
	hasPendingName bool
}

// NewGenerator creates a new source map generator for the given output file.
func NewGenerator(file, sourceRoot string) *Generator {
	return &Generator{
		file:       file,
		sourceRoot: sourceRoot,
	}
}

// AddSource registers a source file and returns its index.
func (g *Generator) AddSource(path string) int {
	if idx, ok := g.sourceIndexMap[path]; ok {
		return idx
	}
	idx := len(g.sources)
	g.sources = append(g.sources, path)
	if g.sourceIndexMap == nil {
		g.sourceIndexMap = make(map[string]int)
	}
	g.sourceIndexMap[path] = idx
	return idx
}

// SetSourceContent sets the original source content for a source index.
func (g *Generator) SetSourceContent(idx int, content string) {
	for len(g.sourcesContent) <= idx {
		g.sourcesContent = append(g.sourcesContent, nil)
	}
	g.sourcesContent[idx] = &content
}

// AddMapping adds a source mapping. All line/column values are 0-based.
func (g *Generator) AddMapping(genLine, genCol, srcIdx, srcLine, srcCol int) {
	if g.isNewGenPos(genLine, genCol) {
		g.commitPending()
		g.pendingGenLine = genLine
		g.pendingGenCol = genCol
		g.hasPendingSrc = false
		g.hasPendingName = false
		g.hasPending = true
	}
	g.pendingSrcIdx = srcIdx
	g.pendingSrcLine = srcLine
	g.pendingSrcCol = srcCol
	g.hasPendingSrc = true
}

// AddNamedMapping adds a source mapping with a name. All line/column values are 0-based.
func (g *Generator) AddNamedMapping(genLine, genCol, srcIdx, srcLine, srcCol int, name string) {
	g.AddMapping(genLine, genCol, srcIdx, srcLine, srcCol)
	nameIdx := g.addName(name)
	g.pendingNameIdx = nameIdx
	g.hasPendingName = true
}

func (g *Generator) addName(name string) int {
	if idx, ok := g.nameIndexMap[name]; ok {
		return idx
	}
	idx := len(g.names)
	g.names = append(g.names, name)
	if g.nameIndexMap == nil {
		g.nameIndexMap = make(map[string]int)
	}
	g.nameIndexMap[name] = idx
	return idx
}

func (g *Generator) isNewGenPos(line, col int) bool {
	return !g.hasPending || g.pendingGenLine != line || g.pendingGenCol != col
}

func (g *Generator) shouldCommit() bool {
	if !g.hasPending {
		return false
	}
	if !g.hasLast {
		return true
	}
	return g.lastGenLine != g.pendingGenLine ||
		g.lastGenCol != g.pendingGenCol ||
		g.lastSrcIdx != g.pendingSrcIdx ||
		g.lastSrcLine != g.pendingSrcLine ||
		g.lastSrcCol != g.pendingSrcCol ||
		g.lastNameIdx != g.pendingNameIdx
}

func (g *Generator) commitPending() {
	if !g.shouldCommit() {
		return
	}

	// Line/comma delimiters
	if g.lastGenLine < g.pendingGenLine {
		for g.lastGenLine < g.pendingGenLine {
			g.mappings.WriteByte(';')
			g.lastGenLine++
		}
		g.lastGenCol = 0
	} else if g.hasLast {
		g.mappings.WriteByte(',')
	}

	// 1. Relative generated column
	g.appendVLQ(g.pendingGenCol - g.lastGenCol)
	g.lastGenCol = g.pendingGenCol

	if g.hasPendingSrc {
		// 2. Relative source index
		g.appendVLQ(g.pendingSrcIdx - g.lastSrcIdx)
		g.lastSrcIdx = g.pendingSrcIdx

		// 3. Relative source line
		g.appendVLQ(g.pendingSrcLine - g.lastSrcLine)
		g.lastSrcLine = g.pendingSrcLine

		// 4. Relative source column
		g.appendVLQ(g.pendingSrcCol - g.lastSrcCol)
		g.lastSrcCol = g.pendingSrcCol

		if g.hasPendingName {
			// 5. Relative name index
			g.appendVLQ(g.pendingNameIdx - g.lastNameIdx)
			g.lastNameIdx = g.pendingNameIdx
		}
	}

	g.hasLast = true
}

const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func (g *Generator) appendVLQ(value int) {
	// Convert to VLQ signed representation: sign bit in LSB
	if value < 0 {
		value = ((-value) << 1) | 1
	} else {
		value = value << 1
	}
	for {
		digit := value & 0x1f
		value >>= 5
		if value > 0 {
			digit |= 0x20 // continuation bit
		}
		g.mappings.WriteByte(base64Chars[digit])
		if value <= 0 {
			break
		}
	}
}

// RawSourceMap returns the structured source map.
func (g *Generator) RawSourceMap() *RawSourceMap {
	g.commitPending()
	sources := make([]string, len(g.sources))
	copy(sources, g.sources)
	names := make([]string, len(g.names))
	copy(names, g.names)
	var sc []*string
	if len(g.sourcesContent) > 0 {
		sc = make([]*string, len(g.sourcesContent))
		copy(sc, g.sourcesContent)
	}
	return &RawSourceMap{
		Version:        3,
		File:           g.file,
		SourceRoot:     g.sourceRoot,
		Sources:        sources,
		Names:          names,
		Mappings:       g.mappings.String(),
		SourcesContent: sc,
	}
}

// String returns the JSON-encoded source map.
func (g *Generator) String() string {
	raw := g.RawSourceMap()
	b, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ============================================================================
// Decoder
// ============================================================================

// DecodedMapping is a single resolved source map mapping.
type DecodedMapping struct {
	GenLine   int // 0-based generated line
	GenCol    int // 0-based generated column
	SrcIdx    int // source index into Sources array
	SrcLine   int // 0-based original line
	SrcCol    int // 0-based original column
	NameIdx   int // name index into Names array (-1 if none)
	HasSource bool
	HasName   bool
}

// Decode parses a RawSourceMap into a slice of DecodedMappings.
func Decode(raw *RawSourceMap) ([]DecodedMapping, error) {
	var result []DecodedMapping
	if raw.Mappings == "" {
		return result, nil
	}

	var (
		genLine int
		genCol  int
		srcIdx  int
		srcLine int
		srcCol  int
		nameIdx int
	)

	mappings := raw.Mappings
	for genLine <= strings.Count(mappings, ";") || len(mappings) > 0 {
		// Find next line (semicolon) or end
		lineEnd := strings.IndexByte(mappings, ';')
		var lineStr string
		if lineEnd < 0 {
			lineStr = mappings
			mappings = ""
		} else {
			lineStr = mappings[:lineEnd]
			mappings = mappings[lineEnd+1:]
		}

		genCol = 0 // reset column at each new generated line

		if lineStr != "" {
			segments := strings.Split(lineStr, ",")
			for _, seg := range segments {
				if seg == "" {
					continue
				}
				values, err := decodeVLQSegment(seg)
				if err != nil {
					return nil, err
				}
				if len(values) < 1 {
					continue
				}

				m := DecodedMapping{GenLine: genLine}
				genCol += values[0]
				m.GenCol = genCol

				if len(values) >= 4 {
					srcIdx += values[1]
					srcLine += values[2]
					srcCol += values[3]
					m.SrcIdx = srcIdx
					m.SrcLine = srcLine
					m.SrcCol = srcCol
					m.HasSource = true
				}
				if len(values) >= 5 {
					nameIdx += values[4]
					m.NameIdx = nameIdx
					m.HasName = true
				}

				result = append(result, m)
			}
		}

		if lineEnd < 0 {
			break
		}
		genLine++
	}
	return result, nil
}

// DecodeJSON parses a JSON source map string into DecodedMappings.
func DecodeJSON(jsonStr string) (*RawSourceMap, []DecodedMapping, error) {
	var raw RawSourceMap
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, nil, err
	}
	mappings, err := Decode(&raw)
	if err != nil {
		return nil, nil, err
	}
	return &raw, mappings, nil
}

// OriginalPositionFor finds the mapping at or just before the given generated position.
// Returns a zero DecodedMapping with HasSource==false if no mapping found.
func OriginalPositionFor(mappings []DecodedMapping, genLine, genCol int) DecodedMapping {
	var best DecodedMapping
	found := false
	for _, m := range mappings {
		if !m.HasSource {
			continue
		}
		if m.GenLine == genLine && m.GenCol <= genCol {
			if !found || m.GenCol > best.GenCol {
				best = m
				found = true
			}
		}
	}
	return best
}

// MappingForName finds a mapping that has the given name in the Names array.
// Returns a zero DecodedMapping with HasName==false if not found.
func MappingForName(raw *RawSourceMap, mappings []DecodedMapping, name string) DecodedMapping {
	for _, m := range mappings {
		if m.HasName && m.NameIdx < len(raw.Names) && raw.Names[m.NameIdx] == name {
			return m
		}
	}
	return DecodedMapping{}
}

// decodeVLQSegment decodes a base64-VLQ segment into a slice of signed integers.
func decodeVLQSegment(seg string) ([]int, error) {
	var values []int
	i := 0
	for i < len(seg) {
		var value int
		var shift uint
		for {
			if i >= len(seg) {
				return nil, fmt.Errorf("sourcemap: unexpected end of VLQ at %q", seg)
			}
			c := seg[i]
			i++
			digit := base64Decode[c]
			if digit < 0 {
				return nil, fmt.Errorf("sourcemap: invalid base64 char %q in VLQ", c)
			}
			value |= (digit & 0x1f) << shift
			shift += 5
			if digit&0x20 == 0 {
				break
			}
		}
		// Sign bit is in the LSB
		if value&1 != 0 {
			value = -(value >> 1)
		} else {
			value = value >> 1
		}
		values = append(values, value)
	}
	return values, nil
}

// base64Decode maps base64 characters to their 6-bit values. -1 = invalid.
var base64Decode [256]int

func init() {
	for i := range base64Decode {
		base64Decode[i] = -1
	}
	for i, c := range base64Chars {
		base64Decode[c] = i
	}
}
