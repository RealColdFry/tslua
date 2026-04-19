package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestTry_BreakContinueAcrossPcall covers break/continue statements inside a
// try/catch/finally body. The transpiler wraps the try body in a pcall(function()
// ... end), so a direct break or goto would cross the Lua function boundary.
// The fix is a sentinel-flag assignment plus return, with dispatch after the
// pcall returns and finally has run.
//
// Regression:
//   - https://github.com/RealColdFry/tslua/issues/87
//   - https://github.com/RealColdFry/tslua/issues/92
func TestTry_BreakContinueAcrossPcall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		body     string
		want     string
		needGoto bool
	}{
		{
			"unlabeled continue inside try/finally",
			`const log: number[] = [];
			for (let i = 0; i < 10; i++) {
				try {
					if (i === 2) { i = 7; continue; }
				} finally {}
				log.push(i);
			}
			return log;`,
			`{0, 1, 8, 9}`,
			false,
		},
		{
			"unlabeled break inside try/catch",
			`const log: number[] = [];
			for (let i = 0; i < 10; i++) {
				try {
					if (i === 3) break;
				} catch {}
				log.push(i);
			}
			return log;`,
			`{0, 1, 2}`,
			false,
		},
		{
			"continue inside try/finally runs finally",
			`const log: number[] = [];
			for (let i = 0; i < 3; i++) {
				try {
					if (i === 1) continue;
					log.push(i);
				} finally {
					log.push(100 + i);
				}
			}
			return log;`,
			`{0, 100, 101, 2, 102}`,
			false,
		},
		{
			"labeled break inside try targets outer loop",
			`const log: number[] = [];
			outer: for (let i = 0; i < 5; i++) {
				for (let j = 0; j < 5; j++) {
					try {
						if (j === 1 && i === 2) break outer;
					} finally {}
					log.push(i * 10 + j);
				}
			}
			return log;`,
			`{0, 1, 2, 3, 4, 10, 11, 12, 13, 14, 20}`,
			true,
		},
		{
			"labeled continue inside try targets outer loop",
			`const log: number[] = [];
			outer: for (let i = 0; i < 3; i++) {
				for (let j = 0; j < 3; j++) {
					try {
						if (j === 1) continue outer;
					} finally {}
					log.push(i * 10 + j);
				}
			}
			return log;`,
			`{0, 10, 20}`,
			true,
		},
		{
			"break inside try inside inner loop only exits inner",
			`const log: number[] = [];
			for (let i = 0; i < 3; i++) {
				for (let j = 0; j < 5; j++) {
					try {
						if (j === 2) break;
					} finally {}
					log.push(i * 10 + j);
				}
			}
			return log;`,
			`{0, 1, 10, 11, 20, 21}`,
			false,
		},
	}

	targets := []struct {
		name   string
		target transpiler.LuaTarget
	}{
		{"LuaJIT", transpiler.LuaTargetLuaJIT},
		{"5.1", transpiler.LuaTargetLua51},
		{"5.2", transpiler.LuaTargetLua52},
		{"5.3", transpiler.LuaTargetLua53},
		{"5.4", transpiler.LuaTargetLua54},
		{"5.5", transpiler.LuaTargetLua55},
	}

	for _, c := range cases {
		for _, tgt := range targets {
			if c.needGoto && !tgt.target.SupportsGoto() {
				continue
			}
			t.Run(c.name+" ["+tgt.name+"]", func(t *testing.T) {
				t.Parallel()
				ExpectFunction(t, c.body, c.want, Opts{LuaTarget: tgt.target})
			})
		}
	}
}

// TestAsyncTry_LabeledBreakContinue covers labeled break/continue inside an
// async try body. Previously the labeled branch short-circuited before the
// try-scope check, emitting a goto that crossed the __TS__AsyncAwaiter function
// boundary. See https://github.com/RealColdFry/tslua/issues/92.
func TestAsyncTry_LabeledBreakContinue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"labeled break outer inside async try",
			`const log: number[] = [];
			async function run() {
				outer: for (let i = 0; i < 3; i++) {
					try {
						await Promise.resolve();
						if (i === 1) break outer;
						log.push(i);
					} catch {}
				}
			}
			run();
			return log;`,
			`{0}`,
		},
		{
			"labeled continue outer inside async try",
			`const log: number[] = [];
			async function run() {
				outer: for (let i = 0; i < 3; i++) {
					for (let j = 0; j < 3; j++) {
						try {
							await Promise.resolve();
							if (j === 1) continue outer;
							log.push(i * 10 + j);
						} catch {}
					}
				}
			}
			run();
			return log;`,
			`{0, 10, 20}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, c.body, c.want, Opts{LuaTarget: transpiler.LuaTargetLuaJIT})
		})
	}
}
