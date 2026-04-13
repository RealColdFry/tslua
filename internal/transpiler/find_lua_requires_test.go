package transpiler

import (
	"testing"
)

func TestFindLuaRequires(t *testing.T) {
	tests := []struct {
		name string
		lua  string
		want []LuaRequire
	}{
		{
			name: "simple require with double quotes",
			lua:  `require("foo")`,
			want: []LuaRequire{{From: 0, To: 13, RequirePath: "foo"}},
		},
		{
			name: "simple require with single quotes",
			lua:  `require('bar')`,
			want: []LuaRequire{{From: 0, To: 13, RequirePath: "bar"}},
		},
		{
			name: "require without parentheses",
			lua:  `require "baz"`,
			want: []LuaRequire{{From: 0, To: 12, RequirePath: "baz"}},
		},
		{
			name: "require with spaces inside parens",
			lua:  `require( "spaced" )`,
			want: []LuaRequire{{From: 0, To: 18, RequirePath: "spaced"}},
		},
		{
			name: "local assignment",
			lua:  `local x = require("mod")`,
			want: []LuaRequire{{From: 10, To: 23, RequirePath: "mod"}},
		},
		{
			name: "multiple requires",
			lua:  "require(\"a\")\nrequire(\"b\")",
			want: []LuaRequire{
				{From: 0, To: 11, RequirePath: "a"},
				{From: 13, To: 24, RequirePath: "b"},
			},
		},
		{
			name: "require in single-line comment is skipped",
			lua:  `-- require("skipped")`,
		},
		{
			name: "require in multi-line comment is skipped",
			lua:  `--[[ require("skipped") ]]`,
		},
		{
			name: "require in string is skipped",
			lua:  `local s = "require('skipped')"`,
		},
		{
			name: "require after bracket",
			lua:  `x[require("idx")]`,
			want: []LuaRequire{{From: 2, To: 15, RequirePath: "idx"}},
		},
		{
			name: "require after open paren",
			lua:  `f(require("arg"))`,
			want: []LuaRequire{{From: 2, To: 15, RequirePath: "arg"}},
		},
		{
			name: "dotted path",
			lua:  `require("a.b.c")`,
			want: []LuaRequire{{From: 0, To: 15, RequirePath: "a.b.c"}},
		},
		{
			name: "not a require (different identifier)",
			lua:  `required("nope")`,
		},
		{
			name: "require at start after comment",
			lua:  "-- comment\nrequire(\"mod\")",
			want: []LuaRequire{{From: 11, To: 24, RequirePath: "mod"}},
		},
		{
			name: "empty input",
			lua:  "",
		},
		{
			name: "lualib_bundle require",
			lua:  `local ____lualib = require("lualib_bundle")`,
			want: []LuaRequire{{From: 19, To: 42, RequirePath: "lualib_bundle"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindLuaRequires(tt.lua)
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d requires, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("require[%d]: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
