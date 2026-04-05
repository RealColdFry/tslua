package luatest

import "testing"

// TestPrivateFieldsNoPanic verifies that private class fields (#x) don't
// panic during transpilation (they emit diagnostics instead).
func TestPrivateFieldsNoPanic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
	}{
		{
			"private field read",
			`class Foo { #x: number = 5; getX(): number { return this.#x; } }`,
		},
		{
			"private field write",
			`class A { #x: number = 0; setX(v: number): void { this.#x = v; } }`,
		},
		{
			"private field read and write",
			`class A {
				#x: number = 0;
				setX(v: number): void { this.#x = v; }
				getX(): number { return this.#x; }
			}`,
		},
		{
			"private method",
			`class A { #doStuff(): number { return 42; } run(): number { return this.#doStuff(); } }`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Should not panic — emits diagnostics instead
			TranspileTS(t, tc.code, Opts{})
		})
	}
}
