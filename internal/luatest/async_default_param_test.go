package luatest

import (
	"strings"
	"testing"
)

// TestAsyncDefaultParam verifies that default parameter initialization in async
// functions happens before the async body, not inside it.
// Found via awesome-config wild testing: tslua moved the default check inside
// __TS__AsyncAwaiter, but it should be outside (synchronous evaluation).
func TestAsyncDefaultParam(t *testing.T) {
	t.Parallel()

	code := `export async function test(cmd: string, pidof: string = cmd.split(" ")[0]): Promise<string> { return pidof; }
	export function __main() { }`
	results := TranspileTS(t, code, Opts{})
	lua := results[0].Lua

	// Look for the actual call, not the import
	asyncIdx := strings.Index(lua, "__TS__AsyncAwaiter(function")
	defaultIdx := strings.Index(lua, "pidof == nil")
	if asyncIdx < 0 {
		t.Fatalf("expected __TS__AsyncAwaiter(function in output:\n%s", lua)
	}
	if defaultIdx < 0 {
		t.Fatalf("expected 'pidof == nil' default check in output:\n%s", lua)
	}
	if defaultIdx > asyncIdx {
		t.Errorf("default param check should be BEFORE __TS__AsyncAwaiter (synchronous), but it's inside the async body:\n%s", lua)
	}
}
