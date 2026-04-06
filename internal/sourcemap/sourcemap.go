// Package sourcemap implements V3 source map generation.
// Reference: https://sourcemaps.info/spec.html
package sourcemap

import (
	"encoding/json"
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
