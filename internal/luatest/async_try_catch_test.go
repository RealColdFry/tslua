package luatest

import (
	"testing"

	"github.com/realcoldfry/tslua/internal/transpiler"
)

// TestAsyncTryCatch_AwaitInCatchFinally verifies that await works correctly
// inside catch and finally blocks of async functions.
// See: https://github.com/TypeScriptToLua/TypeScriptToLua/issues/1659
func TestAsyncTryCatch_AwaitInCatchFinally(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"await in catch block",
			`const log: string[] = [];
			async function run() {
				try {
					throw "oops";
				} catch (e) {
					log.push("catch");
					const a = await Promise.resolve("resolved");
					log.push(a);
				}
			}
			run();
			return log;`,
			`{"catch", "resolved"}`,
		},
		{
			"await in finally block",
			`const log: string[] = [];
			async function run() {
				try {
					log.push("try");
				} finally {
					log.push("finally");
					const a = await Promise.resolve("resolved");
					log.push(a);
				}
			}
			run();
			return log;`,
			`{"try", "finally", "resolved"}`,
		},
		{
			"await in both catch and finally",
			`const log: string[] = [];
			async function run() {
				try {
					throw "oops";
				} catch (e) {
					log.push("catch");
					const a = await Promise.resolve("c-resolved");
					log.push(a);
				} finally {
					log.push("finally");
					const b = await Promise.resolve("f-resolved");
					log.push(b);
				}
			}
			run();
			return log;`,
			`{"catch", "c-resolved", "finally", "f-resolved"}`,
		},
		{
			"rejected promise triggers catch with await",
			`const log: string[] = [];
			let promiseReject: any = undefined;
			async function inner() {
				return new Promise((resolve, reject) => {
					promiseReject = reject;
				});
			}
			async function run() {
				try {
					await inner();
				} catch (e) {
					log.push("catch");
					const a = await Promise.resolve(true);
					log.push(String(a));
				} finally {
					log.push("finally");
					const a = await Promise.resolve(true);
					log.push(String(a));
				}
			}
			run();
			if (promiseReject) { promiseReject(); }
			return log;`,
			`{"catch", "true", "finally", "true"}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, c.body, c.want, Opts{
				LuaTarget: transpiler.LuaTargetLuaJIT,
			})
		})
	}
}
