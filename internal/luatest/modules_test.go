package luatest

import (
	"strings"
	"testing"
)

func TestModules_SameDirectoryImportPath(t *testing.T) {
	t.Parallel()

	// Regression: same-directory imports were emitted as bare require("types")
	// instead of the full path require("sub.types"). This caused module-not-found
	// errors when main.ts and the imported file were in the same directory but
	// the Lua package.path only matched fully-qualified module names.
	cases := []struct {
		name       string
		mainCode   string
		extra      map[string]string
		wantResult string
		wantReq    string // expected require path substring
		badReq     string // require path that should NOT appear
	}{
		{
			name:     "sibling in subdirectory",
			mainCode: `import { value } from "./sub/helper"; export const result = value;`,
			extra: map[string]string{
				"sub/helper.ts": `export const value = 42;`,
			},
			wantResult: "42",
			wantReq:    `require("sub.helper")`,
			badReq:     `require("helper")`,
		},
		{
			name:     "deep sibling same directory",
			mainCode: `import { greet } from "./sub/utils"; export const result = greet();`,
			extra: map[string]string{
				"sub/utils.ts": `import { name } from "./types"; export function greet() { return "hello " + name; }`,
				"sub/types.ts": `export const name = "world";`,
			},
			wantResult: `"hello world"`,
			wantReq:    `require("sub.types")`,
			badReq:     `require("types")`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results := TranspileTS(t, tc.mainCode, Opts{ExtraFiles: tc.extra})

			got := RunLua(t, results, "mod.result", Opts{})
			if got != tc.wantResult {
				t.Errorf("got %s, want %s", got, tc.wantResult)
			}

			// Verify require paths in all emitted files
			for _, r := range results {
				if tc.badReq != "" && strings.Contains(r.Lua, tc.badReq) {
					t.Errorf("%s: should not contain %s, got:\n%s", r.FileName, tc.badReq, r.Lua)
				}
			}
		})
	}
}
