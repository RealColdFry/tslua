package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestTry_FinallyRunsWhenCatchRethrows verifies that the `finally` block runs
// even when its sibling `catch` rethrows. ECMAScript guarantees finally always
// runs after catch, including when catch itself throws.
//
// Regression: https://github.com/RealColdFry/tslua/issues/93
func TestTry_FinallyRunsWhenCatchRethrows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"catch rethrows, finally still runs",
			`const log: string[] = [];
			try {
				try {
					throw "inner";
				} catch {
					throw "rethrown";
				} finally {
					log.push("finally");
				}
			} catch (e) {
				log.push("outer:" + (e as string));
			}
			return log;`,
			`{"finally", "outer:rethrown"}`,
		},
		{
			"catch throws new error, finally runs before outer catch",
			`const log: string[] = [];
			try {
				try {
					throw "inner";
				} catch {
					log.push("caught");
					throw "different";
				} finally {
					log.push("finally");
				}
			} catch (e) {
				log.push("outer:" + (e as string));
			}
			return log;`,
			`{"caught", "finally", "outer:different"}`,
		},
		{
			// ECMA: a return in finally suppresses any in-flight exception
			// from catch. The outer try/catch must NOT see the rethrown error.
			"return in finally suppresses rethrown error",
			`const log: string[] = [];
			function inner(): string {
				try {
					throw "inner";
				} catch {
					throw "rethrown";
				} finally {
					log.push("finally");
					return "finally-return";
				}
			}
			let result: string;
			try {
				result = inner();
			} catch (e) {
				result = "outer:" + (e as string);
			}
			log.push("result:" + result);
			return log;`,
			`{"finally", "result:finally-return"}`,
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
			t.Run(c.name+" ["+tgt.name+"]", func(t *testing.T) {
				t.Parallel()
				ExpectFunction(t, c.body, c.want, Opts{LuaTarget: tgt.target})
			})
		}
	}
}
