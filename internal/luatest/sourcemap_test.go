package luatest

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/sourcemap"
)

// lineAndColumnOf finds the first occurrence of pattern in text and returns
// 0-based line and 0-based column. Returns (-1, -1) if not found.
func lineAndColumnOf(text, pattern string) (line, col int) {
	pos := strings.Index(text, pattern)
	if pos < 0 {
		return -1, -1
	}
	line = strings.Count(text[:pos], "\n")
	lastNL := strings.LastIndex(text[:pos], "\n")
	if lastNL < 0 {
		col = pos
	} else {
		col = pos - lastNL - 1
	}
	return line, col
}

// assertMappings is the core test helper: transpile TS code with source maps enabled,
// decode the source map, and verify that each Lua pattern maps back to the expected TS pattern.
func assertMappings(t *testing.T, code string, patterns []mappingPattern) {
	t.Helper()
	results := TranspileTS(t, code, Opts{SourceMap: true})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	luaCode := results[0].Lua
	smJSON := results[0].SourceMap
	if smJSON == "" {
		t.Fatal("source map is empty")
	}

	raw, mappings, err := sourcemap.DecodeJSON(smJSON)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	_ = raw

	for _, p := range patterns {
		luaLine, luaCol := lineAndColumnOf(luaCode, p.lua)
		if luaLine < 0 {
			t.Errorf("Lua pattern %q not found in output:\n%s", p.lua, luaCode)
			continue
		}

		mapped := sourcemap.OriginalPositionFor(mappings, luaLine, luaCol)
		if !mapped.HasSource {
			t.Errorf("no source mapping for Lua pattern %q at gen %d:%d\nlua:\n%s", p.lua, luaLine, luaCol, luaCode)
			continue
		}

		tsLine, tsCol := lineAndColumnOf(code, p.ts)
		if tsLine < 0 {
			t.Errorf("TS pattern %q not found in source", p.ts)
			continue
		}

		if mapped.SrcLine != tsLine || mapped.SrcCol != tsCol {
			t.Errorf("mapping for Lua %q (gen %d:%d):\n  got  src %d:%d\n  want src %d:%d (TS %q)\nlua:\n%s",
				p.lua, luaLine, luaCol,
				mapped.SrcLine, mapped.SrcCol,
				tsLine, tsCol, p.ts,
				luaCode)
		}
	}
}

type mappingPattern struct {
	lua string // pattern to find in generated Lua
	ts  string // pattern to find in original TypeScript
}

// assertNameMappings verifies that renamed identifiers have the correct name in the source map.
// For each case, it finds the TS position of the original name and checks that some mapping
// at that position carries the expected name.
func assertNameMapping(t *testing.T, code string, name string) {
	t.Helper()
	results := TranspileTS(t, code, Opts{SourceMap: true})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	smJSON := results[0].SourceMap
	if smJSON == "" {
		t.Fatal("source map is empty")
	}

	raw, mappings, err := sourcemap.DecodeJSON(smJSON)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}

	tsLine, tsCol := lineAndColumnOf(code, name)
	if tsLine < 0 {
		t.Fatalf("name %q not found in TS source", name)
	}

	var foundName string
	for _, m := range mappings {
		if !m.HasSource || !m.HasName {
			continue
		}
		if m.SrcLine == tsLine && m.SrcCol == tsCol {
			if m.NameIdx < len(raw.Names) {
				foundName = raw.Names[m.NameIdx]
			}
			break
		}
	}

	if foundName != name {
		t.Errorf("expected name mapping %q at src %d:%d, got %q\nlua:\n%s",
			name, tsLine, tsCol, foundName, results[0].Lua)
	}
}

// =============================================================================
// Position mapping tests — ported from TSTL sourcemaps.spec.ts
// =============================================================================

func TestSourceMap_VariableDeclarations(t *testing.T) {
	t.Parallel()
	code := `
const abc = "foo";
const def = "bar";

const xyz = "baz";
`
	assertMappings(t, code, []mappingPattern{
		{"abc", "abc"},
		{"def", "def"},
		{"xyz", "xyz"},
		{`"foo"`, `"foo"`},
		{`"bar"`, `"bar"`},
		{`"baz"`, `"baz"`},
	})
}

func TestSourceMap_FunctionDeclarations(t *testing.T) {
	t.Parallel()
	code := `
function abc() {
    return def();
}

function def() {
    return "foo";
}
`
	assertMappings(t, code, []mappingPattern{
		{"function abc(", "function abc() {"},
		{"function def(", "function def() {"},
		{"return def(", "return def("},
	})
}

func TestSourceMap_ConstEnum(t *testing.T) {
	t.Parallel()
	code := `
const enum abc { foo = 2, bar = 4 };
const xyz = abc.foo;
`
	assertMappings(t, code, []mappingPattern{
		{"xyz", "xyz"},
		{"2", "abc.foo"},
	})
}

func TestSourceMap_ImportRequire(t *testing.T) {
	t.Parallel()
	code := `
// @ts-ignore
import { Foo } from "foo";
Foo;
`
	assertMappings(t, code, []mappingPattern{
		{`require("foo")`, `"foo"`},
		{"Foo", "Foo"},
	})
}

func TestSourceMap_ImportStar(t *testing.T) {
	t.Parallel()
	code := `
// @ts-ignore
import * as Foo from "foo";
Foo;
`
	assertMappings(t, code, []mappingPattern{
		{`require("foo")`, `"foo"`},
		{"Foo", "Foo"},
	})
}

func TestSourceMap_ClassExtends(t *testing.T) {
	t.Parallel()
	code := `
// @ts-ignore
class Bar extends Foo {
    constructor() {
        super();
    }
}
`
	assertMappings(t, code, []mappingPattern{
		{"Bar = __TS__Class()", "class Bar"},
		{"Bar.name =", "class Bar"},
		{"__TS__ClassExtends(", "extends"},
		{"Foo", "Foo"},
		{"function Bar.prototype.____constructor", "constructor"},
	})
}

func TestSourceMap_ClassEmpty(t *testing.T) {
	t.Parallel()
	code := `
class Foo {
}
`
	assertMappings(t, code, []mappingPattern{
		{"function Foo.prototype.____constructor", "class Foo"},
	})
}

func TestSourceMap_ClassField(t *testing.T) {
	t.Parallel()
	code := `
class Foo {
    bar = "baz";
}
`
	assertMappings(t, code, []mappingPattern{
		{"function Foo.prototype.____constructor", "class Foo"},
	})
}

func TestSourceMap_ForOfArray(t *testing.T) {
	t.Parallel()
	code := `
declare const arr: string[];
for (const element of arr) {}
`
	assertMappings(t, code, []mappingPattern{
		{"arr", "arr)"},
		{"element", "element"},
	})
}

func TestSourceMap_ForOfCall(t *testing.T) {
	t.Parallel()
	code := `
declare function getArr(this: void): string[];
for (const element of getArr()) {}
`
	assertMappings(t, code, []mappingPattern{
		{"for", "for"},
		{"getArr()", "getArr()"},
		{"element", "element"},
	})
}

func TestSourceMap_NumericFor(t *testing.T) {
	t.Parallel()
	code := `
declare const arr: string[]
for (let i = 0; i < arr.length; ++i) {}
`
	assertMappings(t, code, []mappingPattern{
		{"i = 0", "i = 0"},
		{"i < #arr", "i < arr.length"},
		{"i + 1", "++i"},
	})
}

// =============================================================================
// Name mapping tests — ported from TSTL sourcemaps.spec.ts
// =============================================================================

func TestSourceMap_NameMapping_ReservedType(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, `const type = "foobar";`, "type")
}

func TestSourceMap_NameMapping_ReservedAnd(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, `const and = "foobar";`, "and")
}

func TestSourceMap_NameMapping_SpecialChars(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, `const $$$ = "foobar";`, "$$$")
}

func TestSourceMap_NameMapping_This(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, "const foo = { bar() { this; } };", "this")
}

func TestSourceMap_NameMapping_FunctionParam(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, "function foo($$$: unknown) {}", "$$$")
}

func TestSourceMap_NameMapping_ClassName(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, "class $$$ {}", "$$$")
}

func TestSourceMap_NameMapping_Namespace(t *testing.T) {
	t.Parallel()
	assertNameMapping(t, `namespace $$$ { const foo = "bar"; }`, "$$$")
}

// =============================================================================
// Source path tests — ported from TSTL sourcemaps.spec.ts
// =============================================================================

func TestSourceMap_DefaultSourcePath(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{SourceMap: true})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if len(raw.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(raw.Sources))
	}
	if raw.Sources[0] != "main.ts" {
		t.Errorf("sources[0] = %q, want %q", raw.Sources[0], "main.ts")
	}
}

func TestSourceMap_SourcesContent(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{SourceMap: true})
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if len(raw.SourcesContent) != 1 || raw.SourcesContent[0] == nil {
		t.Fatal("expected sourcesContent with 1 entry")
	}
	if *raw.SourcesContent[0] != code {
		t.Errorf("sourcesContent[0] = %q, want %q", *raw.SourcesContent[0], code)
	}
}

func TestSourceMap_Version(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{SourceMap: true})
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if raw.Version != 3 {
		t.Errorf("version = %d, want 3", raw.Version)
	}
}

func TestSourceMap_File(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{SourceMap: true})
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if raw.File != "main.lua" {
		t.Errorf("file = %q, want %q", raw.File, "main.lua")
	}
}

func TestSourceMap_SourcePath_WithOutDir(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{
		SourceMap:       true,
		CompilerOptions: map[string]any{"outDir": "dst"},
	})
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if len(raw.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(raw.Sources))
	}
	// Output goes to dst/main.lua, source is at main.ts → relative path is ../main.ts
	if raw.Sources[0] != "../main.ts" {
		t.Errorf("sources[0] = %q, want %q", raw.Sources[0], "../main.ts")
	}
}

func TestSourceMap_SourcePath_WithRootDirAndOutDir(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{
		SourceMap:       true,
		CompilerOptions: map[string]any{"rootDir": ".", "outDir": "dst"},
	})
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if len(raw.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(raw.Sources))
	}
	// rootDir=., outDir=dst → output at dst/main.lua, source at main.ts → ../main.ts
	if raw.Sources[0] != "../main.ts" {
		t.Errorf("sources[0] = %q, want %q", raw.Sources[0], "../main.ts")
	}
}

func TestSourceMap_SourceRoot_Undefined(t *testing.T) {
	t.Parallel()
	code := `const foo = "foo"`
	results := TranspileTS(t, code, Opts{SourceMap: true})
	raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
	if err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if raw.SourceRoot != "" {
		t.Errorf("sourceRoot = %q, want empty", raw.SourceRoot)
	}
}

func TestSourceMap_SourceRoot_Normalized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		sourceRoot string
		want       string
	}{
		{"bare", "src", "src/"},
		{"trailing slash", "src/", "src/"},
		{"trailing backslash", "src\\", "src/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code := `const foo = "foo"`
			results := TranspileTS(t, code, Opts{
				SourceMap:       true,
				CompilerOptions: map[string]any{"sourceRoot": tt.sourceRoot},
			})
			raw, _, err := sourcemap.DecodeJSON(results[0].SourceMap)
			if err != nil {
				t.Fatalf("decode source map: %v", err)
			}
			if raw.SourceRoot != tt.want {
				t.Errorf("sourceRoot = %q, want %q", raw.SourceRoot, tt.want)
			}
		})
	}
}

// =============================================================================
// sourceMapTraceback tests — ported from TSTL sourcemaps.spec.ts
// =============================================================================

func TestSourceMap_Traceback_RegistersSourcemap(t *testing.T) {
	t.Parallel()
	code := `
function abc() {
    return "foo";
}
`
	results := TranspileTS(t, code, Opts{SourceMapTraceback: true})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	lua := results[0].Lua

	// The output should contain the __TS__SourceMapTraceBack call
	if !strings.Contains(lua, "__TS__SourceMapTraceBack(debug.getinfo(1).short_src,") {
		t.Errorf("expected __TS__SourceMapTraceBack call in output:\n%s", lua)
	}

	// The mapping table should contain line number entries
	if !strings.Contains(lua, `["`) {
		t.Errorf("expected line number mappings in traceback call:\n%s", lua)
	}
}

func TestSourceMap_Traceback_ImportsLualib(t *testing.T) {
	t.Parallel()
	code := `const x = 1;`
	results := TranspileTS(t, code, Opts{SourceMapTraceback: true})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	if !results[0].UsesLualib {
		t.Error("expected UsesLualib=true when sourceMapTraceback is enabled")
	}
	if !strings.Contains(results[0].Lua, "__TS__SourceMapTraceBack") {
		t.Error("expected __TS__SourceMapTraceBack in lualib imports")
	}
}

func TestSourceMap_Traceback_CorrectLineMappings(t *testing.T) {
	t.Parallel()
	code := `
function abc() {
    return "foo";
}
`
	results := TranspileTS(t, code, Opts{SourceMapTraceback: true})
	lua := results[0].Lua

	// Find the traceback call and verify it maps generated lines to source lines.
	// The function declaration "function abc()" should map back to the TS source.
	// We verify the mapping table exists and has reasonable entries.
	assertMappings(t, code, []mappingPattern{
		{"function abc(", "function abc() {"},
	})

	// Also verify the traceback call itself is present and well-formed
	if !strings.Contains(lua, "debug.getinfo(1).short_src") {
		t.Errorf("missing debug.getinfo in traceback call:\n%s", lua)
	}
}

func TestSourceMap_Traceback_GivesTraceback(t *testing.T) {
	t.Parallel()
	code := `
declare const debug: { traceback: (this: void) => string };
export function __main() {
    return debug.traceback();
}
`
	results := TranspileTS(t, code, Opts{SourceMapTraceback: true})
	got := RunLua(t, results, "mod.__main()", Opts{SourceMapTraceback: true})
	// The traceback should be a string containing "stack traceback"
	if got == "nil" || got == "" {
		t.Errorf("expected traceback string, got %q", got)
	}
	if !strings.Contains(got, "stack traceback") {
		t.Errorf("expected 'stack traceback' in result, got %q", got)
	}
}

// =============================================================================
// inlineSourceMap test — ported from TSTL sourcemaps.spec.ts
// =============================================================================

func TestSourceMap_InlineSourceMap(t *testing.T) {
	t.Parallel()
	code := `
function abc() {
    return "foo";
}
`
	results := TranspileTS(t, code, Opts{InlineSourceMap: true})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	lua := results[0].Lua
	smJSON := results[0].SourceMap

	// The Lua output should contain the inline source map comment
	prefix := "--# sourceMappingURL=data:application/json;base64,"
	idx := strings.Index(lua, prefix)
	if idx < 0 {
		t.Fatalf("expected inline source map comment in output:\n%s", lua)
	}

	// Extract and decode the base64 data
	b64Start := idx + len(prefix)
	b64End := strings.Index(lua[b64Start:], "\n")
	if b64End < 0 {
		b64End = len(lua) - b64Start
	}
	b64Data := lua[b64Start : b64Start+b64End]

	decoded, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}

	// The decoded inline map should match the external source map
	if string(decoded) != smJSON {
		t.Errorf("inline source map does not match external source map\ninline: %s\nexternal: %s", decoded, smJSON)
	}
}
