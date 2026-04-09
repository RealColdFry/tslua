package luatest

import (
	"strings"
	"testing"
)

// TestRawRequirePathNormalization verifies that raw require() calls in TS source
// get their paths normalized to Lua dot-separated format.
// Found via dota2bot wild testing: require("bots/ts_libs/utils/json") in TS
// should become require("bots.ts_libs.utils.json") in Lua output, since Lua's
// require uses dots as path separators.
func TestRawRequirePathNormalization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		code       string
		wantInLua  string // expected in output
		dontWant   string // should NOT appear
	}{
		{
			"raw require with slashes becomes dots",
			`declare function require(mod: string): any;
			require("bots/ts_libs/utils/json");
			export function __main() {}`,
			`require("bots.ts_libs.utils.json")`,
			`require("bots/ts_libs/utils/json")`,
		},
		{
			"raw require assigned to local",
			`declare function require(mod: string): any;
			const jmz = require("bots/FunLib/jmz_func");
			export function __main() { return jmz; }`,
			`require("bots.FunLib.jmz_func")`,
			`require("bots/FunLib/jmz_func")`,
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
