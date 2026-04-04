package luatest

import (
	"strings"
	"testing"
)

func TestJSON_BasicTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		json    string
		wantLua string
	}{
		{"number", `0`, "return 0\n"},
		{"string", `""`, `return ""` + "\n"},
		{"empty array", `[]`, "return {}\n"},
		{"array", `[1, "2", []]`, "return {1, \"2\", {}}\n"},
		{"object", `{"a": "b"}`, "return {a = \"b\"}\n"},
		{"nested object", `{"a": {"b": "c"}}`, "return {a = {b = \"c\"}}\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, `import * as data from "./data.json"; export const result = data;`, Opts{
				MainFileName: "main.ts",
				ExtraFiles:   map[string]string{"data.json": tc.json},
				CompilerOptions: map[string]any{
					"resolveJsonModule": true,
					"esModuleInterop":   true,
				},
			})
			// Find the data.json result
			for _, r := range results {
				if strings.HasSuffix(r.FileName, "data.lua") || strings.HasSuffix(r.FileName, "data.json") {
					if r.Lua != tc.wantLua {
						t.Errorf("JSON %s:\ngot:  %q\nwant: %q", tc.name, r.Lua, tc.wantLua)
					}
					return
				}
			}
			t.Errorf("no data.json result found in %d results", len(results))
		})
	}
}

func TestJSON_EmptyFile(t *testing.T) {
	t.Parallel()

	results := TranspileTS(t, `import * as data from "./data.json"; export const result = data;`, Opts{
		MainFileName: "main.ts",
		ExtraFiles:   map[string]string{"data.json": ""},
		CompilerOptions: map[string]any{
			"resolveJsonModule": true,
			"esModuleInterop":   true,
		},
	})
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "data.lua") || strings.HasSuffix(r.FileName, "data.json") {
			if !strings.Contains(r.Lua, `error("Unexpected end of JSON input")`) {
				t.Errorf("expected error call for empty JSON, got: %q", r.Lua)
			}
			return
		}
	}
	t.Error("no data.json result found")
}

func TestShebang_Preserved(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
	}{
		{"LF", "#!/usr/bin/env lua\nconst foo = true;\n"},
		{"CRLF", "#!/usr/bin/env lua\r\nconst foo = true;\r\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, Opts{})
			if len(results) == 0 {
				t.Fatal("no results")
			}
			lua := results[0].Lua
			if !strings.HasPrefix(lua, "#!/usr/bin/env lua") {
				t.Errorf("shebang not preserved, got: %q", lua[:min(len(lua), 80)])
			}
		})
	}
}
