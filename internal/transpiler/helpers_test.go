package transpiler

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/lua"
)

func TestLuaSafeName(t *testing.T) {
	// luaSafeName is only called on names known to be unsafe (after hasUnsafeIdentifierName check).
	// It always prepends ____ and hex-encodes non-alphanumeric chars.
	tests := []struct {
		input string
		want  string
	}{
		{"and", "____and"},
		{"end", "____end"},
		{"local", "____local"},
		{"nil", "____nil"},
		{"return", "____return"},
		{"not", "____not"},
		{"repeat", "____repeat"},
		{"goto", "____goto"},
		{"$$$", "_____24_24_24"},
		{"a$b", "____a_24b"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := luaSafeName(tt.input)
			if got != tt.want {
				t.Errorf("luaSafeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsLuaKeyword(t *testing.T) {
	keywords := []string{
		"and", "break", "do", "else", "elseif", "end",
		"false", "for", "function", "goto", "if", "in",
		"local", "nil", "not", "or", "repeat", "return",
		"then", "true", "until", "while",
	}
	for _, kw := range keywords {
		if !isLuaKeyword(kw) {
			t.Errorf("isLuaKeyword(%q) = false, want true", kw)
		}
	}

	nonKeywords := []string{"print", "self", "And", "END", "class", "var", "let", "const", ""}
	for _, nk := range nonKeywords {
		if isLuaKeyword(nk) {
			t.Errorf("isLuaKeyword(%q) = true, want false", nk)
		}
	}
}

func TestSafeModuleVarName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "module", "____module"},
		{"nested path", "src/utils/helper", "____helper"},
		{"kebab case", "kebab-module", "____kebab_2Dmodule"},
		{"with dot", "file.ext", "____file_2Eext"},
		{"with at", "@scope/pkg", "____pkg"},
		{"quoted", `"module"`, "____module"},
		{"digits", "lib2", "____lib2"},
		{"underscores", "__internal", "______internal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeModuleVarName(tt.input)
			if got != tt.want {
				t.Errorf("safeModuleVarName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatConstantValue(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   string
		wantOK bool
	}{
		{"string", "hello", `"hello"`, true},
		{"string with quotes", `say "hi"`, `"say \"hi\""`, true},
		{"integer float", 42.0, "42", true},
		{"fractional float", 3.14, "3.14", true},
		{"zero", 0.0, "0", true},
		{"negative int", -1.0, "-1", true},
		{"negative float", -2.5, "-2.5", true},
		{"large int", 1000000.0, "1000000", true},
		{"bool (unsupported type)", true, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := formatConstantValue(tt.input)
			if ok != tt.wantOK {
				t.Errorf("formatConstantValue(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			gotStr := ""
			if got != nil {
				gotStr = lua.PrintExpression(got)
			}
			if gotStr != tt.want {
				t.Errorf("formatConstantValue(%v) = %q, want %q", tt.input, gotStr, tt.want)
			}
		})
	}
}

func TestIsLuaGlobal(t *testing.T) {
	globals := []string{
		"print", "tonumber", "tostring", "type", "error",
		"pcall", "xpcall", "require", "pairs", "ipairs",
		"setmetatable", "getmetatable", "select", "unpack", "assert",
	}
	for _, g := range globals {
		if !isLuaGlobal(g) {
			t.Errorf("isLuaGlobal(%q) = false, want true", g)
		}
	}

	nonGlobals := []string{"foo", "math", "string", "table", "os", "io", "self", ""}
	for _, ng := range nonGlobals {
		if isLuaGlobal(ng) {
			t.Errorf("isLuaGlobal(%q) = true, want false", ng)
		}
	}
}
