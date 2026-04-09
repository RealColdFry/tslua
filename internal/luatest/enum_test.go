package luatest

import (
	"strings"
	"testing"
)

func TestEnum_LocalInit(t *testing.T) {
	t.Parallel()

	t.Run("namespace-nested enum", func(t *testing.T) {
		t.Parallel()
		tsCode := `
			namespace A {
				export enum TestEnum { B, C }
			}
			export function __main() {
				return [A.TestEnum.B, A.TestEnum.C, A.TestEnum[0], A.TestEnum[1]];
			}`
		results := TranspileTS(t, tsCode, Opts{})
		got := RunLua(t, results, "mod.__main()", Opts{})
		if got != "{0, 1, \"B\", \"C\"}" {
			t.Errorf("got %s, want {0, 1, \"B\", \"C\"}", got)
		}
		// Verify we emit local init (no global leak) for same-file enum
		for _, r := range results {
			if strings.HasSuffix(r.FileName, "main.ts") {
				if strings.Contains(r.Lua, "A.TestEnum = A.TestEnum or") {
					t.Error("expected plain {} init for same-file namespace enum, got or ({}) pattern")
				}
				if !strings.Contains(r.Lua, "A.TestEnum = {}") {
					t.Errorf("expected A.TestEnum = {} in output:\n%s", r.Lua)
				}
			}
		}
	})

	t.Run("same-file merging is local", func(t *testing.T) {
		t.Parallel()
		tsCode := `
			enum Foo { A = 10 }
			enum Foo { B = 20 }
			export function __main() {
				return [Foo.A, Foo.B, Foo[10], Foo[20]];
			}`
		results := TranspileTS(t, tsCode, Opts{})
		got := RunLua(t, results, "mod.__main()", Opts{})
		if got != "{10, 20, \"A\", \"B\"}" {
			t.Errorf("got %s, want {10, 20, \"A\", \"B\"}", got)
		}
		for _, r := range results {
			if strings.HasSuffix(r.FileName, "main.ts") {
				if !strings.Contains(r.Lua, "local Foo = {}") {
					t.Errorf("expected local Foo = {} in output:\n%s", r.Lua)
				}
			}
		}
	})

	t.Run("cross-file merging uses global", func(t *testing.T) {
		t.Parallel()
		tsCode := `
			import "./other"
			enum Shared { A = 1, B = 2 }
			export function __main() {
				return [Shared.A, Shared.B, Shared.C, Shared.D];
			}`
		extra := map[string]string{
			"other.ts": `enum Shared { C = 3, D = 4 }`,
		}
		results := TranspileTS(t, tsCode, Opts{ExtraFiles: extra})
		got := RunLua(t, results, "mod.__main()", Opts{})
		if got != "{1, 2, 3, 4}" {
			t.Errorf("got %s, want {1, 2, 3, 4}", got)
		}
	})
}

// TestCompileMembersOnly_DeclareEnum verifies that @compileMembersOnly on a
// declare enum emits bare member names (globals), not ____exports.MEMBER.
// Found via dota2bot wild testing: Dota engine globals like BOT_ACTION_DESIRE_NONE
// were incorrectly prefixed with ____exports.
func TestCompileMembersOnly_DeclareEnum(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		code      string
		wantInLua string
		dontWant  string
	}{
		{
			"declare enum members are bare globals",
			"/** @compileMembersOnly */\n" +
				"export declare enum Globals { FOO, BAR }\n" +
				"export const x = Globals.FOO;\n",
			"= FOO",
			"____exports.FOO",
		},
		{
			"declare enum used as value in another enum",
			"/** @compileMembersOnly */\n" +
				"export declare enum Globals { FOO }\n" +
				"export enum MyEnum { A = Globals.FOO }\n",
			"= FOO",
			"____exports.FOO",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.code, Opts{})
			lua := results[0].Lua
			if !strings.Contains(lua, tc.wantInLua) {
				t.Errorf("expected %q in output\ngot:\n%s", tc.wantInLua, lua)
			}
			if tc.dontWant != "" && strings.Contains(lua, tc.dontWant) {
				t.Errorf("did not expect %q in output\ngot:\n%s", tc.dontWant, lua)
			}
		})
	}
}
