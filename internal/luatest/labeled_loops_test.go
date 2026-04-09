package luatest

import (
	"strings"
	"testing"
)

func TestLabeledBreak(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"labeled break exits outer loop",
			`let result = "";
			outer: for (let i = 0; i < 3; i++) {
				for (let j = 0; j < 3; j++) {
					if (j === 1) break outer;
					result += i + "," + j + " ";
				}
			}
			return result.trim();`,
			`"0,0"`,
		},
		{
			"labeled break on while",
			`let count = 0;
			outer: while (true) {
				let inner = 0;
				while (true) {
					inner++;
					if (inner > 2) break outer;
				}
				count++;
			}
			return count;`,
			`0`,
		},
		{
			"labeled break skips else branch",
			`let result = 0;
			outer: for (let i = 0; i < 5; i++) {
				for (let j = 0; j < 5; j++) {
					if (i * j > 6) break outer;
					result += 1;
				}
			}
			return result;`,
			`14`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

func TestLabeledContinue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"labeled continue skips to outer loop",
			`let result = "";
			outer: for (let i = 0; i < 3; i++) {
				for (let j = 0; j < 3; j++) {
					if (j === 1) continue outer;
					result += i + "," + j + " ";
				}
			}
			return result.trim();`,
			`"0,0 1,0 2,0"`,
		},
		{
			"labeled continue on while loop",
			`let sum = 0;
			let i = 0;
			outer: while (i < 3) {
				i++;
				let j = 0;
				while (j < 3) {
					j++;
					if (j === 2) continue outer;
					sum += j;
				}
			}
			return sum;`,
			`3`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ExpectFunction(t, tc.body, tc.want, Opts{})
		})
	}
}

func TestLabeledStatement_NoBreakOrContinue(t *testing.T) {
	t.Parallel()
	// Label that isn't targeted by break/continue should just emit the inner statement
	ExpectFunction(t, `
		let x = 0;
		myLabel: for (let i = 0; i < 3; i++) {
			x += i;
		}
		return x;
	`, `3`, Opts{})
}

func TestLabeledStatement_Codegen(t *testing.T) {
	t.Parallel()
	// Verify labeled break emits goto labels
	code := `export function __main() {
		let result = 0;
		outer: for (let i = 0; i < 3; i++) {
			for (let j = 0; j < 3; j++) {
				if (j === 1) break outer;
				result += 1;
			}
		}
		return result;
	}`
	results := TranspileTS(t, code, Opts{})
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			if !strings.Contains(r.Lua, "::__break_outer::") {
				t.Errorf("expected goto break label in output, got:\n%s", r.Lua)
			}
			if !strings.Contains(r.Lua, "goto __break_outer") {
				t.Errorf("expected goto statement in output, got:\n%s", r.Lua)
			}
		}
	}
}
