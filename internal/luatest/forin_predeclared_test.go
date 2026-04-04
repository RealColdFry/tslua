// Tests that for...in with a pre-declared variable assigns to the outer variable.
package luatest

import (
	"testing"
)

func TestForIn_PreDeclaredVariableKeepsLastValue(t *testing.T) {
	t.Parallel()

	tsCode := `export function __main() {
		const obj = { x: "y", foo: "bar" };
		let x = "";
		for (x in obj) {}
		return x;
	}`
	results := TranspileTS(t, tsCode, Opts{})
	got := RunLua(t, results, "mod.__main()", Opts{})

	// Order is not guaranteed in for...in, but x must be one of the keys
	if got != `"x"` && got != `"foo"` {
		t.Errorf("expected x to be \"x\" or \"foo\", got %s", got)
	}
}

func TestForIn_PreDeclaredVariable(t *testing.T) {
	t.Parallel()

	tsCode := `export function __main() {
		const obj = { x: "y", foo: "bar" };
		let result: string[] = [];
		let x = "";
		for (x in obj) {
			result.push(x);
		}
		return result;
	}`
	results := TranspileTS(t, tsCode, Opts{})
	got := RunLua(t, results, "mod.__main()", Opts{})

	// Order is not guaranteed, but both keys must appear
	if got != `{"foo", "x"}` && got != `{"x", "foo"}` {
		t.Errorf("expected both keys, got %s", got)
	}
}
