package transpiler

import (
	"testing"
)

func TestScanLuaForLualibDeps(t *testing.T) {
	tests := []struct {
		name string
		lua  string
		want []string
	}{
		{
			name: "single lualib reference",
			lua:  "local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf",
			want: []string{"__TS__ArrayIndexOf"},
		},
		{
			name: "multiple lualib references",
			lua: "local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf\n" +
				"local __TS__ArrayConcat = ____lualib.__TS__ArrayConcat",
			want: []string{"__TS__ArrayIndexOf", "__TS__ArrayConcat"},
		},
		{
			name: "mismatched local name is ignored",
			lua:  "local myAlias = ____lualib.__TS__ArrayIndexOf",
			want: nil,
		},
		{
			name: "no lualib references",
			lua:  "local x = 42\nprint(x)",
			want: nil,
		},
		{
			name: "lualib in comment is not matched",
			lua:  "-- local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf",
			want: nil,
		},
		{
			name: "full file with require and usage",
			lua: "local ____lualib = require(\"lualib_bundle\")\n" +
				"local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf\n" +
				"__TS__ArrayIndexOf({}, 1)\n",
			want: []string{"__TS__ArrayIndexOf"},
		},
		{
			name: "indented line does not match",
			lua:  "  local __TS__ArrayIndexOf = ____lualib.__TS__ArrayIndexOf",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanLuaForLualibDeps(tt.lua)
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d deps, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("dep[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
