// Tests for .d.ts declaration file emission.
package luatest

import (
	"strings"
	"testing"
)

func TestDtsEmission_NoSelfInFile(t *testing.T) {
	t.Parallel()
	tsCode := `export function bar() {}`
	results := TranspileTS(t, tsCode, Opts{
		CompilerOptions: map[string]any{
			"declaration": true,
		},
		NoImplicitSelf: true,
	})
	if len(results) == 0 {
		t.Fatal("no transpile results")
	}
	r := results[0]
	if r.Declaration == "" {
		t.Fatal("expected .d.ts declaration output, got empty string")
	}
	if !strings.Contains(r.Declaration, "@noSelfInFile") {
		t.Errorf("declaration should contain @noSelfInFile, got:\n%s", r.Declaration)
	}
}
